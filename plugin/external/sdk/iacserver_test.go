package sdk_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"maps"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	goplugin "github.com/GoCodeAlone/go-plugin"
	"github.com/GoCodeAlone/workflow/iac/providerclient"
	"github.com/GoCodeAlone/workflow/iac/wfctlhelpers"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/plugin/external/contract"
	"github.com/hashicorp/go-hclog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"github.com/GoCodeAlone/workflow/plugin/external/sdk"
)

type updateLegacyProvider struct {
	fullProviderStub
	pb.UnimplementedResourceDriverServer
	legacyCalls int
}

func (p *updateLegacyProvider) Update(_ context.Context, req *pb.ResourceUpdateRequest) (*pb.ResourceUpdateResponse, error) {
	p.legacyCalls++
	return &pb.ResourceUpdateResponse{Output: &pb.ResourceOutput{Name: req.GetRef().GetName(), Type: req.GetRef().GetType(), ProviderId: req.GetRef().GetProviderId()}}, nil
}

type updateStateProvider struct {
	updateLegacyProvider
	stateCalls int
	prior      *interfaces.ResourceState
	process    bool
}

func (p *updateStateProvider) UpdateWithState(ctx context.Context, ref interfaces.ResourceRef, spec interfaces.ResourceSpec, prior *interfaces.ResourceState) (*interfaces.ResourceOutput, error) {
	if spec.Type == "infra.legacy" && prior == nil {
		response, err := p.Update(ctx, &pb.ResourceUpdateRequest{Ref: &pb.ResourceRef{Name: ref.Name, Type: ref.Type, ProviderId: ref.ProviderID}})
		if err != nil {
			return nil, err
		}
		return &interfaces.ResourceOutput{Name: response.Output.Name, Type: response.Output.Type, ProviderID: response.Output.ProviderId}, nil
	}
	if err := interfaces.ValidateUpdatePriorState(ref, spec, prior); err != nil {
		return nil, err
	}
	p.stateCalls++
	p.prior = prior
	outputs := prior.Outputs
	if p.process {
		outputs = maps.Clone(prior.Outputs)
		outputs["fixture_server_pid"] = os.Getpid()
		outputs["fixture_state_calls"] = p.stateCalls
		outputs["fixture_legacy_calls"] = p.legacyCalls
	}
	return &interfaces.ResourceOutput{Name: spec.Name, Type: spec.Type, ProviderID: ref.ProviderID, Outputs: outputs}, nil
}

func startUpdateProvider(t *testing.T, provider any) *grpc.ClientConn {
	t.Helper()
	srv := grpc.NewServer()
	if err := sdk.RegisterAllIaCProviderServices(srv, provider); err != nil {
		t.Fatal(err)
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	t.Cleanup(func() { _ = lis.Close() })
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestSDK_UpdateWithState_RoundTripAndLegacy(t *testing.T) {
	provider := &updateStateProvider{}
	conn := startUpdateProvider(t, provider)
	adapter := providerclient.New(conn, map[string]bool{providerclient.IaCServiceResourceDriver: true})
	driver, err := adapter.ResourceDriver("infra.fixture")
	if err != nil {
		t.Fatal(err)
	}
	prior := &interfaces.ResourceState{ID: "fixture", Name: "fixture", Type: "infra.fixture", Provider: "fixture-provider", ProviderRef: "fixture-instance", ProviderID: "fixture-id", ConfigHash: "fixture-hash", AppliedConfigSource: "apply", AppliedConfig: map[string]any{"credential_ref": "secrets://fixture/credential"}, Outputs: map[string]any{"rotation_generation": float64(9)}}
	ref := interfaces.ResourceRef{Name: prior.Name, Type: prior.Type, ProviderID: prior.ProviderID}
	spec := interfaces.ResourceSpec{Name: prior.Name, Type: prior.Type}
	updater, ok := driver.(interfaces.ResourceStateUpdater)
	if !ok {
		t.Fatal("client omitted state-aware update")
	}
	out, err := updater.UpdateWithState(t.Context(), ref, spec, prior)
	if err != nil || out == nil || provider.stateCalls != 1 || provider.legacyCalls != 0 || !reflect.DeepEqual(provider.prior, prior) {
		t.Fatalf("SDK failed state-aware update round trip: err=%v state_calls=%d legacy_calls=%d", err, provider.stateCalls, provider.legacyCalls)
	}
	legacy := &updateLegacyProvider{}
	legacyClient := pb.NewResourceDriverClient(startUpdateProvider(t, legacy))
	_, err = legacyClient.Update(t.Context(), &pb.ResourceUpdateRequest{ResourceType: ref.Type, Ref: &pb.ResourceRef{Name: ref.Name, Type: ref.Type, ProviderId: ref.ProviderID}, Spec: &pb.ResourceSpec{Name: spec.Name, Type: spec.Type}})
	if err != nil || legacy.legacyCalls != 1 {
		t.Fatal("legacy Update RPC lost backward compatibility")
	}
	_, err = pb.NewResourceDriverClient(conn).Update(t.Context(), &pb.ResourceUpdateRequest{ResourceType: "infra.legacy", Ref: &pb.ResourceRef{Name: "legacy", Type: "infra.legacy", ProviderId: "legacy-id"}, Spec: &pb.ResourceSpec{Name: "legacy", Type: "infra.legacy"}})
	if err != nil || provider.legacyCalls != 1 || provider.stateCalls != 1 {
		t.Fatal("non-state-dependent legacy Update failed on a mixed state-aware provider")
	}
}

func TestSDK_UpdateWithState_ApplyWithoutLookupPreservesLegacy(t *testing.T) {
	for _, name := range []string{"legacy provider", "mixed legacy resource", "state-dependent resource"} {
		t.Run(name, func(t *testing.T) {
			legacy := &updateLegacyProvider{}
			mixed := &updateStateProvider{}
			var service any = legacy
			resourceType := "infra.legacy"
			if name != "legacy provider" {
				service = mixed
			}
			if name == "state-dependent resource" {
				resourceType = "infra.fixture"
			}
			provider := providerclient.New(startUpdateProvider(t, service), map[string]bool{
				providerclient.IaCServiceResourceDriver: true,
			})
			state := interfaces.ResourceState{Name: "fixture", Type: resourceType, ProviderID: "fixture-id"}
			plan := &interfaces.IaCPlan{Actions: []interfaces.PlanAction{{
				Action:   "update",
				Resource: interfaces.ResourceSpec{Name: state.Name, Type: state.Type},
				Current:  &state,
			}}}
			result, err := wfctlhelpers.ApplyPlanWithHooks(t.Context(), provider, plan, wfctlhelpers.ApplyPlanHooks{})
			if err != nil || result == nil {
				t.Fatalf("apply without new hook: err=%v result=%v", err, result)
			}
			if name == "state-dependent resource" {
				if len(result.Errors) != 1 || mixed.stateCalls != 0 || mixed.legacyCalls != 0 {
					t.Fatal("state-dependent update without persisted state reached mutation")
				}
				return
			}
			calls := legacy.legacyCalls + mixed.legacyCalls
			if len(result.Errors) != 0 || calls != 1 || mixed.stateCalls != 0 {
				t.Fatalf("legacy update requires new lookup: errors=%v legacy_calls=%d native_calls=%d", result.Errors, calls, mixed.stateCalls)
			}
		})
	}
}

func TestSDK_UpdateWithState_InvalidPriorNeverMutates(t *testing.T) {
	outputs, _ := json.Marshal(map[string]any{"rotation_generation": 9})
	valid := &pb.ResourceUpdateRequest{ResourceType: "infra.fixture", Ref: &pb.ResourceRef{Name: "fixture", Type: "infra.fixture", ProviderId: "fixture-id"}, Spec: &pb.ResourceSpec{Name: "fixture", Type: "infra.fixture"}, PriorState: &pb.ResourceState{Name: "fixture", Type: "infra.fixture", ProviderId: "fixture-id", OutputsJson: outputs}}
	for _, name := range []string{"missing", "wrong_name", "wrong_type", "wrong_provider_id", "wrong_route_type", "invalid_outputs", "invalid_config"} {
		t.Run(name, func(t *testing.T) {
			provider := &updateStateProvider{}
			client := pb.NewResourceDriverClient(startUpdateProvider(t, provider))
			req := proto.Clone(valid).(*pb.ResourceUpdateRequest)
			switch name {
			case "missing":
				req.PriorState = nil
			case "wrong_name":
				req.PriorState.Name = "other"
			case "wrong_type":
				req.PriorState.Type = "infra.other"
			case "wrong_provider_id":
				req.PriorState.ProviderId = "other-id"
			case "wrong_route_type":
				req.ResourceType = "infra.other"
			case "invalid_outputs":
				req.PriorState.OutputsJson = []byte(`{"secret":"sentinel-credential" broken}`)
			case "invalid_config":
				req.PriorState.AppliedConfigJson = []byte(`{"secret":"sentinel-credential" broken}`)
			}
			_, err := client.Update(t.Context(), req)
			if status.Code(err) != codes.InvalidArgument || provider.stateCalls != 0 || provider.legacyCalls != 0 || strings.Contains(err.Error(), "sentinel-credential") {
				t.Fatalf("invalid prior crossed mutation boundary: code=%v state_calls=%d legacy_calls=%d", status.Code(err), provider.stateCalls, provider.legacyCalls)
			}
		})
	}
}

type updateConsumerPlugin struct{}

func (*updateConsumerPlugin) GRPCServer(*goplugin.GRPCBroker, *grpc.Server) error {
	return fmt.Errorf("consumer fixture is host-only")
}

func (*updateConsumerPlugin) GRPCClient(_ context.Context, _ *goplugin.GRPCBroker, conn *grpc.ClientConn) (any, error) {
	return providerclient.New(conn, map[string]bool{providerclient.IaCServiceResourceDriver: true}), nil
}

// The child serves the real SDK; the parent uses only the public providerclient
// and shared apply APIs against reopened filesystem state and a stale plan.
func TestSDK_UpdateWithState_ExternalConsumerRestart(t *testing.T) {
	if os.Getenv("WORKFLOW_IAC_UPDATE_TEST_PROCESS") == "1" {
		sdk.ServeIaCPlugin(&updateStateProvider{process: true}, sdk.IaCServeOptions{})
		return
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("SDK consumer executable sha256=%x", sha256.Sum256(data))
	dir := t.TempDir()
	configFile := filepath.Join(dir, "state.yaml")
	stateDir := filepath.Join(dir, "state")
	config := fmt.Sprintf("modules:\n  - name: state\n    type: iac.state\n    config:\n      backend: filesystem\n      directory: %q\n", stateDir)
	if err := os.WriteFile(configFile, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	stale := interfaces.ResourceState{ID: "fixture", Name: "fixture", Type: "infra.fixture", Provider: "fixture-provider", ProviderID: "fixture-id", Outputs: map[string]any{"rotation_generation": float64(1), "credential_ref": "secrets://fixture/credential"}}
	savedPlan, err := json.Marshal(interfaces.IaCPlan{Actions: []interfaces.PlanAction{{Action: "update", Resource: interfaces.ResourceSpec{Name: stale.Name, Type: stale.Type}, Current: &stale}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, generation := range []float64{7, 9} {
		store, err := wfctlhelpers.ResolveStateStore(configFile, "", "")
		if err != nil {
			t.Fatal(err)
		}
		latest := stale
		latest.Outputs = maps.Clone(stale.Outputs)
		latest.Outputs["rotation_generation"] = generation
		if err := store.SaveResource(ctx, latest); err != nil {
			t.Fatal(err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		restarted, err := wfctlhelpers.ResolveStateStore(configFile, "", "")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = restarted.Close() })
		cmd := exec.CommandContext(ctx, binary, "-test.run=^TestSDK_UpdateWithState_ExternalConsumerRestart$")
		cmd.Dir = dir
		cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "TMPDIR=" + dir, "WORKFLOW_IAC_UPDATE_TEST_PROCESS=1"}
		client := goplugin.NewClient(&goplugin.ClientConfig{HandshakeConfig: contract.Handshake, Plugins: goplugin.PluginSet{"iac": &updateConsumerPlugin{}}, Cmd: cmd, StartTimeout: 10 * time.Second, Logger: hclog.NewNullLogger()})
		t.Cleanup(client.Kill)
		protocol, err := client.Client()
		if err != nil {
			t.Fatal(err)
		}
		consumer, err := protocol.Dispense("iac")
		if err != nil {
			t.Fatal(err)
		}
		provider, ok := consumer.(*providerclient.Adapter)
		if !ok {
			t.Fatal("SDK plugin did not expose the public providerclient adapter")
		}
		var plan interfaces.IaCPlan
		if err := json.Unmarshal(savedPlan, &plan); err != nil {
			t.Fatal(err)
		}
		result, err := wfctlhelpers.ApplyPlanWithHooks(ctx, provider, &plan, wfctlhelpers.ApplyPlanHooks{LookupCurrentState: func(ctx context.Context, ref interfaces.ResourceRef) (*interfaces.ResourceState, error) {
			return restarted.GetResource(ctx, ref.Name)
		}})
		if err != nil || result == nil || len(result.Errors) != 0 || len(result.Resources) != 1 {
			t.Fatalf("separate SDK process failed saved-plan dispatch: err=%v result=%v", err, result)
		}
		outputs := result.Resources[0].Outputs
		if outputs["rotation_generation"] != generation || outputs["credential_ref"] != "secrets://fixture/credential" || outputs["fixture_server_pid"] != float64(cmd.Process.Pid) || outputs["fixture_state_calls"] != float64(1) || outputs["fixture_legacy_calls"] != float64(0) {
			t.Fatal("separate SDK process did not receive latest persisted state on its exact update call")
		}
		client.Kill()
		if !client.Exited() {
			t.Fatal("owned SDK plugin process did not terminate")
		}
		t.Logf("SDK child pid=%d received generation=%.0f native_updates=1 legacy_updates=0; terminated=true", cmd.Process.Pid, generation)
	}
	state, err := os.ReadFile(filepath.Join(stateDir, "fixture.json"))
	if err != nil || !strings.Contains(string(state), "secrets://fixture/credential") || strings.Contains(string(state), "sentinel-credential") {
		t.Fatal("fixture state lost credential-reference-only persistence")
	}
}

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
// triggers registration of all 8 typed services (Required + 7 optional)
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
		"workflow.plugin.external.iac.IaCProviderOwnership",
		"workflow.plugin.external.iac.IaCProviderMigrationRepairer",
		"workflow.plugin.external.iac.IaCProviderValidator",
		"workflow.plugin.external.iac.IaCProviderDriftConfigDetector",
		"workflow.plugin.external.iac.IaCProviderFinalizer",
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

// TestRegisterAll_RegistersIaCProviderFinalizer asserts that a provider whose
// type also satisfies pb.IaCProviderFinalizerServer gets the
// IaCProviderFinalizer service auto-registered — same opt-in posture as the
// other IaCProvider* optionals. Per workflow#695 Phase 2.5 / ADR 0024
// (absence of registration IS the negative signal; no compat shim).
func TestRegisterAll_RegistersIaCProviderFinalizer(t *testing.T) {
	grpcSrv := grpc.NewServer()
	provider := &finalizerProviderStub{}
	if err := sdk.RegisterAllIaCProviderServices(grpcSrv, provider); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	info := grpcSrv.GetServiceInfo()
	if _, ok := info["workflow.plugin.external.iac.IaCProviderFinalizer"]; !ok {
		t.Fatalf("IaCProviderFinalizer service NOT registered despite provider satisfying interface; have: %v", serviceNames(info))
	}
}

func TestRegisterAll_RegistersIaCRequirementDiscovery(t *testing.T) {
	grpcSrv := grpc.NewServer()
	provider := &requirementDiscoveryProviderStub{}
	if err := sdk.RegisterAllIaCProviderServices(grpcSrv, provider); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	info := grpcSrv.GetServiceInfo()
	if _, ok := info[pb.IaCRequirementDiscovery_ServiceDesc.ServiceName]; !ok {
		t.Fatalf("IaCRequirementDiscovery service NOT registered despite provider satisfying interface; have: %v", serviceNames(info))
	}
}

func TestRegisterAll_RegistersIaCProviderRequirementMapper(t *testing.T) {
	grpcSrv := grpc.NewServer()
	provider := &requirementMapperProviderStub{}
	if err := sdk.RegisterAllIaCProviderServices(grpcSrv, provider); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	info := grpcSrv.GetServiceInfo()
	if _, ok := info[pb.IaCProviderRequirementMapper_ServiceDesc.ServiceName]; !ok {
		t.Fatalf("IaCProviderRequirementMapper service NOT registered despite provider satisfying interface; have: %v", serviceNames(info))
	}
}

func TestRegisterAll_RegistersIaCProviderOwnership(t *testing.T) {
	grpcSrv := grpc.NewServer()
	provider := &ownershipProviderStub{}
	if err := sdk.RegisterAllIaCProviderServices(grpcSrv, provider); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	info := grpcSrv.GetServiceInfo()
	if _, ok := info[pb.IaCProviderOwnership_ServiceDesc.ServiceName]; !ok {
		t.Fatalf("IaCProviderOwnership service NOT registered despite provider satisfying interface; have: %v", serviceNames(info))
	}
}

// TestRegisterAll_SkipsIaCProviderFinalizerWhenNotImplemented locks the
// negative signal contract: a provider that does NOT satisfy
// pb.IaCProviderFinalizerServer MUST NOT have the service registered.
// Per ADR 0024 + ADR 0040 invariant on optional services.
func TestRegisterAll_SkipsIaCProviderFinalizerWhenNotImplemented(t *testing.T) {
	grpcSrv := grpc.NewServer()
	provider := &fullProviderStub{} // no finalizer embed
	if err := sdk.RegisterAllIaCProviderServices(grpcSrv, provider); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	info := grpcSrv.GetServiceInfo()
	if _, ok := info["workflow.plugin.external.iac.IaCProviderFinalizer"]; ok {
		t.Fatalf("IaCProviderFinalizer service WAS registered despite provider not satisfying interface; have: %v", serviceNames(info))
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

// finalizerProviderStub satisfies IaCProviderRequired (the required minimum
// for ServeIaCPlugin) AND IaCProviderFinalizer — representative of the DO
// plugin shape under workflow#695 Phase 2.5.
type finalizerProviderStub struct {
	pb.UnimplementedIaCProviderRequiredServer
	pb.UnimplementedIaCProviderFinalizerServer
}

type requirementDiscoveryProviderStub struct {
	pb.UnimplementedIaCProviderRequiredServer
	pb.UnimplementedIaCRequirementDiscoveryServer
}

type requirementMapperProviderStub struct {
	pb.UnimplementedIaCProviderRequiredServer
	pb.UnimplementedIaCProviderRequirementMapperServer
}

type ownershipProviderStub struct {
	pb.UnimplementedIaCProviderRequiredServer
	pb.UnimplementedIaCProviderOwnershipServer
}

// allCapabilitiesStub satisfies every required + optional IaC service plus
// ResourceDriver — used to assert auto-registration covers the full surface.
type allCapabilitiesStub struct {
	pb.UnimplementedIaCProviderRequiredServer
	pb.UnimplementedIaCProviderEnumeratorServer
	pb.UnimplementedIaCProviderDriftDetectorServer
	pb.UnimplementedIaCProviderCredentialRevokerServer
	pb.UnimplementedIaCProviderOwnershipServer
	pb.UnimplementedIaCProviderMigrationRepairerServer
	pb.UnimplementedIaCProviderValidatorServer
	pb.UnimplementedIaCProviderDriftConfigDetectorServer
	pb.UnimplementedIaCProviderFinalizerServer
	pb.UnimplementedIaCRequirementDiscoveryServer
	pb.UnimplementedIaCProviderRequirementMapperServer
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
