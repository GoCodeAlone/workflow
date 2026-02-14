package api

import (
	"net/http"

	"github.com/GoCodeAlone/workflow/store"
	"github.com/google/uuid"
)

// DashboardHandler handles system and per-workflow dashboard endpoints.
type DashboardHandler struct {
	executions  store.ExecutionStore
	logs        store.LogStore
	workflows   store.WorkflowStore
	projects    store.ProjectStore
	permissions *PermissionService
}

// NewDashboardHandler creates a new DashboardHandler.
func NewDashboardHandler(
	executions store.ExecutionStore,
	logs store.LogStore,
	workflows store.WorkflowStore,
	projects store.ProjectStore,
	permissions *PermissionService,
) *DashboardHandler {
	return &DashboardHandler{
		executions:  executions,
		logs:        logs,
		workflows:   workflows,
		projects:    projects,
		permissions: permissions,
	}
}

// systemDashboard is the response for the system-wide dashboard.
type systemDashboard struct {
	TotalWorkflows    int                   `json:"total_workflows"`
	WorkflowSummaries []workflowDashSummary `json:"workflow_summaries"`
}

// workflowDashSummary is a per-workflow summary for the dashboard.
type workflowDashSummary struct {
	WorkflowID   uuid.UUID                     `json:"workflow_id"`
	WorkflowName string                        `json:"workflow_name"`
	Status       store.WorkflowStatus          `json:"status"`
	Executions   map[store.ExecutionStatus]int `json:"executions"`
	LogCounts    map[store.LogLevel]int        `json:"log_counts"`
}

// System handles GET /api/v1/dashboard.
func (h *DashboardHandler) System(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil {
		WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Get all accessible projects for this user
	projects, err := h.projects.ListForUser(r.Context(), user.ID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	var allWorkflows []*store.WorkflowRecord
	for _, p := range projects {
		pid := p.ID
		wfs, err := h.workflows.List(r.Context(), store.WorkflowFilter{ProjectID: &pid})
		if err != nil {
			continue
		}
		allWorkflows = append(allWorkflows, wfs...)
	}

	summaries := make([]workflowDashSummary, 0, len(allWorkflows))
	for _, wf := range allWorkflows {
		execCounts, _ := h.executions.CountByStatus(r.Context(), wf.ID)
		if execCounts == nil {
			execCounts = make(map[store.ExecutionStatus]int)
		}
		logCounts, _ := h.logs.CountByLevel(r.Context(), wf.ID)
		if logCounts == nil {
			logCounts = make(map[store.LogLevel]int)
		}
		summaries = append(summaries, workflowDashSummary{
			WorkflowID:   wf.ID,
			WorkflowName: wf.Name,
			Status:       wf.Status,
			Executions:   execCounts,
			LogCounts:    logCounts,
		})
	}

	WriteJSON(w, http.StatusOK, systemDashboard{
		TotalWorkflows:    len(allWorkflows),
		WorkflowSummaries: summaries,
	})
}

// Workflow handles GET /api/v1/workflows/{id}/dashboard.
func (h *DashboardHandler) Workflow(w http.ResponseWriter, r *http.Request) {
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

	wf, err := h.workflows.Get(r.Context(), wfID)
	if err != nil {
		WriteError(w, http.StatusNotFound, "workflow not found")
		return
	}

	execCounts, _ := h.executions.CountByStatus(r.Context(), wf.ID)
	if execCounts == nil {
		execCounts = make(map[store.ExecutionStatus]int)
	}
	logCounts, _ := h.logs.CountByLevel(r.Context(), wf.ID)
	if logCounts == nil {
		logCounts = make(map[store.LogLevel]int)
	}

	// Get recent executions
	recentExecs, _ := h.executions.ListExecutions(r.Context(), store.ExecutionFilter{
		WorkflowID: &wfID,
		Pagination: store.Pagination{Limit: 10},
	})
	if recentExecs == nil {
		recentExecs = []*store.WorkflowExecution{}
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"workflow":          wf,
		"execution_counts":  execCounts,
		"log_counts":        logCounts,
		"recent_executions": recentExecs,
	})
}
