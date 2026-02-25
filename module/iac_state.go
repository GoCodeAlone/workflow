package module

// IaCState tracks the state of an infrastructure resource.
type IaCState struct {
	ResourceID   string         `json:"resource_id"`
	ResourceType string         `json:"resource_type"` // e.g. "kubernetes", "ecs"
	Provider     string         `json:"provider"`      // e.g. "aws", "gcp", "local"
	Status       string         `json:"status"`        // planned, provisioning, active, destroying, destroyed, error
	Outputs      map[string]any `json:"outputs"`       // provider-specific outputs
	Config       map[string]any `json:"config"`        // the config used to provision
	CreatedAt    string         `json:"created_at"`
	UpdatedAt    string         `json:"updated_at"`
	Error        string         `json:"error,omitempty"`
}

// IaCStateStore is the interface for IaC state persistence backends.
type IaCStateStore interface {
	// GetState retrieves a state record by resource ID. Returns nil, nil when not found.
	GetState(resourceID string) (*IaCState, error)

	// SaveState inserts or replaces a state record.
	SaveState(state *IaCState) error

	// ListStates returns all state records matching the provided key=value filter.
	// Pass an empty map to return all records.
	ListStates(filter map[string]string) ([]*IaCState, error)

	// DeleteState removes a state record by resource ID.
	DeleteState(resourceID string) error

	// Lock acquires an exclusive lock for the given resource ID.
	// Returns an error if the resource is already locked.
	Lock(resourceID string) error

	// Unlock releases the lock for the given resource ID.
	Unlock(resourceID string) error
}
