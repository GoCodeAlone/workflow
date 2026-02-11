package iam

import (
	"context"
	"encoding/json"

	"github.com/GoCodeAlone/workflow/store"
	"github.com/google/uuid"
)

// ExternalIdentity represents an identity from an external IAM system.
type ExternalIdentity struct {
	Provider   string            `json:"provider"`
	Identifier string            `json:"identifier"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// IAMProvider defines the interface for an external IAM provider.
type IAMProvider interface {
	// Type returns the provider type identifier.
	Type() store.IAMProviderType
	// ValidateConfig checks that the provider configuration is valid.
	ValidateConfig(config json.RawMessage) error
	// ResolveIdentities extracts external identities from the given credentials/token.
	ResolveIdentities(ctx context.Context, config json.RawMessage, credentials map[string]string) ([]ExternalIdentity, error)
	// TestConnection tests that the provider configuration can connect.
	TestConnection(ctx context.Context, config json.RawMessage) error
}

// IAMResolver combines IAM providers with the store to resolve external identities to roles.
type IAMResolver struct {
	iamStore  store.IAMStore
	providers map[store.IAMProviderType]IAMProvider
}

// NewIAMResolver creates a new IAMResolver.
func NewIAMResolver(iamStore store.IAMStore) *IAMResolver {
	return &IAMResolver{
		iamStore:  iamStore,
		providers: make(map[store.IAMProviderType]IAMProvider),
	}
}

// RegisterProvider registers an IAM provider implementation.
func (r *IAMResolver) RegisterProvider(p IAMProvider) {
	r.providers[p.Type()] = p
}

// GetProvider returns the registered provider for the given type, if any.
func (r *IAMResolver) GetProvider(providerType store.IAMProviderType) (IAMProvider, bool) {
	p, ok := r.providers[providerType]
	return p, ok
}

// ResolveRole resolves the highest role for an external identity across all enabled providers
// in a company for a specific resource.
func (r *IAMResolver) ResolveRole(ctx context.Context, companyID uuid.UUID, identity ExternalIdentity, resourceType string, resourceID uuid.UUID) (store.Role, error) {
	enabled := true
	providers, err := r.iamStore.ListProviders(ctx, store.IAMProviderFilter{
		CompanyID: &companyID,
		Enabled:   &enabled,
	})
	if err != nil {
		return "", err
	}

	var bestRole store.Role
	for _, provider := range providers {
		role, err := r.iamStore.ResolveRole(ctx, provider.ID, identity.Identifier, resourceType, resourceID)
		if err != nil {
			continue
		}
		if bestRole == "" || roleWeight(role) > roleWeight(bestRole) {
			bestRole = role
		}
	}
	if bestRole == "" {
		return "", store.ErrNotFound
	}
	return bestRole, nil
}

// roleWeight maps roles to an integer weight for comparison.
func roleWeight(role store.Role) int {
	switch role {
	case store.RoleViewer:
		return 1
	case store.RoleEditor:
		return 2
	case store.RoleAdmin:
		return 3
	case store.RoleOwner:
		return 4
	default:
		return 0
	}
}
