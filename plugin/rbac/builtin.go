package rbac

import (
	"context"
	"sync"

	"github.com/GoCodeAlone/workflow/auth"
	coreRBAC "github.com/GoCodeAlone/workflow/auth/rbac"
)

// BuiltinProvider wraps the existing PolicyEngine to implement PermissionProvider.
type BuiltinProvider struct {
	engine *coreRBAC.PolicyEngine
	mu     sync.RWMutex
}

// NewBuiltinProvider creates a BuiltinProvider backed by the given PolicyEngine.
func NewBuiltinProvider(engine *coreRBAC.PolicyEngine) *BuiltinProvider {
	return &BuiltinProvider{engine: engine}
}

// Name returns the provider identifier.
func (b *BuiltinProvider) Name() string { return "builtin" }

// CheckPermission maps the PermissionProvider interface to PolicyEngine.Allowed.
// The subject is treated as a role name.
func (b *BuiltinProvider) CheckPermission(_ context.Context, subject, resource, action string) (bool, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.engine.Allowed(subject, coreRBAC.Resource(resource), coreRBAC.Action(action)), nil
}

// ListPermissions returns all permissions for the given role.
func (b *BuiltinProvider) ListPermissions(_ context.Context, subject string) ([]auth.Permission, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	role, ok := b.engine.GetRole(subject)
	if !ok {
		return nil, nil
	}
	perms := make([]auth.Permission, 0, len(role.Permissions))
	for _, p := range role.Permissions {
		perms = append(perms, auth.Permission{
			Resource: string(p.Resource),
			Action:   string(p.Action),
			Effect:   "allow",
		})
	}
	return perms, nil
}

// SyncRoles registers role definitions in the underlying PolicyEngine.
// This allows dynamic role creation beyond the 4 built-in roles.
func (b *BuiltinProvider) SyncRoles(_ context.Context, roles []auth.RoleDefinition) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, rd := range roles {
		perms := make([]coreRBAC.Permission, 0, len(rd.Permissions))
		for _, p := range rd.Permissions {
			perms = append(perms, coreRBAC.Permission{
				Resource: coreRBAC.Resource(p.Resource),
				Action:   coreRBAC.Action(p.Action),
			})
		}
		b.engine.RegisterRole(&coreRBAC.Role{
			Name:        rd.Name,
			Description: rd.Description,
			Permissions: perms,
		})
	}
	return nil
}
