package sdk_test

import (
	"strings"
	"testing"

	"google.golang.org/grpc"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

// TestRegisterAllIaCProviderServices_RequiredSatisfied_RegistersRequired
// asserts that a provider satisfying IaCProviderRequiredServer succeeds and
// the gRPC server actually advertises the typed service.
func TestRegisterAllIaCProviderServices_RequiredSatisfied_RegistersRequired(t *testing.T) {
	grpcSrv := grpc.NewServer()
	provider := &fullProviderStub{}
	if err := sdk.RegisterAllIaCProviderServices(grpcSrv, provider); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	info := grpcSrv.GetServiceInfo()
	if _, ok := info["workflow.plugin.external.iac.IaCProviderRequired"]; !ok {
		t.Fatalf("required service not registered; have services: %v", serviceNames(info))
	}
}

// TestRegisterAllIaCProviderServices_OptionalSatisfied_RegistersOptional
// asserts auto-detection: a provider that satisfies the Enumerator interface
// (and only that optional) gets the Enumerator service registered, but other
// optional services stay absent.
func TestRegisterAllIaCProviderServices_OptionalSatisfied_RegistersOptional(t *testing.T) {
	grpcSrv := grpc.NewServer()
	provider := &enumeratorOnlyStub{}
	if err := sdk.RegisterAllIaCProviderServices(grpcSrv, provider); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	info := grpcSrv.GetServiceInfo()
	if _, ok := info["workflow.plugin.external.iac.IaCProviderEnumerator"]; !ok {
		t.Fatalf("Enumerator optional service NOT registered despite provider satisfying interface; have: %v", serviceNames(info))
	}
	if _, ok := info["workflow.plugin.external.iac.IaCProviderDriftDetector"]; ok {
		t.Fatalf("DriftDetector incorrectly registered (provider doesn't satisfy)")
	}
}

// TestRegisterAllIaCProviderServices_RequiredMissing_ReturnsError
// asserts that an empty provider produces an actionable error naming the
// unsatisfied required interface — the bug-class prevention pivot.
func TestRegisterAllIaCProviderServices_RequiredMissing_ReturnsError(t *testing.T) {
	grpcSrv := grpc.NewServer()
	provider := &emptyStub{} // doesn't satisfy IaCProviderRequiredServer
	err := sdk.RegisterAllIaCProviderServices(grpcSrv, provider)
	if err == nil {
		t.Fatalf("expected error for unsatisfied required interface; got nil")
	}
	if !strings.Contains(err.Error(), "IaCProviderRequiredServer") {
		t.Fatalf("error message must name the unsatisfied interface; got %q", err.Error())
	}
}

// TestRegisterAllIaCProviderServices_TypedNilPointer_ReturnsError
// asserts the typed-nil-pointer hardening: a (*T)(nil) wrapped in an
// `any` interface is non-nil at the interface layer (interface header
// has a type), but dereferences to nil at first method call. Previous
// `provider == nil` check missed it; reflect-based check catches it
// and rejects with a typed error before any registration happens.
//
// Per cycle 4 code-review PR 611 typed-nil hardening (Copilot finding).
func TestRegisterAllIaCProviderServices_TypedNilPointer_ReturnsError(t *testing.T) {
	grpcSrv := grpc.NewServer()
	var provider *fullProviderStub // typed-nil pointer
	err := sdk.RegisterAllIaCProviderServices(grpcSrv, provider)
	if err == nil {
		t.Fatalf("expected error for typed-nil pointer; got nil")
	}
	if !strings.Contains(err.Error(), "typed-nil") {
		t.Errorf("error must name typed-nil; got %q", err.Error())
	}
	if got := len(grpcSrv.GetServiceInfo()); got != 0 {
		t.Errorf("no services should be registered on rejection; got %d", got)
	}
}

// TestRegisterAllIaCProviderServices_AllOptionals_AllRegistered
// asserts that a provider satisfying every optional + required interface
// triggers registration of all 7 typed services (Required + 6 optional)
// plus the ResourceDriver.
func TestRegisterAllIaCProviderServices_AllOptionals_AllRegistered(t *testing.T) {
	grpcSrv := grpc.NewServer()
	provider := &allCapabilitiesStub{}
	if err := sdk.RegisterAllIaCProviderServices(grpcSrv, provider); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	info := grpcSrv.GetServiceInfo()
	wantServices := []string{
		"workflow.plugin.external.iac.IaCProviderRequired",
		"workflow.plugin.external.iac.IaCProviderEnumerator",
		"workflow.plugin.external.iac.IaCProviderDriftDetector",
		"workflow.plugin.external.iac.IaCProviderCredentialRevoker",
		"workflow.plugin.external.iac.IaCProviderMigrationRepairer",
		"workflow.plugin.external.iac.IaCProviderValidator",
		"workflow.plugin.external.iac.IaCProviderDriftConfigDetector",
		"workflow.plugin.external.iac.ResourceDriver",
	}
	for _, name := range wantServices {
		if _, ok := info[name]; !ok {
			t.Errorf("expected service %q registered; have: %v", name, serviceNames(info))
		}
	}
}

func serviceNames(info map[string]grpc.ServiceInfo) []string {
	out := make([]string, 0, len(info))
	for k := range info {
		out = append(out, k)
	}
	return out
}

// fullProviderStub satisfies IaCProviderRequired + Enumerator + DriftDetector
// (representative of an early-stage DO plugin shape).
type fullProviderStub struct {
	pb.UnimplementedIaCProviderRequiredServer
	pb.UnimplementedIaCProviderEnumeratorServer
	pb.UnimplementedIaCProviderDriftDetectorServer
}

// enumeratorOnlyStub satisfies Required + Enumerator only.
type enumeratorOnlyStub struct {
	pb.UnimplementedIaCProviderRequiredServer
	pb.UnimplementedIaCProviderEnumeratorServer
}

// allCapabilitiesStub satisfies every required + optional IaC service plus
// ResourceDriver — used to assert auto-registration covers the full surface.
type allCapabilitiesStub struct {
	pb.UnimplementedIaCProviderRequiredServer
	pb.UnimplementedIaCProviderEnumeratorServer
	pb.UnimplementedIaCProviderDriftDetectorServer
	pb.UnimplementedIaCProviderCredentialRevokerServer
	pb.UnimplementedIaCProviderMigrationRepairerServer
	pb.UnimplementedIaCProviderValidatorServer
	pb.UnimplementedIaCProviderDriftConfigDetectorServer
	pb.UnimplementedResourceDriverServer
}

// emptyStub satisfies no IaC interface; the helper must reject it.
type emptyStub struct{}
