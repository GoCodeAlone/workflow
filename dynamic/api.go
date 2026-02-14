package dynamic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// APIHandler exposes HTTP endpoints for managing dynamic components.
type APIHandler struct {
	loader   *Loader
	registry *ComponentRegistry
}

// NewAPIHandler creates a new API handler.
func NewAPIHandler(loader *Loader, registry *ComponentRegistry) *APIHandler {
	return &APIHandler{
		loader:   loader,
		registry: registry,
	}
}

// loadComponentRequest is the JSON body for loading/updating a component.
type loadComponentRequest struct {
	Source string `json:"source"`
}

// RegisterRoutes registers the dynamic component API routes on the given mux.
func (h *APIHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/dynamic/components", h.handleComponents)
	mux.HandleFunc("/api/dynamic/components/", h.handleComponentByID)
}

func (h *APIHandler) handleComponents(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listComponents(w, r)
	case http.MethodPost:
		h.createComponent(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *APIHandler) handleComponentByID(w http.ResponseWriter, r *http.Request) {
	// Extract ID from path: /api/dynamic/components/{id}
	id := strings.TrimPrefix(r.URL.Path, "/api/dynamic/components/")
	if id == "" {
		http.Error(w, "component id required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.getComponent(w, id)
	case http.MethodPut:
		h.updateComponent(w, r, id)
	case http.MethodDelete:
		h.deleteComponent(w, id)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *APIHandler) listComponents(w http.ResponseWriter, _ *http.Request) {
	infos := h.registry.List()
	writeJSON(w, http.StatusOK, infos)
}

func (h *APIHandler) createComponent(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	defer func() { _ = r.Body.Close() }()

	// Expect JSON with "id" and "source" fields
	var req struct {
		ID     string `json:"id"`
		Source string `json:"source"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.ID == "" || req.Source == "" {
		http.Error(w, "id and source are required", http.StatusBadRequest)
		return
	}

	comp, err := h.loader.LoadFromString(req.ID, req.Source)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	writeJSON(w, http.StatusCreated, comp.Info())
}

func (h *APIHandler) getComponent(w http.ResponseWriter, id string) {
	comp, ok := h.registry.Get(id)
	if !ok {
		http.Error(w, "component not found", http.StatusNotFound)
		return
	}

	// Return info plus source
	type response struct {
		ComponentInfo
		Source string `json:"source"`
	}
	resp := response{
		ComponentInfo: comp.Info(),
		Source:        comp.Source(),
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *APIHandler) updateComponent(w http.ResponseWriter, r *http.Request, id string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	defer func() { _ = r.Body.Close() }()

	var req loadComponentRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Source == "" {
		http.Error(w, "source is required", http.StatusBadRequest)
		return
	}

	comp, err := h.loader.Reload(id, req.Source)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	writeJSON(w, http.StatusOK, comp.Info())
}

func (h *APIHandler) deleteComponent(w http.ResponseWriter, id string) {
	comp, ok := h.registry.Get(id)
	if !ok {
		http.Error(w, "component not found", http.StatusNotFound)
		return
	}

	// Stop if running
	info := comp.Info()
	if info.Status == StatusRunning {
		_ = comp.Stop(context.Background())
	}

	if err := h.registry.Unregister(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
