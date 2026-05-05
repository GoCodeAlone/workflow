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
	// Write .wfctl-lock.yaml lockfile (new canonical path).
	if err := os.WriteFile(filepath.Join(dir, ".wfctl-lock.yaml"), []byte(`plugins:
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

// --- T34 Dockerfile linting tests ---

func writeDockerfileAuditFixture(t *testing.T, cfgYAML, dockerfileContent string) (cfgPath string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath = filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0600); err != nil {
		t.Fatal(err)
	}
	if dockerfileContent != "" {
		if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(dockerfileContent), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return cfgPath
}

// TestDockerfileAudit_UserRoot checks that USER root → critical.
func TestDockerfileAudit_UserRoot(t *testing.T) {
	cfgPath := writeDockerfileAuditFixture(t, `
ci:
  build:
    containers:
      - name: app
        method: dockerfile
`, `FROM golang:1.22
USER root
RUN go build .
`)
	findings := runBuildAuditChecks(cfgPath, filepath.Dir(cfgPath))
	if !hasMatch(findings, "CRITICAL", "root") {
		t.Errorf("expected CRITICAL for USER root, got: %v", findings)
	}
}

// TestDockerfileAudit_NoUser checks that missing USER → critical.
func TestDockerfileAudit_NoUser(t *testing.T) {
	cfgPath := writeDockerfileAuditFixture(t, `
ci:
  build:
    containers:
      - name: app
        method: dockerfile
`, `FROM golang:1.22
RUN go build .
COPY . .
`)
	findings := runBuildAuditChecks(cfgPath, filepath.Dir(cfgPath))
	if !hasMatch(findings, "CRITICAL", "user") {
		t.Errorf("expected CRITICAL for missing USER directive, got: %v", findings)
	}
}

// TestDockerfileAudit_LatestTag checks FROM :latest → warn.
func TestDockerfileAudit_LatestTag(t *testing.T) {
	cfgPath := writeDockerfileAuditFixture(t, `
ci:
  build:
    containers:
      - name: app
        method: dockerfile
`, `FROM golang:latest
USER app
RUN go build .
`)
	findings := runBuildAuditChecks(cfgPath, filepath.Dir(cfgPath))
	if !hasMatch(findings, "WARN", "latest") {
		t.Errorf("expected WARN for FROM :latest, got: %v", findings)
	}
}

// TestDockerfileAudit_AddURL checks ADD https:// → warn.
func TestDockerfileAudit_AddURL(t *testing.T) {
	cfgPath := writeDockerfileAuditFixture(t, `
ci:
  build:
    containers:
      - name: app
        method: dockerfile
`, `FROM golang:1.22
USER app
ADD https://example.com/file.tar.gz /tmp/
`)
	findings := runBuildAuditChecks(cfgPath, filepath.Dir(cfgPath))
	if !hasMatch(findings, "WARN", "add") {
		t.Errorf("expected WARN for ADD URL, got: %v", findings)
	}
}

// TestDockerfileAudit_EmbeddedSecret checks secret pattern → critical.
func TestDockerfileAudit_EmbeddedSecret(t *testing.T) {
	cfgPath := writeDockerfileAuditFixture(t, `
ci:
  build:
    containers:
      - name: app
        method: dockerfile
`, `FROM golang:1.22
USER app
ENV API_KEY=abc123secret
`)
	findings := runBuildAuditChecks(cfgPath, filepath.Dir(cfgPath))
	if !hasMatch(findings, "CRITICAL", "secret") && !hasMatch(findings, "CRITICAL", "api") {
		t.Errorf("expected CRITICAL for embedded secret, got: %v", findings)
	}
}

// TestDockerfileAudit_Clean checks a clean Dockerfile produces no Dockerfile findings.
func TestDockerfileAudit_Clean(t *testing.T) {
	cfgPath := writeDockerfileAuditFixture(t, `
ci:
  build:
    containers:
      - name: app
        method: dockerfile
`, `FROM golang:1.22-alpine
RUN addgroup -S app && adduser -S app -G app
USER app
COPY . .
RUN go build .
`)
	findings := runBuildAuditChecks(cfgPath, filepath.Dir(cfgPath))
	for _, f := range findings {
		if f.File != "" && (f.Severity == "CRITICAL" || f.Severity == "WARN") {
			t.Errorf("unexpected Dockerfile finding in clean file: %v", f)
		}
	}
}

// TestDockerfileAudit_BaseImagePolicy checks allow_prefixes enforcement.
func TestDockerfileAudit_BaseImagePolicy(t *testing.T) {
	cfgPath := writeDockerfileAuditFixture(t, `
ci:
  build:
    security:
      hardened: true
      sbom: true
      provenance: slsa-3
      base_image_policy:
        allow_prefixes:
          - gcr.io/distroless/
    containers:
      - name: app
        method: dockerfile
`, `FROM golang:1.22-alpine
USER app
`)
	findings := runBuildAuditChecks(cfgPath, filepath.Dir(cfgPath))
	if !hasMatch(findings, "WARN", "policy") && !hasMatch(findings, "WARN", "allow") {
		t.Errorf("expected WARN for base image policy violation, got: %v", findings)
	}
}

// TestBuildAudit_CriticalExitsOne checks that CRITICAL findings cause exit 1 even without --strict.
func TestBuildAudit_CriticalExitsOne(t *testing.T) {
	cfgPath := writeDockerfileAuditFixture(t, `
ci:
  build:
    containers:
      - name: app
        method: dockerfile
`, `FROM golang:1.22
USER root
`)
	err := runBuildSecurityAudit([]string{"--config", cfgPath})
	if err == nil {
		t.Error("expected non-nil error when critical findings exist")
	}
}

// TestBuildAudit_WarnNoStrictExitsZero checks that WARN alone exits 0 without --strict.
func TestBuildAudit_WarnNoStrictExitsZero(t *testing.T) {
	cfgPath := writeDockerfileAuditFixture(t, `
ci:
  build:
    security:
      hardened: false
`, "")
	err := runBuildSecurityAudit([]string{"--config", cfgPath})
	if err != nil {
		t.Errorf("expected exit 0 for WARN without --strict, got: %v", err)
	}
}

// TestBuildAudit_NoteOnlyStrictExitsZero checks that NOTE-only findings do NOT trigger
// exit 1 even with --strict (NOTE is informational, not a warning).
func TestBuildAudit_NoteOnlyStrictExitsZero(t *testing.T) {
	cfgPath := writeBuildAuditConfig(t, `
ci:
  build:
    security:
      hardened: true
      sbom: true
      provenance: slsa-3
environments:
  local:
    build:
      security:
        hardened: false
`)
	err := runBuildSecurityAudit([]string{"--strict", "--config", cfgPath})
	if err != nil {
		t.Errorf("expected exit 0 for NOTE-only findings with --strict, got: %v", err)
	}
}
