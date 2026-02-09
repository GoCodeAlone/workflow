package handlers

import (
	"context"
	"log"
	"log/slog"
	"testing"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/mock"
	"github.com/GoCodeAlone/workflow/module"
)

// TestStateMachineWorkflow tests the state machine workflow handler
func TestStateMachineWorkflow(t *testing.T) {
	// Run two separate tests to avoid conflicts
	t.Run("ExplicitRegistration", func(t *testing.T) {
		// Create a unique module name for this test
		moduleBaseName := "test-order-processor"
		uniqueModuleName := moduleBaseName + "-" + time.Now().Format("20060102150405.000000000")
		testExplicitRegistration(t, uniqueModuleName)
	})

	t.Run("ConfigRegistration", func(t *testing.T) {
		// Create a completely different unique name for the config test
		moduleBaseName := "cfg-processor"
		uniqueModuleName := moduleBaseName + "-" + time.Now().Format("20060102150405.000000000")

		// Create a unique state tracker name to avoid conflicts with the same timestamp
		stateTrackerName := "tracker-" + time.Now().Format("20060102150405.000000000")

		testConfigRegistration(t, uniqueModuleName, stateTrackerName)
	})
}

// testExplicitRegistration tests state machine workflow with explicit registration
func testExplicitRegistration(t *testing.T, engineName string) {
	t.Logf("Running explicit registration test with engine name: %s", engineName)

	// Create a simple application with proper config provider and logger
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), slog.New(slog.NewTextHandler(log.Writer(), &slog.HandlerOptions{
		AddSource: true,
	})))

	// Initialize the app
	err := app.Init()
	if err != nil {
		t.Fatalf("Failed to initialize app: %v", err)
	}

	// Create namespace provider for testing
	namespace := module.NewStandardNamespace("test", "")

	// Use a channel with timeout for safer synchronization than a WaitGroup
	handlerCalled := make(chan struct{})

	// Create transition handlers
	validationHandlerCalled := false
	validationHandler := module.NewFunctionTransitionHandler(func(ctx context.Context, event module.TransitionEvent) error {
		t.Logf("Validation handler called with event: %+v", event)
		validationHandlerCalled = true
		if event.TransitionID != "submit_order" {
			t.Errorf("Expected transition ID 'submit_order', got '%s'", event.TransitionID)
		}
		if event.FromState != "new" || event.ToState != "validating" {
			t.Errorf("Expected transition from 'new' to 'validating', got '%s' to '%s'",
				event.FromState, event.ToState)
		}
		// Signal that the handler was called
		close(handlerCalled)
		return nil
	})

	// Initialize the state machine explicitly
	stateMachine := module.NewStateMachineEngineWithNamespace(engineName, namespace)

	// Register the state machine with the application
	app.RegisterModule(stateMachine)

	// Initialize the app again after registering the module
	err = app.Init()
	if err != nil {
		t.Fatalf("Failed to initialize app with state machine: %v", err)
	}

	// Set a transition handler directly with the state machine
	stateMachine.SetTransitionHandler(validationHandler)

	// Register a state machine definition manually
	definition := &module.StateMachineDefinition{
		Name:         "test-workflow",
		Description:  "Test order workflow",
		InitialState: "new",
		States: map[string]*module.State{
			"new": {
				Name:        "new",
				Description: "New order state",
				IsFinal:     false,
			},
			"validating": {
				Name:        "validating",
				Description: "Validating order",
				IsFinal:     false,
			},
			"validated": {
				Name:        "validated",
				Description: "Order validated",
				IsFinal:     true,
			},
		},
		Transitions: map[string]*module.Transition{
			"submit_order": {
				Name:      "submit_order",
				FromState: "new",
				ToState:   "validating",
			},
			"validate_order": {
				Name:      "validate_order",
				FromState: "validating",
				ToState:   "validated",
			},
		},
	}

	err = stateMachine.RegisterDefinition(definition)
	if err != nil {
		t.Fatalf("Failed to register state machine definition: %v", err)
	}

	ctx := context.Background()

	// Create a workflow instance
	instance, err := stateMachine.CreateWorkflow("test-workflow", "order-123", map[string]interface{}{
		"customer": "test-customer",
		"amount":   99.99,
	})
	if err != nil {
		t.Fatalf("Failed to create workflow instance: %v", err)
	}

	// Verify initial state
	if instance.CurrentState != "new" {
		t.Errorf("Expected initial state 'new', got '%s'", instance.CurrentState)
	}

	// Trigger transition
	err = stateMachine.TriggerTransition(ctx, "order-123", "submit_order", map[string]interface{}{
		"validated": true,
	})
	if err != nil {
		t.Fatalf("Failed to trigger transition: %v", err)
	}

	// Wait for handler to be called with a timeout
	select {
	case <-handlerCalled:
		t.Log("Validation handler was called successfully")
	case <-time.After(3 * time.Second):
		t.Error("Timed out waiting for validation handler to be called")
	}

	// Verify handler was called
	if !validationHandlerCalled {
		t.Errorf("Expected validation handler to be called")
	}

	// Check if the state changed
	updatedInstance, err := stateMachine.GetInstance("order-123")
	if err != nil {
		t.Fatalf("Failed to get workflow instance: %v", err)
	}

	if updatedInstance.CurrentState != "validating" {
		t.Errorf("Expected state to change to 'validating', but got '%s'", updatedInstance.CurrentState)
	}
}

// testConfigRegistration tests state machine workflow with configuration-based registration
func testConfigRegistration(t *testing.T, engineName string, stateTrackerName string) {
	t.Logf("Running config registration test with engine name: %s", engineName)

	// For config-based test, create a completely separate and clean application
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), slog.New(slog.NewTextHandler(log.Writer(), &slog.HandlerOptions{
		AddSource: true,
	})))

	// Create validation handler with channel for synchronization
	handlerCalled := make(chan struct{})
	validationHandlerCalled := false
	validationHandler := module.NewFunctionTransitionHandler(func(ctx context.Context, event module.TransitionEvent) error {
		t.Logf("Config test: Validation handler called with event: %+v", event)
		validationHandlerCalled = true
		if event.TransitionID != "submit_order" {
			t.Errorf("Expected transition ID 'submit_order', got '%s'", event.TransitionID)
		}
		if event.FromState != "new" || event.ToState != "validating" {
			t.Errorf("Expected transition from 'new' to 'validating', got '%s' to '%s'",
				event.FromState, event.ToState)
		}
		close(handlerCalled)
		return nil
	})

	// Create unique handler name and register before config setup
	validationHandlerName := "test-validation-handler-" + time.Now().Format("20060102150405.000000000")
	if err := app.RegisterService(validationHandlerName, validationHandler); err != nil {
		t.Fatalf("Failed to register validation handler: %v", err)
	}

	// Create a clean workflow engine for this test
	engine := workflow.NewStdEngine(app, &mock.Logger{LogEntries: make([]string, 0)})

	// Register workflow handlers - this should be done BEFORE any configuration
	engine.RegisterWorkflowHandler(NewStateMachineWorkflowHandler())

	// Create a unique name for the engine to avoid collisions
	stateMachineEngineName := engineName + "-engine"

	// Create a minimal state machine workflow configuration
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			// First add a state tracker module to the config
			{
				Name: stateTrackerName,
				Type: "workflow.statetracker",
				Config: map[string]interface{}{
					"description": "State tracker for tests",
				},
			},
			// Add state machine engine module
			{
				Name: stateMachineEngineName,
				Type: "statemachine.engine",
				Config: map[string]interface{}{
					"description": "Order processing state machine",
					// Connect to our state tracker service by name
					"stateTracker": stateTrackerName,
				},
			},
		},
		Workflows: map[string]interface{}{
			"statemachine": map[string]interface{}{
				"engine": stateMachineEngineName,
				"definitions": []interface{}{
					map[string]interface{}{
						"name":         "test-workflow",
						"description":  "Test order workflow",
						"initialState": "new",
						"states": map[string]interface{}{
							"new": map[string]interface{}{
								"description": "New order",
								"isFinal":     false,
								"isError":     false,
							},
							"validating": map[string]interface{}{
								"description": "Validating order",
								"isFinal":     false,
								"isError":     false,
							},
							"validated": map[string]interface{}{
								"description": "Order validated",
								"isFinal":     true,
								"isError":     false,
							},
						},
						"transitions": map[string]interface{}{
							"submit_order": map[string]interface{}{
								"fromState": "new",
								"toState":   "validating",
							},
							"validate_order": map[string]interface{}{
								"fromState": "validating",
								"toState":   "validated",
							},
						},
					},
				},
				"hooks": []interface{}{
					map[string]interface{}{
						"workflowType": "test-workflow",
						"transitions":  []string{"submit_order"},
						"handler":      validationHandlerName,
					},
				},
			},
		},
	}

	// Add state tracker module factory to the engine so it can create it during BuildFromConfig
	engine.AddModuleType("workflow.statetracker", func(name string, config map[string]interface{}) modular.Module {
		return module.NewStateTracker(name)
	})

	// Configure engine
	err := engine.BuildFromConfig(cfg)
	if err != nil {
		t.Fatalf("Failed to configure workflow: %v", err)
	}

	// Start engine
	ctx := context.Background()
	err = engine.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start workflow: %v", err)
	}

	// Get the state machine from the application
	var stateMachineSvc interface{}
	err = app.GetService(stateMachineEngineName, &stateMachineSvc)
	if err != nil {
		t.Fatalf("Failed to get state machine service: %v", err)
	}

	stateMachine, ok := stateMachineSvc.(*module.StateMachineEngine)
	if !ok {
		t.Fatalf("Expected a StateMachineEngine, got something else")
	}

	// Create a workflow instance
	instance, err := stateMachine.CreateWorkflow("test-workflow", "order-123", map[string]interface{}{
		"customer": "test-customer",
		"amount":   99.99,
	})
	if err != nil {
		t.Fatalf("Failed to create workflow instance: %v", err)
	}

	// Verify initial state
	if instance.CurrentState != "new" {
		t.Errorf("Expected initial state 'new', got '%s'", instance.CurrentState)
	}

	// Trigger transition
	err = stateMachine.TriggerTransition(ctx, "order-123", "submit_order", map[string]interface{}{
		"validated": true,
	})
	if err != nil {
		t.Fatalf("Failed to trigger transition: %v", err)
	}

	// Wait for handler to be called with a timeout
	select {
	case <-handlerCalled:
		t.Log("Validation handler was called successfully")
	case <-time.After(3 * time.Second):
		t.Error("Timed out waiting for validation handler to be called")
	}

	// Verify handler was called
	if !validationHandlerCalled {
		t.Errorf("Expected validation handler to be called")
	}

	// Check if the state changed
	updatedInstance, err := stateMachine.GetInstance("order-123")
	if err != nil {
		t.Fatalf("Failed to get workflow instance: %v", err)
	}

	if updatedInstance.CurrentState != "validating" {
		t.Errorf("Expected state to change to 'validating', but got '%s'", updatedInstance.CurrentState)
	}

	// Stop engine
	err = engine.Stop(ctx)
	if err != nil {
		t.Fatalf("Failed to stop workflow: %v", err)
	}
}
