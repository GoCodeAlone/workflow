package main

// iac_typed_fixture_test.go — shared bufconn-backed *typedIaCAdapter fixture
// for wfctl tests (Task 17 / PR 618 of the strict-contracts force-cutover
// plan, docs/plans/2026-05-10-strict-contracts-force-cutover.md).
//
// Per ADR-0028, the 5 wfctl IaC dispatch sites are pure typed-pb: they
// type-assert `provider.(*typedIaCAdapter)` and use the typed-client
// accessors for capability discovery. Test fixtures that previously
// injected a fake `interfaces.IaCProvider` no longer reach the dispatch
// path (the type-assert fails). The migration replaces those fakes with
// a real `*typedIaCAdapter` wired to an in-process bufconn-served
// pb.IaCProvider* gRPC server.
//
// Pattern precedents:
//   - plugin/external/sdk/iac_e2e_test.go (PR #603) — bufconn end-to-end
//     of the typed RPC contract
//   - cmd/wfctl/discover_typed_loader_test.go (PR #609) — boundary test
//     for the typed loader returning *typedIaCAdapter
//   - cmd/wfctl/iac_typed_adapter_test.go (PR #605) — adapter unit +
//     in-process gRPC integration tests
//
// Fixture shape (declarative, struct-based):
//
//	adapter := fixtureTypedAdapter{
//	    Required:   &fixtureRequiredServer{name: "do", version: "0.0.0"},
//	    Enumerator: &recordingEnumeratorServer{...},
//	}.build(t)
//
// Each non-nil optional-service field results in the matching pb service
// being registered on the in-process gRPC server, which makes the typed
// adapter's accessor for that service return a real client. nil means
// the optional service is NOT registered — the accessor returns nil and
// the dispatch site sees the same shape as a plugin that didn't advertise
// it.
//
// Common mock server types (recordingEnumeratorServer,
// recordingResourceDriverServer, etc.) live alongside fixtureTypedAdapter
// in this file so the migrated fixtures share a single source of truth
// for the mock shapes.

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/GoCodeAlone/workflow/interfaces"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

// fixtureBufSize matches PR #603's e2eBufSize so bufconn behaviour is
// identical between the SDK end-to-end test and the wfctl fixtures.
const fixtureBufSize = 1024 * 1024

// fixtureRequiredServer is the baseline pb.IaCProviderRequiredServer for
// test fixtures. Embeds UnimplementedIaCProviderRequiredServer so additions
// to the proto don't retroactively break tests; only Name + Version are
// overridden so the adapter's Name()/Version() Go methods return what
// each fixture expects.
type fixtureRequiredServer struct {
	pb.UnimplementedIaCProviderRequiredServer
	name    string
	version string
}

func (s *fixtureRequiredServer) Name(_ context.Context, _ *pb.NameRequest) (*pb.NameResponse, error) {
	return &pb.NameResponse{Name: s.name}, nil
}

func (s *fixtureRequiredServer) Version(_ context.Context, _ *pb.VersionRequest) (*pb.VersionResponse, error) {
	return &pb.VersionResponse{Version: s.version}, nil
}

// fixtureTypedAdapter declaratively configures a bufconn-backed
// *typedIaCAdapter. Each non-nil field results in the corresponding
// pb service being registered on the in-process gRPC server, mirroring
// the ContractRegistry-driven optional-client construction in production.
// nil → service not registered → accessor returns nil → dispatch site
// sees the "absence of registration IS the negative signal" contract
// from the typed-IaC design.
type fixtureTypedAdapter struct {
	// Required handler. If nil, defaults to fixtureRequiredServer{} which
	// returns empty Name/Version and Unimplemented for everything else.
	Required pb.IaCProviderRequiredServer

	// Optional services. nil → service not registered → accessor returns nil.
	Enumerator        pb.IaCProviderEnumeratorServer
	DriftDetector     pb.IaCProviderDriftDetectorServer
	CredentialRevoker pb.IaCProviderCredentialRevokerServer
	RegionLister      pb.IaCProviderRegionListerServer
	MigrationRepairer pb.IaCProviderMigrationRepairerServer
	Validator         pb.IaCProviderValidatorServer
	DriftConfigDetect pb.IaCProviderDriftConfigDetectorServer
	ResourceDriver    pb.ResourceDriverServer
	LogCapture        pb.IaCProviderLogCaptureServer
}

// build spins up a bufconn-backed gRPC server running f's set of services,
// dials it, and returns a *typedIaCAdapter satisfying interfaces.IaCProvider.
// All cleanup (server.Stop, listener.Close, conn.Close) is registered via
// t.Cleanup so the fixture composes naturally with parallel/subtests and
// frees resources at test end without a manual defer chain at every
// call site.
func (f fixtureTypedAdapter) build(t *testing.T) *typedIaCAdapter {
	t.Helper()
	if f.Required == nil {
		f.Required = &fixtureRequiredServer{}
	}

	listener := bufconn.Listen(fixtureBufSize)
	t.Cleanup(func() { _ = listener.Close() })
	server := grpc.NewServer()

	pb.RegisterIaCProviderRequiredServer(server, f.Required)
	registered := map[string]bool{iacServiceRequired: true}

	if f.Enumerator != nil {
		pb.RegisterIaCProviderEnumeratorServer(server, f.Enumerator)
		registered[iacServiceEnumerator] = true
	}
	if f.DriftDetector != nil {
		pb.RegisterIaCProviderDriftDetectorServer(server, f.DriftDetector)
		registered[iacServiceDriftDetector] = true
	}
	if f.CredentialRevoker != nil {
		pb.RegisterIaCProviderCredentialRevokerServer(server, f.CredentialRevoker)
		registered[iacServiceCredentialRevoker] = true
	}
	if f.RegionLister != nil {
		pb.RegisterIaCProviderRegionListerServer(server, f.RegionLister)
		registered[iacServiceRegionLister] = true
	}
	if f.MigrationRepairer != nil {
		pb.RegisterIaCProviderMigrationRepairerServer(server, f.MigrationRepairer)
		registered[iacServiceMigrationRepairer] = true
	}
	if f.Validator != nil {
		pb.RegisterIaCProviderValidatorServer(server, f.Validator)
		registered[iacServiceValidator] = true
	}
	if f.DriftConfigDetect != nil {
		pb.RegisterIaCProviderDriftConfigDetectorServer(server, f.DriftConfigDetect)
		registered[iacServiceDriftConfigDetect] = true
	}
	if f.ResourceDriver != nil {
		pb.RegisterResourceDriverServer(server, f.ResourceDriver)
		registered[iacServiceResourceDriver] = true
	}
	if f.LogCapture != nil {
		pb.RegisterIaCProviderLogCaptureServer(server, f.LogCapture)
		registered[iacServiceLogCapture] = true
	}

	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("fixtureTypedAdapter: grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	return newTypedIaCAdapter(conn, registered)
}

// ── Common mock server types ────────────────────────────────────────────────
//
// These types provide reusable pb.IaCProvider*Server mocks for the migrated
// wfctl fixtures. Each one records inputs (so tests can assert call counts
// + arguments) and emits canned responses (so tests can drive happy / error
// paths). All are mutex-guarded so a future test that issues parallel RPCs
// through the same fixture doesn't race the recorded state.

// recordingEnumeratorServer is the bufconn analogue of the legacy
// fakeEnumeratingProvider — returns canned EnumerateByTag /  EnumerateAll
// responses (or canned errors) per call. Tests assert against the recorded
// inputs / counts after the run completes.
type recordingEnumeratorServer struct {
	pb.UnimplementedIaCProviderEnumeratorServer

	mu sync.Mutex

	// Canned responses (write once before run, read after).
	tagRefs         []interfaces.ResourceRef
	allOutputs      []*pb.ResourceOutput
	enumerateTagErr error
	enumerateAllErr error

	// Recorded inputs (read after run, optional assertion).
	enumerateAllType string
}

func (s *recordingEnumeratorServer) EnumerateByTag(_ context.Context, _ *pb.EnumerateByTagRequest) (*pb.EnumerateByTagResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.enumerateTagErr != nil {
		return nil, s.enumerateTagErr
	}
	return &pb.EnumerateByTagResponse{Refs: refsToPB(s.tagRefs)}, nil
}

func (s *recordingEnumeratorServer) EnumerateAll(_ context.Context, req *pb.EnumerateAllRequest) (*pb.EnumerateAllResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.enumerateAllType = req.GetResourceType()
	if s.enumerateAllErr != nil {
		return nil, s.enumerateAllErr
	}
	return &pb.EnumerateAllResponse{Outputs: append([]*pb.ResourceOutput(nil), s.allOutputs...)}, nil
}

// recordingResourceDriverServer is a minimal pb.ResourceDriverServer that
// records Delete invocations and lets tests inject per-call errors. Used
// by infra cleanup fixtures that previously implemented a fake
// interfaces.ResourceDriver inline. Other RPCs left to the embedded
// Unimplemented* defaults — fixtures that need Create/Read/etc. should
// embed this server and override the relevant method.
type recordingResourceDriverServer struct {
	pb.UnimplementedResourceDriverServer

	mu sync.Mutex

	// Canned per-call errors. Indexed by zero-based call number.
	deleteErrors map[int]error

	// Recorded state.
	deleteCount int
	deletedRefs []interfaces.ResourceRef
}

func (s *recordingResourceDriverServer) Delete(_ context.Context, req *pb.ResourceDeleteRequest) (*pb.ResourceDeleteResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.deleteCount
	s.deleteCount++
	s.deletedRefs = append(s.deletedRefs, refFromPB(req.GetRef()))
	if s.deleteErrors != nil {
		if err, ok := s.deleteErrors[idx]; ok {
			return nil, err
		}
	}
	return &pb.ResourceDeleteResponse{}, nil
}

// callCount returns the number of Delete RPCs received. Mutex-guarded so a
// concurrent dispatch loop reading the count after a t.Run subtest doesn't
// race the gRPC handler's mutation.
func (s *recordingResourceDriverServer) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deleteCount
}

// recordingDriftDetectorServer returns canned DetectDrift responses. Used by
// infra apply-refresh fixtures that previously injected a fake
// interfaces.IaCProvider whose DetectDrift returned canned []DriftResult.
//
// pbDrifts stores the pre-marshalled proto-wire shape so the gRPC handler
// emits the canned response without any encode-time failure mode at RPC
// time. Construction-time marshalling (driftsToPB → t.Fatalf at fixture
// build) means a fixture author that supplies un-marshallable
// Expected/Actual maps sees the failure deterministically at test setup
// rather than via a silently-empty ExpectedJson on the wire.
type recordingDriftDetectorServer struct {
	pb.UnimplementedIaCProviderDriftDetectorServer

	mu sync.Mutex

	pbDrifts []*pb.DriftResult
	driftErr error
}

func (s *recordingDriftDetectorServer) DetectDrift(_ context.Context, _ *pb.DetectDriftRequest) (*pb.DetectDriftResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.driftErr != nil {
		return nil, s.driftErr
	}
	return &pb.DetectDriftResponse{Drifts: append([]*pb.DriftResult(nil), s.pbDrifts...)}, nil
}

// driftsToPB converts a slice of engine-side DriftResults to the proto wire
// shape, mirroring the inverse driftsFromPB helper in iac_typed_adapter.go.
// Returns an error if any DriftResult's Expected or Actual map fails to
// marshal to JSON so callers (fixture builders) can fail-fast at test setup
// rather than emitting a silently-truncated response on the wire.
func driftsToPB(drifts []interfaces.DriftResult) ([]*pb.DriftResult, error) {
	if len(drifts) == 0 {
		return nil, nil
	}
	out := make([]*pb.DriftResult, 0, len(drifts))
	for i, d := range drifts {
		expectedJSON, err := marshalJSONMap(d.Expected)
		if err != nil {
			return nil, fmt.Errorf("drifts[%d].Expected (resource %q): %w", i, d.Name, err)
		}
		actualJSON, err := marshalJSONMap(d.Actual)
		if err != nil {
			return nil, fmt.Errorf("drifts[%d].Actual (resource %q): %w", i, d.Name, err)
		}
		out = append(out, &pb.DriftResult{
			Name:         d.Name,
			Type:         d.Type,
			Drifted:      d.Drifted,
			Class:        driftClassToPB(d.Class),
			ExpectedJson: expectedJSON,
			ActualJson:   actualJSON,
			Fields:       append([]string(nil), d.Fields...),
		})
	}
	return out, nil
}
