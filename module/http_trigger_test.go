package module

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/GoCodeAlone/modular"
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

// pipelineContextResultEngine is a mock WorkflowEngine that simulates a pipeline
// setting response_status/response_body/response_headers in result.Current
// without writing directly to the HTTP response writer. It populates the
// PipelineResultHolder stored in the context, the way the real engine does.
type pipelineContextResultEngine struct {
	result map[string]any
}

func (e *pipelineContextResultEngine) TriggerWorkflow(ctx context.Context, _ string, _ string, _ map[string]any) error {
	if holder, ok := ctx.Value(PipelineResultContextKey).(*PipelineResultHolder); ok && holder != nil {
		holder.Set(e.result)
	}
	return nil
}

// TestHTTPTrigger_PipelineContextResponse verifies that when a pipeline step
// sets response_status/response_body/response_headers in result.Current without
// writing to the HTTP response writer, the trigger uses those values instead of
// the generic 202 fallback.
func TestHTTPTrigger_PipelineContextResponse(t *testing.T) {
	app := NewMockApplication()
	router := NewMockHTTPRouter("test-router")
	_ = app.RegisterService("httpRouter", router)

	engine := &pipelineContextResultEngine{result: map[string]any{
		"response_status": 403,
		"response_body":   `{"error":"forbidden"}`,
		"response_headers": map[string]any{
			"Content-Type": "application/json",
		},
	}}
	_ = app.RegisterService("workflowEngine", engine)

	trigger := NewHTTPTrigger()
	app.RegisterModule(trigger)

	cfg := map[string]any{
		"routes": []any{
			map[string]any{
				"path":     "/api/secure",
				"method":   "GET",
				"workflow": "secure-workflow",
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

	handler := router.routes["GET /api/secure"]
	if handler == nil {
		t.Fatal("handler not registered")
	}

	req := httptest.NewRequest("GET", "/api/secure", nil)
	w := httptest.NewRecorder()
	handler.Handle(w, req)

	resp := w.Result()
	if resp.StatusCode != 403 {
		t.Errorf("expected 403 from pipeline context, got %d", resp.StatusCode)
	}
	if w.Body.String() != `{"error":"forbidden"}` {
		t.Errorf("expected pipeline body, got %q", w.Body.String())
	}
	if w.Header().Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type header, got %q", w.Header().Get("Content-Type"))
	}
}

// TestHTTPTrigger_PipelineContextResponse_NoStatus verifies that when
// response_status is absent from the pipeline result, the trigger still
// falls back to the generic 202 accepted response.
func TestHTTPTrigger_PipelineContextResponse_NoStatus(t *testing.T) {
	app := NewMockApplication()
	router := NewMockHTTPRouter("test-router")
	_ = app.RegisterService("httpRouter", router)

	engine := &pipelineContextResultEngine{result: map[string]any{
		"some_internal_data": "secret",
	}}
	_ = app.RegisterService("workflowEngine", engine)

	trigger := NewHTTPTrigger()
	app.RegisterModule(trigger)

	cfg := map[string]any{
		"routes": []any{
			map[string]any{
				"path":     "/api/noisy",
				"method":   "GET",
				"workflow": "noisy-workflow",
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

	handler := router.routes["GET /api/noisy"]
	req := httptest.NewRequest("GET", "/api/noisy", nil)
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

// TestCoercePipelineStatus verifies that coercePipelineStatus handles all
// common numeric and string types that pipeline steps may emit.
func TestCoercePipelineStatus(t *testing.T) {
	tests := []struct {
		name   string
		input  any
		want   int
		wantOK bool
	}{
		{"int", 403, 403, true},
		{"int64", int64(201), 201, true},
		{"float64 whole", float64(200), 200, true},
		{"float64 fractional", float64(200.5), 0, false},
		{"json.Number int", json.Number("404"), 404, true},
		{"json.Number float", json.Number("404.5"), 0, false},
		{"string numeric", "500", 500, true},
		{"string with spaces", " 403 ", 403, true},
		{"string non-numeric", "ok", 0, false},
		{"nil", nil, 0, false},
		{"bool", true, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := coercePipelineStatus(tt.input)
			if ok != tt.wantOK {
				t.Errorf("coercePipelineStatus(%v): ok=%v, want %v", tt.input, ok, tt.wantOK)
			}
			if ok && got != tt.want {
				t.Errorf("coercePipelineStatus(%v): got %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// TestHTTPTrigger_PipelineContextResponse_Float64Status verifies that a
// response_status emitted as float64 (common after generic JSON decoding) is
// correctly coerced into an HTTP status code.
func TestHTTPTrigger_PipelineContextResponse_Float64Status(t *testing.T) {
	app := NewMockApplication()
	router := NewMockHTTPRouter("test-router")
	_ = app.RegisterService("httpRouter", router)

	engine := &pipelineContextResultEngine{result: map[string]any{
		"response_status": float64(422),
		"response_body":   `{"error":"unprocessable"}`,
	}}
	_ = app.RegisterService("workflowEngine", engine)

	trigger := NewHTTPTrigger()
	app.RegisterModule(trigger)

	cfg := map[string]any{
		"routes": []any{
			map[string]any{
				"path":     "/api/validate",
				"method":   "POST",
				"workflow": "validate-wf",
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

	handler := router.routes["POST /api/validate"]
	req := httptest.NewRequest("POST", "/api/validate", nil)
	w := httptest.NewRecorder()
	handler.Handle(w, req)

	if w.Result().StatusCode != 422 {
		t.Errorf("expected 422 from float64 status, got %d", w.Result().StatusCode)
	}
}

// TestHTTPTrigger_PipelineContextResponse_MapStringStringHeaders verifies that
// response_headers emitted as map[string]string are applied correctly.
func TestHTTPTrigger_PipelineContextResponse_MapStringStringHeaders(t *testing.T) {
	app := NewMockApplication()
	router := NewMockHTTPRouter("test-router")
	_ = app.RegisterService("httpRouter", router)

	engine := &pipelineContextResultEngine{result: map[string]any{
		"response_status":  200,
		"response_body":    `ok`,
		"response_headers": map[string]string{"X-Custom": "value"},
	}}
	_ = app.RegisterService("workflowEngine", engine)

	trigger := NewHTTPTrigger()
	app.RegisterModule(trigger)

	cfg := map[string]any{
		"routes": []any{
			map[string]any{
				"path":     "/api/hdr",
				"method":   "GET",
				"workflow": "hdr-wf",
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

	handler := router.routes["GET /api/hdr"]
	req := httptest.NewRequest("GET", "/api/hdr", nil)
	w := httptest.NewRecorder()
	handler.Handle(w, req)

	if w.Result().StatusCode != 200 {
		t.Errorf("expected 200, got %d", w.Result().StatusCode)
	}
	if w.Header().Get("X-Custom") != "value" {
		t.Errorf("expected X-Custom header, got %q", w.Header().Get("X-Custom"))
	}
}

// TestHTTPTrigger_PipelineOutput verifies that when a pipeline sets _pipeline_output
// in the result holder, the HTTP trigger writes it as JSON with status 200 instead
// of falling back to the generic 202 accepted response.
func TestHTTPTrigger_PipelineOutput(t *testing.T) {
	app := NewMockApplication()
	router := NewMockHTTPRouter("test-router")
	_ = app.RegisterService("httpRouter", router)

	engine := &pipelineContextResultEngine{result: map[string]any{
		"_pipeline_output": map[string]any{
			"gameId": "test-123",
			"status": "active",
		},
	}}
	_ = app.RegisterService("workflowEngine", engine)

	trigger := NewHTTPTrigger()
	app.RegisterModule(trigger)

	cfg := map[string]any{
		"routes": []any{
			map[string]any{
				"path":     "/api/game",
				"method":   "GET",
				"workflow": "game-workflow",
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

	handler := router.routes["GET /api/game"]
	if handler == nil {
		t.Fatal("handler not registered")
	}

	req := httptest.NewRequest("GET", "/api/game", nil)
	w := httptest.NewRecorder()
	handler.Handle(w, req)

	resp := w.Result()
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type=application/json, got %q", ct)
	}
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body["gameId"] != "test-123" {
		t.Errorf("expected gameId=test-123, got %v", body["gameId"])
	}
	if body["status"] != "active" {
		t.Errorf("expected status=active, got %v", body["status"])
	}
	// Verify it's not the generic fallback
	if strings.Contains(w.Body.String(), "workflow triggered") {
		t.Error("got generic fallback response instead of pipeline output")
	}
}
