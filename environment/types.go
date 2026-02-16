package environment

import "time"

// Status constants for environment lifecycle.
const (
	StatusActive         = "active"
	StatusProvisioning   = "provisioning"
	StatusError          = "error"
	StatusDecommissioned = "decommissioned"
)

// Environment represents a deployment target (e.g., staging, production)
// associated with a workflow and cloud provider.
type Environment struct {
	ID         string            `json:"id"`
	WorkflowID string            `json:"workflow_id"`
	Name       string            `json:"name"`
	Provider   string            `json:"provider"`
	Region     string            `json:"region"`
	Config     map[string]any    `json:"config"`
	Secrets    map[string]string `json:"secrets,omitempty"`
	Status     string            `json:"status"`
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
}

// Filter specifies optional criteria for listing environments.
type Filter struct {
	WorkflowID string
	Provider   string
	Status     string
}

// ConnectionTestResult holds the outcome of a connectivity test.
type ConnectionTestResult struct {
	Success bool          `json:"success"`
	Message string        `json:"message"`
	Latency time.Duration `json:"latency"`
}
