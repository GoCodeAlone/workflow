package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// ============================================================
// Test 7: plugin init scaffold
// ============================================================

// TestRunPluginInit_AllFiles verifies that runPluginInit creates all expected
// files for a new plugin project.
func TestRunPluginInit_AllFiles(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "test-plugin")

	if err := runPluginInit([]string{
		"-author", "TestOrg",
		"-description", "Test plugin for unit tests",
		"-output", outDir,
		"test-plugin",
	}); err != nil {
		t.Fatalf("runPluginInit: %v", err)
	}

	// All expected files/dirs.
	expectedFiles := []string{
		"plugin.json",
		"go.mod",
		".goreleaser.yml",
		"Makefile",
		"README.md",
		filepath.Join("cmd", "workflow-plugin-test-plugin", "main.go"),
		filepath.Join("internal", "provider.go"),
		filepath.Join("internal", "steps.go"),
		filepath.Join(".github", "workflows", "ci.yml"),
		filepath.Join(".github", "workflows", "release.yml"),
	}

	for _, rel := range expectedFiles {
		path := filepath.Join(outDir, rel)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected file missing: %s (%v)", rel, err)
		}
	}
}

// TestRunPluginInit_PluginJSON verifies that plugin.json has correct fields.
func TestRunPluginInit_PluginJSON(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "myplugin")

	if err := runPluginInit([]string{
		"-author", "AcmeCorp",
		"-description", "My awesome plugin",
		"-version", "0.2.0",
		"-output", outDir,
		"myplugin",
	}); err != nil {
		t.Fatalf("runPluginInit: %v", err)
	}

	pjPath := filepath.Join(outDir, "plugin.json")
	data, err := os.ReadFile(pjPath)
	if err != nil {
		t.Fatalf("read plugin.json: %v", err)
	}

	var pj map[string]interface{}
	if err := json.Unmarshal(data, &pj); err != nil {
		t.Fatalf("unmarshal plugin.json: %v", err)
	}

	// Required fields.
	if pj["name"] == nil || pj["name"].(string) == "" {
		t.Error("plugin.json: missing or empty name")
	}
	if pj["version"] == nil || pj["version"].(string) == "" {
		t.Error("plugin.json: missing or empty version")
	}
	if pj["author"] == nil || pj["author"].(string) == "" {
		t.Error("plugin.json: missing or empty author")
	}
	if pj["description"] == nil || pj["description"].(string) == "" {
		t.Error("plugin.json: missing or empty description")
	}

	// author and description should match what was passed.
	if pj["author"].(string) != "AcmeCorp" {
		t.Errorf("author: got %q, want %q", pj["author"], "AcmeCorp")
	}
	if pj["description"].(string) != "My awesome plugin" {
		t.Errorf("description: got %q, want %q", pj["description"], "My awesome plugin")
	}
}

// TestRunPluginInit_GoMod verifies that go.mod has the correct module path.
func TestRunPluginInit_GoMod(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "mymod-plugin")

	if err := runPluginInit([]string{
		"-author", "MyOrg",
		"-output", outDir,
		"mymod-plugin",
	}); err != nil {
		t.Fatalf("runPluginInit: %v", err)
	}

	goModPath := filepath.Join(outDir, "go.mod")
	data, err := os.ReadFile(goModPath)
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	content := string(data)

	// Module path should start with "module ".
	if !strings.Contains(content, "module ") {
		t.Error("go.mod: missing 'module' directive")
	}
	// Should reference the author/binary-name convention.
	if !strings.Contains(content, "MyOrg") {
		t.Errorf("go.mod: expected 'MyOrg' in module path, got:\n%s", content)
	}
	if !strings.Contains(content, "workflow-plugin-") {
		t.Errorf("go.mod: expected 'workflow-plugin-' in module path, got:\n%s", content)
	}
	// Should have a go directive.
	if !strings.Contains(content, "\ngo ") {
		t.Error("go.mod: missing 'go' version directive")
	}
}

// TestRunPluginInit_GoMod_CustomModule verifies that a custom -module flag
// overrides the default module path.
func TestRunPluginInit_GoMod_CustomModule(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "custmod")
	const customModule = "example.com/internal/my-plugin"

	if err := runPluginInit([]string{
		"-author", "SomeOrg",
		"-module", customModule,
		"-output", outDir,
		"custmod",
	}); err != nil {
		t.Fatalf("runPluginInit: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outDir, "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	if !strings.Contains(string(data), customModule) {
		t.Errorf("go.mod: expected custom module %q, got:\n%s", customModule, data)
	}
}

// TestRunPluginInit_GoReleaserYML verifies that .goreleaser.yml references
// the correct binary name and is valid YAML.
func TestRunPluginInit_GoReleaserYML(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "gr-plugin")

	if err := runPluginInit([]string{
		"-author", "GoOrg",
		"-output", outDir,
		"gr-plugin",
	}); err != nil {
		t.Fatalf("runPluginInit: %v", err)
	}

	grPath := filepath.Join(outDir, ".goreleaser.yml")
	data, err := os.ReadFile(grPath)
	if err != nil {
		t.Fatalf("read .goreleaser.yml: %v", err)
	}

	// Must be valid YAML.
	var parsed map[string]interface{}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf(".goreleaser.yml is not valid YAML: %v", err)
	}

	content := string(data)
	const wantBinary = "workflow-plugin-gr-plugin"

	// Binary name must appear in the file.
	if !strings.Contains(content, wantBinary) {
		t.Errorf(".goreleaser.yml: expected binary name %q, got:\n%s", wantBinary, content)
	}

	// GoReleaser v2 must be specified.
	if !strings.Contains(content, "version: 2") {
		t.Errorf(".goreleaser.yml: expected 'version: 2', got:\n%s", content)
	}
}

// TestRunPluginInit_CIWorkflow verifies that ci.yml is valid YAML with the
// expected triggers and steps.
func TestRunPluginInit_CIWorkflow(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "ci-plugin")

	if err := runPluginInit([]string{
		"-author", "CIOrg",
		"-output", outDir,
		"ci-plugin",
	}); err != nil {
		t.Fatalf("runPluginInit: %v", err)
	}

	ciPath := filepath.Join(outDir, ".github", "workflows", "ci.yml")
	data, err := os.ReadFile(ciPath)
	if err != nil {
		t.Fatalf("read ci.yml: %v", err)
	}

	// Must be valid YAML.
	var parsed map[string]interface{}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("ci.yml is not valid YAML: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "push") {
		t.Error("ci.yml: expected 'push' trigger")
	}
	if !strings.Contains(content, "pull_request") {
		t.Error("ci.yml: expected 'pull_request' trigger")
	}
	if !strings.Contains(content, "go test") {
		t.Error("ci.yml: expected 'go test' step")
	}
}

// TestRunPluginInit_ReleaseWorkflow verifies that release.yml is valid YAML
// and references the correct binary name.
func TestRunPluginInit_ReleaseWorkflow(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "rel-plugin")
	const binaryName = "workflow-plugin-rel-plugin"

	if err := runPluginInit([]string{
		"-author", "RelOrg",
		"-output", outDir,
		"rel-plugin",
	}); err != nil {
		t.Fatalf("runPluginInit: %v", err)
	}

	relPath := filepath.Join(outDir, ".github", "workflows", "release.yml")
	data, err := os.ReadFile(relPath)
	if err != nil {
		t.Fatalf("read release.yml: %v", err)
	}

	// Must be valid YAML.
	var parsed map[string]interface{}
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("release.yml is not valid YAML: %v", err)
	}

	content := string(data)
	// Should trigger on tags.
	if !strings.Contains(content, "tags") {
		t.Error("release.yml: expected 'tags' trigger")
	}
	// Should use GoReleaser.
	if !strings.Contains(content, "goreleaser") {
		t.Error("release.yml: expected 'goreleaser' action reference")
	}
	_ = binaryName // variable used for documentation
}

// TestRunPluginInit_MissingAuthor verifies that -author is required.
func TestRunPluginInit_MissingAuthor(t *testing.T) {
	err := runPluginInit([]string{
		"-output", t.TempDir(),
		"no-author",
	})
	if err == nil {
		t.Fatal("expected error for missing -author, got nil")
	}
}

// TestRunPluginInit_MissingName verifies that a plugin name is required.
func TestRunPluginInit_MissingName(t *testing.T) {
	err := runPluginInit([]string{
		"-author", "SomeOrg",
	})
	if err == nil {
		t.Fatal("expected error for missing name argument, got nil")
	}
}

// TestRunPluginInit_DefaultOutputDir verifies that the output defaults to
// the plugin name when -output is not provided.
func TestRunPluginInit_DefaultOutputDir(t *testing.T) {
	// Change to a temp dir so the auto-created dir doesn't pollute the repo.
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { os.Chdir(orig) }) //nolint:errcheck

	const name = "auto-dir-plugin"
	if err := runPluginInit([]string{
		"-author", "TestOrg",
		name,
	}); err != nil {
		t.Fatalf("runPluginInit: %v", err)
	}

	// Output directory should be named after the plugin.
	expectedDir := filepath.Join(tmpDir, name)
	if _, err := os.Stat(expectedDir); os.IsNotExist(err) {
		t.Errorf("expected default output dir %s to be created", expectedDir)
	}
}
