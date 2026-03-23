package bdd

import "github.com/GoCodeAlone/workflow/wftest"

// Config holds configuration for a BDD test run.
type Config struct {
	strict     bool
	globalOpts []wftest.Option
}

// Option configures a BDD test run.
type Option func(*Config)

// WithConfig sets a default config file path applied to every scenario's harness.
func WithConfig(path string) Option {
	return func(c *Config) {
		c.globalOpts = append(c.globalOpts, wftest.WithConfig(path))
	}
}

// WithYAML sets default inline YAML applied to every scenario's harness.
func WithYAML(yaml string) Option {
	return func(c *Config) {
		c.globalOpts = append(c.globalOpts, wftest.WithYAML(yaml))
	}
}

// WithMockStep registers a mock step handler applied to every scenario's harness.
func WithMockStep(name string, handler wftest.StepHandler) Option {
	return func(c *Config) {
		c.globalOpts = append(c.globalOpts, wftest.MockStep(name, handler))
	}
}

// Strict makes the runner fail on undefined or pending steps.
func Strict() Option {
	return func(c *Config) { c.strict = true }
}
