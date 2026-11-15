package discovery

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/galq/blackflame/internal/config"
	"github.com/galq/blackflame/internal/metrics"
)

type Sink interface {
	UpsertDiscovered(ctx context.Context, source string, items []config.DiscoveredItem) error
}

type SourceStatus struct {
	LastSync time.Time `json:"lastSync,omitempty"`
	Error    string    `json:"error,omitempty"`
	Count    int       `json:"count"`
}

type Status struct {
	Docker     SourceStatus `json:"docker"`
	Kubernetes SourceStatus `json:"kubernetes"`
}

type Manager struct {
	sink     Sink
	logger   *slog.Logger
	metrics  *metrics.Registry
	provider func() *config.Config
	onChange func(context.Context)

	mu      sync.RWMutex
	status  Status
	lastRun map[string]time.Time
}

func New(sink Sink, logger *slog.Logger, registry *metrics.Registry, provider func() *config.Config, onChange func(context.Context)) *Manager {
	return &Manager{
		sink:     sink,
		logger:   logger,
		metrics:  registry,
		provider: provider,
		onChange: onChange,
		lastRun:  map[string]time.Time{},
	}
}

func (m *Manager) Start(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.runDue(ctx)
		}
	}
}

func (m *Manager) Refresh(ctx context.Context) {
	m.runDocker(ctx)
	m.runKubernetes(ctx)
}

func (m *Manager) Status() Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status
}

func (m *Manager) runDue(ctx context.Context) {
	cfg := m.provider()
	if cfg == nil || m.sink == nil {
		return
	}
	now := time.Now()
	if cfg.Discovery.Docker.Enabled && m.due("docker", now, cfg.Discovery.Docker.IntervalSeconds) {
		m.runDocker(ctx)
	}
	if cfg.Discovery.Kubernetes.Enabled && m.due("kubernetes", now, cfg.Discovery.Kubernetes.IntervalSeconds) {
		m.runKubernetes(ctx)
	}
}

func (m *Manager) due(source string, now time.Time, intervalSeconds int) bool {
	if intervalSeconds < 5 {
		intervalSeconds = 30
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	last, ok := m.lastRun[source]
	if ok && now.Sub(last) < time.Duration(intervalSeconds)*time.Second {
		return false
	}
	m.lastRun[source] = now
	return true
}

func (m *Manager) runDocker(ctx context.Context) {
	cfg := m.provider()
	if cfg == nil || !cfg.Discovery.Docker.Enabled || m.sink == nil {
		return
	}
	items, err := discoverDocker(ctx, cfg.Discovery.Docker.Socket)
	m.finish(ctx, "docker", items, err)
}

func (m *Manager) runKubernetes(ctx context.Context) {
	cfg := m.provider()
	if cfg == nil || !cfg.Discovery.Kubernetes.Enabled || m.sink == nil {
		return
	}
	items, err := discoverKubernetes(ctx, cfg.Discovery.Kubernetes)
	m.finish(ctx, "kubernetes", items, err)
}

func (m *Manager) finish(ctx context.Context, source string, items []config.DiscoveredItem, err error) {
	status := SourceStatus{Count: len(items)}
	if err != nil {
		status.Error = err.Error()
		m.metrics.IncDiscoveryError()
		m.logger.Warn("discovery failed", "source", source, "error", err)
	} else {
		if upsertErr := m.sink.UpsertDiscovered(ctx, source, items); upsertErr != nil {
			status.Error = upsertErr.Error()
			m.metrics.IncDiscoveryError()
			m.logger.Warn("discovery upsert failed", "source", source, "error", upsertErr)
		} else {
			status.LastSync = time.Now().UTC()
			if m.onChange != nil {
				m.onChange(ctx)
			}
			m.logger.Info("discovery complete", "source", source, "items", len(items))
		}
	}
	m.mu.Lock()
	if source == config.SourceDocker {
		m.status.Docker = status
	} else {
		m.status.Kubernetes = status
	}
	m.mu.Unlock()
}
