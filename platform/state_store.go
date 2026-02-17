package platform

import (
	"context"
	"time"
)

// StateStore manages the persistent state of provisioned resources.
// Each provider has its own state store, and there is an aggregate
// layer that spans across providers. State is partitioned by context path.
type StateStore interface {
	// SaveResource persists the state of a resource within a context path.
	SaveResource(ctx context.Context, contextPath string, output *ResourceOutput) error

	// GetResource retrieves a resource's state by context path and resource name.
	GetResource(ctx context.Context, contextPath, resourceName string) (*ResourceOutput, error)

	// ListResources returns all resources in a context path.
	ListResources(ctx context.Context, contextPath string) ([]*ResourceOutput, error)

	// DeleteResource removes a resource from state.
	DeleteResource(ctx context.Context, contextPath, resourceName string) error

	// SavePlan persists an execution plan.
	SavePlan(ctx context.Context, plan *Plan) error

	// GetPlan retrieves an execution plan by its ID.
	GetPlan(ctx context.Context, planID string) (*Plan, error)

	// ListPlans lists plans for a context path, ordered by creation time descending.
	// The limit parameter controls the maximum number of plans returned.
	ListPlans(ctx context.Context, contextPath string, limit int) ([]*Plan, error)

	// Lock acquires an advisory lock for a context path to prevent concurrent
	// modifications to the same infrastructure. The TTL controls the maximum
	// lock duration.
	Lock(ctx context.Context, contextPath string, ttl time.Duration) (LockHandle, error)

	// Dependencies returns dependency references for resources that depend on
	// the given resource.
	Dependencies(ctx context.Context, contextPath, resourceName string) ([]DependencyRef, error)

	// AddDependency records a cross-resource or cross-tier dependency.
	AddDependency(ctx context.Context, dep DependencyRef) error
}

// LockHandle represents an advisory lock on a context path.
// Locks prevent concurrent modifications and must be explicitly released.
type LockHandle interface {
	// Unlock releases the advisory lock.
	Unlock(ctx context.Context) error

	// Refresh extends the lock TTL to prevent expiration during long operations.
	Refresh(ctx context.Context, ttl time.Duration) error
}

// DependencyRef tracks cross-resource and cross-tier dependencies.
// These are used for impact analysis when upstream resources change.
type DependencyRef struct {
	// SourceContext is the context path of the depended-upon resource.
	SourceContext string `json:"sourceContext"`

	// SourceResource is the name of the depended-upon resource.
	SourceResource string `json:"sourceResource"`

	// TargetContext is the context path of the dependent resource.
	TargetContext string `json:"targetContext"`

	// TargetResource is the name of the dependent resource.
	TargetResource string `json:"targetResource"`

	// Type is the dependency type: "hard" (must exist) or "soft" (optional).
	Type string `json:"type"`
}
