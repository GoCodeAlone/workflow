package rbac

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
)

// Action represents an operation that can be performed on a resource.
type Action string

const (
	ActionRead   Action = "read"
	ActionWrite  Action = "write"
	ActionDelete Action = "delete"
	ActionAdmin  Action = "admin"
)

// Resource represents a type of resource in the system.
type Resource string

const (
	ResourceWorkflows Resource = "workflows"
	ResourceModules   Resource = "modules"
	ResourceConfigs   Resource = "configs"
	ResourceUsers     Resource = "users"
	ResourceSecrets   Resource = "secrets"
	ResourceAll       Resource = "*"
)

// Permission represents permission to perform an action on a resource.
type Permission struct {
	Resource Resource `json:"resource"`
	Action   Action   `json:"action"`
}

// String returns a human-readable representation of the permission.
func (p Permission) String() string {
	return string(p.Resource) + ":" + string(p.Action)
}

// ParsePermission parses a "resource:action" string into a Permission.
func ParsePermission(s string) (Permission, error) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return Permission{}, errors.New("rbac: invalid permission format, expected resource:action")
	}
	return Permission{
		Resource: Resource(parts[0]),
		Action:   Action(parts[1]),
	}, nil
}

// Role represents a named set of permissions.
type Role struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Permissions []Permission `json:"permissions"`
}

// HasPermission checks whether this role grants the given permission.
func (r *Role) HasPermission(resource Resource, action Action) bool {
	for _, p := range r.Permissions {
		if (p.Resource == ResourceAll || p.Resource == resource) &&
			(p.Action == ActionAdmin || p.Action == action) {
			return true
		}
	}
	return false
}

// Built-in roles.
var (
	RoleViewer = &Role{
		Name:        "viewer",
		Description: "Read-only access to all resources",
		Permissions: []Permission{
			{Resource: ResourceWorkflows, Action: ActionRead},
			{Resource: ResourceModules, Action: ActionRead},
			{Resource: ResourceConfigs, Action: ActionRead},
		},
	}

	RoleEditor = &Role{
		Name:        "editor",
		Description: "Read and write access to workflows and modules",
		Permissions: []Permission{
			{Resource: ResourceWorkflows, Action: ActionRead},
			{Resource: ResourceWorkflows, Action: ActionWrite},
			{Resource: ResourceModules, Action: ActionRead},
			{Resource: ResourceModules, Action: ActionWrite},
			{Resource: ResourceConfigs, Action: ActionRead},
			{Resource: ResourceConfigs, Action: ActionWrite},
		},
	}

	RoleOperator = &Role{
		Name:        "operator",
		Description: "Full workflow and module management including deletion",
		Permissions: []Permission{
			{Resource: ResourceWorkflows, Action: ActionRead},
			{Resource: ResourceWorkflows, Action: ActionWrite},
			{Resource: ResourceWorkflows, Action: ActionDelete},
			{Resource: ResourceModules, Action: ActionRead},
			{Resource: ResourceModules, Action: ActionWrite},
			{Resource: ResourceModules, Action: ActionDelete},
			{Resource: ResourceConfigs, Action: ActionRead},
			{Resource: ResourceConfigs, Action: ActionWrite},
			{Resource: ResourceConfigs, Action: ActionDelete},
		},
	}

	RoleAdmin = &Role{
		Name:        "admin",
		Description: "Full access to all resources",
		Permissions: []Permission{
			{Resource: ResourceAll, Action: ActionAdmin},
		},
	}
)

// BuiltinRoles returns all predefined roles.
func BuiltinRoles() []*Role {
	return []*Role{RoleViewer, RoleEditor, RoleOperator, RoleAdmin}
}

// contextKey is an unexported type for context keys to prevent collisions.
type contextKey int

const (
	userRoleKey contextKey = iota
	userIDKey
)

// ContextWithRole stores a role name in the context.
func ContextWithRole(ctx context.Context, roleName string) context.Context {
	return context.WithValue(ctx, userRoleKey, roleName)
}

// RoleFromContext extracts the role name from the context.
func RoleFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(userRoleKey).(string)
	return v, ok
}

// ContextWithUserID stores a user ID in the context.
func ContextWithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}

// UserIDFromContext extracts the user ID from the context.
func UserIDFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(userIDKey).(string)
	return v, ok
}

// PolicyEngine manages roles and evaluates permissions.
type PolicyEngine struct {
	mu    sync.RWMutex
	roles map[string]*Role
}

// NewPolicyEngine creates a PolicyEngine pre-loaded with built-in roles.
func NewPolicyEngine() *PolicyEngine {
	pe := &PolicyEngine{
		roles: make(map[string]*Role),
	}
	for _, r := range BuiltinRoles() {
		pe.roles[r.Name] = r
	}
	return pe
}

// RegisterRole adds or replaces a role definition.
func (pe *PolicyEngine) RegisterRole(role *Role) {
	pe.mu.Lock()
	defer pe.mu.Unlock()
	pe.roles[role.Name] = role
}

// GetRole retrieves a role by name.
func (pe *PolicyEngine) GetRole(name string) (*Role, bool) {
	pe.mu.RLock()
	defer pe.mu.RUnlock()
	r, ok := pe.roles[name]
	return r, ok
}

// ListRoles returns all registered roles.
func (pe *PolicyEngine) ListRoles() []*Role {
	pe.mu.RLock()
	defer pe.mu.RUnlock()
	roles := make([]*Role, 0, len(pe.roles))
	for _, r := range pe.roles {
		roles = append(roles, r)
	}
	return roles
}

// Allowed checks whether the given role has permission for the resource and action.
func (pe *PolicyEngine) Allowed(roleName string, resource Resource, action Action) bool {
	pe.mu.RLock()
	defer pe.mu.RUnlock()
	role, ok := pe.roles[roleName]
	if !ok {
		return false
	}
	return role.HasPermission(resource, action)
}

// RoleExtractor is a function that extracts a role name from the request.
type RoleExtractor func(r *http.Request) (string, error)

// Middleware returns HTTP middleware that enforces RBAC permissions.
// The roleExtractor determines how the user's role is obtained from the request.
func Middleware(pe *PolicyEngine, resource Resource, action Action, roleExtractor RoleExtractor) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			roleName, err := roleExtractor(r)
			if err != nil {
				http.Error(w, "unauthorized: "+err.Error(), http.StatusUnauthorized)
				return
			}

			if !pe.Allowed(roleName, resource, action) {
				http.Error(w, "forbidden: insufficient permissions", http.StatusForbidden)
				return
			}

			ctx := ContextWithRole(r.Context(), roleName)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// HeaderRoleExtractor returns a RoleExtractor that reads the role from an HTTP header.
func HeaderRoleExtractor(header string) RoleExtractor {
	return func(r *http.Request) (string, error) {
		role := r.Header.Get(header)
		if role == "" {
			return "", errors.New("missing role header: " + header)
		}
		return role, nil
	}
}

// ContextRoleExtractor returns a RoleExtractor that reads the role from the request context.
func ContextRoleExtractor() RoleExtractor {
	return func(r *http.Request) (string, error) {
		role, ok := RoleFromContext(r.Context())
		if !ok || role == "" {
			return "", errors.New("no role in request context")
		}
		return role, nil
	}
}
