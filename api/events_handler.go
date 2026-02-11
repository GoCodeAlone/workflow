package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/GoCodeAlone/workflow/store"
	"github.com/google/uuid"
)

// EventsHandler handles event inspection and streaming endpoints.
type EventsHandler struct {
	executions  store.ExecutionStore
	logs        store.LogStore
	permissions *PermissionService
}

// NewEventsHandler creates a new EventsHandler.
func NewEventsHandler(executions store.ExecutionStore, logs store.LogStore, permissions *PermissionService) *EventsHandler {
	return &EventsHandler{
		executions:  executions,
		logs:        logs,
		permissions: permissions,
	}
}

// List handles GET /api/v1/workflows/{id}/events - lists recent execution events.
func (h *EventsHandler) List(w http.ResponseWriter, r *http.Request) {
	wfID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid workflow id")
		return
	}

	user := UserFromContext(r.Context())
	if user == nil {
		WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if !h.permissions.CanAccess(r.Context(), user.ID, "workflow", wfID, store.RoleViewer) {
		WriteError(w, http.StatusForbidden, "forbidden")
		return
	}

	executions, err := h.executions.ListExecutions(r.Context(), store.ExecutionFilter{
		WorkflowID: &wfID,
		Pagination: store.Pagination{Limit: 50},
	})
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if executions == nil {
		executions = []*store.WorkflowExecution{}
	}
	WriteJSON(w, http.StatusOK, executions)
}

// Stream handles GET /api/v1/workflows/{id}/events/stream (SSE).
func (h *EventsHandler) Stream(w http.ResponseWriter, r *http.Request) {
	wfID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid workflow id")
		return
	}

	user := UserFromContext(r.Context())
	if user == nil {
		WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if !h.permissions.CanAccess(r.Context(), user.ID, "workflow", wfID, store.RoleViewer) {
		WriteError(w, http.StatusForbidden, "forbidden")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		WriteError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	lastSeen := time.Now()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			executions, err := h.executions.ListExecutions(r.Context(), store.ExecutionFilter{
				WorkflowID: &wfID,
				Since:      &lastSeen,
			})
			if err != nil {
				continue
			}
			for _, e := range executions {
				data, err := json.Marshal(e)
				if err != nil {
					continue
				}
				fmt.Fprintf(w, "data: %s\n\n", data)
				if e.StartedAt.After(lastSeen) {
					lastSeen = e.StartedAt
				}
			}
			flusher.Flush()
		}
	}
}
