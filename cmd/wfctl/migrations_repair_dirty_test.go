package main

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestRunMigrationsRepairDirtyRequiresConfirmation(t *testing.T) {
	cfgPath := writeMigrationStatusConfig(t)
	t.Setenv("DATABASE_URL", "postgres://secret@example/db")

	err := runMigrations([]string{"repair-dirty", "--config", cfgPath, "--env", "staging", "--expected-dirty-version", "20260426000005", "--force-version", "20260426000004"})
	if err == nil {
		t.Fatal("expected confirmation error")
	}
	if !strings.Contains(err.Error(), "confirm-force") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunMigrationsRepairDirtyPassesExactVersionAndThenUp(t *testing.T) {
	cfgPath := writeMigrationStatusConfig(t)
	t.Setenv("DATABASE_URL", "postgres://secret@example/db")
	var calls []string
	restore := stubMigrationRepairRunner(t, &calls, []migrationCommandResult{
		{Stdout: "Current: 20260426000005\nDirty: true\nNo pending migrations.\n"},
		{},
		{Stdout: "Current: 20260426000004\nDirty: false\nNo pending migrations.\n"},
	})
	defer restore()

	if err := runMigrations([]string{"repair-dirty", "--config", cfgPath, "--env", "staging", "--expected-dirty-version", "20260426000005", "--force-version", "20260426000004", "--confirm-force", "FORCE_MIGRATION_METADATA", "--then-up"}); err != nil {
		t.Fatal(err)
	}

	want := []string{
		"status",
		"repair-dirty --expected-dirty-version 20260426000005 --force-version 20260426000004 --confirm-force FORCE_MIGRATION_METADATA --then-up",
		"status",
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestRunMigrationsRepairDirtyApprovalRequiredForProdWithoutApprovedToken(t *testing.T) {
	cfgPath := writeMigrationStatusConfig(t)
	t.Setenv("DATABASE_URL", "postgres://secret@example/db")
	var calls []string
	restore := stubMigrationRepairRunner(t, &calls, nil)
	defer restore()

	out, err := captureStdout(t, func() error {
		return runMigrations([]string{"repair-dirty", "--config", cfgPath, "--env", "prod", "--plugin-dir", "custom/plugins", "--expected-dirty-version", "20260426000005", "--force-version", "20260426000004", "--confirm-force", "FORCE_MIGRATION_METADATA"})
	})
	if err == nil {
		t.Fatal("expected approval-required error")
	}
	if len(calls) != 0 {
		t.Fatalf("repair touched plugin before approval: %#v", calls)
	}
	var got struct {
		Decision              string `json:"decision"`
		Destructive           bool   `json:"destructive"`
		HumanApprovalRequired bool   `json:"human_approval_required"`
		ApprovalCommand       string `json:"approval_command"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode approval JSON: %v\n%s", err, out)
	}
	if got.Decision != "fail" || !got.Destructive || !got.HumanApprovalRequired {
		t.Fatalf("unexpected approval result: %+v", got)
	}
	if !strings.Contains(got.ApprovalCommand, "'wfctl' 'migrations' 'repair-dirty'") || !strings.Contains(got.ApprovalCommand, "'--plugin-dir' 'custom/plugins'") || strings.Contains(got.ApprovalCommand, "postgres://secret@example/db") {
		t.Fatalf("bad approval command: %q", got.ApprovalCommand)
	}
}

func TestRunMigrationsRepairDirtyRejectsWrongApprovalTokenForProd(t *testing.T) {
	cfgPath := writeMigrationStatusConfig(t)
	t.Setenv("DATABASE_URL", "postgres://secret@example/db")
	t.Setenv("WFCTL_MIGRATION_REPAIR_APPROVAL_TOKEN", "expected-token")
	var calls []string
	restore := stubMigrationRepairRunner(t, &calls, nil)
	defer restore()

	err := runMigrations([]string{"repair-dirty", "--config", cfgPath, "--env", "prod", "--expected-dirty-version", "20260426000005", "--force-version", "20260426000004", "--confirm-force", "FORCE_MIGRATION_METADATA", "--approved-token", "wrong-token"})
	if err == nil {
		t.Fatal("expected approval error")
	}
	if len(calls) != 0 {
		t.Fatalf("repair touched plugin with wrong approval token: %#v", calls)
	}
}

func TestRunMigrationsRepairDirtyAcceptsApprovalTokenFromEnvForProd(t *testing.T) {
	cfgPath := writeMigrationStatusConfig(t)
	t.Setenv("DATABASE_URL", "postgres://secret@example/db")
	t.Setenv("WFCTL_MIGRATION_REPAIR_APPROVAL_TOKEN", "expected-token")
	t.Setenv("WFCTL_MIGRATION_REPAIR_APPROVED_TOKEN", "expected-token")
	var calls []string
	restore := stubMigrationRepairRunner(t, &calls, []migrationCommandResult{
		{Stdout: "Current: 20260426000005\nDirty: true\nNo pending migrations.\n"},
		{},
		{Stdout: "Current: 20260426000004\nDirty: false\nNo pending migrations.\n"},
	})
	defer restore()

	err := runMigrations([]string{"repair-dirty", "--config", cfgPath, "--env", "prod", "--expected-dirty-version", "20260426000005", "--force-version", "20260426000004", "--confirm-force", "FORCE_MIGRATION_METADATA", "--approved-token-env", "WFCTL_MIGRATION_REPAIR_APPROVED_TOKEN"})
	if err != nil {
		t.Fatalf("expected env approval token to allow repair: %v", err)
	}
	if !containsMigrationString(calls, "repair-dirty --expected-dirty-version 20260426000005 --force-version 20260426000004 --confirm-force FORCE_MIGRATION_METADATA") {
		t.Fatalf("plugin calls = %#v; want repair-dirty", calls)
	}
}

func TestRunMigrationsRepairDirtyUpIfCleanIsIdempotent(t *testing.T) {
	cfgPath := writeMigrationStatusConfig(t)
	t.Setenv("DATABASE_URL", "postgres://secret@example/db")
	var calls []string
	restore := stubMigrationRepairRunner(t, &calls, []migrationCommandResult{
		{Stdout: "Current: 20260426000004\nDirty: false\nNo pending migrations.\n"},
		{},
		{Stdout: "Current: 20260426000004\nDirty: false\nNo pending migrations.\n"},
	})
	defer restore()

	if err := runMigrations([]string{"repair-dirty", "--config", cfgPath, "--env", "staging", "--expected-dirty-version", "20260426000005", "--force-version", "20260426000004", "--confirm-force", "FORCE_MIGRATION_METADATA", "--up-if-clean"}); err != nil {
		t.Fatal(err)
	}
	want := []string{"status", "up", "status"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestRunMigrationsRepairDirtyUpIfCleanFailsIfPostUpIsDirty(t *testing.T) {
	cfgPath := writeMigrationStatusConfig(t)
	t.Setenv("DATABASE_URL", "postgres://secret@example/db")
	var calls []string
	restore := stubMigrationRepairRunner(t, &calls, []migrationCommandResult{
		{Stdout: "Current: 20260426000004\nDirty: false\nNo pending migrations.\n"},
		{},
		{Stdout: "Current: 20260426000005\nDirty: true\nNo pending migrations.\n"},
	})
	defer restore()

	err := runMigrations([]string{"repair-dirty", "--config", cfgPath, "--env", "staging", "--expected-dirty-version", "20260426000005", "--force-version", "20260426000004", "--confirm-force", "FORCE_MIGRATION_METADATA", "--up-if-clean"})
	if err == nil {
		t.Fatal("runMigrations() error = nil; want dirty post-up failure")
	}
	if !strings.Contains(err.Error(), "dirty after up") {
		t.Fatalf("runMigrations() error = %v; want dirty after up", err)
	}
	want := []string{"status", "up", "status"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestRunMigrationsRepairDirtyPrintsPostRepairStatus(t *testing.T) {
	cfgPath := writeMigrationStatusConfig(t)
	t.Setenv("DATABASE_URL", "postgres://secret@example/db")
	var calls []string
	restore := stubMigrationRepairRunner(t, &calls, []migrationCommandResult{
		{Stdout: "Current: 20260426000005\nDirty: true\nNo pending migrations.\n"},
		{},
		{Stdout: "Current: 20260426000004\nDirty: false\nNo pending migrations.\n"},
	})
	defer restore()

	out, err := captureStdout(t, func() error {
		return runMigrations([]string{"repair-dirty", "--config", cfgPath, "--env", "staging", "--expected-dirty-version", "20260426000005", "--force-version", "20260426000004", "--confirm-force", "FORCE_MIGRATION_METADATA", "--format", "json"})
	})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Decision  string                    `json:"decision"`
		Migration migrationValidationRecord `json:"migration"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode repair JSON: %v\n%s", err, out)
	}
	if got.Decision != "pass" || got.Migration.Current != "20260426000004" || got.Migration.Dirty {
		t.Fatalf("unexpected repair result: %+v", got)
	}
}

func TestRunMigrationsRepairDirtyTypedConfirmationRepairsDirtyState(t *testing.T) {
	cfgPath := writeMigrationStatusConfig(t)
	t.Setenv("DATABASE_URL", "postgres://secret@example/db")
	var calls []string
	restore := stubMigrationRepairRunner(t, &calls, []migrationCommandResult{
		{Stdout: "Current: 20260426000005\nDirty: true\nNo pending migrations.\n"},
		{},
		{Stdout: "Current: 20260426000004\nDirty: false\nNo pending migrations.\n"},
	})
	defer restore()

	if err := runMigrations([]string{"repair-dirty", "--config", cfgPath, "--env", "staging", "--expected-dirty-version", "20260426000005", "--force-version", "20260426000004", "--confirm-force", "FORCE_MIGRATION_METADATA"}); err != nil {
		t.Fatal(err)
	}
	if !containsMigrationString(calls, "repair-dirty --expected-dirty-version 20260426000005 --force-version 20260426000004 --confirm-force FORCE_MIGRATION_METADATA") {
		t.Fatalf("repair command not invoked: %#v", calls)
	}
}

func stubMigrationRepairRunner(t *testing.T, calls *[]string, results []migrationCommandResult) func() {
	t.Helper()
	oldFactory := newMigrationPluginRunner
	newMigrationPluginRunner = func() migrationPluginRunner {
		return migrationPluginRunner{
			exec: func(_ context.Context, _ string, args []string, env map[string]string) (migrationCommandResult, error) {
				if env["DATABASE_URL"] != "postgres://secret@example/db" {
					t.Fatalf("DATABASE_URL = %q", env["DATABASE_URL"])
				}
				command := migrationCommandFromArgs(args)
				*calls = append(*calls, command)
				if len(results) == 0 {
					return migrationCommandResult{}, nil
				}
				result := results[0]
				results = results[1:]
				return result, nil
			},
		}
	}
	return func() { newMigrationPluginRunner = oldFactory }
}

func containsMigrationString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
