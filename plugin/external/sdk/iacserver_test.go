package sdk_test

import (
	"context"
	"net"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"

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

// TestRegisterAll_RegistersIaCStateBackend asserts that a provider whose type
// also satisfies pb.IaCStateBackendServer gets the IaCStateBackend service
// auto-registered — exactly like the IaCProvider* optionals. Amendment A2
// (decisions/0035).
func TestRegisterAll_RegistersIaCStateBackend(t *testing.T) {
	grpcSrv := grpc.NewServer()
	provider := &stateBackendProviderStub{}
	if err := sdk.RegisterAllIaCProviderServices(grpcSrv, provider); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	info := grpcSrv.GetServiceInfo()
	if _, ok := info["workflow.plugin.external.iac.IaCStateBackend"]; !ok {
		t.Fatalf("IaCStateBackend service NOT registered despite provider satisfying interface; have: %v", serviceNames(info))
	}
}

// stateBackendProviderStub satisfies IaCProviderRequired (the required minimum
// for ServeIaCPlugin) AND IaCStateBackend — representative of an IaC plugin
// whose provider type also serves state storage.
type stateBackendProviderStub struct {
	pb.UnimplementedIaCProviderRequiredServer
	pb.UnimplementedIaCStateBackendServer
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

// TestRegisterAllIaCProviderServices_PluginServiceBridgeRegistered asserts
// that after calling RegisterAllIaCProviderServices, the server also exposes
// "workflow.plugin.v1.PluginService" so the wfctl host can call
// GetContractRegistry without getting "unknown service". This is the fix for
// the DO plugin v1.0.0 incompatibility where ServeIaCPlugin (which calls
// RegisterAllIaCProviderServices) didn't register PluginService, causing
// wfctl's NewExternalPluginAdapter to fail.
func TestRegisterAllIaCProviderServices_PluginServiceBridgeRegistered(t *testing.T) {
	grpcSrv := grpc.NewServer()
	if err := sdk.RegisterAllIaCProviderServices(grpcSrv, &fullProviderStub{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := grpcSrv.GetServiceInfo()["workflow.plugin.v1.PluginService"]; !ok {
		t.Fatalf("PluginService bridge not registered; have: %v", serviceNames(grpcSrv.GetServiceInfo()))
	}
}

// TestRegisterAllIaCProviderServices_PluginServiceBridgeAnswersGetContractRegistry
// verifies the bridge returns a ContractRegistry containing the registered
// IaC services when GetContractRegistry is called via a live gRPC client.
// This exercises the end-to-end path that wfctl's NewExternalPluginAdapter
// takes when loading a DO v1.0.0-style plugin via discoverAndLoadIaCProvider.
func TestRegisterAllIaCProviderServices_PluginServiceBridgeAnswersGetContractRegistry(t *testing.T) {
	t.Parallel()

	// Spin up an in-process gRPC server with the IaC services + bridge.
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	grpcSrv := grpc.NewServer()
	if err := sdk.RegisterAllIaCProviderServices(grpcSrv, &allCapabilitiesStub{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	go func() { _ = grpcSrv.Serve(lis) }()
	t.Cleanup(func() { grpcSrv.Stop() })

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	// Call GetContractRegistry via the PluginServiceClient — exactly what
	// wfctl's NewExternalPluginAdapter does via pb.NewPluginServiceClient.
	client := pb.NewPluginServiceClient(conn)
	registry, err := client.GetContractRegistry(context.Background(), &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetContractRegistry: %v — PluginService bridge did not answer (DO v1.0.0 incompatibility fix is broken)", err)
	}

	services := map[string]bool{}
	for _, c := range registry.GetContracts() {
		if c.GetKind() == pb.ContractKind_CONTRACT_KIND_SERVICE {
			services[c.GetServiceName()] = true
		}
	}

	// The IaCProviderRequired service MUST appear — this is what wfctl's
	// buildTypedIaCAdapterFrom checks via registeredIaCServices().
	if !services["workflow.plugin.external.iac.IaCProviderRequired"] {
		t.Errorf("GetContractRegistry did not include IaCProviderRequired; got services: %v", services)
	}
}

// TestRegisterAllIaCProviderServices_PluginServiceAlreadyRegistered_NoPanic
// asserts that calling RegisterAllIaCProviderServices on a server that already
// has PluginService registered (e.g. a mixed plugin using both sdk.Serve and
// RegisterAllIaCProviderServices) does NOT panic from double-registration.
func TestRegisterAllIaCProviderServices_PluginServiceAlreadyRegistered_NoPanic(t *testing.T) {
	grpcSrv := grpc.NewServer()
	// Pre-register PluginService (simulates a mixed sdk.Serve + IaC plugin).
	// Use an embedded-by-value stub so the pattern is idiomatic Go and not
	// a pointer-to-unimplemented (which the generated gRPC code warns against).
	type minimalPluginSvc struct {
		pb.UnimplementedPluginServiceServer
	}
	pb.RegisterPluginServiceServer(grpcSrv, &minimalPluginSvc{})
	// RegisterAllIaCProviderServices must not panic on double-registration.
	if err := sdk.RegisterAllIaCProviderServices(grpcSrv, &fullProviderStub{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
