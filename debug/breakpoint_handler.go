package debug

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// BreakpointHandler provides an HTTP API for managing pipeline breakpoints
// and controlling paused executions.
type BreakpointHandler struct {
	manager *BreakpointManager
	logger  *slog.Logger
}

// NewBreakpointHandler creates a new BreakpointHandler.
func NewBreakpointHandler(manager *BreakpointManager, logger *slog.Logger) *BreakpointHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &BreakpointHandler{
		manager: manager,
		logger:  logger,
	}
}

// RegisterRoutes registers the breakpoint debug API routes on the provided mux.
func (h *BreakpointHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/debug/breakpoints", h.handleListBreakpoints)
	mux.HandleFunc("POST /api/v1/debug/breakpoints", h.handleSetBreakpoint)
	mux.HandleFunc("DELETE /api/v1/debug/breakpoints/{pipeline}/{step}", h.handleRemoveBreakpoint)
	mux.HandleFunc("PUT /api/v1/debug/breakpoints/{pipeline}/{step}/enable", h.handleEnableBreakpoint)
	mux.HandleFunc("PUT /api/v1/debug/breakpoints/{pipeline}/{step}/disable", h.handleDisableBreakpoint)
	mux.HandleFunc("DELETE /api/v1/debug/breakpoints", h.handleClearAll)
	mux.HandleFunc("GET /api/v1/debug/paused", h.handleListPaused)
	mux.HandleFunc("GET /api/v1/debug/paused/{id}", h.handleGetPaused)
	mux.HandleFunc("POST /api/v1/debug/paused/{id}/resume", h.handleResume)
}

// handleListBreakpoints returns all registered breakpoints.
func (h *BreakpointHandler) handleListBreakpoints(w http.ResponseWriter, _ *http.Request) {
	bps := h.manager.ListBreakpoints()
	writeJSON(w, http.StatusOK, bps)
}

// setBreakpointRequest is the JSON body for setting a breakpoint.
type setBreakpointRequest struct {
	Pipeline  string `json:"pipeline"`
	Step      string `json:"step"`
	Condition string `json:"condition"`
}

// handleSetBreakpoint creates a new breakpoint for a pipeline/step.
func (h *BreakpointHandler) handleSetBreakpoint(w http.ResponseWriter, r *http.Request) {
	var req setBreakpointRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Pipeline == "" || req.Step == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "pipeline and step are required"})
		return
	}

	bp := h.manager.SetBreakpoint(req.Pipeline, req.Step, req.Condition)
	writeJSON(w, http.StatusCreated, bp)
}

// handleRemoveBreakpoint removes a breakpoint by pipeline/step.
func (h *BreakpointHandler) handleRemoveBreakpoint(w http.ResponseWriter, r *http.Request) {
	pipeline := r.PathValue("pipeline")
	step := r.PathValue("step")
	if pipeline == "" || step == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "pipeline and step are required"})
		return
	}

	if !h.manager.RemoveBreakpoint(pipeline, step) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "breakpoint not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// handleEnableBreakpoint enables a breakpoint by pipeline/step.
func (h *BreakpointHandler) handleEnableBreakpoint(w http.ResponseWriter, r *http.Request) {
	pipeline := r.PathValue("pipeline")
	step := r.PathValue("step")
	if pipeline == "" || step == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "pipeline and step are required"})
		return
	}

	if !h.manager.EnableBreakpoint(pipeline, step) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "breakpoint not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "enabled"})
}

// handleDisableBreakpoint disables a breakpoint by pipeline/step.
func (h *BreakpointHandler) handleDisableBreakpoint(w http.ResponseWriter, r *http.Request) {
	pipeline := r.PathValue("pipeline")
	step := r.PathValue("step")
	if pipeline == "" || step == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "pipeline and step are required"})
		return
	}

	if !h.manager.DisableBreakpoint(pipeline, step) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "breakpoint not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "disabled"})
}

// handleClearAll removes all breakpoints and aborts all paused executions.
func (h *BreakpointHandler) handleClearAll(w http.ResponseWriter, _ *http.Request) {
	h.manager.ClearAll()
	writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}

// handleListPaused returns all currently paused executions.
func (h *BreakpointHandler) handleListPaused(w http.ResponseWriter, _ *http.Request) {
	paused := h.manager.ListPaused()
	writeJSON(w, http.StatusOK, paused)
}

// handleGetPaused returns a specific paused execution.
func (h *BreakpointHandler) handleGetPaused(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "execution id is required"})
		return
	}

	pe, ok := h.manager.GetPaused(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "paused execution not found"})
		return
	}
	writeJSON(w, http.StatusOK, pe)
}

// resumeRequest is the JSON body for resuming a paused execution.
type resumeRequest struct {
	Action string         `json:"action"` // "continue", "skip", "abort", "step_over"
	Data   map[string]any `json:"data"`
}

// handleResume sends a resume action to a paused execution.
func (h *BreakpointHandler) handleResume(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "execution id is required"})
		return
	}

	var req resumeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	switch req.Action {
	case "continue", "skip", "abort", "step_over":
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "action must be one of: continue, skip, abort, step_over",
		})
		return
	}

	action := ResumeAction(req)

	if err := h.manager.Resume(id, action); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "resumed"})
}
