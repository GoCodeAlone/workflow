package interfaces

import "context"

// ResourceDriver handles CRUD for a single resource type within a provider.
type ResourceDriver interface {
	Create(ctx context.Context, spec ResourceSpec) (*ResourceOutput, error)
	Read(ctx context.Context, ref ResourceRef) (*ResourceOutput, error)
	Update(ctx context.Context, ref ResourceRef, spec ResourceSpec) (*ResourceOutput, error)
	Delete(ctx context.Context, ref ResourceRef) error
	Diff(ctx context.Context, desired ResourceSpec, current *ResourceOutput) (*DiffResult, error)
	HealthCheck(ctx context.Context, ref ResourceRef) (*HealthResult, error)
	Scale(ctx context.Context, ref ResourceRef, replicas int) (*ResourceOutput, error)
	// SensitiveKeys returns output keys whose values should be masked in logs and plan output.
	SensitiveKeys() []string
}

// ResourceOutput is the concrete output of a provisioned or read resource.
type ResourceOutput struct {
	Name       string          `json:"name"`
	Type       string          `json:"type"`
	ProviderID string          `json:"provider_id"`
	Outputs    map[string]any  `json:"outputs"`             // IPs, endpoints, connection strings
	Sensitive  map[string]bool `json:"sensitive,omitempty"` // keys whose values are sensitive
	Status     string          `json:"status"`
}

// DiffResult summarises the differences between desired and actual resource state.
type DiffResult struct {
	NeedsUpdate  bool          `json:"needs_update"`
	NeedsReplace bool          `json:"needs_replace"`
	Changes      []FieldChange `json:"changes"`
}

// FieldChange describes a single field-level difference.
type FieldChange struct {
	Path     string `json:"path"`
	Old      any    `json:"old"`
	New      any    `json:"new"`
	ForceNew bool   `json:"force_new"` // change requires resource replacement
}

// HealthResult is the outcome of a health check for a resource.
type HealthResult struct {
	Healthy bool   `json:"healthy"`
	Message string `json:"message,omitempty"`
}
