package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestConfig(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}
	return path
}

const validConfig = `
modules:
  - name: server
    type: http.server
    config:
      address: ":8080"
  - name: router
    type: http.router
workflows:
  http:
    routes: []
triggers:
  http:
    port: 8080
`

const minimalConfig = `
modules:
  - name: server
    type: http.server
    config:
      address: ":8080"
`

const invalidConfig = `
modules:
  - name: ""
    type: ""
`

func TestRunValidateValid(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir, "valid.yaml", minimalConfig)
	if err := runValidate([]string{path}); err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}
}

func TestRunValidateInvalid(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir, "invalid.yaml", invalidConfig)
	err := runValidate([]string{path})
	if err == nil {
		t.Fatal("expected error for invalid config")
	}
	if !strings.Contains(err.Error(), "validation failed") {
		t.Errorf("expected validation error, got: %v", err)
	}
}

func TestRunValidateStrict(t *testing.T) {
	dir := t.TempDir()
	emptyConfig := "modules: []\n"
	path := writeTestConfig(t, dir, "empty.yaml", emptyConfig)
	err := runValidate([]string{"-strict", path})
	if err == nil {
		t.Fatal("expected error in strict mode with empty modules")
	}
}

func TestRunValidateMissingArg(t *testing.T) {
	err := runValidate([]string{})
	if err == nil {
		t.Fatal("expected error when no config file given")
	}
}

func TestRunInspect(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir, "config.yaml", validConfig)
	if err := runInspect([]string{path}); err != nil {
		t.Fatalf("inspect failed: %v", err)
	}
}

func TestRunInspectWithDeps(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir, "config.yaml", validConfig)
	if err := runInspect([]string{"-deps", path}); err != nil {
		t.Fatalf("inspect with deps failed: %v", err)
	}
}

func TestRunInspectMissingArg(t *testing.T) {
	err := runInspect([]string{})
	if err == nil {
		t.Fatal("expected error when no config file given")
	}
}

func TestRunSchema(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "schema.json")
	if err := runSchema([]string{"-output", outPath}); err != nil {
		t.Fatalf("schema generation failed: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("failed to read schema output: %v", err)
	}

	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	if schema["title"] == nil {
		t.Error("expected title in schema")
	}
}

func TestRunSchemaStdout(t *testing.T) {
	if err := runSchema([]string{}); err != nil {
		t.Fatalf("schema to stdout failed: %v", err)
	}
}

func TestRunPluginMissingSubcommand(t *testing.T) {
	err := runPlugin([]string{})
	if err == nil {
		t.Fatal("expected error when no plugin subcommand given")
	}
}

func TestRunPluginInitMissingName(t *testing.T) {
	err := runPluginInit([]string{"-author", "test"})
	if err == nil {
		t.Fatal("expected error when no plugin name given")
	}
}

func TestRunPluginInitMissingAuthor(t *testing.T) {
	err := runPluginInit([]string{"my-plugin"})
	if err == nil {
		t.Fatal("expected error when no author given")
	}
}

func TestRunPluginInit(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "test-plugin")
	err := runPluginInit([]string{
		"-author", "tester",
		"-description", "A test plugin",
		"-output", outDir,
		"test-plugin",
	})
	if err != nil {
		t.Fatalf("plugin init failed: %v", err)
	}

	// Check manifest was created
	if _, err := os.Stat(filepath.Join(outDir, "plugin.json")); os.IsNotExist(err) {
		t.Error("expected plugin.json to be created")
	}
	// Check source file was created
	if _, err := os.Stat(filepath.Join(outDir, "test-plugin.go")); os.IsNotExist(err) {
		t.Error("expected test-plugin.go to be created")
	}
}

func TestRunPluginDocs(t *testing.T) {
	// First scaffold a plugin
	dir := t.TempDir()
	outDir := filepath.Join(dir, "doc-plugin")
	err := runPluginInit([]string{
		"-author", "tester",
		"-description", "A doc test plugin",
		"-output", outDir,
		"doc-plugin",
	})
	if err != nil {
		t.Fatalf("plugin init failed: %v", err)
	}

	// Then generate docs
	if err := runPluginDocs([]string{outDir}); err != nil {
		t.Fatalf("plugin docs failed: %v", err)
	}
}

func TestRunPluginDocsMissingDir(t *testing.T) {
	err := runPluginDocs([]string{})
	if err == nil {
		t.Fatal("expected error when no directory given")
	}
}
