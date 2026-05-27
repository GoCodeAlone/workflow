package external

import (
	"testing"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	goproto "google.golang.org/protobuf/proto"
)

func TestContractDescriptorPreservesMessageContractFields(t *testing.T) {
	descriptor := &pb.ContractDescriptor{
		Kind:            pb.ContractKind_CONTRACT_KIND_MESSAGE,
		Mode:            pb.ContractMode_CONTRACT_MODE_STRICT_PROTO,
		ContractType:    "compute.network_audit_evidence.v1",
		ProtoPackage:    "workflow_plugin_compute_core.protocol.v1",
		MessageNames:    []string{"NetworkAuditRecord", "NetworkAuditRecordProjection"},
		GoImportPath:    "github.com/GoCodeAlone/workflow-plugin-compute-core/protocol/pb",
		SchemaDigest:    "sha256:0123456789abcdef",
		ProtocolVersion: "compute.v1alpha1",
	}
	data, err := goproto.Marshal(descriptor)
	if err != nil {
		t.Fatalf("marshal descriptor: %v", err)
	}
	var roundTrip pb.ContractDescriptor
	if err := goproto.Unmarshal(data, &roundTrip); err != nil {
		t.Fatalf("unmarshal descriptor: %v", err)
	}
	if roundTrip.GetKind() != pb.ContractKind_CONTRACT_KIND_MESSAGE {
		t.Fatalf("kind = %v, want MESSAGE", roundTrip.GetKind())
	}
	if roundTrip.GetContractType() != descriptor.ContractType {
		t.Fatalf("contract_type = %q, want %q", roundTrip.GetContractType(), descriptor.ContractType)
	}
	if roundTrip.GetProtoPackage() != descriptor.ProtoPackage {
		t.Fatalf("proto_package = %q, want %q", roundTrip.GetProtoPackage(), descriptor.ProtoPackage)
	}
	if got, want := roundTrip.GetMessageNames(), descriptor.MessageNames; len(got) != len(want) || got[1] != want[1] {
		t.Fatalf("message_names = %v, want %v", got, want)
	}
	if roundTrip.GetGoImportPath() != descriptor.GoImportPath {
		t.Fatalf("go_import_path = %q, want %q", roundTrip.GetGoImportPath(), descriptor.GoImportPath)
	}
	if roundTrip.GetSchemaDigest() != descriptor.SchemaDigest {
		t.Fatalf("schema_digest = %q, want %q", roundTrip.GetSchemaDigest(), descriptor.SchemaDigest)
	}
	if roundTrip.GetProtocolVersion() != descriptor.ProtocolVersion {
		t.Fatalf("protocol_version = %q, want %q", roundTrip.GetProtocolVersion(), descriptor.ProtocolVersion)
	}
}
