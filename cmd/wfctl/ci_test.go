package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateGitHubActions(t *testing.T) {
	opts := ciOptions{
		Platform:    "github_actions",
		InfraConfig: "infra.yaml",
		Runner:      "ubuntu-latest",
	}

	files, err := generateCIFiles(opts)
	if err != nil {
		t.Fatalf("generateCIFiles: %v", err)
	}

	infraYML, ok := files[".github/workflows/infra.yml"]
	if !ok {
		t.Fatal("expected .github/workflows/infra.yml in output")
	}

	markers := []string{
		"actions/checkout@v4",
		"actions/setup-go@v5",
		"wfctl infra plan",
		"permissions",
		"actions/github-script@v7",
	}
	for _, m := range markers {
		if !strings.Contains(infraYML, m) {
			t.Errorf("infra.yml missing marker %q", m)
		}
	}

	buildYML, ok := files[".github/workflows/build.yml"]
	if !ok {
		t.Fatal("expected .github/workflows/build.yml in output")
	}
	if !strings.Contains(buildYML, "actions/checkout@v4") {
		t.Error("build.yml missing actions/checkout@v4")
	}
	if !strings.Contains(buildYML, "actions/setup-go@v5") {
		t.Error("build.yml missing actions/setup-go@v5")
	}
}

func TestGenerateGitLabCI(t *testing.T) {
	opts := ciOptions{
		Platform:    "gitlab_ci",
		InfraConfig: "infra.yaml",
	}

	files, err := generateCIFiles(opts)
	if err != nil {
		t.Fatalf("generateCIFiles: %v", err)
	}

	content, ok := files[".gitlab-ci.yml"]
	if !ok {
		t.Fatal("expected .gitlab-ci.yml in output")
	}

	markers := []string{
		"rules:",
		"needs:",
		"wfctl infra plan",
		"environment:",
	}
	for _, m := range markers {
		if !strings.Contains(content, m) {
			t.Errorf(".gitlab-ci.yml missing marker %q", m)
		}
	}

	// Ensure deprecated only: syntax is NOT used
	if strings.Contains(content, "\nonly:") {
		t.Error(".gitlab-ci.yml uses deprecated 'only:' syntax")
	}
}

func TestCIGenerateMissingPlatform(t *testing.T) {
	err := runCIGenerate([]string{})
	if err == nil {
		t.Fatal("expected error when --platform is missing")
	}
	if !strings.Contains(err.Error(), "--platform") {
		t.Errorf("expected error to mention --platform, got: %v", err)
	}
}

func TestCIGenerateUnsupportedPlatform(t *testing.T) {
	_, err := generateCIFiles(ciOptions{Platform: "travis_ci", InfraConfig: "infra.yaml"})
	if err == nil {
		t.Fatal("expected error for unsupported platform")
	}
	if !strings.Contains(err.Error(), "unsupported platform") {
		t.Errorf("expected 'unsupported platform' in error, got: %v", err)
	}
}

func TestResolveCIConfigExplicit(t *testing.T) {
	cfg, err := resolveCIConfig("my-config.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg != "my-config.yaml" {
		t.Errorf("expected my-config.yaml, got %s", cfg)
	}
}

func TestResolveCIConfigDefaults(t *testing.T) {
	// In the absence of app.yaml or infra.yaml, falls back to infra.yaml
	cfg, err := resolveCIConfig("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == "" {
		t.Error("expected a non-empty config path")
	}
}

func TestResolveCIConfigPicksAppYaml(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig) //nolint:errcheck

	if err := os.WriteFile(filepath.Join(dir, "app.yaml"), []byte("modules: []"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := resolveCIConfig("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg != "app.yaml" {
		t.Errorf("expected app.yaml, got %s", cfg)
	}
}

func TestCIGenerateWritesFiles(t *testing.T) {
	dir := t.TempDir()

	err := runCIGenerate([]string{
		"--platform", "gitlab_ci",
		"--config", "infra.yaml",
		"--output", dir,
	})
	if err != nil {
		t.Fatalf("runCIGenerate: %v", err)
	}

	dest := filepath.Join(dir, ".gitlab-ci.yml")
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("expected file %s to exist: %v", dest, err)
	}
	if !strings.Contains(string(data), "rules:") {
		t.Error("generated .gitlab-ci.yml missing 'rules:'")
	}
}
