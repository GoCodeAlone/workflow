package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunMigrationsCICheckFailsClosedOnDirty(t *testing.T) {
	cfgPath := writeMigrationStatusConfig(t)
	t.Setenv("DATABASE_URL", "postgres://secret@example/db")
	restore := stubMigrationStatusRunner(t, migrationCommandResult{
		Stdout: "Current: 20260426000005\nDirty: true\nNo pending migrations.\n",
	}, nil)
	defer restore()

	out, err := captureStdout(t, func() error {
		return runMigrations([]string{"ci-check", "--config", cfgPath, "--env", "ci", "--format", "json"})
	})
	if err == nil {
		t.Fatal("expected dirty ci-check error")
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode ci-check JSON: %v\n%s", err, out)
	}
	if got["decision"] != "fail" || got["destructive"] != false || got["human_approval_required"] != false {
		t.Fatalf("unexpected ci-check result: %#v", got)
	}
	reasons := got["reasons"].([]any)
	if len(reasons) != 1 || reasons[0] != "migration app is dirty at version 20260426000005" {
		t.Fatalf("reasons = %#v", reasons)
	}
}

func TestRunMigrationsCICheckRequiresPassingValidationResultForSHA(t *testing.T) {
	cfgPath := writeMigrationStatusConfig(t)
	resultPath := writeMigrationValidationResultFixture(t, migrationValidationResult{
		Decision: "pass",
		Commit:   "abc123",
		Migrations: []migrationValidationRecord{{
			Name:       "app",
			Lint:       "pass",
			FreshCycle: "pass",
			Dirty:      false,
		}},
	})
	t.Setenv("DATABASE_URL", "postgres://secret@example/db")
	restore := stubMigrationStatusRunner(t, migrationCommandResult{
		Stdout: "Current: 20260426000005\nNo pending migrations.\n",
	}, nil)
	defer restore()

	if _, err := captureStdout(t, func() error {
		return runMigrations([]string{"ci-check", "--config", cfgPath, "--env", "ci", "--commit", "abc123", "--validation-result", resultPath, "--require-validation-result", "--format", "json"})
	}); err != nil {
		t.Fatalf("expected matching validation result to pass: %v", err)
	}

	out, err := captureStdout(t, func() error {
		return runMigrations([]string{"ci-check", "--config", cfgPath, "--env", "ci", "--commit", "different", "--validation-result", resultPath, "--require-same-sha", "--format", "json"})
	})
	if err == nil {
		t.Fatal("expected sha mismatch error")
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode ci-check JSON: %v\n%s", err, out)
	}
	reasons := got["reasons"].([]any)
	if len(reasons) != 1 || !strings.Contains(reasons[0].(string), "validation result commit abc123 does not match different") {
		t.Fatalf("reasons = %#v", reasons)
	}
}

func TestRunMigrationsCICheckFailsClosedWhenPluginLoadFails(t *testing.T) {
	cfgPath := writeMigrationStatusConfig(t)
	t.Setenv("DATABASE_URL", "postgres://secret@example/db")
	restore := stubMigrationStatusRunner(t, migrationCommandResult{}, errors.New("plugin missing at postgres://secret@example/db"))
	defer restore()

	out, err := captureStdout(t, func() error {
		return runMigrations([]string{"ci-check", "--config", cfgPath, "--env", "ci", "--format", "json"})
	})
	if err == nil {
		t.Fatal("expected plugin error")
	}
	if strings.Contains(err.Error(), "postgres://secret@example/db") || strings.Contains(out, "postgres://secret@example/db") {
		t.Fatalf("ci-check leaked DSN: err=%v out=%s", err, out)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode ci-check JSON: %v\n%s", err, out)
	}
	if got["decision"] != "fail" {
		t.Fatalf("decision = %v, want fail", got["decision"])
	}
	reasons := got["reasons"].([]any)
	if len(reasons) != 1 || !strings.Contains(reasons[0].(string), "migration app status failed") {
		t.Fatalf("reasons = %#v", reasons)
	}
}

func writeMigrationValidationResultFixture(t *testing.T, result migrationValidationResult) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "migrations-result.json")
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
