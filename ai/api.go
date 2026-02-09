package ai

import (
	"encoding/json"
	"net/http"
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
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"providers": providers,
	})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
