package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

func TestDestructiveGate_StagingWithoutApprovalWritesExplicitArtifact(t *testing.T) {
	artifactPath := filepath.Join(t.TempDir(), "approval.json")

	result, err := requireDestructiveApproval(testDestructiveDecision("staging"), false, artifactPath)
	if err == nil {
		t.Fatal("expected approval error")
	}
	if !strings.Contains(err.Error(), "approval required") {
		t.Fatalf("error = %q, want approval required", err)
	}
	if result == nil || result.Status != interfaces.MigrationRepairStatusApprovalRequired {
		t.Fatalf("result = %+v, want status %q", result, interfaces.MigrationRepairStatusApprovalRequired)
	}

	decision := readDestructiveDecisionArtifact(t, artifactPath)
	assertDestructiveDecision(t, decision, "staging")
}

func TestDestructiveGate_StagingWithoutApprovalWritesDefaultLocalArtifact(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv("RUNNER_TEMP", "")
	t.Chdir(dir)

	result, err := requireDestructiveApproval(testDestructiveDecision("staging"), false, "")
	if err == nil {
		t.Fatal("expected approval error")
	}
	if result == nil || result.Status != interfaces.MigrationRepairStatusApprovalRequired {
		t.Fatalf("result = %+v, want status %q", result, interfaces.MigrationRepairStatusApprovalRequired)
	}

	artifactPath := filepath.Join(dir, "wfctl-destructive-approval.json")
	decision := readDestructiveDecisionArtifact(t, artifactPath)
	assertDestructiveDecision(t, decision, "staging")
}

func TestDestructiveGate_ProdWithoutApprovalWritesDefaultArtifact(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv("RUNNER_TEMP", "")
	t.Chdir(dir)

	result, err := requireDestructiveApproval(testDestructiveDecision("prod"), false, "")
	if err == nil {
		t.Fatal("expected approval error")
	}
	if result == nil || result.Status != interfaces.MigrationRepairStatusApprovalRequired {
		t.Fatalf("result = %+v, want status %q", result, interfaces.MigrationRepairStatusApprovalRequired)
	}

	artifactPath := filepath.Join(dir, "wfctl-destructive-approval.json")
	decision := readDestructiveDecisionArtifact(t, artifactPath)
	assertDestructiveDecision(t, decision, "prod")
}

func TestDestructiveGate_GitHubActionsDefaultsArtifactToRunnerTemp(t *testing.T) {
	runnerTemp := t.TempDir()
	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("RUNNER_TEMP", runnerTemp)

	result, err := requireDestructiveApproval(testDestructiveDecision("staging"), false, "")
	if err == nil {
		t.Fatal("expected approval error")
	}
	if result == nil || result.Status != interfaces.MigrationRepairStatusApprovalRequired {
		t.Fatalf("result = %+v, want status %q", result, interfaces.MigrationRepairStatusApprovalRequired)
	}

	artifactPath := filepath.Join(runnerTemp, "wfctl-destructive-approval.json")
	decision := readDestructiveDecisionArtifact(t, artifactPath)
	assertDestructiveDecision(t, decision, "staging")
}

func TestDestructiveGate_DevExecutesWithoutExplicitApproval(t *testing.T) {
	result, err := requireDestructiveApproval(testDestructiveDecision("dev"), false, filepath.Join(t.TempDir(), "approval.json"))
	if err != nil {
		t.Fatalf("requireDestructiveApproval: %v", err)
	}
	if result != nil {
		t.Fatalf("result = %+v, want nil", result)
	}
}

func TestDestructiveGate_StagingWithApprovalExecutes(t *testing.T) {
	artifactPath := filepath.Join(t.TempDir(), "approval.json")

	result, err := requireDestructiveApproval(testDestructiveDecision("staging"), true, artifactPath)
	if err != nil {
		t.Fatalf("requireDestructiveApproval: %v", err)
	}
	if result != nil {
		t.Fatalf("result = %+v, want nil", result)
	}
	if _, err := os.Stat(artifactPath); !os.IsNotExist(err) {
		t.Fatalf("artifact exists after approved gate: stat err = %v", err)
	}
}

func testDestructiveDecision(env string) destructiveDecision {
	return destructiveDecision{
		Operation:            "migration_repair_dirty",
		Env:                  env,
		App:                  "bmw-app",
		Database:             "bmw-database",
		ExpectedDirtyVersion: "20260426000005",
		ForceVersion:         "20260422000001",
	}
}

func readDestructiveDecisionArtifact(t *testing.T, path string) destructiveDecision {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read approval artifact: %v", err)
	}
	if !strings.Contains(string(data), `"operation":"migration_repair_dirty"`) {
		t.Fatalf("artifact %s missing compact operation JSON: %s", path, data)
	}
	var decision destructiveDecision
	if err := json.Unmarshal(data, &decision); err != nil {
		t.Fatalf("decode approval artifact: %v", err)
	}
	return decision
}

func assertDestructiveDecision(t *testing.T, got destructiveDecision, env string) {
	t.Helper()
	if got.Operation != "migration_repair_dirty" {
		t.Fatalf("operation = %q", got.Operation)
	}
	if got.Env != env {
		t.Fatalf("env = %q, want %q", got.Env, env)
	}
	if got.App != "bmw-app" {
		t.Fatalf("app = %q", got.App)
	}
	if got.Database != "bmw-database" {
		t.Fatalf("database = %q", got.Database)
	}
	if got.ExpectedDirtyVersion != "20260426000005" {
		t.Fatalf("expected_dirty_version = %q", got.ExpectedDirtyVersion)
	}
	if got.ForceVersion != "20260422000001" {
		t.Fatalf("force_version = %q", got.ForceVersion)
	}
	if !got.RequiresApproval {
		t.Fatal("requires_approval = false, want true")
	}
}
