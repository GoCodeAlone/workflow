package workflow

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/dynamic"
	"github.com/GoCodeAlone/workflow/handlers"
	"github.com/GoCodeAlone/workflow/mock"
	"github.com/GoCodeAlone/workflow/module"
)

// setupEngineTest creates an isolated test environment for engine tests
func setupEngineTest(t *testing.T) (*StdEngine, modular.Application, context.Context, context.CancelFunc) {
	t.Helper()

	// Create isolated mock logger
	mockLogger := &mock.Logger{LogEntries: make([]string, 0)}

	// Create isolated application with nil config
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), mockLogger)

	// Initialize the application
	err := app.Init()
	if err != nil {
		t.Fatalf("Failed to initialize app: %v", err)
	}

	// Create engine with the isolated app
	engine := NewStdEngine(app, mockLogger)

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)

	return engine, app, ctx, cancel
}

func TestEngineWithHTTPWorkflow(t *testing.T) {
	// Use the helper function for setup
	engine, _, ctx, cancel := setupEngineTest(t)
	defer cancel()

	// Register workflow handlers
	engine.RegisterWorkflowHandler(handlers.NewHTTPWorkflowHandler())

	t.Log("TestEngineWithHTTPWorkflow completed successfully")

	// Start engine
	err := engine.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start workflow: %v", err)
	}

	// Stop engine
	err = engine.Stop(ctx)
	if err != nil {
		t.Fatalf("Failed to stop workflow: %v", err)
	}
}

func TestEngineWithMessagingWorkflow(t *testing.T) {
	// Use the helper function for setup
	engine, _, ctx, cancel := setupEngineTest(t)
	defer cancel()

	// Register workflow handlers
	engine.RegisterWorkflowHandler(handlers.NewMessagingWorkflowHandler())

	t.Log("TestEngineWithMessagingWorkflow completed successfully")

	// Start engine
	err := engine.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start workflow: %v", err)
	}

	// Stop engine
	err = engine.Stop(ctx)
	if err != nil {
		t.Fatalf("Failed to stop workflow: %v", err)
	}
}

func TestEngineWithDataPipelineWorkflow(t *testing.T) {
	// Use the helper function for setup
	engine, _, ctx, cancel := setupEngineTest(t)
	defer cancel()

	// Register workflow handlers
	engine.RegisterWorkflowHandler(handlers.NewHTTPWorkflowHandler())
	engine.RegisterWorkflowHandler(handlers.NewMessagingWorkflowHandler())

	t.Log("TestEngineWithDataPipelineWorkflow completed successfully")

	// Start engine
	err := engine.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start workflow: %v", err)
	}

	// Stop engine
	err = engine.Stop(ctx)
	if err != nil {
		t.Fatalf("Failed to stop workflow: %v", err)
	}
}

// TestEngineTriggerIntegration tests the engine's integration with triggers
func TestEngineTriggerIntegration(t *testing.T) {
	// Create a mock application
	app := newMockApplication()

	// Create the engine
	engine := NewStdEngine(app, app.Logger())

	// Register a mock trigger with a matching configType
	mockTrigger := &mockTrigger{
		name:       "mock.trigger",
		configType: "mock", // This should match the key in the Triggers map
	}
	engine.RegisterTrigger(mockTrigger)

	// Create a simple workflow config with triggers
	cfg := &config.WorkflowConfig{
		Modules:   []config.ModuleConfig{},
		Workflows: map[string]any{},
		Triggers: map[string]any{
			"mock": map[string]any{
				"enabled": true,
				"config": map[string]any{
					"test": "value",
				},
			},
		},
	}

	// Build workflows from config
	err := engine.BuildFromConfig(cfg)
	if err != nil {
		t.Fatalf("Failed to build workflows: %v", err)
	}

	// Check if trigger was configured
	if !mockTrigger.configuredCalled {
		t.Error("Trigger Configure method was not called")
	}

	// Check if the engine registered itself as a service
	engineSvc, ok := app.services["workflowEngine"]
	if !ok {
		t.Error("StdEngine did not register itself as a service")
	}
	if engineSvc != engine {
		t.Error("Registered engine service is not the same instance")
	}

	// Test starting the engine
	ctx := context.Background()
	err = engine.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start engine: %v", err)
	}

	// Check if trigger was started
	if !mockTrigger.startCalled {
		t.Error("Trigger Start method was not called")
	}

	// Test stopping the engine
	err = engine.Stop(ctx)
	if err != nil {
		t.Fatalf("Failed to stop engine: %v", err)
	}

	// Check if trigger was stopped
	if !mockTrigger.stopCalled {
		t.Error("Trigger Stop method was not called")
	}
}

// TestEngineTriggerWorkflow tests the TriggerWorkflow method
func TestEngineTriggerWorkflow(t *testing.T) {
	// Create a mock application
	app := newMockApplication()

	// Create the engine
	engine := NewStdEngine(app, app.Logger())

	// Register a mock workflow handler
	mockHandler := &mockWorkflowHandler{
		name:       "mock.handler",
		handlesFor: []string{"test-workflow"},
	}
	engine.RegisterWorkflowHandler(mockHandler)

	// Test triggering a workflow
	ctx := context.Background()
	data := map[string]any{
		"param1": "value1",
		"param2": 42,
	}

	// Trigger a workflow
	err := engine.TriggerWorkflow(ctx, "test-workflow", "test-action", data)
	if err != nil {
		t.Fatalf("Failed to trigger workflow: %v", err)
	}

	// Test triggering an unknown workflow
	err = engine.TriggerWorkflow(ctx, "unknown-workflow", "test-action", data)
	if err == nil {
		t.Error("Expected error when triggering unknown workflow, but got nil")
	}
}

// Mock implementations for testing

// mockApplication implements modular.Application
type mockApplication struct {
	configs  map[string]any
	services map[string]any
	logger   *mockLogger
	// Add configSections to store registered config sections
	configSections map[string]modular.ConfigProvider
	modules        map[string]modular.Module
}

func (a *mockApplication) Config(key string) any {
	return a.configs[key]
}

func (a *mockApplication) Inject(name string, service any) {
	if a.services == nil {
		a.services = make(map[string]any)
	}
	a.services[name] = service
}

func (a *mockApplication) Logger() modular.Logger {
	return a.logger
}

func (a *mockApplication) Service(name string) (any, bool) {
	svc, ok := a.services[name]
	return svc, ok
}

func (a *mockApplication) Must(name string) any {
	svc, ok := a.services[name]
	if !ok {
		panic(fmt.Sprintf("Service %s not found", name))
	}
	return svc
}

func (a *mockApplication) Init() error {
	return nil
}

// GetService retrieves a service by name and populates the out parameter if provided
func (a *mockApplication) GetService(name string, out any) error {
	svc, ok := a.services[name]
	if !ok {
		return fmt.Errorf("service %s not found", name)
	}

	// If out is provided, try to assign the service to it using reflection
	if out != nil {
		// Get reflect values
		outVal := reflect.ValueOf(out)
		if outVal.Kind() != reflect.Pointer {
			return fmt.Errorf("out parameter must be a pointer")
		}

		// Dereference the pointer
		outVal = outVal.Elem()
		if !outVal.CanSet() {
			return fmt.Errorf("out parameter cannot be set")
		}

		// Set the value if compatible
		svcVal := reflect.ValueOf(svc)
		if !svcVal.Type().AssignableTo(outVal.Type()) {
			return fmt.Errorf("service type %s not assignable to out parameter type %s",
				svcVal.Type(), outVal.Type())
		}

		outVal.Set(svcVal)
	}

	return nil
}

// RegisterService registers a service with the application
func (a *mockApplication) RegisterService(name string, service any) error {
	if a.services == nil {
		a.services = make(map[string]any)
	}

	if _, exists := a.services[name]; exists {
		return fmt.Errorf("service already registered: %s", name)
	}

	a.services[name] = service
	return nil
}

// SvcRegistry returns the service registry for this application
func (a *mockApplication) SvcRegistry() modular.ServiceRegistry {
	if a.services == nil {
		a.services = make(map[string]any)
	}
	return a.services
}

// Run starts the application and blocks until stopped
func (a *mockApplication) Run() error {
	// Mock implementation - in a real app this would block
	return nil
}

// Start starts the application and returns immediately
func (a *mockApplication) Start() error {
	// Mock implementation
	return nil
}

// Stop stops the application
func (a *mockApplication) Stop() error {
	// Mock implementation
	return nil
}

// GetConfigSection returns a registered configuration section
func (a *mockApplication) GetConfigSection(name string) (modular.ConfigProvider, error) {
	if a.configSections == nil {
		a.configSections = make(map[string]modular.ConfigProvider)
	}

	provider, ok := a.configSections[name]
	if !ok {
		return nil, fmt.Errorf("config section %s not found", name)
	}
	return provider, nil
}

// RegisterConfigSection registers a configuration section
func (a *mockApplication) RegisterConfigSection(name string, provider modular.ConfigProvider) {
	if a.configSections == nil {
		a.configSections = make(map[string]modular.ConfigProvider)
	}
	a.configSections[name] = provider
}

// RegisterModule registers a module with the application
func (a *mockApplication) RegisterModule(module modular.Module) {
	if a.modules == nil {
		a.modules = make(map[string]modular.Module)
	}
	a.modules[module.Name()] = module
}

// Modules returns a map of registered modules
func (a *mockApplication) Modules() map[string]modular.Module {
	return a.modules
}

// ConfigSections returns the configuration sections for this application
func (a *mockApplication) ConfigSections() map[string]modular.ConfigProvider {
	return a.configSections
}

// ConfigProvider returns the configuration provider for this application
func (a *mockApplication) ConfigProvider() modular.ConfigProvider {
	return &mockConfigProvider{configs: a.configs}
}

// IsVerboseConfig returns whether verbose config debugging is enabled
func (a *mockApplication) IsVerboseConfig() bool {
	return false // Default to false for tests
}

// SetVerboseConfig sets verbose config debugging (no-op for tests)
func (a *mockApplication) SetVerboseConfig(enabled bool) {
	// No-op for tests
}

// SetLogger sets the application's logger
func (a *mockApplication) SetLogger(logger modular.Logger) {
	a.logger = logger.(*mockLogger) // Assume it's a mockLogger for tests
}

// GetServicesByModule returns all services provided by a specific module
func (a *mockApplication) GetServicesByModule(moduleName string) []string {
	return []string{}
}

// GetServiceEntry retrieves detailed information about a registered service
func (a *mockApplication) GetServiceEntry(serviceName string) (*modular.ServiceRegistryEntry, bool) {
	return nil, false
}

// GetServicesByInterface returns all services that implement the given interface
func (a *mockApplication) GetServicesByInterface(interfaceType reflect.Type) []*modular.ServiceRegistryEntry {
	return nil
}

// StartTime returns the time when the application was started
func (a *mockApplication) StartTime() time.Time {
	return time.Time{}
}

// GetModule returns the module with the given name
func (a *mockApplication) GetModule(name string) modular.Module {
	if a.modules == nil {
		return nil
	}
	return a.modules[name]
}

// GetAllModules returns a map of all registered modules
func (a *mockApplication) GetAllModules() map[string]modular.Module {
	return a.modules
}

// OnConfigLoaded registers a callback to run after config loading
func (a *mockApplication) OnConfigLoaded(hook func(modular.Application) error) {
	// No-op for tests
}

// mockConfigProvider implements the modular.ConfigProvider interface
type mockConfigProvider struct {
	configs map[string]any
}

func (p *mockConfigProvider) GetConfig() any {
	return p.configs
}

// newMockApplication creates a new mock application for testing
func newMockApplication() *mockApplication {
	return &mockApplication{
		configs:        make(map[string]any),
		services:       make(map[string]any),
		logger:         &mockLogger{},
		configSections: make(map[string]modular.ConfigProvider),
		modules:        make(map[string]modular.Module),
	}
}

// mockLogger implements modular.Logger
type mockLogger struct {
	mu   sync.Mutex
	logs []string
}

func (l *mockLogger) Debug(format string, args ...any) {
	l.mu.Lock()
	l.logs = append(l.logs, fmt.Sprintf("[DEBUG] "+format, args...))
	l.mu.Unlock()
}

func (l *mockLogger) Info(format string, args ...any) {
	l.mu.Lock()
	l.logs = append(l.logs, fmt.Sprintf("[INFO] "+format, args...))
	l.mu.Unlock()
}

func (l *mockLogger) Warn(format string, args ...any) {
	l.mu.Lock()
	l.logs = append(l.logs, fmt.Sprintf("[WARN] "+format, args...))
	l.mu.Unlock()
}

func (l *mockLogger) Error(format string, args ...any) {
	l.mu.Lock()
	l.logs = append(l.logs, fmt.Sprintf("[ERROR] "+format, args...))
	l.mu.Unlock()
}

func (l *mockLogger) Fatal(format string, args ...any) {
	l.logs = append(l.logs, fmt.Sprintf("[FATAL] "+format, args...))
}

// mockTrigger implements a simple trigger for testing
type mockTrigger struct {
	name             string
	configType       string
	initCalled       bool
	startCalled      bool
	stopCalled       bool
	configuredCalled bool
}

func (t *mockTrigger) Name() string {
	return t.name
}

func (t *mockTrigger) Init(app modular.Application) error {
	t.initCalled = true
	return nil
}

func (t *mockTrigger) Start(ctx context.Context) error {
	t.startCalled = true
	return nil
}

func (t *mockTrigger) Stop(ctx context.Context) error {
	t.stopCalled = true
	return nil
}

func (t *mockTrigger) Configure(app modular.Application, triggerConfig any) error {
	t.configuredCalled = true
	return nil
}

// mockWorkflowHandler implements a simple workflow handler for testing
type mockWorkflowHandler struct {
	name       string
	handlesFor []string
}

func (h *mockWorkflowHandler) Name() string {
	return h.name
}

func (h *mockWorkflowHandler) CanHandle(workflowType string) bool {
	return slices.Contains(h.handlesFor, workflowType)
}

func (h *mockWorkflowHandler) ConfigureWorkflow(app modular.Application, workflowConfig any) error {
	return nil
}

func (h *mockWorkflowHandler) ExecuteWorkflow(ctx context.Context, workflowType string, action string, data map[string]any) (map[string]any, error) {
	return nil, nil
}

func TestEngine_AddModuleType(t *testing.T) {
	app := newMockApplication()
	engine := NewStdEngine(app, app.Logger())

	called := false
	engine.AddModuleType("custom.module", func(name string, cfg map[string]any) modular.Module {
		called = true
		return &mockModule{name: name}
	})

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "my-custom", Type: "custom.module", Config: map[string]any{}},
		},
		Workflows: map[string]any{},
		Triggers:  map[string]any{},
	}

	err := engine.BuildFromConfig(cfg)
	if err != nil {
		t.Fatalf("BuildFromConfig failed: %v", err)
	}
	if !called {
		t.Error("expected custom factory to be called")
	}
}

func TestEngine_BuildFromConfig_BuiltinModules(t *testing.T) {
	tests := []struct {
		name       string
		moduleType string
		config     map[string]any
	}{
		{"http-server", "http.server", map[string]any{"address": ":8080"}},
		{"http-router", "http.router", map[string]any{}},
		{"http-handler", "http.handler", map[string]any{"contentType": "text/html"}},
		{"api-handler", "api.handler", map[string]any{"resourceName": "orders"}},
		{"auth-mw", "http.middleware.auth", map[string]any{"authType": "Bearer"}},
		{"logging-mw", "http.middleware.logging", map[string]any{"logLevel": "debug"}},
		{"ratelimit-mw", "http.middleware.ratelimit", map[string]any{"requestsPerMinute": 100.0, "burstSize": 20.0}},
		{"cors-mw", "http.middleware.cors", map[string]any{
			"allowedOrigins": []any{"http://localhost"},
			"allowedMethods": []any{"GET", "POST"},
		}},
		{"broker", "messaging.broker", map[string]any{}},
		{"msg-handler", "messaging.handler", map[string]any{}},
		{"sm-engine", "statemachine.engine", map[string]any{}},
		{"tracker", "state.tracker", map[string]any{}},
		{"connector", "state.connector", map[string]any{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newMockApplication()
			engine := NewStdEngine(app, app.Logger())

			cfg := &config.WorkflowConfig{
				Modules: []config.ModuleConfig{
					{Name: tt.name, Type: tt.moduleType, Config: tt.config},
				},
				Workflows: map[string]any{},
				Triggers:  map[string]any{},
			}

			err := engine.BuildFromConfig(cfg)
			if err != nil {
				t.Fatalf("BuildFromConfig failed for %s: %v", tt.moduleType, err)
			}
		})
	}
}

func TestEngine_BuildFromConfig_UnknownModuleType(t *testing.T) {
	app := newMockApplication()
	engine := NewStdEngine(app, app.Logger())

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "unknown", Type: "nonexistent.type", Config: map[string]any{}},
		},
		Workflows: map[string]any{},
		Triggers:  map[string]any{},
	}

	err := engine.BuildFromConfig(cfg)
	if err == nil {
		t.Fatal("expected error for unknown module type")
	}
}

func TestEngine_BuildFromConfig_NoHandlerForWorkflow(t *testing.T) {
	app := newMockApplication()
	engine := NewStdEngine(app, app.Logger())

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{},
		Workflows: map[string]any{
			"unknown-type": map[string]any{},
		},
		Triggers: map[string]any{},
	}

	err := engine.BuildFromConfig(cfg)
	if err == nil {
		t.Fatal("expected error for unhandled workflow type")
	}
}

func TestEngine_BuildFromConfig_WithWorkflowHandler(t *testing.T) {
	app := newMockApplication()
	engine := NewStdEngine(app, app.Logger())
	engine.RegisterWorkflowHandler(&mockWorkflowHandler{
		name:       "test",
		handlesFor: []string{"my-workflow"},
	})

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{},
		Workflows: map[string]any{
			"my-workflow": map[string]any{"key": "val"},
		},
		Triggers: map[string]any{},
	}

	err := engine.BuildFromConfig(cfg)
	if err != nil {
		t.Fatalf("BuildFromConfig failed: %v", err)
	}
}

func TestEngine_BuildFromConfig_TriggerNoHandler(t *testing.T) {
	app := newMockApplication()
	engine := NewStdEngine(app, app.Logger())

	cfg := &config.WorkflowConfig{
		Modules:   []config.ModuleConfig{},
		Workflows: map[string]any{},
		Triggers: map[string]any{
			"unknown-trigger": map[string]any{},
		},
	}

	err := engine.BuildFromConfig(cfg)
	if err == nil {
		t.Fatal("expected error for unhandled trigger type")
	}
}

func TestEngine_BuildFromConfig_ModularModules(t *testing.T) {
	modularTypes := []string{
		"httpserver.modular",
		"scheduler.modular",
		"auth.modular",
		"eventbus.modular",
		"cache.modular",
		"chimux.router",
		"eventlogger.modular",
		"httpclient.modular",
		"database.modular",
		"jsonschema.modular",
		"http.proxy",
		"reverseproxy",
	}

	for _, modType := range modularTypes {
		t.Run(modType, func(t *testing.T) {
			app := newMockApplication()
			engine := NewStdEngine(app, app.Logger())

			cfg := &config.WorkflowConfig{
				Modules: []config.ModuleConfig{
					{Name: "test-" + modType, Type: modType, Config: map[string]any{}},
				},
				Workflows: map[string]any{},
				Triggers:  map[string]any{},
			}

			err := engine.BuildFromConfig(cfg)
			if err != nil {
				t.Fatalf("BuildFromConfig failed for %s: %v", modType, err)
			}
		})
	}
}

func TestCanHandleTrigger(t *testing.T) {
	tests := []struct {
		triggerName string
		triggerType string
		expected    bool
	}{
		{"trigger.http", "http", true},
		{"trigger.schedule", "schedule", true},
		{"trigger.event", "event", true},
		{"mock.trigger", "mock", true},
		{"any.trigger", "unknown", false},
		{"trigger.http", "schedule", false},
	}

	for _, tt := range tests {
		t.Run(tt.triggerName+"_"+tt.triggerType, func(t *testing.T) {
			trigger := &mockTrigger{name: tt.triggerName}
			result := canHandleTrigger(trigger, tt.triggerType)
			if result != tt.expected {
				t.Errorf("canHandleTrigger(%q, %q) = %v, want %v", tt.triggerName, tt.triggerType, result, tt.expected)
			}
		})
	}
}

// mockModule implements modular.Module for testing
type mockModule struct {
	name string
}

func (m *mockModule) Name() string { return m.name }
func (m *mockModule) Init(app modular.Application) error {
	return app.RegisterService(m.name, m)
}

// errorMockTrigger returns errors from Start/Stop.
type errorMockTrigger struct {
	mockTrigger
	startErr error
	stopErr  error
}

func (t *errorMockTrigger) Start(ctx context.Context) error { return t.startErr }
func (t *errorMockTrigger) Stop(ctx context.Context) error  { return t.stopErr }

// errorMockWorkflowHandler returns error from ConfigureWorkflow.
type errorMockWorkflowHandler struct {
	mockWorkflowHandler
	configureErr error
	executeErr   error
}

func (h *errorMockWorkflowHandler) ConfigureWorkflow(app modular.Application, workflowConfig any) error {
	return h.configureErr
}

func (h *errorMockWorkflowHandler) ExecuteWorkflow(ctx context.Context, workflowType string, action string, data map[string]any) (map[string]any, error) {
	if h.executeErr != nil {
		return nil, h.executeErr
	}
	return map[string]any{"status": "ok"}, nil
}

// errorMockApplication extends mockApplication with Stop that returns errors.
type errorMockApplication struct {
	mockApplication
	stopErr error
}

func (a *errorMockApplication) Stop() error {
	return a.stopErr
}

func TestEngine_SetDynamicRegistry(t *testing.T) {
	app := newMockApplication()
	engine := NewStdEngine(app, app.Logger())

	registry := dynamic.NewComponentRegistry()
	engine.SetDynamicRegistry(registry)

	if engine.dynamicRegistry != registry {
		t.Error("expected dynamicRegistry to be set")
	}
}

func TestEngine_BuildFromConfig_EventBusBridge(t *testing.T) {
	app := newMockApplication()
	engine := NewStdEngine(app, app.Logger())

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "eb-bridge", Type: "messaging.broker.eventbus", Config: map[string]any{}},
		},
		Workflows: map[string]any{},
		Triggers:  map[string]any{},
	}

	err := engine.BuildFromConfig(cfg)
	if err != nil {
		t.Fatalf("BuildFromConfig failed for messaging.broker.eventbus: %v", err)
	}
}

func TestEngine_BuildFromConfig_MetricsHealthRequestID(t *testing.T) {
	tests := []struct {
		name       string
		moduleType string
	}{
		{"metrics", "metrics.collector"},
		{"health", "health.checker"},
		{"requestid", "http.middleware.requestid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newMockApplication()
			engine := NewStdEngine(app, app.Logger())

			cfg := &config.WorkflowConfig{
				Modules: []config.ModuleConfig{
					{Name: tt.name, Type: tt.moduleType, Config: map[string]any{}},
				},
				Workflows: map[string]any{},
				Triggers:  map[string]any{},
			}

			err := engine.BuildFromConfig(cfg)
			if err != nil {
				t.Fatalf("BuildFromConfig failed for %s: %v", tt.moduleType, err)
			}
		})
	}
}

func TestEngine_BuildFromConfig_DynamicComponent_NoRegistry(t *testing.T) {
	app := newMockApplication()
	engine := NewStdEngine(app, app.Logger())

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "dyn-comp", Type: "dynamic.component", Config: map[string]any{}},
		},
		Workflows: map[string]any{},
		Triggers:  map[string]any{},
	}

	err := engine.BuildFromConfig(cfg)
	if err == nil {
		t.Fatal("expected error for dynamic.component with nil registry")
	}
	if !strings.Contains(err.Error(), "dynamic registry not set") {
		t.Errorf("expected error containing 'dynamic registry not set', got: %v", err)
	}
}

func TestEngine_BuildFromConfig_DynamicComponent_NotFound(t *testing.T) {
	app := newMockApplication()
	engine := NewStdEngine(app, app.Logger())

	registry := dynamic.NewComponentRegistry()
	engine.SetDynamicRegistry(registry)

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "missing-comp", Type: "dynamic.component", Config: map[string]any{}},
		},
		Workflows: map[string]any{},
		Triggers:  map[string]any{},
	}

	err := engine.BuildFromConfig(cfg)
	if err == nil {
		t.Fatal("expected error for missing dynamic component")
	}
	if !strings.Contains(err.Error(), "not found in registry") {
		t.Errorf("expected error containing 'not found in registry', got: %v", err)
	}
}

func TestEngine_BuildFromConfig_DynamicComponent_Full(t *testing.T) {
	app := newMockApplication()
	engine := NewStdEngine(app, app.Logger())

	pool := dynamic.NewInterpreterPool()
	registry := dynamic.NewComponentRegistry()
	loader := dynamic.NewLoader(pool, registry)

	source := `package component

import "context"

func Name() string { return "test-comp" }
func Init(services map[string]interface{}) error { return nil }
func Start(ctx context.Context) error { return nil }
func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) { return params, nil }
func Stop(ctx context.Context) error { return nil }
`
	_, err := loader.LoadFromString("test-comp", source)
	if err != nil {
		t.Fatalf("LoadFromString failed: %v", err)
	}

	engine.SetDynamicRegistry(registry)

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name: "test-comp",
				Type: "dynamic.component",
				Config: map[string]any{
					"provides": []any{"my-svc"},
					"requires": []any{"other-svc"},
				},
			},
		},
		Workflows: map[string]any{},
		Triggers:  map[string]any{},
	}

	err = engine.BuildFromConfig(cfg)
	if err != nil {
		t.Fatalf("BuildFromConfig failed for dynamic.component: %v", err)
	}
}

func TestEngine_BuildFromConfig_DatabaseWorkflow(t *testing.T) {
	app := newMockApplication()
	engine := NewStdEngine(app, app.Logger())

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name: "wf-db",
				Type: "database.workflow",
				Config: map[string]any{
					"driver":       "sqlite3",
					"dsn":          ":memory:",
					"maxOpenConns": float64(10),
				},
			},
		},
		Workflows: map[string]any{},
		Triggers:  map[string]any{},
	}

	err := engine.BuildFromConfig(cfg)
	if err != nil {
		t.Fatalf("BuildFromConfig failed for database.workflow: %v", err)
	}
}

func TestEngine_BuildFromConfig_DataTransformerWebhook(t *testing.T) {
	app := newMockApplication()
	engine := NewStdEngine(app, app.Logger())

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "transformer", Type: "data.transformer", Config: map[string]any{}},
			{Name: "webhook", Type: "webhook.sender", Config: map[string]any{"maxRetries": float64(3)}},
		},
		Workflows: map[string]any{},
		Triggers:  map[string]any{},
	}

	err := engine.BuildFromConfig(cfg)
	if err != nil {
		t.Fatalf("BuildFromConfig failed for data.transformer/webhook.sender: %v", err)
	}
}

func TestEngine_BuildFromConfig_RateLimitIntConfig(t *testing.T) {
	app := newMockApplication()
	engine := NewStdEngine(app, app.Logger())

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name: "rl",
				Type: "http.middleware.ratelimit",
				Config: map[string]any{
					"requestsPerMinute": 120,
					"burstSize":         25,
				},
			},
		},
		Workflows: map[string]any{},
		Triggers:  map[string]any{},
	}

	err := engine.BuildFromConfig(cfg)
	if err != nil {
		t.Fatalf("BuildFromConfig failed for rate limiter with int config: %v", err)
	}
}

func TestEngine_BuildFromConfig_DefaultConfigs(t *testing.T) {
	t.Run("http.handler no contentType", func(t *testing.T) {
		app := newMockApplication()
		engine := NewStdEngine(app, app.Logger())

		cfg := &config.WorkflowConfig{
			Modules: []config.ModuleConfig{
				{Name: "h1", Type: "http.handler", Config: map[string]any{}},
			},
			Workflows: map[string]any{},
			Triggers:  map[string]any{},
		}

		err := engine.BuildFromConfig(cfg)
		if err != nil {
			t.Fatalf("BuildFromConfig failed: %v", err)
		}
	})

	t.Run("api.handler no resourceName", func(t *testing.T) {
		app := newMockApplication()
		engine := NewStdEngine(app, app.Logger())

		cfg := &config.WorkflowConfig{
			Modules: []config.ModuleConfig{
				{Name: "a1", Type: "api.handler", Config: map[string]any{}},
			},
			Workflows: map[string]any{},
			Triggers:  map[string]any{},
		}

		err := engine.BuildFromConfig(cfg)
		if err != nil {
			t.Fatalf("BuildFromConfig failed: %v", err)
		}
	})
}

func TestEngine_BuildFromConfig_HandlerConfigureError(t *testing.T) {
	app := newMockApplication()
	engine := NewStdEngine(app, app.Logger())

	engine.RegisterWorkflowHandler(&errorMockWorkflowHandler{
		mockWorkflowHandler: mockWorkflowHandler{
			name:       "err-handler",
			handlesFor: []string{"failing-workflow"},
		},
		configureErr: fmt.Errorf("configure failed"),
	})

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{},
		Workflows: map[string]any{
			"failing-workflow": map[string]any{},
		},
		Triggers: map[string]any{},
	}

	err := engine.BuildFromConfig(cfg)
	if err == nil {
		t.Fatal("expected error from handler ConfigureWorkflow")
	}
	if !strings.Contains(err.Error(), "configure failed") {
		t.Errorf("expected error containing 'configure failed', got: %v", err)
	}
}

func TestEngine_Start_TriggerStartError(t *testing.T) {
	app := newMockApplication()
	engine := NewStdEngine(app, app.Logger())

	trigger := &errorMockTrigger{
		mockTrigger: mockTrigger{name: "err-trigger"},
		startErr:    fmt.Errorf("trigger start failed"),
	}
	engine.triggers = append(engine.triggers, trigger)

	err := engine.Start(context.Background())
	if err == nil {
		t.Fatal("expected error from trigger Start")
	}
	if !strings.Contains(err.Error(), "trigger start failed") {
		t.Errorf("expected error containing 'trigger start failed', got: %v", err)
	}
}

func TestEngine_Stop_TriggerAndAppErrors(t *testing.T) {
	errApp := &errorMockApplication{
		mockApplication: *newMockApplication(),
		stopErr:         fmt.Errorf("app stop failed"),
	}

	engine := NewStdEngine(errApp, errApp.Logger())

	trigger := &errorMockTrigger{
		mockTrigger: mockTrigger{name: "err-trigger"},
		stopErr:     fmt.Errorf("trigger stop failed"),
	}
	engine.triggers = append(engine.triggers, trigger)

	err := engine.Stop(context.Background())
	if err == nil {
		t.Fatal("expected error from Stop")
	}
	// The last error should be the app stop error (it overwrites the trigger error)
	if !strings.Contains(err.Error(), "app stop failed") {
		t.Errorf("expected error containing 'app stop failed', got: %v", err)
	}
}

func TestEngine_TriggerWorkflow_WithEventEmitter(t *testing.T) {
	app := newMockApplication()
	engine := NewStdEngine(app, app.Logger())

	handler := &errorMockWorkflowHandler{
		mockWorkflowHandler: mockWorkflowHandler{
			name:       "test-handler",
			handlesFor: []string{"test-wf"},
		},
	}
	engine.RegisterWorkflowHandler(handler)

	// Create a no-op event emitter (no eventbus registered)
	engine.eventEmitter = module.NewWorkflowEventEmitter(app)

	err := engine.TriggerWorkflow(context.Background(), "test-wf", "act", map[string]any{"key": "val"})
	if err != nil {
		t.Fatalf("TriggerWorkflow should succeed, got: %v", err)
	}
}

func TestEngine_TriggerWorkflow_FailureWithEventEmitter(t *testing.T) {
	app := newMockApplication()
	engine := NewStdEngine(app, app.Logger())

	handler := &errorMockWorkflowHandler{
		mockWorkflowHandler: mockWorkflowHandler{
			name:       "fail-handler",
			handlesFor: []string{"fail-wf"},
		},
		executeErr: fmt.Errorf("execution failed"),
	}
	engine.RegisterWorkflowHandler(handler)

	// Create a no-op event emitter (no eventbus registered)
	engine.eventEmitter = module.NewWorkflowEventEmitter(app)

	err := engine.TriggerWorkflow(context.Background(), "fail-wf", "act", map[string]any{})
	if err == nil {
		t.Fatal("expected error from failing handler")
	}
	if !strings.Contains(err.Error(), "execution failed") {
		t.Errorf("expected error containing 'execution failed', got: %v", err)
	}
}

func TestEngine_TriggerWorkflow_WithMetricsCollector(t *testing.T) {
	app := newMockApplication()
	engine := NewStdEngine(app, app.Logger())

	handler := &errorMockWorkflowHandler{
		mockWorkflowHandler: mockWorkflowHandler{
			name:       "metrics-handler",
			handlesFor: []string{"metrics-wf"},
		},
	}
	engine.RegisterWorkflowHandler(handler)

	// Register a MetricsCollector service
	mc := module.NewMetricsCollector("test-metrics")
	app.services["metrics.collector"] = mc

	engine.eventEmitter = module.NewWorkflowEventEmitter(app)

	err := engine.TriggerWorkflow(context.Background(), "metrics-wf", "act", map[string]any{})
	if err != nil {
		t.Fatalf("TriggerWorkflow should succeed, got: %v", err)
	}
}

func TestCanHandleTrigger_EventBus(t *testing.T) {
	trigger := &mockTrigger{name: module.EventBusTriggerName}
	result := canHandleTrigger(trigger, "eventbus")
	if !result {
		t.Errorf("canHandleTrigger(%q, %q) = false, want true", module.EventBusTriggerName, "eventbus")
	}
}
