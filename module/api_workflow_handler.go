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

// resolveConversationRouting sets programId, affiliateId, and programName on the
// conversation data map by matching the message body against known keywords, then
// falling back to shortcode and provider. This mirrors the routing logic in the
// conversation-router dynamic component but runs synchronously at creation time
// so the fields are persisted before the async workflow pipeline starts.
func (h *RESTAPIHandler) resolveConversationRouting(data map[string]any, msgBody string) {
	// Keyword -> programId mapping (mirrors conversation_router.go)
	keywordProgram := map[string]string{
		"HELLO": "prog-001", "HELP": "prog-001", "CRISIS": "prog-001",
		"TEEN": "prog-002", "WELLNESS": "prog-003", "PARTNER": "prog-004",
	}
	// programId -> affiliateId
	programAffiliate := map[string]string{
		"prog-001": "aff-001", "prog-002": "aff-001",
		"prog-003": "aff-002", "prog-004": "aff-003",
	}
	// programId -> display name
	programName := map[string]string{
		"prog-001": "Crisis Text Line", "prog-002": "Teen Support Line",
		"prog-003": "Wellness Chat", "prog-004": "Partner Assist",
	}
	// shortCode -> programId
	shortCodeProgram := map[string]string{
		"741741": "prog-001", "741742": "prog-002",
	}
	// provider -> programId
	providerProgram := map[string]string{
		"twilio": "prog-001", "webchat": "prog-001",
		"aws": "prog-003", "partner": "prog-004",
	}

	var resolvedProgram string

	// 1. Keyword match (highest priority)
	words := strings.Fields(msgBody)
	if len(words) > 0 {
		firstWord := strings.ToUpper(words[0])
		if pid, ok := keywordProgram[firstWord]; ok {
			resolvedProgram = pid
		}
	}

	// 2. Shortcode match
	if resolvedProgram == "" {
		shortCode, _ := data["shortCode"].(string)
		if shortCode == "" {
			shortCode, _ = data["toNumber"].(string)
		}
		if pid, ok := shortCodeProgram[shortCode]; ok {
			resolvedProgram = pid
		}
	}

	// 3. Provider match
	if resolvedProgram == "" {
		provider, _ := data["provider"].(string)
		if pid, ok := providerProgram[strings.ToLower(provider)]; ok {
			resolvedProgram = pid
		}
	}

	// 4. Default fallback
	if resolvedProgram == "" {
		resolvedProgram = "prog-001"
	}

	data["programId"] = resolvedProgram
	data["affiliateId"] = programAffiliate[resolvedProgram]
	if name, ok := programName[resolvedProgram]; ok {
		data["programName"] = name
	}
}

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

	// Build the instance ID.
	// Handlers that bridge to conversations (webhooks, webchat) should set
	// instanceIDPrefix: "conv-" in their config so the conversations-api
	// can find the same state machine instance by the conversation's own ID.
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
		// Normalize field names for components that expect lowercase keys.
		// Twilio webhooks send "Body" but components expect "body".
		transitionData := make(map[string]any)
		maps.Copy(transitionData, resource.Data)
		if _, hasLower := transitionData["body"]; !hasLower {
			if b, ok := transitionData["Body"].(string); ok {
				transitionData["body"] = b
			}
		}
		if err := smEngine.TriggerTransition(bgCtx, instanceId, transitionName, transitionData); err != nil {
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
	// Wait for the pipeline to settle: poll until the state stops changing or we time out.
	// Multi-step pipelines (e.g., new → validating → validated → paying → paid → shipping →
	// shipped → delivered) need time to progress through each step.
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

		// Merge enrichment data from the workflow instance back into the resource.
		// This captures data added by processing steps (e.g., programId from keyword-matcher,
		// affiliateId from conversation-router).
		// Only set keys that don't already exist in the API handler's resource data,
		// since the handler is authoritative for fields it manages directly
		// (e.g., messages, tags, riskLevel).
		for k, v := range instance.Data {
			// Don't overwrite core fields that the resource already manages
			if k == "id" || k == "state" || k == "lastUpdate" {
				continue
			}
			if _, exists := res.Data[k]; !exists {
				res.Data[k] = v
			}
		}

		res.Data["state"] = res.State
		res.Data["lastUpdate"] = res.LastUpdate
		h.resources[resourceId] = res

		// Write-through to persistence
		if h.persistence != nil {
			_ = h.persistence.SaveResource(h.resourceName, res.ID, res.Data)
		}

		// Update the bridged conversation resource's state from the engine.
		// Only update the state field, not the full data (which was already
		// set by bridgeToConversation with routing info, messages, etc.).
		if h.instanceIDPrefix == "conv-" && h.persistence != nil {
			convoId := fmt.Sprintf("conv-%s", resourceId)
			h.updateConversationState(convoId, res.State)
		}
	}
}

// updateConversationState updates just the state field of a bridged conversation resource.
// Uses LoadResources to read the existing data, then updates the state and saves back.
func (h *RESTAPIHandler) updateConversationState(convoId, newState string) {
	if h.persistence == nil {
		return
	}
	loaded, err := h.persistence.LoadResources("conversations")
	if err != nil {
		return
	}
	data, ok := loaded[convoId]
	if !ok {
		return
	}
	data["state"] = newState
	data["lastUpdate"] = time.Now().UTC().Format(time.RFC3339)
	_ = h.persistence.SaveResource("conversations", convoId, data)
}

// bridgeToConversation creates a conversation resource in the "conversations" persistence
// store from webhook/webchat data. This bridges the gap between inbound handlers
// (webhooks-api, webchat-api) and the conversations-api that the SPA reads from.
func (h *RESTAPIHandler) bridgeToConversation(webhookId string, data map[string]any) {
	convoId := fmt.Sprintf("conv-%s", webhookId)

	convoData := map[string]any{
		"id":        convoId,
		"state":     "new",
		"createdAt": time.Now().UTC().Format(time.RFC3339),
	}

	// Copy key fields from the webhook data
	for _, field := range []string{
		"from", "From", "provider", "messages", "riskLevel", "tags",
	} {
		if v, ok := data[field]; ok {
			convoData[field] = v
		}
	}

	// Normalize: ensure "from" is set (Twilio sends "From")
	if _, ok := convoData["from"]; !ok {
		if f, ok := convoData["From"]; ok {
			convoData["from"] = f
		}
	}

	// Resolve routing (programId, affiliateId) from message content
	bodyText := ""
	for _, field := range []string{"body", "Body", "message", "content"} {
		if b, ok := data[field].(string); ok && b != "" {
			bodyText = b
			break
		}
	}
	if bodyText != "" {
		h.resolveConversationRouting(convoData, bodyText)
	}

	convoData["lastUpdate"] = time.Now().UTC().Format(time.RFC3339)

	if err := h.persistence.SaveResource("conversations", convoId, convoData); err != nil {
		h.logger.Warn(fmt.Sprintf("Failed to bridge conversation '%s': %v", convoId, err))
	}
}

// handleTransition handles state transitions for state machine resources
func (h *RESTAPIHandler) handleTransition(resourceId string, w http.ResponseWriter, r *http.Request) {
	if resourceId == "" {
		w.WriteHeader(http.StatusBadRequest)
		if err := json.NewEncoder(w).Encode(map[string]string{"error": "Resource ID is required for transition"}); err != nil {
			// Log error but continue since response is already committed
			_ = err
		}
		return
	}

	// Parse the transition request
	var transitionRequest struct {
		Transition   string         `json:"transition"`
		Data         map[string]any `json:"data,omitempty"`
		WorkflowType string         `json:"workflowType,omitempty"` // Optional workflow type override
	}

	if err := json.NewDecoder(r.Body).Decode(&transitionRequest); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		if encErr := json.NewEncoder(w).Encode(map[string]string{"error": "Invalid transition request format"}); encErr != nil {
			// Log error but continue since response is already committed
			_ = encErr
		}
		return
	}

	if transitionRequest.Transition == "" {
		w.WriteHeader(http.StatusBadRequest)
		if err := json.NewEncoder(w).Encode(map[string]string{"error": "Transition name is required"}); err != nil {
			// Log error but continue since response is already committed
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
			// Log error but continue since response is already committed
			_ = err
		}
		return
	}

	// Add resource data to workflow data
	maps.Copy(workflowData, resource.Data)

	// Add custom transition data if provided
	if transitionRequest.Data != nil {
		maps.Copy(workflowData, transitionRequest.Data)
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
				// Get the engine name for this resource type
				if engineName, found := connector.GetEngineForResourceType(h.resourceName); found {
					// Get the state machine engine by name
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
			// Log error but continue since response is already committed
			_ = err
		}
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
				if encErr := json.NewEncoder(w).Encode(map[string]any{
					"success":    false,
					"error":      fmt.Sprintf("Failed to create workflow instance: %s", err.Error()),
					"id":         resourceId,
					"instanceId": instanceId,
				}); encErr != nil {
					// Log error but continue since response is already committed
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

	// Try different engine types
	switch e := engine.(type) {
	case interface {
		TriggerWorkflow(ctx context.Context, workflowType string, action string, data map[string]any) error
	}:
		// Using the main workflow engine
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
		// Using the state machine engine directly
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
			// Log error but continue since response is already committed
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
			// Log error but continue since response is already committed
			_ = encErr
		}
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
		GetWorkflowState(ctx context.Context, workflowType string, instanceID string) (map[string]any, error)
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
	if err := json.NewEncoder(w).Encode(result); err != nil {
		// Log error but continue since response is already committed
		_ = err
	}
}

// handleSubAction handles POST requests to sub-resource actions like /assign, /transfer, etc.
// These map to state machine transitions on the parent resource.
func (h *RESTAPIHandler) handleSubAction(resourceId, subAction string, w http.ResponseWriter, r *http.Request) {
	if resourceId == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Resource ID is required"})
		return
	}

	// Parse request body for additional data
	var body map[string]any
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	if body == nil {
		body = make(map[string]any)
	}

	// Attach the authenticated user's ID
	if userID := extractUserID(r); userID != "" {
		h.fieldMapping.SetValue(body, "responderId", userID)
	}

	// Tag is a data-only update, no state transition
	if subAction == "tag" {
		h.handleTagAction(resourceId, body, w)
		return
	}

	// Messages sub-action: append to the resource's messages array (no state transition)
	if subAction == "messages" {
		h.mu.Lock()
		resource, exists := h.resources[resourceId]
		if !exists {
			h.mu.Unlock()
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Resource not found"})
			return
		}

		// Build message record
		msg := map[string]any{
			"body":      h.fieldMapping.ResolveString(body, "body"),
			"direction": h.fieldMapping.ResolveString(body, "direction"),
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		}
		if from := h.fieldMapping.ResolveString(body, "from"); from != "" {
			msg["from"] = from
		}
		if userID := h.fieldMapping.ResolveString(body, "userId"); userID != "" {
			msg["sender"] = userID
		} else if respID := h.fieldMapping.ResolveString(body, "responderId"); respID != "" {
			msg["sender"] = respID
		}
		if direction := h.fieldMapping.ResolveString(body, "direction"); direction == "outbound" {
			msg["status"] = "sent"
		} else {
			msg["status"] = "delivered"
		}

		// Append to messages array (initialize if nil)
		msgs := h.fieldMapping.ResolveSlice(resource.Data, "messages")
		if msgs == nil {
			msgs = []any{}
		}
		msgs = append(msgs, msg)
		h.fieldMapping.SetValue(resource.Data, "messages", msgs)

		// Assess risk level from all messages
		riskLevel, riskTags := h.assessRiskLevel(msgs)
		h.fieldMapping.SetValue(resource.Data, "riskLevel", riskLevel)
		if len(riskTags) > 0 {
			existingTags := h.fieldMapping.ResolveSlice(resource.Data, "tags")
			tagSet := make(map[string]bool)
			for _, t := range existingTags {
				if s, ok := t.(string); ok {
					tagSet[s] = true
				}
			}
			for _, t := range riskTags {
				tagSet[t] = true
			}
			allTags := make([]any, 0, len(tagSet))
			for t := range tagSet {
				allTags = append(allTags, t)
			}
			h.fieldMapping.SetValue(resource.Data, "tags", allTags)
		}

		resource.LastUpdate = time.Now().UTC().Format(time.RFC3339)
		h.resources[resourceId] = resource
		h.mu.Unlock()

		// Persist
		if h.persistence != nil {
			h.fieldMapping.SetValue(resource.Data, "state", resource.State)
			h.fieldMapping.SetValue(resource.Data, "lastUpdate", resource.LastUpdate)
			_ = h.persistence.SaveResource(h.resourceName, resourceId, resource.Data)
		}

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"messageId":      fmt.Sprintf("msg-%s-%d", resourceId, len(msgs)),
			"conversationId": resourceId,
			"direction":      h.fieldMapping.ResolveString(body, "direction"),
			"status":         msg["status"],
			"timestamp":      msg["timestamp"],
		})
		return
	}

	// Look up sub-action in the configurable transition map
	transitionName, ok := h.transitionMap[subAction]
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Unknown action: %s", subAction)})
		return
	}

	// Refine transition based on request body or current state
	if subAction == "escalate" {
		if escType, ok := body["type"].(string); ok && escType == "police" {
			transitionName = "escalate_to_police"
		}
	}
	if subAction == "close" {
		h.mu.RLock()
		if res, exists := h.resources[resourceId]; exists {
			switch res.State {
			case "wrap_up":
				transitionName = "close_from_wrap_up"
			case "follow_up_active":
				transitionName = "close_from_followup"
			}
		}
		h.mu.RUnlock()
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
		// Create it if missing
		if _, err := smEngine.CreateWorkflow(h.workflowType, instanceId, workflowData); err != nil {
			h.logger.Error(fmt.Sprintf("Failed to create workflow instance for sub-action: %v", err))
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Failed to create workflow instance"})
			return
		}
	}

	// Trigger the transition, with fallback for state-dependent actions
	err := smEngine.TriggerTransition(r.Context(), instanceId, transitionName, workflowData)
	if err != nil {
		// For "assign" action, try fallback transitions for different source states
		if subAction == "assign" && strings.Contains(err.Error(), "cannot trigger transition") {
			fallbacks := []string{"assign_from_new", "assign_responder"}
			for _, fb := range fallbacks {
				if fb == transitionName {
					continue // already tried
				}
				if fbErr := smEngine.TriggerTransition(r.Context(), instanceId, fb, workflowData); fbErr == nil {
					transitionName = fb
					err = nil
					break
				}
			}
		}
	}
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
		// Merge body data into the resource
		maps.Copy(res.Data, body)
		h.resources[resourceId] = res

		if h.persistence != nil {
			_ = h.persistence.SaveResource(h.resourceName, res.ID, res.Data)
		}
	}
	h.mu.Unlock()

	// Publish event
	h.publishEvent(map[string]any{
		"eventType":  h.resourceName + "." + subAction,
		"resourceId": resourceId,
		"action":     subAction,
		"state":      currentState,
	})

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
	// This ensures that auto-transitions (e.g., assigned→active) update
	// the resource state in persistence after the HTTP response is sent.
	go h.syncResourceStateFromEngine(instanceId, resourceId, smEngine)
}
