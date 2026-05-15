package sdk

// End-to-end integration test (internal package sdk) — exercises the IaC
// bridge through a real pb.PluginServiceClient over bufconn, the canonical
// runtime-launch-validation evidence for the workflow-side plumbing path per
// decisions/0038. The engine's ExternalPluginAdapter.ModuleFactories() calls
// GetModuleTypes + CreateModule on this exact client interface — bufconn uses
// the same gRPC dispatch the production engine does. Subprocess-handshake
// coverage lives in plan-2 Tasks 7+11 (plugin repos) against real binaries.

import (
	"context"
	"testing"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
)

// TestEndToEnd_IaCBridge_EngineAdapterSeesModules spins up an IaC bridge with
// IaCServeOptions.Modules wired, dials it via the standard
// pb.PluginServiceClient over bufconn, and exercises the GetModuleTypes +
// CreateModule pair the engine adapter calls — proving the engine sees the
// modules without any engine-side change. Locks the design's "engine-side:
// zero change" claim.
func TestEndToEnd_IaCBridge_EngineAdapterSeesModules(t *testing.T) {
	fakeMod := &fakeModuleProvider{
		types:    []string{"storage.test"},
		instance: &fakeModuleInstance{},
	}
	opts := IaCServeOptions{
		Modules: map[string]ModuleProvider{"storage.test": fakeMod},
	}
	s := grpc.NewServer()
	if err := registerAllIaCProviderServicesWithOpts(s, &fakeIaCRequiredProvider{}, opts); err != nil {
		t.Fatalf("register: %v", err)
	}
	client := dialBridge(t, s)
	ctx := context.Background()

	// GetModuleTypes — the adapter's first call when populating ModuleFactories.
	types, err := client.GetModuleTypes(ctx, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetModuleTypes: %v", err)
	}
	if len(types.GetTypes()) != 1 || types.GetTypes()[0] != "storage.test" {
		t.Fatalf("GetModuleTypes = %v, want [storage.test]", types.GetTypes())
	}

	// CreateModule — the adapter's second call (per-instance) — exercises the
	// full module-creation handshake the engine's RemoteModule path triggers.
	cfg, err := structpb.NewStruct(map[string]any{"k": "v"})
	if err != nil {
		t.Fatalf("structpb.NewStruct: %v", err)
	}
	resp, err := client.CreateModule(ctx, &pb.CreateModuleRequest{
		Type:   "storage.test",
		Name:   "n",
		Config: cfg,
	})
	if err != nil {
		t.Fatalf("CreateModule: %v", err)
	}
	if resp.GetError() != "" {
		t.Fatalf("CreateModule plugin-side error: %s", resp.GetError())
	}
	if resp.GetHandleId() == "" {
		t.Fatal("CreateModule must return a non-empty HandleId — the engine's RemoteModule keys all subsequent lifecycle RPCs by it")
	}
}
