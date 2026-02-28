package module

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/redis/go-redis/v9"
)

// TokenBlacklist is the interface for checking and adding revoked JWT IDs.
type TokenBlacklist interface {
	Add(jti string, expiresAt time.Time)
	IsBlacklisted(jti string) bool
}

// TokenBlacklistModule maintains a set of revoked JWT IDs (JTIs).
// It supports two backends: "memory" (default) and "redis".
type TokenBlacklistModule struct {
	name            string
	backend         string
	redisURL        string
	cleanupInterval time.Duration

	// memory backend
	entries sync.Map // jti (string) -> expiry (time.Time)

	// redis backend
	redisClient *redis.Client

	logger modular.Logger
	stopCh chan struct{}
}

// NewTokenBlacklistModule creates a new TokenBlacklistModule.
func NewTokenBlacklistModule(name, backend, redisURL string, cleanupInterval time.Duration) *TokenBlacklistModule {
	if backend == "" {
		backend = "memory"
	}
	if cleanupInterval <= 0 {
		cleanupInterval = 5 * time.Minute
	}
	return &TokenBlacklistModule{
		name:            name,
		backend:         backend,
		redisURL:        redisURL,
		cleanupInterval: cleanupInterval,
		stopCh:          make(chan struct{}),
	}
}

// Name returns the module name.
func (m *TokenBlacklistModule) Name() string { return m.name }

// Init initializes the module.
func (m *TokenBlacklistModule) Init(app modular.Application) error {
	m.logger = app.Logger()
	return nil
}

// Start connects to Redis (if configured) and starts the cleanup goroutine.
func (m *TokenBlacklistModule) Start(ctx context.Context) error {
	if m.backend == "redis" {
		if m.redisURL == "" {
			return fmt.Errorf("auth.token-blacklist %q: redis_url is required for redis backend", m.name)
		}
		opts, err := redis.ParseURL(m.redisURL)
		if err != nil {
			return fmt.Errorf("auth.token-blacklist %q: invalid redis_url: %w", m.name, err)
		}
		m.redisClient = redis.NewClient(opts)
		if err := m.redisClient.Ping(ctx).Err(); err != nil {
			_ = m.redisClient.Close()
			m.redisClient = nil
			return fmt.Errorf("auth.token-blacklist %q: redis ping failed: %w", m.name, err)
		}
		m.logger.Info("token blacklist started", "name", m.name, "backend", "redis")
		return nil
	}

	// memory backend: start cleanup goroutine
	go m.runCleanup()
	m.logger.Info("token blacklist started", "name", m.name, "backend", "memory")
	return nil
}

// Stop shuts down the module.
func (m *TokenBlacklistModule) Stop(_ context.Context) error {
	select {
	case <-m.stopCh:
		// already closed
	default:
		close(m.stopCh)
	}
	if m.redisClient != nil {
		return m.redisClient.Close()
	}
	return nil
}

// Add marks a JTI as revoked until expiresAt.
func (m *TokenBlacklistModule) Add(jti string, expiresAt time.Time) {
	if m.backend == "redis" && m.redisClient != nil {
		ttl := time.Until(expiresAt)
		if ttl <= 0 {
			return // already expired, nothing to blacklist
		}
		_ = m.redisClient.Set(context.Background(), m.redisKey(jti), "1", ttl).Err()
		return
	}
	m.entries.Store(jti, expiresAt)
}

// IsBlacklisted returns true if the JTI is revoked and has not yet expired.
func (m *TokenBlacklistModule) IsBlacklisted(jti string) bool {
	if m.backend == "redis" && m.redisClient != nil {
		n, err := m.redisClient.Exists(context.Background(), m.redisKey(jti)).Result()
		return err == nil && n > 0
	}
	val, ok := m.entries.Load(jti)
	if !ok {
		return false
	}
	expiry, ok := val.(time.Time)
	return ok && time.Now().Before(expiry)
}

func (m *TokenBlacklistModule) redisKey(jti string) string {
	return "blacklist:" + jti
}

func (m *TokenBlacklistModule) runCleanup() {
	ticker := time.NewTicker(m.cleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			now := time.Now()
			m.entries.Range(func(key, value any) bool {
				if expiry, ok := value.(time.Time); ok && now.After(expiry) {
					m.entries.Delete(key)
				}
				return true
			})
		}
	}
}

// ProvidesServices registers this module as a service.
func (m *TokenBlacklistModule) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{Name: m.name, Description: "JWT token blacklist", Instance: m},
	}
}

// RequiresServices returns service dependencies (none).
func (m *TokenBlacklistModule) RequiresServices() []modular.ServiceDependency {
	return nil
}
