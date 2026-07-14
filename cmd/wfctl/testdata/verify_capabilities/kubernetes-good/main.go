package main

import (
	"context"
	_ "embed"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	sdk "github.com/GoCodeAlone/workflow/plugin/external/sdk"
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

func main() {
	sdk.ServeIaCPlugin(fixture{}, sdk.IaCServeOptions{
		ManifestProvider: sdk.MustEmbedManifest(manifestData),
		BuildVersion:     sdk.ResolveBuildVersion(Version),
	})
}
