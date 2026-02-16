package provider

import "time"

// DeployRequest describes a deployment to execute on a cloud provider.
type DeployRequest struct {
	Environment string            `json:"environment"`
	Strategy    string            `json:"strategy"` // "rolling", "blue-green", "canary"
	Image       string            `json:"image"`
	Config      map[string]any    `json:"config"`
	HealthCheck HealthCheckConfig `json:"health_check"`
}

// DeployResult captures the outcome of initiating a deployment.
type DeployResult struct {
	DeployID    string    `json:"deploy_id"`
	Status      string    `json:"status"` // "pending", "in_progress", "succeeded", "failed"
	Message     string    `json:"message"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
}

// DeployStatus describes the current state of an in-progress or completed deployment.
type DeployStatus struct {
	DeployID  string           `json:"deploy_id"`
	Status    string           `json:"status"`
	Progress  int              `json:"progress"` // 0-100
	Message   string           `json:"message"`
	Instances []InstanceStatus `json:"instances"`
}

// RegistryAuth holds credentials for authenticating with a container registry.
type RegistryAuth struct {
	Username      string `json:"username"`
	Password      string `json:"password"`
	Token         string `json:"token"`
	ServerAddress string `json:"server_address"`
}

// ImageTag describes a container image in a registry.
type ImageTag struct {
	Repository string    `json:"repository"`
	Tag        string    `json:"tag"`
	Digest     string    `json:"digest"`
	Size       int64     `json:"size"`
	PushedAt   time.Time `json:"pushed_at"`
}

// ConnectionResult describes the outcome of a connectivity test.
type ConnectionResult struct {
	Success bool           `json:"success"`
	Message string         `json:"message"`
	Latency time.Duration  `json:"latency"`
	Details map[string]any `json:"details"`
}

// Metrics holds resource and request metrics for a deployment.
type Metrics struct {
	CPU           float64        `json:"cpu"`
	Memory        float64        `json:"memory"`
	RequestCount  int64          `json:"request_count"`
	ErrorRate     float64        `json:"error_rate"`
	Latency       time.Duration  `json:"latency"`
	CustomMetrics map[string]any `json:"custom_metrics"`
}

// HealthCheckConfig defines health check parameters for a deployment.
type HealthCheckConfig struct {
	Path               string        `json:"path"`
	Interval           time.Duration `json:"interval"`
	Timeout            time.Duration `json:"timeout"`
	HealthyThreshold   int           `json:"healthy_threshold"`
	UnhealthyThreshold int           `json:"unhealthy_threshold"`
}

// InstanceStatus describes the state of an individual instance within a deployment.
type InstanceStatus struct {
	ID      string `json:"id"`
	Status  string `json:"status"` // "running", "pending", "stopped", "failed"
	Health  string `json:"health"` // "healthy", "unhealthy", "unknown"
	Address string `json:"address"`
}
