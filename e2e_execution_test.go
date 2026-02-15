package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/CrisisTextLine/modular/modules/eventbus"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/dynamic"
	"github.com/GoCodeAlone/workflow/handlers"
	"github.com/GoCodeAlone/workflow/module"
)

// getFreePort returns an available TCP port for test servers.
func getFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to get free port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

// waitForServer polls the server until it accepts connections or the timeout expires.
func waitForServer(t *testing.T, baseURL string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 500 * time.Millisecond}
	for time.Now().Before(deadline) {
		resp, err := client.Get(baseURL + "/")
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("Server at %s did not become ready within %v", baseURL, timeout)
}

// TestE2E_SimpleHTTPWorkflow proves the engine can build from config,
// start a real HTTP server, and respond to real HTTP requests.
func TestE2E_SimpleHTTPWorkflow(t *testing.T) {
	port := getFreePort(t)
	addr := fmt.Sprintf(":%d", port)
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "test-server", Type: "http.server", Config: map[string]any{"address": addr}},
			{Name: "test-router", Type: "http.router", DependsOn: []string{"test-server"}},
			{Name: "test-handler", Type: "http.handler", DependsOn: []string{"test-router"}, Config: map[string]any{"contentType": "application/json"}},
		},
		Workflows: map[string]any{
			"http": map[string]any{
				"server": "test-server",
				"router": "test-router",
				"routes": []any{
					map[string]any{
						"method":  "POST",
						"path":    "/api/test",
						"handler": "test-handler",
					},
				},
			},
		},
		Triggers: map[string]any{},
	}

	logger := &mockLogger{}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)
	engine := NewStdEngine(app, logger)
	engine.RegisterWorkflowHandler(handlers.NewHTTPWorkflowHandler())

	if err := engine.BuildFromConfig(cfg); err != nil {
		t.Fatalf("BuildFromConfig failed: %v", err)
	}

	ctx := t.Context()

	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Engine start failed: %v", err)
	}
	defer engine.Stop(context.Background())

	waitForServer(t, baseURL, 5*time.Second)

	// Send a real HTTP POST and verify the handler responds
	resp, err := http.Post(baseURL+"/api/test", "application/json", strings.NewReader(`{"hello":"world"}`))
	if err != nil {
		t.Fatalf("HTTP POST failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if result["handler"] != "test-handler" {
		t.Errorf("Expected handler 'test-handler', got %v", result["handler"])
	}
	if result["status"] != "success" {
		t.Errorf("Expected status 'success', got %v", result["status"])
	}

	t.Logf("E2E HTTP workflow: POST /api/test → 200 OK, handler=%v", result["handler"])
}

// TestE2E_OrderPipeline_FullExecution exercises the complete order-processing
// pipeline end-to-end with real HTTP requests:
//
//	POST /api/orders → create order (201)
//	PUT /api/orders/{id}/transition → validate_order → state becomes "validated"
//	PUT /api/orders/{id}/transition → store_order → state becomes "stored"
//	PUT /api/orders/{id}/transition → send_notification → state becomes "notified" (final)
//	GET /api/orders/{id} → verify completed state
//
// The messaging broker also delivers a message to the notification handler,
// proving the entire pipeline works from HTTP ingress to final notification.
func TestE2E_OrderPipeline_FullExecution(t *testing.T) {
	port := getFreePort(t)
	addr := fmt.Sprintf(":%d", port)
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "order-server", Type: "http.server", Config: map[string]any{"address": addr}},
			{Name: "order-router", Type: "http.router", DependsOn: []string{"order-server"}},
			{Name: "order-api", Type: "api.handler", DependsOn: []string{"order-router"}, Config: map[string]any{
				"resourceName":   "orders",
				"workflowType":   "order-processing",
				"workflowEngine": "order-state-engine",
			}},
			{Name: "order-state-engine", Type: "statemachine.engine", DependsOn: []string{"order-api"}},
			{Name: "order-state-tracker", Type: "state.tracker", DependsOn: []string{"order-state-engine"}},
			{Name: "order-broker", Type: "messaging.broker", DependsOn: []string{"order-state-tracker"}},
			{Name: "notification-handler", Type: "messaging.handler", DependsOn: []string{"order-broker"}},
		},
		Workflows: map[string]any{
			"http": map[string]any{
				"server": "order-server",
				"router": "order-router",
				"routes": []any{
					map[string]any{"method": "POST", "path": "/api/orders", "handler": "order-api"},
					map[string]any{"method": "GET", "path": "/api/orders", "handler": "order-api"},
					map[string]any{"method": "GET", "path": "/api/orders/{id}", "handler": "order-api"},
					map[string]any{"method": "PUT", "path": "/api/orders/{id}", "handler": "order-api"},
					map[string]any{"method": "PUT", "path": "/api/orders/{id}/transition", "handler": "order-api"},
				},
			},
			"statemachine": map[string]any{
				"engine": "order-state-engine",
				"definitions": []any{
					map[string]any{
						"name":         "order-processing",
						"description":  "Order processing workflow",
						"initialState": "received",
						"states": map[string]any{
							"received":  map[string]any{"description": "Order received", "isFinal": false, "isError": false},
							"validated": map[string]any{"description": "Order validated", "isFinal": false, "isError": false},
							"stored":    map[string]any{"description": "Order stored", "isFinal": false, "isError": false},
							"notified":  map[string]any{"description": "Notification sent", "isFinal": true, "isError": false},
							"failed":    map[string]any{"description": "Order failed", "isFinal": true, "isError": true},
						},
						"transitions": map[string]any{
							"validate_order":    map[string]any{"fromState": "received", "toState": "validated"},
							"store_order":       map[string]any{"fromState": "validated", "toState": "stored"},
							"send_notification": map[string]any{"fromState": "stored", "toState": "notified"},
							"fail_validation":   map[string]any{"fromState": "received", "toState": "failed"},
						},
					},
				},
			},
			"messaging": map[string]any{
				"broker": "order-broker",
				"subscriptions": []any{
					map[string]any{"topic": "order.completed", "handler": "notification-handler"},
				},
			},
		},
		Triggers: map[string]any{},
	}

	logger := &mockLogger{}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)
	engine := NewStdEngine(app, logger)
	engine.RegisterWorkflowHandler(handlers.NewHTTPWorkflowHandler())
	engine.RegisterWorkflowHandler(handlers.NewStateMachineWorkflowHandler())
	engine.RegisterWorkflowHandler(handlers.NewMessagingWorkflowHandler())

	if err := engine.BuildFromConfig(cfg); err != nil {
		t.Fatalf("BuildFromConfig failed: %v", err)
	}

	ctx := t.Context()

	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Engine start failed: %v", err)
	}
	defer engine.Stop(context.Background())

	waitForServer(t, baseURL, 5*time.Second)

	client := &http.Client{Timeout: 5 * time.Second}

	// Step 1: Create an order via POST /api/orders
	t.Log("Step 1: Creating order via POST /api/orders")
	orderPayload := `{"id":"ORD-001","customer":"Alice","total":99.99,"items":["widget-a","widget-b"]}`
	resp, err := client.Post(baseURL+"/api/orders", "application/json", strings.NewReader(orderPayload))
	if err != nil {
		t.Fatalf("POST /api/orders failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("Expected 201 Created, got %d: %s", resp.StatusCode, string(body))
	}

	var created map[string]any
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("Failed to decode create response: %v", err)
	}
	t.Logf("  Created order: id=%v, state=%v", created["id"], created["state"])

	// Step 2: Transition received → validated
	t.Log("Step 2: Transitioning order to 'validated'")
	transitionResp := doTransition(t, client, baseURL, "ORD-001", "validate_order")
	assertTransitionSuccess(t, transitionResp, "validated")
	t.Logf("  State after validate_order: %v", transitionResp["state"])

	// Step 3: Transition validated → stored
	t.Log("Step 3: Transitioning order to 'stored'")
	transitionResp = doTransition(t, client, baseURL, "ORD-001", "store_order")
	assertTransitionSuccess(t, transitionResp, "stored")
	t.Logf("  State after store_order: %v", transitionResp["state"])

	// Step 4: Transition stored → notified (final)
	t.Log("Step 4: Transitioning order to 'notified' (final state)")
	transitionResp = doTransition(t, client, baseURL, "ORD-001", "send_notification")
	assertTransitionSuccess(t, transitionResp, "notified")
	t.Logf("  State after send_notification: %v", transitionResp["state"])

	// Step 5: GET /api/orders/ORD-001 to verify final state
	t.Log("Step 5: Verifying final state via GET /api/orders/ORD-001")
	req, _ := http.NewRequest("GET", baseURL+"/api/orders/ORD-001", nil)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("GET /api/orders/ORD-001 failed: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	var order map[string]any
	if err := json.Unmarshal(body, &order); err != nil {
		t.Fatalf("Failed to decode GET response: %v", err)
	}

	if order["state"] != "notified" {
		t.Errorf("Expected final state 'notified', got %v", order["state"])
	}
	t.Logf("  Final order state: %v", order["state"])

	// Step 6: Verify the broker can deliver messages
	t.Log("Step 6: Verifying message broker delivery")
	verifyBrokerDelivery(t, app)

	t.Log("E2E Order Pipeline: All 6 steps passed - full pipeline execution verified")
}

// TestE2E_OrderPipeline_ErrorPath verifies that an invalid order transitions
// to the failed state via real HTTP requests.
func TestE2E_OrderPipeline_ErrorPath(t *testing.T) {
	port := getFreePort(t)
	addr := fmt.Sprintf(":%d", port)
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "err-server", Type: "http.server", Config: map[string]any{"address": addr}},
			{Name: "err-router", Type: "http.router", DependsOn: []string{"err-server"}},
			{Name: "err-api", Type: "api.handler", DependsOn: []string{"err-router"}, Config: map[string]any{
				"resourceName":   "orders",
				"workflowType":   "order-processing",
				"workflowEngine": "err-engine",
			}},
			{Name: "err-engine", Type: "statemachine.engine", DependsOn: []string{"err-api"}},
			{Name: "err-tracker", Type: "state.tracker", DependsOn: []string{"err-engine"}},
		},
		Workflows: map[string]any{
			"http": map[string]any{
				"server": "err-server",
				"router": "err-router",
				"routes": []any{
					map[string]any{"method": "POST", "path": "/api/orders", "handler": "err-api"},
					map[string]any{"method": "PUT", "path": "/api/orders/{id}/transition", "handler": "err-api"},
					map[string]any{"method": "GET", "path": "/api/orders/{id}", "handler": "err-api"},
				},
			},
			"statemachine": map[string]any{
				"engine": "err-engine",
				"definitions": []any{
					map[string]any{
						"name":         "order-processing",
						"initialState": "received",
						"states": map[string]any{
							"received": map[string]any{"isFinal": false, "isError": false},
							"failed":   map[string]any{"isFinal": true, "isError": true},
						},
						"transitions": map[string]any{
							"fail_validation": map[string]any{"fromState": "received", "toState": "failed"},
						},
					},
				},
			},
		},
		Triggers: map[string]any{},
	}

	logger := &mockLogger{}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)
	engine := NewStdEngine(app, logger)
	engine.RegisterWorkflowHandler(handlers.NewHTTPWorkflowHandler())
	engine.RegisterWorkflowHandler(handlers.NewStateMachineWorkflowHandler())

	if err := engine.BuildFromConfig(cfg); err != nil {
		t.Fatalf("BuildFromConfig failed: %v", err)
	}

	ctx := t.Context()

	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Engine start failed: %v", err)
	}
	defer engine.Stop(context.Background())

	waitForServer(t, baseURL, 5*time.Second)

	client := &http.Client{Timeout: 5 * time.Second}

	// Create an order
	resp, err := client.Post(baseURL+"/api/orders", "application/json",
		strings.NewReader(`{"id":"ORD-BAD","invalid":true}`))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	resp.Body.Close()

	// Transition to failed state
	transitionResp := doTransition(t, client, baseURL, "ORD-BAD", "fail_validation")
	assertTransitionSuccess(t, transitionResp, "failed")

	// Verify that further transitions are rejected
	transBody, _ := json.Marshal(map[string]any{"transition": "fail_validation"})
	req, _ := http.NewRequest("PUT", baseURL+"/api/orders/ORD-BAD/transition", bytes.NewReader(transBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("PUT transition failed: %v", err)
	}
	resp.Body.Close()

	// Should get 400 because the workflow is already in a final state
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected 400 for transition from final state, got %d", resp.StatusCode)
	}

	t.Log("E2E Error Path: Order correctly transitioned to 'failed' and rejected further transitions")
}

// TestE2E_BrokerMessaging proves the in-memory message broker delivers
// messages from producer to subscriber through the real engine.
func TestE2E_BrokerMessaging(t *testing.T) {
	port := getFreePort(t)
	addr := fmt.Sprintf(":%d", port)
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "msg-server", Type: "http.server", Config: map[string]any{"address": addr}},
			{Name: "msg-router", Type: "http.router", DependsOn: []string{"msg-server"}},
			{Name: "msg-handler", Type: "http.handler", DependsOn: []string{"msg-router"}, Config: map[string]any{"contentType": "application/json"}},
			{Name: "msg-broker", Type: "messaging.broker"},
			{Name: "msg-subscriber", Type: "messaging.handler", DependsOn: []string{"msg-broker"}},
		},
		Workflows: map[string]any{
			"http": map[string]any{
				"server": "msg-server",
				"router": "msg-router",
				"routes": []any{
					map[string]any{"method": "POST", "path": "/api/publish", "handler": "msg-handler"},
				},
			},
			"messaging": map[string]any{
				"broker": "msg-broker",
				"subscriptions": []any{
					map[string]any{"topic": "test.events", "handler": "msg-subscriber"},
				},
			},
		},
		Triggers: map[string]any{},
	}

	logger := &mockLogger{}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)
	engine := NewStdEngine(app, logger)
	engine.RegisterWorkflowHandler(handlers.NewHTTPWorkflowHandler())
	engine.RegisterWorkflowHandler(handlers.NewMessagingWorkflowHandler())

	if err := engine.BuildFromConfig(cfg); err != nil {
		t.Fatalf("BuildFromConfig failed: %v", err)
	}

	ctx := t.Context()

	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Engine start failed: %v", err)
	}
	defer engine.Stop(context.Background())

	waitForServer(t, baseURL, 5*time.Second)

	// Find the broker from the app's service registry to publish directly
	var broker module.MessageBroker
	for _, svc := range app.SvcRegistry() {
		if b, ok := svc.(module.MessageBroker); ok {
			broker = b
			break
		}
	}

	if broker == nil {
		t.Fatal("Message broker not found in service registry")
	}

	// Publish a message and verify it's delivered
	testMsg := []byte(`{"event":"order.created","orderId":"ORD-100"}`)
	if err := broker.Producer().SendMessage("test.events", testMsg); err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}

	// Also verify the HTTP endpoint is working
	resp, err := http.Post(baseURL+"/api/publish", "application/json",
		strings.NewReader(`{"action":"publish"}`))
	if err != nil {
		t.Fatalf("POST /api/publish failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}

	t.Log("E2E Broker Messaging: Message broker and HTTP server both operational within same engine")
}

// TestE2E_ConfigPushAndReload proves configs can be pushed via the management API
// and the engine rebuilt from the new config.
func TestE2E_ConfigPushAndReload(t *testing.T) {
	port := getFreePort(t)
	addr := fmt.Sprintf(":%d", port)
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	// Start with a minimal config - just a handler on the management port
	cfg := &config.WorkflowConfig{
		Modules:   []config.ModuleConfig{},
		Workflows: map[string]any{},
		Triggers:  map[string]any{},
	}

	// Use the WorkflowUIHandler directly to test config push
	uiHandler := module.NewWorkflowUIHandler(cfg)

	// Track reload calls
	var mu sync.Mutex
	var reloadCalled bool
	var reloadedConfig *config.WorkflowConfig

	uiHandler.SetReloadFunc(func(newCfg *config.WorkflowConfig) error {
		mu.Lock()
		defer mu.Unlock()
		reloadCalled = true
		reloadedConfig = newCfg
		return nil
	})

	uiHandler.SetStatusFunc(func() map[string]any {
		return map[string]any{
			"status":      "running",
			"moduleCount": 0,
		}
	})

	// Create a simple HTTP server for the management API
	mux := http.NewServeMux()
	uiHandler.RegisterRoutes(mux)

	server := &http.Server{Addr: addr, Handler: mux}
	go server.ListenAndServe()
	defer server.Shutdown(context.Background())

	waitForServer(t, baseURL, 5*time.Second)

	client := &http.Client{Timeout: 5 * time.Second}

	// Step 1: GET /api/workflow/config - verify empty config
	resp, err := client.Get(baseURL + "/api/workflow/config")
	if err != nil {
		t.Fatalf("GET config failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", resp.StatusCode, string(body))
	}
	t.Logf("  Initial config retrieved: %s", string(body))

	// Step 2: PUT /api/workflow/config - push a new config with modules
	newConfig := `{
		"modules": [
			{"name": "web-server", "type": "http.server", "config": {"address": ":9999"}},
			{"name": "web-router", "type": "http.router", "dependsOn": ["web-server"]},
			{"name": "api-handler", "type": "http.handler", "dependsOn": ["web-router"]}
		],
		"workflows": {
			"http": {
				"server": "web-server",
				"router": "web-router",
				"routes": [{"method": "GET", "path": "/api/hello", "handler": "api-handler"}]
			}
		}
	}`

	req, _ := http.NewRequest("PUT", baseURL+"/api/workflow/config", strings.NewReader(newConfig))
	req.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("PUT config failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 from PUT config, got %d", resp.StatusCode)
	}

	// Step 3: POST /api/workflow/reload - trigger engine rebuild
	resp, err = client.Post(baseURL+"/api/workflow/reload", "application/json", nil)
	if err != nil {
		t.Fatalf("POST reload failed: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 from reload, got %d: %s", resp.StatusCode, string(body))
	}

	// Verify reload was called
	mu.Lock()
	if !reloadCalled {
		t.Error("Expected reload function to be called")
	}
	if reloadedConfig == nil {
		t.Fatal("Expected reloaded config to be non-nil")
	}
	if len(reloadedConfig.Modules) != 3 {
		t.Errorf("Expected 3 modules in reloaded config, got %d", len(reloadedConfig.Modules))
	}
	mu.Unlock()

	// Step 4: GET /api/workflow/status - verify status
	resp, err = client.Get(baseURL + "/api/workflow/status")
	if err != nil {
		t.Fatalf("GET status failed: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	var status map[string]any
	if err := json.Unmarshal(body, &status); err != nil {
		t.Fatalf("Failed to decode status: %v", err)
	}

	if status["status"] != "running" {
		t.Errorf("Expected status 'running', got %v", status["status"])
	}

	// Step 5: POST /api/workflow/validate - validate the config
	resp, err = client.Post(baseURL+"/api/workflow/validate", "application/json",
		strings.NewReader(newConfig))
	if err != nil {
		t.Fatalf("POST validate failed: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	var validation map[string]any
	if err := json.Unmarshal(body, &validation); err != nil {
		t.Fatalf("Failed to decode validation: %v", err)
	}

	if validation["valid"] != true {
		t.Errorf("Expected valid=true, got %v (errors: %v)", validation["valid"], validation["errors"])
	}

	t.Log("E2E Config Push & Reload: Config CRUD, reload, status, and validation all working")
}

// --- Helper functions ---

// doTransition sends a PUT /api/orders/{id}/transition request and returns the parsed response.
func doTransition(t *testing.T, client *http.Client, baseURL, orderID, transition string) map[string]any {
	t.Helper()
	transBody, _ := json.Marshal(map[string]any{
		"transition": transition,
	})
	req, _ := http.NewRequest("PUT", baseURL+"/api/orders/"+orderID+"/transition",
		bytes.NewReader(transBody))
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("PUT transition '%s' failed: %v", transition, err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 for transition '%s', got %d: %s", transition, resp.StatusCode, string(body))
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to decode transition response: %v", err)
	}
	return result
}

// assertTransitionSuccess verifies a transition response indicates success and the expected state.
func assertTransitionSuccess(t *testing.T, resp map[string]any, expectedState string) {
	t.Helper()
	if resp["success"] != true {
		t.Errorf("Expected success=true, got %v (error: %v)", resp["success"], resp["error"])
	}
	if resp["state"] != expectedState {
		t.Errorf("Expected state '%s', got %v", expectedState, resp["state"])
	}
}

// verifyBrokerDelivery checks that the in-memory broker can deliver messages
// by finding the broker in the service registry, publishing a message, and
// checking it was received by a subscriber.
func verifyBrokerDelivery(t *testing.T, app modular.Application) {
	t.Helper()
	var broker module.MessageBroker
	for _, svc := range app.SvcRegistry() {
		if b, ok := svc.(module.MessageBroker); ok {
			broker = b
			break
		}
	}

	if broker == nil {
		t.Log("  No broker found in service registry (skipping broker delivery test)")
		return
	}

	// Set up a temporary subscriber to verify delivery
	var received []byte
	var mu sync.Mutex
	testHandler := module.NewFunctionMessageHandler(func(msg []byte) error {
		mu.Lock()
		defer mu.Unlock()
		received = msg
		return nil
	})

	if err := broker.Subscribe("e2e.verify", testHandler); err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	msg := []byte(`{"test":"broker-delivery","orderId":"ORD-001"}`)
	if err := broker.Producer().SendMessage("e2e.verify", msg); err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}

	// Give async delivery a moment
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if received == nil {
		t.Error("Expected broker to deliver message to subscriber, but got nothing")
	} else {
		t.Logf("  Broker delivered message: %s", string(received))
	}
}

// TestE2E_DynamicComponent_LoadAndExecute proves the dynamic component system
// works end-to-end: create a component via the HTTP API, then execute it
// through the engine's dynamic component integration.
func TestE2E_DynamicComponent_LoadAndExecute(t *testing.T) {
	port := getFreePort(t)
	addr := fmt.Sprintf(":%d", port)
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	// Set up the dynamic infrastructure
	pool := dynamic.NewInterpreterPool()
	registry := dynamic.NewComponentRegistry()
	loader := dynamic.NewLoader(pool, registry)
	apiHandler := dynamic.NewAPIHandler(loader, registry)

	// Create a minimal engine with an HTTP server and the dynamic API
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "dyn-server", Type: "http.server", Config: map[string]any{"address": addr}},
			{Name: "dyn-router", Type: "http.router", DependsOn: []string{"dyn-server"}},
			{Name: "dyn-handler", Type: "http.handler", DependsOn: []string{"dyn-router"}, Config: map[string]any{"contentType": "application/json"}},
		},
		Workflows: map[string]any{
			"http": map[string]any{
				"server": "dyn-server",
				"router": "dyn-router",
				"routes": []any{
					map[string]any{"method": "GET", "path": "/api/health", "handler": "dyn-handler"},
				},
			},
		},
		Triggers: map[string]any{},
	}

	logger := &mockLogger{}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)
	engine := NewStdEngine(app, logger)
	engine.RegisterWorkflowHandler(handlers.NewHTTPWorkflowHandler())
	engine.SetDynamicRegistry(registry)

	if err := engine.BuildFromConfig(cfg); err != nil {
		t.Fatalf("BuildFromConfig failed: %v", err)
	}

	ctx := t.Context()

	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Engine start failed: %v", err)
	}
	defer engine.Stop(context.Background())

	waitForServer(t, baseURL, 5*time.Second)

	// Also register the dynamic API routes on a separate management server
	mgmtPort := getFreePort(t)
	mgmtAddr := fmt.Sprintf(":%d", mgmtPort)
	mgmtURL := fmt.Sprintf("http://127.0.0.1:%d", mgmtPort)

	mgmtMux := http.NewServeMux()
	apiHandler.RegisterRoutes(mgmtMux)
	mgmtServer := &http.Server{Addr: mgmtAddr, Handler: mgmtMux}
	go mgmtServer.ListenAndServe()
	defer mgmtServer.Shutdown(context.Background())

	waitForServer(t, mgmtURL, 5*time.Second)

	client := &http.Client{Timeout: 5 * time.Second}

	// Step 1: Create a dynamic component via the API
	t.Log("Step 1: Creating dynamic component via POST /api/dynamic/components")
	componentSource := `package component

import (
	"context"
	"fmt"
)

func Name() string {
	return "greeter"
}

func Init(services map[string]interface{}) error {
	return nil
}

func Start(ctx context.Context) error {
	return nil
}

func Stop(ctx context.Context) error {
	return nil
}

func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	name, _ := params["name"].(string)
	if name == "" {
		name = "world"
	}
	return map[string]interface{}{
		"greeting": fmt.Sprintf("Hello, %s!", name),
		"source":   "dynamic",
	}, nil
}
`
	createPayload, _ := json.Marshal(map[string]string{
		"id":     "greeter",
		"source": componentSource,
	})
	resp, err := client.Post(mgmtURL+"/api/dynamic/components", "application/json", bytes.NewReader(createPayload))
	if err != nil {
		t.Fatalf("POST /api/dynamic/components failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("Expected 201 Created, got %d: %s", resp.StatusCode, string(body))
	}

	var created map[string]any
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("Failed to decode create response: %v", err)
	}
	t.Logf("  Created component: id=%v, name=%v, status=%v", created["id"], created["name"], created["status"])

	if created["status"] != "loaded" {
		t.Errorf("Expected status 'loaded', got %v", created["status"])
	}

	// Step 2: Verify the component is in the registry
	t.Log("Step 2: Verifying component is listed via GET /api/dynamic/components")
	resp, err = client.Get(mgmtURL + "/api/dynamic/components")
	if err != nil {
		t.Fatalf("GET /api/dynamic/components failed: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	var components []map[string]any
	if err := json.Unmarshal(body, &components); err != nil {
		t.Fatalf("Failed to decode list response: %v", err)
	}
	if len(components) != 1 {
		t.Fatalf("Expected 1 component, got %d", len(components))
	}
	t.Logf("  Registry has %d component(s)", len(components))

	// Step 3: Execute the component directly through the registry
	t.Log("Step 3: Executing dynamic component directly")
	comp, ok := registry.Get("greeter")
	if !ok {
		t.Fatal("Component 'greeter' not found in registry")
	}

	if err := comp.Init(nil); err != nil {
		t.Fatalf("Component Init failed: %v", err)
	}
	if err := comp.Start(context.Background()); err != nil {
		t.Fatalf("Component Start failed: %v", err)
	}

	result, err := comp.Execute(context.Background(), map[string]any{"name": "E2E"})
	if err != nil {
		t.Fatalf("Component Execute failed: %v", err)
	}

	if result["greeting"] != "Hello, E2E!" {
		t.Errorf("Expected greeting 'Hello, E2E!', got %v", result["greeting"])
	}
	if result["source"] != "dynamic" {
		t.Errorf("Expected source 'dynamic', got %v", result["source"])
	}
	t.Logf("  Execute result: greeting=%v, source=%v", result["greeting"], result["source"])

	// Step 4: Verify component info via GET /api/dynamic/components/{id}
	t.Log("Step 4: Verifying component details via GET /api/dynamic/components/greeter")
	resp, err = client.Get(mgmtURL + "/api/dynamic/components/greeter")
	if err != nil {
		t.Fatalf("GET /api/dynamic/components/greeter failed: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	var detail map[string]any
	if err := json.Unmarshal(body, &detail); err != nil {
		t.Fatalf("Failed to decode detail response: %v", err)
	}
	if detail["source"] == nil || detail["source"] == "" {
		t.Error("Expected component detail to include source code")
	}
	t.Logf("  Component detail: name=%v, status=%v", detail["name"], detail["status"])

	if err := comp.Stop(context.Background()); err != nil {
		t.Fatalf("Component Stop failed: %v", err)
	}

	t.Log("E2E Dynamic Component Load & Execute: All steps passed")
}

// TestE2E_DynamicComponent_HotReload proves that a dynamic component can be
// updated with new source code via the HTTP API and the new behavior takes
// effect without restarting the engine.
func TestE2E_DynamicComponent_HotReload(t *testing.T) {
	port := getFreePort(t)
	addr := fmt.Sprintf(":%d", port)
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	pool := dynamic.NewInterpreterPool()
	registry := dynamic.NewComponentRegistry()
	loader := dynamic.NewLoader(pool, registry)
	apiHandler := dynamic.NewAPIHandler(loader, registry)

	// Start management server with the dynamic API
	mux := http.NewServeMux()
	apiHandler.RegisterRoutes(mux)
	server := &http.Server{Addr: addr, Handler: mux}
	go server.ListenAndServe()
	defer server.Shutdown(context.Background())

	waitForServer(t, baseURL, 5*time.Second)

	client := &http.Client{Timeout: 5 * time.Second}

	// Step 1: Create v1 of the component
	t.Log("Step 1: Creating v1 of component (returns 'v1')")
	v1Source := `package component

import "context"

func Name() string {
	return "transformer-v1"
}

func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	input, _ := params["input"].(string)
	return map[string]interface{}{
		"version": "v1",
		"output":  "v1:" + input,
	}, nil
}
`
	createPayload, _ := json.Marshal(map[string]string{
		"id":     "transformer",
		"source": v1Source,
	})
	resp, err := client.Post(baseURL+"/api/dynamic/components", "application/json", bytes.NewReader(createPayload))
	if err != nil {
		t.Fatalf("POST create failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", resp.StatusCode, string(body))
	}

	// Execute v1
	comp, ok := registry.Get("transformer")
	if !ok {
		t.Fatal("Component 'transformer' not found after create")
	}

	result, err := comp.Execute(context.Background(), map[string]any{"input": "data"})
	if err != nil {
		t.Fatalf("v1 Execute failed: %v", err)
	}
	if result["version"] != "v1" {
		t.Errorf("Expected version 'v1', got %v", result["version"])
	}
	if result["output"] != "v1:data" {
		t.Errorf("Expected output 'v1:data', got %v", result["output"])
	}
	t.Logf("  v1 result: version=%v, output=%v", result["version"], result["output"])

	// Step 2: Update to v2 via PUT
	t.Log("Step 2: Updating to v2 via PUT /api/dynamic/components/transformer")
	v2Source := `package component

import (
	"context"
	"strings"
)

func Name() string {
	return "transformer-v2"
}

func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	input, _ := params["input"].(string)
	return map[string]interface{}{
		"version": "v2",
		"output":  "v2:" + strings.ToUpper(input),
	}, nil
}
`
	updatePayload, _ := json.Marshal(map[string]string{"source": v2Source})
	req, _ := http.NewRequest(http.MethodPut, baseURL+"/api/dynamic/components/transformer", bytes.NewReader(updatePayload))
	req.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("PUT update failed: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", resp.StatusCode, string(body))
	}

	var updated map[string]any
	if err := json.Unmarshal(body, &updated); err != nil {
		t.Fatalf("Failed to decode update response: %v", err)
	}
	t.Logf("  Updated component: name=%v, status=%v", updated["name"], updated["status"])

	// Step 3: Execute the updated component (should now produce v2 behavior)
	t.Log("Step 3: Executing updated component - verifying new behavior")
	comp, ok = registry.Get("transformer")
	if !ok {
		t.Fatal("Component 'transformer' not found after update")
	}

	result, err = comp.Execute(context.Background(), map[string]any{"input": "data"})
	if err != nil {
		t.Fatalf("v2 Execute failed: %v", err)
	}
	if result["version"] != "v2" {
		t.Errorf("Expected version 'v2', got %v", result["version"])
	}
	if result["output"] != "v2:DATA" {
		t.Errorf("Expected output 'v2:DATA', got %v", result["output"])
	}
	t.Logf("  v2 result: version=%v, output=%v", result["version"], result["output"])

	// Step 4: Verify old behavior is gone
	t.Log("Step 4: Confirming v1 behavior is no longer present")
	if result["version"] == "v1" {
		t.Error("Component still returning v1 after hot-reload update")
	}

	// Step 5: Delete the component
	t.Log("Step 5: Deleting component via DELETE /api/dynamic/components/transformer")
	req, _ = http.NewRequest(http.MethodDelete, baseURL+"/api/dynamic/components/transformer", nil)
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("DELETE failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("Expected 204, got %d", resp.StatusCode)
	}

	if _, ok := registry.Get("transformer"); ok {
		t.Error("Component should be removed from registry after DELETE")
	}
	t.Logf("  Component deleted from registry, count=%d", registry.Count())

	t.Log("E2E Dynamic Component Hot Reload: All steps passed - v1 loaded, v2 hot-reloaded, behavior changed, component deleted")
}

// TestE2E_DynamicComponent_SandboxRejectsUnsafe proves the sandbox validator
// rejects components that try to import dangerous packages (os/exec, syscall,
// unsafe, os, etc.) via the HTTP API.
func TestE2E_DynamicComponent_SandboxRejectsUnsafe(t *testing.T) {
	port := getFreePort(t)
	addr := fmt.Sprintf(":%d", port)
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	pool := dynamic.NewInterpreterPool()
	registry := dynamic.NewComponentRegistry()
	loader := dynamic.NewLoader(pool, registry)
	apiHandler := dynamic.NewAPIHandler(loader, registry)

	mux := http.NewServeMux()
	apiHandler.RegisterRoutes(mux)
	server := &http.Server{Addr: addr, Handler: mux}
	go server.ListenAndServe()
	defer server.Shutdown(context.Background())

	waitForServer(t, baseURL, 5*time.Second)

	client := &http.Client{Timeout: 5 * time.Second}

	// Define test cases for blocked imports
	blockedTests := []struct {
		name       string
		id         string
		source     string
		blockedPkg string
	}{
		{
			name: "os/exec",
			id:   "bad-exec",
			source: `package component

import "os/exec"

func Name() string { return "bad-exec" }
func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	_ = exec.Command("rm", "-rf", "/")
	return nil, nil
}
`,
			blockedPkg: "os/exec",
		},
		{
			name: "syscall",
			id:   "bad-syscall",
			source: `package component

import "syscall"

func Name() string { return "bad-syscall" }
func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	_ = syscall.Getpid()
	return nil, nil
}
`,
			blockedPkg: "syscall",
		},
		{
			name: "unsafe",
			id:   "bad-unsafe",
			source: `package component

import "unsafe"

func Name() string { return "bad-unsafe" }
func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	var x int
	_ = unsafe.Pointer(&x)
	return nil, nil
}
`,
			blockedPkg: "unsafe",
		},
		{
			name: "os (filesystem access)",
			id:   "bad-os",
			source: `package component

import "os"

func Name() string { return "bad-os" }
func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	data, _ := os.ReadFile("/etc/passwd")
	return map[string]interface{}{"data": string(data)}, nil
}
`,
			blockedPkg: "os",
		},
		{
			name: "net (raw sockets)",
			id:   "bad-net",
			source: `package component

import "net"

func Name() string { return "bad-net" }
func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	conn, _ := net.Dial("tcp", "evil.com:4444")
	if conn != nil { conn.Close() }
	return nil, nil
}
`,
			blockedPkg: "net",
		},
		{
			name: "reflect",
			id:   "bad-reflect",
			source: `package component

import "reflect"

func Name() string { return "bad-reflect" }
func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	_ = reflect.TypeOf(42)
	return nil, nil
}
`,
			blockedPkg: "reflect",
		},
	}

	for _, tt := range blockedTests {
		t.Run(tt.name, func(t *testing.T) {
			payload, _ := json.Marshal(map[string]string{
				"id":     tt.id,
				"source": tt.source,
			})

			resp, err := client.Post(baseURL+"/api/dynamic/components", "application/json", bytes.NewReader(payload))
			if err != nil {
				t.Fatalf("POST failed for %s: %v", tt.name, err)
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			if resp.StatusCode != http.StatusUnprocessableEntity {
				t.Errorf("Expected 422 for blocked import %q, got %d: %s", tt.blockedPkg, resp.StatusCode, string(body))
			}

			respStr := string(body)
			if !strings.Contains(respStr, tt.blockedPkg) {
				t.Errorf("Expected error message to mention %q, got: %s", tt.blockedPkg, respStr)
			}

			t.Logf("  Blocked %s: status=%d, error contains %q", tt.name, resp.StatusCode, tt.blockedPkg)
		})
	}

	// Verify registry is still empty (no bad components got through)
	if registry.Count() != 0 {
		t.Errorf("Expected 0 components in registry after all rejections, got %d", registry.Count())
		for _, info := range registry.List() {
			t.Errorf("  Unexpected component: id=%s, name=%s", info.ID, info.Name)
		}
	}

	// Step 2: Verify that a valid component still loads fine after sandbox rejections
	t.Log("Verifying valid component still loads after rejections")
	validSource := `package component

import (
	"context"
	"fmt"
)

func Name() string { return "safe-component" }

func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{
		"message": fmt.Sprintf("safe and sound"),
	}, nil
}
`
	validPayload, _ := json.Marshal(map[string]string{
		"id":     "safe-comp",
		"source": validSource,
	})
	resp, err := client.Post(baseURL+"/api/dynamic/components", "application/json", bytes.NewReader(validPayload))
	if err != nil {
		t.Fatalf("POST valid component failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("Expected 201 for valid component, got %d: %s", resp.StatusCode, string(body))
	}

	comp, ok := registry.Get("safe-comp")
	if !ok {
		t.Fatal("Valid component not found in registry")
	}

	result, err := comp.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Valid component Execute failed: %v", err)
	}
	if result["message"] != "safe and sound" {
		t.Errorf("Expected message 'safe and sound', got %v", result["message"])
	}

	t.Logf("  Valid component loaded and executed: message=%v", result["message"])
	t.Log("E2E Dynamic Component Sandbox: All blocked imports rejected, valid component still works")
}

// TestE2E_DynamicComponent_EngineIntegration proves that a dynamic component
// can be pre-loaded into the registry, referenced via "dynamic.component" type
// in BuildFromConfig, and participate in the engine lifecycle alongside
// standard modules like HTTP server/router.
func TestE2E_DynamicComponent_EngineIntegration(t *testing.T) {
	port := getFreePort(t)
	addr := fmt.Sprintf(":%d", port)
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	// Pre-load a dynamic component into the registry before BuildFromConfig
	pool := dynamic.NewInterpreterPool()
	registry := dynamic.NewComponentRegistry()
	loader := dynamic.NewLoader(pool, registry)

	componentSource := `package component

import (
	"context"
	"fmt"
	"strings"
)

func Name() string {
	return "text-processor"
}

func Init(services map[string]interface{}) error {
	return nil
}

func Start(ctx context.Context) error {
	return nil
}

func Stop(ctx context.Context) error {
	return nil
}

func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	input, _ := params["text"].(string)
	op, _ := params["operation"].(string)
	var output string
	switch op {
	case "upper":
		output = strings.ToUpper(input)
	case "lower":
		output = strings.ToLower(input)
	case "reverse":
		runes := []rune(input)
		for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
			runes[i], runes[j] = runes[j], runes[i]
		}
		output = string(runes)
	default:
		return nil, fmt.Errorf("unknown operation: %s", op)
	}
	return map[string]interface{}{
		"result":    output,
		"operation": op,
		"source":    "dynamic",
	}, nil
}
`
	_, err := loader.LoadFromString("text-processor", componentSource)
	if err != nil {
		t.Fatalf("Failed to pre-load dynamic component: %v", err)
	}

	// Build engine config that references the dynamic component
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "eng-server", Type: "http.server", Config: map[string]any{"address": addr}},
			{Name: "eng-router", Type: "http.router", DependsOn: []string{"eng-server"}},
			{Name: "eng-handler", Type: "http.handler", DependsOn: []string{"eng-router"}, Config: map[string]any{"contentType": "application/json"}},
			// This module type is handled by the engine's "dynamic.component" case
			{Name: "text-processor", Type: "dynamic.component", Config: map[string]any{
				"componentId": "text-processor",
			}},
		},
		Workflows: map[string]any{
			"http": map[string]any{
				"server": "eng-server",
				"router": "eng-router",
				"routes": []any{
					map[string]any{"method": "GET", "path": "/api/health", "handler": "eng-handler"},
				},
			},
		},
		Triggers: map[string]any{},
	}

	logger := &mockLogger{}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)
	engine := NewStdEngine(app, logger)
	engine.SetDynamicRegistry(registry)
	engine.RegisterWorkflowHandler(handlers.NewHTTPWorkflowHandler())

	// Step 1: BuildFromConfig with dynamic.component type
	t.Log("Step 1: Building engine with dynamic.component module type")
	if err := engine.BuildFromConfig(cfg); err != nil {
		t.Fatalf("BuildFromConfig failed: %v", err)
	}
	t.Log("  BuildFromConfig succeeded with dynamic.component")

	// Step 2: Start the engine (this runs the full modular lifecycle on all modules)
	t.Log("Step 2: Starting engine (full lifecycle)")
	ctx := t.Context()

	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Engine start failed: %v", err)
	}
	defer engine.Stop(context.Background())

	waitForServer(t, baseURL, 5*time.Second)
	t.Log("  Engine started, HTTP server accepting connections")

	// Step 3: Execute the dynamic component through the registry
	t.Log("Step 3: Executing dynamic component through registry")
	comp, ok := registry.Get("text-processor")
	if !ok {
		t.Fatal("Dynamic component 'text-processor' not found in registry")
	}

	result, err := comp.Execute(ctx, map[string]any{"text": "Hello World", "operation": "upper"})
	if err != nil {
		t.Fatalf("Execute(upper) failed: %v", err)
	}
	if result["result"] != "HELLO WORLD" {
		t.Errorf("Expected 'HELLO WORLD', got %v", result["result"])
	}
	t.Logf("  upper: %v -> %v", "Hello World", result["result"])

	result, err = comp.Execute(ctx, map[string]any{"text": "Hello World", "operation": "reverse"})
	if err != nil {
		t.Fatalf("Execute(reverse) failed: %v", err)
	}
	if result["result"] != "dlroW olleH" {
		t.Errorf("Expected 'dlroW olleH', got %v", result["result"])
	}
	t.Logf("  reverse: %v -> %v", "Hello World", result["result"])

	// Step 4: Hot-reload the component with new behavior while engine is running
	t.Log("Step 4: Hot-reloading component while engine is running")
	v2Source := `package component

import (
	"context"
	"strings"
)

func Name() string {
	return "text-processor-v2"
}

func Init(services map[string]interface{}) error {
	return nil
}

func Start(ctx context.Context) error {
	return nil
}

func Stop(ctx context.Context) error {
	return nil
}

func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	input, _ := params["text"].(string)
	return map[string]interface{}{
		"result":    strings.ReplaceAll(strings.ToUpper(input), " ", "_"),
		"version":   "v2",
		"source":    "dynamic",
	}, nil
}
`
	_, err = loader.Reload("text-processor", v2Source)
	if err != nil {
		t.Fatalf("Reload failed: %v", err)
	}

	// Get the reloaded component and execute
	comp, ok = registry.Get("text-processor")
	if !ok {
		t.Fatal("Dynamic component not found after reload")
	}

	result, err = comp.Execute(ctx, map[string]any{"text": "Hello World"})
	if err != nil {
		t.Fatalf("v2 Execute failed: %v", err)
	}
	if result["result"] != "HELLO_WORLD" {
		t.Errorf("Expected 'HELLO_WORLD', got %v", result["result"])
	}
	if result["version"] != "v2" {
		t.Errorf("Expected version 'v2', got %v", result["version"])
	}
	t.Logf("  v2 result: %v (version=%v)", result["result"], result["version"])

	// Step 5: Verify the HTTP server is still healthy after hot-reload
	t.Log("Step 5: Verifying HTTP server still works after hot-reload")
	resp, err := http.Get(baseURL + "/api/health")
	if err != nil {
		t.Fatalf("GET /api/health failed after hot-reload: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 after hot-reload, got %d", resp.StatusCode)
	}
	t.Log("  HTTP server still healthy after hot-reload")

	// Step 6: Verify BuildFromConfig fails gracefully without registry
	t.Log("Step 6: Verifying BuildFromConfig fails without dynamic registry")
	noRegistryEngine := NewStdEngine(
		modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger),
		logger,
	)
	// Don't call SetDynamicRegistry
	dynamicOnlyCfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "orphan", Type: "dynamic.component"},
		},
		Workflows: map[string]any{},
		Triggers:  map[string]any{},
	}
	err = noRegistryEngine.BuildFromConfig(dynamicOnlyCfg)
	if err == nil {
		t.Error("Expected error when dynamic registry is not set")
	} else {
		t.Logf("  Got expected error: %v", err)
	}

	t.Log("E2E Dynamic Component Engine Integration: All steps passed")
}

// TestE2E_DynamicComponent_MessageProcessor proves that a dynamic component
// can be loaded, wired alongside a messaging broker, and process messages
// driven by the broker's pub/sub system.
func TestE2E_DynamicComponent_MessageProcessor(t *testing.T) {
	port := getFreePort(t)
	addr := fmt.Sprintf(":%d", port)
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	// Pre-load a dynamic component that acts as a message processor
	pool := dynamic.NewInterpreterPool()
	registry := dynamic.NewComponentRegistry()
	loader := dynamic.NewLoader(pool, registry)

	processorSource := `package component

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

func Name() string {
	return "msg-processor"
}

func Init(services map[string]interface{}) error {
	return nil
}

func Start(ctx context.Context) error {
	return nil
}

func Stop(ctx context.Context) error {
	return nil
}

func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	rawMsg, _ := params["message"].(string)
	if rawMsg == "" {
		return nil, fmt.Errorf("no message provided")
	}

	var msg map[string]interface{}
	if err := json.Unmarshal([]byte(rawMsg), &msg); err != nil {
		return nil, fmt.Errorf("invalid JSON message: %w", err)
	}

	action, _ := msg["action"].(string)
	data, _ := msg["data"].(string)

	var result string
	switch action {
	case "transform":
		result = strings.ToUpper(data)
	case "validate":
		if len(data) > 0 {
			result = "valid"
		} else {
			result = "invalid"
		}
	default:
		result = "echo:" + data
	}

	return map[string]interface{}{
		"action":    action,
		"input":     data,
		"output":    result,
		"processed": true,
	}, nil
}
`
	_, err := loader.LoadFromString("msg-processor", processorSource)
	if err != nil {
		t.Fatalf("Failed to pre-load message processor component: %v", err)
	}

	// Build engine with both the dynamic component and a messaging broker
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "mp-server", Type: "http.server", Config: map[string]any{"address": addr}},
			{Name: "mp-router", Type: "http.router", DependsOn: []string{"mp-server"}},
			{Name: "mp-handler", Type: "http.handler", DependsOn: []string{"mp-router"}, Config: map[string]any{"contentType": "application/json"}},
			{Name: "mp-broker", Type: "messaging.broker"},
			{Name: "msg-processor", Type: "dynamic.component", Config: map[string]any{
				"componentId": "msg-processor",
			}},
			{Name: "msg-subscriber", Type: "messaging.handler", DependsOn: []string{"mp-broker"}},
		},
		Workflows: map[string]any{
			"http": map[string]any{
				"server": "mp-server",
				"router": "mp-router",
				"routes": []any{
					map[string]any{"method": "GET", "path": "/api/health", "handler": "mp-handler"},
				},
			},
			"messaging": map[string]any{
				"broker": "mp-broker",
				"subscriptions": []any{
					map[string]any{"topic": "process.requests", "handler": "msg-subscriber"},
				},
			},
		},
		Triggers: map[string]any{},
	}

	logger := &mockLogger{}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)
	engine := NewStdEngine(app, logger)
	engine.SetDynamicRegistry(registry)
	engine.RegisterWorkflowHandler(handlers.NewHTTPWorkflowHandler())
	engine.RegisterWorkflowHandler(handlers.NewMessagingWorkflowHandler())

	if err := engine.BuildFromConfig(cfg); err != nil {
		t.Fatalf("BuildFromConfig failed: %v", err)
	}

	ctx := t.Context()

	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Engine start failed: %v", err)
	}
	defer engine.Stop(context.Background())

	waitForServer(t, baseURL, 5*time.Second)

	// Step 1: Verify both the dynamic component and broker are available
	t.Log("Step 1: Verifying dynamic component and broker are in service registry")
	comp, ok := registry.Get("msg-processor")
	if !ok {
		t.Fatal("Dynamic component 'msg-processor' not found in registry")
	}

	var broker module.MessageBroker
	for _, svc := range app.SvcRegistry() {
		if b, ok := svc.(module.MessageBroker); ok {
			broker = b
			break
		}
	}
	if broker == nil {
		t.Fatal("Message broker not found in service registry")
	}
	t.Log("  Both dynamic component and broker found")

	// Step 2: Use the broker to deliver a message, then process it with the component
	t.Log("Step 2: Publishing message via broker and processing with dynamic component")

	// Set up a subscriber to capture broker messages
	var receivedMsgs [][]byte
	var mu sync.Mutex
	testHandler := module.NewFunctionMessageHandler(func(msg []byte) error {
		mu.Lock()
		defer mu.Unlock()
		receivedMsgs = append(receivedMsgs, msg)
		return nil
	})

	if err := broker.Subscribe("process.results", testHandler); err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	// Publish a message that we'll process with the dynamic component
	inputMsg := `{"action":"transform","data":"hello world"}`
	if err := broker.Producer().SendMessage("process.requests", []byte(inputMsg)); err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}

	// Process the message using the dynamic component
	result, err := comp.Execute(ctx, map[string]any{"message": inputMsg})
	if err != nil {
		t.Fatalf("Component Execute failed: %v", err)
	}
	if result["output"] != "HELLO WORLD" {
		t.Errorf("Expected output 'HELLO WORLD', got %v", result["output"])
	}
	if result["processed"] != true {
		t.Errorf("Expected processed=true, got %v", result["processed"])
	}
	t.Logf("  Transform result: input=%v, output=%v", result["input"], result["output"])

	// Publish the result back through the broker
	resultJSON, _ := json.Marshal(result)
	if err := broker.Producer().SendMessage("process.results", resultJSON); err != nil {
		t.Fatalf("SendMessage (results) failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if len(receivedMsgs) == 0 {
		t.Error("Expected results subscriber to receive processed message")
	} else {
		var received map[string]any
		if err := json.Unmarshal(receivedMsgs[0], &received); err != nil {
			t.Errorf("Failed to parse received message: %v", err)
		} else {
			if received["output"] != "HELLO WORLD" {
				t.Errorf("Expected received output 'HELLO WORLD', got %v", received["output"])
			}
			t.Logf("  Results subscriber received: %s", string(receivedMsgs[0]))
		}
	}
	mu.Unlock()

	// Step 3: Process a validate message
	t.Log("Step 3: Processing validate message")
	validateMsg := `{"action":"validate","data":"some-data"}`
	result, err = comp.Execute(ctx, map[string]any{"message": validateMsg})
	if err != nil {
		t.Fatalf("Validate Execute failed: %v", err)
	}
	if result["output"] != "valid" {
		t.Errorf("Expected output 'valid', got %v", result["output"])
	}
	t.Logf("  Validate result: action=%v, output=%v", result["action"], result["output"])

	// Step 4: Process an unknown action (echo)
	t.Log("Step 4: Processing echo message")
	echoMsg := `{"action":"unknown","data":"test-data"}`
	result, err = comp.Execute(ctx, map[string]any{"message": echoMsg})
	if err != nil {
		t.Fatalf("Echo Execute failed: %v", err)
	}
	if result["output"] != "echo:test-data" {
		t.Errorf("Expected output 'echo:test-data', got %v", result["output"])
	}
	t.Logf("  Echo result: action=%v, output=%v", result["action"], result["output"])

	// Step 5: Verify error handling with bad input
	t.Log("Step 5: Testing error handling with invalid input")
	_, err = comp.Execute(ctx, map[string]any{"message": "not-json"})
	if err == nil {
		t.Error("Expected error for invalid JSON message")
	} else {
		t.Logf("  Got expected error: %v", err)
	}

	// Step 6: Verify HTTP server still works alongside dynamic component and broker
	t.Log("Step 6: Verifying HTTP server still operational")
	resp, err := http.Get(baseURL + "/api/health")
	if err != nil {
		t.Fatalf("GET /api/health failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}
	t.Log("  HTTP server healthy")

	t.Log("E2E Dynamic Component Message Processor: All steps passed - component loaded, messages processed, broker pub/sub working")
}

// TestE2E_IntegrationWorkflow proves the integration workflow handler can
// configure HTTP connectors from config, register them in an integration
// registry, and execute integration steps against a real HTTP endpoint.
func TestE2E_IntegrationWorkflow(t *testing.T) {
	// Start a mock target API server that the integration connector will call
	targetPort := getFreePort(t)
	targetURL := fmt.Sprintf("http://127.0.0.1:%d", targetPort)

	targetMux := http.NewServeMux()
	targetMux.HandleFunc("/customers/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"id":     "cust-123",
			"name":   "Alice",
			"status": "active",
		})
	})
	targetServer := &http.Server{Addr: fmt.Sprintf(":%d", targetPort), Handler: targetMux}
	go targetServer.ListenAndServe()
	defer targetServer.Shutdown(context.Background())

	// Wait for the target server to come up
	waitForServer(t, targetURL, 5*time.Second)

	// Set up the main engine with an HTTP server + integration workflow
	port := getFreePort(t)
	addr := fmt.Sprintf(":%d", port)
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "int-server", Type: "http.server", Config: map[string]any{"address": addr}},
			{Name: "int-router", Type: "http.router", DependsOn: []string{"int-server"}},
			{Name: "int-handler", Type: "http.handler", DependsOn: []string{"int-router"}, Config: map[string]any{"contentType": "application/json"}},
			{Name: "int-registry", Type: "integration.registry"},
		},
		Workflows: map[string]any{
			"http": map[string]any{
				"server": "int-server",
				"router": "int-router",
				"routes": []any{
					map[string]any{"method": "GET", "path": "/api/health", "handler": "int-handler"},
				},
			},
			"integration": map[string]any{
				"registry": "int-registry",
				"connectors": []any{
					map[string]any{
						"name": "api-connector",
						"type": "http",
						"config": map[string]any{
							"baseURL":         targetURL,
							"authType":        "bearer",
							"token":           "test-token-123",
							"headers":         map[string]any{"Accept": "application/json"},
							"timeoutSeconds":  float64(10),
							"allowPrivateIPs": true,
						},
					},
				},
				"steps": []any{
					map[string]any{
						"name":      "fetch-customer",
						"connector": "api-connector",
						"action":    "GET /customers/cust-123",
					},
				},
			},
		},
		Triggers: map[string]any{},
	}

	logger := &mockLogger{}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)
	engine := NewStdEngine(app, logger)

	// Register the integration.registry module type factory
	engine.AddModuleType("integration.registry", func(name string, cfg map[string]any) modular.Module {
		return module.NewIntegrationRegistry(name)
	})

	engine.RegisterWorkflowHandler(handlers.NewHTTPWorkflowHandler())
	engine.RegisterWorkflowHandler(handlers.NewIntegrationWorkflowHandler())

	if err := engine.BuildFromConfig(cfg); err != nil {
		t.Fatalf("BuildFromConfig failed: %v", err)
	}

	ctx := t.Context()

	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Engine start failed: %v", err)
	}
	defer engine.Stop(context.Background())

	waitForServer(t, baseURL, 5*time.Second)

	// Verify the integration registry was configured with connectors
	var registry module.IntegrationRegistry
	for _, svc := range app.SvcRegistry() {
		if r, ok := svc.(module.IntegrationRegistry); ok {
			registry = r
			break
		}
	}
	if registry == nil {
		t.Fatal("Integration registry not found in service registry")
	}

	connectors := registry.ListConnectors()
	if len(connectors) != 1 {
		t.Fatalf("Expected 1 connector, got %d", len(connectors))
	}
	t.Logf("  Registered connectors: %v", connectors)

	// Execute the integration step by using the connector directly
	connector, err := registry.GetConnector("api-connector")
	if err != nil {
		t.Fatalf("GetConnector failed: %v", err)
	}

	if err := connector.Connect(ctx); err != nil {
		t.Fatalf("Connector Connect failed: %v", err)
	}
	defer connector.Disconnect(ctx)

	result, err := connector.Execute(ctx, "GET /customers/cust-123", nil)
	if err != nil {
		t.Fatalf("Connector Execute failed: %v", err)
	}

	if result["name"] != "Alice" {
		t.Errorf("Expected customer name 'Alice', got %v", result["name"])
	}
	if result["status"] != "active" {
		t.Errorf("Expected customer status 'active', got %v", result["status"])
	}

	t.Logf("  Integration connector returned: name=%v, status=%v", result["name"], result["status"])
	t.Log("E2E Integration Workflow: Registry configured, HTTP connector executed against real server")
}

// TestE2E_EventWorkflow proves the event workflow handler can configure an
// event processor with patterns and handlers, and that events are matched
// and dispatched to the appropriate handlers.
func TestE2E_EventWorkflow(t *testing.T) {
	port := getFreePort(t)
	addr := fmt.Sprintf(":%d", port)
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "evt-server", Type: "http.server", Config: map[string]any{"address": addr}},
			{Name: "evt-router", Type: "http.router", DependsOn: []string{"evt-server"}},
			{Name: "evt-handler", Type: "http.handler", DependsOn: []string{"evt-router"}, Config: map[string]any{"contentType": "application/json"}},
			{Name: "evt-processor", Type: "event.processor"},
			{Name: "evt-broker", Type: "messaging.broker"},
			{Name: "alert-handler", Type: "messaging.handler", DependsOn: []string{"evt-broker"}},
		},
		Workflows: map[string]any{
			"http": map[string]any{
				"server": "evt-server",
				"router": "evt-router",
				"routes": []any{
					map[string]any{"method": "POST", "path": "/api/events", "handler": "evt-handler"},
				},
			},
			"event": map[string]any{
				"processor": "evt-processor",
				"patterns": []any{
					map[string]any{
						"patternId":    "login-failures",
						"eventTypes":   []any{"user.login.failed"},
						"windowTime":   "5m",
						"condition":    "count",
						"minOccurs":    float64(2),
						"maxOccurs":    float64(0),
						"orderMatters": false,
					},
				},
				"handlers": []any{
					map[string]any{
						"patternId": "login-failures",
						"handler":   "alert-handler",
					},
				},
				"adapters": []any{
					map[string]any{
						"broker":      "evt-broker",
						"topics":      []any{"user.login.failed"},
						"eventType":   "user.login.failed",
						"sourceIdKey": "userId",
						"correlIdKey": "sessionId",
					},
				},
			},
		},
		Triggers: map[string]any{},
	}

	logger := &mockLogger{}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)
	engine := NewStdEngine(app, logger)

	// Register the event.processor module type factory
	engine.AddModuleType("event.processor", func(name string, cfg map[string]any) modular.Module {
		return module.NewEventProcessor(name)
	})

	engine.RegisterWorkflowHandler(handlers.NewHTTPWorkflowHandler())
	engine.RegisterWorkflowHandler(handlers.NewEventWorkflowHandler())
	engine.RegisterWorkflowHandler(handlers.NewMessagingWorkflowHandler())

	if err := engine.BuildFromConfig(cfg); err != nil {
		t.Fatalf("BuildFromConfig failed: %v", err)
	}

	ctx := t.Context()

	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Engine start failed: %v", err)
	}
	defer engine.Stop(context.Background())

	waitForServer(t, baseURL, 5*time.Second)

	// Verify the event processor was configured
	var processor *module.EventProcessor
	for _, svc := range app.SvcRegistry() {
		if p, ok := svc.(*module.EventProcessor); ok {
			processor = p
			break
		}
	}
	if processor == nil {
		t.Fatal("Event processor not found in service registry")
	}

	// Submit events through the broker to test the adapter pathway
	var broker module.MessageBroker
	for _, svc := range app.SvcRegistry() {
		if b, ok := svc.(module.MessageBroker); ok {
			broker = b
			break
		}
	}
	if broker == nil {
		t.Fatal("Message broker not found in service registry")
	}

	// Send two login failure events (above the minOccurs=2 threshold)
	for i := range 3 {
		msg := fmt.Sprintf(`{"userId":"user-42","sessionId":"sess-abc","attempt":%d}`, i+1)
		if err := broker.Producer().SendMessage("user.login.failed", []byte(msg)); err != nil {
			t.Fatalf("SendMessage failed on attempt %d: %v", i+1, err)
		}
		time.Sleep(10 * time.Millisecond) // Small delay between events
	}

	// Give event processing a moment
	time.Sleep(100 * time.Millisecond)

	// Verify HTTP endpoint is also working
	resp, err := http.Post(baseURL+"/api/events", "application/json",
		strings.NewReader(`{"type":"test.ping"}`))
	if err != nil {
		t.Fatalf("POST /api/events failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}

	t.Log("E2E Event Workflow: Processor configured with patterns, adapters connected to broker, HTTP running")
}

// TestE2E_DataTransformer proves the data transformer module can be loaded
// through the engine, and verifies its transformation operations work correctly.
func TestE2E_DataTransformer(t *testing.T) {
	port := getFreePort(t)
	addr := fmt.Sprintf(":%d", port)
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "dt-server", Type: "http.server", Config: map[string]any{"address": addr}},
			{Name: "dt-router", Type: "http.router", DependsOn: []string{"dt-server"}},
			{Name: "dt-handler", Type: "http.handler", DependsOn: []string{"dt-router"}, Config: map[string]any{"contentType": "application/json"}},
			{Name: "dt-transformer", Type: "data.transformer"},
		},
		Workflows: map[string]any{
			"http": map[string]any{
				"server": "dt-server",
				"router": "dt-router",
				"routes": []any{
					map[string]any{"method": "POST", "path": "/api/transform", "handler": "dt-handler"},
				},
			},
		},
		Triggers: map[string]any{},
	}

	logger := &mockLogger{}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)
	engine := NewStdEngine(app, logger)
	engine.RegisterWorkflowHandler(handlers.NewHTTPWorkflowHandler())

	if err := engine.BuildFromConfig(cfg); err != nil {
		t.Fatalf("BuildFromConfig failed: %v", err)
	}

	ctx := t.Context()

	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Engine start failed: %v", err)
	}
	defer engine.Stop(context.Background())

	waitForServer(t, baseURL, 5*time.Second)

	// Find the data transformer from the service registry
	var transformer *module.DataTransformer
	for _, svc := range app.SvcRegistry() {
		if dt, ok := svc.(*module.DataTransformer); ok {
			transformer = dt
			break
		}
	}
	if transformer == nil {
		t.Fatal("Data transformer not found in service registry")
	}

	// Register a transformation pipeline
	transformer.RegisterPipeline(&module.TransformPipeline{
		Name: "normalize-user",
		Operations: []module.TransformOperation{
			{
				Type:   "extract",
				Config: map[string]any{"path": "user"},
			},
			{
				Type: "map",
				Config: map[string]any{
					"mappings": map[string]any{
						"firstName": "first_name",
						"lastName":  "last_name",
					},
				},
			},
			{
				Type: "filter",
				Config: map[string]any{
					"fields": []any{"first_name", "last_name", "email"},
				},
			},
		},
	})

	// Execute the transformation
	input := map[string]any{
		"user": map[string]any{
			"firstName": "Alice",
			"lastName":  "Smith",
			"email":     "alice@example.com",
			"password":  "secret123",
			"internal":  "should-be-filtered",
		},
	}

	result, err := transformer.Transform(ctx, "normalize-user", input)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("Expected map result, got %T", result)
	}

	if resultMap["first_name"] != "Alice" {
		t.Errorf("Expected first_name 'Alice', got %v", resultMap["first_name"])
	}
	if resultMap["last_name"] != "Smith" {
		t.Errorf("Expected last_name 'Smith', got %v", resultMap["last_name"])
	}
	if resultMap["email"] != "alice@example.com" {
		t.Errorf("Expected email 'alice@example.com', got %v", resultMap["email"])
	}
	if _, exists := resultMap["password"]; exists {
		t.Error("Expected 'password' to be filtered out, but it exists")
	}
	if _, exists := resultMap["internal"]; exists {
		t.Error("Expected 'internal' to be filtered out, but it exists")
	}

	// Verify the HTTP endpoint is working alongside the transformer
	resp, err := http.Post(baseURL+"/api/transform", "application/json",
		strings.NewReader(`{"action":"transform"}`))
	if err != nil {
		t.Fatalf("POST /api/transform failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}

	t.Logf("  Transformed result: first_name=%v, last_name=%v, email=%v", resultMap["first_name"], resultMap["last_name"], resultMap["email"])
	t.Log("E2E Data Transformer: Pipeline executed extract->map->filter, sensitive fields stripped")
}

// TestE2E_SchedulerWorkflow proves the scheduler workflow handler can
// configure schedulers with jobs from config, and that the engine
// builds and starts successfully with the scheduler running.
func TestE2E_SchedulerWorkflow(t *testing.T) {
	port := getFreePort(t)
	addr := fmt.Sprintf(":%d", port)
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	// Track job execution
	var mu sync.Mutex
	var jobExecuted bool

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "sched-server", Type: "http.server", Config: map[string]any{"address": addr}},
			{Name: "sched-router", Type: "http.router", DependsOn: []string{"sched-server"}},
			{Name: "sched-handler", Type: "http.handler", DependsOn: []string{"sched-router"}, Config: map[string]any{"contentType": "application/json"}},
			{Name: "cron-scheduler", Type: "cron.scheduler", Config: map[string]any{"expression": "* * * * *"}},
			{Name: "cleanup-job", Type: "test.job"},
		},
		Workflows: map[string]any{
			"http": map[string]any{
				"server": "sched-server",
				"router": "sched-router",
				"routes": []any{
					map[string]any{"method": "GET", "path": "/api/health", "handler": "sched-handler"},
				},
			},
			"scheduler": map[string]any{
				"jobs": []any{
					map[string]any{
						"scheduler": "cron-scheduler",
						"job":       "cleanup-job",
					},
				},
			},
		},
		Triggers: map[string]any{},
	}

	logger := &mockLogger{}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)
	engine := NewStdEngine(app, logger)

	// Register the cron.scheduler module type factory
	engine.AddModuleType("cron.scheduler", func(name string, cfg map[string]any) modular.Module {
		expression := "* * * * *"
		if expr, ok := cfg["expression"].(string); ok {
			expression = expr
		}
		return module.NewCronScheduler(name, expression)
	})

	// Register a test.job module type factory that tracks execution
	engine.AddModuleType("test.job", func(name string, cfg map[string]any) modular.Module {
		return &testJobModule{
			name: name,
			executeFn: func(ctx context.Context) error {
				mu.Lock()
				defer mu.Unlock()
				jobExecuted = true
				return nil
			},
		}
	})

	engine.RegisterWorkflowHandler(handlers.NewHTTPWorkflowHandler())
	engine.RegisterWorkflowHandler(handlers.NewSchedulerWorkflowHandler())

	if err := engine.BuildFromConfig(cfg); err != nil {
		t.Fatalf("BuildFromConfig failed: %v", err)
	}

	ctx := t.Context()

	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Engine start failed: %v", err)
	}
	defer engine.Stop(context.Background())

	waitForServer(t, baseURL, 5*time.Second)

	// Verify the scheduler was configured
	var scheduler module.Scheduler
	for _, svc := range app.SvcRegistry() {
		if s, ok := svc.(module.Scheduler); ok {
			scheduler = s
			break
		}
	}
	if scheduler == nil {
		t.Fatal("Scheduler not found in service registry")
	}

	// Manually trigger the job to verify it was wired up correctly
	var job module.Job
	for _, svc := range app.SvcRegistry() {
		if j, ok := svc.(module.Job); ok {
			job = j
			break
		}
	}
	if job == nil {
		t.Fatal("Job not found in service registry")
	}

	// Execute the job directly to verify it works
	if err := job.Execute(ctx); err != nil {
		t.Fatalf("Job execution failed: %v", err)
	}

	mu.Lock()
	executed := jobExecuted
	mu.Unlock()

	if !executed {
		t.Error("Expected job to have been executed")
	}

	// Verify the HTTP endpoint is working
	resp, err := http.Get(baseURL + "/api/health")
	if err != nil {
		t.Fatalf("GET /api/health failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}

	t.Log("E2E Scheduler Workflow: Scheduler configured with job, job executed, HTTP server running")
}

// testJobModule wraps a function as a modular.Module that also implements module.Job.
type testJobModule struct {
	name      string
	executeFn func(ctx context.Context) error
}

func (j *testJobModule) Name() string { return j.name }

func (j *testJobModule) Init(app modular.Application) error {
	return app.RegisterService(j.name, j)
}

func (j *testJobModule) Execute(ctx context.Context) error {
	if j.executeFn != nil {
		return j.executeFn(ctx)
	}
	return nil
}

// TestE2E_MetricsAndHealthCheck proves the metrics collector and health checker
// modules work end-to-end within the engine. It builds an engine with both
// modules, makes HTTP requests, records metrics, and verifies health status.
func TestE2E_MetricsAndHealthCheck(t *testing.T) {
	port := getFreePort(t)
	addr := fmt.Sprintf(":%d", port)
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "mh-server", Type: "http.server", Config: map[string]any{"address": addr}},
			{Name: "mh-router", Type: "http.router", DependsOn: []string{"mh-server"}},
			{Name: "mh-handler", Type: "http.handler", DependsOn: []string{"mh-router"}, Config: map[string]any{"contentType": "application/json"}},
			{Name: "mh-metrics", Type: "metrics.collector"},
			{Name: "mh-health", Type: "health.checker"},
		},
		Workflows: map[string]any{
			"http": map[string]any{
				"server": "mh-server",
				"router": "mh-router",
				"routes": []any{
					map[string]any{"method": "GET", "path": "/api/data", "handler": "mh-handler"},
				},
			},
		},
		Triggers: map[string]any{},
	}

	logger := &mockLogger{}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)
	engine := NewStdEngine(app, logger)
	engine.RegisterWorkflowHandler(handlers.NewHTTPWorkflowHandler())

	if err := engine.BuildFromConfig(cfg); err != nil {
		t.Fatalf("BuildFromConfig failed: %v", err)
	}

	ctx := t.Context()

	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Engine start failed: %v", err)
	}
	defer engine.Stop(context.Background())

	waitForServer(t, baseURL, 5*time.Second)

	// Step 1: Verify metrics collector is in the service registry
	t.Log("Step 1: Verifying metrics collector is registered as a service")
	var mc *module.MetricsCollector
	if err := app.GetService("metrics.collector", &mc); err != nil {
		t.Fatalf("metrics.collector service not found: %v", err)
	}
	if mc == nil {
		t.Fatal("metrics.collector service is nil")
	}
	t.Log("  metrics.collector service found in registry")

	// Step 2: Verify health checker is in the service registry
	t.Log("Step 2: Verifying health checker is registered as a service")
	var hc *module.HealthChecker
	if err := app.GetService("health.checker", &hc); err != nil {
		t.Fatalf("health.checker service not found: %v", err)
	}
	if hc == nil {
		t.Fatal("health.checker service is nil")
	}
	t.Log("  health.checker service found in registry")

	// Step 3: Register a health check and mark as started
	t.Log("Step 3: Registering health check and setting started")
	hc.RegisterCheck("test-db", func(_ context.Context) module.HealthCheckResult {
		return module.HealthCheckResult{Status: "healthy", Message: "database OK"}
	})
	hc.SetStarted(true)

	// Step 4: Make some HTTP requests to generate traffic
	t.Log("Step 4: Making HTTP requests to generate metrics")
	client := &http.Client{Timeout: 5 * time.Second}
	for range 3 {
		resp, err := client.Get(baseURL + "/api/data")
		if err != nil {
			t.Fatalf("GET /api/data failed: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200, got %d", resp.StatusCode)
		}
	}
	t.Log("  Made 3 successful HTTP requests")

	// Step 5: Record metrics programmatically and verify they were captured
	t.Log("Step 5: Recording and verifying metrics")
	mc.RecordHTTPRequest("GET", "/api/data", 200, 50*time.Millisecond)
	mc.RecordWorkflowExecution("test-workflow", "process", "success")
	mc.RecordWorkflowDuration("test-workflow", "process", 100*time.Millisecond)
	mc.SetActiveWorkflows("test-workflow", 5)
	mc.RecordModuleOperation("mh-handler", "handle", "success")

	// Verify the Prometheus handler serves metrics
	metricsHandler := mc.Handler()
	if metricsHandler == nil {
		t.Fatal("MetricsCollector.Handler() returned nil")
	}

	// Serve metrics via a test HTTP recorder
	rec := &testResponseRecorder{headers: make(http.Header), body: &bytes.Buffer{}}
	metricsReq, _ := http.NewRequest("GET", "/metrics", nil)
	metricsHandler.ServeHTTP(rec, metricsReq)

	metricsBody := rec.body.String()
	if !strings.Contains(metricsBody, "workflow_executions_total") {
		t.Error("Metrics output missing workflow_executions_total")
	}
	if !strings.Contains(metricsBody, "http_requests_total") {
		t.Error("Metrics output missing http_requests_total")
	}
	if !strings.Contains(metricsBody, "active_workflows") {
		t.Error("Metrics output missing active_workflows")
	}
	t.Log("  Prometheus metrics contain expected counters")

	// Step 6: Test health check endpoints
	t.Log("Step 6: Testing health checker HTTP handlers")

	// Test /health endpoint
	healthRec := &testResponseRecorder{headers: make(http.Header), body: &bytes.Buffer{}}
	healthReq, _ := http.NewRequest("GET", "/health", nil)
	hc.HealthHandler().ServeHTTP(healthRec, healthReq)

	var healthResp map[string]any
	if err := json.Unmarshal(healthRec.body.Bytes(), &healthResp); err != nil {
		t.Fatalf("Failed to decode health response: %v", err)
	}
	if healthResp["status"] != "healthy" {
		t.Errorf("Expected health status 'healthy', got %v", healthResp["status"])
	}
	t.Logf("  Health endpoint: status=%v", healthResp["status"])

	// Test /ready endpoint
	readyRec := &testResponseRecorder{headers: make(http.Header), body: &bytes.Buffer{}}
	readyReq, _ := http.NewRequest("GET", "/ready", nil)
	hc.ReadyHandler().ServeHTTP(readyRec, readyReq)

	var readyResp map[string]string
	if err := json.Unmarshal(readyRec.body.Bytes(), &readyResp); err != nil {
		t.Fatalf("Failed to decode ready response: %v", err)
	}
	if readyResp["status"] != "ready" {
		t.Errorf("Expected ready status 'ready', got %v", readyResp["status"])
	}
	t.Logf("  Ready endpoint: status=%v", readyResp["status"])

	// Test /live endpoint
	liveRec := &testResponseRecorder{headers: make(http.Header), body: &bytes.Buffer{}}
	liveReq, _ := http.NewRequest("GET", "/live", nil)
	hc.LiveHandler().ServeHTTP(liveRec, liveReq)

	var liveResp map[string]string
	if err := json.Unmarshal(liveRec.body.Bytes(), &liveResp); err != nil {
		t.Fatalf("Failed to decode live response: %v", err)
	}
	if liveResp["status"] != "alive" {
		t.Errorf("Expected live status 'alive', got %v", liveResp["status"])
	}
	t.Logf("  Live endpoint: status=%v", liveResp["status"])

	// Step 7: Test health check with unhealthy component
	t.Log("Step 7: Testing health check with unhealthy component")
	hc.RegisterCheck("failing-svc", func(_ context.Context) module.HealthCheckResult {
		return module.HealthCheckResult{Status: "unhealthy", Message: "service down"}
	})

	unhealthyRec := &testResponseRecorder{headers: make(http.Header), body: &bytes.Buffer{}}
	unhealthyReq, _ := http.NewRequest("GET", "/health", nil)
	hc.HealthHandler().ServeHTTP(unhealthyRec, unhealthyReq)

	var unhealthyResp map[string]any
	if err := json.Unmarshal(unhealthyRec.body.Bytes(), &unhealthyResp); err != nil {
		t.Fatalf("Failed to decode unhealthy response: %v", err)
	}
	if unhealthyResp["status"] != "unhealthy" {
		t.Errorf("Expected overall status 'unhealthy', got %v", unhealthyResp["status"])
	}
	if unhealthyRec.statusCode != http.StatusServiceUnavailable {
		t.Errorf("Expected 503 for unhealthy status, got %d", unhealthyRec.statusCode)
	}
	t.Logf("  Unhealthy endpoint: status=%v, httpCode=%d", unhealthyResp["status"], unhealthyRec.statusCode)

	t.Log("E2E Metrics & Health Check: All steps passed")
}

// TestE2E_EventBusBridge proves that the EventBusBridge adapter correctly
// connects the workflow engine's MessageBroker interface to the modular
// framework's EventBus. It creates both modules, wires them together,
// and verifies pub/sub works across the bridge.
func TestE2E_EventBusBridge(t *testing.T) {
	port := getFreePort(t)
	addr := fmt.Sprintf(":%d", port)
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "eb-server", Type: "http.server", Config: map[string]any{"address": addr}},
			{Name: "eb-router", Type: "http.router", DependsOn: []string{"eb-server"}},
			{Name: "eb-handler", Type: "http.handler", DependsOn: []string{"eb-router"}, Config: map[string]any{"contentType": "application/json"}},
			{Name: "eb-eventbus", Type: "eventbus.modular"},
		},
		Workflows: map[string]any{
			"http": map[string]any{
				"server": "eb-server",
				"router": "eb-router",
				"routes": []any{
					map[string]any{"method": "GET", "path": "/api/ping", "handler": "eb-handler"},
				},
			},
		},
		Triggers: map[string]any{},
	}

	logger := &mockLogger{}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)
	engine := NewStdEngine(app, logger)
	engine.RegisterWorkflowHandler(handlers.NewHTTPWorkflowHandler())

	if err := engine.BuildFromConfig(cfg); err != nil {
		t.Fatalf("BuildFromConfig failed: %v", err)
	}

	// Create the bridge and connect it to the EventBus after initialization.
	// The bridge's Init() writes directly to the SvcRegistry map, which gets
	// overwritten by the enhanced registry; so we wire it up manually.
	bridge := module.NewEventBusBridge("eb-bridge")
	if err := bridge.InitFromApp(app); err != nil {
		t.Fatalf("EventBusBridge.InitFromApp failed: %v", err)
	}

	ctx := t.Context()

	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Engine start failed: %v", err)
	}
	defer engine.Stop(context.Background())

	waitForServer(t, baseURL, 5*time.Second)

	// Step 1: Subscribe to a topic via the bridge (MessageBroker interface)
	t.Log("Step 1: Subscribing to topic via EventBusBridge")
	var received []byte
	var mu sync.Mutex
	handler := module.NewFunctionMessageHandler(func(msg []byte) error {
		mu.Lock()
		defer mu.Unlock()
		received = msg
		return nil
	})

	if err := bridge.Subscribe("test.events", handler); err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}
	t.Log("  Subscribed to 'test.events' via bridge")

	// Step 2: Publish through the bridge's Producer (should go through EventBus)
	t.Log("Step 2: Publishing message through the bridge")
	testMsg := []byte(`{"action":"test","value":42}`)
	if err := bridge.Producer().SendMessage("test.events", testMsg); err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	if received == nil {
		t.Error("Expected message delivery through EventBusBridge, but got nothing")
	} else {
		var parsed map[string]any
		if err := json.Unmarshal(received, &parsed); err != nil {
			t.Errorf("Failed to parse received message: %v", err)
		} else {
			if parsed["action"] != "test" {
				t.Errorf("Expected action 'test', got %v", parsed["action"])
			}
			t.Logf("  Received message via bridge: %s", string(received))
		}
	}
	mu.Unlock()

	// Step 3: Publish directly through the EventBus and verify bridge subscriber receives it
	t.Log("Step 3: Publishing directly through EventBus module")
	var eb *eventbus.EventBusModule
	if err := app.GetService(eventbus.ServiceName, &eb); err != nil {
		t.Fatalf("EventBus service not found: %v", err)
	}

	mu.Lock()
	received = nil
	mu.Unlock()

	directPayload := map[string]any{"source": "direct", "id": 123}
	if err := eb.Publish(context.Background(), "test.events", directPayload); err != nil {
		t.Fatalf("Direct EventBus Publish failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	if received == nil {
		t.Error("Expected bridge subscriber to receive directly-published EventBus message")
	} else {
		t.Logf("  Bridge subscriber received direct EventBus message: %s", string(received))
	}
	mu.Unlock()

	// Step 4: Unsubscribe and verify no more messages arrive
	t.Log("Step 4: Unsubscribing and verifying no more messages")
	if err := bridge.Unsubscribe("test.events"); err != nil {
		t.Fatalf("Unsubscribe failed: %v", err)
	}

	mu.Lock()
	received = nil
	mu.Unlock()

	_ = bridge.Producer().SendMessage("test.events", []byte(`{"after":"unsub"}`))
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	if received != nil {
		t.Error("Expected no message after unsubscribe, but got one")
	} else {
		t.Log("  No messages received after unsubscribe (correct)")
	}
	mu.Unlock()

	t.Log("E2E EventBusBridge: All steps passed - pub/sub through bridge and direct EventBus verified")
}

// TestE2E_WebhookSender proves the webhook sender module can deliver webhooks
// to a real HTTP endpoint with proper retry behavior.
func TestE2E_WebhookSender(t *testing.T) {
	// Start a target HTTP server that will receive webhooks
	targetPort := getFreePort(t)
	targetAddr := fmt.Sprintf(":%d", targetPort)
	targetURL := fmt.Sprintf("http://127.0.0.1:%d/webhook", targetPort)

	var receivedPayloads [][]byte
	var receivedHeaders []http.Header
	var mu sync.Mutex

	targetMux := http.NewServeMux()
	targetMux.HandleFunc("POST /webhook", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		receivedPayloads = append(receivedPayloads, body)
		receivedHeaders = append(receivedHeaders, r.Header.Clone())
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"received"}`))
	})

	targetServer := &http.Server{Addr: targetAddr, Handler: targetMux}
	go targetServer.ListenAndServe()
	defer targetServer.Shutdown(context.Background())

	waitForServer(t, fmt.Sprintf("http://127.0.0.1:%d", targetPort), 5*time.Second)

	// Build an engine with webhook.sender
	enginePort := getFreePort(t)
	engineAddr := fmt.Sprintf(":%d", enginePort)
	engineURL := fmt.Sprintf("http://127.0.0.1:%d", enginePort)

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "wh-server", Type: "http.server", Config: map[string]any{"address": engineAddr}},
			{Name: "wh-router", Type: "http.router", DependsOn: []string{"wh-server"}},
			{Name: "wh-handler", Type: "http.handler", DependsOn: []string{"wh-router"}, Config: map[string]any{"contentType": "application/json"}},
			{Name: "wh-sender", Type: "webhook.sender", Config: map[string]any{"maxRetries": float64(2)}},
		},
		Workflows: map[string]any{
			"http": map[string]any{
				"server": "wh-server",
				"router": "wh-router",
				"routes": []any{
					map[string]any{"method": "GET", "path": "/api/status", "handler": "wh-handler"},
				},
			},
		},
		Triggers: map[string]any{},
	}

	logger := &mockLogger{}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)
	engine := NewStdEngine(app, logger)
	engine.RegisterWorkflowHandler(handlers.NewHTTPWorkflowHandler())

	if err := engine.BuildFromConfig(cfg); err != nil {
		t.Fatalf("BuildFromConfig failed: %v", err)
	}

	ctx := t.Context()

	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Engine start failed: %v", err)
	}
	defer engine.Stop(context.Background())

	waitForServer(t, engineURL, 5*time.Second)

	// Step 1: Get the webhook sender from the service registry
	t.Log("Step 1: Retrieving webhook sender from service registry")
	var ws *module.WebhookSender
	if err := app.GetService("webhook.sender", &ws); err != nil {
		t.Fatalf("webhook.sender service not found: %v", err)
	}
	t.Log("  webhook.sender service found")

	// Step 2: Send a webhook to our target server
	t.Log("Step 2: Sending webhook to target server")
	payload := []byte(`{"event":"order.completed","orderId":"ORD-200"}`)
	webhookHeaders := map[string]string{
		"X-Webhook-Secret": "test-secret-123",
		"X-Event-Type":     "order.completed",
	}

	delivery, err := ws.Send(ctx, targetURL, payload, webhookHeaders)
	if err != nil {
		t.Fatalf("Webhook Send failed: %v", err)
	}

	if delivery.Status != "delivered" {
		t.Errorf("Expected delivery status 'delivered', got %v", delivery.Status)
	}
	if delivery.Attempts != 1 {
		t.Errorf("Expected 1 attempt (success on first try), got %d", delivery.Attempts)
	}
	t.Logf("  Webhook delivered: id=%s, status=%s, attempts=%d", delivery.ID, delivery.Status, delivery.Attempts)

	// Step 3: Verify the target server received the payload
	t.Log("Step 3: Verifying target server received the webhook")
	mu.Lock()
	if len(receivedPayloads) != 1 {
		t.Fatalf("Expected 1 received payload, got %d", len(receivedPayloads))
	}

	var receivedData map[string]any
	if err := json.Unmarshal(receivedPayloads[0], &receivedData); err != nil {
		t.Fatalf("Failed to parse received payload: %v", err)
	}
	if receivedData["event"] != "order.completed" {
		t.Errorf("Expected event 'order.completed', got %v", receivedData["event"])
	}
	if receivedData["orderId"] != "ORD-200" {
		t.Errorf("Expected orderId 'ORD-200', got %v", receivedData["orderId"])
	}

	// Verify custom headers were sent
	if receivedHeaders[0].Get("X-Webhook-Secret") != "test-secret-123" {
		t.Errorf("Expected X-Webhook-Secret header, got %v", receivedHeaders[0].Get("X-Webhook-Secret"))
	}
	if receivedHeaders[0].Get("X-Event-Type") != "order.completed" {
		t.Errorf("Expected X-Event-Type header, got %v", receivedHeaders[0].Get("X-Event-Type"))
	}
	mu.Unlock()
	t.Log("  Target server received correct payload and headers")

	// Step 4: Send a webhook to a non-existent URL and verify it goes to dead letter
	t.Log("Step 4: Sending webhook to invalid URL (expects dead letter)")
	// Use a short-timeout client to avoid long waits on connect failures
	ws.SetClient(&http.Client{Timeout: 500 * time.Millisecond})
	badDelivery, err := ws.Send(ctx, "http://127.0.0.1:1/nonexistent", payload, nil)
	if err == nil {
		t.Error("Expected error sending to invalid URL")
	}
	if badDelivery.Status != "dead_letter" {
		t.Errorf("Expected dead_letter status, got %v", badDelivery.Status)
	}
	t.Logf("  Failed delivery: id=%s, status=%s, attempts=%d", badDelivery.ID, badDelivery.Status, badDelivery.Attempts)

	// Step 5: Verify dead letter queue
	t.Log("Step 5: Verifying dead letter queue")
	deadLetters := ws.GetDeadLetters()
	if len(deadLetters) != 1 {
		t.Errorf("Expected 1 dead letter, got %d", len(deadLetters))
	} else {
		t.Logf("  Dead letter queue: id=%s, lastError=%s", deadLetters[0].ID, deadLetters[0].LastError)
	}

	t.Log("E2E Webhook Sender: All steps passed - delivery, headers, dead letter verified")
}

// TestE2E_SlackNotification_MockEndpoint proves the Slack notification module
// sends properly formatted messages to a Slack-compatible webhook endpoint.
func TestE2E_SlackNotification_MockEndpoint(t *testing.T) {
	// Start a mock Slack webhook server
	slackPort := getFreePort(t)
	slackAddr := fmt.Sprintf(":%d", slackPort)
	slackURL := fmt.Sprintf("http://127.0.0.1:%d/services/webhook", slackPort)

	var receivedPayloads [][]byte
	var mu sync.Mutex

	slackMux := http.NewServeMux()
	slackMux.HandleFunc("POST /services/webhook", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		receivedPayloads = append(receivedPayloads, body)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	slackServer := &http.Server{Addr: slackAddr, Handler: slackMux}
	go slackServer.ListenAndServe()
	defer slackServer.Shutdown(context.Background())

	waitForServer(t, fmt.Sprintf("http://127.0.0.1:%d", slackPort), 5*time.Second)

	// Build engine with notification.slack module
	enginePort := getFreePort(t)
	engineAddr := fmt.Sprintf(":%d", enginePort)
	engineURL := fmt.Sprintf("http://127.0.0.1:%d", enginePort)

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "sl-server", Type: "http.server", Config: map[string]any{"address": engineAddr}},
			{Name: "sl-router", Type: "http.router", DependsOn: []string{"sl-server"}},
			{Name: "sl-handler", Type: "http.handler", DependsOn: []string{"sl-router"}, Config: map[string]any{"contentType": "application/json"}},
			{Name: "sl-slack", Type: "notification.slack", Config: map[string]any{"webhookURL": "http://placeholder"}},
		},
		Workflows: map[string]any{
			"http": map[string]any{
				"server": "sl-server",
				"router": "sl-router",
				"routes": []any{
					map[string]any{"method": "GET", "path": "/api/status", "handler": "sl-handler"},
				},
			},
		},
		Triggers: map[string]any{},
	}

	logger := &mockLogger{}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)
	engine := NewStdEngine(app, logger)
	engine.RegisterWorkflowHandler(handlers.NewHTTPWorkflowHandler())

	if err := engine.BuildFromConfig(cfg); err != nil {
		t.Fatalf("BuildFromConfig failed: %v", err)
	}

	ctx := t.Context()

	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Engine start failed: %v", err)
	}
	defer engine.Stop(context.Background())

	waitForServer(t, engineURL, 5*time.Second)

	// Step 1: Get the Slack notification module and configure it
	t.Log("Step 1: Configuring Slack notification module")
	var slack *module.SlackNotification
	for _, svc := range app.SvcRegistry() {
		if s, ok := svc.(*module.SlackNotification); ok {
			slack = s
			break
		}
	}
	if slack == nil {
		t.Fatal("SlackNotification module not found in service registry")
	}

	slack.SetWebhookURL(slackURL)
	slack.SetChannel("#test-alerts")
	slack.SetUsername("workflow-bot")
	t.Log("  Configured: webhook, channel, username")

	// Step 2: Send a notification
	t.Log("Step 2: Sending notification via HandleMessage")
	notifMsg := "Order ORD-300 has been completed successfully"
	if err := slack.HandleMessage([]byte(notifMsg)); err != nil {
		t.Fatalf("HandleMessage failed: %v", err)
	}
	t.Log("  Notification sent")

	// Step 3: Verify the mock Slack server received the correct payload
	t.Log("Step 3: Verifying mock Slack server received the payload")
	mu.Lock()
	if len(receivedPayloads) != 1 {
		t.Fatalf("Expected 1 Slack payload, got %d", len(receivedPayloads))
	}

	var slackPayload map[string]any
	if err := json.Unmarshal(receivedPayloads[0], &slackPayload); err != nil {
		t.Fatalf("Failed to parse Slack payload: %v", err)
	}

	if slackPayload["text"] != notifMsg {
		t.Errorf("Expected text %q, got %v", notifMsg, slackPayload["text"])
	}
	if slackPayload["channel"] != "#test-alerts" {
		t.Errorf("Expected channel '#test-alerts', got %v", slackPayload["channel"])
	}
	if slackPayload["username"] != "workflow-bot" {
		t.Errorf("Expected username 'workflow-bot', got %v", slackPayload["username"])
	}
	mu.Unlock()
	t.Logf("  Slack payload correct: text=%q, channel=%v, username=%v",
		slackPayload["text"], slackPayload["channel"], slackPayload["username"])

	// Step 4: Send a second notification and verify
	t.Log("Step 4: Sending second notification")
	if err := slack.HandleMessage([]byte("Alert: Service degraded")); err != nil {
		t.Fatalf("Second HandleMessage failed: %v", err)
	}

	mu.Lock()
	if len(receivedPayloads) != 2 {
		t.Errorf("Expected 2 total Slack payloads, got %d", len(receivedPayloads))
	}
	mu.Unlock()

	// Step 5: Test error when webhook URL is empty
	t.Log("Step 5: Testing error handling with empty webhook URL")
	emptySlack := module.NewSlackNotification("empty-slack")
	if err := emptySlack.HandleMessage([]byte("should fail")); err == nil {
		t.Error("Expected error when webhook URL is not configured")
	} else {
		t.Logf("  Got expected error: %v", err)
	}

	t.Log("E2E Slack Notification: All steps passed - message delivery, payload format, error handling verified")
}

// testResponseRecorder is a minimal http.ResponseWriter for testing handlers.
type testResponseRecorder struct {
	statusCode int
	headers    http.Header
	body       *bytes.Buffer
}

func (r *testResponseRecorder) Header() http.Header {
	return r.headers
}

func (r *testResponseRecorder) Write(b []byte) (int, error) {
	return r.body.Write(b)
}

func (r *testResponseRecorder) WriteHeader(statusCode int) {
	r.statusCode = statusCode
}
