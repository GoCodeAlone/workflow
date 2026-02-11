package api

import (
	"context"

	"github.com/GoCodeAlone/workflow/store"
	"github.com/google/uuid"
)

// roleWeight maps roles to an integer weight for comparison.
// Higher weight = more permissions.
var roleWeight = map[store.Role]int{
	store.RoleViewer: 1,
	store.RoleEditor: 2,
	store.RoleAdmin:  3,
	store.RoleOwner:  4,
}

// RoleAtLeast returns true if role has at least the given minimum role level.
func RoleAtLeast(role, minRole store.Role) bool {
	return roleWeight[role] >= roleWeight[minRole]
}

// PermissionService resolves effective permissions across the resource hierarchy.
type PermissionService struct {
	memberships store.MembershipStore
	workflows   store.WorkflowStore
	projects    store.ProjectStore
}

// NewPermissionService creates a new PermissionService.
func NewPermissionService(memberships store.MembershipStore, workflows store.WorkflowStore, projects store.ProjectStore) *PermissionService {
	return &PermissionService{
		memberships: memberships,
		workflows:   workflows,
		projects:    projects,
	}
}

// GetEffectiveRole resolves the cascading effective role for a user on a resource.
// Cascade: workflow_permissions -> project_memberships -> company_memberships.
func (ps *PermissionService) GetEffectiveRole(ctx context.Context, userID uuid.UUID, resourceType string, resourceID uuid.UUID) (store.Role, error) {
	switch resourceType {
	case "workflow":
		// Check workflow-level permissions via memberships with workflow-linked project.
		wf, err := ps.workflows.Get(ctx, resourceID)
		if err != nil {
			return "", err
		}
		proj, err := ps.projects.Get(ctx, wf.ProjectID)
		if err != nil {
			return "", err
		}
		// If the user created the workflow, they're the owner.
		if wf.CreatedBy == userID {
			return store.RoleOwner, nil
		}
		// Try project-level membership, cascading to company.
		role, err := ps.memberships.GetEffectiveRole(ctx, userID, proj.CompanyID, &wf.ProjectID)
		if err != nil {
			return "", err
		}
		return role, nil

	case "project":
		proj, err := ps.projects.Get(ctx, resourceID)
		if err != nil {
			return "", err
		}
		role, err := ps.memberships.GetEffectiveRole(ctx, userID, proj.CompanyID, &resourceID)
		if err != nil {
			return "", err
		}
		return role, nil

	case "company", "organization":
		role, err := ps.memberships.GetEffectiveRole(ctx, userID, resourceID, nil)
		if err != nil {
			return "", err
		}
		return role, nil

	default:
		return "", store.ErrNotFound
	}
}

// CanAccess returns true if the user has at least minRole on the given resource.
func (ps *PermissionService) CanAccess(ctx context.Context, userID uuid.UUID, resourceType string, resourceID uuid.UUID, minRole store.Role) bool {
	role, err := ps.GetEffectiveRole(ctx, userID, resourceType, resourceID)
	if err != nil {
		return false
	}
	return RoleAtLeast(role, minRole)
}
