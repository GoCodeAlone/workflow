package ai

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Handler provides HTTP handlers for the AI service API.
type Handler struct {
	service *Service
}

// NewHandler creates a new AI API handler.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// RegisterRoutes registers the AI API routes on a ServeMux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/ai/generate", h.HandleGenerate)
	mux.HandleFunc("POST /api/ai/component", h.HandleComponent)
	mux.HandleFunc("POST /api/ai/suggest", h.HandleSuggest)
	mux.HandleFunc("GET /api/ai/providers", h.HandleProviders)
}

// ServeHTTP implements http.Handler for config-driven delegate dispatch.
// It handles both query (GET) and command (POST) operations for AI,
// dispatching based on the last path segment.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	seg := lastSeg(r.URL.Path)

	switch r.Method {
	case http.MethodGet:
		switch seg {
		case "providers":
			h.HandleProviders(w, r)
		default:
			writeError(w, http.StatusNotFound, "not found")
		}
	case http.MethodPost:
		switch seg {
		case "generate":
			h.HandleGenerate(w, r)
		case "component":
			h.HandleComponent(w, r)
		case "suggest":
			h.HandleSuggest(w, r)
		default:
			writeError(w, http.StatusNotFound, "not found")
		}
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

// lastSeg extracts the last non-empty segment from a URL path.
func lastSeg(path string) string {
	path = strings.TrimRight(path, "/")
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		return path[idx+1:]
	}
	return path
}

// HandleGenerate handles POST /api/ai/generate
func (h *Handler) HandleGenerate(w http.ResponseWriter, r *http.Request) {
	var req GenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.Intent == "" {
		writeError(w, http.StatusBadRequest, "intent is required")
		return
	}

	resp, err := h.service.GenerateWorkflow(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "generation failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// HandleComponent handles POST /api/ai/component
func (h *Handler) HandleComponent(w http.ResponseWriter, r *http.Request) {
	var spec ComponentSpec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if spec.Name == "" || spec.Interface == "" {
		writeError(w, http.StatusBadRequest, "name and interface are required")
		return
	}

	code, err := h.service.GenerateComponent(r.Context(), spec)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "component generation failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"code": code})
}

// HandleSuggest handles POST /api/ai/suggest
func (h *Handler) HandleSuggest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UseCase string `json:"useCase"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if req.UseCase == "" {
		writeError(w, http.StatusBadRequest, "useCase is required")
		return
	}

	suggestions, err := h.service.SuggestWorkflow(r.Context(), req.UseCase)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "suggestion failed: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, suggestions)
}

// HandleProviders handles GET /api/ai/providers
func (h *Handler) HandleProviders(w http.ResponseWriter, r *http.Request) {
	providers := h.service.Providers()
	writeJSON(w, http.StatusOK, map[string]any{
		"providers": providers,
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
