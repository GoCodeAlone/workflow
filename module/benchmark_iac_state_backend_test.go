package module

import (
	"context"
	"net"
	"strconv"
	"strings"
	"testing"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// oneMBState builds an IaCState whose JSON payload is ~1 MB (Outputs map padded).
func oneMBState() *IaCState {
	big := strings.Repeat("x", 1024)
	outputs := make(map[string]any, 1024)
	for i := 0; i < 1024; i++ {
		outputs["k"+strconv.Itoa(i)] = big
	}
	return &IaCState{
		ResourceID: "bench-resource", ResourceType: "kubernetes", Provider: "azure",
		Status: "active", Outputs: outputs, Config: map[string]any{"size": "large"},
		CreatedAt: "2026-05-14T00:00:00Z", UpdatedAt: "2026-05-14T00:00:00Z",
	}
}

// BenchmarkIaCStateBackend_InProcess is the baseline: direct IaCStateStore calls.
func BenchmarkIaCStateBackend_InProcess(b *testing.B) {
	store := NewMemoryIaCStateStore()
	st := oneMBState()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := store.Lock(st.ResourceID); err != nil {
			b.Fatal(err)
		}
		if _, err := store.GetState(st.ResourceID); err != nil {
			b.Fatal(err)
		}
		if err := store.SaveState(st); err != nil {
			b.Fatal(err)
		}
		if err := store.Unlock(st.ResourceID); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkIaCStateBackend_GRPC is the post-extraction path: same store, same
// cycle, but every call crosses a real (in-memory bufconn) gRPC boundary.
func BenchmarkIaCStateBackend_GRPC(b *testing.B) {
	// 4 MiB in-memory listener buffer. Note: this sizes the bufconn pipe only;
	// gRPC's own max message size is configured separately via dial/server options.
	lis := bufconn.Listen(4 << 20)
	b.Cleanup(func() { _ = lis.Close() })
	srv := grpc.NewServer()
	pb.RegisterIaCStateBackendServer(srv, &iacStateBackendServer{store: NewMemoryIaCStateStore()})
	go func() { _ = srv.Serve(lis) }()
	defer srv.Stop()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		b.Fatal(err)
	}
	defer conn.Close()
	client := pb.NewIaCStateBackendClient(conn)
	st := oneMBState()
	pbState, err := iacStateToProto(st)
	if err != nil {
		b.Fatal(err)
	}
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := client.Lock(ctx, &pb.LockRequest{ResourceId: st.ResourceID}); err != nil {
			b.Fatal(err)
		}
		if _, err := client.GetState(ctx, &pb.GetStateRequest{ResourceId: st.ResourceID}); err != nil {
			b.Fatal(err)
		}
		if _, err := client.SaveState(ctx, &pb.SaveStateRequest{State: pbState}); err != nil {
			b.Fatal(err)
		}
		if _, err := client.Unlock(ctx, &pb.UnlockRequest{ResourceId: st.ResourceID}); err != nil {
			b.Fatal(err)
		}
	}
}
