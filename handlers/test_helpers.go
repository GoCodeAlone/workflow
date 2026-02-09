// Package handlers provides workflow handling capabilities
package handlers

import (
	"context"
	"reflect"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/mock"
)

// TestServiceRegistry provides a simple service registry for testing
type TestServiceRegistry struct {
	services       map[string]interface{}
	config         modular.ConfigProvider
	logger         modular.Logger
	configSections map[string]modular.ConfigProvider
}

// NewTestServiceRegistry creates a new test service registry
func NewTestServiceRegistry() *TestServiceRegistry {
	// Use the new mock.NewConfigProvider() function if it exists
	// Otherwise, create and initialize a proper config provider
	mockConfig := &mock.ConfigProvider{ConfigData: make(map[string]interface{})}
	mockConfig.UpdateConfigWithProperEnvStructure()

	return &TestServiceRegistry{
		services:       make(map[string]interface{}),
		configSections: make(map[string]modular.ConfigProvider),
		config:         mockConfig,
		logger:         &mock.Logger{LogEntries: make([]string, 0)},
	}
}

// GetService implements service retrieval for testing
func (t *TestServiceRegistry) GetService(name string, dest interface{}) error {
	return nil // Simplified implementation for tests
}

// RegisterService implements service registration for testing
func (t *TestServiceRegistry) RegisterService(name string, service interface{}) error {
	t.services[name] = service
	return nil
}

// SvcRegistry returns the services map as modular.ServiceRegistry
func (t *TestServiceRegistry) SvcRegistry() modular.ServiceRegistry {
	return t.services
}

// Init initializes the test registry
func (t *TestServiceRegistry) Init() error {
	return nil
}

// Start simulates application start
func (t *TestServiceRegistry) Start() error {
	return nil
}

// Stop simulates application stop
func (t *TestServiceRegistry) Stop() error {
	return nil
}

// Run simulates application run
func (t *TestServiceRegistry) Run() error {
	return nil
}

// Logger returns a logger
func (t *TestServiceRegistry) Logger() modular.Logger {
	return t.logger
}

// ConfigProvider returns the config provider
func (t *TestServiceRegistry) ConfigProvider() modular.ConfigProvider {
	return t.config
}

// ConfigSections returns configuration sections
func (t *TestServiceRegistry) ConfigSections() map[string]modular.ConfigProvider {
	return t.configSections
}

// RegisterConfigSection registers a config section
func (t *TestServiceRegistry) RegisterConfigSection(name string, config modular.ConfigProvider) {
	t.configSections[name] = config
}

// GetConfigSection returns a config section
func (t *TestServiceRegistry) GetConfigSection(section string) (modular.ConfigProvider, error) {
	return t.configSections[section], nil
}

// IsVerboseConfig returns whether verbose config debugging is enabled
func (t *TestServiceRegistry) IsVerboseConfig() bool {
	return false // Default to false for tests
}

// SetVerboseConfig sets verbose config debugging (no-op for tests)
func (t *TestServiceRegistry) SetVerboseConfig(enabled bool) {
	// No-op for tests
}

// SetLogger sets the application's logger
func (t *TestServiceRegistry) SetLogger(logger modular.Logger) {
	t.logger = logger
}

// RegisterModule registers a module in the test registry
func (t *TestServiceRegistry) RegisterModule(module modular.Module) {
	// Simplified implementation for tests
}

// GetServicesByModule returns all services provided by a specific module
func (t *TestServiceRegistry) GetServicesByModule(moduleName string) []string {
	return []string{}
}

// GetServiceEntry retrieves detailed information about a registered service
func (t *TestServiceRegistry) GetServiceEntry(serviceName string) (*modular.ServiceRegistryEntry, bool) {
	return nil, false
}

// GetServicesByInterface returns all services that implement the given interface
func (t *TestServiceRegistry) GetServicesByInterface(interfaceType reflect.Type) []*modular.ServiceRegistryEntry {
	return nil
}

// StartTime returns the time when the application was started
func (t *TestServiceRegistry) StartTime() time.Time {
	return time.Time{}
}

// GetModule returns the module with the given name
func (t *TestServiceRegistry) GetModule(name string) modular.Module {
	return nil
}

// GetAllModules returns a map of all registered modules
func (t *TestServiceRegistry) GetAllModules() map[string]modular.Module {
	return nil
}

// OnConfigLoaded registers a callback to run after config loading
func (t *TestServiceRegistry) OnConfigLoaded(hook func(modular.Application) error) {
	// No-op for tests
}

// CreateMockApplication creates a mock application for testing
func CreateMockApplication() modular.Application {
	return NewTestServiceRegistry()
}

// SetMockConfig sets a custom config provider
func (t *TestServiceRegistry) SetMockConfig(config modular.ConfigProvider) {
	t.config = config
}

// SetMockLogger sets a custom logger
func (t *TestServiceRegistry) SetMockLogger(logger modular.Logger) {
	t.logger = logger
}

// TestJob is a simple job implementation for testing
type TestJob struct {
	ExecuteFn func(ctx context.Context) error
}

// Execute executes the job
func (j *TestJob) Execute(ctx context.Context) error {
	if j.ExecuteFn != nil {
		return j.ExecuteFn(ctx)
	}
	return nil
}

// NewTestJob creates a new test job
func NewTestJob(fn func(ctx context.Context) error) *TestJob {
	return &TestJob{
		ExecuteFn: fn,
	}
}

// SchedulerTestHelper contains utilities for testing schedulers
type SchedulerTestHelper struct {
	App modular.Application
}

// NewSchedulerTestHelper creates a new scheduler test helper
func NewSchedulerTestHelper(app modular.Application) *SchedulerTestHelper {
	return &SchedulerTestHelper{
		App: app,
	}
}

// RegisterTestJob registers a test job with the application
func (h *SchedulerTestHelper) RegisterTestJob(name string, fn func(ctx context.Context) error) *TestJob {
	job := NewTestJob(fn)
	// Use RegisterService instead of direct map assignment to work with the enhanced service registry
	if err := h.App.RegisterService(name, job); err != nil {
		// If it fails (e.g., already registered), fall back to direct assignment
		h.App.SvcRegistry()[name] = job
	}
	return job
}

// TriggerJobExecution manually triggers execution of a job
func (h *SchedulerTestHelper) TriggerJobExecution(ctx context.Context, jobName string) error {
	if job, exists := h.App.SvcRegistry()[jobName]; exists {
		if executableJob, ok := job.(interface {
			Execute(ctx context.Context) error
		}); ok {
			return executableJob.Execute(ctx)
		}
	}
	return nil
}

// MockEngine is a simplified engine for testing
type MockEngine struct {
	app      modular.Application
	handlers map[string]interface{}
}

// NewTestEngine creates a workflow engine for testing
func NewTestEngine(app modular.Application) *MockEngine {
	engine := &MockEngine{
		app:      app,
		handlers: make(map[string]interface{}),
	}

	// Register workflow handlers
	engine.RegisterHandler("event", &EventWorkflowHandler{})
	engine.RegisterHandler("integration", &IntegrationWorkflowHandler{})
	engine.RegisterHandler("state-machine", &StateMachineWorkflowHandler{})

	return engine
}

// RegisterHandler registers a handler with the mock engine
func (e *MockEngine) RegisterHandler(name string, handler interface{}) {
	e.handlers[name] = handler
}

// Start simulates starting the engine
func (e *MockEngine) Start(ctx context.Context) error {
	// Mock implementation that does nothing but succeed
	return nil
}

// Stop simulates stopping the engine
func (e *MockEngine) Stop(ctx context.Context) error {
	// Mock implementation that does nothing but succeed
	return nil
}

// BuildFromConfig simulates building a workflow from config
func (e *MockEngine) BuildFromConfig(cfg interface{}) error {
	// Mock implementation that does nothing but succeed
	return nil
}

// AddModuleType adds a module type to the mock engine
func (e *MockEngine) AddModuleType(name string, creator interface{}) {
	// Mock implementation
}
