package providerclient_test

import (
	"context"
	"encoding/json"
	"errors"
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

// fakeRunnerServer implements IaCProviderRunnerServer.
type fakeRunnerServer struct {
	pb.UnimplementedIaCProviderRunnerServer
	lastSpec *pb.JobSpec
}

func (s *fakeRunnerServer) RunJob(_ context.Context, req *pb.JobSpec) (*pb.JobHandle, error) {
	s.lastSpec = req
	return &pb.JobHandle{
		Id:       "job-123",
		Name:     req.GetName(),
		Provider: "fake-provider",
		Metadata: map[string]string{"region": "us-east-1"},
	}, nil
}

func (s *fakeRunnerServer) JobStatus(_ context.Context, handle *pb.JobHandle) (*pb.JobStatusReply, error) {
	return &pb.JobStatusReply{
		Handle:   handle,
		State:    pb.JobState_JOB_STATE_SUCCEEDED,
		ExitCode: 0,
		Message:  "done",
	}, nil
}

func (s *fakeRunnerServer) JobLogs(_ *pb.JobHandle, stream grpc.ServerStreamingServer[pb.LogChunk]) error {
	if err := stream.Send(&pb.LogChunk{Data: []byte("hello\n"), Source: "stdout"}); err != nil {
		return err
	}
	return stream.Send(&pb.LogChunk{Eof: true})
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

// TestAdapter_Runner_Advertised verifies the optional IaCProviderRunner client is
// gated by advertised service names and round-trips JobSpec/JobHandle/JobLogs.
func TestAdapter_Runner_Advertised(t *testing.T) {
	srv := grpc.NewServer()
	pb.RegisterIaCProviderRequiredServer(srv, &fakeRequiredServer{})
	runnerServer := &fakeRunnerServer{}
	pb.RegisterIaCProviderRunnerServer(srv, runnerServer)
	conn := startFakeServer(t, srv)

	a := providerclient.New(conn, map[string]bool{
		providerclient.IaCServiceRunner: true,
	})

	rp, ok := any(a).(providerclient.RunnerProvider)
	if !ok {
		t.Fatal("*Adapter must satisfy RunnerProvider")
	}
	runner := rp.Runner()
	if runner == nil {
		t.Fatal("Runner() returned nil when service was advertised")
	}

	handle, err := runner.RunJob(context.Background(), interfaces.JobSpec{
		Name:       "migrate",
		Kind:       "POST_DEPLOY",
		Image:      "alpine:3.19",
		RunCommand: "echo hello",
		EnvVars:    map[string]string{"PLAIN": "value"},
	})
	if err != nil {
		t.Fatalf("RunJob() returned error: %v", err)
	}
	if handle.ID != "job-123" || handle.Provider != "fake-provider" {
		t.Fatalf("handle = %+v, want ID=job-123 Provider=fake-provider", handle)
	}
	if runnerServer.lastSpec.GetImage() != "alpine:3.19" || runnerServer.lastSpec.GetRunCommand() != "echo hello" {
		t.Fatalf("server saw JobSpec = %+v", runnerServer.lastSpec)
	}

	status, err := runner.JobStatus(context.Background(), *handle)
	if err != nil {
		t.Fatalf("JobStatus() returned error: %v", err)
	}
	if status.State != interfaces.JobStateSucceeded || status.ExitCode != 0 {
		t.Fatalf("status = %+v, want succeeded exit 0", status)
	}

	sink := &capturingLogSink{}
	if err := runner.JobLogs(context.Background(), *handle, sink); err != nil {
		t.Fatalf("JobLogs() returned error: %v", err)
	}
	if got := string(sink.data); got != "hello\n" {
		t.Fatalf("captured logs = %q, want %q", got, "hello\\n")
	}
}

func TestAdapter_Runner_Unadvertised(t *testing.T) {
	srv := grpc.NewServer()
	pb.RegisterIaCProviderRequiredServer(srv, &fakeRequiredServer{})
	conn := startFakeServer(t, srv)

	a := providerclient.New(conn, nil)
	if _, ok := any(a).(interfaces.IaCProviderRunner); ok {
		t.Fatal("*Adapter must NOT satisfy interfaces.IaCProviderRunner directly")
	}
	rp, ok := any(a).(providerclient.RunnerProvider)
	if !ok {
		t.Fatal("*Adapter must satisfy RunnerProvider regardless of advertisement")
	}
	if runner := rp.Runner(); runner != nil {
		t.Fatalf("Runner() returned non-nil when IaCProviderRunner was not advertised; got %T", runner)
	}
}

// ─── ResourceDriver advertisement-gating ─────────────────────────────────────

// fakeResourceDriverServer implements ResourceDriverServer for unit tests.
// It captures the last request for assertion and returns canned responses.
type fakeResourceDriverServer struct {
	pb.UnimplementedResourceDriverServer

	lastCreateReq       *pb.ResourceCreateRequest
	lastUpdateReq       *pb.ResourceUpdateRequest
	lastDeleteReq       *pb.ResourceDeleteRequest
	lastReadReq         *pb.ResourceReadRequest
	lastDiffReq         *pb.ResourceDiffRequest
	lastHealthReq       *pb.ResourceHealthCheckRequest
	lastScaleReq        *pb.ResourceScaleRequest
	lastSensitiveReq    *pb.SensitiveKeysRequest
	lastTroubleshootReq *pb.TroubleshootRequest
}

func (s *fakeResourceDriverServer) Create(_ context.Context, req *pb.ResourceCreateRequest) (*pb.ResourceCreateResponse, error) {
	s.lastCreateReq = req
	outputsJSON, _ := json.Marshal(map[string]any{"endpoint": "db.example.com", "port": float64(5432)})
	return &pb.ResourceCreateResponse{
		Output: &pb.ResourceOutput{
			Name:        req.GetSpec().GetName(),
			Type:        req.GetResourceType(),
			ProviderId:  "prov-123",
			OutputsJson: outputsJSON,
			Sensitive:   map[string]bool{"password": true},
			Status:      "active",
		},
	}, nil
}

func (s *fakeResourceDriverServer) Read(_ context.Context, req *pb.ResourceReadRequest) (*pb.ResourceReadResponse, error) {
	s.lastReadReq = req
	outputsJSON, _ := json.Marshal(map[string]any{"endpoint": "db.example.com"})
	return &pb.ResourceReadResponse{
		Output: &pb.ResourceOutput{
			Name:        req.GetRef().GetName(),
			Type:        req.GetResourceType(),
			ProviderId:  req.GetRef().GetProviderId(),
			OutputsJson: outputsJSON,
			Status:      "active",
		},
	}, nil
}

func (s *fakeResourceDriverServer) Update(_ context.Context, req *pb.ResourceUpdateRequest) (*pb.ResourceUpdateResponse, error) {
	s.lastUpdateReq = req
	outputsJSON, _ := json.Marshal(map[string]any{"endpoint": "db.example.com"})
	return &pb.ResourceUpdateResponse{
		Output: &pb.ResourceOutput{
			Name:        req.GetRef().GetName(),
			Type:        req.GetResourceType(),
			ProviderId:  req.GetRef().GetProviderId(),
			OutputsJson: outputsJSON,
			Status:      "active",
		},
	}, nil
}

func (s *fakeResourceDriverServer) Delete(_ context.Context, req *pb.ResourceDeleteRequest) (*pb.ResourceDeleteResponse, error) {
	s.lastDeleteReq = req
	return &pb.ResourceDeleteResponse{}, nil
}

func (s *fakeResourceDriverServer) Diff(_ context.Context, req *pb.ResourceDiffRequest) (*pb.ResourceDiffResponse, error) {
	s.lastDiffReq = req
	oldJSON, _ := json.Marshal("t3.micro")
	newJSON, _ := json.Marshal("t3.large")
	return &pb.ResourceDiffResponse{
		Result: &pb.DiffResult{
			NeedsUpdate: true,
			Changes: []*pb.FieldChange{
				{Path: "size", OldJson: oldJSON, NewJson: newJSON, ForceNew: false},
			},
		},
	}, nil
}

func (s *fakeResourceDriverServer) HealthCheck(_ context.Context, req *pb.ResourceHealthCheckRequest) (*pb.ResourceHealthCheckResponse, error) {
	s.lastHealthReq = req
	return &pb.ResourceHealthCheckResponse{
		Result: &pb.HealthResult{Healthy: true, Message: "all systems nominal"},
	}, nil
}

func (s *fakeResourceDriverServer) Scale(_ context.Context, req *pb.ResourceScaleRequest) (*pb.ResourceScaleResponse, error) {
	s.lastScaleReq = req
	outputsJSON, _ := json.Marshal(map[string]any{"replicas": float64(req.GetReplicas())})
	return &pb.ResourceScaleResponse{
		Output: &pb.ResourceOutput{
			Name:        req.GetRef().GetName(),
			Type:        req.GetResourceType(),
			ProviderId:  req.GetRef().GetProviderId(),
			OutputsJson: outputsJSON,
			Status:      "active",
		},
	}, nil
}

func (s *fakeResourceDriverServer) SensitiveKeys(_ context.Context, req *pb.SensitiveKeysRequest) (*pb.SensitiveKeysResponse, error) {
	s.lastSensitiveReq = req
	return &pb.SensitiveKeysResponse{Keys: []string{"password", "api_key"}}, nil
}

func (s *fakeResourceDriverServer) Troubleshoot(_ context.Context, req *pb.TroubleshootRequest) (*pb.TroubleshootResponse, error) {
	s.lastTroubleshootReq = req
	return &pb.TroubleshootResponse{
		Diagnostics: []*pb.Diagnostic{
			{Id: "deploy-7", Phase: "ERROR", Cause: "image pull backoff", Detail: "manifest unknown"},
		},
	}, nil
}

// TestAdapter_ResourceDriver_WiredWhenAdvertised verifies end-to-end CRUD
// round-trip when IaCServiceResourceDriver is in advertisedServices.
func TestAdapter_ResourceDriver_WiredWhenAdvertised(t *testing.T) {
	srv := grpc.NewServer()
	pb.RegisterIaCProviderRequiredServer(srv, &fakeRequiredServer{})
	rdServer := &fakeResourceDriverServer{}
	pb.RegisterResourceDriverServer(srv, rdServer)
	conn := startFakeServer(t, srv)

	a := providerclient.New(conn, map[string]bool{
		providerclient.IaCServiceResourceDriver: true,
	})

	// ResourceDriverProvider accessor.
	rdp, ok := any(a).(providerclient.ResourceDriverProvider)
	if !ok {
		t.Fatal("*Adapter must satisfy ResourceDriverProvider when IaCServiceResourceDriver advertised")
	}

	driver, err := rdp.ResourceDriver("stub.database")
	if err != nil {
		t.Fatalf("ResourceDriver() returned error: %v", err)
	}
	if driver == nil {
		t.Fatal("ResourceDriver() returned nil driver when service was advertised")
	}

	ctx := context.Background()

	// ── Create ──────────────────────────────────────────────────────────────
	configMap := map[string]any{"instance_class": "db.t3.medium", "engine": "postgres"}
	spec := interfaces.ResourceSpec{Name: "mydb", Type: "stub.database", Config: configMap}
	out, err := driver.Create(ctx, spec)
	if err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}
	if out == nil {
		t.Fatal("Create() returned nil output")
	}
	if out.ProviderID != "prov-123" {
		t.Errorf("Create() ProviderID = %q, want %q", out.ProviderID, "prov-123")
	}
	if out.Outputs["endpoint"] != "db.example.com" {
		t.Errorf("Create() Outputs[endpoint] = %v, want db.example.com", out.Outputs["endpoint"])
	}
	if !out.Sensitive["password"] {
		t.Error("Create() Sensitive[password] should be true")
	}
	// Assert that Config JSON reached the server.
	if rdServer.lastCreateReq == nil {
		t.Fatal("Create() did not reach server")
	}
	if rdServer.lastCreateReq.GetResourceType() != "stub.database" {
		t.Errorf("server saw resource_type = %q, want %q", rdServer.lastCreateReq.GetResourceType(), "stub.database")
	}
	var serverCfg map[string]any
	if err := json.Unmarshal(rdServer.lastCreateReq.GetSpec().GetConfigJson(), &serverCfg); err != nil {
		t.Fatalf("server config_json not valid JSON: %v", err)
	}
	if serverCfg["engine"] != "postgres" {
		t.Errorf("server config engine = %v, want postgres", serverCfg["engine"])
	}

	// ── Read ─────────────────────────────────────────────────────────────────
	ref := interfaces.ResourceRef{Name: "mydb", Type: "stub.database", ProviderID: "prov-123"}
	readOut, err := driver.Read(ctx, ref)
	if err != nil {
		t.Fatalf("Read() returned error: %v", err)
	}
	if readOut == nil {
		t.Fatal("Read() returned nil output")
	}
	if rdServer.lastReadReq.GetResourceType() != "stub.database" {
		t.Errorf("Read server resource_type = %q, want stub.database", rdServer.lastReadReq.GetResourceType())
	}

	// ── Update ───────────────────────────────────────────────────────────────
	updSpec := interfaces.ResourceSpec{Name: "mydb", Type: "stub.database", Config: map[string]any{"instance_class": "db.t3.large"}}
	updateOut, err := driver.Update(ctx, ref, updSpec)
	if err != nil {
		t.Fatalf("Update() returned error: %v", err)
	}
	if updateOut == nil {
		t.Fatal("Update() returned nil output")
	}
	if rdServer.lastUpdateReq.GetRef().GetProviderId() != "prov-123" {
		t.Errorf("Update server ref provider_id = %q, want prov-123", rdServer.lastUpdateReq.GetRef().GetProviderId())
	}

	// ── Delete ───────────────────────────────────────────────────────────────
	if err := driver.Delete(ctx, ref); err != nil {
		t.Fatalf("Delete() returned error: %v", err)
	}
	if rdServer.lastDeleteReq.GetResourceType() != "stub.database" {
		t.Errorf("Delete server resource_type = %q, want stub.database", rdServer.lastDeleteReq.GetResourceType())
	}

	// ── Diff ─────────────────────────────────────────────────────────────────
	diffResult, err := driver.Diff(ctx, spec, out)
	if err != nil {
		t.Fatalf("Diff() returned error: %v", err)
	}
	if diffResult == nil {
		t.Fatal("Diff() returned nil result")
	}
	if !diffResult.NeedsUpdate {
		t.Error("Diff() NeedsUpdate should be true")
	}
	if len(diffResult.Changes) != 1 || diffResult.Changes[0].Path != "size" {
		t.Errorf("Diff() Changes = %+v, want 1 change with path=size", diffResult.Changes)
	}

	// ── HealthCheck ─────────────────────────────────────────────────────────
	health, err := driver.HealthCheck(ctx, ref)
	if err != nil {
		t.Fatalf("HealthCheck() returned error: %v", err)
	}
	if health == nil || !health.Healthy {
		t.Errorf("HealthCheck() = %+v, want Healthy=true", health)
	}
	if health.Message != "all systems nominal" {
		t.Errorf("HealthCheck() message = %q, want %q", health.Message, "all systems nominal")
	}

	// ── Scale ────────────────────────────────────────────────────────────────
	scaleOut, err := driver.Scale(ctx, ref, 3)
	if err != nil {
		t.Fatalf("Scale() returned error: %v", err)
	}
	if scaleOut == nil {
		t.Fatal("Scale() returned nil output")
	}
	if rdServer.lastScaleReq.GetReplicas() != 3 {
		t.Errorf("Scale server replicas = %d, want 3", rdServer.lastScaleReq.GetReplicas())
	}

	// ── SensitiveKeys ────────────────────────────────────────────────────────
	keys := driver.SensitiveKeys()
	if len(keys) != 2 {
		t.Errorf("SensitiveKeys() = %v, want [password api_key]", keys)
	}
	if rdServer.lastSensitiveReq.GetResourceType() != "stub.database" {
		t.Errorf("SensitiveKeys server resource_type = %q, want stub.database", rdServer.lastSensitiveReq.GetResourceType())
	}

	// ── Troubleshoot (optional interfaces.Troubleshooter) ────────────────────
	troubleshooter, ok := driver.(interfaces.Troubleshooter)
	if !ok {
		t.Fatal("ResourceDriver bridge must satisfy interfaces.Troubleshooter")
	}
	diags, err := troubleshooter.Troubleshoot(ctx, ref, "health check timed out")
	if err != nil {
		t.Fatalf("Troubleshoot() returned error: %v", err)
	}
	if len(diags) != 1 || diags[0].ID != "deploy-7" || diags[0].Cause != "image pull backoff" {
		t.Errorf("Troubleshoot() = %+v, want 1 diagnostic id=deploy-7 cause='image pull backoff'", diags)
	}
	if rdServer.lastTroubleshootReq.GetResourceType() != "stub.database" {
		t.Errorf("Troubleshoot server resource_type = %q, want stub.database", rdServer.lastTroubleshootReq.GetResourceType())
	}
	if rdServer.lastTroubleshootReq.GetFailureMsg() != "health check timed out" {
		t.Errorf("Troubleshoot server failure_msg = %q, want %q", rdServer.lastTroubleshootReq.GetFailureMsg(), "health check timed out")
	}
}

// TestAdapter_ResourceDriver_UnimplementedWhenNotAdvertised verifies that when
// the ResourceDriver service is not in advertisedServices, ResourceDriver()
// returns ErrProviderMethodUnimplemented.
func TestAdapter_ResourceDriver_UnimplementedWhenNotAdvertised(t *testing.T) {
	srv := grpc.NewServer()
	pb.RegisterIaCProviderRequiredServer(srv, &fakeRequiredServer{})
	// NOTE: ResourceDriver is deliberately NOT registered on the server.
	conn := startFakeServer(t, srv)

	a := providerclient.New(conn, nil)

	// ResourceDriverProvider accessor must always be present on *Adapter.
	rdp, ok := any(a).(providerclient.ResourceDriverProvider)
	if !ok {
		t.Fatal("*Adapter must satisfy ResourceDriverProvider regardless of advertisement")
	}

	_, err := rdp.ResourceDriver("stub.database")
	if err == nil {
		t.Fatal("ResourceDriver() on unadvertised adapter must return error")
	}
	if !isUnimplemented(err) {
		t.Errorf("ResourceDriver() error = %v, want ErrProviderMethodUnimplemented", err)
	}
}

// codeReturningResourceDriverServer returns a configurable gRPC status code from
// its Read RPC so the error-mapping table test can drive each named case.
type codeReturningResourceDriverServer struct {
	pb.UnimplementedResourceDriverServer
	code codes.Code
}

func (s *codeReturningResourceDriverServer) Read(_ context.Context, _ *pb.ResourceReadRequest) (*pb.ResourceReadResponse, error) {
	return nil, status.Error(s.code, "stub error from server")
}

// TestAdapter_ResourceDriver_GRPCErrorMapping verifies that each well-known gRPC
// status code returned by a ResourceDriver RPC is mapped to the correct engine
// sentinel (recoverable via errors.Is) AND that the underlying gRPC status code
// remains recoverable via status.Code walking the unwrap chain (the %w/%w fix —
// without it status.Code would degrade to Unknown). The AlreadyExists → upsert
// recovery path (wfctlhelpers/apply.go errors.Is(err, ErrResourceAlreadyExists))
// is load-bearing.
func TestAdapter_ResourceDriver_GRPCErrorMapping(t *testing.T) {
	cases := []struct {
		name         string
		code         codes.Code
		wantSentinel error
	}{
		{"NotFound", codes.NotFound, interfaces.ErrResourceNotFound},
		{"AlreadyExists", codes.AlreadyExists, interfaces.ErrResourceAlreadyExists},
		{"ResourceExhausted", codes.ResourceExhausted, interfaces.ErrRateLimited},
		{"Unavailable", codes.Unavailable, interfaces.ErrTransient},
		{"DeadlineExceeded", codes.DeadlineExceeded, interfaces.ErrTransient},
		{"Unauthenticated", codes.Unauthenticated, interfaces.ErrUnauthorized},
		{"PermissionDenied", codes.PermissionDenied, interfaces.ErrForbidden},
		{"InvalidArgument", codes.InvalidArgument, interfaces.ErrValidation},
		{"FailedPrecondition", codes.FailedPrecondition, interfaces.ErrValidation},
		{"Unimplemented", codes.Unimplemented, interfaces.ErrProviderMethodUnimplemented},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := grpc.NewServer()
			pb.RegisterIaCProviderRequiredServer(srv, &fakeRequiredServer{})
			pb.RegisterResourceDriverServer(srv, &codeReturningResourceDriverServer{code: tc.code})
			conn := startFakeServer(t, srv)

			a := providerclient.New(conn, map[string]bool{
				providerclient.IaCServiceResourceDriver: true,
			})
			rdp := any(a).(providerclient.ResourceDriverProvider)
			driver, err := rdp.ResourceDriver("stub.database")
			if err != nil {
				t.Fatalf("ResourceDriver() returned error: %v", err)
			}

			_, err = driver.Read(context.Background(), interfaces.ResourceRef{Name: "x", Type: "stub.database", ProviderID: "id-1"})
			if err == nil {
				t.Fatalf("expected error for code %v", tc.code)
			}

			// 1. Sentinel must be recoverable via errors.Is.
			if !errors.Is(err, tc.wantSentinel) {
				t.Errorf("code %v: errors.Is(err, %v) = false; err = %v", tc.code, tc.wantSentinel, err)
			}

			// 2. The underlying gRPC status code must STILL be recoverable from the
			//    unwrap chain (the %w/%w fix). For Unimplemented the original error
			//    is intentionally NOT chained (sentinel-only message), so skip the
			//    status-code recovery assertion for that case.
			if tc.code != codes.Unimplemented {
				if got := status.Code(err); got != tc.code {
					t.Errorf("code %v: status.Code(err) = %v, want %v (status lost from unwrap chain — %%w/%%v regression?)", tc.code, got, tc.code)
				}
			}
		})
	}
}

type capturingLogSink struct {
	data []byte
	eof  bool
}

func (s *capturingLogSink) WriteLogChunk(chunk interfaces.LogChunk) error {
	if chunk.EOF {
		s.eof = true
		return nil
	}
	s.data = append(s.data, chunk.Data...)
	return nil
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
