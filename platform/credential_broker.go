package platform

import (
	"context"
	"time"
)

// CredentialBroker issues and manages credentials scoped to specific tiers,
// environments, and applications. It integrates with the existing secrets.Provider
// system to store credential values securely. Only credential references are
// persisted in state; actual values live in the secrets backend.
type CredentialBroker interface {
	// IssueCredential creates a scoped credential for the given platform context.
	// The credential value is stored in the secrets backend and a reference is returned.
	IssueCredential(ctx context.Context, pctx *PlatformContext, request CredentialRequest) (*CredentialRef, error)

	// RevokeCredential invalidates a previously issued credential and removes it
	// from the secrets backend.
	RevokeCredential(ctx context.Context, ref *CredentialRef) error

	// ResolveCredential retrieves the actual credential value from a reference.
	// This is the only way to access the credential value at runtime.
	ResolveCredential(ctx context.Context, ref *CredentialRef) (string, error)

	// RotateCredential replaces an existing credential with a new one, revoking
	// the old credential. Returns the new credential reference.
	RotateCredential(ctx context.Context, ref *CredentialRef) (*CredentialRef, error)

	// ListCredentials returns all credential references for a given platform context.
	ListCredentials(ctx context.Context, pctx *PlatformContext) ([]*CredentialRef, error)
}

// CredentialRequest specifies what credential to issue, including its type,
// scope, and lifetime.
type CredentialRequest struct {
	// Name is a human-readable name for the credential.
	Name string `json:"name"`

	// Type is the credential type: "api_key", "database", "tls_cert", "token".
	Type string `json:"type"`

	// Scope lists the resource names this credential can access.
	Scope []string `json:"scope"`

	// TTL is the credential lifetime. A zero value means no expiry.
	TTL time.Duration `json:"ttl"`

	// Renewable indicates whether the credential can be renewed before expiry.
	Renewable bool `json:"renewable"`
}

// CredentialRef is a pointer to a stored credential. The actual credential
// value is never stored in state -- only this reference is persisted.
// Use CredentialBroker.ResolveCredential to retrieve the actual value.
type CredentialRef struct {
	// ID is the unique credential identifier.
	ID string `json:"id"`

	// Name is the human-readable credential name.
	Name string `json:"name"`

	// SecretPath is the path in the secrets backend where the value is stored.
	SecretPath string `json:"secretPath"`

	// Provider is the name of the secrets provider that holds the value.
	Provider string `json:"provider"`

	// ExpiresAt is when the credential expires.
	ExpiresAt time.Time `json:"expiresAt"`

	// Tier is the infrastructure tier the credential is scoped to.
	Tier Tier `json:"tier"`

	// ContextPath is the hierarchical context path (org/env/app).
	ContextPath string `json:"contextPath"`
}
