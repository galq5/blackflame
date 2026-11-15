package config

import (
	"context"
	"time"
)

// Snapshot is an immutable view of the authoritative content configuration.
// A non-nil Err with a non-nil Config means the last good config is still
// being served after a failed reload.
type Snapshot struct {
	Config   *Config
	Source   string
	LoadedAt time.Time
	Err      error
}

func (s Snapshot) Ready() bool {
	return s.Config != nil
}

// Store is the content configuration boundary between UI mode and GitOps mode.
type Store interface {
	Snapshot() Snapshot
	Reload(ctx context.Context) error
	Watch() <-chan struct{}
	ReadOnly() bool
	Close() error
}
