package module

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/GoCodeAlone/modular"
)

// RESTResource represents a simple in-memory resource store for REST APIs
type RESTResource struct {
	ID         string                 `json:"id"`
	Data       map[string]interface{} `json:"data"`
	State      string                 `json:"state,omitempty"`
	LastUpdate string                 `json:"lastUpdate,omitempty"`
}

// RESTAPIHandler provides CRUD operations for a REST API
type RESTAPIHandler struct {
	name         string
	resourceName string
	resources    map[string]RESTResource
	mu           sync.RWMutex
	eventBroker  MessageProducer // Optional dependency for publishing events
	logger       modular.Logger
	app          modular.Application
}

// RESTAPIHandlerConfig contains configuration for a REST API handler
type RESTAPIHandlerConfig struct {
	ResourceName  string `json:"resourceName" yaml:"resourceName"`
	PublishEvents bool   `json:"publishEvents" yaml:"publishEvents"`
}

// NewRESTAPIHandler creates a new REST API handler
func NewRESTAPIHandler(name, resourceName string) *RESTAPIHandler {
	return &RESTAPIHandler{
		name:         name,
		resourceName: resourceName,
		resources:    make(map[string]RESTResource),
	}
}

// Name returns the unique identifier for this module
func (h *RESTAPIHandler) Name() string {
	return h.name
}

// Constructor returns a function to construct this module with dependencies
func (h *RESTAPIHandler) Constructor() modular.ModuleConstructor {
	return func(app modular.Application, services map[string]any) (modular.Module, error) {
		// Create a new instance with the same name
		handler := NewRESTAPIHandler(h.name, h.resourceName)
		handler.app = app
		handler.logger = app.Logger()

		// Look for a message broker service for event publishing
		if broker, ok := services["message-broker"]; ok {
			if mb, ok := broker.(MessageBroker); ok {
				handler.eventBroker = mb.Producer()
			}
		}

		return handler, nil
	}
}

// Init initializes the module with the application context
func (h *RESTAPIHandler) Init(app modular.Application) error {
	h.app = app
	h.logger = app.Logger()
	// Get configuration if available
	configSection, err := app.GetConfigSection("workflow")
	if err == nil {
		if config := configSection.GetConfig(); config != nil {
			// Try to extract our module's configuration
			// This is a bit verbose but handles nested module configurations
			if modules, ok := config.(map[string]interface{})["modules"].([]interface{}); ok {
				for _, mod := range modules {
					if m, ok := mod.(map[string]interface{}); ok {
						if m["name"] == h.name {
							if cfg, ok := m["config"].(map[string]interface{}); ok {
								if rn, ok := cfg["resourceName"].(string); ok && rn != "" {
									h.resourceName = rn
								}
							}
						}
					}
				}
			}
		}
	}

	return nil
}

// Handle implements the HTTPHandler interface
func (h *RESTAPIHandler) Handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Extract path segments for proper routing
	pathSegments := strings.Split(strings.Trim(r.URL.Path, "/"), "/")

	// Check if this is a resource-specific request (has ID) or a collection request
	id := r.PathValue("id")
	isTransitionRequest := false

	// We expect paths like:
	// - /api/orders (collection)
	// - /api/orders/123 (specific resource)
	// - /api/orders/123/transition (resource action)

	if len(pathSegments) >= 3 && pathSegments[0] == "api" && pathSegments[1] == h.resourceName {
		// Check if this is a transition request
		if len(pathSegments) >= 4 && pathSegments[3] == "transition" {
			isTransitionRequest = true
		}
	}

	//h.logger.Debug(fmt.Sprintf("[%s] %s %s %s %+v\n", h.resourceName, r.Method, r.URL.Path, id, pathSegments))

	// Route based on method and path structure
	switch {
	case isTransitionRequest && r.Method == http.MethodPut:
		// Handle state machine transition request
		h.handleTransition(id, w, r)
	case r.Method == http.MethodGet && id != "":
		// Get a specific resource
		h.handleGet(id, w, r)
	case r.Method == http.MethodGet:
		// List all resources
		h.handleGetAll(w, r)
	case r.Method == http.MethodPost:
		// Create a new resource
		h.handlePost(id, w, r)
	case r.Method == http.MethodPut && id != "":
		// Update an existing resource
		h.handlePut(id, w, r)
	case r.Method == http.MethodDelete && id != "":
		// Delete a resource
		h.handleDelete(id, w, r)
	default:
		// Method not allowed or invalid path
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "Method not allowed or invalid path"})
	}
}

// handleGet handles GET requests for listing or retrieving resources
func (h *RESTAPIHandler) handleGet(id string, w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if id == "" {
		// List all resources
		resources := make([]RESTResource, 0, len(h.resources))
		for _, resource := range h.resources {
			resources = append(resources, resource)
		}
		json.NewEncoder(w).Encode(resources)
		return
	}

	// Get a specific resource
	if resource, ok := h.resources[id]; ok {
		// Check if we have a state tracker we can use to enhance the resource
		var stateTracker interface{}
		_ = h.app.GetService(StateTrackerName, &stateTracker)

		// If we found a state tracker, try to get state info for this resource
		if stateTracker != nil {
			if tracker, ok := stateTracker.(*StateTracker); ok {
				if stateInfo, exists := tracker.GetState(h.resourceName, id); exists {
					// Enhance the resource with state info
					resource.State = stateInfo.CurrentState
					resource.LastUpdate = stateInfo.LastUpdate.Format(time.RFC3339)

					// Update data fields from state info if available
					if stateInfo.Data != nil {
						for k, v := range stateInfo.Data {
							resource.Data[k] = v
						}
					}
				}
			}
		}

		json.NewEncoder(w).Encode(resource)
		return
	}

	// Not found
	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(map[string]string{"error": "Resource not found"})
}

// handleGetAll handles GET requests for listing all resources
func (h *RESTAPIHandler) handleGetAll(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// List all resources
	resources := make([]RESTResource, 0, len(h.resources))
	for _, resource := range h.resources {
		resources = append(resources, resource)
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resources)
}

// handlePost handles POST requests for creating resources
func (h *RESTAPIHandler) handlePost(id string, w http.ResponseWriter, r *http.Request) {
	var data map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"})
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// If ID is provided in the URL, use it; otherwise use the ID from the body
	if id == "" {
		if idFromBody, ok := data["id"].(string); ok && idFromBody != "" {
			id = idFromBody
		} else {
			// Generate an ID (in a real app, use a proper UUID generator)
			id = fmt.Sprintf("%d", len(h.resources)+1)
		}
	}

	// Extract state if present, default to "new" for state machine resources
	state := "new"
	if stateVal, ok := data["state"].(string); ok && stateVal != "" {
		state = stateVal
	}

	// Set the current time for last update
	lastUpdate := time.Now().Format(time.RFC3339)

	// Create or update the resource
	resource := RESTResource{
		ID:         id,
		Data:       data,
		State:      state,
		LastUpdate: lastUpdate,
	}
	h.resources[id] = resource

	// Publish event if broker is available
	if h.eventBroker != nil {
		eventData, _ := json.Marshal(map[string]interface{}{
			"eventType": h.resourceName + ".created",
			"resource":  resource,
		})

		// Non-blocking event publishing
		go func() {
			if err := h.eventBroker.SendMessage(h.resourceName+"-events", eventData); err != nil {
				fmt.Printf("Failed to publish event: %v\n", err)
			}
		}()
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(h.resources[id])
}

// handlePut handles PUT requests for updating resources
func (h *RESTAPIHandler) handlePut(id string, w http.ResponseWriter, r *http.Request) {
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "ID is required for PUT"})
		return
	}

	var data map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"})
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// Check if resource exists
	if _, ok := h.resources[id]; !ok {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Resource not found"})
		return
	}

	// Update the resource
	h.resources[id] = RESTResource{
		ID:   id,
		Data: data,
	}

	json.NewEncoder(w).Encode(h.resources[id])

	// Existing implementation plus event publishing:
	if h.eventBroker != nil {
		eventData, _ := json.Marshal(map[string]interface{}{
			"eventType": h.resourceName + ".updated",
			"resource":  h.resources[id],
		})

		// Non-blocking event publishing
		go func() {
			if err := h.eventBroker.SendMessage(h.resourceName+"-events", eventData); err != nil {
				fmt.Printf("Failed to publish event: %v\n", err)
			}
		}()
	}
}

// handleDelete handles DELETE requests for removing resources
func (h *RESTAPIHandler) handleDelete(id string, w http.ResponseWriter, r *http.Request) {
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "ID is required for DELETE"})
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// Check if resource exists
	if _, ok := h.resources[id]; !ok {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Resource not found"})
		return
	}

	// Delete the resource
	delete(h.resources, id)

	w.WriteHeader(http.StatusNoContent)

	// Existing implementation plus event publishing:
	if h.eventBroker != nil {
		eventData, _ := json.Marshal(map[string]interface{}{
			"eventType":  h.resourceName + ".deleted",
			"resourceId": id,
		})

		// Non-blocking event publishing
		go func() {
			if err := h.eventBroker.SendMessage(h.resourceName+"-events", eventData); err != nil {
				fmt.Printf("Failed to publish event: %v\n", err)
			}
		}()
	}
}

// handleTransition handles state transitions for state machine resources
func (h *RESTAPIHandler) handleTransition(id string, w http.ResponseWriter, r *http.Request) {
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Resource ID is required for transition"})
		return
	}

	// Parse the transition request
	var transitionRequest struct {
		Transition string                 `json:"transition"`
		Data       map[string]interface{} `json:"data,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&transitionRequest); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid transition request format"})
		return
	}

	if transitionRequest.Transition == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Transition name is required"})
		return
	}

	// Prepare the workflow data
	workflowData := make(map[string]interface{})

	// Merge existing resource data
	h.mu.RLock()
	resource, exists := h.resources[id]
	h.mu.RUnlock()

	if !exists {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Resource not found"})
		return
	}

	// Add resource data to workflow data
	for k, v := range resource.Data {
		workflowData[k] = v
	}

	// Add custom transition data if provided
	if transitionRequest.Data != nil {
		for k, v := range transitionRequest.Data {
			workflowData[k] = v
		}
	}

	// Ensure we have the required fields
	workflowData["id"] = id
	workflowData["instanceId"] = id

	// Find workflow engine to trigger the transition
	var engine interface{}

	// Look for the workflow engine in the service registry
	for name, svc := range h.app.SvcRegistry() {
		if strings.Contains(strings.ToLower(name), "engine") ||
			strings.Contains(strings.ToLower(name), "workflow") ||
			strings.Contains(strings.ToLower(name), "processor") {
			engine = svc
			break
		}
	}

	if engine == nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Workflow engine not found"})
		return
	}

	// Try to trigger the workflow
	var result map[string]interface{}
	var err error

	// Try different engine types
	switch e := engine.(type) {
	case interface {
		TriggerWorkflow(ctx context.Context, workflowType string, action string, data map[string]interface{}) error
	}:
		// Using the main engine
		err = e.TriggerWorkflow(r.Context(), "statemachine", transitionRequest.Transition, workflowData)
		result = map[string]interface{}{
			"success":    err == nil,
			"id":         id,
			"transition": transitionRequest.Transition,
		}

	case interface {
		TriggerTransition(ctx context.Context, instanceID, transitionID string, data map[string]interface{}) error
	}:
		// Using the state machine directly
		err = e.TriggerTransition(r.Context(), id, transitionRequest.Transition, workflowData)
		result = map[string]interface{}{
			"success":    err == nil,
			"id":         id,
			"transition": transitionRequest.Transition,
		}

	default:
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Workflow engine does not support transitions"})
		return
	}

	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":    false,
			"error":      err.Error(),
			"transition": transitionRequest.Transition,
		})
		return
	}

	// Now we need to query the state machine for the current state
	var currentState string
	var lastUpdate = time.Now().Format(time.RFC3339)

	// Try to get the current state from the state machine engine
	switch e := engine.(type) {
	case interface {
		GetInstance(instanceID string) (*WorkflowInstance, error)
	}:
		// If the engine has a direct method to get instance state
		instance, err := e.GetInstance(id)
		if err == nil && instance != nil {
			currentState = instance.CurrentState
		}
	case interface {
		GetWorkflowState(ctx context.Context, workflowType string, instanceID string) (map[string]interface{}, error)
	}:
		// Try a more generic method
		stateData, err := e.GetWorkflowState(r.Context(), "statemachine", id)
		if err == nil && stateData != nil {
			if state, ok := stateData["currentState"].(string); ok {
				currentState = state
			}
		}
	}

	// Update the resource with the current state
	if currentState != "" {
		h.mu.Lock()

		// Get the existing resource
		if existingResource, exists := h.resources[id]; exists {
			// Update the state and lastUpdate fields
			existingResource.State = currentState
			existingResource.LastUpdate = lastUpdate

			// Also update the Data map to reflect the current state
			existingResource.Data["state"] = currentState
			existingResource.Data["lastUpdate"] = lastUpdate

			// Save the updated resource
			h.resources[id] = existingResource

			// Add the updated state to the result
			result["state"] = currentState
			result["lastUpdate"] = lastUpdate
			result["resource"] = existingResource
		}

		h.mu.Unlock()
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(result)
}

// Start is a no-op for this handler
func (h *RESTAPIHandler) Start(ctx context.Context) error {
	return nil
}

// Stop is a no-op for this handler
func (h *RESTAPIHandler) Stop(ctx context.Context) error {
	return nil
}

// ProvidesServices returns the services provided by this module
func (h *RESTAPIHandler) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        h.name,
			Description: fmt.Sprintf("REST API handler for %s resource", h.resourceName),
			Instance:    h,
		},
	}
}

// RequiresServices returns the services required by this module
func (h *RESTAPIHandler) RequiresServices() []modular.ServiceDependency {
	return []modular.ServiceDependency{
		{
			Name:     "message-broker",
			Required: false, // Optional dependency
		},
	}
}
