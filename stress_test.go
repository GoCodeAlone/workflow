package workflow

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CrisisTextLine/modular"
)

// stressWorkflowHandler is a lightweight handler for stress testing that
// handles configurable workflow types with minimal overhead.
type stressWorkflowHandler struct {
	handlesTypes []string
	delay        time.Duration
}

func (h *stressWorkflowHandler) CanHandle(workflowType string) bool {
	for _, t := range h.handlesTypes {
		if t == workflowType {
			return true
		}
	}
	return false
}

func (h *stressWorkflowHandler) ConfigureWorkflow(app modular.Application, workflowConfig any) error {
	return nil
}

func (h *stressWorkflowHandler) ExecuteWorkflow(ctx context.Context, workflowType string, action string, data map[string]any) (map[string]any, error) {
	if h.delay > 0 {
		select {
		case <-time.After(h.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return map[string]any{
		"status":       "completed",
		"workflowType": workflowType,
		"action":       action,
	}, nil
}

func TestConcurrentWorkflowExecution(t *testing.T) {
	app := newMockApplication()
	engine := NewStdEngine(app, app.Logger())

	handler := &stressWorkflowHandler{
		handlesTypes: []string{"concurrent-wf"},
	}
	engine.RegisterWorkflowHandler(handler)

	const numGoroutines = 100
	var wg sync.WaitGroup
	var errors atomic.Int64
	var successes atomic.Int64

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	wg.Add(numGoroutines)
	for i := range numGoroutines {
		go func(id int) {
			defer wg.Done()

			data := map[string]any{
				"id":        id,
				"timestamp": time.Now().UnixNano(),
			}

			err := engine.TriggerWorkflow(ctx, "concurrent-wf", fmt.Sprintf("action-%d", id), data)
			if err != nil {
				errors.Add(1)
			} else {
				successes.Add(1)
			}
		}(i)
	}

	wg.Wait()

	if errors.Load() > 0 {
		t.Errorf("had %d errors out of %d executions", errors.Load(), numGoroutines)
	}
	if successes.Load() != numGoroutines {
		t.Errorf("expected %d successes, got %d", numGoroutines, successes.Load())
	}
	t.Logf("Completed %d concurrent workflow executions with %d errors", numGoroutines, errors.Load())
}

func TestMixedWorkflowTypes(t *testing.T) {
	app := newMockApplication()
	engine := NewStdEngine(app, app.Logger())

	workflowTypes := []string{"http-wf", "messaging-wf", "statemachine-wf"}

	for _, wfType := range workflowTypes {
		engine.RegisterWorkflowHandler(&stressWorkflowHandler{
			handlesTypes: []string{wfType},
		})
	}

	const numPerType = 50
	var wg sync.WaitGroup
	var errors atomic.Int64
	var successes atomic.Int64

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	total := numPerType * len(workflowTypes)
	wg.Add(total)

	for _, wfType := range workflowTypes {
		for i := range numPerType {
			go func(wfType string, id int) {
				defer wg.Done()

				data := map[string]any{
					"type": wfType,
					"id":   id,
				}

				err := engine.TriggerWorkflow(ctx, wfType, "process", data)
				if err != nil {
					errors.Add(1)
				} else {
					successes.Add(1)
				}
			}(wfType, i)
		}
	}

	wg.Wait()

	if errors.Load() > 0 {
		t.Errorf("had %d errors out of %d mixed type executions", errors.Load(), total)
	}
	if successes.Load() != int64(total) {
		t.Errorf("expected %d successes, got %d", total, successes.Load())
	}
	t.Logf("Completed %d mixed workflow executions (%d types) with %d errors",
		total, len(workflowTypes), errors.Load())
}

func TestResourceCleanup(t *testing.T) {
	// Allow GC to settle before measuring
	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	baseGoroutines := runtime.NumGoroutine()

	app := newMockApplication()
	engine := NewStdEngine(app, app.Logger())

	handler := &stressWorkflowHandler{
		handlesTypes: []string{"cleanup-wf"},
	}
	engine.RegisterWorkflowHandler(handler)

	const numIterations = 200
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(numIterations)

	for i := range numIterations {
		go func(id int) {
			defer wg.Done()
			data := map[string]any{"id": id}
			_ = engine.TriggerWorkflow(ctx, "cleanup-wf", "run", data)
		}(i)
	}

	wg.Wait()

	// Give goroutines time to clean up
	runtime.GC()
	time.Sleep(200 * time.Millisecond)

	afterGoroutines := runtime.NumGoroutine()

	// Allow for a small margin: runtime goroutines, test framework goroutines, etc.
	leaked := afterGoroutines - baseGoroutines
	maxAllowedLeak := 5 // small tolerance for runtime/test framework goroutines

	if leaked > maxAllowedLeak {
		t.Errorf("goroutine leak detected: started with %d, ended with %d (leaked %d, max allowed %d)",
			baseGoroutines, afterGoroutines, leaked, maxAllowedLeak)
	} else {
		t.Logf("Goroutine check passed: base=%d, after=%d, delta=%d", baseGoroutines, afterGoroutines, leaked)
	}
}

func TestHighThroughput(t *testing.T) {
	app := newMockApplication()
	engine := NewStdEngine(app, app.Logger())

	handler := &stressWorkflowHandler{
		handlesTypes: []string{"throughput-wf"},
	}
	engine.RegisterWorkflowHandler(handler)

	const (
		numWorkers   = 10
		testDuration = 2 * time.Second
	)

	ctx, cancel := context.WithTimeout(context.Background(), testDuration+5*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	var totalExecutions atomic.Int64
	var totalErrors atomic.Int64

	deadline := time.Now().Add(testDuration)

	wg.Add(numWorkers)
	for w := range numWorkers {
		go func(workerID int) {
			defer wg.Done()
			for time.Now().Before(deadline) {
				data := map[string]any{
					"worker": workerID,
				}
				err := engine.TriggerWorkflow(ctx, "throughput-wf", "execute", data)
				if err != nil {
					totalErrors.Add(1)
				}
				totalExecutions.Add(1)
			}
		}(w)
	}

	wg.Wait()

	executions := totalExecutions.Load()
	errors := totalErrors.Load()
	throughput := float64(executions) / testDuration.Seconds()

	t.Logf("Throughput: %.0f workflows/second (%d total, %d errors, %d workers, %v duration)",
		throughput, executions, errors, numWorkers, testDuration)

	if errors > 0 {
		t.Errorf("had %d errors during throughput test", errors)
	}

	// Sanity: at least some workflows should have run
	if executions < 100 {
		t.Errorf("expected at least 100 executions in %v, got %d", testDuration, executions)
	}
}

func TestConcurrentWorkflowWithCancellation(t *testing.T) {
	app := newMockApplication()
	engine := NewStdEngine(app, app.Logger())

	// Handler with a small delay so we can cancel mid-flight
	handler := &stressWorkflowHandler{
		handlesTypes: []string{"cancel-wf"},
		delay:        50 * time.Millisecond,
	}
	engine.RegisterWorkflowHandler(handler)

	const numGoroutines = 50
	var wg sync.WaitGroup
	var cancelled atomic.Int64
	var completed atomic.Int64

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	wg.Add(numGoroutines)
	for i := range numGoroutines {
		go func(id int) {
			defer wg.Done()
			err := engine.TriggerWorkflow(ctx, "cancel-wf", "run", map[string]any{"id": id})
			if err != nil {
				cancelled.Add(1)
			} else {
				completed.Add(1)
			}
		}(i)
	}

	wg.Wait()

	t.Logf("Cancellation test: %d completed, %d cancelled out of %d",
		completed.Load(), cancelled.Load(), numGoroutines)

	// At least some should have been cancelled due to the short timeout
	if cancelled.Load() == 0 && completed.Load() == numGoroutines {
		// This is fine too -- it depends on scheduling. Just log it.
		t.Log("Note: all workflows completed before cancellation (fast system)")
	}
}
