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

func TestGRPCIaCStateStoreRoundTrip(t *testing.T) {
	lis := bufconn.Listen(4 << 20)
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

	want := &IaCState{ResourceID: "r1", ResourceType: "kubernetes", Provider: "azure", Status: "active",
		Outputs: map[string]any{"endpoint": "https://x"}, Config: map[string]any{"size": "L"}}
	if err := store.SaveState(want); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	got, err := store.GetState("r1")
	if err != nil || got == nil {
		t.Fatalf("GetState: %v (got=%v)", err, got)
	}
	if got.ResourceID != "r1" || got.Status != "active" || got.Outputs["endpoint"] != "https://x" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if err := store.Lock("r1"); err != nil {
		t.Fatalf("Lock: %v", err)
	}
	missing, err := store.GetState("nope")
	if err != nil || missing != nil {
		t.Fatalf("GetState(missing) should be nil,nil — got %v,%v", missing, err)
	}
	if err := store.Unlock("r1"); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
}
