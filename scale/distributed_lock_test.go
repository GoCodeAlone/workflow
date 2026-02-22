package scale

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func TestInMemoryLockAcquireRelease(t *testing.T) {
	lock := NewInMemoryLock()
	ctx := context.Background()

	release, err := lock.Acquire(ctx, "test-key", 0)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	if release == nil {
		t.Fatal("release function should not be nil")
	}

	// Release the lock
	release()

	// Should be able to acquire again after release
	release2, err := lock.Acquire(ctx, "test-key", 0)
	if err != nil {
		t.Fatalf("second Acquire failed: %v", err)
	}
	defer release2()
}

func TestInMemoryLockContention(t *testing.T) {
	lock := NewInMemoryLock()
	ctx := context.Background()

	// Verify mutual exclusion: two goroutines competing for the same key
	// should never overlap their critical sections.
	var (
		counter  int64
		maxSeen  atomic.Int64
		wg       sync.WaitGroup
		acquired atomic.Int64
	)

	const goroutines = 10
	const iterations = 50

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				release, err := lock.Acquire(ctx, "contended-key", 0)
				if err != nil {
					t.Errorf("Acquire failed: %v", err)
					return
				}

				// Critical section: increment counter, check for concurrent access
				val := atomic.AddInt64(&counter, 1)
				if val > 1 {
					maxSeen.Store(val)
				}
				// Simulate some work
				time.Sleep(10 * time.Microsecond)
				atomic.AddInt64(&counter, -1)

				acquired.Add(1)
				release()
			}
		}()
	}

	wg.Wait()

	if maxSeen.Load() > 1 {
		t.Errorf("mutual exclusion violated: saw %d concurrent holders", maxSeen.Load())
	}

	expected := int64(goroutines * iterations)
	if acquired.Load() != expected {
		t.Errorf("expected %d acquisitions, got %d", expected, acquired.Load())
	}
}

func TestInMemoryLockTryAcquire(t *testing.T) {
	lock := NewInMemoryLock()
	ctx := context.Background()

	// First TryAcquire should succeed
	release, acquired, err := lock.TryAcquire(ctx, "try-key", 0)
	if err != nil {
		t.Fatalf("TryAcquire failed: %v", err)
	}
	if !acquired {
		t.Fatal("expected to acquire the lock")
	}
	if release == nil {
		t.Fatal("release function should not be nil")
	}

	// Second TryAcquire should fail (non-blocking)
	_, acquired2, err := lock.TryAcquire(ctx, "try-key", 0)
	if err != nil {
		t.Fatalf("second TryAcquire failed: %v", err)
	}
	if acquired2 {
		t.Error("expected lock to be unavailable")
	}

	// Release and try again
	release()

	release3, acquired3, err := lock.TryAcquire(ctx, "try-key", 0)
	if err != nil {
		t.Fatalf("third TryAcquire failed: %v", err)
	}
	if !acquired3 {
		t.Error("expected to acquire the lock after release")
	}
	defer release3()
}

func TestInMemoryLockContextCancel(t *testing.T) {
	lock := NewInMemoryLock()
	ctx := context.Background()

	// Hold the lock
	release, err := lock.Acquire(ctx, "cancel-key", 0)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	// Try to acquire with a cancelled context
	cancelCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err = lock.Acquire(cancelCtx, "cancel-key", 0)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error from cancelled context")
	}

	if elapsed < 40*time.Millisecond {
		t.Errorf("expected to wait at least ~50ms, waited %v", elapsed)
	}

	if elapsed > 500*time.Millisecond {
		t.Errorf("expected to fail quickly after context cancel, waited %v", elapsed)
	}

	// Clean up
	release()
}

func TestInMemoryLockTTL(t *testing.T) {
	lock := NewInMemoryLock()
	ctx := context.Background()

	// Acquire with a short TTL
	_, err := lock.Acquire(ctx, "ttl-key", 50*time.Millisecond)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	// Lock should auto-release after TTL
	time.Sleep(100 * time.Millisecond)

	// Should be able to acquire again
	release2, acquired, err := lock.TryAcquire(ctx, "ttl-key", 0)
	if err != nil {
		t.Fatalf("TryAcquire after TTL failed: %v", err)
	}
	if !acquired {
		t.Error("expected lock to be available after TTL expired")
	}
	if release2 != nil {
		release2()
	}
}

func TestInMemoryLockDifferentKeys(t *testing.T) {
	lock := NewInMemoryLock()
	ctx := context.Background()

	// Locks on different keys should not interfere
	release1, err := lock.Acquire(ctx, "key-a", 0)
	if err != nil {
		t.Fatalf("Acquire key-a failed: %v", err)
	}

	release2, err := lock.Acquire(ctx, "key-b", 0)
	if err != nil {
		t.Fatalf("Acquire key-b failed: %v", err)
	}

	release1()
	release2()
}

func TestInMemoryLockDoubleRelease(t *testing.T) {
	lock := NewInMemoryLock()
	ctx := context.Background()

	release, err := lock.Acquire(ctx, "double-release", 0)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	// Double release should be safe (no panic)
	release()
	release()
}

func TestPGAdvisoryLockHashConsistency(t *testing.T) {
	// Verify that the same key always produces the same hash
	keys := []string{
		"state-machine:order-123",
		"workflow:tenant-abc:instance-xyz",
		"lock:key:with:colons",
		"",
		"a",
		"very-long-key-that-might-be-a-full-resource-identifier-with-many-segments",
	}

	for _, key := range keys {
		hash1 := hashToInt64(key)
		hash2 := hashToInt64(key)
		if hash1 != hash2 {
			t.Errorf("inconsistent hash for %q: %d != %d", key, hash1, hash2)
		}
	}

	// Different keys should (very likely) produce different hashes
	h1 := hashToInt64("key-alpha")
	h2 := hashToInt64("key-beta")
	if h1 == h2 {
		t.Error("different keys produced the same hash (unlikely collision)")
	}
}

func TestRedisLockAcquireRelease(t *testing.T) {
	mr := miniredis.RunT(t)
	lock := NewRedisLock(mr.Addr())
	defer lock.Close() //nolint:errcheck

	ctx := context.Background()

	release, err := lock.Acquire(ctx, "test-key", time.Second)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	if release == nil {
		t.Fatal("release function should not be nil")
	}

	release()

	// Lock should be available again after release.
	release2, err := lock.Acquire(ctx, "test-key", time.Second)
	if err != nil {
		t.Fatalf("second Acquire failed: %v", err)
	}
	defer release2()
}

func TestRedisLockTryAcquireFree(t *testing.T) {
	mr := miniredis.RunT(t)
	lock := NewRedisLock(mr.Addr())
	defer lock.Close() //nolint:errcheck

	ctx := context.Background()

	release, acquired, err := lock.TryAcquire(ctx, "try-key", time.Second)
	if err != nil {
		t.Fatalf("TryAcquire failed: %v", err)
	}
	if !acquired {
		t.Fatal("expected to acquire the free lock")
	}
	if release == nil {
		t.Fatal("release function should not be nil")
	}
	defer release()
}

func TestRedisLockTryAcquireHeld(t *testing.T) {
	mr := miniredis.RunT(t)
	lock := NewRedisLock(mr.Addr())
	defer lock.Close() //nolint:errcheck

	ctx := context.Background()

	release, acquired, err := lock.TryAcquire(ctx, "held-key", time.Second)
	if err != nil || !acquired {
		t.Fatalf("first TryAcquire failed: acquired=%v err=%v", acquired, err)
	}
	defer release()

	_, acquired2, err := lock.TryAcquire(ctx, "held-key", time.Second)
	if err != nil {
		t.Fatalf("second TryAcquire returned unexpected error: %v", err)
	}
	if acquired2 {
		t.Error("expected TryAcquire to fail when lock is held")
	}
}

func TestRedisLockReleaseIdempotent(t *testing.T) {
	mr := miniredis.RunT(t)
	lock := NewRedisLock(mr.Addr())
	defer lock.Close() //nolint:errcheck

	ctx := context.Background()

	release, err := lock.Acquire(ctx, "idempotent-key", time.Second)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	// Calling release twice must not panic or error.
	release()
	release()
}

func TestRedisLockDifferentHolderCannotRelease(t *testing.T) {
	mr := miniredis.RunT(t)
	lock1 := NewRedisLock(mr.Addr())
	lock2 := NewRedisLock(mr.Addr())
	defer lock1.Close() //nolint:errcheck
	defer lock2.Close() //nolint:errcheck

	ctx := context.Background()

	// lock1 acquires the lock.
	release1, err := lock1.Acquire(ctx, "safety-key", time.Second)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	defer release1()

	// lock2 tries to acquire — should not succeed immediately.
	_, acquired, err := lock2.TryAcquire(ctx, "safety-key", time.Second)
	if err != nil {
		t.Fatalf("TryAcquire on lock2 returned error: %v", err)
	}
	if acquired {
		t.Fatal("lock2 should not have acquired a lock held by lock1")
	}

	// Simulate lock2 calling the release function it should not have —
	// build a release with a wrong token and ensure lock1's key is intact.
	fakeRelease := lock2.buildRelease("safety-key", "wrong-token")
	fakeRelease() // must not delete lock1's key

	// lock1's lock should still be held.
	_, stillHeld, err := lock2.TryAcquire(ctx, "safety-key", time.Second)
	if err != nil {
		t.Fatalf("TryAcquire after fake release returned error: %v", err)
	}
	if stillHeld {
		t.Error("lock1's lock should still be held after lock2 attempted a fake release")
	}
}

func TestRedisLockTTLExpiry(t *testing.T) {
	mr := miniredis.RunT(t)
	lock := NewRedisLock(mr.Addr())
	defer lock.Close() //nolint:errcheck

	ctx := context.Background()

	_, err := lock.Acquire(ctx, "ttl-key", 500*time.Millisecond)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	// Advance miniredis clock past the TTL.
	mr.FastForward(time.Second)

	// Lock should be available after TTL expiry.
	release, acquired, err := lock.TryAcquire(ctx, "ttl-key", time.Second)
	if err != nil {
		t.Fatalf("TryAcquire after TTL failed: %v", err)
	}
	if !acquired {
		t.Error("expected lock to be available after TTL expired")
	}
	if release != nil {
		release()
	}
}

func TestRedisLockAcquireContextCancel(t *testing.T) {
	mr := miniredis.RunT(t)
	lock := NewRedisLock(mr.Addr())
	defer lock.Close() //nolint:errcheck

	ctx := context.Background()

	// Hold the lock with the same client (use a different key path via options).
	release, err := lock.Acquire(ctx, "cancel-redis-key", time.Minute)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	defer release()

	// Second acquire should block until context is cancelled.
	cancelCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	_, err = lock.Acquire(cancelCtx, "cancel-redis-key", time.Minute)
	if err == nil {
		t.Fatal("expected error when context is cancelled")
	}
}

func TestRedisLockNewRedisLockWithOptions(t *testing.T) {
	mr := miniredis.RunT(t)
	lock := NewRedisLockWithOptions(mr.Addr(), "", 0)
	defer lock.Close() //nolint:errcheck

	ctx := context.Background()

	release, acquired, err := lock.TryAcquire(ctx, "opts-key", time.Second)
	if err != nil {
		t.Fatalf("TryAcquire failed: %v", err)
	}
	if !acquired {
		t.Fatal("expected to acquire lock with options constructor")
	}
	defer release()
}

func TestDistributedLockInterface(t *testing.T) {
	// Verify all implementations satisfy the interface at compile time.
	var _ DistributedLock = (*InMemoryLock)(nil)
	var _ DistributedLock = (*PGAdvisoryLock)(nil)
	var _ DistributedLock = (*RedisLock)(nil)
}
