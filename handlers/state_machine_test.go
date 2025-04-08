package handlers

import (
	"context"
	"log"
	"log/slog"
	"testing"
	"time"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/mock"
	"github.com/GoCodeAlone/workflow/module"
)

// TestStateMachineWorkflow tests the state machine workflow handler
func TestStateMachineWorkflow(t *testing.T) {
	// Create a simple application with proper config provider and logger
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), slog.New(slog.NewTextHandler(log.Writer(), nil)))

	// Initialize the app
	err := app.Init()
	if err != nil {
		t.Fatalf("Failed to initialize app: %v", err)
	}

	// Create a unique module name for this test to avoid conflicts
	moduleBaseName := "test-order-processor"
	uniqueModuleName := moduleBaseName + "-" + time.Now().Format("20060102150405.000000000")

	// Create namespace provider for testing
	namespace := module.NewStandardNamespace("test", "")

	// Create workflow engine
	engine = workflow.NewStdEngine(app, &mock.Logger{LogEntries: make([]string, 0)})

	// Register workflow handlers
	engine.RegisterWorkflowHandler(NewStateMachineWorkflowHandlerWithNamespace(namespace))

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

	// Initialize the state machine before registering handlers
	stateMachine := module.NewStateMachineEngineWithNamespace(uniqueModuleName, namespace)

	// Register a state machine definition manually to ensure it exists
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

	// Set a transition handler directly with the state machine
	stateMachine.SetTransitionHandler(validationHandler)

	// Register in service registry to make it available to workflow
	validationHandlerName := namespace.FormatName("validation-handler")
	app.RegisterService(validationHandlerName, validationHandler)

	// Use our adapter for proper modular.Module implementation
	stateMachineModule := &stateMachineModuleAdapter{
		engine:    stateMachine,
		namespace: namespace,
	}
	app.RegisterModule(stateMachineModule)

	// Create a minimal state machine workflow configuration
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name: uniqueModuleName, // Use the unique generated name instead of just stateMachine.Name()
				Type: "statemachine.engine",
				Config: map[string]interface{}{
					"description": "Order processing state machine",
				},
			},
		},
		Workflows: map[string]interface{}{
			"statemachine": map[string]interface{}{
				"engine": uniqueModuleName, // Use the unique name here too
				"definitions": []interface{}{
					map[string]interface{}{
						"name":         "test-workflow",
						"description":  "Test order workflow",
						"initialState": "new",
						"states": map[string]interface{}{
							"new": map[string]interface{}{
								"description": "New order",
								"isFinal":     false,
							},
							"validating": map[string]interface{}{
								"description": "Validating order",
								"isFinal":     false,
							},
							"validated": map[string]interface{}{
								"description": "Order validated",
								"isFinal":     true,
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
						"handler":      "validation-handler",
					},
				},
			},
		},
	}

	// Configure engine
	err = engine.BuildFromConfig(cfg)
	if err != nil {
		t.Fatalf("Failed to configure workflow: %v", err)
	}

	// Start engine
	ctx := context.Background()
	err = engine.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start workflow: %v", err)
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

type stateMachineModuleAdapter struct {
	engine    *module.StateMachineEngine
	namespace module.ModuleNamespaceProvider
}

func (a *stateMachineModuleAdapter) Init(app modular.Application) error {
	// Pass the full application to the engine
	return a.engine.Init(app)
}

func (a *stateMachineModuleAdapter) Name() string {
	return a.engine.Name()
}

func (a *stateMachineModuleAdapter) Start(ctx context.Context) error {
	return nil
}

func (a *stateMachineModuleAdapter) Stop(ctx context.Context) error {
	return nil
}

func (a *stateMachineModuleAdapter) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        a.engine.Name(),
			Description: "State Machine StdEngine",
			Instance:    a.engine,
		},
	}
}

func (a *stateMachineModuleAdapter) RequiresServices() []modular.ServiceDependency {
	return nil
}
