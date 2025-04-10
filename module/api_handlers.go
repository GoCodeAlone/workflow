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

	// Workflow-related fields
	workflowType     string // The type of workflow to use (e.g., "order-workflow")
	workflowEngine   string // The name of the workflow engine service to use
	instanceIDPrefix string // Optional prefix for workflow instance IDs
	instanceIDField  string // Field in resource data to use for instance ID (defaults to "id")
}

// RESTAPIHandlerConfig contains configuration for a REST API handler
type RESTAPIHandlerConfig struct {
	ResourceName     string `json:"resourceName" yaml:"resourceName"`
	PublishEvents    bool   `json:"publishEvents" yaml:"publishEvents"`
	WorkflowType     string `json:"workflowType" yaml:"workflowType"`         // The type of workflow to use for state machine operations
	WorkflowEngine   string `json:"workflowEngine" yaml:"workflowEngine"`     // The name of the workflow engine to use
	InstanceIDPrefix string `json:"instanceIDPrefix" yaml:"instanceIDPrefix"` // Optional prefix for workflow instance IDs
	InstanceIDField  string `json:"instanceIDField" yaml:"instanceIDField"`   // Field in resource data to use for instance ID (defaults to "id")
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

	// Default values for workflow configuration
	h.instanceIDField = "id" // Default to using "id" field if not specified

	// Get configuration if available
	configSection, err := app.GetConfigSection("workflow")
	if err == nil && configSection != nil {
		if config := configSection.GetConfig(); config != nil {
			// Try to extract our module's configuration
			// This is a bit verbose but handles nested module configurations
			if modules, ok := config.(map[string]interface{})["modules"].([]interface{}); ok {
				for _, mod := range modules {
					if m, ok := mod.(map[string]interface{}); ok {
						if m["name"] == h.name {
							if cfg, ok := m["config"].(map[string]interface{}); ok {
								// Extract resource name
								if rn, ok := cfg["resourceName"].(string); ok && rn != "" {
									h.resourceName = rn
								}

								// Extract workflow type
								if wt, ok := cfg["workflowType"].(string); ok && wt != "" {
									h.workflowType = wt
								}

								// Extract workflow engine
								if we, ok := cfg["workflowEngine"].(string); ok && we != "" {
									h.workflowEngine = we
								}

								// Extract instance ID prefix
								if prefix, ok := cfg["instanceIDPrefix"].(string); ok {
									h.instanceIDPrefix = prefix
								}

								// Extract instance ID field
								if field, ok := cfg["instanceIDField"].(string); ok && field != "" {
									h.instanceIDField = field
								}
							}
						}
					}
				}
			}

			// If workflowType is not set but we have a state machine configuration,
			// try to extract the default workflow type from there
			if h.workflowType == "" {
				if statemachine, ok := config.(map[string]interface{})["workflows"].(map[string]interface{})["statemachine"]; ok {
					if smConfig, ok := statemachine.(map[string]interface{}); ok {
						if defs, ok := smConfig["definitions"].([]interface{}); ok && len(defs) > 0 {
							if def, ok := defs[0].(map[string]interface{}); ok {
								if name, ok := def["name"].(string); ok && name != "" {
									h.workflowType = name
									h.logger.Info(fmt.Sprintf("Using default workflow type from state machine definition: %s", h.workflowType))
								}
							}
						}
					}
				}
			}

			// If workflow engine is not set but we have a state machine configuration,
			// try to extract the engine name from there
			if h.workflowEngine == "" {
				if statemachine, ok := config.(map[string]interface{})["workflows"].(map[string]interface{})["statemachine"]; ok {
					if smConfig, ok := statemachine.(map[string]interface{}); ok {
						if engine, ok := smConfig["engine"].(string); ok && engine != "" {
							h.workflowEngine = engine
							h.logger.Info(fmt.Sprintf("Using state machine engine from configuration: %s", h.workflowEngine))
						}
					}
				}
			}
		}
	}

	// Log workflow configuration
	if h.workflowType != "" {
		h.logger.Info(fmt.Sprintf("REST API handler '%s' configured with workflow type: %s", h.name, h.workflowType))
		if h.workflowEngine != "" {
			h.logger.Info(fmt.Sprintf("Using workflow engine: %s", h.workflowEngine))
		}
		if h.instanceIDPrefix != "" {
			h.logger.Info(fmt.Sprintf("Using instance ID prefix: %s", h.instanceIDPrefix))
		}
		h.logger.Info(fmt.Sprintf("Using instance ID field: %s", h.instanceIDField))
	}

	return nil
}

// Handle implements the HTTPHandler interface
func (h *RESTAPIHandler) Handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Extract path segments for proper routing
	pathSegments := strings.Split(strings.Trim(r.URL.Path, "/"), "/")

	// Check if this is a resource-specific request (has ID) or a collection request
	resourceId := r.PathValue("id")
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

	// Route based on method and path structure
	switch {
	case isTransitionRequest && r.Method == http.MethodPut:
		// Handle state machine transition request
		h.handleTransition(resourceId, w, r)
	case r.Method == http.MethodGet && resourceId != "":
		// Get a specific resource
		h.handleGet(resourceId, w, r)
	case r.Method == http.MethodGet:
		// List all resources
		h.handleGetAll(w, r)
	case r.Method == http.MethodPost:
		// Create a new resource
		h.handlePost(resourceId, w, r)
	case r.Method == http.MethodPut && resourceId != "":
		// Update an existing resource
		h.handlePut(resourceId, w, r)
	case r.Method == http.MethodDelete && resourceId != "":
		// Delete a resource
		h.handleDelete(resourceId, w, r)
	default:
		// Method not allowed or invalid path
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "Method not allowed or invalid path"})
	}
}

// handleGet handles GET requests for listing or retrieving resources
func (h *RESTAPIHandler) handleGet(resourceId string, w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if resourceId == "" {
		// List all resources
		resources := make([]RESTResource, 0, len(h.resources))
		for _, resource := range h.resources {
			resources = append(resources, resource)
		}
		json.NewEncoder(w).Encode(resources)
		return
	}

	// Get a specific resource
	if resource, ok := h.resources[resourceId]; ok {
		// Check if we have a state tracker we can use to enhance the resource
		var stateTracker interface{}
		_ = h.app.GetService(StateTrackerName, &stateTracker)

		// If we found a state tracker, try to get state info for this resource
		if stateTracker != nil {
			if tracker, ok := stateTracker.(*StateTracker); ok {
				if stateInfo, exists := tracker.GetState(h.resourceName, resourceId); exists {
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
func (h *RESTAPIHandler) handlePost(resourceId string, w http.ResponseWriter, r *http.Request) {
	var data map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"})
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// If ID is provided in the URL, use it; otherwise use the ID from the body
	if resourceId == "" {
		if idFromBody, ok := data["id"].(string); ok && idFromBody != "" {
			resourceId = idFromBody
		} else {
			// Generate an ID (TODO: use a proper UUID generator)
			resourceId = fmt.Sprintf("%d", len(h.resources)+1)
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
		ID:         resourceId,
		Data:       data,
		State:      state,
		LastUpdate: lastUpdate,
	}
	h.resources[resourceId] = resource

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
	json.NewEncoder(w).Encode(h.resources[resourceId])
}

// handlePut handles PUT requests for updating resources
func (h *RESTAPIHandler) handlePut(resourceId string, w http.ResponseWriter, r *http.Request) {
	if resourceId == "" {
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
	if _, ok := h.resources[resourceId]; !ok {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Resource not found"})
		return
	}

	// Update the resource
	h.resources[resourceId] = RESTResource{
		ID:   resourceId,
		Data: data,
	}

	json.NewEncoder(w).Encode(h.resources[resourceId])

	// Existing implementation plus event publishing:
	if h.eventBroker != nil {
		eventData, _ := json.Marshal(map[string]interface{}{
			"eventType": h.resourceName + ".updated",
			"resource":  h.resources[resourceId],
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
func (h *RESTAPIHandler) handleDelete(resourceId string, w http.ResponseWriter, r *http.Request) {
	if resourceId == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "ID is required for DELETE"})
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// Check if resource exists
	if _, ok := h.resources[resourceId]; !ok {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "Resource not found"})
		return
	}

	// Delete the resource
	delete(h.resources, resourceId)

	w.WriteHeader(http.StatusNoContent)

	// Existing implementation plus event publishing:
	if h.eventBroker != nil {
		eventData, _ := json.Marshal(map[string]interface{}{
			"eventType":  h.resourceName + ".deleted",
			"resourceId": resourceId,
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
func (h *RESTAPIHandler) handleTransition(resourceId string, w http.ResponseWriter, r *http.Request) {
	if resourceId == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Resource ID is required for transition"})
		return
	}

	// Parse the transition request
	var transitionRequest struct {
		Transition   string                 `json:"transition"`
		Data         map[string]interface{} `json:"data,omitempty"`
		WorkflowType string                 `json:"workflowType,omitempty"` // Optional workflow type override
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
	resource, exists := h.resources[resourceId]
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

	// Determine the workflow type to use
	workflowType := h.workflowType // Use configured workflow type by default

	// If a workflow type was specified in the transition request, use that instead
	if transitionRequest.WorkflowType != "" {
		workflowType = transitionRequest.WorkflowType
	}

	// If we still don't have a workflow type, check the resource data for one
	if workflowType == "" {
		if wt, ok := workflowData["workflowType"].(string); ok && wt != "" {
			workflowType = wt
		} else {
			// Use a default workflow type if we have nothing else
			workflowType = "order-workflow" // Fallback default
		}
	}

	// Generate the instance ID using our configuration
	var instanceId string

	// Check if we have a specific instance ID field configured
	if h.instanceIDField != "" && h.instanceIDField != "id" {
		// Try to get the instance ID from the specified field in the resource data
		if idVal, ok := workflowData[h.instanceIDField].(string); ok && idVal != "" {
			instanceId = idVal
		}
	}

	// If we didn't get an ID from a custom field, use the resource ID
	if instanceId == "" {
		instanceId = resourceId
	}

	// Add prefix if configured
	if h.instanceIDPrefix != "" {
		instanceId = h.instanceIDPrefix + instanceId
	}

	// Set the required IDs in the workflow data
	workflowData["id"] = resourceId             // Original resource ID
	workflowData["instanceId"] = instanceId     // Workflow instance ID (with optional prefix)
	workflowData["workflowType"] = workflowType // Workflow type

	// Find the workflow engine to use
	var engine interface{}
	var stateMachineEngine *StateMachineEngine
	var isStateMachineEngine bool

	// First, try to use the specifically configured engine if available
	if h.workflowEngine != "" {
		var engineSvc interface{}
		if err := h.app.GetService(h.workflowEngine, &engineSvc); err == nil && engineSvc != nil {
			engine = engineSvc
			if sm, ok := engineSvc.(*StateMachineEngine); ok {
				stateMachineEngine = sm
				isStateMachineEngine = true
			}
			h.logger.Debug(fmt.Sprintf("Using configured workflow engine: %s", h.workflowEngine))
		} else {
			h.logger.Warn(fmt.Sprintf("Configured workflow engine '%s' not found, will try to discover one", h.workflowEngine))
		}
	}

	// If no specific engine was configured or found, try to find one from a connector
	if engine == nil {
		var stateConnector interface{}
		if err := h.app.GetService(StateMachineStateConnectorName, &stateConnector); err == nil && stateConnector != nil {
			if connector, ok := stateConnector.(*StateMachineStateConnector); ok {
				// Get the engine name for this resource type
				if engineName, found := connector.GetEngineForResourceType(h.resourceName); found {
					// Get the state machine engine by name
					var engineSvc interface{}
					if err := h.app.GetService(engineName, &engineSvc); err == nil && engineSvc != nil {
						engine = engineSvc
						if sm, ok := engineSvc.(*StateMachineEngine); ok {
							stateMachineEngine = sm
							isStateMachineEngine = true
						}
						h.logger.Debug(fmt.Sprintf("Found workflow engine from connector: %s", engineName))
					}
				}
			}
		}
	}

	// If still not found, try to find any state machine engine
	if engine == nil {
		for name, svc := range h.app.SvcRegistry() {
			if sm, ok := svc.(*StateMachineEngine); ok {
				engine = sm
				stateMachineEngine = sm
				isStateMachineEngine = true
				h.logger.Debug(fmt.Sprintf("Found state machine engine: %s", name))
				break
			}
		}
	}

	// If still not found, look for any engine-like service
	if engine == nil {
		for name, svc := range h.app.SvcRegistry() {
			if strings.Contains(strings.ToLower(name), "engine") ||
				strings.Contains(strings.ToLower(name), "workflow") ||
				strings.Contains(strings.ToLower(name), "processor") {
				engine = svc
				if sm, ok := svc.(*StateMachineEngine); ok {
					stateMachineEngine = sm
					isStateMachineEngine = true
				}
				h.logger.Debug(fmt.Sprintf("Found potential workflow engine: %s", name))
				break
			}
		}
	}

	if engine == nil {
		h.logger.Error("No workflow engine found. Available services:")
		for name := range h.app.SvcRegistry() {
			h.logger.Debug(" - " + name)
		}

		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Workflow engine not found"})
		return
	}

	// Check if the workflow instance exists, and create it if it doesn't
	var instanceExists bool
	if isStateMachineEngine {
		// Check if the instance exists
		existingInstance, err := stateMachineEngine.GetInstance(instanceId)
		instanceExists = (err == nil && existingInstance != nil)

		// If the instance doesn't exist, create it
		if !instanceExists {
			h.logger.Info(fmt.Sprintf("Creating new workflow instance '%s' of type '%s'", instanceId, workflowType))
			_, err := stateMachineEngine.CreateWorkflow(workflowType, instanceId, workflowData)
			if err != nil {
				h.logger.Error(fmt.Sprintf("Failed to create workflow instance: %s", err.Error()))
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"success":    false,
					"error":      fmt.Sprintf("Failed to create workflow instance: %s", err.Error()),
					"id":         resourceId,
					"instanceId": instanceId,
				})
				return
			}
			h.logger.Info(fmt.Sprintf("Successfully created workflow instance '%s'", instanceId))

			// If this is the very first transition and it's not from the initial state,
			// we might need to skip it since the instance was just created
			if transitionRequest.Transition == "submit_order" &&
				resource.State == "new" {
				// This is expected behavior - the instance is created in the "new" state
				// and the first transition is typically from "new" to another state
				h.logger.Debug("First transition for new workflow instance")
			}
		}
	}

	// Try to trigger the workflow transition
	var result map[string]interface{}
	var err error

	// Try different engine types
	switch e := engine.(type) {
	case interface {
		TriggerWorkflow(ctx context.Context, workflowType string, action string, data map[string]interface{}) error
	}:
		// Using the main workflow engine
		h.logger.Info(fmt.Sprintf("Triggering workflow '%s' with action '%s' for instance '%s'",
			workflowType, transitionRequest.Transition, instanceId))
		err = e.TriggerWorkflow(r.Context(), "statemachine", transitionRequest.Transition, workflowData)
		result = map[string]interface{}{
			"success":    err == nil,
			"id":         resourceId,
			"instanceId": instanceId,
			"transition": transitionRequest.Transition,
		}

	case interface {
		TriggerTransition(ctx context.Context, instanceID string, transitionID string, data map[string]interface{}) error
	}:
		// Using the state machine engine directly
		h.logger.Info(fmt.Sprintf("Triggering transition '%s' for instance '%s'",
			transitionRequest.Transition, instanceId))
		err = e.TriggerTransition(r.Context(), instanceId, transitionRequest.Transition, workflowData)
		result = map[string]interface{}{
			"success":    err == nil,
			"id":         resourceId,
			"instanceId": instanceId,
			"transition": transitionRequest.Transition,
		}

	default:
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Workflow engine does not support transitions"})
		return
	}

	if err != nil {
		h.logger.Error(fmt.Sprintf("Transition failed: %s", err.Error()))
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":    false,
			"error":      err.Error(),
			"id":         resourceId,
			"instanceId": instanceId,
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
		instance, err := e.GetInstance(instanceId)
		if err == nil && instance != nil {
			currentState = instance.CurrentState
			h.logger.Debug(fmt.Sprintf("Retrieved current state from engine: %s", currentState))
		} else if err != nil {
			h.logger.Warn(fmt.Sprintf("Failed to get instance state: %s", err.Error()))
		}
	case interface {
		GetWorkflowState(ctx context.Context, workflowType string, instanceID string) (map[string]interface{}, error)
	}:
		// Try a more generic method
		stateData, err := e.GetWorkflowState(r.Context(), workflowType, instanceId)
		if err == nil && stateData != nil {
			if state, ok := stateData["currentState"].(string); ok {
				currentState = state
				h.logger.Debug(fmt.Sprintf("Retrieved current state from workflow state: %s", currentState))
			}
		} else if err != nil {
			h.logger.Warn(fmt.Sprintf("Failed to get workflow state: %s", err.Error()))
		}
	}

	// If we couldn't get the state from the engine, try the state tracker
	if currentState == "" {
		var stateTracker interface{}
		if err := h.app.GetService(StateTrackerName, &stateTracker); err == nil && stateTracker != nil {
			if tracker, ok := stateTracker.(*StateTracker); ok {
				if stateInfo, exists := tracker.GetState(h.resourceName, resourceId); exists {
					currentState = stateInfo.CurrentState
					h.logger.Debug(fmt.Sprintf("Retrieved current state from state tracker: %s", currentState))
				}
			}
		}
	}

	// Update the resource with the current state
	if currentState != "" {
		h.mu.Lock()

		// Get the existing resource
		if existingResource, exists := h.resources[resourceId]; exists {
			// Update the state and lastUpdate fields
			existingResource.State = currentState
			existingResource.LastUpdate = lastUpdate

			// Also update the Data map to reflect the current state
			existingResource.Data["state"] = currentState
			existingResource.Data["lastUpdate"] = lastUpdate
			existingResource.Data["workflowType"] = workflowType // Save the workflow type
			existingResource.Data["instanceId"] = instanceId     // Save the instance ID

			// Save the updated resource
			h.resources[resourceId] = existingResource

			// Add the updated state to the result
			result["state"] = currentState
			result["lastUpdate"] = lastUpdate
			result["resource"] = existingResource
		}

		h.mu.Unlock()
	} else {
		h.logger.Warn("Could not determine the current state after transition")
	}

	h.logger.Info(fmt.Sprintf("Transition '%s' completed successfully for resource '%s'",
		transitionRequest.Transition, resourceId))

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
