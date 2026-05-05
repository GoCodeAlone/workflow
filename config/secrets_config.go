package config

// SecretStoreConfig defines a named secret storage backend.
type SecretStoreConfig struct {
	Provider string         `json:"provider" yaml:"provider"`
	Config   map[string]any `json:"config,omitempty" yaml:"config,omitempty"`
}

// SecretGen describes a secret to generate and store during bootstrap.
// It lives here (rather than cmd/wfctl) so that WorkflowConfig.Secrets.Generate
// is preserved when a config round-trips through config.LoadFromFile / marshal.
type SecretGen struct {
	Key    string `json:"key" yaml:"key"`
	Type   string `json:"type" yaml:"type"`                         // e.g. "random_hex", "provider_credential"
	Length int    `json:"length,omitempty" yaml:"length,omitempty"` // for random generators
	Source string `json:"source,omitempty" yaml:"source,omitempty"` // for provider_credential
	Name   string `json:"name,omitempty" yaml:"name,omitempty"`     // optional human-readable label
}

// SecretsConfig defines secret management for the application.
type SecretsConfig struct {
	// DefaultStore names the store (from secretStores) to use when a secret has no explicit store.
	DefaultStore string `json:"defaultStore,omitempty" yaml:"defaultStore,omitempty"`
	// Entries lists the secrets this application requires.
	Entries []SecretEntry `json:"entries,omitempty" yaml:"entries,omitempty"`
	// Provider is the legacy single-store provider name. Kept for backward compatibility.
	// Prefer secretStores + defaultStore for new configs.
	Provider string                 `json:"provider,omitempty" yaml:"provider,omitempty"`
	Config   map[string]any         `json:"config,omitempty" yaml:"config,omitempty"`
	Rotation *SecretsRotationConfig `json:"rotation,omitempty" yaml:"rotation,omitempty"`
	// Generate lists secrets to create during `wfctl infra bootstrap`.
	Generate []SecretGen `json:"generate,omitempty" yaml:"generate,omitempty"`
}

// SecretsRotationConfig defines default rotation policy.
type SecretsRotationConfig struct {
	Enabled  bool   `json:"enabled" yaml:"enabled"`
	Interval string `json:"interval,omitempty" yaml:"interval,omitempty"`
	Strategy string `json:"strategy,omitempty" yaml:"strategy,omitempty"`
}

// SecretEntry declares a single secret the application needs.
type SecretEntry struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	// Store names the store (from secretStores) this secret lives in.
	// Overrides defaultStore and environment secretsStoreOverride.
	Store    string                 `json:"store,omitempty" yaml:"store,omitempty"`
	Rotation *SecretsRotationConfig `json:"rotation,omitempty" yaml:"rotation,omitempty"`
}
