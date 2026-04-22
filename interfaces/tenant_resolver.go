package interfaces

import (
	"context"
	"net/http"
)

// Tenant represents an identified tenant in a multi-tenant system.
type Tenant struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Slug     string         `json:"slug"`
	Domains  []string       `json:"domains,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
	IsActive bool           `json:"is_active"`
}

// IsZero reports whether t is the zero value (no ID set).
func (t Tenant) IsZero() bool {
	return t.ID == ""
}

// Selector extracts a tenant lookup key from an HTTP request.
// Match returns the key, whether the selector matched, and any error.
type Selector interface {
	Match(r *http.Request) (key string, matched bool, err error)
}

// TenantResolver resolves the tenant for an inbound request.
// Implementations combine one or more Selectors with a lookup strategy.
type TenantResolver interface {
	// Resolve returns the Tenant associated with the request.
	// Returns the zero Tenant and no error when no tenant is found.
	Resolve(ctx context.Context, r *http.Request) (Tenant, error)
}
