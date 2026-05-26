package main

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestRunMigrationsUpAppliesAndVerifiesCleanStatus(t *testing.T) {
	cfgPath := writeMigrationStatusConfig(t)
	t.Setenv("DATABASE_URL", "postgres://secret@example/db")
	var calls []string
	restore := stubMigrationUpRunner(t, &calls, []migrationCommandResult{
		{},
		{Stdout: "Current: 20260426000002\nDirty: false\nNo pending migrations.\n"},
	})
	defer restore()

	if err := runMigrations([]string{"up", "--config", cfgPath, "--env", "prod"}); err != nil {
		t.Fatal(err)
	}

	want := []string{"up", "status"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestRunMigrationsUpFailsWhenPostStatusIsDirty(t *testing.T) {
	cfgPath := writeMigrationStatusConfig(t)
	t.Setenv("DATABASE_URL", "postgres://secret@example/db")
	var calls []string
	restore := stubMigrationUpRunner(t, &calls, []migrationCommandResult{
		{},
		{Stdout: "Current: 20260426000002\nDirty: true\nNo pending migrations.\n"},
	})
	defer restore()

	err := runMigrations([]string{"up", "--config", cfgPath, "--env", "prod"})
	if err == nil {
		t.Fatal("runMigrations() error = nil; want dirty post-up failure")
	}
	if !strings.Contains(err.Error(), "dirty after up") {
		t.Fatalf("runMigrations() error = %v; want dirty after up", err)
	}
}

func TestRunMigrationsUpFailsWhenPostStatusHasPendingMigrations(t *testing.T) {
	cfgPath := writeMigrationStatusConfig(t)
	t.Setenv("DATABASE_URL", "postgres://secret@example/db")
	var calls []string
	restore := stubMigrationUpRunner(t, &calls, []migrationCommandResult{
		{},
		{Stdout: "Current: 20260426000002\nDirty: false\nPending: 20260426000003\n"},
	})
	defer restore()

	err := runMigrations([]string{"up", "--config", cfgPath, "--env", "prod"})
	if err == nil {
		t.Fatal("runMigrations() error = nil; want pending migration failure")
	}
	if !strings.Contains(err.Error(), "pending migrations") {
		t.Fatalf("runMigrations() error = %v; want pending migrations", err)
	}
}

func TestRunMigrationsUpJSONOutputRedactsDSN(t *testing.T) {
	cfgPath := writeMigrationStatusConfig(t)
	t.Setenv("DATABASE_URL", "postgres://secret@example/db")
	var calls []string
	restore := stubMigrationUpRunner(t, &calls, []migrationCommandResult{
		{},
		{Stdout: "Current: 20260426000002\nDirty: false\nNo pending migrations.\n"},
	})
	defer restore()

	out, err := captureStdout(t, func() error {
		return runMigrations([]string{"up", "--config", cfgPath, "--env", "prod", "--format", "json"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "postgres://secret@example/db") {
		t.Fatalf("output leaked DSN: %s", out)
	}
	var got migrationStatusResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, out)
	}
	if got.Decision != "pass" || len(got.Migrations) != 1 || got.Migrations[0].Current != "20260426000002" {
		t.Fatalf("unexpected result: %+v", got)
	}
	if len(got.Migrations[0].Pending) != 0 {
		t.Fatalf("pending = %#v", got.Migrations[0].Pending)
	}
}

func stubMigrationUpRunner(t *testing.T, calls *[]string, results []migrationCommandResult) func() {
	t.Helper()
	oldFactory := newMigrationPluginRunner
	newMigrationPluginRunner = func() migrationPluginRunner {
		return migrationPluginRunner{
			exec: func(_ context.Context, _ string, args []string, env map[string]string) (migrationCommandResult, error) {
				if env["DATABASE_URL"] != "postgres://secret@example/db" {
					t.Fatalf("DATABASE_URL = %q", env["DATABASE_URL"])
				}
				*calls = append(*calls, migrationCommandFromArgs(args))
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
