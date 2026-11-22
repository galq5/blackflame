package widgets

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/galq/blackflame/internal/config"
)

type Card struct {
	Type      string    `json:"type"`
	Title     string    `json:"title"`
	Public    bool      `json:"public"`
	Data      any       `json:"data,omitempty"`
	Error     string    `json:"error,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type cacheEntry struct {
	configKey string
	data      any
	err       string
	updated   time.Time
	expires   time.Time
}

type Manager struct {
	logger *slog.Logger

	mu    sync.Mutex
	cache map[string]cacheEntry
}

func New(logger *slog.Logger) *Manager {
	return &Manager{
		logger: logger,
		cache:  map[string]cacheEntry{},
	}
}

type widgetJob struct {
	settings   any
	widgetType string
	title      string
	public     bool
	refresh    time.Duration
	budget     time.Duration
	fetch      func(context.Context) (any, error)
}

func (m *Manager) Cards(ctx context.Context, widgets config.Widgets, publicOnly bool) []Card {
	jobs := make([]widgetJob, 0, 5)
	if widgets.PiHole != nil && widgets.PiHole.Enabled {
		cfg := widgets.PiHole
		jobs = append(jobs, widgetJob{
			settings:   cfg,
			widgetType: "pihole",
			title:      titleOr(cfg.Title, "Pi-hole"),
			public:     cfg.Public,
			refresh:    cfg.Refresh(),
			fetch:      func(ctx context.Context) (any, error) { return fetchPiHole(ctx, cfg) },
		})
	}
	if widgets.Proxmox != nil && widgets.Proxmox.Enabled {
		cfg := widgets.Proxmox
		jobs = append(jobs, widgetJob{
			settings:   cfg,
			widgetType: "proxmox",
			title:      titleOr(cfg.Title, "Proxmox"),
			public:     cfg.Public,
			refresh:    cfg.Refresh(),
			// One round trip per node: give larger clusters room inside
			// the fetch budget instead of a guaranteed deadline error.
			budget: 60 * time.Second,
			fetch:  func(ctx context.Context) (any, error) { return fetchProxmox(ctx, cfg) },
		})
	}
	if widgets.Kubernetes != nil && widgets.Kubernetes.Enabled {
		cfg := widgets.Kubernetes
		jobs = append(jobs, widgetJob{
			settings:   cfg,
			widgetType: "kubernetes",
			title:      titleOr(cfg.Title, "Kubernetes"),
			public:     cfg.Public,
			refresh:    cfg.Refresh(),
			// One round trip per namespace: same reasoning as Proxmox.
			budget: 60 * time.Second,
			fetch:  func(ctx context.Context) (any, error) { return fetchKubernetes(ctx, cfg) },
		})
	}
	if widgets.Prometheus != nil && widgets.Prometheus.Enabled {
		cfg := widgets.Prometheus
		jobs = append(jobs, widgetJob{
			settings:   cfg,
			widgetType: "prometheus",
			title:      titleOr(cfg.Title, "Prometheus"),
			public:     cfg.Public,
			refresh:    cfg.Refresh(),
			fetch:      func(ctx context.Context) (any, error) { return fetchPrometheus(ctx, cfg) },
		})
	}
	if widgets.Calendar != nil && widgets.Calendar.Enabled {
		cfg := widgets.Calendar
		jobs = append(jobs, widgetJob{
			settings:   cfg,
			widgetType: "calendar",
			title:      titleOr(cfg.Title, "Calendar"),
			public:     cfg.Public,
			refresh:    cfg.Refresh(),
			fetch:      func(ctx context.Context) (any, error) { return fetchCalendar(ctx, cfg) },
		})
	}

	visible := make([]widgetJob, 0, len(jobs))
	for _, job := range jobs {
		if publicOnly && !job.public {
			continue
		}
		visible = append(visible, job)
	}
	if len(visible) == 0 {
		return []Card{}
	}

	results := make(chan Card, len(visible))
	for _, job := range visible {
		job := job
		go func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					m.logger.Error("widget panicked", "type", job.widgetType, "panic", recovered)
					results <- Card{
						Type:      job.widgetType,
						Title:     job.title,
						Public:    job.public,
						Error:     "Widget failed",
						UpdatedAt: time.Now().UTC(),
					}
				}
			}()
			results <- m.card(ctx, job)
		}()
	}
	cards := make([]Card, 0, len(visible))
	for range visible {
		cards = append(cards, <-results)
	}
	if publicOnly {
		for i := range cards {
			if cards[i].Error != "" {
				cards[i].Error = "Widget unavailable"
			}
		}
	}
	return cards
}

func (m *Manager) card(ctx context.Context, job widgetJob) Card {
	encoded, encodeErr := json.Marshal(job.settings)
	configKey := string(encoded)
	if entry, ok := m.cached(job.widgetType, configKey); ok && encodeErr == nil {
		return Card{
			Type:      job.widgetType,
			Title:     job.title,
			Public:    job.public,
			Data:      entry.data,
			Error:     entry.err,
			UpdatedAt: entry.updated,
		}
	}

	budget := job.budget
	if budget <= 0 {
		budget = 15 * time.Second
	}
	fetchCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	data, err := job.fetch(fetchCtx)
	card := Card{
		Type:      job.widgetType,
		Title:     job.title,
		Public:    job.public,
		Data:      data,
		UpdatedAt: time.Now().UTC(),
	}
	if err != nil {
		card.Data = nil
		card.Error = err.Error()
		m.logger.Debug("widget fetch failed", "type", job.widgetType, "error", err)
	}
	ttl := job.refresh
	if err != nil {
		ttl = 15 * time.Second
	}
	if encodeErr == nil {
		m.store(job.widgetType, cacheEntry{
			configKey: configKey,
			data:      card.Data,
			err:       card.Error,
			updated:   card.UpdatedAt,
			expires:   time.Now().Add(ttl),
		})
	}
	return card
}

func (m *Manager) cached(widgetType, configKey string) (cacheEntry, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.cache[widgetType]
	if !ok || time.Now().After(entry.expires) {
		if ok {
			delete(m.cache, widgetType)
		}
		return cacheEntry{}, false
	}
	return entry, entry.configKey == configKey
}

func (m *Manager) store(widgetType string, entry cacheEntry) {
	m.mu.Lock()
	m.cache[widgetType] = entry
	m.mu.Unlock()
}

func titleOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func newHTTPClient(insecure bool) *http.Client {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		TLSClientConfig: &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: insecure, // #nosec G402 -- explicit per-widget opt-in
		},
		// Bound how long an abandoned keep-alive connection (and its
		// readLoop goroutine) can linger after a fetch completes.
		IdleConnTimeout: 30 * time.Second,
	}
	return &http.Client{Transport: transport, Timeout: 15 * time.Second}
}

// redactURL masks credential-like query values so fetch errors never echo
// secrets (e.g. a Pi-hole v5 auth token in the URL) back to the dashboard
// or into the logs.
func redactURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.RawQuery == "" {
		return raw
	}
	query := parsed.Query()
	changed := false
	for key := range query {
		switch strings.ToLower(key) {
		case "auth", "password", "pass", "token", "secret", "sid", "key", "apikey", "api_key":
			query.Set(key, "REDACTED")
			changed = true
		}
	}
	if !changed {
		return raw
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func getJSON(ctx context.Context, client *http.Client, url string, headers map[string]string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("%s returned %d: %s", redactURL(url), resp.StatusCode, string(body))
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(target); err != nil {
		return fmt.Errorf("decode %s: %w", redactURL(url), err)
	}
	return nil
}
