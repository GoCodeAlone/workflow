package module

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
)

// maxRequestBodySize is the maximum allowed request body size (1MB).
const maxRequestBodySize = 1 << 20

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

// handleGet handles GET requests for a specific resource.
func (h *RESTAPIHandler) handleGet(resourceId string, w http.ResponseWriter, r *http.Request) {
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
		if err := json.NewEncoder(w).Encode(resource); err != nil {
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		}
		return
	}

	// Not found
	w.WriteHeader(http.StatusNotFound)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": "Resource not found"}); err != nil {
		_ = err
	}
}

// handleGetAll handles GET requests for listing all resources.
func (h *RESTAPIHandler) handleGetAll(w http.ResponseWriter, r *http.Request) {
	h.syncFromPersistence()
	h.mu.RLock()
	defer h.mu.RUnlock()

	// If there's an authenticated user, filter resources to only show theirs
	currentUserID := extractUserID(r)

	resources := make([]RESTResource, 0, len(h.resources))
	for _, resource := range h.resources {
		// If user is authenticated and resource has a userId, only include matching resources
		if currentUserID != "" {
			if resUserID, ok := resource.Data["userId"].(string); ok && resUserID != currentUserID {
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

	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resources); err != nil {
		_ = err
	}
}

// handlePost handles POST requests for creating resources.
func (h *RESTAPIHandler) handlePost(resourceId string, w http.ResponseWriter, r *http.Request) {
	// Limit request body size to prevent denial-of-service
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)

	var data map[string]any
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		if encErr := json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request body"}); encErr != nil {
			_ = encErr
		}
		return
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

	lastUpdate := time.Now().Format(time.RFC3339)

	// Store the ID in data so it's available downstream
	h.fieldMapping.SetValue(data, "id", resourceId)

	// Create the resource
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

	// If a workflow engine is configured, create an instance and trigger the initial transition
	if h.workflowType != "" && h.workflowEngine != "" {
		h.startWorkflowForResource(r.Context(), resourceId, resource)
	}

	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(resource); err != nil {
		_ = err
	}
}

// handlePut handles PUT requests for updating resources.
func (h *RESTAPIHandler) handlePut(resourceId string, w http.ResponseWriter, r *http.Request) {
	if resourceId == "" {
		w.WriteHeader(http.StatusBadRequest)
		if err := json.NewEncoder(w).Encode(map[string]string{"error": "ID is required for PUT"}); err != nil {
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
		_ = err
	}
}

// handleDelete handles DELETE requests for removing resources.
func (h *RESTAPIHandler) handleDelete(resourceId string, w http.ResponseWriter, r *http.Request) {
	if resourceId == "" {
		w.WriteHeader(http.StatusBadRequest)
		if err := json.NewEncoder(w).Encode(map[string]string{"error": "ID is required for DELETE"}); err != nil {
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
}

// handleTagAction handles POST /tag — updates resource tags without a state transition.
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
		// Return the resource data plus any configured summary fields
		summary := map[string]any{
			"id":         resourceId,
			"state":      resource.State,
			"lastUpdate": resource.LastUpdate,
		}
		for _, key := range h.summaryFields {
			if v, ok := resource.Data[key]; ok {
				summary[key] = v
			}
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(summary)

	default:
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("Unknown sub-resource: %s", subAction)})
	}
}

// loadSeedData reads a JSON file containing an array of resources and populates the resources map.
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
