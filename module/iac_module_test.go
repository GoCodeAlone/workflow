package module

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
)

// configureStateBackendClient is a pb.IaCStateBackendClient stub for the
// IaCModule.Init() Configure-wiring tests: it records the Configure request it
// received and can be told to fail.
type configureStateBackendClient struct {
	gotConfigure *pb.ConfigureRequest
	configureErr error
}

func (c *configureStateBackendClient) Configure(_ context.Context, r *pb.ConfigureRequest, _ ...grpc.CallOption) (*pb.ConfigureResponse, error) {
	c.gotConfigure = r
	if c.configureErr != nil {
		return nil, c.configureErr
	}
	return &pb.ConfigureResponse{}, nil
}
func (*configureStateBackendClient) GetState(context.Context, *pb.GetStateRequest, ...grpc.CallOption) (*pb.GetStateResponse, error) {
	return nil, nil
}
func (*configureStateBackendClient) SaveState(context.Context, *pb.SaveStateRequest, ...grpc.CallOption) (*pb.SaveStateResponse, error) {
	return nil, nil
}
func (*configureStateBackendClient) ListStates(context.Context, *pb.ListStatesRequest, ...grpc.CallOption) (*pb.ListStatesResponse, error) {
	return nil, nil
}
func (*configureStateBackendClient) DeleteState(context.Context, *pb.DeleteStateRequest, ...grpc.CallOption) (*pb.DeleteStateResponse, error) {
	return nil, nil
}
func (*configureStateBackendClient) Lock(context.Context, *pb.LockRequest, ...grpc.CallOption) (*pb.LockResponse, error) {
	return nil, nil
}
func (*configureStateBackendClient) Unlock(context.Context, *pb.UnlockRequest, ...grpc.CallOption) (*pb.UnlockResponse, error) {
	return nil, nil
}
func (*configureStateBackendClient) ListBackendNames(context.Context, *pb.ListBackendNamesRequest, ...grpc.CallOption) (*pb.ListBackendNamesResponse, error) {
	return nil, nil
}

// TestIaCModuleConfigureWiring asserts IaCModule.Init() calls the plugin
// backend's Configure RPC with the backend name and the JSON-encoded module
// config before the store becomes usable.
func TestIaCModuleConfigureWiring(t *testing.T) {
	const backend = "azure_blob_configure_wiring_test"
	fake := &configureStateBackendClient{}
	if err := iacStateBackendRegistryInstance.register(backend, fake); err != nil {
		t.Fatalf("register: %v", err)
	}
	defer func() {
		iacStateBackendRegistryInstance.mu.Lock()
		delete(iacStateBackendRegistryInstance.clients, backend)
		iacStateBackendRegistryInstance.mu.Unlock()
	}()

	cfg := map[string]any{"backend": backend, "container": "tfstate", "account": "wf"}
	m := NewIaCModule("iac-plugin", cfg)
	if err := m.Init(NewMockApplication()); err != nil {
		t.Fatalf("Init: %v", err)
	}

	if fake.gotConfigure == nil {
		t.Fatal("Init did not call the backend's Configure RPC")
	}
	if fake.gotConfigure.BackendName != backend {
		t.Fatalf("Configure BackendName = %q, want %q", fake.gotConfigure.BackendName, backend)
	}
	got, err := jsonBytesToMap(fake.gotConfigure.ConfigJson)
	if err != nil {
		t.Fatalf("Configure ConfigJson not valid JSON: %v", err)
	}
	if !reflect.DeepEqual(got, cfg) {
		t.Fatalf("Configure ConfigJson = %+v, want %+v", got, cfg)
	}
	if _, ok := m.store.(*grpcIaCStateStore); !ok {
		t.Fatalf("m.store is %T, want *grpcIaCStateStore", m.store)
	}
}

// TestIaCModuleConfigureError asserts a Configure RPC failure aborts Init() with
// a wrapped error naming the module and the backend.
func TestIaCModuleConfigureError(t *testing.T) {
	const backend = "azure_blob_configure_error_test"
	sentinel := errors.New("plugin rejected config")
	fake := &configureStateBackendClient{configureErr: sentinel}
	if err := iacStateBackendRegistryInstance.register(backend, fake); err != nil {
		t.Fatalf("register: %v", err)
	}
	defer func() {
		iacStateBackendRegistryInstance.mu.Lock()
		delete(iacStateBackendRegistryInstance.clients, backend)
		iacStateBackendRegistryInstance.mu.Unlock()
	}()

	m := NewIaCModule("iac-plugin", map[string]any{"backend": backend})
	err := m.Init(NewMockApplication())
	if err == nil {
		t.Fatal("Init must fail when the backend's Configure RPC errors")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("Init error must wrap the Configure error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "iac-plugin") || !strings.Contains(err.Error(), backend) {
		t.Fatalf("Init error must name the module and backend, got: %v", err)
	}
	if m.store != nil {
		t.Fatalf("m.store must stay nil when Configure fails, got %T", m.store)
	}
}
