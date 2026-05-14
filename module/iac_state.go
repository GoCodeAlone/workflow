package module

import "context"

// IaCState tracks the state of an infrastructure resource.
type IaCState struct {
	ResourceID   string         `json:"resource_id"`
	ResourceType string         `json:"resource_type"` // e.g. "kubernetes", "ecs"
	Provider     string         `json:"provider"`      // e.g. "aws", "gcp", "local"
	ProviderRef  string         `json:"provider_ref,omitempty"`
	ProviderID   string         `json:"provider_id,omitempty"`
	ConfigHash   string         `json:"config_hash,omitempty"`
	Status       string         `json:"status"`  // planned, provisioning, active, destroying, destroyed, error
	Outputs      map[string]any `json:"outputs"` // provider-specific outputs
	Config       map[string]any `json:"config"`  // the config used to provision
	Dependencies []string       `json:"dependencies,omitempty"`
	CreatedAt    string         `json:"created_at"`
	UpdatedAt    string         `json:"updated_at"`
	Error        string         `json:"error,omitempty"`
}

// IaCStateStore is the interface for IaC state persistence backends.
type IaCStateStore interface {
	// GetState retrieves a state record by resource ID. Returns nil, nil when not found.
	GetState(ctx context.Context, resourceID string) (*IaCState, error)

	// SaveState inserts or replaces a state record.
	SaveState(ctx context.Context, state *IaCState) error

	// ListStates returns all state records matching the provided key=value filter.
	// Pass a nil or empty map to return all records — both are treated as "no
	// filter" (ranging over a nil map is valid Go, and most call sites pass nil).
	ListStates(ctx context.Context, filter map[string]string) ([]*IaCState, error)

	// DeleteState removes a state record by resource ID.
	DeleteState(ctx context.Context, resourceID string) error

	// Lock acquires an exclusive lock for the given resource ID.
	// Returns an error if the resource is already locked.
	Lock(ctx context.Context, resourceID string) error

	// Unlock releases the lock for the given resource ID.
	Unlock(ctx context.Context, resourceID string) error
}
