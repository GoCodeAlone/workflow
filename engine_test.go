package workflow

import (
	"context"
	"testing"
	"time"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/handlers"
	"github.com/GoCodeAlone/workflow/mock"
)

// setupEngineTest creates an isolated test environment for engine tests
func setupEngineTest(t *testing.T) (*Engine, modular.Application, context.Context, context.CancelFunc) {
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
	engine := NewEngine(app, mockLogger)

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
