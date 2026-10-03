package sdk_test

import (
	"context"
	"errors"
	"net"
	"slices"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/providerclient"
	"github.com/GoCodeAlone/workflow/interfaces"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"github.com/GoCodeAlone/workflow/plugin/external/sdk"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type sensitiveInputContractProvider struct {
	pb.UnimplementedIaCProviderRequiredServer
	pb.UnimplementedResourceDriverServer
	pb.UnimplementedResourceSensitiveInputDeclarerServer
	paths  []string
	err    error
	typeID string
}

func (p *sensitiveInputContractProvider) SensitiveInputPaths(_ context.Context, req *pb.ResourceSensitiveInputPathsRequest) (*pb.ResourceSensitiveInputPathsResponse, error) {
	p.typeID = req.GetResourceType()
	if p.err != nil {
		return nil, p.err
	}
	return &pb.ResourceSensitiveInputPathsResponse{Paths: p.paths}, nil
}

type legacySensitiveInputProvider struct {
	pb.UnimplementedIaCProviderRequiredServer
	pb.UnimplementedResourceDriverServer
}

func TestSensitiveInputContractSDKRoundTripAndLegacyAbsence(t *testing.T) {
	for _, tc := range []struct {
		name     string
		provider any
		want     []string
		wantErr  error
		wantCode codes.Code
	}{
		{"declared", &sensitiveInputContractProvider{paths: []string{"/password", "/env/*/value"}}, []string{"/password", "/env/*/value"}, nil, codes.OK},
		{"legacy absent", &legacySensitiveInputProvider{}, nil, interfaces.ErrProviderMethodUnimplemented, codes.Unknown},
		{"unimplemented", &sensitiveInputContractProvider{err: status.Error(codes.Unimplemented, "old provider")}, nil, interfaces.ErrProviderMethodUnimplemented, codes.Unknown},
		{"malformed declaration", &sensitiveInputContractProvider{paths: []string{"/bad~2"}}, nil, interfaces.ErrValidation, codes.Unknown},
		{"permission denied", &sensitiveInputContractProvider{err: status.Error(codes.PermissionDenied, "denied")}, nil, nil, codes.PermissionDenied},
		{"unavailable", &sensitiveInputContractProvider{err: status.Error(codes.Unavailable, "unavailable")}, nil, nil, codes.Unavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := grpc.NewServer()
			if err := sdk.RegisterAllIaCProviderServices(srv, tc.provider); err != nil {
				t.Fatal(err)
			}
			listener := bufconn.Listen(1024 * 1024)
			t.Cleanup(srv.Stop)
			go func() { _ = srv.Serve(listener) }()
			conn, err := grpc.NewClient("passthrough:///sensitive-input-test",
				grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
				grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = conn.Close() })
			advertised := serviceNamesFromRegistry(sdk.BuildContractRegistry(srv))
			adapter := providerclient.New(conn, advertised)
			driver, err := adapter.ResourceDriver("stub.database")
			if err != nil {
				t.Fatal(err)
			}
			declarer, ok := driver.(interfaces.ResourceSensitiveInputDeclarer)
			if !ok {
				t.Fatal("remote driver must expose declaration discovery")
			}
			got, err := declarer.SensitiveInputPaths(t.Context())
			if (tc.wantErr != nil && !errors.Is(err, tc.wantErr)) || status.Code(err) != tc.wantCode || !slices.Equal(got, tc.want) {
				t.Fatalf("paths=%v err=%v, want=%v err=%v", got, err, tc.want, tc.wantErr)
			}
			if p, ok := tc.provider.(*sensitiveInputContractProvider); ok && p.typeID != "stub.database" {
				t.Fatalf("RPC resource type=%q", p.typeID)
			}
		})
	}
}
