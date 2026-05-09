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

func TestInfraAlign_RA4_TopLevelSecretsGenerate_DoesNotFire(t *testing.T) {
	os.Unsetenv("STAGING_PG_PASSWORD")
	yaml := `
appName: test
secrets:
  generate:
    - key: STAGING_PG_PASSWORD
      type: random_hex
      length: 32
modules:
  - name: api
    type: infra.container_service
    config:
      image: "myapp:latest"
      http_port: 8080
      env_vars:
        DATABASE_URL: "postgres://user:${STAGING_PG_PASSWORD}@host:5432/db"
`
	cfg := writeAlignYAML(t, yaml)
	opts := alignOptions{configFile: cfg}
	findings, err := runInfraAlignChecks(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if findingsHaveRule(findings, "R-A4") {
		t.Errorf("unexpected R-A4 finding for top-level secrets.generate key: %v", findings)
	}
}

func TestInfraAlign_RA4_TopLevelSecretsEntries_DoesNotFire(t *testing.T) {
	os.Unsetenv("STAGING_PG_PASSWORD")
	yaml := `
appName: test
secrets:
  entries:
    - name: STAGING_PG_PASSWORD
      store: vault
modules:
  - name: api
    type: infra.container_service
    config:
      image: "myapp:latest"
      http_port: 8080
      env_vars:
        DATABASE_URL: "postgres://user:${STAGING_PG_PASSWORD}@host:5432/db"
`
	cfg := writeAlignYAML(t, yaml)
	opts := alignOptions{configFile: cfg}
	findings, err := runInfraAlignChecks(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if findingsHaveRule(findings, "R-A4") {
		t.Errorf("unexpected R-A4 finding for top-level secrets.entries name: %v", findings)
	}
}

// TestInfraAlign_RA4_TopLevelSecrets_FromImport_DoesNotFire pins the imports
// merge path: when a `secrets:` block is declared in an imported file rather
// than the main config, R-A4 must still see those keys. This requires
// processImports to merge WorkflowConfig.Secrets — without that, cfg.Secrets
// is nil/empty after LoadFromFile and R-A4 fires false-positive.
func TestInfraAlign_RA4_TopLevelSecrets_FromImport_DoesNotFire(t *testing.T) {
	os.Unsetenv("STAGING_PG_PASSWORD")
	os.Unsetenv("STAGING_API_TOKEN")
	dir := t.TempDir()

	sharedYAML := `
secrets:
  generate:
    - key: STAGING_PG_PASSWORD
      type: random_hex
      length: 32
  entries:
    - name: STAGING_API_TOKEN
      store: vault
`
	if err := os.WriteFile(filepath.Join(dir, "shared.yaml"), []byte(sharedYAML), 0644); err != nil {
		t.Fatal(err)
	}

	mainYAML := `
appName: test
imports:
  - shared.yaml
modules:
  - name: api
    type: infra.container_service
    config:
      image: "myapp:latest"
      http_port: 8080
      env_vars:
        DATABASE_URL: "postgres://user:${STAGING_PG_PASSWORD}@host:5432/db"
        API_TOKEN: "${STAGING_API_TOKEN}"
`
	mainPath := filepath.Join(dir, "main.yaml")
	if err := os.WriteFile(mainPath, []byte(mainYAML), 0644); err != nil {
		t.Fatal(err)
	}

	opts := alignOptions{configFile: mainPath}
	findings, err := runInfraAlignChecks(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if findingsHaveRule(findings, "R-A4") {
		t.Errorf("unexpected R-A4 finding for imported top-level secrets: %v", findings)
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

// TestAlignExitCode_ErrorSeverity_Returns1 verifies that ERROR severity
// (introduced in rev3 of the spaces-key plan for R-A9) blocks deploy with
// exit 1 even without --strict. Without this, an ERROR finding silently
// downgrades to non-blocking — defeating the rev3 requirement that the
// doubled-create anti-pattern fail CI.
func TestAlignExitCode_ErrorSeverity_Returns1(t *testing.T) {
	findings := []AlignFinding{
		{Rule: "R-A9", Severity: "ERROR", Resource: "SPACES_access_key", Message: "doubled-create"},
	}
	if code := alignExitCode(findings, false); code != 1 {
		t.Errorf("alignExitCode(ERROR, strict=false) = %d, want 1 — ERROR must always block", code)
	}
	if code := alignExitCode(findings, true); code != 1 {
		t.Errorf("alignExitCode(ERROR, strict=true) = %d, want 1", code)
	}
}

// TestAlignRender_ErrorSeverity_CountedInSummary verifies that the markdown
// summary includes ERROR alongside FAIL/WARN counts. Without this, ERROR
// findings would render in the table but be invisible in the summary line,
// hiding the deploy-blocking signal from CI consumers.
func TestAlignRender_ErrorSeverity_CountedInSummary(t *testing.T) {
	findings := []AlignFinding{
		{Rule: "R-A9", Severity: "ERROR", Resource: "SPACES_access_key", Message: "doubled-create"},
		{Rule: "R-A6", Severity: "WARN", Resource: "nats", Message: "advisory"},
	}
	out := renderAlignMarkdown(findings)
	if !strings.Contains(out, "1 ERROR") {
		t.Errorf("summary should report '1 ERROR', got: %s", out)
	}
	if !strings.Contains(out, "1 WARN") {
		t.Errorf("summary should still report '1 WARN', got: %s", out)
	}
}

// ── R-A9: suspicious provider_credential key ──────────────────────────────────

// TestCheckRA9_SuspiciousProviderCredentialKey is a table-driven unit test
// for checkRA9. Each case provides secretGens directly to alignContext so the
// rule logic is tested without file I/O.
func TestCheckRA9_SuspiciousProviderCredentialKey(t *testing.T) {
	cases := []struct {
		name        string
		gens        []SecretGen
		wantFinding bool
		wantMsgSub  string // substring expected in finding.Message
	}{
		{
			name:        "clean — key SPACES with digitalocean.spaces source",
			gens:        []SecretGen{{Key: "SPACES", Type: "provider_credential", Source: "digitalocean.spaces"}},
			wantFinding: false,
		},
		{
			name:        "suspicious — key ends with _access_key for digitalocean.spaces",
			gens:        []SecretGen{{Key: "SPACES_access_key", Type: "provider_credential", Source: "digitalocean.spaces"}},
			wantFinding: true,
			wantMsgSub:  "_access_key",
		},
		{
			name:        "suspicious — key ends with _secret_key",
			gens:        []SecretGen{{Key: "MY_THING_secret_key", Type: "provider_credential", Source: "digitalocean.spaces"}},
			wantFinding: true,
			wantMsgSub:  "_secret_key",
		},
		{
			name:        "not provider_credential — random_hex with _access_key suffix is fine",
			gens:        []SecretGen{{Key: "FOO_access_key", Type: "random_hex", Length: 32}},
			wantFinding: false,
		},
		{
			name:        "unknown source — no rule applies until source is in providerCredentialSubKeys",
			gens:        []SecretGen{{Key: "FOO_access_key", Type: "provider_credential", Source: "aws.s3"}},
			wantFinding: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := &alignContext{secretGens: tc.gens}
			findings := checkRA9(ctx)
			if tc.wantFinding && len(findings) == 0 {
				t.Fatal("expected R-A9 finding, got none")
			}
			if !tc.wantFinding && len(findings) != 0 {
				t.Fatalf("expected no findings, got: %+v", findings)
			}
			if tc.wantFinding && tc.wantMsgSub != "" && !strings.Contains(findings[0].Message, tc.wantMsgSub) {
				t.Errorf("expected message to contain %q, got: %s", tc.wantMsgSub, findings[0].Message)
			}
			if tc.wantFinding && findings[0].Rule != "R-A9" {
				t.Errorf("expected Rule=R-A9, got %q", findings[0].Rule)
			}
			if tc.wantFinding && findings[0].Severity != "ERROR" {
				t.Errorf("expected Severity=ERROR, got %q", findings[0].Severity)
			}
		})
	}
}

// TestInfraAlign_RA9_SuspiciousKey_Fires verifies R-A9 fires end-to-end
// through runInfraAlignChecks with a YAML that uses the bad pattern.
func TestInfraAlign_RA9_SuspiciousKey_Fires(t *testing.T) {
	yaml := `
secrets:
  provider: github
  config:
    repo: example/test
    token_env: GH_TOKEN
  generate:
    - key: SPACES_access_key
      type: provider_credential
      source: digitalocean.spaces
      name: example-deploy-key
modules:
  - name: do-provider
    type: iac.provider
    config:
      provider: digitalocean
      token: ${DIGITALOCEAN_TOKEN}
`
	cfg := writeAlignYAML(t, yaml)
	opts := alignOptions{configFile: cfg}
	findings, err := runInfraAlignChecks(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !findingsHaveRuleAndSeverity(findings, "R-A9", "ERROR") {
		t.Errorf("expected R-A9 ERROR, got: %v", findings)
	}
}

// TestInfraAlign_RA9_CleanKey_DoesNotFire verifies the canonical SPACES key
// (BMW/core-dump pattern) does not trigger R-A9.
func TestInfraAlign_RA9_CleanKey_DoesNotFire(t *testing.T) {
	yaml := `
secrets:
  provider: github
  config:
    repo: example/test
    token_env: GH_TOKEN
  generate:
    - key: SPACES
      type: provider_credential
      source: digitalocean.spaces
      name: example-deploy-key
`
	cfg := writeAlignYAML(t, yaml)
	opts := alignOptions{configFile: cfg}
	findings, err := runInfraAlignChecks(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if findingsHaveRule(findings, "R-A9") {
		t.Errorf("unexpected R-A9 finding for canonical key: %v", findings)
	}
}

// TestInfraAlign_RA9_CanonicalSingleEntry_Passes is the positive happy-path fixture for
// the R-A9 severity flip (rev3): the canonical single-entry SPACES key with
// no doubled-create anti-pattern must pass `wfctl infra align --strict` with
// exit code 0 and produce zero R-A9 findings.
//
// This is the inverse of TestInfraAlign_RA9_SuspiciousKey_Fires: it locks in
// that the rule does not regress into false positives once it fires as ERROR.
func TestInfraAlign_RA9_CanonicalSingleEntry_Passes(t *testing.T) {
	yaml := `
secrets:
  generate:
    - key: SPACES
      type: provider_credential
      source: digitalocean.spaces
      name: my-deploy-key
`
	cfg := writeAlignYAML(t, yaml)
	opts := alignOptions{configFile: cfg, strict: true}
	findings, err := runInfraAlignChecks(opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if findingsHaveRule(findings, "R-A9") {
		t.Fatalf("canonical shape should not trigger R-A9; got: %+v", findings)
	}
	if code := alignExitCode(findings, true); code != 0 {
		t.Fatalf("canonical shape should pass --strict; got exit=%d, findings=%+v", code, findings)
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
