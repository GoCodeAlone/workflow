package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/GoCodeAlone/workflow/store"
	"github.com/google/uuid"
)

// OrgHandler handles organization CRUD endpoints.
// Organizations are companies nested under a parent company.
// The store treats Company and Organization as the same type.
type OrgHandler struct {
	companies   store.CompanyStore
	memberships store.MembershipStore
	permissions *PermissionService
}

// NewOrgHandler creates a new OrgHandler.
func NewOrgHandler(companies store.CompanyStore, memberships store.MembershipStore, permissions *PermissionService) *OrgHandler {
	return &OrgHandler{
		companies:   companies,
		memberships: memberships,
		permissions: permissions,
	}
}

// Create handles POST /api/v1/companies/{cid}/organizations.
func (h *OrgHandler) Create(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil {
		WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	parentID, err := uuid.Parse(r.PathValue("cid"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid company id")
		return
	}

	// Verify parent company exists
	if _, err := h.companies.Get(r.Context(), parentID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "company not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Check permission on parent company
	if !h.permissions.CanAccess(r.Context(), user.ID, "company", parentID, store.RoleEditor) {
		WriteError(w, http.StatusForbidden, "forbidden")
		return
	}

	var req struct {
		Name     string          `json:"name"`
		Slug     string          `json:"slug"`
		Metadata json.RawMessage `json:"metadata,omitempty"`
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
	org := &store.Company{
		ID:        uuid.New(),
		Name:      req.Name,
		Slug:      req.Slug,
		OwnerID:   parentID, // Parent company ID stored as OwnerID for orgs
		Metadata:  req.Metadata,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := h.companies.Create(r.Context(), org); err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			WriteError(w, http.StatusConflict, "organization slug already exists")
			return
		}
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	WriteJSON(w, http.StatusCreated, org)
}

// List handles GET /api/v1/companies/{cid}/organizations.
func (h *OrgHandler) List(w http.ResponseWriter, r *http.Request) {
	parentID, err := uuid.Parse(r.PathValue("cid"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid company id")
		return
	}

	orgs, err := h.companies.List(r.Context(), store.CompanyFilter{
		OwnerID: &parentID,
	})
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	WriteJSON(w, http.StatusOK, orgs)
}

// Get handles GET /api/v1/organizations/{id}.
func (h *OrgHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid organization id")
		return
	}
	org, err := h.companies.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "organization not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	WriteJSON(w, http.StatusOK, org)
}

// Update handles PUT /api/v1/organizations/{id}.
func (h *OrgHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid organization id")
		return
	}

	org, err := h.companies.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "organization not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	var req struct {
		Name     *string          `json:"name"`
		Slug     *string          `json:"slug"`
		Metadata *json.RawMessage `json:"metadata"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name != nil {
		org.Name = *req.Name
	}
	if req.Slug != nil {
		org.Slug = *req.Slug
	}
	if req.Metadata != nil {
		org.Metadata = *req.Metadata
	}
	org.UpdatedAt = time.Now()

	if err := h.companies.Update(r.Context(), org); err != nil {
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	WriteJSON(w, http.StatusOK, org)
}

// Delete handles DELETE /api/v1/organizations/{id}.
func (h *OrgHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid organization id")
		return
	}
	if err := h.companies.Delete(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "organization not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
