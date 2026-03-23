package wftest

import "github.com/GoCodeAlone/workflow/plugin"

// Option configures a Harness when passed to New.
// All built-in option constructors (WithYAML, MockStep, etc.) implement this
// interface. *Recorder also implements Option so RecordStep can be passed
// directly to New without a separate MockStep call.
type Option interface {
	applyTo(*Harness)
}

// optionFunc is a function that implements Option.
type optionFunc func(*Harness)

func (f optionFunc) applyTo(h *Harness) { f(h) }

// WithYAML configures the harness with inline YAML config.
func WithYAML(yaml string) Option {
	return optionFunc(func(h *Harness) { h.yamlConfig = yaml })
}

// WithConfig loads config from a YAML file path.
func WithConfig(path string) Option {
	return optionFunc(func(h *Harness) { h.configPath = path })
}

// WithPlugin loads an additional engine plugin into the harness before BuildFromConfig.
func WithPlugin(p plugin.EnginePlugin) Option {
	return optionFunc(func(h *Harness) { h.extraPlugins = append(h.extraPlugins, p) })
}

// MockStep registers a mock factory for stepType that calls handler on every
// execution. The mock is installed after built-in plugins load, so it
// overrides real implementations. Use Returns for fixed output or NewRecorder
// to capture calls.
func MockStep(stepType string, handler StepHandler) Option {
	return optionFunc(func(h *Harness) {
		if h.mockSteps == nil {
			h.mockSteps = make(map[string]StepHandler)
		}
		h.mockSteps[stepType] = handler
	})
}

// WithMockModule registers a fake module in the service registry so steps that
// look up a named dependency find this mock instead of a real module.
func WithMockModule(mod *MockModule) Option {
	return optionFunc(func(h *Harness) { h.mockModules = append(h.mockModules, mod) })
}

// WithServer starts a real HTTP listener backed by the engine's HTTP router.
// After harness creation, use h.BaseURL() to get the server URL.
// The server is stopped automatically via t.Cleanup.
func WithServer() Option {
	return optionFunc(func(h *Harness) { h.serverMode = true })
}

// WithState initializes an in-memory StateStore for the harness.
// The store is accessible via h.State() and is also registered as the
// "wftest.state_store" service so pipeline steps can look it up.
func WithState() Option {
	return optionFunc(func(h *Harness) { h.state = NewStateStore() })
}
