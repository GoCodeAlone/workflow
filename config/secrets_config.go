package config

// SecretsConfig defines secret management for the application.
type SecretsConfig struct {
	Provider string                `json:"provider" yaml:"provider"`
	Config   map[string]any        `json:"config,omitempty" yaml:"config,omitempty"`
	Rotation *SecretsRotationConfig `json:"rotation,omitempty" yaml:"rotation,omitempty"`
	Entries  []SecretEntry         `json:"entries,omitempty" yaml:"entries,omitempty"`
}

// SecretsRotationConfig defines default rotation policy.
type SecretsRotationConfig struct {
	Enabled  bool   `json:"enabled" yaml:"enabled"`
	Interval string `json:"interval,omitempty" yaml:"interval,omitempty"`
	Strategy string `json:"strategy,omitempty" yaml:"strategy,omitempty"`
}

// SecretEntry declares a single secret the application needs.
type SecretEntry struct {
	Name        string                `json:"name" yaml:"name"`
	Description string                `json:"description,omitempty" yaml:"description,omitempty"`
	Rotation    *SecretsRotationConfig `json:"rotation,omitempty" yaml:"rotation,omitempty"`
}
