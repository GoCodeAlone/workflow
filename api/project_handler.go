package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/GoCodeAlone/workflow/store"
	"github.com/google/uuid"
)

// ProjectHandler handles project CRUD and member endpoints.
type ProjectHandler struct {
	projects    store.ProjectStore
	companies   store.CompanyStore
	memberships store.MembershipStore
	permissions *PermissionService
}

// NewProjectHandler creates a new ProjectHandler.
func NewProjectHandler(projects store.ProjectStore, companies store.CompanyStore, memberships store.MembershipStore, permissions *PermissionService) *ProjectHandler {
	return &ProjectHandler{
		projects:    projects,
		companies:   companies,
		memberships: memberships,
		permissions: permissions,
	}
}

// Create handles POST /api/v1/organizations/{oid}/projects.
func (h *ProjectHandler) Create(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil {
		WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	orgID, err := uuid.Parse(r.PathValue("oid"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid organization id")
		return
	}

	// Verify org exists
	org, err := h.companies.Get(r.Context(), orgID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "organization not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Check permission on the org (treated as company)
	if !h.permissions.CanAccess(r.Context(), user.ID, "company", orgID, store.RoleEditor) {
		WriteError(w, http.StatusForbidden, "forbidden")
		return
	}

	var req struct {
		Name        string          `json:"name"`
		Slug        string          `json:"slug"`
		Description string          `json:"description"`
		Metadata    json.RawMessage `json:"metadata,omitempty"`
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
	project := &store.Project{
		ID:          uuid.New(),
		CompanyID:   org.ID,
		Name:        req.Name,
		Slug:        req.Slug,
		Description: req.Description,
		Metadata:    req.Metadata,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := h.projects.Create(r.Context(), project); err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			WriteError(w, http.StatusConflict, "project slug already exists")
			return
		}
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Creator becomes project owner
	membership := &store.Membership{
		ID:        uuid.New(),
		UserID:    user.ID,
		CompanyID: org.ID,
		ProjectID: &project.ID,
		Role:      store.RoleOwner,
		CreatedAt: now,
		UpdatedAt: now,
	}
	_ = h.memberships.Create(r.Context(), membership)

	WriteJSON(w, http.StatusCreated, project)
}

// List handles GET /api/v1/organizations/{oid}/projects.
func (h *ProjectHandler) List(w http.ResponseWriter, r *http.Request) {
	orgID, err := uuid.Parse(r.PathValue("oid"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid organization id")
		return
	}
	projects, err := h.projects.List(r.Context(), store.ProjectFilter{CompanyID: &orgID})
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	WriteJSON(w, http.StatusOK, projects)
}

// Get handles GET /api/v1/projects/{id}.
func (h *ProjectHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid project id")
		return
	}
	project, err := h.projects.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "project not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	WriteJSON(w, http.StatusOK, project)
}

// Update handles PUT /api/v1/projects/{id}.
func (h *ProjectHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid project id")
		return
	}

	project, err := h.projects.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "project not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	var req struct {
		Name        *string          `json:"name"`
		Slug        *string          `json:"slug"`
		Description *string          `json:"description"`
		Metadata    *json.RawMessage `json:"metadata"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name != nil {
		project.Name = *req.Name
	}
	if req.Slug != nil {
		project.Slug = *req.Slug
	}
	if req.Description != nil {
		project.Description = *req.Description
	}
	if req.Metadata != nil {
		project.Metadata = *req.Metadata
	}
	project.UpdatedAt = time.Now()

	if err := h.projects.Update(r.Context(), project); err != nil {
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	WriteJSON(w, http.StatusOK, project)
}

// Delete handles DELETE /api/v1/projects/{id}.
func (h *ProjectHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid project id")
		return
	}
	if err := h.projects.Delete(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "project not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// AddMember handles POST /api/v1/projects/{id}/members.
func (h *ProjectHandler) AddMember(w http.ResponseWriter, r *http.Request) {
	projectID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid project id")
		return
	}

	project, err := h.projects.Get(r.Context(), projectID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "project not found")
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
	userID, err := uuid.Parse(req.UserID)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid user_id")
		return
	}
	if !store.ValidRoles[req.Role] {
		WriteError(w, http.StatusBadRequest, "invalid role")
		return
	}

	now := time.Now()
	m := &store.Membership{
		ID:        uuid.New(),
		UserID:    userID,
		CompanyID: project.CompanyID,
		ProjectID: &projectID,
		Role:      req.Role,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := h.memberships.Create(r.Context(), m); err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			WriteError(w, http.StatusConflict, "user already a member")
			return
		}
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	WriteJSON(w, http.StatusCreated, m)
}

// ListMembers handles GET /api/v1/projects/{id}/members.
func (h *ProjectHandler) ListMembers(w http.ResponseWriter, r *http.Request) {
	projectID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid project id")
		return
	}
	page, pageSize := parsePagination(r)
	members, err := h.memberships.List(r.Context(), store.MembershipFilter{
		ProjectID: &projectID,
		Pagination: store.Pagination{
			Offset: (page - 1) * pageSize,
			Limit:  pageSize,
		},
	})
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	WritePaginated(w, members, len(members), page, pageSize)
}
