package deploy

import (
	"context"
	"time"
)

// DeploymentStrategy defines the interface for workflow deployment strategies.
type DeploymentStrategy interface {
	// Name returns the strategy identifier (e.g., "rolling", "blue-green", "canary").
	Name() string

	// Validate checks the strategy-specific configuration.
	Validate(config map[string]any) error

	// Execute runs the deployment according to the plan.
	Execute(ctx context.Context, plan *DeploymentPlan) (*DeploymentResult, error)
}

// DeploymentPlan describes a deployment to execute.
type DeploymentPlan struct {
	WorkflowID  string         `json:"workflow_id"`
	FromVersion int            `json:"from_version"`
	ToVersion   int            `json:"to_version"`
	Strategy    string         `json:"strategy"` // "rolling", "blue-green", "canary"
	Config      map[string]any `json:"config"`
}

// DeploymentResult captures the outcome of a deployment.
type DeploymentResult struct {
	Status      string    `json:"status"` // "success", "failed", "rolled_back"
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
	Message     string    `json:"message"`
	RolledBack  bool      `json:"rolled_back"`
}
