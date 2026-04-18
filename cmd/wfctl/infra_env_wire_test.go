package main

import (
	"os"
	"path/filepath"
	"testing"
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
