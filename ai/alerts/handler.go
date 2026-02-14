package alerts

import (
	"encoding/json"
	"net/http"
	"time"
)

// Handler provides HTTP handlers for the alerts API.
type Handler struct {
	engine *AlertEngine
}

// NewHandler creates a new alerts API handler.
func NewHandler(engine *AlertEngine) *Handler {
	return &Handler{engine: engine}
}

// RegisterRoutes registers the alerts API routes on a ServeMux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/alerts", h.HandleGetAlerts)
	mux.HandleFunc("POST /api/alerts/{id}/acknowledge", h.HandleAcknowledge)
	mux.HandleFunc("POST /api/alerts/{id}/resolve", h.HandleResolve)
}

// HandleGetAlerts handles GET /api/alerts.
// Supports query parameters: type, severity, conversationId, unresolved, since, limit.
func (h *Handler) HandleGetAlerts(w http.ResponseWriter, r *http.Request) {
	filter := AlertFilter{}

	if t := r.URL.Query().Get("type"); t != "" {
		at := AlertType(t)
		filter.Type = &at
	}
	if s := r.URL.Query().Get("severity"); s != "" {
		sev := Severity(s)
		filter.Severity = &sev
	}
	if cid := r.URL.Query().Get("conversationId"); cid != "" {
		filter.ConversationID = cid
	}
	if r.URL.Query().Get("unresolved") == "true" {
		filter.Unresolved = true
	}
	if since := r.URL.Query().Get("since"); since != "" {
		if t, err := time.Parse(time.RFC3339, since); err == nil {
			filter.Since = &t
		}
	}

	alerts := h.engine.GetAlerts(filter)
	if alerts == nil {
		alerts = []Alert{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"alerts": alerts,
		"count":  len(alerts),
	})
}

// HandleAcknowledge handles POST /api/alerts/{id}/acknowledge.
func (h *Handler) HandleAcknowledge(w http.ResponseWriter, r *http.Request) {
	alertID := r.PathValue("id")
	if alertID == "" {
		writeError(w, http.StatusBadRequest, "alert id is required")
		return
	}

	if err := h.engine.AcknowledgeAlert(alertID); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "acknowledged"})
}

// HandleResolve handles POST /api/alerts/{id}/resolve.
func (h *Handler) HandleResolve(w http.ResponseWriter, r *http.Request) {
	alertID := r.PathValue("id")
	if alertID == "" {
		writeError(w, http.StatusBadRequest, "alert id is required")
		return
	}

	if err := h.engine.ResolveAlert(alertID); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "resolved"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
