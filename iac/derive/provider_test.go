package derive

import (
	"context"
	"encoding/json"
	"net"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/requirements"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func TestExternalProviderMapperUsesStrictProtoClient(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	pb.RegisterIaCProviderRequirementMapperServer(srv, mapperServer{})
	go func() {
		_ = srv.Serve(listener)
	}()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc client: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	mapper := ExternalProviderMapper{Client: pb.NewIaCProviderRequirementMapperClient(conn)}
	result, err := mapper.MapRequirements(context.Background(), MapRequest{
		Provider:    "digitalocean",
		Runtime:     requirements.RuntimeDigitalOceanAppPlatform,
		Environment: "prod",
		Requirements: []requirements.Requirement{{
			Key:  "observability.telemetry.default",
			Kind: requirements.KindObservability,
		}},
	})
	if err != nil {
		t.Fatalf("map requirements: %v", err)
	}
	if len(result.Modules) != 1 || result.Modules[0].Name != "otel" {
		t.Fatalf("modules = %#v", result.Modules)
	}
	if result.Modules[0].Config["image"] != "otel/opentelemetry-collector-contrib:latest" {
		t.Fatalf("module config = %#v", result.Modules[0].Config)
	}
}

type mapperServer struct {
	pb.UnimplementedIaCProviderRequirementMapperServer
}

func (mapperServer) MapRequirements(_ context.Context, req *pb.MapRequirementsRequest) (*pb.MapRequirementsResponse, error) {
	cfg, _ := json.Marshal(map[string]any{"image": "otel/opentelemetry-collector-contrib:latest"})
	return &pb.MapRequirementsResponse{
		AcceptedKeys: []string{req.GetRequirements()[0].GetKey()},
		Modules: []*pb.DerivedModuleSpec{{
			Name:       "otel",
			Type:       "infra.container_service",
			Satisfies:  []string{req.GetRequirements()[0].GetKey()},
			ConfigJson: cfg,
		}},
	}, nil
}
