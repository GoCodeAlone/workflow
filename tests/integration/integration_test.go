package integration

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/handlers"
	"github.com/GoCodeAlone/workflow/module"
)

// testLogger is a simple logger for integration tests.
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

// newTestApp creates an isolated modular.Application for testing.
func newTestApp(t *testing.T) (modular.Application, *testLogger) {
	t.Helper()
	logger := &testLogger{}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)
	return app, logger
}

// newTestEngine creates an engine with all standard workflow handlers registered.
func newTestEngine(t *testing.T) (*workflow.StdEngine, modular.Application, *testLogger) {
	t.Helper()
	app, logger := newTestApp(t)
	engine := workflow.NewStdEngine(app, logger)
	engine.RegisterWorkflowHandler(handlers.NewHTTPWorkflowHandler())
	engine.RegisterWorkflowHandler(handlers.NewMessagingWorkflowHandler())
	engine.RegisterWorkflowHandler(handlers.NewStateMachineWorkflowHandler())
	engine.RegisterWorkflowHandler(handlers.NewSchedulerWorkflowHandler())
	engine.RegisterWorkflowHandler(handlers.NewIntegrationWorkflowHandler())
	return engine, app, logger
}

// ---------- HTTP Workflow Integration ----------

func TestHTTPWorkflow_EndToEnd(t *testing.T) {
	engine, _, _ := newTestEngine(t)

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "httpServer", Type: "http.server", Config: map[string]any{"address": ":0"}},
			{Name: "httpRouter", Type: "http.router", Config: map[string]any{}},
			{Name: "apiHandler", Type: "http.handler", Config: map[string]any{"contentType": "application/json"}},
			{Name: "healthHandler", Type: "http.handler", Config: map[string]any{"contentType": "application/json"}},
		},
		Workflows: map[string]any{
			"http": map[string]any{
				"routes": []any{
					map[string]any{"method": "GET", "path": "/api/data", "handler": "apiHandler"},
					map[string]any{"method": "GET", "path": "/health", "handler": "healthHandler"},
				},
			},
		},
		Triggers: map[string]any{},
	}

	err := engine.BuildFromConfig(cfg)
	if err != nil {
		t.Fatalf("BuildFromConfig failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer func() {
		if err := engine.Stop(ctx); err != nil {
			t.Errorf("Stop failed: %v", err)
		}
	}()

	t.Log("HTTP workflow end-to-end: config -> build -> start -> stop succeeded")
}

func TestHTTPWorkflow_WithMiddleware(t *testing.T) {
	engine, _, _ := newTestEngine(t)

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "httpServer", Type: "http.server", Config: map[string]any{"address": ":0"}},
			{Name: "httpRouter", Type: "http.router", Config: map[string]any{}},
			{Name: "loggingMW", Type: "http.middleware.logging", Config: map[string]any{"logLevel": "debug"}},
			{Name: "handler1", Type: "http.handler", Config: map[string]any{"contentType": "application/json"}},
		},
		Workflows: map[string]any{
			"http": map[string]any{
				"routes": []any{
					map[string]any{
						"method":      "GET",
						"path":        "/api/items",
						"handler":     "handler1",
						"middlewares": []any{"loggingMW"},
					},
				},
			},
		},
		Triggers: map[string]any{},
	}

	err := engine.BuildFromConfig(cfg)
	if err != nil {
		t.Fatalf("BuildFromConfig with middleware failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if err := engine.Stop(ctx); err != nil {
		t.Errorf("Stop failed: %v", err)
	}
}

// ---------- Messaging Workflow Integration ----------

func TestMessagingWorkflow_EndToEnd(t *testing.T) {
	engine, _, _ := newTestEngine(t)

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "broker", Type: "messaging.broker", Config: map[string]any{}},
			{Name: "msgHandler", Type: "messaging.handler", Config: map[string]any{}},
		},
		Workflows: map[string]any{
			"messaging": map[string]any{
				"subscriptions": []any{
					map[string]any{"topic": "test-topic", "handler": "msgHandler"},
				},
			},
		},
		Triggers: map[string]any{},
	}

	err := engine.BuildFromConfig(cfg)
	if err != nil {
		t.Fatalf("BuildFromConfig failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Verify broker is accessible via the app service registry
	app := engine.GetApp()
	var brokerSvc any
	if err := app.GetService("broker", &brokerSvc); err != nil {
		t.Fatalf("broker service not found: %v", err)
	}
	broker, ok := brokerSvc.(module.MessageBroker)
	if !ok {
		t.Fatal("broker service does not implement MessageBroker")
	}

	// Publish a message to the topic
	err = broker.Producer().SendMessage("test-topic", []byte(`{"event":"test"}`))
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}

	if err := engine.Stop(ctx); err != nil {
		t.Errorf("Stop failed: %v", err)
	}
	t.Log("Messaging workflow end-to-end succeeded")
}

func TestMessagingWorkflow_MultipleSubscriptions(t *testing.T) {
	engine, _, _ := newTestEngine(t)

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "broker", Type: "messaging.broker", Config: map[string]any{}},
			{Name: "handlerA", Type: "messaging.handler", Config: map[string]any{}},
			{Name: "handlerB", Type: "messaging.handler", Config: map[string]any{}},
		},
		Workflows: map[string]any{
			"messaging": map[string]any{
				"subscriptions": []any{
					map[string]any{"topic": "topic-a", "handler": "handlerA"},
					map[string]any{"topic": "topic-b", "handler": "handlerB"},
				},
			},
		},
		Triggers: map[string]any{},
	}

	if err := engine.BuildFromConfig(cfg); err != nil {
		t.Fatalf("BuildFromConfig failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if err := engine.Stop(ctx); err != nil {
		t.Errorf("Stop failed: %v", err)
	}
}

// ---------- State Machine Workflow Integration ----------

func TestStateMachineWorkflow_EndToEnd(t *testing.T) {
	engine, _, _ := newTestEngine(t)

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "sm-engine", Type: "statemachine.engine", Config: map[string]any{}},
			{Name: "workflow.service.statetracker", Type: "state.tracker", Config: map[string]any{}},
			{Name: "workflow.connector.statemachine", Type: "state.connector", Config: map[string]any{}},
		},
		Workflows: map[string]any{
			"statemachine": map[string]any{
				"engine": "sm-engine",
				"definitions": []any{
					map[string]any{
						"name":         "test-workflow",
						"description":  "A simple test state machine",
						"initialState": "new",
						"states": map[string]any{
							"new":       map[string]any{"description": "Initial state", "isFinal": false},
							"active":    map[string]any{"description": "Active state", "isFinal": false},
							"completed": map[string]any{"description": "Completed state", "isFinal": true},
						},
						"transitions": map[string]any{
							"activate": map[string]any{"fromState": "new", "toState": "active"},
							"complete": map[string]any{"fromState": "active", "toState": "completed"},
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Verify state machine engine is accessible
	app := engine.GetApp()
	var smSvc any
	if err := app.GetService("sm-engine", &smSvc); err != nil {
		t.Fatalf("state machine engine not found: %v", err)
	}
	smEngine, ok := smSvc.(*module.StateMachineEngine)
	if !ok {
		t.Fatal("sm-engine is not a StateMachineEngine")
	}

	// Create an instance and drive it through transitions.
	// First arg is the workflow type (stored for identification), second is the definition name.
	instance, err := smEngine.CreateWorkflow("test-workflow", "test-workflow", map[string]any{"key": "value"})
	if err != nil {
		t.Fatalf("CreateWorkflow failed: %v", err)
	}
	if instance.CurrentState != "new" {
		t.Errorf("expected initial state 'new', got %q", instance.CurrentState)
	}

	// Transition: new -> active
	if err := smEngine.TriggerTransition(ctx, instance.ID, "activate", nil); err != nil {
		t.Fatalf("TriggerTransition 'activate' failed: %v", err)
	}
	instance, _ = smEngine.GetInstance(instance.ID)
	if instance.CurrentState != "active" {
		t.Errorf("expected state 'active', got %q", instance.CurrentState)
	}

	// Transition: active -> completed
	if err := smEngine.TriggerTransition(ctx, instance.ID, "complete", nil); err != nil {
		t.Fatalf("TriggerTransition 'complete' failed: %v", err)
	}
	instance, _ = smEngine.GetInstance(instance.ID)
	if instance.CurrentState != "completed" {
		t.Errorf("expected state 'completed', got %q", instance.CurrentState)
	}

	if err := engine.Stop(ctx); err != nil {
		t.Errorf("Stop failed: %v", err)
	}
	t.Log("State machine workflow end-to-end succeeded")
}

// ---------- Combined Multi-Workflow Integration ----------

func TestMultiWorkflow_HTTPAndMessaging(t *testing.T) {
	engine, _, _ := newTestEngine(t)

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "httpServer", Type: "http.server", Config: map[string]any{"address": ":0"}},
			{Name: "httpRouter", Type: "http.router", Config: map[string]any{}},
			{Name: "apiHandler", Type: "http.handler", Config: map[string]any{"contentType": "application/json"}},
			{Name: "broker", Type: "messaging.broker", Config: map[string]any{}},
			{Name: "msgHandler", Type: "messaging.handler", Config: map[string]any{}},
		},
		Workflows: map[string]any{
			"http": map[string]any{
				"routes": []any{
					map[string]any{"method": "GET", "path": "/api/data", "handler": "apiHandler"},
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if err := engine.Stop(ctx); err != nil {
		t.Errorf("Stop failed: %v", err)
	}
	t.Log("Multi-workflow (HTTP + messaging) integration succeeded")
}

func TestMultiWorkflow_AllHandlerTypes(t *testing.T) {
	engine, _, _ := newTestEngine(t)

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			// HTTP
			{Name: "httpServer", Type: "http.server", Config: map[string]any{"address": ":0"}},
			{Name: "httpRouter", Type: "http.router", Config: map[string]any{}},
			{Name: "handler1", Type: "http.handler", Config: map[string]any{}},
			// Messaging
			{Name: "broker", Type: "messaging.broker", Config: map[string]any{}},
			{Name: "msgHandler", Type: "messaging.handler", Config: map[string]any{}},
			// State machine
			{Name: "sm-engine", Type: "statemachine.engine", Config: map[string]any{}},
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
			"statemachine": map[string]any{
				"engine": "sm-engine",
				"definitions": []any{
					map[string]any{
						"name":         "simple-wf",
						"initialState": "start",
						"states": map[string]any{
							"start": map[string]any{"isFinal": false},
							"end":   map[string]any{"isFinal": true},
						},
						"transitions": map[string]any{
							"finish": map[string]any{"fromState": "start", "toState": "end"},
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if err := engine.Stop(ctx); err != nil {
		t.Errorf("Stop failed: %v", err)
	}
	t.Log("All handler types combined integration succeeded")
}

// ---------- Engine Lifecycle Integration ----------

func TestEngine_StartStop_Idempotent(t *testing.T) {
	engine, _, _ := newTestEngine(t)

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "broker", Type: "messaging.broker", Config: map[string]any{}},
		},
		Workflows: map[string]any{},
		Triggers:  map[string]any{},
	}

	if err := engine.BuildFromConfig(cfg); err != nil {
		t.Fatalf("BuildFromConfig failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if err := engine.Stop(ctx); err != nil {
		t.Errorf("Stop failed: %v", err)
	}
}

func TestEngine_BuildFromConfig_ValidationError(t *testing.T) {
	engine, _, _ := newTestEngine(t)

	// Missing required module name
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "", Type: "http.server", Config: map[string]any{"address": ":8080"}},
		},
		Workflows: map[string]any{},
		Triggers:  map[string]any{},
	}

	err := engine.BuildFromConfig(cfg)
	if err == nil {
		t.Fatal("expected validation error for empty module name")
	}
}

// ---------- Config Loading Integration ----------

func TestConfigLoadFromString_Build(t *testing.T) {
	yamlContent := `
modules:
  - name: broker
    type: messaging.broker
    config: {}
  - name: handler1
    type: messaging.handler
    config: {}
workflows:
  messaging:
    subscriptions:
      - topic: test-events
        handler: handler1
triggers: {}
`
	cfg, err := config.LoadFromString(yamlContent)
	if err != nil {
		t.Fatalf("LoadFromString failed: %v", err)
	}

	engine, _, _ := newTestEngine(t)
	if err := engine.BuildFromConfig(cfg); err != nil {
		t.Fatalf("BuildFromConfig from YAML string failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if err := engine.Stop(ctx); err != nil {
		t.Errorf("Stop failed: %v", err)
	}
}

// ---------- Metrics and Health Integration ----------

func TestMetricsAndHealth_Integration(t *testing.T) {
	engine, _, _ := newTestEngine(t)

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "httpServer", Type: "http.server", Config: map[string]any{"address": ":0"}},
			{Name: "httpRouter", Type: "http.router", Config: map[string]any{}},
			{Name: "handler1", Type: "http.handler", Config: map[string]any{}},
			{Name: "metrics", Type: "metrics.collector", Config: map[string]any{}},
			{Name: "health", Type: "health.checker", Config: map[string]any{}},
		},
		Workflows: map[string]any{
			"http": map[string]any{
				"routes": []any{
					map[string]any{"method": "GET", "path": "/api/test", "handler": "handler1"},
				},
			},
		},
		Triggers: map[string]any{},
	}

	if err := engine.BuildFromConfig(cfg); err != nil {
		t.Fatalf("BuildFromConfig failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if err := engine.Stop(ctx); err != nil {
		t.Errorf("Stop failed: %v", err)
	}
	t.Log("Metrics and health integration succeeded")
}

// ---------- Custom Module Factory Integration ----------

func TestCustomModuleFactory_Integration(t *testing.T) {
	engine, _, _ := newTestEngine(t)

	factoryCalled := false
	engine.AddModuleType("custom.test", func(name string, cfg map[string]any) modular.Module {
		factoryCalled = true
		return module.NewSimpleMessageHandler(name)
	})

	yamlCfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "custom1", Type: "custom.test", Config: map[string]any{}},
			{Name: "broker", Type: "messaging.broker", Config: map[string]any{}},
		},
		Workflows: map[string]any{
			"messaging": map[string]any{
				"subscriptions": []any{
					map[string]any{"topic": "test", "handler": "custom1"},
				},
			},
		},
		Triggers: map[string]any{},
	}

	if err := engine.BuildFromConfig(yamlCfg); err != nil {
		t.Fatalf("BuildFromConfig failed: %v", err)
	}
	if !factoryCalled {
		t.Error("custom module factory was not invoked")
	}
}
