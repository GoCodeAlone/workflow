package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/GoCodeAlone/workflow/store"
	"github.com/google/uuid"
)

// WorkflowOrchestrator is implemented by the multi-workflow engine manager to
// allow the API layer to start and stop live workflow engines. It is optional;
// when nil the Deploy and Stop handlers only update the database status.
type WorkflowOrchestrator interface {
	DeployWorkflow(ctx context.Context, workflowID uuid.UUID) error
	StopWorkflow(ctx context.Context, workflowID uuid.UUID) error
}

// WorkflowHandler handles workflow CRUD and lifecycle endpoints.
type WorkflowHandler struct {
	workflows    store.WorkflowStore
	projects     store.ProjectStore
	memberships  store.MembershipStore
	permissions  *PermissionService
	orchestrator WorkflowOrchestrator // optional; nil when no engine manager is wired
}

// NewWorkflowHandler creates a new WorkflowHandler.
func NewWorkflowHandler(workflows store.WorkflowStore, projects store.ProjectStore, memberships store.MembershipStore, permissions *PermissionService) *WorkflowHandler {
	return &WorkflowHandler{
		workflows:   workflows,
		projects:    projects,
		memberships: memberships,
		permissions: permissions,
	}
}

// Create handles POST /api/v1/projects/{pid}/workflows.
func (h *WorkflowHandler) Create(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil {
		WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	projectID, err := uuid.Parse(r.PathValue("pid"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid project id")
		return
	}

	// Verify project exists
	if _, err := h.projects.Get(r.Context(), projectID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "project not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Check permission
	if !h.permissions.CanAccess(r.Context(), user.ID, "project", projectID, store.RoleEditor) {
		WriteError(w, http.StatusForbidden, "forbidden")
		return
	}

	var req struct {
		Name        string `json:"name"`
		Slug        string `json:"slug"`
		Description string `json:"description"`
		ConfigYAML  string `json:"config_yaml"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		WriteError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.Slug == "" {
		req.Slug = slugify(req.Name)
	}

	now := time.Now()
	wf := &store.WorkflowRecord{
		ID:          uuid.New(),
		ProjectID:   projectID,
		Name:        req.Name,
		Slug:        req.Slug,
		Description: req.Description,
		ConfigYAML:  req.ConfigYAML,
		Version:     1,
		Status:      store.WorkflowStatusDraft,
		CreatedBy:   user.ID,
		UpdatedBy:   user.ID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := h.workflows.Create(r.Context(), wf); err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			WriteError(w, http.StatusConflict, "workflow slug already exists in project")
			return
		}
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	WriteJSON(w, http.StatusCreated, wf)
}

// ListAll handles GET /api/v1/workflows (all accessible workflows).
func (h *WorkflowHandler) ListAll(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil {
		WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Get all projects the user has access to
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
	if allWorkflows == nil {
		allWorkflows = []*store.WorkflowRecord{}
	}
	WriteJSON(w, http.StatusOK, allWorkflows)
}

// ListInProject handles GET /api/v1/projects/{pid}/workflows.
func (h *WorkflowHandler) ListInProject(w http.ResponseWriter, r *http.Request) {
	projectID, err := uuid.Parse(r.PathValue("pid"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid project id")
		return
	}
	wfs, err := h.workflows.List(r.Context(), store.WorkflowFilter{ProjectID: &projectID})
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if wfs == nil {
		wfs = []*store.WorkflowRecord{}
	}
	WriteJSON(w, http.StatusOK, wfs)
}

// Get handles GET /api/v1/workflows/{id}.
func (h *WorkflowHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid workflow id")
		return
	}
	wf, err := h.workflows.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "workflow not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	WriteJSON(w, http.StatusOK, wf)
}

// Update handles PUT /api/v1/workflows/{id}.
func (h *WorkflowHandler) Update(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil {
		WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid workflow id")
		return
	}

	wf, err := h.workflows.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "workflow not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	var req struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		ConfigYAML  *string `json:"config_yaml"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name != nil {
		wf.Name = *req.Name
	}
	if req.Description != nil {
		wf.Description = *req.Description
	}
	if req.ConfigYAML != nil {
		wf.ConfigYAML = *req.ConfigYAML
		wf.Version++
	}
	wf.UpdatedBy = user.ID
	wf.UpdatedAt = time.Now()

	if err := h.workflows.Update(r.Context(), wf); err != nil {
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	WriteJSON(w, http.StatusOK, wf)
}

// Delete handles DELETE /api/v1/workflows/{id}.
func (h *WorkflowHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid workflow id")
		return
	}
	if err := h.workflows.Delete(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "workflow not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Deploy handles POST /api/v1/workflows/{id}/deploy.
func (h *WorkflowHandler) Deploy(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid workflow id")
		return
	}
	wf, err := h.workflows.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "workflow not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if h.orchestrator != nil {
		if oErr := h.orchestrator.DeployWorkflow(r.Context(), id); oErr != nil {
			WriteError(w, http.StatusInternalServerError, oErr.Error())
			return
		}
		// Re-fetch to get updated status written by the orchestrator.
		wf, err = h.workflows.Get(r.Context(), id)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "internal error")
			return
		}
	} else {
		wf.Status = store.WorkflowStatusActive
		wf.UpdatedAt = time.Now()
		if err := h.workflows.Update(r.Context(), wf); err != nil {
			WriteError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	WriteJSON(w, http.StatusOK, wf)
}

// Stop handles POST /api/v1/workflows/{id}/stop.
func (h *WorkflowHandler) Stop(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid workflow id")
		return
	}
	wf, err := h.workflows.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "workflow not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if h.orchestrator != nil {
		if oErr := h.orchestrator.StopWorkflow(r.Context(), id); oErr != nil {
			WriteError(w, http.StatusInternalServerError, oErr.Error())
			return
		}
		// Re-fetch to get updated status written by the orchestrator.
		wf, err = h.workflows.Get(r.Context(), id)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "internal error")
			return
		}
	} else {
		wf.Status = store.WorkflowStatusStopped
		wf.UpdatedAt = time.Now()
		if err := h.workflows.Update(r.Context(), wf); err != nil {
			WriteError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}
	WriteJSON(w, http.StatusOK, wf)
}

// Status handles GET /api/v1/workflows/{id}/status.
func (h *WorkflowHandler) Status(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid workflow id")
		return
	}
	wf, err := h.workflows.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "workflow not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"id":      wf.ID,
		"status":  wf.Status,
		"version": wf.Version,
	})
}

// ListVersions handles GET /api/v1/workflows/{id}/versions.
func (h *WorkflowHandler) ListVersions(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid workflow id")
		return
	}
	versions, err := h.workflows.ListVersions(r.Context(), id)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if versions == nil {
		versions = []*store.WorkflowRecord{}
	}
	WriteJSON(w, http.StatusOK, versions)
}

// GetVersion handles GET /api/v1/workflows/{id}/versions/{v}.
func (h *WorkflowHandler) GetVersion(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid workflow id")
		return
	}
	v, err := strconv.Atoi(r.PathValue("v"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid version number")
		return
	}
	wf, err := h.workflows.GetVersion(r.Context(), id, v)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "version not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	WriteJSON(w, http.StatusOK, wf)
}

// SetPermission handles POST /api/v1/workflows/{id}/permissions.
func (h *WorkflowHandler) SetPermission(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil {
		WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	wfID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid workflow id")
		return
	}

	wf, err := h.workflows.Get(r.Context(), wfID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "workflow not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	var req struct {
		UserID string     `json:"user_id"`
		Role   store.Role `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	targetUserID, err := uuid.Parse(req.UserID)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid user_id")
		return
	}
	if !store.ValidRoles[req.Role] {
		WriteError(w, http.StatusBadRequest, "invalid role")
		return
	}

	proj, err := h.projects.Get(r.Context(), wf.ProjectID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	now := time.Now()
	m := &store.Membership{
		ID:        uuid.New(),
		UserID:    targetUserID,
		CompanyID: proj.CompanyID,
		ProjectID: &wf.ProjectID,
		Role:      req.Role,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := h.memberships.Create(r.Context(), m); err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			// Update existing membership role
			members, listErr := h.memberships.List(r.Context(), store.MembershipFilter{
				UserID:    &targetUserID,
				ProjectID: &wf.ProjectID,
			})
			if listErr != nil || len(members) == 0 {
				WriteError(w, http.StatusInternalServerError, "internal error")
				return
			}
			members[0].Role = req.Role
			members[0].UpdatedAt = now
			if updateErr := h.memberships.Update(r.Context(), members[0]); updateErr != nil {
				WriteError(w, http.StatusInternalServerError, "internal error")
				return
			}
			WriteJSON(w, http.StatusOK, members[0])
			return
		}
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	WriteJSON(w, http.StatusCreated, m)
}

// ListPermissions handles GET /api/v1/workflows/{id}/permissions.
func (h *WorkflowHandler) ListPermissions(w http.ResponseWriter, r *http.Request) {
	wfID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid workflow id")
		return
	}

	wf, err := h.workflows.Get(r.Context(), wfID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "workflow not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	members, err := h.memberships.List(r.Context(), store.MembershipFilter{
		ProjectID: &wf.ProjectID,
	})
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if members == nil {
		members = []*store.Membership{}
	}
	WriteJSON(w, http.StatusOK, members)
}
