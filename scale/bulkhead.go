package scale

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
)

// ErrTenantLimitExceeded is returned when a tenant has reached its concurrency limit.
var ErrTenantLimitExceeded = fmt.Errorf("tenant concurrency limit exceeded")

// Bulkhead provides per-tenant resource isolation to prevent noisy neighbors.
// Each tenant has configurable concurrency limits enforced via semaphores.
type Bulkhead struct {
	tenantLimits map[string]*tenantLimit
	defaultLimit *tenantLimit
	mu           sync.RWMutex
}

// TenantLimitConfig defines the configuration for a tenant's concurrency constraints.
// Use this when calling SetLimit to configure tenant-specific limits.
type TenantLimitConfig struct {
	// MaxConcurrent is the maximum number of concurrent operations allowed.
	MaxConcurrent int
	// RateLimit is the maximum requests per second (reserved for future use).
	RateLimit float64
}

// tenantLimit holds internal state for enforcing tenant concurrency limits.
type tenantLimit struct {
	maxConcurrent int
	rateLimit     float64
	semaphore     chan struct{}
	rejected      atomic.Int64
	active        atomic.Int64
}

// BulkheadStats holds current usage statistics for a tenant.
type BulkheadStats struct {
	TenantID      string `json:"tenant_id"`
	Active        int    `json:"active"`
	MaxConcurrent int    `json:"max_concurrent"`
	Rejected      int64  `json:"rejected"`
}

// BulkheadConfig configures the bulkhead defaults.
type BulkheadConfig struct {
	// DefaultMaxConcurrent is the default concurrency limit for tenants without specific limits.
	DefaultMaxConcurrent int
	// DefaultRateLimit is the default rate limit (requests per second).
	DefaultRateLimit float64
}

// DefaultBulkheadConfig returns sensible defaults.
func DefaultBulkheadConfig() BulkheadConfig {
	return BulkheadConfig{
		DefaultMaxConcurrent: 10,
		DefaultRateLimit:     100.0,
	}
}

// NewBulkhead creates a new bulkhead with the given configuration.
func NewBulkhead(cfg BulkheadConfig) *Bulkhead {
	if cfg.DefaultMaxConcurrent <= 0 {
		cfg.DefaultMaxConcurrent = 10
	}

	defaultLimit := &tenantLimit{
		maxConcurrent: cfg.DefaultMaxConcurrent,
		rateLimit:     cfg.DefaultRateLimit,
		semaphore:     make(chan struct{}, cfg.DefaultMaxConcurrent),
	}

	return &Bulkhead{
		tenantLimits: make(map[string]*tenantLimit),
		defaultLimit: defaultLimit,
	}
}

// getLimit returns the limit for the given tenant, falling back to the default.
func (b *Bulkhead) getLimit(tenantID string) *tenantLimit {
	b.mu.RLock()
	limit, ok := b.tenantLimits[tenantID]
	b.mu.RUnlock()
	if ok {
		return limit
	}
	return b.defaultLimit
}

// Acquire attempts to acquire a slot for the given tenant.
// Returns ErrTenantLimitExceeded if the tenant has reached its concurrency limit.
// On success, returns a release function that must be called when the operation completes.
func (b *Bulkhead) Acquire(ctx context.Context, tenantID string) (func(), error) {
	limit := b.getLimit(tenantID)

	select {
	case limit.semaphore <- struct{}{}:
		limit.active.Add(1)
		var releaseOnce sync.Once
		return func() {
			releaseOnce.Do(func() {
				<-limit.semaphore
				limit.active.Add(-1)
			})
		}, nil
	default:
		limit.rejected.Add(1)
		return nil, ErrTenantLimitExceeded
	}
}

// AcquireWait attempts to acquire a slot for the given tenant, blocking until
// a slot is available or the context is cancelled.
func (b *Bulkhead) AcquireWait(ctx context.Context, tenantID string) (func(), error) {
	limit := b.getLimit(tenantID)

	select {
	case limit.semaphore <- struct{}{}:
		limit.active.Add(1)
		var releaseOnce sync.Once
		return func() {
			releaseOnce.Do(func() {
				<-limit.semaphore
				limit.active.Add(-1)
			})
		}, nil
	case <-ctx.Done():
		limit.rejected.Add(1)
		return nil, fmt.Errorf("bulkhead acquire for tenant %s: %w", tenantID, ctx.Err())
	}
}

// SetLimit configures limits for a specific tenant.
// If the tenant already has a limit, it replaces the semaphore while
// preserving rejection stats.
func (b *Bulkhead) SetLimit(tenantID string, cfg TenantLimitConfig) {
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = b.defaultLimit.maxConcurrent
	}

	newLimit := &tenantLimit{
		maxConcurrent: cfg.MaxConcurrent,
		rateLimit:     cfg.RateLimit,
		semaphore:     make(chan struct{}, cfg.MaxConcurrent),
	}

	b.mu.Lock()
	b.tenantLimits[tenantID] = newLimit
	b.mu.Unlock()
}

// RemoveLimit removes the specific limit for a tenant, reverting to the default.
func (b *Bulkhead) RemoveLimit(tenantID string) {
	b.mu.Lock()
	delete(b.tenantLimits, tenantID)
	b.mu.Unlock()
}

// Stats returns current usage statistics per tenant.
// Includes all tenants with specific limits plus the default bucket.
func (b *Bulkhead) Stats() map[string]BulkheadStats {
	b.mu.RLock()
	defer b.mu.RUnlock()

	stats := make(map[string]BulkheadStats, len(b.tenantLimits)+1)

	for tenantID, limit := range b.tenantLimits {
		stats[tenantID] = BulkheadStats{
			TenantID:      tenantID,
			Active:        int(limit.active.Load()),
			MaxConcurrent: limit.maxConcurrent,
			Rejected:      limit.rejected.Load(),
		}
	}

	// Include default stats
	stats["_default"] = BulkheadStats{
		TenantID:      "_default",
		Active:        int(b.defaultLimit.active.Load()),
		MaxConcurrent: b.defaultLimit.maxConcurrent,
		Rejected:      b.defaultLimit.rejected.Load(),
	}

	return stats
}
