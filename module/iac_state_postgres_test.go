package module_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/module"
)

// mockPGConn implements module.PostgresConn for testing without a real database.
type mockPGConn struct {
	rows  map[string]*module.IaCState // resource_id -> state
	locks map[string]bool
}

func newMockPGConn() *mockPGConn {
	return &mockPGConn{
		rows:  make(map[string]*module.IaCState),
		locks: make(map[string]bool),
	}
}

func (m *mockPGConn) UpsertState(_ context.Context, st *module.IaCState) error {
	if st == nil {
		return fmt.Errorf("state is nil")
	}
	if st.ResourceID == "" {
		return fmt.Errorf("resource_id is empty")
	}
	cp := *st
	m.rows[st.ResourceID] = &cp
	return nil
}

func (m *mockPGConn) GetState(_ context.Context, name string) (*module.IaCState, error) {
	st, ok := m.rows[name]
	if !ok {
		return nil, nil
	}
	cp := *st
	return &cp, nil
}

func (m *mockPGConn) ListRows(_ context.Context) ([]*module.IaCState, error) {
	var results []*module.IaCState
	for _, st := range m.rows {
		cp := *st
		results = append(results, &cp)
	}
	return results, nil
}

func (m *mockPGConn) DeleteRow(_ context.Context, name string) (bool, error) {
	if _, ok := m.rows[name]; !ok {
		return false, nil
	}
	delete(m.rows, name)
	return true, nil
}

func (m *mockPGConn) AcquireAdvisoryLock(_ context.Context, key int64) error {
	k := fmt.Sprint(key)
	if m.locks[k] {
		return fmt.Errorf("already locked: %d", key)
	}
	m.locks[k] = true
	return nil
}

func (m *mockPGConn) ReleaseAdvisoryLock(_ context.Context, key int64) (bool, error) {
	k := fmt.Sprint(key)
	if !m.locks[k] {
		return false, nil
	}
	delete(m.locks, k)
	return true, nil
}

func (m *mockPGConn) Close() {}

func newTestPostgresStore(conn module.PostgresConn) *module.PostgresIaCStateStore {
	return module.NewPostgresIaCStateStoreWithConn(conn)
}

func TestPostgresIaCStateStore_GetState_NotFound(t *testing.T) {
	store := newTestPostgresStore(newMockPGConn())
	st, err := store.GetState("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if st != nil {
		t.Fatalf("expected nil, got %+v", st)
	}
}

func TestPostgresIaCStateStore_SaveAndGetState(t *testing.T) {
	store := newTestPostgresStore(newMockPGConn())

	state := &module.IaCState{
		ResourceID:   "pg-cluster",
		ResourceType: "kubernetes",
		Provider:     "aws",
		Status:       "active",
		Config:       map[string]any{"region": "us-east-1"},
		Outputs:      map[string]any{"endpoint": "https://k8s.example.com"},
	}
	if err := store.SaveState(state); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	got, err := store.GetState("pg-cluster")
	if err != nil {
		t.Fatalf("GetState: %v", err)
	}
	if got == nil {
		t.Fatal("expected state, got nil")
	}
	if got.Provider != "aws" {
		t.Errorf("Provider = %q, want %q", got.Provider, "aws")
	}
	if got.Status != "active" {
		t.Errorf("Status = %q, want %q", got.Status, "active")
	}
}

func TestPostgresIaCStateStore_SaveState_Nil(t *testing.T) {
	store := newTestPostgresStore(newMockPGConn())
	if err := store.SaveState(nil); err == nil {
		t.Fatal("expected error for nil state")
	}
}

func TestPostgresIaCStateStore_SaveState_EmptyID(t *testing.T) {
	store := newTestPostgresStore(newMockPGConn())
	if err := store.SaveState(&module.IaCState{}); err == nil {
		t.Fatal("expected error for empty resource_id")
	}
}

func TestPostgresIaCStateStore_ListStates(t *testing.T) {
	conn := newMockPGConn()
	store := newTestPostgresStore(conn)

	states := []*module.IaCState{
		{ResourceID: "r1", ResourceType: "k8s", Provider: "aws", Status: "active"},
		{ResourceID: "r2", ResourceType: "db", Provider: "gcp", Status: "active"},
		{ResourceID: "r3", ResourceType: "k8s", Provider: "aws", Status: "destroyed"},
	}
	for _, st := range states {
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

	filtered, err := store.ListStates(map[string]string{"provider": "aws"})
	if err != nil {
		t.Fatalf("ListStates(provider=aws): %v", err)
	}
	if len(filtered) != 2 {
		t.Errorf("ListStates(provider=aws) = %d, want 2", len(filtered))
	}
}

func TestPostgresIaCStateStore_DeleteState(t *testing.T) {
	store := newTestPostgresStore(newMockPGConn())

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

func TestPostgresIaCStateStore_DeleteState_NotFound(t *testing.T) {
	store := newTestPostgresStore(newMockPGConn())
	if err := store.DeleteState("nonexistent"); err == nil {
		t.Fatal("expected error deleting nonexistent state")
	}
}

func TestPostgresIaCStateStore_LockUnlock(t *testing.T) {
	store := newTestPostgresStore(newMockPGConn())

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

func TestPostgresIaCStateStore_Unlock_NotLocked(t *testing.T) {
	store := newTestPostgresStore(newMockPGConn())
	if err := store.Unlock("not-locked"); err == nil {
		t.Fatal("expected error unlocking non-locked resource")
	}
}

func TestPostgresIaCStateStore_Schema_HasProviderID(t *testing.T) {
	if !strings.Contains(module.CreateTableSQL, "provider_id") {
		t.Error("createTableSQL is missing provider_id column (required by spec)")
	}
}
