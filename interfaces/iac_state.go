package interfaces

import (
	"context"
	"time"
)

// IaCLockHandle is returned by IaCStateStore.Lock and is used to release the lock.
type IaCLockHandle interface {
	Unlock(ctx context.Context) error
}

// IaCStateStore provides persistent state tracking for managed resources.
type IaCStateStore interface {
	SaveResource(ctx context.Context, state ResourceState) error
	GetResource(ctx context.Context, name string) (*ResourceState, error)
	ListResources(ctx context.Context) ([]ResourceState, error)
	DeleteResource(ctx context.Context, name string) error

	SavePlan(ctx context.Context, plan IaCPlan) error
	GetPlan(ctx context.Context, id string) (*IaCPlan, error)

	Lock(ctx context.Context, resource string, ttl time.Duration) (IaCLockHandle, error)

	Close() error
}

// ResourceState is the persisted state record for a single managed resource.
type ResourceState struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Type           string         `json:"type"`
	Provider       string         `json:"provider"`
	ProviderRef    string         `json:"provider_ref,omitempty"`
	ProviderID     string         `json:"provider_id"`
	ConfigHash     string         `json:"config_hash"`
	AppliedConfig  map[string]any `json:"applied_config"`
	Outputs        map[string]any `json:"outputs"`
	Dependencies   []string       `json:"dependencies"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	LastDriftCheck time.Time      `json:"last_drift_check,omitempty"`
}

// IaCPlan is the complete execution plan for a set of infrastructure changes.
type IaCPlan struct {
	ID        string       `json:"id"`
	Actions   []PlanAction `json:"actions"`
	CreatedAt time.Time    `json:"created_at"`
}

// PlanAction is a single planned change within an IaCPlan.
type PlanAction struct {
	Action   string         `json:"action"` // create, update, replace, delete
	Resource ResourceSpec   `json:"resource"`
	Current  *ResourceState `json:"current,omitempty"`
	Changes  []FieldChange  `json:"changes,omitempty"`
}

// ApplyResult summarises the outcome of applying a plan.
type ApplyResult struct {
	PlanID    string           `json:"plan_id"`
	Resources []ResourceOutput `json:"resources"`
	Errors    []ActionError    `json:"errors,omitempty"`
}

// DestroyResult summarises the outcome of a destroy operation.
type DestroyResult struct {
	Destroyed []string      `json:"destroyed"`
	Errors    []ActionError `json:"errors,omitempty"`
}

// ActionError captures a resource-level error during apply or destroy.
type ActionError struct {
	Resource string `json:"resource"`
	Action   string `json:"action"`
	Error    string `json:"error"`
}
