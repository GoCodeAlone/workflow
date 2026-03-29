package config

// ServiceConfig defines a single service within a multi-service application.
type ServiceConfig struct {
	Description string         `json:"description,omitempty" yaml:"description,omitempty"`
	Binary      string         `json:"binary,omitempty" yaml:"binary,omitempty"`
	Scaling     *ScalingConfig `json:"scaling,omitempty" yaml:"scaling,omitempty"`
	Modules     []ModuleConfig `json:"modules,omitempty" yaml:"modules,omitempty"`
	Workflows   map[string]any `json:"workflows,omitempty" yaml:"workflows,omitempty"`
	Pipelines   map[string]any `json:"pipelines,omitempty" yaml:"pipelines,omitempty"`
	Triggers    map[string]any `json:"triggers,omitempty" yaml:"triggers,omitempty"`
	Plugins     []string       `json:"plugins,omitempty" yaml:"plugins,omitempty"`
	Expose      []ExposeConfig `json:"expose,omitempty" yaml:"expose,omitempty"`
}

// ScalingConfig defines how a service scales.
type ScalingConfig struct {
	Replicas int    `json:"replicas,omitempty" yaml:"replicas,omitempty"`
	Min      int    `json:"min,omitempty" yaml:"min,omitempty"`
	Max      int    `json:"max,omitempty" yaml:"max,omitempty"`
	Metric   string `json:"metric,omitempty" yaml:"metric,omitempty"`
	Target   int    `json:"target,omitempty" yaml:"target,omitempty"`
}

// ExposeConfig defines a port that the service exposes.
type ExposeConfig struct {
	Port     int    `json:"port" yaml:"port"`
	Protocol string `json:"protocol,omitempty" yaml:"protocol,omitempty"`
}

// MeshConfig defines inter-service communication.
type MeshConfig struct {
	Transport string            `json:"transport,omitempty" yaml:"transport,omitempty"`
	Discovery string            `json:"discovery,omitempty" yaml:"discovery,omitempty"`
	NATS      *MeshNATSConfig   `json:"nats,omitempty" yaml:"nats,omitempty"`
	Routes    []MeshRouteConfig `json:"routes,omitempty" yaml:"routes,omitempty"`
}

// MeshNATSConfig holds NATS-specific mesh configuration.
type MeshNATSConfig struct {
	URL       string `json:"url" yaml:"url"`
	ClusterID string `json:"clusterId,omitempty" yaml:"clusterId,omitempty"`
}

// MeshRouteConfig declares a communication path between services.
type MeshRouteConfig struct {
	From     string `json:"from" yaml:"from"`
	To       string `json:"to" yaml:"to"`
	Via      string `json:"via" yaml:"via"`
	Subject  string `json:"subject,omitempty" yaml:"subject,omitempty"`
	Endpoint string `json:"endpoint,omitempty" yaml:"endpoint,omitempty"`
}
