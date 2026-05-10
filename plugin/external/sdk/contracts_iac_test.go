package sdk_test

import (
	"testing"

	"google.golang.org/grpc"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

// TestBuildContractRegistry_AdvertisesRegisteredIaCServices asserts that
// after calling RegisterAllIaCProviderServices, BuildContractRegistry
// returns a *pb.ContractRegistry that lists the registered IaC services
// as SERVICE-kind ContractDescriptors. wfctl uses this for capability
// discovery against IaC plugins (per design §Optional services — single
// mechanism, no new server-reflection dependency).
func TestBuildContractRegistry_AdvertisesRegisteredIaCServices(t *testing.T) {
	grpcSrv := grpc.NewServer()
	provider := &iacContractProviderStub{}
	if err := sdk.RegisterAllIaCProviderServices(grpcSrv, provider); err != nil {
		t.Fatalf("register: %v", err)
	}

	registry := sdk.BuildContractRegistry(grpcSrv)
	if registry == nil {
		t.Fatalf("expected non-nil ContractRegistry")
	}

	services := serviceNamesFromRegistry(registry)
	want := []string{
		"workflow.plugin.external.iac.IaCProviderRequired",
		"workflow.plugin.external.iac.IaCProviderEnumerator",
		"workflow.plugin.external.iac.IaCProviderDriftDetector",
	}
	for _, name := range want {
		if !services[name] {
			t.Errorf("ContractRegistry missing service %q; have: %v", name, services)
		}
	}
}

// TestBuildContractRegistry_ServiceContractsUseStrictProtoMode asserts
// that auto-emitted IaC service descriptors carry Mode=STRICT_PROTO so
// the host can distinguish them from the legacy structpb-mode contracts
// produced by Module/Step/Trigger ContractProvider implementations.
func TestBuildContractRegistry_ServiceContractsUseStrictProtoMode(t *testing.T) {
	grpcSrv := grpc.NewServer()
	if err := sdk.RegisterAllIaCProviderServices(grpcSrv, &iacContractProviderStub{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	registry := sdk.BuildContractRegistry(grpcSrv)

	for _, c := range registry.Contracts {
		if c.Kind != pb.ContractKind_CONTRACT_KIND_SERVICE {
			t.Errorf("unexpected non-service contract kind %v for %q", c.Kind, c.ServiceName)
			continue
		}
		if c.Mode != pb.ContractMode_CONTRACT_MODE_STRICT_PROTO {
			t.Errorf("service %q should be STRICT_PROTO mode; got %v", c.ServiceName, c.Mode)
		}
	}
}

// TestBuildContractRegistry_NilServer_ReturnsEmpty asserts the helper is
// safe to call with a nil server (returns an empty registry rather than
// panicking) — defensive contract for callers that may construct the
// helper before the gRPC server exists.
func TestBuildContractRegistry_NilServer_ReturnsEmpty(t *testing.T) {
	registry := sdk.BuildContractRegistry(nil)
	if registry == nil {
		t.Fatalf("expected non-nil empty ContractRegistry")
	}
	if len(registry.Contracts) != 0 {
		t.Fatalf("expected empty contracts; got %d", len(registry.Contracts))
	}
}

func serviceNamesFromRegistry(r *pb.ContractRegistry) map[string]bool {
	out := make(map[string]bool, len(r.Contracts))
	for _, c := range r.Contracts {
		if c.Kind == pb.ContractKind_CONTRACT_KIND_SERVICE {
			out[c.ServiceName] = true
		}
	}
	return out
}

// iacContractProviderStub satisfies Required + Enumerator + DriftDetector
// to exercise the multi-service registration path.
type iacContractProviderStub struct {
	pb.UnimplementedIaCProviderRequiredServer
	pb.UnimplementedIaCProviderEnumeratorServer
	pb.UnimplementedIaCProviderDriftDetectorServer
}
