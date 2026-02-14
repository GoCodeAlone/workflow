package promotion

import (
	"encoding/json"
	"net/http"
)

// Handler provides HTTP endpoints for multi-environment promotion.
type Handler struct {
	pipeline *Pipeline
}

// NewHandler creates a new promotion HTTP handler.
func NewHandler(pipeline *Pipeline) *Handler {
	return &Handler{pipeline: pipeline}
}

// RegisterRoutes registers promotion API routes on the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/environments", h.listEnvironments)
	mux.HandleFunc("POST /api/promote", h.promote)
	mux.HandleFunc("POST /api/promotions/{id}/approve", h.approve)
	mux.HandleFunc("POST /api/promotions/{id}/reject", h.reject)
	mux.HandleFunc("GET /api/promotions", h.listPromotions)
	mux.HandleFunc("GET /api/promotions/{id}", h.getPromotion)
	mux.HandleFunc("POST /api/deploy", h.deploy)
	mux.HandleFunc("GET /api/configs/{workflow}", h.getConfigs)
}

func (h *Handler) listEnvironments(w http.ResponseWriter, r *http.Request) {
	envs := h.pipeline.ListEnvironments()
	writeJSON(w, http.StatusOK, map[string]any{"environments": envs})
}

func (h *Handler) promote(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WorkflowName string          `json:"workflowName"`
		FromEnv      EnvironmentName `json:"fromEnv"`
		ToEnv        EnvironmentName `json:"toEnv"`
		RequestedBy  string          `json:"requestedBy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	record, err := h.pipeline.Promote(r.Context(), body.WorkflowName, body.FromEnv, body.ToEnv, body.RequestedBy)
	if err != nil {
		status := http.StatusBadRequest
		if record != nil && record.Status == PromotionFailed {
			status = http.StatusUnprocessableEntity
		}
		writeJSON(w, status, map[string]any{"error": err.Error(), "promotion": record})
		return
	}

	code := http.StatusOK
	if record.Status == PromotionPending {
		code = http.StatusAccepted
	}
	writeJSON(w, code, record)
}

func (h *Handler) approve(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		ApprovedBy string `json:"approvedBy"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	record, err := h.pipeline.Approve(r.Context(), id, body.ApprovedBy)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (h *Handler) reject(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		RejectedBy string `json:"rejectedBy"`
		Reason     string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	record, err := h.pipeline.Reject(id, body.RejectedBy, body.Reason)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (h *Handler) listPromotions(w http.ResponseWriter, r *http.Request) {
	records := h.pipeline.ListPromotions()
	writeJSON(w, http.StatusOK, map[string]any{"items": records, "total": len(records)})
}

func (h *Handler) getPromotion(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	record, ok := h.pipeline.GetPromotion(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (h *Handler) deploy(w http.ResponseWriter, r *http.Request) {
	var body struct {
		WorkflowName string          `json:"workflowName"`
		Environment  EnvironmentName `json:"environment"`
		ConfigYAML   string          `json:"configYaml"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if err := h.pipeline.Deploy(r.Context(), body.WorkflowName, body.Environment, body.ConfigYAML); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deployed"})
}

func (h *Handler) getConfigs(w http.ResponseWriter, r *http.Request) {
	workflow := r.PathValue("workflow")
	configs := h.pipeline.GetAllConfigs(workflow)
	writeJSON(w, http.StatusOK, map[string]any{"workflow": workflow, "configs": configs})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
