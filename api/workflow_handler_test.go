package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/store"
	"github.com/google/uuid"
)

func newTestWorkflowHandler() (*WorkflowHandler, *mockWorkflowStore, *mockProjectStore, *mockMembershipStore) {
	workflows := &mockWorkflowStore{workflows: make(map[uuid.UUID]*store.WorkflowRecord)}
	projects := &mockProjectStore{projects: make(map[uuid.UUID]*store.Project)}
	memberships := &mockMembershipStore{}
	permissions := NewPermissionService(memberships, workflows, projects)
	h := NewWorkflowHandler(workflows, projects, memberships, permissions)
	return h, workflows, projects, memberships
}

func setupTestProject(t *testing.T, projects *mockProjectStore, memberships *mockMembershipStore, userID uuid.UUID) *store.Project {
	t.Helper()
	companyID := uuid.New()
	project := &store.Project{
		ID:        uuid.New(),
		CompanyID: companyID,
		Name:      "Test Project",
		Slug:      "test-project",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	_ = projects.Create(context.Background(), project)

	// Give user editor access via membership
	_ = memberships.Create(context.Background(), &store.Membership{
		ID:        uuid.New(),
		UserID:    userID,
		CompanyID: companyID,
		ProjectID: &project.ID,
		Role:      store.RoleOwner,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})

	return project
}

func TestWorkflowCreate(t *testing.T) {
	h, _, projects, memberships := newTestWorkflowHandler()

	user := &store.User{
		ID:     uuid.New(),
		Email:  "wf@example.com",
		Active: true,
	}
	project := setupTestProject(t, projects, memberships, user.ID)

	t.Run("success", func(t *testing.T) {
		body := makeJSON(map[string]string{
			"name":        "My Workflow",
			"description": "A test workflow",
			"config_yaml": "modules: []",
		})
		req := httptest.NewRequest("POST", "/api/v1/projects/"+project.ID.String()+"/workflows", body)
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("pid", project.ID.String())
		ctx := SetUserContext(req.Context(), user)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()
		h.Create(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var resp map[string]any
		_ = json.NewDecoder(w.Result().Body).Decode(&resp)
		data, _ := resp["data"].(map[string]any)
		if data["name"] != "My Workflow" {
			t.Fatalf("expected name 'My Workflow', got %v", data["name"])
		}
		if data["status"] != "draft" {
			t.Fatalf("expected status 'draft', got %v", data["status"])
		}
	})

	t.Run("missing name", func(t *testing.T) {
		body := makeJSON(map[string]string{"description": "no name"})
		req := httptest.NewRequest("POST", "/api/v1/projects/"+project.ID.String()+"/workflows", body)
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("pid", project.ID.String())
		ctx := SetUserContext(req.Context(), user)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()
		h.Create(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}
	})

	t.Run("project not found", func(t *testing.T) {
		body := makeJSON(map[string]string{"name": "WF"})
		fakeID := uuid.New().String()
		req := httptest.NewRequest("POST", "/api/v1/projects/"+fakeID+"/workflows", body)
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("pid", fakeID)
		ctx := SetUserContext(req.Context(), user)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()
		h.Create(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
	})
}

func TestWorkflowGet(t *testing.T) {
	h, workflows, _, _ := newTestWorkflowHandler()

	wf := &store.WorkflowRecord{
		ID:        uuid.New(),
		ProjectID: uuid.New(),
		Name:      "Test WF",
		Slug:      "test-wf",
		Status:    store.WorkflowStatusDraft,
		Version:   1,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	_ = workflows.Create(context.Background(), wf)

	user := &store.User{ID: uuid.New(), Email: "get@example.com", Active: true}

	t.Run("found", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/v1/workflows/"+wf.ID.String(), nil)
		req.SetPathValue("id", wf.ID.String())
		ctx := SetUserContext(req.Context(), user)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()
		h.Get(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("not found", func(t *testing.T) {
		fakeID := uuid.New().String()
		req := httptest.NewRequest("GET", "/api/v1/workflows/"+fakeID, nil)
		req.SetPathValue("id", fakeID)
		ctx := SetUserContext(req.Context(), user)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()
		h.Get(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", w.Code)
		}
	})
}

func TestWorkflowDeploy(t *testing.T) {
	h, workflows, _, _ := newTestWorkflowHandler()

	wf := &store.WorkflowRecord{
		ID:        uuid.New(),
		ProjectID: uuid.New(),
		Name:      "Deploy WF",
		Slug:      "deploy-wf",
		Status:    store.WorkflowStatusDraft,
		Version:   1,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	_ = workflows.Create(context.Background(), wf)

	user := &store.User{ID: uuid.New(), Email: "deploy@example.com", Active: true}

	req := httptest.NewRequest("POST", "/api/v1/workflows/"+wf.ID.String()+"/deploy", nil)
	req.SetPathValue("id", wf.ID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Deploy(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Check status was updated
	updated, _ := workflows.Get(context.Background(), wf.ID)
	if updated.Status != store.WorkflowStatusActive {
		t.Fatalf("expected status active, got %s", updated.Status)
	}
}

func TestWorkflowStop(t *testing.T) {
	h, workflows, _, _ := newTestWorkflowHandler()

	wf := &store.WorkflowRecord{
		ID:        uuid.New(),
		ProjectID: uuid.New(),
		Name:      "Stop WF",
		Slug:      "stop-wf",
		Status:    store.WorkflowStatusActive,
		Version:   1,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	_ = workflows.Create(context.Background(), wf)

	user := &store.User{ID: uuid.New(), Email: "stop@example.com", Active: true}

	req := httptest.NewRequest("POST", "/api/v1/workflows/"+wf.ID.String()+"/stop", nil)
	req.SetPathValue("id", wf.ID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Stop(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	updated, _ := workflows.Get(context.Background(), wf.ID)
	if updated.Status != store.WorkflowStatusStopped {
		t.Fatalf("expected status stopped, got %s", updated.Status)
	}
}

func TestWorkflowUpdate(t *testing.T) {
	h, workflows, projects, memberships := newTestWorkflowHandler()

	user := &store.User{ID: uuid.New(), Email: "upd@example.com", Active: true}
	project := setupTestProject(t, projects, memberships, user.ID)

	wf := &store.WorkflowRecord{
		ID:         uuid.New(),
		ProjectID:  project.ID,
		Name:       "Original",
		Slug:       "original",
		ConfigYAML: "modules: []",
		Status:     store.WorkflowStatusDraft,
		Version:    1,
		CreatedBy:  user.ID,
		UpdatedBy:  user.ID,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	_ = workflows.Create(context.Background(), wf)

	newName := "Updated"
	newConfig := "modules: [http]"
	body := makeJSON(map[string]*string{
		"name":        &newName,
		"config_yaml": &newConfig,
	})
	req := httptest.NewRequest("PUT", "/api/v1/workflows/"+wf.ID.String(), body)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", wf.ID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Update(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	updated, _ := workflows.Get(context.Background(), wf.ID)
	if updated.Name != "Updated" {
		t.Fatalf("expected name Updated, got %s", updated.Name)
	}
	if updated.Version != 2 {
		t.Fatalf("expected version 2, got %d", updated.Version)
	}
}

func TestWorkflowDelete(t *testing.T) {
	h, workflows, _, _ := newTestWorkflowHandler()

	wf := &store.WorkflowRecord{
		ID:        uuid.New(),
		ProjectID: uuid.New(),
		Name:      "Delete WF",
		Slug:      "delete-wf",
		Status:    store.WorkflowStatusDraft,
		Version:   1,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	_ = workflows.Create(context.Background(), wf)

	user := &store.User{ID: uuid.New(), Email: "del@example.com", Active: true}

	req := httptest.NewRequest("DELETE", "/api/v1/workflows/"+wf.ID.String(), nil)
	req.SetPathValue("id", wf.ID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Delete(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}

	_, err := workflows.Get(context.Background(), wf.ID)
	if err == nil {
		t.Fatal("expected workflow to be deleted")
	}
}

func TestWorkflowStatus(t *testing.T) {
	h, workflows, _, _ := newTestWorkflowHandler()

	wf := &store.WorkflowRecord{
		ID:        uuid.New(),
		ProjectID: uuid.New(),
		Name:      "Status WF",
		Slug:      "status-wf",
		Status:    store.WorkflowStatusActive,
		Version:   3,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	_ = workflows.Create(context.Background(), wf)

	user := &store.User{ID: uuid.New(), Email: "status@example.com", Active: true}

	req := httptest.NewRequest("GET", "/api/v1/workflows/"+wf.ID.String()+"/status", nil)
	req.SetPathValue("id", wf.ID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Status(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	_ = json.NewDecoder(w.Result().Body).Decode(&resp)
	data, _ := resp["data"].(map[string]any)
	if data["status"] != "active" {
		t.Fatalf("expected status active, got %v", data["status"])
	}
	if data["version"] != float64(3) {
		t.Fatalf("expected version 3, got %v", data["version"])
	}
}
