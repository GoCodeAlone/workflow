package scale

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Task represents a unit of work to be executed by the worker pool.
type Task struct {
	ID       string
	TenantID string
	Execute  func(ctx context.Context) error
}

// TaskResult holds the outcome of a task execution.
type TaskResult struct {
	TaskID   string
	TenantID string
	Err      error
	Duration time.Duration
}

// WorkerPoolConfig configures the worker pool.
type WorkerPoolConfig struct {
	// MinWorkers is the minimum number of goroutines kept alive.
	MinWorkers int
	// MaxWorkers is the maximum number of goroutines allowed.
	MaxWorkers int
	// QueueSize is the capacity of the task queue.
	QueueSize int
	// IdleTimeout is how long an idle worker waits before exiting (above MinWorkers).
	IdleTimeout time.Duration
}

// DefaultWorkerPoolConfig returns sensible defaults.
func DefaultWorkerPoolConfig() WorkerPoolConfig {
	return WorkerPoolConfig{
		MinWorkers:  4,
		MaxWorkers:  64,
		QueueSize:   1024,
		IdleTimeout: 30 * time.Second,
	}
}

// WorkerPool manages a pool of goroutines for executing workflow tasks.
type WorkerPool struct {
	cfg     WorkerPoolConfig
	tasks   chan Task
	results chan TaskResult
	wg      sync.WaitGroup
	cancel  context.CancelFunc
	ctx     context.Context

	activeWorkers atomic.Int64
	totalTasks    atomic.Int64
	completedOK   atomic.Int64
	completedErr  atomic.Int64

	mu      sync.Mutex
	running bool
}

// NewWorkerPool creates a new worker pool.
func NewWorkerPool(cfg WorkerPoolConfig) *WorkerPool {
	if cfg.MinWorkers <= 0 {
		cfg.MinWorkers = 4
	}
	if cfg.MaxWorkers < cfg.MinWorkers {
		cfg.MaxWorkers = cfg.MinWorkers
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 1024
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 30 * time.Second
	}

	return &WorkerPool{
		cfg:     cfg,
		tasks:   make(chan Task, cfg.QueueSize),
		results: make(chan TaskResult, cfg.QueueSize),
	}
}

// Start launches the minimum number of workers.
func (p *WorkerPool) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.running {
		return fmt.Errorf("worker pool already running")
	}

	p.ctx, p.cancel = context.WithCancel(ctx)
	p.running = true

	for i := 0; i < p.cfg.MinWorkers; i++ {
		p.spawnWorker(false)
	}

	return nil
}

// Submit adds a task to the queue. It spawns an extra worker if the queue is
// getting full and the pool hasn't reached MaxWorkers.
func (p *WorkerPool) Submit(task Task) error {
	p.mu.Lock()
	if !p.running {
		p.mu.Unlock()
		return fmt.Errorf("worker pool not running")
	}
	p.mu.Unlock()

	p.totalTasks.Add(1)

	// Try non-blocking send first
	select {
	case p.tasks <- task:
		p.maybeScale()
		return nil
	default:
	}

	// Queue is full, try scaling up
	p.maybeScale()

	// Blocking send with context
	select {
	case p.tasks <- task:
		return nil
	case <-p.ctx.Done():
		return p.ctx.Err()
	}
}

// Results returns the results channel for reading task outcomes.
func (p *WorkerPool) Results() <-chan TaskResult {
	return p.results
}

// Stop gracefully shuts down the worker pool.
func (p *WorkerPool) Stop() error {
	p.mu.Lock()
	if !p.running {
		p.mu.Unlock()
		return nil
	}
	p.running = false
	p.mu.Unlock()

	p.cancel()
	close(p.tasks)
	p.wg.Wait()
	close(p.results)
	return nil
}

// Stats returns current pool statistics.
func (p *WorkerPool) Stats() WorkerPoolStats {
	return WorkerPoolStats{
		ActiveWorkers:  int(p.activeWorkers.Load()),
		PendingTasks:   len(p.tasks),
		TotalSubmitted: p.totalTasks.Load(),
		CompletedOK:    p.completedOK.Load(),
		CompletedErr:   p.completedErr.Load(),
	}
}

// WorkerPoolStats holds pool statistics.
type WorkerPoolStats struct {
	ActiveWorkers  int
	PendingTasks   int
	TotalSubmitted int64
	CompletedOK    int64
	CompletedErr   int64
}

// maybeScale spawns an additional worker if the queue occupancy is above 75%
// and the worker count is below MaxWorkers.
func (p *WorkerPool) maybeScale() {
	queueLen := len(p.tasks)
	threshold := p.cfg.QueueSize * 3 / 4
	if queueLen > threshold && int(p.activeWorkers.Load()) < p.cfg.MaxWorkers {
		p.spawnWorker(true)
	}
}

// spawnWorker starts a new worker goroutine. If ephemeral is true the worker
// exits after IdleTimeout without work, as long as the count stays above MinWorkers.
func (p *WorkerPool) spawnWorker(ephemeral bool) {
	p.wg.Add(1)
	p.activeWorkers.Add(1)

	go func() {
		defer p.wg.Done()
		defer p.activeWorkers.Add(-1)

		idleTimer := time.NewTimer(p.cfg.IdleTimeout)
		defer idleTimer.Stop()

		for {
			// Reset the idle timer each iteration
			if !idleTimer.Stop() {
				select {
				case <-idleTimer.C:
				default:
				}
			}
			idleTimer.Reset(p.cfg.IdleTimeout)

			select {
			case task, ok := <-p.tasks:
				if !ok {
					return
				}
				start := time.Now()
				err := task.Execute(p.ctx)
				dur := time.Since(start)
				if err != nil {
					p.completedErr.Add(1)
				} else {
					p.completedOK.Add(1)
				}
				select {
				case p.results <- TaskResult{
					TaskID:   task.ID,
					TenantID: task.TenantID,
					Err:      err,
					Duration: dur,
				}:
				case <-p.ctx.Done():
					return
				}

			case <-idleTimer.C:
				if ephemeral && int(p.activeWorkers.Load()) > p.cfg.MinWorkers {
					return
				}

			case <-p.ctx.Done():
				return
			}
		}
	}()
}
