package wftest

import "github.com/GoCodeAlone/workflow/plugin"

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

// WithPlugin loads an additional engine plugin into the harness before BuildFromConfig.
func WithPlugin(p plugin.EnginePlugin) Option {
	return func(h *Harness) { h.extraPlugins = append(h.extraPlugins, p) }
}

// WithServer starts a real HTTP listener backed by the engine's HTTP router.
// After harness creation, use h.BaseURL() to get the server URL.
// The server is stopped automatically via t.Cleanup.
func WithServer() Option {
	return func(h *Harness) { h.serverMode = true }
}
