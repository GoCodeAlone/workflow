package module_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/module"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

func makeState(id, rtype, provider, status string) *module.IaCState {
	return &module.IaCState{
		ResourceID:   id,
		ResourceType: rtype,
		Provider:     provider,
		Status:       status,
		Outputs:      map[string]any{"endpoint": "https://example.com"},
		Config:       map[string]any{"version": "1.29"},
		CreatedAt:    "2026-01-01T00:00:00Z",
		UpdatedAt:    "2026-01-01T00:00:00Z",
	}
}

// runStateStoreSuite runs the shared CRUD + lock tests against any IaCStateStore.
func runStateStoreSuite(t *testing.T, store module.IaCStateStore) {
	t.Helper()

	// ── SaveState / GetState ──────────────────────────────────────────────────

	t.Run("SaveAndGet", func(t *testing.T) {
		st := makeState("res-1", "kubernetes", "local", "planned")
		if err := store.SaveState(context.Background(), st); err != nil {
			t.Fatalf("SaveState: %v", err)
		}
		got, err := store.GetState(context.Background(), "res-1")
		if err != nil {
			t.Fatalf("GetState: %v", err)
		}
		if got == nil {
			t.Fatal("GetState returned nil for saved state")
		}
		if got.Status != "planned" {
			t.Errorf("expected status=planned, got %q", got.Status)
		}
		if got.ResourceType != "kubernetes" {
			t.Errorf("expected resource_type=kubernetes, got %q", got.ResourceType)
		}
	})

	t.Run("GetNotFound", func(t *testing.T) {
		got, err := store.GetState(context.Background(), "nonexistent")
		if err != nil {
			t.Fatalf("GetState unexpected error: %v", err)
		}
		if got != nil {
			t.Error("expected nil for nonexistent resource, got non-nil")
		}
	})

	t.Run("SaveState_NilError", func(t *testing.T) {
		if err := store.SaveState(context.Background(), nil); err == nil {
			t.Error("expected error for nil state, got nil")
		}
	})

	t.Run("SaveState_EmptyIDError", func(t *testing.T) {
		st := &module.IaCState{ResourceID: "", Status: "planned"}
		if err := store.SaveState(context.Background(), st); err == nil {
			t.Error("expected error for empty resource_id, got nil")
		}
	})

	t.Run("UpdateState", func(t *testing.T) {
		st := makeState("res-update", "kubernetes", "local", "planned")
		if err := store.SaveState(context.Background(), st); err != nil {
			t.Fatalf("SaveState: %v", err)
		}
		st.Status = "active"
		if err := store.SaveState(context.Background(), st); err != nil {
			t.Fatalf("SaveState update: %v", err)
		}
		got, _ := store.GetState(context.Background(), "res-update")
		if got.Status != "active" {
			t.Errorf("expected status=active after update, got %q", got.Status)
		}
	})

	// ── ListStates ────────────────────────────────────────────────────────────

	t.Run("ListAll", func(t *testing.T) {
		// Seed two distinct resources.
		_ = store.SaveState(context.Background(), makeState("list-a", "kubernetes", "aws", "active"))
		_ = store.SaveState(context.Background(), makeState("list-b", "ecs", "aws", "planned"))

		all, err := store.ListStates(context.Background(), map[string]string{})
		if err != nil {
			t.Fatalf("ListStates: %v", err)
		}
		if len(all) < 2 {
			t.Errorf("expected at least 2 states, got %d", len(all))
		}
	})

	t.Run("ListByStatus", func(t *testing.T) {
		_ = store.SaveState(context.Background(), makeState("filter-active", "kubernetes", "gcp", "active"))
		_ = store.SaveState(context.Background(), makeState("filter-destroyed", "kubernetes", "gcp", "destroyed"))

		active, err := store.ListStates(context.Background(), map[string]string{"status": "active"})
		if err != nil {
			t.Fatalf("ListStates by status: %v", err)
		}
		for _, s := range active {
			if s.Status != "active" {
				t.Errorf("filter returned unexpected status %q", s.Status)
			}
		}
	})

	t.Run("ListByProvider", func(t *testing.T) {
		_ = store.SaveState(context.Background(), makeState("prov-aws", "kubernetes", "aws", "active"))
		_ = store.SaveState(context.Background(), makeState("prov-gcp", "kubernetes", "gcp", "active"))

		awsOnly, err := store.ListStates(context.Background(), map[string]string{"provider": "aws"})
		if err != nil {
			t.Fatalf("ListStates by provider: %v", err)
		}
		for _, s := range awsOnly {
			if s.Provider != "aws" {
				t.Errorf("filter returned unexpected provider %q", s.Provider)
			}
		}
	})

	// ── DeleteState ───────────────────────────────────────────────────────────

	t.Run("DeleteState", func(t *testing.T) {
		_ = store.SaveState(context.Background(), makeState("del-me", "kubernetes", "local", "active"))
		if err := store.DeleteState(context.Background(), "del-me"); err != nil {
			t.Fatalf("DeleteState: %v", err)
		}
		got, _ := store.GetState(context.Background(), "del-me")
		if got != nil {
			t.Error("expected nil after delete, got non-nil")
		}
	})

	t.Run("DeleteNotFound", func(t *testing.T) {
		if err := store.DeleteState(context.Background(), "ghost-resource"); err == nil {
			t.Error("expected error for nonexistent resource, got nil")
		}
	})

	// ── Lock / Unlock ─────────────────────────────────────────────────────────

	t.Run("LockAndUnlock", func(t *testing.T) {
		if err := store.Lock(context.Background(), "lock-res"); err != nil {
			t.Fatalf("Lock: %v", err)
		}
		if err := store.Unlock(context.Background(), "lock-res"); err != nil {
			t.Fatalf("Unlock: %v", err)
		}
	})

	t.Run("DoubleLock", func(t *testing.T) {
		if err := store.Lock(context.Background(), "double-lock"); err != nil {
			t.Fatalf("first Lock: %v", err)
		}
		// Second lock must fail.
		if err := store.Lock(context.Background(), "double-lock"); err == nil {
			t.Error("expected error on double-lock, got nil")
		}
		// Clean up.
		_ = store.Unlock(context.Background(), "double-lock")
	})

	t.Run("UnlockNotLocked", func(t *testing.T) {
		if err := store.Unlock(context.Background(), "never-locked"); err == nil {
			t.Error("expected error unlocking a non-locked resource, got nil")
		}
	})
}

// ─── Memory store tests ───────────────────────────────────────────────────────

func TestIaCStateStore_Memory(t *testing.T) {
	store := module.NewMemoryIaCStateStore()
	runStateStoreSuite(t, store)
}

// ─── Filesystem store tests ───────────────────────────────────────────────────

func TestIaCStateStore_Filesystem(t *testing.T) {
	dir := t.TempDir()
	store := module.NewFSIaCStateStore(dir)
	runStateStoreSuite(t, store)
}

func TestIaCStateStore_Filesystem_PersistAcrossInstances(t *testing.T) {
	dir := t.TempDir()

	st := makeState("persist-res", "kubernetes", "local", "active")
	store1 := module.NewFSIaCStateStore(dir)
	if err := store1.SaveState(context.Background(), st); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	// New store instance pointing at the same directory.
	store2 := module.NewFSIaCStateStore(dir)
	got, err := store2.GetState(context.Background(), "persist-res")
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if got == nil || got.Status != "active" {
		t.Errorf("expected persisted status=active, got %v", got)
	}
}

func TestIaCStateStore_Filesystem_JSONFiles(t *testing.T) {
	dir := t.TempDir()
	store := module.NewFSIaCStateStore(dir)

	if err := store.SaveState(context.Background(), makeState("json-check", "ecs", "aws", "planned")); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	found := false
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".json" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected at least one .json file in state directory")
	}
}

func TestIaCModule_MemoryBackend(t *testing.T) {
	m := module.NewIaCModule("iac-mem", map[string]any{"backend": "memory"})
	app := module.NewMockApplication()
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	svc, ok := app.Services["iac-mem"]
	if !ok {
		t.Fatal("expected iac-mem in service registry")
	}
	if _, ok := svc.(module.IaCStateStore); !ok {
		t.Fatalf("service is %T, want IaCStateStore", svc)
	}
}

func TestIaCModule_FilesystemBackend(t *testing.T) {
	dir := t.TempDir()
	m := module.NewIaCModule("iac-fs", map[string]any{
		"backend":   "filesystem",
		"directory": dir,
	})
	app := module.NewMockApplication()
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	svc, ok := app.Services["iac-fs"]
	if !ok {
		t.Fatal("expected iac-fs in service registry")
	}
	if _, ok := svc.(module.IaCStateStore); !ok {
		t.Fatalf("service is %T, want IaCStateStore", svc)
	}
}

func TestIaCModule_InvalidBackend(t *testing.T) {
	m := module.NewIaCModule("bad", map[string]any{"backend": "s3"})
	if err := m.Init(module.NewMockApplication()); err == nil {
		t.Error("expected error for unsupported backend, got nil")
	}
}

// TestIaCModuleAzureBlobRequiresPlugin asserts that backend: azure_blob with no
// plugin registered returns the plugin-guidance error — the in-core azure_blob
// backend has been removed; it is now served by workflow-plugin-azure.
func TestIaCModuleAzureBlobRequiresPlugin(t *testing.T) {
	m := module.NewIaCModule("st", map[string]any{
		"backend":      "azure_blob",
		"container":    "c",
		"account_url":  "https://x",
		"account_name": "n",
		"account_key":  "k",
	})
	err := m.Init(module.NewMockApplication())
	if err == nil {
		t.Fatal("azure_blob with no plugin loaded must error — in-core backend is gone")
	}
	if !strings.Contains(err.Error(), "azure_blob") {
		t.Fatalf("error should name the missing backend: %v", err)
	}
	// The error must be the plugin-guidance error, NOT an in-core construction
	// failure — the in-core azure_blob backend has been deleted.
	if !strings.Contains(err.Error(), "plugin") {
		t.Fatalf("error should be the plugin-guidance error (mention loading a plugin), got: %v", err)
	}
}
