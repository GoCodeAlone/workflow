package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// ── helpers ──────────────────────────────────────────────────────────────────

func writeAlignYAML(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "align-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	return f.Name()
}

func writeAlignPlanJSON(t *testing.T, plan interfaces.IaCPlan) string {
	t.Helper()
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.CreateTemp(t.TempDir(), "plan-*.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	return f.Name()
}

func findingsHaveRule(findings []AlignFinding, rule string) bool {
	for _, f := range findings {
		if f.Rule == rule {
			return true
		}
	}
	return false
}

func findingsHaveRuleAndSeverity(findings []AlignFinding, rule, severity string) bool {
	for _, f := range findings {
		if f.Rule == rule && f.Severity == severity {
			return true
		}
	}
	return false
}

// ── R-A1: container/runtime alignment ─────────────────────────────────────

func TestInfraAlign_RA1_OrphanedImageRef_Fires(t *testing.T) {
	yaml := `
modules:
  - name: build
    type: ci.build
    config:
      containers:
        - name: otherapp
          dockerfile: Dockerfile
  - name: api
    type: infra.container_service
    config:
      image: "myapp:latest"
      http_port: 8080
`
	// ci.build exists but has no container named "myapp" → orphaned reference
	cfg := writeAlignYAML(t, yaml)
	opts := alignOptions{configFile: cfg}
	findings, err := runInfraAlignChecks(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !findingsHaveRuleAndSeverity(findings, "R-A1", "FAIL") {
		t.Errorf("expected R-A1 FAIL for orphaned image reference, got: %v", findings)
	}
}

func TestInfraAlign_RA1_OrphanedImageRef_DoesNotFire(t *testing.T) {
	yaml := `
modules:
  - name: build
    type: ci.build
    config:
      containers:
        - name: myapp
          dockerfile: Dockerfile
  - name: api
    type: infra.container_service
    config:
      image: "myapp:latest"
      http_port: 8080
`
	cfg := writeAlignYAML(t, yaml)
	opts := alignOptions{configFile: cfg}
	findings, err := runInfraAlignChecks(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should not have R-A1 FAIL for orphaned image
	for _, f := range findings {
		if f.Rule == "R-A1" && f.Severity == "FAIL" && strings.Contains(f.Message, "orphaned") {
			t.Errorf("unexpected R-A1 FAIL (orphan): %v", f)
		}
	}
}

func TestInfraAlign_RA1_DockerfileUser_Fires(t *testing.T) {
	dir := t.TempDir()
	dfPath := filepath.Join(dir, "Dockerfile")
	if err := os.WriteFile(dfPath, []byte("FROM alpine\nRUN echo hi\n"), 0644); err != nil {
		t.Fatal(err)
	}

	yamlContent := `
modules:
  - name: build
    type: ci.build
    config:
      containers:
        - name: myapp
          dockerfile: "` + dfPath + `"
  - name: api
    type: infra.container_service
    config:
      image: "myapp:latest"
      dockerfile: "` + dfPath + `"
      http_port: 8080
`
	cfg := writeAlignYAML(t, yamlContent)
	opts := alignOptions{configFile: cfg}
	findings, err := runInfraAlignChecks(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !findingsHaveRuleAndSeverity(findings, "R-A1", "FAIL") {
		t.Errorf("expected R-A1 FAIL for missing USER directive, got: %v", findings)
	}
}

func TestInfraAlign_RA1_DockerfileUser_DoesNotFire(t *testing.T) {
	dir := t.TempDir()
	dfPath := filepath.Join(dir, "Dockerfile")
	if err := os.WriteFile(dfPath, []byte("FROM alpine\nUSER appuser\n"), 0644); err != nil {
		t.Fatal(err)
	}

	yamlContent := `
modules:
  - name: build
    type: ci.build
    config:
      containers:
        - name: myapp
          dockerfile: "` + dfPath + `"
  - name: api
    type: infra.container_service
    config:
      image: "myapp:latest"
      dockerfile: "` + dfPath + `"
      http_port: 8080
`
	cfg := writeAlignYAML(t, yamlContent)
	opts := alignOptions{configFile: cfg}
	findings, err := runInfraAlignChecks(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, f := range findings {
		if f.Rule == "R-A1" && strings.Contains(f.Message, "USER") {
			t.Errorf("unexpected R-A1 USER finding: %v", f)
		}
	}
}

func TestInfraAlign_RA1_RootUser_Fires(t *testing.T) {
	dir := t.TempDir()
	dfPath := filepath.Join(dir, "Dockerfile")
	if err := os.WriteFile(dfPath, []byte("FROM alpine\nUSER root\n"), 0644); err != nil {
		t.Fatal(err)
	}

	yamlContent := `
modules:
  - name: build
    type: ci.build
    config:
      containers:
        - name: myapp
          dockerfile: "` + dfPath + `"
  - name: api
    type: infra.container_service
    config:
      image: "myapp:latest"
      dockerfile: "` + dfPath + `"
      http_port: 8080
`
	cfg := writeAlignYAML(t, yamlContent)
	opts := alignOptions{configFile: cfg}
	findings, err := runInfraAlignChecks(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !findingsHaveRuleAndSeverity(findings, "R-A1", "FAIL") {
		t.Errorf("expected R-A1 FAIL for root USER, got: %v", findings)
	}
}

// ── R-A2: health-check alignment ───────────────────────────────────────────

func TestInfraAlign_RA2_HealthCheckPathMissing_Fires(t *testing.T) {
	dir := t.TempDir()
	srcFile := filepath.Join(dir, "main.go")
	if err := os.WriteFile(srcFile, []byte(`package main

func main() {}
`), 0644); err != nil {
		t.Fatal(err)
	}

	yamlContent := `
modules:
  - name: api
    type: infra.container_service
    config:
      image: "myapp:latest"
      http_port: 8080
      src_dir: "` + dir + `"
      health_check:
        path: "/healthz"
`
	cfg := writeAlignYAML(t, yamlContent)
	opts := alignOptions{configFile: cfg}
	findings, err := runInfraAlignChecks(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !findingsHaveRule(findings, "R-A2") {
		t.Errorf("expected R-A2 finding for missing health check path in source, got: %v", findings)
	}
}

func TestInfraAlign_RA2_HealthCheckPathFound_DoesNotFire(t *testing.T) {
	dir := t.TempDir()
	srcFile := filepath.Join(dir, "main.go")
	if err := os.WriteFile(srcFile, []byte(`package main

func healthzHandler() { /* /healthz */ }
`), 0644); err != nil {
		t.Fatal(err)
	}

	yamlContent := `
modules:
  - name: api
    type: infra.container_service
    config:
      image: "myapp:latest"
      http_port: 8080
      src_dir: "` + dir + `"
      health_check:
        path: "/healthz"
`
	cfg := writeAlignYAML(t, yamlContent)
	opts := alignOptions{configFile: cfg}
	findings, err := runInfraAlignChecks(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if findingsHaveRule(findings, "R-A2") {
		t.Errorf("unexpected R-A2 finding when path IS present in source: %v", findings)
	}
}

// ── R-A3: service-to-service DNS alignment ─────────────────────────────────

func TestInfraAlign_RA3_UnknownHostname_Fires(t *testing.T) {
	yaml := `
modules:
  - name: api
    type: infra.container_service
    config:
      image: "myapp:latest"
      http_port: 8080
      env_vars:
        CACHE_URL: "redis-cache.internal:6379"
`
	// No container_service named redis-cache
	cfg := writeAlignYAML(t, yaml)
	opts := alignOptions{configFile: cfg}
	findings, err := runInfraAlignChecks(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !findingsHaveRuleAndSeverity(findings, "R-A3", "FAIL") {
		t.Errorf("expected R-A3 FAIL for unknown DNS hostname, got: %v", findings)
	}
}

func TestInfraAlign_RA3_KnownHostname_DoesNotFire(t *testing.T) {
	yaml := `
modules:
  - name: redis-cache
    type: infra.container_service
    config:
      image: "redis:7"
      internal_ports:
        - 6379
  - name: api
    type: infra.container_service
    config:
      image: "myapp:latest"
      http_port: 8080
      env_vars:
        CACHE_URL: "redis-cache.internal:6379"
`
	cfg := writeAlignYAML(t, yaml)
	opts := alignOptions{configFile: cfg}
	findings, err := runInfraAlignChecks(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if findingsHaveRule(findings, "R-A3") {
		t.Errorf("unexpected R-A3 finding for known service: %v", findings)
	}
}

func TestInfraAlign_RA3_PortMismatch_Fires(t *testing.T) {
	yaml := `
modules:
  - name: redis-cache
    type: infra.container_service
    config:
      image: "redis:7"
      internal_ports:
        - 6380
  - name: api
    type: infra.container_service
    config:
      image: "myapp:latest"
      http_port: 8080
      env_vars:
        CACHE_URL: "redis-cache.internal:6379"
`
	cfg := writeAlignYAML(t, yaml)
	opts := alignOptions{configFile: cfg}
	findings, err := runInfraAlignChecks(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !findingsHaveRuleAndSeverity(findings, "R-A3", "FAIL") {
		t.Errorf("expected R-A3 FAIL for port mismatch, got: %v", findings)
	}
}

// ── R-A4: env-var resolution ───────────────────────────────────────────────

func TestInfraAlign_RA4_UnresolvedToken_Fires(t *testing.T) {
	// Ensure STRIPE_KEY is NOT in env
	os.Unsetenv("STRIPE_KEY")
	yaml := `
modules:
  - name: api
    type: infra.container_service
    config:
      image: "myapp:latest"
      http_port: 8080
      env_vars:
        PAYMENT_KEY: "${STRIPE_KEY}"
`
	cfg := writeAlignYAML(t, yaml)
	opts := alignOptions{configFile: cfg}
	findings, err := runInfraAlignChecks(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !findingsHaveRuleAndSeverity(findings, "R-A4", "FAIL") {
		t.Errorf("expected R-A4 FAIL for unresolved token, got: %v", findings)
	}
}

func TestInfraAlign_RA4_ResolvedToken_DoesNotFire(t *testing.T) {
	t.Setenv("STRIPE_KEY", "sk_test_123")
	yaml := `
modules:
  - name: api
    type: infra.container_service
    config:
      image: "myapp:latest"
      http_port: 8080
      env_vars:
        PAYMENT_KEY: "${STRIPE_KEY}"
`
	cfg := writeAlignYAML(t, yaml)
	opts := alignOptions{configFile: cfg}
	findings, err := runInfraAlignChecks(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if findingsHaveRule(findings, "R-A4") {
		t.Errorf("unexpected R-A4 finding when token IS set: %v", findings)
	}
}

func TestInfraAlign_RA4_SecretsGenerate_DoesNotFire(t *testing.T) {
	os.Unsetenv("DB_PASSWORD")
	yaml := `
modules:
  - name: api
    type: infra.container_service
    config:
      image: "myapp:latest"
      http_port: 8080
      env_vars:
        DB_PASS: "${DB_PASSWORD}"
  - name: secrets
    type: secrets.generate
    config:
      generate:
        - key: DB_PASSWORD
          length: 32
`
	cfg := writeAlignYAML(t, yaml)
	opts := alignOptions{configFile: cfg}
	findings, err := runInfraAlignChecks(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if findingsHaveRule(findings, "R-A4") {
		t.Errorf("unexpected R-A4 finding for secrets.generate key: %v", findings)
	}
}

// ── R-A5: migrations alignment ─────────────────────────────────────────────

func TestInfraAlign_RA5_PreDeployMigrateNoDB_Fires(t *testing.T) {
	yaml := `
modules:
  - name: build
    type: ci.build
    config:
      containers:
        - name: migrator
          dockerfile: Dockerfile.migrate
  - name: api
    type: infra.container_service
    config:
      image: "myapp:latest"
      http_port: 8080
      pre_deploy:
        kind: migrate
        image: "migrator:latest"
`
	// No infra.database module — FAIL
	cfg := writeAlignYAML(t, yaml)
	opts := alignOptions{configFile: cfg}
	findings, err := runInfraAlignChecks(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !findingsHaveRuleAndSeverity(findings, "R-A5", "FAIL") {
		t.Errorf("expected R-A5 FAIL for pre_deploy migrate with no DB, got: %v", findings)
	}
}

func TestInfraAlign_RA5_PreDeployMigrate_WithDB_DoesNotFire(t *testing.T) {
	yaml := `
modules:
  - name: build
    type: ci.build
    config:
      containers:
        - name: migrator
          dockerfile: Dockerfile.migrate
  - name: api
    type: infra.container_service
    config:
      image: "myapp:latest"
      http_port: 8080
      pre_deploy:
        kind: migrate
        image: "migrator:latest"
  - name: db
    type: infra.database
    config:
      engine: postgres
      trusted_sources:
        - type: app
          value: api
`
	cfg := writeAlignYAML(t, yaml)
	opts := alignOptions{configFile: cfg}
	findings, err := runInfraAlignChecks(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if findingsHaveRule(findings, "R-A5") {
		t.Errorf("unexpected R-A5 finding when DB + trusted_sources present: %v", findings)
	}
}

// ── R-A6: network/exposure alignment ──────────────────────────────────────

func TestInfraAlign_RA6_InternalWithHTTPPort_Fires(t *testing.T) {
	yaml := `
modules:
  - name: api
    type: infra.container_service
    config:
      image: "myapp:latest"
      expose: internal
      http_port: 8080
`
	cfg := writeAlignYAML(t, yaml)
	opts := alignOptions{configFile: cfg}
	findings, err := runInfraAlignChecks(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !findingsHaveRuleAndSeverity(findings, "R-A6", "FAIL") {
		t.Errorf("expected R-A6 FAIL for expose:internal + http_port, got: %v", findings)
	}
}

func TestInfraAlign_RA6_InternalWithHTTPPort_DoesNotFire(t *testing.T) {
	yaml := `
modules:
  - name: api
    type: infra.container_service
    config:
      image: "myapp:latest"
      http_port: 8080
`
	cfg := writeAlignYAML(t, yaml)
	opts := alignOptions{configFile: cfg}
	findings, err := runInfraAlignChecks(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if findingsHaveRuleAndSeverity(findings, "R-A6", "FAIL") {
		t.Errorf("unexpected R-A6 FAIL for normal http_port: %v", findings)
	}
}

func TestInfraAlign_RA6_InternalServiceWithoutExposeInternal_Warns(t *testing.T) {
	yaml := `
modules:
  - name: nats-broker
    type: infra.container_service
    config:
      image: "nats:latest"
      http_port: 4222
`
	cfg := writeAlignYAML(t, yaml)
	opts := alignOptions{configFile: cfg}
	findings, err := runInfraAlignChecks(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !findingsHaveRuleAndSeverity(findings, "R-A6", "WARN") {
		t.Errorf("expected R-A6 WARN for internal service name without expose:internal, got: %v", findings)
	}
}

// ── R-A7: plan-output sanity ──────────────────────────────────────────────

func TestInfraAlign_RA7_DeleteProtectedResource_Fires(t *testing.T) {
	plan := interfaces.IaCPlan{
		ID:        "plan-1",
		CreatedAt: time.Now(),
		Actions: []interfaces.PlanAction{
			{
				Action: "delete",
				Resource: interfaces.ResourceSpec{
					Name:   "prod-db",
					Type:   "infra.database",
					Config: map[string]any{"protected": true},
				},
			},
		},
	}

	planFile := writeAlignPlanJSON(t, plan)
	yaml := `
modules:
  - name: prod-db
    type: infra.database
    config:
      engine: postgres
      protected: true
`
	cfg := writeAlignYAML(t, yaml)
	opts := alignOptions{configFile: cfg, planFile: planFile}
	findings, err := runInfraAlignChecks(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !findingsHaveRuleAndSeverity(findings, "R-A7", "FAIL") {
		t.Errorf("expected R-A7 FAIL for delete of protected resource, got: %v", findings)
	}
}

func TestInfraAlign_RA7_DeleteProtectedResource_DoesNotFire(t *testing.T) {
	plan := interfaces.IaCPlan{
		ID:        "plan-2",
		CreatedAt: time.Now(),
		Actions: []interfaces.PlanAction{
			{
				Action: "update",
				Resource: interfaces.ResourceSpec{
					Name:   "prod-db",
					Type:   "infra.database",
					Config: map[string]any{"protected": true},
				},
			},
		},
	}

	planFile := writeAlignPlanJSON(t, plan)
	yaml := `
modules:
  - name: prod-db
    type: infra.database
    config:
      engine: postgres
      protected: true
`
	cfg := writeAlignYAML(t, yaml)
	opts := alignOptions{configFile: cfg, planFile: planFile}
	findings, err := runInfraAlignChecks(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if findingsHaveRuleAndSeverity(findings, "R-A7", "FAIL") {
		t.Errorf("unexpected R-A7 FAIL for non-delete action: %v", findings)
	}
}

func TestInfraAlign_RA7_TooManyChanges_Warns(t *testing.T) {
	actions := make([]interfaces.PlanAction, 55)
	for i := range actions {
		actions[i] = interfaces.PlanAction{
			Action: "create",
			Resource: interfaces.ResourceSpec{
				Name:   "resource-" + string(rune('a'+i%26)),
				Type:   "infra.container_service",
				Config: map[string]any{},
			},
		}
	}
	plan := interfaces.IaCPlan{
		ID:        "plan-big",
		CreatedAt: time.Now(),
		Actions:   actions,
	}

	planFile := writeAlignPlanJSON(t, plan)
	cfg := writeAlignYAML(t, `modules: []`)
	opts := alignOptions{configFile: cfg, planFile: planFile, maxChanges: 50}
	findings, err := runInfraAlignChecks(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !findingsHaveRuleAndSeverity(findings, "R-A7", "WARN") {
		t.Errorf("expected R-A7 WARN for too many changes, got: %v", findings)
	}
}

// ── R-A8: WebAuthn alignment ───────────────────────────────────────────────

func TestInfraAlign_RA8_RPIDMismatch_Fires(t *testing.T) {
	yaml := `
modules:
  - name: auth
    type: infra.container_service
    config:
      image: "auth:latest"
      http_port: 8080
      env_vars:
        WEBAUTHN_RP_ID: "example.com"
        WEBAUTHN_ORIGIN: "https://app.other.com"
`
	cfg := writeAlignYAML(t, yaml)
	opts := alignOptions{configFile: cfg}
	findings, err := runInfraAlignChecks(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !findingsHaveRuleAndSeverity(findings, "R-A8", "FAIL") {
		t.Errorf("expected R-A8 FAIL for RP_ID/ORIGIN mismatch, got: %v", findings)
	}
}

func TestInfraAlign_RA8_RPIDMatch_DoesNotFire(t *testing.T) {
	yaml := `
modules:
  - name: auth
    type: infra.container_service
    config:
      image: "auth:latest"
      http_port: 8080
      env_vars:
        WEBAUTHN_RP_ID: "example.com"
        WEBAUTHN_ORIGIN: "https://example.com"
`
	cfg := writeAlignYAML(t, yaml)
	opts := alignOptions{configFile: cfg}
	findings, err := runInfraAlignChecks(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if findingsHaveRule(findings, "R-A8") {
		t.Errorf("unexpected R-A8 finding when RP_ID matches ORIGIN host: %v", findings)
	}
}

// ── exit-code and output tests ─────────────────────────────────────────────

func TestInfraAlign_ExitCode_FailOnFail(t *testing.T) {
	os.Unsetenv("STRIPE_KEY")
	yaml := `
modules:
  - name: api
    type: infra.container_service
    config:
      image: "myapp:latest"
      http_port: 8080
      env_vars:
        KEY: "${STRIPE_KEY}"
`
	cfg := writeAlignYAML(t, yaml)
	opts := alignOptions{configFile: cfg}
	findings, err := runInfraAlignChecks(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exitCode := alignExitCode(findings, false); exitCode != 1 {
		t.Errorf("expected exit code 1 on FAIL findings, got %d", exitCode)
	}
}

func TestInfraAlign_ExitCode_WarnNoStrict(t *testing.T) {
	findings := []AlignFinding{
		{Rule: "R-A6", Severity: "WARN", Resource: "nats", Message: "test warn"},
	}
	if code := alignExitCode(findings, false); code != 0 {
		t.Errorf("expected exit 0 for WARN without --strict, got %d", code)
	}
}

func TestInfraAlign_ExitCode_WarnStrict(t *testing.T) {
	findings := []AlignFinding{
		{Rule: "R-A6", Severity: "WARN", Resource: "nats", Message: "test warn"},
	}
	if code := alignExitCode(findings, true); code != 1 {
		t.Errorf("expected exit 1 for WARN with --strict, got %d", code)
	}
}

func TestInfraAlign_RenderMarkdown(t *testing.T) {
	findings := []AlignFinding{
		{Rule: "R-A6", Severity: "WARN", Resource: "nats-broker", Message: "internal service should use expose: internal"},
		{Rule: "R-A4", Severity: "FAIL", Resource: "api", Message: "unresolved env var: ${STRIPE_KEY}"},
	}
	out := renderAlignMarkdown(findings)
	if !strings.Contains(out, "| Rule |") {
		t.Error("expected markdown table header")
	}
	if !strings.Contains(out, "R-A6") {
		t.Error("expected R-A6 in output")
	}
	if !strings.Contains(out, "R-A4") {
		t.Error("expected R-A4 in output")
	}
	if !strings.Contains(out, "1 FAIL") {
		t.Error("expected FAIL count in summary")
	}
	if !strings.Contains(out, "1 WARN") {
		t.Error("expected WARN count in summary")
	}
}
