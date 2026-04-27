package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRunMigrationsValidateRunsLintAndFreshCycle(t *testing.T) {
	cfgPath := writeMigrationValidateConfig(t)
	t.Setenv("DATABASE_URL", "postgres://secret@example/db")

	var calls []string
	oldEphemeral := migrationEphemeralDB
	migrationEphemeralDB = migrationEphemeralDatabaseOperations{
		Create: func(_ context.Context, name, baseDSN string) (string, func(), error) {
			calls = append(calls, "ephemeral "+name+" "+baseDSN)
			return "postgres://ephemeral/" + name, func() {
				calls = append(calls, "cleanup ephemeral "+name)
			}, nil
		},
	}
	defer func() { migrationEphemeralDB = oldEphemeral }()
	oldFactory := newMigrationPluginRunner
	newMigrationPluginRunner = func() migrationPluginRunner {
		return migrationPluginRunner{
			exec: func(_ context.Context, pluginName string, args []string, env map[string]string) (migrationCommandResult, error) {
				calls = append(calls, pluginName+" "+strings.Join(args, " ")+" "+env["DATABASE_URL"])
				if strings.Contains(strings.Join(args, " "), " test ") && env["DATABASE_URL"] != "postgres://ephemeral/app-fresh" {
					t.Fatalf("fresh cycle used DATABASE_URL = %q", env["DATABASE_URL"])
				}
				if strings.Contains(strings.Join(args, " "), " lint ") && env["DATABASE_URL"] != "postgres://secret@example/db" {
					t.Fatalf("runner env DATABASE_URL = %q", env["DATABASE_URL"])
				}
				return migrationCommandResult{Stdout: `{"dirty":false}`}, nil
			},
		}
	}
	defer func() { newMigrationPluginRunner = oldFactory }()

	err := runMigrations([]string{"validate", "--config", cfgPath, "--env", "ci"})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"workflow-plugin-migrations --wfctl-cli migrate lint --driver golang-migrate --source-dir migrations postgres://secret@example/db",
		"ephemeral app-fresh postgres://secret@example/db",
		"workflow-plugin-migrations --wfctl-cli migrate test --driver golang-migrate --source-dir migrations postgres://ephemeral/app-fresh",
		"cleanup ephemeral app-fresh",
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestRunMigrationsValidateJSONOutput(t *testing.T) {
	cfgPath := writeMigrationValidateConfig(t)
	resultPath := filepath.Join(t.TempDir(), "result.json")
	t.Setenv("DATABASE_URL", "postgres://secret@example/db")
	t.Setenv("WFCTL_MIGRATION_VALIDATION_DATABASE_URL", "postgres://validation@example/db")

	oldFactory := newMigrationPluginRunner
	newMigrationPluginRunner = func() migrationPluginRunner {
		return migrationPluginRunner{
			exec: func(_ context.Context, _ string, _ []string, _ map[string]string) (migrationCommandResult, error) {
				return migrationCommandResult{}, nil
			},
		}
	}
	defer func() { newMigrationPluginRunner = oldFactory }()

	out, err := captureStdout(t, func() error {
		return runMigrations([]string{
			"validate",
			"--config", cfgPath,
			"--env", "ci",
			"--commit", "abc123",
			"--format", "json",
			"--result-file", resultPath,
		})
	})
	if err != nil {
		t.Fatal(err)
	}

	var got migrationValidationResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode stdout: %v\n%s", err, out)
	}
	if got.Decision != "pass" || got.Commit != "abc123" {
		t.Fatalf("unexpected validation result: %+v", got)
	}
	if len(got.Migrations) != 1 || got.Migrations[0].Name != "app" || got.Migrations[0].Lint != "pass" || got.Migrations[0].FreshCycle != "pass" {
		t.Fatalf("unexpected migration result: %+v", got.Migrations)
	}
	data, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatalf("read result file: %v", err)
	}
	if strings.Contains(string(data), "postgres://secret@example/db") {
		t.Fatal("result file leaked DSN")
	}
}

func TestRunMigrationsMissingSubcommand(t *testing.T) {
	err := runMigrations(nil)
	if err == nil {
		t.Fatal("expected missing subcommand error")
	}
	if !strings.Contains(err.Error(), "usage: wfctl migrations") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func writeMigrationValidateConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "infra.yaml")
	data := []byte(`
version: 1
ci:
  migrations:
    - name: app
      source_dir: migrations
      database:
        env: DATABASE_URL
      validation:
        lint: true
        fresh_cycle: true
`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
