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

// mockAzureClient is an in-memory implementation of AzureBlobClient for testing.
type mockAzureClient struct {
	mu      sync.Mutex
	blobs   map[string][]byte  // name -> body
	leases  map[string]string  // name -> leaseID (empty = not leased)
}

func newMockAzureClient() *mockAzureClient {
	return &mockAzureClient{
		blobs:  make(map[string][]byte),
		leases: make(map[string]string),
	}
}

func (m *mockAzureClient) DownloadBlob(_ context.Context, name string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.blobs[name]
	if !ok {
		return nil, module.ErrAzureBlobNotFound
	}
	return data, nil
}

func (m *mockAzureClient) UploadBlob(_ context.Context, name string, data []byte, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blobs[name] = data
	return nil
}

func (m *mockAzureClient) DeleteBlob(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.blobs[name]; !ok {
		return module.ErrAzureBlobNotFound
	}
	delete(m.blobs, name)
	return nil
}

func (m *mockAzureClient) ListBlobs(_ context.Context, prefix string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var names []string
	for name := range m.blobs {
		if strings.HasPrefix(name, prefix) {
			names = append(names, name)
		}
	}
	return names, nil
}

func (m *mockAzureClient) AcquireLease(_ context.Context, name string, _ int32) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Ensure blob exists (leases require an existing blob).
	if _, ok := m.blobs[name]; !ok {
		// Create a placeholder for the lock blob.
		m.blobs[name] = []byte{}
	}
	if leaseID := m.leases[name]; leaseID != "" {
		return "", fmt.Errorf("blob %q is already leased", name)
	}
	leaseID := fmt.Sprintf("lease-%s", name)
	m.leases[name] = leaseID
	return leaseID, nil
}

func (m *mockAzureClient) ReleaseLease(_ context.Context, name, leaseID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.leases[name] != leaseID {
		return fmt.Errorf("blob %q has no lease %q", name, leaseID)
	}
	delete(m.leases, name)
	delete(m.blobs, name)
	return nil
}

func newTestAzureStore(client module.AzureBlobClient) *module.AzureBlobIaCStateStore {
	return module.NewAzureBlobIaCStateStoreWithClient(client, "test-container", "iac-state/")
}

func TestAzureBlobIaCStateStore_GetState_NotFound(t *testing.T) {
	store := newTestAzureStore(newMockAzureClient())
	st, err := store.GetState("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if st != nil {
		t.Fatalf("expected nil, got %+v", st)
	}
}

func TestAzureBlobIaCStateStore_SaveAndGetState(t *testing.T) {
	store := newTestAzureStore(newMockAzureClient())

	state := &module.IaCState{
		ResourceID:   "az-cluster",
		ResourceType: "kubernetes",
		Provider:     "azure",
		Status:       "active",
	}
	if err := store.SaveState(state); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	got, err := store.GetState("az-cluster")
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if got == nil {
		t.Fatal("expected state, got nil")
	}
	if got.Provider != "azure" {
		t.Errorf("Provider = %q, want %q", got.Provider, "azure")
	}
}

func TestAzureBlobIaCStateStore_SaveState_Nil(t *testing.T) {
	store := newTestAzureStore(newMockAzureClient())
	if err := store.SaveState(nil); err == nil {
		t.Fatal("expected error for nil state")
	}
}

func TestAzureBlobIaCStateStore_SaveState_EmptyID(t *testing.T) {
	store := newTestAzureStore(newMockAzureClient())
	if err := store.SaveState(&module.IaCState{}); err == nil {
		t.Fatal("expected error for empty resource_id")
	}
}

func TestAzureBlobIaCStateStore_ListStates(t *testing.T) {
	store := newTestAzureStore(newMockAzureClient())

	for _, st := range []*module.IaCState{
		{ResourceID: "r1", ResourceType: "k8s", Provider: "azure", Status: "active"},
		{ResourceID: "r2", ResourceType: "db", Provider: "azure", Status: "active"},
		{ResourceID: "r3", ResourceType: "k8s", Provider: "gcp", Status: "destroyed"},
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

	filtered, err := store.ListStates(map[string]string{"provider": "azure"})
	if err != nil {
		t.Fatalf("ListStates(provider=azure): %v", err)
	}
	if len(filtered) != 2 {
		t.Errorf("ListStates(provider=azure) = %d, want 2", len(filtered))
	}
}

func TestAzureBlobIaCStateStore_ListStates_SkipsLockBlobs(t *testing.T) {
	store := newTestAzureStore(newMockAzureClient())

	if err := store.SaveState(&module.IaCState{ResourceID: "r1", Status: "active"}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	if err := store.Lock("r1"); err != nil {
		t.Fatalf("Lock: %v", err)
	}

	results, err := store.ListStates(nil)
	if err != nil {
		t.Fatalf("ListStates: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result (lock blob excluded), got %d", len(results))
	}
}

func TestAzureBlobIaCStateStore_DeleteState(t *testing.T) {
	store := newTestAzureStore(newMockAzureClient())

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

func TestAzureBlobIaCStateStore_DeleteState_NotFound(t *testing.T) {
	store := newTestAzureStore(newMockAzureClient())
	if err := store.DeleteState("nonexistent"); err == nil {
		t.Fatal("expected error deleting nonexistent state")
	}
}

func TestAzureBlobIaCStateStore_LockUnlock(t *testing.T) {
	store := newTestAzureStore(newMockAzureClient())

	if err := store.Lock("res-1"); err != nil {
		t.Fatalf("Lock: %v", err)
	}
	if err := store.Lock("res-1"); err == nil {
		t.Fatal("expected error on double lock")
	}
	if err := store.Unlock("res-1"); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if err := store.Lock("res-1"); err != nil {
		t.Fatalf("Lock after unlock: %v", err)
	}
}

func TestAzureBlobIaCStateStore_Unlock_NotLocked(t *testing.T) {
	store := newTestAzureStore(newMockAzureClient())
	if err := store.Unlock("not-locked"); err == nil {
		t.Fatal("expected error unlocking non-locked resource")
	}
}

// TestAzureBlobIaCStateStore_Unlock_PassesLeaseID verifies that Unlock passes the
// correct leaseID to ReleaseLease. The mock enforces leaseID matching, so this
// test will fail if ReleaseLease ignores the leaseID parameter.
func TestAzureBlobIaCStateStore_Unlock_PassesLeaseID(t *testing.T) {
	client := newMockAzureClient()
	store := newTestAzureStore(client)

	if err := store.Lock("res-lease"); err != nil {
		t.Fatalf("Lock: %v", err)
	}
	// Unlock must pass the correct leaseID — mock rejects wrong/empty leaseIDs.
	if err := store.Unlock("res-lease"); err != nil {
		t.Fatalf("Unlock with leaseID: %v", err)
	}
	// After unlock, should be able to re-lock.
	if err := store.Lock("res-lease"); err != nil {
		t.Fatalf("Lock after Unlock: %v", err)
	}
}

func TestAzureBlobIaCStateStore_JSONRoundTrip(t *testing.T) {
	store := newTestAzureStore(newMockAzureClient())

	state := &module.IaCState{
		ResourceID: "az-rt",
		Provider:   "azure",
		Status:     "active",
		Outputs:    map[string]any{"fqdn": "myapp.azurewebsites.net"},
	}
	if err := store.SaveState(state); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	got, err := store.GetState("az-rt")
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	wantJSON, _ := json.Marshal(state)
	gotJSON, _ := json.Marshal(got)
	if string(wantJSON) != string(gotJSON) {
		t.Errorf("round-trip mismatch:\n  want: %s\n  got:  %s", wantJSON, gotJSON)
	}
}
