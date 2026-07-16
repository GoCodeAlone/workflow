package main

import (
	"context"
	_ "embed"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var Version = "dev"

//go:embed plugin.json
var manifestData []byte

type fixture struct {
	pb.UnimplementedIaCProviderRequiredServer
	pb.UnimplementedResourceDriverServer
}

func (fixture) Name(context.Context, *pb.NameRequest) (*pb.NameResponse, error) {
	return &pb.NameResponse{Name: "verify-kubernetes"}, nil
}

func (fixture) Capabilities(context.Context, *pb.CapabilitiesRequest) (*pb.CapabilitiesResponse, error) {
	return &pb.CapabilitiesResponse{Capabilities: []*pb.IaCCapabilityDeclaration{
		{ResourceType: "infra.k8s_cluster"},
		{ResourceType: "infra.managed_cluster"},
	}}, nil
}

func (fixture) Read(_ context.Context, request *pb.ResourceReadRequest) (*pb.ResourceReadResponse, error) {
	const resourceType = "infra.managed_cluster"
	if request.GetResourceType() != resourceType || request.GetRef().GetType() != resourceType {
		return nil, status.Errorf(codes.InvalidArgument,
			"managed-b requires resource type %q, got request=%q ref=%q",
			resourceType, request.GetResourceType(), request.GetRef().GetType())
	}
	return &pb.ResourceReadResponse{Output: &pb.ResourceOutput{
		Name:        request.GetRef().GetName(),
		Type:        resourceType,
		Status:      "running",
		OutputsJson: []byte(`{"status":"running","endpoint":"https://managed-b.example.test","version":"1.31"}`),
	}}, nil
}

func main() {
	sdk.ServeIaCPlugin(fixture{}, sdk.IaCServeOptions{
		ManifestProvider: sdk.MustEmbedManifest(manifestData),
		BuildVersion:     sdk.ResolveBuildVersion(Version),
	})
}
