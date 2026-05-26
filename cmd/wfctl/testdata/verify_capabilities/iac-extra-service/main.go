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

// FinalizeApply satisfied by embedded UnimplementedIaCProviderFinalizerServer.
type fixture struct {
	pb.UnimplementedIaCProviderRequiredServer
	pb.UnimplementedIaCProviderFinalizerServer
}

func (fixture) Name(context.Context, *pb.NameRequest) (*pb.NameResponse, error) {
	return &pb.NameResponse{Name: "verify-iac-extra"}, nil
}

func main() {
	sdk.ServeIaCPlugin(fixture{}, sdk.IaCServeOptions{
		ManifestProvider: sdk.MustEmbedManifest(manifestData),
		BuildVersion:     sdk.ResolveBuildVersion(Version),
	})
}
