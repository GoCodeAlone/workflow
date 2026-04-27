package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

type migrationRepairProvider struct {
	stateReturningProvider
	called bool
	req    interfaces.MigrationRepairRequest
	result *interfaces.MigrationRepairResult
	err    error
}

func (p *migrationRepairProvider) RepairDirtyMigration(_ context.Context, req interfaces.MigrationRepairRequest) (*interfaces.MigrationRepairResult, error) {
	p.called = true
	p.req = req
	if p.result != nil || p.err != nil {
		return p.result, p.err
	}
	return &interfaces.MigrationRepairResult{
		ProviderJobID: "job-123",
		Status:        interfaces.MigrationRepairStatusSucceeded,
		Logs:          "repair complete",
		Diagnostics: []interfaces.Diagnostic{{
			ID:     "deploy-123",
			Phase:  "ACTIVE",
			Cause:  "job completed",
			Detail: "repair complete",
		}},
	}, nil
}

func TestRunMigrateRepairDirtyResolvesEnvAndCallsProvider(t *testing.T) {
	cfgPath := writeMigrateRepairInfraConfig(t, t.TempDir())
	fake := &migrationRepairProvider{}
	restore := installMigrateRepairProvider(t, fake, "digitalocean")
	defer restore()

	out, err := captureMigrateRepairStdout(t, func() error {
		return runMigrate([]string{
			"repair-dirty",
			"--config", cfgPath,
			"--env", "staging",
			"--database", "bmw-database",
			"--app", "bmw-app",
			"--job-image", "registry.example/workflow-migrate:sha",
			"--expected-dirty-version", "20260426000005",
			"--force-version", "20260422000001",
			"--then-up",
			"--confirm-force", interfaces.MigrationRepairConfirmation,
			"--approve-destructive",
		})
	})
	if err != nil {
		t.Fatalf("runMigrate repair-dirty: %v", err)
	}
	if !fake.called {
		t.Fatal("provider RepairDirtyMigration was not called")
	}
	if fake.req.AppResourceName != "bmw-staging" {
		t.Fatalf("AppResourceName = %q, want env-resolved bmw-staging", fake.req.AppResourceName)
	}
	if fake.req.DatabaseResourceName != "bmw-staging-db" {
		t.Fatalf("DatabaseResourceName = %q, want env-resolved bmw-staging-db", fake.req.DatabaseResourceName)
	}
	if !fake.req.ThenUp || fake.req.UpIfClean {
		t.Fatalf("ThenUp/UpIfClean = %v/%v, want true/false", fake.req.ThenUp, fake.req.UpIfClean)
	}
	if !strings.Contains(out, "provider job job-123: succeeded") {
		t.Fatalf("stdout = %q, want provider job status", out)
	}
}

func TestRunMigrateRepairDirtyUsesCanonicalEnvDefaults(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
environments:
  staging:
    provider: do-provider
    region: nyc3
modules:
  - name: do-provider
    type: iac.provider
    config:
      provider: digitalocean
      token: test-token
  - name: bmw-database
    type: infra.database
    config:
      name: base-db
    environments:
      staging:
        config:
          name: bmw-staging-db
  - name: bmw-app
    type: infra.container_service
    config:
      name: base-app
    environments:
      staging:
        config:
          name: bmw-staging
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	fake := &migrationRepairProvider{}
	restore := installMigrateRepairProvider(t, fake, "digitalocean")
	defer restore()

	_, err := captureMigrateRepairStdout(t, func() error {
		return runMigrate([]string{
			"repair-dirty",
			"--config", cfgPath,
			"--env", "staging",
			"--database", "bmw-database",
			"--app", "bmw-app",
			"--job-image", "registry.example/workflow-migrate:sha",
			"--expected-dirty-version", "20260426000005",
			"--force-version", "20260422000001",
			"--then-up",
			"--confirm-force", interfaces.MigrationRepairConfirmation,
			"--approve-destructive",
		})
	})
	if err != nil {
		t.Fatalf("runMigrate repair-dirty: %v", err)
	}
	if !fake.called {
		t.Fatal("provider RepairDirtyMigration was not called")
	}
	if fake.req.AppResourceName != "bmw-staging" || fake.req.DatabaseResourceName != "bmw-staging-db" {
		t.Fatalf("request resources = %q/%q, want canonical env-resolved names", fake.req.AppResourceName, fake.req.DatabaseResourceName)
	}
}

func TestRunMigrateRepairDirtyRejectsDifferentProviderRefsWithSameType(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
environments:
  staging:
    region: nyc3
modules:
  - name: do-provider-app
    type: iac.provider
    config:
      provider: digitalocean
      token: app-token
  - name: do-provider-db
    type: iac.provider
    config:
      provider: digitalocean
      token: db-token
  - name: bmw-database
    type: infra.database
    config:
      provider: do-provider-db
      name: base-db
    environments:
      staging:
        config:
          name: bmw-staging-db
  - name: bmw-app
    type: infra.container_service
    config:
      provider: do-provider-app
      name: base-app
    environments:
      staging:
        config:
          name: bmw-staging
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	fake := &migrationRepairProvider{}
	restore := installMigrateRepairProvider(t, fake, "digitalocean")
	defer restore()

	_, err := captureMigrateRepairStdout(t, func() error {
		return runMigrate([]string{
			"repair-dirty",
			"--config", cfgPath,
			"--env", "staging",
			"--database", "bmw-database",
			"--app", "bmw-app",
			"--job-image", "registry.example/workflow-migrate:sha",
			"--expected-dirty-version", "20260426000005",
			"--force-version", "20260422000001",
			"--then-up",
			"--confirm-force", interfaces.MigrationRepairConfirmation,
			"--approve-destructive",
		})
	})
	if err == nil {
		t.Fatal("expected provider ref mismatch error")
	}
	if fake.called {
		t.Fatal("provider should not be called for mismatched provider refs")
	}
	if !strings.Contains(err.Error(), "same provider module") {
		t.Fatalf("error = %v, want provider module mismatch guidance", err)
	}
}

func TestRunMigrateRepairDirtyPropagatesJobEnv(t *testing.T) {
	cfgPath := writeMigrateRepairInfraConfig(t, t.TempDir())
	secret := "postgres://secret"
	fake := &migrationRepairProvider{
		result: &interfaces.MigrationRepairResult{
			ProviderJobID: "job-123",
			Status:        interfaces.MigrationRepairStatusSucceeded,
			Logs:          "connected to postgres://secret and repaired",
			Diagnostics: []interfaces.Diagnostic{{
				ID:     "deploy-123",
				Phase:  "ACTIVE",
				Cause:  "job used postgres://secret",
				Detail: "repair complete for postgres://secret",
			}},
		},
	}
	restore := installMigrateRepairProvider(t, fake, "digitalocean")
	defer restore()
	t.Setenv("DATABASE_URL", secret)

	out, err := captureMigrateRepairStdout(t, func() error {
		return runMigrate([]string{
			"repair-dirty",
			"--config", cfgPath,
			"--env", "staging",
			"--database", "bmw-database",
			"--app", "bmw-app",
			"--job-image", "registry.example/workflow-migrate:sha",
			"--expected-dirty-version", "20260426000005",
			"--force-version", "20260422000001",
			"--up-if-clean",
			"--confirm-force", interfaces.MigrationRepairConfirmation,
			"--approve-destructive",
			"--job-env", "PUBLIC_VALUE=visible",
			"--job-env-from-env", "DATABASE_URL",
		})
	})
	if err != nil {
		t.Fatalf("runMigrate repair-dirty: %v", err)
	}
	if fake.req.Env["PUBLIC_VALUE"] != "visible" {
		t.Fatalf("PUBLIC_VALUE env = %q", fake.req.Env["PUBLIC_VALUE"])
	}
	if fake.req.Env["DATABASE_URL"] != secret {
		t.Fatalf("DATABASE_URL env was not propagated")
	}
	if strings.Contains(out, secret) {
		t.Fatalf("stdout leaked secret: %q", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Fatalf("stdout = %q, want redacted secret marker", out)
	}
}

func TestRunMigrateRepairDirtyRedactsJobEnvFromProviderError(t *testing.T) {
	cfgPath := writeMigrateRepairInfraConfig(t, t.TempDir())
	secret := "postgres://error-secret"
	fake := &migrationRepairProvider{
		err: fmt.Errorf("migration failed while connecting to %s", secret),
	}
	restore := installMigrateRepairProvider(t, fake, "digitalocean")
	defer restore()
	t.Setenv("DATABASE_URL", secret)

	_, err := captureMigrateRepairStdout(t, func() error {
		return runMigrate([]string{
			"repair-dirty",
			"--config", cfgPath,
			"--env", "staging",
			"--database", "bmw-database",
			"--app", "bmw-app",
			"--job-image", "registry.example/workflow-migrate:sha",
			"--expected-dirty-version", "20260426000005",
			"--force-version", "20260422000001",
			"--up-if-clean",
			"--confirm-force", interfaces.MigrationRepairConfirmation,
			"--approve-destructive",
			"--job-env-from-env", "DATABASE_URL",
		})
	})
	if err == nil {
		t.Fatal("expected provider error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaked secret: %v", err)
	}
	if !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("error = %v, want redacted marker", err)
	}
}

func TestRunMigrateRepairDirtyMissingEnvFromEnvFailsBeforeProvider(t *testing.T) {
	cfgPath := writeMigrateRepairInfraConfig(t, t.TempDir())
	fake := &migrationRepairProvider{}
	restore := installMigrateRepairProvider(t, fake, "digitalocean")
	defer restore()
	t.Setenv("DATABASE_URL", "")

	_, err := captureMigrateRepairStdout(t, func() error {
		return runMigrate([]string{
			"repair-dirty",
			"--config", cfgPath,
			"--env", "staging",
			"--database", "bmw-database",
			"--app", "bmw-app",
			"--job-image", "registry.example/workflow-migrate:sha",
			"--expected-dirty-version", "20260426000005",
			"--force-version", "20260422000001",
			"--confirm-force", interfaces.MigrationRepairConfirmation,
			"--approve-destructive",
			"--job-env-from-env", "DATABASE_URL",
		})
	})
	if err == nil {
		t.Fatal("expected missing env error")
	}
	if fake.called {
		t.Fatal("provider should not be called when --job-env-from-env is missing")
	}
	if !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("error = %v, want missing env key", err)
	}
}

func TestRunMigrateRepairDirtyRequiresApprovalForStaging(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeMigrateRepairInfraConfig(t, dir)
	artifactPath := filepath.Join(dir, "approval.json")
	fake := &migrationRepairProvider{}
	restore := installMigrateRepairProvider(t, fake, "digitalocean")
	defer restore()

	out, err := captureMigrateRepairStdout(t, func() error {
		return runMigrate([]string{
			"repair-dirty",
			"--config", cfgPath,
			"--env", "staging",
			"--database", "bmw-database",
			"--app", "bmw-app",
			"--job-image", "registry.example/workflow-migrate:sha",
			"--expected-dirty-version", "20260426000005",
			"--force-version", "20260422000001",
			"--confirm-force", interfaces.MigrationRepairConfirmation,
			"--approval-artifact", artifactPath,
		})
	})
	if err == nil {
		t.Fatal("expected approval required error")
	}
	if fake.called {
		t.Fatal("provider should not be called before approval")
	}
	if !strings.Contains(out, interfaces.MigrationRepairStatusApprovalRequired) {
		t.Fatalf("stdout = %q, want approval_required status", out)
	}
	data, readErr := os.ReadFile(artifactPath)
	if readErr != nil {
		t.Fatalf("read artifact: %v", readErr)
	}
	for _, want := range []string{`"app":"bmw-app"`, `"database":"bmw-database"`, `"expected_dirty_version":"20260426000005"`, `"force_version":"20260422000001"`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("artifact missing %s: %s", want, data)
		}
	}
}

func TestRunMigrateRepairDirtyReportsUnsupportedProvider(t *testing.T) {
	cfgPath := writeMigrateRepairInfraConfig(t, t.TempDir())
	plain := &stateReturningProvider{}
	restore := installMigrateRepairProvider(t, plain, "digitalocean")
	defer restore()

	out, err := captureMigrateRepairStdout(t, func() error {
		return runMigrate([]string{
			"repair-dirty",
			"--config", cfgPath,
			"--env", "dev",
			"--database", "bmw-database",
			"--app", "bmw-app",
			"--job-image", "registry.example/workflow-migrate:sha",
			"--expected-dirty-version", "20260426000005",
			"--force-version", "20260422000001",
			"--confirm-force", interfaces.MigrationRepairConfirmation,
		})
	})
	if err == nil {
		t.Fatal("expected unsupported provider error")
	}
	if !strings.Contains(out, interfaces.MigrationRepairStatusUnsupported) {
		t.Fatalf("stdout = %q, want unsupported status", out)
	}
}

func TestRunMigrateRepairDirtyWritesGitHubStepSummary(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeMigrateRepairInfraConfig(t, dir)
	summaryPath := filepath.Join(dir, "summary.md")
	t.Setenv("GITHUB_STEP_SUMMARY", summaryPath)
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("WFCTL_ALLOW_TEST_STEP_SUMMARY", "true")
	secret := "postgres://summary-secret"
	t.Setenv("DATABASE_URL", secret)
	fake := &migrationRepairProvider{
		result: &interfaces.MigrationRepairResult{
			ProviderJobID: "job-123",
			Status:        interfaces.MigrationRepairStatusSucceeded,
			Logs:          "repair complete for postgres://summary-secret",
			Diagnostics: []interfaces.Diagnostic{{
				ID:     "deploy-123",
				Phase:  "ACTIVE",
				Cause:  "job used postgres://summary-secret",
				Detail: "repair complete for postgres://summary-secret",
			}},
		},
	}
	restore := installMigrateRepairProvider(t, fake, "digitalocean")
	defer restore()

	_, err := captureMigrateRepairStdout(t, func() error {
		return runMigrate([]string{
			"repair-dirty",
			"--config", cfgPath,
			"--env", "staging",
			"--database", "bmw-database",
			"--app", "bmw-app",
			"--job-image", "registry.example/workflow-migrate:sha",
			"--expected-dirty-version", "20260426000005",
			"--force-version", "20260422000001",
			"--then-up",
			"--confirm-force", interfaces.MigrationRepairConfirmation,
			"--approve-destructive",
			"--job-env-from-env", "DATABASE_URL",
		})
	})
	if err != nil {
		t.Fatalf("runMigrate repair-dirty: %v", err)
	}
	summary, readErr := os.ReadFile(summaryPath)
	if readErr != nil {
		t.Fatalf("read summary: %v", readErr)
	}
	for _, want := range []string{"migration_repair_dirty", "staging", "job-123", "succeeded", "repair complete"} {
		if !strings.Contains(string(summary), want) {
			t.Fatalf("summary missing %q:\n%s", want, summary)
		}
	}
	if strings.Contains(string(summary), secret) {
		t.Fatalf("summary leaked secret:\n%s", summary)
	}
	if !strings.Contains(string(summary), "[REDACTED]") {
		t.Fatalf("summary missing redacted secret marker:\n%s", summary)
	}
}

func writeMigrateRepairInfraConfig(t *testing.T, dir string) string {
	t.Helper()
	cfgPath := filepath.Join(dir, "infra.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
environments:
  staging:
    provider: digitalocean
    region: nyc3
  dev:
    provider: digitalocean
    region: nyc3
modules:
  - name: do-provider
    type: iac.provider
    config:
      provider: digitalocean
      token: test-token
  - name: bmw-database
    type: infra.database
    config:
      provider: do-provider
      name: base-db
    environments:
      staging:
        config:
          name: bmw-staging-db
      dev:
        config:
          name: bmw-dev-db
  - name: bmw-app
    type: infra.container_service
    config:
      provider: do-provider
      name: base-app
    environments:
      staging:
        config:
          name: bmw-staging
      dev:
        config:
          name: bmw-dev
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return cfgPath
}

func installMigrateRepairProvider(t *testing.T, provider interfaces.IaCProvider, wantProviderType string) func() {
	t.Helper()
	orig := resolveIaCProvider
	resolveIaCProvider = func(_ context.Context, providerType string, _ map[string]any) (interfaces.IaCProvider, io.Closer, error) {
		if providerType != wantProviderType {
			t.Fatalf("providerType = %q, want %q", providerType, wantProviderType)
		}
		return provider, nil, nil
	}
	return func() { resolveIaCProvider = orig }
}

func captureMigrateRepairStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = oldStdout
		_ = r.Close()
	}()

	runErr := fn()
	_ = w.Close()
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	return buf.String(), runErr
}

var _ interfaces.ProviderMigrationRepairer = (*migrationRepairProvider)(nil)

func TestRunMigrateRepairDirtyHelp(t *testing.T) {
	out, err := captureMigrateRepairStdout(t, func() error {
		return runMigrate([]string{"repair-dirty", "--help"})
	})
	if err != nil {
		t.Fatalf("help: %v", err)
	}
	for _, want := range []string{"--expected-dirty-version", "--force-version", "--confirm-force", "--approve-destructive"} {
		if !strings.Contains(out, want) {
			t.Fatalf("help missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "Required guard flags for non-dev environments") {
		t.Fatalf("help missing non-dev approval guidance:\n%s", out)
	}
}

func TestRunMigrateRepairDirtyRejectsInvalidJobEnv(t *testing.T) {
	cfgPath := writeMigrateRepairInfraConfig(t, t.TempDir())
	fake := &migrationRepairProvider{}
	restore := installMigrateRepairProvider(t, fake, "digitalocean")
	defer restore()

	_, err := captureMigrateRepairStdout(t, func() error {
		return runMigrate([]string{
			"repair-dirty",
			"--config", cfgPath,
			"--env", "dev",
			"--database", "bmw-database",
			"--app", "bmw-app",
			"--job-image", "registry.example/workflow-migrate:sha",
			"--expected-dirty-version", "20260426000005",
			"--force-version", "20260422000001",
			"--confirm-force", interfaces.MigrationRepairConfirmation,
			"--job-env", "missing-equals",
		})
	})
	if err == nil {
		t.Fatal("expected invalid --job-env error")
	}
	if !strings.Contains(fmt.Sprint(err), "KEY=VALUE") {
		t.Fatalf("error = %v, want KEY=VALUE guidance", err)
	}
}

func TestRunMigrateRepairDirtyRejectsInvalidJobEnvWithoutLeakingValue(t *testing.T) {
	cfgPath := writeMigrateRepairInfraConfig(t, t.TempDir())
	secret := "postgres://parse-secret"
	fake := &migrationRepairProvider{}
	restore := installMigrateRepairProvider(t, fake, "digitalocean")
	defer restore()

	_, err := captureMigrateRepairStdout(t, func() error {
		return runMigrate([]string{
			"repair-dirty",
			"--config", cfgPath,
			"--env", "dev",
			"--database", "bmw-database",
			"--app", "bmw-app",
			"--job-image", "registry.example/workflow-migrate:sha",
			"--expected-dirty-version", "20260426000005",
			"--force-version", "20260422000001",
			"--confirm-force", interfaces.MigrationRepairConfirmation,
			"--job-env", "=" + secret,
		})
	})
	if err == nil {
		t.Fatal("expected invalid --job-env error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaked secret: %v", err)
	}
	if fake.called {
		t.Fatal("provider should not be called for invalid --job-env")
	}
}
