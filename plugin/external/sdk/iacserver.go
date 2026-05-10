package sdk

import (
	"context"
	"fmt"

	goplugin "github.com/GoCodeAlone/go-plugin"
	"google.golang.org/grpc"

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
	if s == nil {
		return fmt.Errorf("RegisterAllIaCProviderServices: grpc server is nil")
	}
	if provider == nil {
		return fmt.Errorf("RegisterAllIaCProviderServices: provider is nil")
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
}

// GRPCServer is invoked by go-plugin once it has constructed the
// *grpc.Server. Delegates to RegisterAllIaCProviderServices so every
// typed IaC service the provider satisfies gets registered in one call.
//
// Returning an error here causes go-plugin to abort plugin startup —
// surfacing missing required methods as a plugin-startup error rather
// than a generic "unimplemented" status at the first RPC dispatch.
func (p *iacGRPCPlugin) GRPCServer(_ *goplugin.GRPCBroker, s *grpc.Server) error {
	return RegisterAllIaCProviderServices(s, p.provider)
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
// Blocks until the host process terminates the connection.
func ServeIaCPlugin(provider any, opts IaCServeOptions) {
	goplugin.Serve(&goplugin.ServeConfig{
		HandshakeConfig: resolveServeHandshake(opts),
		Plugins: goplugin.PluginSet{
			"iac": &iacGRPCPlugin{provider: provider},
		},
		GRPCServer: goplugin.DefaultGRPCServer,
	})
}

// resolveServeHandshake returns the goplugin handshake to use for an IaC
// plugin. Defaults to ext.Handshake (the canonical wfctl<->plugin handshake)
// when the caller did not supply a non-zero PluginInfo.HandshakeConfig.
// Extracted from ServeIaCPlugin so the resolution rule is unit-testable
// without invoking goplugin.Serve's blocking loop.
func resolveServeHandshake(opts IaCServeOptions) goplugin.HandshakeConfig {
	if opts.PluginInfo != nil && opts.PluginInfo.HandshakeConfig.MagicCookieKey != "" {
		return opts.PluginInfo.HandshakeConfig
	}
	return ext.Handshake
}
