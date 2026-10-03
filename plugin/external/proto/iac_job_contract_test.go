package proto_test

import (
	"testing"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestProviderJobContractTypedTargetTimeoutAndSeparateCancellation(t *testing.T) {
	fields := (&pb.JobSpec{}).ProtoReflect().Descriptor().Fields()
	target := fields.ByName("target")
	if target == nil || target.Number() != 11 || target.Kind() != protoreflect.MessageKind || target.Message().FullName() != "workflow.plugin.external.iac.ResourceRef" {
		t.Fatal("JobSpec target must be a typed ResourceRef at field 11")
	}
	timeout := fields.ByName("timeout_seconds")
	if timeout == nil || timeout.Number() != 12 || timeout.Kind() != protoreflect.Int32Kind {
		t.Fatal("JobSpec timeout_seconds must be int32 at field 12")
	}
	runner := pb.File_iac_proto.Services().ByName("IaCProviderRunner")
	if runner.Methods().Len() != 3 || runner.Methods().ByName("CancelJob") != nil {
		t.Fatal("existing runner RPC surface must remain unchanged")
	}
	canceler := pb.File_iac_proto.Services().ByName("IaCProviderJobCanceler")
	if canceler == nil || canceler.Methods().Len() != 1 {
		t.Fatal("cancellation must have its own optional service")
	}
	method := canceler.Methods().ByName("CancelJob")
	if method == nil || method.Input().FullName() != "workflow.plugin.external.iac.JobHandle" || method.Output().FullName() != "google.protobuf.Empty" || method.IsStreamingClient() || method.IsStreamingServer() {
		t.Fatal("CancelJob must accept a typed JobHandle and return Empty")
	}
}
