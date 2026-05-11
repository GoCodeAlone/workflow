package main

import (
	"context"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

type provider struct {
	pb.UnimplementedIaCProviderRequiredServer
}

func (p *provider) Name(context.Context, *pb.NameRequest) (*pb.NameResponse, error) {
	return &pb.NameResponse{Name: "iac-pass"}, nil
}

func (p *provider) Version(context.Context, *pb.VersionRequest) (*pb.VersionResponse, error) {
	return &pb.VersionResponse{Version: "0.1.0"}, nil
}

func (p *provider) Capabilities(context.Context, *pb.CapabilitiesRequest) (*pb.CapabilitiesResponse, error) {
	return &pb.CapabilitiesResponse{CanonicalKeys: []string{"region"}}, nil
}

func main() {
	sdk.ServeIaCPlugin(&provider{}, sdk.IaCServeOptions{})
}
