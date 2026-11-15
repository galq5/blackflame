package config

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"sync"
	"time"
)

// YAMLStore is the authoritative content store for GitOps mode.
type YAMLStore struct {
	path string

	mu   sync.RWMutex
	snap Snapshot

	notify    chan struct{}
	done      chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup
}

func NewYAMLStore(ctx context.Context, path string) (*YAMLStore, error) {
	if path == "" {
		return nil, fmt.Errorf("config path is required")
	}
	store := &YAMLStore{
		path:   path,
		notify: make(chan struct{}, 1),
		done:   make(chan struct{}),
	}
	if err := store.Reload(ctx); err != nil {
		return nil, err
	}
	store.wg.Add(1)
	go store.watchLoop()
	return store, nil
}

func (s *YAMLStore) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snap
}

func (s *YAMLStore) Reload(ctx context.Context) error {
	_ = ctx
	cfg, err := LoadFile(s.path)
	now := time.Now().UTC()
	s.mu.Lock()
	if err != nil {
		s.snap.Err = err
		if s.snap.Source == "" {
			s.snap.Source = s.path
		}
		s.mu.Unlock()
		s.signal()
		return err
	}
	s.snap = Snapshot{
		Config:   cfg,
		Source:   s.path,
		LoadedAt: now,
	}
	s.mu.Unlock()
	s.signal()
	return nil
}

func (s *YAMLStore) Watch() <-chan struct{} { return s.notify }

func (s *YAMLStore) ReadOnly() bool { return true }

func (s *YAMLStore) Close() error {
	s.closeOnce.Do(func() {
		close(s.done)
		s.wg.Wait()
	})
	return nil
}

func (s *YAMLStore) signal() {
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

func (s *YAMLStore) watchLoop() {
	defer s.wg.Done()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	last := s.fileHash()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			current := s.fileHash()
			if current == last {
				continue
			}
			last = current
			_ = s.Reload(context.Background())
		}
	}
}

func (s *YAMLStore) fileHash() string {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return "error:" + err.Error()
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:])
}
