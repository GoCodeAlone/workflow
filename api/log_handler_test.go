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

func newTestLogHandler() (*LogHandler, *mockLogStore, *mockWorkflowStore, *mockMembershipStore, *mockProjectStore) {
	logs := newMockLogStore()
	workflows := &mockWorkflowStore{workflows: make(map[uuid.UUID]*store.WorkflowRecord)}
	memberships := &mockMembershipStore{}
	projects := &mockProjectStore{projects: make(map[uuid.UUID]*store.Project)}
	perms := NewPermissionService(memberships, workflows, projects)
	h := NewLogHandler(logs, perms)
	return h, logs, workflows, memberships, projects
}

// --- tests ---

func TestLogHandler_Query_Success(t *testing.T) {
	h, logs, workflows, memberships, projects := newTestLogHandler()
	user := &store.User{ID: uuid.New(), Email: "log@example.com", Active: true}
	wf := setupWorkflowWithPerms(t, workflows, projects, memberships, user.ID, store.RoleViewer)

	_ = logs.Append(context.Background(), &store.ExecutionLog{
		WorkflowID: wf.ID,
		Level:      store.LogLevelInfo,
		Message:    "test log",
		CreatedAt:  time.Now(),
	})

	req := httptest.NewRequest("GET", "/api/v1/workflows/"+wf.ID.String()+"/logs", nil)
	req.SetPathValue("id", wf.ID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Query(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestLogHandler_Query_Unauthorized(t *testing.T) {
	h, _, _, _, _ := newTestLogHandler()
	wfID := uuid.New()

	req := httptest.NewRequest("GET", "/api/v1/workflows/"+wfID.String()+"/logs", nil)
	req.SetPathValue("id", wfID.String())
	w := httptest.NewRecorder()
	h.Query(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestLogHandler_Query_Forbidden(t *testing.T) {
	h, _, workflows, _, projects := newTestLogHandler()
	user := &store.User{ID: uuid.New(), Email: "log@example.com", Active: true}

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

	req := httptest.NewRequest("GET", "/api/v1/workflows/"+wf.ID.String()+"/logs", nil)
	req.SetPathValue("id", wf.ID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Query(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestLogHandler_Query_WithLevelFilter(t *testing.T) {
	h, logs, workflows, memberships, projects := newTestLogHandler()
	user := &store.User{ID: uuid.New(), Email: "log@example.com", Active: true}
	wf := setupWorkflowWithPerms(t, workflows, projects, memberships, user.ID, store.RoleViewer)

	_ = logs.Append(context.Background(), &store.ExecutionLog{
		WorkflowID: wf.ID, Level: store.LogLevelInfo, Message: "info", CreatedAt: time.Now(),
	})
	_ = logs.Append(context.Background(), &store.ExecutionLog{
		WorkflowID: wf.ID, Level: store.LogLevelError, Message: "error", CreatedAt: time.Now(),
	})

	req := httptest.NewRequest("GET", "/api/v1/workflows/"+wf.ID.String()+"/logs?level=error", nil)
	req.SetPathValue("id", wf.ID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Query(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestLogHandler_Query_WithModuleFilter(t *testing.T) {
	h, logs, workflows, memberships, projects := newTestLogHandler()
	user := &store.User{ID: uuid.New(), Email: "log@example.com", Active: true}
	wf := setupWorkflowWithPerms(t, workflows, projects, memberships, user.ID, store.RoleViewer)

	_ = logs.Append(context.Background(), &store.ExecutionLog{
		WorkflowID: wf.ID, Level: store.LogLevelInfo, ModuleName: "http", Message: "req", CreatedAt: time.Now(),
	})

	req := httptest.NewRequest("GET", "/api/v1/workflows/"+wf.ID.String()+"/logs?module=http", nil)
	req.SetPathValue("id", wf.ID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Query(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestLogHandler_Query_WithExecutionIDFilter(t *testing.T) {
	h, logs, workflows, memberships, projects := newTestLogHandler()
	user := &store.User{ID: uuid.New(), Email: "log@example.com", Active: true}
	wf := setupWorkflowWithPerms(t, workflows, projects, memberships, user.ID, store.RoleViewer)

	execID := uuid.New()
	_ = logs.Append(context.Background(), &store.ExecutionLog{
		WorkflowID:  wf.ID,
		ExecutionID: &execID,
		Level:       store.LogLevelInfo,
		Message:     "exec log",
		CreatedAt:   time.Now(),
	})

	req := httptest.NewRequest("GET", "/api/v1/workflows/"+wf.ID.String()+"/logs?execution_id="+execID.String(), nil)
	req.SetPathValue("id", wf.ID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Query(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestLogHandler_Query_Empty(t *testing.T) {
	h, _, workflows, memberships, projects := newTestLogHandler()
	user := &store.User{ID: uuid.New(), Email: "log@example.com", Active: true}
	wf := setupWorkflowWithPerms(t, workflows, projects, memberships, user.ID, store.RoleViewer)

	req := httptest.NewRequest("GET", "/api/v1/workflows/"+wf.ID.String()+"/logs", nil)
	req.SetPathValue("id", wf.ID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Query(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestLogHandler_Query_InvalidWorkflowID(t *testing.T) {
	h, _, _, _, _ := newTestLogHandler()
	user := &store.User{ID: uuid.New(), Email: "log@example.com", Active: true}

	req := httptest.NewRequest("GET", "/api/v1/workflows/bad-id/logs", nil)
	req.SetPathValue("id", "bad-id")
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Query(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestLogHandler_Query_WithSinceFilter(t *testing.T) {
	h, logs, workflows, memberships, projects := newTestLogHandler()
	user := &store.User{ID: uuid.New(), Email: "log@example.com", Active: true}
	wf := setupWorkflowWithPerms(t, workflows, projects, memberships, user.ID, store.RoleViewer)

	_ = logs.Append(context.Background(), &store.ExecutionLog{
		WorkflowID: wf.ID, Level: store.LogLevelInfo, Message: "recent", CreatedAt: time.Now(),
	})

	since := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
	req := httptest.NewRequest("GET", "/api/v1/workflows/"+wf.ID.String()+"/logs?since="+since, nil)
	req.SetPathValue("id", wf.ID.String())
	ctx := SetUserContext(req.Context(), user)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Query(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}
