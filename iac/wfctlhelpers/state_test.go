package wfctlhelpers_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/wfctlhelpers"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// TestResolveStateStore_MemoryBackend verifies that wfctlhelpers.ResolveStateStore
// resolves an iac.state module configured with backend: memory to a usable
// interfaces.IaCStateStore. It asserts an empty fresh store, then exercises a
// full round-trip (SaveResource / ListResources / GetResource / DeleteResource)
// to confirm the store contract is satisfied end-to-end.
func TestResolveStateStore_MemoryBackend(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "test.yaml")
	if err := os.WriteFile(cfgPath, []byte(`modules:
  - name: iac-state
    type: iac.state
    config:
      backend: memory
`), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := wfctlhelpers.ResolveStateStore(cfgPath, "", "")
	if err != nil {
		t.Fatalf("ResolveStateStore: %v", err)
	}
	if store == nil {
		t.Fatal("ResolveStateStore returned nil store with nil error")
	}
	resources, err := store.ListResources(context.Background())
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(resources) != 0 {
		t.Errorf("expected 0 resources from fresh memory store, got %d", len(resources))
	}
	// Round-trip: save + list + get + delete a resource so the test
	// exercises every method the handler library will use.
	state := interfaces.ResourceState{
		ID:       "vpc-test",
		Name:     "vpc-test",
		Type:     "infra.vpc",
		Provider: "stub",
	}
	if err := store.SaveResource(context.Background(), state); err != nil {
		t.Fatalf("SaveResource: %v", err)
	}
	got, err := store.GetResource(context.Background(), "vpc-test")
	if err != nil {
		t.Fatalf("GetResource: %v", err)
	}
	if got == nil || got.Name != "vpc-test" {
		t.Errorf("GetResource returned unexpected: %+v", got)
	}
	if err := store.DeleteResource(context.Background(), "vpc-test"); err != nil {
		t.Fatalf("DeleteResource: %v", err)
	}
}

// TestResolveStateStore_NoIaCStateModule returns a no-op store (not an
// error) when no iac.state module is declared. Mirrors the wfctl-internal
// resolveStateStore behavior so callers don't need to special-case
// configs that skip state persistence.
func TestResolveStateStore_NoIaCStateModule(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "test.yaml")
	if err := os.WriteFile(cfgPath, []byte(`modules:
  - name: other
    type: http.server
    config: {}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := wfctlhelpers.ResolveStateStore(cfgPath, "", "")
	if err != nil {
		t.Fatalf("ResolveStateStore: %v", err)
	}
	if store == nil {
		t.Fatal("ResolveStateStore returned nil for missing iac.state module")
	}
	resources, err := store.ListResources(context.Background())
	if err != nil {
		t.Fatalf("ListResources on noop store: %v", err)
	}
	if len(resources) != 0 {
		t.Errorf("noop store ListResources: expected 0, got %d", len(resources))
	}
}

// TestResolveStateStore_FilesystemBackend verifies the lifted helper builds
// a filesystem-backed store when backend: filesystem is configured. The
// directory is read from config.directory.
func TestResolveStateStore_FilesystemBackend(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "iac-state")
	cfgPath := filepath.Join(dir, "test.yaml")
	if err := os.WriteFile(cfgPath, []byte(`modules:
  - name: iac-state
    type: iac.state
    config:
      backend: filesystem
      directory: `+stateDir+`
`), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := wfctlhelpers.ResolveStateStore(cfgPath, "", "")
	if err != nil {
		t.Fatalf("ResolveStateStore: %v", err)
	}
	// Round-trip ensures the directory is created on first write.
	state := interfaces.ResourceState{
		ID:       "vpc-fs",
		Name:     "vpc-fs",
		Type:     "infra.vpc",
		Provider: "stub",
	}
	if err := store.SaveResource(context.Background(), state); err != nil {
		t.Fatalf("SaveResource (filesystem): %v", err)
	}
	list, err := store.ListResources(context.Background())
	if err != nil {
		t.Fatalf("ListResources (filesystem): %v", err)
	}
	if len(list) != 1 || list[0].Name != "vpc-fs" {
		t.Errorf("ListResources got %+v, want one vpc-fs", list)
	}
}

// TestResolveStateStore_UnknownBackend returns a clear error for unknown
// backends so config typos don't silently fall back to filesystem.
func TestResolveStateStore_UnknownBackend(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "test.yaml")
	if err := os.WriteFile(cfgPath, []byte(`modules:
  - name: iac-state
    type: iac.state
    config:
      backend: not-a-real-backend
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := wfctlhelpers.ResolveStateStore(cfgPath, "", ""); err == nil {
		t.Fatal("expected error for unknown backend, got nil")
	}
}
