package module

import (
	"context"
	"errors"
	"testing"
)

// mockTransitionTrigger implements TransitionTrigger for testing without a real engine.
type mockTransitionTrigger struct {
	triggerErr   error
	capturedID   string
	capturedEvt  string
	capturedData map[string]any
}

func (m *mockTransitionTrigger) TriggerTransition(_ context.Context, workflowID, transitionName string, data map[string]any) error {
	m.capturedID = workflowID
	m.capturedEvt = transitionName
	m.capturedData = data
	return m.triggerErr
}

// setupOrderStateMachine creates a StateMachineEngine with a simple order workflow.
func setupOrderStateMachine(t *testing.T, instanceID, initialState string) *StateMachineEngine {
	t.Helper()

	engine := NewStateMachineEngine("order-sm")
	def := &StateMachineDefinition{
		Name:         "order",
		InitialState: "pending",
		States: map[string]*State{
			"pending":  {Name: "pending"},
			"approved": {Name: "approved"},
			"rejected": {Name: "rejected", IsFinal: true},
		},
		Transitions: map[string]*Transition{
			"approve": {Name: "approve", FromState: "pending", ToState: "approved"},
			"reject":  {Name: "reject", FromState: "pending", ToState: "rejected"},
		},
	}
	if err := engine.RegisterDefinition(def); err != nil {
		t.Fatalf("register definition: %v", err)
	}

	if instanceID != "" {
		instance, err := engine.CreateWorkflow("order", instanceID, nil)
		if err != nil {
			t.Fatalf("create workflow: %v", err)
		}
		// If caller wants a non-initial state, force it directly for test setup
		if initialState != "" && initialState != def.InitialState {
			instance.CurrentState = initialState
		}
	}

	return engine
}

// newAppWithSM registers the engine under "order-sm" in a MockApplication.
func newAppWithSM(engine *StateMachineEngine) *MockApplication {
	app := NewMockApplication()
	app.Services["order-sm"] = engine
	return app
}

// --- Factory validation tests ---

func TestStateMachineTransitionStep_MissingStatemachine(t *testing.T) {
	factory := NewStateMachineTransitionStepFactory()
	_, err := factory("step1", map[string]any{
		"entity_id": "order-1",
		"event":     "approve",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing statemachine")
	}
}

func TestStateMachineTransitionStep_MissingEntityID(t *testing.T) {
	factory := NewStateMachineTransitionStepFactory()
	_, err := factory("step1", map[string]any{
		"statemachine": "order-sm",
		"event":        "approve",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing entity_id")
	}
}

func TestStateMachineTransitionStep_MissingEvent(t *testing.T) {
	factory := NewStateMachineTransitionStepFactory()
	_, err := factory("step1", map[string]any{
		"statemachine": "order-sm",
		"entity_id":    "order-1",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing event")
	}
}

// --- Execution tests: using real StateMachineEngine ---

func TestStateMachineTransitionStep_SuccessfulTransition(t *testing.T) {
	engine := setupOrderStateMachine(t, "order-1", "")
	app := newAppWithSM(engine)

	factory := NewStateMachineTransitionStepFactory()
	step, err := factory("approve-order", map[string]any{
		"statemachine": "order-sm",
		"entity_id":    "order-1",
		"event":        "approve",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	ok, _ := result.Output["transition_ok"].(bool)
	if !ok {
		t.Error("expected transition_ok=true")
	}
	newState, _ := result.Output["new_state"].(string)
	if newState != "approved" {
		t.Errorf("expected new_state='approved', got %q", newState)
	}
}

func TestStateMachineTransitionStep_TemplatedEntityIDAndEvent(t *testing.T) {
	engine := setupOrderStateMachine(t, "order-42", "")
	app := newAppWithSM(engine)

	factory := NewStateMachineTransitionStepFactory()
	step, err := factory("dynamic-approve", map[string]any{
		"statemachine": "order-sm",
		"entity_id":    "{{.order_id}}",
		"event":        "{{.action}}",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"order_id": "order-42",
		"action":   "approve",
	}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	ok, _ := result.Output["transition_ok"].(bool)
	if !ok {
		t.Error("expected transition_ok=true")
	}
}

func TestStateMachineTransitionStep_InvalidTransition_FailOnErrorFalse(t *testing.T) {
	engine := setupOrderStateMachine(t, "order-1", "")
	app := newAppWithSM(engine)

	factory := NewStateMachineTransitionStepFactory()
	// "reject" is valid, but "approve" from "approved" is not — trigger approve twice
	step, err := factory("double-approve", map[string]any{
		"statemachine": "order-sm",
		"entity_id":    "order-1",
		"event":        "approve",
		"fail_on_error": false,
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	// First transition succeeds
	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("first execute error: %v", err)
	}

	// Second transition: "approve" from "approved" is invalid
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("expected no pipeline error with fail_on_error=false, got: %v", err)
	}

	ok, _ := result.Output["transition_ok"].(bool)
	if ok {
		t.Error("expected transition_ok=false for invalid transition")
	}
	errMsg, _ := result.Output["error"].(string)
	if errMsg == "" {
		t.Error("expected error message in output")
	}
}

func TestStateMachineTransitionStep_InvalidTransition_FailOnErrorTrue(t *testing.T) {
	engine := setupOrderStateMachine(t, "order-1", "")
	app := newAppWithSM(engine)

	factory := NewStateMachineTransitionStepFactory()
	step, err := factory("strict-approve", map[string]any{
		"statemachine":  "order-sm",
		"entity_id":     "order-1",
		"event":         "approve",
		"fail_on_error": true,
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	// First transition succeeds
	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("first execute error: %v", err)
	}

	// Second transition should fail with pipeline error
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected pipeline error with fail_on_error=true")
	}
}

func TestStateMachineTransitionStep_WithData(t *testing.T) {
	engine := setupOrderStateMachine(t, "order-1", "")
	app := newAppWithSM(engine)

	factory := NewStateMachineTransitionStepFactory()
	step, err := factory("approve-with-data", map[string]any{
		"statemachine": "order-sm",
		"entity_id":    "order-1",
		"event":        "approve",
		"data": map[string]any{
			"approved_by": "{{.user_id}}",
		},
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{"user_id": "u-99"}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	ok, _ := result.Output["transition_ok"].(bool)
	if !ok {
		t.Error("expected transition_ok=true")
	}

	// Verify data was merged into instance
	instance, err := engine.GetInstance("order-1")
	if err != nil {
		t.Fatalf("get instance: %v", err)
	}
	approvedBy, _ := instance.Data["approved_by"].(string)
	if approvedBy != "u-99" {
		t.Errorf("expected approved_by='u-99', got %q", approvedBy)
	}
}

func TestStateMachineTransitionStep_ServiceNotFound(t *testing.T) {
	app := NewMockApplication()

	factory := NewStateMachineTransitionStepFactory()
	step, err := factory("step1", map[string]any{
		"statemachine": "nonexistent-sm",
		"entity_id":    "order-1",
		"event":        "approve",
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

func TestStateMachineTransitionStep_ServiceWrongType(t *testing.T) {
	app := NewMockApplication()
	// Register something that is neither *StateMachineEngine nor TransitionTrigger
	app.Services["order-sm"] = "not-an-engine"

	factory := NewStateMachineTransitionStepFactory()
	step, err := factory("step1", map[string]any{
		"statemachine": "order-sm",
		"entity_id":    "order-1",
		"event":        "approve",
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

func TestStateMachineTransitionStep_NoAppContext(t *testing.T) {
	factory := NewStateMachineTransitionStepFactory()
	step, err := factory("step1", map[string]any{
		"statemachine": "order-sm",
		"entity_id":    "order-1",
		"event":        "approve",
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

// --- Execution tests: using mock TransitionTrigger ---

func TestStateMachineTransitionStep_MockTrigger_Success(t *testing.T) {
	mock := &mockTransitionTrigger{}
	app := NewMockApplication()
	app.Services["order-sm"] = mock

	factory := NewStateMachineTransitionStepFactory()
	step, err := factory("mock-approve", map[string]any{
		"statemachine": "order-sm",
		"entity_id":    "order-1",
		"event":        "approve",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if mock.capturedID != "order-1" {
		t.Errorf("expected capturedID='order-1', got %q", mock.capturedID)
	}
	if mock.capturedEvt != "approve" {
		t.Errorf("expected capturedEvt='approve', got %q", mock.capturedEvt)
	}

	ok, _ := result.Output["transition_ok"].(bool)
	if !ok {
		t.Error("expected transition_ok=true")
	}
}

func TestStateMachineTransitionStep_MockTrigger_Error_NoFail(t *testing.T) {
	mock := &mockTransitionTrigger{triggerErr: errors.New("invalid transition")}
	app := NewMockApplication()
	app.Services["order-sm"] = mock

	factory := NewStateMachineTransitionStepFactory()
	step, err := factory("mock-fail", map[string]any{
		"statemachine": "order-sm",
		"entity_id":    "order-1",
		"event":        "approve",
		"fail_on_error": false,
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("expected no pipeline error, got: %v", err)
	}

	ok, _ := result.Output["transition_ok"].(bool)
	if ok {
		t.Error("expected transition_ok=false")
	}
	errMsg, _ := result.Output["error"].(string)
	if errMsg == "" {
		t.Error("expected error in output")
	}
}

func TestStateMachineTransitionStep_MockTrigger_Error_Fail(t *testing.T) {
	mock := &mockTransitionTrigger{triggerErr: errors.New("invalid transition")}
	app := NewMockApplication()
	app.Services["order-sm"] = mock

	factory := NewStateMachineTransitionStepFactory()
	step, err := factory("mock-strict-fail", map[string]any{
		"statemachine":  "order-sm",
		"entity_id":     "order-1",
		"event":         "approve",
		"fail_on_error": true,
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected pipeline error with fail_on_error=true")
	}
}
