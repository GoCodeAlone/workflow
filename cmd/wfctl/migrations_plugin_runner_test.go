package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
		"test",
		"--driver",
		"golang-migrate",
		"--source-dir",
		"migrations",
	}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("args = %#v, want %#v", gotArgs, wantArgs)
	}
	if gotEnv["DATABASE_URL"] != "postgres://user:secret@example.com/app" {
		t.Fatalf("env did not receive DSN: %#v", gotEnv)
	}
}

func TestBuildMigrationPluginArgsMatchPluginRootContract(t *testing.T) {
	got := buildMigrationPluginArgs(migrationPluginRunConfig{
		Driver:    "golang-migrate",
		SourceDir: "migrations",
	}, []string{"status"})
	want := []string{"--wfctl-cli", "status", "--driver", "golang-migrate", "--source-dir", "migrations"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func TestBuildMigrationPluginLintArgsMatchPluginContract(t *testing.T) {
	got := buildMigrationPluginLintArgs(migrationPluginRunConfig{SourceDir: "migrations"})
	want := []string{"--wfctl-cli", "lint", "migrations"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
}

func TestMigrationPluginRunnerRedactsDSNInErrorsAndOutput(t *testing.T) {
	const secretDSN = "postgres://user:super-secret@example.com/app?sslmode=require"
	runner := migrationPluginRunner{
		exec: func(_ context.Context, _ string, _ []string, _ map[string]string) (migrationCommandResult, error) {
			return migrationCommandResult{
				Stdout: "connected to " + secretDSN,
				Stderr: "failed for postgres://user:super-secret@example.com/app",
			}, errors.New("migration failed for password super-secret")
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
	if strings.Contains(result.Stdout, secretDSN) || strings.Contains(result.Stderr, "super-secret") || strings.Contains(msg, "super-secret") {
		t.Fatalf("output leaked DSN: stdout=%q stderr=%q", result.Stdout, result.Stderr)
	}
}

func TestDefaultMigrationPluginExecutorDoesNotInheritProcessSecrets(t *testing.T) {
	root := t.TempDir()
	pluginDir := filepath.Join(root, "migrations")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	envPath := filepath.Join(root, "env.txt")
	binaryPath := filepath.Join(pluginDir, "migrations")
	script := "#!/bin/sh\n/usr/bin/env > " + envPath + "\n"
	if err := os.WriteFile(binaryPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LEAKED_CI_SECRET", "must-not-reach-plugin")

	_, err := defaultMigrationPluginExecutor(context.Background(), "workflow-plugin-migrations", []string{"--wfctl-cli", "migrate", "status"}, map[string]string{
		"DATABASE_URL":      "postgres://user:secret@example.com/app",
		"WFCTL_PLUGIN_DIR":  root,
		"PLUGIN_PUBLIC_VAR": "ok",
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	envDump := string(data)
	if strings.Contains(envDump, "LEAKED_CI_SECRET") || strings.Contains(envDump, "must-not-reach-plugin") {
		t.Fatalf("plugin inherited process secret env:\n%s", envDump)
	}
	if !strings.Contains(envDump, "DATABASE_URL=postgres://user:secret@example.com/app") || !strings.Contains(envDump, "PLUGIN_PUBLIC_VAR=ok") {
		t.Fatalf("plugin did not receive explicit env:\n%s", envDump)
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

func TestMigrationPluginRunnerRejectsUnsafePluginName(t *testing.T) {
	runner := migrationPluginRunner{
		exec: func(context.Context, string, []string, map[string]string) (migrationCommandResult, error) {
			t.Fatal("unsafe plugin name must be rejected before exec")
			return migrationCommandResult{}, nil
		},
	}

	_, err := runner.run(context.Background(), migrationPluginRunConfig{
		Plugin:    "../workflow-plugin-migrations",
		Driver:    "golang-migrate",
		SourceDir: "migrations",
		DSN:       "postgres://user:secret@example.com/app",
	}, "status")
	if err == nil {
		t.Fatal("expected unsafe plugin name error")
	}
	if !strings.Contains(err.Error(), "unsafe plugin name") {
		t.Fatalf("unexpected error: %v", err)
	}
}
