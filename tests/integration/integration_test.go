package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/handlers"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
	pluginai "github.com/GoCodeAlone/workflow/plugins/ai"
	pluginapi "github.com/GoCodeAlone/workflow/plugins/api"
	pluginauth "github.com/GoCodeAlone/workflow/plugins/auth"
	plugincicd "github.com/GoCodeAlone/workflow/plugins/cicd"
	pluginff "github.com/GoCodeAlone/workflow/plugins/featureflags"
	pluginhttp "github.com/GoCodeAlone/workflow/plugins/http"
	pluginintegration "github.com/GoCodeAlone/workflow/plugins/integration"
	pluginmessaging "github.com/GoCodeAlone/workflow/plugins/messaging"
	pluginmodcompat "github.com/GoCodeAlone/workflow/plugins/modularcompat"
	pluginobs "github.com/GoCodeAlone/workflow/plugins/observability"
	pluginpipeline "github.com/GoCodeAlone/workflow/plugins/pipelinesteps"
	pluginscheduler "github.com/GoCodeAlone/workflow/plugins/scheduler"
	pluginsecrets "github.com/GoCodeAlone/workflow/plugins/secrets"
	pluginsm "github.com/GoCodeAlone/workflow/plugins/statemachine"
	pluginstorage "github.com/GoCodeAlone/workflow/plugins/storage"
	"github.com/GoCodeAlone/workflow/secrets"
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
func integrationPlugins() []plugin.EnginePlugin {
	return []plugin.EnginePlugin{
		pluginhttp.New(),
		pluginobs.New(),
		pluginmessaging.New(),
		pluginsm.New(),
		pluginauth.New(),
		pluginstorage.New(),
		pluginapi.New(),
		pluginpipeline.New(),
		plugincicd.New(),
		pluginff.New(),
		pluginsecrets.New(),
		pluginmodcompat.New(),
		pluginscheduler.New(),
		pluginintegration.New(),
		pluginai.New(),
	}
}

func newTestEngine(t *testing.T) (*workflow.StdEngine, modular.Application, *testLogger) {
	t.Helper()
	app, logger := newTestApp(t)
	engine := workflow.NewStdEngine(app, logger)
	for _, p := range integrationPlugins() {
		if err := engine.LoadPlugin(p); err != nil {
			t.Fatalf("LoadPlugin(%s) failed: %v", p.Name(), err)
		}
	}
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

// ---------------------------------------------------------------------------
// HTTP Client Integration — end-to-end: YAML config → engine → service registry
// ---------------------------------------------------------------------------

// integrationMemSecretsProvider is a simple map-backed secrets.Provider for
// integration tests.  It avoids a dependency on the module-internal helper.
type integrationMemSecretsProvider struct {
	mu   sync.RWMutex
	data map[string]string
}

func newIntegrationMemSecretsProvider(initial map[string]string) *integrationMemSecretsProvider {
	p := &integrationMemSecretsProvider{data: make(map[string]string)}
	for k, v := range initial {
		p.data[k] = v
	}
	return p
}

func (p *integrationMemSecretsProvider) Name() string { return "integration-mem-secrets" }

func (p *integrationMemSecretsProvider) Get(_ context.Context, key string) (string, error) {
	if key == "" {
		return "", secrets.ErrInvalidKey
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	v, ok := p.data[key]
	if !ok {
		return "", secrets.ErrNotFound
	}
	return v, nil
}

func (p *integrationMemSecretsProvider) Set(_ context.Context, key, value string) error {
	if key == "" {
		return secrets.ErrInvalidKey
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.data[key] = value
	return nil
}

func (p *integrationMemSecretsProvider) Delete(_ context.Context, key string) error {
	if key == "" {
		return secrets.ErrInvalidKey
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.data, key)
	return nil
}

func (p *integrationMemSecretsProvider) List(_ context.Context) ([]string, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	keys := make([]string, 0, len(p.data))
	for k := range p.data {
		keys = append(keys, k)
	}
	return keys, nil
}

// TestHTTPClient_Integration_EndToEnd proves the full path:
//
//	YAML config string
//	  → config.LoadFromString
//	  → engine.BuildFromConfig  (http.client factory called, module registered)
//	  → engine.Start            (Init + Start on each module, credentials resolved)
//	  → app.GetService          (service registry lookup)
//	  → module.Client().Do      (real HTTP round-trip with Authorization header)
//
// PR 3 owns the module itself; PR 4 will add the step.http_call → client: ref wiring.
// This test therefore exercises YAML → engine → service registry → working http client,
// which is the end-to-end guarantee this PR is responsible for.
func TestHTTPClient_Integration_EndToEnd(t *testing.T) {
	const wantToken = "integration-bearer-token" //nolint:gosec // G101: test credential, not a real secret

	// Upstream records received requests so we can assert the auth header.
	var requestCount int32
	var gotAuthHeader string
	var headerMu sync.Mutex

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		headerMu.Lock()
		gotAuthHeader = r.Header.Get("Authorization")
		headerMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer upstream.Close()

	// Seed a secrets provider with the bearer token; it will be pre-registered in
	// the app so the http.client module can resolve it during Start().
	secretsProv := newIntegrationMemSecretsProvider(map[string]string{
		"api-bearer": wantToken,
	})

	// Build the workflow config from an inline YAML string — this is the
	// "minimal inline YAML" required by Task 1.13 Step 2.
	yamlContent := `
modules:
  - name: upstream-client
    type: http.client
    config:
      base_url: "` + upstream.URL + `"
      timeout: 5s
      auth:
        type: static_bearer
        bearer_token_ref:
          provider: integration-secrets
          key: api-bearer
workflows: {}
triggers: {}
`
	cfg, err := config.LoadFromString(yamlContent)
	if err != nil {
		t.Fatalf("LoadFromString failed: %v", err)
	}

	// Build engine with http plugin loaded (provides http.client factory).
	app, logger := newTestApp(t)
	engine := workflow.NewStdEngine(app, logger)
	for _, p := range integrationPlugins() {
		if err := engine.LoadPlugin(p); err != nil {
			t.Fatalf("LoadPlugin(%s) failed: %v", p.Name(), err)
		}
	}

	// Register the secrets provider in the app *before* BuildFromConfig so that
	// Init/Start can resolve the bearer_token_ref during engine start.
	if err := app.RegisterService("integration-secrets", secretsProv); err != nil {
		t.Fatalf("RegisterService(integration-secrets): %v", err)
	}

	if err := engine.BuildFromConfig(cfg); err != nil {
		t.Fatalf("BuildFromConfig failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := engine.Start(ctx); err != nil {
		t.Fatalf("engine.Start failed: %v", err)
	}
	defer func() {
		if err := engine.Stop(ctx); err != nil {
			t.Errorf("engine.Stop failed: %v", err)
		}
	}()

	// Look up the http.client service from the DI registry by the module name.
	var httpClientSvc module.HTTPClient
	if err := app.GetService("upstream-client", &httpClientSvc); err != nil {
		t.Fatalf("GetService(upstream-client): %v — http.client module not in service registry", err)
	}
	if httpClientSvc == nil {
		t.Fatal("http.client service is nil")
	}

	// Verify BaseURL was set from the YAML config.
	if httpClientSvc.BaseURL() != upstream.URL {
		t.Errorf("BaseURL: got %q, want %q", httpClientSvc.BaseURL(), upstream.URL)
	}

	// Make a real HTTP request through the configured client.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, upstream.URL+"/api/check", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := httpClientSvc.Client().Do(req)
	if err != nil {
		t.Fatalf("http request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// The upstream must have received exactly one request.
	if n := atomic.LoadInt32(&requestCount); n != 1 {
		t.Errorf("expected 1 upstream request, got %d", n)
	}

	// The Authorization header must carry the token that was in the secrets provider.
	headerMu.Lock()
	got := gotAuthHeader
	headerMu.Unlock()

	want := "Bearer " + wantToken
	if got != want {
		t.Errorf("Authorization header: got %q, want %q", got, want)
	}

	t.Logf("End-to-end: YAML config → engine → service registry → http.client → upstream OK (auth header set: %v)", got != "")
}
