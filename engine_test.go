package workflow

import (
	"context"
	"fmt"
	"testing"
	"time"

	"reflect"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/handlers"
	"github.com/GoCodeAlone/workflow/mock"
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
		Workflows: map[string]interface{}{},
		Triggers: map[string]interface{}{
			"mock": map[string]interface{}{
				"enabled": true,
				"config": map[string]interface{}{
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
	data := map[string]interface{}{
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
	configs  map[string]interface{}
	services map[string]interface{}
	logger   *mockLogger
	// Add configSections to store registered config sections
	configSections map[string]modular.ConfigProvider
	modules        map[string]modular.Module
}

func (a *mockApplication) Config(key string) interface{} {
	return a.configs[key]
}

func (a *mockApplication) Inject(name string, service interface{}) {
	if a.services == nil {
		a.services = make(map[string]interface{})
	}
	a.services[name] = service
}

func (a *mockApplication) Logger() modular.Logger {
	return a.logger
}

func (a *mockApplication) Service(name string) (interface{}, bool) {
	svc, ok := a.services[name]
	return svc, ok
}

func (a *mockApplication) Must(name string) interface{} {
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
func (a *mockApplication) GetService(name string, out interface{}) error {
	svc, ok := a.services[name]
	if !ok {
		return fmt.Errorf("service %s not found", name)
	}

	// If out is provided, try to assign the service to it using reflection
	if out != nil {
		// Get reflect values
		outVal := reflect.ValueOf(out)
		if outVal.Kind() != reflect.Ptr {
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
func (a *mockApplication) RegisterService(name string, service interface{}) error {
	if a.services == nil {
		a.services = make(map[string]interface{})
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
		a.services = make(map[string]interface{})
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
	configs map[string]interface{}
}

func (p *mockConfigProvider) GetConfig() interface{} {
	return p.configs
}

// newMockApplication creates a new mock application for testing
func newMockApplication() *mockApplication {
	return &mockApplication{
		configs:        make(map[string]interface{}),
		services:       make(map[string]interface{}),
		logger:         &mockLogger{},
		configSections: make(map[string]modular.ConfigProvider),
		modules:        make(map[string]modular.Module),
	}
}

// mockLogger implements modular.Logger
type mockLogger struct {
	logs []string
}

func (l *mockLogger) Debug(format string, args ...interface{}) {
	l.logs = append(l.logs, fmt.Sprintf("[DEBUG] "+format, args...))
}

func (l *mockLogger) Info(format string, args ...interface{}) {
	l.logs = append(l.logs, fmt.Sprintf("[INFO] "+format, args...))
}

func (l *mockLogger) Warn(format string, args ...interface{}) {
	l.logs = append(l.logs, fmt.Sprintf("[WARN] "+format, args...))
}

func (l *mockLogger) Error(format string, args ...interface{}) {
	l.logs = append(l.logs, fmt.Sprintf("[ERROR] "+format, args...))
}

func (l *mockLogger) Fatal(format string, args ...interface{}) {
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

func (t *mockTrigger) Configure(app modular.Application, triggerConfig interface{}) error {
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
	for _, wt := range h.handlesFor {
		if wt == workflowType {
			return true
		}
	}
	return false
}

func (h *mockWorkflowHandler) ConfigureWorkflow(app modular.Application, workflowConfig interface{}) error {
	return nil
}

func (h *mockWorkflowHandler) ExecuteWorkflow(ctx context.Context, workflowType string, action string, data map[string]interface{}) (map[string]interface{}, error) {
	return nil, nil
}

func TestEngine_AddModuleType(t *testing.T) {
	app := newMockApplication()
	engine := NewStdEngine(app, app.Logger())

	called := false
	engine.AddModuleType("custom.module", func(name string, cfg map[string]interface{}) modular.Module {
		called = true
		return &mockModule{name: name}
	})

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "my-custom", Type: "custom.module", Config: map[string]interface{}{}},
		},
		Workflows: map[string]interface{}{},
		Triggers:  map[string]interface{}{},
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
		config     map[string]interface{}
	}{
		{"http-server", "http.server", map[string]interface{}{"address": ":8080"}},
		{"http-router", "http.router", map[string]interface{}{}},
		{"http-handler", "http.handler", map[string]interface{}{"contentType": "text/html"}},
		{"api-handler", "api.handler", map[string]interface{}{"resourceName": "orders"}},
		{"auth-mw", "http.middleware.auth", map[string]interface{}{"authType": "Bearer"}},
		{"logging-mw", "http.middleware.logging", map[string]interface{}{"logLevel": "debug"}},
		{"ratelimit-mw", "http.middleware.ratelimit", map[string]interface{}{"requestsPerMinute": 100.0, "burstSize": 20.0}},
		{"cors-mw", "http.middleware.cors", map[string]interface{}{
			"allowedOrigins": []interface{}{"http://localhost"},
			"allowedMethods": []interface{}{"GET", "POST"},
		}},
		{"broker", "messaging.broker", map[string]interface{}{}},
		{"msg-handler", "messaging.handler", map[string]interface{}{}},
		{"sm-engine", "statemachine.engine", map[string]interface{}{}},
		{"tracker", "state.tracker", map[string]interface{}{}},
		{"connector", "state.connector", map[string]interface{}{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newMockApplication()
			engine := NewStdEngine(app, app.Logger())

			cfg := &config.WorkflowConfig{
				Modules: []config.ModuleConfig{
					{Name: tt.name, Type: tt.moduleType, Config: tt.config},
				},
				Workflows: map[string]interface{}{},
				Triggers:  map[string]interface{}{},
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
			{Name: "unknown", Type: "nonexistent.type", Config: map[string]interface{}{}},
		},
		Workflows: map[string]interface{}{},
		Triggers:  map[string]interface{}{},
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
		Workflows: map[string]interface{}{
			"unknown-type": map[string]interface{}{},
		},
		Triggers: map[string]interface{}{},
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
		Workflows: map[string]interface{}{
			"my-workflow": map[string]interface{}{"key": "val"},
		},
		Triggers: map[string]interface{}{},
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
		Workflows: map[string]interface{}{},
		Triggers: map[string]interface{}{
			"unknown-trigger": map[string]interface{}{},
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
					{Name: "test-" + modType, Type: modType, Config: map[string]interface{}{}},
				},
				Workflows: map[string]interface{}{},
				Triggers:  map[string]interface{}{},
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
