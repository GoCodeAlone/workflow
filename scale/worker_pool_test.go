package scale

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkerPoolStartStop(t *testing.T) {
	pool := NewWorkerPool(WorkerPoolConfig{
		MinWorkers: 2,
		MaxWorkers: 4,
		QueueSize:  16,
	})

	ctx := context.Background()
	if err := pool.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	stats := pool.Stats()
	if stats.ActiveWorkers < 2 {
		t.Errorf("expected at least 2 active workers, got %d", stats.ActiveWorkers)
	}

	if err := pool.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestWorkerPoolDoubleStart(t *testing.T) {
	pool := NewWorkerPool(DefaultWorkerPoolConfig())
	ctx := context.Background()

	if err := pool.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer pool.Stop()

	if err := pool.Start(ctx); err == nil {
		t.Error("expected error on double start")
	}
}

func TestWorkerPoolSubmitAndResults(t *testing.T) {
	pool := NewWorkerPool(WorkerPoolConfig{
		MinWorkers: 2,
		MaxWorkers: 4,
		QueueSize:  64,
	})

	ctx := context.Background()
	if err := pool.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	const taskCount = 20
	var completed atomic.Int64

	for i := 0; i < taskCount; i++ {
		taskID := fmt.Sprintf("task-%d", i)
		err := pool.Submit(Task{
			ID:       taskID,
			TenantID: "tenant-1",
			Execute: func(ctx context.Context) error {
				completed.Add(1)
				return nil
			},
		})
		if err != nil {
			t.Fatalf("Submit failed: %v", err)
		}
	}

	// Collect results
	resultCount := 0
	timeout := time.After(5 * time.Second)
	for resultCount < taskCount {
		select {
		case result := <-pool.Results():
			if result.Err != nil {
				t.Errorf("task %s failed: %v", result.TaskID, result.Err)
			}
			resultCount++
		case <-timeout:
			t.Fatalf("timed out waiting for results, got %d/%d", resultCount, taskCount)
		}
	}

	if err := pool.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	if v := completed.Load(); v != taskCount {
		t.Errorf("expected %d completed, got %d", taskCount, v)
	}

	stats := pool.Stats()
	if stats.CompletedOK != taskCount {
		t.Errorf("expected %d completed OK in stats, got %d", taskCount, stats.CompletedOK)
	}
}

func TestWorkerPoolSubmitWhenStopped(t *testing.T) {
	pool := NewWorkerPool(DefaultWorkerPoolConfig())

	err := pool.Submit(Task{
		ID:      "task-1",
		Execute: func(ctx context.Context) error { return nil },
	})
	if err == nil {
		t.Error("expected error submitting to non-running pool")
	}
}

func TestWorkerPoolTaskError(t *testing.T) {
	pool := NewWorkerPool(WorkerPoolConfig{
		MinWorkers: 1,
		MaxWorkers: 2,
		QueueSize:  16,
	})

	ctx := context.Background()
	if err := pool.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer pool.Stop()

	_ = pool.Submit(Task{
		ID:       "fail-task",
		TenantID: "t1",
		Execute: func(ctx context.Context) error {
			return fmt.Errorf("simulated failure")
		},
	})

	select {
	case result := <-pool.Results():
		if result.Err == nil {
			t.Error("expected error in result")
		}
		if result.TaskID != "fail-task" {
			t.Errorf("expected task ID 'fail-task', got %q", result.TaskID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for result")
	}

	stats := pool.Stats()
	if stats.CompletedErr != 1 {
		t.Errorf("expected 1 error, got %d", stats.CompletedErr)
	}
}

func TestWorkerPoolConcurrentSubmit(t *testing.T) {
	pool := NewWorkerPool(WorkerPoolConfig{
		MinWorkers: 4,
		MaxWorkers: 16,
		QueueSize:  256,
	})

	ctx := context.Background()
	if err := pool.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	const goroutines = 10
	const tasksPerGoroutine = 20
	totalTasks := goroutines * tasksPerGoroutine

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < tasksPerGoroutine; i++ {
				_ = pool.Submit(Task{
					ID:       fmt.Sprintf("g%d-t%d", g, i),
					TenantID: fmt.Sprintf("tenant-%d", g),
					Execute: func(ctx context.Context) error {
						time.Sleep(time.Millisecond)
						return nil
					},
				})
			}
		}(g)
	}

	wg.Wait()

	// Collect all results
	collected := 0
	timeout := time.After(10 * time.Second)
	for collected < totalTasks {
		select {
		case <-pool.Results():
			collected++
		case <-timeout:
			t.Fatalf("timed out, collected %d/%d", collected, totalTasks)
		}
	}

	if err := pool.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestDefaultWorkerPoolConfig(t *testing.T) {
	cfg := DefaultWorkerPoolConfig()
	if cfg.MinWorkers <= 0 {
		t.Error("MinWorkers should be positive")
	}
	if cfg.MaxWorkers < cfg.MinWorkers {
		t.Error("MaxWorkers should be >= MinWorkers")
	}
	if cfg.QueueSize <= 0 {
		t.Error("QueueSize should be positive")
	}
	if cfg.IdleTimeout <= 0 {
		t.Error("IdleTimeout should be positive")
	}
}
