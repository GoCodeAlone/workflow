package module

import (
	"context"
	"testing"
)

// --- Factory validation tests ---

func TestStateMachineGetStep_MissingStatemachine(t *testing.T) {
	factory := NewStateMachineGetStepFactory()
	_, err := factory("get-state", map[string]any{
		"entity_id": "order-1",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing statemachine")
	}
}

func TestStateMachineGetStep_MissingEntityID(t *testing.T) {
	factory := NewStateMachineGetStepFactory()
	_, err := factory("get-state", map[string]any{
		"statemachine": "order-sm",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing entity_id")
	}
}

// --- Execution tests ---

func TestStateMachineGetStep_ReturnsCurrentState(t *testing.T) {
	engine := setupOrderStateMachine(t, "order-1", "")
	app := newAppWithSM(engine)

	factory := NewStateMachineGetStepFactory()
	step, err := factory("get-order-state", map[string]any{
		"statemachine": "order-sm",
		"entity_id":    "order-1",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	state, _ := result.Output["current_state"].(string)
	if state != "pending" {
		t.Errorf("expected current_state='pending', got %q", state)
	}
	entityID, _ := result.Output["entity_id"].(string)
	if entityID != "order-1" {
		t.Errorf("expected entity_id='order-1', got %q", entityID)
	}
}

func TestStateMachineGetStep_TemplatedEntityID(t *testing.T) {
	engine := setupOrderStateMachine(t, "order-99", "")
	app := newAppWithSM(engine)

	factory := NewStateMachineGetStepFactory()
	step, err := factory("get-state-template", map[string]any{
		"statemachine": "order-sm",
		"entity_id":    "{{.order_id}}",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{"order_id": "order-99"}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	state, _ := result.Output["current_state"].(string)
	if state != "pending" {
		t.Errorf("expected current_state='pending', got %q", state)
	}
	entityID, _ := result.Output["entity_id"].(string)
	if entityID != "order-99" {
		t.Errorf("expected entity_id='order-99', got %q", entityID)
	}
}

func TestStateMachineGetStep_ReturnsStateAfterTransition(t *testing.T) {
	engine := setupOrderStateMachine(t, "order-1", "")
	app := newAppWithSM(engine)

	// Trigger a transition first
	if err := engine.TriggerTransition(context.Background(), "order-1", "approve", nil); err != nil {
		t.Fatalf("trigger transition: %v", err)
	}

	factory := NewStateMachineGetStepFactory()
	step, err := factory("get-approved-state", map[string]any{
		"statemachine": "order-sm",
		"entity_id":    "order-1",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	state, _ := result.Output["current_state"].(string)
	if state != "approved" {
		t.Errorf("expected current_state='approved', got %q", state)
	}
}

func TestStateMachineGetStep_InstanceNotFound(t *testing.T) {
	engine := setupOrderStateMachine(t, "", "") // no instances created
	app := newAppWithSM(engine)

	factory := NewStateMachineGetStepFactory()
	step, err := factory("get-missing", map[string]any{
		"statemachine": "order-sm",
		"entity_id":    "nonexistent",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error for nonexistent instance")
	}
}

func TestStateMachineGetStep_ServiceNotFound(t *testing.T) {
	app := NewMockApplication()

	factory := NewStateMachineGetStepFactory()
	step, err := factory("get-state", map[string]any{
		"statemachine": "nonexistent-sm",
		"entity_id":    "order-1",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error for missing service")
	}
}

func TestStateMachineGetStep_ServiceWrongType(t *testing.T) {
	app := NewMockApplication()
	app.Services["order-sm"] = "not-an-engine"

	factory := NewStateMachineGetStepFactory()
	step, err := factory("get-state", map[string]any{
		"statemachine": "order-sm",
		"entity_id":    "order-1",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error for wrong service type")
	}
}

func TestStateMachineGetStep_NoAppContext(t *testing.T) {
	factory := NewStateMachineGetStepFactory()
	step, err := factory("get-state", map[string]any{
		"statemachine": "order-sm",
		"entity_id":    "order-1",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error for nil app")
	}
}
