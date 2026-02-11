package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/GoCodeAlone/workflow/store"
	"github.com/google/uuid"
)

// CompanyHandler handles company CRUD endpoints.
type CompanyHandler struct {
	companies   store.CompanyStore
	memberships store.MembershipStore
	permissions *PermissionService
}

// NewCompanyHandler creates a new CompanyHandler.
func NewCompanyHandler(companies store.CompanyStore, memberships store.MembershipStore, permissions *PermissionService) *CompanyHandler {
	return &CompanyHandler{
		companies:   companies,
		memberships: memberships,
		permissions: permissions,
	}
}

// Create handles POST /api/v1/companies.
func (h *CompanyHandler) Create(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil {
		WriteError(w, http.StatusUnauthorized, "unauthorized")
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
	company := &store.Company{
		ID:        uuid.New(),
		Name:      req.Name,
		Slug:      req.Slug,
		OwnerID:   user.ID,
		Metadata:  req.Metadata,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := h.companies.Create(r.Context(), company); err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			WriteError(w, http.StatusConflict, "company slug already exists")
			return
		}
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Creator becomes owner
	membership := &store.Membership{
		ID:        uuid.New(),
		UserID:    user.ID,
		CompanyID: company.ID,
		Role:      store.RoleOwner,
		CreatedAt: now,
		UpdatedAt: now,
	}
	_ = h.memberships.Create(r.Context(), membership)

	WriteJSON(w, http.StatusCreated, company)
}

// List handles GET /api/v1/companies.
func (h *CompanyHandler) List(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	if user == nil {
		WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	companies, err := h.companies.ListForUser(r.Context(), user.ID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	WriteJSON(w, http.StatusOK, companies)
}

// Get handles GET /api/v1/companies/{id}.
func (h *CompanyHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid company id")
		return
	}
	company, err := h.companies.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "company not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	WriteJSON(w, http.StatusOK, company)
}

// Update handles PUT /api/v1/companies/{id}.
func (h *CompanyHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid company id")
		return
	}

	company, err := h.companies.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "company not found")
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
		company.Name = *req.Name
	}
	if req.Slug != nil {
		company.Slug = *req.Slug
	}
	if req.Metadata != nil {
		company.Metadata = *req.Metadata
	}
	company.UpdatedAt = time.Now()

	if err := h.companies.Update(r.Context(), company); err != nil {
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	WriteJSON(w, http.StatusOK, company)
}

// Delete handles DELETE /api/v1/companies/{id}.
func (h *CompanyHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid company id")
		return
	}
	if err := h.companies.Delete(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "company not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// AddMember handles POST /api/v1/companies/{id}/members.
func (h *CompanyHandler) AddMember(w http.ResponseWriter, r *http.Request) {
	companyID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid company id")
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
		CompanyID: companyID,
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

// ListMembers handles GET /api/v1/companies/{id}/members.
func (h *CompanyHandler) ListMembers(w http.ResponseWriter, r *http.Request) {
	companyID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid company id")
		return
	}
	page, pageSize := parsePagination(r)
	members, err := h.memberships.List(r.Context(), store.MembershipFilter{
		CompanyID: &companyID,
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

// UpdateMember handles PUT /api/v1/companies/{id}/members/{uid}.
func (h *CompanyHandler) UpdateMember(w http.ResponseWriter, r *http.Request) {
	companyID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid company id")
		return
	}
	memberUserID, err := uuid.Parse(r.PathValue("uid"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid user id")
		return
	}

	var req struct {
		Role store.Role `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !store.ValidRoles[req.Role] {
		WriteError(w, http.StatusBadRequest, "invalid role")
		return
	}

	// Find the membership
	members, err := h.memberships.List(r.Context(), store.MembershipFilter{
		UserID:    &memberUserID,
		CompanyID: &companyID,
	})
	if err != nil || len(members) == 0 {
		WriteError(w, http.StatusNotFound, "membership not found")
		return
	}

	m := members[0]
	m.Role = req.Role
	m.UpdatedAt = time.Now()
	if err := h.memberships.Update(r.Context(), m); err != nil {
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	WriteJSON(w, http.StatusOK, m)
}

// RemoveMember handles DELETE /api/v1/companies/{id}/members/{uid}.
func (h *CompanyHandler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	companyID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid company id")
		return
	}
	memberUserID, err := uuid.Parse(r.PathValue("uid"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid user id")
		return
	}

	members, err := h.memberships.List(r.Context(), store.MembershipFilter{
		UserID:    &memberUserID,
		CompanyID: &companyID,
	})
	if err != nil || len(members) == 0 {
		WriteError(w, http.StatusNotFound, "membership not found")
		return
	}
	if err := h.memberships.Delete(r.Context(), members[0].ID); err != nil {
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- helpers ---

func parsePagination(r *http.Request) (page, pageSize int) {
	page = 1
	pageSize = 50
	if v := r.URL.Query().Get("page"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 {
			page = p
		}
	}
	if v := r.URL.Query().Get("page_size"); v != "" {
		if ps, err := strconv.Atoi(v); err == nil && ps > 0 && ps <= 100 {
			pageSize = ps
		}
	}
	return
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		if r == ' ' || r == '_' {
			return '-'
		}
		return -1
	}, s)
	return s
}
