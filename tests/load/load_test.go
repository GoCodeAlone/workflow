package load

import (
	"context"
	"fmt"
	"runtime"
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

// loadWorkflowHandler is a lightweight handler for load testing.
type loadWorkflowHandler struct {
	handlesTypes []string
	delay        time.Duration
}

func (h *loadWorkflowHandler) CanHandle(workflowType string) bool {
	for _, t := range h.handlesTypes {
		if t == workflowType {
			return true
		}
	}
	return false
}

func (h *loadWorkflowHandler) ConfigureWorkflow(app modular.Application, workflowConfig any) error {
	return nil
}

func (h *loadWorkflowHandler) ExecuteWorkflow(ctx context.Context, workflowType string, action string, data map[string]any) (map[string]any, error) {
	if h.delay > 0 {
		select {
		case <-time.After(h.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return map[string]any{"status": "completed", "type": workflowType}, nil
}

func newLoadEngine(t *testing.T) *workflow.StdEngine {
	t.Helper()
	logger := &testLogger{}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)
	engine := workflow.NewStdEngine(app, logger)
	return engine
}

// ---------- Sustained Throughput Test ----------

func TestLoad_SustainedThroughput(t *testing.T) {
	engine := newLoadEngine(t)
	handler := &loadWorkflowHandler{handlesTypes: []string{"throughput-wf"}}
	engine.RegisterWorkflowHandler(handler)

	const (
		numWorkers   = 20
		testDuration = 3 * time.Second
	)

	ctx, cancel := context.WithTimeout(context.Background(), testDuration+5*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	var totalExecs atomic.Int64
	var totalErrors atomic.Int64

	deadline := time.Now().Add(testDuration)

	wg.Add(numWorkers)
	for w := range numWorkers {
		go func(workerID int) {
			defer wg.Done()
			for time.Now().Before(deadline) {
				data := map[string]any{"worker": workerID, "ts": time.Now().UnixNano()}
				err := engine.TriggerWorkflow(ctx, "throughput-wf", "execute", data)
				if err != nil {
					totalErrors.Add(1)
				}
				totalExecs.Add(1)
			}
		}(w)
	}
	wg.Wait()

	execs := totalExecs.Load()
	errs := totalErrors.Load()
	throughput := float64(execs) / testDuration.Seconds()

	t.Logf("Sustained throughput: %.0f wf/sec (%d total, %d errors, %d workers, %v)",
		throughput, execs, errs, numWorkers, testDuration)

	if errs > 0 {
		t.Errorf("had %d errors during sustained throughput test", errs)
	}
	if execs < 1000 {
		t.Errorf("expected at least 1000 executions in %v, got %d", testDuration, execs)
	}
}

// ---------- Concurrent API Simulation ----------

func TestLoad_ConcurrentAPISimulation(t *testing.T) {
	engine := newLoadEngine(t)
	loadAllPlugins(t, engine)

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "httpServer", Type: "http.server", Config: map[string]any{"address": ":0"}},
			{Name: "httpRouter", Type: "http.router", Config: map[string]any{}},
			{Name: "handler1", Type: "http.handler", Config: map[string]any{}},
			{Name: "broker", Type: "messaging.broker", Config: map[string]any{}},
			{Name: "msgHandler", Type: "messaging.handler", Config: map[string]any{}},
		},
		Workflows: map[string]any{
			"http": map[string]any{
				"routes": []any{
					map[string]any{"method": "GET", "path": "/api/test", "handler": "handler1"},
				},
			},
			"messaging": map[string]any{
				"subscriptions": []any{
					map[string]any{"topic": "events", "handler": "msgHandler"},
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

	// Simulate concurrent message publishing
	app := engine.GetApp()
	var brokerSvc any
	if err := app.GetService("broker", &brokerSvc); err != nil {
		t.Fatalf("broker not found: %v", err)
	}
	broker := brokerSvc.(module.MessageBroker)

	const numMessages = 1000
	var wg sync.WaitGroup
	var sendErrors atomic.Int64

	wg.Add(numMessages)
	for i := range numMessages {
		go func(id int) {
			defer wg.Done()
			msg := []byte(fmt.Sprintf(`{"id":%d,"action":"test"}`, id))
			if err := broker.Producer().SendMessage("events", msg); err != nil {
				sendErrors.Add(1)
			}
		}(i)
	}
	wg.Wait()

	t.Logf("Concurrent API simulation: sent %d messages, %d errors", numMessages, sendErrors.Load())

	if sendErrors.Load() > 0 {
		t.Errorf("had %d message send errors", sendErrors.Load())
	}

	if err := engine.Stop(ctx); err != nil {
		t.Errorf("Stop failed: %v", err)
	}
}

// ---------- Memory Stability Over Iterations ----------

func TestLoad_MemoryStability(t *testing.T) {
	engine := newLoadEngine(t)
	handler := &loadWorkflowHandler{handlesTypes: []string{"mem-wf"}}
	engine.RegisterWorkflowHandler(handler)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Warm up
	for range 100 {
		_ = engine.TriggerWorkflow(ctx, "mem-wf", "warmup", map[string]any{})
	}
	runtime.GC()
	time.Sleep(100 * time.Millisecond)

	var baseStats runtime.MemStats
	runtime.ReadMemStats(&baseStats)
	baseAlloc := baseStats.Alloc

	// Run many iterations
	const totalIterations = 10000
	for i := range totalIterations {
		data := map[string]any{
			"iteration": i,
			"payload":   fmt.Sprintf("data-iteration-%d", i),
		}
		if err := engine.TriggerWorkflow(ctx, "mem-wf", "run", data); err != nil {
			t.Fatalf("TriggerWorkflow failed at iteration %d: %v", i, err)
		}
	}

	runtime.GC()
	time.Sleep(200 * time.Millisecond)

	var afterStats runtime.MemStats
	runtime.ReadMemStats(&afterStats)
	afterAlloc := afterStats.Alloc

	// Check that memory growth is bounded
	// Allow up to 50MB of growth (generous for 10k iterations)
	maxGrowthBytes := uint64(50 * 1024 * 1024)
	growth := uint64(0)
	if afterAlloc > baseAlloc {
		growth = afterAlloc - baseAlloc
	}

	t.Logf("Memory stability: base=%dKB, after=%dKB, growth=%dKB over %d iterations",
		baseAlloc/1024, afterAlloc/1024, growth/1024, totalIterations)

	if growth > maxGrowthBytes {
		t.Errorf("memory grew by %dMB (max allowed %dMB) over %d iterations",
			growth/(1024*1024), maxGrowthBytes/(1024*1024), totalIterations)
	}
}

// ---------- State Machine Throughput Under Load ----------

func TestLoad_StateMachineThroughput(t *testing.T) {
	logger := &testLogger{}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)
	engine := workflow.NewStdEngine(app, logger)
	loadAllPlugins(t, engine)
	engine.RegisterWorkflowHandler(handlers.NewStateMachineWorkflowHandler())

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "sm-engine", Type: "statemachine.engine", Config: map[string]any{}},
		},
		Workflows: map[string]any{
			"statemachine": map[string]any{
				"engine": "sm-engine",
				"definitions": []any{
					map[string]any{
						"name":         "load-wf",
						"initialState": "start",
						"states": map[string]any{
							"start":  map[string]any{"isFinal": false},
							"middle": map[string]any{"isFinal": false},
							"end":    map[string]any{"isFinal": true},
						},
						"transitions": map[string]any{
							"advance": map[string]any{"fromState": "start", "toState": "middle"},
							"finish":  map[string]any{"fromState": "middle", "toState": "end"},
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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	var smSvc any
	if err := app.GetService("sm-engine", &smSvc); err != nil {
		t.Fatalf("sm-engine not found: %v", err)
	}
	smEngine := smSvc.(*module.StateMachineEngine)

	const numInstances = 500
	var wg sync.WaitGroup
	var errors atomic.Int64
	var completed atomic.Int64

	start := time.Now()
	wg.Add(numInstances)
	for i := range numInstances {
		go func(id int) {
			defer wg.Done()

			instanceID := fmt.Sprintf("load-wf-%d", id)
			instance, err := smEngine.CreateWorkflow("load-wf", instanceID, map[string]any{"id": id})
			if err != nil {
				errors.Add(1)
				return
			}

			if err := smEngine.TriggerTransition(ctx, instance.ID, "advance", nil); err != nil {
				errors.Add(1)
				return
			}
			if err := smEngine.TriggerTransition(ctx, instance.ID, "finish", nil); err != nil {
				errors.Add(1)
				return
			}

			inst, err := smEngine.GetInstance(instance.ID)
			if err != nil || inst.CurrentState != "end" {
				errors.Add(1)
				return
			}
			completed.Add(1)
		}(i)
	}
	wg.Wait()
	elapsed := time.Since(start)

	throughput := float64(completed.Load()) / elapsed.Seconds()
	t.Logf("State machine throughput: %.0f instances/sec (%d completed, %d errors in %v)",
		throughput, completed.Load(), errors.Load(), elapsed)

	if errors.Load() > 0 {
		t.Errorf("had %d errors during state machine load test", errors.Load())
	}

	if err := engine.Stop(ctx); err != nil {
		t.Errorf("Stop failed: %v", err)
	}
}

// ---------- Multi-Type Workflow Load ----------

func TestLoad_MultiTypeWorkflow(t *testing.T) {
	engine := newLoadEngine(t)

	types := []string{"type-a", "type-b", "type-c", "type-d"}
	for _, typ := range types {
		engine.RegisterWorkflowHandler(&loadWorkflowHandler{handlesTypes: []string{typ}})
	}

	const (
		numWorkers   = 10
		testDuration = 2 * time.Second
	)

	ctx, cancel := context.WithTimeout(context.Background(), testDuration+5*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	var totalExecs atomic.Int64
	var totalErrors atomic.Int64
	typeCounts := make([]atomic.Int64, len(types))

	deadline := time.Now().Add(testDuration)

	wg.Add(numWorkers)
	for w := range numWorkers {
		go func(workerID int) {
			defer wg.Done()
			i := 0
			for time.Now().Before(deadline) {
				typeIdx := i % len(types)
				err := engine.TriggerWorkflow(ctx, types[typeIdx], "run", map[string]any{
					"worker": workerID,
					"seq":    i,
				})
				if err != nil {
					totalErrors.Add(1)
				} else {
					typeCounts[typeIdx].Add(1)
				}
				totalExecs.Add(1)
				i++
			}
		}(w)
	}
	wg.Wait()

	execs := totalExecs.Load()
	errs := totalErrors.Load()
	throughput := float64(execs) / testDuration.Seconds()

	t.Logf("Multi-type load: %.0f wf/sec (%d total, %d errors)", throughput, execs, errs)
	for i, typ := range types {
		t.Logf("  %s: %d executions", typ, typeCounts[i].Load())
	}

	if errs > 0 {
		t.Errorf("had %d errors during multi-type load test", errs)
	}
}

// ---------- Goroutine Leak Detection Under Load ----------

func TestLoad_GoroutineLeakDetection(t *testing.T) {
	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	baseGoroutines := runtime.NumGoroutine()

	engine := newLoadEngine(t)
	handler := &loadWorkflowHandler{handlesTypes: []string{"leak-wf"}}
	engine.RegisterWorkflowHandler(handler)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	const numIterations = 5000
	var wg sync.WaitGroup
	wg.Add(numIterations)
	for i := range numIterations {
		go func(id int) {
			defer wg.Done()
			_ = engine.TriggerWorkflow(ctx, "leak-wf", "run", map[string]any{"id": id})
		}(i)
	}
	wg.Wait()

	// Give goroutines time to clean up
	runtime.GC()
	time.Sleep(300 * time.Millisecond)

	afterGoroutines := runtime.NumGoroutine()
	leaked := afterGoroutines - baseGoroutines
	maxAllowed := 10 // generous tolerance for runtime/test framework goroutines

	t.Logf("Goroutine leak check: base=%d, after=%d, delta=%d", baseGoroutines, afterGoroutines, leaked)

	if leaked > maxAllowed {
		t.Errorf("goroutine leak: base=%d, after=%d (leaked %d, max %d)",
			baseGoroutines, afterGoroutines, leaked, maxAllowed)
	}
}
