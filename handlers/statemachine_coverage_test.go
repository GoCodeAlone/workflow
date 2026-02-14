package handlers

import (
	"context"
	"strings"
	"testing"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/module"
)

func TestStateMachineWorkflowHandler_Name(t *testing.T) {
	h := NewStateMachineWorkflowHandler()
	name := h.Name()
	if name == "" {
		t.Error("expected non-empty name")
	}
	if !strings.Contains(name, "statemachine") {
		t.Errorf("expected name to contain 'statemachine', got '%s'", name)
	}
}

func TestStateMachineExecuteWorkflow_NoAppContext(t *testing.T) {
	h := NewStateMachineWorkflowHandler()
	ctx := context.Background()

	_, err := h.ExecuteWorkflow(ctx, "statemachine", "transition", map[string]any{
		"instanceId": "test-123",
	})
	if err == nil || !strings.Contains(err.Error(), "application context not available") {
		t.Fatalf("expected app context error, got: %v", err)
	}
}

func TestStateMachineExecuteWorkflow_MissingInstanceID(t *testing.T) {
	h := NewStateMachineWorkflowHandler()
	app := newMockApp()
	ctx := context.WithValue(context.Background(), applicationContextKey, modular.Application(app))

	_, err := h.ExecuteWorkflow(ctx, "statemachine", "transition", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "workflow instance ID not provided") {
		t.Fatalf("expected instance ID error, got: %v", err)
	}
}

func TestStateMachineExecuteWorkflow_InstanceIDFromIdField(t *testing.T) {
	h := NewStateMachineWorkflowHandler()
	app := newMockApp()

	engine := module.NewStateMachineEngine("test-engine")
	app.services["test-engine"] = engine

	ctx := context.WithValue(context.Background(), applicationContextKey, modular.Application(app))

	// Use "id" instead of "instanceId" - should still be found
	// Will fail because engine has no such instance, but at least the ID extraction works
	_, err := h.ExecuteWorkflow(ctx, "statemachine", "test-engine:do-transition", map[string]any{
		"id": "some-instance",
	})
	// Should get past ID parsing and fail on transition
	if err == nil {
		t.Fatal("expected error (instance not found)")
	}
	if strings.Contains(err.Error(), "workflow instance ID not provided") {
		t.Fatalf("ID should have been found from 'id' field, got: %v", err)
	}
}

func TestStateMachineExecuteWorkflow_NoEngineFound(t *testing.T) {
	h := NewStateMachineWorkflowHandler()
	app := newMockApp()

	ctx := context.WithValue(context.Background(), applicationContextKey, modular.Application(app))

	_, err := h.ExecuteWorkflow(ctx, "statemachine", "do-transition", map[string]any{
		"instanceId": "test-123",
	})
	if err == nil || !strings.Contains(err.Error(), "no state machine engine found") {
		t.Fatalf("expected no engine found error, got: %v", err)
	}
}

func TestStateMachineExecuteWorkflow_EngineFoundInRegistry(t *testing.T) {
	h := NewStateMachineWorkflowHandler()
	app := newMockApp()

	engine := module.NewStateMachineEngine("sm-engine")
	// Put engine in SvcRegistry so it can be found by scanning
	app.services["sm-engine"] = engine

	ctx := context.WithValue(context.Background(), applicationContextKey, modular.Application(app))

	// No colon in action so it scans for engine in registry
	_, err := h.ExecuteWorkflow(ctx, "statemachine", "transition", map[string]any{
		"instanceId": "test-123",
	})
	// Should find the engine but fail on instance not found
	if err == nil {
		t.Fatal("expected error (instance not found)")
	}
	if strings.Contains(err.Error(), "no state machine engine found") {
		t.Fatalf("engine should have been found by scanning, got: %v", err)
	}
}

func TestStateMachineExecuteWorkflow_NamedEngineNotFound(t *testing.T) {
	h := NewStateMachineWorkflowHandler()
	app := newMockApp()

	ctx := context.WithValue(context.Background(), applicationContextKey, modular.Application(app))

	_, err := h.ExecuteWorkflow(ctx, "statemachine", "missing-engine:transition", map[string]any{
		"instanceId": "test-123",
	})
	if err == nil {
		t.Fatal("expected error for missing engine")
	}
	// When GetService returns nil for engineSvc, it falls through to type assertion
	// which fails with "is not a StateMachineEngine"
}

func TestStateMachineExecuteWorkflow_ServiceNotEngine(t *testing.T) {
	h := NewStateMachineWorkflowHandler()
	app := newMockApp()
	app.services["not-engine"] = "just-a-string"

	ctx := context.WithValue(context.Background(), applicationContextKey, modular.Application(app))

	_, err := h.ExecuteWorkflow(ctx, "statemachine", "not-engine:transition", map[string]any{
		"instanceId": "test-123",
	})
	if err == nil || !strings.Contains(err.Error(), "is not a StateMachineEngine") {
		t.Fatalf("expected not engine error, got: %v", err)
	}
}

func TestStateMachineExecuteWorkflow_SuccessfulTransition(t *testing.T) {
	h := NewStateMachineWorkflowHandler()
	app := newMockApp()

	engine := module.NewStateMachineEngine("sm-engine")

	// Register a workflow definition with states and transitions
	_ = engine.RegisterDefinition(&module.StateMachineDefinition{
		Name:         "test-workflow",
		InitialState: "new",
		States: map[string]*module.State{
			"new":        {Name: "new"},
			"processing": {Name: "processing"},
		},
		Transitions: map[string]*module.Transition{
			"start": {
				Name:      "start",
				FromState: "new",
				ToState:   "processing",
			},
		},
	})

	// Create a workflow instance
	instance, err := engine.CreateWorkflow("test-workflow", "test-instance", map[string]any{})
	if err != nil {
		t.Fatalf("failed to create workflow: %v", err)
	}

	app.services["sm-engine"] = engine

	ctx := context.WithValue(context.Background(), applicationContextKey, modular.Application(app))

	result, err := h.ExecuteWorkflow(ctx, "statemachine", "sm-engine:start", map[string]any{
		"instanceId": instance.ID,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["success"] != true {
		t.Errorf("expected success=true, got %v", result["success"])
	}
	if result["currentState"] != "processing" {
		t.Errorf("expected currentState='processing', got '%v'", result["currentState"])
	}
}
