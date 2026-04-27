package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func TestRunBuildPhase_NilConfig(t *testing.T) {
	if err := runBuildPhase(nil, false); err != nil {
		t.Fatalf("nil build config should not error: %v", err)
	}
}

func TestRunTestPhase_NilConfig(t *testing.T) {
	if err := runTestPhase(nil, false); err != nil {
		t.Fatalf("nil test config should not error: %v", err)
	}
}

func TestRunDeployPhase_NilConfig(t *testing.T) {
	err := runDeployPhase(nil, "staging", false)
	if err == nil {
		t.Fatal("expected error for nil deploy config")
	}
}

func TestRunDeployPhase_MissingEnv(t *testing.T) {
	deploy := &config.CIDeployConfig{
		Environments: map[string]*config.CIDeployEnvironment{
			"staging": {Provider: "aws-ecs"},
		},
	}
	err := runDeployPhase(deploy, "production", false)
	if err == nil {
		t.Fatal("expected error for missing environment")
	}
}

func TestRunDeployPhase_RequiresApproval(t *testing.T) {
	deploy := &config.CIDeployConfig{
		Environments: map[string]*config.CIDeployEnvironment{
			"prod": {Provider: "aws-ecs", RequireApproval: true},
		},
	}
	if err := runDeployPhase(deploy, "prod", false); err != nil {
		t.Fatalf("approval skip should not error: %v", err)
	}
}

func TestRunCIRunDeployRunsMigrationGuardBeforeDeploy(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "app.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
version: 1
ci:
  deploy:
    environments:
      staging:
        provider: aws-ecs
        strategy: rolling
  migrations:
    - name: app
      source_dir: migrations
      database:
        env: DATABASE_URL
`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATABASE_URL", "postgres://secret@example/db")
	restore := stubMigrationStatusRunner(t, migrationCommandResult{
		Stdout: "Current: 20260426000005\nNo pending migrations.\nWARNING: database is in dirty state!\n",
	}, nil)
	defer restore()

	err := runCIRun([]string{"--config", cfgPath, "--phase", "deploy", "--env", "staging"})
	if err == nil {
		t.Fatal("expected deploy to fail before rollout on dirty migration")
	}
	if !strings.Contains(err.Error(), "migration guard failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunCIRunDeploySkipsMigrationGuardWhenNoMigrations(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "app.yaml")
	if err := os.WriteFile(cfgPath, []byte(`
version: 1
ci:
  deploy:
    environments:
      staging:
        provider: aws-ecs
        strategy: rolling
`), 0o644); err != nil {
		t.Fatal(err)
	}
	oldFactory := newMigrationPluginRunner
	newMigrationPluginRunner = func() migrationPluginRunner {
		return migrationPluginRunner{
			exec: func(context.Context, string, []string, map[string]string) (migrationCommandResult, error) {
				t.Fatal("migration guard should not run without ci.migrations")
				return migrationCommandResult{}, nil
			},
		}
	}
	defer func() { newMigrationPluginRunner = oldFactory }()

	if err := runCIRun([]string{"--config", cfgPath, "--phase", "deploy", "--env", "staging"}); err != nil {
		t.Fatalf("deploy without migrations should still run: %v", err)
	}
}

func TestCurrentCICommitSHAUsesWorkflowRunHeadSHA(t *testing.T) {
	eventPath := filepath.Join(t.TempDir(), "event.json")
	if err := os.WriteFile(eventPath, []byte(`{"workflow_run":{"head_sha":"abc123"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_EVENT_PATH", eventPath)
	t.Setenv("GITHUB_SHA", "merge-ref-sha")

	if got := currentCICommitSHA(); got != "abc123" {
		t.Fatalf("currentCICommitSHA() = %q, want abc123", got)
	}
}

func TestCurrentCICommitSHAPrefersExplicitOverride(t *testing.T) {
	t.Setenv("WFCTL_CI_COMMIT_SHA", "override-sha")
	t.Setenv("GITHUB_SHA", "github-sha")

	if got := currentCICommitSHA(); got != "override-sha" {
		t.Fatalf("currentCICommitSHA() = %q, want override-sha", got)
	}
}

func TestCurrentCICommitSHAFallsBackToProviderSHA(t *testing.T) {
	t.Setenv("GITHUB_SHA", "github-sha")

	if got := currentCICommitSHA(); got != "github-sha" {
		t.Fatalf("currentCICommitSHA() = %q, want github-sha", got)
	}
}

func TestRunDeployPhase_Placeholder(t *testing.T) {
	deploy := &config.CIDeployConfig{
		Environments: map[string]*config.CIDeployEnvironment{
			"staging": {Provider: "aws-ecs", Strategy: "rolling"},
		},
	}
	if err := runDeployPhase(deploy, "staging", false); err != nil {
		t.Fatalf("placeholder deploy should not error: %v", err)
	}
}

func TestRunBuildPhase_EmptyBuild(t *testing.T) {
	build := &config.CIBuildConfig{}
	if err := runBuildPhase(build, false); err != nil {
		t.Fatalf("empty build config should not error: %v", err)
	}
}

func TestRunTestPhase_EmptyTest(t *testing.T) {
	test := &config.CITestConfig{}
	if err := runTestPhase(test, false); err != nil {
		t.Fatalf("empty test config should not error: %v", err)
	}
}
