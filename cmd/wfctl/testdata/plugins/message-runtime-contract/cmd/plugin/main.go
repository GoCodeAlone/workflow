package main

import (
	"context"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"github.com/GoCodeAlone/workflow/plugin/external/sdk"
	"google.golang.org/protobuf/types/known/emptypb"
)

var Version = "0.0.0"

type provider struct {
	pb.UnimplementedPluginServiceServer
}

func main() {
	_ = sdk.WithBuildVersion(sdk.ResolveBuildVersion(Version))
}

func (provider) GetContractRegistry(context.Context, *emptypb.Empty) (*pb.ContractRegistry, error) {
	return sdk.BuildMessageContractRegistry(sdk.MessageContract{
		ContractType:    "compute.network_audit_evidence.v1",
		ProtoPackage:    "workflow_plugin_compute_core.protocol.v1",
		MessageNames:    []string{"NetworkAuditRecord", "NetworkAuditRecordProjection"},
		GoImportPath:    "github.com/GoCodeAlone/workflow-plugin-compute-core/protocol/pb",
		SchemaDigest:    "sha256:0123456789abcdef",
		ProtocolVersion: "compute.v1alpha1",
	})
}
