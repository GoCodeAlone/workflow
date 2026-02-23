package module

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"strings"
	"time"
)

// startWorkflowForResource creates a workflow instance and triggers the initial transition
// for a newly created resource. Uses background context for async processing since
// the HTTP request context is cancelled when the handler returns.
func (h *RESTAPIHandler) startWorkflowForResource(_ context.Context, resourceId string, resource RESTResource) {
	// Find the state machine engine
	var engineSvc any
	if err := h.app.GetService(h.workflowEngine, &engineSvc); err != nil {
		h.logger.Warn(fmt.Sprintf("Workflow engine '%s' not found: %v", h.workflowEngine, err))
		return
	}

	smEngine, ok := engineSvc.(*StateMachineEngine)
	if !ok {
		h.logger.Warn(fmt.Sprintf("Service '%s' is not a StateMachineEngine", h.workflowEngine))
		return
	}

	// Build the instance ID
	instanceId := resourceId
	if h.instanceIDPrefix != "" {
		instanceId = h.instanceIDPrefix + resourceId
	}

	// Create the workflow instance
	_, err := smEngine.CreateWorkflow(h.workflowType, instanceId, resource.Data)
	if err != nil {
		h.logger.Error(fmt.Sprintf("Failed to create workflow instance '%s': %v", instanceId, err))
		return
	}
	h.logger.Info(fmt.Sprintf("Created workflow instance '%s' for resource '%s'", instanceId, resourceId))

	// Trigger the initial transition asynchronously so we don't block the HTTP response.
	// Use context.Background() since the HTTP request context is cancelled when the
	// handler returns, which would abort the processing pipeline.
	go func() {
		bgCtx := context.Background()
		transitionName := h.initialTransition
		if transitionName == "" {
			transitionName = "start_validation" // default convention
		}
		if err := smEngine.TriggerTransition(bgCtx, instanceId, transitionName, resource.Data); err != nil {
			h.logger.Warn(fmt.Sprintf("Failed to trigger initial transition '%s' for '%s': %v",
				transitionName, instanceId, err))
		} else {
			h.logger.Info(fmt.Sprintf("Triggered '%s' for workflow instance '%s'", transitionName, instanceId))
			// Update the resource state from the engine after the transition chain completes
			h.syncResourceStateFromEngine(instanceId, resourceId, smEngine)
		}
	}()
}

// syncResourceStateFromEngine reads the workflow instance state and updates the in-memory resource.
// It polls the state machine until the state settles (stops changing) or a timeout is reached,
// which handles multi-step pipelines that progress through several states asynchronously.
func (h *RESTAPIHandler) syncResourceStateFromEngine(instanceId, resourceId string, engine *StateMachineEngine) {
	const (
		pollInterval = 300 * time.Millisecond
		maxWait      = 5 * time.Second
	)
	time.Sleep(pollInterval) // initial wait for first transition

	var lastState string
	var stableCount int
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		inst, err := engine.GetInstance(instanceId)
		if err != nil || inst == nil {
			return
		}
		if inst.CurrentState == lastState {
			stableCount++
			if stableCount >= 2 || inst.Completed {
				break // State hasn't changed for 2 consecutive polls — pipeline settled
			}
		} else {
			lastState = inst.CurrentState
			stableCount = 0
		}
		time.Sleep(pollInterval)
	}

	instance, err := engine.GetInstance(instanceId)
	if err != nil || instance == nil {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if res, exists := h.resources[resourceId]; exists {
		res.State = instance.CurrentState
		res.LastUpdate = instance.LastUpdated.Format(time.RFC3339)
		res.Data["state"] = res.State
		res.Data["lastUpdate"] = res.LastUpdate
		h.resources[resourceId] = res

		// Write-through to persistence
		if h.persistence != nil {
			_ = h.persistence.SaveResource(h.resourceName, res.ID, res.Data)
		}
	}
}

// handleTransition handles state transitions for state machine resources.
func (h *RESTAPIHandler) handleTransition(resourceId string, w http.ResponseWriter, r *http.Request) {
	if resourceId == "" {
		w.WriteHeader(http.StatusBadRequest)
		if err := json.NewEncoder(w).Encode(map[string]string{"error": "Resource ID is required for transition"}); err != nil {
			_ = err
		}
		return
	}

	// Limit request body size to prevent denial-of-service
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)

	// Parse the transition request
	var transitionRequest struct {
		Transition   string         `json:"transition"`
		Data         map[string]any `json:"data,omitempty"`
		WorkflowType string         `json:"workflowType,omitempty"` // Optional workflow type override
	}

	if err := json.NewDecoder(r.Body).Decode(&transitionRequest); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		if encErr := json.NewEncoder(w).Encode(map[string]string{"error": "Invalid transition request format"}); encErr != nil {
			_ = encErr
		}
		return
	}

	if transitionRequest.Transition == "" {
		w.WriteHeader(http.StatusBadRequest)
		if err := json.NewEncoder(w).Encode(map[string]string{"error": "Transition name is required"}); err != nil {
			_ = err
		}
		return
	}

	// Prepare the workflow data
	workflowData := make(map[string]any)

	// Merge existing resource data
	h.mu.RLock()
	resource, exists := h.resources[resourceId]
	h.mu.RUnlock()

	if !exists {
		w.WriteHeader(http.StatusNotFound)
		if err := json.NewEncoder(w).Encode(map[string]string{"error": "Resource not found"}); err != nil {
			_ = err
		}
		return
	}

	maps.Copy(workflowData, resource.Data)

	// Add custom transition data if provided
	if transitionRequest.Data != nil {
		maps.Copy(workflowData, transitionRequest.Data)
	}

	// Determine the workflow type to use
	workflowType := h.workflowType

	// If a workflow type was specified in the transition request, use that instead
	if transitionRequest.WorkflowType != "" {
		workflowType = transitionRequest.WorkflowType
	}

	// If still not set, check the resource data
	if workflowType == "" {
		if wt, ok := workflowData["workflowType"].(string); ok && wt != "" {
			workflowType = wt
		}
	}

	// Generate the instance ID using our configuration
	var instanceId string
	if h.instanceIDField != "" && h.instanceIDField != "id" {
		if idVal, ok := workflowData[h.instanceIDField].(string); ok && idVal != "" {
			instanceId = idVal
		}
	}
	if instanceId == "" {
		instanceId = resourceId
	}
	if h.instanceIDPrefix != "" {
		instanceId = h.instanceIDPrefix + instanceId
	}

	// Set the required IDs in the workflow data
	workflowData["id"] = resourceId
	workflowData["instanceId"] = instanceId
	if workflowType != "" {
		workflowData["workflowType"] = workflowType
	}

	// Find the workflow engine to use
	var engine any
	var stateMachineEngine *StateMachineEngine
	var isStateMachineEngine bool

	// First, try to use the specifically configured engine if available
	if h.workflowEngine != "" {
		var engineSvc any
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
		var stateConnector any
		if err := h.app.GetService(StateMachineStateConnectorName, &stateConnector); err == nil && stateConnector != nil {
			if connector, ok := stateConnector.(*StateMachineStateConnector); ok {
				if engineName, found := connector.GetEngineForResourceType(h.resourceName); found {
					var engineSvc any
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
		if err := json.NewEncoder(w).Encode(map[string]string{"error": "Workflow engine not found"}); err != nil {
			_ = err
		}
		return
	}

	// Check if the workflow instance exists, and create it if it doesn't
	if isStateMachineEngine {
		existingInstance, err := stateMachineEngine.GetInstance(instanceId)
		if err != nil || existingInstance == nil {
			if workflowType == "" {
				w.WriteHeader(http.StatusBadRequest)
				if err := json.NewEncoder(w).Encode(map[string]string{"error": "Workflow type is required to create a new instance"}); err != nil {
					_ = err
				}
				return
			}
			h.logger.Info(fmt.Sprintf("Creating new workflow instance '%s' of type '%s'", instanceId, workflowType))
			_, err := stateMachineEngine.CreateWorkflow(workflowType, instanceId, workflowData)
			if err != nil {
				h.logger.Error(fmt.Sprintf("Failed to create workflow instance: %s", err.Error()))
				w.WriteHeader(http.StatusInternalServerError)
				if encErr := json.NewEncoder(w).Encode(map[string]any{
					"success":    false,
					"error":      fmt.Sprintf("Failed to create workflow instance: %s", err.Error()),
					"id":         resourceId,
					"instanceId": instanceId,
				}); encErr != nil {
					_ = encErr
				}
				return
			}
			h.logger.Info(fmt.Sprintf("Successfully created workflow instance '%s'", instanceId))
		}
	}

	// Try to trigger the workflow transition
	var result map[string]any
	var err error

	switch e := engine.(type) {
	case interface {
		TriggerWorkflow(ctx context.Context, workflowType string, action string, data map[string]any) error
	}:
		h.logger.Info(fmt.Sprintf("Triggering workflow '%s' with action '%s' for instance '%s'",
			workflowType, transitionRequest.Transition, instanceId))
		err = e.TriggerWorkflow(r.Context(), "statemachine", transitionRequest.Transition, workflowData)
		result = map[string]any{
			"success":    err == nil,
			"id":         resourceId,
			"instanceId": instanceId,
			"transition": transitionRequest.Transition,
		}

	case interface {
		TriggerTransition(ctx context.Context, instanceID string, transitionID string, data map[string]any) error
	}:
		h.logger.Info(fmt.Sprintf("Triggering transition '%s' for instance '%s'",
			transitionRequest.Transition, instanceId))
		err = e.TriggerTransition(r.Context(), instanceId, transitionRequest.Transition, workflowData)
		result = map[string]any{
			"success":    err == nil,
			"id":         resourceId,
			"instanceId": instanceId,
			"transition": transitionRequest.Transition,
		}

	default:
		w.WriteHeader(http.StatusInternalServerError)
		if err := json.NewEncoder(w).Encode(map[string]string{"error": "Workflow engine does not support transitions"}); err != nil {
			_ = err
		}
		return
	}

	if err != nil {
		h.logger.Error(fmt.Sprintf("Transition failed: %s", err.Error()))
		w.WriteHeader(http.StatusBadRequest)
		if encErr := json.NewEncoder(w).Encode(map[string]any{
			"success":    false,
			"error":      err.Error(),
			"id":         resourceId,
			"instanceId": instanceId,
			"transition": transitionRequest.Transition,
		}); encErr != nil {
			_ = encErr
		}
		return
	}

	// Now query the state machine for the current state
	var currentState string
	var lastUpdate = time.Now().Format(time.RFC3339)

	switch e := engine.(type) {
	case interface {
		GetInstance(instanceID string) (*WorkflowInstance, error)
	}:
		instance, err := e.GetInstance(instanceId)
		if err == nil && instance != nil {
			currentState = instance.CurrentState
			h.logger.Debug(fmt.Sprintf("Retrieved current state from engine: %s", currentState))
		} else if err != nil {
			h.logger.Warn(fmt.Sprintf("Failed to get instance state: %s", err.Error()))
		}
	case interface {
		GetWorkflowState(ctx context.Context, workflowType string, instanceID string) (map[string]any, error)
	}:
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
		var stateTracker any
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
		if existingResource, exists := h.resources[resourceId]; exists {
			existingResource.State = currentState
			existingResource.LastUpdate = lastUpdate
			existingResource.Data["state"] = currentState
			existingResource.Data["lastUpdate"] = lastUpdate
			existingResource.Data["workflowType"] = workflowType
			existingResource.Data["instanceId"] = instanceId
			h.resources[resourceId] = existingResource
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
	if err := json.NewEncoder(w).Encode(result); err != nil {
		_ = err
	}
}

// handleSubAction handles POST requests to sub-resource actions like /assign, /transfer, etc.
// These map to state machine transitions on the parent resource via the configured transitionMap.
func (h *RESTAPIHandler) handleSubAction(resourceId, subAction string, w http.ResponseWriter, r *http.Request) {
	if resourceId == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Resource ID is required"})
		return
	}

	// Limit request body size to prevent denial-of-service
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)

	// Parse request body for additional data
	var body map[string]any
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err.Error() != "EOF" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"})
			return
		}
	}
	if body == nil {
		body = make(map[string]any)
	}

	// Tag is a data-only update, no state transition
	if subAction == "tag" {
		h.handleTagAction(resourceId, body, w)
		return
	}

	// Look up sub-action in the configurable transition map
	transitionName, ok := h.transitionMap[subAction]
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Unknown action: %s", subAction)})
		return
	}

	// Find the state machine engine
	var smEngine *StateMachineEngine
	if h.workflowEngine != "" {
		var engineSvc any
		if err := h.app.GetService(h.workflowEngine, &engineSvc); err == nil {
			smEngine, _ = engineSvc.(*StateMachineEngine)
		}
	}
	if smEngine == nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Workflow engine not available"})
		return
	}

	// Build instance ID
	instanceId := resourceId
	if h.instanceIDPrefix != "" {
		instanceId = h.instanceIDPrefix + resourceId
	}

	// Merge existing resource data into the transition payload
	h.mu.RLock()
	resource, exists := h.resources[resourceId]
	h.mu.RUnlock()
	if !exists {
		// Try syncing from persistence first
		h.syncFromPersistence()
		h.mu.RLock()
		resource, exists = h.resources[resourceId]
		h.mu.RUnlock()
	}
	if !exists {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Resource not found"})
		return
	}

	workflowData := make(map[string]any)
	maps.Copy(workflowData, resource.Data)
	maps.Copy(workflowData, body)

	// Ensure workflow instance exists
	if _, err := smEngine.GetInstance(instanceId); err != nil {
		if _, err := smEngine.CreateWorkflow(h.workflowType, instanceId, workflowData); err != nil {
			h.logger.Error(fmt.Sprintf("Failed to create workflow instance for sub-action: %v", err))
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to create workflow instance"})
			return
		}
	}

	// Trigger the transition
	err := smEngine.TriggerTransition(r.Context(), instanceId, transitionName, workflowData)
	if err != nil {
		h.logger.Error(fmt.Sprintf("Sub-action '%s' (transition '%s') failed for resource '%s': %v",
			subAction, transitionName, resourceId, err))
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success":    false,
			"error":      err.Error(),
			"action":     subAction,
			"transition": transitionName,
		})
		return
	}

	// Read back the updated state
	var currentState string
	if instance, err := smEngine.GetInstance(instanceId); err == nil && instance != nil {
		currentState = instance.CurrentState
	}

	// Update the in-memory resource
	lastUpdate := time.Now().Format(time.RFC3339)
	h.mu.Lock()
	if res, ok := h.resources[resourceId]; ok {
		if currentState != "" {
			res.State = currentState
			res.Data["state"] = currentState
		}
		res.LastUpdate = lastUpdate
		res.Data["lastUpdate"] = lastUpdate
		maps.Copy(res.Data, body)
		h.resources[resourceId] = res

		if h.persistence != nil {
			_ = h.persistence.SaveResource(h.resourceName, res.ID, res.Data)
		}
	}
	h.mu.Unlock()

	h.logger.Info(fmt.Sprintf("Sub-action '%s' completed for resource '%s' → state '%s'",
		subAction, resourceId, currentState))

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success":    true,
		"action":     subAction,
		"transition": transitionName,
		"id":         resourceId,
		"state":      currentState,
		"lastUpdate": lastUpdate,
	})

	// Sync resource state after auto-transitions complete (runs async).
	go h.syncResourceStateFromEngine(instanceId, resourceId, smEngine)
}
