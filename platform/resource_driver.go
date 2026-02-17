package platform

import (
	"context"
	"time"
)

// ResourceDriver handles CRUD lifecycle for a specific provider resource type.
// Each provider composes multiple drivers, one per resource type it supports.
// Drivers are responsible for the actual interaction with the provider API.
type ResourceDriver interface {
	// ResourceType returns the fully qualified resource type (e.g., "aws.eks_cluster").
	ResourceType() string

	// Create provisions a new resource with the given properties.
	// Returns the resource output when provisioning is complete.
	Create(ctx context.Context, name string, properties map[string]any) (*ResourceOutput, error)

	// Read fetches the current state of an existing resource from the provider.
	Read(ctx context.Context, name string) (*ResourceOutput, error)

	// Update modifies an existing resource to match the desired properties.
	// Both current and desired property maps are provided for diffing.
	Update(ctx context.Context, name string, current, desired map[string]any) (*ResourceOutput, error)

	// Delete removes a resource from the provider.
	Delete(ctx context.Context, name string) error

	// HealthCheck returns the health status of a managed resource.
	HealthCheck(ctx context.Context, name string) (*HealthStatus, error)

	// Scale adjusts resource scaling parameters if the resource type supports it.
	// Returns ErrNotScalable if the resource type does not support scaling.
	Scale(ctx context.Context, name string, scaleParams map[string]any) (*ResourceOutput, error)

	// Diff compares the desired properties with the actual provider state and
	// returns the list of field differences.
	Diff(ctx context.Context, name string, desired map[string]any) ([]DiffEntry, error)
}

// HealthStatus represents the health of a managed resource as reported
// by the provider.
type HealthStatus struct {
	// Status is the health state: "healthy", "unhealthy", "degraded", or "unknown".
	Status string `json:"status"`

	// Message is a human-readable description of the health state.
	Message string `json:"message"`

	// Details contains provider-specific health check details.
	Details map[string]any `json:"details,omitempty"`

	// CheckedAt is when the health check was performed.
	CheckedAt time.Time `json:"checkedAt"`
}
