package migration

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
)

// DistributedLock provides mutual exclusion for migration operations across
// multiple processes or nodes.
type DistributedLock interface {
	// Acquire obtains the lock for the given key. The returned release function
	// must be called to release the lock. The lock is held until release is called
	// or the context is cancelled.
	Acquire(ctx context.Context, key string) (release func(), err error)
}

// PostgresLock implements DistributedLock using PostgreSQL advisory locks.
type PostgresLock struct {
	db *sql.DB
}

// NewPostgresLock creates a new PostgresLock.
func NewPostgresLock(db *sql.DB) *PostgresLock {
	return &PostgresLock{db: db}
}

// Acquire obtains a PostgreSQL advisory lock. The lock key is hashed to an int64
// from the string key. The returned release function unlocks the advisory lock.
func (l *PostgresLock) Acquire(ctx context.Context, key string) (func(), error) {
	lockID := hashLockKey(key)

	_, err := l.db.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, lockID)
	if err != nil {
		return nil, fmt.Errorf("pg_advisory_lock(%d): %w", lockID, err)
	}

	release := func() {
		_, _ = l.db.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, lockID)
	}
	return release, nil
}

// SQLiteLock implements DistributedLock using a process-local mutex.
// SQLite is inherently single-writer, so a mutex is sufficient for
// ensuring only one migration runs at a time within a process.
// For cross-process safety, SQLite's built-in file locking provides protection.
type SQLiteLock struct {
	mu sync.Mutex
}

// NewSQLiteLock creates a new SQLiteLock. The db parameter is accepted for
// interface consistency but the lock uses an in-process mutex since SQLite
// does not support advisory locks.
func NewSQLiteLock(_ *sql.DB) *SQLiteLock {
	return &SQLiteLock{}
}

// Acquire obtains the mutex lock. Returns an error if the context is already cancelled.
func (l *SQLiteLock) Acquire(ctx context.Context, _ string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("acquire sqlite lock: %w", err)
	}

	l.mu.Lock()
	return func() { l.mu.Unlock() }, nil
}

// hashLockKey produces a stable int64 hash from a string key for use with
// pg_advisory_lock. Uses FNV-1a.
func hashLockKey(key string) int64 {
	var h uint64 = 14695981039346656037 // FNV offset basis
	for i := 0; i < len(key); i++ {
		h ^= uint64(key[i])
		h *= 1099511628211 // FNV prime
	}
	return int64(h & 0x7FFFFFFFFFFFFFFF) //nolint:gosec // intentional truncation for advisory lock key
}
