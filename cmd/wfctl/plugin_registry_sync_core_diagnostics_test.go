package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPluginRegistrySyncCore_PreservesCompilerDiagnosticForDisplay(t *testing.T) {
	workflowRepo := t.TempDir()
	if err := os.WriteFile(filepath.Join(workflowRepo, "go.mod"), []byte("module example.com/core-inspector-fixture\n\ngo 99.0.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOENV", "off")
	t.Setenv("GOTOOLCHAIN", "local")
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOSUMDB", "off")
	registryDir := t.TempDir()

	// Exercise the real compiler subprocess, command boundary, and CLI display
	// helper. A future Go requirement fails before any dependency resolution.
	err := runPluginRegistrySyncCore([]string{"--fix", "--workflow-repo", workflowRepo, "--registry-dir", registryDir})
	if err == nil {
		t.Fatal("expected compiler version failure")
	}
	displayed := commandErrorForDisplay(fmt.Errorf("pipeline step failed: %w", err))
	for _, want := range []string{"inspect workflow core plugins:", "requires go >= 99.0.0", "GOTOOLCHAIN=local"} {
		if !strings.Contains(displayed.Error(), want) {
			t.Errorf("displayed error %q lost compiler diagnostic %q", displayed, want)
		}
	}
	if exitErr, ok := errors.AsType[*exec.ExitError](displayed); !ok || exitErr.ExitCode() != 1 {
		t.Errorf("displayed error must retain compiler exit status 1: %v", displayed)
	}
	entries, err := os.ReadDir(workflowRepo)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "go.mod" {
		t.Errorf("inspector scratch directory was not cleaned up: %v", entries)
	}
	entries, err = os.ReadDir(registryDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("failed inspection changed registry: %v", entries)
	}
}

func TestPluginRegistrySyncCore_PreservesMissingCompilerContext(t *testing.T) {
	workflowRepo := t.TempDir()
	if err := os.WriteFile(filepath.Join(workflowRepo, "go.mod"), []byte("module example.com/core-inspector-fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	err := runPluginRegistrySyncCore([]string{"--workflow-repo", workflowRepo, "--registry-dir", t.TempDir()})
	if err == nil {
		t.Fatal("expected missing compiler failure")
	}
	displayed := commandErrorForDisplay(fmt.Errorf("pipeline step failed: %w", err))
	if !strings.Contains(displayed.Error(), "inspect workflow core plugins:") {
		t.Errorf("displayed error %q lost inspector context", displayed)
	}
	if !errors.Is(displayed, exec.ErrNotFound) {
		t.Errorf("displayed error must retain exec.ErrNotFound: %v", displayed)
	}
}
