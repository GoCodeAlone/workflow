package external

// iac_provider_wiring_test.go — Task 3: WiringHook service registration for
// external iac.provider plugins.
//
// Tests that ExternalPluginAdapter.WiringHooks() returns a hook when the plugin
// advertises IaCProviderRequired, and that running the hook registers the adapter
// as an interfaces.IaCProvider service in the modular application DI graph.
//
// Uses package external (internal test) to access unexported PluginClient fields,
// matching the pattern in adapter_test.go.

import (
	"context"
	"net"
	"testing"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/iac/providerclient"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/plugin"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// ─── Fake gRPC server helpers ────────────────────────────────────────────────

// minimalIaCProviderServer implements just enough of IaCProviderRequiredServer
// for the WiringHook to succeed (Conn() is all that matters; RPC methods
// are unused in the hook itself).
type minimalIaCProviderServer struct {
	pb.UnimplementedIaCProviderRequiredServer
}

func (s *minimalIaCProviderServer) Name(_ context.Context, _ *pb.NameRequest) (*pb.NameResponse, error) {
	return &pb.NameResponse{Name: "test-iac-provider"}, nil
}

func (s *minimalIaCProviderServer) Version(_ context.Context, _ *pb.VersionRequest) (*pb.VersionResponse, error) {
	return &pb.VersionResponse{Version: "1.0.0"}, nil
}

func (s *minimalIaCProviderServer) Initialize(_ context.Context, _ *pb.InitializeRequest) (*pb.InitializeResponse, error) {
	return &pb.InitializeResponse{}, nil
}

func (s *minimalIaCProviderServer) Capabilities(_ context.Context, _ *pb.CapabilitiesRequest) (*pb.CapabilitiesResponse, error) {
	return &pb.CapabilitiesResponse{}, nil
}

func (s *minimalIaCProviderServer) Plan(_ context.Context, _ *pb.PlanRequest) (*pb.PlanResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not needed in test")
}

func (s *minimalIaCProviderServer) Destroy(_ context.Context, _ *pb.DestroyRequest) (*pb.DestroyResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not needed in test")
}

func (s *minimalIaCProviderServer) Status(_ context.Context, _ *pb.StatusRequest) (*pb.StatusResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not needed in test")
}

func (s *minimalIaCProviderServer) Import(_ context.Context, _ *pb.ImportRequest) (*pb.ImportResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not needed in test")
}

func (s *minimalIaCProviderServer) ResolveSizing(_ context.Context, _ *pb.ResolveSizingRequest) (*pb.ResolveSizingResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not needed in test")
}

func (s *minimalIaCProviderServer) BootstrapStateBackend(_ context.Context, _ *pb.BootstrapStateBackendRequest) (*pb.BootstrapStateBackendResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not needed in test")
}

// startIaCProviderServer starts an in-process gRPC server with the required
// IaCProviderRequired service registered. Returns a *grpc.ClientConn.
func startIaCProviderServer(t *testing.T) *grpc.ClientConn {
	t.Helper()
	lis := bufconn.Listen(4 << 20)
	t.Cleanup(func() { _ = lis.Close() })
	srv := grpc.NewServer()
	pb.RegisterIaCProviderRequiredServer(srv, &minimalIaCProviderServer{})
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// newIaCProviderAdapter builds an ExternalPluginAdapter that advertises the
// IaCProviderRequired service via its ContractRegistry and holds a live
// gRPC connection to the fake server. Uses package-internal access to
// construct the adapter without a full go-plugin subprocess.
func newIaCProviderAdapter(t *testing.T, pluginName string, conn *grpc.ClientConn) *ExternalPluginAdapter {
	t.Helper()
	iacServiceName := pb.IaCProviderRequired_ServiceDesc.ServiceName
	diskManifest := &plugin.PluginManifest{
		Name:        pluginName,
		Version:     "1.0.0",
		IaCServices: []string{iacServiceName},
	}
	// Build a ContractRegistry advertising the IaCProviderRequired service.
	registry := &pb.ContractRegistry{
		Contracts: []*pb.ContractDescriptor{
			{
				Kind:        pb.ContractKind_CONTRACT_KIND_SERVICE,
				ServiceName: iacServiceName,
				Method:      "Plan", // representative method
			},
		},
	}
	a := &ExternalPluginAdapter{
		name:             pluginName,
		manifest:         &pb.Manifest{Name: pluginName, Version: "1.0.0"},
		diskManifest:     diskManifest,
		contractRegistry: registry,
		contracts:        buildContractDescriptorCache(registry),
		client:           &PluginClient{conn: conn},
	}
	return a
}

// ─── Tests ──────────────────────────────────────────────────────────────────

// TestWiringHook_IaCProviderPlugin_ReturnsHook asserts that an adapter whose
// diskManifest.IaCServices includes IaCProviderRequired returns a non-empty
// WiringHooks() slice.
func TestWiringHook_IaCProviderPlugin_ReturnsHook(t *testing.T) {
	conn := startIaCProviderServer(t)
	a := newIaCProviderAdapter(t, "my-iac-provider", conn)

	hooks := a.WiringHooks()
	if len(hooks) == 0 {
		t.Fatal("WiringHooks() must return at least one hook for an iac.provider plugin")
	}
	// Verify the hook has a meaningful name (doc convention).
	if hooks[0].Name == "" {
		t.Error("WiringHook.Name must not be empty")
	}
}

// TestWiringHook_NonIaCPlugin_ReturnsNil asserts that an adapter without
// IaCProviderRequired in its ContractRegistry returns an empty WiringHooks() slice.
func TestWiringHook_NonIaCPlugin_ReturnsNil(t *testing.T) {
	// Build an adapter with no IaCServices in the diskManifest.
	diskManifest := &plugin.PluginManifest{Name: "non-iac-plugin", Version: "1.0.0"}
	a := &ExternalPluginAdapter{
		name:             "non-iac-plugin",
		manifest:         &pb.Manifest{Name: "non-iac-plugin"},
		diskManifest:     diskManifest,
		contractRegistry: &pb.ContractRegistry{},
		contracts:        buildContractDescriptorCache(&pb.ContractRegistry{}),
	}
	if hooks := a.WiringHooks(); len(hooks) != 0 {
		t.Errorf("WiringHooks() for non-iac plugin = %v (len %d), want empty", hooks, len(hooks))
	}
}

// TestWiringHook_RegistersIaCProviderService asserts that running the WiringHook
// registers the adapter as an interfaces.IaCProvider service in the modular app
// DI graph under the plugin name. This is the core of Task 3.
func TestWiringHook_RegistersIaCProviderService(t *testing.T) {
	conn := startIaCProviderServer(t)
	a := newIaCProviderAdapter(t, "my-iac-provider", conn)

	hooks := a.WiringHooks()
	if len(hooks) == 0 {
		t.Fatal("expected at least one WiringHook")
	}

	// Create a real modular application (the same type the engine uses).
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), nil)

	// Run the hook (simulating what engine.BuildFromConfig does after app.Init()).
	if err := hooks[0].Hook(app, nil); err != nil {
		t.Fatalf("WiringHook.Hook returned error: %v", err)
	}

	// Assert app.GetService("my-iac-provider", &p) resolves a non-nil interfaces.IaCProvider.
	var p interfaces.IaCProvider
	if err := app.GetService("my-iac-provider", &p); err != nil {
		t.Fatalf("app.GetService(\"my-iac-provider\", &p): %v", err)
	}
	if p == nil {
		t.Fatal("registered service is nil")
	}

	// Verify it is a *providerclient.Adapter (not some other type).
	if _, ok := p.(*providerclient.Adapter); !ok {
		t.Errorf("registered service is %T, want *providerclient.Adapter", p)
	}
}

// TestWiringHook_TwoPlugins_RegisterUnderDistinctNames asserts that two distinct
// iac.provider plugins register under distinct names, both resolvable.
func TestWiringHook_TwoPlugins_RegisterUnderDistinctNames(t *testing.T) {
	conn1 := startIaCProviderServer(t)
	conn2 := startIaCProviderServer(t)

	a1 := newIaCProviderAdapter(t, "provider-alpha", conn1)
	a2 := newIaCProviderAdapter(t, "provider-beta", conn2)

	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), nil)

	// Run both hooks.
	for _, a := range []*ExternalPluginAdapter{a1, a2} {
		hooks := a.WiringHooks()
		if len(hooks) == 0 {
			t.Fatalf("expected WiringHooks for %T", a)
		}
		if err := hooks[0].Hook(app, nil); err != nil {
			t.Fatalf("WiringHook.Hook error: %v", err)
		}
	}

	// Both must be resolvable under their distinct names.
	var p1, p2 interfaces.IaCProvider
	if err := app.GetService("provider-alpha", &p1); err != nil {
		t.Fatalf("GetService(provider-alpha): %v", err)
	}
	if err := app.GetService("provider-beta", &p2); err != nil {
		t.Fatalf("GetService(provider-beta): %v", err)
	}
	if p1 == nil || p2 == nil {
		t.Fatal("one or both providers are nil")
	}
	// Distinct — must not be the same pointer.
	if p1 == p2 {
		t.Error("provider-alpha and provider-beta resolved to the same service")
	}
}

// TestWiringHook_IaCProviderPlugin_ReturnsHook and the functions below must
// not reference the 'external' package (we're inside it).
// Ensure the used import is live by using it in one test.
var _ = (*providerclient.Adapter)(nil)
