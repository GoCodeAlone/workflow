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
		"--dsn",
		"postgres://user:secret@example.com/app",
	}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("args = %#v, want %#v", gotArgs, wantArgs)
	}
	if gotEnv != nil {
		t.Fatalf("env = %#v, want nil", gotEnv)
	}
}

func TestMigrationPluginRunnerRedactsDSNInErrors(t *testing.T) {
	const secretDSN = "postgres://user:super-secret@example.com/app"
	runner := migrationPluginRunner{
		exec: func(_ context.Context, _ string, _ []string, _ map[string]string) (migrationCommandResult, error) {
			return migrationCommandResult{}, errors.New("migration failed for " + secretDSN)
		},
	}

	_, err := runner.run(context.Background(), migrationPluginRunConfig{
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
}
