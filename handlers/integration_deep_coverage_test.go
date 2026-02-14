package handlers

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/module"
)

// --- SetEventEmitter tests ---

func TestSetEventEmitter(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	if h.eventEmitter != nil {
		t.Fatal("expected nil emitter before SetEventEmitter")
	}

	app := newMockApp()
	emitter := module.NewWorkflowEventEmitter(app)
	h.SetEventEmitter(emitter)

	if h.eventEmitter == nil {
		t.Fatal("expected non-nil emitter after SetEventEmitter")
	}
}

// --- ExecuteIntegrationWorkflow with event emitter (step started/completed/failed) ---

func TestExecuteIntegrationWorkflow_EventEmitter_StepStartedAndCompleted(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	emitter := module.NewWorkflowEventEmitter(app)
	h.SetEventEmitter(emitter)

	registry := module.NewIntegrationRegistry("test-registry")
	conn := &controllableMockConnector{
		name:      "emitter-conn",
		connected: true,
	}
	registry.RegisterConnector(conn)

	steps := []IntegrationStep{
		{
			Name:      "step1",
			Connector: "emitter-conn",
			Action:    "do-something",
		},
	}

	result, err := h.ExecuteIntegrationWorkflow(context.Background(), registry, steps, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["step1"] == nil {
		t.Error("expected step1 result")
	}
}

func TestExecuteIntegrationWorkflow_EventEmitter_StepFailed(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	emitter := module.NewWorkflowEventEmitter(app)
	h.SetEventEmitter(emitter)

	registry := module.NewIntegrationRegistry("test-registry")
	conn := &controllableMockConnector{
		name:       "fail-conn",
		connected:  true,
		executeErr: fmt.Errorf("step failed"),
	}
	registry.RegisterConnector(conn)

	steps := []IntegrationStep{
		{
			Name:      "fail-step",
			Connector: "fail-conn",
			Action:    "do-something",
			// No OnError, so it should return error and emit StepFailed
		},
	}

	_, err := h.ExecuteIntegrationWorkflow(context.Background(), registry, steps, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "error executing step 'fail-step'") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestExecuteIntegrationWorkflow_EventEmitter_OnSuccessPath(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	emitter := module.NewWorkflowEventEmitter(app)
	h.SetEventEmitter(emitter)

	registry := module.NewIntegrationRegistry("test-registry")
	conn := &controllableMockConnector{
		name:      "emitter-conn",
		connected: true,
	}
	registry.RegisterConnector(conn)

	steps := []IntegrationStep{
		{
			Name:      "step1",
			Connector: "emitter-conn",
			Action:    "do-something",
			OnSuccess: "next-handler",
		},
	}

	result, err := h.ExecuteIntegrationWorkflow(context.Background(), registry, steps, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["step1"] == nil {
		t.Error("expected step1 result")
	}
}

// --- ExecuteIntegrationWorkflow connector == nil check ---

// nullConnectorRegistry is a mock registry that returns nil connector without error
type nullConnectorRegistry struct {
	name string
}

func (r *nullConnectorRegistry) Name() string                                    { return r.name }
func (r *nullConnectorRegistry) Init(_ modular.Application) error                { return nil }
func (r *nullConnectorRegistry) Start() error                                    { return nil }
func (r *nullConnectorRegistry) Stop() error                                     { return nil }
func (r *nullConnectorRegistry) RegisterConnector(_ module.IntegrationConnector) {}
func (r *nullConnectorRegistry) GetConnector(_ string) (module.IntegrationConnector, error) {
	return nil, nil // Returns nil connector without error
}
func (r *nullConnectorRegistry) ListConnectors() []string { return nil }

func TestExecuteIntegrationWorkflow_NilConnector(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	registry := &nullConnectorRegistry{name: "null-registry"}

	steps := []IntegrationStep{
		{
			Name:      "step1",
			Connector: "missing",
			Action:    "do-something",
		},
	}

	_, err := h.ExecuteIntegrationWorkflow(context.Background(), registry, steps, nil)
	if err == nil {
		t.Fatal("expected error for nil connector")
	}
	if !strings.Contains(err.Error(), "connector 'missing' not found") {
		t.Errorf("expected connector not found error, got: %v", err)
	}
}

// --- ConnectButNotConnected tests ---

// staleConnector connects but still reports not connected
type staleConnector struct {
	name string
}

func (c *staleConnector) GetName() string                    { return c.name }
func (c *staleConnector) Connect(_ context.Context) error    { return nil }
func (c *staleConnector) Disconnect(_ context.Context) error { return nil }
func (c *staleConnector) IsConnected() bool                  { return false } // Always returns false
func (c *staleConnector) Execute(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
	return nil, nil
}

func TestExecuteIntegrationWorkflow_ConnectSucceedsButNotConnected(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	registry := module.NewIntegrationRegistry("test-registry")
	conn := &staleConnector{name: "stale-conn"}
	registry.RegisterConnector(conn)

	steps := []IntegrationStep{
		{
			Name:      "step1",
			Connector: "stale-conn",
			Action:    "do-something",
		},
	}

	_, err := h.ExecuteIntegrationWorkflow(context.Background(), registry, steps, nil)
	if err == nil {
		t.Fatal("expected error for connector not connected after attempt")
	}
	if !strings.Contains(err.Error(), "connector not connected after connection attempt") {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- Retry with invalid delay string ---

func TestExecuteIntegrationWorkflow_RetryWithInvalidDelay(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	registry := module.NewIntegrationRegistry("test-registry")

	callCount := 0
	conn := &controllableMockConnector{
		name:      "retry-conn",
		connected: true,
		executeFn: func(ctx context.Context, action string, params map[string]any) (map[string]any, error) {
			callCount++
			if callCount == 1 {
				return nil, fmt.Errorf("temporary error")
			}
			return map[string]any{"ok": true}, nil
		},
	}
	registry.RegisterConnector(conn)

	// Invalid duration string should fall back to default 1s
	steps := []IntegrationStep{
		{
			Name:       "retry-step",
			Connector:  "retry-conn",
			Action:     "do-something",
			RetryCount: 1,
			RetryDelay: "not-a-duration",
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := h.ExecuteIntegrationWorkflow(ctx, registry, steps, nil)
	if err != nil {
		t.Fatalf("expected success after retry with default delay, got: %v", err)
	}
	if result["retry-step"] == nil {
		t.Error("expected retry-step result")
	}
}

// --- Retry with OnError after exhaustion and emitter ---

func TestExecuteIntegrationWorkflow_RetryExhaustedWithOnErrorAndEmitter(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	emitter := module.NewWorkflowEventEmitter(app)
	h.SetEventEmitter(emitter)

	registry := module.NewIntegrationRegistry("test-registry")
	conn := &controllableMockConnector{
		name:       "fail-conn",
		connected:  true,
		executeErr: fmt.Errorf("persistent error"),
	}
	registry.RegisterConnector(conn)

	steps := []IntegrationStep{
		{
			Name:       "step1",
			Connector:  "fail-conn",
			Action:     "do-something",
			RetryCount: 1,
			RetryDelay: "1ms",
			OnError:    "continue",
		},
	}

	result, err := h.ExecuteIntegrationWorkflow(context.Background(), registry, steps, nil)
	if err != nil {
		t.Fatalf("expected OnError to handle failure, got: %v", err)
	}
	if result["step1_error"] == nil {
		t.Error("expected step1_error in results")
	}
}

// --- Multi-step dispatch with variable substitution across steps ---

func TestExecuteIntegrationWorkflow_MultiStepVariableChain(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	registry := module.NewIntegrationRegistry("test-registry")

	var step2Params, step3Params map[string]any
	callIdx := 0
	conn := &controllableMockConnector{
		name:      "chain-conn",
		connected: true,
		executeFn: func(ctx context.Context, action string, params map[string]any) (map[string]any, error) {
			callIdx++
			switch callIdx {
			case 1:
				return map[string]any{"userId": "user-42", "email": "test@example.com"}, nil
			case 2:
				step2Params = params
				return map[string]any{"orderId": "order-99"}, nil
			case 3:
				step3Params = params
				return map[string]any{"status": "completed"}, nil
			default:
				return nil, fmt.Errorf("unexpected call")
			}
		},
	}
	registry.RegisterConnector(conn)

	steps := []IntegrationStep{
		{
			Name:      "fetch-user",
			Connector: "chain-conn",
			Action:    "get-user",
		},
		{
			Name:      "create-order",
			Connector: "chain-conn",
			Action:    "create-order",
			Input: map[string]any{
				"userId": "${fetch-user}",
				"static": "value",
			},
		},
		{
			Name:      "confirm-order",
			Connector: "chain-conn",
			Action:    "confirm",
			Input: map[string]any{
				"orderId": "${create-order}",
				"email":   "${fetch-user}",
			},
		},
	}

	initialCtx := map[string]any{"tenant": "acme"}

	result, err := h.ExecuteIntegrationWorkflow(context.Background(), registry, steps, initialCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify initial context is preserved
	if result["tenant"] != "acme" {
		t.Errorf("expected tenant=acme, got %v", result["tenant"])
	}

	// Verify all steps produced results
	if result["fetch-user"] == nil {
		t.Error("expected fetch-user result")
	}
	if result["create-order"] == nil {
		t.Error("expected create-order result")
	}
	if result["confirm-order"] == nil {
		t.Error("expected confirm-order result")
	}

	// Verify variable substitution in step2
	if step2Params["static"] != "value" {
		t.Errorf("expected static=value, got %v", step2Params["static"])
	}
	if step2Params["userId"] == nil {
		t.Error("expected userId to be resolved from fetch-user result")
	}

	// Verify variable substitution in step3
	if step3Params["orderId"] == nil {
		t.Error("expected orderId to be resolved from create-order result")
	}
	if step3Params["email"] == nil {
		t.Error("expected email to be resolved from fetch-user result")
	}
}

// --- ExecuteWorkflow multi-step dispatch with all optional fields ---

func TestExecuteWorkflow_MultiStepDispatch(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	registry := module.NewIntegrationRegistry("test-registry")
	app.services["test-registry"] = registry

	callCount := 0
	conn := &controllableMockConnector{
		name:      "multi-conn",
		connected: true,
		executeFn: func(ctx context.Context, action string, params map[string]any) (map[string]any, error) {
			callCount++
			return map[string]any{"step": callCount}, nil
		},
	}
	registry.RegisterConnector(conn)

	ctx := context.WithValue(context.Background(), applicationContextKey, modular.Application(app))

	data := map[string]any{
		"steps": []any{
			map[string]any{
				"connector":  "multi-conn",
				"action":     "step-a",
				"input":      map[string]any{"key": "val-a"},
				"transform":  "jq .data",
				"onSuccess":  "next",
				"onError":    "handle",
				"retryCount": float64(2),
				"retryDelay": "100ms",
			},
			map[string]any{
				"connector": "multi-conn",
				"action":    "step-b",
			},
			map[string]any{
				"connector": "multi-conn",
				"action":    "step-c",
				"onSuccess": "done",
			},
		},
	}

	result, err := h.ExecuteWorkflow(ctx, "integration", "test-registry", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if callCount != 3 {
		t.Errorf("expected 3 step executions, got %d", callCount)
	}
}

// --- ExecuteWorkflow with invalid step data in array ---

func TestExecuteWorkflow_InvalidStepDataInArray(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	registry := module.NewIntegrationRegistry("test-registry")
	app.services["test-registry"] = registry

	ctx := context.WithValue(context.Background(), applicationContextKey, modular.Application(app))

	data := map[string]any{
		"steps": []any{
			"not-a-map", // Invalid step data
		},
	}

	_, err := h.ExecuteWorkflow(ctx, "integration", "test-registry", data)
	if err == nil {
		t.Fatal("expected error for invalid step data")
	}
	if !strings.Contains(err.Error(), "invalid step data") {
		t.Errorf("expected invalid step data error, got: %v", err)
	}
}

// --- Namespace resolution in ConfigureWorkflow ---

func TestConfigureWorkflow_WithNamespace(t *testing.T) {
	namespace := module.NewStandardNamespace("tenant1", "region1")
	h := NewIntegrationWorkflowHandlerWithNamespace(namespace)
	app := newMockApp()

	// The handler will resolve "test-registry" using namespace
	resolvedName := namespace.ResolveDependency("test-registry")
	registry := module.NewIntegrationRegistry(resolvedName)
	app.services[resolvedName] = registry

	err := h.ConfigureWorkflow(app, map[string]any{
		"registry": "test-registry",
		"connectors": []any{
			map[string]any{
				"name": "api-conn",
				"type": "http",
				"config": map[string]any{
					"baseURL": "http://example.com",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

// --- ConfigureWorkflow database connector missing driver only ---

func TestConfigureWorkflow_DatabaseConnectorMissingDriver(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	registry := module.NewIntegrationRegistry("test-registry")
	app.services["test-registry"] = registry

	err := h.ConfigureWorkflow(app, map[string]any{
		"registry": "test-registry",
		"connectors": []any{
			map[string]any{
				"name": "db-conn",
				"type": "database",
				"config": map[string]any{
					"dsn": ":memory:",
				},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "driver and dsn must be specified") {
		t.Fatalf("expected driver/dsn error, got: %v", err)
	}
}

// --- ConfigureWorkflow database connector missing dsn only ---

func TestConfigureWorkflow_DatabaseConnectorMissingDSN(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	registry := module.NewIntegrationRegistry("test-registry")
	app.services["test-registry"] = registry

	err := h.ConfigureWorkflow(app, map[string]any{
		"registry": "test-registry",
		"connectors": []any{
			map[string]any{
				"name": "db-conn",
				"type": "database",
				"config": map[string]any{
					"driver": "sqlite3",
				},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "driver and dsn must be specified") {
		t.Fatalf("expected driver/dsn error, got: %v", err)
	}
}

// --- ConfigureWorkflow database connector without maxOpenConns ---

func TestConfigureWorkflow_DatabaseConnectorWithoutMaxOpenConns(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	registry := module.NewIntegrationRegistry("test-registry")
	app.services["test-registry"] = registry

	err := h.ConfigureWorkflow(app, map[string]any{
		"registry": "test-registry",
		"connectors": []any{
			map[string]any{
				"name": "db-conn",
				"type": "database",
				"config": map[string]any{
					"driver": "sqlite3",
					"dsn":    ":memory:",
					// No maxOpenConns
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

// --- ConfigureWorkflow HTTP connector with allowPrivateIPs ---

func TestConfigureWorkflow_HTTPConnectorWithPrivateIPs(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	registry := module.NewIntegrationRegistry("test-registry")
	app.services["test-registry"] = registry

	err := h.ConfigureWorkflow(app, map[string]any{
		"registry": "test-registry",
		"connectors": []any{
			map[string]any{
				"name": "api-conn",
				"type": "http",
				"config": map[string]any{
					"baseURL":         "http://internal-service",
					"allowPrivateIPs": true,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

// --- ConfigureWorkflow HTTP connector with rate limit ---

func TestConfigureWorkflow_HTTPConnectorWithRateLimit(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	registry := module.NewIntegrationRegistry("test-registry")
	app.services["test-registry"] = registry

	err := h.ConfigureWorkflow(app, map[string]any{
		"registry": "test-registry",
		"connectors": []any{
			map[string]any{
				"name": "api-conn",
				"type": "http",
				"config": map[string]any{
					"baseURL":           "http://example.com",
					"requestsPerMinute": float64(120),
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

// --- Service helper edge cases ---

func TestFixMessagingHandlerServices_WithEventProcessor(t *testing.T) {
	app := newMockApp()
	app.services["eventProcessor"] = "mock-processor"

	svcs := FixMessagingHandlerServices(app)
	if svcs["eventProcessor"] != "mock-processor" {
		t.Errorf("expected eventProcessor in services, got %v", svcs)
	}
}

func TestApplicationHelper_Service_CacheHitAndMiss(t *testing.T) {
	app := newMockApp()
	app.services["real-svc"] = "real-value"

	helper := NewApplicationHelper(app)

	// First call: cache miss, service found
	svc := helper.Service("real-svc")
	if svc != "real-value" {
		t.Errorf("expected 'real-value', got %v", svc)
	}

	// Second call: cache hit
	svc2 := helper.Service("real-svc")
	if svc2 != "real-value" {
		t.Errorf("expected cached 'real-value', got %v", svc2)
	}

	// Not found: no caching
	svc3 := helper.Service("nonexistent")
	if svc3 != nil {
		t.Errorf("expected nil for nonexistent, got %v", svc3)
	}
}

// --- PatchAppServiceCalls ---

func TestPatchAppServiceCalls(t *testing.T) {
	app := CreateMockApplication()
	// Should not panic
	PatchAppServiceCalls(app)
}

// --- ExecuteWorkflow with steps containing retryCount 0 (edge case) ---

func TestExecuteWorkflow_StepWithZeroRetry(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	registry := module.NewIntegrationRegistry("test-registry")
	app.services["test-registry"] = registry

	conn := &controllableMockConnector{
		name:      "test-conn",
		connected: true,
	}
	registry.RegisterConnector(conn)

	ctx := context.WithValue(context.Background(), applicationContextKey, modular.Application(app))

	data := map[string]any{
		"steps": []any{
			map[string]any{
				"connector":  "test-conn",
				"action":     "do-something",
				"retryCount": float64(0),
				"retryDelay": "0s",
			},
		},
	}

	result, err := h.ExecuteWorkflow(ctx, "integration", "test-registry", data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

// --- ExecuteIntegrationWorkflow retry with emitter and exhaustion without OnError ---

func TestExecuteIntegrationWorkflow_RetryExhaustedNoOnErrorWithEmitter(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	emitter := module.NewWorkflowEventEmitter(app)
	h.SetEventEmitter(emitter)

	registry := module.NewIntegrationRegistry("test-registry")
	conn := &controllableMockConnector{
		name:       "fail-conn",
		connected:  true,
		executeErr: fmt.Errorf("permanent error"),
	}
	registry.RegisterConnector(conn)

	steps := []IntegrationStep{
		{
			Name:       "fail-step",
			Connector:  "fail-conn",
			Action:     "do-something",
			RetryCount: 1,
			RetryDelay: "1ms",
			// No OnError - should return error and emit StepFailed
		},
	}

	_, err := h.ExecuteIntegrationWorkflow(context.Background(), registry, steps, nil)
	if err == nil {
		t.Fatal("expected error after retries exhausted")
	}
	if !strings.Contains(err.Error(), "error executing step 'fail-step'") {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- ExecuteIntegrationWorkflow with emitter and multi-step where one fails mid-chain ---

func TestExecuteIntegrationWorkflow_EventEmitter_MultiStepWithFailure(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	emitter := module.NewWorkflowEventEmitter(app)
	h.SetEventEmitter(emitter)

	registry := module.NewIntegrationRegistry("test-registry")
	callIdx := 0
	conn := &controllableMockConnector{
		name:      "multi-conn",
		connected: true,
		executeFn: func(ctx context.Context, action string, params map[string]any) (map[string]any, error) {
			callIdx++
			if callIdx == 2 {
				return nil, fmt.Errorf("step 2 failed")
			}
			return map[string]any{"step": callIdx}, nil
		},
	}
	registry.RegisterConnector(conn)

	steps := []IntegrationStep{
		{Name: "step1", Connector: "multi-conn", Action: "action1"},
		{Name: "step2", Connector: "multi-conn", Action: "action2"},
		{Name: "step3", Connector: "multi-conn", Action: "action3"},
	}

	_, err := h.ExecuteIntegrationWorkflow(context.Background(), registry, steps, nil)
	if err == nil {
		t.Fatal("expected error from step2")
	}
	if !strings.Contains(err.Error(), "error executing step 'step2'") {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- ExecuteIntegrationWorkflow with OnError handler continuing across steps ---

func TestExecuteIntegrationWorkflow_OnErrorContinuation(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	registry := module.NewIntegrationRegistry("test-registry")

	callIdx := 0
	conn := &controllableMockConnector{
		name:      "cont-conn",
		connected: true,
		executeFn: func(ctx context.Context, action string, params map[string]any) (map[string]any, error) {
			callIdx++
			if callIdx == 1 {
				return nil, fmt.Errorf("first step failed")
			}
			return map[string]any{"success": true}, nil
		},
	}
	registry.RegisterConnector(conn)

	steps := []IntegrationStep{
		{
			Name:      "step1",
			Connector: "cont-conn",
			Action:    "fail-action",
			OnError:   "continue",
		},
		{
			Name:      "step2",
			Connector: "cont-conn",
			Action:    "succeed-action",
		},
	}

	result, err := h.ExecuteIntegrationWorkflow(context.Background(), registry, steps, nil)
	if err != nil {
		t.Fatalf("expected no error (OnError=continue), got: %v", err)
	}
	// step1 should have stored error
	if result["step1_error"] == nil {
		t.Error("expected step1_error in results")
	}
	// step2 should have succeeded
	if result["step2"] == nil {
		t.Error("expected step2 result")
	}
}

// --- NewIntegrationWorkflowHandlerWithNamespace with explicit namespace ---

func TestNewIntegrationWorkflowHandlerWithNamespace_ExplicitNamespace(t *testing.T) {
	namespace := module.NewStandardNamespace("myTenant", "myRegion")
	h := NewIntegrationWorkflowHandlerWithNamespace(namespace)

	name := h.Name()
	if name == "" {
		t.Error("expected non-empty name")
	}
	// The name should be formatted by the namespace
	if !strings.Contains(name, IntegrationWorkflowHandlerName) {
		t.Errorf("expected name to contain '%s', got '%s'", IntegrationWorkflowHandlerName, name)
	}
}

// --- ConfigureWorkflow with no steps section ---

func TestConfigureWorkflow_NoSteps(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	registry := module.NewIntegrationRegistry("test-registry")
	app.services["test-registry"] = registry

	// Config with connectors but no steps section at all
	err := h.ConfigureWorkflow(app, map[string]any{
		"registry": "test-registry",
		"connectors": []any{
			map[string]any{
				"name": "api-conn",
				"type": "http",
				"config": map[string]any{
					"baseURL": "http://example.com",
				},
			},
		},
		// No "steps" key
	})
	if err != nil {
		t.Fatalf("expected no error when steps are omitted, got: %v", err)
	}
}

// --- ConfigureWorkflow with no connectors key (nil) ---

func TestConfigureWorkflow_NilConnectors(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	registry := module.NewIntegrationRegistry("test-registry")
	app.services["test-registry"] = registry

	err := h.ConfigureWorkflow(app, map[string]any{
		"registry": "test-registry",
		// No connectors key at all
	})
	if err == nil || !strings.Contains(err.Error(), "no connectors defined") {
		t.Fatalf("expected no connectors error, got: %v", err)
	}
}

// --- ConfigureWorkflow HTTP connector with no auth type ---

func TestConfigureWorkflow_HTTPConnectorNoAuth(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	registry := module.NewIntegrationRegistry("test-registry")
	app.services["test-registry"] = registry

	err := h.ConfigureWorkflow(app, map[string]any{
		"registry": "test-registry",
		"connectors": []any{
			map[string]any{
				"name": "api-conn",
				"type": "http",
				"config": map[string]any{
					"baseURL": "http://example.com",
					// No authType
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

// --- ConfigureWorkflow HTTP connector with nil config ---

func TestConfigureWorkflow_HTTPConnectorNilConfig(t *testing.T) {
	h := NewIntegrationWorkflowHandler()
	app := newMockApp()
	registry := module.NewIntegrationRegistry("test-registry")
	app.services["test-registry"] = registry

	err := h.ConfigureWorkflow(app, map[string]any{
		"registry": "test-registry",
		"connectors": []any{
			map[string]any{
				"name": "api-conn",
				"type": "http",
				// No "config" key — config will be nil map
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "baseURL not specified") {
		t.Fatalf("expected baseURL error, got: %v", err)
	}
}
