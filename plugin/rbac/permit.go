package rbac

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/workflow/auth"
)

// PermitProvider is a stub for permit.io integration.
// It defines the interface shape; the full SDK integration is left for
// when the permit.io dependency is added.
type PermitProvider struct {
	apiKey   string
	endpoint string
}

// NewPermitProvider creates a PermitProvider with the given API key and endpoint.
func NewPermitProvider(apiKey, endpoint string) *PermitProvider {
	return &PermitProvider{apiKey: apiKey, endpoint: endpoint}
}

// Name returns the provider identifier.
func (p *PermitProvider) Name() string { return "permit" }

// CheckPermission calls the permit.io PDP to evaluate access.
func (p *PermitProvider) CheckPermission(_ context.Context, _, _, _ string) (bool, error) {
	return false, fmt.Errorf("permit.io provider not implemented: configure API key at %s", p.endpoint)
}

// ListPermissions retrieves permissions from permit.io for the subject.
func (p *PermitProvider) ListPermissions(_ context.Context, _ string) ([]auth.Permission, error) {
	return nil, fmt.Errorf("permit.io provider not implemented")
}

// SyncRoles pushes role definitions to permit.io.
func (p *PermitProvider) SyncRoles(_ context.Context, _ []auth.RoleDefinition) error {
	return fmt.Errorf("permit.io provider not implemented")
}
