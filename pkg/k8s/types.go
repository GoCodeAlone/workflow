package k8s

import "time"

// StatusResult describes the overall status of a deployed application.
type StatusResult struct {
	AppName   string      `json:"appName"`
	Namespace string      `json:"namespace"`
	Phase     string      `json:"phase"`
	Ready     int         `json:"ready"`
	Desired   int         `json:"desired"`
	Pods      []PodStatus `json:"pods,omitempty"`
	Message   string      `json:"message,omitempty"`
	UpdatedAt time.Time   `json:"updatedAt"`
}

// PodStatus describes the status of a single pod.
type PodStatus struct {
	Name       string            `json:"name"`
	Phase      string            `json:"phase"`
	Ready      bool              `json:"ready"`
	Restarts   int32             `json:"restarts"`
	Age        time.Duration     `json:"age"`
	Containers []ContainerStatus `json:"containers,omitempty"`
}

// ContainerStatus describes the status of a container within a pod.
type ContainerStatus struct {
	Name         string `json:"name"`
	Ready        bool   `json:"ready"`
	RestartCount int32  `json:"restartCount"`
	State        string `json:"state"`
	Reason       string `json:"reason,omitempty"`
	Message      string `json:"message,omitempty"`
}
