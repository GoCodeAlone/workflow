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

func TestIaCGRPCPlugin_GRPCServer_SharesServerWithProvider(t *testing.T) {
	grpcSrv := grpc.NewServer()
	provider := &serveTestServerAwareStub{}
	p := &iacGRPCPlugin{provider: provider}

	if err := p.GRPCServer(nil, grpcSrv); err != nil {
		t.Fatalf("GRPCServer returned error: %v", err)
	}
	if provider.grpcSrv != grpcSrv {
		t.Fatalf("expected provider SetGRPCServer hook to receive framework server")
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
	got, err := resolveServeHandshake(IaCServeOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
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

// TestServeIaCPlugin_DefaultsToWorkflowHandshake_WhenPluginInfoZeroValue
// asserts that a PluginInfo with the zero-valued HandshakeConfig is
// treated identically to a nil PluginInfo — caller passed an empty
// PluginInfo{} for forward-extensibility, not as a partial override.
func TestServeIaCPlugin_DefaultsToWorkflowHandshake_WhenPluginInfoZeroValue(t *testing.T) {
	got, err := resolveServeHandshake(IaCServeOptions{PluginInfo: &PluginInfo{}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != ext.Handshake {
		t.Fatalf("expected ext.Handshake; got %+v", got)
	}
}

// TestServeIaCPlugin_HonorsOverrideHandshake_WhenProvided asserts that
// callers can supply a non-zero handshake (e.g., for non-workflow hosts)
// via IaCServeOptions.PluginInfo.HandshakeConfig — both MagicCookieKey
// AND MagicCookieValue must be set for the override to be valid.
func TestServeIaCPlugin_HonorsOverrideHandshake_WhenProvided(t *testing.T) {
	custom := goplugin.HandshakeConfig{
		ProtocolVersion:  42,
		MagicCookieKey:   "CUSTOM_COOKIE",
		MagicCookieValue: "v",
	}
	got, err := resolveServeHandshake(IaCServeOptions{
		PluginInfo: &PluginInfo{HandshakeConfig: custom},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != custom {
		t.Fatalf("expected custom handshake; got %+v", got)
	}
}

// TestServeIaCPlugin_PartialHandshakeOverride_ReturnsError asserts that
// a partial handshake override (any non-zero field but missing
// MagicCookieKey or MagicCookieValue) is rejected with a typed error
// rather than silently coerced to ext.Handshake. Per cycle 4 PR 600
// IMPORTANT review (Copilot finding): partial overrides look
// intentional but cannot produce a valid handshake; falling back to
// defaults silently swallows the misconfig until dial-time when the
// host rejects the cookie.
func TestServeIaCPlugin_PartialHandshakeOverride_ReturnsError(t *testing.T) {
	cases := []struct {
		name string
		hs   goplugin.HandshakeConfig
	}{
		{
			name: "only_protocol_version_set",
			hs:   goplugin.HandshakeConfig{ProtocolVersion: 7},
		},
		{
			name: "only_magic_cookie_key_set",
			hs:   goplugin.HandshakeConfig{MagicCookieKey: "X"},
		},
		{
			name: "only_magic_cookie_value_set",
			hs:   goplugin.HandshakeConfig{MagicCookieValue: "Y"},
		},
		{
			name: "key_set_value_empty",
			hs: goplugin.HandshakeConfig{
				ProtocolVersion: 7,
				MagicCookieKey:  "X",
			},
		},
		{
			name: "value_set_key_empty",
			hs: goplugin.HandshakeConfig{
				ProtocolVersion:  7,
				MagicCookieValue: "Y",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := resolveServeHandshake(IaCServeOptions{
				PluginInfo: &PluginInfo{HandshakeConfig: tc.hs},
			})
			if err == nil {
				t.Fatalf("expected error for partial override %+v; got nil", tc.hs)
			}
			if !strings.Contains(err.Error(), "partial") {
				t.Errorf("error must name 'partial'; got %q", err.Error())
			}
		})
	}
}

// serveTestAllStub satisfies Required + Enumerator + ResourceDriver to
// exercise the auto-registration path through the GRPCServer callback.
type serveTestAllStub struct {
	pb.UnimplementedIaCProviderRequiredServer
	pb.UnimplementedIaCProviderEnumeratorServer
	pb.UnimplementedResourceDriverServer
}

type serveTestServerAwareStub struct {
	serveTestAllStub
	grpcSrv *grpc.Server
}

func (s *serveTestServerAwareStub) SetGRPCServer(grpcSrv *grpc.Server) {
	s.grpcSrv = grpcSrv
}

// emptyServeStub satisfies no IaC interface. The GRPCServer callback
// MUST surface this as an error so go-plugin aborts startup.
type emptyServeStub struct{}
