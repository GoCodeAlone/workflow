package module_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/GoCodeAlone/workflow/module"
)

// mockGCSClient is an in-memory implementation of GCSObjectClient for testing.
type mockGCSClient struct {
	mu         sync.Mutex
	objects    map[string][]byte // key -> body
	generation map[string]int64  // key -> current generation
	errOnPut   map[string]error  // key -> error to return on conditional Put
}

func newMockGCSClient() *mockGCSClient {
	return &mockGCSClient{
		objects:    make(map[string][]byte),
		generation: make(map[string]int64),
		errOnPut:   make(map[string]error),
	}
}

func (m *mockGCSClient) ReadObject(_ context.Context, key string) ([]byte, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.objects[key]
	if !ok {
		return nil, 0, module.ErrGCSNotFound
	}
	return data, m.generation[key], nil
}

func (m *mockGCSClient) WriteObject(_ context.Context, key string, data []byte, _ string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.generation[key]++
	m.objects[key] = data
	return m.generation[key], nil
}

func (m *mockGCSClient) WriteObjectIfGenerationMatch(_ context.Context, key string, data []byte, _ string, generation int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err, ok := m.errOnPut[key]; ok {
		return 0, err
	}
	curr := m.generation[key]
	if generation == 0 {
		// Must not exist
		if _, exists := m.objects[key]; exists {
			return 0, fmt.Errorf("precondition failed: object exists")
		}
	} else if curr != generation {
		return 0, fmt.Errorf("precondition failed: generation mismatch (want %d, have %d)", generation, curr)
	}
	m.generation[key]++
	m.objects[key] = data
	return m.generation[key], nil
}

func (m *mockGCSClient) DeleteObject(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.objects[key]; !ok {
		return module.ErrGCSNotFound
	}
	delete(m.objects, key)
	delete(m.generation, key)
	return nil
}

func (m *mockGCSClient) ListObjects(_ context.Context, prefix string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var keys []string
	for k := range m.objects {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func newTestGCSStore(client module.GCSObjectClient) *module.GCSIaCStateStore {
	return module.NewGCSIaCStateStoreWithClient(client, "test-bucket", "iac-state/")
}

func TestGCSIaCStateStore_GetState_NotFound(t *testing.T) {
	store := newTestGCSStore(newMockGCSClient())

	st, err := store.GetState("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if st != nil {
		t.Fatalf("expected nil, got %+v", st)
	}
}

func TestGCSIaCStateStore_SaveAndGetState(t *testing.T) {
	store := newTestGCSStore(newMockGCSClient())

	state := &module.IaCState{
		ResourceID:   "gcs-cluster",
		ResourceType: "kubernetes",
		Provider:     "gcp",
		Status:       "active",
	}
	if err := store.SaveState(state); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	got, err := store.GetState("gcs-cluster")
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if got == nil {
		t.Fatal("expected state, got nil")
	}
	if got.Provider != "gcp" {
		t.Errorf("Provider = %q, want %q", got.Provider, "gcp")
	}
}

func TestGCSIaCStateStore_SaveState_Nil(t *testing.T) {
	store := newTestGCSStore(newMockGCSClient())
	if err := store.SaveState(nil); err == nil {
		t.Fatal("expected error for nil state")
	}
}

func TestGCSIaCStateStore_SaveState_EmptyID(t *testing.T) {
	store := newTestGCSStore(newMockGCSClient())
	if err := store.SaveState(&module.IaCState{}); err == nil {
		t.Fatal("expected error for empty resource_id")
	}
}

func TestGCSIaCStateStore_ListStates(t *testing.T) {
	store := newTestGCSStore(newMockGCSClient())

	for _, st := range []*module.IaCState{
		{ResourceID: "r1", ResourceType: "k8s", Provider: "gcp", Status: "active"},
		{ResourceID: "r2", ResourceType: "db", Provider: "gcp", Status: "active"},
		{ResourceID: "r3", ResourceType: "k8s", Provider: "aws", Status: "destroyed"},
	} {
		if err := store.SaveState(st); err != nil {
			t.Fatalf("SaveState %q: %v", st.ResourceID, err)
		}
	}

	all, err := store.ListStates(nil)
	if err != nil {
		t.Fatalf("ListStates(nil): %v", err)
	}
	if len(all) != 3 {
		t.Errorf("ListStates = %d, want 3", len(all))
	}

	filtered, err := store.ListStates(map[string]string{"provider": "gcp"})
	if err != nil {
		t.Fatalf("ListStates(provider=gcp): %v", err)
	}
	if len(filtered) != 2 {
		t.Errorf("ListStates(provider=gcp) = %d, want 2", len(filtered))
	}
}

func TestGCSIaCStateStore_DeleteState(t *testing.T) {
	store := newTestGCSStore(newMockGCSClient())

	if err := store.SaveState(&module.IaCState{ResourceID: "del-me", Status: "active"}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	if err := store.DeleteState("del-me"); err != nil {
		t.Fatalf("DeleteState: %v", err)
	}
	st, err := store.GetState("del-me")
	if err != nil {
		t.Fatalf("GetState after delete: %v", err)
	}
	if st != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestGCSIaCStateStore_DeleteState_NotFound(t *testing.T) {
	store := newTestGCSStore(newMockGCSClient())
	if err := store.DeleteState("nonexistent"); err == nil {
		t.Fatal("expected error deleting nonexistent state")
	}
}

func TestGCSIaCStateStore_LockUnlock(t *testing.T) {
	store := newTestGCSStore(newMockGCSClient())

	if err := store.Lock("res-1"); err != nil {
		t.Fatalf("Lock: %v", err)
	}
	// Double lock must fail.
	if err := store.Lock("res-1"); err == nil {
		t.Fatal("expected error on double lock")
	}
	if err := store.Unlock("res-1"); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	// Re-lock should succeed after unlock.
	if err := store.Lock("res-1"); err != nil {
		t.Fatalf("Lock after unlock: %v", err)
	}
}

func TestGCSIaCStateStore_Unlock_NotLocked(t *testing.T) {
	store := newTestGCSStore(newMockGCSClient())
	if err := store.Unlock("not-locked"); err == nil {
		t.Fatal("expected error unlocking non-locked resource")
	}
}

func TestGCSIaCStateStore_JSONRoundTrip(t *testing.T) {
	store := newTestGCSStore(newMockGCSClient())

	state := &module.IaCState{
		ResourceID: "rt-gcs",
		Provider:   "gcp",
		Status:     "active",
		Outputs:    map[string]any{"endpoint": "https://gcs.example.com"},
	}
	if err := store.SaveState(state); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	got, err := store.GetState("rt-gcs")
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	wantJSON, _ := json.Marshal(state)
	gotJSON, _ := json.Marshal(got)
	if string(wantJSON) != string(gotJSON) {
		t.Errorf("round-trip mismatch:\n  want: %s\n  got:  %s", wantJSON, gotJSON)
	}
}
