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

// --- mock log store ---

type mockLogStore struct {
	logs   []*store.ExecutionLog
	nextID int64
}

func newMockLogStore() *mockLogStore {
	return &mockLogStore{}
}

func (m *mockLogStore) Append(_ context.Context, l *store.ExecutionLog) error {
	m.nextID++
	l.ID = m.nextID
	m.logs = append(m.logs, l)
	return nil
}

func (m *mockLogStore) Query(_ context.Context, f store.LogFilter) ([]*store.ExecutionLog, error) {
	var result []*store.ExecutionLog
	for _, l := range m.logs {
		if f.WorkflowID != nil && l.WorkflowID != *f.WorkflowID {
			continue
		}
		if f.Level != "" && l.Level != f.Level {
			continue
		}
		if f.ModuleName != "" && l.ModuleName != f.ModuleName {
			continue
		}
		if f.ExecutionID != nil && (l.ExecutionID == nil || *l.ExecutionID != *f.ExecutionID) {
			continue
		}
		result = append(result, l)
	}
	return result, nil
}

func (m *mockLogStore) CountByLevel(_ context.Context, workflowID uuid.UUID) (map[store.LogLevel]int, error) {
	counts := make(map[store.LogLevel]int)
	for _, l := range m.logs {
		if l.WorkflowID == workflowID {
			counts[l.Level]++
		}
	}
	return counts, nil
}

// --- mock project store for dashboard (with ListForUser) ---

type mockProjectStoreForDashboard struct {
	projects     map[uuid.UUID]*store.Project
	userProjects map[uuid.UUID][]*store.Project
}

func newMockProjectStoreForDashboard() *mockProjectStoreForDashboard {
	return &mockProjectStoreForDashboard{
		projects:     make(map[uuid.UUID]*store.Project),
		userProjects: make(map[uuid.UUID][]*store.Project),
	}
}

func (m *mockProjectStoreForDashboard) Create(_ context.Context, p *store.Project) error {
	m.projects[p.ID] = p
	return nil
}

func (m *mockProjectStoreForDashboard) Get(_ context.Context, id uuid.UUID) (*store.Project, error) {
	p, ok := m.projects[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return p, nil
}

func (m *mockProjectStoreForDashboard) GetBySlug(_ context.Context, _ uuid.UUID, _ string) (*store.Project, error) {
	return nil, store.ErrNotFound
}

func (m *mockProjectStoreForDashboard) Update(_ context.Context, p *store.Project) error {
	m.projects[p.ID] = p
	return nil
}

func (m *mockProjectStoreForDashboard) Delete(_ context.Context, id uuid.UUID) error {
	delete(m.projects, id)
	return nil
}

func (m *mockProjectStoreForDashboard) List(_ context.Context, _ store.ProjectFilter) ([]*store.Project, error) {
	var result []*store.Project
	for _, p := range m.projects {
		result = append(result, p)
	}
	return result, nil
}

func (m *mockProjectStoreForDashboard) ListForUser(_ context.Context, userID uuid.UUID) ([]*store.Project, error) {
	return m.userProjects[userID], nil
}

// --- helpers ---

func newTestDashboardHandler() (*DashboardHandler, *mockExecutionStore, *mockLogStore, *mockWorkflowStore, *mockProjectStoreForDashboard, *mockMembershipStore) {
	executions := newMockExecutionStore()
	logs := newMockLogStore()
	workflows := &mockWorkflowStore{workflows: make(map[uuid.UUID]*store.WorkflowRecord)}
	projects := newMockProjectStoreForDashboard()
	memberships := &mockMembershipStore{}
	perms := NewPermissionService(memberships, workflows, projects)
	h := NewDashboardHandler(executions, logs, workflows, projects, perms)
	return h, executions, logs, workflows, projects, memberships
}

// --- tests ---

func TestDashboardHandler_System_Success(t *testing.T) {
	h, _, _, _, _, _ := newTestDashboardHandler()
	user := &store.User{ID: uuid.New(), Email: "dash@example.com", Active: true}

	req := httptest.NewRequest("GET", "/api/v1/dashboard", nil)
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.System(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDashboardHandler_System_Unauthorized(t *testing.T) {
	h, _, _, _, _, _ := newTestDashboardHandler()

	req := httptest.NewRequest("GET", "/api/v1/dashboard", nil)
	w := httptest.NewRecorder()
	h.System(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestDashboardHandler_System_NoWorkflows(t *testing.T) {
	h, _, _, _, projects, _ := newTestDashboardHandler()
	user := &store.User{ID: uuid.New(), Email: "dash@example.com", Active: true}

	// User has a project, but no workflows
	proj := &store.Project{ID: uuid.New(), CompanyID: uuid.New(), Name: "Empty"}
	projects.projects[proj.ID] = proj
	projects.userProjects[user.ID] = []*store.Project{proj}

	req := httptest.NewRequest("GET", "/api/v1/dashboard", nil)
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.System(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := decodeBody(t, w.Result())
	data, _ := body["data"].(map[string]any)
	if data["total_workflows"] != float64(0) {
		t.Fatalf("expected 0 workflows, got %v", data["total_workflows"])
	}
}

func TestDashboardHandler_System_WithWorkflows(t *testing.T) {
	h, _, _, workflows, projects, _ := newTestDashboardHandler()
	user := &store.User{ID: uuid.New(), Email: "dash@example.com", Active: true}

	proj := &store.Project{ID: uuid.New(), CompanyID: uuid.New(), Name: "Test"}
	projects.projects[proj.ID] = proj
	projects.userProjects[user.ID] = []*store.Project{proj}

	wf := &store.WorkflowRecord{
		ID:        uuid.New(),
		ProjectID: proj.ID,
		Name:      "WF1",
		Status:    store.WorkflowStatusActive,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	workflows.workflows[wf.ID] = wf

	req := httptest.NewRequest("GET", "/api/v1/dashboard", nil)
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.System(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := decodeBody(t, w.Result())
	data, _ := body["data"].(map[string]any)
	if data["total_workflows"] != float64(1) {
		t.Fatalf("expected 1 workflow, got %v", data["total_workflows"])
	}
}

func TestDashboardHandler_Workflow_Success(t *testing.T) {
	h, _, _, workflows, projects, memberships := newTestDashboardHandler()
	user := &store.User{ID: uuid.New(), Email: "dash@example.com", Active: true}

	companyID := uuid.New()
	proj := &store.Project{ID: uuid.New(), CompanyID: companyID, Name: "Test"}
	projects.projects[proj.ID] = proj

	wf := &store.WorkflowRecord{
		ID:        uuid.New(),
		ProjectID: proj.ID,
		Name:      "WF1",
		Status:    store.WorkflowStatusActive,
		CreatedBy: uuid.New(),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	workflows.workflows[wf.ID] = wf

	_ = memberships.Create(context.Background(), &store.Membership{
		ID:        uuid.New(),
		UserID:    user.ID,
		CompanyID: companyID,
		ProjectID: &proj.ID,
		Role:      store.RoleViewer,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})

	req := httptest.NewRequest("GET", "/api/v1/workflows/"+wf.ID.String()+"/dashboard", nil)
	req.SetPathValue("id", wf.ID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Workflow(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDashboardHandler_Workflow_Unauthorized(t *testing.T) {
	h, _, _, _, _, _ := newTestDashboardHandler()
	wfID := uuid.New()

	req := httptest.NewRequest("GET", "/api/v1/workflows/"+wfID.String()+"/dashboard", nil)
	req.SetPathValue("id", wfID.String())
	w := httptest.NewRecorder()
	h.Workflow(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestDashboardHandler_Workflow_NotFound(t *testing.T) {
	h, _, _, workflows, projects, memberships := newTestDashboardHandler()
	user := &store.User{ID: uuid.New(), Email: "dash@example.com", Active: true}

	// Setup perms for a workflow, then ensure workflow doesn't exist for the dashboard fetch
	companyID := uuid.New()
	projectID := uuid.New()
	wfID := uuid.New()
	projects.projects[projectID] = &store.Project{ID: projectID, CompanyID: companyID}
	// Create the workflow record in store for permission check
	workflows.workflows[wfID] = &store.WorkflowRecord{
		ID:        wfID,
		ProjectID: projectID,
		CreatedBy: uuid.New(),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	_ = memberships.Create(context.Background(), &store.Membership{
		ID:        uuid.New(),
		UserID:    user.ID,
		CompanyID: companyID,
		ProjectID: &projectID,
		Role:      store.RoleViewer,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})

	// Now delete the workflow to simulate not found after permission check
	delete(workflows.workflows, wfID)

	req := httptest.NewRequest("GET", "/api/v1/workflows/"+wfID.String()+"/dashboard", nil)
	req.SetPathValue("id", wfID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Workflow(w, req)

	// CanAccess will fail because workflow doesn't exist anymore, returns 403
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestDashboardHandler_Workflow_Forbidden(t *testing.T) {
	h, _, _, workflows, projects, _ := newTestDashboardHandler()
	user := &store.User{ID: uuid.New(), Email: "dash@example.com", Active: true}

	companyID := uuid.New()
	proj := &store.Project{ID: uuid.New(), CompanyID: companyID}
	projects.projects[proj.ID] = proj

	wf := &store.WorkflowRecord{
		ID:        uuid.New(),
		ProjectID: proj.ID,
		Name:      "WF1",
		CreatedBy: uuid.New(),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	workflows.workflows[wf.ID] = wf
	// No membership for user

	req := httptest.NewRequest("GET", "/api/v1/workflows/"+wf.ID.String()+"/dashboard", nil)
	req.SetPathValue("id", wf.ID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Workflow(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestDashboardHandler_Workflow_WithExecutions(t *testing.T) {
	h, executions, _, workflows, projects, memberships := newTestDashboardHandler()
	user := &store.User{ID: uuid.New(), Email: "dash@example.com", Active: true}

	companyID := uuid.New()
	proj := &store.Project{ID: uuid.New(), CompanyID: companyID}
	projects.projects[proj.ID] = proj

	wf := &store.WorkflowRecord{
		ID:        uuid.New(),
		ProjectID: proj.ID,
		Name:      "WF1",
		Status:    store.WorkflowStatusActive,
		CreatedBy: uuid.New(),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	workflows.workflows[wf.ID] = wf

	_ = memberships.Create(context.Background(), &store.Membership{
		ID:        uuid.New(),
		UserID:    user.ID,
		CompanyID: companyID,
		ProjectID: &proj.ID,
		Role:      store.RoleViewer,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})

	executions.executions[uuid.New()] = &store.WorkflowExecution{
		ID:         uuid.New(),
		WorkflowID: wf.ID,
		Status:     store.ExecutionStatusCompleted,
		StartedAt:  time.Now(),
	}

	req := httptest.NewRequest("GET", "/api/v1/workflows/"+wf.ID.String()+"/dashboard", nil)
	req.SetPathValue("id", wf.ID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Workflow(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDashboardHandler_Workflow_EmptyState(t *testing.T) {
	h, _, _, workflows, projects, memberships := newTestDashboardHandler()
	user := &store.User{ID: uuid.New(), Email: "dash@example.com", Active: true}

	companyID := uuid.New()
	proj := &store.Project{ID: uuid.New(), CompanyID: companyID}
	projects.projects[proj.ID] = proj

	wf := &store.WorkflowRecord{
		ID:        uuid.New(),
		ProjectID: proj.ID,
		Name:      "WF1",
		Status:    store.WorkflowStatusDraft,
		CreatedBy: uuid.New(),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	workflows.workflows[wf.ID] = wf

	_ = memberships.Create(context.Background(), &store.Membership{
		ID:        uuid.New(),
		UserID:    user.ID,
		CompanyID: companyID,
		ProjectID: &proj.ID,
		Role:      store.RoleViewer,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})

	req := httptest.NewRequest("GET", "/api/v1/workflows/"+wf.ID.String()+"/dashboard", nil)
	req.SetPathValue("id", wf.ID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Workflow(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}
