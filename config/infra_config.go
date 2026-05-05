package config

// InfraConfig is the "infra:" top-level section of a workflow config file.
// It lives in the config package so that WorkflowConfig.Infra is preserved
// when a config round-trips through LoadFromFile / marshal.
type InfraConfig struct {
	// AutoBootstrap controls whether `wfctl infra apply` automatically runs
	// `wfctl infra bootstrap` when no state backend exists yet.
	AutoBootstrap *bool `json:"auto_bootstrap,omitempty" yaml:"auto_bootstrap,omitempty"`
}
