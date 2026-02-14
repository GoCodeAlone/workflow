package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/iam"
	"github.com/GoCodeAlone/workflow/store"
	"github.com/google/uuid"
)

// --- mock IAM store ---

type mockIAMStore struct {
	providers map[uuid.UUID]*store.IAMProviderConfig
	mappings  map[uuid.UUID]*store.IAMRoleMapping
}

func newMockIAMStore() *mockIAMStore {
	return &mockIAMStore{
		providers: make(map[uuid.UUID]*store.IAMProviderConfig),
		mappings:  make(map[uuid.UUID]*store.IAMRoleMapping),
	}
}

func (m *mockIAMStore) CreateProvider(_ context.Context, p *store.IAMProviderConfig) error {
	for _, existing := range m.providers {
		if existing.CompanyID == p.CompanyID && existing.Name == p.Name {
			return store.ErrDuplicate
		}
	}
	m.providers[p.ID] = p
	return nil
}

func (m *mockIAMStore) GetProvider(_ context.Context, id uuid.UUID) (*store.IAMProviderConfig, error) {
	p, ok := m.providers[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return p, nil
}

func (m *mockIAMStore) UpdateProvider(_ context.Context, p *store.IAMProviderConfig) error {
	if _, ok := m.providers[p.ID]; !ok {
		return store.ErrNotFound
	}
	m.providers[p.ID] = p
	return nil
}

func (m *mockIAMStore) DeleteProvider(_ context.Context, id uuid.UUID) error {
	if _, ok := m.providers[id]; !ok {
		return store.ErrNotFound
	}
	delete(m.providers, id)
	return nil
}

func (m *mockIAMStore) ListProviders(_ context.Context, f store.IAMProviderFilter) ([]*store.IAMProviderConfig, error) {
	var result []*store.IAMProviderConfig
	for _, p := range m.providers {
		if f.CompanyID != nil && p.CompanyID != *f.CompanyID {
			continue
		}
		result = append(result, p)
	}
	return result, nil
}

func (m *mockIAMStore) CreateMapping(_ context.Context, mapping *store.IAMRoleMapping) error {
	for _, existing := range m.mappings {
		if existing.ProviderID == mapping.ProviderID &&
			existing.ExternalIdentifier == mapping.ExternalIdentifier &&
			existing.ResourceType == mapping.ResourceType &&
			existing.ResourceID == mapping.ResourceID {
			return store.ErrDuplicate
		}
	}
	m.mappings[mapping.ID] = mapping
	return nil
}

func (m *mockIAMStore) GetMapping(_ context.Context, id uuid.UUID) (*store.IAMRoleMapping, error) {
	mapping, ok := m.mappings[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return mapping, nil
}

func (m *mockIAMStore) DeleteMapping(_ context.Context, id uuid.UUID) error {
	if _, ok := m.mappings[id]; !ok {
		return store.ErrNotFound
	}
	delete(m.mappings, id)
	return nil
}

func (m *mockIAMStore) ListMappings(_ context.Context, f store.IAMRoleMappingFilter) ([]*store.IAMRoleMapping, error) {
	var result []*store.IAMRoleMapping
	for _, mapping := range m.mappings {
		if f.ProviderID != nil && mapping.ProviderID != *f.ProviderID {
			continue
		}
		result = append(result, mapping)
	}
	return result, nil
}

func (m *mockIAMStore) ResolveRole(_ context.Context, _ uuid.UUID, _ string, _ string, _ uuid.UUID) (store.Role, error) {
	return "", store.ErrNotFound
}

// --- helpers ---

func newTestIAMHandler() (*IAMHandler, *mockIAMStore, *mockMembershipStore, *mockWorkflowStore, *mockProjectStore) {
	iamStore := newMockIAMStore()
	memberships := &mockMembershipStore{}
	workflows := &mockWorkflowStore{workflows: make(map[uuid.UUID]*store.WorkflowRecord)}
	projects := &mockProjectStore{projects: make(map[uuid.UUID]*store.Project)}
	perms := NewPermissionService(memberships, workflows, projects)
	resolver := iam.NewIAMResolver(iamStore)
	h := NewIAMHandler(iamStore, resolver, perms)
	return h, iamStore, memberships, workflows, projects
}

func setupCompanyWithPerms(t *testing.T, memberships *mockMembershipStore, userID uuid.UUID, role store.Role) uuid.UUID {
	t.Helper()
	companyID := uuid.New()
	_ = memberships.Create(context.Background(), &store.Membership{
		ID:        uuid.New(),
		UserID:    userID,
		CompanyID: companyID,
		Role:      role,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})
	return companyID
}

// --- tests ---

func TestIAMHandler_CreateProvider_Success(t *testing.T) {
	h, _, memberships, _, _ := newTestIAMHandler()
	user := &store.User{ID: uuid.New(), Email: "iam@example.com", Active: true}
	companyID := setupCompanyWithPerms(t, memberships, user.ID, store.RoleAdmin)

	body := makeJSON(map[string]any{
		"provider_type": "custom",
		"name":          "My Provider",
		"config":        map[string]string{"url": "https://example.com"},
	})
	req := httptest.NewRequest("POST", "/api/v1/companies/"+companyID.String()+"/iam/providers", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", companyID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.CreateProvider(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestIAMHandler_CreateProvider_Unauthorized(t *testing.T) {
	h, _, _, _, _ := newTestIAMHandler()
	companyID := uuid.New()

	body := makeJSON(map[string]any{
		"provider_type": "custom", "name": "Provider",
		"config": map[string]string{},
	})
	req := httptest.NewRequest("POST", "/api/v1/companies/"+companyID.String()+"/iam/providers", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", companyID.String())
	w := httptest.NewRecorder()
	h.CreateProvider(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestIAMHandler_CreateProvider_Forbidden(t *testing.T) {
	h, _, memberships, _, _ := newTestIAMHandler()
	user := &store.User{ID: uuid.New(), Email: "iam@example.com", Active: true}
	companyID := setupCompanyWithPerms(t, memberships, user.ID, store.RoleViewer) // Not admin

	body := makeJSON(map[string]any{
		"provider_type": "custom", "name": "Provider",
		"config": map[string]string{},
	})
	req := httptest.NewRequest("POST", "/api/v1/companies/"+companyID.String()+"/iam/providers", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", companyID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.CreateProvider(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestIAMHandler_CreateProvider_MissingName(t *testing.T) {
	h, _, memberships, _, _ := newTestIAMHandler()
	user := &store.User{ID: uuid.New(), Email: "iam@example.com", Active: true}
	companyID := setupCompanyWithPerms(t, memberships, user.ID, store.RoleAdmin)

	body := makeJSON(map[string]any{
		"provider_type": "custom",
		"config":        map[string]string{},
	})
	req := httptest.NewRequest("POST", "/api/v1/companies/"+companyID.String()+"/iam/providers", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", companyID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.CreateProvider(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestIAMHandler_CreateProvider_MissingType(t *testing.T) {
	h, _, memberships, _, _ := newTestIAMHandler()
	user := &store.User{ID: uuid.New(), Email: "iam@example.com", Active: true}
	companyID := setupCompanyWithPerms(t, memberships, user.ID, store.RoleAdmin)

	body := makeJSON(map[string]any{
		"name":   "Provider",
		"config": map[string]string{},
	})
	req := httptest.NewRequest("POST", "/api/v1/companies/"+companyID.String()+"/iam/providers", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", companyID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.CreateProvider(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestIAMHandler_ListProviders_Success(t *testing.T) {
	h, iamStore, memberships, _, _ := newTestIAMHandler()
	user := &store.User{ID: uuid.New(), Email: "iam@example.com", Active: true}
	companyID := setupCompanyWithPerms(t, memberships, user.ID, store.RoleViewer)

	iamStore.providers[uuid.New()] = &store.IAMProviderConfig{
		ID:           uuid.New(),
		CompanyID:    companyID,
		ProviderType: "custom",
		Name:         "Test",
		Enabled:      true,
	}

	req := httptest.NewRequest("GET", "/api/v1/companies/"+companyID.String()+"/iam/providers", nil)
	req.SetPathValue("id", companyID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.ListProviders(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestIAMHandler_ListProviders_Forbidden(t *testing.T) {
	h, _, _, _, _ := newTestIAMHandler()
	user := &store.User{ID: uuid.New(), Email: "iam@example.com", Active: true}
	companyID := uuid.New() // No membership

	req := httptest.NewRequest("GET", "/api/v1/companies/"+companyID.String()+"/iam/providers", nil)
	req.SetPathValue("id", companyID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.ListProviders(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestIAMHandler_GetProvider_Success(t *testing.T) {
	h, iamStore, memberships, _, _ := newTestIAMHandler()
	user := &store.User{ID: uuid.New(), Email: "iam@example.com", Active: true}
	companyID := setupCompanyWithPerms(t, memberships, user.ID, store.RoleViewer)

	providerID := uuid.New()
	iamStore.providers[providerID] = &store.IAMProviderConfig{
		ID:        providerID,
		CompanyID: companyID,
		Name:      "Provider",
	}

	req := httptest.NewRequest("GET", "/api/v1/iam/providers/"+providerID.String(), nil)
	req.SetPathValue("id", providerID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.GetProvider(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestIAMHandler_GetProvider_NotFound(t *testing.T) {
	h, _, _, _, _ := newTestIAMHandler()
	fakeID := uuid.New()

	req := httptest.NewRequest("GET", "/api/v1/iam/providers/"+fakeID.String(), nil)
	req.SetPathValue("id", fakeID.String())
	w := httptest.NewRecorder()
	h.GetProvider(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestIAMHandler_UpdateProvider_Success(t *testing.T) {
	h, iamStore, memberships, _, _ := newTestIAMHandler()
	user := &store.User{ID: uuid.New(), Email: "iam@example.com", Active: true}
	companyID := setupCompanyWithPerms(t, memberships, user.ID, store.RoleAdmin)

	providerID := uuid.New()
	iamStore.providers[providerID] = &store.IAMProviderConfig{
		ID:           providerID,
		CompanyID:    companyID,
		ProviderType: "custom",
		Name:         "Old Name",
		Enabled:      true,
	}

	newName := "New Name"
	body := makeJSON(map[string]*string{"name": &newName})
	req := httptest.NewRequest("PUT", "/api/v1/iam/providers/"+providerID.String(), body)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", providerID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.UpdateProvider(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestIAMHandler_UpdateProvider_NotFound(t *testing.T) {
	h, _, _, _, _ := newTestIAMHandler()
	user := &store.User{ID: uuid.New(), Email: "iam@example.com", Active: true}
	fakeID := uuid.New()

	body := makeJSON(map[string]any{"name": "x"})
	req := httptest.NewRequest("PUT", "/api/v1/iam/providers/"+fakeID.String(), body)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", fakeID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.UpdateProvider(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestIAMHandler_UpdateProvider_Forbidden(t *testing.T) {
	h, iamStore, memberships, _, _ := newTestIAMHandler()
	user := &store.User{ID: uuid.New(), Email: "iam@example.com", Active: true}
	companyID := setupCompanyWithPerms(t, memberships, user.ID, store.RoleViewer) // Not admin

	providerID := uuid.New()
	iamStore.providers[providerID] = &store.IAMProviderConfig{
		ID:        providerID,
		CompanyID: companyID,
		Name:      "Provider",
	}

	body := makeJSON(map[string]any{"name": "New"})
	req := httptest.NewRequest("PUT", "/api/v1/iam/providers/"+providerID.String(), body)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", providerID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.UpdateProvider(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestIAMHandler_DeleteProvider_Success(t *testing.T) {
	h, iamStore, memberships, _, _ := newTestIAMHandler()
	user := &store.User{ID: uuid.New(), Email: "iam@example.com", Active: true}
	companyID := setupCompanyWithPerms(t, memberships, user.ID, store.RoleAdmin)

	providerID := uuid.New()
	iamStore.providers[providerID] = &store.IAMProviderConfig{
		ID:        providerID,
		CompanyID: companyID,
		Name:      "Delete Me",
	}

	req := httptest.NewRequest("DELETE", "/api/v1/iam/providers/"+providerID.String(), nil)
	req.SetPathValue("id", providerID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.DeleteProvider(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

func TestIAMHandler_DeleteProvider_NotFound(t *testing.T) {
	h, _, _, _, _ := newTestIAMHandler()
	fakeID := uuid.New()

	req := httptest.NewRequest("DELETE", "/api/v1/iam/providers/"+fakeID.String(), nil)
	req.SetPathValue("id", fakeID.String())
	w := httptest.NewRecorder()
	h.DeleteProvider(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestIAMHandler_TestConnection_Success(t *testing.T) {
	h, iamStore, memberships, _, _ := newTestIAMHandler()
	user := &store.User{ID: uuid.New(), Email: "iam@example.com", Active: true}
	companyID := setupCompanyWithPerms(t, memberships, user.ID, store.RoleAdmin)

	providerID := uuid.New()
	iamStore.providers[providerID] = &store.IAMProviderConfig{
		ID:           providerID,
		CompanyID:    companyID,
		ProviderType: "custom", // No registered provider for "custom"
		Name:         "Custom Provider",
	}

	req := httptest.NewRequest("POST", "/api/v1/iam/providers/"+providerID.String()+"/test", nil)
	req.SetPathValue("id", providerID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.TestConnection(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := decodeBody(t, w.Result())
	data, _ := body["data"].(map[string]any)
	// "custom" type has no registered provider, so success=false
	if data["success"] != false {
		t.Fatalf("expected success=false for unregistered provider type, got %v", data["success"])
	}
}

func TestIAMHandler_TestConnection_NoProvider(t *testing.T) {
	h, _, _, _, _ := newTestIAMHandler()
	user := &store.User{ID: uuid.New(), Email: "iam@example.com", Active: true}
	fakeID := uuid.New()

	req := httptest.NewRequest("POST", "/api/v1/iam/providers/"+fakeID.String()+"/test", nil)
	req.SetPathValue("id", fakeID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.TestConnection(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestIAMHandler_CreateMapping_Success(t *testing.T) {
	h, iamStore, memberships, _, _ := newTestIAMHandler()
	user := &store.User{ID: uuid.New(), Email: "iam@example.com", Active: true}
	companyID := setupCompanyWithPerms(t, memberships, user.ID, store.RoleAdmin)

	providerID := uuid.New()
	iamStore.providers[providerID] = &store.IAMProviderConfig{
		ID:        providerID,
		CompanyID: companyID,
		Name:      "Provider",
	}

	resourceID := uuid.New()
	body := makeJSON(map[string]any{
		"external_identifier": "group:engineers",
		"resource_type":       "company",
		"resource_id":         resourceID.String(),
		"role":                "editor",
	})
	req := httptest.NewRequest("POST", "/api/v1/iam/providers/"+providerID.String()+"/mappings", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", providerID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.CreateMapping(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestIAMHandler_CreateMapping_MissingFields(t *testing.T) {
	h, iamStore, memberships, _, _ := newTestIAMHandler()
	user := &store.User{ID: uuid.New(), Email: "iam@example.com", Active: true}
	companyID := setupCompanyWithPerms(t, memberships, user.ID, store.RoleAdmin)

	providerID := uuid.New()
	iamStore.providers[providerID] = &store.IAMProviderConfig{
		ID:        providerID,
		CompanyID: companyID,
		Name:      "Provider",
	}

	// Missing external_identifier
	body := makeJSON(map[string]any{
		"resource_type": "company",
		"resource_id":   uuid.New().String(),
		"role":          "editor",
	})
	req := httptest.NewRequest("POST", "/api/v1/iam/providers/"+providerID.String()+"/mappings", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", providerID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.CreateMapping(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestIAMHandler_ListMappings_Success(t *testing.T) {
	h, iamStore, memberships, _, _ := newTestIAMHandler()
	user := &store.User{ID: uuid.New(), Email: "iam@example.com", Active: true}
	companyID := setupCompanyWithPerms(t, memberships, user.ID, store.RoleViewer)

	providerID := uuid.New()
	iamStore.providers[providerID] = &store.IAMProviderConfig{
		ID:        providerID,
		CompanyID: companyID,
		Name:      "Provider",
	}

	mappingID := uuid.New()
	iamStore.mappings[mappingID] = &store.IAMRoleMapping{
		ID:                 mappingID,
		ProviderID:         providerID,
		ExternalIdentifier: "group:admins",
		ResourceType:       "company",
		ResourceID:         uuid.New(),
		Role:               store.RoleAdmin,
	}

	req := httptest.NewRequest("GET", "/api/v1/iam/providers/"+providerID.String()+"/mappings", nil)
	req.SetPathValue("id", providerID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.ListMappings(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestIAMHandler_DeleteMapping_Success(t *testing.T) {
	h, iamStore, memberships, _, _ := newTestIAMHandler()
	user := &store.User{ID: uuid.New(), Email: "iam@example.com", Active: true}
	companyID := setupCompanyWithPerms(t, memberships, user.ID, store.RoleAdmin)

	providerID := uuid.New()
	iamStore.providers[providerID] = &store.IAMProviderConfig{
		ID:        providerID,
		CompanyID: companyID,
		Name:      "Provider",
	}

	mappingID := uuid.New()
	iamStore.mappings[mappingID] = &store.IAMRoleMapping{
		ID:                 mappingID,
		ProviderID:         providerID,
		ExternalIdentifier: "group:admins",
		ResourceType:       "company",
		ResourceID:         uuid.New(),
		Role:               store.RoleAdmin,
	}

	req := httptest.NewRequest("DELETE", "/api/v1/iam/mappings/"+mappingID.String(), nil)
	req.SetPathValue("id", mappingID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.DeleteMapping(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}
}
