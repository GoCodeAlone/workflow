package main

import (
	"context"
	"errors"
	"net"
	"slices"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/interfaces"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"github.com/GoCodeAlone/workflow/plugin/external/sdk"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type typedSensitiveInputLegacyProvider struct {
	pb.UnimplementedIaCProviderRequiredServer
	pb.UnimplementedResourceDriverServer
}

type typedSensitiveInputProvider struct {
	typedSensitiveInputLegacyProvider
	pb.UnimplementedResourceSensitiveInputDeclarerServer
	paths    []string
	err      error
	requests chan string
	canceled chan struct{}
}

func (p *typedSensitiveInputProvider) SensitiveInputPaths(ctx context.Context, req *pb.ResourceSensitiveInputPathsRequest) (*pb.ResourceSensitiveInputPathsResponse, error) {
	p.requests <- req.GetResourceType()
	if p.canceled != nil {
		<-ctx.Done()
		close(p.canceled)
		return nil, status.FromContextError(ctx.Err()).Err()
	}
	if p.err != nil {
		return nil, p.err
	}
	return &pb.ResourceSensitiveInputPathsResponse{Paths: p.paths}, nil
}

func typedSensitiveInputConnection(t *testing.T, provider any, opts ...grpc.DialOption) (*grpc.ClientConn, map[string]bool) {
	t.Helper()
	server := grpc.NewServer()
	t.Cleanup(server.Stop)
	if err := sdk.RegisterAllIaCProviderServices(server, provider); err != nil {
		t.Fatal(err)
	}
	advertised := make(map[string]bool)
	for _, contract := range sdk.BuildContractRegistry(server).GetContracts() {
		if contract.GetKind() == pb.ContractKind_CONTRACT_KIND_SERVICE {
			advertised[contract.GetServiceName()] = true
		}
	}
	listener := bufconn.Listen(1024 * 1024)
	t.Cleanup(func() { _ = listener.Close() })
	go func() { _ = server.Serve(listener) }()
	opts = append(opts,
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	conn, err := grpc.NewClient("passthrough:///typed-sensitive-input-test", opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn, advertised
}

func typedSensitiveInputDeclarer(t *testing.T, adapter *typedIaCAdapter, resourceType string) interfaces.ResourceSensitiveInputDeclarer {
	t.Helper()
	driver, err := adapter.ResourceDriver(resourceType)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := driver.(*typedResourceDriver); !ok {
		t.Fatalf("driver = %T, want real typed CLI driver", driver)
	}
	declarer, ok := driver.(interfaces.ResourceSensitiveInputDeclarer)
	if !ok {
		t.Fatal("typed CLI driver must expose optional sensitive input declarations")
	}
	return declarer
}

func TestTypedResourceDriver_SensitiveInputPaths(t *testing.T) {
	service := pb.ResourceSensitiveInputDeclarer_ServiceDesc.ServiceName
	for _, tc := range []struct {
		name         string
		legacy       bool
		unadvertised bool
		paths        []string
		rpcErr       error
		want         []string
		wantErr      error
		wantCode     codes.Code
	}{
		{name: "legacy absence", legacy: true, wantErr: interfaces.ErrProviderMethodUnimplemented, wantCode: codes.Unknown},
		{name: "registered but not advertised", unadvertised: true, paths: []string{"/password"}, wantErr: interfaces.ErrProviderMethodUnimplemented, wantCode: codes.Unknown},
		{name: "advertised unimplemented", rpcErr: status.Error(codes.Unimplemented, "old provider"), wantErr: interfaces.ErrProviderMethodUnimplemented, wantCode: codes.Unimplemented},
		{name: "permission denied", rpcErr: status.Error(codes.PermissionDenied, "denied"), wantCode: codes.PermissionDenied},
		{name: "unavailable", rpcErr: status.Error(codes.Unavailable, "unavailable"), wantCode: codes.Unavailable},
		{name: "malformed escape", paths: []string{"/password", "/bad~2"}, wantErr: interfaces.ErrValidation, wantCode: codes.Unknown},
		{name: "root pointer", paths: []string{""}, wantErr: interfaces.ErrValidation, wantCode: codes.Unknown},
		{name: "relative pointer", paths: []string{"env/*/value"}, wantErr: interfaces.ErrValidation, wantCode: codes.Unknown},
		{name: "nested wildcard", paths: []string{"/password", "/services/*/env/*/value", "/escaped~1key/~0token"}, want: []string{"/password", "/services/*/env/*/value", "/escaped~1key/~0token"}},
		{name: "empty declaration"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := &typedSensitiveInputProvider{paths: tc.paths, err: tc.rpcErr, requests: make(chan string, 1)}
			var registeredProvider any = provider
			if tc.legacy {
				registeredProvider = &typedSensitiveInputLegacyProvider{}
			}
			// Retain the actual decoded RPC response to verify defensive copying.
			var response *pb.ResourceSensitiveInputPathsResponse
			conn, advertised := typedSensitiveInputConnection(t, registeredProvider,
				grpc.WithUnaryInterceptor(func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
					err := invoker(ctx, method, req, reply, cc, opts...)
					response, _ = reply.(*pb.ResourceSensitiveInputPathsResponse)
					return err
				}),
			)
			if advertised[service] == tc.legacy {
				t.Fatalf("SDK advertised sensitive input service = %v, legacy = %v", advertised[service], tc.legacy)
			}
			if tc.unadvertised {
				delete(advertised, service)
			}
			declarer := typedSensitiveInputDeclarer(t, newTypedIaCAdapter(conn, advertised), "stub.database")
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			got, err := declarer.SensitiveInputPaths(ctx)
			if !slices.Equal(got, tc.want) || !errors.Is(err, tc.wantErr) && tc.wantErr != nil || status.Code(err) != tc.wantCode {
				t.Fatalf("paths = %v, err = %v (%v); want %v, %v (%v)", got, err, status.Code(err), tc.want, tc.wantErr, tc.wantCode)
			}
			if tc.wantErr == nil && errors.Is(err, interfaces.ErrProviderMethodUnimplemented) {
				t.Fatalf("RPC failure misclassified as optional absence: %v", err)
			}
			if tc.wantCode == codes.OK && err != nil {
				t.Fatalf("unexpected declaration error: %v", err)
			}
			if err != nil && got != nil {
				t.Fatalf("failed declaration returned partial paths: %v", got)
			}
			if tc.rpcErr != nil && tc.wantErr == nil && status.Convert(err).Message() != status.Convert(tc.rpcErr).Message() {
				t.Fatalf("RPC failure message changed: %v", err)
			}
			select {
			case resourceType := <-provider.requests:
				if tc.unadvertised || tc.legacy {
					t.Fatal("unadvertised service received an RPC")
				}
				if resourceType != "stub.database" {
					t.Fatalf("RPC resource type = %q, want stub.database", resourceType)
				}
			default:
				if !tc.unadvertised && !tc.legacy {
					t.Fatal("advertised service did not receive an RPC")
				}
			}
			if len(got) > 0 {
				got[0] = "/caller-change"
				if response.GetPaths()[0] != tc.want[0] {
					t.Fatal("returned paths alias the decoded RPC response")
				}
				response.Paths[0] = "/response-change"
				if got[0] != "/caller-change" {
					t.Fatal("decoded RPC response aliases the returned paths")
				}
			}
		})
	}
}

func TestTypedResourceDriver_SensitiveInputPathsCallerCancellation(t *testing.T) {
	provider := &typedSensitiveInputProvider{requests: make(chan string, 1), canceled: make(chan struct{})}
	conn, advertised := typedSensitiveInputConnection(t, provider)
	declarer := typedSensitiveInputDeclarer(t, newTypedIaCAdapter(conn, advertised), "infra.container_service")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	type result struct {
		paths []string
		err   error
	}
	results := make(chan result, 1)
	go func() {
		paths, err := declarer.SensitiveInputPaths(ctx)
		results <- result{paths: paths, err: err}
	}()
	select {
	case resourceType := <-provider.requests:
		if resourceType != "infra.container_service" {
			t.Fatalf("RPC resource type = %q", resourceType)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RPC did not reach the provider")
	}
	cancel()
	select {
	case got := <-results:
		if got.paths != nil || status.Code(got.err) != codes.Canceled || errors.Is(got.err, interfaces.ErrProviderMethodUnimplemented) {
			t.Fatalf("paths = %v, err = %v; want caller cancellation", got.paths, got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("caller cancellation did not stop the RPC")
	}
	select {
	case <-provider.canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("caller cancellation did not reach the provider context")
	}
}
