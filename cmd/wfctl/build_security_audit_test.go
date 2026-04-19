package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeBuildAuditConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestBuildSecurityAudit_HardenedFalse checks check 1: hardened=false → WARN.
func TestBuildSecurityAudit_HardenedFalse(t *testing.T) {
	cfgPath := writeBuildAuditConfig(t, `
ci:
  build:
    security:
      hardened: false
    containers:
      - name: app
        method: dockerfile
`)
	findings := runBuildAuditChecks(cfgPath, filepath.Dir(cfgPath))
	if !hasMatch(findings, "WARN", "hardened") {
		t.Errorf("expected WARN about hardened=false, got: %v", findings)
	}
}

// TestBuildSecurityAudit_HardenedTrue checks no WARN when hardened=true.
func TestBuildSecurityAudit_HardenedTrue(t *testing.T) {
	cfgPath := writeBuildAuditConfig(t, `
ci:
  build:
    security:
      hardened: true
      sbom: true
      provenance: slsa-3
`)
	findings := runBuildAuditChecks(cfgPath, filepath.Dir(cfgPath))
	for _, f := range findings {
		if f.Severity == "WARN" && strings.Contains(strings.ToLower(f.Message), "hardened") {
			t.Errorf("unexpected WARN about hardened: %v", f)
		}
	}
}

// TestBuildSecurityAudit_DockerfileNoSBOM checks check 2: dockerfile without sbom/provenance → WARN.
func TestBuildSecurityAudit_DockerfileNoSBOM(t *testing.T) {
	cfgPath := writeBuildAuditConfig(t, `
ci:
  build:
    security:
      hardened: true
      sbom: false
      provenance: ""
    containers:
      - name: app
        method: dockerfile
`)
	findings := runBuildAuditChecks(cfgPath, filepath.Dir(cfgPath))
	if !hasMatch(findings, "WARN", "sbom") && !hasMatch(findings, "WARN", "provenance") {
		t.Errorf("expected WARN about sbom/provenance for dockerfile, got: %v", findings)
	}
}

// TestBuildSecurityAudit_RegistryNoRetention checks check 3: registry without retention → WARN.
func TestBuildSecurityAudit_RegistryNoRetention(t *testing.T) {
	cfgPath := writeBuildAuditConfig(t, `
ci:
  registries:
    - name: ghcr
      type: ghcr
      path: ghcr.io/myorg
`)
	findings := runBuildAuditChecks(cfgPath, filepath.Dir(cfgPath))
	if !hasMatch(findings, "WARN", "retention") {
		t.Errorf("expected WARN about missing retention, got: %v", findings)
	}
}

// TestBuildSecurityAudit_RegistryWithRetention checks no WARN when retention is defined.
func TestBuildSecurityAudit_RegistryWithRetention(t *testing.T) {
	cfgPath := writeBuildAuditConfig(t, `
ci:
  registries:
    - name: ghcr
      type: ghcr
      path: ghcr.io/myorg
      retention:
        keep_latest: 5
`)
	findings := runBuildAuditChecks(cfgPath, filepath.Dir(cfgPath))
	for _, f := range findings {
		if f.Severity == "WARN" && strings.Contains(strings.ToLower(f.Message), "retention") {
			t.Errorf("unexpected WARN about retention: %v", f)
		}
	}
}

// TestBuildSecurityAudit_PluginsNoLockfile checks check 4: plugins in config without lockfile → WARN.
func TestBuildSecurityAudit_PluginsNoLockfile(t *testing.T) {
	cfgPath := writeBuildAuditConfig(t, `
requires:
  plugins:
    - name: my-plugin
      version: 1.0.0
`)
	// No .wfctl.yaml exists in the temp dir.
	findings := runBuildAuditChecks(cfgPath, filepath.Dir(cfgPath))
	if !hasMatch(findings, "WARN", "lock") {
		t.Errorf("expected WARN about missing plugins lockfile, got: %v", findings)
	}
}

// TestBuildSecurityAudit_PluginsWithLockfile checks no WARN when lockfile exists.
func TestBuildSecurityAudit_PluginsWithLockfile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
requires:
  plugins:
    - name: my-plugin
      version: 1.0.0
`), 0600); err != nil {
		t.Fatal(err)
	}
	// Write .wfctl.yaml lockfile.
	if err := os.WriteFile(filepath.Join(dir, ".wfctl.yaml"), []byte(`plugins:
  my-plugin:
    version: 1.0.0
`), 0600); err != nil {
		t.Fatal(err)
	}
	findings := runBuildAuditChecks(cfgPath, dir)
	for _, f := range findings {
		if f.Severity == "WARN" && strings.Contains(strings.ToLower(f.Message), "lock") {
			t.Errorf("unexpected WARN about lockfile: %v", f)
		}
	}
}

// TestBuildSecurityAudit_EnvAuthMissing checks check 5: auth.env var not set → WARN.
func TestBuildSecurityAudit_EnvAuthMissing(t *testing.T) {
	envVar := "WFCTL_TEST_AUDIT_TOKEN_MISSING_XYZ"
	os.Unsetenv(envVar)
	cfgPath := writeBuildAuditConfig(t, `
ci:
  registries:
    - name: ghcr
      type: ghcr
      path: ghcr.io/myorg
      auth:
        env: `+envVar+`
      retention:
        keep_latest: 5
`)
	findings := runBuildAuditChecks(cfgPath, filepath.Dir(cfgPath))
	if !hasMatch(findings, "WARN", envVar) {
		t.Errorf("expected WARN about missing env var %s, got: %v", envVar, findings)
	}
}

// TestBuildSecurityAudit_EnvAuthSet checks no WARN when env var is set.
func TestBuildSecurityAudit_EnvAuthSet(t *testing.T) {
	envVar := "WFCTL_TEST_AUDIT_TOKEN_SET_XYZ"
	t.Setenv(envVar, "sometoken")
	cfgPath := writeBuildAuditConfig(t, `
ci:
  registries:
    - name: ghcr
      type: ghcr
      path: ghcr.io/myorg
      auth:
        env: `+envVar+`
      retention:
        keep_latest: 5
`)
	findings := runBuildAuditChecks(cfgPath, filepath.Dir(cfgPath))
	for _, f := range findings {
		if f.Severity == "WARN" && strings.Contains(f.Message, envVar) {
			t.Errorf("unexpected WARN about env var: %v", f)
		}
	}
}

// TestBuildSecurityAudit_LocalHardeningDisabled checks check 6: local env disabling hardening → NOTE.
func TestBuildSecurityAudit_LocalHardeningDisabled(t *testing.T) {
	cfgPath := writeBuildAuditConfig(t, `
ci:
  build:
    security:
      hardened: true
environments:
  local:
    build:
      security:
        hardened: false
`)
	findings := runBuildAuditChecks(cfgPath, filepath.Dir(cfgPath))
	if !hasMatch(findings, "NOTE", "local") {
		t.Errorf("expected NOTE about local env disabling hardening, got: %v", findings)
	}
	// Must NOT be a WARN (it's expected for local).
	for _, f := range findings {
		if f.Severity == "WARN" && strings.Contains(strings.ToLower(f.Message), "local") && strings.Contains(strings.ToLower(f.Message), "harden") {
			t.Errorf("local hardening override should be NOTE not WARN: %v", f)
		}
	}
}

// TestBuildSecurityAudit_StrictExitCode checks --strict causes exit 1 when warnings exist.
func TestBuildSecurityAudit_StrictExitCode(t *testing.T) {
	cfgPath := writeBuildAuditConfig(t, `
ci:
  build:
    security:
      hardened: false
`)
	err := runBuildSecurityAudit([]string{"--strict", "--config", cfgPath})
	if err == nil {
		t.Error("expected non-nil error in strict mode with warnings")
	}
}

// TestBuildSecurityAudit_NoStrictClean checks exit 0 when no warnings.
func TestBuildSecurityAudit_NoStrictClean(t *testing.T) {
	cfgPath := writeBuildAuditConfig(t, `
ci:
  build:
    security:
      hardened: true
      sbom: true
      provenance: slsa-3
  registries:
    - name: ghcr
      type: ghcr
      path: ghcr.io/myorg
      retention:
        keep_latest: 5
`)
	err := runBuildSecurityAudit([]string{"--config", cfgPath})
	if err != nil {
		t.Errorf("expected nil error with clean config, got: %v", err)
	}
}

// hasMatch returns true if any finding matches the given severity and message substring.
func hasMatch(findings []buildAuditFinding, severity, msgSubstr string) bool {
	for _, f := range findings {
		if f.Severity == severity && strings.Contains(strings.ToLower(f.Message), strings.ToLower(msgSubstr)) {
			return true
		}
	}
	return false
}
