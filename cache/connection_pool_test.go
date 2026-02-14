package cache

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockConn is a test connection implementation.
type mockConn struct {
	closed   atomic.Bool
	pingErr  error
	closeErr error
}

func (m *mockConn) Close() error {
	m.closed.Store(true)
	return m.closeErr
}

func (m *mockConn) Ping(ctx context.Context) error {
	return m.pingErr
}

func newMockFactory() ConnFactory {
	return func(ctx context.Context) (Conn, error) {
		return &mockConn{}, nil
	}
}

func newFailingFactory() ConnFactory {
	return func(ctx context.Context) (Conn, error) {
		return nil, fmt.Errorf("connection refused")
	}
}

func TestConnectionPoolStartStop(t *testing.T) {
	pool := NewConnectionPool(ConnectionPoolConfig{
		MinConns:            2,
		MaxConns:            5,
		IdleTimeout:         time.Minute,
		HealthCheckInterval: time.Minute,
		AcquireTimeout:      time.Second,
	}, newMockFactory())

	ctx := context.Background()
	if err := pool.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	stats := pool.Stats()
	if stats.TotalConns != 2 {
		t.Errorf("expected 2 total conns, got %d", stats.TotalConns)
	}
	if stats.IdleConns != 2 {
		t.Errorf("expected 2 idle conns, got %d", stats.IdleConns)
	}

	if err := pool.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

func TestConnectionPoolAcquireRelease(t *testing.T) {
	pool := NewConnectionPool(ConnectionPoolConfig{
		MinConns:            1,
		MaxConns:            5,
		IdleTimeout:         time.Minute,
		HealthCheckInterval: time.Minute,
		AcquireTimeout:      time.Second,
	}, newMockFactory())

	ctx := context.Background()
	if err := pool.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer pool.Close()

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	stats := pool.Stats()
	if stats.ActiveConns != 1 {
		t.Errorf("expected 1 active conn, got %d", stats.ActiveConns)
	}

	if err := conn.Close(); err != nil {
		t.Fatalf("Release failed: %v", err)
	}

	stats = pool.Stats()
	if stats.ActiveConns != 0 {
		t.Errorf("expected 0 active conns after release, got %d", stats.ActiveConns)
	}
	if stats.IdleConns != 1 {
		t.Errorf("expected 1 idle conn after release, got %d", stats.IdleConns)
	}
}

func TestConnectionPoolMaxConns(t *testing.T) {
	pool := NewConnectionPool(ConnectionPoolConfig{
		MinConns:            0,
		MaxConns:            2,
		IdleTimeout:         time.Minute,
		HealthCheckInterval: time.Minute,
		AcquireTimeout:      200 * time.Millisecond,
	}, newMockFactory())

	ctx := context.Background()
	if err := pool.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer pool.Close()

	// Acquire both connections
	c1, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire 1 failed: %v", err)
	}
	c2, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire 2 failed: %v", err)
	}

	// Third should time out
	_, err = pool.Acquire(ctx)
	if err == nil {
		t.Error("expected timeout error when pool is exhausted")
	}

	// Release one and try again
	c1.Close()

	c3, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire 3 after release failed: %v", err)
	}

	c2.Close()
	c3.Close()
}

func TestConnectionPoolClosed(t *testing.T) {
	pool := NewConnectionPool(DefaultConnectionPoolConfig(), newMockFactory())

	ctx := context.Background()
	if err := pool.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	pool.Close()

	_, err := pool.Acquire(ctx)
	if err == nil {
		t.Error("expected error acquiring from closed pool")
	}

	// Double close should not panic
	pool.Close()
}

func TestConnectionPoolFactoryFailure(t *testing.T) {
	pool := NewConnectionPool(ConnectionPoolConfig{
		MinConns:            0,
		MaxConns:            5,
		IdleTimeout:         time.Minute,
		HealthCheckInterval: time.Minute,
		AcquireTimeout:      time.Second,
	}, newFailingFactory())

	ctx := context.Background()
	// Start with 0 MinConns should succeed
	if err := pool.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer pool.Close()

	_, err := pool.Acquire(ctx)
	if err == nil {
		t.Error("expected error from failing factory")
	}

	stats := pool.Stats()
	if stats.AcquireErr != 1 {
		t.Errorf("expected 1 acquire error, got %d", stats.AcquireErr)
	}
}

func TestConnectionPoolStartFailure(t *testing.T) {
	pool := NewConnectionPool(ConnectionPoolConfig{
		MinConns:            3,
		MaxConns:            5,
		IdleTimeout:         time.Minute,
		HealthCheckInterval: time.Minute,
		AcquireTimeout:      time.Second,
	}, newFailingFactory())

	ctx := context.Background()
	if err := pool.Start(ctx); err == nil {
		t.Error("expected error starting pool with failing factory and MinConns > 0")
	}
}

func TestConnectionPoolConcurrentAcquire(t *testing.T) {
	pool := NewConnectionPool(ConnectionPoolConfig{
		MinConns:            2,
		MaxConns:            20,
		IdleTimeout:         time.Minute,
		HealthCheckInterval: time.Minute,
		AcquireTimeout:      10 * time.Second,
	}, newMockFactory())

	ctx := context.Background()
	if err := pool.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer pool.Close()

	const goroutines = 20
	var wg sync.WaitGroup
	var acquireOK atomic.Int64

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, err := pool.Acquire(ctx)
			if err != nil {
				t.Errorf("Acquire failed: %v", err)
				return
			}
			acquireOK.Add(1)
			time.Sleep(time.Millisecond)
			conn.Close()
		}()
	}

	wg.Wait()

	if got := acquireOK.Load(); got != goroutines {
		t.Errorf("expected %d successful acquires, got %d", goroutines, got)
	}
}

func TestConnectionPoolDoubleRelease(t *testing.T) {
	pool := NewConnectionPool(ConnectionPoolConfig{
		MinConns:            0,
		MaxConns:            5,
		IdleTimeout:         time.Minute,
		HealthCheckInterval: time.Minute,
		AcquireTimeout:      time.Second,
	}, newMockFactory())

	ctx := context.Background()
	if err := pool.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer pool.Close()

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	// First close should work
	conn.Close()

	// Second close should be a no-op (not panic or double-release)
	conn.Close()

	stats := pool.Stats()
	if stats.IdleConns != 1 {
		t.Errorf("expected 1 idle conn (not duplicated), got %d", stats.IdleConns)
	}
}

func TestConnectionPoolPing(t *testing.T) {
	pool := NewConnectionPool(ConnectionPoolConfig{
		MinConns:            0,
		MaxConns:            5,
		IdleTimeout:         time.Minute,
		HealthCheckInterval: time.Minute,
		AcquireTimeout:      time.Second,
	}, newMockFactory())

	ctx := context.Background()
	if err := pool.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer pool.Close()

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}
	defer conn.Close()

	if err := conn.Ping(ctx); err != nil {
		t.Errorf("Ping failed: %v", err)
	}
}

func TestDefaultConnectionPoolConfig(t *testing.T) {
	cfg := DefaultConnectionPoolConfig()
	if cfg.MinConns <= 0 {
		t.Error("MinConns should be positive")
	}
	if cfg.MaxConns < cfg.MinConns {
		t.Error("MaxConns should be >= MinConns")
	}
	if cfg.IdleTimeout <= 0 {
		t.Error("IdleTimeout should be positive")
	}
	if cfg.HealthCheckInterval <= 0 {
		t.Error("HealthCheckInterval should be positive")
	}
	if cfg.AcquireTimeout <= 0 {
		t.Error("AcquireTimeout should be positive")
	}
}
