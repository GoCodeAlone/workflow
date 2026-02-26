package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
)

// RedisNoSQLConfig holds configuration for the nosql.redis module.
//
// This is a general-purpose key-value data store backed by Redis (distinct from
// the cache.redis module which is for TTL-based caching).
//
// When addr == "memory://" the module falls back to the in-memory backend.
type RedisNoSQLConfig struct {
	Addr     string `json:"addr"     yaml:"addr"`     // "memory://" => in-memory fallback
	Password string `json:"password" yaml:"password"` //nolint:gosec // G117: config struct field, not a hardcoded secret
	DB       int    `json:"db"       yaml:"db"`
}

// RedisNoSQL is the nosql.redis module.
// In memory mode (addr: "memory://") it delegates to MemoryNoSQL.
// For real Redis, replace backend with a redis.Client and implement
// Get/Put/Delete/Query using HGetAll, HSet, Del, Scan.
type RedisNoSQL struct {
	name    string
	cfg     RedisNoSQLConfig
	backend NoSQLStore
}

// NewRedisNoSQL creates a new RedisNoSQL module.
func NewRedisNoSQL(name string, cfg RedisNoSQLConfig) *RedisNoSQL {
	return &RedisNoSQL{name: name, cfg: cfg}
}

func (r *RedisNoSQL) Name() string { return r.name }

func (r *RedisNoSQL) Init(_ modular.Application) error {
	if r.cfg.Addr == "memory://" || r.cfg.Addr == "" {
		r.backend = NewMemoryNoSQL(r.name+"-mem", MemoryNoSQLConfig{})
		return nil
	}
	// Full Redis implementation:
	// r.client = redis.NewClient(&redis.Options{Addr: r.cfg.Addr, Password: r.cfg.Password, DB: r.cfg.DB})
	// Store items as JSON strings: GET/SET key -> json.Marshal(item)
	// Query via SCAN with match pattern
	return fmt.Errorf("nosql.redis %q: real Redis addr not yet implemented; use addr: memory:// for testing", r.name)
}

func (r *RedisNoSQL) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{Name: r.name, Description: "Redis NoSQL store: " + r.name, Instance: r},
	}
}

func (r *RedisNoSQL) RequiresServices() []modular.ServiceDependency { return nil }

func (r *RedisNoSQL) Get(ctx context.Context, key string) (map[string]any, error) {
	if r.backend == nil {
		return nil, fmt.Errorf("nosql.redis %q: not initialized", r.name)
	}
	// Real implementation: val, err := r.client.Get(ctx, key).Result(); json.Unmarshal([]byte(val), &item)
	return r.backend.Get(ctx, key)
}

func (r *RedisNoSQL) Put(ctx context.Context, key string, item map[string]any) error {
	if r.backend == nil {
		return fmt.Errorf("nosql.redis %q: not initialized", r.name)
	}
	// Real implementation: b, _ := json.Marshal(item); r.client.Set(ctx, key, b, 0)
	return r.backend.Put(ctx, key, item)
}

func (r *RedisNoSQL) Delete(ctx context.Context, key string) error {
	if r.backend == nil {
		return fmt.Errorf("nosql.redis %q: not initialized", r.name)
	}
	// Real implementation: r.client.Del(ctx, key)
	return r.backend.Delete(ctx, key)
}

func (r *RedisNoSQL) Query(ctx context.Context, params map[string]any) ([]map[string]any, error) {
	if r.backend == nil {
		return nil, fmt.Errorf("nosql.redis %q: not initialized", r.name)
	}
	// Real implementation: SCAN with MATCH prefix* then GET each key
	return r.backend.Query(ctx, params)
}
