package iam

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/GoCodeAlone/workflow/store"
)

// OIDCConfig holds configuration for the OIDC provider.
type OIDCConfig struct {
	Issuer   string `json:"issuer"`
	ClientID string `json:"client_id"`
	ClaimKey string `json:"claim_key"` // Which claim to use as the external identifier (e.g. "sub", "email")
}

// OIDCProvider maps OIDC claims to roles.
// This is a stub implementation that validates config format but does not make
// actual OIDC discovery or token validation calls.
type OIDCProvider struct{}

func (p *OIDCProvider) Type() store.IAMProviderType {
	return store.IAMProviderOIDC
}

func (p *OIDCProvider) ValidateConfig(config json.RawMessage) error {
	var c OIDCConfig
	if err := json.Unmarshal(config, &c); err != nil {
		return fmt.Errorf("invalid oidc config: %w", err)
	}
	if c.Issuer == "" {
		return fmt.Errorf("issuer is required")
	}
	if c.ClientID == "" {
		return fmt.Errorf("client_id is required")
	}
	return nil
}

func (p *OIDCProvider) ResolveIdentities(_ context.Context, config json.RawMessage, credentials map[string]string) ([]ExternalIdentity, error) {
	var c OIDCConfig
	if err := json.Unmarshal(config, &c); err != nil {
		return nil, fmt.Errorf("invalid oidc config: %w", err)
	}

	claimKey := c.ClaimKey
	if claimKey == "" {
		claimKey = "sub"
	}

	claimValue, ok := credentials[claimKey]
	if !ok || claimValue == "" {
		return nil, fmt.Errorf("claim %q not found in credentials", claimKey)
	}

	return []ExternalIdentity{
		{
			Provider:   string(store.IAMProviderOIDC),
			Identifier: claimValue,
			Attributes: map[string]string{claimKey: claimValue},
		},
	}, nil
}

func (p *OIDCProvider) TestConnection(_ context.Context, config json.RawMessage) error {
	return p.ValidateConfig(config)
}
