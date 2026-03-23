package wftest

// Option configures a Harness.
type Option func(*Harness)

// WithYAML configures the harness with inline YAML config.
func WithYAML(yaml string) Option {
	return func(h *Harness) { h.yamlConfig = yaml }
}

// WithConfig loads config from a YAML file path.
func WithConfig(path string) Option {
	return func(h *Harness) { h.configPath = path }
}
