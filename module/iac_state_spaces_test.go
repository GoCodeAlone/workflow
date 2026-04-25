package module_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/GoCodeAlone/workflow/module"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// mockS3Client implements spacesS3Client for testing.
type mockS3Client struct {
	mu      sync.Mutex
	objects map[string][]byte // key -> body
}

func newMockS3Client() *mockS3Client {
	return &mockS3Client{objects: make(map[string][]byte)}
}

func (m *mockS3Client) GetObject(_ context.Context, input *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := aws.ToString(input.Key)
	data, ok := m.objects[key]
	if !ok {
		return nil, &types.NoSuchKey{Message: aws.String("NoSuchKey: " + key)}
	}
	return &s3.GetObjectOutput{
		Body: io.NopCloser(bytes.NewReader(data)),
	}, nil
}

func (m *mockS3Client) PutObject(_ context.Context, input *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := aws.ToString(input.Key)
	// Honour If-None-Match: * — fail if the object already exists (atomic lock semantics).
	if aws.ToString(input.IfNoneMatch) == "*" {
		if _, exists := m.objects[key]; exists {
			return nil, fmt.Errorf("PreconditionFailed: object %q already exists", key)
		}
	}
	data, err := io.ReadAll(input.Body)
	if err != nil {
		return nil, err
	}
	m.objects[key] = data
	return &s3.PutObjectOutput{}, nil
}

func (m *mockS3Client) DeleteObject(_ context.Context, input *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := aws.ToString(input.Key)
	delete(m.objects, key)
	return &s3.DeleteObjectOutput{}, nil
}

func (m *mockS3Client) ListObjectsV2(_ context.Context, input *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	prefix := aws.ToString(input.Prefix)
	var contents []types.Object
	for key := range m.objects {
		if strings.HasPrefix(key, prefix) {
			contents = append(contents, types.Object{Key: aws.String(key)})
		}
	}
	return &s3.ListObjectsV2Output{
		Contents:    contents,
		IsTruncated: aws.Bool(false),
	}, nil
}

func (m *mockS3Client) HeadObject(_ context.Context, input *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := aws.ToString(input.Key)
	if _, ok := m.objects[key]; !ok {
		return nil, &types.NotFound{Message: aws.String("NotFound: " + key)}
	}
	return &s3.HeadObjectOutput{}, nil
}

func newTestSpacesStore(client *mockS3Client) *module.SpacesIaCStateStore {
	return module.NewSpacesIaCStateStoreWithClient(client, "test-bucket", "iac-state/")
}

func TestSpacesIaCStateStore_GetState_NotFound(t *testing.T) {
	store := newTestSpacesStore(newMockS3Client())

	st, err := store.GetState("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if st != nil {
		t.Fatalf("expected nil state, got %+v", st)
	}
}

func TestSpacesIaCStateStore_SaveAndGetState(t *testing.T) {
	store := newTestSpacesStore(newMockS3Client())

	state := &module.IaCState{
		ResourceID:   "cluster-1",
		ResourceType: "kubernetes",
		Provider:     "digitalocean",
		Status:       "active",
		Outputs:      map[string]any{"endpoint": "https://k8s.example.com"},
		Config:       map[string]any{"region": "nyc3"},
		Dependencies: []string{"network-1"},
		CreatedAt:    "2026-03-09T00:00:00Z",
		UpdatedAt:    "2026-03-09T00:00:00Z",
	}

	if err := store.SaveState(state); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	got, err := store.GetState("cluster-1")
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if got == nil {
		t.Fatal("expected state, got nil")
	}
	if got.ResourceID != "cluster-1" {
		t.Errorf("ResourceID = %q, want %q", got.ResourceID, "cluster-1")
	}
	if got.Provider != "digitalocean" {
		t.Errorf("Provider = %q, want %q", got.Provider, "digitalocean")
	}
	if got.Status != "active" {
		t.Errorf("Status = %q, want %q", got.Status, "active")
	}
	if len(got.Dependencies) != 1 || got.Dependencies[0] != "network-1" {
		t.Errorf("Dependencies = %#v, want [network-1]", got.Dependencies)
	}
}

func TestSpacesIaCStateStore_SaveState_Nil(t *testing.T) {
	store := newTestSpacesStore(newMockS3Client())

	err := store.SaveState(nil)
	if err == nil {
		t.Fatal("expected error for nil state")
	}
}

func TestSpacesIaCStateStore_SaveState_EmptyID(t *testing.T) {
	store := newTestSpacesStore(newMockS3Client())

	err := store.SaveState(&module.IaCState{})
	if err == nil {
		t.Fatal("expected error for empty resource_id")
	}
}

func TestSpacesIaCStateStore_ListStates(t *testing.T) {
	client := newMockS3Client()
	store := newTestSpacesStore(client)

	states := []*module.IaCState{
		{ResourceID: "r1", ResourceType: "kubernetes", Provider: "aws", Status: "active"},
		{ResourceID: "r2", ResourceType: "database", Provider: "digitalocean", Status: "active"},
		{ResourceID: "r3", ResourceType: "kubernetes", Provider: "aws", Status: "destroyed"},
	}
	for _, st := range states {
		if err := store.SaveState(st); err != nil {
			t.Fatalf("SaveState %q: %v", st.ResourceID, err)
		}
	}

	// No filter — returns all.
	all, err := store.ListStates(nil)
	if err != nil {
		t.Fatalf("ListStates(nil): %v", err)
	}
	if len(all) != 3 {
		t.Errorf("ListStates(nil) = %d items, want 3", len(all))
	}

	// Filter by provider.
	filtered, err := store.ListStates(map[string]string{"provider": "aws"})
	if err != nil {
		t.Fatalf("ListStates(provider=aws): %v", err)
	}
	if len(filtered) != 2 {
		t.Errorf("ListStates(provider=aws) = %d items, want 2", len(filtered))
	}

	// Filter by status.
	active, err := store.ListStates(map[string]string{"status": "active"})
	if err != nil {
		t.Fatalf("ListStates(status=active): %v", err)
	}
	if len(active) != 2 {
		t.Errorf("ListStates(status=active) = %d items, want 2", len(active))
	}
}

func TestSpacesIaCStateStore_ListStates_SkipsLockFiles(t *testing.T) {
	client := newMockS3Client()
	store := newTestSpacesStore(client)

	// Save a state and lock it — lock file should be skipped in list.
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
		t.Errorf("ListStates returned %d items (expected 1, lock file should be excluded)", len(results))
	}
}

func TestSpacesIaCStateStore_DeleteState(t *testing.T) {
	store := newTestSpacesStore(newMockS3Client())

	if err := store.SaveState(&module.IaCState{ResourceID: "del-me", Status: "active"}); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	if err := store.DeleteState("del-me"); err != nil {
		t.Fatalf("DeleteState: %v", err)
	}

	// Should be gone.
	st, err := store.GetState("del-me")
	if err != nil {
		t.Fatalf("GetState after delete: %v", err)
	}
	if st != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestSpacesIaCStateStore_DeleteState_NotFound(t *testing.T) {
	store := newTestSpacesStore(newMockS3Client())

	err := store.DeleteState("nonexistent")
	if err == nil {
		t.Fatal("expected error deleting nonexistent state")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, expected 'not found'", err)
	}
}

func TestSpacesIaCStateStore_LockUnlock(t *testing.T) {
	store := newTestSpacesStore(newMockS3Client())

	// Lock should succeed.
	if err := store.Lock("res-1"); err != nil {
		t.Fatalf("Lock: %v", err)
	}

	// Double-lock should fail.
	if err := store.Lock("res-1"); err == nil {
		t.Fatal("expected error on double lock")
	}

	// Unlock should succeed.
	if err := store.Unlock("res-1"); err != nil {
		t.Fatalf("Unlock: %v", err)
	}

	// Re-lock after unlock should succeed.
	if err := store.Lock("res-1"); err != nil {
		t.Fatalf("Lock after unlock: %v", err)
	}
}

func TestSpacesIaCStateStore_Unlock_NotLocked(t *testing.T) {
	store := newTestSpacesStore(newMockS3Client())

	err := store.Unlock("not-locked")
	if err == nil {
		t.Fatal("expected error unlocking a resource that is not locked")
	}
	if !strings.Contains(err.Error(), "not locked") {
		t.Errorf("error = %q, expected 'not locked'", err)
	}
}

func TestSpacesIaCStateStore_SaveState_Overwrite(t *testing.T) {
	store := newTestSpacesStore(newMockS3Client())

	original := &module.IaCState{ResourceID: "r1", Status: "planned"}
	if err := store.SaveState(original); err != nil {
		t.Fatalf("SaveState (original): %v", err)
	}

	updated := &module.IaCState{ResourceID: "r1", Status: "active"}
	if err := store.SaveState(updated); err != nil {
		t.Fatalf("SaveState (updated): %v", err)
	}

	got, err := store.GetState("r1")
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if got.Status != "active" {
		t.Errorf("Status = %q, want %q (overwrite failed)", got.Status, "active")
	}
}

func TestSpacesIaCStateStore_SanitizesResourceID(t *testing.T) {
	client := newMockS3Client()
	store := newTestSpacesStore(client)

	state := &module.IaCState{ResourceID: "ns/cluster\\1", Status: "active"}
	if err := store.SaveState(state); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	// Verify the key was sanitized.
	client.mu.Lock()
	_, exists := client.objects["iac-state/ns_cluster_1.json"]
	client.mu.Unlock()
	if !exists {
		t.Error("expected sanitized key 'iac-state/ns_cluster_1.json' in mock objects")
	}

	// Retrieve by original ID.
	got, err := store.GetState("ns/cluster\\1")
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if got == nil {
		t.Fatal("expected state, got nil")
	}
}

// TestSpacesIaCStateStore_GetState_BadJSON verifies graceful handling of corrupt data.
func TestSpacesIaCStateStore_GetState_BadJSON(t *testing.T) {
	client := newMockS3Client()
	store := newTestSpacesStore(client)

	// Manually inject bad JSON.
	client.mu.Lock()
	client.objects["iac-state/bad.json"] = []byte("{invalid json")
	client.mu.Unlock()

	_, err := store.GetState("bad")
	if err == nil {
		t.Fatal("expected unmarshal error for bad JSON")
	}
	if !strings.Contains(err.Error(), "unmarshal") {
		t.Errorf("error = %q, expected 'unmarshal' substring", err)
	}
}

// Ensure the mock properly serializes JSON round-trip.
func TestSpacesIaCStateStore_JSONRoundTrip(t *testing.T) {
	store := newTestSpacesStore(newMockS3Client())

	state := &module.IaCState{
		ResourceID:   "rt-1",
		ResourceType: "ecs",
		Provider:     "aws",
		ProviderID:   "arn:aws:ecs:us-east-1:123:cluster/test",
		ConfigHash:   "config-hash-rt-1",
		Status:       "provisioning",
		Outputs:      map[string]any{"arn": "arn:aws:ecs:us-east-1:123:cluster/test"},
		Config:       map[string]any{"cpu": float64(256), "memory": float64(512)},
		CreatedAt:    "2026-01-01T00:00:00Z",
		UpdatedAt:    "2026-03-09T12:00:00Z",
		Error:        "timeout waiting for stabilization",
	}

	if err := store.SaveState(state); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	got, err := store.GetState("rt-1")
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}

	// Compare via JSON to handle map ordering.
	wantJSON, _ := json.Marshal(state)
	gotJSON, _ := json.Marshal(got)
	if string(wantJSON) != string(gotJSON) {
		t.Errorf("round-trip mismatch:\n  want: %s\n  got:  %s", wantJSON, gotJSON)
	}
}

// errS3Client is a mock that returns errors for all operations.
type errS3Client struct{}

func (e *errS3Client) GetObject(_ context.Context, _ *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	return nil, fmt.Errorf("simulated GetObject failure")
}
func (e *errS3Client) PutObject(_ context.Context, _ *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	return nil, fmt.Errorf("simulated PutObject failure")
}
func (e *errS3Client) DeleteObject(_ context.Context, _ *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	return nil, fmt.Errorf("simulated DeleteObject failure")
}
func (e *errS3Client) ListObjectsV2(_ context.Context, _ *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	return nil, fmt.Errorf("simulated ListObjectsV2 failure")
}
func (e *errS3Client) HeadObject(_ context.Context, _ *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	return nil, fmt.Errorf("simulated HeadObject failure")
}

func TestSpacesIaCStateStore_ErrorPropagation(t *testing.T) {
	store := module.NewSpacesIaCStateStoreWithClient(&errS3Client{}, "test-bucket", "iac-state/")

	// GetState error.
	_, err := store.GetState("x")
	if err == nil || !strings.Contains(err.Error(), "simulated") {
		t.Errorf("GetState error = %v, want simulated error", err)
	}

	// SaveState error.
	err = store.SaveState(&module.IaCState{ResourceID: "x"})
	if err == nil || !strings.Contains(err.Error(), "simulated") {
		t.Errorf("SaveState error = %v, want simulated error", err)
	}

	// ListStates error.
	_, err = store.ListStates(nil)
	if err == nil || !strings.Contains(err.Error(), "simulated") {
		t.Errorf("ListStates error = %v, want simulated error", err)
	}

	// DeleteState error (HeadObject fails).
	err = store.DeleteState("x")
	if err == nil || !strings.Contains(err.Error(), "simulated") {
		t.Errorf("DeleteState error = %v, want simulated error", err)
	}

	// Lock error (HeadObject fails with non-NotFound).
	err = store.Lock("x")
	if err == nil || !strings.Contains(err.Error(), "simulated") {
		t.Errorf("Lock error = %v, want simulated error", err)
	}

	// Unlock error (HeadObject fails with non-NotFound).
	err = store.Unlock("x")
	if err == nil || !strings.Contains(err.Error(), "simulated") {
		t.Errorf("Unlock error = %v, want simulated error", err)
	}
}
