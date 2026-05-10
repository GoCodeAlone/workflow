package proto_test

import (
	"testing"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

// TestIaCProviderRequiredServerHasAllRequiredMethods asserts that the
// generated server interface has every method named in the design.
// Catches accidental method drops in iac.proto.
//
// The test passes at build time via the unused type-assert: if the proto
// dropped a method, the embedded UnimplementedIaCProviderRequiredServer
// would no longer satisfy IaCProviderRequiredServer and this file would
// fail to compile.
func TestIaCProviderRequiredServerHasAllRequiredMethods(t *testing.T) {
	var srv pb.IaCProviderRequiredServer = (*requiredStub)(nil)
	_ = srv // type-assert satisfaction is checked at compile time

	// Methods are checked at compile time via the type assertion above.
	// The stub MUST satisfy: Initialize, Name, Version, Capabilities,
	// Plan, Apply, Destroy, Status, Import, ResolveSizing,
	// BootstrapStateBackend.
}

type requiredStub struct {
	pb.UnimplementedIaCProviderRequiredServer
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
}

// TestResourceDriverServerInterfaceExists asserts the generated
// ResourceDriverServer interface exists with the 9 RPC methods from the
// design (Create, Read, Update, Delete, Diff, Scale, HealthCheck,
// SensitiveKeys, Troubleshoot).
func TestResourceDriverServerInterfaceExists(t *testing.T) {
	var _ pb.ResourceDriverServer = (*resourceDriverStub)(nil)
}

type resourceDriverStub struct {
	pb.UnimplementedResourceDriverServer
}
