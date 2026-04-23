package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

// infraEnvWireFixture writes a config with two-env setup where staging omits
// bmw-dns (staging: null) and returns its path.
func infraEnvWireFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cfg := `modules:
  - name: iac-state
    type: iac.state
    config:
      backend: filesystem
      directory: /tmp/test-iac-state
  - name: bmw-database
    type: infra.database
    config:
      size: small
    environments:
      staging:
        config:
          size: small
      prod:
        config:
          size: large
  - name: bmw-dns
    type: infra.dns
    environments:
      staging: null
      prod:
        config:
          domain: example.com
`
	path := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestWriteEnvResolvedConfig verifies writeEnvResolvedConfig produces a temp
// file that excludes null-env modules and includes env-specific config.
func TestWriteEnvResolvedConfig_StagingExcludesDNS(t *testing.T) {
	fixture := infraEnvWireFixture(t)

	tmp, err := writeEnvResolvedConfig(fixture, "staging")
	if err != nil {
		t.Fatalf("writeEnvResolvedConfig: %v", err)
	}
	defer os.Remove(tmp)

	// Load and verify the resolved config.
	resources, err := planResourcesForEnv(tmp, "")
	if err != nil {
		t.Fatalf("planResourcesForEnv on resolved config: %v", err)
	}

	for _, r := range resources {
		if r.Name == "bmw-dns" {
			t.Fatal("staging resolved config should not contain bmw-dns")
		}
	}
	var sawDB bool
	for _, r := range resources {
		if r.Name == "bmw-database" {
			sawDB = true
			if r.Config["size"] != "small" {
				t.Fatalf("staging db size: want small, got %v", r.Config["size"])
			}
		}
	}
	if !sawDB {
		t.Fatal("staging resolved config should contain bmw-database")
	}
}

func TestWriteEnvResolvedConfig_ProdIncludesDNS(t *testing.T) {
	fixture := infraEnvWireFixture(t)

	tmp, err := writeEnvResolvedConfig(fixture, "prod")
	if err != nil {
		t.Fatalf("writeEnvResolvedConfig: %v", err)
	}
	defer os.Remove(tmp)

	resources, err := planResourcesForEnv(tmp, "")
	if err != nil {
		t.Fatalf("planResourcesForEnv on resolved config: %v", err)
	}

	var sawDNS, sawLargeDB bool
	for _, r := range resources {
		if r.Name == "bmw-dns" {
			sawDNS = true
		}
		if r.Name == "bmw-database" && r.Config["size"] == "large" {
			sawLargeDB = true
		}
	}
	if !sawDNS {
		t.Fatal("prod resolved config should contain bmw-dns")
	}
	if !sawLargeDB {
		t.Fatal("prod resolved config should have large db")
	}
}

// TestInfraApply_EnvFlagProducesResolvedConfig verifies that when --env staging
// is passed, the resolved config written to the temp file excludes null-env
// modules (bmw-dns). We test this by inspecting the resolved temp file.
func TestInfraApply_EnvFlagProducesResolvedConfig(t *testing.T) {
	fixture := infraEnvWireFixture(t)

	// Simulate the env-resolve step that runInfraApply now performs.
	tmp, err := writeEnvResolvedConfig(fixture, "staging")
	if err != nil {
		t.Fatalf("writeEnvResolvedConfig: %v", err)
	}
	defer os.Remove(tmp)

	resources, err := planResourcesForEnv(tmp, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range resources {
		if r.Name == "bmw-dns" {
			t.Fatal("apply with --env staging should not include bmw-dns")
		}
	}
}

// TestWriteEnvResolvedConfig_PreservesTopLevelSections verifies that secrets,
// secretStores, and other non-module sections survive in the temp file.
func TestWriteEnvResolvedConfig_PreservesTopLevelSections(t *testing.T) {
	dir := t.TempDir()
	cfg := `secretStores:
  my-store:
    provider: env
secrets:
  defaultStore: my-store
  entries:
    - name: DB_PASS
environments:
  staging:
    provider: digitalocean
    region: nyc3
    secretsStoreOverride: my-store
  prod:
    provider: digitalocean
    region: nyc1
modules:
  - name: iac-state
    type: iac.state
    config:
      backend: filesystem
    environments:
      staging:
        config:
          prefix: staging/
      prod:
        config:
          prefix: prod/
  - name: bmw-database
    type: infra.database
    config:
      size: small
    environments:
      staging:
        config:
          size: small
      prod:
        config:
          size: large
  - name: bmw-dns
    type: infra.dns
    environments:
      staging: null
      prod:
        config:
          domain: example.com
`
	path := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	tmp, err := writeEnvResolvedConfig(path, "staging")
	if err != nil {
		t.Fatalf("writeEnvResolvedConfig: %v", err)
	}
	defer os.Remove(tmp)

	// Re-parse using planResourcesForEnv to check modules.
	resources, resErr := planResourcesForEnv(tmp, "")
	if resErr != nil {
		t.Fatalf("planResourcesForEnv on temp file: %v", resErr)
	}

	// bmw-dns should be excluded (staging: null).
	for _, r := range resources {
		if r.Name == "bmw-dns" {
			t.Fatal("staging temp config should not contain bmw-dns")
		}
	}

	// Verify secrets section is preserved by loading the temp file with LoadFromFile.
	tmpCfg, cfgErr := config.LoadFromFile(tmp)
	if cfgErr != nil {
		t.Fatalf("load temp file: %v", cfgErr)
	}
	if tmpCfg.Secrets == nil {
		t.Fatal("secrets section must be preserved in temp file")
	}
	if tmpCfg.Secrets.DefaultStore != "my-store" {
		t.Fatalf("want defaultStore=my-store, got %q", tmpCfg.Secrets.DefaultStore)
	}
	if len(tmpCfg.SecretStores) == 0 {
		t.Fatal("secretStores section must be preserved in temp file")
	}
}

// TestPlanResourcesForEnv_UsesEnvOverrideNames asserts that planResourcesForEnv
// returns ResolvedModule.Name values from env-level config overrides (not the
// raw module names). This is the unit-level companion to
// TestPlanApplyEquivalence_EnvOverrideNames.
func TestPlanResourcesForEnv_UsesEnvOverrideNames(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
modules:
  - name: bmw-vpc
    type: infra.vpc
    config:
      cidr: "10.0.0.0/24"
    environments:
      staging:
        config:
          name: bmw-staging-vpc

  - name: bmw-db
    type: infra.database
    config:
      engine: postgres
    environments:
      staging:
        config:
          name: bmw-staging-db
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	resources, err := planResourcesForEnv(cfgPath, "staging")
	if err != nil {
		t.Fatalf("planResourcesForEnv: %v", err)
	}

	names := map[string]bool{}
	for _, r := range resources {
		names[r.Name] = true
	}

	if !names["bmw-staging-vpc"] {
		t.Errorf("want bmw-staging-vpc in plan names, got %v", names)
	}
	if !names["bmw-staging-db"] {
		t.Errorf("want bmw-staging-db in plan names, got %v", names)
	}
	if names["bmw-vpc"] {
		t.Errorf("bmw-vpc (raw module name) should NOT appear after env override; got %v", names)
	}
	if names["bmw-db"] {
		t.Errorf("bmw-db (raw module name) should NOT appear after env override; got %v", names)
	}
}
