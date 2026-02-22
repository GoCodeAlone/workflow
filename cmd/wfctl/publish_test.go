package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---- validateRegistryManifest tests ----

func TestValidateRegistryManifest_Valid(t *testing.T) {
	m := &registryManifest{
		Name:        "my-plugin",
		Version:     "1.0.0",
		Author:      "test-author",
		Description: "A test plugin",
		Type:        "external",
		Tier:        "community",
		License:     "MIT",
	}
	if err := validateRegistryManifest(m); err != nil {
		t.Errorf("expected valid manifest, got error: %v", err)
	}
}

func TestValidateRegistryManifest_MissingName(t *testing.T) {
	m := &registryManifest{
		Version:     "1.0.0",
		Author:      "test-author",
		Description: "A test plugin",
		Type:        "external",
		Tier:        "community",
		License:     "MIT",
	}
	err := validateRegistryManifest(m)
	if err == nil {
		t.Fatal("expected error for missing name")
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Errorf("expected 'name is required' in error, got: %v", err)
	}
}

func TestValidateRegistryManifest_MissingVersion(t *testing.T) {
	m := &registryManifest{
		Name:        "my-plugin",
		Author:      "test-author",
		Description: "A test plugin",
		Type:        "external",
		Tier:        "community",
		License:     "MIT",
	}
	err := validateRegistryManifest(m)
	if err == nil {
		t.Fatal("expected error for missing version")
	}
	if !strings.Contains(err.Error(), "version is required") {
		t.Errorf("expected 'version is required' in error, got: %v", err)
	}
}

func TestValidateRegistryManifest_InvalidVersion(t *testing.T) {
	m := &registryManifest{
		Name:        "my-plugin",
		Version:     "not-semver",
		Author:      "test-author",
		Description: "A test plugin",
		Type:        "external",
		Tier:        "community",
		License:     "MIT",
	}
	err := validateRegistryManifest(m)
	if err == nil {
		t.Fatal("expected error for invalid version")
	}
	if !strings.Contains(err.Error(), "semantic version") {
		t.Errorf("expected 'semantic version' in error, got: %v", err)
	}
}

func TestValidateRegistryManifest_InvalidType(t *testing.T) {
	m := &registryManifest{
		Name:        "my-plugin",
		Version:     "1.0.0",
		Author:      "test-author",
		Description: "A test plugin",
		Type:        "invalid-type",
		Tier:        "community",
		License:     "MIT",
	}
	err := validateRegistryManifest(m)
	if err == nil {
		t.Fatal("expected error for invalid type")
	}
	if !strings.Contains(err.Error(), "builtin, external, ui") {
		t.Errorf("expected enum list in error, got: %v", err)
	}
}

func TestValidateRegistryManifest_InvalidTier(t *testing.T) {
	m := &registryManifest{
		Name:        "my-plugin",
		Version:     "1.0.0",
		Author:      "test-author",
		Description: "A test plugin",
		Type:        "external",
		Tier:        "silver",
		License:     "MIT",
	}
	err := validateRegistryManifest(m)
	if err == nil {
		t.Fatal("expected error for invalid tier")
	}
	if !strings.Contains(err.Error(), "core, community, premium") {
		t.Errorf("expected enum list in error, got: %v", err)
	}
}

func TestValidateRegistryManifest_MissingLicense(t *testing.T) {
	m := &registryManifest{
		Name:        "my-plugin",
		Version:     "1.0.0",
		Author:      "test-author",
		Description: "A test plugin",
		Type:        "external",
		Tier:        "community",
	}
	err := validateRegistryManifest(m)
	if err == nil {
		t.Fatal("expected error for missing license")
	}
	if !strings.Contains(err.Error(), "license is required") {
		t.Errorf("expected 'license is required' in error, got: %v", err)
	}
}

func TestValidateRegistryManifest_MultipleErrors(t *testing.T) {
	// Empty manifest should report all required fields missing
	m := &registryManifest{}
	err := validateRegistryManifest(m)
	if err == nil {
		t.Fatal("expected errors for empty manifest")
	}
	for _, field := range []string{"name", "version", "author", "description"} {
		if !strings.Contains(err.Error(), field) {
			t.Errorf("expected %q in error, got: %v", field, err)
		}
	}
}

// ---- detectLicenseType tests ----

func TestDetectLicenseType_MIT(t *testing.T) {
	content := "MIT License\nPermission is hereby granted, free of charge"
	got := detectLicenseType(content)
	if got != "MIT" {
		t.Errorf("expected MIT, got %q", got)
	}
}

func TestDetectLicenseType_Apache(t *testing.T) {
	content := "Apache License, Version 2.0"
	got := detectLicenseType(content)
	if got != "Apache-2.0" {
		t.Errorf("expected Apache-2.0, got %q", got)
	}
}

func TestDetectLicenseType_Unknown(t *testing.T) {
	got := detectLicenseType("some unknown license text")
	if got != "" {
		t.Errorf("expected empty string for unknown license, got %q", got)
	}
}

// ---- auto-detect from Go source tests ----

func TestScanGoSource_DetectsEngineManifest(t *testing.T) {
	dir := t.TempDir()

	// Write a go.mod
	goMod := "module github.com/example/my-plugin\n\ngo 1.21\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0640); err != nil {
		t.Fatal(err)
	}

	// Write a main.go with EngineManifest()
	mainGo := `package main

import "github.com/GoCodeAlone/workflow/plugin"

func EngineManifest() plugin.PluginManifest {
	return plugin.PluginManifest{
		Name:        "my-plugin",
		Version:     "0.2.0",
		Description: "My custom plugin",
	}
}

func main() {}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainGo), 0640); err != nil {
		t.Fatal(err)
	}

	meta, err := scanGoSource(dir)
	if err != nil {
		t.Fatalf("scanGoSource error: %v", err)
	}

	if !meta.isPlugin {
		t.Error("expected isPlugin=true")
	}
	if meta.name != "my-plugin" {
		t.Errorf("expected name 'my-plugin', got %q", meta.name)
	}
	if meta.version != "0.2.0" {
		t.Errorf("expected version '0.2.0', got %q", meta.version)
	}
	if meta.description != "My custom plugin" {
		t.Errorf("expected description, got %q", meta.description)
	}
	if meta.modulePath != "github.com/example/my-plugin" {
		t.Errorf("expected module path, got %q", meta.modulePath)
	}
}

func TestScanGoSource_NoEngineManifest(t *testing.T) {
	dir := t.TempDir()

	mainGo := `package main

func main() {}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(mainGo), 0640); err != nil {
		t.Fatal(err)
	}

	meta, err := scanGoSource(dir)
	if err != nil {
		t.Fatalf("scanGoSource error: %v", err)
	}
	if meta.isPlugin {
		t.Error("expected isPlugin=false when no EngineManifest()")
	}
}

// ---- loadRegistryManifest tests ----

func TestLoadRegistryManifest_Valid(t *testing.T) {
	dir := t.TempDir()

	m := registryManifest{
		Name:        "test-plugin",
		Version:     "1.0.0",
		Author:      "test-author",
		Description: "Test description",
		Type:        "external",
		Tier:        "community",
		License:     "MIT",
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(path, data, 0640); err != nil {
		t.Fatal(err)
	}

	got, err := loadRegistryManifest(path)
	if err != nil {
		t.Fatalf("loadRegistryManifest error: %v", err)
	}
	if got.Name != m.Name {
		t.Errorf("expected name %q, got %q", m.Name, got.Name)
	}
	if got.License != "MIT" {
		t.Errorf("expected license MIT, got %q", got.License)
	}
}

func TestLoadRegistryManifest_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(path, []byte("not-json"), 0640); err != nil {
		t.Fatal(err)
	}
	_, err := loadRegistryManifest(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// ---- dry-run integration test ----

func TestRunPublish_DryRun(t *testing.T) {
	dir := t.TempDir()

	m := registryManifest{
		Name:        "dry-run-plugin",
		Version:     "0.1.0",
		Author:      "tester",
		Description: "A plugin for dry-run testing",
		Type:        "external",
		Tier:        "community",
		License:     "MIT",
	}
	data, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0640); err != nil {
		t.Fatal(err)
	}

	// Capture stdout
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runPublish([]string{"--dir", dir, "--dry-run"})

	w.Close()
	os.Stdout = origStdout

	buf := new(strings.Builder)
	b := make([]byte, 4096)
	for {
		n, readErr := r.Read(b)
		buf.Write(b[:n])
		if readErr != nil {
			break
		}
	}
	output := buf.String()

	if err != nil {
		t.Fatalf("runPublish dry-run error: %v", err)
	}
	if !strings.Contains(output, "dry-run-plugin") {
		t.Errorf("expected plugin name in output, got: %s", output)
	}
	if !strings.Contains(output, `"version": "0.1.0"`) {
		t.Errorf("expected version in output, got: %s", output)
	}
}

func TestRunPublish_OutputFile(t *testing.T) {
	dir := t.TempDir()

	m := registryManifest{
		Name:        "output-plugin",
		Version:     "1.2.3",
		Author:      "tester",
		Description: "A plugin for output testing",
		Type:        "external",
		Tier:        "community",
		License:     "Apache-2.0",
	}
	data, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0640); err != nil {
		t.Fatal(err)
	}

	outFile := filepath.Join(dir, "out-manifest.json")

	// Suppress stdout
	origStdout := os.Stdout
	_, w, _ := os.Pipe()
	os.Stdout = w
	err := runPublish([]string{"--dir", dir, "--output", outFile})
	w.Close()
	os.Stdout = origStdout

	if err != nil {
		t.Fatalf("runPublish output error: %v", err)
	}

	written, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("output file not created: %v", err)
	}

	var got registryManifest
	if err := json.Unmarshal(written, &got); err != nil {
		t.Fatalf("output file is not valid JSON: %v", err)
	}
	if got.Name != "output-plugin" {
		t.Errorf("expected name 'output-plugin', got %q", got.Name)
	}
}

func TestRunPublish_MissingRequiredFields(t *testing.T) {
	dir := t.TempDir()

	// manifest missing author, description, license
	m := map[string]string{
		"name":    "bad-plugin",
		"version": "1.0.0",
		"type":    "external",
		"tier":    "community",
	}
	data, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0640); err != nil {
		t.Fatal(err)
	}

	err := runPublish([]string{"--dir", dir, "--dry-run"})
	if err == nil {
		t.Fatal("expected validation error for incomplete manifest")
	}
	if !strings.Contains(err.Error(), "validation errors") {
		t.Errorf("expected 'validation errors' in error, got: %v", err)
	}
}
