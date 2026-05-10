package main

// discover_typed_loader_test.go — boundary test for the typed-IaC
// loader cutover (PR #609 / Task 16). Per spec Step 1: assert
// discoverAndLoadIaCProvider's post-LoadPlugin path returns the typed
// adapter (*typedIaCAdapter) rather than the legacy
// *remoteIaCProvider (which no longer exists post-cutover anyway).
//
// Tests the unit-testable seam buildTypedIaCAdapterFrom(adapter), not
// discoverAndLoadIaCProvider end-to-end — the latter spawns a real
// plugin subprocess + reads the filesystem, which is the cross-plugin-
// build matrix's job (Task 6, on main). Surfacing the boundary
// invariant here catches signature drift between the loader and
// typedIaCAdapter without paying the subprocess cost.

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

// stubIaCAdapter satisfies iacAdapterAccessor against an in-process
// gRPC server registered with a stub IaCProviderRequiredServer. Used
// by the boundary tests to avoid spawning a real plugin subprocess.
type stubIaCAdapter struct {
	conn     *grpc.ClientConn
	registry *pb.ContractRegistry
	regErr   error
}

func (s *stubIaCAdapter) Conn() *grpc.ClientConn                 { return s.conn }
func (s *stubIaCAdapter) ContractRegistry() *pb.ContractRegistry { return s.registry }
func (s *stubIaCAdapter) ContractRegistryError() error           { return s.regErr }

// requiredOnlyServer satisfies pb.IaCProviderRequiredServer with the
// absolute minimum: only Initialize responds (every other method left
// to UnimplementedIaCProviderRequiredServer's defaults). Initialize is
// the one method buildTypedIaCAdapterFrom calls during loader path.
type requiredOnlyServer struct {
	pb.UnimplementedIaCProviderRequiredServer
}

func (s *requiredOnlyServer) Initialize(_ context.Context, _ *pb.InitializeRequest) (*pb.InitializeResponse, error) {
	return &pb.InitializeResponse{}, nil
}

// startInProcessTypedServer spins up an in-process gRPC server that
// registers the typed IaCProviderRequired service and returns a
// dial-back conn the test can hand to a stubIaCAdapter.
func startInProcessTypedServer(t *testing.T) (*grpc.Server, *grpc.ClientConn) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	srv := grpc.NewServer()
	pb.RegisterIaCProviderRequiredServer(srv, &requiredOnlyServer{})
	go func() { _ = srv.Serve(lis) }()
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		srv.Stop()
		t.Fatalf("grpc.NewClient: %v", err)
	}
	return srv, conn
}

// TestDiscoverAndLoadIaCProvider_ReturnsTypedClient asserts that the
// loader's post-LoadPlugin path returns the typed adapter
// (*typedIaCAdapter) — the cutover invariant. Per Spec Step 1.
func TestDiscoverAndLoadIaCProvider_ReturnsTypedClient(t *testing.T) {
	srv, conn := startInProcessTypedServer(t)
	defer srv.Stop()
	defer conn.Close()

	stub := &stubIaCAdapter{
		conn: conn,
		registry: &pb.ContractRegistry{
			Contracts: []*pb.ContractDescriptor{
				{
					Kind:        pb.ContractKind_CONTRACT_KIND_SERVICE,
					ServiceName: iacServiceRequired,
				},
			},
		},
	}

	provider, err := buildTypedIaCAdapterFrom(context.Background(), "stub-provider", "workflow-plugin-stub", map[string]any{}, stub)
	if err != nil {
		t.Fatalf("buildTypedIaCAdapterFrom: %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil interfaces.IaCProvider; got nil")
	}
	// Cutover invariant: the loader returns *typedIaCAdapter, NOT
	// the legacy *remoteIaCProvider (which is deleted in this PR
	// alongside the InvokeService string-dispatch surface). A
	// regression that re-introduces the legacy proxy would fail
	// this assertion at compile time (remoteIaCProvider type no
	// longer exists) AND at runtime via the explicit cast below.
	if _, ok := provider.(*typedIaCAdapter); !ok {
		t.Fatalf("expected *typedIaCAdapter; got %T", provider)
	}
}

// TestDiscoverAndLoadIaCProvider_RejectsMissingRequiredService asserts
// that the loader rejects plugins whose ContractRegistry omits the
// IaCProviderRequired service — the strict-contracts hard-cutover
// invariant. Plugins that haven't migrated to the typed protocol
// fail loud at load time with a `wfctl plugin update` hint.
func TestDiscoverAndLoadIaCProvider_RejectsMissingRequiredService(t *testing.T) {
	srv, conn := startInProcessTypedServer(t)
	defer srv.Stop()
	defer conn.Close()

	stub := &stubIaCAdapter{
		conn:     conn,
		registry: &pb.ContractRegistry{}, // empty: no IaCProviderRequired
	}

	_, err := buildTypedIaCAdapterFrom(context.Background(), "stub-provider", "workflow-plugin-stub", map[string]any{}, stub)
	if err == nil {
		t.Fatal("expected error when ContractRegistry omits IaCProviderRequired; got nil")
	}
	// Message contract: must name the missing service + actionable
	// upgrade hint so operators know how to recover.
	msg := err.Error()
	for _, want := range []string{
		"workflow-plugin-stub",
		iacServiceRequired,
		"wfctl plugin update",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message %q missing expected substring %q", msg, want)
		}
	}
}

// TestDiscoverAndLoadIaCProvider_SurfacesContractRegistryError asserts
// that a transport-level ContractRegistry RPC failure is surfaced AS
// the underlying error rather than masked by the generic "does not
// register the required service" message — per Copilot finding on PR #609.
func TestDiscoverAndLoadIaCProvider_SurfacesContractRegistryError(t *testing.T) {
	srv, conn := startInProcessTypedServer(t)
	defer srv.Stop()
	defer conn.Close()

	regErr := errors.New("ContractRegistry transport reset")
	stub := &stubIaCAdapter{
		conn:   conn,
		regErr: regErr,
	}

	_, err := buildTypedIaCAdapterFrom(context.Background(), "stub-provider", "workflow-plugin-stub", map[string]any{}, stub)
	if err == nil {
		t.Fatal("expected error when ContractRegistry RPC failed; got nil")
	}
	if !errors.Is(err, regErr) {
		t.Fatalf("expected errors.Is(err, regErr); got %v", err)
	}
	if !strings.Contains(err.Error(), "ContractRegistry RPC failed") {
		t.Errorf("expected RPC-failure framing in error; got %q", err.Error())
	}
}
