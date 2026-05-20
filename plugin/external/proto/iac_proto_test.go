package proto_test

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

// iacRequiredMethodsCheck is a locally-enumerated method-signature interface
// covering every RPC defined for the IaCProviderRequired service in
// iac.proto. The blank assertion below pins pb.IaCProviderRequiredServer's
// method set to this list at compile time: if a method is dropped from
// iac.proto and the bindings are regenerated, the assertion fails to compile
// and surfaces the drop loudly. The previous (`var _ pb.X = stub{}`) form
// would still compile because the regenerated stub would also lose the
// method — that test silently followed the proto rather than guarding it.
//
// Apply was removed from iac.proto per workflow#699 (2026-05-17); the
// list below tracks the post-cutover required surface.
type iacRequiredMethodsCheck interface {
	Initialize(context.Context, *pb.InitializeRequest) (*pb.InitializeResponse, error)
	Name(context.Context, *pb.NameRequest) (*pb.NameResponse, error)
	Version(context.Context, *pb.VersionRequest) (*pb.VersionResponse, error)
	Capabilities(context.Context, *pb.CapabilitiesRequest) (*pb.CapabilitiesResponse, error)
	Plan(context.Context, *pb.PlanRequest) (*pb.PlanResponse, error)
	Destroy(context.Context, *pb.DestroyRequest) (*pb.DestroyResponse, error)
	Status(context.Context, *pb.StatusRequest) (*pb.StatusResponse, error)
	Import(context.Context, *pb.ImportRequest) (*pb.ImportResponse, error)
	ResolveSizing(context.Context, *pb.ResolveSizingRequest) (*pb.ResolveSizingResponse, error)
	BootstrapStateBackend(context.Context, *pb.BootstrapStateBackendRequest) (*pb.BootstrapStateBackendResponse, error)
}

// Compile-time guard: drop an RPC from iac.proto and this fails.
var _ iacRequiredMethodsCheck = (pb.IaCProviderRequiredServer)(nil)

// TestIaCProviderRequiredServerHasAllRequiredMethods exists so `go test`
// reports the guard's status; the actual check is at compile time above.
func TestIaCProviderRequiredServerHasAllRequiredMethods(t *testing.T) {
	var _ iacRequiredMethodsCheck = (pb.IaCProviderRequiredServer)(nil)
}

// resourceDriverMethodsCheck enumerates the 9 RPCs ResourceDriver must
// expose, matching iac.proto's `service ResourceDriver` block.
type resourceDriverMethodsCheck interface {
	Create(context.Context, *pb.ResourceCreateRequest) (*pb.ResourceCreateResponse, error)
	Read(context.Context, *pb.ResourceReadRequest) (*pb.ResourceReadResponse, error)
	Update(context.Context, *pb.ResourceUpdateRequest) (*pb.ResourceUpdateResponse, error)
	Delete(context.Context, *pb.ResourceDeleteRequest) (*pb.ResourceDeleteResponse, error)
	Diff(context.Context, *pb.ResourceDiffRequest) (*pb.ResourceDiffResponse, error)
	Scale(context.Context, *pb.ResourceScaleRequest) (*pb.ResourceScaleResponse, error)
	HealthCheck(context.Context, *pb.ResourceHealthCheckRequest) (*pb.ResourceHealthCheckResponse, error)
	SensitiveKeys(context.Context, *pb.SensitiveKeysRequest) (*pb.SensitiveKeysResponse, error)
	Troubleshoot(context.Context, *pb.TroubleshootRequest) (*pb.TroubleshootResponse, error)
}

var _ resourceDriverMethodsCheck = (pb.ResourceDriverServer)(nil)

// TestResourceDriverServerInterfaceExists asserts the generated
// ResourceDriverServer interface still has all 9 RPC methods. Drop one
// from iac.proto and the compile-time guard above will fail.
func TestResourceDriverServerInterfaceExists(t *testing.T) {
	var _ resourceDriverMethodsCheck = (pb.ResourceDriverServer)(nil)
}

// TestOptionalServicesHaveDistinctInterfaces asserts each optional
// service has its own server interface (not method-on-required).
func TestOptionalServicesHaveDistinctInterfaces(t *testing.T) {
	type optional interface {
		pb.IaCProviderEnumeratorServer
		pb.IaCProviderDriftDetectorServer
		pb.IaCProviderCredentialRevokerServer
		pb.IaCProviderMigrationRepairerServer
		pb.IaCProviderValidatorServer
		pb.IaCProviderDriftConfigDetectorServer
		pb.IaCProviderLogCaptureServer
	}
	var _ optional = (*allOptionalStub)(nil)
}

type allOptionalStub struct {
	pb.UnimplementedIaCProviderEnumeratorServer
	pb.UnimplementedIaCProviderDriftDetectorServer
	pb.UnimplementedIaCProviderCredentialRevokerServer
	pb.UnimplementedIaCProviderMigrationRepairerServer
	pb.UnimplementedIaCProviderValidatorServer
	pb.UnimplementedIaCProviderDriftConfigDetectorServer
	pb.UnimplementedIaCProviderLogCaptureServer
}

// TestMigrationRepairConfirmationStringMatchesProtoComment guards the
// confirm_force string constant against drift between iac.proto's comment
// (lines ~322-324) and the Go-side interfaces.MigrationRepairConfirmation
// constant. They must match exactly: "FORCE_MIGRATION_METADATA".
func TestMigrationRepairConfirmationStringMatchesProtoComment(t *testing.T) {
	if interfaces.MigrationRepairConfirmation != "FORCE_MIGRATION_METADATA" {
		t.Fatalf("interfaces.MigrationRepairConfirmation drifted from proto comment in iac.proto:322-324; got %q want %q",
			interfaces.MigrationRepairConfirmation, "FORCE_MIGRATION_METADATA")
	}
}

// TestApplyResultActionsRoundTrip was deleted per workflow#699:
// pb.ApplyResult + pb.ActionResult are gone from iac.proto (the
// per-action outcome surfacing now flows through engine-side hooks,
// not the Apply RPC's response). pb.ActionStatus enum survives —
// covered by TestActionStatusEnumValues below.

// TestActionStatusEnumValues pins the wire-tag → constant mapping for
// ActionStatus. Per plan T1: 0=UNSPECIFIED (rejected by wfctl), 1=SUCCESS,
// 2=ERROR, 3=DELETE_FAILED. Tags 4-5 reserved for Phase 2.3 compensation;
// this test fails loudly if any tag is reassigned.
func TestActionStatusEnumValues(t *testing.T) {
	cases := []struct {
		name string
		val  pb.ActionStatus
		tag  int32
	}{
		{"UNSPECIFIED", pb.ActionStatus_ACTION_STATUS_UNSPECIFIED, 0},
		{"SUCCESS", pb.ActionStatus_ACTION_STATUS_SUCCESS, 1},
		{"ERROR", pb.ActionStatus_ACTION_STATUS_ERROR, 2},
		{"DELETE_FAILED", pb.ActionStatus_ACTION_STATUS_DELETE_FAILED, 3},
	}
	for _, c := range cases {
		if int32(c.val) != c.tag {
			t.Errorf("ActionStatus_%s = %d, want %d", c.name, int32(c.val), c.tag)
		}
	}
}
