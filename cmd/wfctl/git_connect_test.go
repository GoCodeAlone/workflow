package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunGitNoSubcommand(t *testing.T) {
	err := runGit([]string{})
	if err == nil {
		t.Fatal("expected error when no subcommand given")
	}
}

func TestRunGitUnknownSubcommand(t *testing.T) {
	err := runGit([]string{"unknown"})
	if err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
}

func TestRunGitConnectNoRepo(t *testing.T) {
	err := runGitConnect([]string{})
	if err == nil {
		t.Fatal("expected error when no -repo given")
	}
}

func TestRunGitConnectInvalidRepo(t *testing.T) {
	err := runGitConnect([]string{"-repo", "no-slash-here"})
	if err == nil {
		t.Fatal("expected error for invalid repo format")
	}
	if !strings.Contains(err.Error(), "invalid repository format") {
		t.Errorf("expected 'invalid repository format' in error, got: %v", err)
	}
}

func TestRunGitConnectInvalidRepoEmptyParts(t *testing.T) {
	err := runGitConnect([]string{"-repo", "/no-owner"})
	if err == nil {
		t.Fatal("expected error for empty owner")
	}
}

func TestWriteWfctlConfig(t *testing.T) {
	dir := t.TempDir()

	// Change to temp dir so .wfctl.yaml is written there
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working dir: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	cfg := &wfctlConfig{
		ProjectName:     "my-api",
		ProjectVersion:  "1.0.0",
		ConfigFile:      "workflow.yaml",
		GitRepository:   "GoCodeAlone/my-api",
		GitBranch:       "main",
		GitAutoPush:     false,
		GenerateActions: true,
		DeployTarget:    "kubernetes",
		DeployNamespace: "production",
	}

	if err := writeWfctlConfig(cfg); err != nil {
		t.Fatalf("writeWfctlConfig failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".wfctl.yaml"))
	if err != nil {
		t.Fatalf("failed to read .wfctl.yaml: %v", err)
	}
	content := string(data)

	checks := map[string]string{
		"project name":    "name: my-api",
		"version":         `version: "1.0.0"`,
		"configFile":      "configFile: workflow.yaml",
		"repository":      "repository: GoCodeAlone/my-api",
		"branch":          "branch: main",
		"autoPush":        "autoPush: false",
		"generateActions": "generateActions: true",
		"deploy target":   "target: kubernetes",
		"namespace":       "namespace: production",
	}

	for field, want := range checks {
		if !strings.Contains(content, want) {
			t.Errorf("expected %s in .wfctl.yaml (%q), got:\n%s", field, want, content)
		}
	}
}

func TestLoadWfctlConfig(t *testing.T) {
	dir := t.TempDir()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working dir: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	content := `project:
  name: test-project
  version: "2.0.0"
  configFile: config.yaml
git:
  repository: owner/test-project
  branch: develop
  autoPush: true
  generateActions: false
deploy:
  target: docker
  namespace: staging
`
	if err := os.WriteFile(".wfctl.yaml", []byte(content), 0640); err != nil {
		t.Fatalf("failed to write .wfctl.yaml: %v", err)
	}

	cfg, err := loadWfctlConfig()
	if err != nil {
		t.Fatalf("loadWfctlConfig failed: %v", err)
	}

	if cfg.ProjectName != "test-project" {
		t.Errorf("expected ProjectName=test-project, got %s", cfg.ProjectName)
	}
	if cfg.ProjectVersion != "2.0.0" {
		t.Errorf("expected ProjectVersion=2.0.0, got %s", cfg.ProjectVersion)
	}
	if cfg.ConfigFile != "config.yaml" {
		t.Errorf("expected ConfigFile=config.yaml, got %s", cfg.ConfigFile)
	}
	if cfg.GitRepository != "owner/test-project" {
		t.Errorf("expected GitRepository=owner/test-project, got %s", cfg.GitRepository)
	}
	if cfg.GitBranch != "develop" {
		t.Errorf("expected GitBranch=develop, got %s", cfg.GitBranch)
	}
	if !cfg.GitAutoPush {
		t.Error("expected GitAutoPush=true")
	}
	if cfg.GenerateActions {
		t.Error("expected GenerateActions=false")
	}
	if cfg.DeployTarget != "docker" {
		t.Errorf("expected DeployTarget=docker, got %s", cfg.DeployTarget)
	}
	if cfg.DeployNamespace != "staging" {
		t.Errorf("expected DeployNamespace=staging, got %s", cfg.DeployNamespace)
	}
}

func TestLoadWfctlConfigMissing(t *testing.T) {
	dir := t.TempDir()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working dir: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	_, err = loadWfctlConfig()
	if err == nil {
		t.Fatal("expected error when .wfctl.yaml is missing")
	}
}

func TestRepoDisplayURL(t *testing.T) {
	cases := []struct {
		repo     string
		useHTTPS bool
		want     string
	}{
		{"owner/repo", false, "git@github.com:owner/repo.git"},
		{"owner/repo", true, "https://github.com/owner/repo.git"},
	}
	for _, c := range cases {
		got := repoDisplayURL(c.repo, c.useHTTPS)
		if got != c.want {
			t.Errorf("repoDisplayURL(%q, %v) = %q, want %q", c.repo, c.useHTTPS, got, c.want)
		}
	}
}

func TestConfigOnlyFiles(t *testing.T) {
	dir := t.TempDir()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working dir: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	cfg := &wfctlConfig{
		ConfigFile: "workflow.yaml",
	}

	// Without optional files
	files := configOnlyFiles(cfg)
	found := map[string]bool{}
	for _, f := range files {
		found[f] = true
	}
	if !found[".wfctl.yaml"] {
		t.Error("expected .wfctl.yaml in config-only files")
	}
	if !found["workflow.yaml"] {
		t.Error("expected workflow.yaml in config-only files")
	}

	// With plugin.json present
	if err := os.WriteFile("plugin.json", []byte(`{}`), 0640); err != nil {
		t.Fatalf("failed to write plugin.json: %v", err)
	}
	files = configOnlyFiles(cfg)
	found = map[string]bool{}
	for _, f := range files {
		found[f] = true
	}
	if !found["plugin.json"] {
		t.Error("expected plugin.json in config-only files when it exists")
	}

	// With .github/workflows present
	if err := os.MkdirAll(filepath.Join(".github", "workflows"), 0750); err != nil {
		t.Fatalf("failed to mkdir .github/workflows: %v", err)
	}
	files = configOnlyFiles(cfg)
	found = map[string]bool{}
	for _, f := range files {
		found[f] = true
	}
	if !found[filepath.Join(".github", "workflows")] {
		t.Error("expected .github/workflows in config-only files when it exists")
	}
}

func TestWriteDefaultGitignore(t *testing.T) {
	dir := t.TempDir()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working dir: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(origDir) }()

	if err := writeDefaultGitignore(); err != nil {
		t.Fatalf("writeDefaultGitignore failed: %v", err)
	}

	data, err := os.ReadFile(".gitignore")
	if err != nil {
		t.Fatalf("failed to read .gitignore: %v", err)
	}
	content := string(data)

	for _, want := range []string{".env", "bin/", "dist/", "node_modules/"} {
		if !strings.Contains(content, want) {
			t.Errorf("expected %q in .gitignore", want)
		}
	}
}
