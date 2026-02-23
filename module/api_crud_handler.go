package module

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

// maxRequestBodySize is the maximum allowed request body size (1MB).
const maxRequestBodySize = 1 << 20

// firstNonEmpty returns the first non-empty string value found in data for the given keys.
func firstNonEmpty(data map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := data[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// extractUserID extracts the authenticated user's ID from the request context.
// Returns empty string if no auth claims are present.
func extractUserID(r *http.Request) string {
	claims, ok := r.Context().Value(authClaimsContextKey).(map[string]any)
	if !ok {
		return ""
	}
	if sub, ok := claims["sub"].(string); ok {
		return sub
	}
	return ""
}

// extractAuthClaims extracts role, affiliateId, and programIds from the JWT claims
// in the request context. Returns empty values if no auth claims are present.
func extractAuthClaims(r *http.Request) (role, affiliateId string, programIds []string) {
	claims, ok := r.Context().Value(authClaimsContextKey).(map[string]any)
	if !ok {
		return "", "", nil
	}
	role, _ = claims["role"].(string)
	affiliateId, _ = claims["affiliateId"].(string)
	if pids, ok := claims["programIds"].([]any); ok {
		for _, pid := range pids {
			if s, ok := pid.(string); ok {
				programIds = append(programIds, s)
			}
		}
	}
	return role, affiliateId, programIds
}

// syncFromPersistence merges any resources from the persistence store that are
// not yet in the in-memory map. This allows resources created by other handler
// instances sharing the same persistence resourceName to be visible.
func (h *RESTAPIHandler) syncFromPersistence() {
	if h.persistence == nil {
		return
	}
	loadFrom := h.resourceName
	if h.sourceResourceName != "" {
		loadFrom = h.sourceResourceName
	}
	loaded, err := h.persistence.LoadResources(loadFrom)
	if err != nil || len(loaded) == 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	// For view handlers (sourceResourceName set), always refresh from persistence
	// to reflect state changes made by other handlers.
	isView := h.sourceResourceName != ""

	for id, data := range loaded {
		if !isView {
			if _, exists := h.resources[id]; exists {
				continue // don't overwrite in-memory state for primary handlers
			}
		}
		state := h.fieldMapping.ResolveString(data, "state")
		lastUpdate := h.fieldMapping.ResolveString(data, "lastUpdate")
		h.resources[id] = RESTResource{
			ID:         id,
			Data:       data,
			State:      state,
			LastUpdate: lastUpdate,
		}
	}

	// For view handlers, remove resources that no longer exist in persistence
	if isView {
		for id := range h.resources {
			if _, exists := loaded[id]; !exists {
				delete(h.resources, id)
			}
		}
	}
}

// handleGet handles GET requests for listing or retrieving resources
func (h *RESTAPIHandler) handleGet(resourceId string, w http.ResponseWriter, r *http.Request) {
	// Handle virtual "health" endpoint for view handlers (e.g., /api/queue/health)
	if resourceId == "health" && h.sourceResourceName != "" {
		h.handleQueueHealth(w, r)
		return
	}

	h.syncFromPersistence()
	h.mu.RLock()
	defer h.mu.RUnlock()

	if resourceId == "" {
		// List all resources
		resources := make([]RESTResource, 0, len(h.resources))
		for _, resource := range h.resources {
			resources = append(resources, resource)
		}
		if err := json.NewEncoder(w).Encode(resources); err != nil {
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		}
		return
	}

	// Get a specific resource
	if resource, ok := h.resources[resourceId]; ok {
		// Try to get the latest state and enrichment data from the workflow engine
		if h.workflowEngine != "" {
			instanceId := resourceId
			if h.instanceIDPrefix != "" {
				instanceId = h.instanceIDPrefix + resourceId
			}
			var engineSvc any
			if err := h.app.GetService(h.workflowEngine, &engineSvc); err == nil {
				if smEngine, ok := engineSvc.(*StateMachineEngine); ok {
					if instance, err := smEngine.GetInstance(instanceId); err == nil && instance != nil {
						resource.State = instance.CurrentState
						resource.LastUpdate = instance.LastUpdated.Format(time.RFC3339)
						// Merge enrichment data from processing pipeline.
						// Only set keys that don't already exist in the API handler's
						// resource data, since the handler is authoritative for fields
						// it manages directly (e.g., messages, tags, riskLevel).
						for k, v := range instance.Data {
							if k == "id" || k == "state" || k == "lastUpdate" {
								continue
							}
							if _, exists := resource.Data[k]; !exists {
								resource.Data[k] = v
							}
						}
					}
				}
			}
		}

		// Also check state tracker for additional data enrichment
		var stateTracker any
		_ = h.app.GetService(StateTrackerName, &stateTracker)
		if stateTracker != nil {
			if tracker, ok := stateTracker.(*StateTracker); ok {
				if stateInfo, exists := tracker.GetState(h.resourceName, resourceId); exists {
					// Use state tracker state if we didn't get one from the engine
					if resource.State == "" || resource.State == "new" {
						resource.State = stateInfo.CurrentState
						resource.LastUpdate = stateInfo.LastUpdate.Format(time.RFC3339)
					}
					// Merge data from the state tracker (only new keys)
					if stateInfo.Data != nil {
						for k, v := range stateInfo.Data {
							if _, exists := resource.Data[k]; !exists {
								resource.Data[k] = v
							}
						}
					}
				}
			}
		}

		if err := json.NewEncoder(w).Encode(resource); err != nil {
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		}
		return
	}

	// Not found
	w.WriteHeader(http.StatusNotFound)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": "Resource not found"}); err != nil {
		// Log error but continue since response is already committed
		_ = err
	}
}

// handleGetAll handles GET requests for listing all resources
func (h *RESTAPIHandler) handleGetAll(w http.ResponseWriter, r *http.Request) {
	h.syncFromPersistence()
	h.mu.RLock()
	defer h.mu.RUnlock()

	// If there's an authenticated user, filter resources to only show theirs
	currentUserID := extractUserID(r)

	// Extract affiliate/program filtering from query params and JWT claims
	role, jwtAffiliateId, jwtProgramIds := extractAuthClaims(r)
	queryAffiliateId := r.URL.Query().Get("affiliateId")
	queryProgramId := r.URL.Query().Get("programId")
	queryRole := r.URL.Query().Get("role")

	// Determine effective filter values: query params take precedence, then JWT claims
	filterAffiliateId := queryAffiliateId
	if filterAffiliateId == "" && role != "admin" {
		filterAffiliateId = jwtAffiliateId
	}
	var filterProgramIds []string
	if queryProgramId != "" {
		filterProgramIds = strings.Split(queryProgramId, ",")
	} else if role != "admin" && len(jwtProgramIds) > 0 {
		filterProgramIds = jwtProgramIds
	}

	// Role-based query param filter (e.g., ?role=responder filters users by role)
	filterRole := queryRole

	// Admin role bypasses affiliate/program filtering
	isAdmin := role == "admin"

	// Optionally get the state machine engine for live state lookup
	var smEngine *StateMachineEngine
	if h.workflowEngine != "" {
		var engineSvc any
		if err := h.app.GetService(h.workflowEngine, &engineSvc); err == nil {
			smEngine, _ = engineSvc.(*StateMachineEngine)
		}
	}

	resources := make([]RESTResource, 0, len(h.resources))
	for _, resource := range h.resources {
		// If user is authenticated and resource has a userId, only include matching resources
		if currentUserID != "" {
			if resUserID, ok := resource.Data["userId"].(string); ok && resUserID != currentUserID {
				continue
			}
		}

		// Enrich with live state and data from the workflow engine BEFORE filtering,
		// so that fields added by processing steps (programId, affiliateId) are available.
		if smEngine != nil {
			instanceId := resource.ID
			if h.instanceIDPrefix != "" {
				instanceId = h.instanceIDPrefix + resource.ID
			}
			if instance, err := smEngine.GetInstance(instanceId); err == nil && instance != nil {
				resource.State = instance.CurrentState
				resource.LastUpdate = instance.LastUpdated.Format(time.RFC3339)
				// Only set keys that don't already exist in the API handler's
				// resource data, since the handler is authoritative for fields
				// it manages directly (e.g., messages, tags, riskLevel).
				for k, v := range instance.Data {
					if k == "id" || k == "state" || k == "lastUpdate" {
						continue
					}
					if _, exists := resource.Data[k]; !exists {
						resource.Data[k] = v
					}
				}
			}
		}

		// Apply affiliate filter (skip for admin).
		// Only filter resources that have an affiliateId field (e.g., conversations).
		// Resources without affiliateId (e.g., keywords, affiliates, surveys) are not filtered.
		if !isAdmin && filterAffiliateId != "" {
			resAffiliateId, _ := resource.Data["affiliateId"].(string)
			if resAffiliateId != "" && resAffiliateId != filterAffiliateId {
				continue
			}
		}

		// Apply program filter (skip for admin).
		// Only filter resources that have a programId field (e.g., conversations).
		// Resources without programId (e.g., users) are not filtered by program.
		if !isAdmin && len(filterProgramIds) > 0 {
			resProgramId, _ := resource.Data["programId"].(string)
			if resProgramId != "" {
				found := slices.Contains(filterProgramIds, resProgramId)
				if !found {
					continue
				}
			}
		}

		// Apply role query param filter (for user resources)
		if filterRole != "" {
			resRole, _ := resource.Data["role"].(string)
			if resRole != "" && resRole != filterRole {
				continue
			}
		}

		// Apply state filter if configured (e.g., queue handler only shows "queued" resources)
		if h.stateFilter != "" {
			resState := resource.State
			if resState == "" {
				resState, _ = resource.Data["state"].(string)
			}
			if resState != h.stateFilter {
				continue
			}
		}

		resources = append(resources, resource)
	}

	// For view handlers (sourceResourceName set), return summary with count
	if h.sourceResourceName != "" {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"totalQueued":   len(resources),
			"count":         len(resources),
			"conversations": resources,
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resources); err != nil {
		// Log error but continue since response is already committed
		_ = err
	}
}

// handlePost handles POST requests for creating resources
func (h *RESTAPIHandler) handlePost(resourceId string, w http.ResponseWriter, r *http.Request) {
	// Limit request body size to prevent denial-of-service
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)

	var data map[string]any
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		if encErr := json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"}); encErr != nil {
			// Log error but continue since response is already committed
			_ = encErr
		}
		return
	}

	// Attach the authenticated user's ID to the resource data
	if userID := extractUserID(r); userID != "" {
		h.fieldMapping.SetValue(data, "userId", userID)
	}

	// Check for follow-up messages: if the POST body contains a sessionId or
	// conversationId referencing an existing resource, append the message to
	// that resource instead of creating a new one. This supports webchat
	// follow-up messages where the client sends to the same creation endpoint.
	if followUpID := firstNonEmpty(data, "sessionId", "conversationId"); followUpID != "" {
		h.mu.Lock()
		resource, exists := h.resources[followUpID]
		if !exists && h.instanceIDPrefix != "" {
			prefixed := h.instanceIDPrefix + followUpID
			if r2, ok := h.resources[prefixed]; ok {
				followUpID = prefixed
				resource = r2
				exists = true
			}
		}
		if exists {
			// Extract message body from various field names
			msgBody := ""
			for _, field := range []string{"message", "body", "content", "text"} {
				if b, ok := data[field].(string); ok && b != "" {
					msgBody = b
					break
				}
			}
			if msgBody != "" {
				msg := map[string]any{
					"body":      msgBody,
					"direction": "inbound",
					"sender":    "texter",
					"status":    "delivered",
					"timestamp": time.Now().UTC().Format(time.RFC3339),
				}
				msgs := h.fieldMapping.ResolveSlice(resource.Data, "messages")
				if msgs == nil {
					msgs = []any{}
				}
				msgs = append(msgs, msg)
				h.fieldMapping.SetValue(resource.Data, "messages", msgs)
				h.resources[followUpID] = resource
				h.mu.Unlock()

				if h.persistence != nil {
					_ = h.persistence.SaveResource(h.resourceName, followUpID, resource.Data)

					// Bridge follow-up messages to the conversation resource
					// so the SPA (which reads from "conversations") sees them too.
					if h.instanceIDPrefix == "conv-" {
						convoId := "conv-" + strings.TrimPrefix(followUpID, "conv-")
						convoData, loadErr := h.persistence.LoadResource("conversations", convoId)
						if loadErr == nil && convoData != nil {
							convoMsgs := h.fieldMapping.ResolveSlice(convoData, "messages")
							if convoMsgs == nil {
								convoMsgs = []any{}
							}
							convoMsgs = append(convoMsgs, msg)
							h.fieldMapping.SetValue(convoData, "messages", convoMsgs)
							_ = h.persistence.SaveResource("conversations", convoId, convoData)
						}
					}
				}

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"conversationId": followUpID,
					"messageId":      fmt.Sprintf("msg-%s-%d", followUpID, len(msgs)),
					"status":         "delivered",
				})
				return
			}
			h.mu.Unlock()
		} else {
			h.mu.Unlock()
		}
	}

	h.mu.Lock()

	// If ID is provided in the URL, use it; otherwise use the ID from the body
	if resourceId == "" {
		if idFromBody := h.fieldMapping.ResolveString(data, "id"); idFromBody != "" {
			resourceId = idFromBody
		} else {
			resourceId = uuid.New().String()
		}
	}

	// Extract state if present, default to "new" for state machine resources
	state := "new"
	if stateVal := h.fieldMapping.ResolveString(data, "state"); stateVal != "" {
		state = stateVal
	}

	// Set the current time for last update
	lastUpdate := time.Now().Format(time.RFC3339)

	// Store the ID in data so it's available downstream
	h.fieldMapping.SetValue(data, "id", resourceId)

	// Assess risk level from initial message content if present
	if bodyText := h.fieldMapping.ResolveString(data, "body"); bodyText != "" {
		initialMsgs := []any{map[string]any{"body": bodyText}}
		riskLevel, riskTags := h.assessRiskLevel(initialMsgs)
		h.fieldMapping.SetValue(data, "riskLevel", riskLevel)
		if len(riskTags) > 0 {
			tagIfaces := make([]any, len(riskTags))
			for i, t := range riskTags {
				tagIfaces[i] = t
			}
			h.fieldMapping.SetValue(data, "tags", tagIfaces)
		}
	}

	// Initialize messages array with the initial inbound message if present.
	// This ensures the chat view shows the texter's first message immediately.
	if _, hasMessages := data["messages"]; !hasMessages {
		// Extract message body from various field names (webhooks use different casing)
		bodyText := ""
		for _, field := range []string{"body", "Body", "message", "content"} {
			if b, ok := data[field].(string); ok && b != "" {
				bodyText = b
				break
			}
		}
		if bodyText != "" {
			from := ""
			for _, field := range []string{"from", "From"} {
				if f, ok := data[field].(string); ok && f != "" {
					from = f
					break
				}
			}
			data["messages"] = []any{
				map[string]any{
					"body":      bodyText,
					"direction": "inbound",
					"from":      from,
					"sender":    "texter",
					"status":    "delivered",
					"timestamp": time.Now().UTC().Format(time.RFC3339),
				},
			}
		} else {
			data["messages"] = []any{}
		}
	}

	// If this is a conversation resource and has a message body but no programId,
	// try to resolve routing from the message content using dynamic components.
	if h.resourceName == "conversations" {
		if _, hasProgramId := data["programId"]; !hasProgramId {
			// Extract message body from various field names
			msgBody := ""
			for _, field := range []string{"body", "Body", "message", "content"} {
				if b, ok := data[field].(string); ok && b != "" {
					msgBody = b
					break
				}
			}
			if msgBody != "" {
				h.resolveConversationRouting(data, msgBody)
			}
		}
	}

	// Create or update the resource
	resource := RESTResource{
		ID:         resourceId,
		Data:       data,
		State:      state,
		LastUpdate: lastUpdate,
	}
	h.resources[resourceId] = resource

	h.mu.Unlock()

	// Write-through to persistence
	if h.persistence != nil {
		h.fieldMapping.SetValue(resource.Data, "state", resource.State)
		h.fieldMapping.SetValue(resource.Data, "lastUpdate", resource.LastUpdate)
		_ = h.persistence.SaveResource(h.resourceName, resource.ID, resource.Data)
	}

	// Bridge: when a webhook/webchat handler creates a resource, also create a
	// corresponding conversation resource so the SPA can list it via /api/conversations.
	// Only bridge if the handler has instanceIDPrefix "conv-" (set in config for
	// webchat-api and webhooks-api), which signals it participates in conversation lifecycle.
	if h.instanceIDPrefix == "conv-" && h.persistence != nil {
		h.bridgeToConversation(resourceId, data)
	}

	// Publish event if broker is available
	h.publishEvent(map[string]any{
		"eventType": h.resourceName + ".created",
		"resource":  resource,
	})

	// If a workflow engine is configured, create an instance and trigger the initial transition
	if h.workflowType != "" && h.workflowEngine != "" {
		h.startWorkflowForResource(r.Context(), resourceId, resource)
	}

	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(resource); err != nil {
		// Log error but continue since response is already committed
		_ = err
	}
}

// handlePut handles PUT requests for updating resources
func (h *RESTAPIHandler) handlePut(resourceId string, w http.ResponseWriter, r *http.Request) {
	if resourceId == "" {
		w.WriteHeader(http.StatusBadRequest)
		if err := json.NewEncoder(w).Encode(map[string]string{"error": "ID is required for PUT"}); err != nil {
			// Log error but continue since response is already committed
			_ = err
		}
		return
	}

	// Limit request body size to prevent denial-of-service
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)

	var data map[string]any
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		if encErr := json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"}); encErr != nil {
			// Log error but continue since response is already committed
			_ = encErr
		}
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// Check if resource exists
	if _, ok := h.resources[resourceId]; !ok {
		w.WriteHeader(http.StatusNotFound)
		if err := json.NewEncoder(w).Encode(map[string]string{"error": "Resource not found"}); err != nil {
			// Log error but continue since response is already committed
			_ = err
		}
		return
	}

	// Update the resource
	h.resources[resourceId] = RESTResource{
		ID:   resourceId,
		Data: data,
	}

	// Write-through to persistence
	if h.persistence != nil {
		_ = h.persistence.SaveResource(h.resourceName, resourceId, data)
	}

	if err := json.NewEncoder(w).Encode(h.resources[resourceId]); err != nil {
		// Log error but continue since response is already committed
		_ = err
	}

	// Publish event
	h.publishEvent(map[string]any{
		"eventType": h.resourceName + ".updated",
		"resource":  h.resources[resourceId],
	})
}

// handleDelete handles DELETE requests for removing resources
func (h *RESTAPIHandler) handleDelete(resourceId string, w http.ResponseWriter, r *http.Request) {
	if resourceId == "" {
		w.WriteHeader(http.StatusBadRequest)
		if err := json.NewEncoder(w).Encode(map[string]string{"error": "ID is required for DELETE"}); err != nil {
			// Log error but continue since response is already committed
			_ = err
		}
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// Check if resource exists
	if _, ok := h.resources[resourceId]; !ok {
		w.WriteHeader(http.StatusNotFound)
		if err := json.NewEncoder(w).Encode(map[string]string{"error": "Resource not found"}); err != nil {
			// Log error but continue since response is already committed
			_ = err
		}
		return
	}

	// Delete the resource
	delete(h.resources, resourceId)

	// Write-through to persistence
	if h.persistence != nil {
		_ = h.persistence.DeleteResource(h.resourceName, resourceId)
	}

	w.WriteHeader(http.StatusNoContent)

	// Publish event
	h.publishEvent(map[string]any{
		"eventType":  h.resourceName + ".deleted",
		"resourceId": resourceId,
	})
}

// handleQueueHealth returns aggregated queue health data grouped by program.
func (h *RESTAPIHandler) handleQueueHealth(w http.ResponseWriter, r *http.Request) {
	h.syncFromPersistence()
	h.mu.RLock()
	defer h.mu.RUnlock()

	// Extract affiliate/program filtering
	role, jwtAffiliateId, jwtProgramIds := extractAuthClaims(r)
	queryAffiliateId := r.URL.Query().Get("affiliateId")
	filterAffiliateId := queryAffiliateId
	if filterAffiliateId == "" && role != "admin" {
		filterAffiliateId = jwtAffiliateId
	}
	var filterProgramIds []string
	if qp := r.URL.Query().Get("programId"); qp != "" {
		filterProgramIds = strings.Split(qp, ",")
	} else if role != "admin" && len(jwtProgramIds) > 0 {
		filterProgramIds = jwtProgramIds
	}
	isAdmin := role == "admin"

	type programStats struct {
		ProgramID      string  `json:"programId"`
		ProgramName    string  `json:"programName"`
		Depth          int     `json:"depth"`
		Queued         int     `json:"queued"`
		AvgWaitSeconds float64 `json:"avgWaitSeconds"`
		OldestMessage  string  `json:"oldestMessageAt,omitempty"`
		AlertThreshold int     `json:"alertThreshold"`
	}

	programs := make(map[string]*programStats)
	now := time.Now()

	for _, res := range h.resources {
		state := res.State
		if state == "" {
			state = h.fieldMapping.ResolveString(res.Data, "state")
		}
		if h.stateFilter != "" && state != h.stateFilter {
			continue
		}

		// Apply affiliate filter
		resAffiliateId := h.fieldMapping.ResolveString(res.Data, "affiliateId")
		if !isAdmin && filterAffiliateId != "" && resAffiliateId != "" && resAffiliateId != filterAffiliateId {
			continue
		}

		pid := h.fieldMapping.ResolveString(res.Data, "programId")

		// Apply program filter
		if !isAdmin && len(filterProgramIds) > 0 && pid != "" {
			found := slices.Contains(filterProgramIds, pid)
			if !found {
				continue
			}
		}
		if pid == "" {
			pid = "default"
		}

		ps, ok := programs[pid]
		if !ok {
			pName := h.fieldMapping.ResolveString(res.Data, "programName")
			if pName == "" {
				pName = pid
			}
			ps = &programStats{
				ProgramID:      pid,
				ProgramName:    pName,
				AlertThreshold: 10,
			}
			programs[pid] = ps
		}
		ps.Depth++
		ps.Queued++

		// Track oldest message for wait time calculation
		if created := h.fieldMapping.ResolveString(res.Data, "createdAt"); created != "" {
			if t, err := time.Parse(time.RFC3339, created); err == nil {
				if ps.OldestMessage == "" || created < ps.OldestMessage {
					ps.OldestMessage = created
				}
				waitSecs := now.Sub(t).Seconds()
				ps.AvgWaitSeconds = (ps.AvgWaitSeconds*float64(ps.Depth-1) + waitSecs) / float64(ps.Depth)
			}
		}
	}

	result := make([]programStats, 0, len(programs))
	for _, ps := range programs {
		result = append(result, *ps)
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"programs": result,
		"alerts":   0,
	})
}

// handleTagAction handles POST /tag — updates resource data without a state transition.
func (h *RESTAPIHandler) handleTagAction(resourceId string, body map[string]any, w http.ResponseWriter) {
	h.mu.Lock()
	defer h.mu.Unlock()

	res, exists := h.resources[resourceId]
	if !exists {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Resource not found"})
		return
	}

	// Merge tag data into the resource
	tags := h.fieldMapping.ResolveSlice(res.Data, "tags")
	if newTag, ok := body["tag"].(string); ok && newTag != "" {
		tags = append(tags, newTag)
		h.fieldMapping.SetValue(res.Data, "tags", tags)
	}
	if newTags, ok := body["tags"].([]any); ok {
		tags = append(tags, newTags...)
		h.fieldMapping.SetValue(res.Data, "tags", tags)
	}
	res.LastUpdate = time.Now().Format(time.RFC3339)
	h.fieldMapping.SetValue(res.Data, "lastUpdate", res.LastUpdate)
	h.resources[resourceId] = res

	if h.persistence != nil {
		_ = h.persistence.SaveResource(h.resourceName, res.ID, res.Data)
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"id":      resourceId,
		"tags":    tags,
	})
}

// handleSubActionGet handles GET requests for sub-resource data (e.g., /summary).
func (h *RESTAPIHandler) handleSubActionGet(resourceId, subAction string, w http.ResponseWriter, r *http.Request) {
	if resourceId == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Resource ID is required"})
		return
	}

	h.syncFromPersistence()
	h.mu.RLock()
	resource, exists := h.resources[resourceId]
	h.mu.RUnlock()

	if !exists {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Resource not found"})
		return
	}

	switch subAction {
	case "summary":
		// Return the resource data plus any summary fields
		summary := map[string]any{
			"id":         resourceId,
			"state":      resource.State,
			"lastUpdate": resource.LastUpdate,
		}
		// Copy relevant summary fields from resource data (configurable via summaryFields)
		for _, key := range h.summaryFields {
			if v, ok := resource.Data[key]; ok {
				summary[key] = v
			}
		}
		// Enrich with live state from workflow engine
		if h.workflowEngine != "" {
			instanceId := resourceId
			if h.instanceIDPrefix != "" {
				instanceId = h.instanceIDPrefix + resourceId
			}
			var engineSvc any
			if err := h.app.GetService(h.workflowEngine, &engineSvc); err == nil {
				if smEngine, ok := engineSvc.(*StateMachineEngine); ok {
					if instance, err := smEngine.GetInstance(instanceId); err == nil && instance != nil {
						summary["state"] = instance.CurrentState
						summary["lastUpdate"] = instance.LastUpdated.Format(time.RFC3339)
					}
				}
			}
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(summary)

	default:
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Unknown sub-resource: %s", subAction)})
	}
}

// loadSeedData reads a JSON file containing an array of resources and populates the resources map
func (h *RESTAPIHandler) loadSeedData(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading seed file: %w", err)
	}

	var seeds []struct {
		ID    string         `json:"id"`
		Data  map[string]any `json:"data"`
		State string         `json:"state"`
	}
	if err := json.Unmarshal(data, &seeds); err != nil {
		return fmt.Errorf("parsing seed file: %w", err)
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	for _, seed := range seeds {
		if seed.ID == "" {
			continue
		}
		resource := RESTResource{
			ID:         seed.ID,
			Data:       seed.Data,
			State:      seed.State,
			LastUpdate: time.Now().Format(time.RFC3339),
		}
		if resource.Data == nil {
			resource.Data = make(map[string]any)
		}
		h.resources[seed.ID] = resource
	}

	return nil
}
