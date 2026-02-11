package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/GoCodeAlone/workflow/store"
	"github.com/google/uuid"
)

// --- mock company store ---

type mockCompanyStore struct {
	companies     map[uuid.UUID]*store.Company
	slugIndex     map[string]*store.Company
	userCompanies map[uuid.UUID][]*store.Company
}

func newMockCompanyStore() *mockCompanyStore {
	return &mockCompanyStore{
		companies:     make(map[uuid.UUID]*store.Company),
		slugIndex:     make(map[string]*store.Company),
		userCompanies: make(map[uuid.UUID][]*store.Company),
	}
}

func (m *mockCompanyStore) Create(_ context.Context, c *store.Company) error {
	if _, ok := m.slugIndex[c.Slug]; ok {
		return store.ErrDuplicate
	}
	m.companies[c.ID] = c
	m.slugIndex[c.Slug] = c
	return nil
}

func (m *mockCompanyStore) Get(_ context.Context, id uuid.UUID) (*store.Company, error) {
	c, ok := m.companies[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return c, nil
}

func (m *mockCompanyStore) GetBySlug(_ context.Context, slug string) (*store.Company, error) {
	c, ok := m.slugIndex[slug]
	if !ok {
		return nil, store.ErrNotFound
	}
	return c, nil
}

func (m *mockCompanyStore) Update(_ context.Context, c *store.Company) error {
	if _, ok := m.companies[c.ID]; !ok {
		return store.ErrNotFound
	}
	m.companies[c.ID] = c
	return nil
}

func (m *mockCompanyStore) Delete(_ context.Context, id uuid.UUID) error {
	if _, ok := m.companies[id]; !ok {
		return store.ErrNotFound
	}
	delete(m.companies, id)
	return nil
}

func (m *mockCompanyStore) List(_ context.Context, f store.CompanyFilter) ([]*store.Company, error) {
	var result []*store.Company
	for _, c := range m.companies {
		if f.OwnerID != nil && c.OwnerID != *f.OwnerID {
			continue
		}
		result = append(result, c)
	}
	return result, nil
}

func (m *mockCompanyStore) ListForUser(_ context.Context, userID uuid.UUID) ([]*store.Company, error) {
	return m.userCompanies[userID], nil
}

// --- helpers ---

func newTestCompanyHandler() (*CompanyHandler, *mockCompanyStore, *mockMembershipStore) {
	companies := newMockCompanyStore()
	memberships := &mockMembershipStore{}
	workflows := &mockWorkflowStore{workflows: make(map[uuid.UUID]*store.WorkflowRecord)}
	projects := &mockProjectStore{projects: make(map[uuid.UUID]*store.Project)}
	perms := NewPermissionService(memberships, workflows, projects)
	h := NewCompanyHandler(companies, memberships, perms)
	return h, companies, memberships
}

// --- tests ---

func TestCompanyHandler_Create_Success(t *testing.T) {
	h, _, _ := newTestCompanyHandler()
	user := &store.User{ID: uuid.New(), Email: "co@example.com", Active: true}

	body := makeJSON(map[string]string{
		"name": "Acme Corp",
		"slug": "acme-corp",
	})
	req := httptest.NewRequest("POST", "/api/v1/companies", body)
	req.Header.Set("Content-Type", "application/json")
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Create(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeBody(t, w.Result())
	data, _ := resp["data"].(map[string]interface{})
	if data["name"] != "Acme Corp" {
		t.Fatalf("expected name Acme Corp, got %v", data["name"])
	}
}

func TestCompanyHandler_Create_Unauthorized(t *testing.T) {
	h, _, _ := newTestCompanyHandler()

	body := makeJSON(map[string]string{"name": "Test"})
	req := httptest.NewRequest("POST", "/api/v1/companies", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Create(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestCompanyHandler_Create_DuplicateSlug(t *testing.T) {
	h, _, _ := newTestCompanyHandler()
	user := &store.User{ID: uuid.New(), Email: "co@example.com", Active: true}

	// Create first
	body := makeJSON(map[string]string{"name": "Test", "slug": "test"})
	req := httptest.NewRequest("POST", "/api/v1/companies", body)
	req.Header.Set("Content-Type", "application/json")
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Create(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("setup failed: %d", w.Code)
	}

	// Create duplicate
	body2 := makeJSON(map[string]string{"name": "Test2", "slug": "test"})
	req2 := httptest.NewRequest("POST", "/api/v1/companies", body2)
	req2.Header.Set("Content-Type", "application/json")
	ctx2 := SetUserContext(req2.Context(), user)
	req2 = req2.WithContext(ctx2)
	w2 := httptest.NewRecorder()
	h.Create(w2, req2)

	if w2.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w2.Code)
	}
}

func TestCompanyHandler_Create_AutoSlug(t *testing.T) {
	h, _, _ := newTestCompanyHandler()
	user := &store.User{ID: uuid.New(), Email: "co@example.com", Active: true}

	body := makeJSON(map[string]string{"name": "My Company Name"})
	req := httptest.NewRequest("POST", "/api/v1/companies", body)
	req.Header.Set("Content-Type", "application/json")
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Create(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeBody(t, w.Result())
	data, _ := resp["data"].(map[string]interface{})
	if data["slug"] != "my-company-name" {
		t.Fatalf("expected auto-slug my-company-name, got %v", data["slug"])
	}
}

func TestCompanyHandler_List_Success(t *testing.T) {
	h, companies, _ := newTestCompanyHandler()
	user := &store.User{ID: uuid.New(), Email: "co@example.com", Active: true}

	companies.userCompanies[user.ID] = []*store.Company{
		{ID: uuid.New(), Name: "Test Co"},
	}

	req := httptest.NewRequest("GET", "/api/v1/companies", nil)
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestCompanyHandler_Get_Success(t *testing.T) {
	h, companies, _ := newTestCompanyHandler()
	companyID := uuid.New()
	companies.companies[companyID] = &store.Company{ID: companyID, Name: "Found"}

	req := httptest.NewRequest("GET", "/api/v1/companies/"+companyID.String(), nil)
	req.SetPathValue("id", companyID.String())
	w := httptest.NewRecorder()
	h.Get(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestCompanyHandler_Get_NotFound(t *testing.T) {
	h, _, _ := newTestCompanyHandler()
	fakeID := uuid.New()

	req := httptest.NewRequest("GET", "/api/v1/companies/"+fakeID.String(), nil)
	req.SetPathValue("id", fakeID.String())
	w := httptest.NewRecorder()
	h.Get(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestCompanyHandler_Update_Success(t *testing.T) {
	h, companies, _ := newTestCompanyHandler()
	companyID := uuid.New()
	companies.companies[companyID] = &store.Company{ID: companyID, Name: "Old", Slug: "old"}

	newName := "New Name"
	body := makeJSON(map[string]*string{"name": &newName})
	req := httptest.NewRequest("PUT", "/api/v1/companies/"+companyID.String(), body)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", companyID.String())
	w := httptest.NewRecorder()
	h.Update(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCompanyHandler_Delete_Success(t *testing.T) {
	h, companies, _ := newTestCompanyHandler()
	companyID := uuid.New()
	companies.companies[companyID] = &store.Company{ID: companyID, Name: "Delete Me"}

	req := httptest.NewRequest("DELETE", "/api/v1/companies/"+companyID.String(), nil)
	req.SetPathValue("id", companyID.String())
	w := httptest.NewRecorder()
	h.Delete(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

func TestCompanyHandler_Delete_NotFound(t *testing.T) {
	h, _, _ := newTestCompanyHandler()
	fakeID := uuid.New()

	req := httptest.NewRequest("DELETE", "/api/v1/companies/"+fakeID.String(), nil)
	req.SetPathValue("id", fakeID.String())
	w := httptest.NewRecorder()
	h.Delete(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestCompanyHandler_AddMember_Success(t *testing.T) {
	h, _, _ := newTestCompanyHandler()
	companyID := uuid.New()
	memberUserID := uuid.New()

	body := makeJSON(map[string]string{
		"user_id": memberUserID.String(),
		"role":    "editor",
	})
	req := httptest.NewRequest("POST", "/api/v1/companies/"+companyID.String()+"/members", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", companyID.String())
	w := httptest.NewRecorder()
	h.AddMember(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCompanyHandler_ListMembers_Success(t *testing.T) {
	h, _, _ := newTestCompanyHandler()
	companyID := uuid.New()

	req := httptest.NewRequest("GET", "/api/v1/companies/"+companyID.String()+"/members", nil)
	req.SetPathValue("id", companyID.String())
	w := httptest.NewRecorder()
	h.ListMembers(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}
