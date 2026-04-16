package config

// EnvironmentConfig defines a deployment environment with its provider and overrides.
type EnvironmentConfig struct {
	Provider        string            `json:"provider" yaml:"provider"`
	Region          string            `json:"region,omitempty" yaml:"region,omitempty"`
	EnvVars         map[string]string `json:"envVars,omitempty" yaml:"envVars,omitempty"`
	SecretsProvider string            `json:"secretsProvider,omitempty" yaml:"secretsProvider,omitempty"`
	SecretsPrefix   string            `json:"secretsPrefix,omitempty" yaml:"secretsPrefix,omitempty"`
	// SecretsStoreOverride forces all secrets in this environment to use a specific named store.
	// Overrides defaultStore but is itself overridden by a per-secret Store field.
	SecretsStoreOverride string          `json:"secretsStoreOverride,omitempty" yaml:"secretsStoreOverride,omitempty"`
	ApprovalRequired     bool            `json:"approvalRequired,omitempty" yaml:"approvalRequired,omitempty"`
	Exposure             *ExposureConfig `json:"exposure,omitempty" yaml:"exposure,omitempty"`
}

// ExposureConfig defines how a service is exposed to the network.
type ExposureConfig struct {
	Method           string                  `json:"method" yaml:"method"`
	Tailscale        *TailscaleConfig        `json:"tailscale,omitempty" yaml:"tailscale,omitempty"`
	CloudflareTunnel *CloudflareTunnelConfig `json:"cloudflareTunnel,omitempty" yaml:"cloudflareTunnel,omitempty"`
	PortForward      map[string]string       `json:"portForward,omitempty" yaml:"portForward,omitempty"`
}

// TailscaleConfig for Tailscale Funnel exposure.
type TailscaleConfig struct {
	Funnel   bool   `json:"funnel,omitempty" yaml:"funnel,omitempty"`
	Hostname string `json:"hostname,omitempty" yaml:"hostname,omitempty"`
}

// CloudflareTunnelConfig for Cloudflare Tunnel exposure.
type CloudflareTunnelConfig struct {
	TunnelName string `json:"tunnelName,omitempty" yaml:"tunnelName,omitempty"`
	Domain     string `json:"domain,omitempty" yaml:"domain,omitempty"`
}
