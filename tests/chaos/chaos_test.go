package chaos

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/handlers"
	"github.com/GoCodeAlone/workflow/module"
)

type testLogger struct {
	mu      sync.Mutex
	entries []string
}

func (l *testLogger) Debug(msg string, args ...any) {
	l.mu.Lock()
	l.entries = append(l.entries, fmt.Sprintf("[DEBUG] "+msg, args...))
	l.mu.Unlock()
}
func (l *testLogger) Info(msg string, args ...any) {
	l.mu.Lock()
	l.entries = append(l.entries, fmt.Sprintf("[INFO] "+msg, args...))
	l.mu.Unlock()
}
func (l *testLogger) Warn(msg string, args ...any) {
	l.mu.Lock()
	l.entries = append(l.entries, fmt.Sprintf("[WARN] "+msg, args...))
	l.mu.Unlock()
}
func (l *testLogger) Error(msg string, args ...any) {
	l.mu.Lock()
	l.entries = append(l.entries, fmt.Sprintf("[ERROR] "+msg, args...))
	l.mu.Unlock()
}
func (l *testLogger) Fatal(msg string, args ...any) {
	l.mu.Lock()
	l.entries = append(l.entries, fmt.Sprintf("[FATAL] "+msg, args...))
	l.mu.Unlock()
}

func newChaosEngine(t *testing.T) (*workflow.StdEngine, modular.Application) {
	t.Helper()
	logger := &testLogger{}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)
	engine := workflow.NewStdEngine(app, logger)
	loadAllPlugins(t, engine)
	engine.RegisterWorkflowHandler(handlers.NewHTTPWorkflowHandler())
	engine.RegisterWorkflowHandler(handlers.NewMessagingWorkflowHandler())
	engine.RegisterWorkflowHandler(handlers.NewStateMachineWorkflowHandler())
	engine.RegisterWorkflowHandler(handlers.NewIntegrationWorkflowHandler())
	return engine, app
}

// chaosWorkflowHandler is a handler that can inject failures.
type chaosWorkflowHandler struct {
	name         string
	failRate     float64 // 0.0 to 1.0 chance of failure
	delay        time.Duration
	execCount    int64
	failCount    int64
	successCount int64
}

func (h *chaosWorkflowHandler) CanHandle(workflowType string) bool {
	return workflowType == h.name
}

func (h *chaosWorkflowHandler) ConfigureWorkflow(app modular.Application, workflowConfig any) error {
	return nil
}

func (h *chaosWorkflowHandler) ExecuteWorkflow(ctx context.Context, workflowType string, action string, data map[string]any) (map[string]any, error) {
	atomic.AddInt64(&h.execCount, 1)

	if h.delay > 0 {
		select {
		case <-time.After(h.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if h.failRate > 0 && rand.Float64() < h.failRate {
		atomic.AddInt64(&h.failCount, 1)
		return nil, fmt.Errorf("chaos: injected failure for %s/%s", workflowType, action)
	}

	atomic.AddInt64(&h.successCount, 1)
	return map[string]any{"status": "ok"}, nil
}

// ---------- Context Cancellation Storm ----------

func TestChaos_ContextCancellationStorm(t *testing.T) {
	engine, _ := newChaosEngine(t)

	handler := &chaosWorkflowHandler{name: "cancel-storm", delay: 100 * time.Millisecond}
	engine.RegisterWorkflowHandler(handler)

	const numGoroutines = 200
	var wg sync.WaitGroup
	var cancelled atomic.Int64
	var completed atomic.Int64

	wg.Add(numGoroutines)
	for i := range numGoroutines {
		go func(id int) {
			defer wg.Done()
			// Each goroutine gets its own short-lived context
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(rand.Intn(50))*time.Millisecond)
			defer cancel()

			err := engine.TriggerWorkflow(ctx, "cancel-storm", fmt.Sprintf("action-%d", id), map[string]any{"id": id})
			if err != nil {
				cancelled.Add(1)
			} else {
				completed.Add(1)
			}
		}(i)
	}
	wg.Wait()

	t.Logf("Cancellation storm: %d completed, %d cancelled out of %d", completed.Load(), cancelled.Load(), numGoroutines)
	// The test passes as long as there are no panics or deadlocks.
	// Some cancellations are expected.
	if completed.Load()+cancelled.Load() != numGoroutines {
		t.Errorf("expected %d total outcomes, got %d", numGoroutines, completed.Load()+cancelled.Load())
	}
}

// ---------- Random Component Failure Mid-Execution ----------

func TestChaos_RandomFailureMidExecution(t *testing.T) {
	engine, _ := newChaosEngine(t)

	// 50% failure rate
	handler := &chaosWorkflowHandler{name: "flaky-wf", failRate: 0.5, delay: 5 * time.Millisecond}
	engine.RegisterWorkflowHandler(handler)

	const numExecutions = 500
	var wg sync.WaitGroup
	var errors atomic.Int64
	var successes atomic.Int64

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	wg.Add(numExecutions)
	for i := range numExecutions {
		go func(id int) {
			defer wg.Done()
			err := engine.TriggerWorkflow(ctx, "flaky-wf", "run", map[string]any{"id": id})
			if err != nil {
				errors.Add(1)
			} else {
				successes.Add(1)
			}
		}(i)
	}
	wg.Wait()

	t.Logf("Random failure test: %d successes, %d errors out of %d", successes.Load(), errors.Load(), numExecutions)

	// With 50% fail rate and 500 executions, we expect roughly 250 of each.
	// Allow wide tolerance.
	if errors.Load() == 0 {
		t.Error("expected some failures with 50%% fail rate, got 0")
	}
	if successes.Load() == 0 {
		t.Error("expected some successes with 50%% fail rate, got 0")
	}
}

// ---------- Concurrent Module Registration/Deregistration ----------

func TestChaos_ConcurrentModuleRegistration(t *testing.T) {
	logger := &testLogger{}
	const numIterations = 100

	var panicCount atomic.Int64

	// Serialize BuildFromConfig calls. The modular framework's EnvFeeder
	// has global state (SetFieldTracker) that isn't safe for concurrent
	// Init() calls. We serialize the build phase but still run
	// Start/Stop concurrently to stress lifecycle management.
	var buildMu sync.Mutex

	var wg sync.WaitGroup
	wg.Add(numIterations)
	for i := range numIterations {
		go func(id int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panicCount.Add(1)
				}
			}()

			// Each goroutine creates its own isolated app and engine
			app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)
			engine := workflow.NewStdEngine(app, logger)
			loadAllPlugins(t, engine)
			engine.RegisterWorkflowHandler(handlers.NewHTTPWorkflowHandler())
			engine.RegisterWorkflowHandler(handlers.NewMessagingWorkflowHandler())

			cfg := &config.WorkflowConfig{
				Modules: []config.ModuleConfig{
					{Name: fmt.Sprintf("broker-%d", id), Type: "messaging.broker", Config: map[string]any{}},
					{Name: fmt.Sprintf("handler-%d", id), Type: "messaging.handler", Config: map[string]any{}},
				},
				Workflows: map[string]any{
					"messaging": map[string]any{
						"subscriptions": []any{
							map[string]any{
								"topic":   fmt.Sprintf("topic-%d", id),
								"handler": fmt.Sprintf("handler-%d", id),
							},
						},
					},
				},
				Triggers: map[string]any{},
			}

			buildMu.Lock()
			buildErr := engine.BuildFromConfig(cfg)
			buildMu.Unlock()
			if buildErr != nil {
				return
			}

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = engine.Start(ctx)
			_ = engine.Stop(ctx)
		}(i)
	}
	wg.Wait()

	if panicCount.Load() > 0 {
		t.Errorf("concurrent module registration caused %d panics", panicCount.Load())
	}
	t.Logf("Concurrent module registration: %d iterations with %d panics", numIterations, panicCount.Load())
}

// ---------- State Machine Chaos: Concurrent Transitions ----------

func TestChaos_StateMachine_ConcurrentTransitions(t *testing.T) {
	engine, app := newChaosEngine(t)

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "sm-engine", Type: "statemachine.engine", Config: map[string]any{}},
		},
		Workflows: map[string]any{
			"statemachine": map[string]any{
				"engine": "sm-engine",
				"definitions": []any{
					map[string]any{
						"name":         "chaos-wf",
						"initialState": "open",
						"states": map[string]any{
							"open":   map[string]any{"isFinal": false},
							"active": map[string]any{"isFinal": false},
							"closed": map[string]any{"isFinal": true},
						},
						"transitions": map[string]any{
							"activate": map[string]any{"fromState": "open", "toState": "active"},
							"close":    map[string]any{"fromState": "active", "toState": "closed"},
						},
					},
				},
			},
		},
		Triggers: map[string]any{},
	}

	if err := engine.BuildFromConfig(cfg); err != nil {
		t.Fatalf("BuildFromConfig failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	var smSvc any
	if err := app.GetService("sm-engine", &smSvc); err != nil {
		t.Fatalf("sm-engine not found: %v", err)
	}
	smEngine := smSvc.(*module.StateMachineEngine)

	// Create many instances and drive them through transitions concurrently
	const numInstances = 100
	var wg sync.WaitGroup
	var transitionErrors atomic.Int64
	var completedInstances atomic.Int64

	wg.Add(numInstances)
	for i := range numInstances {
		go func(id int) {
			defer wg.Done()

			instanceID := fmt.Sprintf("chaos-wf-%d", id)
			instance, err := smEngine.CreateWorkflow("chaos-wf", instanceID, map[string]any{"id": id})
			if err != nil {
				transitionErrors.Add(1)
				return
			}

			// Transition open -> active
			if err := smEngine.TriggerTransition(ctx, instance.ID, "activate", nil); err != nil {
				transitionErrors.Add(1)
				return
			}

			// Transition active -> closed
			if err := smEngine.TriggerTransition(ctx, instance.ID, "close", nil); err != nil {
				transitionErrors.Add(1)
				return
			}

			// Verify final state
			inst, err := smEngine.GetInstance(instance.ID)
			if err != nil {
				transitionErrors.Add(1)
				return
			}
			if inst.CurrentState != "closed" {
				transitionErrors.Add(1)
				return
			}
			completedInstances.Add(1)
		}(i)
	}
	wg.Wait()

	t.Logf("State machine chaos: %d completed, %d errors out of %d instances",
		completedInstances.Load(), transitionErrors.Load(), numInstances)

	if transitionErrors.Load() > 0 {
		t.Errorf("had %d transition errors", transitionErrors.Load())
	}

	if err := engine.Stop(ctx); err != nil {
		t.Errorf("Stop failed: %v", err)
	}
}

// ---------- Rapid Build-Start-Stop Cycles ----------

func TestChaos_RapidLifecycleCycles(t *testing.T) {
	const numCycles = 50
	var wg sync.WaitGroup
	var failures atomic.Int64

	// Serialize BuildFromConfig calls. The modular framework's EnvFeeder
	// has global state (SetFieldTracker) that isn't safe for concurrent
	// Init() calls. We serialize the build phase but run Start/Stop
	// concurrently to stress lifecycle management.
	var buildMu sync.Mutex

	wg.Add(numCycles)
	for i := range numCycles {
		go func(id int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					failures.Add(1)
				}
			}()

			logger := &testLogger{}
			app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)
			engine := workflow.NewStdEngine(app, logger)
			loadAllPlugins(t, engine)
			engine.RegisterWorkflowHandler(handlers.NewMessagingWorkflowHandler())

			cfg := &config.WorkflowConfig{
				Modules: []config.ModuleConfig{
					{Name: "broker", Type: "messaging.broker", Config: map[string]any{}},
					{Name: "handler", Type: "messaging.handler", Config: map[string]any{}},
				},
				Workflows: map[string]any{
					"messaging": map[string]any{
						"subscriptions": []any{
							map[string]any{"topic": "t", "handler": "handler"},
						},
					},
				},
				Triggers: map[string]any{},
			}

			buildMu.Lock()
			buildErr := engine.BuildFromConfig(cfg)
			buildMu.Unlock()
			if buildErr != nil {
				failures.Add(1)
				return
			}

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			if err := engine.Start(ctx); err != nil {
				failures.Add(1)
				return
			}
			if err := engine.Stop(ctx); err != nil {
				failures.Add(1)
			}
		}(i)
	}
	wg.Wait()

	if failures.Load() > 0 {
		t.Errorf("rapid lifecycle cycles had %d failures out of %d", failures.Load(), numCycles)
	}
	t.Logf("Rapid lifecycle: %d cycles with %d failures", numCycles, failures.Load())
}

// ---------- Mixed Workflow Types Under Cancellation ----------

func TestChaos_MixedWorkflowsWithCancellation(t *testing.T) {
	engine, _ := newChaosEngine(t)

	types := []string{"wf-fast", "wf-slow", "wf-flaky"}
	engine.RegisterWorkflowHandler(&chaosWorkflowHandler{name: "wf-fast", delay: 0})
	engine.RegisterWorkflowHandler(&chaosWorkflowHandler{name: "wf-slow", delay: 200 * time.Millisecond})
	engine.RegisterWorkflowHandler(&chaosWorkflowHandler{name: "wf-flaky", failRate: 0.3, delay: 10 * time.Millisecond})

	const numPerType = 100
	var wg sync.WaitGroup
	total := numPerType * len(types)
	var completed atomic.Int64
	var errors atomic.Int64

	// Use a short context that will expire for slow workflows
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	wg.Add(total)
	for _, wfType := range types {
		for i := range numPerType {
			go func(typ string, id int) {
				defer wg.Done()
				err := engine.TriggerWorkflow(ctx, typ, "run", map[string]any{"id": id})
				if err != nil {
					errors.Add(1)
				} else {
					completed.Add(1)
				}
			}(wfType, i)
		}
	}
	wg.Wait()

	t.Logf("Mixed workflows: %d completed, %d errors out of %d total",
		completed.Load(), errors.Load(), total)
	// Test passes if no panics or deadlocks
}
