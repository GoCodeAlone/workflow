package scale

import (
	"context"
	"database/sql"
	"fmt"
	"hash/fnv"
	"sync"
	"time"
)

// DistributedLock provides distributed locking for state machine transitions
// and other coordination needs across multiple Workflow instances.
type DistributedLock interface {
	// Acquire obtains a lock for the given key. Returns a release function.
	// Blocks until the lock is acquired or context is cancelled.
	Acquire(ctx context.Context, key string, ttl time.Duration) (release func(), err error)
	// TryAcquire attempts to acquire a lock without blocking.
	// Returns false if the lock is already held.
	TryAcquire(ctx context.Context, key string, ttl time.Duration) (release func(), acquired bool, err error)
}

// --- InMemoryLock ---

// InMemoryLock implements DistributedLock for testing and single-server deployments.
// Uses sync.Mutex per key with a map.
type InMemoryLock struct {
	mu    sync.Mutex
	locks map[string]*lockEntry
}

type lockEntry struct {
	mu      sync.Mutex
	waiters chan struct{} // signals when the lock is released
	held    bool
}

// NewInMemoryLock creates a new in-memory distributed lock.
func NewInMemoryLock() *InMemoryLock {
	return &InMemoryLock{
		locks: make(map[string]*lockEntry),
	}
}

// getOrCreateEntry returns the lock entry for the given key, creating one if necessary.
func (l *InMemoryLock) getOrCreateEntry(key string) *lockEntry {
	l.mu.Lock()
	defer l.mu.Unlock()

	entry, ok := l.locks[key]
	if !ok {
		entry = &lockEntry{
			waiters: make(chan struct{}, 1),
		}
		l.locks[key] = entry
	}
	return entry
}

// Acquire obtains a lock for the given key, blocking until acquired or context cancelled.
func (l *InMemoryLock) Acquire(ctx context.Context, key string, ttl time.Duration) (func(), error) {
	entry := l.getOrCreateEntry(key)

	for {
		entry.mu.Lock()
		if !entry.held {
			entry.held = true
			entry.mu.Unlock()

			var releaseOnce sync.Once
			release := func() {
				releaseOnce.Do(func() {
					entry.mu.Lock()
					entry.held = false
					entry.mu.Unlock()
					// Signal one waiter
					select {
					case entry.waiters <- struct{}{}:
					default:
					}
				})
			}

			// If ttl > 0, schedule automatic release
			if ttl > 0 {
				go func() {
					timer := time.NewTimer(ttl)
					defer timer.Stop()
					select {
					case <-timer.C:
						release()
					case <-ctx.Done():
					}
				}()
			}

			return release, nil
		}
		entry.mu.Unlock()

		// Wait for release signal or context cancellation
		select {
		case <-entry.waiters:
			// Lock was released, try again
			continue
		case <-ctx.Done():
			return nil, fmt.Errorf("acquire lock for %s: %w", key, ctx.Err())
		}
	}
}

// TryAcquire attempts to acquire a lock without blocking.
// Returns false if the lock is already held.
func (l *InMemoryLock) TryAcquire(ctx context.Context, key string, ttl time.Duration) (func(), bool, error) {
	entry := l.getOrCreateEntry(key)

	entry.mu.Lock()
	if entry.held {
		entry.mu.Unlock()
		return nil, false, nil
	}
	entry.held = true
	entry.mu.Unlock()

	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			entry.mu.Lock()
			entry.held = false
			entry.mu.Unlock()
			// Signal one waiter
			select {
			case entry.waiters <- struct{}{}:
			default:
			}
		})
	}

	// If ttl > 0, schedule automatic release
	if ttl > 0 {
		go func() {
			timer := time.NewTimer(ttl)
			defer timer.Stop()
			select {
			case <-timer.C:
				release()
			case <-ctx.Done():
			}
		}()
	}

	return release, true, nil
}

// --- PGAdvisoryLock ---

// PGAdvisoryLock implements DistributedLock using PostgreSQL advisory locks
// (pg_advisory_lock / pg_advisory_unlock). The key string is hashed to int64
// for use as the lock ID.
type PGAdvisoryLock struct {
	db *sql.DB
}

// NewPGAdvisoryLock creates a new PostgreSQL advisory lock implementation.
func NewPGAdvisoryLock(db *sql.DB) *PGAdvisoryLock {
	return &PGAdvisoryLock{db: db}
}

// Acquire obtains a PostgreSQL advisory lock for the given key.
// Blocks until the lock is acquired or context is cancelled.
// Note: ttl is not natively supported by pg_advisory_lock; the lock is held
// until explicitly released or the session ends.
func (l *PGAdvisoryLock) Acquire(ctx context.Context, key string, ttl time.Duration) (func(), error) {
	lockID := hashToInt64(key)

	// Use a dedicated connection to ensure the advisory lock is tied to it.
	conn, err := l.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire lock connection for %s: %w", key, err)
	}

	_, err = conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", lockID)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("acquire lock for %s: %w", key, err)
	}

	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			// Use a background context for unlock since the original ctx may be cancelled
			_, _ = conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", lockID)
			conn.Close()
		})
	}

	return release, nil
}

// TryAcquire attempts to acquire a PostgreSQL advisory lock without blocking.
// Returns false if the lock is already held.
func (l *PGAdvisoryLock) TryAcquire(ctx context.Context, key string, ttl time.Duration) (func(), bool, error) {
	lockID := hashToInt64(key)

	conn, err := l.db.Conn(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("try acquire lock connection for %s: %w", key, err)
	}

	var acquired bool
	err = conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", lockID).Scan(&acquired)
	if err != nil {
		conn.Close()
		return nil, false, fmt.Errorf("try acquire lock for %s: %w", key, err)
	}

	if !acquired {
		conn.Close()
		return nil, false, nil
	}

	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			_, _ = conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", lockID)
			conn.Close()
		})
	}

	return release, true, nil
}

// hashToInt64 converts a string key to an int64 using FNV-1a hash.
// The same key always produces the same hash value.
func hashToInt64(key string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	v := h.Sum64() & 0x7FFFFFFFFFFFFFFF // Clear sign bit; always <= math.MaxInt64.
	return int64(v)                     //nolint:gosec // masked to non-negative range
}

// --- RedisLock ---

// RedisLock implements DistributedLock using Redis SET NX with TTL.
// Requires a Redis client connection. This is a stub implementation;
// the full implementation will be provided when the Redis client is
// integrated as a direct dependency.
type RedisLock struct {
	// addr is the Redis server address.
	addr string
}

// NewRedisLock creates a new Redis distributed lock stub.
// Full implementation requires a Redis client (e.g., go-redis).
func NewRedisLock(addr string) *RedisLock {
	return &RedisLock{addr: addr}
}

// Acquire obtains a lock using Redis SET NX with TTL.
// This is a stub that returns an error indicating Redis is not yet configured.
func (l *RedisLock) Acquire(_ context.Context, key string, _ time.Duration) (func(), error) {
	return nil, fmt.Errorf("redis lock not implemented: configure Redis client for key %s at %s", key, l.addr)
}

// TryAcquire attempts to acquire a Redis lock without blocking.
// This is a stub that returns an error indicating Redis is not yet configured.
func (l *RedisLock) TryAcquire(_ context.Context, key string, _ time.Duration) (func(), bool, error) {
	return nil, false, fmt.Errorf("redis lock not implemented: configure Redis client for key %s at %s", key, l.addr)
}
