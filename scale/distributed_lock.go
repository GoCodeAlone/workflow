package scale

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"log"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
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

// redisReleaseScript atomically releases a Redis lock only if the caller
// holds it (i.e., the stored value matches the token).
var redisReleaseScript = redis.NewScript(`
if redis.call("get", KEYS[1]) == ARGV[1] then
    return redis.call("del", KEYS[1])
else
    return 0
end
`)

// RedisLock implements DistributedLock using Redis SET NX with TTL.
// Uses a unique token per acquisition to ensure only the holder can release.
type RedisLock struct {
	addr     string
	password string
	db       int
	client   *redis.Client
	initOnce sync.Once
}

// NewRedisLock creates a new Redis distributed lock using the given address.
// The Redis client is created lazily on first use.
func NewRedisLock(addr string) *RedisLock {
	return NewRedisLockWithOptions(addr, "", 0)
}

// NewRedisLockWithOptions creates a new Redis distributed lock with full
// connection options. The Redis client is created lazily on first use.
func NewRedisLockWithOptions(addr, password string, db int) *RedisLock {
	return &RedisLock{addr: addr, password: password, db: db}
}

// connect initialises the Redis client exactly once.
func (l *RedisLock) connect() {
	l.initOnce.Do(func() {
		l.client = redis.NewClient(&redis.Options{
			Addr:     l.addr,
			Password: l.password,
			DB:       l.db,
		})
	})
}

// Close releases the underlying Redis client connection.
func (l *RedisLock) Close() error {
	l.connect()
	return l.client.Close()
}

// randomToken generates a cryptographically random hex string used as the
// lock token, ensuring only the holder can release.
func randomToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate lock token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// buildRelease returns a release function that atomically deletes the lock
// only when the stored token matches, making it safe to call multiple times.
func (l *RedisLock) buildRelease(key, token string) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			ctx := context.Background()
			if err := redisReleaseScript.Run(ctx, l.client, []string{key}, token).Err(); err != nil {
				log.Printf("distributed lock: failed to release Redis lock for key %s: %v", key, err)
			}
		})
	}
}

// Acquire obtains a Redis lock for the given key using SET NX PX.
// Retries with exponential backoff until the lock is acquired or ctx is
// cancelled. Returns a release function that atomically deletes the lock.
func (l *RedisLock) Acquire(ctx context.Context, key string, ttl time.Duration) (func(), error) {
	l.connect()

	token, err := randomToken()
	if err != nil {
		return nil, err
	}

	backoff := 16 * time.Millisecond
	const maxBackoff = 512 * time.Millisecond

	for {
		cmd := l.client.SetArgs(ctx, key, token, redis.SetArgs{Mode: "NX", TTL: ttl})
		if err := cmd.Err(); err != nil && err != redis.Nil {
			return nil, fmt.Errorf("acquire redis lock for %s: %w", key, err)
		}
		if cmd.Val() == "OK" {
			return l.buildRelease(key, token), nil
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("acquire redis lock for %s: %w", key, ctx.Err())
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// TryAcquire attempts to acquire a Redis lock for the given key without
// blocking. Returns (release, true, nil) if acquired, (nil, false, nil) if
// the lock is already held.
func (l *RedisLock) TryAcquire(ctx context.Context, key string, ttl time.Duration) (func(), bool, error) {
	l.connect()

	token, err := randomToken()
	if err != nil {
		return nil, false, err
	}

	cmd := l.client.SetArgs(ctx, key, token, redis.SetArgs{Mode: "NX", TTL: ttl})
	if err := cmd.Err(); err != nil && err != redis.Nil {
		return nil, false, fmt.Errorf("try acquire redis lock for %s: %w", key, err)
	}
	if cmd.Val() != "OK" {
		return nil, false, nil
	}
	return l.buildRelease(key, token), true, nil
}
