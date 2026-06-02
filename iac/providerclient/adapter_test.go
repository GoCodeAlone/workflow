package providerclient_test

import (
	"context"
	"encoding/json"
	"net"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/providerclient"
	"github.com/GoCodeAlone/workflow/interfaces"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// Compile-time interface assertions — the primary guard for Task 2.
var _ interfaces.IaCProvider = (*providerclient.Adapter)(nil)
var _ interfaces.IaCProviderRegionLister = (*providerclient.Adapter)(nil)

// ─── In-proc fake gRPC servers ──────────────────────────────────────────────

// fakeRequiredServer implements IaCProviderRequiredServer with deterministic responses.
type fakeRequiredServer struct {
	pb.UnimplementedIaCProviderRequiredServer
}

func (s *fakeRequiredServer) Name(_ context.Context, _ *pb.NameRequest) (*pb.NameResponse, error) {
	return &pb.NameResponse{Name: "fake-provider"}, nil
}

func (s *fakeRequiredServer) Version(_ context.Context, _ *pb.VersionRequest) (*pb.VersionResponse, error) {
	return &pb.VersionResponse{Version: "0.1.0"}, nil
}

func (s *fakeRequiredServer) Initialize(_ context.Context, _ *pb.InitializeRequest) (*pb.InitializeResponse, error) {
	return &pb.InitializeResponse{}, nil
}

func (s *fakeRequiredServer) Capabilities(_ context.Context, _ *pb.CapabilitiesRequest) (*pb.CapabilitiesResponse, error) {
	return &pb.CapabilitiesResponse{
		Capabilities: []*pb.IaCCapabilityDeclaration{
			{ResourceType: "infra.vpc", Tier: 1, Operations: []string{"create", "delete"}},
		},
	}, nil
}

func (s *fakeRequiredServer) Plan(_ context.Context, req *pb.PlanRequest) (*pb.PlanResponse, error) {
	return &pb.PlanResponse{
		Plan: &pb.IaCPlan{
			Id:          "plan-1",
			DesiredHash: "abc123",
		},
	}, nil
}

func (s *fakeRequiredServer) Destroy(_ context.Context, req *pb.DestroyRequest) (*pb.DestroyResponse, error) {
	destroyed := make([]string, 0, len(req.GetRefs()))
	for _, r := range req.GetRefs() {
		destroyed = append(destroyed, r.GetName())
	}
	return &pb.DestroyResponse{Result: &pb.DestroyResult{Destroyed: destroyed}}, nil
}

func (s *fakeRequiredServer) Status(_ context.Context, req *pb.StatusRequest) (*pb.StatusResponse, error) {
	statuses := make([]*pb.ResourceStatus, 0, len(req.GetRefs()))
	for _, r := range req.GetRefs() {
		outputsJSON, _ := json.Marshal(map[string]any{"id": r.GetProviderId()})
		statuses = append(statuses, &pb.ResourceStatus{
			Name:        r.GetName(),
			Type:        r.GetType(),
			ProviderId:  r.GetProviderId(),
			Status:      "running",
			OutputsJson: outputsJSON,
		})
	}
	return &pb.StatusResponse{Statuses: statuses}, nil
}

func (s *fakeRequiredServer) Import(_ context.Context, _ *pb.ImportRequest) (*pb.ImportResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented in fake")
}

func (s *fakeRequiredServer) ResolveSizing(_ context.Context, _ *pb.ResolveSizingRequest) (*pb.ResolveSizingResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented in fake")
}

func (s *fakeRequiredServer) BootstrapStateBackend(_ context.Context, _ *pb.BootstrapStateBackendRequest) (*pb.BootstrapStateBackendResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented in fake")
}

// fakeRegionListerServer implements IaCProviderRegionListerServer.
type fakeRegionListerServer struct {
	pb.UnimplementedIaCProviderRegionListerServer
}

func (s *fakeRegionListerServer) ListRegions(_ context.Context, req *pb.ListRegionsRequest) (*pb.ListRegionsResponse, error) {
	return &pb.ListRegionsResponse{
		Regions: []*pb.ProviderRegion{
			{Name: "us-east-1", DisplayName: "US East"},
			{Name: "us-west-2", DisplayName: "US West"},
		},
	}, nil
}

// ─── Test setup helper ───────────────────────────────────────────────────────

// startFakeServer starts an in-process gRPC server on a bufconn listener and
// returns a *grpc.ClientConn and a cleanup function. The caller registers
// services on srv before calling startFakeServer.
func startFakeServer(t *testing.T, srv *grpc.Server) *grpc.ClientConn {
	t.Helper()
	lis := bufconn.Listen(4 << 20)
	t.Cleanup(func() { _ = lis.Close() })
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

// ─── Tests ──────────────────────────────────────────────────────────────────

// TestAdapter_Name verifies Name() delegates to the IaCProviderRequired.Name RPC.
func TestAdapter_Name(t *testing.T) {
	srv := grpc.NewServer()
	pb.RegisterIaCProviderRequiredServer(srv, &fakeRequiredServer{})
	pb.RegisterIaCProviderRegionListerServer(srv, &fakeRegionListerServer{})
	conn := startFakeServer(t, srv)

	a := providerclient.New(conn)
	if got := a.Name(); got != "fake-provider" {
		t.Errorf("Name() = %q, want %q", got, "fake-provider")
	}
}

// TestAdapter_Plan verifies Plan() delegates and translates the response.
func TestAdapter_Plan(t *testing.T) {
	srv := grpc.NewServer()
	pb.RegisterIaCProviderRequiredServer(srv, &fakeRequiredServer{})
	pb.RegisterIaCProviderRegionListerServer(srv, &fakeRegionListerServer{})
	conn := startFakeServer(t, srv)

	a := providerclient.New(conn)
	plan, err := a.Plan(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("Plan() returned error: %v", err)
	}
	if plan == nil {
		t.Fatal("Plan() returned nil plan")
	}
	if plan.ID != "plan-1" {
		t.Errorf("plan.ID = %q, want %q", plan.ID, "plan-1")
	}
	if plan.DesiredHash != "abc123" {
		t.Errorf("plan.DesiredHash = %q, want %q", plan.DesiredHash, "abc123")
	}
}

// TestAdapter_Destroy verifies Destroy() delegates refs and returns destroy result.
func TestAdapter_Destroy(t *testing.T) {
	srv := grpc.NewServer()
	pb.RegisterIaCProviderRequiredServer(srv, &fakeRequiredServer{})
	pb.RegisterIaCProviderRegionListerServer(srv, &fakeRegionListerServer{})
	conn := startFakeServer(t, srv)

	a := providerclient.New(conn)
	refs := []interfaces.ResourceRef{{Name: "vpc-1", Type: "infra.vpc", ProviderID: "vpc-abc"}}
	result, err := a.Destroy(context.Background(), refs)
	if err != nil {
		t.Fatalf("Destroy() returned error: %v", err)
	}
	if result == nil {
		t.Fatal("Destroy() returned nil result")
	}
	if len(result.Destroyed) != 1 || result.Destroyed[0] != "vpc-1" {
		t.Errorf("Destroyed = %v, want [vpc-1]", result.Destroyed)
	}
}

// TestAdapter_Status verifies Status() delegates and translates ResourceStatus.
func TestAdapter_Status(t *testing.T) {
	srv := grpc.NewServer()
	pb.RegisterIaCProviderRequiredServer(srv, &fakeRequiredServer{})
	pb.RegisterIaCProviderRegionListerServer(srv, &fakeRegionListerServer{})
	conn := startFakeServer(t, srv)

	a := providerclient.New(conn)
	refs := []interfaces.ResourceRef{{Name: "vpc-1", Type: "infra.vpc", ProviderID: "vpc-abc"}}
	statuses, err := a.Status(context.Background(), refs)
	if err != nil {
		t.Fatalf("Status() returned error: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("len(statuses) = %d, want 1", len(statuses))
	}
	if statuses[0].Name != "vpc-1" || statuses[0].Status != "running" {
		t.Errorf("status = %+v, want Name=vpc-1 Status=running", statuses[0])
	}
}

// TestAdapter_ListRegions verifies ListRegions() delegates to IaCProviderRegionLister.
func TestAdapter_ListRegions(t *testing.T) {
	srv := grpc.NewServer()
	pb.RegisterIaCProviderRequiredServer(srv, &fakeRequiredServer{})
	pb.RegisterIaCProviderRegionListerServer(srv, &fakeRegionListerServer{})
	conn := startFakeServer(t, srv)

	a := providerclient.New(conn)
	regions, err := a.ListRegions(context.Background(), "prod")
	if err != nil {
		t.Fatalf("ListRegions() returned error: %v", err)
	}
	if len(regions) == 0 {
		t.Error("ListRegions() returned no regions")
	}
	// Verify region names round-trip correctly.
	found := false
	for _, r := range regions {
		if r == "us-east-1" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ListRegions() = %v, expected us-east-1 to be present", regions)
	}
}

// TestAdapter_TypeAssertIaCProvider verifies Adapter satisfies interfaces.IaCProvider.
// The compile-time assertion (var _ interfaces.IaCProvider = (*Adapter)(nil)) at the
// top of this file is the load-bearing check; this runtime test verifies New() returns
// a non-nil adapter that is usable as the interface.
func TestAdapter_TypeAssertIaCProvider(t *testing.T) {
	srv := grpc.NewServer()
	pb.RegisterIaCProviderRequiredServer(srv, &fakeRequiredServer{})
	pb.RegisterIaCProviderRegionListerServer(srv, &fakeRegionListerServer{})
	conn := startFakeServer(t, srv)

	a := providerclient.New(conn)
	if a == nil {
		t.Fatal("providerclient.New returned nil")
	}
	// Verify the name RPC works through the interfaces.IaCProvider method.
	var p interfaces.IaCProvider = a
	if got := p.Name(); got != "fake-provider" {
		t.Errorf("p.Name() via interface = %q, want fake-provider", got)
	}
}

// TestAdapter_TypeAssertIaCProviderRegionLister verifies Adapter satisfies
// interfaces.IaCProviderRegionLister so step.iac_provider_catalog can
// type-assert it.
func TestAdapter_TypeAssertIaCProviderRegionLister(t *testing.T) {
	srv := grpc.NewServer()
	pb.RegisterIaCProviderRequiredServer(srv, &fakeRequiredServer{})
	pb.RegisterIaCProviderRegionListerServer(srv, &fakeRegionListerServer{})
	conn := startFakeServer(t, srv)

	a := providerclient.New(conn)
	var p interfaces.IaCProvider = a
	rl, ok := p.(interfaces.IaCProviderRegionLister)
	if !ok {
		t.Fatal("Adapter must satisfy interfaces.IaCProviderRegionLister")
	}
	if rl == nil {
		t.Fatal("IaCProviderRegionLister type-assert returned nil")
	}
}
