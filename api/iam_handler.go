package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/GoCodeAlone/workflow/iam"
	"github.com/GoCodeAlone/workflow/store"
	"github.com/google/uuid"
)

// IAMHandler handles IAM provider and role mapping endpoints.
type IAMHandler struct {
	iamStore    store.IAMStore
	resolver    *iam.IAMResolver
	permissions *PermissionService
}

// NewIAMHandler creates a new IAMHandler.
func NewIAMHandler(iamStore store.IAMStore, resolver *iam.IAMResolver, permissions *PermissionService) *IAMHandler {
	return &IAMHandler{
		iamStore:    iamStore,
		resolver:    resolver,
		permissions: permissions,
	}
}

// CreateProvider handles POST /api/v1/companies/{id}/iam/providers.
func (h *IAMHandler) CreateProvider(w http.ResponseWriter, r *http.Request) {
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

	var req struct {
		ProviderType store.IAMProviderType `json:"provider_type"`
		Name         string                `json:"name"`
		Config       json.RawMessage       `json:"config"`
		Enabled      *bool                 `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		WriteError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.ProviderType == "" {
		WriteError(w, http.StatusBadRequest, "provider_type is required")
		return
	}

	// Validate config with the provider if registered
	if provider, ok := h.resolver.GetProvider(req.ProviderType); ok {
		if err := provider.ValidateConfig(req.Config); err != nil {
			WriteError(w, http.StatusBadRequest, "invalid provider config: "+err.Error())
			return
		}
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	now := time.Now()
	p := &store.IAMProviderConfig{
		ID:           uuid.New(),
		CompanyID:    companyID,
		ProviderType: req.ProviderType,
		Name:         req.Name,
		Config:       req.Config,
		Enabled:      enabled,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := h.iamStore.CreateProvider(r.Context(), p); err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			WriteError(w, http.StatusConflict, "provider name already exists in company")
			return
		}
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	WriteJSON(w, http.StatusCreated, p)
}

// ListProviders handles GET /api/v1/companies/{id}/iam/providers.
func (h *IAMHandler) ListProviders(w http.ResponseWriter, r *http.Request) {
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

	if !h.permissions.CanAccess(r.Context(), user.ID, "company", companyID, store.RoleViewer) {
		WriteError(w, http.StatusForbidden, "forbidden")
		return
	}

	providers, err := h.iamStore.ListProviders(r.Context(), store.IAMProviderFilter{CompanyID: &companyID})
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if providers == nil {
		providers = []*store.IAMProviderConfig{}
	}
	WriteJSON(w, http.StatusOK, providers)
}

// GetProvider handles GET /api/v1/iam/providers/{id}.
func (h *IAMHandler) GetProvider(w http.ResponseWriter, r *http.Request) {
	providerID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid provider id")
		return
	}

	p, err := h.iamStore.GetProvider(r.Context(), providerID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "provider not found")
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

	if !h.permissions.CanAccess(r.Context(), user.ID, "company", p.CompanyID, store.RoleViewer) {
		WriteError(w, http.StatusForbidden, "forbidden")
		return
	}

	WriteJSON(w, http.StatusOK, p)
}

// UpdateProvider handles PUT /api/v1/iam/providers/{id}.
func (h *IAMHandler) UpdateProvider(w http.ResponseWriter, r *http.Request) {
	providerID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid provider id")
		return
	}

	p, err := h.iamStore.GetProvider(r.Context(), providerID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "provider not found")
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

	if !h.permissions.CanAccess(r.Context(), user.ID, "company", p.CompanyID, store.RoleAdmin) {
		WriteError(w, http.StatusForbidden, "forbidden")
		return
	}

	var req struct {
		Name    *string         `json:"name"`
		Config  json.RawMessage `json:"config"`
		Enabled *bool           `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name != nil {
		p.Name = *req.Name
	}
	if req.Config != nil {
		if provider, ok := h.resolver.GetProvider(p.ProviderType); ok {
			if err := provider.ValidateConfig(req.Config); err != nil {
				WriteError(w, http.StatusBadRequest, "invalid provider config: "+err.Error())
				return
			}
		}
		p.Config = req.Config
	}
	if req.Enabled != nil {
		p.Enabled = *req.Enabled
	}

	if err := h.iamStore.UpdateProvider(r.Context(), p); err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			WriteError(w, http.StatusConflict, "provider name already exists in company")
			return
		}
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	WriteJSON(w, http.StatusOK, p)
}

// DeleteProvider handles DELETE /api/v1/iam/providers/{id}.
func (h *IAMHandler) DeleteProvider(w http.ResponseWriter, r *http.Request) {
	providerID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid provider id")
		return
	}

	p, err := h.iamStore.GetProvider(r.Context(), providerID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "provider not found")
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

	if !h.permissions.CanAccess(r.Context(), user.ID, "company", p.CompanyID, store.RoleAdmin) {
		WriteError(w, http.StatusForbidden, "forbidden")
		return
	}

	if err := h.iamStore.DeleteProvider(r.Context(), providerID); err != nil {
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// TestConnection handles POST /api/v1/iam/providers/{id}/test.
func (h *IAMHandler) TestConnection(w http.ResponseWriter, r *http.Request) {
	providerID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid provider id")
		return
	}

	p, err := h.iamStore.GetProvider(r.Context(), providerID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "provider not found")
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

	if !h.permissions.CanAccess(r.Context(), user.ID, "company", p.CompanyID, store.RoleAdmin) {
		WriteError(w, http.StatusForbidden, "forbidden")
		return
	}

	provider, ok := h.resolver.GetProvider(p.ProviderType)
	if !ok {
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"message": "no provider implementation registered for type: " + string(p.ProviderType),
		})
		return
	}

	if err := provider.TestConnection(r.Context(), p.Config); err != nil {
		WriteJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "connection successful",
	})
}

// CreateMapping handles POST /api/v1/iam/providers/{id}/mappings.
func (h *IAMHandler) CreateMapping(w http.ResponseWriter, r *http.Request) {
	providerID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid provider id")
		return
	}

	p, err := h.iamStore.GetProvider(r.Context(), providerID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "provider not found")
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

	if !h.permissions.CanAccess(r.Context(), user.ID, "company", p.CompanyID, store.RoleAdmin) {
		WriteError(w, http.StatusForbidden, "forbidden")
		return
	}

	var req struct {
		ExternalIdentifier string     `json:"external_identifier"`
		ResourceType       string     `json:"resource_type"`
		ResourceID         uuid.UUID  `json:"resource_id"`
		Role               store.Role `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ExternalIdentifier == "" || req.ResourceType == "" || req.ResourceID == uuid.Nil {
		WriteError(w, http.StatusBadRequest, "external_identifier, resource_type, and resource_id are required")
		return
	}
	if !store.ValidRoles[req.Role] {
		WriteError(w, http.StatusBadRequest, "invalid role")
		return
	}

	m := &store.IAMRoleMapping{
		ID:                 uuid.New(),
		ProviderID:         providerID,
		ExternalIdentifier: req.ExternalIdentifier,
		ResourceType:       req.ResourceType,
		ResourceID:         req.ResourceID,
		Role:               req.Role,
		CreatedAt:          time.Now(),
	}

	if err := h.iamStore.CreateMapping(r.Context(), m); err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			WriteError(w, http.StatusConflict, "mapping already exists")
			return
		}
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	WriteJSON(w, http.StatusCreated, m)
}

// ListMappings handles GET /api/v1/iam/providers/{id}/mappings.
func (h *IAMHandler) ListMappings(w http.ResponseWriter, r *http.Request) {
	providerID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid provider id")
		return
	}

	p, err := h.iamStore.GetProvider(r.Context(), providerID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "provider not found")
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

	if !h.permissions.CanAccess(r.Context(), user.ID, "company", p.CompanyID, store.RoleViewer) {
		WriteError(w, http.StatusForbidden, "forbidden")
		return
	}

	mappings, err := h.iamStore.ListMappings(r.Context(), store.IAMRoleMappingFilter{ProviderID: &providerID})
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if mappings == nil {
		mappings = []*store.IAMRoleMapping{}
	}
	WriteJSON(w, http.StatusOK, mappings)
}

// DeleteMapping handles DELETE /api/v1/iam/mappings/{id}.
func (h *IAMHandler) DeleteMapping(w http.ResponseWriter, r *http.Request) {
	mappingID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid mapping id")
		return
	}

	m, err := h.iamStore.GetMapping(r.Context(), mappingID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteError(w, http.StatusNotFound, "mapping not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Get provider to check permissions
	p, err := h.iamStore.GetProvider(r.Context(), m.ProviderID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	user := UserFromContext(r.Context())
	if user == nil {
		WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if !h.permissions.CanAccess(r.Context(), user.ID, "company", p.CompanyID, store.RoleAdmin) {
		WriteError(w, http.StatusForbidden, "forbidden")
		return
	}

	if err := h.iamStore.DeleteMapping(r.Context(), mappingID); err != nil {
		WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
