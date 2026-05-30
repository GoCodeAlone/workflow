package cigen_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/cigen"
)

func TestAnalyze_GoldenFixture(t *testing.T) {
	plan, err := cigen.Analyze([]string{"testdata/app.yaml"}, cigen.Options{
		WfctlVersion: "v0.66.0",
	})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	// PluginInstall: infra.container_service + analytics.google_provider + iac.provider → true
	if !plan.PluginInstall {
		t.Error("expected PluginInstall=true (infra/analytics/iac modules present)")
	}

	// PlanGuard: infra.container_service has protected: true
	if !plan.PlanGuard {
		t.Error("expected PlanGuard=true (protected module present)")
	}

	// Migrations: ci.migrations[0].database.env = APP_DB_URL
	if plan.Migrations == nil {
		t.Fatal("expected Migrations to be non-nil")
	}
	if plan.Migrations.DBEnv != "APP_DB_URL" {
		t.Errorf("Migrations.DBEnv = %q, want %q", plan.Migrations.DBEnv, "APP_DB_URL")
	}
	if plan.Migrations.Source != "migrations" {
		t.Errorf("Migrations.Source = %q, want %q", plan.Migrations.Source, "migrations")
	}

	// Single phase (no PhaseConfig option provided)
	if len(plan.Phases) != 1 {
		t.Errorf("expected 1 phase, got %d", len(plan.Phases))
	}
	if plan.Phases[0].Name != "deploy" {
		t.Errorf("expected phase name %q, got %q", "deploy", plan.Phases[0].Name)
	}

	// Triggers: default PR+PushMain+Dispatch
	if !plan.Triggers.PR {
		t.Error("expected Triggers.PR=true")
	}
	if !plan.Triggers.PushMain {
		t.Error("expected Triggers.PushMain=true")
	}
	if !plan.Triggers.Dispatch {
		t.Error("expected Triggers.Dispatch=true")
	}

	// WfctlVersion
	if plan.WfctlVersion != "v0.66.0" {
		t.Errorf("WfctlVersion = %q, want %q", plan.WfctlVersion, "v0.66.0")
	}

	// DefaultBranch and Runner defaults
	if plan.DefaultBranch != "main" {
		t.Errorf("DefaultBranch = %q, want %q", plan.DefaultBranch, "main")
	}
	if plan.Runner != "ubuntu-latest" {
		t.Errorf("Runner = %q, want %q", plan.Runner, "ubuntu-latest")
	}
}

func TestAnalyze_PhaseConfig(t *testing.T) {
	plan, err := cigen.Analyze([]string{"testdata/app.yaml"}, cigen.Options{
		PhaseConfig: "testdata/prereq.yaml",
	})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(plan.Phases) != 2 {
		t.Fatalf("expected 2 phases, got %d", len(plan.Phases))
	}
	if plan.Phases[0].Name != "prereq" {
		t.Errorf("expected first phase %q, got %q", "prereq", plan.Phases[0].Name)
	}
	if plan.Phases[0].ConfigPath != "testdata/prereq.yaml" {
		t.Errorf("expected prereq config path %q, got %q", "testdata/prereq.yaml", plan.Phases[0].ConfigPath)
	}
	if plan.Phases[1].Name != "deploy" {
		t.Errorf("expected second phase %q, got %q", "deploy", plan.Phases[1].Name)
	}
}

func TestAnalyze_DefaultWfctlVersion(t *testing.T) {
	plan, err := cigen.Analyze([]string{"testdata/app.yaml"}, cigen.Options{})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if plan.WfctlVersion != "latest" {
		t.Errorf("expected WfctlVersion=%q, got %q", "latest", plan.WfctlVersion)
	}
}

func TestAnalyze_NoMigrationsNoMigrationsSpec(t *testing.T) {
	// A minimal config with no ci.migrations should yield Migrations==nil
	plan, err := cigen.Analyze([]string{"testdata/minimal.yaml"}, cigen.Options{})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if plan.Migrations != nil {
		t.Errorf("expected Migrations=nil for config with no ci.migrations, got %+v", plan.Migrations)
	}
}

func TestAnalyze_AbsolutePathRelativized(t *testing.T) {
	// When given an absolute path under cwd, the resulting phase ConfigPath must
	// be relativized (no leading slash) so the generated CI `paths:` filter and
	// `--config` args reference a checkout-relative path.
	abs, err := filepath.Abs("testdata/app.yaml")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	if !filepath.IsAbs(abs) {
		t.Fatalf("expected an absolute path, got %q", abs)
	}

	plan, err := cigen.Analyze([]string{abs}, cigen.Options{})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(plan.Phases) != 1 {
		t.Fatalf("expected 1 phase, got %d", len(plan.Phases))
	}
	got := plan.Phases[0].ConfigPath
	if filepath.IsAbs(got) {
		t.Errorf("expected relativized ConfigPath, got absolute %q", got)
	}
	if strings.HasPrefix(got, "/") {
		t.Errorf("ConfigPath must not start with /, got %q", got)
	}
	if got != filepath.Join("testdata", "app.yaml") {
		t.Errorf("expected relative path %q, got %q", filepath.Join("testdata", "app.yaml"), got)
	}
}

func TestAnalyze_ConfigPathAliasUsedVerbatim(t *testing.T) {
	// When ConfigPathAlias is set (the MCP path), the primary phase ConfigPath
	// must be the alias verbatim, NOT the real (temp/absolute) path.
	abs, err := filepath.Abs("testdata/app.yaml")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	plan, err := cigen.Analyze([]string{abs}, cigen.Options{
		ConfigPathAlias: "deploy.yaml",
	})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if plan.Phases[len(plan.Phases)-1].ConfigPath != "deploy.yaml" {
		t.Errorf("expected primary phase ConfigPath to be alias %q, got %q",
			"deploy.yaml", plan.Phases[len(plan.Phases)-1].ConfigPath)
	}
}

func TestAnalyze_MultisiteGolden(t *testing.T) {
	// Golden test: load the REAL gocodealone-multisite configs (copied verbatim
	// into testdata/multisite/) and assert Analyze produces the expected shape.
	// The exact warning text is asserted from the plan.json warnings[] array
	// produced by the real binary run during the Task 18 evidence session.
	plan, err := cigen.Analyze(
		[]string{"testdata/multisite/deploy.yaml"},
		cigen.Options{
			PhaseConfig: "testdata/multisite/deploy.prereq.yaml",
		},
	)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	// Two phases: prereq then deploy
	if len(plan.Phases) != 2 {
		t.Fatalf("expected 2 phases, got %d", len(plan.Phases))
	}
	if plan.Phases[0].Name != "prereq" {
		t.Errorf("expected phase[0].Name=%q, got %q", "prereq", plan.Phases[0].Name)
	}
	if plan.Phases[1].Name != "deploy" {
		t.Errorf("expected phase[1].Name=%q, got %q", "deploy", plan.Phases[1].Name)
	}

	// Secrets union: deploy.yaml has 16 named secrets (14 entries + 2 provider keys)
	// The real plan.json contains 16 secrets total.
	if len(plan.Secrets) < 14 {
		t.Errorf("expected ≥14 secrets in union, got %d", len(plan.Secrets))
	}

	// PluginInstall: iac.provider, iac.state, analytics.google_provider, infra.database → true
	if !plan.PluginInstall {
		t.Error("expected PluginInstall=true (iac.* + analytics.* modules present)")
	}

	// PlanGuard: multisite-pg and gocodealone-multisite are both protected: true
	if !plan.PlanGuard {
		t.Error("expected PlanGuard=true (protected modules present)")
	}

	// Migrations: ci.migrations[0].database.env = MULTISITE_DB_URL
	if plan.Migrations == nil {
		t.Fatal("expected Migrations to be non-nil")
	}
	if plan.Migrations.DBEnv != "MULTISITE_DB_URL" {
		t.Errorf("Migrations.DBEnv = %q, want %q", plan.Migrations.DBEnv, "MULTISITE_DB_URL")
	}

	// Smoke: infra.container_service with domain gocodealone.tech + health_check /healthz
	if plan.Smoke == nil {
		t.Fatal("expected Smoke to be non-nil")
	}
	if !strings.HasSuffix(plan.Smoke.URL, "/healthz") {
		t.Errorf("Smoke.URL = %q, expected suffix /healthz", plan.Smoke.URL)
	}

	// Warnings: must include a DB-url warning (state-derived) and a SPACES casing warning
	if len(plan.Warnings) == 0 {
		t.Fatal("expected non-empty Warnings")
	}
	dbWarn := false
	casewarn := false
	for _, w := range plan.Warnings {
		if strings.Contains(w, "MULTISITE_DB_URL") && strings.Contains(w, "hash suffix") {
			dbWarn = true
		}
		if strings.Contains(w, "SPACES_access_key") && strings.Contains(w, "upper-case") {
			casewarn = true
		}
	}
	if !dbWarn {
		t.Errorf("expected warning mentioning MULTISITE_DB_URL hash suffix, got: %v", plan.Warnings)
	}
	if !casewarn {
		t.Errorf("expected warning mentioning SPACES_access_key upper-case, got: %v", plan.Warnings)
	}
}
