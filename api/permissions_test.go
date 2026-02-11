package api

import (
	"context"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/store"
	"github.com/google/uuid"
)

// --- RoleAtLeast tests ---

func TestRoleAtLeast_OwnerAboveAll(t *testing.T) {
	if !RoleAtLeast(store.RoleOwner, store.RoleAdmin) {
		t.Fatal("owner should be at least admin")
	}
	if !RoleAtLeast(store.RoleOwner, store.RoleEditor) {
		t.Fatal("owner should be at least editor")
	}
	if !RoleAtLeast(store.RoleOwner, store.RoleViewer) {
		t.Fatal("owner should be at least viewer")
	}
}

func TestRoleAtLeast_AdminAboveEditor(t *testing.T) {
	if !RoleAtLeast(store.RoleAdmin, store.RoleEditor) {
		t.Fatal("admin should be at least editor")
	}
	if !RoleAtLeast(store.RoleAdmin, store.RoleViewer) {
		t.Fatal("admin should be at least viewer")
	}
	if RoleAtLeast(store.RoleAdmin, store.RoleOwner) {
		t.Fatal("admin should NOT be at least owner")
	}
}

func TestRoleAtLeast_EditorAboveViewer(t *testing.T) {
	if !RoleAtLeast(store.RoleEditor, store.RoleViewer) {
		t.Fatal("editor should be at least viewer")
	}
	if RoleAtLeast(store.RoleEditor, store.RoleAdmin) {
		t.Fatal("editor should NOT be at least admin")
	}
}

func TestRoleAtLeast_ViewerIsMinimum(t *testing.T) {
	if !RoleAtLeast(store.RoleViewer, store.RoleViewer) {
		t.Fatal("viewer should be at least viewer")
	}
	if RoleAtLeast(store.RoleViewer, store.RoleEditor) {
		t.Fatal("viewer should NOT be at least editor")
	}
}

func TestRoleAtLeast_SameRole(t *testing.T) {
	for _, role := range []store.Role{store.RoleOwner, store.RoleAdmin, store.RoleEditor, store.RoleViewer} {
		if !RoleAtLeast(role, role) {
			t.Fatalf("%s should be at least %s", role, role)
		}
	}
}

// --- PermissionService tests ---

func newTestPermissionService() (*PermissionService, *mockMembershipStore, *mockWorkflowStore, *mockProjectStore) {
	memberships := &mockMembershipStore{}
	workflows := &mockWorkflowStore{workflows: make(map[uuid.UUID]*store.WorkflowRecord)}
	projects := &mockProjectStore{projects: make(map[uuid.UUID]*store.Project)}
	ps := NewPermissionService(memberships, workflows, projects)
	return ps, memberships, workflows, projects
}

func TestPermissionService_GetEffectiveRole_Workflow_Creator(t *testing.T) {
	ps, _, workflows, projects := newTestPermissionService()
	userID := uuid.New()
	companyID := uuid.New()
	projectID := uuid.New()

	projects.projects[projectID] = &store.Project{ID: projectID, CompanyID: companyID}
	wfID := uuid.New()
	workflows.workflows[wfID] = &store.WorkflowRecord{
		ID:        wfID,
		ProjectID: projectID,
		CreatedBy: userID, // User is creator
	}

	role, err := ps.GetEffectiveRole(context.Background(), userID, "workflow", wfID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if role != store.RoleOwner {
		t.Fatalf("expected owner role for workflow creator, got %s", role)
	}
}

func TestPermissionService_GetEffectiveRole_Workflow_ProjectMember(t *testing.T) {
	ps, memberships, workflows, projects := newTestPermissionService()
	userID := uuid.New()
	companyID := uuid.New()
	projectID := uuid.New()

	projects.projects[projectID] = &store.Project{ID: projectID, CompanyID: companyID}
	wfID := uuid.New()
	workflows.workflows[wfID] = &store.WorkflowRecord{
		ID:        wfID,
		ProjectID: projectID,
		CreatedBy: uuid.New(), // Different from userID
	}

	_ = memberships.Create(context.Background(), &store.Membership{
		ID:        uuid.New(),
		UserID:    userID,
		CompanyID: companyID,
		ProjectID: &projectID,
		Role:      store.RoleEditor,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})

	role, err := ps.GetEffectiveRole(context.Background(), userID, "workflow", wfID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if role != store.RoleEditor {
		t.Fatalf("expected editor role, got %s", role)
	}
}

func TestPermissionService_GetEffectiveRole_Workflow_CompanyMember(t *testing.T) {
	ps, memberships, workflows, projects := newTestPermissionService()
	userID := uuid.New()
	companyID := uuid.New()
	projectID := uuid.New()

	projects.projects[projectID] = &store.Project{ID: projectID, CompanyID: companyID}
	wfID := uuid.New()
	workflows.workflows[wfID] = &store.WorkflowRecord{
		ID:        wfID,
		ProjectID: projectID,
		CreatedBy: uuid.New(),
	}

	// Company-level membership only (no project membership)
	_ = memberships.Create(context.Background(), &store.Membership{
		ID:        uuid.New(),
		UserID:    userID,
		CompanyID: companyID,
		Role:      store.RoleAdmin,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})

	role, err := ps.GetEffectiveRole(context.Background(), userID, "workflow", wfID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if role != store.RoleAdmin {
		t.Fatalf("expected admin role from company cascade, got %s", role)
	}
}

func TestPermissionService_GetEffectiveRole_Project(t *testing.T) {
	ps, memberships, _, projects := newTestPermissionService()
	userID := uuid.New()
	companyID := uuid.New()
	projectID := uuid.New()

	projects.projects[projectID] = &store.Project{ID: projectID, CompanyID: companyID}

	_ = memberships.Create(context.Background(), &store.Membership{
		ID:        uuid.New(),
		UserID:    userID,
		CompanyID: companyID,
		ProjectID: &projectID,
		Role:      store.RoleViewer,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})

	role, err := ps.GetEffectiveRole(context.Background(), userID, "project", projectID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if role != store.RoleViewer {
		t.Fatalf("expected viewer role, got %s", role)
	}
}

func TestPermissionService_GetEffectiveRole_Company(t *testing.T) {
	ps, memberships, _, _ := newTestPermissionService()
	userID := uuid.New()
	companyID := uuid.New()

	_ = memberships.Create(context.Background(), &store.Membership{
		ID:        uuid.New(),
		UserID:    userID,
		CompanyID: companyID,
		Role:      store.RoleOwner,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})

	role, err := ps.GetEffectiveRole(context.Background(), userID, "company", companyID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if role != store.RoleOwner {
		t.Fatalf("expected owner role, got %s", role)
	}
}

func TestPermissionService_GetEffectiveRole_UnknownResource(t *testing.T) {
	ps, _, _, _ := newTestPermissionService()

	_, err := ps.GetEffectiveRole(context.Background(), uuid.New(), "unknown", uuid.New())
	if err == nil {
		t.Fatal("expected error for unknown resource type")
	}
}

func TestPermissionService_CanAccess_Allowed(t *testing.T) {
	ps, memberships, _, _ := newTestPermissionService()
	userID := uuid.New()
	companyID := uuid.New()

	_ = memberships.Create(context.Background(), &store.Membership{
		ID:        uuid.New(),
		UserID:    userID,
		CompanyID: companyID,
		Role:      store.RoleAdmin,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})

	if !ps.CanAccess(context.Background(), userID, "company", companyID, store.RoleViewer) {
		t.Fatal("admin should be able to access as viewer")
	}
}

func TestPermissionService_CanAccess_Denied(t *testing.T) {
	ps, memberships, _, _ := newTestPermissionService()
	userID := uuid.New()
	companyID := uuid.New()

	_ = memberships.Create(context.Background(), &store.Membership{
		ID:        uuid.New(),
		UserID:    userID,
		CompanyID: companyID,
		Role:      store.RoleViewer,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})

	if ps.CanAccess(context.Background(), userID, "company", companyID, store.RoleAdmin) {
		t.Fatal("viewer should NOT be able to access as admin")
	}
}

func TestPermissionService_CanAccess_NoMembership(t *testing.T) {
	ps, _, _, _ := newTestPermissionService()

	if ps.CanAccess(context.Background(), uuid.New(), "company", uuid.New(), store.RoleViewer) {
		t.Fatal("should not have access without membership")
	}
}

func TestPermissionService_CascadingRole_CompanyToProject(t *testing.T) {
	ps, memberships, _, projects := newTestPermissionService()
	userID := uuid.New()
	companyID := uuid.New()
	projectID := uuid.New()

	projects.projects[projectID] = &store.Project{ID: projectID, CompanyID: companyID}

	// Only company-level membership
	_ = memberships.Create(context.Background(), &store.Membership{
		ID:        uuid.New(),
		UserID:    userID,
		CompanyID: companyID,
		Role:      store.RoleEditor,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})

	role, err := ps.GetEffectiveRole(context.Background(), userID, "project", projectID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if role != store.RoleEditor {
		t.Fatalf("expected editor role cascaded from company, got %s", role)
	}
}

func TestPermissionService_CascadingRole_ProjectToWorkflow(t *testing.T) {
	ps, memberships, workflows, projects := newTestPermissionService()
	userID := uuid.New()
	companyID := uuid.New()
	projectID := uuid.New()

	projects.projects[projectID] = &store.Project{ID: projectID, CompanyID: companyID}
	wfID := uuid.New()
	workflows.workflows[wfID] = &store.WorkflowRecord{
		ID:        wfID,
		ProjectID: projectID,
		CreatedBy: uuid.New(),
	}

	// Project-level membership
	_ = memberships.Create(context.Background(), &store.Membership{
		ID:        uuid.New(),
		UserID:    userID,
		CompanyID: companyID,
		ProjectID: &projectID,
		Role:      store.RoleAdmin,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})

	if !ps.CanAccess(context.Background(), userID, "workflow", wfID, store.RoleAdmin) {
		t.Fatal("admin on project should have admin access to workflow in that project")
	}
}
