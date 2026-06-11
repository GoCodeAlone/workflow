package config

// VariablesConfig defines non-sensitive configuration values an application
// expects operators to set in an external variable provider such as GitHub
// Actions variables. Credential material belongs in SecretsConfig instead.
type VariablesConfig struct {
	// Entries lists the non-secret variables this application requires.
	Entries []VariableEntry `json:"entries,omitempty" yaml:"entries,omitempty"`
}

// VariableEntry declares a single non-sensitive variable the application needs.
type VariableEntry struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Required    bool   `json:"required,omitempty" yaml:"required,omitempty"`
	Default     string `json:"default,omitempty" yaml:"default,omitempty"`
}
