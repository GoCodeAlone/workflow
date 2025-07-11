package module

import (
	"context"
	"fmt"
	"reflect"

	"github.com/GoCodeAlone/modular"
)

// MockApplication is a mock implementation of modular.Application for testing
type MockApplication struct {
	Services         map[string]interface{}
	Config           map[string]interface{}
	ConfigSectionMap map[string]modular.ConfigProvider
	MockLogger       *MockLogger
	Modules          map[string]modular.Module
}

// NewMockApplication creates a new instance of a MockApplication
func NewMockApplication() *MockApplication {
	return &MockApplication{
		Services:         make(map[string]interface{}),
		Config:           make(map[string]interface{}),
		ConfigSectionMap: make(map[string]modular.ConfigProvider),
		MockLogger:       &MockLogger{},
		Modules:          make(map[string]modular.Module),
	}
}

func (a *MockApplication) RegisterService(name string, service interface{}) error {
	a.Services[name] = service
	return nil
}

func (a *MockApplication) GetService(name string, out interface{}) error {
	service, exists := a.Services[name]
	if !exists {
		return fmt.Errorf("service %s not found", name)
	}

	// Use reflection to set the output pointer
	outVal := reflect.ValueOf(out)
	if outVal.Kind() != reflect.Ptr {
		return fmt.Errorf("out parameter must be a pointer")
	}

	// Get the element that the pointer points to
	outElem := outVal.Elem()

	// If outElem is an interface, we can set it directly
	if outElem.Kind() == reflect.Interface {
		outElem.Set(reflect.ValueOf(service))
		return nil
	}

	// Otherwise, try to convert the service to the appropriate type
	svcVal := reflect.ValueOf(service)
	if !svcVal.Type().AssignableTo(outElem.Type()) {
		return fmt.Errorf("service type %s not assignable to output type %s",
			svcVal.Type(), outElem.Type())
	}

	outElem.Set(svcVal)
	return nil
}

func (a *MockApplication) ConfigProvider() modular.ConfigProvider {
	return &MockConfigProvider{Config: a.Config}
}

func (a *MockApplication) GetConfig() map[string]interface{} {
	return a.Config
}

func (a *MockApplication) GetConfigSection(section string) (modular.ConfigProvider, error) {
	if val, ok := a.ConfigSectionMap[section]; ok {
		return val, nil
	}
	return nil, fmt.Errorf("config section %s not found", section)
}

func (a *MockApplication) ConfigSections() map[string]modular.ConfigProvider {
	return a.ConfigSectionMap
}

func (a *MockApplication) Logger() modular.Logger {
	return a.MockLogger
}

func (a *MockApplication) Init() error {
	return nil
}

func (a *MockApplication) Start() error {
	return nil
}

func (a *MockApplication) Stop() error {
	return nil
}

func (a *MockApplication) RegisterConfigSection(name string, config modular.ConfigProvider) {
	a.ConfigSectionMap[name] = config
}

// RegisterModule registers a module with the application
func (a *MockApplication) RegisterModule(module modular.Module) {
	if a.Modules == nil {
		a.Modules = make(map[string]modular.Module)
	}
	a.Modules[module.Name()] = module
}

// Run satisfies the modular.Application interface
func (a *MockApplication) Run() error {
	// Simple implementation for testing
	return nil
}

// IsVerboseConfig returns whether verbose config debugging is enabled
func (a *MockApplication) IsVerboseConfig() bool {
	return false // Default to false for tests
}

// SetVerboseConfig sets verbose config debugging (no-op for tests)
func (a *MockApplication) SetVerboseConfig(enabled bool) {
	// No-op for tests
}

// SetLogger sets the application's logger
func (a *MockApplication) SetLogger(logger modular.Logger) {
	a.MockLogger = logger.(*MockLogger) // Assume it's a MockLogger for tests
}

// SvcRegistry satisfies the modular.Application interface
func (a *MockApplication) SvcRegistry() modular.ServiceRegistry {
	// Return the Services map directly as it implements the needed interface
	return a.Services
}

// modularServiceRegistryAdapter adapts our MockApplication to the modular.ServiceRegistry interface
type modularServiceRegistryAdapter struct {
	app *MockApplication
}

// GetService forwards to the app's GetService method
func (a *modularServiceRegistryAdapter) GetService(name string, out interface{}) error {
	return a.app.GetService(name, out)
}

// RegisterService forwards to the app's RegisterService method
func (a *modularServiceRegistryAdapter) RegisterService(name string, service interface{}) error {
	return a.app.RegisterService(name, service)
}

// MockConfigProvider is a mock implementation of modular.ConfigProvider for testing
type MockConfigProvider struct {
	Config map[string]interface{} // Changed from lowercase config to Config to match usage elsewhere
}

func (p *MockConfigProvider) GetConfig() any {
	return p.Config // Changed to use the capitalized Config field
}

// MockLogger implements modular.Logger for testing
type MockLogger struct {
	Messages []string
}

func (l *MockLogger) Debug(format string, args ...interface{}) {
	l.Messages = append(l.Messages, fmt.Sprintf(format, args...))
}

func (l *MockLogger) Info(format string, args ...interface{}) {
	l.Messages = append(l.Messages, fmt.Sprintf(format, args...))
}

func (l *MockLogger) Warn(format string, args ...interface{}) {
	l.Messages = append(l.Messages, fmt.Sprintf(format, args...))
}

func (l *MockLogger) Error(format string, args ...interface{}) {
	l.Messages = append(l.Messages, fmt.Sprintf(format, args...))
}

func (l *MockLogger) Fatal(format string, args ...interface{}) {
	l.Messages = append(l.Messages, fmt.Sprintf(format, args...))
}

// WorkflowTriggerInfo captures information about a workflow that was triggered
type WorkflowTriggerInfo struct {
	WorkflowType string
	Action       string
	Data         map[string]interface{}
}

// MockWorkflowEngine is a mock implementation of the WorkflowEngine interface
type MockWorkflowEngine struct {
	triggeredWorkflows []WorkflowTriggerInfo
}

func NewMockWorkflowEngine() *MockWorkflowEngine {
	return &MockWorkflowEngine{
		triggeredWorkflows: make([]WorkflowTriggerInfo, 0),
	}
}

func (e *MockWorkflowEngine) TriggerWorkflow(ctx context.Context, workflowType string, action string, data map[string]interface{}) error {
	e.triggeredWorkflows = append(e.triggeredWorkflows, WorkflowTriggerInfo{
		WorkflowType: workflowType,
		Action:       action,
		Data:         data,
	})
	return nil
}
