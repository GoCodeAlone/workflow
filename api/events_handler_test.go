package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/store"
	"github.com/google/uuid"
)

// --- helpers ---

func newTestEventsHandler() (*EventsHandler, *mockExecutionStore, *mockLogStore, *mockWorkflowStore, *mockMembershipStore, *mockProjectStore) {
	executions := newMockExecutionStore()
	logs := newMockLogStore()
	workflows := &mockWorkflowStore{workflows: make(map[uuid.UUID]*store.WorkflowRecord)}
	memberships := &mockMembershipStore{}
	projects := &mockProjectStore{projects: make(map[uuid.UUID]*store.Project)}
	perms := NewPermissionService(memberships, workflows, projects)
	h := NewEventsHandler(executions, logs, perms)
	return h, executions, logs, workflows, memberships, projects
}

// --- tests ---

func TestEventsHandler_List_Success(t *testing.T) {
	h, executions, _, workflows, memberships, projects := newTestEventsHandler()
	user := &store.User{ID: uuid.New(), Email: "events@example.com", Active: true}
	wf := setupWorkflowWithPerms(t, workflows, projects, memberships, user.ID, store.RoleViewer)

	executions.executions[uuid.New()] = &store.WorkflowExecution{
		ID:         uuid.New(),
		WorkflowID: wf.ID,
		Status:     store.ExecutionStatusCompleted,
		StartedAt:  time.Now(),
	}

	req := httptest.NewRequest("GET", "/api/v1/workflows/"+wf.ID.String()+"/events", nil)
	req.SetPathValue("id", wf.ID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestEventsHandler_List_Unauthorized(t *testing.T) {
	h, _, _, _, _, _ := newTestEventsHandler()
	wfID := uuid.New()

	req := httptest.NewRequest("GET", "/api/v1/workflows/"+wfID.String()+"/events", nil)
	req.SetPathValue("id", wfID.String())
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestEventsHandler_List_Forbidden(t *testing.T) {
	h, _, _, workflows, _, projects := newTestEventsHandler()
	user := &store.User{ID: uuid.New(), Email: "events@example.com", Active: true}

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

	req := httptest.NewRequest("GET", "/api/v1/workflows/"+wf.ID.String()+"/events", nil)
	req.SetPathValue("id", wf.ID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestEventsHandler_List_Empty(t *testing.T) {
	h, _, _, workflows, memberships, projects := newTestEventsHandler()
	user := &store.User{ID: uuid.New(), Email: "events@example.com", Active: true}
	wf := setupWorkflowWithPerms(t, workflows, projects, memberships, user.ID, store.RoleViewer)

	req := httptest.NewRequest("GET", "/api/v1/workflows/"+wf.ID.String()+"/events", nil)
	req.SetPathValue("id", wf.ID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestEventsHandler_List_InvalidID(t *testing.T) {
	h, _, _, _, _, _ := newTestEventsHandler()
	user := &store.User{ID: uuid.New(), Email: "events@example.com", Active: true}

	req := httptest.NewRequest("GET", "/api/v1/workflows/bad-id/events", nil)
	req.SetPathValue("id", "bad-id")
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestEventsHandler_List_WithExecutions(t *testing.T) {
	h, executions, _, workflows, memberships, projects := newTestEventsHandler()
	user := &store.User{ID: uuid.New(), Email: "events@example.com", Active: true}
	wf := setupWorkflowWithPerms(t, workflows, projects, memberships, user.ID, store.RoleViewer)

	for range 3 {
		execID := uuid.New()
		executions.executions[execID] = &store.WorkflowExecution{
			ID:         execID,
			WorkflowID: wf.ID,
			Status:     store.ExecutionStatusCompleted,
			StartedAt:  time.Now(),
		}
	}

	req := httptest.NewRequest("GET", "/api/v1/workflows/"+wf.ID.String()+"/events", nil)
	req.SetPathValue("id", wf.ID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestEventsHandler_List_MultipleWorkflows(t *testing.T) {
	h, executions, _, workflows, memberships, projects := newTestEventsHandler()
	user := &store.User{ID: uuid.New(), Email: "events@example.com", Active: true}
	wf1 := setupWorkflowWithPerms(t, workflows, projects, memberships, user.ID, store.RoleViewer)

	// Add execution to wf1
	execID1 := uuid.New()
	executions.executions[execID1] = &store.WorkflowExecution{
		ID: execID1, WorkflowID: wf1.ID, Status: store.ExecutionStatusCompleted, StartedAt: time.Now(),
	}
	// Add execution to another workflow
	execID2 := uuid.New()
	executions.executions[execID2] = &store.WorkflowExecution{
		ID: execID2, WorkflowID: uuid.New(), Status: store.ExecutionStatusCompleted, StartedAt: time.Now(),
	}

	req := httptest.NewRequest("GET", "/api/v1/workflows/"+wf1.ID.String()+"/events", nil)
	req.SetPathValue("id", wf1.ID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.List(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}
