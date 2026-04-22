package interfaces

import "fmt"

// TenantRegistry persists and retrieves Tenant records.
type TenantRegistry interface {
	// Ensure creates the tenant if it does not exist, or returns the existing one.
	Ensure(spec TenantSpec) (Tenant, error)

	// GetByID looks up a tenant by its opaque ID.
	GetByID(id string) (Tenant, error)

	// GetByDomain looks up a tenant by one of its associated domains.
	GetByDomain(domain string) (Tenant, error)

	// GetBySlug looks up a tenant by its unique URL slug.
	GetBySlug(slug string) (Tenant, error)

	// List returns tenants matching the filter (all tenants if filter is zero).
	List(filter TenantFilter) ([]Tenant, error)

	// Update applies a partial patch to an existing tenant.
	Update(id string, patch TenantPatch) (Tenant, error)

	// Disable soft-deletes a tenant (sets IsActive=false).
	Disable(id string) error
}

// TenantSpec is the creation payload for a Tenant.
type TenantSpec struct {
	Name     string         `json:"name"`
	Slug     string         `json:"slug"`
	Domains  []string       `json:"domains,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Validate checks that TenantSpec has the required fields.
func (s TenantSpec) Validate() error {
	if s.Name == "" {
		return fmt.Errorf("%w: tenant name is required", ErrValidation)
	}
	if s.Slug == "" {
		return fmt.Errorf("%w: tenant slug is required", ErrValidation)
	}
	return nil
}

// TenantPatch is a partial update payload; nil pointer fields are left unchanged.
type TenantPatch struct {
	Name     *string        `json:"name,omitempty"`
	Domains  []string       `json:"domains,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
	IsActive *bool          `json:"is_active,omitempty"`
}

// TenantFilter constrains the results of TenantRegistry.List.
// All zero-value fields are treated as "no constraint".
type TenantFilter struct {
	ActiveOnly bool   `json:"active_only,omitempty"`
	Domain     string `json:"domain,omitempty"`
	Slug       string `json:"slug,omitempty"`
	Limit      int    `json:"limit,omitempty"`
	Offset     int    `json:"offset,omitempty"`
}
