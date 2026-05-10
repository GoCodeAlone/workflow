package sdk

// Internal test (package sdk, not sdk_test) so the unexported
// iacGRPCPlugin can be exercised directly without spawning a real
// plugin subprocess.

import (
	"context"
	"strings"
	"testing"

	goplugin "github.com/GoCodeAlone/go-plugin"
	"google.golang.org/grpc"

	ext "github.com/GoCodeAlone/workflow/plugin/external"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

// TestIaCGRPCPlugin_GRPCServer_RegistersAllServices asserts that the
// go-plugin framework callback delegates to RegisterAllIaCProviderServices,
// so every typed service the provider satisfies is registered on the
// framework-managed *grpc.Server. This is the cycle 3 I-1 invariant:
// service registration happens INSIDE GRPCServer, not in caller-allocated
// servers.
func TestIaCGRPCPlugin_GRPCServer_RegistersAllServices(t *testing.T) {
	grpcSrv := grpc.NewServer()
	provider := &serveTestAllStub{}
	p := &iacGRPCPlugin{provider: provider}

	if err := p.GRPCServer(nil, grpcSrv); err != nil {
		t.Fatalf("GRPCServer returned error: %v", err)
	}

	info := grpcSrv.GetServiceInfo()
	want := []string{
		"workflow.plugin.external.iac.IaCProviderRequired",
		"workflow.plugin.external.iac.IaCProviderEnumerator",
		"workflow.plugin.external.iac.ResourceDriver",
	}
	for _, name := range want {
		if _, ok := info[name]; !ok {
			t.Errorf("expected %q registered; have: %v", name, len(info))
		}
	}
}

// TestIaCGRPCPlugin_GRPCServer_PropagatesAutoRegisterError asserts the
// callback surfaces RegisterAllIaCProviderServices errors so go-plugin
// aborts plugin startup with an actionable message rather than booting
// a half-registered server.
func TestIaCGRPCPlugin_GRPCServer_PropagatesAutoRegisterError(t *testing.T) {
	grpcSrv := grpc.NewServer()
	p := &iacGRPCPlugin{provider: &emptyServeStub{}}

	err := p.GRPCServer(nil, grpcSrv)
	if err == nil {
		t.Fatalf("expected error for unsatisfied required interface; got nil")
	}
	if !strings.Contains(err.Error(), "IaCProviderRequiredServer") {
		t.Fatalf("error must name the missing interface; got %q", err.Error())
	}
}

// TestIaCGRPCPlugin_GRPCClient_NoOp asserts the plugin-side GRPCClient
// adapter returns (nil, nil). The host (wfctl) builds its own typed
// pb.IaCProviderRequiredClient directly from the connection.
func TestIaCGRPCPlugin_GRPCClient_NoOp(t *testing.T) {
	p := &iacGRPCPlugin{}
	out, err := p.GRPCClient(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("GRPCClient returned error: %v", err)
	}
	if out != nil {
		t.Fatalf("expected nil client; got %T", out)
	}
}

// TestIaCGRPCPlugin_SatisfiesGoPluginPlugin guarantees that
// iacGRPCPlugin remains compatible with the goplugin.Plugin interface
// (GRPCServer + GRPCClient) — a refactor that renames either method
// or changes its signature would fail this assertion at compile time.
//
// (The GoCodeAlone fork of go-plugin is gRPC-only; there is no separate
// GRPCPlugin alias to assert against. Plugin is the canonical
// interface.)
func TestIaCGRPCPlugin_SatisfiesGoPluginPlugin(t *testing.T) {
	var _ goplugin.Plugin = (*iacGRPCPlugin)(nil)
}

// TestServeIaCPlugin_DefaultsToWorkflowHandshake_WhenPluginInfoNil
// asserts the entrypoint defaults to ext.Handshake when callers pass
// IaCServeOptions{} (no override). Verified indirectly via the
// resolveServeHandshake helper extracted from ServeIaCPlugin so the
// blocking goplugin.Serve loop is not invoked in tests.
func TestServeIaCPlugin_DefaultsToWorkflowHandshake_WhenPluginInfoNil(t *testing.T) {
	got := resolveServeHandshake(IaCServeOptions{})
	if got.MagicCookieKey != ext.Handshake.MagicCookieKey {
		t.Fatalf("expected default ext.Handshake; got cookie key %q", got.MagicCookieKey)
	}
	if got.MagicCookieValue != ext.Handshake.MagicCookieValue {
		t.Fatalf("expected default ext.Handshake; got cookie value %q", got.MagicCookieValue)
	}
	if got.ProtocolVersion != ext.Handshake.ProtocolVersion {
		t.Fatalf("expected default protocol version %d; got %d",
			ext.Handshake.ProtocolVersion, got.ProtocolVersion)
	}
}

// TestServeIaCPlugin_HonorsOverrideHandshake_WhenProvided asserts that
// callers can supply a non-zero handshake (e.g., for non-workflow hosts)
// via IaCServeOptions.PluginInfo.HandshakeConfig.
func TestServeIaCPlugin_HonorsOverrideHandshake_WhenProvided(t *testing.T) {
	custom := goplugin.HandshakeConfig{
		ProtocolVersion:  42,
		MagicCookieKey:   "CUSTOM_COOKIE",
		MagicCookieValue: "v",
	}
	got := resolveServeHandshake(IaCServeOptions{
		PluginInfo: &PluginInfo{HandshakeConfig: custom},
	})
	if got != custom {
		t.Fatalf("expected custom handshake; got %+v", got)
	}
}

// serveTestAllStub satisfies Required + Enumerator + ResourceDriver to
// exercise the auto-registration path through the GRPCServer callback.
type serveTestAllStub struct {
	pb.UnimplementedIaCProviderRequiredServer
	pb.UnimplementedIaCProviderEnumeratorServer
	pb.UnimplementedResourceDriverServer
}

// emptyServeStub satisfies no IaC interface. The GRPCServer callback
// MUST surface this as an error so go-plugin aborts startup.
type emptyServeStub struct{}
