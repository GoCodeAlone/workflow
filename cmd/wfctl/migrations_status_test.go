package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunMigrationsStatusReportsDirty(t *testing.T) {
	cfgPath := writeMigrationStatusConfig(t)
	t.Setenv("DATABASE_URL", "postgres://secret@example/db")
	restore := stubMigrationStatusRunner(t, migrationCommandResult{
		Stdout: "Current: 20260426000005\nDirty: true\nNo pending migrations.\n",
	}, nil)
	defer restore()

	out, err := captureStdout(t, func() error {
		return runMigrations([]string{"status", "--config", cfgPath, "--env", "ci", "--format", "json"})
	})
	if err == nil {
		t.Fatal("expected dirty status error")
	}
	if strings.Contains(err.Error(), "postgres://secret@example/db") {
		t.Fatalf("status error leaked DSN: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode status JSON: %v\n%s", err, out)
	}
	if got["decision"] != "fail" {
		t.Fatalf("decision = %v, want fail", got["decision"])
	}
	reasons := got["reasons"].([]any)
	if len(reasons) != 1 || reasons[0] != "migration app is dirty at version 20260426000005" {
		t.Fatalf("reasons = %#v", reasons)
	}
	migrations := got["migrations"].([]any)
	migration := migrations[0].(map[string]any)
	if migration["name"] != "app" || migration["driver"] != "golang-migrate" || migration["current"] != "20260426000005" || migration["dirty"] != true {
		t.Fatalf("unexpected migration status: %#v", migration)
	}
}

func TestRunMigrationsStatusReportsCurrentPendingDirtyAndDriver(t *testing.T) {
	cfgPath := writeMigrationStatusObserveConfig(t)
	t.Setenv("DATABASE_URL", "postgres://secret@example/db")
	restore := stubMigrationStatusRunner(t, migrationCommandResult{
		Stdout: "Current: 20260426000001\nDirty: false\nPending: [20260426000002 20260426000003]\n",
	}, nil)
	defer restore()

	out, err := captureStdout(t, func() error {
		return runMigrations([]string{"status", "--config", cfgPath, "--env", "ci", "--format", "json"})
	})
	if err != nil {
		t.Fatal(err)
	}

	var got struct {
		Decision   string `json:"decision"`
		Migrations []struct {
			Name    string   `json:"name"`
			Driver  string   `json:"driver"`
			Current string   `json:"current"`
			Pending []string `json:"pending"`
			Dirty   bool     `json:"dirty"`
		} `json:"migrations"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode status JSON: %v\n%s", err, out)
	}
	if got.Decision != "pass" || len(got.Migrations) != 1 {
		t.Fatalf("unexpected status: %+v", got)
	}
	migration := got.Migrations[0]
	if migration.Name != "app" || migration.Driver != "golang-migrate" || migration.Current != "20260426000001" || migration.Dirty {
		t.Fatalf("unexpected migration status: %+v", migration)
	}
	wantPending := strings.Join([]string{"20260426000002", "20260426000003"}, ",")
	if strings.Join(migration.Pending, ",") != wantPending {
		t.Fatalf("pending = %#v, want %s", migration.Pending, wantPending)
	}
}

func writeMigrationStatusConfig(t *testing.T) string {
	t.Helper()
	return writeMigrationStatusConfigData(t, `
version: 1
ci:
  migrations:
    - name: app
      source_dir: migrations
      database:
        env: DATABASE_URL
      validation:
        forbid_dirty: true
`)
}

func writeMigrationStatusObserveConfig(t *testing.T) string {
	t.Helper()
	return writeMigrationStatusConfigData(t, `
version: 1
ci:
  migrations:
    - name: app
      source_dir: migrations
      database:
        env: DATABASE_URL
`)
}

func writeMigrationStatusConfigData(t *testing.T, data string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "infra.yaml")
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func stubMigrationStatusRunner(t *testing.T, result migrationCommandResult, runErr error) func() {
	t.Helper()
	oldFactory := newMigrationPluginRunner
	newMigrationPluginRunner = func() migrationPluginRunner {
		return migrationPluginRunner{
			exec: func(_ context.Context, pluginName string, args []string, env map[string]string) (migrationCommandResult, error) {
				if pluginName != "workflow-plugin-migrations" {
					t.Fatalf("pluginName = %q", pluginName)
				}
				if got := strings.Join(args, " "); !strings.Contains(got, "--wfctl-cli status") {
					t.Fatalf("args = %q, want status", got)
				}
				if env["DATABASE_URL"] != "postgres://secret@example/db" {
					t.Fatalf("DATABASE_URL = %q", env["DATABASE_URL"])
				}
				return result, runErr
			},
		}
	}
	return func() { newMigrationPluginRunner = oldFactory }
}
