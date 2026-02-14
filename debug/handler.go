package debug

import (
	"encoding/json"
	"net/http"
)

// Handler provides an HTTP API for controlling the debugger.
type Handler struct {
	debugger *Debugger
}

// NewHandler creates a new debug HTTP API handler.
func NewHandler(d *Debugger) *Handler {
	return &Handler{debugger: d}
}

// RegisterRoutes registers the debug API routes on the provided mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/debug/state", h.handleGetState)
	mux.HandleFunc("GET /api/debug/breakpoints", h.handleListBreakpoints)
	mux.HandleFunc("POST /api/debug/breakpoints", h.handleAddBreakpoint)
	mux.HandleFunc("DELETE /api/debug/breakpoints/{id}", h.handleRemoveBreakpoint)
	mux.HandleFunc("POST /api/debug/step", h.handleStep)
	mux.HandleFunc("POST /api/debug/continue", h.handleContinue)
	mux.HandleFunc("POST /api/debug/reset", h.handleReset)
}

func (h *Handler) handleGetState(w http.ResponseWriter, _ *http.Request) {
	state := h.debugger.State()
	writeJSON(w, http.StatusOK, state)
}

func (h *Handler) handleListBreakpoints(w http.ResponseWriter, _ *http.Request) {
	bps := h.debugger.ListBreakpoints()
	writeJSON(w, http.StatusOK, bps)
}

type addBreakpointRequest struct {
	Type      BreakpointType `json:"type"`
	Target    string         `json:"target"`
	Condition string         `json:"condition"`
}

type addBreakpointResponse struct {
	ID string `json:"id"`
}

func (h *Handler) handleAddBreakpoint(w http.ResponseWriter, r *http.Request) {
	var req addBreakpointRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Type == "" || req.Target == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "type and target are required"})
		return
	}
	switch req.Type {
	case BreakOnModule, BreakOnWorkflow, BreakOnTrigger:
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "type must be module, workflow, or trigger"})
		return
	}

	id := h.debugger.AddBreakpoint(req.Type, req.Target, req.Condition)
	writeJSON(w, http.StatusCreated, addBreakpointResponse{ID: id})
}

func (h *Handler) handleRemoveBreakpoint(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "breakpoint id is required"})
		return
	}
	if err := h.debugger.RemoveBreakpoint(id); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

func (h *Handler) handleStep(w http.ResponseWriter, _ *http.Request) {
	if err := h.debugger.Step(); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "stepped"})
}

func (h *Handler) handleContinue(w http.ResponseWriter, _ *http.Request) {
	if err := h.debugger.Continue(); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "continued"})
}

func (h *Handler) handleReset(w http.ResponseWriter, _ *http.Request) {
	h.debugger.Reset()
	writeJSON(w, http.StatusOK, map[string]string{"status": "reset"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
