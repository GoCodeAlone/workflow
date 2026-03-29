package config

// SecurityConfig defines security policies for the application.
type SecurityConfig struct {
	TLS      *SecurityTLSConfig      `json:"tls,omitempty" yaml:"tls,omitempty"`
	Network  *SecurityNetworkConfig  `json:"network,omitempty" yaml:"network,omitempty"`
	Identity *SecurityIdentityConfig `json:"identity,omitempty" yaml:"identity,omitempty"`
	Runtime  *SecurityRuntimeConfig  `json:"runtime,omitempty" yaml:"runtime,omitempty"`
	Scanning *SecurityScanningConfig `json:"scanning,omitempty" yaml:"scanning,omitempty"`
}

// SecurityTLSConfig defines TLS requirements.
type SecurityTLSConfig struct {
	Internal   bool   `json:"internal,omitempty" yaml:"internal,omitempty"`
	External   bool   `json:"external,omitempty" yaml:"external,omitempty"`
	Provider   string `json:"provider,omitempty" yaml:"provider,omitempty"`
	MinVersion string `json:"minVersion,omitempty" yaml:"minVersion,omitempty"`
}

// SecurityNetworkConfig defines network isolation policy.
type SecurityNetworkConfig struct {
	DefaultPolicy string `json:"defaultPolicy,omitempty" yaml:"defaultPolicy,omitempty"`
}

// SecurityIdentityConfig defines service identity management.
type SecurityIdentityConfig struct {
	Provider   string `json:"provider,omitempty" yaml:"provider,omitempty"`
	PerService bool   `json:"perService,omitempty" yaml:"perService,omitempty"`
}

// SecurityRuntimeConfig defines container runtime security.
type SecurityRuntimeConfig struct {
	ReadOnlyFilesystem bool     `json:"readOnlyFilesystem,omitempty" yaml:"readOnlyFilesystem,omitempty"`
	NoNewPrivileges    bool     `json:"noNewPrivileges,omitempty" yaml:"noNewPrivileges,omitempty"`
	RunAsNonRoot       bool     `json:"runAsNonRoot,omitempty" yaml:"runAsNonRoot,omitempty"`
	DropCapabilities   []string `json:"dropCapabilities,omitempty" yaml:"dropCapabilities,omitempty"`
	AddCapabilities    []string `json:"addCapabilities,omitempty" yaml:"addCapabilities,omitempty"`
}

// SecurityScanningConfig defines automated security scanning.
type SecurityScanningConfig struct {
	ContainerScan  bool `json:"containerScan,omitempty" yaml:"containerScan,omitempty"`
	DependencyScan bool `json:"dependencyScan,omitempty" yaml:"dependencyScan,omitempty"`
	SAST           bool `json:"sast,omitempty" yaml:"sast,omitempty"`
}
