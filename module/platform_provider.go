package module

// PlatformProvider is implemented by infrastructure modules that manage cloud resources.
// It provides a generic lifecycle interface for plan → apply → status → destroy.
type PlatformProvider interface {
	Plan() (*PlatformPlan, error)
	Apply() (*PlatformResult, error)
	Status() (any, error)
	Destroy() error
}

// PlatformPlan describes the changes a platform module intends to make.
type PlatformPlan struct {
	Provider string           `json:"provider"`
	Resource string           `json:"resource"`
	Actions  []PlatformAction `json:"actions"`
}

// PlatformAction describes a single change within a plan.
type PlatformAction struct {
	Type     string `json:"type"` // create, update, delete, noop
	Resource string `json:"resource"`
	Detail   string `json:"detail"`
}

// PlatformResult is returned from Apply.
type PlatformResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	State   any    `json:"state"`
}
