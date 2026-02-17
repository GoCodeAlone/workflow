package migration

import (
	"context"
	"testing"
)

func TestSQLiteLock_AcquireRelease(t *testing.T) {
	lock := NewSQLiteLock(nil)
	ctx := context.Background()

	release, err := lock.Acquire(ctx, "test_lock")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}

	// Lock was acquired successfully; release it.
	release()
}

func TestSQLiteLock_MultipleAcquires(t *testing.T) {
	lock := NewSQLiteLock(nil)
	ctx := context.Background()

	// Acquire and release multiple times sequentially.
	for i := 0; i < 5; i++ {
		release, err := lock.Acquire(ctx, "test_lock")
		if err != nil {
			t.Fatalf("acquire %d: %v", i, err)
		}
		release()
	}
}

func TestSQLiteLock_CancelledContext(t *testing.T) {
	lock := NewSQLiteLock(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := lock.Acquire(ctx, "test_lock")
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
}

func TestHashLockKey(t *testing.T) {
	tests := []struct {
		key1 string
		key2 string
		same bool
	}{
		{"migration_runner", "migration_runner", true},
		{"key_a", "key_b", false},
		{"", "", true},
	}

	for _, tt := range tests {
		h1 := hashLockKey(tt.key1)
		h2 := hashLockKey(tt.key2)
		if (h1 == h2) != tt.same {
			t.Errorf("hashLockKey(%q) == hashLockKey(%q): got %v, want %v", tt.key1, tt.key2, h1 == h2, tt.same)
		}
	}
}

func TestPostgresLock_Interface(t *testing.T) {
	// Verify PostgresLock satisfies the DistributedLock interface at compile time.
	var _ DistributedLock = (*PostgresLock)(nil)
	var _ DistributedLock = (*SQLiteLock)(nil)
}
