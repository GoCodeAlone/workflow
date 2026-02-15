package cache

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Conn represents a pooled connection.
type Conn interface {
	// Close returns the connection to the pool or destroys it.
	Close() error
	// Ping checks if the connection is still alive.
	Ping(ctx context.Context) error
}

// ConnFactory creates new connections.
type ConnFactory func(ctx context.Context) (Conn, error)

// ConnectionPoolConfig configures the connection pool.
type ConnectionPoolConfig struct {
	// MinConns is the minimum number of connections kept open.
	MinConns int
	// MaxConns is the maximum number of connections allowed.
	MaxConns int
	// IdleTimeout is how long an idle connection is kept before being closed.
	IdleTimeout time.Duration
	// HealthCheckInterval is how often idle connections are health-checked.
	HealthCheckInterval time.Duration
	// AcquireTimeout is the maximum time to wait for a connection.
	AcquireTimeout time.Duration
}

// DefaultConnectionPoolConfig returns sensible defaults.
func DefaultConnectionPoolConfig() ConnectionPoolConfig {
	return ConnectionPoolConfig{
		MinConns:            2,
		MaxConns:            20,
		IdleTimeout:         5 * time.Minute,
		HealthCheckInterval: 30 * time.Second,
		AcquireTimeout:      5 * time.Second,
	}
}

type pooledConn struct {
	conn      Conn
	lastUsed  time.Time
	createdAt time.Time
}

// ConnectionPool manages a pool of reusable connections with health checking.
type ConnectionPool struct {
	cfg     ConnectionPoolConfig
	factory ConnFactory

	mu       sync.Mutex
	idle     []*pooledConn
	totalOut atomic.Int64 // connections currently checked out
	totalAll atomic.Int64 // total connections (idle + checked out)

	closed     bool
	closeCh    chan struct{}
	sem        chan struct{} // semaphore limiting total connections
	acquireOK  atomic.Int64
	acquireErr atomic.Int64
}

// NewConnectionPool creates a new connection pool.
func NewConnectionPool(cfg ConnectionPoolConfig, factory ConnFactory) *ConnectionPool {
	if cfg.MinConns < 0 {
		cfg.MinConns = 0
	}
	if cfg.MaxConns < 1 {
		cfg.MaxConns = 1
	}
	if cfg.MaxConns < cfg.MinConns {
		cfg.MaxConns = cfg.MinConns
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 5 * time.Minute
	}
	if cfg.HealthCheckInterval <= 0 {
		cfg.HealthCheckInterval = 30 * time.Second
	}
	if cfg.AcquireTimeout <= 0 {
		cfg.AcquireTimeout = 5 * time.Second
	}

	p := &ConnectionPool{
		cfg:     cfg,
		factory: factory,
		idle:    make([]*pooledConn, 0, cfg.MaxConns),
		closeCh: make(chan struct{}),
		sem:     make(chan struct{}, cfg.MaxConns),
	}

	return p
}

// Start initializes the pool with minimum connections and starts the health checker.
func (p *ConnectionPool) Start(ctx context.Context) error {
	for i := 0; i < p.cfg.MinConns; i++ {
		conn, err := p.factory(ctx)
		if err != nil {
			return fmt.Errorf("failed to create initial connection %d: %w", i, err)
		}
		p.mu.Lock()
		p.idle = append(p.idle, &pooledConn{
			conn:      conn,
			lastUsed:  time.Now(),
			createdAt: time.Now(),
		})
		p.mu.Unlock()
		p.totalAll.Add(1)
		p.sem <- struct{}{} // reserve a slot
	}

	go p.healthCheckLoop()

	return nil
}

// Acquire obtains a connection from the pool.
func (p *ConnectionPool) Acquire(ctx context.Context) (Conn, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, fmt.Errorf("pool is closed")
	}
	p.mu.Unlock()

	// Try to get an idle connection first
	p.mu.Lock()
	for len(p.idle) > 0 {
		// Pop from the end (LIFO for warm connections)
		pc := p.idle[len(p.idle)-1]
		p.idle = p.idle[:len(p.idle)-1]

		// Check idle timeout
		if time.Since(pc.lastUsed) > p.cfg.IdleTimeout {
			_ = pc.conn.Close()
			<-p.sem // release slot
			p.totalAll.Add(-1)
			continue
		}

		p.mu.Unlock()
		p.totalOut.Add(1)
		p.acquireOK.Add(1)
		return &returnableConn{conn: pc.conn, pool: p}, nil
	}
	p.mu.Unlock()

	// No idle connections available. Try to create a new one.
	acquireCtx, cancel := context.WithTimeout(ctx, p.cfg.AcquireTimeout)
	defer cancel()

	// Wait for a semaphore slot (respects MaxConns)
	select {
	case p.sem <- struct{}{}:
		// Got a slot, create a new connection
		conn, err := p.factory(acquireCtx)
		if err != nil {
			<-p.sem // release slot on failure
			p.acquireErr.Add(1)
			return nil, fmt.Errorf("failed to create connection: %w", err)
		}
		p.totalAll.Add(1)
		p.totalOut.Add(1)
		p.acquireOK.Add(1)
		return &returnableConn{conn: conn, pool: p}, nil

	case <-acquireCtx.Done():
		p.acquireErr.Add(1)
		return nil, fmt.Errorf("timed out waiting for connection: %w", acquireCtx.Err())
	}
}

// release returns a connection to the pool.
func (p *ConnectionPool) release(conn Conn) {
	p.totalOut.Add(-1)

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		_ = conn.Close()
		<-p.sem
		p.totalAll.Add(-1)
		return
	}

	p.idle = append(p.idle, &pooledConn{
		conn:      conn,
		lastUsed:  time.Now(),
		createdAt: time.Now(),
	})
	p.mu.Unlock()
}

// Close shuts down the pool and closes all connections.
func (p *ConnectionPool) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	close(p.closeCh)
	idle := p.idle
	p.idle = nil
	p.mu.Unlock()

	for _, pc := range idle {
		_ = pc.conn.Close()
		<-p.sem
		p.totalAll.Add(-1)
	}

	return nil
}

// Stats returns current pool statistics.
func (p *ConnectionPool) Stats() ConnectionPoolStats {
	p.mu.Lock()
	idleCount := len(p.idle)
	p.mu.Unlock()

	return ConnectionPoolStats{
		TotalConns:   int(p.totalAll.Load()),
		IdleConns:    idleCount,
		ActiveConns:  int(p.totalOut.Load()),
		AcquireCount: p.acquireOK.Load(),
		AcquireErr:   p.acquireErr.Load(),
		MaxConns:     p.cfg.MaxConns,
	}
}

// ConnectionPoolStats holds pool statistics.
type ConnectionPoolStats struct {
	TotalConns   int
	IdleConns    int
	ActiveConns  int
	AcquireCount int64
	AcquireErr   int64
	MaxConns     int
}

func (p *ConnectionPool) healthCheckLoop() {
	ticker := time.NewTicker(p.cfg.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.runHealthChecks()
		case <-p.closeCh:
			return
		}
	}
}

func (p *ConnectionPool) runHealthChecks() {
	p.mu.Lock()
	toCheck := make([]*pooledConn, len(p.idle))
	copy(toCheck, p.idle)
	p.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var healthy []*pooledConn
	var unhealthy []*pooledConn

	for _, pc := range toCheck {
		if time.Since(pc.lastUsed) > p.cfg.IdleTimeout {
			unhealthy = append(unhealthy, pc)
			continue
		}
		if err := pc.conn.Ping(ctx); err != nil {
			unhealthy = append(unhealthy, pc)
		} else {
			healthy = append(healthy, pc)
		}
	}

	// Close unhealthy connections
	for _, pc := range unhealthy {
		_ = pc.conn.Close()
		<-p.sem
		p.totalAll.Add(-1)
	}

	p.mu.Lock()
	p.idle = healthy
	p.mu.Unlock()

	// Replenish to MinConns if needed
	currentTotal := int(p.totalAll.Load())
	if currentTotal < p.cfg.MinConns {
		for i := currentTotal; i < p.cfg.MinConns; i++ {
			conn, err := p.factory(ctx)
			if err != nil {
				break
			}
			p.mu.Lock()
			p.idle = append(p.idle, &pooledConn{
				conn:      conn,
				lastUsed:  time.Now(),
				createdAt: time.Now(),
			})
			p.mu.Unlock()
			p.totalAll.Add(1)
			p.sem <- struct{}{}
		}
	}
}

// returnableConn wraps a connection so Close returns it to the pool.
type returnableConn struct {
	conn Conn
	pool *ConnectionPool
	once sync.Once
}

func (r *returnableConn) Close() error {
	r.once.Do(func() {
		r.pool.release(r.conn)
	})
	return nil
}

func (r *returnableConn) Ping(ctx context.Context) error {
	return r.conn.Ping(ctx)
}
