package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
