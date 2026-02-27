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
	if !strings.Contains(err.Error(), "failed validation") {
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

func TestRunValidateMultipleFiles(t *testing.T) {
	dir := t.TempDir()
	path1 := writeTestConfig(t, dir, "a.yaml", minimalConfig)
	path2 := writeTestConfig(t, dir, "b.yaml", minimalConfig)
	if err := runValidate([]string{path1, path2}); err != nil {
		t.Fatalf("expected both valid, got error: %v", err)
	}
}

func TestRunValidateDir(t *testing.T) {
	dir := t.TempDir()
	writeTestConfig(t, dir, "a.yaml", minimalConfig)
	writeTestConfig(t, dir, "b.yaml", minimalConfig)
	if err := runValidate([]string{"--dir", dir}); err != nil {
		t.Fatalf("expected all valid, got error: %v", err)
	}
}

func TestRunValidateBatchWithFailure(t *testing.T) {
	dir := t.TempDir()
	path1 := writeTestConfig(t, dir, "good.yaml", minimalConfig)
	path2 := writeTestConfig(t, dir, "bad.yaml", invalidConfig)
	err := runValidate([]string{path1, path2})
	if err == nil {
		t.Fatal("expected error when one config is invalid")
	}
	if !strings.Contains(err.Error(), "1 config(s) failed") {
		t.Errorf("expected batch failure message, got: %v", err)
	}
}

func TestRunValidateSkipUnknownTypes(t *testing.T) {
	dir := t.TempDir()
	unknownTypeConfig := `
modules:
  - name: custom-thing
    type: custom.unknown.type
    config: {}
`
	path := writeTestConfig(t, dir, "custom.yaml", unknownTypeConfig)
	// Should fail without the flag
	err := runValidate([]string{path})
	if err == nil {
		t.Fatal("expected error for unknown type")
	}
	// Should pass with the flag
	if err := runValidate([]string{"--skip-unknown-types", path}); err != nil {
		t.Fatalf("expected pass with --skip-unknown-types, got: %v", err)
	}
}

func TestRunValidateSnakeCaseConfig(t *testing.T) {
	dir := t.TempDir()
	// "content_type" is the snake_case form of the known camelCase field "contentType"
	snakeCaseConfig := `
modules:
  - name: handler
    type: http.handler
    config:
      content_type: "application/json"
triggers:
  http:
    port: 8080
`
	path := writeTestConfig(t, dir, "snake.yaml", snakeCaseConfig)
	// validateFile returns the detailed error; runValidate returns a summary
	err := validateFile(path, false, false, false)
	if err == nil {
		t.Fatal("expected error for snake_case config field")
	}
	if !strings.Contains(err.Error(), "content_type") {
		t.Errorf("expected error to mention snake_case field 'content_type', got: %v", err)
	}
	if !strings.Contains(err.Error(), "contentType") {
		t.Errorf("expected error to suggest camelCase 'contentType', got: %v", err)
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

func TestRunValidatePluginDir(t *testing.T) {
	// Create a fake external plugin directory with a plugin.json declaring a custom module type.
	pluginsDir := t.TempDir()
	pluginSubdir := filepath.Join(pluginsDir, "my-ext-plugin")
	if err := os.MkdirAll(pluginSubdir, 0755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"moduleTypes": ["custom.ext.validate.testonly"]}`
	if err := os.WriteFile(filepath.Join(pluginSubdir, "plugin.json"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}

	// Config using the external plugin module type
	dir := t.TempDir()
	configContent := `
modules:
  - name: ext-mod
    type: custom.ext.validate.testonly
`
	path := writeTestConfig(t, dir, "workflow.yaml", configContent)

	// Without --plugin-dir: should fail (unknown type)
	if err := runValidate([]string{path}); err == nil {
		t.Fatal("expected error for unknown external module type without --plugin-dir")
	}

	// With --plugin-dir: should pass
	if err := runValidate([]string{"--plugin-dir", pluginsDir, path}); err != nil {
		t.Errorf("expected valid config with --plugin-dir, got: %v", err)
	}
}
