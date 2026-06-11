# package sdk

Import path: `github.com/GoCodeAlone/workflow/plugin/external/sdk`

Version: `local`

Source: https://github.com/GoCodeAlone/workflow/tree/local/plugin/external/sdk

## Warnings

None

## Synopsis

Package sdk is the public runtime SDK for out-of-process Workflow plugins.

Plugin binaries implement PluginProvider plus optional provider interfaces
such as StepProvider, TypedStepProvider, ModuleProvider, ContractProvider, or
CLIProvider. A typical main function constructs the provider and calls Serve:

	func main() {
		sdk.Serve(internal.NewProvider(),
			sdk.WithBuildVersion(sdk.ResolveBuildVersion(internal.Version)),
		)
	}

Plugins that also expose CLI commands or build hooks can use ServePluginFull.
IaC provider plugins should use ServeIaCPlugin so typed IaC gRPC services are
registered and advertised consistently.

## Variables

ErrTypedContractNotHandled lets mixed typed/legacy providers decline a type
so the server can fall back to the legacy provider path.

```go
var ErrTypedContractNotHandled = errors.New("typed contract not handled")
```

## Functions

### func BuildContractRegistry

BuildContractRegistry enumerates the gRPC services registered on
grpcSrv and returns a *pb.ContractRegistry with a SERVICE-kind
ContractDescriptor for each one. Mode is set to
CONTRACT_MODE_STRICT_PROTO so the host can distinguish typed IaC
services from the legacy structpb-mode contracts produced by
Module/Step/Trigger ContractProvider implementations.

Why this exists (per cycle 3 I-1 of the strict-contracts force-cutover
design): wfctl needs a single mechanism to discover "is the optional
service registered on this plugin handle?". Reusing the existing
ContractRegistry shape keeps Module/Step/Trigger and IaC capability
discovery on the same wire surface — no new server-reflection
dependency required.

The helper is safe to call with a nil server; it returns an empty
(but non-nil) ContractRegistry. Service descriptors are emitted in a
deterministic alphabetical order so callers can rely on stable
FileDescriptorSet-adjacent output for diff/compare operations and
the wftest BDD test in Task 15.

IaC plugin authors typically wire this into their ContractProvider
implementation:

	func (p *plugin) ContractRegistry() *pb.ContractRegistry {
	    return sdk.BuildContractRegistry(p.grpcServer)
	}

where p.grpcServer was captured inside the iacGRPCPlugin.GRPCServer
callback at startup. The ContractProvider hook keeps the wfctl-side
GetContractRegistry RPC path unchanged.

```go
func BuildContractRegistry(grpcSrv *grpc.Server) *pb.ContractRegistry
```

### func BuildContractRegistryForPlugin

BuildContractRegistryForPlugin enumerates gRPC services registered on
grpcSrv whose name STARTS WITH namespacePrefix and returns a
*pb.ContractRegistry with one SERVICE-kind STRICT_PROTO ContractDescriptor
per matching service. Filters out go-plugin infra services (PluginService,
GRPCBroker, GRPCStdio, grpc.health.v1.Health) so downstream contract-diff
(workflow#767) sees only plugin-owned services.

Safe to call with nil server; returns empty (but non-nil) registry.
Names alphabetically sorted for stable diff output.

Typical caller: iacPluginServiceBridge.GetContractRegistry derives prefix
from pb.IaCProviderRequired_ServiceDesc.ServiceName minus the ".IaCProviderRequired"
suffix so the filter cannot drift from the .proto package declaration.

BuildContractRegistry (full-surface, no filter) is retained for callers
that want every registered service.

```go
func BuildContractRegistryForPlugin(grpcSrv *grpc.Server, namespacePrefix string) *pb.ContractRegistry
```

### func BuildMessageContractRegistry

BuildMessageContractRegistry returns a registry containing MESSAGE-kind
descriptors. Descriptor-only plugins can expose these contracts statically
in plugin.contracts.json; runtime-backed plugins can return the same shape
from ContractRegistry for parity tests.

```go
func BuildMessageContractRegistry(contracts ...MessageContract) (*pb.ContractRegistry, error)
```

### func DispatchArgs

DispatchArgs is the testable core of ServePluginFull. It inspects args (which
should be os.Args in production) and dispatches accordingly.
stdin and stdout are used for hook payload I/O; pass os.Stdin/os.Stdout in
production and an in-memory reader/writer in tests.

Returns:
  - -1 if no wfctl flag is present (caller should fall back to Serve)
  - 0 on success
  - >0 on error

```go
func DispatchArgs(args []string, p PluginProvider, cli CLIProvider, hooks HookHandler, stdin io.Reader, stdout io.Writer) int
```

### func EmbedManifest

EmbedManifest parses plugin.json content (typically loaded via go:embed) into
the canonical *plugin.PluginManifest type and runs the canonical Validate().

Validate() requires ALL of: Name, Version, Author, Description (verified at
plugin/manifest.go:183-201). A plugin.json missing any of these is rejected.
This matches the same contract enforced by the engine's manager.go on disk
load — there is no "minimal" path. If you cannot supply Author or
Description at build time, the plugin should not ship a release.

Plugin authors write:

	//go:embed plugin.json
	var manifestJSON []byte
	var manifest = sdk.MustEmbedManifest(manifestJSON)

The returned manifest is passed into sdk.Serve via WithManifestProvider, or
into sdk.IaCServeOptions.ManifestProvider for ServeIaCPlugin. The SDK wires
it into the appropriate GetManifest gRPC handler so the workflow engine sees
a fully-populated manifest at plugin registration time.

Parses via the canonical *plugin.PluginManifest (camelCase JSON tags matching
the plugin.json authoring convention), NOT directly into *pb.Manifest (which
has snake_case proto JSON tags and would silently drop configMutable etc.).

For production code paths that need to recover from a missing/malformed
plugin.json (e.g., plugins that ship with multiple manifest candidates),
prefer EmbedManifest with explicit error handling over MustEmbedManifest.
MustEmbedManifest panics at process startup, which surfaces misconfiguration
loudly but is unrecoverable.

```go
func EmbedManifest(content []byte) (*pluginpkg.PluginManifest, error)
```

### func MustEmbedManifest

MustEmbedManifest panics on parse or validation error. Intended for
package-level var initialization in plugin main packages — failure indicates
a build-time misconfiguration that must be fixed before the binary ships.

WARNING: panic semantics make this a process-startup canary. Plugin
authors who need graceful degradation (e.g., to recover from a
missing/malformed plugin.json in tooling-only code paths) should use
EmbedManifest with explicit error handling instead.

```go
func MustEmbedManifest(content []byte) *pluginpkg.PluginManifest
```

### func RegisterAllIaCProviderServices

RegisterAllIaCProviderServices uses Go type-assertion to register every
typed IaC gRPC service that the provider satisfies, in a single call.

REQUIRED service:

	pb.IaCProviderRequiredServer — every IaC plugin MUST implement every
	method on this interface. The type-assert here surfaces missing
	methods at plugin-startup time as a clear error rather than at the
	first RPC dispatch with a generic "unimplemented" status.

OPTIONAL services (auto-detected):

	pb.IaCProviderEnumeratorServer
	pb.IaCProviderDriftDetectorServer
	pb.IaCProviderCredentialRevokerServer
	pb.IaCProviderRegionListerServer
	pb.IaCProviderOwnershipServer
	pb.IaCProviderMigrationRepairerServer
	pb.IaCProviderValidatorServer
	pb.IaCProviderDriftConfigDetectorServer
	pb.IaCProviderLogCaptureServer
	pb.IaCProviderRunnerServer
	pb.IaCRequirementDiscoveryServer
	pb.IaCProviderRequirementMapperServer
	pb.IaCStateBackendServer

ResourceDriver:

	pb.ResourceDriverServer — separate gRPC service, also auto-registered
	when the provider satisfies it.

Per cycle 3 I-1 of the strict-contracts force-cutover design: plugin
authors write ONE call; they cannot omit registration for a capability
they implemented. That eliminates the registration-omission bug class
(the same shape as the legacy InvokeService case-string-typo bug) by
removing the manual step entirely.

Capability discovery on the host side uses the existing ContractRegistry
RPC + FileDescriptorSet mechanism (kept via §Salvage in the design);
the SDK auto-publishes the registered services there in Task 5.

Plugin authors who DO NOT want a capability advertised must NOT
implement those methods at the Go level — there is no half-implemented
stub-and-forget-to-register failure mode.

```go
func RegisterAllIaCProviderServices(s *grpc.Server, provider any) error
```

### func ResolveBuildVersion

ResolveBuildVersion returns the operator-visible build-version string.

When declared is non-empty AND not a known dev sentinel ("", "dev",
"(devel)"), returns declared as-is. This is the typical path for
goreleaser-built plugin binaries where the ldflag injects the release
tag into a package-level Version var.

Otherwise consults runtime/debug.ReadBuildInfo() as fallback:
  - "(devel) [@ shortsha[.dirty]]" when vcs.revision is set
  - "(devel)" when no VCS info

Intended call sites (plugin author chooses ANY package-level Version var):

	var Version = "dev"   // ldflag-injected at release time

	sdk.ServeIaCPlugin(srv, sdk.IaCServeOptions{
	    BuildVersion: sdk.ResolveBuildVersion(internal.Version),
	})
	sdk.Serve(p, sdk.WithManifestProvider(m),
	    sdk.WithBuildVersion(sdk.ResolveBuildVersion(internal.Version)))

Goreleaser config provides the tag:

	ldflags:
	  - -X github.com/<...>/internal.Version={{.Version}}

Mirrors the wfctl pattern at cmd/wfctl/main.go (var version = "dev" +
debug.ReadBuildInfo() fallback). Closes workflow#758.

```go
func ResolveBuildVersion(declared string) string
```

### func Serve

Serve is the entry point for plugin authors. It starts the gRPC plugin server
and blocks until the host process terminates the connection.

If provider implements UIProvider, Serve writes a "ui.json" file to the
current working directory (if one does not already exist). Plugin authors
can also maintain "ui.json" manually without implementing UIProvider.

Usage:

	func main() {
	    sdk.Serve(&myPlugin{})
	}

With disk-embedded manifest:

	func main() {
	    sdk.Serve(&myPlugin{}, sdk.WithManifestProvider(manifest))
	}

```go
func Serve(provider PluginProvider, opts ...ServeOption)
```

### func ServeIaCPlugin

ServeIaCPlugin starts a typed IaC plugin gRPC server with auto
registration of every IaC service the provider satisfies. Plugin
authors call this once in main.go:

	func main() {
	    sdk.ServeIaCPlugin(&doProvider{}, sdk.IaCServeOptions{})
	}

Per cycle 3 I-1 of the strict-contracts force-cutover design, the
service registration happens INSIDE go-plugin's GRPCServer callback
(see iacGRPCPlugin.GRPCServer), so plugin authors cannot pre-create
a *grpc.Server and forget to register a typed service on it.

Blocks until the host process terminates the connection. Panics on
invalid IaCServeOptions (e.g., partial handshake override missing
MagicCookieKey or MagicCookieValue) — see resolveServeHandshake.
Plugin authors fix the misconfig at the call site; the panic is
preferable to a silent fallback that produces a broken handshake at
dial time.

```go
func ServeIaCPlugin(provider any, opts IaCServeOptions)
```

### func ServePluginFull

ServePluginFull is the multi-mode entry point for plugin authors that use
CLI commands and/or build-pipeline hook handlers in addition to the standard
gRPC plugin server.

Dispatch rules (os.Args inspected at startup):
 1. --wfctl-cli  → CLIProvider.RunCLI is called; process exits with its return code.
 2. --wfctl-hook → HookHandler.HandleBuildHook is called with the event name and
    the JSON payload read from stdin; result is written to stdout; process exits 0.
 3. Neither flag → falls through to the standard gRPC Serve(p).

Plugins that don't need CLI/hook capabilities keep using Serve(p).

Usage:

	func main() {
	    sdk.ServePluginFull(&myPlugin{}, &myCLI{}, &myHooks{})
	}

With disk-embedded manifest:

	func main() {
	    sdk.ServePluginFull(&myPlugin{}, &myCLI{}, &myHooks{},
	        sdk.WithManifestProvider(manifest))
	}

```go
func ServePluginFull(p PluginProvider, cli CLIProvider, hooks HookHandler, opts ...ServeOption)
```

## Types

### type AssetProvider

AssetProvider allows plugins to serve embedded static assets (e.g., UI files).

```go
type AssetProvider interface {
	GetAsset(path string) (content []byte, contentType string, err error)
}
```

### type CLIProvider

CLIProvider is implemented by plugins that expose top-level wfctl subcommands.
When wfctl detects a matching command it invokes the plugin binary with
--wfctl-cli <command> [args...], and the plugin must exit with the returned code.

```go
type CLIProvider interface {
	// RunCLI handles the command. args contains the command and all subsequent
	// arguments (the plugin binary path and the --wfctl-cli flag are stripped).
	// The return value becomes the process exit code.
	RunCLI(args []string) int
}
```

### type ConfigField

ConfigField describes a configuration field.

```go
type ConfigField struct {
	Name		string
	Type		string
	Description	string
	DefaultValue	string
	Required	bool
	Options		[]string
}
```

### type ConfigProvider

ConfigProvider is optionally implemented by plugins that need to inject
config (modules, workflows, triggers) into the host config before
module registration.

```go
type ConfigProvider interface {
	// ConfigFragment returns YAML config to merge into the host config.
	ConfigFragment() ([]byte, error)
}
```

### type ContractProvider

ContractProvider is optionally implemented to expose typed contract descriptors.

```go
type ContractProvider interface {
	ContractRegistry() *pb.ContractRegistry
}
```

### type HookHandler

HookHandler is implemented by plugins that register build-pipeline hook handlers.
When wfctl dispatches a hook event it invokes the plugin binary with
--wfctl-hook <event>, writes the JSON payload to stdin, and reads the
JSON result from stdout.

```go
type HookHandler interface {
	// HandleBuildHook handles the given hook event.
	// payload is the raw JSON payload from wfctl.
	// result is the raw JSON response written back to wfctl.
	// A non-nil error causes wfctl to apply the plugin's on_hook_failure policy.
	HandleBuildHook(event string, payload []byte) (result []byte, err error)
}
```

### type IaCServeOptions

IaCServeOptions configures the IaC plugin gRPC server entrypoint.

Plugin authors typically zero-value this; ServeIaCPlugin then uses the
canonical host<->plugin handshake (ext.Handshake). The struct exists as
a forward-extension point so future metadata fields (PluginInfo) can be
added without breaking the API.

```go
type IaCServeOptions struct {
	// PluginInfo overrides the default handshake/metadata. When nil,
	// ServeIaCPlugin uses ext.Handshake (the canonical wfctl<->plugin
	// handshake — required for compatibility with the workflow host).
	PluginInfo	*PluginInfo

	// ManifestProvider, when set, is returned by the bridge's GetManifest
	// RPC. Typically populated via sdk.MustEmbedManifest from a go:embed-ed
	// plugin.json. When nil, GetManifest returns codes.Unimplemented and the
	// engine falls back to its manager.go-loaded plugin.json (workflow plan
	// Task 1).
	ManifestProvider	*pluginpkg.PluginManifest

	// BuildVersion, when non-empty, overrides any ManifestProvider.Version
	// in the GetManifest RPC response. Typically populated via
	// sdk.ResolveBuildVersion(<plugin's ldflag-injected Version var>) so
	// operator + engine observe the release tag at runtime even when the
	// committed plugin.json carries a dev sentinel ("0.0.0"). Closes
	// workflow#758.
	BuildVersion	string

	// Modules supplies plugin-native module providers. When non-nil, the
	// bridge wires GetModuleTypes / CreateModule / InitModule / StartModule /
	// StopModule / DestroyModule to delegate to grpc_server.go's existing
	// PluginService implementation via a thin mapBackedProvider adapter.
	// Zero-value = current behavior (Unimplemented for those RPCs).
	// See decisions/0038.
	Modules	map[string]ModuleProvider

	// Steps supplies plugin-native step providers. Same wiring rationale as
	// Modules; values are sdk.StepProvider — the same interface non-IaC
	// plugins consume via sdk.Serve.
	Steps	map[string]StepProvider

	// TypedModules supplies plugin-native module providers that implement the
	// strict-proto TypedModuleProvider surface (sdk.TypedModuleFactory or a
	// custom implementor). When non-nil, mapBackedProvider implements
	// TypedModuleProvider and grpc_server.go's CreateModule path dispatches
	// CreateTypedModule on the looked-up entry — passing the host-supplied
	// *anypb.Any TypedConfig directly to the typed factory's proto-message
	// unpack. The legacy Modules map remains supported alongside (the
	// dispatch is Typed-first, then legacy-fallback). See decisions/0039.
	TypedModules	map[string]TypedModuleProvider

	// TypedSteps supplies plugin-native step providers that implement
	// TypedStepProvider. Same wiring rationale as TypedModules. See
	// decisions/0039.
	TypedSteps	map[string]TypedStepProvider
}
```

### type MessageAwareModule

MessageAwareModule is optionally implemented by ModuleInstance to receive message capabilities.

```go
type MessageAwareModule interface {
	SetMessagePublisher(pub MessagePublisher)
	SetMessageSubscriber(sub MessageSubscriber)
}
```

### type MessageContract

MessageContract describes a descriptor-only protobuf message contract.

```go
type MessageContract struct {
	ContractType	string
	ProtoPackage	string
	MessageNames	[]string
	GoImportPath	string
	SchemaDigest	string
	ProtocolVersion	string
}
```

### type MessagePublisher

MessagePublisher is provided to modules that need to send messages to the host.

```go
type MessagePublisher interface {
	Publish(topic string, payload []byte, metadata map[string]string) (messageID string, err error)
}
```

### type MessageSubscriber

MessageSubscriber is provided to modules that need to receive messages from the host.

```go
type MessageSubscriber interface {
	Subscribe(topic string, handler func(payload []byte, metadata map[string]string) error) error
	Unsubscribe(topic string) error
}
```

### type ModuleInstance

ModuleInstance is a remote module's lifecycle.

```go
type ModuleInstance interface {
	Init() error
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}
```

### type ModuleProvider

ModuleProvider is optionally implemented to provide module types.

```go
type ModuleProvider interface {
	// ModuleTypes returns the module type names this plugin provides.
	ModuleTypes() []string
	// CreateModule creates a module instance of the given type.
	CreateModule(typeName, name string, config map[string]any) (ModuleInstance, error)
}
```

### type ModuleSchemaData

ModuleSchemaData describes a module type for the UI.

```go
type ModuleSchemaData struct {
	Type		string
	Label		string
	Category	string
	Description	string
	Inputs		[]ServiceIO
	Outputs		[]ServiceIO
	ConfigFields	[]ConfigField
}
```

### type PluginInfo

PluginInfo carries the metadata that go-plugin needs to serve an IaC
plugin. Currently only HandshakeConfig is meaningful; reserved as the
extension point for future Name/Version metadata fields without
breaking the IaCServeOptions API.

```go
type PluginInfo struct {
	// HandshakeConfig is the go-plugin handshake. Plugin authors should
	// leave this zero-valued to inherit ext.Handshake — the host (wfctl)
	// and plugin MUST agree on the cookie + protocol version, so override
	// only when implementing a non-workflow host.
	HandshakeConfig goplugin.HandshakeConfig
}
```

### type PluginManifest

PluginManifest is the runtime metadata a plugin returns to the host.

For release-built plugins, prefer sdk.WithBuildVersion with
ResolveBuildVersion so Version reflects the Git tag injected by the build
instead of the committed plugin.json sentinel.

```go
type PluginManifest struct {
	// Name is the canonical plugin name, usually workflow-plugin-<short-name>.
	Name	string
	// Version is the operator-visible runtime version.
	Version	string
	// Author identifies the organization or person that maintains the plugin.
	Author	string
	// Description is shown in registry and documentation output.
	Description	string
	// ConfigMutable reports whether tenants can override the config fragment.
	ConfigMutable	bool
	// SampleCategory marks sample/app plugins for grouped presentation.
	SampleCategory	string
}
```

### type PluginProvider

PluginProvider is the minimum interface every external plugin implements.

```go
type PluginProvider interface {
	// Manifest returns the plugin's metadata.
	Manifest() PluginManifest
}
```

### type SchemaProvider

SchemaProvider is optionally implemented to provide UI schemas.

```go
type SchemaProvider interface {
	ModuleSchemas() []ModuleSchemaData
}
```

### type ServeOption

ServeOption configures Serve and ServePluginFull.

```go
type ServeOption func(*grpcServer)
```

## Functions

### func WithBuildVersion

WithBuildVersion sets the runtime build-version surfaced via GetManifest.
Single-channel precedence: takes precedence over any ManifestProvider.Version
or provider.Manifest().Version. Typically populated via
sdk.ResolveBuildVersion(<plugin's ldflag-injected Version var>).

Recommended pattern:

	import (
	    "github.com/<...>/internal"  // ldflag-injected Version var
	    sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
	)

	func main() {
	    sdk.Serve(&myPlugin{},
	        sdk.WithManifestProvider(manifest),
	        sdk.WithBuildVersion(sdk.ResolveBuildVersion(internal.Version)),
	    )
	}

Goreleaser config injects the tag at build time:

	ldflags:
	  - -X github.com/<...>/internal.Version={{.Version}}

Closes workflow#758.

```go
func WithBuildVersion(v string) ServeOption
```

### func WithManifestProvider

WithManifestProvider wires a canonical *plugin.PluginManifest (typically
loaded via sdk.EmbedManifest) into the gRPC GetManifest handler. When set,
the disk-embedded manifest takes precedence over the provider's Manifest()
method.

Recommended pattern:

	//go:embed plugin.json
	var manifestJSON []byte
	var manifest = sdk.MustEmbedManifest(manifestJSON)

	func main() {
	    sdk.Serve(&myProvider{}, sdk.WithManifestProvider(manifest))
	}

```go
func WithManifestProvider(m *pluginpkg.PluginManifest) ServeOption
```

### type ServiceContextInvoker

ServiceContextInvoker is optionally implemented by ModuleInstance to receive
the gRPC request context for long-running service invocations.

```go
type ServiceContextInvoker interface {
	InvokeMethodContext(ctx context.Context, method string, args map[string]any) (map[string]any, error)
}
```

### type ServiceIO

ServiceIO describes a service input or output.

```go
type ServiceIO struct {
	Name		string
	Type		string
	Description	string
}
```

### type ServiceInvoker

ServiceInvoker is optionally implemented by ModuleInstance to handle
service method invocations from the host. The host calls InvokeService
with a method name and a map of arguments; the implementation dispatches
to the appropriate logic and returns a result map.

```go
type ServiceInvoker interface {
	InvokeMethod(method string, args map[string]any) (map[string]any, error)
}
```

### type StepInstance

StepInstance is a remote pipeline step.

```go
type StepInstance interface {
	Execute(ctx context.Context, triggerData map[string]any, stepOutputs map[string]map[string]any, current map[string]any, metadata map[string]any, config map[string]any) (*StepResult, error)
}
```

### type StepProvider

StepProvider is optionally implemented to provide step types.

```go
type StepProvider interface {
	// StepTypes returns the step type names this plugin provides.
	StepTypes() []string
	// CreateStep creates a step instance of the given type.
	CreateStep(typeName, name string, config map[string]any) (StepInstance, error)
}
```

### type StepResult

StepResult is the output of a step execution.

```go
type StepResult struct {
	Output		map[string]any
	StopPipeline	bool
}
```

### type TelemetryAttrs

TelemetryAttrs aliases the host telemetry attribute map type for plugin APIs.

```go
type TelemetryAttrs = telemetry.Attrs
```

### type TelemetryLogEmitter

TelemetryLogEmitter aliases the host log emitter interface for plugin APIs.

```go
type TelemetryLogEmitter = telemetry.LogEmitter
```

### type TelemetryLogRecord

TelemetryLogRecord aliases the host log record type for plugin APIs.

```go
type TelemetryLogRecord = telemetry.LogRecord
```

### type TelemetryMetricEmitter

TelemetryMetricEmitter aliases the host metric emitter interface for plugin APIs.

```go
type TelemetryMetricEmitter = telemetry.MetricEmitter
```

### type TelemetryMetricKind

TelemetryMetricKind aliases the host metric kind enum for plugin APIs.

```go
type TelemetryMetricKind = telemetry.MetricKind
```

### type TelemetryMetricRecord

TelemetryMetricRecord aliases the host metric record type for plugin APIs.

```go
type TelemetryMetricRecord = telemetry.MetricRecord
```

### type TelemetryMetricRecorder

TelemetryMetricRecorder aliases the host metric recorder interface for plugin APIs.

```go
type TelemetryMetricRecorder = telemetry.MetricRecorder
```

### type TelemetrySpanEvent

TelemetrySpanEvent aliases the host span event type for plugin APIs.

```go
type TelemetrySpanEvent = telemetry.SpanEvent
```

### type TelemetrySpanRecorder

TelemetrySpanRecorder aliases the host span recorder interface for plugin APIs.

```go
type TelemetrySpanRecorder = telemetry.SpanRecorder
```

### type TelemetryTraceAnnotator

TelemetryTraceAnnotator aliases the host trace annotator interface for plugin APIs.

```go
type TelemetryTraceAnnotator = telemetry.TraceAnnotator
```

### type TriggerCallback

TriggerCallback allows a trigger to fire workflow actions on the host.

```go
type TriggerCallback func(action string, data map[string]any) error
```

### type TriggerInstance

TriggerInstance is a remote trigger's lifecycle.

```go
type TriggerInstance interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}
```

### type TriggerProvider

TriggerProvider is optionally implemented by plugins that provide trigger types.

```go
type TriggerProvider interface {
	TriggerTypes() []string
	CreateTrigger(typeName string, config map[string]any, cb TriggerCallback) (TriggerInstance, error)
}
```

### type TypedModuleCreator

TypedModuleCreator constructs a module after its typed config has been
unpacked and validated.

```go
type TypedModuleCreator[C proto.Message] func(name string, config C) (ModuleInstance, error)
```

### type TypedModuleFactory

TypedModuleFactory is a single-module TypedModuleProvider implementation.

```go
type TypedModuleFactory[C proto.Message] struct {
	// contains filtered or unexported fields
}
```

## Functions

### func NewTypedModuleFactory

NewTypedModuleFactory returns a provider for one typed module type. The
factory validates typed_config before invoking create.

```go
func NewTypedModuleFactory[C proto.Message](
	typeName string,
	configPrototype C,
	create TypedModuleCreator[C],
) *TypedModuleFactory[C]
```

## Methods

### func CreateTypedModule

```go
func (f *TypedModuleFactory[C]) CreateTypedModule(typeName, name string, config *anypb.Any) (ModuleInstance, error)
```

### func TypedModuleTypes

```go
func (f *TypedModuleFactory[C]) TypedModuleTypes() []string
```

### type TypedModuleInstance

TypedModuleInstance adapts protobuf-typed module config while preserving the
normal ModuleInstance lifecycle interface.

```go
type TypedModuleInstance[C proto.Message] struct {
	ModuleInstance
	// contains filtered or unexported fields
}
```

## Functions

### func NewTypedModuleInstance

NewTypedModuleInstance returns a ModuleInstance wrapper that can accept typed
module config from CreateModuleRequest. The wrapped instance still owns the
module lifecycle.

```go
func NewTypedModuleInstance[C proto.Message](configPrototype C, module ModuleInstance) *TypedModuleInstance[C]
```

## Methods

### func Init

```go
func (m *TypedModuleInstance[C]) Init() error
```

### func InvokeMethod

```go
func (m *TypedModuleInstance[C]) InvokeMethod(method string, args map[string]any) (map[string]any, error)
```

### func InvokeTypedMethod

```go
func (m *TypedModuleInstance[C]) InvokeTypedMethod(method string, input *anypb.Any) (*anypb.Any, error)
```

### func SetMessagePublisher

```go
func (m *TypedModuleInstance[C]) SetMessagePublisher(pub MessagePublisher)
```

### func SetMessageSubscriber

```go
func (m *TypedModuleInstance[C]) SetMessageSubscriber(sub MessageSubscriber)
```

### func Start

```go
func (m *TypedModuleInstance[C]) Start(ctx context.Context) error
```

### func Stop

```go
func (m *TypedModuleInstance[C]) Stop(ctx context.Context) error
```

### func TypedConfig

TypedConfig returns the unpacked module config most recently supplied by the
host.

```go
func (m *TypedModuleInstance[C]) TypedConfig() C
```

### type TypedModuleProvider

TypedModuleProvider creates protobuf-typed module instances after validating
typed_config. Implement this instead of ModuleProvider for strict typed modules.

```go
type TypedModuleProvider interface {
	TypedModuleTypes() []string
	CreateTypedModule(typeName, name string, config *anypb.Any) (ModuleInstance, error)
}
```

### type TypedServiceInvoker

TypedServiceInvoker is optionally implemented by ModuleInstance to handle
strict protobuf service method invocations from the host.

```go
type TypedServiceInvoker interface {
	InvokeTypedMethod(method string, input *anypb.Any) (*anypb.Any, error)
}
```

### type TypedStepFactory

TypedStepFactory is a single-step TypedStepProvider implementation.

```go
type TypedStepFactory[C proto.Message, I proto.Message, O proto.Message] struct {
	// contains filtered or unexported fields
}
```

## Functions

### func NewTypedStepFactory

NewTypedStepFactory returns a provider for one typed step type. The factory
validates typed_config before returning an instance, so failed creation does
not leak a partially-created step into plugin-local state.

```go
func NewTypedStepFactory[C proto.Message, I proto.Message, O proto.Message](
	typeName string,
	configPrototype C,
	inputPrototype I,
	handler TypedStepHandler[C, I, O],
) *TypedStepFactory[C, I, O]
```

## Methods

### func CreateTypedStep

```go
func (f *TypedStepFactory[C, I, O]) CreateTypedStep(typeName, _ string, config *anypb.Any) (StepInstance, error)
```

### func TypedStepTypes

```go
func (f *TypedStepFactory[C, I, O]) TypedStepTypes() []string
```

### type TypedStepHandler

TypedStepHandler executes a typed step with protobuf config and input.

```go
type TypedStepHandler[C proto.Message, I proto.Message, O proto.Message] func(context.Context, TypedStepRequest[C, I]) (*TypedStepResult[O], error)
```

### type TypedStepInstance

TypedStepInstance adapts a protobuf-typed step implementation to the legacy
StepInstance interface and the typed gRPC execution path.

```go
type TypedStepInstance[C proto.Message, I proto.Message, O proto.Message] struct {
	// contains filtered or unexported fields
}
```

## Functions

### func NewTypedStepInstance

NewTypedStepInstance returns a StepInstance that validates typed Any payloads
before invoking handler. configPrototype and inputPrototype define the
expected protobuf message types.

```go
func NewTypedStepInstance[C proto.Message, I proto.Message, O proto.Message](
	configPrototype C,
	inputPrototype I,
	handler TypedStepHandler[C, I, O],
) *TypedStepInstance[C, I, O]
```

## Methods

### func Execute

Execute keeps TypedStepInstance assignable to StepInstance. Typed plugins
should normally execute through ExecuteStep with typed_input; legacy map-only
execution cannot safely populate arbitrary protobuf messages.

```go
func (s *TypedStepInstance[C, I, O]) Execute(context.Context, map[string]any, map[string]map[string]any, map[string]any, map[string]any, map[string]any) (*StepResult, error)
```

### type TypedStepProvider

TypedStepProvider creates protobuf-typed step instances after validating
typed_config. Implement this instead of StepProvider for strict typed steps.

```go
type TypedStepProvider interface {
	TypedStepTypes() []string
	CreateTypedStep(typeName, name string, config *anypb.Any) (StepInstance, error)
}
```

### type TypedStepRequest

TypedStepRequest is passed to typed step handlers after protobuf Any payloads
have been validated and unpacked.

```go
type TypedStepRequest[C proto.Message, I proto.Message] struct {
	Config	C
	Input	I

	TriggerData	map[string]any
	StepOutputs	map[string]map[string]any
	Current		map[string]any
	Metadata	map[string]any
}
```

### type TypedStepResult

TypedStepResult is returned from typed step handlers and packed into Any.

```go
type TypedStepResult[O proto.Message] struct {
	Output		O
	StopPipeline	bool
}
```

### type UIProvider

UIProvider is an optional interface that PluginProvider implementations can
satisfy to declare UI assets and navigation contributions.

If a PluginProvider implements UIProvider, the SDK Serve() function will
write a "ui.json" file to the plugin's working directory on first start
(if one does not already exist). Alternatively, authors can maintain
"ui.json" manually without implementing this interface.

# Type aliases

The UI manifest types (UIManifest, UINavItem) are defined in the
github.com/GoCodeAlone/workflow/plugin/external package so that both the
host engine and plugin processes share the same type definitions without
introducing an import cycle.

```go
type UIProvider interface {
	// UIManifest returns the UI manifest for this plugin.
	UIManifest() ext.UIManifest
}
```

