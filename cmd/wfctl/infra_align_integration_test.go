package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestInfraAlignIntegration_FullConfig exercises the align checker against a
// realistic multi-service config that intentionally has several issues, then
// verifies that each expected rule fires and no unexpected rules fire.
func TestInfraAlignIntegration_FullConfig(t *testing.T) {
	dir := t.TempDir()

	// Write a Dockerfile with no USER directive (triggers R-A1)
	dfPath := filepath.Join(dir, "Dockerfile.api")
	if err := os.WriteFile(dfPath, []byte("FROM golang:1.22\nRUN go build -o /app .\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Write source file that does NOT contain /healthz (triggers R-A2)
	srcFile := filepath.Join(dir, "main.go")
	if err := os.WriteFile(srcFile, []byte(`package main

func main() {}
`), 0644); err != nil {
		t.Fatal(err)
	}

	// Unset env var to trigger R-A4
	os.Unsetenv("SECRET_TOKEN")

	yamlContent := `
modules:
  - name: api-build
    type: ci.build
    config:
      containers:
        - name: api
          dockerfile: "` + dfPath + `"

  - name: api
    type: infra.container_service
    config:
      image: "api:latest"
      dockerfile: "` + dfPath + `"
      http_port: 8080
      src_dir: "` + dir + `"
      health_check:
        path: "/healthz"
      env_vars:
        CACHE_URL: "redis-cache.internal:6379"
        TOKEN: "${SECRET_TOKEN}"

  - name: redis-cache
    type: infra.container_service
    config:
      image: "redis:7"
      internal_ports:
        - 6379

  - name: nats-broker
    type: infra.container_service
    config:
      image: "nats:latest"
      http_port: 4222

  - name: auth
    type: infra.container_service
    config:
      image: "auth:latest"
      http_port: 8081
      env_vars:
        WEBAUTHN_RP_ID: "example.com"
        WEBAUTHN_ORIGIN: "https://other.example.org"
`

	cfg := writeAlignYAML(t, yamlContent)
	opts := alignOptions{configFile: cfg}
	findings, err := runInfraAlignChecks(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expected findings:
	// R-A1: api Dockerfile has no USER directive
	if !findingsHaveRuleAndSeverity(findings, "R-A1", "FAIL") {
		t.Error("expected R-A1 FAIL (no USER in Dockerfile)")
	}

	// R-A2: /healthz not found in src_dir source files
	if !findingsHaveRule(findings, "R-A2") {
		t.Error("expected R-A2 finding (/healthz not in source)")
	}

	// R-A4: ${SECRET_TOKEN} is unresolved
	if !findingsHaveRuleAndSeverity(findings, "R-A4", "FAIL") {
		t.Error("expected R-A4 FAIL (unresolved ${SECRET_TOKEN})")
	}

	// R-A6: nats-broker has http_port but no expose:internal
	if !findingsHaveRuleAndSeverity(findings, "R-A6", "WARN") {
		t.Error("expected R-A6 WARN (nats-broker should have expose:internal)")
	}

	// R-A8: auth has WEBAUTHN_RP_ID/ORIGIN mismatch
	if !findingsHaveRuleAndSeverity(findings, "R-A8", "FAIL") {
		t.Error("expected R-A8 FAIL (WebAuthn RP_ID mismatch)")
	}

	// No R-A3 findings: redis-cache.internal:6379 is properly declared
	for _, f := range findings {
		if f.Rule == "R-A3" {
			t.Errorf("unexpected R-A3 finding: %v", f)
		}
	}

	// No R-A5 findings: no pre_deploy migrate
	for _, f := range findings {
		if f.Rule == "R-A5" {
			t.Errorf("unexpected R-A5 finding: %v", f)
		}
	}

	// No R-A7 findings: no plan file provided
	for _, f := range findings {
		if f.Rule == "R-A7" {
			t.Errorf("unexpected R-A7 finding (no plan file): %v", f)
		}
	}

	// Verify markdown output is well-formed
	out := renderAlignMarkdown(findings)
	if len(out) == 0 {
		t.Error("expected non-empty markdown output")
	}
	if out == "## wfctl infra align\n\nNo alignment issues found.\n" {
		t.Error("expected findings in markdown output, got none")
	}
}
