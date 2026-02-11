package api

import (
	"net/http"
	"time"

	"github.com/GoCodeAlone/workflow/store"
	"github.com/google/uuid"
)

// AuditHandler handles audit log query endpoints.
type AuditHandler struct {
	audit       store.AuditStore
	permissions *PermissionService
}

// NewAuditHandler creates a new AuditHandler.
func NewAuditHandler(audit store.AuditStore, permissions *PermissionService) *AuditHandler {
	return &AuditHandler{
		audit:       audit,
		permissions: permissions,
	}
}

// Query handles GET /api/v1/companies/{id}/audit.
func (h *AuditHandler) Query(w http.ResponseWriter, r *http.Request) {
	companyID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid company id")
		return
	}

	user := UserFromContext(r.Context())
	if user == nil {
		WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if !h.permissions.CanAccess(r.Context(), user.ID, "company", companyID, store.RoleAdmin) {
		WriteError(w, http.StatusForbidden, "forbidden")
		return
	}

	filter := store.AuditFilter{}
	if action := r.URL.Query().Get("action"); action != "" {
		filter.Action = action
	}
	if rt := r.URL.Query().Get("resource_type"); rt != "" {
		filter.ResourceType = rt
	}
	if rid := r.URL.Query().Get("resource_id"); rid != "" {
		id, err := uuid.Parse(rid)
		if err == nil {
			filter.ResourceID = &id
		}
	}
	if uid := r.URL.Query().Get("user_id"); uid != "" {
		id, err := uuid.Parse(uid)
		if err == nil {
			filter.UserID = &id
		}
	}
	if since := r.URL.Query().Get("since"); since != "" {
		t, err := time.Parse(time.RFC3339, since)
		if err == nil {
			filter.Since = &t
		}
	}

	entries, err := h.audit.Query(r.Context(), filter)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if entries == nil {
		entries = []*store.AuditEntry{}
	}
	WriteJSON(w, http.StatusOK, entries)
}
