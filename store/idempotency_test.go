package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// testIdempotencyStoreFunc is a constructor that tests call to get a fresh store.
type testIdempotencyStoreFunc func(t *testing.T) IdempotencyStore

// runIdempotencyTests runs the full test suite against any IdempotencyStore implementation.
func runIdempotencyTests(t *testing.T, name string, newStore testIdempotencyStoreFunc) {
	t.Run(name+"/CheckNewKey", func(t *testing.T) {
		testCheckNewKey(t, newStore)
	})
	t.Run(name+"/StoreAndCheck", func(t *testing.T) {
		testStoreAndCheck(t, newStore)
	})
	t.Run(name+"/ExpiredKey", func(t *testing.T) {
		testExpiredKey(t, newStore)
	})
	t.Run(name+"/Cleanup", func(t *testing.T) {
		testCleanup(t, newStore)
	})
	t.Run(name+"/ConcurrentAccess", func(t *testing.T) {
		testConcurrentAccess(t, newStore)
	})
	t.Run(name+"/DuplicateKey", func(t *testing.T) {
		testDuplicateKey(t, newStore)
	})
}

// --- Test cases ---

func testCheckNewKey(t *testing.T, newStore testIdempotencyStoreFunc) {
	s := newStore(t)
	ctx := context.Background()

	rec, err := s.Check(ctx, "nonexistent-key")
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if rec != nil {
		t.Fatalf("expected nil for unknown key, got %+v", rec)
	}
}

func testStoreAndCheck(t *testing.T, newStore testIdempotencyStoreFunc) {
	s := newStore(t)
	ctx := context.Background()

	execID := uuid.New()
	result := json.RawMessage(`{"status":"ok","count":42}`)
	now := time.Now().Truncate(time.Second)

	rec := &IdempotencyRecord{
		Key:         "order-123-step-validate",
		ExecutionID: execID,
		StepName:    "validate",
		Result:      result,
		CreatedAt:   now,
		ExpiresAt:   now.Add(1 * time.Hour),
	}

	if err := s.Store(ctx, rec); err != nil {
		t.Fatalf("Store: %v", err)
	}

	got, err := s.Check(ctx, "order-123-step-validate")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if got == nil {
		t.Fatal("expected record, got nil")
	}
	if got.Key != rec.Key {
		t.Errorf("Key: got %q, want %q", got.Key, rec.Key)
	}
	if got.ExecutionID != execID {
		t.Errorf("ExecutionID: got %v, want %v", got.ExecutionID, execID)
	}
	if got.StepName != "validate" {
		t.Errorf("StepName: got %q, want %q", got.StepName, "validate")
	}
	if string(got.Result) != string(result) {
		t.Errorf("Result: got %s, want %s", got.Result, result)
	}
}

func testExpiredKey(t *testing.T, newStore testIdempotencyStoreFunc) {
	s := newStore(t)
	ctx := context.Background()

	// Store a record that is already expired.
	rec := &IdempotencyRecord{
		Key:         "expired-key",
		ExecutionID: uuid.New(),
		StepName:    "process",
		Result:      json.RawMessage(`{"done":true}`),
		CreatedAt:   time.Now().Add(-2 * time.Hour).Truncate(time.Second),
		ExpiresAt:   time.Now().Add(-1 * time.Hour).Truncate(time.Second),
	}

	if err := s.Store(ctx, rec); err != nil {
		t.Fatalf("Store: %v", err)
	}

	got, err := s.Check(ctx, "expired-key")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for expired key, got %+v", got)
	}
}

func testCleanup(t *testing.T, newStore testIdempotencyStoreFunc) {
	s := newStore(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Second)

	// Store two expired records.
	for _, key := range []string{"expired-1", "expired-2"} {
		rec := &IdempotencyRecord{
			Key:         key,
			ExecutionID: uuid.New(),
			StepName:    "step",
			Result:      json.RawMessage(`{}`),
			CreatedAt:   now.Add(-2 * time.Hour),
			ExpiresAt:   now.Add(-1 * time.Hour),
		}
		if err := s.Store(ctx, rec); err != nil {
			t.Fatalf("Store %s: %v", key, err)
		}
	}

	// Store one valid record.
	valid := &IdempotencyRecord{
		Key:         "valid-key",
		ExecutionID: uuid.New(),
		StepName:    "step",
		Result:      json.RawMessage(`{"valid":true}`),
		CreatedAt:   now,
		ExpiresAt:   now.Add(1 * time.Hour),
	}
	if err := s.Store(ctx, valid); err != nil {
		t.Fatalf("Store valid: %v", err)
	}

	removed, err := s.Cleanup(ctx)
	if err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if removed != 2 {
		t.Errorf("Cleanup removed %d keys, want 2", removed)
	}

	// Valid key should still exist.
	got, err := s.Check(ctx, "valid-key")
	if err != nil {
		t.Fatalf("Check valid: %v", err)
	}
	if got == nil {
		t.Fatal("valid key should still exist after cleanup")
	}

	// Expired keys should be gone.
	for _, key := range []string{"expired-1", "expired-2"} {
		got, err := s.Check(ctx, key)
		if err != nil {
			t.Fatalf("Check %s: %v", key, err)
		}
		if got != nil {
			t.Errorf("expected nil for cleaned-up key %s", key)
		}
	}
}

func testConcurrentAccess(t *testing.T, newStore testIdempotencyStoreFunc) {
	s := newStore(t)
	ctx := context.Background()

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)

	now := time.Now().Truncate(time.Second)

	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()

			key := uuid.New().String()
			rec := &IdempotencyRecord{
				Key:         key,
				ExecutionID: uuid.New(),
				StepName:    "concurrent-step",
				Result:      json.RawMessage(`{"n":` + json.Number(string(rune('0'+n%10))).String() + `}`),
				CreatedAt:   now,
				ExpiresAt:   now.Add(1 * time.Hour),
			}

			if err := s.Store(ctx, rec); err != nil {
				t.Errorf("Store goroutine %d: %v", n, err)
				return
			}

			got, err := s.Check(ctx, key)
			if err != nil {
				t.Errorf("Check goroutine %d: %v", n, err)
				return
			}
			if got == nil {
				t.Errorf("Check goroutine %d: expected record, got nil", n)
			}
		}(i)
	}

	wg.Wait()
}

func testDuplicateKey(t *testing.T, newStore testIdempotencyStoreFunc) {
	s := newStore(t)
	ctx := context.Background()

	now := time.Now().Truncate(time.Second)
	rec := &IdempotencyRecord{
		Key:         "dup-key",
		ExecutionID: uuid.New(),
		StepName:    "first",
		Result:      json.RawMessage(`{"attempt":1}`),
		CreatedAt:   now,
		ExpiresAt:   now.Add(1 * time.Hour),
	}

	if err := s.Store(ctx, rec); err != nil {
		t.Fatalf("Store first: %v", err)
	}

	// Second store with same key should return ErrDuplicate.
	dup := &IdempotencyRecord{
		Key:         "dup-key",
		ExecutionID: uuid.New(),
		StepName:    "second",
		Result:      json.RawMessage(`{"attempt":2}`),
		CreatedAt:   now,
		ExpiresAt:   now.Add(1 * time.Hour),
	}

	err := s.Store(ctx, dup)
	if err == nil {
		t.Fatal("expected error for duplicate key, got nil")
	}
	if err != ErrDuplicate {
		t.Fatalf("expected ErrDuplicate, got %v", err)
	}

	// Original record should be unchanged.
	got, err := s.Check(ctx, "dup-key")
	if err != nil {
		t.Fatalf("Check after dup: %v", err)
	}
	if got == nil {
		t.Fatal("expected original record")
	}
	if got.StepName != "first" {
		t.Errorf("StepName: got %q, want %q", got.StepName, "first")
	}
}

// --- Test runners ---

func TestInMemoryIdempotencyStore(t *testing.T) {
	runIdempotencyTests(t, "InMemory", func(t *testing.T) IdempotencyStore {
		t.Helper()
		return NewInMemoryIdempotencyStore()
	})
}

func TestSQLiteIdempotencyStore(t *testing.T) {
	runIdempotencyTests(t, "SQLite", func(t *testing.T) IdempotencyStore {
		t.Helper()
		// Use shared cache + WAL mode so concurrent goroutines share the same
		// in-memory database and don't block each other.
		dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared&_journal_mode=WAL&_busy_timeout=5000", t.Name())
		db, err := sql.Open("sqlite", dsn)
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		t.Cleanup(func() { db.Close() })

		store, err := NewSQLiteIdempotencyStore(db)
		if err != nil {
			t.Fatalf("NewSQLiteIdempotencyStore: %v", err)
		}
		return store
	})
}

// --- Interface compliance ---

func TestIdempotencyStoreInterface(t *testing.T) {
	var _ IdempotencyStore = (*InMemoryIdempotencyStore)(nil)
	var _ IdempotencyStore = (*SQLiteIdempotencyStore)(nil)
}
