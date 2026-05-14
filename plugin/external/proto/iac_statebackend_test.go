package proto

import (
	"context"
	"testing"
)

// Compile-level guard: the IaCStateBackend service + its messages must exist
// in the generated package with the IaCStateStore-mirroring shape.
func TestIaCStateBackendGeneratedTypesExist(t *testing.T) {
	var _ IaCStateBackendServer // service interface generated
	var _ IaCStateBackendClient // client interface generated
	_ = &GetStateRequest{ResourceId: "r"}
	_ = &GetStateResponse{Exists: true, State: &IaCState{}}
	_ = &SaveStateRequest{State: &IaCState{}}
	_ = &ListStatesRequest{Filter: map[string]string{"k": "v"}}
	_ = &LockRequest{ResourceId: "r"}
	_ = &UnlockRequest{ResourceId: "r"}
	// IaCState mirrors module.IaCState; free-form Outputs/Config cross the wire
	// as JSON bytes per the iac.proto hard invariant (NO google.protobuf.Struct).
	s := &IaCState{ResourceId: "r", ResourceType: "kubernetes", Provider: "azure",
		Status: "active", OutputsJson: []byte(`{}`), ConfigJson: []byte(`{}`)}
	if s.GetResourceId() != "r" {
		t.Fatalf("IaCState.ResourceId accessor missing")
	}
}

func TestIaCStateBackendListBackendNamesGenerated(t *testing.T) {
	_ = &ListBackendNamesRequest{}
	resp := &ListBackendNamesResponse{BackendNames: []string{"azure_blob"}}
	if resp.GetBackendNames()[0] != "azure_blob" {
		t.Fatalf("ListBackendNamesResponse.BackendNames accessor missing")
	}
	// the RPC must be on the IaCStateBackend service interfaces:
	var _ interface {
		ListBackendNames(context.Context, *ListBackendNamesRequest) (*ListBackendNamesResponse, error)
	} = (IaCStateBackendServer)(nil)
}
