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

// CRITICAL: this struct must NOT embed pb.UnimplementedIaCProviderFinalizerServer.
// Embedding the Unimplemented type satisfies the IaCProviderFinalizerServer
// interface (via mustEmbedUnimplementedIaCProviderFinalizerServer sentinel),
// which would make sdk.ServeIaCPlugin's type-assertion succeed and REGISTER
// the Finalizer service — defeating the missing-service test scenario.
type fixture struct {
	pb.UnimplementedIaCProviderRequiredServer
}

func (fixture) Name(context.Context, *pb.NameRequest) (*pb.NameResponse, error) {
	return &pb.NameResponse{Name: "verify-iac-missing"}, nil
}

func main() {
	sdk.ServeIaCPlugin(fixture{}, sdk.IaCServeOptions{
		ManifestProvider: sdk.MustEmbedManifest(manifestData),
		BuildVersion:     sdk.ResolveBuildVersion(Version),
	})
}
