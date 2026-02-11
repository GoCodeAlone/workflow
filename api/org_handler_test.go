package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/store"
	"github.com/google/uuid"
)

// --- helpers ---

func newTestOrgHandler() (*OrgHandler, *mockCompanyStore, *mockMembershipStore) {
	companies := newMockCompanyStore()
	memberships := &mockMembershipStore{}
	workflows := &mockWorkflowStore{workflows: make(map[uuid.UUID]*store.WorkflowRecord)}
	projects := &mockProjectStore{projects: make(map[uuid.UUID]*store.Project)}
	perms := NewPermissionService(memberships, workflows, projects)
	h := NewOrgHandler(companies, memberships, perms)
	return h, companies, memberships
}

// --- tests ---

func TestOrgHandler_Create_Success(t *testing.T) {
	h, companies, memberships := newTestOrgHandler()
	user := &store.User{ID: uuid.New(), Email: "org@example.com", Active: true}

	parentID := uuid.New()
	companies.companies[parentID] = &store.Company{ID: parentID, Name: "Parent Co"}

	// Give user editor permission on parent company
	_ = memberships.Create(context.Background(), &store.Membership{
		ID:        uuid.New(),
		UserID:    user.ID,
		CompanyID: parentID,
		Role:      store.RoleEditor,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})

	body := makeJSON(map[string]string{"name": "Engineering"})
	req := httptest.NewRequest("POST", "/api/v1/companies/"+parentID.String()+"/organizations", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("cid", parentID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Create(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestOrgHandler_Create_Unauthorized(t *testing.T) {
	h, companies, _ := newTestOrgHandler()
	parentID := uuid.New()
	companies.companies[parentID] = &store.Company{ID: parentID, Name: "Parent"}

	body := makeJSON(map[string]string{"name": "Org"})
	req := httptest.NewRequest("POST", "/api/v1/companies/"+parentID.String()+"/organizations", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("cid", parentID.String())
	w := httptest.NewRecorder()
	h.Create(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestOrgHandler_Create_ParentNotFound(t *testing.T) {
	h, _, _ := newTestOrgHandler()
	user := &store.User{ID: uuid.New(), Email: "org@example.com", Active: true}
	fakeID := uuid.New()

	body := makeJSON(map[string]string{"name": "Org"})
	req := httptest.NewRequest("POST", "/api/v1/companies/"+fakeID.String()+"/organizations", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("cid", fakeID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Create(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestOrgHandler_Create_Forbidden(t *testing.T) {
	h, companies, memberships := newTestOrgHandler()
	user := &store.User{ID: uuid.New(), Email: "org@example.com", Active: true}

	parentID := uuid.New()
	companies.companies[parentID] = &store.Company{ID: parentID, Name: "Parent"}

	// Only viewer (needs editor)
	_ = memberships.Create(context.Background(), &store.Membership{
		ID:        uuid.New(),
		UserID:    user.ID,
		CompanyID: parentID,
		Role:      store.RoleViewer,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})

	body := makeJSON(map[string]string{"name": "Org"})
	req := httptest.NewRequest("POST", "/api/v1/companies/"+parentID.String()+"/organizations", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("cid", parentID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Create(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestOrgHandler_List_Success(t *testing.T) {
	h, companies, _ := newTestOrgHandler()
	parentID := uuid.New()
	orgID := uuid.New()
	companies.companies[parentID] = &store.Company{ID: parentID, Name: "Parent"}
	companies.companies[orgID] = &store.Company{ID: orgID, Name: "Child Org", OwnerID: parentID}

	req := httptest.NewRequest("GET", "/api/v1/companies/"+parentID.String()+"/organizations", nil)
	req.SetPathValue("cid", parentID.String())
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestOrgHandler_Get_Success(t *testing.T) {
	h, companies, _ := newTestOrgHandler()
	orgID := uuid.New()
	companies.companies[orgID] = &store.Company{ID: orgID, Name: "Org"}

	req := httptest.NewRequest("GET", "/api/v1/organizations/"+orgID.String(), nil)
	req.SetPathValue("id", orgID.String())
	w := httptest.NewRecorder()
	h.Get(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestOrgHandler_Update_Success(t *testing.T) {
	h, companies, _ := newTestOrgHandler()
	orgID := uuid.New()
	companies.companies[orgID] = &store.Company{ID: orgID, Name: "Old Org", Slug: "old-org"}

	newName := "New Org"
	body := makeJSON(map[string]*string{"name": &newName})
	req := httptest.NewRequest("PUT", "/api/v1/organizations/"+orgID.String(), body)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", orgID.String())
	w := httptest.NewRecorder()
	h.Update(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestOrgHandler_Delete_Success(t *testing.T) {
	h, companies, _ := newTestOrgHandler()
	orgID := uuid.New()
	companies.companies[orgID] = &store.Company{ID: orgID, Name: "Delete Me"}

	req := httptest.NewRequest("DELETE", "/api/v1/organizations/"+orgID.String(), nil)
	req.SetPathValue("id", orgID.String())
	w := httptest.NewRecorder()
	h.Delete(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}
