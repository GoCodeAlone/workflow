package module

import (
	"context"
	"net"
	"testing"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// captureStateBackendClient is a pb.IaCStateBackendClient stub that records the
// last Configure request it received. Only Configure is exercised; the other
// methods exist solely to satisfy the interface.
type captureStateBackendClient struct {
	gotConfigure *pb.ConfigureRequest
}

func (*captureStateBackendClient) GetState(context.Context, *pb.GetStateRequest, ...grpc.CallOption) (*pb.GetStateResponse, error) {
	return nil, nil
}
func (*captureStateBackendClient) SaveState(context.Context, *pb.SaveStateRequest, ...grpc.CallOption) (*pb.SaveStateResponse, error) {
	return nil, nil
}
func (*captureStateBackendClient) ListStates(context.Context, *pb.ListStatesRequest, ...grpc.CallOption) (*pb.ListStatesResponse, error) {
	return nil, nil
}
func (*captureStateBackendClient) DeleteState(context.Context, *pb.DeleteStateRequest, ...grpc.CallOption) (*pb.DeleteStateResponse, error) {
	return nil, nil
}
func (*captureStateBackendClient) Lock(context.Context, *pb.LockRequest, ...grpc.CallOption) (*pb.LockResponse, error) {
	return nil, nil
}
func (*captureStateBackendClient) Unlock(context.Context, *pb.UnlockRequest, ...grpc.CallOption) (*pb.UnlockResponse, error) {
	return nil, nil
}
func (*captureStateBackendClient) ListBackendNames(context.Context, *pb.ListBackendNamesRequest, ...grpc.CallOption) (*pb.ListBackendNamesResponse, error) {
	return nil, nil
}
func (c *captureStateBackendClient) Configure(_ context.Context, r *pb.ConfigureRequest, _ ...grpc.CallOption) (*pb.ConfigureResponse, error) {
	c.gotConfigure = r
	return &pb.ConfigureResponse{}, nil
}

func TestGRPCIaCStateStoreConfigure(t *testing.T) {
	fake := &captureStateBackendClient{}
	store := newGRPCIaCStateStore(fake)

	cfg := map[string]any{"container": "tfstate", "account": "wf"}
	if err := store.Configure(context.Background(), "azure_blob", cfg); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if fake.gotConfigure == nil {
		t.Fatal("Configure did not call the client")
	}
	if fake.gotConfigure.BackendName != "azure_blob" {
		t.Fatalf("BackendName = %q, want azure_blob", fake.gotConfigure.BackendName)
	}
	got, err := jsonBytesToMap(fake.gotConfigure.ConfigJson)
	if err != nil {
		t.Fatalf("ConfigJson not valid JSON: %v", err)
	}
	if got["container"] != "tfstate" || got["account"] != "wf" {
		t.Fatalf("ConfigJson round-trip mismatch: %+v", got)
	}
}

func TestGRPCIaCStateStoreRoundTrip(t *testing.T) {
	lis := bufconn.Listen(4 << 20)
	t.Cleanup(func() { _ = lis.Close() })
	srv := grpc.NewServer()
	pb.RegisterIaCStateBackendServer(srv, &iacStateBackendServer{store: NewMemoryIaCStateStore()})
	go func() { _ = srv.Serve(lis) }()
	defer srv.Stop()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	var store IaCStateStore = newGRPCIaCStateStore(pb.NewIaCStateBackendClient(conn))
	ctx := context.Background()

	want := &IaCState{ResourceID: "r1", ResourceType: "kubernetes", Provider: "azure", Status: "active",
		Outputs: map[string]any{"endpoint": "https://x"}, Config: map[string]any{"size": "L"}}
	if err := store.SaveState(ctx, want); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	got, err := store.GetState(ctx, "r1")
	if err != nil || got == nil {
		t.Fatalf("GetState: %v (got=%v)", err, got)
	}
	if got.ResourceID != "r1" || got.Status != "active" || got.Outputs["endpoint"] != "https://x" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if err := store.Lock(ctx, "r1"); err != nil {
		t.Fatalf("Lock: %v", err)
	}
	missing, err := store.GetState(ctx, "nope")
	if err != nil || missing != nil {
		t.Fatalf("GetState(missing) should be nil,nil — got %v,%v", missing, err)
	}
	if err := store.Unlock(ctx, "r1"); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
}
