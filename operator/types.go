package operator

import (
	"time"
)

// WorkflowDefinitionSpec defines a workflow to be deployed.
type WorkflowDefinitionSpec struct {
	Name       string       `json:"name" yaml:"name"`
	Version    int          `json:"version" yaml:"version"`
	ConfigYAML string       `json:"configYAML" yaml:"configYAML"`
	Replicas   int          `json:"replicas,omitempty" yaml:"replicas,omitempty"`
	Resources  ResourceSpec `json:"resources,omitempty" yaml:"resources,omitempty"`
	Env        string       `json:"env,omitempty" yaml:"env,omitempty"` // dev/staging/prod
}

// ResourceSpec defines CPU and memory resource requests and limits.
type ResourceSpec struct {
	CPURequest    string `json:"cpuRequest,omitempty" yaml:"cpuRequest,omitempty"`
	CPULimit      string `json:"cpuLimit,omitempty" yaml:"cpuLimit,omitempty"`
	MemoryRequest string `json:"memoryRequest,omitempty" yaml:"memoryRequest,omitempty"`
	MemoryLimit   string `json:"memoryLimit,omitempty" yaml:"memoryLimit,omitempty"`
}

// WorkflowDefinitionStatus reflects the observed state of a WorkflowDefinition.
type WorkflowDefinitionStatus struct {
	Phase           string    `json:"phase"`         // Pending, Running, Failed, Terminated
	Replicas        int       `json:"replicas"`      // desired replicas from spec
	ReadyReplicas   int       `json:"readyReplicas"` // replicas currently ready
	Message         string    `json:"message,omitempty"`
	LastTransition  time.Time `json:"lastTransition"`
	ObservedVersion int       `json:"observedVersion"` // last reconciled spec version
}

// WorkflowDefinition is the CRD for a deployed workflow.
type WorkflowDefinition struct {
	APIVersion string                   `json:"apiVersion" yaml:"apiVersion"`
	Kind       string                   `json:"kind" yaml:"kind"`
	Metadata   ObjectMeta               `json:"metadata" yaml:"metadata"`
	Spec       WorkflowDefinitionSpec   `json:"spec" yaml:"spec"`
	Status     WorkflowDefinitionStatus `json:"status" yaml:"status"`
}

// ObjectMeta contains standard Kubernetes object metadata.
type ObjectMeta struct {
	Name        string            `json:"name" yaml:"name"`
	Namespace   string            `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Labels      map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty" yaml:"annotations,omitempty"`
}

// Phase constants for WorkflowDefinitionStatus.
const (
	PhasePending    = "Pending"
	PhaseRunning    = "Running"
	PhaseFailed     = "Failed"
	PhaseTerminated = "Terminated"
)

// definitionKey returns a unique key for a WorkflowDefinition in the form "namespace/name".
func definitionKey(namespace, name string) string {
	if namespace == "" {
		namespace = "default"
	}
	return namespace + "/" + name
}
