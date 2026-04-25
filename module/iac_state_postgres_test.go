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

type scanErrRows struct {
	scanned bool
}

type scanSuccessRows struct {
	scanned bool
}

type scanBadJSONRows struct {
	scanned bool
	cfgJSON string
	outJSON string
}

func (r *scanErrRows) Next() bool {
	if r.scanned {
		return false
	}
	r.scanned = true
	return true
}

func (r *scanErrRows) Scan(_ ...any) error {
	return fmt.Errorf("bad row")
}

func (r *scanErrRows) Err() error { return nil }

func (r *scanSuccessRows) Next() bool {
	if r.scanned {
		return false
	}
	r.scanned = true
	return true
}

func (r *scanSuccessRows) Scan(dest ...any) error {
	if len(dest) != 9 {
		return fmt.Errorf("dest len = %d, want 9", len(dest))
	}
	*(dest[0].(*string)) = "site-dns"
	*(dest[1].(*string)) = "infra.dns"
	*(dest[2].(*string)) = "digitalocean"
	*(dest[3].(*string)) = "do-prod"
	*(dest[4].(*string)) = "example.com"
	*(dest[5].(*string)) = "hash-123"
	*(dest[6].(*string)) = "active"
	*(dest[7].(*string)) = `{"domain":"example.com"}`
	*(dest[8].(*string)) = `{"records":[]}`
	return nil
}

func (r *scanSuccessRows) Err() error { return nil }

func (r *scanBadJSONRows) Next() bool {
	if r.scanned {
		return false
	}
	r.scanned = true
	return true
}

func (r *scanBadJSONRows) Scan(dest ...any) error {
	if len(dest) != 9 {
		return fmt.Errorf("dest len = %d, want 9", len(dest))
	}
	*(dest[0].(*string)) = "site-dns"
	*(dest[1].(*string)) = "infra.dns"
	*(dest[2].(*string)) = "digitalocean"
	*(dest[3].(*string)) = "do-prod"
	*(dest[4].(*string)) = "example.com"
	*(dest[5].(*string)) = "hash-123"
	*(dest[6].(*string)) = "active"
	*(dest[7].(*string)) = r.cfgJSON
	*(dest[8].(*string)) = r.outJSON
	return nil
}

func (r *scanBadJSONRows) Err() error { return nil }

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
		ProviderRef:  "aws-provider",
		ProviderID:   "provider-cluster-123",
		ConfigHash:   "config-hash-123",
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
	if got.ProviderRef != "aws-provider" {
		t.Errorf("ProviderRef = %q, want %q", got.ProviderRef, "aws-provider")
	}
	if got.ProviderID != "provider-cluster-123" {
		t.Errorf("ProviderID = %q, want %q", got.ProviderID, "provider-cluster-123")
	}
	if got.ConfigHash != "config-hash-123" {
		t.Errorf("ConfigHash = %q, want %q", got.ConfigHash, "config-hash-123")
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
		{ResourceID: "r1", ResourceType: "k8s", Provider: "aws", ProviderRef: "aws-east", Status: "active"},
		{ResourceID: "r2", ResourceType: "db", Provider: "gcp", ProviderRef: "gcp-main", Status: "active"},
		{ResourceID: "r3", ResourceType: "k8s", Provider: "aws", ProviderRef: "aws-west", Status: "destroyed"},
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
	providerRefs := map[string]bool{}
	for _, st := range filtered {
		providerRefs[st.ProviderRef] = true
	}
	for _, want := range []string{"aws-east", "aws-west"} {
		if !providerRefs[want] {
			t.Errorf("filtered ProviderRefs = %#v, want %q", providerRefs, want)
		}
	}
}

func TestScanIaCStateRows_ReturnsScanError(t *testing.T) {
	_, err := module.ScanIaCStateRowsForTest(&scanErrRows{})
	if err == nil {
		t.Fatal("expected scan error, got nil")
	}
	if !strings.Contains(err.Error(), "scan iac_resources row") {
		t.Fatalf("error = %q, want scan iac_resources row", err)
	}
}

func TestScanIaCStateRows_PopulatesProviderRef(t *testing.T) {
	states, err := module.ScanIaCStateRowsForTest(&scanSuccessRows{})
	if err != nil {
		t.Fatalf("ScanIaCStateRowsForTest: %v", err)
	}
	if len(states) != 1 {
		t.Fatalf("states = %d, want 1", len(states))
	}
	got := states[0]
	if got.ResourceID != "site-dns" || got.Provider != "digitalocean" || got.ProviderRef != "do-prod" || got.ProviderID != "example.com" {
		t.Fatalf("state = %#v, want provider_ref and provider_id populated", got)
	}
	if got.Config["domain"] != "example.com" {
		t.Fatalf("Config = %#v, want domain", got.Config)
	}
}

func TestScanIaCStateRows_ReturnsAppliedConfigDecodeError(t *testing.T) {
	_, err := module.ScanIaCStateRowsForTest(&scanBadJSONRows{cfgJSON: `{`, outJSON: `{}`})
	if err == nil {
		t.Fatal("expected applied_config decode error, got nil")
	}
	if !strings.Contains(err.Error(), "applied_config") {
		t.Fatalf("error = %q, want applied_config context", err)
	}
}

func TestScanIaCStateRows_ReturnsOutputsDecodeError(t *testing.T) {
	_, err := module.ScanIaCStateRowsForTest(&scanBadJSONRows{cfgJSON: `{}`, outJSON: `{`})
	if err == nil {
		t.Fatal("expected outputs decode error, got nil")
	}
	if !strings.Contains(err.Error(), "outputs") {
		t.Fatalf("error = %q, want outputs context", err)
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
	if !strings.Contains(module.CreateTableSQL, "provider_ref") {
		t.Error("createTableSQL is missing provider_ref column (required by provider instance scoping)")
	}
	if !strings.Contains(module.CreateTableSQL, "provider_id") {
		t.Error("createTableSQL is missing provider_id column (required by spec)")
	}
	if !strings.Contains(module.CreateTableSQL, "config_hash") {
		t.Error("createTableSQL is missing config_hash column (required by spec)")
	}
}

func TestPostgresIaCStateStore_Migration_AddsColumnsForExistingPreChangeSchema(t *testing.T) {
	legacySchemaColumns := map[string]bool{
		"name":           true,
		"type":           true,
		"status":         true,
		"applied_config": true,
		"outputs":        true,
		"created_at":     true,
		"updated_at":     true,
	}
	newColumns := []string{"provider", "provider_ref", "provider_id", "config_hash", "dependencies"}

	for _, col := range newColumns {
		if legacySchemaColumns[col] {
			t.Fatalf("test setup invalid: %s already exists in legacy schema", col)
		}
		found := false
		for _, stmt := range module.MigrateTableSQL {
			fields := strings.Fields(stmt)
			if len(fields) >= 9 &&
				strings.EqualFold(fields[0], "ALTER") &&
				strings.EqualFold(fields[1], "TABLE") &&
				fields[2] == "iac_resources" &&
				strings.EqualFold(fields[3], "ADD") &&
				strings.EqualFold(fields[4], "COLUMN") &&
				strings.EqualFold(fields[5], "IF") &&
				strings.EqualFold(fields[6], "NOT") &&
				strings.EqualFold(fields[7], "EXISTS") &&
				fields[8] == col {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("MigrateTableSQL missing additive migration for %q", col)
		}
	}
}
