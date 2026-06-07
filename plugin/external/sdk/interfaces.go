package sdk

import (
	"context"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"github.com/GoCodeAlone/workflow/telemetry"
	"google.golang.org/protobuf/types/known/anypb"
)

// PluginProvider is the minimum interface every external plugin implements.
type PluginProvider interface {
	// Manifest returns the plugin's metadata.
	Manifest() PluginManifest
}

// ContractProvider is optionally implemented to expose typed contract descriptors.
type ContractProvider interface {
	ContractRegistry() *pb.ContractRegistry
}

// PluginManifest is the runtime metadata a plugin returns to the host.
//
// For release-built plugins, prefer sdk.WithBuildVersion with
// ResolveBuildVersion so Version reflects the Git tag injected by the build
// instead of the committed plugin.json sentinel.
type PluginManifest struct {
	// Name is the canonical plugin name, usually workflow-plugin-<short-name>.
	Name string
	// Version is the operator-visible runtime version.
	Version string
	// Author identifies the organization or person that maintains the plugin.
	Author string
	// Description is shown in registry and documentation output.
	Description string
	// ConfigMutable reports whether tenants can override the config fragment.
	ConfigMutable bool
	// SampleCategory marks sample/app plugins for grouped presentation.
	SampleCategory string
}

// AssetProvider allows plugins to serve embedded static assets (e.g., UI files).
type AssetProvider interface {
	GetAsset(path string) (content []byte, contentType string, err error)
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
	Execute(ctx context.Context, triggerData map[string]any, stepOutputs map[string]map[string]any, current map[string]any, metadata map[string]any, config map[string]any) (*StepResult, error)
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

// ServiceInvoker is optionally implemented by ModuleInstance to handle
// service method invocations from the host. The host calls InvokeService
// with a method name and a map of arguments; the implementation dispatches
// to the appropriate logic and returns a result map.
type ServiceInvoker interface {
	InvokeMethod(method string, args map[string]any) (map[string]any, error)
}

// ServiceContextInvoker is optionally implemented by ModuleInstance to receive
// the gRPC request context for long-running service invocations.
type ServiceContextInvoker interface {
	InvokeMethodContext(ctx context.Context, method string, args map[string]any) (map[string]any, error)
}

// TypedServiceInvoker is optionally implemented by ModuleInstance to handle
// strict protobuf service method invocations from the host.
type TypedServiceInvoker interface {
	InvokeTypedMethod(method string, input *anypb.Any) (*anypb.Any, error)
}

// TelemetryAttrs aliases the host telemetry attribute map type for plugin APIs.
type TelemetryAttrs = telemetry.Attrs

// TelemetryMetricKind aliases the host metric kind enum for plugin APIs.
type TelemetryMetricKind = telemetry.MetricKind

// TelemetryMetricRecord aliases the host metric record type for plugin APIs.
type TelemetryMetricRecord = telemetry.MetricRecord

// TelemetryMetricRecorder aliases the host metric recorder interface for plugin APIs.
type TelemetryMetricRecorder = telemetry.MetricRecorder

// TelemetryMetricEmitter aliases the host metric emitter interface for plugin APIs.
type TelemetryMetricEmitter = telemetry.MetricEmitter

// TelemetryLogRecord aliases the host log record type for plugin APIs.
type TelemetryLogRecord = telemetry.LogRecord

// TelemetryLogEmitter aliases the host log emitter interface for plugin APIs.
type TelemetryLogEmitter = telemetry.LogEmitter

// TelemetrySpanEvent aliases the host span event type for plugin APIs.
type TelemetrySpanEvent = telemetry.SpanEvent

// TelemetrySpanRecorder aliases the host span recorder interface for plugin APIs.
type TelemetrySpanRecorder = telemetry.SpanRecorder

// TelemetryTraceAnnotator aliases the host trace annotator interface for plugin APIs.
type TelemetryTraceAnnotator = telemetry.TraceAnnotator
