package workflow

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/handlers"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/mock"
	"github.com/GoCodeAlone/workflow/module"
)

func init() {
	// Register default factories for tests. In production code, the
	// setup package does this via a blank import.
	if DefaultHandlerFactory == nil {
		DefaultHandlerFactory = func() []WorkflowHandler {
			return []WorkflowHandler{
				handlers.NewHTTPWorkflowHandler(),
				handlers.NewMessagingWorkflowHandler(),
				handlers.NewStateMachineWorkflowHandler(),
				handlers.NewSchedulerWorkflowHandler(),
				handlers.NewIntegrationWorkflowHandler(),
				handlers.NewPipelineWorkflowHandler(),
				handlers.NewEventWorkflowHandler(),
				handlers.NewPlatformWorkflowHandler(),
			}
		}
	}
	if DefaultTriggerFactory == nil {
		DefaultTriggerFactory = func() []interfaces.Trigger {
			return []interfaces.Trigger{
				module.NewHTTPTrigger(),
				module.NewEventTrigger(),
				module.NewScheduleTrigger(),
				module.NewEventBusTrigger(),
				module.NewReconciliationTrigger(),
			}
		}
	}
}

func TestEngineBuilder_Build_Defaults(t *testing.T) {
	// Build with no options — should create engine with default app/logger
	engine, err := NewEngineBuilder().Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if engine == nil {
		t.Fatal("Build() returned nil engine")
	}
	if engine.App() == nil {
		t.Fatal("engine.App() is nil; expected default application")
	}
}

func TestEngineBuilder_Build_CustomAppAndLogger(t *testing.T) {
	logger := &mock.Logger{LogEntries: make([]string, 0)}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)

	engine, err := NewEngineBuilder().
		WithApplication(app).
		WithLogger(logger).
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if engine.App() != app {
		t.Error("engine.App() does not match provided application")
	}
}

func TestEngineBuilder_WithDefaultHandlers(t *testing.T) {
	logger := &mock.Logger{LogEntries: make([]string, 0)}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)

	engine, err := NewEngineBuilder().
		WithApplication(app).
		WithLogger(logger).
		WithDefaultHandlers().
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	// Verify that the engine can handle known workflow types
	workflowTypes := []string{"http", "messaging", "statemachine", "scheduler", "integration"}
	for _, wt := range workflowTypes {
		found := false
		for _, handler := range engine.workflowHandlers {
			if handler.CanHandle(wt) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected handler for workflow type %q to be registered", wt)
		}
	}
}

func TestEngineBuilder_WithDefaultTriggers(t *testing.T) {
	logger := &mock.Logger{LogEntries: make([]string, 0)}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)

	engine, err := NewEngineBuilder().
		WithApplication(app).
		WithLogger(logger).
		WithDefaultTriggers().
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	// There should be at least 5 triggers (HTTP, Event, Schedule, EventBus, Reconciliation)
	if len(engine.triggers) < 5 {
		t.Errorf("expected at least 5 triggers, got %d", len(engine.triggers))
	}

	// Verify specific trigger names
	triggerNames := make(map[string]bool)
	for _, trigger := range engine.triggers {
		triggerNames[trigger.Name()] = true
	}
	expectedTriggers := []string{"trigger.http", "trigger.event", "trigger.schedule"}
	for _, name := range expectedTriggers {
		if !triggerNames[name] {
			t.Errorf("expected trigger %q to be registered", name)
		}
	}
}

func TestEngineBuilder_WithDynamicComponents(t *testing.T) {
	logger := &mock.Logger{LogEntries: make([]string, 0)}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)

	engine, err := NewEngineBuilder().
		WithApplication(app).
		WithLogger(logger).
		WithDynamicComponents().
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if engine.dynamicRegistry == nil {
		t.Error("expected dynamicRegistry to be set")
	}
	if engine.dynamicLoader == nil {
		t.Error("expected dynamicLoader to be set")
	}
}

func TestEngineBuilder_WithAllDefaults(t *testing.T) {
	engine, err := NewEngineBuilder().WithAllDefaults().Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	// Should have handlers, triggers, and dynamic components
	if len(engine.workflowHandlers) == 0 {
		t.Error("expected workflow handlers to be registered")
	}
	if len(engine.triggers) == 0 {
		t.Error("expected triggers to be registered")
	}
	if engine.dynamicRegistry == nil {
		t.Error("expected dynamicRegistry to be set")
	}
	if engine.dynamicLoader == nil {
		t.Error("expected dynamicLoader to be set")
	}
}

func TestEngineBuilder_WithCustomHandler(t *testing.T) {
	handler := handlers.NewHTTPWorkflowHandler()
	engine, err := NewEngineBuilder().
		WithHandler(handler).
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if len(engine.workflowHandlers) != 1 {
		t.Errorf("expected 1 handler, got %d", len(engine.workflowHandlers))
	}
}

func TestEngineBuilder_WithCustomTrigger(t *testing.T) {
	trigger := module.NewHTTPTrigger()
	engine, err := NewEngineBuilder().
		WithTrigger(trigger).
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if len(engine.triggers) != 1 {
		t.Errorf("expected 1 trigger, got %d", len(engine.triggers))
	}
}

func TestEngineBuilder_WithPlugins(t *testing.T) {
	logger := &mock.Logger{LogEntries: make([]string, 0)}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)

	engine, err := NewEngineBuilder().
		WithApplication(app).
		WithLogger(logger).
		WithPlugins(allPlugins()...).
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	loadedPlugins := engine.LoadedPlugins()
	if len(loadedPlugins) == 0 {
		t.Error("expected at least one loaded plugin")
	}
}

func TestEngineBuilder_WithPlugin(t *testing.T) {
	logger := &mock.Logger{LogEntries: make([]string, 0)}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)

	// Pick just one plugin to test single-plugin loading
	plugins := allPlugins()
	if len(plugins) == 0 {
		t.Skip("no plugins available")
	}
	engine, err := NewEngineBuilder().
		WithApplication(app).
		WithLogger(logger).
		WithPlugin(plugins[0]).
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if len(engine.LoadedPlugins()) != 1 {
		t.Errorf("expected 1 loaded plugin, got %d", len(engine.LoadedPlugins()))
	}
}

func TestEngineBuilder_Chaining(t *testing.T) {
	// Test that all builder methods return the same builder for chaining
	mockLogger := &mock.Logger{LogEntries: make([]string, 0)}
	mockApp := modular.NewStdApplication(modular.NewStdConfigProvider(nil), mockLogger)
	b := NewEngineBuilder()
	result := b.
		WithApplication(mockApp).
		WithLogger(mockLogger).
		WithDefaultHandlers().
		WithDefaultTriggers().
		WithDynamicComponents().
		WithAllDefaults().
		WithHandler(&builderTestHandler{canHandleType: "test"}).
		WithTrigger(module.NewHTTPTrigger()).
		WithPlugins().
		WithPluginLoader(nil).
		WithConfigPath("test.yaml")

	if result != b {
		t.Error("expected chained calls to return the same builder")
	}
}

func TestEngineBuilder_BuildFromConfig(t *testing.T) {
	logger := &mock.Logger{LogEntries: make([]string, 0)}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)

	cfg := &config.WorkflowConfig{
		Modules:   []config.ModuleConfig{},
		Workflows: map[string]any{},
	}

	engine, err := NewEngineBuilder().
		WithApplication(app).
		WithLogger(logger).
		WithAllDefaults().
		WithPlugins(allPlugins()...).
		BuildFromConfig(cfg)
	if err != nil {
		t.Fatalf("BuildFromConfig() error: %v", err)
	}
	if engine == nil {
		t.Fatal("BuildFromConfig() returned nil engine")
	}
}

func TestEngineBuilder_BuildAndConfigure_NoPath(t *testing.T) {
	_, err := NewEngineBuilder().BuildAndConfigure()
	if err == nil {
		t.Fatal("expected error when no config path is set")
	}
	if err.Error() != "no config path set; call WithConfigPath() first" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestEngineBuilder_BuildAndConfigure_InvalidPath(t *testing.T) {
	_, err := NewEngineBuilder().
		WithConfigPath("/nonexistent/path.yaml").
		BuildAndConfigure()
	if err == nil {
		t.Fatal("expected error for invalid config path")
	}
}

func TestEngineBuilder_StartStop(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)

	cfg := &config.WorkflowConfig{
		Modules:   []config.ModuleConfig{},
		Workflows: map[string]any{},
	}

	engine, err := NewEngineBuilder().
		WithApplication(app).
		WithLogger(logger).
		WithAllDefaults().
		WithPlugins(allPlugins()...).
		BuildFromConfig(cfg)
	if err != nil {
		t.Fatalf("BuildFromConfig() error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	if err := engine.Stop(ctx); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}
}

func TestEngineBuilder_DefaultsWithoutExplicitApp(t *testing.T) {
	// Build with all defaults but no explicit app — should work
	engine, err := NewEngineBuilder().
		WithAllDefaults().
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if engine == nil {
		t.Fatal("expected non-nil engine")
	}
	if engine.App() == nil {
		t.Fatal("expected default app to be created")
	}
}

func TestEngineBuilder_CombineDefaultAndCustomHandlers(t *testing.T) {
	customHandler := &builderTestHandler{canHandleType: "custom"}

	engine, err := NewEngineBuilder().
		WithDefaultHandlers().
		WithHandler(customHandler).
		Build()
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	// Should have the default handlers plus the custom one
	foundCustom := false
	for _, h := range engine.workflowHandlers {
		if h.CanHandle("custom") {
			foundCustom = true
			break
		}
	}
	if !foundCustom {
		t.Error("custom handler not found among registered handlers")
	}

	// Should also have default handlers
	if !hasHandlerForType(engine, "http") {
		t.Error("expected default HTTP handler to be registered")
	}
}

// builderTestHandler is a simple test double for WorkflowHandler used by builder tests.
type builderTestHandler struct {
	canHandleType string
}

func (m *builderTestHandler) CanHandle(workflowType string) bool {
	return workflowType == m.canHandleType
}

func (m *builderTestHandler) ConfigureWorkflow(_ modular.Application, _ any) error {
	return nil
}

func (m *builderTestHandler) ExecuteWorkflow(_ context.Context, _ string, _ string, _ map[string]any) (map[string]any, error) {
	return nil, nil
}

func hasHandlerForType(engine *StdEngine, workflowType string) bool {
	for _, handler := range engine.workflowHandlers {
		if handler.CanHandle(workflowType) {
			return true
		}
	}
	return false
}
