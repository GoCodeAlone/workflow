package sdk

import (
	"context"
	"fmt"
	"reflect"
	"sort"

	goplugin "github.com/GoCodeAlone/go-plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/emptypb"

	pluginpkg "github.com/GoCodeAlone/workflow/plugin"
	ext "github.com/GoCodeAlone/workflow/plugin/external"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

// RegisterAllIaCProviderServices uses Go type-assertion to register every
// typed IaC gRPC service that the provider satisfies, in a single call.
//
// REQUIRED service:
//
//	pb.IaCProviderRequiredServer — every IaC plugin MUST implement every
//	method on this interface. The type-assert here surfaces missing
//	methods at plugin-startup time as a clear error rather than at the
//	first RPC dispatch with a generic "unimplemented" status.
//
// OPTIONAL services (auto-detected):
//
//	pb.IaCProviderEnumeratorServer
//	pb.IaCProviderDriftDetectorServer
//	pb.IaCProviderCredentialRevokerServer
//	pb.IaCProviderMigrationRepairerServer
//	pb.IaCProviderValidatorServer
//	pb.IaCProviderDriftConfigDetectorServer
//	pb.IaCStateBackendServer
//
// ResourceDriver:
//
//	pb.ResourceDriverServer — separate gRPC service, also auto-registered
//	when the provider satisfies it.
//
// Per cycle 3 I-1 of the strict-contracts force-cutover design: plugin
// authors write ONE call; they cannot omit registration for a capability
// they implemented. That eliminates the registration-omission bug class
// (the same shape as the legacy InvokeService case-string-typo bug) by
// removing the manual step entirely.
//
// Capability discovery on the host side uses the existing ContractRegistry
// RPC + FileDescriptorSet mechanism (kept via §Salvage in the design);
// the SDK auto-publishes the registered services there in Task 5.
//
// Plugin authors who DO NOT want a capability advertised must NOT
// implement those methods at the Go level — there is no half-implemented
// stub-and-forget-to-register failure mode.
func RegisterAllIaCProviderServices(s *grpc.Server, provider any) error {
	return registerAllIaCProviderServicesWithOpts(s, provider, IaCServeOptions{})
}

// registerAllIaCProviderServicesWithOpts is the internal variant of
// RegisterAllIaCProviderServices that threads IaCServeOptions through to the
// PluginService bridge so disk-embedded manifests (via
// IaCServeOptions.ManifestProvider) flow into the bridge's GetManifest RPC.
// Public 2-arg callers go through RegisterAllIaCProviderServices, which
// delegates here with a zero-valued IaCServeOptions.
func registerAllIaCProviderServicesWithOpts(s *grpc.Server, provider any, opts IaCServeOptions) error {
	if err := registerIaCServicesOnly(s, provider); err != nil {
		return err
	}
	// Register a minimal PluginService so the wfctl host can call
	// GetContractRegistry to discover the typed IaC services registered
	// above. Strict-cutover IaC plugins (e.g. DO v1.0.0) that use
	// ServeIaCPlugin do NOT register the SDK grpcServer (which normally
	// handles GetContractRegistry for non-IaC plugins). Without this
	// bridge, wfctl's NewExternalPluginAdapter fails with
	// "unknown service workflow.plugin.v1.PluginService" when it calls
	// GetContractRegistry, blocking the typedIaCAdapter load path.
	//
	// Guard: skip registration if PluginService is already on the server
	// (e.g. a mixed plugin that called sdk.Serve AND RegisterAllIaC).
	// gRPC panics on double-registration; the guard prevents that.
	if _, alreadyRegistered := s.GetServiceInfo()[pb.PluginService_ServiceDesc.ServiceName]; !alreadyRegistered {
		bridge := &iacPluginServiceBridge{
			grpcSrv:      s,
			diskManifest: opts.ManifestProvider,
		}
		// Wire the optional grpc_server.go delegate when the caller supplied
		// any (legacy or typed) module/step providers. Zero-value across all
		// four maps ⇒ delegate stays nil ⇒ module/step RPCs continue returning
		// Unimplemented (current behavior preserved for strict-cutover IaC
		// plugins). Per decisions/0038 + decisions/0039.
		if opts.Modules != nil || opts.Steps != nil || opts.TypedModules != nil || opts.TypedSteps != nil {
			bridge.delegate = newGRPCServer(&mapBackedProvider{
				modules:      opts.Modules,
				steps:        opts.Steps,
				typedModules: opts.TypedModules,
				typedSteps:   opts.TypedSteps,
			})
		}
		pb.RegisterPluginServiceServer(s, bridge)
	}
	return nil
}

// registerIaCServicesOnly extracts the body of the original
// RegisterAllIaCProviderServices (nil checks + typed-nil hardening +
// IaCProviderRequired assertion + all optional-service auto-registration +
// ResourceDriver + IaCStateBackend auto-registration), EXCLUDING the
// PluginService bridge registration. Kept as a separate helper so the
// typed-nil + nil-provider hardening (R2-3) survives the extraction — moving
// the registration block alone would have split the hardening across two
// call sites.
func registerIaCServicesOnly(s *grpc.Server, provider any) error {
	if s == nil {
		return fmt.Errorf("RegisterAllIaCProviderServices: grpc server is nil")
	}
	if provider == nil {
		return fmt.Errorf("RegisterAllIaCProviderServices: provider is nil")
	}
	// Typed-nil hardening: a typed-nil pointer (e.g., var p *MyProvider;
	// RegisterAll(s, p)) wraps as a non-nil interface value but
	// dereferences to nil at first method call → panic. Reject early
	// with the same pattern the user-visible nil-check uses.
	if rv := reflect.ValueOf(provider); rv.Kind() == reflect.Pointer && rv.IsNil() {
		return fmt.Errorf("RegisterAllIaCProviderServices: provider is a typed-nil %T pointer", provider)
	}
	// Per workflow#699 (Task 5): pb.IaCProviderRequiredServer no longer
	// declares Apply, so this type-assertion auto-tightens to the trimmed
	// required surface. Plugins that retain a Go-level Apply method still
	// satisfy the interface (extra methods are permitted); plugins missing
	// any of the still-required methods (Initialize/Name/Version/
	// Capabilities/Plan/Destroy/Status/Import/ResolveSizing/
	// BootstrapStateBackend) fail-loud here as before.
	required, ok := provider.(pb.IaCProviderRequiredServer)
	if !ok {
		return fmt.Errorf(
			"RegisterAllIaCProviderServices: provider %T does not satisfy "+
				"pb.IaCProviderRequiredServer (missing methods); see "+
				"docs/plans/2026-05-10-strict-contracts-force-cutover-design.md",
			provider,
		)
	}
	pb.RegisterIaCProviderRequiredServer(s, required)

	if v, ok := provider.(pb.IaCProviderEnumeratorServer); ok {
		pb.RegisterIaCProviderEnumeratorServer(s, v)
	}
	if v, ok := provider.(pb.IaCProviderDriftDetectorServer); ok {
		pb.RegisterIaCProviderDriftDetectorServer(s, v)
	}
	if v, ok := provider.(pb.IaCProviderCredentialRevokerServer); ok {
		pb.RegisterIaCProviderCredentialRevokerServer(s, v)
	}
	if v, ok := provider.(pb.IaCProviderMigrationRepairerServer); ok {
		pb.RegisterIaCProviderMigrationRepairerServer(s, v)
	}
	if v, ok := provider.(pb.IaCProviderValidatorServer); ok {
		pb.RegisterIaCProviderValidatorServer(s, v)
	}
	if v, ok := provider.(pb.IaCProviderDriftConfigDetectorServer); ok {
		pb.RegisterIaCProviderDriftConfigDetectorServer(s, v)
	}
	// IaCProviderFinalizer is the workflow#695 Phase 2.5 optional service
	// for plugins needing a post-apply-loop finalizer hook under v2
	// dispatch. Per ADR 0024 the absence of this registration IS the
	// negative signal (no compat shim, no NotSupported flag) — the
	// wfctl-side typed adapter (cmd/wfctl/iac_typed_adapter.go)
	// service-presence-probes via its Finalizer() accessor and gates
	// the ApplyPlanHooks.OnPlanComplete wiring on a non-nil return.
	if v, ok := provider.(pb.IaCProviderFinalizerServer); ok {
		pb.RegisterIaCProviderFinalizerServer(s, v)
	}
	if v, ok := provider.(pb.ResourceDriverServer); ok {
		pb.RegisterResourceDriverServer(s, v)
	}
	// Per decisions/0035 (Amendment A2): IaCStateBackend is an optional
	// service auto-detected exactly like the IaCProvider* optionals — a
	// plugin whose provider type also implements pb.IaCStateBackendServer
	// serves it with no extra wiring. Note: a pure-storage plugin (one that
	// does NOT satisfy IaCProviderRequired) cannot use ServeIaCPlugin today
	// — a deferred limitation, not addressed here.
	if v, ok := provider.(pb.IaCStateBackendServer); ok {
		pb.RegisterIaCStateBackendServer(s, v)
	}
	return nil
}

// iacPluginServiceBridge is a minimal pb.PluginServiceServer registered on
// the gRPC server by RegisterAllIaCProviderServices. It implements
// GetContractRegistry (always) plus GetManifest (when diskManifest is wired
// via IaCServeOptions.ManifestProvider).
//
// Other PluginService methods (InvokeService, GetModuleTypes, etc.) remain
// unimplemented (via UnimplementedPluginServiceServer) — strict-cutover IaC
// plugins do not support string-dispatch or module/step/trigger contracts.
type iacPluginServiceBridge struct {
	pb.UnimplementedPluginServiceServer
	grpcSrv      *grpc.Server
	diskManifest *pluginpkg.PluginManifest

	// delegate, when non-nil, handles GetModuleTypes / CreateModule /
	// InitModule / StartModule / StopModule / DestroyModule / GetStepTypes /
	// CreateStep / ExecuteStep / DestroyStep by forwarding to grpc_server.go's
	// existing implementation. Constructed by
	// registerAllIaCProviderServicesWithOpts when IaCServeOptions.Modules or
	// .Steps is non-nil. Zero-value ⇒ those RPCs continue returning
	// Unimplemented via UnimplementedPluginServiceServer. See decisions/0038.
	delegate *grpcServer
}

// GetModuleTypes forwards to the delegate when wired, else falls back to the
// Unimplemented default. Same pattern for the 9 sibling forwarding methods.
func (b *iacPluginServiceBridge) GetModuleTypes(ctx context.Context, req *emptypb.Empty) (*pb.TypeList, error) {
	if b.delegate == nil {
		return b.UnimplementedPluginServiceServer.GetModuleTypes(ctx, req)
	}
	return b.delegate.GetModuleTypes(ctx, req)
}

func (b *iacPluginServiceBridge) CreateModule(ctx context.Context, req *pb.CreateModuleRequest) (*pb.HandleResponse, error) {
	if b.delegate == nil {
		return b.UnimplementedPluginServiceServer.CreateModule(ctx, req)
	}
	return b.delegate.CreateModule(ctx, req)
}

func (b *iacPluginServiceBridge) InitModule(ctx context.Context, req *pb.HandleRequest) (*pb.ErrorResponse, error) {
	if b.delegate == nil {
		return b.UnimplementedPluginServiceServer.InitModule(ctx, req)
	}
	return b.delegate.InitModule(ctx, req)
}

func (b *iacPluginServiceBridge) StartModule(ctx context.Context, req *pb.HandleRequest) (*pb.ErrorResponse, error) {
	if b.delegate == nil {
		return b.UnimplementedPluginServiceServer.StartModule(ctx, req)
	}
	return b.delegate.StartModule(ctx, req)
}

func (b *iacPluginServiceBridge) StopModule(ctx context.Context, req *pb.HandleRequest) (*pb.ErrorResponse, error) {
	if b.delegate == nil {
		return b.UnimplementedPluginServiceServer.StopModule(ctx, req)
	}
	return b.delegate.StopModule(ctx, req)
}

func (b *iacPluginServiceBridge) DestroyModule(ctx context.Context, req *pb.HandleRequest) (*pb.ErrorResponse, error) {
	if b.delegate == nil {
		return b.UnimplementedPluginServiceServer.DestroyModule(ctx, req)
	}
	return b.delegate.DestroyModule(ctx, req)
}

func (b *iacPluginServiceBridge) GetStepTypes(ctx context.Context, req *emptypb.Empty) (*pb.TypeList, error) {
	if b.delegate == nil {
		return b.UnimplementedPluginServiceServer.GetStepTypes(ctx, req)
	}
	return b.delegate.GetStepTypes(ctx, req)
}

func (b *iacPluginServiceBridge) CreateStep(ctx context.Context, req *pb.CreateStepRequest) (*pb.HandleResponse, error) {
	if b.delegate == nil {
		return b.UnimplementedPluginServiceServer.CreateStep(ctx, req)
	}
	return b.delegate.CreateStep(ctx, req)
}

func (b *iacPluginServiceBridge) ExecuteStep(ctx context.Context, req *pb.ExecuteStepRequest) (*pb.ExecuteStepResponse, error) {
	if b.delegate == nil {
		return b.UnimplementedPluginServiceServer.ExecuteStep(ctx, req)
	}
	return b.delegate.ExecuteStep(ctx, req)
}

func (b *iacPluginServiceBridge) DestroyStep(ctx context.Context, req *pb.HandleRequest) (*pb.ErrorResponse, error) {
	if b.delegate == nil {
		return b.UnimplementedPluginServiceServer.DestroyStep(ctx, req)
	}
	return b.delegate.DestroyStep(ctx, req)
}

// GetContractRegistry returns the set of gRPC services registered on
// grpcSrv at call time, encoded as a *pb.ContractRegistry. wfctl uses
// this to gate optional typed-client construction (Enumerator, DriftDetector,
// etc.) after loading an IaC plugin via discoverAndLoadIaCProvider.
func (b *iacPluginServiceBridge) GetContractRegistry(_ context.Context, _ *emptypb.Empty) (*pb.ContractRegistry, error) {
	return BuildContractRegistry(b.grpcSrv), nil
}

// GetManifest returns the disk-embedded *plugin.PluginManifest as a
// *pb.Manifest when set via IaCServeOptions.ManifestProvider. Returns
// codes.Unimplemented when no manifest is wired, which triggers the engine's
// disk-fallback path (workflow plan Task 1) — so even IaC plugins that
// haven't adopted sdk.EmbedManifest still get clean registration via the
// engine's manager.go-loaded plugin.json.
func (b *iacPluginServiceBridge) GetManifest(_ context.Context, _ *emptypb.Empty) (*pb.Manifest, error) {
	if b.diskManifest == nil {
		return nil, status.Error(codes.Unimplemented, "manifest not embedded; engine falls back to disk plugin.json")
	}
	return &pb.Manifest{
		Name:           b.diskManifest.Name,
		Version:        b.diskManifest.Version,
		Author:         b.diskManifest.Author,
		Description:    b.diskManifest.Description,
		ConfigMutable:  b.diskManifest.ConfigMutable,
		SampleCategory: b.diskManifest.SampleCategory,
	}, nil
}

// IaCServeOptions configures the IaC plugin gRPC server entrypoint.
//
// Plugin authors typically zero-value this; ServeIaCPlugin then uses the
// canonical host<->plugin handshake (ext.Handshake). The struct exists as
// a forward-extension point so future metadata fields (PluginInfo) can be
// added without breaking the API.
type IaCServeOptions struct {
	// PluginInfo overrides the default handshake/metadata. When nil,
	// ServeIaCPlugin uses ext.Handshake (the canonical wfctl<->plugin
	// handshake — required for compatibility with the workflow host).
	PluginInfo *PluginInfo

	// ManifestProvider, when set, is returned by the bridge's GetManifest
	// RPC. Typically populated via sdk.MustEmbedManifest from a go:embed-ed
	// plugin.json. When nil, GetManifest returns codes.Unimplemented and the
	// engine falls back to its manager.go-loaded plugin.json (workflow plan
	// Task 1).
	ManifestProvider *pluginpkg.PluginManifest

	// Modules supplies plugin-native module providers. When non-nil, the
	// bridge wires GetModuleTypes / CreateModule / InitModule / StartModule /
	// StopModule / DestroyModule to delegate to grpc_server.go's existing
	// PluginService implementation via a thin mapBackedProvider adapter.
	// Zero-value = current behavior (Unimplemented for those RPCs).
	// See decisions/0038.
	Modules map[string]ModuleProvider

	// Steps supplies plugin-native step providers. Same wiring rationale as
	// Modules; values are sdk.StepProvider — the same interface non-IaC
	// plugins consume via sdk.Serve.
	Steps map[string]StepProvider

	// TypedModules supplies plugin-native module providers that implement the
	// strict-proto TypedModuleProvider surface (sdk.TypedModuleFactory or a
	// custom implementor). When non-nil, mapBackedProvider implements
	// TypedModuleProvider and grpc_server.go's CreateModule path dispatches
	// CreateTypedModule on the looked-up entry — passing the host-supplied
	// *anypb.Any TypedConfig directly to the typed factory's proto-message
	// unpack. The legacy Modules map remains supported alongside (the
	// dispatch is Typed-first, then legacy-fallback). See decisions/0039.
	TypedModules map[string]TypedModuleProvider

	// TypedSteps supplies plugin-native step providers that implement
	// TypedStepProvider. Same wiring rationale as TypedModules. See
	// decisions/0039.
	TypedSteps map[string]TypedStepProvider
}

// mapBackedProvider adapts user-supplied module/step provider maps to the
// sdk.PluginProvider + sdk.ModuleProvider + sdk.StepProvider interfaces that
// grpc_server.go's existing PluginService implementation expects. Per
// decisions/0038, this is the smallest viable extraction path that lets the
// IaC bridge reuse newGRPCServer's handle-state + lifecycle code without
// refactoring grpc_server.go.
//
// The adapter is intentionally thin: ModuleTypes/StepTypes return map keys;
// CreateModule/CreateStep look up the named provider in the map and delegate.
// Manifest returns a zero-valued PluginManifest — the iacPluginServiceBridge
// implements GetManifest directly (using IaCServeOptions.ManifestProvider)
// and never calls back through this adapter, so Manifest's return value is
// never observed; it exists solely to satisfy the PluginProvider interface
// contract that newGRPCServer requires. ContractRegistry is intentionally NOT
// implemented — the iacPluginServiceBridge implements GetContractRegistry
// directly (walks the gRPC server's registered services) and never calls
// back through the delegate.
type mapBackedProvider struct {
	modules      map[string]ModuleProvider
	steps        map[string]StepProvider
	typedModules map[string]TypedModuleProvider
	typedSteps   map[string]TypedStepProvider
}

// Manifest satisfies sdk.PluginProvider. Return value is unobserved (the
// bridge handles GetManifest directly via IaCServeOptions.ManifestProvider)
// — the method exists only to satisfy the interface so newGRPCServer's
// PluginProvider parameter type-checks at compile time.
func (p *mapBackedProvider) Manifest() PluginManifest { return PluginManifest{} }

// ModuleTypes returns the keys of the legacy modules map in deterministic
// (lexicographic) order. Sorting matters because Go map iteration is
// randomized — without it, GetModuleTypes responses would differ run-to-run,
// breaking cache keys, golden files, and any caller that compares the list
// as an ordered sequence.
//
// The typed-module names are surfaced separately via TypedModuleTypes().
// grpc_server.go's GetModuleTypes calls both methods and merges the lists
// when the provider implements TypedModuleProvider (Typed-primary-first,
// then legacy-only extras, with duplicates skipped). See decisions/0039.
func (p *mapBackedProvider) ModuleTypes() []string {
	out := make([]string, 0, len(p.modules))
	for name := range p.modules {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// CreateModule looks up the named module provider in the legacy map and
// delegates to it. The typed map is checked separately via
// CreateTypedModule (the gRPC dispatcher tries the typed path first); a
// type registered ONLY in typedModules will surface here as "unknown" so the
// host can distinguish "not in legacy" from "not at all" — but in practice
// grpc_server.CreateModule short-circuits on the typed path and never reaches
// this code for a typed-only type.
func (p *mapBackedProvider) CreateModule(typeName, name string, config map[string]any) (ModuleInstance, error) {
	mp, ok := p.modules[typeName]
	if !ok {
		return nil, fmt.Errorf("mapBackedProvider: unknown module type %q", typeName)
	}
	return mp.CreateModule(typeName, name, config)
}

// TypedModuleTypes returns the keys of the typed-module map in deterministic
// (lexicographic) order. Required by TypedModuleProvider; consumed by
// grpc_server.go's GetModuleTypes when it merges typed + legacy names.
func (p *mapBackedProvider) TypedModuleTypes() []string {
	out := make([]string, 0, len(p.typedModules))
	for name := range p.typedModules {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// CreateTypedModule looks up the named typed-module provider and delegates
// the strict-proto factory call to it. Returns ErrTypedContractNotHandled
// when the type is not in the typed map — the gRPC dispatcher then falls
// back to the legacy CreateModule path. See decisions/0039.
func (p *mapBackedProvider) CreateTypedModule(typeName, name string, config *anypb.Any) (ModuleInstance, error) {
	mp, ok := p.typedModules[typeName]
	if !ok {
		return nil, fmt.Errorf("%w: module type %q", ErrTypedContractNotHandled, typeName)
	}
	return mp.CreateTypedModule(typeName, name, config)
}

// StepTypes returns the keys of the legacy steps map in deterministic
// (lexicographic) order. Same rationale + merge contract as ModuleTypes.
func (p *mapBackedProvider) StepTypes() []string {
	out := make([]string, 0, len(p.steps))
	for name := range p.steps {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// CreateStep looks up the named step provider in the legacy map and
// delegates to it. Same Typed-first dispatch semantics as CreateModule.
func (p *mapBackedProvider) CreateStep(typeName, name string, config map[string]any) (StepInstance, error) {
	sp, ok := p.steps[typeName]
	if !ok {
		return nil, fmt.Errorf("mapBackedProvider: unknown step type %q", typeName)
	}
	return sp.CreateStep(typeName, name, config)
}

// TypedStepTypes returns the keys of the typed-step map in deterministic
// (lexicographic) order. Required by TypedStepProvider; consumed by
// grpc_server.go's GetStepTypes when it merges typed + legacy names.
func (p *mapBackedProvider) TypedStepTypes() []string {
	out := make([]string, 0, len(p.typedSteps))
	for name := range p.typedSteps {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// CreateTypedStep looks up the named typed-step provider and delegates the
// strict-proto factory call to it. Returns ErrTypedContractNotHandled when
// the type is not in the typed map — the gRPC dispatcher then falls back to
// the legacy CreateStep path.
func (p *mapBackedProvider) CreateTypedStep(typeName, name string, config *anypb.Any) (StepInstance, error) {
	sp, ok := p.typedSteps[typeName]
	if !ok {
		return nil, fmt.Errorf("%w: step type %q", ErrTypedContractNotHandled, typeName)
	}
	return sp.CreateTypedStep(typeName, name, config)
}

// PluginInfo carries the metadata that go-plugin needs to serve an IaC
// plugin. Currently only HandshakeConfig is meaningful; reserved as the
// extension point for future Name/Version metadata fields without
// breaking the IaCServeOptions API.
type PluginInfo struct {
	// HandshakeConfig is the go-plugin handshake. Plugin authors should
	// leave this zero-valued to inherit ext.Handshake — the host (wfctl)
	// and plugin MUST agree on the cookie + protocol version, so override
	// only when implementing a non-workflow host.
	HandshakeConfig goplugin.HandshakeConfig
}

// iacGRPCPlugin implements goplugin's Plugin interface for the typed IaC
// contract. Service registration happens INSIDE GRPCServer per go-plugin
// v1.7.0 architecture — the framework owns the *grpc.Server lifecycle, so
// plugin authors cannot pre-create the server and forget to register a
// service on it.
//
// Note: the GoCodeAlone fork of go-plugin (v1.7.0) is gRPC-only and does
// not expose hashicorp/go-plugin's NetRPCUnsupportedPlugin embed or a
// GRPCPlugin convenience alias; the canonical Plugin interface (just
// GRPCServer + GRPCClient) is sufficient and matches the existing
// servePlugin pattern in serve.go.
type iacGRPCPlugin struct {
	provider any
	opts     IaCServeOptions
}

// GRPCServer is invoked by go-plugin once it has constructed the
// *grpc.Server. Delegates to registerAllIaCProviderServicesWithOpts so every
// typed IaC service the provider satisfies gets registered in one call AND
// the bridge picks up IaCServeOptions.ManifestProvider for the GetManifest
// RPC.
//
// Returning an error here causes go-plugin to abort plugin startup —
// surfacing missing required methods as a plugin-startup error rather
// than a generic "unimplemented" status at the first RPC dispatch.
func (p *iacGRPCPlugin) GRPCServer(_ *goplugin.GRPCBroker, s *grpc.Server) error {
	return registerAllIaCProviderServicesWithOpts(s, p.provider, p.opts)
}

// GRPCClient is unused on the plugin side. The workflow host (wfctl)
// builds its own typed pb.IaCProviderRequiredClient + per-optional
// clients directly from the gRPC connection; the iacGRPCPlugin's
// client-side adapter is therefore a no-op.
func (p *iacGRPCPlugin) GRPCClient(_ context.Context, _ *goplugin.GRPCBroker, _ *grpc.ClientConn) (any, error) {
	return nil, nil
}

// ServeIaCPlugin starts a typed IaC plugin gRPC server with auto
// registration of every IaC service the provider satisfies. Plugin
// authors call this once in main.go:
//
//	func main() {
//	    sdk.ServeIaCPlugin(&doProvider{}, sdk.IaCServeOptions{})
//	}
//
// Per cycle 3 I-1 of the strict-contracts force-cutover design, the
// service registration happens INSIDE go-plugin's GRPCServer callback
// (see iacGRPCPlugin.GRPCServer), so plugin authors cannot pre-create
// a *grpc.Server and forget to register a typed service on it.
//
// Blocks until the host process terminates the connection. Panics on
// invalid IaCServeOptions (e.g., partial handshake override missing
// MagicCookieKey or MagicCookieValue) — see resolveServeHandshake.
// Plugin authors fix the misconfig at the call site; the panic is
// preferable to a silent fallback that produces a broken handshake at
// dial time.
func ServeIaCPlugin(provider any, opts IaCServeOptions) {
	hs, err := resolveServeHandshake(opts)
	if err != nil {
		panic(fmt.Errorf("ServeIaCPlugin: %w", err))
	}
	goplugin.Serve(&goplugin.ServeConfig{
		HandshakeConfig: hs,
		Plugins: goplugin.PluginSet{
			"iac": &iacGRPCPlugin{provider: provider, opts: opts},
		},
		GRPCServer: goplugin.DefaultGRPCServer,
	})
}

// resolveServeHandshake returns the goplugin handshake to use for an IaC
// plugin. Defaults to ext.Handshake (the canonical wfctl<->plugin
// handshake) when the caller did not supply a PluginInfo OR supplied
// the zero-valued HandshakeConfig. Returns an error when the caller
// supplied a PARTIAL override (any non-zero field but missing
// MagicCookieKey or MagicCookieValue) — partial overrides produce a
// broken handshake at dial time, so the misconfig is rejected early
// rather than silently coerced to defaults.
//
// Per cycle 4 PR 600 IMPORTANT review (Copilot finding): the previous
// guard only checked MagicCookieKey != "", which silently accepted a
// caller setting ProtocolVersion or MagicCookieValue alone — fields
// that look intentional but cannot produce a valid handshake.
//
// Extracted from ServeIaCPlugin so the resolution rule is unit-testable
// without invoking goplugin.Serve's blocking loop.
func resolveServeHandshake(opts IaCServeOptions) (goplugin.HandshakeConfig, error) {
	if opts.PluginInfo == nil {
		return ext.Handshake, nil
	}
	hs := opts.PluginInfo.HandshakeConfig
	if hs == (goplugin.HandshakeConfig{}) {
		// Zero value: caller supplied PluginInfo{} but no handshake
		// override. Treat identically to opts.PluginInfo == nil.
		return ext.Handshake, nil
	}
	if hs.MagicCookieKey == "" || hs.MagicCookieValue == "" {
		return goplugin.HandshakeConfig{}, fmt.Errorf(
			"IaCServeOptions.PluginInfo.HandshakeConfig is a partial "+
				"override (MagicCookieKey=%q MagicCookieValue=%q "+
				"ProtocolVersion=%d): both MagicCookieKey AND "+
				"MagicCookieValue MUST be set when overriding the default "+
				"ext.Handshake — leave the whole struct zero-valued to "+
				"inherit the canonical wfctl<->plugin handshake",
			hs.MagicCookieKey, hs.MagicCookieValue, hs.ProtocolVersion,
		)
	}
	return hs, nil
}
