package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSecurityConfigFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return p
}

// T35: LoadFromFile should apply hardened defaults when ci.build.security is absent.
func TestLoadFromFile_AppliesHardenedDefaults_WhenSecurityAbsent(t *testing.T) {
	cfgPath := writeSecurityConfigFile(t, `
ci:
  build:
    targets:
      - name: server
        type: go
        path: ./cmd/server
`)
	cfg, err := LoadFromFile(cfgPath)
	if err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	if cfg.CI == nil || cfg.CI.Build == nil {
		t.Fatal("CI.Build should be non-nil")
	}
	sec := cfg.CI.Build.Security
	if sec == nil {
		t.Fatal("Security should be non-nil after ApplyDefaults")
	}
	if !sec.Hardened {
		t.Error("Hardened should be true by default")
	}
	if !sec.SBOM {
		t.Error("SBOM should be true by default")
	}
	if sec.Provenance != "slsa-3" {
		t.Errorf("Provenance should be slsa-3, got %q", sec.Provenance)
	}
	if !sec.NonRoot {
		t.Error("NonRoot should be true by default")
	}
}

// T35: Explicit security config should be preserved.
func TestLoadFromFile_PreservesExplicitSecurity(t *testing.T) {
	cfgPath := writeSecurityConfigFile(t, `
ci:
  build:
    security:
      hardened: false
      sbom: false
`)
	cfg, err := LoadFromFile(cfgPath)
	if err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	sec := cfg.CI.Build.Security
	if sec == nil {
		t.Fatal("Security should be non-nil")
	}
	if sec.Hardened {
		t.Error("explicit hardened=false should be preserved")
	}
	if sec.SBOM {
		t.Error("explicit sbom=false should be preserved")
	}
}

// T36: Validate should emit a warning when hardened=false.
func TestCIConfig_Validate_WarnsOnHardenedFalse(t *testing.T) {
	cfgPath := writeSecurityConfigFile(t, `
ci:
  build:
    security:
      hardened: false
      sbom: true
`)
	cfg, err := LoadFromFile(cfgPath)
	if err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}

	// Capture warnings via the warnings slice returned by Validate.
	warnings, err := cfg.CI.ValidateWithWarnings()
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "hardened") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("want hardened warning in %v", warnings)
	}
}

// T36: No warning when hardened=true (default).
func TestCIConfig_Validate_NoWarnWhenHardenedTrue(t *testing.T) {
	cfgPath := writeSecurityConfigFile(t, `
ci:
  build:
    security:
      hardened: true
      sbom: true
`)
	cfg, err := LoadFromFile(cfgPath)
	if err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}

	warnings, err := cfg.CI.ValidateWithWarnings()
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	for _, w := range warnings {
		if strings.Contains(w, "hardened") {
			t.Errorf("unexpected hardened warning: %s", w)
		}
	}
}
