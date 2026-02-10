package handlers

import (
	"context"
	"testing"

	workflowmodule "github.com/GoCodeAlone/workflow/module"
)

func TestNewHTTPWorkflowHandler(t *testing.T) {
	h := NewHTTPWorkflowHandler()
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
}

func TestHTTPWorkflowHandler_CanHandle(t *testing.T) {
	h := NewHTTPWorkflowHandler()

	if !h.CanHandle("http") {
		t.Error("expected CanHandle('http') = true")
	}
	if h.CanHandle("messaging") {
		t.Error("expected CanHandle('messaging') = false")
	}
	if h.CanHandle("") {
		t.Error("expected CanHandle('') = false")
	}
}

func TestHTTPWorkflowHandler_ConfigureWorkflow_InvalidFormat(t *testing.T) {
	h := NewHTTPWorkflowHandler()
	app := CreateMockApplication()

	err := h.ConfigureWorkflow(app, "not a map")
	if err == nil {
		t.Error("expected error for invalid config format")
	}
}

func TestHTTPWorkflowHandler_ConfigureWorkflow_NoRoutes(t *testing.T) {
	h := NewHTTPWorkflowHandler()
	app := CreateMockApplication()

	config := map[string]interface{}{
		"noRoutes": true,
	}
	err := h.ConfigureWorkflow(app, config)
	if err == nil {
		t.Error("expected error for missing routes")
	}
}

func TestHTTPWorkflowHandler_ConfigureWorkflow_NoRouter(t *testing.T) {
	h := NewHTTPWorkflowHandler()
	app := CreateMockApplication()

	config := map[string]interface{}{
		"routes": []interface{}{
			map[string]interface{}{
				"method":  "GET",
				"path":    "/api/test",
				"handler": "testHandler",
			},
		},
	}
	err := h.ConfigureWorkflow(app, config)
	if err == nil {
		t.Error("expected error for missing router")
	}
}

func TestHTTPWorkflowHandler_ConfigureWorkflow_ExplicitRouterNotFound(t *testing.T) {
	h := NewHTTPWorkflowHandler()
	app := CreateMockApplication()

	config := map[string]interface{}{
		"router": "nonexistent-router",
		"routes": []interface{}{
			map[string]interface{}{
				"method":  "GET",
				"path":    "/api/test",
				"handler": "testHandler",
			},
		},
	}
	err := h.ConfigureWorkflow(app, config)
	if err == nil {
		t.Error("expected error for explicit router not found")
	}
}

func TestHTTPWorkflowHandler_ConfigureWorkflow_WithRouterNoServer(t *testing.T) {
	h := NewHTTPWorkflowHandler()
	app := NewTestServiceRegistry()

	router := workflowmodule.NewStandardHTTPRouter("router")
	app.services["router"] = router

	config := map[string]interface{}{
		"routes": []interface{}{
			map[string]interface{}{
				"method":  "GET",
				"path":    "/test",
				"handler": "handler",
			},
		},
	}
	err := h.ConfigureWorkflow(app, config)
	if err == nil {
		t.Error("expected error for missing server")
	}
}

func TestHTTPWorkflowHandler_ConfigureWorkflow_InvalidRouteConfig(t *testing.T) {
	h := NewHTTPWorkflowHandler()
	app := NewTestServiceRegistry()

	router := workflowmodule.NewStandardHTTPRouter("router")
	server := &mockHTTPServer{}
	app.services["router"] = router
	app.services["server"] = server

	config := map[string]interface{}{
		"routes": []interface{}{
			"not a map", // invalid route
		},
	}
	err := h.ConfigureWorkflow(app, config)
	if err == nil {
		t.Error("expected error for invalid route config")
	}
}

func TestHTTPWorkflowHandler_ConfigureWorkflow_IncompleteRoute(t *testing.T) {
	h := NewHTTPWorkflowHandler()
	app := NewTestServiceRegistry()

	router := workflowmodule.NewStandardHTTPRouter("router")
	server := &mockHTTPServer{}
	app.services["router"] = router
	app.services["server"] = server

	config := map[string]interface{}{
		"routes": []interface{}{
			map[string]interface{}{
				"method": "GET",
				// missing path and handler
			},
		},
	}
	err := h.ConfigureWorkflow(app, config)
	if err == nil {
		t.Error("expected error for incomplete route")
	}
}

func TestHTTPWorkflowHandler_ExecuteWorkflow_NoApp(t *testing.T) {
	h := NewHTTPWorkflowHandler()

	_, err := h.ExecuteWorkflow(context.Background(), "http", "status", nil)
	if err == nil {
		t.Error("expected error for missing application context")
	}
}

func TestHTTPWorkflowHandler_ExecuteWorkflow_Status(t *testing.T) {
	h := NewHTTPWorkflowHandler()

	app := NewTestServiceRegistry()
	router := workflowmodule.NewStandardHTTPRouter("router")
	server := &mockHTTPServer{}
	app.services["router"] = router
	app.services["server"] = server

	ctx := context.WithValue(context.Background(), applicationContextKey, app)

	result, err := h.ExecuteWorkflow(ctx, "http", "status", nil)
	if err != nil {
		t.Fatalf("ExecuteWorkflow failed: %v", err)
	}
	if result["status"] != "running" {
		t.Errorf("expected status 'running', got '%v'", result["status"])
	}
}

func TestHTTPWorkflowHandler_ExecuteWorkflow_DefaultCommand(t *testing.T) {
	h := NewHTTPWorkflowHandler()

	app := NewTestServiceRegistry()
	router := workflowmodule.NewStandardHTTPRouter("router")
	server := &mockHTTPServer{}
	app.services["router"] = router
	app.services["server"] = server

	ctx := context.WithValue(context.Background(), applicationContextKey, app)

	result, err := h.ExecuteWorkflow(ctx, "http", "", nil)
	if err != nil {
		t.Fatalf("ExecuteWorkflow failed: %v", err)
	}
	if result["status"] != "running" {
		t.Errorf("expected status 'running', got '%v'", result["status"])
	}
}

func TestHTTPWorkflowHandler_ExecuteWorkflow_Routes(t *testing.T) {
	h := NewHTTPWorkflowHandler()

	app := NewTestServiceRegistry()
	router := workflowmodule.NewStandardHTTPRouter("router")
	server := &mockHTTPServer{}
	app.services["router"] = router
	app.services["server"] = server

	ctx := context.WithValue(context.Background(), applicationContextKey, app)

	result, err := h.ExecuteWorkflow(ctx, "http", "routes", nil)
	if err != nil {
		t.Fatalf("ExecuteWorkflow failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestHTTPWorkflowHandler_ExecuteWorkflow_Check(t *testing.T) {
	h := NewHTTPWorkflowHandler()

	app := NewTestServiceRegistry()
	router := workflowmodule.NewStandardHTTPRouter("router")
	server := &mockHTTPServer{}
	app.services["router"] = router
	app.services["server"] = server

	ctx := context.WithValue(context.Background(), applicationContextKey, app)

	result, err := h.ExecuteWorkflow(ctx, "http", "check", nil)
	if err != nil {
		t.Fatalf("ExecuteWorkflow failed: %v", err)
	}
	if result["healthStatus"] != "healthy" {
		t.Errorf("expected healthStatus 'healthy', got '%v'", result["healthStatus"])
	}
}

func TestHTTPWorkflowHandler_ExecuteWorkflow_Start(t *testing.T) {
	h := NewHTTPWorkflowHandler()

	app := NewTestServiceRegistry()
	router := workflowmodule.NewStandardHTTPRouter("router")
	server := &mockHTTPServer{}
	app.services["router"] = router
	app.services["server"] = server

	ctx := context.WithValue(context.Background(), applicationContextKey, app)

	result, err := h.ExecuteWorkflow(ctx, "http", "start", nil)
	if err != nil {
		t.Fatalf("ExecuteWorkflow failed: %v", err)
	}
	if result["action"] != "server already running" {
		t.Errorf("expected 'server already running', got '%v'", result["action"])
	}
}

func TestHTTPWorkflowHandler_ExecuteWorkflow_UnknownCommand(t *testing.T) {
	h := NewHTTPWorkflowHandler()

	app := NewTestServiceRegistry()
	router := workflowmodule.NewStandardHTTPRouter("router")
	server := &mockHTTPServer{}
	app.services["router"] = router
	app.services["server"] = server

	ctx := context.WithValue(context.Background(), applicationContextKey, app)

	_, err := h.ExecuteWorkflow(ctx, "http", "unknown-command", nil)
	if err == nil {
		t.Error("expected error for unknown command")
	}
}

func TestHTTPWorkflowHandler_ExecuteWorkflow_NoServer(t *testing.T) {
	h := NewHTTPWorkflowHandler()
	app := NewTestServiceRegistry()
	ctx := context.WithValue(context.Background(), applicationContextKey, app)

	_, err := h.ExecuteWorkflow(ctx, "http", "status", nil)
	if err == nil {
		t.Error("expected error for missing server")
	}
}

func TestHTTPWorkflowHandler_ExecuteWorkflow_ExplicitServerRouter(t *testing.T) {
	h := NewHTTPWorkflowHandler()

	app := NewTestServiceRegistry()
	router := workflowmodule.NewStandardHTTPRouter("my-router")
	server := &mockHTTPServer{}
	app.services["my-router"] = router
	app.services["my-server"] = server

	ctx := context.WithValue(context.Background(), applicationContextKey, app)

	data := map[string]interface{}{
		"server": "my-server",
		"router": "my-router",
	}

	result, err := h.ExecuteWorkflow(ctx, "http", "status", data)
	if err != nil {
		t.Fatalf("ExecuteWorkflow failed: %v", err)
	}
	if result["status"] != "running" {
		t.Errorf("expected status 'running', got '%v'", result["status"])
	}
}

// mockHTTPServer implements workflowmodule.HTTPServer for testing
type mockHTTPServer struct {
	routers []workflowmodule.HTTPRouter
}

func (s *mockHTTPServer) AddRouter(router workflowmodule.HTTPRouter) {
	s.routers = append(s.routers, router)
}

func (s *mockHTTPServer) Start(ctx context.Context) error {
	return nil
}

func (s *mockHTTPServer) Stop(ctx context.Context) error {
	return nil
}
