package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func TestRunBuild_DryRunPrintsPlannedActions(t *testing.T) {
	dir := t.TempDir()
	cfg := `ci:
  build:
    targets:
      - name: server
        type: go
        path: ./cmd/server
      - name: ui
        type: nodejs
        path: ./ui
        config:
          script: build
`
	cfgPath := filepath.Join(dir, "build.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	// runBuild with --dry-run should print planned actions without executing.
	err := runBuild([]string{"--config", cfgPath, "--dry-run"})
	if err != nil {
		t.Fatalf("runBuild --dry-run: %v", err)
	}
}

func TestRunBuild_UnknownSubcommandError(t *testing.T) {
	err := runBuild([]string{"no-such-subcommand"})
	if err == nil {
		t.Fatal("want error for unknown subcommand")
	}
	if !strings.Contains(err.Error(), "no-such-subcommand") {
		t.Fatalf("error should mention unknown subcommand, got: %v", err)
	}
}

func TestRunBuild_NoBuildConfigSkipsByDefault(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(cfgPath, []byte("name: fallback-test\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error {
		return runBuild([]string{"--config", cfgPath})
	})
	if err != nil {
		t.Fatalf("runBuild without fallback: %v", err)
	}
	if !strings.Contains(out, "No build configuration, skipping build phase") {
		t.Fatalf("expected default skip message, got:\n%s", out)
	}
}

func TestRunBuild_FallbackGoBuildDryRun(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(cfgPath, []byte("name: fallback-test\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error {
		return runBuild([]string{"--config", cfgPath, "--fallback-go-build", "--dry-run"})
	})
	if err != nil {
		t.Fatalf("runBuild fallback dry-run: %v", err)
	}
	if !strings.Contains(out, "[dry-run] go build ./...") {
		t.Fatalf("expected go build dry-run fallback, got:\n%s", out)
	}
}

func TestRunBuild_FallbackGoBuildRunsWhenNoTargets(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(cfgPath, []byte("name: fallback-test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fallback.test\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.go"), []byte("package main\n\nfunc main() { missing }\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()

	err = runBuild([]string{"--config", cfgPath, "--fallback-go-build"})
	if err == nil {
		t.Fatal("expected fallback go build to run and fail on uncompilable source")
	}
}

func TestHasConfiguredBuildTargetsIgnoresSecurityOnlyConfig(t *testing.T) {
	build := &config.CIBuildConfig{
		Security: &config.CIBuildSecurity{Hardened: true},
	}
	if hasConfiguredBuildTargets(build) {
		t.Fatal("security-only build config should not count as configured build targets")
	}
}
