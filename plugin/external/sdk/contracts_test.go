package sdk

import (
	"net"
	"testing"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
)

func TestBuildMessageContractRegistry(t *testing.T) {
	reg, err := BuildMessageContractRegistry(MessageContract{
		ContractType:    "compute.network_audit_evidence.v1",
		ProtoPackage:    "workflow_plugin_compute_core.protocol.v1",
		MessageNames:    []string{"NetworkAuditRecord", "NetworkAuditRecordProjection"},
		GoImportPath:    "github.com/GoCodeAlone/workflow-plugin-compute-core/protocol/pb",
		SchemaDigest:    "sha256:0123456789abcdef",
		ProtocolVersion: "compute.v1alpha1",
	})
	if err != nil {
		t.Fatalf("BuildMessageContractRegistry: %v", err)
	}
	if len(reg.Contracts) != 1 {
		t.Fatalf("contracts = %d, want 1", len(reg.Contracts))
	}
	contract := reg.Contracts[0]
	if contract.Kind != pb.ContractKind_CONTRACT_KIND_MESSAGE {
		t.Fatalf("kind = %v, want MESSAGE", contract.Kind)
	}
	if contract.ContractType != "compute.network_audit_evidence.v1" {
		t.Fatalf("contract type = %q", contract.ContractType)
	}
	if got := contract.MessageNames; len(got) != 2 || got[0] != "NetworkAuditRecord" {
		t.Fatalf("message names = %v", got)
	}
}

func TestBuildMessageContractRegistryRequiresReleaseMetadata(t *testing.T) {
	_, err := BuildMessageContractRegistry(MessageContract{
		ContractType: "compute.network_audit_evidence.v1",
		ProtoPackage: "workflow_plugin_compute_core.protocol.v1",
		MessageNames: []string{"NetworkAuditRecord"},
	})
	if err == nil {
		t.Fatal("expected missing schema/protocol metadata to fail")
	}
}

func TestBuildContractRegistryForPlugin_NilServer(t *testing.T) {
	reg := BuildContractRegistryForPlugin(nil, "workflow.plugin.external.iac.")
	if reg == nil {
		t.Fatal("want non-nil")
	}
	if len(reg.Contracts) != 0 {
		t.Errorf("want 0 contracts; got %d", len(reg.Contracts))
	}
}

func TestBuildContractRegistryForPlugin_FiltersByPrefix(t *testing.T) {
	s := grpc.NewServer()
	pb.RegisterIaCProviderRequiredServer(s, &stubIaCRequired{})
	pb.RegisterPluginServiceServer(s, &stubPluginService{})
	go func() {
		l, _ := net.Listen("tcp", "127.0.0.1:0")
		_ = s.Serve(l)
	}()
	defer s.Stop()
	reg := BuildContractRegistryForPlugin(s, "workflow.plugin.external.iac.")
	if len(reg.Contracts) != 1 {
		t.Fatalf("want 1 contract (iac.IaCProviderRequired); got %d: %v", len(reg.Contracts), reg.Contracts)
	}
	if reg.Contracts[0].ServiceName != "workflow.plugin.external.iac.IaCProviderRequired" {
		t.Errorf("unexpected service: %s", reg.Contracts[0].ServiceName)
	}
}

type stubIaCRequired struct {
	pb.UnimplementedIaCProviderRequiredServer
}
type stubPluginService struct {
	pb.UnimplementedPluginServiceServer
}
