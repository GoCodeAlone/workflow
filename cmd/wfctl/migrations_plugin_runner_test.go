package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestMigrationPluginRunnerBuildsWorkflowMigrateArgs(t *testing.T) {
	var gotPlugin string
	var gotArgs []string
	var gotEnv map[string]string
	runner := migrationPluginRunner{
		exec: func(_ context.Context, pluginName string, args []string, env map[string]string) (migrationCommandResult, error) {
			gotPlugin = pluginName
			gotArgs = append([]string(nil), args...)
			gotEnv = env
			return migrationCommandResult{Stdout: "ok"}, nil
		},
	}

	result, err := runner.run(context.Background(), migrationPluginRunConfig{
		Plugin:    "workflow-plugin-migrations",
		Driver:    "golang-migrate",
		SourceDir: "migrations",
		DSN:       "postgres://user:secret@example.com/app",
		PluginDir: "custom/plugins",
	}, "test")
	if err != nil {
		t.Fatal(err)
	}

	if result.Stdout != "ok" {
		t.Fatalf("stdout = %q, want ok", result.Stdout)
	}
	if gotPlugin != "workflow-plugin-migrations" {
		t.Fatalf("plugin = %q", gotPlugin)
	}
	wantArgs := []string{
		"--wfctl-cli",
		"migrate",
		"test",
		"--driver",
		"golang-migrate",
		"--source-dir",
		"migrations",
		"--dsn-env",
		"WFCTL_MIGRATION_DSN",
	}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("args = %#v, want %#v", gotArgs, wantArgs)
	}
	if gotEnv["WFCTL_MIGRATION_DSN"] != "postgres://user:secret@example.com/app" {
		t.Fatalf("env did not receive DSN: %#v", gotEnv)
	}
}

func TestMigrationPluginRunnerRedactsDSNInErrorsAndOutput(t *testing.T) {
	const secretDSN = "postgres://user:super-secret@example.com/app"
	runner := migrationPluginRunner{
		exec: func(_ context.Context, _ string, _ []string, _ map[string]string) (migrationCommandResult, error) {
			return migrationCommandResult{
				Stdout: "connected to " + secretDSN,
				Stderr: "failed for " + secretDSN,
			}, errors.New("migration failed for " + secretDSN)
		},
	}

	result, err := runner.run(context.Background(), migrationPluginRunConfig{
		Plugin:    "workflow-plugin-migrations",
		Driver:    "golang-migrate",
		SourceDir: "migrations",
		DSN:       secretDSN,
	}, "test")
	if err == nil {
		t.Fatal("expected error")
	}

	msg := err.Error()
	if strings.Contains(msg, secretDSN) {
		t.Fatalf("error leaked DSN: %s", msg)
	}
	if !strings.Contains(msg, "[REDACTED_DSN]") {
		t.Fatalf("error did not contain redaction marker: %s", msg)
	}
	if strings.Contains(result.Stdout, secretDSN) || strings.Contains(result.Stderr, secretDSN) {
		t.Fatalf("output leaked DSN: stdout=%q stderr=%q", result.Stdout, result.Stderr)
	}
}

func TestMigrationPluginRunnerUsesConfiguredPluginDir(t *testing.T) {
	gotPluginDir := ""
	runner := migrationPluginRunner{
		exec: func(_ context.Context, _ string, _ []string, env map[string]string) (migrationCommandResult, error) {
			gotPluginDir = env["WFCTL_PLUGIN_DIR"]
			return migrationCommandResult{}, nil
		},
	}

	_, err := runner.run(context.Background(), migrationPluginRunConfig{
		Plugin:    "workflow-plugin-migrations",
		PluginDir: "custom/plugins",
		Driver:    "golang-migrate",
		SourceDir: "migrations",
		DSN:       "postgres://user:secret@example.com/app",
	}, "status")
	if err != nil {
		t.Fatal(err)
	}
	if gotPluginDir != "custom/plugins" {
		t.Fatalf("WFCTL_PLUGIN_DIR = %q, want custom/plugins", gotPluginDir)
	}
}
