package environment

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// Handler exposes environment CRUD endpoints over HTTP.
type Handler struct {
	store Store
}

// NewHandler creates a new environment HTTP handler.
func NewHandler(store Store) *Handler {
	return &Handler{store: store}
}

// RegisterRoutes registers environment endpoints on the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/admin/environments", h.handleList)
	mux.HandleFunc("POST /api/v1/admin/environments", h.handleCreate)
	mux.HandleFunc("GET /api/v1/admin/environments/{id}", h.handleGet)
	mux.HandleFunc("PUT /api/v1/admin/environments/{id}", h.handleUpdate)
	mux.HandleFunc("DELETE /api/v1/admin/environments/{id}", h.handleDelete)
	mux.HandleFunc("POST /api/v1/admin/environments/{id}/test", h.handleTestConnection)
}

// ---------- GET /api/v1/admin/environments ----------

func (h *Handler) handleList(w http.ResponseWriter, r *http.Request) {
	filter := Filter{
		WorkflowID: r.URL.Query().Get("workflow_id"),
		Provider:   r.URL.Query().Get("provider"),
		Status:     r.URL.Query().Get("status"),
	}

	envs, err := h.store.List(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list environments")
		return
	}
	if envs == nil {
		envs = []Environment{}
	}
	writeJSON(w, http.StatusOK, envs)
}

// ---------- POST /api/v1/admin/environments ----------

func (h *Handler) handleCreate(w http.ResponseWriter, r *http.Request) {
	var env Environment
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if env.Name == "" || env.Provider == "" || env.WorkflowID == "" {
		writeError(w, http.StatusBadRequest, "name, provider, and workflow_id are required")
		return
	}

	if err := h.store.Create(r.Context(), &env); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create environment")
		return
	}
	writeJSON(w, http.StatusCreated, env)
}

// ---------- GET /api/v1/admin/environments/{id} ----------

func (h *Handler) handleGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing environment id")
		return
	}

	env, err := h.store.Get(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "environment not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get environment")
		return
	}
	writeJSON(w, http.StatusOK, env)
}

// ---------- PUT /api/v1/admin/environments/{id} ----------

func (h *Handler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing environment id")
		return
	}

	// Fetch existing environment first to merge partial updates.
	existing, err := h.store.Get(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "environment not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get environment")
		return
	}

	// Decode the request body as a map to detect which fields were sent.
	var patch map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Apply only the fields that were explicitly provided.
	if v, ok := patch["name"]; ok {
		_ = json.Unmarshal(v, &existing.Name)
	}
	if v, ok := patch["provider"]; ok {
		_ = json.Unmarshal(v, &existing.Provider)
	}
	if v, ok := patch["region"]; ok {
		_ = json.Unmarshal(v, &existing.Region)
	}
	if v, ok := patch["workflow_id"]; ok {
		_ = json.Unmarshal(v, &existing.WorkflowID)
	}
	if v, ok := patch["config"]; ok {
		_ = json.Unmarshal(v, &existing.Config)
	}
	if v, ok := patch["secrets"]; ok {
		_ = json.Unmarshal(v, &existing.Secrets)
	}
	if v, ok := patch["status"]; ok {
		_ = json.Unmarshal(v, &existing.Status)
	}

	if err := h.store.Update(r.Context(), existing); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update environment")
		return
	}
	writeJSON(w, http.StatusOK, existing)
}

// ---------- DELETE /api/v1/admin/environments/{id} ----------

func (h *Handler) handleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing environment id")
		return
	}

	if err := h.store.Delete(r.Context(), id); err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "environment not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to delete environment")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ---------- POST /api/v1/admin/environments/{id}/test ----------

func (h *Handler) handleTestConnection(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing environment id")
		return
	}

	// Verify the environment exists
	_, err := h.store.Get(r.Context(), id)
	if err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "environment not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get environment")
		return
	}

	// Placeholder connectivity test — always succeeds
	result := ConnectionTestResult{
		Success: true,
		Message: "connection test passed (placeholder)",
		Latency: 42 * time.Millisecond,
	}
	writeJSON(w, http.StatusOK, result)
}

// ---------- helpers ----------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// isNotFound returns true if the error indicates a missing row.
func isNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no rows")
}
