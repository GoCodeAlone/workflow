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

// TriggerProvider is optionally implemented by plugins that provide trigger types.
type TriggerProvider interface {
	TriggerTypes() []string
	CreateTrigger(typeName string, config map[string]any, cb TriggerCallback) (TriggerInstance, error)
}

// TriggerCallback allows a trigger to fire workflow actions on the host.
type TriggerCallback func(action string, data map[string]any) error

// TriggerInstance is a remote trigger's lifecycle.
type TriggerInstance interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// SchemaProvider is optionally implemented to provide UI schemas.
type SchemaProvider interface {
	ModuleSchemas() []ModuleSchemaData
}

// ModuleSchemaData describes a module type for the UI.
type ModuleSchemaData struct {
	Type         string
	Label        string
	Category     string
	Description  string
	Inputs       []ServiceIO
	Outputs      []ServiceIO
	ConfigFields []ConfigField
}

// ServiceIO describes a service input or output.
type ServiceIO struct {
	Name        string
	Type        string
	Description string
}

// ConfigField describes a configuration field.
type ConfigField struct {
	Name         string
	Type         string
	Description  string
	DefaultValue string
	Required     bool
	Options      []string
}

// MessagePublisher is provided to modules that need to send messages to the host.
type MessagePublisher interface {
	Publish(topic string, payload []byte, metadata map[string]string) (messageID string, err error)
}

// MessageSubscriber is provided to modules that need to receive messages from the host.
type MessageSubscriber interface {
	Subscribe(topic string, handler func(payload []byte, metadata map[string]string) error) error
	Unsubscribe(topic string) error
}

// MessageAwareModule is optionally implemented by ModuleInstance to receive message capabilities.
type MessageAwareModule interface {
	SetMessagePublisher(pub MessagePublisher)
	SetMessageSubscriber(sub MessageSubscriber)
}

// ConfigProvider is optionally implemented by plugins that need to inject
// config (modules, workflows, triggers) into the host config before
// module registration.
type ConfigProvider interface {
	// ConfigFragment returns YAML config to merge into the host config.
	ConfigFragment() ([]byte, error)
}
