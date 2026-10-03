package module_test

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/module"
)

func cleanupLifecycle() *interfaces.ResourceLifecycle {
	return &interfaces.ResourceLifecycle{
		Generation: "generation-1", Phase: interfaces.ResourcePhaseCloudDeletedSecretCleanupPending,
		Secrets: []interfaces.RoutedSecretReference{
			{Key: "database_password", Store: "named-store", Provider: "file", Scope: "directory", Subject: "secret-dir"},
			{Key: "named-alias", Provider: "env", Scope: "process", Subject: "workflow", VerifiedAbsent: true},
		},
	}
}

func stateWithCleanupLifecycle(t *testing.T, lifecycle *interfaces.ResourceLifecycle) *module.IaCState {
	t.Helper()
	data, err := json.Marshal(struct {
		*module.IaCState
		Lifecycle *interfaces.ResourceLifecycle `json:"lifecycle,omitempty"`
	}{makeState("database", "infra.database", "test-cloud", "active"), lifecycle})
	if err != nil {
		t.Fatal(err)
	}
	var state module.IaCState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	return &state
}

func assertCleanupLifecycle(t *testing.T, state *module.IaCState, want *interfaces.ResourceLifecycle) {
	t.Helper()
	if state == nil {
		t.Fatal("state was lost")
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	var record struct {
		Lifecycle *interfaces.ResourceLifecycle `json:"lifecycle"`
	}
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(record.Lifecycle, want) {
		t.Fatalf("cleanup lifecycle changed: got=%+v want=%+v", record.Lifecycle, want)
	}
	if strings.Contains(string(data), "known-private-cleanup-value") {
		t.Fatal("cleanup metadata stored credential plaintext")
	}
}

func TestCleanupPending_NativeStateRoundTrip(t *testing.T) {
	for _, backend := range []string{"filesystem", "memory"} {
		t.Run(backend, func(t *testing.T) {
			dir := t.TempDir()
			var store module.IaCStateStore = module.NewMemoryIaCStateStore()
			if backend == "filesystem" {
				store = module.NewFSIaCStateStore(dir)
			}
			for _, phase := range []interfaces.ResourcePhase{interfaces.ResourcePhaseActive, interfaces.ResourcePhaseSecretRoutingPending, interfaces.ResourcePhaseCloudDeletePending, interfaces.ResourcePhaseCloudDeletedSecretCleanupPending} {
				lifecycle := cleanupLifecycle()
				lifecycle.Phase = phase
				lifecycle.RoutingCreated = phase == interfaces.ResourcePhaseSecretRoutingPending
				if err := store.SaveState(t.Context(), stateWithCleanupLifecycle(t, lifecycle)); err != nil {
					t.Fatal(err)
				}
				if backend == "filesystem" {
					store = module.NewFSIaCStateStore(dir)
				}
				got, err := store.GetState(t.Context(), "database")
				if err != nil {
					t.Fatal(err)
				}
				assertCleanupLifecycle(t, got, lifecycle)
				listed, err := store.ListStates(t.Context(), nil)
				if err != nil || len(listed) != 1 {
					t.Fatalf("list state: len=%d err=%v", len(listed), err)
				}
				assertCleanupLifecycle(t, listed[0], lifecycle)
			}
			if err := store.SaveState(t.Context(), stateWithCleanupLifecycle(t, nil)); err != nil {
				t.Fatal(err)
			}
			legacy, err := store.GetState(t.Context(), "database")
			if err != nil {
				t.Fatal(err)
			}
			assertCleanupLifecycle(t, legacy, nil)
		})
	}
}

func TestCleanupPending_MemoryLifecycleMutationIsolation(t *testing.T) {
	store := module.NewMemoryIaCStateStore()
	state := stateWithCleanupLifecycle(t, cleanupLifecycle())
	if err := store.SaveState(t.Context(), state); err != nil {
		t.Fatal(err)
	}
	state.Lifecycle.Generation = "caller-change"
	state.Lifecycle.Secrets[0].Key = "caller-change"
	state.Lifecycle.Secrets[1].VerifiedAbsent = false
	got, err := store.GetState(t.Context(), "database")
	if err != nil {
		t.Fatal(err)
	}
	assertCleanupLifecycle(t, got, cleanupLifecycle())
	got.Lifecycle.Phase = interfaces.ResourcePhaseActive
	got.Lifecycle.Secrets[0].VerifiedAbsent = true
	listed, err := store.ListStates(t.Context(), nil)
	if err != nil || len(listed) != 1 {
		t.Fatalf("list: len=%d err=%v", len(listed), err)
	}
	assertCleanupLifecycle(t, listed[0], cleanupLifecycle())
	listed[0].Lifecycle.Secrets[0].Subject = "list-caller-change"
	got, err = store.GetState(t.Context(), "database")
	if err != nil {
		t.Fatal(err)
	}
	assertCleanupLifecycle(t, got, cleanupLifecycle())
	if got.Config["version"] != "1.29" || got.Status != "active" {
		t.Fatal("lifecycle copy changed existing state semantics")
	}
}

type cleanupRows struct {
	visited bool
	payload string
}

func (r *cleanupRows) Next() bool {
	if r.visited {
		return false
	}
	r.visited = true
	return true
}

func (r *cleanupRows) Scan(dest ...any) error {
	if len(dest) != 11 {
		return fmt.Errorf("lifecycle column missing from actual row scan: got %d columns, want 11", len(dest))
	}
	values := []string{"database", "infra.database", "test-cloud", "cloud-provider", "cloud-id", "declarative-hash", "active", `{"password":"${TOKEN}"}`, `{"password":"secret_ref://database_password"}`}
	for i, value := range values {
		*(dest[i].(*string)) = value
	}
	*(dest[9].(*[]string)) = []string{"network"}
	*(dest[10].(*string)) = r.payload
	return nil
}

func (*cleanupRows) Err() error { return nil }

func TestCleanupPending_PostgresRowDecode(t *testing.T) {
	payload, err := json.Marshal(cleanupLifecycle())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		payload string
		want    *interfaces.ResourceLifecycle
		bad     bool
	}{
		{"pending", string(payload), cleanupLifecycle(), false},
		{"legacy null", "null", nil, false},
		{"malformed", `{"generation":`, nil, true},
		{"wrong shape", `"known-private-cleanup-value"`, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			states, err := module.ScanIaCStateRowsForTest(&cleanupRows{payload: tc.payload})
			if tc.bad {
				if err == nil || len(states) != 0 || !strings.Contains(err.Error(), "decode iac_resources") || !strings.Contains(err.Error(), "lifecycle") || strings.Contains(err.Error(), "known-private-cleanup-value") {
					t.Fatalf("invalid lifecycle did not fail closed with safe diagnostics: states=%v err=%v", states, err)
				}
				return
			}
			if err != nil || len(states) != 1 {
				t.Fatalf("decode lifecycle: states=%v err=%v", states, err)
			}
			assertCleanupLifecycle(t, states[0], tc.want)
			if states[0].ProviderRef != "cloud-provider" || states[0].Outputs["password"] != "secret_ref://database_password" {
				t.Fatal("lifecycle decode changed sibling state fields")
			}
		})
	}
}

func TestCleanupPending_LifecycleHasIdentifiersNotValues(t *testing.T) {
	state := stateWithCleanupLifecycle(t, cleanupLifecycle())
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]json.RawMessage
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	var lifecycle map[string]json.RawMessage
	if err := json.Unmarshal(record["lifecycle"], &lifecycle); err != nil {
		t.Fatal(err)
	}
	lifecycle["credential_value"] = json.RawMessage(`"known-private-cleanup-value"`)
	record["lifecycle"], err = json.Marshal(lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	data, err = json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var decoded module.IaCState
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	store := module.NewFSIaCStateStore(t.TempDir())
	if err := store.SaveState(t.Context(), &decoded); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetState(t.Context(), state.ResourceID)
	if err != nil {
		t.Fatal(err)
	}
	assertCleanupLifecycle(t, got, cleanupLifecycle())
}

func TestCleanupPending_PostgresSchema(t *testing.T) {
	if !strings.Contains(strings.Join(strings.Fields(module.CreateTableSQL), " "), "lifecycle JSONB") {
		t.Fatal("new tables have no lifecycle JSONB column")
	}
	columns := []string{"name", "type", "provider", "provider_ref", "provider_id", "status", "config_hash", "applied_config", "outputs", "dependencies", "created_at", "updated_at"}
	statements := module.MigrateStatementsForExistingColumnsForTest(columns)
	if len(statements) != 1 || statements[0] != "ALTER TABLE iac_resources ADD COLUMN IF NOT EXISTS lifecycle JSONB" {
		t.Fatalf("legacy schema lifecycle migration is not additive: %v", statements)
	}
	if statements := module.MigrateStatementsForExistingColumnsForTest(append(columns, "lifecycle")); len(statements) != 0 {
		t.Fatalf("up-to-date schema still needs migration: %v", statements)
	}
}
