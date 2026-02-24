package module

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CrisisTextLine/modular"
)

// TestHTTPTrigger tests the HTTP trigger functionality
func TestHTTPTrigger(t *testing.T) {
	// Create a mock application
	app := NewMockApplication()

	// Create a mock HTTP router
	router := NewMockHTTPRouter("test-router")
	if err := app.RegisterService("httpRouter", router); err != nil {
		t.Fatalf("Failed to register HTTP router: %v", err)
	}

	// Create a mock workflow engine
	engine := NewMockWorkflowEngine()
	if err := app.RegisterService("workflowEngine", engine); err != nil {
		t.Fatalf("Failed to register workflow engine: %v", err)
	}

	// Create the HTTP trigger
	trigger := NewHTTPTrigger()
	if trigger.Name() != HTTPTriggerName {
		t.Errorf("Expected name '%s', got '%s'", HTTPTriggerName, trigger.Name())
	}
	app.RegisterModule(trigger)

	// Configure the trigger
	config := map[string]any{
		"routes": []any{
			map[string]any{
				"path":     "/api/workflows/test",
				"method":   "POST",
				"workflow": "test-workflow",
				"action":   "test-action",
				"params": map[string]any{
					"static_param": "static_value",
				},
			},
		},
	}

	err := trigger.Configure(app, config)
	if err != nil {
		t.Fatalf("Failed to configure trigger: %v", err)
	}

	// Start the trigger
	err = trigger.Start(context.Background())
	if err != nil {
		t.Fatalf("Failed to start trigger: %v", err)
	}

	// Verify the route was added to the router by checking the registered routes
	// No need to cast router since it's already a *MockHTTPRouter
	if len(router.routes) != 1 {
		t.Fatalf("Expected 1 registered route, got %d", len(router.routes))
	}

	routeKey := "POST /api/workflows/test"
	handler, exists := router.routes[routeKey]
	if !exists {
		t.Fatalf("Expected route '%s' to be registered", routeKey)
	}

	// Create a test request
	req := httptest.NewRequest("POST", "/api/workflows/test?query_param=query_value", strings.NewReader(""))
	w := httptest.NewRecorder()

	// Call the handler directly
	handler.Handle(w, req)

	// Verify the workflow was triggered
	if len(engine.triggeredWorkflows) != 1 {
		t.Fatalf("Expected 1 triggered workflow, got %d", len(engine.triggeredWorkflows))
	}

	workflow := engine.triggeredWorkflows[0]
	if workflow.WorkflowType != "test-workflow" {
		t.Errorf("Expected workflow type 'test-workflow', got '%s'", workflow.WorkflowType)
	}
	if workflow.Action != "test-action" {
		t.Errorf("Expected action 'test-action', got '%s'", workflow.Action)
	}

	// Check that parameters were passed correctly
	if workflow.Data["static_param"] != "static_value" {
		t.Errorf("Expected static_param 'static_value', got '%v'", workflow.Data["static_param"])
	}
	if workflow.Data["query_param"] != "query_value" {
		t.Errorf("Expected query_param 'query_value', got '%v'", workflow.Data["query_param"])
	}

	// Test stopping the trigger
	err = trigger.Stop(context.Background())
	if err != nil {
		t.Fatalf("Failed to stop trigger: %v", err)
	}
}

// MockHTTPRouter is a simple mock HTTP router for testing
type MockHTTPRouter struct {
	name   string
	routes map[string]HTTPHandler
}

// NewMockHTTPRouter creates a new mock HTTP router
func NewMockHTTPRouter(name string) *MockHTTPRouter {
	return &MockHTTPRouter{
		name:   name,
		routes: make(map[string]HTTPHandler),
	}
}

func (r *MockHTTPRouter) Name() string {
	return r.name
}

func (r *MockHTTPRouter) AddRoute(method, path string, handler HTTPHandler) {
	key := method + " " + path
	r.routes[key] = handler
}

func (r *MockHTTPRouter) Init(registry modular.ServiceRegistry) error {
	registry[r.name] = r
	return nil
}

func (r *MockHTTPRouter) Configure(app modular.Application, config map[string]any) error {
	return nil
}

func (r *MockHTTPRouter) Start(ctx context.Context) error {
	return nil
}

func (r *MockHTTPRouter) Stop(ctx context.Context) error {
	return nil
}

// responseWritingEngine is a mock WorkflowEngine that writes an HTTP response
// via the ResponseWriter injected into the context, simulating a pipeline
// that contains a step.json_response step.
type responseWritingEngine struct {
	statusCode int
	body       string
}

func (e *responseWritingEngine) TriggerWorkflow(ctx context.Context, workflowType, action string, data map[string]any) error {
	rw, ok := ctx.Value(HTTPResponseWriterContextKey).(http.ResponseWriter)
	if !ok {
		return nil
	}
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(e.statusCode)
	_, _ = rw.Write([]byte(e.body))
	return nil
}

// TestHTTPTrigger_ResponsePassthrough verifies that when a pipeline step writes
// an HTTP response via the injected ResponseWriter, the HTTP trigger does not
// overwrite it with the generic "workflow triggered" fallback.
func TestHTTPTrigger_ResponsePassthrough(t *testing.T) {
	app := NewMockApplication()
	router := NewMockHTTPRouter("test-router")
	_ = app.RegisterService("httpRouter", router)

	engine := &responseWritingEngine{statusCode: 201, body: `{"id":"new-123"}`}
	_ = app.RegisterService("workflowEngine", engine)

	trigger := NewHTTPTrigger()
	app.RegisterModule(trigger)

	cfg := map[string]any{
		"routes": []any{
			map[string]any{
				"path":     "/api/items",
				"method":   "POST",
				"workflow": "create-item",
				"action":   "execute",
			},
		},
	}
	if err := trigger.Configure(app, cfg); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if err := trigger.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	handler := router.routes["POST /api/items"]
	if handler == nil {
		t.Fatal("handler not registered")
	}

	req := httptest.NewRequest("POST", "/api/items", strings.NewReader(""))
	w := httptest.NewRecorder()
	handler.Handle(w, req)

	resp := w.Result()
	if resp.StatusCode != 201 {
		t.Errorf("expected 201 from pipeline, got %d", resp.StatusCode)
	}
	if !strings.Contains(w.Body.String(), "new-123") {
		t.Errorf("expected pipeline body, got %q", w.Body.String())
	}
}

// TestHTTPTrigger_FallbackResponse verifies that when no pipeline step writes
// an HTTP response, the trigger falls back to the generic accepted response.
func TestHTTPTrigger_FallbackResponse(t *testing.T) {
	app := NewMockApplication()
	router := NewMockHTTPRouter("test-router")
	_ = app.RegisterService("httpRouter", router)

	// Standard mock engine — does not write to the response writer.
	engine := NewMockWorkflowEngine()
	_ = app.RegisterService("workflowEngine", engine)

	trigger := NewHTTPTrigger()
	app.RegisterModule(trigger)

	cfg := map[string]any{
		"routes": []any{
			map[string]any{
				"path":     "/api/items",
				"method":   "POST",
				"workflow": "fire-and-forget",
				"action":   "execute",
			},
		},
	}
	if err := trigger.Configure(app, cfg); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if err := trigger.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	handler := router.routes["POST /api/items"]
	req := httptest.NewRequest("POST", "/api/items", strings.NewReader(""))
	w := httptest.NewRecorder()
	handler.Handle(w, req)

	resp := w.Result()
	if resp.StatusCode != 202 {
		t.Errorf("expected fallback 202, got %d", resp.StatusCode)
	}
	if !strings.Contains(w.Body.String(), "workflow triggered") {
		t.Errorf("expected fallback body, got %q", w.Body.String())
	}
}

// TestHTTPTrigger_ResponseWriterInContext verifies that the HTTP response writer
// is correctly threaded through the Go context to the workflow engine.
func TestHTTPTrigger_ResponseWriterInContext(t *testing.T) {
	app := NewMockApplication()
	router := NewMockHTTPRouter("test-router")
	_ = app.RegisterService("httpRouter", router)

	var capturedCtx context.Context
	captureEngine := &captureContextEngine{capture: &capturedCtx}
	_ = app.RegisterService("workflowEngine", captureEngine)

	trigger := NewHTTPTrigger()
	app.RegisterModule(trigger)

	cfg := map[string]any{
		"routes": []any{
			map[string]any{
				"path":     "/test",
				"method":   "GET",
				"workflow": "test-wf",
				"action":   "execute",
			},
		},
	}
	if err := trigger.Configure(app, cfg); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if err := trigger.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	handler := router.routes["GET /test"]
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.Handle(w, req)

	if capturedCtx == nil {
		t.Fatal("context was not captured by engine")
	}
	if capturedCtx.Value(HTTPResponseWriterContextKey) == nil {
		t.Error("HTTPResponseWriterContextKey not present in context passed to TriggerWorkflow")
	}
}

// captureContextEngine captures the context passed to TriggerWorkflow for inspection.
type captureContextEngine struct {
	capture *context.Context
}

func (e *captureContextEngine) TriggerWorkflow(ctx context.Context, _ string, _ string, _ map[string]any) error {
	*e.capture = ctx
	return nil
}
