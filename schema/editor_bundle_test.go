package schema

import (
	"testing"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

func TestExportEditorBundleIncludesCoreAuthoringSections(t *testing.T) {
	bundle, err := ExportEditorBundle(EditorBundleOptions{
		WorkflowVersion: "test-version",
	})
	if err != nil {
		t.Fatalf("export editor bundle: %v", err)
	}

	if bundle.Version == "" {
		t.Fatal("expected explicit bundle schema version")
	}
	if bundle.WorkflowVersion != "test-version" {
		t.Fatalf("workflow version = %q, want test-version", bundle.WorkflowVersion)
	}
	if len(bundle.ModuleSchemas) == 0 {
		t.Fatal("expected module schemas")
	}
	if bundle.ModuleSchemas["http.server"] == nil {
		t.Fatal("expected http.server module schema")
	}
	if len(bundle.StepSchemas) == 0 {
		t.Fatal("expected step schemas")
	}
	if bundle.StepSchemas["step.validate"] == nil {
		t.Fatal("expected step.validate schema")
	}
	if len(bundle.CoercionRules) == 0 {
		t.Fatal("expected coercion rules")
	}
	if len(bundle.Snippets) == 0 {
		t.Fatal("expected editor snippets")
	}
	if bundle.Schemas.App == nil {
		t.Fatal("expected app.yaml schema")
	}
	if bundle.Schemas.Infra == nil {
		t.Fatal("expected infra.yaml schema")
	}
	if bundle.Schemas.Wfctl == nil {
		t.Fatal("expected wfctl.yaml schema")
	}
}

func TestExportEditorBundleNormalizesStrictContractsAndMessageMetadata(t *testing.T) {
	registry := &pb.ContractRegistry{
		FileDescriptorSet: testEditorBundleDescriptorSet(),
		Contracts: []*pb.ContractDescriptor{
			{
				Kind:          pb.ContractKind_CONTRACT_KIND_STEP,
				StepType:      "step.strict_test",
				Mode:          pb.ContractMode_CONTRACT_MODE_STRICT_PROTO,
				ConfigMessage: "workflow.test.StrictConfig",
				InputMessage:  "workflow.test.StrictInput",
				OutputMessage: "workflow.test.StrictOutput",
			},
		},
	}

	bundle, err := ExportEditorBundle(EditorBundleOptions{
		ContractRegistries: []EditorContractRegistrySource{
			{
				Plugin:   "workflow-plugin-strict-test",
				Source:   EditorContractSourceLivePlugin,
				Registry: registry,
			},
		},
	})
	if err != nil {
		t.Fatalf("export editor bundle: %v", err)
	}

	contract := bundle.Contracts["step:step.strict_test"]
	if contract == nil {
		t.Fatalf("expected strict step contract, got keys %v", bundle.Contracts)
	}
	if contract.Plugin != "workflow-plugin-strict-test" {
		t.Fatalf("plugin = %q", contract.Plugin)
	}
	if contract.OwnerType != "step" || contract.OwnerKey != "step.strict_test" {
		t.Fatalf("owner = %s/%s", contract.OwnerType, contract.OwnerKey)
	}
	if contract.Mode != "strict" {
		t.Fatalf("mode = %q, want strict", contract.Mode)
	}
	if contract.ConfigMessage != "workflow.test.StrictConfig" {
		t.Fatalf("config message = %q", contract.ConfigMessage)
	}
	if contract.RequestMessage != "workflow.test.StrictInput" {
		t.Fatalf("request message = %q", contract.RequestMessage)
	}
	if contract.ResponseMessage != "workflow.test.StrictOutput" {
		t.Fatalf("response message = %q", contract.ResponseMessage)
	}
	if contract.DescriptorSetRef == "" {
		t.Fatal("expected descriptor set reference on contract")
	}
	if bundle.DescriptorSets[contract.DescriptorSetRef] == nil {
		t.Fatalf("descriptor set ref %q not present in descriptorSets", contract.DescriptorSetRef)
	}

	input := bundle.Messages["workflow.test.StrictInput"]
	if input == nil {
		t.Fatalf("expected input message metadata, got keys %v", bundle.Messages)
	}
	if input.DescriptorSetRef != contract.DescriptorSetRef {
		t.Fatalf("input descriptor set ref = %q, want %q", input.DescriptorSetRef, contract.DescriptorSetRef)
	}
	assertMessageField(t, input, "customer_id", "string", false)
	assertMessageField(t, input, "quantity", "int32", false)

	output := bundle.Messages["workflow.test.StrictOutput"]
	if output == nil {
		t.Fatal("expected output message metadata")
	}
	assertMessageField(t, output, "accepted", "bool", false)
	assertMessageField(t, output, "warnings", "string", true)
}

func TestExportEditorBundleIncludesExternalDescriptorSetReferences(t *testing.T) {
	bundle, err := ExportEditorBundle(EditorBundleOptions{
		ContractRegistries: []EditorContractRegistrySource{
			{
				Plugin:           "workflow-plugin-ref-test",
				Source:           EditorContractSourcePluginContractsJSON,
				DescriptorSetRef: "descriptors/workflow-plugin-ref-test.pb",
				Registry: &pb.ContractRegistry{
					Contracts: []*pb.ContractDescriptor{
						{
							Kind:         pb.ContractKind_CONTRACT_KIND_STEP,
							StepType:     "step.ref_test",
							Mode:         pb.ContractMode_CONTRACT_MODE_STRICT_PROTO,
							InputMessage: "workflow.test.RefInput",
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("export editor bundle: %v", err)
	}

	contract := bundle.Contracts["step:step.ref_test"]
	if contract == nil {
		t.Fatal("expected step contract")
	}
	if contract.DescriptorSetRef != "descriptors/workflow-plugin-ref-test.pb" {
		t.Fatalf("descriptor set ref = %q", contract.DescriptorSetRef)
	}
	ref := bundle.DescriptorSets["descriptors/workflow-plugin-ref-test.pb"]
	if ref == nil {
		t.Fatalf("expected descriptor set reference entry, got %+v", bundle.DescriptorSets)
	}
	if ref.ExternalRef != "descriptors/workflow-plugin-ref-test.pb" {
		t.Fatalf("external ref = %q", ref.ExternalRef)
	}
	if ref.Plugin != "workflow-plugin-ref-test" {
		t.Fatalf("plugin = %q", ref.Plugin)
	}
}

func assertMessageField(t *testing.T, msg *EditorMessageDescriptor, name, typ string, repeated bool) {
	t.Helper()
	for _, field := range msg.Fields {
		if field.Name == name {
			if field.Type != typ {
				t.Fatalf("%s field type = %q, want %q", name, field.Type, typ)
			}
			if field.Repeated != repeated {
				t.Fatalf("%s repeated = %v, want %v", name, field.Repeated, repeated)
			}
			return
		}
	}
	t.Fatalf("field %q not found in %+v", name, msg.Fields)
}

func testEditorBundleDescriptorSet() *descriptorpb.FileDescriptorSet {
	labelOptional := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
	labelRepeated := descriptorpb.FieldDescriptorProto_LABEL_REPEATED
	typeString := descriptorpb.FieldDescriptorProto_TYPE_STRING
	typeInt32 := descriptorpb.FieldDescriptorProto_TYPE_INT32
	typeBool := descriptorpb.FieldDescriptorProto_TYPE_BOOL

	return &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{
		{
			Name:    strPtr("workflow/test/strict.proto"),
			Package: strPtr("workflow.test"),
			Syntax:  strPtr("proto3"),
			MessageType: []*descriptorpb.DescriptorProto{
				{
					Name: strPtr("StrictConfig"),
					Field: []*descriptorpb.FieldDescriptorProto{
						{Name: strPtr("region"), Number: int32Ptr(1), Label: &labelOptional, Type: &typeString},
					},
				},
				{
					Name: strPtr("StrictInput"),
					Field: []*descriptorpb.FieldDescriptorProto{
						{Name: strPtr("customer_id"), Number: int32Ptr(1), Label: &labelOptional, Type: &typeString},
						{Name: strPtr("quantity"), Number: int32Ptr(2), Label: &labelOptional, Type: &typeInt32},
					},
				},
				{
					Name: strPtr("StrictOutput"),
					Field: []*descriptorpb.FieldDescriptorProto{
						{Name: strPtr("accepted"), Number: int32Ptr(1), Label: &labelOptional, Type: &typeBool},
						{Name: strPtr("warnings"), Number: int32Ptr(2), Label: &labelRepeated, Type: &typeString},
					},
				},
			},
		},
	}}
}

func strPtr(v string) *string { return &v }

func int32Ptr(v int32) *int32 { return &v }
