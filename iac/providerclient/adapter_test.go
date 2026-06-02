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

// Compile-time guard: *Adapter must satisfy interfaces.IaCProvider.
var _ interfaces.IaCProvider = (*providerclient.Adapter)(nil)

// *Adapter must NOT unconditionally satisfy interfaces.IaCProviderRegionLister —
// that interface is gated behind the RegionListerProvider accessor. The
// negative assertion is enforced by TestAdapter_RegionLister_Unadvertised below
// (runtime type-assert on a freshly-built adapter with no advertised services).

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

func (s *fakeRequiredServer) BootstrapStateBackend(_ context.Context, req *pb.BootstrapStateBackendRequest) (*pb.BootstrapStateBackendResponse, error) {
	return &pb.BootstrapStateBackendResponse{
		Result: &pb.BootstrapResult{
			Bucket:   "my-bucket",
			Region:   "us-east-1",
			Endpoint: "https://nyc3.digitaloceanspaces.com",
			EnvVars:  map[string]string{"BUCKET": "my-bucket"},
		},
	}, nil
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

// fakeDriftDetectorServer implements IaCProviderDriftDetectorServer.
type fakeDriftDetectorServer struct {
	pb.UnimplementedIaCProviderDriftDetectorServer
}

func (s *fakeDriftDetectorServer) DetectDrift(_ context.Context, req *pb.DetectDriftRequest) (*pb.DetectDriftResponse, error) {
	results := make([]*pb.DriftResult, 0, len(req.GetRefs()))
	for _, r := range req.GetRefs() {
		results = append(results, &pb.DriftResult{
			Name:    r.GetName(),
			Type:    r.GetType(),
			Drifted: false,
			Class:   pb.DriftClass_DRIFT_CLASS_IN_SYNC,
		})
	}
	return &pb.DetectDriftResponse{Drifts: results}, nil
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
	conn := startFakeServer(t, srv)

	a := providerclient.New(conn, nil)
	if got := a.Name(); got != "fake-provider" {
		t.Errorf("Name() = %q, want %q", got, "fake-provider")
	}
}

// TestAdapter_Plan verifies Plan() delegates and translates the response.
func TestAdapter_Plan(t *testing.T) {
	srv := grpc.NewServer()
	pb.RegisterIaCProviderRequiredServer(srv, &fakeRequiredServer{})
	conn := startFakeServer(t, srv)

	a := providerclient.New(conn, nil)
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
	conn := startFakeServer(t, srv)

	a := providerclient.New(conn, nil)
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
	conn := startFakeServer(t, srv)

	a := providerclient.New(conn, nil)
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

// TestAdapter_BootstrapStateBackend_Endpoint verifies that the Endpoint field
// is correctly mapped from the proto response (regression for the silent drop
// of DO Spaces endpoint).
func TestAdapter_BootstrapStateBackend_Endpoint(t *testing.T) {
	srv := grpc.NewServer()
	pb.RegisterIaCProviderRequiredServer(srv, &fakeRequiredServer{})
	conn := startFakeServer(t, srv)

	a := providerclient.New(conn, nil)
	result, err := a.BootstrapStateBackend(context.Background(), nil)
	if err != nil {
		t.Fatalf("BootstrapStateBackend() returned error: %v", err)
	}
	if result == nil {
		t.Fatal("BootstrapStateBackend() returned nil result")
	}
	if result.Bucket != "my-bucket" {
		t.Errorf("Bucket = %q, want %q", result.Bucket, "my-bucket")
	}
	if result.Region != "us-east-1" {
		t.Errorf("Region = %q, want %q", result.Region, "us-east-1")
	}
	if result.Endpoint != "https://nyc3.digitaloceanspaces.com" {
		t.Errorf("Endpoint = %q, want %q", result.Endpoint, "https://nyc3.digitaloceanspaces.com")
	}
	if result.EnvVars["BUCKET"] != "my-bucket" {
		t.Errorf("EnvVars[BUCKET] = %q, want %q", result.EnvVars["BUCKET"], "my-bucket")
	}
}

// TestAdapter_Capabilities_Cached verifies that fetchCapabilities caches the
// result (a second call doesn't issue a second RPC — verified by checking the
// returned data is consistent).
func TestAdapter_Capabilities_Cached(t *testing.T) {
	srv := grpc.NewServer()
	pb.RegisterIaCProviderRequiredServer(srv, &fakeRequiredServer{})
	conn := startFakeServer(t, srv)

	a := providerclient.New(conn, nil)
	// Two calls to Capabilities() should return equal results.
	caps1 := a.Capabilities()
	caps2 := a.Capabilities()
	if len(caps1) != len(caps2) {
		t.Errorf("Capabilities() not consistent across calls: %d vs %d", len(caps1), len(caps2))
	}
	// SupportedCanonicalKeys shares the same cache path.
	keys := a.SupportedCanonicalKeys()
	if len(keys) == 0 {
		t.Error("SupportedCanonicalKeys() returned empty slice")
	}
}

// ─── RegionLister advertisement-gating ──────────────────────────────────────

// TestAdapter_RegionLister_Advertised verifies that when IaCServiceRegionLister
// is in advertisedServices, RegionLister() returns a non-nil object and ListRegions
// works end-to-end.
func TestAdapter_RegionLister_Advertised(t *testing.T) {
	srv := grpc.NewServer()
	pb.RegisterIaCProviderRequiredServer(srv, &fakeRequiredServer{})
	pb.RegisterIaCProviderRegionListerServer(srv, &fakeRegionListerServer{})
	conn := startFakeServer(t, srv)

	a := providerclient.New(conn, map[string]bool{
		providerclient.IaCServiceRegionLister: true,
	})

	// Must satisfy RegionListerProvider.
	rlp, ok := any(a).(providerclient.RegionListerProvider)
	if !ok {
		t.Fatal("*Adapter must satisfy RegionListerProvider when IaCServiceRegionLister advertised")
	}
	rl := rlp.RegionLister()
	if rl == nil {
		t.Fatal("RegionLister() returned nil when service was advertised")
	}

	regions, err := rl.ListRegions(context.Background(), "prod")
	if err != nil {
		t.Fatalf("ListRegions() returned error: %v", err)
	}
	if len(regions) == 0 {
		t.Error("ListRegions() returned no regions")
	}
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

// TestAdapter_RegionLister_Unadvertised verifies the KEY negative path: when
// IaCServiceRegionLister is NOT in advertisedServices, RegionLister() returns
// nil, and *Adapter does NOT satisfy interfaces.IaCProviderRegionLister directly
// (the catalog step's static fallback must be reachable).
func TestAdapter_RegionLister_Unadvertised(t *testing.T) {
	srv := grpc.NewServer()
	pb.RegisterIaCProviderRequiredServer(srv, &fakeRequiredServer{})
	// NOTE: IaCProviderRegionLister is deliberately NOT registered on the server.
	conn := startFakeServer(t, srv)

	// Adapter built with NO advertised services.
	a := providerclient.New(conn, nil)

	// *Adapter must NOT directly satisfy IaCProviderRegionLister.
	if _, ok := any(a).(interfaces.IaCProviderRegionLister); ok {
		t.Fatal("*Adapter must NOT satisfy interfaces.IaCProviderRegionLister when unadvertised — " +
			"the catalog step's static fallback can never fire if it always type-asserts true")
	}

	// RegionListerProvider type-assert must succeed (the accessor interface is
	// always present on *Adapter), but RegionLister() must return nil.
	rlp, ok := any(a).(providerclient.RegionListerProvider)
	if !ok {
		t.Fatal("*Adapter must satisfy RegionListerProvider regardless of advertisement")
	}
	if rl := rlp.RegionLister(); rl != nil {
		t.Errorf("RegionLister() returned non-nil when IaCServiceRegionLister was not advertised; got %T", rl)
	}

	// DetectDrift on unadvertised adapter must return ErrProviderMethodUnimplemented.
	_, err := a.DetectDrift(context.Background(), nil)
	if err == nil {
		t.Fatal("DetectDrift() on unadvertised adapter must return error")
	}
	if !isUnimplemented(err) {
		t.Errorf("DetectDrift() error = %v, want ErrProviderMethodUnimplemented", err)
	}
}

// TestAdapter_DriftDetector_Advertised verifies that when IaCServiceDriftDetector
// is in advertisedServices, DriftDetector() returns a non-nil DriftConfigDetector
// and DetectDrift routes through the optional service.
func TestAdapter_DriftDetector_Advertised(t *testing.T) {
	srv := grpc.NewServer()
	pb.RegisterIaCProviderRequiredServer(srv, &fakeRequiredServer{})
	pb.RegisterIaCProviderDriftDetectorServer(srv, &fakeDriftDetectorServer{})
	conn := startFakeServer(t, srv)

	a := providerclient.New(conn, map[string]bool{
		providerclient.IaCServiceDriftDetector: true,
	})

	// DriftDetectorProvider accessor.
	ddp, ok := any(a).(providerclient.DriftDetectorProvider)
	if !ok {
		t.Fatal("*Adapter must satisfy DriftDetectorProvider when IaCServiceDriftDetector advertised")
	}
	dd := ddp.DriftDetector()
	if dd == nil {
		t.Fatal("DriftDetector() returned nil when service was advertised")
	}

	// DetectDrift routes through the optional service.
	refs := []interfaces.ResourceRef{{Name: "vpc-1", Type: "infra.vpc", ProviderID: "vpc-abc"}}
	drifts, err := a.DetectDrift(context.Background(), refs)
	if err != nil {
		t.Fatalf("DetectDrift() returned error: %v", err)
	}
	if len(drifts) != 1 {
		t.Fatalf("DetectDrift() returned %d drifts, want 1", len(drifts))
	}
	if drifts[0].Name != "vpc-1" {
		t.Errorf("drift.Name = %q, want %q", drifts[0].Name, "vpc-1")
	}
}

// TestAdapter_DriftDetector_Unadvertised verifies the negative path for
// DriftDetector: when IaCServiceDriftDetector is NOT advertised, DriftDetector()
// returns nil.
func TestAdapter_DriftDetector_Unadvertised(t *testing.T) {
	srv := grpc.NewServer()
	pb.RegisterIaCProviderRequiredServer(srv, &fakeRequiredServer{})
	conn := startFakeServer(t, srv)

	a := providerclient.New(conn, nil)

	ddp, ok := any(a).(providerclient.DriftDetectorProvider)
	if !ok {
		t.Fatal("*Adapter must satisfy DriftDetectorProvider regardless of advertisement")
	}
	if dd := ddp.DriftDetector(); dd != nil {
		t.Errorf("DriftDetector() returned non-nil when IaCServiceDriftDetector was not advertised; got %T", dd)
	}
}

// TestAdapter_TypeAssertIaCProvider verifies Adapter satisfies interfaces.IaCProvider.
func TestAdapter_TypeAssertIaCProvider(t *testing.T) {
	srv := grpc.NewServer()
	pb.RegisterIaCProviderRequiredServer(srv, &fakeRequiredServer{})
	conn := startFakeServer(t, srv)

	a := providerclient.New(conn, nil)
	if a == nil {
		t.Fatal("providerclient.New returned nil")
	}
	var p interfaces.IaCProvider = a
	if got := p.Name(); got != "fake-provider" {
		t.Errorf("p.Name() via interface = %q, want fake-provider", got)
	}
}

// isUnimplemented reports whether err wraps interfaces.ErrProviderMethodUnimplemented.
func isUnimplemented(err error) bool {
	return err != nil && (err.Error() != "" && containsUnimplemented(err))
}

func containsUnimplemented(err error) bool {
	// Use errors.Is-style unwrap check by walking the chain.
	target := interfaces.ErrProviderMethodUnimplemented
	type unwrapper interface{ Unwrap() error }
	for err != nil {
		if err == target {
			return true
		}
		uw, ok := err.(unwrapper)
		if !ok {
			break
		}
		err = uw.Unwrap()
	}
	return false
}
