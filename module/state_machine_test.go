package module

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

func newTestDefinition() *StateMachineDefinition {
	return &StateMachineDefinition{
		Name:         "order-workflow",
		Description:  "Order processing workflow",
		InitialState: "new",
		States: map[string]*State{
			"new":        {Name: "new"},
			"processing": {Name: "processing"},
			"shipped":    {Name: "shipped"},
			"delivered":  {Name: "delivered", IsFinal: true},
			"cancelled":  {Name: "cancelled", IsFinal: true, IsError: true},
		},
		Transitions: map[string]*Transition{
			"process": {Name: "process", FromState: "new", ToState: "processing"},
			"ship":    {Name: "ship", FromState: "processing", ToState: "shipped"},
			"deliver": {Name: "deliver", FromState: "shipped", ToState: "delivered"},
			"cancel":  {Name: "cancel", FromState: "new", ToState: "cancelled"},
		},
	}
}

func TestNewStateMachineEngine(t *testing.T) {
	engine := NewStateMachineEngine("test-engine")
	if engine.Name() != "test-engine" {
		t.Errorf("expected name 'test-engine', got '%s'", engine.Name())
	}
}

func TestNewStateMachineEngineWithNamespace(t *testing.T) {
	ns := NewStandardNamespace("app", "v1")
	engine := NewStateMachineEngineWithNamespace("engine", ns)
	expected := "app-engine-v1"
	if engine.Name() != expected {
		t.Errorf("expected name '%s', got '%s'", expected, engine.Name())
	}
}

func TestNewStandardStateMachineEngine(t *testing.T) {
	engine := NewStandardStateMachineEngine(nil)
	if engine.Name() != StateMachineEngineName {
		t.Errorf("expected name '%s', got '%s'", StateMachineEngineName, engine.Name())
	}
}

func TestStateMachineEngine_RegisterDefinition(t *testing.T) {
	tests := []struct {
		name    string
		def     *StateMachineDefinition
		wantErr bool
	}{
		{
			name:    "valid definition",
			def:     newTestDefinition(),
			wantErr: false,
		},
		{
			name: "empty name",
			def: &StateMachineDefinition{
				Name:         "",
				InitialState: "new",
				States:       map[string]*State{"new": {Name: "new"}},
			},
			wantErr: true,
		},
		{
			name: "no states",
			def: &StateMachineDefinition{
				Name:         "empty",
				InitialState: "new",
				States:       map[string]*State{},
			},
			wantErr: true,
		},
		{
			name: "invalid initial state",
			def: &StateMachineDefinition{
				Name:         "bad-initial",
				InitialState: "nonexistent",
				States:       map[string]*State{"new": {Name: "new"}},
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			engine := NewStateMachineEngine("test")
			err := engine.RegisterDefinition(tc.def)
			if (err != nil) != tc.wantErr {
				t.Errorf("RegisterDefinition() error = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

func TestStateMachineEngine_CreateWorkflow(t *testing.T) {
	engine := NewStateMachineEngine("test")
	if err := engine.RegisterDefinition(newTestDefinition()); err != nil {
		t.Fatalf("failed to register definition: %v", err)
	}

	instance, err := engine.CreateWorkflow("order-workflow", "order-1", map[string]any{
		"customer": "alice",
	})
	if err != nil {
		t.Fatalf("CreateWorkflow failed: %v", err)
	}

	if instance.ID != "order-1" {
		t.Errorf("expected ID 'order-1', got '%s'", instance.ID)
	}
	if instance.WorkflowType != "order-workflow" {
		t.Errorf("expected type 'order-workflow', got '%s'", instance.WorkflowType)
	}
	if instance.CurrentState != "new" {
		t.Errorf("expected state 'new', got '%s'", instance.CurrentState)
	}
	if instance.Data["customer"] != "alice" {
		t.Errorf("expected data customer 'alice', got '%v'", instance.Data["customer"])
	}
	if instance.Completed {
		t.Error("expected Completed=false for new instance")
	}
}

func TestStateMachineEngine_CreateWorkflow_UnknownType(t *testing.T) {
	engine := NewStateMachineEngine("test")

	_, err := engine.CreateWorkflow("nonexistent", "id-1", nil)
	if err == nil {
		t.Error("expected error for unknown workflow type")
	}
}

func TestStateMachineEngine_GetInstance(t *testing.T) {
	engine := NewStateMachineEngine("test")
	if err := engine.RegisterDefinition(newTestDefinition()); err != nil {
		t.Fatalf("failed to register: %v", err)
	}

	_, _ = engine.CreateWorkflow("order-workflow", "order-1", nil)

	instance, err := engine.GetInstance("order-1")
	if err != nil {
		t.Fatalf("GetInstance failed: %v", err)
	}
	if instance.ID != "order-1" {
		t.Errorf("expected ID 'order-1', got '%s'", instance.ID)
	}
}

func TestStateMachineEngine_GetInstance_NotFound(t *testing.T) {
	engine := NewStateMachineEngine("test")

	_, err := engine.GetInstance("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent instance")
	}
}

func TestStateMachineEngine_GetInstancesByType(t *testing.T) {
	engine := NewStateMachineEngine("test")
	if err := engine.RegisterDefinition(newTestDefinition()); err != nil {
		t.Fatalf("failed to register: %v", err)
	}

	_, _ = engine.CreateWorkflow("order-workflow", "order-1", nil)
	_, _ = engine.CreateWorkflow("order-workflow", "order-2", nil)

	instances, err := engine.GetInstancesByType("order-workflow")
	if err != nil {
		t.Fatalf("GetInstancesByType failed: %v", err)
	}
	if len(instances) != 2 {
		t.Errorf("expected 2 instances, got %d", len(instances))
	}
}

func TestStateMachineEngine_GetInstancesByType_NotFound(t *testing.T) {
	engine := NewStateMachineEngine("test")

	_, err := engine.GetInstancesByType("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent workflow type")
	}
}

func TestStateMachineEngine_GetAllInstances(t *testing.T) {
	engine := NewStateMachineEngine("test")
	if err := engine.RegisterDefinition(newTestDefinition()); err != nil {
		t.Fatalf("failed to register: %v", err)
	}

	_, _ = engine.CreateWorkflow("order-workflow", "order-1", nil)
	_, _ = engine.CreateWorkflow("order-workflow", "order-2", nil)

	instances, err := engine.GetAllInstances()
	if err != nil {
		t.Fatalf("GetAllInstances failed: %v", err)
	}
	if len(instances) != 2 {
		t.Errorf("expected 2 instances, got %d", len(instances))
	}
}

func TestStateMachineEngine_TriggerTransition_Valid(t *testing.T) {
	engine := NewStateMachineEngine("test")
	if err := engine.RegisterDefinition(newTestDefinition()); err != nil {
		t.Fatalf("failed to register: %v", err)
	}

	_, _ = engine.CreateWorkflow("order-workflow", "order-1", nil)

	err := engine.TriggerTransition(context.Background(), "order-1", "process", map[string]any{
		"processedBy": "worker-1",
	})
	if err != nil {
		t.Fatalf("TriggerTransition failed: %v", err)
	}

	instance, _ := engine.GetInstance("order-1")
	if instance.CurrentState != "processing" {
		t.Errorf("expected state 'processing', got '%s'", instance.CurrentState)
	}
	if instance.PreviousState != "new" {
		t.Errorf("expected previous state 'new', got '%s'", instance.PreviousState)
	}
	if instance.Data["processedBy"] != "worker-1" {
		t.Errorf("expected data processedBy 'worker-1', got '%v'", instance.Data["processedBy"])
	}
}

func TestStateMachineEngine_TriggerTransition_InvalidInstance(t *testing.T) {
	engine := NewStateMachineEngine("test")
	if err := engine.RegisterDefinition(newTestDefinition()); err != nil {
		t.Fatalf("failed to register: %v", err)
	}

	err := engine.TriggerTransition(context.Background(), "nonexistent", "process", nil)
	if err == nil {
		t.Error("expected error for nonexistent instance")
	}
}

func TestStateMachineEngine_TriggerTransition_InvalidTransition(t *testing.T) {
	engine := NewStateMachineEngine("test")
	if err := engine.RegisterDefinition(newTestDefinition()); err != nil {
		t.Fatalf("failed to register: %v", err)
	}

	_, _ = engine.CreateWorkflow("order-workflow", "order-1", nil)

	err := engine.TriggerTransition(context.Background(), "order-1", "nonexistent", nil)
	if err == nil {
		t.Error("expected error for nonexistent transition")
	}
}

func TestStateMachineEngine_TriggerTransition_WrongState(t *testing.T) {
	engine := NewStateMachineEngine("test")
	if err := engine.RegisterDefinition(newTestDefinition()); err != nil {
		t.Fatalf("failed to register: %v", err)
	}

	_, _ = engine.CreateWorkflow("order-workflow", "order-1", nil)

	// Try to ship from "new" state (should fail, needs "processing")
	err := engine.TriggerTransition(context.Background(), "order-1", "ship", nil)
	if err == nil {
		t.Error("expected error for wrong state transition")
	}
}

func TestStateMachineEngine_TriggerTransition_FinalState(t *testing.T) {
	engine := NewStateMachineEngine("test")
	if err := engine.RegisterDefinition(newTestDefinition()); err != nil {
		t.Fatalf("failed to register: %v", err)
	}

	_, _ = engine.CreateWorkflow("order-workflow", "order-1", nil)

	// Transition to a final state
	err := engine.TriggerTransition(context.Background(), "order-1", "cancel", nil)
	if err != nil {
		t.Fatalf("cancel transition failed: %v", err)
	}

	instance, _ := engine.GetInstance("order-1")
	if !instance.Completed {
		t.Error("expected Completed=true after final state")
	}
	if instance.Error == "" {
		t.Error("expected error message for error final state")
	}
}

func TestStateMachineEngine_TriggerTransition_DeliveredFinalState(t *testing.T) {
	engine := NewStateMachineEngine("test")
	if err := engine.RegisterDefinition(newTestDefinition()); err != nil {
		t.Fatalf("failed to register: %v", err)
	}

	_, _ = engine.CreateWorkflow("order-workflow", "order-1", nil)

	// Walk through full workflow: new -> processing -> shipped -> delivered
	_ = engine.TriggerTransition(context.Background(), "order-1", "process", nil)
	_ = engine.TriggerTransition(context.Background(), "order-1", "ship", nil)
	_ = engine.TriggerTransition(context.Background(), "order-1", "deliver", nil)

	instance, _ := engine.GetInstance("order-1")
	if !instance.Completed {
		t.Error("expected Completed=true after delivered")
	}
	if instance.CurrentState != "delivered" {
		t.Errorf("expected state 'delivered', got '%s'", instance.CurrentState)
	}
	if instance.Error != "" {
		t.Errorf("expected no error for successful delivery, got '%s'", instance.Error)
	}
}

func TestStateMachineEngine_TransitionHandler(t *testing.T) {
	engine := NewStateMachineEngine("test")
	if err := engine.RegisterDefinition(newTestDefinition()); err != nil {
		t.Fatalf("failed to register: %v", err)
	}

	var handlerCalled bool
	var capturedEvent TransitionEvent

	handler := NewFunctionTransitionHandler(func(ctx context.Context, event TransitionEvent) error {
		handlerCalled = true
		capturedEvent = event
		return nil
	})
	engine.SetTransitionHandler(handler)

	if !engine.HasTransitionHandler() {
		t.Error("expected HasTransitionHandler=true")
	}

	_, _ = engine.CreateWorkflow("order-workflow", "order-1", nil)
	_ = engine.TriggerTransition(context.Background(), "order-1", "process", nil)

	if !handlerCalled {
		t.Error("expected transition handler to be called")
	}
	if capturedEvent.FromState != "new" {
		t.Errorf("expected from state 'new', got '%s'", capturedEvent.FromState)
	}
	if capturedEvent.ToState != "processing" {
		t.Errorf("expected to state 'processing', got '%s'", capturedEvent.ToState)
	}
}

func TestStateMachineEngine_TransitionHandler_Error(t *testing.T) {
	engine := NewStateMachineEngine("test")
	if err := engine.RegisterDefinition(newTestDefinition()); err != nil {
		t.Fatalf("failed to register: %v", err)
	}

	handler := NewFunctionTransitionHandler(func(ctx context.Context, event TransitionEvent) error {
		return fmt.Errorf("handler error")
	})
	engine.SetTransitionHandler(handler)

	_, _ = engine.CreateWorkflow("order-workflow", "order-1", nil)
	err := engine.TriggerTransition(context.Background(), "order-1", "process", nil)

	if err == nil {
		t.Error("expected error from transition handler")
	}
}

func TestStateMachineEngine_AddTransitionListener(t *testing.T) {
	engine := NewStateMachineEngine("test")
	if err := engine.RegisterDefinition(newTestDefinition()); err != nil {
		t.Fatalf("failed to register: %v", err)
	}

	var listenerCalled bool
	engine.AddTransitionListener(func(event TransitionEvent) {
		listenerCalled = true
	})

	_, _ = engine.CreateWorkflow("order-workflow", "order-1", nil)
	_ = engine.TriggerTransition(context.Background(), "order-1", "process", nil)

	if !listenerCalled {
		t.Error("expected listener to be called")
	}
}

func TestStateMachineEngine_AddGlobalTransitionHandler_NoExisting(t *testing.T) {
	engine := NewStateMachineEngine("test")
	if err := engine.RegisterDefinition(newTestDefinition()); err != nil {
		t.Fatalf("failed to register: %v", err)
	}

	var called bool
	engine.AddGlobalTransitionHandler(NewFunctionTransitionHandler(func(ctx context.Context, event TransitionEvent) error {
		called = true
		return nil
	}))

	_, _ = engine.CreateWorkflow("order-workflow", "order-1", nil)
	_ = engine.TriggerTransition(context.Background(), "order-1", "process", nil)

	if !called {
		t.Error("expected global handler to be called")
	}
}

func TestStateMachineEngine_AddGlobalTransitionHandler_WithExisting(t *testing.T) {
	engine := NewStateMachineEngine("test")
	if err := engine.RegisterDefinition(newTestDefinition()); err != nil {
		t.Fatalf("failed to register: %v", err)
	}

	callOrder := []string{}

	engine.SetTransitionHandler(NewFunctionTransitionHandler(func(ctx context.Context, event TransitionEvent) error {
		callOrder = append(callOrder, "first")
		return nil
	}))

	engine.AddGlobalTransitionHandler(NewFunctionTransitionHandler(func(ctx context.Context, event TransitionEvent) error {
		callOrder = append(callOrder, "second")
		return nil
	}))

	_, _ = engine.CreateWorkflow("order-workflow", "order-1", nil)
	_ = engine.TriggerTransition(context.Background(), "order-1", "process", nil)

	if len(callOrder) != 2 {
		t.Fatalf("expected 2 handlers called, got %d", len(callOrder))
	}
	if callOrder[0] != "first" || callOrder[1] != "second" {
		t.Errorf("expected call order [first, second], got %v", callOrder)
	}
}

func TestStateMachineEngine_CompositeTransitionHandler(t *testing.T) {
	composite := NewCompositeTransitionHandler()

	var results []string
	composite.AddHandler(NewFunctionTransitionHandler(func(ctx context.Context, event TransitionEvent) error {
		results = append(results, "a")
		return nil
	}))
	composite.AddHandler(NewFunctionTransitionHandler(func(ctx context.Context, event TransitionEvent) error {
		results = append(results, "b")
		return nil
	}))
	composite.AddHandler(nil) // should be ignored

	err := composite.HandleTransition(context.Background(), TransitionEvent{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
}

func TestStateMachineEngine_CompositeTransitionHandler_PropagatesError(t *testing.T) {
	composite := NewCompositeTransitionHandler()

	composite.AddHandler(NewFunctionTransitionHandler(func(ctx context.Context, event TransitionEvent) error {
		return fmt.Errorf("stop here")
	}))
	composite.AddHandler(NewFunctionTransitionHandler(func(ctx context.Context, event TransitionEvent) error {
		t.Error("should not be called after error")
		return nil
	}))

	err := composite.HandleTransition(context.Background(), TransitionEvent{})
	if err == nil {
		t.Error("expected error from composite handler")
	}
}

func TestStateMachineEngine_ListenerAdapter(t *testing.T) {
	var called bool
	adapter := NewListenerAdapter(func(event TransitionEvent) {
		called = true
	})

	err := adapter.HandleTransition(context.Background(), TransitionEvent{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected listener to be called")
	}
}

func TestTransitionEvent_InstanceID(t *testing.T) {
	event := TransitionEvent{WorkflowID: "wf-123"}
	if event.InstanceID() != "wf-123" {
		t.Errorf("expected InstanceID 'wf-123', got '%s'", event.InstanceID())
	}
}

func TestStateMachineEngine_ProvidesServices(t *testing.T) {
	engine := NewStateMachineEngine("my-engine")
	services := engine.ProvidesServices()
	if len(services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(services))
	}
	if services[0].Name != "my-engine" {
		t.Errorf("expected service name 'my-engine', got '%s'", services[0].Name)
	}
}

func TestStateMachineEngine_RequiresServices(t *testing.T) {
	engine := NewStateMachineEngine("my-engine")
	deps := engine.RequiresServices()
	if deps != nil {
		t.Errorf("expected nil dependencies, got %v", deps)
	}
}

func TestStateMachineEngine_InitStartStop(t *testing.T) {
	engine := NewStateMachineEngine("test")
	app := CreateIsolatedApp(t)

	if err := engine.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if err := engine.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if err := engine.Stop(context.Background()); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestStateMachineEngine_RegisterWorkflow(t *testing.T) {
	engine := NewStateMachineEngine("test")

	extDef := ExternalStateMachineDefinition{
		ID:           "ext-workflow",
		Description:  "External workflow",
		InitialState: "start",
		States: map[string]StateMachineStateConfig{
			"start": {ID: "start", Description: "Initial state"},
			"end":   {ID: "end", IsFinal: true},
		},
		Transitions: map[string]StateMachineTransitionConfig{
			"finish": {ID: "finish", FromState: "start", ToState: "end"},
		},
	}

	err := engine.RegisterWorkflow(extDef)
	if err != nil {
		t.Fatalf("RegisterWorkflow failed: %v", err)
	}

	// Verify we can create instances of the registered workflow
	instance, err := engine.CreateWorkflow("ext-workflow", "inst-1", nil)
	if err != nil {
		t.Fatalf("CreateWorkflow failed: %v", err)
	}
	if instance.CurrentState != "start" {
		t.Errorf("expected initial state 'start', got '%s'", instance.CurrentState)
	}
}

func TestStateMachineEngine_ConcurrentOperations(t *testing.T) {
	engine := NewStateMachineEngine("test")
	if err := engine.RegisterDefinition(newTestDefinition()); err != nil {
		t.Fatalf("failed to register: %v", err)
	}

	var wg sync.WaitGroup

	// Create multiple instances concurrently
	for i := range 20 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := fmt.Sprintf("order-%d", n)
			_, err := engine.CreateWorkflow("order-workflow", id, nil)
			if err != nil {
				t.Errorf("concurrent CreateWorkflow failed for %s: %v", id, err)
			}
		}(i)
	}
	wg.Wait()

	instances, _ := engine.GetAllInstances()
	if len(instances) != 20 {
		t.Errorf("expected 20 instances, got %d", len(instances))
	}
}

func TestStateMachineEngine_GetTransitionHandler(t *testing.T) {
	engine := NewStateMachineEngine("test")
	if engine.GetTransitionHandler() != nil {
		t.Error("expected nil handler initially")
	}

	handler := NewFunctionTransitionHandler(func(ctx context.Context, event TransitionEvent) error {
		return nil
	})
	engine.SetTransitionHandler(handler)

	if engine.GetTransitionHandler() == nil {
		t.Error("expected non-nil handler after set")
	}
}

func TestTriggerTransition_HandlerFailure_StateUnchanged(t *testing.T) {
	engine := NewStateMachineEngine("test")
	if err := engine.RegisterDefinition(newTestDefinition()); err != nil {
		t.Fatalf("failed to register: %v", err)
	}

	// Set a handler that always fails
	engine.SetTransitionHandler(NewFunctionTransitionHandler(func(ctx context.Context, event TransitionEvent) error {
		return fmt.Errorf("handler deliberately failed")
	}))

	_, err := engine.CreateWorkflow("order-workflow", "order-1", nil)
	if err != nil {
		t.Fatalf("CreateWorkflow failed: %v", err)
	}

	// Attempt a transition — should fail because the handler returns an error
	err = engine.TriggerTransition(context.Background(), "order-1", "process", map[string]any{
		"note": "should not persist",
	})
	if err == nil {
		t.Fatal("expected error from failing handler, got nil")
	}

	// Verify state was NOT changed
	instance, _ := engine.GetInstance("order-1")
	if instance.CurrentState != "new" {
		t.Errorf("expected state to remain 'new' after handler failure, got '%s'", instance.CurrentState)
	}
	if instance.PreviousState != "" {
		t.Errorf("expected previous state to remain empty after handler failure, got '%s'", instance.PreviousState)
	}
	// Data should also not be merged on failure
	if _, exists := instance.Data["note"]; exists {
		t.Error("expected data not to be merged after handler failure")
	}
}

func TestStateMachineEngine_GetOrphanedInstances_NoOrphans(t *testing.T) {
	engine := NewStateMachineEngine("test")
	if err := engine.RegisterDefinition(newTestDefinition()); err != nil {
		t.Fatalf("failed to register: %v", err)
	}

	_, _ = engine.CreateWorkflow("order-workflow", "order-1", nil)
	_, _ = engine.CreateWorkflow("order-workflow", "order-2", nil)

	orphaned := engine.GetOrphanedInstances()
	if len(orphaned) != 0 {
		t.Errorf("expected 0 orphaned instances, got %d", len(orphaned))
	}
}

func TestStateMachineEngine_GetOrphanedInstances_WithOrphans(t *testing.T) {
	engine := NewStateMachineEngine("test")
	if err := engine.RegisterDefinition(newTestDefinition()); err != nil {
		t.Fatalf("failed to register: %v", err)
	}

	_, _ = engine.CreateWorkflow("order-workflow", "order-1", nil)
	_, _ = engine.CreateWorkflow("order-workflow", "order-2", nil)

	// Simulate configuration drift: mutate an instance to a state that
	// no longer exists in the definition.
	engine.mutex.Lock()
	engine.instances["order-2"].CurrentState = "removed_state"
	engine.mutex.Unlock()

	orphaned := engine.GetOrphanedInstances()
	if len(orphaned) != 1 {
		t.Fatalf("expected 1 orphaned instance, got %d", len(orphaned))
	}
	if orphaned[0].ID != "order-2" {
		t.Errorf("expected orphaned instance 'order-2', got '%s'", orphaned[0].ID)
	}
}

func TestStateMachineEngine_GetOrphanedInstances_NoDefinition(t *testing.T) {
	engine := NewStateMachineEngine("test")
	if err := engine.RegisterDefinition(newTestDefinition()); err != nil {
		t.Fatalf("failed to register: %v", err)
	}

	_, _ = engine.CreateWorkflow("order-workflow", "order-1", nil)

	// Remove the definition to simulate a type that was unregistered
	engine.mutex.Lock()
	delete(engine.definitions, "order-workflow")
	engine.mutex.Unlock()

	orphaned := engine.GetOrphanedInstances()
	if len(orphaned) != 1 {
		t.Fatalf("expected 1 orphaned instance, got %d", len(orphaned))
	}
	if orphaned[0].ID != "order-1" {
		t.Errorf("expected orphaned instance 'order-1', got '%s'", orphaned[0].ID)
	}
}

func TestStateMachineEngine_GetOrphanedInstances_Empty(t *testing.T) {
	engine := NewStateMachineEngine("test")
	orphaned := engine.GetOrphanedInstances()
	if len(orphaned) != 0 {
		t.Errorf("expected 0 orphaned instances, got %d", len(orphaned))
	}
}

func TestStateMachineEngine_AddTransitionListener_WithExistingNonComposite(t *testing.T) {
	engine := NewStateMachineEngine("test")
	if err := engine.RegisterDefinition(newTestDefinition()); err != nil {
		t.Fatalf("failed to register: %v", err)
	}

	callOrder := []string{}

	// Set a non-composite handler first
	engine.SetTransitionHandler(NewFunctionTransitionHandler(func(ctx context.Context, event TransitionEvent) error {
		callOrder = append(callOrder, "handler")
		return nil
	}))

	// Add a listener - this should wrap the existing handler in a composite
	engine.AddTransitionListener(func(event TransitionEvent) {
		callOrder = append(callOrder, "listener")
	})

	_, _ = engine.CreateWorkflow("order-workflow", "order-1", nil)
	_ = engine.TriggerTransition(context.Background(), "order-1", "process", nil)

	if len(callOrder) != 2 {
		t.Errorf("expected 2 calls, got %d: %v", len(callOrder), callOrder)
	}
}
