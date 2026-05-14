package module

import (
	"context"
	"encoding/json"
	"net"
	"strconv"
	"strings"
	"testing"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
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

// benchStateToProto — local, self-contained IaCState -> pb.IaCState converter.
// Task 7 replaces this with the production iacStateToProto.
func benchStateToProto(s *IaCState) *pb.IaCState {
	outJSON, _ := json.Marshal(s.Outputs)
	cfgJSON, _ := json.Marshal(s.Config)
	return &pb.IaCState{
		ResourceId: s.ResourceID, ResourceType: s.ResourceType, Provider: s.Provider,
		Status: s.Status, OutputsJson: outJSON, ConfigJson: cfgJSON,
		CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
	}
}

// benchStateBackendServer wraps an IaCStateStore behind pb.IaCStateBackendServer.
// Task 7 promotes this to the production iacStateBackendServer.
type benchStateBackendServer struct {
	pb.UnimplementedIaCStateBackendServer
	store IaCStateStore
}

func (s *benchStateBackendServer) GetState(_ context.Context, r *pb.GetStateRequest) (*pb.GetStateResponse, error) {
	st, err := s.store.GetState(r.ResourceId)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return &pb.GetStateResponse{Exists: false}, nil
	}
	return &pb.GetStateResponse{Exists: true, State: benchStateToProto(st)}, nil
}
func (s *benchStateBackendServer) SaveState(_ context.Context, r *pb.SaveStateRequest) (*pb.SaveStateResponse, error) {
	if r.State == nil {
		return nil, status.Error(codes.InvalidArgument, "SaveState: request State is nil")
	}
	var outputs, config map[string]any
	if len(r.State.OutputsJson) > 0 {
		if err := json.Unmarshal(r.State.OutputsJson, &outputs); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "SaveState: invalid OutputsJson: %v", err)
		}
	}
	if len(r.State.ConfigJson) > 0 {
		if err := json.Unmarshal(r.State.ConfigJson, &config); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "SaveState: invalid ConfigJson: %v", err)
		}
	}
	return &pb.SaveStateResponse{}, s.store.SaveState(&IaCState{
		ResourceID: r.State.ResourceId, ResourceType: r.State.ResourceType,
		Provider: r.State.Provider, Status: r.State.Status, Outputs: outputs, Config: config,
	})
}
func (s *benchStateBackendServer) Lock(_ context.Context, r *pb.LockRequest) (*pb.LockResponse, error) {
	return &pb.LockResponse{}, s.store.Lock(r.ResourceId)
}
func (s *benchStateBackendServer) Unlock(_ context.Context, r *pb.UnlockRequest) (*pb.UnlockResponse, error) {
	return &pb.UnlockResponse{}, s.store.Unlock(r.ResourceId)
}
func (s *benchStateBackendServer) ListStates(_ context.Context, _ *pb.ListStatesRequest) (*pb.ListStatesResponse, error) {
	return &pb.ListStatesResponse{}, nil
}
func (s *benchStateBackendServer) DeleteState(_ context.Context, r *pb.DeleteStateRequest) (*pb.DeleteStateResponse, error) {
	return &pb.DeleteStateResponse{}, s.store.DeleteState(r.ResourceId)
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
	defer lis.Close()
	srv := grpc.NewServer()
	pb.RegisterIaCStateBackendServer(srv, &benchStateBackendServer{store: NewMemoryIaCStateStore()})
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
	pbState := benchStateToProto(st)
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
