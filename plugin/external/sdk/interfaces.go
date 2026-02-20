// Package sdk provides the public API for building external workflow plugins.
// Plugin authors implement the interfaces defined here and call Serve() to run.
package sdk

import "context"

// PluginProvider is the main interface plugin authors implement.
type PluginProvider interface {
	// Manifest returns the plugin's metadata.
	Manifest() PluginManifest
}

// PluginManifest describes the plugin.
type PluginManifest struct {
	Name        string
	Version     string
	Author      string
	Description string
}

// ModuleProvider is optionally implemented to provide module types.
type ModuleProvider interface {
	// ModuleTypes returns the module type names this plugin provides.
	ModuleTypes() []string
	// CreateModule creates a module instance of the given type.
	CreateModule(typeName, name string, config map[string]any) (ModuleInstance, error)
}

// ModuleInstance is a remote module's lifecycle.
type ModuleInstance interface {
	Init() error
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// StepProvider is optionally implemented to provide step types.
type StepProvider interface {
	// StepTypes returns the step type names this plugin provides.
	StepTypes() []string
	// CreateStep creates a step instance of the given type.
	CreateStep(typeName, name string, config map[string]any) (StepInstance, error)
}

// StepInstance is a remote pipeline step.
type StepInstance interface {
	Execute(ctx context.Context, triggerData map[string]any, stepOutputs map[string]map[string]any, current map[string]any, metadata map[string]any) (*StepResult, error)
}

// StepResult is the output of a step execution.
type StepResult struct {
	Output       map[string]any
	StopPipeline bool
}
