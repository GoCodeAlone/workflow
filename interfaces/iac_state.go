package interfaces

import "time"

// IaCStateStore provides persistent state tracking for managed resources.
type IaCStateStore interface {
	SaveResource(state ResourceState) error
	GetResource(name string) (*ResourceState, error)
	ListResources() ([]ResourceState, error)
	DeleteResource(name string) error

	SavePlan(plan Plan) error
	GetPlan(id string) (*Plan, error)

	Lock(resource string) error
	Unlock(resource string) error

	Close() error
}

// ResourceState is the persisted state record for a single managed resource.
type ResourceState struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Type           string         `json:"type"`
	Provider       string         `json:"provider"`
	ProviderID     string         `json:"provider_id"`
	ConfigHash     string         `json:"config_hash"`
	AppliedConfig  map[string]any `json:"applied_config"`
	Outputs        map[string]any `json:"outputs"`
	Dependencies   []string       `json:"dependencies"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	LastDriftCheck time.Time      `json:"last_drift_check,omitempty"`
}

// Plan is the complete execution plan for a set of infrastructure changes.
type Plan struct {
	ID        string       `json:"id"`
	Actions   []PlanAction `json:"actions"`
	CreatedAt time.Time    `json:"created_at"`
}

// PlanAction is a single planned change within a Plan.
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
