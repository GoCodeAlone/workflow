package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/iac/wfctlhelpers"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/module"
)

func cleanupPendingState() interfaces.ResourceState {
	return interfaces.ResourceState{
		ID: "database", Name: "database", Type: "infra.database", Provider: "test-cloud", ProviderID: "cloud-id",
		AppliedConfig: map[string]any{"password": "${TOKEN}"}, Outputs: map[string]any{"password": "secret_ref://database_password"},
		Lifecycle: &interfaces.ResourceLifecycle{Generation: "generation-1", Phase: interfaces.ResourcePhaseCloudDeletedSecretCleanupPending,
			Secrets: []interfaces.RoutedSecretReference{{Key: "database_password", Store: "named-store", Provider: "file", Scope: "directory", Subject: "secret-dir"}, {Key: "alias", VerifiedAbsent: true}}},
	}
}

func cleanupNativeSavers(dir string) map[string]func(context.Context, interfaces.ResourceState) error {
	return map[string]func(context.Context, interfaces.ResourceState) error{
		"legacy CLI": (&fsWfctlStateStore{dir: dir}).SaveResource,
		"shared helper": func(ctx context.Context, state interfaces.ResourceState) error {
			store, err := wfctlhelpers.ResolveStateStore(filepath.Join(dir, "config.yaml"), "", "")
			if err != nil {
				return err
			}
			defer store.Close()
			return store.SaveResource(ctx, state)
		},
		"native module": func(ctx context.Context, state interfaces.ResourceState) error {
			return module.NewFSIaCStateStore(dir).SaveState(ctx, resourceStateToIaCState(state))
		},
	}
}

func writeCleanupStateConfig(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("modules:\n  - name: state\n    type: iac.state\n    config: {backend: filesystem, directory: "+dir+"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCleanupPending_LegacyAndNativeCompatibility(t *testing.T) {
	for _, source := range []string{"legacy CLI", "shared helper", "native module"} {
		t.Run(source, func(t *testing.T) {
			dir := t.TempDir()
			writeCleanupStateConfig(t, dir)
			state := cleanupPendingState()
			if err := cleanupNativeSavers(dir)[source](t.Context(), state); err != nil {
				t.Fatal(err)
			}
			legacy, err := (&fsWfctlStateStore{dir: dir}).ListResources(t.Context())
			if err != nil || len(legacy) != 1 || !reflect.DeepEqual(legacy[0].Lifecycle, state.Lifecycle) {
				t.Fatalf("legacy reader lost lifecycle: states=%v err=%v", legacy, err)
			}
			helper, err := wfctlhelpers.ResolveStateStore(filepath.Join(dir, "config.yaml"), "", "")
			if err != nil {
				t.Fatal(err)
			}
			defer helper.Close()
			got, err := helper.GetResource(t.Context(), state.ID)
			if err != nil || got == nil || !reflect.DeepEqual(got.Lifecycle, state.Lifecycle) {
				t.Fatalf("shared reader lost lifecycle: state=%v err=%v", got, err)
			}
			converted := iacStateToResourceState(resourceStateToIaCState(state))
			if !reflect.DeepEqual(converted.Lifecycle, state.Lifecycle) {
				t.Fatal("module converters lost cleanup identifiers")
			}
			data, err := os.ReadFile(filepath.Join(dir, "database.json"))
			if err != nil || strings.Contains(string(data), "known-private-cleanup-value") {
				t.Fatalf("state plaintext absence: err=%v", err)
			}
			var native module.IaCState
			if err := json.Unmarshal(data, &native); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(iacStateToResourceState(&native).Lifecycle, state.Lifecycle) {
				t.Fatal("native decoder lost cleanup journal")
			}
		})
	}
}

func TestCleanupPending_StateFileAtomicReplacement(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix hard-link and mode invariant")
	}
	for _, source := range []string{"legacy CLI", "shared helper", "native module"} {
		t.Run(source, func(t *testing.T) {
			dir := t.TempDir()
			writeCleanupStateConfig(t, dir)
			path := filepath.Join(dir, "database.json")
			old := []byte(`{"resource_id":"database","status":"active"}`)
			if err := os.WriteFile(path, old, 0o644); err != nil {
				t.Fatal(err)
			}
			alias := filepath.Join(dir, "previous-record")
			if err := os.Link(path, alias); err != nil {
				t.Fatal(err)
			}
			if err := cleanupNativeSavers(dir)[source](t.Context(), cleanupPendingState()); err != nil {
				t.Fatal(err)
			}
			previous, err := os.ReadFile(alias)
			if err != nil || string(previous) != string(old) {
				t.Fatalf("save modified the old inode instead of atomically replacing it: err=%v", err)
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("replacement file must be 0600: info=%v err=%v", info, err)
			}
			if err := os.WriteFile(filepath.Join(dir, ".iac-state-crash"), []byte(`{"resource_id":"uncommitted-temp"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			states, err := (&fsWfctlStateStore{dir: dir}).ListResources(t.Context())
			if err != nil || len(states) != 1 || states[0].ID != "database" {
				t.Fatalf("temporary state became a resource: states=%v err=%v", states, err)
			}
			native, err := module.NewFSIaCStateStore(dir).ListStates(t.Context(), nil)
			if err != nil || len(native) != 1 || native[0].ResourceID != "database" {
				t.Fatalf("native list exposed temporary state: states=%v err=%v", native, err)
			}
		})
	}
}
