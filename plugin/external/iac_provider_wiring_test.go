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
	"encoding/json"
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

// minimalResourceDriverServer implements just enough of ResourceDriverServer for
// the end-to-end wiring assertion: a Create call must reach the plugin and return
// a canned ResourceOutput, proving the WiringHook constructed a live driver
// bridge (not the unimplemented stub). It echoes the received spec.config_json
// back into the output's outputs_json under "received_config" so the test can
// assert the Config JSON round-tripped across the gRPC boundary.
type minimalResourceDriverServer struct {
	pb.UnimplementedResourceDriverServer
}

func (s *minimalResourceDriverServer) Create(_ context.Context, req *pb.ResourceCreateRequest) (*pb.ResourceCreateResponse, error) {
	// Decode the inbound config_json and echo it back so the caller can verify
	// the spec.Config survived marshaling across the wire.
	var cfg map[string]any
	if raw := req.GetSpec().GetConfigJson(); len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "config_json not valid JSON: %v", err)
		}
	}
	outputsJSON, err := json.Marshal(map[string]any{"received_config": cfg})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "marshal outputs: %v", err)
	}
	return &pb.ResourceCreateResponse{
		Output: &pb.ResourceOutput{
			Name:        req.GetSpec().GetName(),
			Type:        req.GetResourceType(),
			ProviderId:  "wired-prov-1",
			OutputsJson: outputsJSON,
			Status:      "active",
		},
	}, nil
}

// startIaCProviderServer starts an in-process gRPC server with the required
// IaCProviderRequired service registered. Returns a *grpc.ClientConn.
func startIaCProviderServer(t *testing.T) *grpc.ClientConn {
	t.Helper()
	return startIaCProviderServerWith(t, false)
}

// startIaCProviderServerWith starts an in-process gRPC server with the required
// IaCProviderRequired service registered, optionally also registering the
// ResourceDriver service. Returns a *grpc.ClientConn.
func startIaCProviderServerWith(t *testing.T, withResourceDriver bool) *grpc.ClientConn {
	t.Helper()
	lis := bufconn.Listen(4 << 20)
	t.Cleanup(func() { _ = lis.Close() })
	srv := grpc.NewServer()
	pb.RegisterIaCProviderRequiredServer(srv, &minimalIaCProviderServer{})
	if withResourceDriver {
		pb.RegisterResourceDriverServer(srv, &minimalResourceDriverServer{})
	}
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
	return newIaCProviderAdapterWithOptional(t, pluginName, conn)
}

// newIaCProviderAdapterWithOptional builds an ExternalPluginAdapter that
// advertises IaCProviderRequired plus any extra optional service names supplied,
// each as a CONTRACT_KIND_SERVICE descriptor in the ContractRegistry. This lets
// tests drive advertisedOptionalIaCServices() and the end-to-end WiringHook path
// for optional services (ResourceDriver, Runner, etc.).
func newIaCProviderAdapterWithOptional(t *testing.T, pluginName string, conn *grpc.ClientConn, optionalServiceNames ...string) *ExternalPluginAdapter {
	t.Helper()
	iacServiceName := pb.IaCProviderRequired_ServiceDesc.ServiceName
	diskManifest := &plugin.PluginManifest{
		Name:        pluginName,
		Version:     "1.0.0",
		IaCServices: append([]string{iacServiceName}, optionalServiceNames...),
	}
	// Build a ContractRegistry advertising the IaCProviderRequired service plus
	// any optional services.
	contracts := []*pb.ContractDescriptor{
		{
			Kind:        pb.ContractKind_CONTRACT_KIND_SERVICE,
			ServiceName: iacServiceName,
			Method:      "Plan", // representative method
		},
	}
	for _, name := range optionalServiceNames {
		contracts = append(contracts, &pb.ContractDescriptor{
			Kind:        pb.ContractKind_CONTRACT_KIND_SERVICE,
			ServiceName: name,
		})
	}
	registry := &pb.ContractRegistry{Contracts: contracts}
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

// TestAdvertisedOptionalIaCServices_ResourceDriver asserts that a ContractRegistry
// advertising the ResourceDriver service is forwarded by advertisedOptionalIaCServices,
// and that a registry WITHOUT it excludes it. This is the gap PR13 left open: the
// adapter wired ResourceDriver, but the engine never advertised it, so it stayed nil.
func TestAdvertisedOptionalIaCServices_ResourceDriver(t *testing.T) {
	conn := startIaCProviderServer(t)

	// WITH ResourceDriver advertised → present in the map.
	withRD := newIaCProviderAdapterWithOptional(t, "rd-provider", conn, providerclient.IaCServiceResourceDriver)
	got := withRD.advertisedOptionalIaCServices()
	if !got[providerclient.IaCServiceResourceDriver] {
		t.Errorf("advertisedOptionalIaCServices() = %v, want IaCServiceResourceDriver=true", got)
	}

	// WITHOUT ResourceDriver advertised → absent from the map.
	withoutRD := newIaCProviderAdapter(t, "plain-provider", conn)
	got2 := withoutRD.advertisedOptionalIaCServices()
	if got2[providerclient.IaCServiceResourceDriver] {
		t.Errorf("advertisedOptionalIaCServices() = %v, want IaCServiceResourceDriver absent when unadvertised", got2)
	}
}

// TestAdvertisedOptionalIaCServices_Runner asserts the Runner service (added in
// #850) is likewise forwarded when advertised — the switch previously omitted it
// despite the doc comment claiming Runner was collected.
func TestAdvertisedOptionalIaCServices_Runner(t *testing.T) {
	conn := startIaCProviderServer(t)

	withRunner := newIaCProviderAdapterWithOptional(t, "runner-provider", conn, providerclient.IaCServiceRunner)
	got := withRunner.advertisedOptionalIaCServices()
	if !got[providerclient.IaCServiceRunner] {
		t.Errorf("advertisedOptionalIaCServices() = %v, want IaCServiceRunner=true", got)
	}
}

// TestWiringHook_ResourceDriver_WiredEndToEnd is the end-to-end assertion: a plugin
// that advertises ResourceDriver, run through the WiringHook, registers a
// *providerclient.Adapter whose ResourceDriver(type) returns the REAL bridge (not
// ErrProviderMethodUnimplemented), and a Create round-trips to the fake server.
// This closes the half-connected runtime path PR13's lead-verification found.
func TestWiringHook_ResourceDriver_WiredEndToEnd(t *testing.T) {
	conn := startIaCProviderServerWith(t, true /* withResourceDriver */)
	a := newIaCProviderAdapterWithOptional(t, "rd-provider", conn, providerclient.IaCServiceResourceDriver)

	hooks := a.WiringHooks()
	if len(hooks) == 0 {
		t.Fatal("expected at least one WiringHook")
	}

	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), nil)
	if err := hooks[0].Hook(app, nil); err != nil {
		t.Fatalf("WiringHook.Hook returned error: %v", err)
	}

	var p interfaces.IaCProvider
	if err := app.GetService("rd-provider", &p); err != nil {
		t.Fatalf("app.GetService(rd-provider): %v", err)
	}

	// The registered provider must expose the ResourceDriver via the accessor.
	rdp, ok := p.(providerclient.ResourceDriverProvider)
	if !ok {
		t.Fatalf("registered provider %T does not satisfy ResourceDriverProvider", p)
	}
	driver, err := rdp.ResourceDriver("stub.database")
	if err != nil {
		t.Fatalf("ResourceDriver() returned error (advertisement not forwarded?): %v", err)
	}
	if driver == nil {
		t.Fatal("ResourceDriver() returned nil bridge despite advertisement")
	}

	// Create must round-trip to the fake ResourceDriver server.
	out, err := driver.Create(context.Background(), interfaces.ResourceSpec{
		Name:   "mydb",
		Type:   "stub.database",
		Config: map[string]any{"engine": "postgres"},
	})
	if err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}
	if out == nil || out.ProviderID != "wired-prov-1" {
		t.Fatalf("Create() output = %+v, want ProviderID=wired-prov-1", out)
	}

	// Assert the spec.Config JSON round-tripped across the gRPC boundary: the
	// fake server echoes the decoded config back under "received_config".
	received, ok := out.Outputs["received_config"].(map[string]any)
	if !ok {
		t.Fatalf("Create() output missing received_config echo; outputs = %+v", out.Outputs)
	}
	if received["engine"] != "postgres" {
		t.Errorf("config_json round-trip: received_config[engine] = %v, want postgres", received["engine"])
	}
}

// TestWiringHook_ResourceDriver_UnimplementedWhenUnadvertised asserts the negative
// path survives the fix: a plugin that does NOT advertise ResourceDriver still
// yields ErrProviderMethodUnimplemented from ResourceDriver(), so callers' skip/
// fallback logic remains reachable.
func TestWiringHook_ResourceDriver_UnimplementedWhenUnadvertised(t *testing.T) {
	conn := startIaCProviderServer(t)
	a := newIaCProviderAdapter(t, "plain-provider", conn)

	hooks := a.WiringHooks()
	if len(hooks) == 0 {
		t.Fatal("expected at least one WiringHook")
	}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), nil)
	if err := hooks[0].Hook(app, nil); err != nil {
		t.Fatalf("WiringHook.Hook returned error: %v", err)
	}

	var p interfaces.IaCProvider
	if err := app.GetService("plain-provider", &p); err != nil {
		t.Fatalf("app.GetService(plain-provider): %v", err)
	}
	rdp, ok := p.(providerclient.ResourceDriverProvider)
	if !ok {
		t.Fatalf("registered provider %T does not satisfy ResourceDriverProvider", p)
	}
	if _, err := rdp.ResourceDriver("stub.database"); err == nil {
		t.Fatal("ResourceDriver() on unadvertised plugin must return error")
	}
}

// TestWiringHook_IaCProviderPlugin_ReturnsHook and the functions below must
// not reference the 'external' package (we're inside it).
// Ensure the used import is live by using it in one test.
var _ = (*providerclient.Adapter)(nil)
