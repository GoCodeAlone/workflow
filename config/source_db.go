package config

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// DBConfigStore is the database interface needed by DatabaseSource.
type DBConfigStore interface {
	GetConfigDocument(ctx context.Context, key string) ([]byte, error)
	GetConfigDocumentHash(ctx context.Context, key string) (string, error)
	PutConfigDocument(ctx context.Context, key string, data []byte) error
}

// DatabaseSource loads config from a database with caching.
type DatabaseSource struct {
	store           DBConfigStore
	key             string
	refreshInterval time.Duration

	mu         sync.RWMutex
	cached     *WorkflowConfig
	cachedHash string
	cachedAt   time.Time
}

// DatabaseSourceOption configures a DatabaseSource.
type DatabaseSourceOption func(*DatabaseSource)

// WithRefreshInterval sets the cache TTL for the DatabaseSource.
func WithRefreshInterval(d time.Duration) DatabaseSourceOption {
	return func(s *DatabaseSource) { s.refreshInterval = d }
}

// WithConfigKey sets the document key used to look up config in the database.
func WithConfigKey(key string) DatabaseSourceOption {
	return func(s *DatabaseSource) { s.key = key }
}

// NewDatabaseSource creates a DatabaseSource backed by the given store.
func NewDatabaseSource(store DBConfigStore, opts ...DatabaseSourceOption) *DatabaseSource {
	s := &DatabaseSource{
		store:           store,
		key:             "default",
		refreshInterval: 30 * time.Second,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Load retrieves the current configuration, returning a cached copy if still
// within the refresh interval.
func (s *DatabaseSource) Load(ctx context.Context) (*WorkflowConfig, error) {
	s.mu.RLock()
	if s.cached != nil && time.Since(s.cachedAt) < s.refreshInterval {
		cfg := s.cached
		s.mu.RUnlock()
		return cfg, nil
	}
	s.mu.RUnlock()
	return s.refresh(ctx)
}

func (s *DatabaseSource) refresh(ctx context.Context) (*WorkflowConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Double-check after acquiring write lock.
	if s.cached != nil && time.Since(s.cachedAt) < s.refreshInterval {
		return s.cached, nil
	}

	data, err := s.store.GetConfigDocument(ctx, s.key)
	if err != nil {
		return nil, fmt.Errorf("db source: get config %q: %w", s.key, err)
	}

	var cfg WorkflowConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("db source: parse config %q: %w", s.key, err)
	}

	sum := sha256.Sum256(data)
	s.cached = &cfg
	s.cachedHash = hex.EncodeToString(sum[:])
	s.cachedAt = time.Now()

	return &cfg, nil
}

// Hash returns the SHA256 hex digest of the stored config bytes. It first
// tries the fast path of fetching the pre-computed hash from the database,
// and falls back to loading the full document if that fails. The fallback
// always fetches fresh data to ensure change detection is accurate.
func (s *DatabaseSource) Hash(ctx context.Context) (string, error) {
	hash, err := s.store.GetConfigDocumentHash(ctx, s.key)
	if err == nil {
		return hash, nil
	}
	// Fallback: force a fresh load (bypass cache) to get an accurate hash.
	if _, refreshErr := s.refresh(ctx); refreshErr != nil {
		return "", refreshErr
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cachedHash, nil
}

// Name returns a human-readable identifier for this source.
func (s *DatabaseSource) Name() string {
	return fmt.Sprintf("database:%s", s.key)
}
