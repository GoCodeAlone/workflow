package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/GoCodeAlone/workflow/store"
	"github.com/google/uuid"
)

// LogHandler handles log query and streaming endpoints.
type LogHandler struct {
	logs        store.LogStore
	permissions *PermissionService
}

// NewLogHandler creates a new LogHandler.
func NewLogHandler(logs store.LogStore, permissions *PermissionService) *LogHandler {
	return &LogHandler{
		logs:        logs,
		permissions: permissions,
	}
}

// Query handles GET /api/v1/workflows/{id}/logs.
func (h *LogHandler) Query(w http.ResponseWriter, r *http.Request) {
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

	filter := store.LogFilter{WorkflowID: &wfID}
	if level := r.URL.Query().Get("level"); level != "" {
		filter.Level = store.LogLevel(level)
	}
	if module := r.URL.Query().Get("module"); module != "" {
		filter.ModuleName = module
	}
	if execID := r.URL.Query().Get("execution_id"); execID != "" {
		id, err := uuid.Parse(execID)
		if err == nil {
			filter.ExecutionID = &id
		}
	}
	if since := r.URL.Query().Get("since"); since != "" {
		t, err := time.Parse(time.RFC3339, since)
		if err == nil {
			filter.Since = &t
		}
	}

	logs, err := h.logs.Query(r.Context(), filter)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if logs == nil {
		logs = []*store.ExecutionLog{}
	}
	WriteJSON(w, http.StatusOK, logs)
}

// Stream handles GET /api/v1/workflows/{id}/logs/stream (SSE).
func (h *LogHandler) Stream(w http.ResponseWriter, r *http.Request) {
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
			filter := store.LogFilter{
				WorkflowID: &wfID,
				Since:      &lastSeen,
			}
			logs, err := h.logs.Query(r.Context(), filter)
			if err != nil {
				continue
			}
			for _, l := range logs {
				data, err := json.Marshal(l)
				if err != nil {
					continue
				}
				_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
				if l.CreatedAt.After(lastSeen) {
					lastSeen = l.CreatedAt
				}
			}
			flusher.Flush()
		}
	}
}
