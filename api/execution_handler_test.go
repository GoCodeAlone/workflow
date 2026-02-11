package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/store"
	"github.com/google/uuid"
)

// --- mock execution store ---

type mockExecutionStore struct {
	executions map[uuid.UUID]*store.WorkflowExecution
	steps      map[uuid.UUID][]*store.ExecutionStep
}

func newMockExecutionStore() *mockExecutionStore {
	return &mockExecutionStore{
		executions: make(map[uuid.UUID]*store.WorkflowExecution),
		steps:      make(map[uuid.UUID][]*store.ExecutionStep),
	}
}

func (m *mockExecutionStore) CreateExecution(_ context.Context, e *store.WorkflowExecution) error {
	m.executions[e.ID] = e
	return nil
}

func (m *mockExecutionStore) GetExecution(_ context.Context, id uuid.UUID) (*store.WorkflowExecution, error) {
	e, ok := m.executions[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return e, nil
}

func (m *mockExecutionStore) UpdateExecution(_ context.Context, e *store.WorkflowExecution) error {
	if _, ok := m.executions[e.ID]; !ok {
		return store.ErrNotFound
	}
	m.executions[e.ID] = e
	return nil
}

func (m *mockExecutionStore) ListExecutions(_ context.Context, f store.ExecutionFilter) ([]*store.WorkflowExecution, error) {
	var result []*store.WorkflowExecution
	for _, e := range m.executions {
		if f.WorkflowID != nil && e.WorkflowID != *f.WorkflowID {
			continue
		}
		if f.Status != "" && e.Status != f.Status {
			continue
		}
		result = append(result, e)
	}
	return result, nil
}

func (m *mockExecutionStore) CreateStep(_ context.Context, s *store.ExecutionStep) error {
	m.steps[s.ExecutionID] = append(m.steps[s.ExecutionID], s)
	return nil
}

func (m *mockExecutionStore) UpdateStep(_ context.Context, s *store.ExecutionStep) error {
	return nil
}

func (m *mockExecutionStore) ListSteps(_ context.Context, executionID uuid.UUID) ([]*store.ExecutionStep, error) {
	return m.steps[executionID], nil
}

func (m *mockExecutionStore) CountByStatus(_ context.Context, workflowID uuid.UUID) (map[store.ExecutionStatus]int, error) {
	counts := make(map[store.ExecutionStatus]int)
	for _, e := range m.executions {
		if e.WorkflowID == workflowID {
			counts[e.Status]++
		}
	}
	return counts, nil
}

// --- helpers ---

func newTestExecutionHandler() (*ExecutionHandler, *mockExecutionStore, *mockWorkflowStore, *mockMembershipStore, *mockProjectStore) {
	executions := newMockExecutionStore()
	workflows := &mockWorkflowStore{workflows: make(map[uuid.UUID]*store.WorkflowRecord)}
	memberships := &mockMembershipStore{}
	projects := &mockProjectStore{projects: make(map[uuid.UUID]*store.Project)}
	perms := NewPermissionService(memberships, workflows, projects)
	h := NewExecutionHandler(executions, workflows, perms)
	return h, executions, workflows, memberships, projects
}

func setupWorkflowWithPerms(t *testing.T, workflows *mockWorkflowStore, projects *mockProjectStore, memberships *mockMembershipStore, userID uuid.UUID, role store.Role) *store.WorkflowRecord {
	t.Helper()
	companyID := uuid.New()
	projectID := uuid.New()
	projects.projects[projectID] = &store.Project{ID: projectID, CompanyID: companyID}
	wf := &store.WorkflowRecord{
		ID:        uuid.New(),
		ProjectID: projectID,
		Name:      "Test WF",
		Status:    store.WorkflowStatusActive,
		Version:   1,
		CreatedBy: uuid.New(), // Different from userID
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	workflows.workflows[wf.ID] = wf

	_ = memberships.Create(context.Background(), &store.Membership{
		ID:        uuid.New(),
		UserID:    userID,
		CompanyID: companyID,
		ProjectID: &projectID,
		Role:      role,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})

	return wf
}

// --- tests ---

func TestExecutionHandler_List_Success(t *testing.T) {
	h, executions, workflows, memberships, projects := newTestExecutionHandler()
	user := &store.User{ID: uuid.New(), Email: "exec@example.com", Active: true}
	wf := setupWorkflowWithPerms(t, workflows, projects, memberships, user.ID, store.RoleViewer)

	executions.executions[uuid.New()] = &store.WorkflowExecution{
		ID:         uuid.New(),
		WorkflowID: wf.ID,
		Status:     store.ExecutionStatusCompleted,
		StartedAt:  time.Now(),
	}

	req := httptest.NewRequest("GET", "/api/v1/workflows/"+wf.ID.String()+"/executions", nil)
	req.SetPathValue("id", wf.ID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestExecutionHandler_List_WithStatusFilter(t *testing.T) {
	h, executions, workflows, memberships, projects := newTestExecutionHandler()
	user := &store.User{ID: uuid.New(), Email: "exec@example.com", Active: true}
	wf := setupWorkflowWithPerms(t, workflows, projects, memberships, user.ID, store.RoleViewer)

	executions.executions[uuid.New()] = &store.WorkflowExecution{
		ID:         uuid.New(),
		WorkflowID: wf.ID,
		Status:     store.ExecutionStatusCompleted,
		StartedAt:  time.Now(),
	}
	executions.executions[uuid.New()] = &store.WorkflowExecution{
		ID:         uuid.New(),
		WorkflowID: wf.ID,
		Status:     store.ExecutionStatusRunning,
		StartedAt:  time.Now(),
	}

	req := httptest.NewRequest("GET", "/api/v1/workflows/"+wf.ID.String()+"/executions?status=running", nil)
	req.SetPathValue("id", wf.ID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestExecutionHandler_List_Unauthorized(t *testing.T) {
	h, _, _, _, _ := newTestExecutionHandler()
	wfID := uuid.New()

	req := httptest.NewRequest("GET", "/api/v1/workflows/"+wfID.String()+"/executions", nil)
	req.SetPathValue("id", wfID.String())
	// No user context
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestExecutionHandler_List_Forbidden(t *testing.T) {
	h, _, workflows, _, projects := newTestExecutionHandler()
	user := &store.User{ID: uuid.New(), Email: "exec@example.com", Active: true}

	// Create workflow but no membership for this user
	companyID := uuid.New()
	projectID := uuid.New()
	projects.projects[projectID] = &store.Project{ID: projectID, CompanyID: companyID}
	wf := &store.WorkflowRecord{
		ID:        uuid.New(),
		ProjectID: projectID,
		CreatedBy: uuid.New(),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	workflows.workflows[wf.ID] = wf

	req := httptest.NewRequest("GET", "/api/v1/workflows/"+wf.ID.String()+"/executions", nil)
	req.SetPathValue("id", wf.ID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestExecutionHandler_Get_Success(t *testing.T) {
	h, executions, workflows, memberships, projects := newTestExecutionHandler()
	user := &store.User{ID: uuid.New(), Email: "exec@example.com", Active: true}
	wf := setupWorkflowWithPerms(t, workflows, projects, memberships, user.ID, store.RoleViewer)

	execID := uuid.New()
	executions.executions[execID] = &store.WorkflowExecution{
		ID:         execID,
		WorkflowID: wf.ID,
		Status:     store.ExecutionStatusCompleted,
		StartedAt:  time.Now(),
	}

	req := httptest.NewRequest("GET", "/api/v1/executions/"+execID.String(), nil)
	req.SetPathValue("id", execID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Get(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestExecutionHandler_Get_NotFound(t *testing.T) {
	h, _, _, _, _ := newTestExecutionHandler()
	user := &store.User{ID: uuid.New(), Email: "exec@example.com", Active: true}
	execID := uuid.New()

	req := httptest.NewRequest("GET", "/api/v1/executions/"+execID.String(), nil)
	req.SetPathValue("id", execID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Get(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestExecutionHandler_Get_Forbidden(t *testing.T) {
	h, executions, workflows, _, projects := newTestExecutionHandler()
	user := &store.User{ID: uuid.New(), Email: "exec@example.com", Active: true}

	companyID := uuid.New()
	projectID := uuid.New()
	projects.projects[projectID] = &store.Project{ID: projectID, CompanyID: companyID}
	wf := &store.WorkflowRecord{
		ID:        uuid.New(),
		ProjectID: projectID,
		CreatedBy: uuid.New(),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	workflows.workflows[wf.ID] = wf

	execID := uuid.New()
	executions.executions[execID] = &store.WorkflowExecution{
		ID:         execID,
		WorkflowID: wf.ID,
		Status:     store.ExecutionStatusCompleted,
		StartedAt:  time.Now(),
	}

	req := httptest.NewRequest("GET", "/api/v1/executions/"+execID.String(), nil)
	req.SetPathValue("id", execID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Get(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestExecutionHandler_Steps_Success(t *testing.T) {
	h, executions, workflows, memberships, projects := newTestExecutionHandler()
	user := &store.User{ID: uuid.New(), Email: "exec@example.com", Active: true}
	wf := setupWorkflowWithPerms(t, workflows, projects, memberships, user.ID, store.RoleViewer)

	execID := uuid.New()
	executions.executions[execID] = &store.WorkflowExecution{
		ID:         execID,
		WorkflowID: wf.ID,
		Status:     store.ExecutionStatusCompleted,
		StartedAt:  time.Now(),
	}
	executions.steps[execID] = []*store.ExecutionStep{
		{ID: uuid.New(), ExecutionID: execID, StepName: "step1", Status: store.StepStatusCompleted},
	}

	req := httptest.NewRequest("GET", "/api/v1/executions/"+execID.String()+"/steps", nil)
	req.SetPathValue("id", execID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Steps(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestExecutionHandler_Steps_NotFound(t *testing.T) {
	h, _, _, _, _ := newTestExecutionHandler()
	user := &store.User{ID: uuid.New(), Email: "exec@example.com", Active: true}
	execID := uuid.New()

	req := httptest.NewRequest("GET", "/api/v1/executions/"+execID.String()+"/steps", nil)
	req.SetPathValue("id", execID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Steps(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestExecutionHandler_Cancel_Success(t *testing.T) {
	h, executions, workflows, memberships, projects := newTestExecutionHandler()
	user := &store.User{ID: uuid.New(), Email: "exec@example.com", Active: true}
	wf := setupWorkflowWithPerms(t, workflows, projects, memberships, user.ID, store.RoleAdmin)

	execID := uuid.New()
	executions.executions[execID] = &store.WorkflowExecution{
		ID:         execID,
		WorkflowID: wf.ID,
		Status:     store.ExecutionStatusRunning,
		StartedAt:  time.Now(),
	}

	req := httptest.NewRequest("POST", "/api/v1/executions/"+execID.String()+"/cancel", nil)
	req.SetPathValue("id", execID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Cancel(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	updated := executions.executions[execID]
	if updated.Status != store.ExecutionStatusCancelled {
		t.Fatalf("expected status cancelled, got %s", updated.Status)
	}
}

func TestExecutionHandler_Cancel_NotCancellable(t *testing.T) {
	h, executions, workflows, memberships, projects := newTestExecutionHandler()
	user := &store.User{ID: uuid.New(), Email: "exec@example.com", Active: true}
	wf := setupWorkflowWithPerms(t, workflows, projects, memberships, user.ID, store.RoleAdmin)

	execID := uuid.New()
	executions.executions[execID] = &store.WorkflowExecution{
		ID:         execID,
		WorkflowID: wf.ID,
		Status:     store.ExecutionStatusCompleted,
		StartedAt:  time.Now(),
	}

	req := httptest.NewRequest("POST", "/api/v1/executions/"+execID.String()+"/cancel", nil)
	req.SetPathValue("id", execID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Cancel(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
}

func TestExecutionHandler_Cancel_Forbidden(t *testing.T) {
	h, executions, workflows, memberships, projects := newTestExecutionHandler()
	user := &store.User{ID: uuid.New(), Email: "exec@example.com", Active: true}
	wf := setupWorkflowWithPerms(t, workflows, projects, memberships, user.ID, store.RoleViewer)

	execID := uuid.New()
	executions.executions[execID] = &store.WorkflowExecution{
		ID:         execID,
		WorkflowID: wf.ID,
		Status:     store.ExecutionStatusRunning,
		StartedAt:  time.Now(),
	}

	req := httptest.NewRequest("POST", "/api/v1/executions/"+execID.String()+"/cancel", nil)
	req.SetPathValue("id", execID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Cancel(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestExecutionHandler_Cancel_NotFound(t *testing.T) {
	h, _, _, _, _ := newTestExecutionHandler()
	user := &store.User{ID: uuid.New(), Email: "exec@example.com", Active: true}
	execID := uuid.New()

	req := httptest.NewRequest("POST", "/api/v1/executions/"+execID.String()+"/cancel", nil)
	req.SetPathValue("id", execID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Cancel(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestExecutionHandler_Trigger_Success(t *testing.T) {
	h, _, workflows, memberships, projects := newTestExecutionHandler()
	user := &store.User{ID: uuid.New(), Email: "exec@example.com", Active: true}
	wf := setupWorkflowWithPerms(t, workflows, projects, memberships, user.ID, store.RoleEditor)

	body := makeJSON(map[string]interface{}{
		"trigger_data": map[string]string{"key": "value"},
	})
	req := httptest.NewRequest("POST", "/api/v1/workflows/"+wf.ID.String()+"/trigger", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", wf.ID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Trigger(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	resp := decodeBody(t, w.Result())
	data, _ := resp["data"].(map[string]interface{})
	if data["trigger_type"] != "manual" {
		t.Fatalf("expected trigger_type manual, got %v", data["trigger_type"])
	}
}

func TestExecutionHandler_Trigger_Unauthorized(t *testing.T) {
	h, _, _, _, _ := newTestExecutionHandler()
	wfID := uuid.New()

	body := makeJSON(map[string]interface{}{"trigger_data": nil})
	req := httptest.NewRequest("POST", "/api/v1/workflows/"+wfID.String()+"/trigger", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", wfID.String())
	w := httptest.NewRecorder()
	h.Trigger(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestExecutionHandler_Trigger_Forbidden(t *testing.T) {
	h, _, workflows, memberships, projects := newTestExecutionHandler()
	user := &store.User{ID: uuid.New(), Email: "exec@example.com", Active: true}
	wf := setupWorkflowWithPerms(t, workflows, projects, memberships, user.ID, store.RoleViewer)

	body := makeJSON(map[string]interface{}{"trigger_data": nil})
	req := httptest.NewRequest("POST", "/api/v1/workflows/"+wf.ID.String()+"/trigger", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", wf.ID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Trigger(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestExecutionHandler_Trigger_WorkflowNotFound(t *testing.T) {
	h, _, workflows, memberships, projects := newTestExecutionHandler()
	user := &store.User{ID: uuid.New(), Email: "exec@example.com", Active: true}
	wf := setupWorkflowWithPerms(t, workflows, projects, memberships, user.ID, store.RoleEditor)

	// Delete the workflow after setting up perms
	delete(workflows.workflows, wf.ID)

	body := makeJSON(map[string]interface{}{"trigger_data": nil})
	req := httptest.NewRequest("POST", "/api/v1/workflows/"+wf.ID.String()+"/trigger", body)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", wf.ID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Trigger(w, req)

	// CanAccess will fail because workflow was deleted, so it returns 403
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestExecutionHandler_Trigger_InvalidBody(t *testing.T) {
	h, _, workflows, memberships, projects := newTestExecutionHandler()
	user := &store.User{ID: uuid.New(), Email: "exec@example.com", Active: true}
	wf := setupWorkflowWithPerms(t, workflows, projects, memberships, user.ID, store.RoleEditor)

	req := httptest.NewRequest("POST", "/api/v1/workflows/"+wf.ID.String()+"/trigger",
		bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", wf.ID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Trigger(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
