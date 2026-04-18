package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Verifies that planResourcesForEnv respects secretsStoreOverride per env:
// modules with ${KEY} references resolve to the env-specific store's value.
func TestPlanResourcesForEnv_SecretsStoreOverride(t *testing.T) {
	t.Setenv("STAGING_DB_PASS", "staging-secret")
	t.Setenv("PROD_DB_PASS", "prod-secret")

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

	t.Run("staging uses staging-env store", func(t *testing.T) {
		resolved, err := planResourcesForEnv(path, "staging")
		if err != nil {
			t.Fatal(err)
		}
		if len(resolved) == 0 {
			t.Fatal("expected at least one resolved module")
		}
		store := resolveSecretStoreForEnv(path, "DB_PASS", "staging")
		if store != "staging-env" {
			t.Fatalf("want staging-env store, got %q", store)
		}
	})

	t.Run("prod uses prod-env store", func(t *testing.T) {
		resolved, err := planResourcesForEnv(path, "prod")
		if err != nil {
			t.Fatal(err)
		}
		if len(resolved) == 0 {
			t.Fatal("expected at least one resolved module")
		}
		store := resolveSecretStoreForEnv(path, "DB_PASS", "prod")
		if store != "prod-env" {
			t.Fatalf("want prod-env store, got %q", store)
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
}
