package deploy

import "time"

// DeployArtifacts holds the generated deployment artifacts.
type DeployArtifacts struct {
	// Target is the deploy target name that generated these artifacts.
	Target string `json:"target"`
	// AppName is the application name.
	AppName string `json:"appName"`
	// Namespace is the target namespace/environment.
	Namespace string `json:"namespace"`
	// Files maps relative file paths to their content.
	Files map[string][]byte `json:"files,omitempty"`
	// Objects holds structured deployment objects (e.g., k8s unstructured objects).
	Objects []any `json:"-"`
	// Metadata holds target-specific metadata.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// DeployResult captures the outcome of a deploy apply operation.
type DeployResult struct {
	// Status is the deployment status ("success", "failed", "in_progress").
	Status string `json:"status"`
	// Message is a human-readable description of the result.
	Message string `json:"message"`
	// StartedAt is when the deployment started.
	StartedAt time.Time `json:"startedAt"`
	// CompletedAt is when the deployment finished.
	CompletedAt time.Time `json:"completedAt"`
	// Resources lists the resources that were created or updated.
	Resources []DeployedResource `json:"resources,omitempty"`
}

// DeployedResource describes a single deployed resource.
type DeployedResource struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Status    string `json:"status"`
}

// DeployStatus represents the current state of a deployment.
type DeployStatus struct {
	// AppName is the application name.
	AppName string `json:"appName"`
	// Namespace is the deployment namespace.
	Namespace string `json:"namespace"`
	// Phase is the overall deployment phase ("Running", "Pending", "Failed", "Unknown").
	Phase string `json:"phase"`
	// Ready is the number of ready replicas.
	Ready int `json:"ready"`
	// Desired is the desired number of replicas.
	Desired int `json:"desired"`
	// Message is additional status information.
	Message string `json:"message,omitempty"`
	// Resources lists individual resource statuses.
	Resources []ResourceStatus `json:"resources,omitempty"`
}

// ResourceStatus describes the status of an individual deployed resource.
type ResourceStatus struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
}

// ApplyOpts configures how Apply behaves.
type ApplyOpts struct {
	// DryRun simulates the apply without making changes.
	DryRun bool `json:"dryRun,omitempty"`
	// Wait blocks until the deployment is complete or times out.
	Wait bool `json:"wait,omitempty"`
	// WaitTimeout is the maximum time to wait for deployment completion.
	WaitTimeout time.Duration `json:"waitTimeout,omitempty"`
	// Force deletes and recreates resources that cannot be updated.
	Force bool `json:"force,omitempty"`
	// FieldManager is the field manager name for server-side apply.
	FieldManager string `json:"fieldManager,omitempty"`
}

// LogOpts configures how Logs behaves.
type LogOpts struct {
	// Container filters logs to a specific container name.
	Container string `json:"container,omitempty"`
	// Follow streams logs as they are produced.
	Follow bool `json:"follow,omitempty"`
	// TailLines limits output to the last N lines.
	TailLines int64 `json:"tailLines,omitempty"`
	// Since only shows logs newer than this duration.
	Since time.Duration `json:"since,omitempty"`
	// Previous shows logs from previous container instances.
	Previous bool `json:"previous,omitempty"`
}
