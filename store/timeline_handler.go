package store

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

// TimelineHandler provides HTTP endpoints for the Execution Timeline API.
type TimelineHandler struct {
	store  EventStore
	logger *slog.Logger
}

// NewTimelineHandler creates a new TimelineHandler.
func NewTimelineHandler(store EventStore, logger *slog.Logger) *TimelineHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &TimelineHandler{store: store, logger: logger}
}

// RegisterRoutes registers the timeline API routes on the given mux.
func (h *TimelineHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/admin/executions", h.listExecutions)
	mux.HandleFunc("GET /api/v1/admin/executions/{id}/timeline", h.getTimeline)
	mux.HandleFunc("GET /api/v1/admin/executions/{id}/events", h.getEvents)
}

// listExecutions handles GET /api/v1/admin/executions
func (h *TimelineHandler) listExecutions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	filter := ExecutionEventFilter{
		Pipeline: q.Get("pipeline"),
		TenantID: q.Get("tenant_id"),
		Status:   q.Get("status"),
	}

	if limitStr := q.Get("limit"); limitStr != "" {
		var limit int
		if _, err := json.Number(limitStr).Int64(); err == nil {
			n, _ := json.Number(limitStr).Int64()
			limit = int(n)
		}
		filter.Limit = limit
	}

	if offsetStr := q.Get("offset"); offsetStr != "" {
		if n, err := json.Number(offsetStr).Int64(); err == nil {
			filter.Offset = int(n)
		}
	}

	executions, err := h.store.ListExecutions(r.Context(), filter)
	if err != nil {
		h.logger.Error("Failed to list executions", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	if executions == nil {
		executions = []MaterializedExecution{}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"executions": executions,
		"count":      len(executions),
	})
}

// getTimeline handles GET /api/v1/admin/executions/{id}/timeline
func (h *TimelineHandler) getTimeline(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	if idStr == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing execution ID"})
		return
	}

	id, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid execution ID"})
		return
	}

	timeline, err := h.store.GetTimeline(r.Context(), id)
	if err != nil {
		if err == ErrNotFound {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "execution not found"})
			return
		}
		h.logger.Error("Failed to get timeline", "error", err, "execution_id", idStr)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	writeJSON(w, http.StatusOK, timeline)
}

// getEvents handles GET /api/v1/admin/executions/{id}/events
func (h *TimelineHandler) getEvents(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	if idStr == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing execution ID"})
		return
	}

	id, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid execution ID"})
		return
	}

	events, err := h.store.GetEvents(r.Context(), id)
	if err != nil {
		h.logger.Error("Failed to get events", "error", err, "execution_id", idStr)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	if events == nil {
		events = []ExecutionEvent{}
	}

	// Optional type filter
	if typeFilter := r.URL.Query().Get("type"); typeFilter != "" {
		var filtered []ExecutionEvent
		for _, ev := range events {
			if ev.EventType == typeFilter {
				filtered = append(filtered, ev)
			}
		}
		events = filtered
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"events": events,
		"count":  len(events),
	})
}

// writeJSON is a helper to write JSON responses.
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// ---------------------------------------------------------------------------
// Replay API
// ---------------------------------------------------------------------------

// ReplayRequest defines a request to replay an execution.
type ReplayRequest struct {
	Mode          string         `json:"mode"`                    // "exact" or "modified"
	Modifications map[string]any `json:"modifications,omitempty"` // step overrides for "modified" mode
}

// ReplayResult describes the outcome of a replay operation.
type ReplayResult struct {
	OriginalExecutionID uuid.UUID `json:"original_execution_id"`
	NewExecutionID      uuid.UUID `json:"new_execution_id"`
	Type                string    `json:"type"` // "replay"
	Mode                string    `json:"mode"`
	Status              string    `json:"status"` // "queued", "started"
}

// ReplayHandler provides HTTP endpoints for the Request Replay API.
type ReplayHandler struct {
	eventStore EventStore
	logger     *slog.Logger
	// ReplayFunc is called to actually replay an execution. It receives the
	// original execution's timeline and returns a new execution ID.
	// If nil, replays are queued but not executed.
	ReplayFunc func(original *MaterializedExecution, mode string, modifications map[string]any) (uuid.UUID, error)
}

// NewReplayHandler creates a new ReplayHandler.
func NewReplayHandler(store EventStore, logger *slog.Logger) *ReplayHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &ReplayHandler{eventStore: store, logger: logger}
}

// RegisterRoutes registers replay API routes.
func (h *ReplayHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/admin/executions/{id}/replay", h.replayExecution)
	mux.HandleFunc("GET /api/v1/admin/executions/{id}/replay", h.getReplayInfo)
}

// replayExecution handles POST /api/v1/admin/executions/{id}/replay
func (h *ReplayHandler) replayExecution(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	if idStr == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing execution ID"})
		return
	}

	originalID, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid execution ID"})
		return
	}

	// Parse replay request
	var req ReplayRequest
	if r.Body != nil {
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
	}

	if req.Mode == "" {
		req.Mode = "exact"
	}
	if req.Mode != "exact" && req.Mode != "modified" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "mode must be 'exact' or 'modified'"})
		return
	}

	// Get the original execution
	original, err := h.eventStore.GetTimeline(r.Context(), originalID)
	if err != nil {
		if err == ErrNotFound {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "original execution not found"})
			return
		}
		h.logger.Error("Failed to get original execution", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	// Create a new execution ID for the replay
	newExecID := uuid.New()
	status := "queued"

	if h.ReplayFunc != nil {
		replayID, err := h.ReplayFunc(original, req.Mode, req.Modifications)
		if err != nil {
			h.logger.Error("Replay failed", "error", err, "original_id", originalID)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "replay failed: " + err.Error()})
			return
		}
		newExecID = replayID
		status = "started"
	}

	// Record a replay event in the event store
	_ = h.eventStore.Append(r.Context(), newExecID, "execution.replay", map[string]any{
		"original_execution_id": originalID.String(),
		"mode":                  req.Mode,
		"type":                  "replay",
	})

	result := ReplayResult{
		OriginalExecutionID: originalID,
		NewExecutionID:      newExecID,
		Type:                "replay",
		Mode:                req.Mode,
		Status:              status,
	}

	writeJSON(w, http.StatusCreated, result)
}

// getReplayInfo handles GET /api/v1/admin/executions/{id}/replay
func (h *ReplayHandler) getReplayInfo(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	if idStr == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing execution ID"})
		return
	}

	id, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid execution ID"})
		return
	}

	// Check if this execution has replay events
	events, err := h.eventStore.GetEvents(r.Context(), id)
	if err != nil {
		h.logger.Error("Failed to get events", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	var replayEvents []map[string]any
	for _, ev := range events {
		if strings.HasPrefix(ev.EventType, "execution.replay") {
			var data map[string]any
			_ = json.Unmarshal(ev.EventData, &data)
			replayEvents = append(replayEvents, map[string]any{
				"event_type": ev.EventType,
				"data":       data,
				"created_at": ev.CreatedAt,
			})
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"execution_id":  id,
		"replay_events": replayEvents,
		"is_replay":     len(replayEvents) > 0,
	})
}
