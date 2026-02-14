package workflow

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/handlers"
	"github.com/GoCodeAlone/workflow/module"
)

// TestOrderProcessingPipeline_BuildFromConfig loads the YAML config and
// verifies that all 10 modules are created and the engine builds without error.
func TestOrderProcessingPipeline_BuildFromConfig(t *testing.T) {
	cfg, err := config.LoadFromFile("example/order-processing-pipeline.yaml")
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if len(cfg.Modules) != 10 {
		t.Fatalf("Expected 10 modules in config, got %d", len(cfg.Modules))
	}

	// Verify expected module names
	expectedNames := []string{
		"order-server", "order-router", "order-api",
		"order-transformer", "order-state-engine", "order-state-tracker",
		"order-broker", "notification-handler",
		"order-metrics", "order-health",
	}
	for i, name := range expectedNames {
		if cfg.Modules[i].Name != name {
			t.Errorf("Module %d: expected name %q, got %q", i, name, cfg.Modules[i].Name)
		}
	}

	// Build engine from config using the real modular.Application so Init()
	// properly initializes modules and registers their services.
	logger := &mockLogger{}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)

	engine := NewStdEngine(app, logger)

	engine.RegisterWorkflowHandler(handlers.NewHTTPWorkflowHandler())
	engine.RegisterWorkflowHandler(handlers.NewStateMachineWorkflowHandler())
	engine.RegisterWorkflowHandler(handlers.NewMessagingWorkflowHandler())

	err = engine.BuildFromConfig(cfg)
	if err != nil {
		t.Fatalf("BuildFromConfig failed: %v", err)
	}

	// Verify all modules registered in the application
	registeredModules := app.GetAllModules()
	for _, name := range expectedNames {
		if registeredModules[name] == nil {
			t.Errorf("Module %q was not registered in the application", name)
		}
	}
}

// TestOrderProcessingPipeline_EndToEnd exercises the full order processing
// pipeline: create a state machine definition, register a transformer pipeline,
// create a workflow instance, trigger transitions through received->validated->stored->notified,
// publish a message to the broker, and verify the notification handler receives it.
func TestOrderProcessingPipeline_EndToEnd(t *testing.T) {
	app := newMockApplication()
	engine := NewStdEngine(app, app.Logger())

	// Create individual modules directly for end-to-end testing
	stateMachineEngine := module.NewStateMachineEngine("order-state-engine")
	transformer := module.NewDataTransformer("order-transformer")
	broker := module.NewInMemoryMessageBroker("order-broker")
	tracker := module.NewStateTracker("order-state-tracker")
	metrics := module.NewMetricsCollector("order-metrics")
	health := module.NewHealthChecker("order-health")

	// Register modules with the app
	app.RegisterModule(stateMachineEngine)
	app.RegisterModule(transformer)
	app.RegisterModule(broker)
	app.RegisterModule(tracker)
	app.RegisterModule(metrics)
	app.RegisterModule(health)

	// Initialize modules so services get registered
	if err := stateMachineEngine.Init(app); err != nil {
		t.Fatalf("Failed to init state machine engine: %v", err)
	}
	if err := transformer.Init(app); err != nil {
		t.Fatalf("Failed to init transformer: %v", err)
	}
	if err := broker.Init(app); err != nil {
		t.Fatalf("Failed to init broker: %v", err)
	}
	if err := tracker.Init(app); err != nil {
		t.Fatalf("Failed to init tracker: %v", err)
	}
	if err := metrics.Init(app); err != nil {
		t.Fatalf("Failed to init metrics: %v", err)
	}
	if err := health.Init(app); err != nil {
		t.Fatalf("Failed to init health: %v", err)
	}

	// Register order-processing state machine definition
	err := stateMachineEngine.RegisterDefinition(&module.StateMachineDefinition{
		Name:         "order-processing",
		Description:  "Order processing workflow",
		InitialState: "received",
		States: map[string]*module.State{
			"received":  {Name: "received", Description: "Order received"},
			"validated": {Name: "validated", Description: "Order validated"},
			"stored":    {Name: "stored", Description: "Order stored"},
			"notified":  {Name: "notified", Description: "Notification sent", IsFinal: true},
			"failed":    {Name: "failed", Description: "Order failed", IsFinal: true, IsError: true},
		},
		Transitions: map[string]*module.Transition{
			"validate_order":    {Name: "validate_order", FromState: "received", ToState: "validated"},
			"store_order":       {Name: "store_order", FromState: "validated", ToState: "stored"},
			"send_notification": {Name: "send_notification", FromState: "stored", ToState: "notified"},
			"fail_validation":   {Name: "fail_validation", FromState: "received", ToState: "failed"},
		},
	})
	if err != nil {
		t.Fatalf("Failed to register definition: %v", err)
	}

	// Register a transformer pipeline for order validation
	transformer.RegisterPipeline(&module.TransformPipeline{
		Name: "validate-order",
		Operations: []module.TransformOperation{
			{
				Type:   "filter",
				Config: map[string]any{"fields": []any{"orderId", "customer", "total"}},
			},
			{
				Type:   "map",
				Config: map[string]any{"mappings": map[string]any{"customer": "customerName"}},
			},
		},
	})

	// Set up notification tracking via broker subscription
	var mu sync.Mutex
	var receivedMessages [][]byte
	notificationHandler := module.NewFunctionMessageHandler(func(message []byte) error {
		mu.Lock()
		defer mu.Unlock()
		receivedMessages = append(receivedMessages, message)
		return nil
	})
	if err := broker.Subscribe("order.completed", notificationHandler); err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	ctx := context.Background()

	// Step 1: Transform the incoming order data
	orderData := map[string]any{
		"orderId":  "ORD-001",
		"customer": "Alice",
		"total":    99.99,
		"extra":    "should-be-filtered",
	}

	transformed, err := transformer.Transform(ctx, "validate-order", orderData)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	transformedMap, ok := transformed.(map[string]any)
	if !ok {
		t.Fatalf("Expected map result from transform, got %T", transformed)
	}

	// Verify filter removed "extra" and map renamed "customer" to "customerName"
	if _, exists := transformedMap["extra"]; exists {
		t.Error("Expected 'extra' field to be filtered out")
	}
	if _, exists := transformedMap["customerName"]; !exists {
		t.Error("Expected 'customer' to be renamed to 'customerName'")
	}

	// Step 2: Create workflow instance and transition through states
	instance, err := stateMachineEngine.CreateWorkflow("order-processing", "ORD-001", map[string]any{
		"orderId": "ORD-001",
	})
	if err != nil {
		t.Fatalf("CreateWorkflow failed: %v", err)
	}
	if instance.CurrentState != "received" {
		t.Fatalf("Expected initial state 'received', got %q", instance.CurrentState)
	}

	// Track state in the state tracker
	tracker.SetState("order", "ORD-001", "received", nil)

	// Transition: received -> validated
	err = stateMachineEngine.TriggerTransition(ctx, "ORD-001", "validate_order", map[string]any{
		"validated": true,
	})
	if err != nil {
		t.Fatalf("validate_order transition failed: %v", err)
	}
	tracker.SetState("order", "ORD-001", "validated", nil)

	// Transition: validated -> stored
	err = stateMachineEngine.TriggerTransition(ctx, "ORD-001", "store_order", nil)
	if err != nil {
		t.Fatalf("store_order transition failed: %v", err)
	}
	tracker.SetState("order", "ORD-001", "stored", nil)

	// Transition: stored -> notified
	err = stateMachineEngine.TriggerTransition(ctx, "ORD-001", "send_notification", nil)
	if err != nil {
		t.Fatalf("send_notification transition failed: %v", err)
	}
	tracker.SetState("order", "ORD-001", "notified", nil)

	// Verify final state
	inst, err := stateMachineEngine.GetInstance("ORD-001")
	if err != nil {
		t.Fatalf("GetInstance failed: %v", err)
	}
	if inst.CurrentState != "notified" {
		t.Errorf("Expected final state 'notified', got %q", inst.CurrentState)
	}
	if !inst.Completed {
		t.Error("Expected workflow to be marked as completed")
	}

	// Step 3: Publish a message to the broker and verify notification handler receives it
	err = broker.SendMessage("order.completed", []byte(`{"orderId":"ORD-001","status":"notified"}`))
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}

	mu.Lock()
	msgCount := len(receivedMessages)
	mu.Unlock()
	if msgCount != 1 {
		t.Errorf("Expected 1 message received by notification handler, got %d", msgCount)
	}

	// Step 4: Verify state tracker has correct state
	stateInfo, exists := tracker.GetState("order", "ORD-001")
	if !exists {
		t.Fatal("Expected state to exist in tracker")
	}
	if stateInfo.CurrentState != "notified" {
		t.Errorf("Expected tracker state 'notified', got %q", stateInfo.CurrentState)
	}

	// Step 5: Record metrics for the workflow execution
	metrics.RecordWorkflowExecution("order-processing", "process", "success")
	metrics.RecordWorkflowDuration("order-processing", "process", 100*time.Millisecond)

	// Verify health checker can register and run checks
	health.RegisterCheck("state-engine", func(ctx context.Context) module.HealthCheckResult {
		return module.HealthCheckResult{Status: "healthy", Message: "State machine engine running"}
	})

	_ = engine // engine created but pipeline exercised via direct module calls
}

// TestOrderProcessingPipeline_ErrorPath verifies that an invalid order
// transitions to the failed state via the fail_validation transition.
func TestOrderProcessingPipeline_ErrorPath(t *testing.T) {
	app := newMockApplication()

	stateMachineEngine := module.NewStateMachineEngine("order-state-engine")
	tracker := module.NewStateTracker("order-state-tracker")

	if err := stateMachineEngine.Init(app); err != nil {
		t.Fatalf("Failed to init state machine engine: %v", err)
	}
	if err := tracker.Init(app); err != nil {
		t.Fatalf("Failed to init tracker: %v", err)
	}

	// Register the same definition
	err := stateMachineEngine.RegisterDefinition(&module.StateMachineDefinition{
		Name:         "order-processing",
		Description:  "Order processing workflow",
		InitialState: "received",
		States: map[string]*module.State{
			"received":  {Name: "received"},
			"validated": {Name: "validated"},
			"stored":    {Name: "stored"},
			"notified":  {Name: "notified", IsFinal: true},
			"failed":    {Name: "failed", IsFinal: true, IsError: true},
		},
		Transitions: map[string]*module.Transition{
			"validate_order":    {Name: "validate_order", FromState: "received", ToState: "validated"},
			"store_order":       {Name: "store_order", FromState: "validated", ToState: "stored"},
			"send_notification": {Name: "send_notification", FromState: "stored", ToState: "notified"},
			"fail_validation":   {Name: "fail_validation", FromState: "received", ToState: "failed"},
		},
	})
	if err != nil {
		t.Fatalf("Failed to register definition: %v", err)
	}

	ctx := context.Background()

	// Create a workflow for an invalid order
	instance, err := stateMachineEngine.CreateWorkflow("order-processing", "ORD-BAD", map[string]any{
		"orderId": "ORD-BAD",
		"invalid": true,
	})
	if err != nil {
		t.Fatalf("CreateWorkflow failed: %v", err)
	}

	if instance.CurrentState != "received" {
		t.Fatalf("Expected initial state 'received', got %q", instance.CurrentState)
	}

	// Transition: received -> failed via fail_validation
	err = stateMachineEngine.TriggerTransition(ctx, "ORD-BAD", "fail_validation", map[string]any{
		"reason": "Missing required fields",
	})
	if err != nil {
		t.Fatalf("fail_validation transition failed: %v", err)
	}

	// Verify the workflow is now in the failed state
	inst, err := stateMachineEngine.GetInstance("ORD-BAD")
	if err != nil {
		t.Fatalf("GetInstance failed: %v", err)
	}

	if inst.CurrentState != "failed" {
		t.Errorf("Expected state 'failed', got %q", inst.CurrentState)
	}
	if !inst.Completed {
		t.Error("Expected workflow to be marked as completed (failed is a final state)")
	}
	if inst.Error == "" {
		t.Error("Expected error message to be set for error state")
	}

	// Verify that further transitions from 'failed' are not possible
	err = stateMachineEngine.TriggerTransition(ctx, "ORD-BAD", "validate_order", nil)
	if err == nil {
		t.Error("Expected error when trying to transition from 'failed' state, but got nil")
	}

	// Track state in state tracker and verify
	tracker.SetState("order", "ORD-BAD", "failed", map[string]any{
		"reason": "Missing required fields",
	})

	stateInfo, exists := tracker.GetState("order", "ORD-BAD")
	if !exists {
		t.Fatal("Expected state to exist in tracker")
	}
	if stateInfo.CurrentState != "failed" {
		t.Errorf("Expected tracker state 'failed', got %q", stateInfo.CurrentState)
	}
}

// TestOrderProcessingPipeline_TransformChain tests the DataTransformer with
// map, filter, and convert operations chained together.
func TestOrderProcessingPipeline_TransformChain(t *testing.T) {
	transformer := module.NewDataTransformer("order-transformer")

	ctx := context.Background()

	t.Run("map_then_filter", func(t *testing.T) {
		transformer.RegisterPipeline(&module.TransformPipeline{
			Name: "map-and-filter",
			Operations: []module.TransformOperation{
				{
					Type: "map",
					Config: map[string]any{
						"mappings": map[string]any{
							"cust_name": "customerName",
							"cust_id":   "customerId",
						},
					},
				},
				{
					Type: "filter",
					Config: map[string]any{
						"fields": []any{"customerName", "customerId", "total"},
					},
				},
			},
		})

		input := map[string]any{
			"cust_name": "Bob",
			"cust_id":   "C-123",
			"total":     49.99,
			"internal":  "should-be-removed",
		}

		result, err := transformer.Transform(ctx, "map-and-filter", input)
		if err != nil {
			t.Fatalf("Transform failed: %v", err)
		}

		resultMap, ok := result.(map[string]any)
		if !ok {
			t.Fatalf("Expected map result, got %T", result)
		}

		if resultMap["customerName"] != "Bob" {
			t.Errorf("Expected customerName='Bob', got %v", resultMap["customerName"])
		}
		if resultMap["customerId"] != "C-123" {
			t.Errorf("Expected customerId='C-123', got %v", resultMap["customerId"])
		}
		if resultMap["total"] != 49.99 {
			t.Errorf("Expected total=49.99, got %v", resultMap["total"])
		}
		if _, exists := resultMap["internal"]; exists {
			t.Error("Expected 'internal' field to be filtered out")
		}
		if _, exists := resultMap["cust_name"]; exists {
			t.Error("Expected 'cust_name' to be removed after mapping")
		}
	})

	t.Run("extract_nested", func(t *testing.T) {
		transformer.RegisterPipeline(&module.TransformPipeline{
			Name: "extract-customer",
			Operations: []module.TransformOperation{
				{
					Type: "extract",
					Config: map[string]any{
						"path": "order.customer",
					},
				},
			},
		})

		input := map[string]any{
			"order": map[string]any{
				"customer": map[string]any{
					"name":  "Charlie",
					"email": "charlie@example.com",
				},
				"items": []any{"item1", "item2"},
			},
		}

		result, err := transformer.Transform(ctx, "extract-customer", input)
		if err != nil {
			t.Fatalf("Transform failed: %v", err)
		}

		resultMap, ok := result.(map[string]any)
		if !ok {
			t.Fatalf("Expected map result, got %T", result)
		}
		if resultMap["name"] != "Charlie" {
			t.Errorf("Expected name='Charlie', got %v", resultMap["name"])
		}
	})

	t.Run("convert_json_roundtrip", func(t *testing.T) {
		transformer.RegisterPipeline(&module.TransformPipeline{
			Name: "json-roundtrip",
			Operations: []module.TransformOperation{
				{
					Type:   "convert",
					Config: map[string]any{"from": "json", "to": "string"},
				},
				{
					Type:   "convert",
					Config: map[string]any{"from": "string", "to": "json"},
				},
			},
		})

		input := map[string]any{
			"orderId": "ORD-100",
			"amount":  42.0,
		}

		result, err := transformer.Transform(ctx, "json-roundtrip", input)
		if err != nil {
			t.Fatalf("Transform failed: %v", err)
		}

		resultMap, ok := result.(map[string]any)
		if !ok {
			t.Fatalf("Expected map result, got %T", result)
		}
		if resultMap["orderId"] != "ORD-100" {
			t.Errorf("Expected orderId='ORD-100', got %v", resultMap["orderId"])
		}
		if resultMap["amount"] != 42.0 {
			t.Errorf("Expected amount=42.0, got %v", resultMap["amount"])
		}
	})

	t.Run("filter_only", func(t *testing.T) {
		transformer.RegisterPipeline(&module.TransformPipeline{
			Name: "filter-sensitive",
			Operations: []module.TransformOperation{
				{
					Type: "filter",
					Config: map[string]any{
						"fields": []any{"orderId", "status"},
					},
				},
			},
		})

		input := map[string]any{
			"orderId":    "ORD-200",
			"status":     "received",
			"creditCard": "4111-xxxx-xxxx-1111",
			"ssn":        "xxx-xx-xxxx",
		}

		result, err := transformer.Transform(ctx, "filter-sensitive", input)
		if err != nil {
			t.Fatalf("Transform failed: %v", err)
		}

		resultMap, ok := result.(map[string]any)
		if !ok {
			t.Fatalf("Expected map result, got %T", result)
		}

		if len(resultMap) != 2 {
			t.Errorf("Expected 2 fields after filter, got %d", len(resultMap))
		}
		if _, exists := resultMap["creditCard"]; exists {
			t.Error("Expected 'creditCard' to be filtered out")
		}
		if _, exists := resultMap["ssn"]; exists {
			t.Error("Expected 'ssn' to be filtered out")
		}
	})
}
