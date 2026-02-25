package main

import (
	"os"
	"strings"
	"testing"
)

func TestRunGitPushNoWfctlYaml(t *testing.T) {
	dir := t.TempDir()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working dir: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	err = runGitPush([]string{})
	if err == nil {
		t.Fatal("expected error when .wfctl.yaml is missing")
	}
}

func TestRunGitPushNoRepository(t *testing.T) {
	dir := t.TempDir()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working dir: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	// Write a .wfctl.yaml without a repository
	content := `project:
  name: test
  configFile: workflow.yaml
git:
  branch: main
`
	if err := os.WriteFile(".wfctl.yaml", []byte(content), 0640); err != nil {
		t.Fatalf("failed to write .wfctl.yaml: %v", err)
	}

	err = runGitPush([]string{})
	if err == nil {
		t.Fatal("expected error when no repository is configured")
	}
	if !strings.Contains(err.Error(), "no repository configured") {
		t.Errorf("expected 'no repository configured' in error, got: %v", err)
	}
}

func TestCreateAndPushTagNotGitRepo(t *testing.T) {
	dir := t.TempDir()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working dir: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	// Not a git repo — git tag should fail
	err = createAndPushTag("v1.0.0", "test release")
	if err == nil {
		t.Fatal("expected error when not in a git repo")
	}
}

// TestGitPushConfigOnlyLogic tests the staging logic paths without requiring a real git repo.
// It validates the configOnlyFiles helper returns appropriate sets.
func TestGitPushConfigOnlyLogic(t *testing.T) {
	dir := t.TempDir()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working dir: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	cfg := &wfctlConfig{ConfigFile: "myconfig.yaml"}

	// Base: just .wfctl.yaml and config file
	files := configOnlyFiles(cfg)
	if len(files) < 2 {
		t.Errorf("expected at least 2 files, got %d: %v", len(files), files)
	}

	hasWfctl := false
	hasConfig := false
	for _, f := range files {
		if f == ".wfctl.yaml" {
			hasWfctl = true
		}
		if f == "myconfig.yaml" {
			hasConfig = true
		}
	}
	if !hasWfctl {
		t.Error("expected .wfctl.yaml in result")
	}
	if !hasConfig {
		t.Error("expected myconfig.yaml in result")
	}
}

// TestGitPushFlagParsing checks that invalid flags are rejected.
func TestGitPushFlagParsing(t *testing.T) {
	dir := t.TempDir()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working dir: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	// Should fail because of missing .wfctl.yaml, not because of bad flags
	err = runGitPush([]string{"-message", "test commit", "-config-only"})
	if err == nil {
		t.Fatal("expected error (no .wfctl.yaml)")
	}
	// Should not be a flag parsing error
	if strings.Contains(err.Error(), "flag") {
		t.Errorf("unexpected flag parse error: %v", err)
	}
}
