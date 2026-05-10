package sdk

import (
	"fmt"

	"google.golang.org/grpc"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

// RegisterAllIaCProviderServices uses Go type-assertion to register every
// typed IaC gRPC service that the provider satisfies, in a single call.
//
// REQUIRED service:
//
//	pb.IaCProviderRequiredServer — every IaC plugin MUST implement every
//	method on this interface. The type-assert here surfaces missing
//	methods at plugin-startup time as a clear error rather than at the
//	first RPC dispatch with a generic "unimplemented" status.
//
// OPTIONAL services (auto-detected):
//
//	pb.IaCProviderEnumeratorServer
//	pb.IaCProviderDriftDetectorServer
//	pb.IaCProviderCredentialRevokerServer
//	pb.IaCProviderMigrationRepairerServer
//	pb.IaCProviderValidatorServer
//	pb.IaCProviderDriftConfigDetectorServer
//
// ResourceDriver:
//
//	pb.ResourceDriverServer — separate gRPC service, also auto-registered
//	when the provider satisfies it.
//
// Per cycle 3 I-1 of the strict-contracts force-cutover design: plugin
// authors write ONE call; they cannot omit registration for a capability
// they implemented. That eliminates the registration-omission bug class
// (the same shape as the legacy InvokeService case-string-typo bug) by
// removing the manual step entirely.
//
// Capability discovery on the host side uses the existing ContractRegistry
// RPC + FileDescriptorSet mechanism (kept via §Salvage in the design);
// the SDK auto-publishes the registered services there in Task 5.
//
// Plugin authors who DO NOT want a capability advertised must NOT
// implement those methods at the Go level — there is no half-implemented
// stub-and-forget-to-register failure mode.
func RegisterAllIaCProviderServices(s *grpc.Server, provider any) error {
	if s == nil {
		return fmt.Errorf("RegisterAllIaCProviderServices: grpc server is nil")
	}
	if provider == nil {
		return fmt.Errorf("RegisterAllIaCProviderServices: provider is nil")
	}
	required, ok := provider.(pb.IaCProviderRequiredServer)
	if !ok {
		return fmt.Errorf(
			"RegisterAllIaCProviderServices: provider %T does not satisfy "+
				"pb.IaCProviderRequiredServer (missing methods); see "+
				"docs/plans/2026-05-10-strict-contracts-force-cutover-design.md",
			provider,
		)
	}
	pb.RegisterIaCProviderRequiredServer(s, required)

	if v, ok := provider.(pb.IaCProviderEnumeratorServer); ok {
		pb.RegisterIaCProviderEnumeratorServer(s, v)
	}
	if v, ok := provider.(pb.IaCProviderDriftDetectorServer); ok {
		pb.RegisterIaCProviderDriftDetectorServer(s, v)
	}
	if v, ok := provider.(pb.IaCProviderCredentialRevokerServer); ok {
		pb.RegisterIaCProviderCredentialRevokerServer(s, v)
	}
	if v, ok := provider.(pb.IaCProviderMigrationRepairerServer); ok {
		pb.RegisterIaCProviderMigrationRepairerServer(s, v)
	}
	if v, ok := provider.(pb.IaCProviderValidatorServer); ok {
		pb.RegisterIaCProviderValidatorServer(s, v)
	}
	if v, ok := provider.(pb.IaCProviderDriftConfigDetectorServer); ok {
		pb.RegisterIaCProviderDriftConfigDetectorServer(s, v)
	}
	if v, ok := provider.(pb.ResourceDriverServer); ok {
		pb.RegisterResourceDriverServer(s, v)
	}
	return nil
}
