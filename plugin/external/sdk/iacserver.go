package sdk

import (
	"context"
	"fmt"
	"reflect"

	goplugin "github.com/GoCodeAlone/go-plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
		pb.RegisterPluginServiceServer(s, &iacPluginServiceBridge{
			grpcSrv:      s,
			diskManifest: opts.ManifestProvider,
		})
	}
	return nil
}

// registerIaCServicesOnly extracts the body of the original
// RegisterAllIaCProviderServices (nil checks + typed-nil hardening +
// IaCProviderRequired assertion + all optional-service auto-registration +
// ResourceDriver auto-registration), EXCLUDING the PluginService bridge
// registration. Kept as a separate helper so the typed-nil + nil-provider
// hardening (R2-3) survives the extraction — moving the registration block
// alone would have split the hardening across two call sites.
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
	if v, ok := provider.(pb.ResourceDriverServer); ok {
		pb.RegisterResourceDriverServer(s, v)
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
