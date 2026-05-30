package cigen_test

import (
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
