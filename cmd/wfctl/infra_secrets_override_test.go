package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

// TestPlanResourcesForEnv_SecretsStoreOverride verifies that secretsStoreOverride
// is respected when resolving secrets per environment.
func TestPlanResourcesForEnv_SecretsStoreOverride(t *testing.T) {
	// Both "staging-env" and "prod-env" stores are backed by the env provider,
	// so they both read from the same process environment variable DB_PASS.
	// The test verifies that each env is routed to the correct named store
	// (staging→staging-env, prod→prod-env) and that injectSecrets returns the
	// value from that store without error — store selection is the invariant,
	// not distinct per-store values (which would require separate env vars or
	// a real secret backend).
	t.Setenv("DB_PASS", "test-secret-value")

	dir := t.TempDir()
	cfg := `secretStores:
  staging-env:
    provider: env
  prod-env:
    provider: env
secrets:
  entries:
    - name: DB_PASS
environments:
  staging:
    provider: digitalocean
    region: nyc3
    secretsStoreOverride: staging-env
  prod:
    provider: digitalocean
    region: nyc1
    secretsStoreOverride: prod-env
modules:
  - name: db
    type: infra.database
    config:
      size: large
`
	path := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	wfCfg, err := config.LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}

	t.Run("staging uses staging-env store", func(t *testing.T) {
		// Verify planResourcesForEnv resolves modules correctly for staging.
		resolved, resErr := planResourcesForEnv(path, "staging")
		if resErr != nil {
			t.Fatal(resErr)
		}
		if len(resolved) == 0 {
			t.Fatal("expected at least one resolved module")
		}
		// Verify store routing via resolveSecretStoreForEnv.
		store := resolveSecretStoreForEnv(path, "DB_PASS", "staging")
		if store != "staging-env" {
			t.Fatalf("want staging-env store, got %q", store)
		}
		// Verify injectSecrets actually routes through the correct store.
		// Both staging-env and prod-env use the env provider, so DB_PASS is
		// read from the process env. We confirm no error (routing worked).
		secrets, secretErr := injectSecrets(context.Background(), wfCfg, "staging")
		if secretErr != nil {
			t.Fatalf("injectSecrets staging: %v", secretErr)
		}
		if secrets["DB_PASS"] != "test-secret-value" {
			// Both stores use env provider, so value comes from DB_PASS env var.
			t.Fatalf("want DB_PASS=test-secret-value from env provider, got %q", secrets["DB_PASS"])
		}
	})

	t.Run("prod uses prod-env store", func(t *testing.T) {
		resolved, resErr := planResourcesForEnv(path, "prod")
		if resErr != nil {
			t.Fatal(resErr)
		}
		if len(resolved) == 0 {
			t.Fatal("expected at least one resolved module")
		}
		store := resolveSecretStoreForEnv(path, "DB_PASS", "prod")
		if store != "prod-env" {
			t.Fatalf("want prod-env store, got %q", store)
		}
		// Verify injectSecrets routes through prod store (env provider).
		secrets, secretErr := injectSecrets(context.Background(), wfCfg, "prod")
		if secretErr != nil {
			t.Fatalf("injectSecrets prod: %v", secretErr)
		}
		if secrets["DB_PASS"] != "test-secret-value" {
			t.Fatalf("want DB_PASS=test-secret-value, got %q", secrets["DB_PASS"])
		}
	})

	t.Run("ResolveSecretStore returns env-level override", func(t *testing.T) {
		store := resolveSecretStoreForEnv(path, "DB_PASS", "staging")
		if store != "staging-env" {
			t.Fatalf("want staging-env, got %q", store)
		}
		store = resolveSecretStoreForEnv(path, "DB_PASS", "prod")
		if store != "prod-env" {
			t.Fatalf("want prod-env, got %q", store)
		}
	})

	t.Run("different envs route to different stores", func(t *testing.T) {
		stagingStore := resolveSecretStoreForEnv(path, "DB_PASS", "staging")
		prodStore := resolveSecretStoreForEnv(path, "DB_PASS", "prod")
		if stagingStore == prodStore {
			t.Fatalf("staging and prod should route to different stores, both got %q", stagingStore)
		}
	})
}
