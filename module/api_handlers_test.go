package module

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
	if len(deps) != 1 {
		t.Fatalf("expected 1 dependency, got %d", len(deps))
	}
	if deps[0].Name != "message-broker" {
		t.Errorf("expected dependency 'message-broker', got '%s'", deps[0].Name)
	}
	if deps[0].Required {
		t.Error("expected message-broker to be optional")
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

	var result map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result["success"] != true {
		t.Errorf("expected success=true, got %v", result["success"])
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
