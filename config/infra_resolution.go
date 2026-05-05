package config

// InfraEnvironmentResolution defines how an infrastructure resource is resolved
// in a specific environment. A strategy of "container" means run it as a local
// Docker container, "provision" means create it via a cloud provider, and
// "existing" means connect to an already-running instance.
type InfraEnvironmentResolution struct {
	// Strategy determines how the resource is obtained: container, provision, existing.
	Strategy string `json:"strategy" yaml:"strategy"`
	// DockerImage is used when Strategy is "container".
	DockerImage string `json:"dockerImage,omitempty" yaml:"dockerImage,omitempty"`
	// Port overrides the default service port when Strategy is "container".
	Port int `json:"port,omitempty" yaml:"port,omitempty"`
	// Provider names the cloud provider when Strategy is "provision".
	Provider string `json:"provider,omitempty" yaml:"provider,omitempty"`
	// Config holds provider-specific provisioning options.
	Config map[string]any `json:"config,omitempty" yaml:"config,omitempty"`
	// Connection holds connection details when Strategy is "existing".
	Connection *InfraConnectionConfig `json:"connection,omitempty" yaml:"connection,omitempty"`
}

// InfraConnectionConfig holds connection details for an existing infrastructure resource.
type InfraConnectionConfig struct {
	Host string `json:"host" yaml:"host"`
	Port int    `json:"port,omitempty" yaml:"port,omitempty"`
	Auth string `json:"auth,omitempty" yaml:"auth,omitempty"`
}
