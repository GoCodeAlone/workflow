package module

import (
	"context"
	"encoding/json"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
)

// ─────────────────────────────────────────────────────────────────────────────
// IaCState ⇄ pb.IaCState converters.
//
// The free-form Outputs / Config map[string]any fields cross the wire as JSON
// bytes — the iac.proto hard invariant (iac.proto:6-10) forbids
// google.protobuf.Struct. The plugin/host owns json.Marshal/Unmarshal directly.
// ─────────────────────────────────────────────────────────────────────────────

// iacStateToProto converts a module IaCState into its proto wire form.
func iacStateToProto(s *IaCState) (*pb.IaCState, error) {
	if s == nil {
		return nil, nil
	}
	outputsJSON, err := json.Marshal(s.Outputs)
	if err != nil {
		return nil, err
	}
	configJSON, err := json.Marshal(s.Config)
	if err != nil {
		return nil, err
	}
	return &pb.IaCState{
		ResourceId:   s.ResourceID,
		ResourceType: s.ResourceType,
		Provider:     s.Provider,
		ProviderRef:  s.ProviderRef,
		ProviderId:   s.ProviderID,
		ConfigHash:   s.ConfigHash,
		Status:       s.Status,
		OutputsJson:  outputsJSON,
		ConfigJson:   configJSON,
		Dependencies: s.Dependencies,
		CreatedAt:    s.CreatedAt,
		UpdatedAt:    s.UpdatedAt,
		Error:        s.Error,
	}, nil
}

// iacStateFromProto converts a proto IaCState back into a module IaCState.
//
// Empty / "null" / "{}" JSON byte payloads decode to a nil map (not an empty
// non-nil map) so round-trips through a nil Outputs/Config stay clean.
func iacStateFromProto(p *pb.IaCState) (*IaCState, error) {
	if p == nil {
		return nil, nil
	}
	outputs, err := jsonBytesToMap(p.OutputsJson)
	if err != nil {
		return nil, err
	}
	config, err := jsonBytesToMap(p.ConfigJson)
	if err != nil {
		return nil, err
	}
	return &IaCState{
		ResourceID:   p.ResourceId,
		ResourceType: p.ResourceType,
		Provider:     p.Provider,
		ProviderRef:  p.ProviderRef,
		ProviderID:   p.ProviderId,
		ConfigHash:   p.ConfigHash,
		Status:       p.Status,
		Outputs:      outputs,
		Config:       config,
		Dependencies: p.Dependencies,
		CreatedAt:    p.CreatedAt,
		UpdatedAt:    p.UpdatedAt,
		Error:        p.Error,
	}, nil
}

// jsonBytesToMap decodes JSON bytes into a map[string]any. Empty, "null" and
// "{}" inputs yield a nil map.
func jsonBytesToMap(b []byte) (map[string]any, error) {
	s := string(b)
	if len(b) == 0 || s == "null" || s == "{}" {
		return nil, nil
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// grpcIaCStateStore — host-side IaCStateStore implemented over an
// IaCStateBackendClient. The host half of the strict IaCStateBackend contract.
// ─────────────────────────────────────────────────────────────────────────────

// grpcIaCStateStore adapts a pb.IaCStateBackendClient to module.IaCStateStore.
type grpcIaCStateStore struct {
	client pb.IaCStateBackendClient
}

// newGRPCIaCStateStore wraps an IaCStateBackendClient as an IaCStateStore.
func newGRPCIaCStateStore(c pb.IaCStateBackendClient) *grpcIaCStateStore {
	return &grpcIaCStateStore{client: c}
}

// GetState retrieves a state record by resource ID. Returns nil, nil when the
// backend reports the record does not exist.
func (s *grpcIaCStateStore) GetState(ctx context.Context, resourceID string) (*IaCState, error) {
	resp, err := s.client.GetState(ctx, &pb.GetStateRequest{ResourceId: resourceID})
	if err != nil {
		return nil, err
	}
	if !resp.Exists {
		return nil, nil
	}
	return iacStateFromProto(resp.State)
}

// SaveState inserts or replaces a state record.
func (s *grpcIaCStateStore) SaveState(ctx context.Context, state *IaCState) error {
	pbState, err := iacStateToProto(state)
	if err != nil {
		return err
	}
	_, err = s.client.SaveState(ctx, &pb.SaveStateRequest{State: pbState})
	return err
}

// ListStates returns all state records matching the provided key=value filter.
func (s *grpcIaCStateStore) ListStates(ctx context.Context, filter map[string]string) ([]*IaCState, error) {
	resp, err := s.client.ListStates(ctx, &pb.ListStatesRequest{Filter: filter})
	if err != nil {
		return nil, err
	}
	states := make([]*IaCState, 0, len(resp.States))
	for _, p := range resp.States {
		st, convErr := iacStateFromProto(p)
		if convErr != nil {
			return nil, convErr
		}
		states = append(states, st)
	}
	return states, nil
}

// DeleteState removes a state record by resource ID.
func (s *grpcIaCStateStore) DeleteState(ctx context.Context, resourceID string) error {
	_, err := s.client.DeleteState(ctx, &pb.DeleteStateRequest{ResourceId: resourceID})
	return err
}

// Lock acquires an exclusive lock for the given resource ID.
func (s *grpcIaCStateStore) Lock(ctx context.Context, resourceID string) error {
	_, err := s.client.Lock(ctx, &pb.LockRequest{ResourceId: resourceID})
	return err
}

// Unlock releases the lock for the given resource ID.
func (s *grpcIaCStateStore) Unlock(ctx context.Context, resourceID string) error {
	_, err := s.client.Unlock(ctx, &pb.UnlockRequest{ResourceId: resourceID})
	return err
}

// ─────────────────────────────────────────────────────────────────────────────
// iacStateBackendServer — production pb.IaCStateBackendServer that delegates to
// any module.IaCStateStore. The plugin-side half of the contract.
// ─────────────────────────────────────────────────────────────────────────────

// iacStateBackendServer serves an IaCStateStore over the IaCStateBackend gRPC
// contract.
type iacStateBackendServer struct {
	pb.UnimplementedIaCStateBackendServer
	store IaCStateStore
}

// GetState delegates to the backing store, mapping a not-found (nil) result to
// GetStateResponse{Exists: false}.
func (s *iacStateBackendServer) GetState(ctx context.Context, r *pb.GetStateRequest) (*pb.GetStateResponse, error) {
	st, err := s.store.GetState(ctx, r.ResourceId)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return &pb.GetStateResponse{Exists: false}, nil
	}
	pbState, err := iacStateToProto(st)
	if err != nil {
		return nil, err
	}
	return &pb.GetStateResponse{Exists: true, State: pbState}, nil
}

// SaveState delegates a full-state replace to the backing store.
func (s *iacStateBackendServer) SaveState(ctx context.Context, r *pb.SaveStateRequest) (*pb.SaveStateResponse, error) {
	st, err := iacStateFromProto(r.State)
	if err != nil {
		return nil, err
	}
	if err := s.store.SaveState(ctx, st); err != nil {
		return nil, err
	}
	return &pb.SaveStateResponse{}, nil
}

// ListStates delegates a filtered listing to the backing store.
func (s *iacStateBackendServer) ListStates(ctx context.Context, r *pb.ListStatesRequest) (*pb.ListStatesResponse, error) {
	states, err := s.store.ListStates(ctx, r.Filter)
	if err != nil {
		return nil, err
	}
	pbStates := make([]*pb.IaCState, 0, len(states))
	for _, st := range states {
		pbState, convErr := iacStateToProto(st)
		if convErr != nil {
			return nil, convErr
		}
		pbStates = append(pbStates, pbState)
	}
	return &pb.ListStatesResponse{States: pbStates}, nil
}

// DeleteState delegates a delete-by-ID to the backing store.
func (s *iacStateBackendServer) DeleteState(ctx context.Context, r *pb.DeleteStateRequest) (*pb.DeleteStateResponse, error) {
	if err := s.store.DeleteState(ctx, r.ResourceId); err != nil {
		return nil, err
	}
	return &pb.DeleteStateResponse{}, nil
}

// Lock delegates lock acquisition to the backing store.
func (s *iacStateBackendServer) Lock(ctx context.Context, r *pb.LockRequest) (*pb.LockResponse, error) {
	if err := s.store.Lock(ctx, r.ResourceId); err != nil {
		return nil, err
	}
	return &pb.LockResponse{}, nil
}

// Unlock delegates lock release to the backing store.
func (s *iacStateBackendServer) Unlock(ctx context.Context, r *pb.UnlockRequest) (*pb.UnlockResponse, error) {
	if err := s.store.Unlock(ctx, r.ResourceId); err != nil {
		return nil, err
	}
	return &pb.UnlockResponse{}, nil
}
