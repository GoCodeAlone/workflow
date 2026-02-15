package scale

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBulkheadAcquireRelease(t *testing.T) {
	bh := NewBulkhead(BulkheadConfig{
		DefaultMaxConcurrent: 5,
	})
	ctx := context.Background()

	release, err := bh.Acquire(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	if release == nil {
		t.Fatal("release function should not be nil")
	}

	stats := bh.Stats()
	defaultStats := stats["_default"]
	if defaultStats.Active != 1 {
		t.Errorf("expected 1 active, got %d", defaultStats.Active)
	}

	release()

	stats = bh.Stats()
	defaultStats = stats["_default"]
	if defaultStats.Active != 0 {
		t.Errorf("expected 0 active after release, got %d", defaultStats.Active)
	}
}

func TestBulkheadLimitExceeded(t *testing.T) {
	bh := NewBulkhead(BulkheadConfig{
		DefaultMaxConcurrent: 2,
	})
	ctx := context.Background()

	// Acquire two slots (the limit)
	release1, err := bh.Acquire(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("first Acquire failed: %v", err)
	}
	release2, err := bh.Acquire(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("second Acquire failed: %v", err)
	}

	// Third acquire should fail
	_, err = bh.Acquire(ctx, "tenant-1")
	if !errors.Is(err, ErrTenantLimitExceeded) {
		t.Errorf("expected ErrTenantLimitExceeded, got: %v", err)
	}

	// Release one and try again
	release1()

	release3, err := bh.Acquire(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("Acquire after release failed: %v", err)
	}

	release2()
	release3()
}

func TestBulkheadDefaultLimit(t *testing.T) {
	bh := NewBulkhead(BulkheadConfig{
		DefaultMaxConcurrent: 3,
	})
	ctx := context.Background()

	// Tenants without specific limits use the default
	releases := make([]func(), 0, 3)
	for i := 0; i < 3; i++ {
		release, err := bh.Acquire(ctx, "any-tenant")
		if err != nil {
			t.Fatalf("Acquire %d failed: %v", i, err)
		}
		releases = append(releases, release)
	}

	// Fourth should fail (default limit is 3)
	_, err := bh.Acquire(ctx, "any-tenant")
	if !errors.Is(err, ErrTenantLimitExceeded) {
		t.Errorf("expected ErrTenantLimitExceeded, got: %v", err)
	}

	for _, release := range releases {
		release()
	}
}

func TestBulkheadTenantIsolation(t *testing.T) {
	bh := NewBulkhead(BulkheadConfig{
		DefaultMaxConcurrent: 10,
	})
	ctx := context.Background()

	// Set tenant-A to a limit of 2
	bh.SetLimit("tenant-A", TenantLimitConfig{MaxConcurrent: 2})
	// Set tenant-B to a limit of 3
	bh.SetLimit("tenant-B", TenantLimitConfig{MaxConcurrent: 3})

	// Fill up tenant-A
	releaseA1, err := bh.Acquire(ctx, "tenant-A")
	if err != nil {
		t.Fatalf("tenant-A Acquire 1 failed: %v", err)
	}
	releaseA2, err := bh.Acquire(ctx, "tenant-A")
	if err != nil {
		t.Fatalf("tenant-A Acquire 2 failed: %v", err)
	}

	// tenant-A is full
	_, err = bh.Acquire(ctx, "tenant-A")
	if !errors.Is(err, ErrTenantLimitExceeded) {
		t.Error("expected tenant-A to be at capacity")
	}

	// tenant-B should still be available
	releaseB1, err := bh.Acquire(ctx, "tenant-B")
	if err != nil {
		t.Fatalf("tenant-B Acquire 1 failed: %v", err)
	}
	releaseB2, err := bh.Acquire(ctx, "tenant-B")
	if err != nil {
		t.Fatalf("tenant-B Acquire 2 failed: %v", err)
	}
	releaseB3, err := bh.Acquire(ctx, "tenant-B")
	if err != nil {
		t.Fatalf("tenant-B Acquire 3 failed: %v", err)
	}

	// tenant-B is now full
	_, err = bh.Acquire(ctx, "tenant-B")
	if !errors.Is(err, ErrTenantLimitExceeded) {
		t.Error("expected tenant-B to be at capacity")
	}

	// tenant-C uses the default limit (10)
	releaseC, err := bh.Acquire(ctx, "tenant-C")
	if err != nil {
		t.Fatalf("tenant-C Acquire failed: %v", err)
	}

	// Clean up
	releaseA1()
	releaseA2()
	releaseB1()
	releaseB2()
	releaseB3()
	releaseC()
}

func TestBulkheadStats(t *testing.T) {
	bh := NewBulkhead(BulkheadConfig{
		DefaultMaxConcurrent: 5,
	})
	ctx := context.Background()

	bh.SetLimit("tenant-X", TenantLimitConfig{MaxConcurrent: 2})

	// Acquire slots
	releaseX1, _ := bh.Acquire(ctx, "tenant-X")
	releaseX2, _ := bh.Acquire(ctx, "tenant-X")

	// Exceed limit to trigger rejection
	_, _ = bh.Acquire(ctx, "tenant-X")
	_, _ = bh.Acquire(ctx, "tenant-X")

	stats := bh.Stats()

	xStats, ok := stats["tenant-X"]
	if !ok {
		t.Fatal("expected stats for tenant-X")
	}

	if xStats.Active != 2 {
		t.Errorf("expected 2 active for tenant-X, got %d", xStats.Active)
	}
	if xStats.MaxConcurrent != 2 {
		t.Errorf("expected max_concurrent 2 for tenant-X, got %d", xStats.MaxConcurrent)
	}
	if xStats.Rejected != 2 {
		t.Errorf("expected 2 rejected for tenant-X, got %d", xStats.Rejected)
	}

	// Check default stats exist
	if _, ok := stats["_default"]; !ok {
		t.Error("expected _default stats entry")
	}

	releaseX1()
	releaseX2()

	// After release, active should be 0
	stats = bh.Stats()
	xStats = stats["tenant-X"]
	if xStats.Active != 0 {
		t.Errorf("expected 0 active after release, got %d", xStats.Active)
	}
	// Rejected count persists
	if xStats.Rejected != 2 {
		t.Errorf("expected rejected count to persist, got %d", xStats.Rejected)
	}
}

func TestBulkheadConcurrency(t *testing.T) {
	bh := NewBulkhead(BulkheadConfig{
		DefaultMaxConcurrent: 10,
	})
	ctx := context.Background()

	bh.SetLimit("stress-tenant", TenantLimitConfig{MaxConcurrent: 5})

	var (
		wg        sync.WaitGroup
		succeeded atomic.Int64
		rejected  atomic.Int64
		maxActive atomic.Int64
		curActive atomic.Int64
	)

	const goroutines = 20
	const iterations = 100

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				release, err := bh.Acquire(ctx, "stress-tenant")
				if err != nil {
					rejected.Add(1)
					continue
				}

				succeeded.Add(1)
				active := curActive.Add(1)
				// Track max concurrent
				for {
					current := maxActive.Load()
					if active <= current || maxActive.CompareAndSwap(current, active) {
						break
					}
				}

				// Simulate brief work
				time.Sleep(time.Microsecond)
				curActive.Add(-1)
				release()
			}
		}()
	}

	wg.Wait()

	total := succeeded.Load() + rejected.Load()
	expectedTotal := int64(goroutines * iterations)
	if total != expectedTotal {
		t.Errorf("expected %d total attempts, got %d", expectedTotal, total)
	}

	if maxActive.Load() > 5 {
		t.Errorf("concurrency limit violated: max active was %d, limit is 5", maxActive.Load())
	}

	if rejected.Load() == 0 {
		t.Log("no rejections occurred (possible but unlikely under heavy contention)")
	}

	stats := bh.Stats()
	tenantStats := stats["stress-tenant"]
	if tenantStats.Rejected != rejected.Load() {
		t.Errorf("stats rejected mismatch: stats=%d, counted=%d", tenantStats.Rejected, rejected.Load())
	}
}

func TestBulkheadAcquireWait(t *testing.T) {
	bh := NewBulkhead(BulkheadConfig{
		DefaultMaxConcurrent: 1,
	})
	ctx := context.Background()

	bh.SetLimit("wait-tenant", TenantLimitConfig{MaxConcurrent: 1})

	// Acquire the only slot
	release1, err := bh.AcquireWait(ctx, "wait-tenant")
	if err != nil {
		t.Fatalf("AcquireWait failed: %v", err)
	}

	// Second AcquireWait with short timeout should fail
	cancelCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()

	_, err = bh.AcquireWait(cancelCtx, "wait-tenant")
	if err == nil {
		t.Fatal("expected error from AcquireWait with full bulkhead")
	}

	// Release and try blocking acquire
	go func() {
		time.Sleep(20 * time.Millisecond)
		release1()
	}()

	waitCtx, waitCancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer waitCancel()

	release2, err := bh.AcquireWait(waitCtx, "wait-tenant")
	if err != nil {
		t.Fatalf("AcquireWait after release failed: %v", err)
	}
	defer release2()
}

func TestBulkheadRemoveLimit(t *testing.T) {
	bh := NewBulkhead(BulkheadConfig{
		DefaultMaxConcurrent: 10,
	})
	ctx := context.Background()

	bh.SetLimit("removable", TenantLimitConfig{MaxConcurrent: 1})

	release, err := bh.Acquire(ctx, "removable")
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	// Should hit limit
	_, err = bh.Acquire(ctx, "removable")
	if !errors.Is(err, ErrTenantLimitExceeded) {
		t.Error("expected limit exceeded")
	}

	release()

	// Remove the limit - tenant should now use the default (10)
	bh.RemoveLimit("removable")

	releases := make([]func(), 0, 10)
	for i := 0; i < 10; i++ {
		r, err := bh.Acquire(ctx, "removable")
		if err != nil {
			t.Fatalf("Acquire %d after RemoveLimit failed: %v", i, err)
		}
		releases = append(releases, r)
	}
	for _, r := range releases {
		r()
	}
}

func TestBulkheadDoubleRelease(t *testing.T) {
	bh := NewBulkhead(BulkheadConfig{
		DefaultMaxConcurrent: 5,
	})
	ctx := context.Background()

	release, err := bh.Acquire(ctx, "double")
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	// Double release should be safe (no panic, no extra slot returned)
	release()
	release()

	stats := bh.Stats()
	if stats["_default"].Active != 0 {
		t.Errorf("expected 0 active after double release, got %d", stats["_default"].Active)
	}
}

func TestDefaultBulkheadConfig(t *testing.T) {
	cfg := DefaultBulkheadConfig()
	if cfg.DefaultMaxConcurrent <= 0 {
		t.Error("DefaultMaxConcurrent should be positive")
	}
	if cfg.DefaultRateLimit <= 0 {
		t.Error("DefaultRateLimit should be positive")
	}
}
