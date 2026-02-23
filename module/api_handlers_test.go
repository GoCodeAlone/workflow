package module

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/CrisisTextLine/modular"
)

func TestNewRESTAPIHandler(t *testing.T) {
	h := NewRESTAPIHandler("test-handler", "orders")
	if h.Name() != "test-handler" {
		t.Errorf("expected name 'test-handler', got '%s'", h.Name())
	}
	if h.resourceName != "orders" {
		t.Errorf("expected resourceName 'orders', got '%s'", h.resourceName)
	}
	if h.resources == nil {
		t.Fatal("expected resources map to be initialized")
	}
}

func TestRESTAPIHandler_ProvidesServices(t *testing.T) {
	h := NewRESTAPIHandler("test-handler", "orders")
	services := h.ProvidesServices()
	if len(services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(services))
	}
	if services[0].Name != "test-handler" {
		t.Errorf("expected service name 'test-handler', got '%s'", services[0].Name)
	}
}

func TestRESTAPIHandler_RequiresServices(t *testing.T) {
	h := NewRESTAPIHandler("test-handler", "orders")
	deps := h.RequiresServices()
	if len(deps) != 2 {
		t.Fatalf("expected 2 dependencies, got %d", len(deps))
	}
	if deps[0].Name != "message-broker" {
		t.Errorf("expected dependency 'message-broker', got '%s'", deps[0].Name)
	}
	if deps[0].Required {
		t.Error("expected message-broker to be optional")
	}
	if deps[1].Name != "persistence" {
		t.Errorf("expected dependency 'persistence', got '%s'", deps[1].Name)
	}
	if deps[1].Required {
		t.Error("expected persistence to be optional")
	}
}

func TestRESTAPIHandler_Init(t *testing.T) {
	app := CreateIsolatedApp(t)
	h := NewRESTAPIHandler("test-handler", "orders")

	if err := h.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if h.app == nil {
		t.Error("expected app to be set")
	}
	if h.logger == nil {
		t.Error("expected logger to be set")
	}
	if h.instanceIDField != "id" {
		t.Errorf("expected default instanceIDField 'id', got '%s'", h.instanceIDField)
	}
}

func setupHandler(t *testing.T) *RESTAPIHandler {
	t.Helper()
	app := CreateIsolatedApp(t)
	h := NewRESTAPIHandler("test-handler", "orders")
	if err := h.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	return h
}

func TestRESTAPIHandler_HandlePost_CreateResource(t *testing.T) {
	h := setupHandler(t)

	body := `{"id": "order-1", "product": "widget", "quantity": 5}`
	req := httptest.NewRequest(http.MethodPost, "/api/orders", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	h.Handle(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d", http.StatusCreated, w.Code)
	}

	var resource RESTResource
	if err := json.NewDecoder(w.Body).Decode(&resource); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resource.ID != "order-1" {
		t.Errorf("expected resource ID 'order-1', got '%s'", resource.ID)
	}
	if resource.State != "new" {
		t.Errorf("expected state 'new', got '%s'", resource.State)
	}
}

func TestRESTAPIHandler_HandlePost_GeneratedID(t *testing.T) {
	h := setupHandler(t)

	body := `{"product": "widget"}`
	req := httptest.NewRequest(http.MethodPost, "/api/orders", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	h.Handle(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d", http.StatusCreated, w.Code)
	}

	var resource RESTResource
	if err := json.NewDecoder(w.Body).Decode(&resource); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resource.ID == "" {
		t.Error("expected generated ID, got empty string")
	}
}

func TestRESTAPIHandler_HandlePost_InvalidBody(t *testing.T) {
	h := setupHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/orders", bytes.NewBufferString("not json"))
	w := httptest.NewRecorder()

	h.Handle(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestRESTAPIHandler_HandlePost_WithState(t *testing.T) {
	h := setupHandler(t)

	body := `{"id": "order-2", "state": "pending"}`
	req := httptest.NewRequest(http.MethodPost, "/api/orders", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	h.Handle(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d", http.StatusCreated, w.Code)
	}

	var resource RESTResource
	if err := json.NewDecoder(w.Body).Decode(&resource); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resource.State != "pending" {
		t.Errorf("expected state 'pending', got '%s'", resource.State)
	}
}

func TestRESTAPIHandler_HandleGetAll_Empty(t *testing.T) {
	h := setupHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/orders", nil)
	w := httptest.NewRecorder()

	h.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resources []RESTResource
	if err := json.NewDecoder(w.Body).Decode(&resources); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resources) != 0 {
		t.Errorf("expected 0 resources, got %d", len(resources))
	}
}

func TestRESTAPIHandler_HandleGetAll_WithResources(t *testing.T) {
	h := setupHandler(t)

	// Create two resources
	for _, id := range []string{"r1", "r2"} {
		body := `{"id": "` + id + `"}`
		req := httptest.NewRequest(http.MethodPost, "/api/orders", bytes.NewBufferString(body))
		w := httptest.NewRecorder()
		h.Handle(w, req)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/orders", nil)
	w := httptest.NewRecorder()

	h.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resources []RESTResource
	if err := json.NewDecoder(w.Body).Decode(&resources); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resources) != 2 {
		t.Errorf("expected 2 resources, got %d", len(resources))
	}
}

func TestRESTAPIHandler_HandleGet_Specific(t *testing.T) {
	h := setupHandler(t)

	// Create resource
	body := `{"id": "order-1", "product": "widget"}`
	req := httptest.NewRequest(http.MethodPost, "/api/orders", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.Handle(w, req)

	// Get the specific resource using PathValue
	req = httptest.NewRequest(http.MethodGet, "/api/orders/{id}", nil)
	req.SetPathValue("id", "order-1")
	w = httptest.NewRecorder()

	h.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resource RESTResource
	if err := json.NewDecoder(w.Body).Decode(&resource); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resource.ID != "order-1" {
		t.Errorf("expected ID 'order-1', got '%s'", resource.ID)
	}
}

func TestRESTAPIHandler_HandleGet_NotFound(t *testing.T) {
	h := setupHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/orders/{id}", nil)
	req.SetPathValue("id", "nonexistent")
	w := httptest.NewRecorder()

	h.Handle(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestRESTAPIHandler_HandlePut_UpdateResource(t *testing.T) {
	h := setupHandler(t)

	// Create resource first
	body := `{"id": "order-1", "product": "widget"}`
	req := httptest.NewRequest(http.MethodPost, "/api/orders", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.Handle(w, req)

	// Update the resource
	updateBody := `{"product": "gadget", "quantity": 10}`
	req = httptest.NewRequest(http.MethodPut, "/api/orders/{id}", bytes.NewBufferString(updateBody))
	req.SetPathValue("id", "order-1")
	w = httptest.NewRecorder()

	h.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resource RESTResource
	if err := json.NewDecoder(w.Body).Decode(&resource); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resource.ID != "order-1" {
		t.Errorf("expected ID 'order-1', got '%s'", resource.ID)
	}
}

func TestRESTAPIHandler_HandlePut_NotFound(t *testing.T) {
	h := setupHandler(t)

	body := `{"product": "gadget"}`
	req := httptest.NewRequest(http.MethodPut, "/api/orders/{id}", bytes.NewBufferString(body))
	req.SetPathValue("id", "nonexistent")
	w := httptest.NewRecorder()

	h.Handle(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestRESTAPIHandler_HandlePut_InvalidBody(t *testing.T) {
	h := setupHandler(t)

	// Create resource first
	body := `{"id": "order-1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/orders", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.Handle(w, req)

	// Update with invalid body
	req = httptest.NewRequest(http.MethodPut, "/api/orders/{id}", bytes.NewBufferString("not json"))
	req.SetPathValue("id", "order-1")
	w = httptest.NewRecorder()

	h.Handle(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestRESTAPIHandler_HandleDelete_Success(t *testing.T) {
	h := setupHandler(t)

	// Create resource first
	body := `{"id": "order-1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/orders", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.Handle(w, req)

	// Delete the resource
	req = httptest.NewRequest(http.MethodDelete, "/api/orders/{id}", nil)
	req.SetPathValue("id", "order-1")
	w = httptest.NewRecorder()

	h.Handle(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected status %d, got %d", http.StatusNoContent, w.Code)
	}

	// Confirm it's gone
	req = httptest.NewRequest(http.MethodGet, "/api/orders/{id}", nil)
	req.SetPathValue("id", "order-1")
	w = httptest.NewRecorder()

	h.Handle(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d after delete, got %d", http.StatusNotFound, w.Code)
	}
}

func TestRESTAPIHandler_HandleDelete_NotFound(t *testing.T) {
	h := setupHandler(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/orders/{id}", nil)
	req.SetPathValue("id", "nonexistent")
	w := httptest.NewRecorder()

	h.Handle(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestRESTAPIHandler_HandleMethodNotAllowed(t *testing.T) {
	h := setupHandler(t)

	req := httptest.NewRequest(http.MethodPatch, "/api/orders", nil)
	w := httptest.NewRecorder()

	h.Handle(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status %d, got %d", http.StatusMethodNotAllowed, w.Code)
	}
}

func TestRESTAPIHandler_HandleTransition_EmptyResourceID(t *testing.T) {
	h := setupHandler(t)

	body := `{"transition": "submit"}`
	req := httptest.NewRequest(http.MethodPut, "/api/orders//transition", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	// Directly call handleTransition with empty ID
	h.handleTransition("", w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestRESTAPIHandler_HandleTransition_InvalidBody(t *testing.T) {
	h := setupHandler(t)

	req := httptest.NewRequest(http.MethodPut, "/api/orders/order-1/transition", bytes.NewBufferString("not json"))
	w := httptest.NewRecorder()

	h.handleTransition("order-1", w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestRESTAPIHandler_HandleTransition_MissingTransitionName(t *testing.T) {
	h := setupHandler(t)

	body := `{"data": {"key": "value"}}`
	req := httptest.NewRequest(http.MethodPut, "/api/orders/order-1/transition", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	h.handleTransition("order-1", w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestRESTAPIHandler_HandleTransition_ResourceNotFound(t *testing.T) {
	h := setupHandler(t)

	body := `{"transition": "submit"}`
	req := httptest.NewRequest(http.MethodPut, "/api/orders/nonexistent/transition", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	h.handleTransition("nonexistent", w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestRESTAPIHandler_HandleTransition_NoEngine(t *testing.T) {
	h := setupHandler(t)

	// Create a resource first
	createBody := `{"id": "order-1", "product": "widget"}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/orders", bytes.NewBufferString(createBody))
	createW := httptest.NewRecorder()
	h.Handle(createW, createReq)

	// Try to transition - no engine available
	body := `{"transition": "submit"}`
	req := httptest.NewRequest(http.MethodPut, "/api/orders/order-1/transition", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	h.handleTransition("order-1", w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
}

func TestRESTAPIHandler_HandleTransition_WithStateMachineEngine(t *testing.T) {
	h := setupHandler(t)

	// Set up a state machine engine in the app
	engine := NewStateMachineEngine("test-engine")
	def := &StateMachineDefinition{
		Name:         "order-workflow",
		InitialState: "new",
		States: map[string]*State{
			"new":       {Name: "new"},
			"submitted": {Name: "submitted"},
		},
		Transitions: map[string]*Transition{
			"submit": {Name: "submit", FromState: "new", ToState: "submitted"},
		},
	}
	if err := engine.RegisterDefinition(def); err != nil {
		t.Fatalf("failed to register definition: %v", err)
	}

	// Register engine as a service
	if err := h.app.RegisterService("test-engine", engine); err != nil {
		t.Fatalf("failed to register engine: %v", err)
	}

	// Create a resource
	createBody := `{"id": "order-1", "product": "widget"}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/orders", bytes.NewBufferString(createBody))
	createW := httptest.NewRecorder()
	h.Handle(createW, createReq)

	// Trigger transition
	body := `{"transition": "submit", "workflowType": "order-workflow"}`
	req := httptest.NewRequest(http.MethodPut, "/api/orders/order-1/transition", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	h.handleTransition("order-1", w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var result map[string]any
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result["success"] != true {
		t.Errorf("expected success=true, got %v", result["success"])
	}
}

func TestRESTAPIHandler_HandleGet_WithStateTracker(t *testing.T) {
	h := setupHandler(t)

	// Register a state tracker service in the app
	tracker := NewStateTracker("workflow.service.statetracker")
	if err := h.app.RegisterService(StateTrackerName, tracker); err != nil {
		t.Fatalf("failed to register state tracker: %v", err)
	}

	// Create a resource
	createBody := `{"id": "order-1", "product": "widget"}`
	createReq := httptest.NewRequest(http.MethodPost, "/api/orders", bytes.NewBufferString(createBody))
	createW := httptest.NewRecorder()
	h.Handle(createW, createReq)

	// Set state for the resource
	tracker.SetState("orders", "order-1", "processing", map[string]any{
		"priority": "high",
	})

	// Get the resource - should be enhanced with state info
	req := httptest.NewRequest(http.MethodGet, "/api/orders/{id}", nil)
	req.SetPathValue("id", "order-1")
	w := httptest.NewRecorder()

	h.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resource RESTResource
	if err := json.NewDecoder(w.Body).Decode(&resource); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resource.State != "processing" {
		t.Errorf("expected state 'processing', got '%s'", resource.State)
	}
	if resource.Data["priority"] != "high" {
		t.Errorf("expected priority 'high' from state tracker, got %v", resource.Data["priority"])
	}
}

func TestRESTAPIHandler_HandlePut_MissingID(t *testing.T) {
	h := setupHandler(t)

	body := `{"product": "widget"}`
	req := httptest.NewRequest(http.MethodPut, "/api/orders", bytes.NewBufferString(body))
	w := httptest.NewRecorder()

	// Directly call handlePut with empty ID
	h.handlePut("", w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestRESTAPIHandler_HandleDelete_MissingID(t *testing.T) {
	h := setupHandler(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/orders", nil)
	w := httptest.NewRecorder()

	// Directly call handleDelete with empty ID
	h.handleDelete("", w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestRESTAPIHandler_HandleGet_ResourceWithModules(t *testing.T) {
	h := setupHandler(t)

	// Create resources
	for i, id := range []string{"a", "b", "c"} {
		body := `{"id": "` + id + `", "order": ` + fmt.Sprintf("%d", i+1) + `}`
		req := httptest.NewRequest(http.MethodPost, "/api/orders", bytes.NewBufferString(body))
		w := httptest.NewRecorder()
		h.Handle(w, req)
	}

	// List all resources (handleGet with empty ID path)
	req := httptest.NewRequest(http.MethodGet, "/api/orders", nil)
	w := httptest.NewRecorder()
	h.handleGet("", w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resources []RESTResource
	if err := json.NewDecoder(w.Body).Decode(&resources); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if len(resources) != 3 {
		t.Errorf("expected 3 resources, got %d", len(resources))
	}
}

func TestRESTAPIHandler_Constructor(t *testing.T) {
	h := NewRESTAPIHandler("test-handler", "orders")
	constructor := h.Constructor()
	if constructor == nil {
		t.Fatal("expected non-nil constructor")
	}

	app := CreateIsolatedApp(t)
	mod, err := constructor(app, nil)
	if err != nil {
		t.Fatalf("constructor failed: %v", err)
	}
	if mod == nil {
		t.Fatal("expected non-nil module from constructor")
	}
	if mod.Name() != "test-handler" {
		t.Errorf("expected name 'test-handler', got '%s'", mod.Name())
	}
}

func TestRESTAPIHandler_ContentTypeJSON(t *testing.T) {
	h := setupHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/orders", nil)
	w := httptest.NewRecorder()

	h.Handle(w, req)

	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got '%s'", ct)
	}
}

func TestRESTAPIHandler_StartStop(t *testing.T) {
	h := setupHandler(t)
	ctx := context.Background()

	if err := h.Start(ctx); err != nil {
		t.Errorf("Start should return nil, got: %v", err)
	}
	if err := h.Stop(ctx); err != nil {
		t.Errorf("Stop should return nil, got: %v", err)
	}
}

func TestRESTAPIHandler_Init_FullSetup(t *testing.T) {
	app := CreateIsolatedApp(t)

	// Register a workflow config section with module config containing workflowType
	workflowCfg := map[string]any{
		"modules": []any{
			map[string]any{
				"name": "full-handler",
				"config": map[string]any{
					"resourceName": "items",
					"workflowType": "item-workflow",
				},
			},
		},
		"workflows": map[string]any{},
	}
	app.RegisterConfigSection("workflow", modular.NewStdConfigProvider(workflowCfg))

	h := NewRESTAPIHandler("full-handler", "orders")
	if err := h.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if h.workflowType != "item-workflow" {
		t.Errorf("expected workflowType 'item-workflow', got '%s'", h.workflowType)
	}
}

func TestRESTAPIHandler_CRUDRoundTrip(t *testing.T) {
	h := setupHandler(t)

	// POST - Create
	body := `{"id": "item-1", "name": "Test Item"}`
	req := httptest.NewRequest(http.MethodPost, "/api/orders", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.Handle(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CREATE: expected %d, got %d", http.StatusCreated, w.Code)
	}

	// GET - Read
	req = httptest.NewRequest(http.MethodGet, "/api/orders/{id}", nil)
	req.SetPathValue("id", "item-1")
	w = httptest.NewRecorder()
	h.Handle(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("READ: expected %d, got %d", http.StatusOK, w.Code)
	}

	// PUT - Update
	updateBody := `{"name": "Updated Item"}`
	req = httptest.NewRequest(http.MethodPut, "/api/orders/{id}", bytes.NewBufferString(updateBody))
	req.SetPathValue("id", "item-1")
	w = httptest.NewRecorder()
	h.Handle(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UPDATE: expected %d, got %d", http.StatusOK, w.Code)
	}

	// DELETE
	req = httptest.NewRequest(http.MethodDelete, "/api/orders/{id}", nil)
	req.SetPathValue("id", "item-1")
	w = httptest.NewRecorder()
	h.Handle(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DELETE: expected %d, got %d", http.StatusNoContent, w.Code)
	}

	// GET after DELETE - should be 404
	req = httptest.NewRequest(http.MethodGet, "/api/orders/{id}", nil)
	req.SetPathValue("id", "item-1")
	w = httptest.NewRecorder()
	h.Handle(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("GET after DELETE: expected %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestRESTAPIHandler_LoadSeedData(t *testing.T) {
	dir := t.TempDir()
	seedFile := filepath.Join(dir, "seed.json")
	seedData := `[
		{"id": "prod-1", "data": {"name": "Widget", "price": 9.99}, "state": "active"},
		{"id": "prod-2", "data": {"name": "Gadget", "price": 19.99}, "state": "active"},
		{"id": "prod-3", "data": {"name": "Doohickey", "price": 4.99}, "state": "draft"}
	]`
	if err := os.WriteFile(seedFile, []byte(seedData), 0644); err != nil {
		t.Fatal(err)
	}

	app := CreateIsolatedApp(t)
	h := NewRESTAPIHandler("seed-handler", "products")
	h.SetSeedFile(seedFile)
	if err := h.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if err := h.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Verify seed data was loaded
	req := httptest.NewRequest(http.MethodGet, "/api/products", nil)
	w := httptest.NewRecorder()
	h.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	var resources []RESTResource
	if err := json.NewDecoder(w.Body).Decode(&resources); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if len(resources) != 3 {
		t.Fatalf("expected 3 seeded resources, got %d", len(resources))
	}

	// Verify specific resource
	req = httptest.NewRequest(http.MethodGet, "/api/products/{id}", nil)
	req.SetPathValue("id", "prod-1")
	w = httptest.NewRecorder()
	h.Handle(w, req)

	var resource RESTResource
	json.NewDecoder(w.Body).Decode(&resource)
	if resource.State != "active" {
		t.Errorf("expected state 'active', got '%s'", resource.State)
	}
	if resource.Data["name"] != "Widget" {
		t.Errorf("expected name 'Widget', got %v", resource.Data["name"])
	}
}

func TestRESTAPIHandler_LoadSeedData_InvalidFile(t *testing.T) {
	app := CreateIsolatedApp(t)
	h := NewRESTAPIHandler("seed-handler", "products")
	h.SetSeedFile("/nonexistent/path/seed.json")
	// Init should succeed but warn about missing seed file
	if err := h.Init(app); err != nil {
		t.Fatalf("Init should not fail for missing seed file: %v", err)
	}
}

func TestRESTAPIHandler_LoadSeedData_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	seedFile := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(seedFile, []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}

	app := CreateIsolatedApp(t)
	h := NewRESTAPIHandler("seed-handler", "products")
	h.SetSeedFile(seedFile)
	// Init should succeed but warn about bad JSON
	if err := h.Init(app); err != nil {
		t.Fatalf("Init should not fail for bad JSON seed: %v", err)
	}
}

func TestRESTAPIHandler_SetRiskPatterns(t *testing.T) {
	h := NewRESTAPIHandler("test-handler", "conversations")

	// Default patterns should be initialized
	if len(h.riskPatterns) == 0 {
		t.Error("expected default riskPatterns to be initialized")
	}

	// Override with custom patterns
	customPatterns := map[string][]string{
		"test-category": {"test phrase", "another phrase"},
	}
	h.SetRiskPatterns(customPatterns)
	if len(h.riskPatterns) != 1 {
		t.Errorf("expected 1 risk pattern category, got %d", len(h.riskPatterns))
	}
	if _, ok := h.riskPatterns["test-category"]; !ok {
		t.Error("expected 'test-category' in riskPatterns")
	}

	// Reset to nil should fall back to defaults in assessRiskLevel
	h.SetRiskPatterns(nil)
	msgs := []any{map[string]any{"body": "kill myself"}}
	level, tags := h.assessRiskLevel(msgs)
	if level == "low" {
		t.Error("expected non-low risk for suicidal message with default patterns")
	}
	if len(tags) == 0 {
		t.Error("expected tags for suicidal message with default patterns")
	}
}

func TestRESTAPIHandler_AssessRiskLevel_CustomPatterns(t *testing.T) {
	h := NewRESTAPIHandler("test-handler", "conversations")
	h.SetRiskPatterns(map[string][]string{
		"custom-risk": {"danger word"},
	})

	// Should detect custom pattern
	msgs := []any{map[string]any{"body": "this contains danger word here"}}
	_, tags := h.assessRiskLevel(msgs)
	found := false
	for _, tag := range tags {
		if tag == "custom-risk" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'custom-risk' tag from custom patterns")
	}

	// Standard patterns should no longer match
	msgs2 := []any{map[string]any{"body": "kill myself"}}
	level2, _ := h.assessRiskLevel(msgs2)
	if level2 != "low" {
		t.Errorf("expected 'low' with custom patterns that don't match 'kill myself', got '%s'", level2)
	}
}
