package config

// SecretStoreConfig defines a named secret storage backend.
type SecretStoreConfig struct {
	Provider string         `json:"provider" yaml:"provider"`
	Config   map[string]any `json:"config,omitempty" yaml:"config,omitempty"`
}

// SecretsConfig defines secret management for the application.
type SecretsConfig struct {
	// DefaultStore names the store (from secretStores) to use when a secret has no explicit store.
	DefaultStore string `json:"defaultStore,omitempty" yaml:"defaultStore,omitempty"`
	// Entries lists the secrets this application requires.
	Entries []SecretEntry `json:"entries,omitempty" yaml:"entries,omitempty"`
	// Provider is the legacy single-store provider name. Kept for backward compatibility.
	// Prefer secretStores + defaultStore for new configs.
	Provider string                `json:"provider,omitempty" yaml:"provider,omitempty"`
	Config   map[string]any        `json:"config,omitempty" yaml:"config,omitempty"`
	Rotation *SecretsRotationConfig `json:"rotation,omitempty" yaml:"rotation,omitempty"`
}

// SecretsRotationConfig defines default rotation policy.
type SecretsRotationConfig struct {
	Enabled  bool   `json:"enabled" yaml:"enabled"`
	Interval string `json:"interval,omitempty" yaml:"interval,omitempty"`
	Strategy string `json:"strategy,omitempty" yaml:"strategy,omitempty"`
}

// SecretEntry declares a single secret the application needs.
type SecretEntry struct {
	Name        string                 `json:"name" yaml:"name"`
	Description string                 `json:"description,omitempty" yaml:"description,omitempty"`
	// Store names the store (from secretStores) this secret lives in.
	// Overrides defaultStore and environment secretsStoreOverride.
	Store    string                 `json:"store,omitempty" yaml:"store,omitempty"`
	Rotation *SecretsRotationConfig `json:"rotation,omitempty" yaml:"rotation,omitempty"`
}
