package module

import (
	"context"
	"testing"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
)

// fakeStateBackendClient is a no-op pb.IaCStateBackendClient stub. The registry
// tests only need it to satisfy the interface; no method is ever called.
type fakeStateBackendClient struct{}

func (*fakeStateBackendClient) Configure(context.Context, *pb.ConfigureRequest, ...grpc.CallOption) (*pb.ConfigureResponse, error) {
	return nil, nil
}
func (*fakeStateBackendClient) GetState(context.Context, *pb.GetStateRequest, ...grpc.CallOption) (*pb.GetStateResponse, error) {
	return nil, nil
}
func (*fakeStateBackendClient) SaveState(context.Context, *pb.SaveStateRequest, ...grpc.CallOption) (*pb.SaveStateResponse, error) {
	return nil, nil
}
func (*fakeStateBackendClient) ListStates(context.Context, *pb.ListStatesRequest, ...grpc.CallOption) (*pb.ListStatesResponse, error) {
	return nil, nil
}
func (*fakeStateBackendClient) DeleteState(context.Context, *pb.DeleteStateRequest, ...grpc.CallOption) (*pb.DeleteStateResponse, error) {
	return nil, nil
}
func (*fakeStateBackendClient) Lock(context.Context, *pb.LockRequest, ...grpc.CallOption) (*pb.LockResponse, error) {
	return nil, nil
}
func (*fakeStateBackendClient) Unlock(context.Context, *pb.UnlockRequest, ...grpc.CallOption) (*pb.UnlockResponse, error) {
	return nil, nil
}
func (*fakeStateBackendClient) ListBackendNames(context.Context, *pb.ListBackendNamesRequest, ...grpc.CallOption) (*pb.ListBackendNamesResponse, error) {
	return nil, nil
}

func TestIaCStateBackendRegistry(t *testing.T) {
	reg := newIaCStateBackendRegistry()
	if _, ok := reg.resolve("azure_blob"); ok {
		t.Fatal("empty registry should not resolve azure_blob")
	}
	fake := &fakeStateBackendClient{}
	if err := reg.register("azure_blob", fake); err != nil {
		t.Fatalf("register: %v", err)
	}
	got, ok := reg.resolve("azure_blob")
	if !ok || got != fake {
		t.Fatalf("resolve azure_blob: ok=%v got=%v", ok, got)
	}
	for _, reserved := range []string{"memory", "filesystem", "postgres"} {
		if err := reg.register(reserved, fake); err == nil {
			t.Fatalf("register(%q) must fail — reserved core backend name", reserved)
		}
	}
	// Empty / whitespace-only name must be rejected.
	for _, bad := range []string{"", "   "} {
		if err := reg.register(bad, fake); err == nil {
			t.Fatalf("register(%q) must fail — empty backend name", bad)
		}
	}
	// Nil client must be rejected.
	if err := reg.register("nilclient_backend", nil); err == nil {
		t.Fatal("register with nil client must fail")
	}
	// A name surrounded by whitespace is trimmed and registers under the trimmed key.
	if err := reg.register("  spaced_backend  ", fake); err != nil {
		t.Fatalf("register trimmed name: %v", err)
	}
	if _, ok := reg.resolve("spaced_backend"); !ok {
		t.Fatal("trimmed name must resolve under its trimmed key")
	}
}

// TestRegisterIaCStateBackend exercises the exported wrapper the engine calls at
// plugin-load: a non-reserved name registers into the package-level singleton and
// resolves; a reserved core name is rejected.
func TestRegisterIaCStateBackend(t *testing.T) {
	const backend = "azure_blob_wrapper_test"
	fake := &fakeStateBackendClient{}
	if err := RegisterIaCStateBackend(backend, fake); err != nil {
		t.Fatalf("RegisterIaCStateBackend(%q): %v", backend, err)
	}
	defer func() {
		iacStateBackendRegistryInstance.mu.Lock()
		delete(iacStateBackendRegistryInstance.clients, backend)
		iacStateBackendRegistryInstance.mu.Unlock()
	}()
	if got, ok := iacStateBackendRegistryInstance.resolve(backend); !ok || got != fake {
		t.Fatalf("resolve(%q): ok=%v got=%v", backend, ok, got)
	}
	if err := RegisterIaCStateBackend("memory", fake); err == nil {
		t.Fatal("RegisterIaCStateBackend(\"memory\") must fail — reserved core backend name")
	}
}

// TestIaCModule_PluginBackendDispatch exercises the real IaCModule.Init() path:
// a backend name no in-process switch case matches is resolved from the
// package-level iacStateBackendRegistryInstance, yielding a *grpcIaCStateStore.
func TestIaCModule_PluginBackendDispatch(t *testing.T) {
	const backend = "azure_blob_test_only"
	fake := &fakeStateBackendClient{}
	if err := iacStateBackendRegistryInstance.register(backend, fake); err != nil {
		t.Fatalf("register: %v", err)
	}
	defer func() {
		iacStateBackendRegistryInstance.mu.Lock()
		delete(iacStateBackendRegistryInstance.clients, backend)
		iacStateBackendRegistryInstance.mu.Unlock()
	}()

	m := NewIaCModule("iac-plugin", map[string]any{"backend": backend})
	if err := m.Init(NewMockApplication()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, ok := m.store.(*grpcIaCStateStore); !ok {
		t.Fatalf("m.store is %T, want *grpcIaCStateStore", m.store)
	}
}
