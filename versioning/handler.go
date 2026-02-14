package versioning

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// Handler provides HTTP endpoints for workflow versioning.
type Handler struct {
	store   *VersionStore
	applyFn RollbackFunc
}

// NewHandler creates a new versioning HTTP handler.
func NewHandler(store *VersionStore, applyFn RollbackFunc) *Handler {
	return &Handler{store: store, applyFn: applyFn}
}

// RegisterRoutes registers versioning API routes on the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/workflows/{name}/versions", h.listVersions)
	mux.HandleFunc("GET /api/workflows/{name}/versions/{version}", h.getVersion)
	mux.HandleFunc("POST /api/workflows/{name}/versions", h.createVersion)
	mux.HandleFunc("POST /api/workflows/{name}/rollback/{version}", h.rollback)
	mux.HandleFunc("GET /api/workflows/{name}/diff", h.diffVersions)
	mux.HandleFunc("GET /api/workflows", h.listWorkflows)
}

func (h *Handler) listVersions(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	versions := h.store.List(name)
	writeJSON(w, http.StatusOK, map[string]any{"items": versions, "total": len(versions)})
}

func (h *Handler) getVersion(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	vStr := r.PathValue("version")
	version, err := strconv.Atoi(vStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid version number"})
		return
	}

	v, ok := h.store.Get(name, version)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "version not found"})
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (h *Handler) createVersion(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var body struct {
		ConfigYAML  string `json:"configYaml"`
		Description string `json:"description"`
		CreatedBy   string `json:"createdBy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	v, err := h.store.Save(name, body.ConfigYAML, body.Description, body.CreatedBy)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, v)
}

func (h *Handler) rollback(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	vStr := r.PathValue("version")
	version, err := strconv.Atoi(vStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid version number"})
		return
	}

	var body struct {
		CreatedBy string `json:"createdBy"`
	}
	// Body is optional
	_ = json.NewDecoder(r.Body).Decode(&body)

	v, err := Rollback(h.store, name, version, body.CreatedBy, h.applyFn)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (h *Handler) diffVersions(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")

	from, err := strconv.Atoi(fromStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid from version"})
		return
	}
	to, err := strconv.Atoi(toStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid to version"})
		return
	}

	diff, err := Compare(h.store, name, from, to)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, diff)
}

func (h *Handler) listWorkflows(w http.ResponseWriter, r *http.Request) {
	names := h.store.ListWorkflows()
	writeJSON(w, http.StatusOK, map[string]any{"workflows": names})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
