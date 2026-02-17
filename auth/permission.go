package auth

import (
	"context"
	"fmt"
	"sync"
)

// PermissionProvider abstracts permission evaluation so different backends
// (built-in RBAC, permit.io, AWS IAM, etc.) can be plugged in.
type PermissionProvider interface {
	// Name returns the unique identifier for this provider.
	Name() string
	// CheckPermission evaluates whether subject may perform action on resource.
	CheckPermission(ctx context.Context, subject, resource, action string) (bool, error)
	// ListPermissions returns all permissions granted to the subject.
	ListPermissions(ctx context.Context, subject string) ([]Permission, error)
	// SyncRoles pushes role definitions into the provider.
	SyncRoles(ctx context.Context, roles []RoleDefinition) error
}

// Permission represents a single access control entry.
type Permission struct {
	Resource string `json:"resource"`
	Action   string `json:"action"`
	Effect   string `json:"effect"` // "allow" or "deny"
}

// RoleDefinition describes a named role and its permissions.
type RoleDefinition struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Permissions []Permission `json:"permissions"`
}

// PermissionManager aggregates one or more PermissionProviders and delegates
// permission checks to the primary provider.
type PermissionManager struct {
	providers map[string]PermissionProvider
	primary   PermissionProvider
	mu        sync.RWMutex
}

// NewPermissionManager creates an empty PermissionManager.
func NewPermissionManager() *PermissionManager {
	return &PermissionManager{
		providers: make(map[string]PermissionProvider),
	}
}

// AddProvider registers a provider. The first provider added automatically
// becomes the primary if none has been set.
func (pm *PermissionManager) AddProvider(p PermissionProvider) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.providers[p.Name()] = p
	if pm.primary == nil {
		pm.primary = p
	}
}

// SetPrimary designates the named provider as the one used for Check calls.
func (pm *PermissionManager) SetPrimary(name string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	p, ok := pm.providers[name]
	if !ok {
		return fmt.Errorf("permission provider %q not registered", name)
	}
	pm.primary = p
	return nil
}

// Check delegates a permission check to the primary provider.
func (pm *PermissionManager) Check(ctx context.Context, subject, resource, action string) (bool, error) {
	pm.mu.RLock()
	p := pm.primary
	pm.mu.RUnlock()
	if p == nil {
		return false, fmt.Errorf("no permission provider configured")
	}
	return p.CheckPermission(ctx, subject, resource, action)
}

// ListAll aggregates permissions from every registered provider.
func (pm *PermissionManager) ListAll(ctx context.Context, subject string) ([]Permission, error) {
	pm.mu.RLock()
	providers := make([]PermissionProvider, 0, len(pm.providers))
	for _, p := range pm.providers {
		providers = append(providers, p)
	}
	pm.mu.RUnlock()

	var all []Permission
	for _, p := range providers {
		perms, err := p.ListPermissions(ctx, subject)
		if err != nil {
			return nil, fmt.Errorf("provider %q: %w", p.Name(), err)
		}
		all = append(all, perms...)
	}
	return all, nil
}

// Provider returns the named provider, if registered.
func (pm *PermissionManager) Provider(name string) (PermissionProvider, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	p, ok := pm.providers[name]
	return p, ok
}

// Providers returns the names of all registered providers.
func (pm *PermissionManager) Providers() []string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	names := make([]string, 0, len(pm.providers))
	for n := range pm.providers {
		names = append(names, n)
	}
	return names
}
