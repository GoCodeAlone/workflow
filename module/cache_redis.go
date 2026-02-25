package module

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/redis/go-redis/v9"
)

// CacheModule defines the interface for cache operations used by pipeline steps.
type CacheModule interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
}

// RedisClient is the subset of go-redis client methods used by RedisCache.
// Keeping it as an interface enables mocking in tests.
type RedisClient interface {
	Ping(ctx context.Context) *redis.StatusCmd
	Get(ctx context.Context, key string) *redis.StringCmd
	Set(ctx context.Context, key string, value any, expiration time.Duration) *redis.StatusCmd
	Del(ctx context.Context, keys ...string) *redis.IntCmd
	Close() error
}

// RedisCacheConfig holds configuration for the cache.redis module.
type RedisCacheConfig struct {
	Address    string
	Password   string
	DB         int
	Prefix     string
	DefaultTTL time.Duration
}

// RedisCache is a module that connects to a Redis instance and exposes
// Get/Set/Delete operations for use by pipeline steps.
type RedisCache struct {
	name   string
	cfg    RedisCacheConfig
	client RedisClient
	logger modular.Logger
}

// NewRedisCache creates a new RedisCache module with the given name and config.
func NewRedisCache(name string, cfg RedisCacheConfig) *RedisCache {
	return &RedisCache{
		name:   name,
		cfg:    cfg,
		logger: &noopLogger{},
	}
}

// NewRedisCacheWithClient creates a RedisCache backed by a pre-built client.
// This is intended for testing only.
func NewRedisCacheWithClient(name string, cfg RedisCacheConfig, client RedisClient) *RedisCache {
	return &RedisCache{
		name:   name,
		cfg:    cfg,
		client: client,
		logger: &noopLogger{},
	}
}

func (r *RedisCache) Name() string { return r.name }

func (r *RedisCache) Init(app modular.Application) error {
	r.logger = app.Logger()
	return nil
}

// Start connects to Redis and verifies the connection with PING.
func (r *RedisCache) Start(ctx context.Context) error {
	if r.client != nil {
		// Already set (e.g. in tests)
		return nil
	}

	opts := &redis.Options{
		Addr: r.cfg.Address,
		DB:   r.cfg.DB,
	}
	if r.cfg.Password != "" {
		opts.Password = r.cfg.Password
	}

	r.client = redis.NewClient(opts)

	if err := r.client.Ping(ctx).Err(); err != nil {
		_ = r.client.Close()
		r.client = nil
		return fmt.Errorf("cache.redis %q: ping failed: %w", r.name, err)
	}

	r.logger.Info("Redis cache started", "name", r.name, "address", r.cfg.Address)
	return nil
}

// Stop closes the Redis connection.
func (r *RedisCache) Stop(_ context.Context) error {
	if r.client != nil {
		r.logger.Info("Redis cache stopped", "name", r.name)
		return r.client.Close()
	}
	return nil
}

// Get retrieves a value from Redis by key (with prefix applied).
// Returns redis.Nil wrapped in an error when the key does not exist.
func (r *RedisCache) Get(ctx context.Context, key string) (string, error) {
	if r.client == nil {
		return "", fmt.Errorf("cache.redis %q: not started", r.name)
	}
	val, err := r.client.Get(ctx, r.prefixed(key)).Result()
	if err != nil {
		return "", err
	}
	return val, nil
}

// Set stores a value in Redis with optional TTL.  A zero duration uses the
// module-level default; if the default is also zero the key never expires.
func (r *RedisCache) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	if r.client == nil {
		return fmt.Errorf("cache.redis %q: not started", r.name)
	}
	if ttl == 0 {
		ttl = r.cfg.DefaultTTL
	}
	return r.client.Set(ctx, r.prefixed(key), value, ttl).Err()
}

// Delete removes a key from Redis (with prefix applied).
func (r *RedisCache) Delete(ctx context.Context, key string) error {
	if r.client == nil {
		return fmt.Errorf("cache.redis %q: not started", r.name)
	}
	return r.client.Del(ctx, r.prefixed(key)).Err()
}

func (r *RedisCache) prefixed(key string) string {
	return r.cfg.Prefix + key
}

func (r *RedisCache) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{Name: r.name, Description: "Redis cache connection", Instance: r},
	}
}

func (r *RedisCache) RequiresServices() []modular.ServiceDependency {
	return nil
}

// ExpandEnvString resolves ${VAR} and $VAR environment variable references.
func ExpandEnvString(s string) string {
	return os.ExpandEnv(s)
}
