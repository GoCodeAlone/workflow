package module

import (
	"encoding/json"
	"net/http"
	"strings"
)

// RuntimeHandler exposes HTTP endpoints for managing runtime workflow instances.
type RuntimeHandler struct {
	manager *RuntimeManager
}

// NewRuntimeHandler creates a new handler backed by a RuntimeManager.
func NewRuntimeHandler(manager *RuntimeManager) *RuntimeHandler {
	return &RuntimeHandler{manager: manager}
}

// RegisterRoutes registers runtime management routes on the given mux.
func (h *RuntimeHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/admin/runtime/instances", h.handleList)
	mux.HandleFunc("POST /api/v1/admin/runtime/instances/{id}/stop", h.handleStop)
}

// ServeHTTP implements http.Handler for delegate dispatch.
// The delegate step passes the full original path, so we match against it.
func (h *RuntimeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	path := r.URL.Path

	switch {
	case r.Method == http.MethodGet && (path == "/" || path == "" ||
		strings.HasSuffix(strings.TrimSuffix(path, "/"), "/instances") ||
		strings.HasSuffix(strings.TrimSuffix(path, "/"), "/runtime/instances")):
		h.listInstances(w)
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/stop"):
		id := extractID(path, "/stop")
		h.stopInstance(w, id)
	default:
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	}
}

func (h *RuntimeHandler) handleList(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	h.listInstances(w)
}

func (h *RuntimeHandler) handleStop(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	id := r.PathValue("id")
	h.stopInstance(w, id)
}

func (h *RuntimeHandler) listInstances(w http.ResponseWriter) {
	instances := h.manager.ListInstances()
	resp := map[string]any{
		"instances": instances,
		"total":     len(instances),
	}
	json.NewEncoder(w).Encode(resp)
}

func (h *RuntimeHandler) stopInstance(w http.ResponseWriter, id string) {
	if id == "" {
		http.Error(w, `{"error":"missing instance id"}`, http.StatusBadRequest)
		return
	}
	if err := h.manager.StopWorkflow(nil, id); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"status": "stopped"})
}

// extractID pulls the segment before the given suffix from a URL path.
// e.g., "/some-id/stop" with suffix "/stop" returns "some-id".
func extractID(path, suffix string) string {
	path = strings.TrimSuffix(path, suffix)
	path = strings.TrimPrefix(path, "/")
	parts := strings.Split(path, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return ""
}
