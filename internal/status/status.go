package status

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/galq/blackflame/internal/config"
	"github.com/galq/blackflame/internal/events"
	"github.com/galq/blackflame/internal/metrics"
)

type Result struct {
	Key        string     `json:"key"`
	Up         bool       `json:"up"`
	StatusCode int        `json:"statusCode,omitempty"`
	LatencyMS  int64      `json:"latencyMs"`
	CertExpiry *time.Time `json:"certExpiry,omitempty"`
	Error      string     `json:"error,omitempty"`
	CheckedAt  time.Time  `json:"checkedAt"`
}

type Manager struct {
	db       *sql.DB
	hub      *events.Hub
	logger   *slog.Logger
	metrics  *metrics.Registry
	provider func() *config.Config
	client   *http.Client

	mu        sync.RWMutex
	results   map[string]Result
	lastCheck map[string]time.Time
	running   atomic.Bool
}

func New(db *sql.DB, hub *events.Hub, logger *slog.Logger, registry *metrics.Registry, provider func() *config.Config) *Manager {
	return &Manager{
		db:       db,
		hub:      hub,
		logger:   logger,
		metrics:  registry,
		provider: provider,
		client: &http.Client{
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				MaxIdleConns:          64,
				MaxIdleConnsPerHost:   8,
				IdleConnTimeout:       60 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 10 * time.Second,
			},
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return http.ErrUseLastResponse
				}
				if blockedTarget(req.URL.String()) {
					return fmt.Errorf("redirect to a blocked cloud metadata endpoint")
				}
				return nil
			},
		},
		results:   map[string]Result{},
		lastCheck: map[string]time.Time{},
	}
}

func (m *Manager) Start(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	m.checkDue(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.checkDue(ctx)
		}
	}
}

func (m *Manager) Results() map[string]Result {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]Result, len(m.results))
	for key, result := range m.results {
		out[key] = result
	}
	return out
}

func (m *Manager) ResultsFor(cfg *config.Config) map[string]Result {
	if cfg == nil {
		return map[string]Result{}
	}
	keys := map[string]struct{}{}
	for _, board := range cfg.Boards {
		for _, section := range board.Sections {
			for _, item := range section.Items {
				keys[item.Key(board.Slug)] = struct{}{}
			}
		}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := map[string]Result{}
	for key := range keys {
		if result, ok := m.results[key]; ok {
			out[key] = result
		}
	}
	return out
}

func (m *Manager) checkDue(ctx context.Context) {
	if !m.running.CompareAndSwap(false, true) {
		return
	}
	defer m.running.Store(false)

	cfg := m.provider()
	if cfg == nil || !cfg.Status.Enabled {
		return
	}
	interval := time.Duration(cfg.Status.IntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 60 * time.Second
	}
	now := time.Now()
	minStatus, maxStatus, statusErr := config.ParseStatusRange(cfg.Status.ExpectedStatus)
	if statusErr != nil {
		minStatus, maxStatus = 200, 399
	}
	type target struct {
		key     string
		url     string
		timeout time.Duration
		jitter  time.Duration
	}
	var targets []target
	m.mu.Lock()
	for _, board := range cfg.Boards {
		for _, section := range board.Sections {
			for _, item := range section.Items {
				if !item.Checkable() {
					continue
				}
				key := item.Key(board.Slug)
				if last, ok := m.lastCheck[key]; ok && now.Sub(last) < interval {
					continue
				}
				m.lastCheck[key] = now
				targets = append(targets, target{
					key:     key,
					url:     item.URL,
					timeout: time.Duration(cfg.Status.TimeoutSeconds) * time.Second,
					jitter:  time.Duration(len(targets)%10) * 500 * time.Millisecond,
				})
			}
		}
	}
	m.mu.Unlock()

	if len(targets) == 0 {
		return
	}
	concurrency := cfg.Status.MaxConcurrency
	if concurrency < 1 {
		concurrency = 16
	}
	semaphore := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for _, target := range targets {
		target := target
		wg.Add(1)
		go func() {
			defer wg.Done()
			if target.jitter > 0 {
				select {
				case <-time.After(target.jitter):
				case <-ctx.Done():
					return
				}
			}
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				return
			}
			m.checkOne(ctx, target.key, target.url, target.timeout, minStatus, maxStatus)
		}()
	}
	wg.Wait()
}

func (m *Manager) checkOne(ctx context.Context, key, rawURL string, timeout time.Duration, minStatus, maxStatus int) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if blockedTarget(rawURL) {
		m.record(ctx, Result{Key: key, Up: false, Error: "target is a blocked cloud metadata endpoint", CheckedAt: time.Now().UTC()})
		return
	}

	start := time.Now()
	req, err := http.NewRequestWithContext(checkCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		m.record(ctx, Result{Key: key, Up: false, Error: err.Error(), CheckedAt: time.Now().UTC()})
		return
	}
	req.Header.Set("User-Agent", "Blackflame/0.1")
	resp, err := m.client.Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		m.record(ctx, Result{Key: key, Up: false, LatencyMS: latency, Error: err.Error(), CheckedAt: time.Now().UTC()})
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	result := Result{
		Key:        key,
		Up:         resp.StatusCode >= minStatus && resp.StatusCode <= maxStatus,
		StatusCode: resp.StatusCode,
		LatencyMS:  latency,
		CheckedAt:  time.Now().UTC(),
	}
	if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		expiry := resp.TLS.PeerCertificates[0].NotAfter.UTC()
		result.CertExpiry = &expiry
	}
	m.record(ctx, result)
}

func (m *Manager) record(ctx context.Context, result Result) {
	m.mu.Lock()
	m.results[result.Key] = result
	m.mu.Unlock()

	var certExpiry any
	if result.CertExpiry != nil {
		certExpiry = result.CertExpiry.Format(time.RFC3339)
	}
	var statusCode any
	if result.StatusCode != 0 {
		statusCode = result.StatusCode
	}
	_, err := m.db.ExecContext(ctx, `
		INSERT INTO status_latest(item_key, up, status_code, latency_ms, cert_expiry, error, checked_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(item_key) DO UPDATE SET
			up = excluded.up,
			status_code = excluded.status_code,
			latency_ms = excluded.latency_ms,
			cert_expiry = excluded.cert_expiry,
			error = excluded.error,
			checked_at = excluded.checked_at`,
		result.Key, boolInt(result.Up), statusCode, result.LatencyMS, certExpiry, nullString(result.Error), result.CheckedAt.Format(time.RFC3339),
	)
	if err != nil {
		m.logger.Warn("store status result", "key", result.Key, "error", err)
	}
	m.metrics.IncStatus(result.Up)
	m.hub.Publish("status", result)
}

func blockedTarget(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	switch host {
	case "metadata.google.internal", "metadata":
		return true
	case "fd00:ec2::254":
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	// Normalize IPv4-mapped IPv6 so http://[::ffff:169.254.169.254]/ cannot
	// slip past the literal comparisons below.
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	switch {
	case ip.IsLinkLocalUnicast(), ip.IsLinkLocalMulticast():
		// 169.254.169.254 and friends on every platform.
		return true
	case ip.Equal(net.ParseIP("100.100.100.200")):
		// Alibaba metadata endpoint.
		return true
	}
	return false
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
