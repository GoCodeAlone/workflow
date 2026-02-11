package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/GoCodeAlone/workflow/store"
	"github.com/google/uuid"
)

// ExecutionHandler handles execution tracking endpoints.
type ExecutionHandler struct {
	executions  store.ExecutionStore
	workflows   store.WorkflowStore
	permissions *PermissionService
}

// NewExecutionHandler creates a new ExecutionHandler.
func NewExecutionHandler(executions store.ExecutionStore, workflows store.WorkflowStore, permissions *PermissionService) *ExecutionHandler {
	return &ExecutionHandler{
		executions:  executions,
		workflows:   workflows,
		permissions: permissions,
	}
}

// List handles GET /api/v1/workflows/{id}/executions.
func (h *ExecutionHandler) List(w http.ResponseWriter, r *http.Request) {
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

	filter := store.ExecutionFilter{WorkflowID: &wfID}
	if status := r.URL.Query().Get("status"); status != "" {
		filter.Status = store.ExecutionStatus(status)
	}
	if since := r.URL.Query().Get("since"); since != "" {
		t, err := time.Parse(time.RFC3339, since)
		if err == nil {
			filter.Since = &t
		}
	}

	executions, err := h.executions.ListExecutions(r.Context(), filter)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if executions == nil {
		executions = []*store.WorkflowExecution{}
	}
	WriteJSON(w, http.StatusOK, executions)
}

// Get handles GET /api/v1/executions/{id}.
func (h *ExecutionHandler) Get(w http.ResponseWriter, r *http.Request) {
	execID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid execution id")
		return
	}

	exec, err := h.executions.GetExecution(r.Context(), execID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "execution not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	user := UserFromContext(r.Context())
	if user == nil {
		WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if !h.permissions.CanAccess(r.Context(), user.ID, "workflow", exec.WorkflowID, store.RoleViewer) {
		WriteError(w, http.StatusForbidden, "forbidden")
		return
	}

	WriteJSON(w, http.StatusOK, exec)
}

// Steps handles GET /api/v1/executions/{id}/steps.
func (h *ExecutionHandler) Steps(w http.ResponseWriter, r *http.Request) {
	execID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid execution id")
		return
	}

	exec, err := h.executions.GetExecution(r.Context(), execID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "execution not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	user := UserFromContext(r.Context())
	if user == nil {
		WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if !h.permissions.CanAccess(r.Context(), user.ID, "workflow", exec.WorkflowID, store.RoleViewer) {
		WriteError(w, http.StatusForbidden, "forbidden")
		return
	}

	steps, err := h.executions.ListSteps(r.Context(), execID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if steps == nil {
		steps = []*store.ExecutionStep{}
	}
	WriteJSON(w, http.StatusOK, steps)
}

// Cancel handles POST /api/v1/executions/{id}/cancel.
func (h *ExecutionHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	execID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid execution id")
		return
	}

	exec, err := h.executions.GetExecution(r.Context(), execID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "execution not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	user := UserFromContext(r.Context())
	if user == nil {
		WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if !h.permissions.CanAccess(r.Context(), user.ID, "workflow", exec.WorkflowID, store.RoleAdmin) {
		WriteError(w, http.StatusForbidden, "forbidden")
		return
	}

	if exec.Status != store.ExecutionStatusPending && exec.Status != store.ExecutionStatusRunning {
		WriteError(w, http.StatusConflict, "execution is not in a cancellable state")
		return
	}

	now := time.Now()
	durationMs := now.Sub(exec.StartedAt).Milliseconds()
	exec.Status = store.ExecutionStatusCancelled
	exec.CompletedAt = &now
	exec.DurationMs = &durationMs

	if err := h.executions.UpdateExecution(r.Context(), exec); err != nil {
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	WriteJSON(w, http.StatusOK, exec)
}

// Trigger handles POST /api/v1/workflows/{id}/trigger.
func (h *ExecutionHandler) Trigger(w http.ResponseWriter, r *http.Request) {
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

	if !h.permissions.CanAccess(r.Context(), user.ID, "workflow", wfID, store.RoleEditor) {
		WriteError(w, http.StatusForbidden, "forbidden")
		return
	}

	// Verify workflow exists
	if _, err := h.workflows.Get(r.Context(), wfID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "workflow not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	var req struct {
		TriggerData json.RawMessage `json:"trigger_data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	now := time.Now()
	exec := &store.WorkflowExecution{
		ID:          uuid.New(),
		WorkflowID:  wfID,
		TriggerType: "manual",
		TriggerData: req.TriggerData,
		Status:      store.ExecutionStatusPending,
		StartedAt:   now,
	}

	if err := h.executions.CreateExecution(r.Context(), exec); err != nil {
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	WriteJSON(w, http.StatusCreated, exec)
}
