//go:build integration
// +build integration

// Package sdk integration tests — only built with `-tags=integration`.
// These exercise the typed IaC contract end-to-end through a real
// in-process gRPC channel (bufconn), catching the bug class the
// strict-contracts cutover closes:
//
//   - missing client method bridge — the typed pb.IaCProvider*Client
//     would fail to compile if iac.proto dropped a method
//   - missing server dispatcher — the auto-registration helper would
//     reject a stub that doesn't satisfy IaCProviderRequiredServer
//   - structpb conversion drops — typed messages mean no
//     map[string]any roundtrip surface
//
// Subprocess wire-test variant runs in
// .github/workflows/cross-plugin-build-test.yml against the real DO
// plugin binary once it ships at v1.0.0; the in-process variant runs
// in workflow's own CI on every IaC-touching PR.
package sdk_test

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

const (
	e2eBufSize     = 1024 * 1024
	e2eRPCDeadline = 5 * time.Second
)

// TestIaC_EndToEnd_RequiredAndOptional_TypedDispatch starts an
// in-process gRPC server with the typed IaCProviderRequired +
// Enumerator services registered via sdk.RegisterAllIaCProviderServices,
// dials it through bufconn, invokes typed RPCs on both services, and
// asserts the responses come back with the expected shape.
//
// This is the canonical workflow-side smoke test for the typed IaC
// contract. The cross-plugin-build matrix runs an analogous test
// against the real DO plugin binary; the in-process flavor here
// exercises the same code paths through a real gRPC channel without
// requiring the DO plugin to be cross-built first.
func TestIaC_EndToEnd_RequiredAndOptional_TypedDispatch(t *testing.T) {
	listener := bufconn.Listen(e2eBufSize)
	t.Cleanup(func() { _ = listener.Close() })
	server := grpc.NewServer()

	provider := &e2eProvider{
		name:    "fake-iac",
		version: "0.0.0",
		enumerateAllResp: &pb.EnumerateAllResponse{
			Outputs: []*pb.ResourceOutput{
				{
					Name:       "test-spaces-key",
					Type:       "infra.spaces_key",
					ProviderId: "ABC123",
					Status:     "active",
					Sensitive:  map[string]bool{"secret": true},
				},
			},
		},
	}
	if err := sdk.RegisterAllIaCProviderServices(server, provider); err != nil {
		t.Fatalf("RegisterAllIaCProviderServices: %v", err)
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
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	// Per cycle 4 review PR 603: bound RPC deadline so a transport-
	// level hang doesn't pin the CI worker until the suite-wide
	// timeout. Same instance covers all 3 RPCs in this test.
	ctx, cancel := context.WithTimeout(context.Background(), e2eRPCDeadline)
	t.Cleanup(cancel)

	// Required service: Name + Version typed RPCs.
	requiredClient := pb.NewIaCProviderRequiredClient(conn)
	nameResp, err := requiredClient.Name(ctx, &pb.NameRequest{})
	if err != nil {
		t.Fatalf("Name: %v", err)
	}
	if nameResp.Name != "fake-iac" {
		t.Errorf("expected name=fake-iac, got %q", nameResp.Name)
	}
	versionResp, err := requiredClient.Version(ctx, &pb.VersionRequest{})
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if versionResp.Version != "0.0.0" {
		t.Errorf("expected version=0.0.0, got %q", versionResp.Version)
	}

	// Optional service: Enumerator.EnumerateAll typed RPC.
	enumClient := pb.NewIaCProviderEnumeratorClient(conn)
	enumResp, err := enumClient.EnumerateAll(ctx, &pb.EnumerateAllRequest{
		ResourceType: "infra.spaces_key",
	})
	if err != nil {
		t.Fatalf("EnumerateAll: %v", err)
	}
	if got := len(enumResp.Outputs); got != 1 {
		t.Fatalf("expected 1 output; got %d", got)
	}
	out := enumResp.Outputs[0]
	if out.Name != "test-spaces-key" {
		t.Errorf("output.Name = %q; want test-spaces-key", out.Name)
	}
	if out.ProviderId != "ABC123" {
		t.Errorf("output.ProviderId = %q; want ABC123", out.ProviderId)
	}
	// Crucial assertion: typed map<string,bool> survived the roundtrip.
	// The pre-cutover structpb path silently dropped this map.
	if got := out.Sensitive["secret"]; !got {
		t.Errorf("output.Sensitive[\"secret\"] = false; want true (typed map<string,bool> roundtrip lost?)")
	}
}

// TestIaC_EndToEnd_OptionalNotRegistered_ClientFailsTyped asserts that
// when a provider does NOT satisfy the optional Enumerator interface,
// the typed enumerator client receives a typed gRPC error (codes.
// Unimplemented). This is the "absence of registration IS the negative
// signal" contract from the design — wfctl observes the absence at the
// gRPC layer rather than via a NotSupported flag in the response body.
func TestIaC_EndToEnd_OptionalNotRegistered_ClientFailsTyped(t *testing.T) {
	listener := bufconn.Listen(e2eBufSize)
	t.Cleanup(func() { _ = listener.Close() })
	server := grpc.NewServer()
	provider := &requiredOnlyProvider{}
	// requiredOnlyProvider does NOT embed UnimplementedIaCProviderEnumeratorServer,
	// so RegisterAllIaCProviderServices skips Enumerator registration —
	// absence of the registration IS the negative signal per design.
	if err := sdk.RegisterAllIaCProviderServices(server, provider); err != nil {
		t.Fatalf("register: %v", err)
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
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	// Per cycle 4 review PR 603: bound RPC deadline so a transport-
	// level hang doesn't pin the CI worker until the suite-wide
	// timeout.
	ctx, cancel := context.WithTimeout(context.Background(), e2eRPCDeadline)
	t.Cleanup(cancel)

	enumClient := pb.NewIaCProviderEnumeratorClient(conn)
	_, err = enumClient.EnumerateAll(ctx, &pb.EnumerateAllRequest{ResourceType: "x"})
	if err == nil {
		t.Fatalf("expected typed gRPC error for unregistered Enumerator service; got nil")
	}
	// Per cycle 4 review MINOR-1: pin the failure to codes.Unimplemented
	// specifically so a future bufconn/transport behavior change can't
	// silently mask the absence-of-registration signal under a different
	// status code.
	if got := status.Code(err); got != codes.Unimplemented {
		t.Fatalf("expected codes.Unimplemented for unregistered service; got %v (err=%v)", got, err)
	}
}

// e2eProvider satisfies pb.IaCProviderRequiredServer + pb.IaCProviderEnumeratorServer
// for the in-process E2E test. It overrides only Name, Version, and
// EnumerateAll; every other Required RPC delegates to the
// Unimplemented*Server defaults (codes.Unimplemented), which is fine
// for this smoke test — we are exercising the wire layer, not provider
// behavior.
type e2eProvider struct {
	pb.UnimplementedIaCProviderRequiredServer
	pb.UnimplementedIaCProviderEnumeratorServer

	name             string
	version          string
	enumerateAllResp *pb.EnumerateAllResponse
}

func (p *e2eProvider) Name(_ context.Context, _ *pb.NameRequest) (*pb.NameResponse, error) {
	return &pb.NameResponse{Name: p.name}, nil
}

func (p *e2eProvider) Version(_ context.Context, _ *pb.VersionRequest) (*pb.VersionResponse, error) {
	return &pb.VersionResponse{Version: p.version}, nil
}

func (p *e2eProvider) EnumerateAll(_ context.Context, _ *pb.EnumerateAllRequest) (*pb.EnumerateAllResponse, error) {
	if p.enumerateAllResp == nil {
		return &pb.EnumerateAllResponse{}, nil
	}
	return p.enumerateAllResp, nil
}

// requiredOnlyProvider satisfies pb.IaCProviderRequiredServer ONLY —
// it deliberately omits the UnimplementedIaCProviderEnumeratorServer
// embed so the auto-registration helper does NOT advertise the
// optional Enumerator service. The "absence of registration IS the
// negative signal" contract from the design is exercised end-to-end
// via TestIaC_EndToEnd_OptionalNotRegistered_ClientFailsTyped.
type requiredOnlyProvider struct {
	pb.UnimplementedIaCProviderRequiredServer
}
