package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunInitMissingName(t *testing.T) {
	err := runInit([]string{})
	if err == nil {
		t.Fatal("expected error when no project name given")
	}
}

func TestRunInitDefaultTemplate(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "my-api")
	err := runInit([]string{"--output", outDir, "my-api"})
	if err != nil {
		t.Fatalf("init with default template failed: %v", err)
	}

	for _, f := range []string{"go.mod", "main.go", "workflow.yaml", "README.md", "Dockerfile", ".gitignore"} {
		if _, err := os.Stat(filepath.Join(outDir, f)); os.IsNotExist(err) {
			t.Errorf("expected %s to be created", f)
		}
	}
}

func TestRunInitAPIServiceTemplate(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "my-service")
	err := runInit([]string{"--template", "api-service", "--author", "myorg", "--output", outDir, "my-service"})
	if err != nil {
		t.Fatalf("init api-service failed: %v", err)
	}

	// Check go.mod has correct module path
	data, err := os.ReadFile(filepath.Join(outDir, "go.mod"))
	if err != nil {
		t.Fatalf("failed to read go.mod: %v", err)
	}
	if !strings.Contains(string(data), "github.com/myorg/my-service") {
		t.Errorf("go.mod missing expected module path, got: %s", string(data))
	}

	// Check workflow.yaml uses project name for module names
	yml, err := os.ReadFile(filepath.Join(outDir, "workflow.yaml"))
	if err != nil {
		t.Fatalf("failed to read workflow.yaml: %v", err)
	}
	if !strings.Contains(string(yml), "my-service-server") {
		t.Errorf("workflow.yaml missing expected module name, got: %s", string(yml))
	}
}

func TestRunInitEventProcessorTemplate(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "my-processor")
	err := runInit([]string{"--template", "event-processor", "--author", "myorg", "--output", outDir, "my-processor"})
	if err != nil {
		t.Fatalf("init event-processor failed: %v", err)
	}

	for _, f := range []string{"go.mod", "main.go", "workflow.yaml"} {
		if _, err := os.Stat(filepath.Join(outDir, f)); os.IsNotExist(err) {
			t.Errorf("expected %s to be created", f)
		}
	}

	yml, err := os.ReadFile(filepath.Join(outDir, "workflow.yaml"))
	if err != nil {
		t.Fatalf("failed to read workflow.yaml: %v", err)
	}
	if !strings.Contains(string(yml), "statemachine") {
		t.Errorf("event-processor workflow.yaml missing statemachine, got: %s", string(yml))
	}
}

func TestRunInitFullStackTemplate(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "my-app")
	err := runInit([]string{"--template", "full-stack", "--author", "myorg", "--output", outDir, "my-app"})
	if err != nil {
		t.Fatalf("init full-stack failed: %v", err)
	}

	for _, f := range []string{
		"go.mod", "main.go", "workflow.yaml",
		"ui/package.json", "ui/vite.config.ts", "ui/index.html",
		"ui/src/main.tsx", "ui/src/App.tsx",
	} {
		if _, err := os.Stat(filepath.Join(outDir, f)); os.IsNotExist(err) {
			t.Errorf("expected %s to be created", f)
		}
	}
}

func TestRunInitPluginTemplate(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "my-plugin")
	err := runInit([]string{"--template", "plugin", "--author", "myorg", "--output", outDir, "my-plugin"})
	if err != nil {
		t.Fatalf("init plugin failed: %v", err)
	}

	for _, f := range []string{"go.mod", "main.go", "plugin.go", "README.md"} {
		if _, err := os.Stat(filepath.Join(outDir, f)); os.IsNotExist(err) {
			t.Errorf("expected %s to be created", f)
		}
	}

	// Plugin should not have workflow.yaml
	if _, err := os.Stat(filepath.Join(outDir, "workflow.yaml")); err == nil {
		t.Error("plugin template should not generate workflow.yaml")
	}

	data, err := os.ReadFile(filepath.Join(outDir, "go.mod"))
	if err != nil {
		t.Fatalf("failed to read go.mod: %v", err)
	}
	if strings.Contains(string(data), "yaml.v3") {
		t.Errorf("plugin go.mod should not depend on yaml.v3")
	}
}

func TestRunInitUIPluginTemplate(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "my-ui-plugin")
	err := runInit([]string{"--template", "ui-plugin", "--author", "myorg", "--output", outDir, "my-ui-plugin"})
	if err != nil {
		t.Fatalf("init ui-plugin failed: %v", err)
	}

	for _, f := range []string{
		"go.mod", "main.go", "plugin.go",
		"ui/package.json", "ui/src/App.tsx",
	} {
		if _, err := os.Stat(filepath.Join(outDir, f)); os.IsNotExist(err) {
			t.Errorf("expected %s to be created", f)
		}
	}
}

func TestRunInitUnknownTemplate(t *testing.T) {
	err := runInit([]string{"--template", "nonexistent", "my-project"})
	if err == nil {
		t.Fatal("expected error for unknown template")
	}
	if !strings.Contains(err.Error(), "unknown template") {
		t.Errorf("expected 'unknown template' in error, got: %v", err)
	}
}

func TestRunInitInvalidProjectName(t *testing.T) {
	err := runInit([]string{"my project with spaces"})
	if err == nil {
		t.Fatal("expected error for invalid project name")
	}
}

func TestRunInitListTemplates(t *testing.T) {
	err := runInit([]string{"--list"})
	if err != nil {
		t.Fatalf("list templates failed: %v", err)
	}
}

func TestRunInitCamelCase(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"my-api", "MyApi"},
		{"my_service", "MyService"},
		{"hello", "Hello"},
		{"my-event-processor", "MyEventProcessor"},
	}
	for _, c := range cases {
		got := toCamelCaseInit(c.input)
		if got != c.want {
			t.Errorf("toCamelCaseInit(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}
