package community

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/GoCodeAlone/workflow/plugin"
)

func validManifest() *plugin.PluginManifest {
	return &plugin.PluginManifest{
		Name:        "test-plugin",
		Version:     "1.0.0",
		Author:      "Test Author",
		Description: "A test plugin",
		License:     "MIT",
	}
}

func setupValidPluginDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Write manifest
	m := validManifest()
	data, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	// Write source file (valid Go with only stdlib imports)
	source := `package component

import "context"

func Name() string { return "test-plugin" }

func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{"status": "ok"}, nil
}
`
	if err := os.WriteFile(filepath.Join(dir, "test-plugin.go"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	// Write test file
	testSource := `package component_test

import "testing"

func TestPlugin(t *testing.T) {
	// placeholder
}
`
	if err := os.WriteFile(filepath.Join(dir, "test-plugin_test.go"), []byte(testSource), 0644); err != nil {
		t.Fatal(err)
	}

	return dir
}

func TestSubmissionValidatorValidDirectory(t *testing.T) {
	dir := setupValidPluginDir(t)
	v := NewSubmissionValidator()

	result, err := v.ValidateDirectory(dir)
	if err != nil {
		t.Fatalf("ValidateDirectory error: %v", err)
	}

	for _, check := range result.Checks {
		if !check.Passed && check.Name != "has_license" {
			t.Errorf("check %q failed: %s", check.Name, check.Message)
		}
	}
}

func TestSubmissionValidatorNonexistentDir(t *testing.T) {
	v := NewSubmissionValidator()
	result, err := v.ValidateDirectory("/nonexistent/path")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Valid {
		t.Error("expected invalid result for nonexistent directory")
	}
}

func TestSubmissionValidatorNotADirectory(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(tmpFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	v := NewSubmissionValidator()
	result, err := v.ValidateDirectory(tmpFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Valid {
		t.Error("expected invalid result for file")
	}
}

func TestSubmissionValidatorNoManifest(t *testing.T) {
	dir := t.TempDir()
	v := NewSubmissionValidator()

	result, err := v.ValidateDirectory(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Valid {
		t.Error("expected invalid result for missing manifest")
	}
}

func TestSubmissionValidatorInvalidManifest(t *testing.T) {
	dir := t.TempDir()
	// Invalid manifest (missing fields)
	m := &plugin.PluginManifest{Name: "x"} // Missing version, author, etc.
	data, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	v := NewSubmissionValidator()
	result, err := v.ValidateDirectory(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Valid {
		t.Error("expected invalid result for invalid manifest")
	}
}

func TestSubmissionValidatorNoSourceFiles(t *testing.T) {
	dir := t.TempDir()
	m := validManifest()
	data, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	v := NewSubmissionValidator()
	result, err := v.ValidateDirectory(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Valid {
		t.Error("expected invalid result for no source files")
	}
}

func TestSubmissionValidatorNoTestFiles(t *testing.T) {
	dir := t.TempDir()
	m := validManifest()
	data, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	source := `package component

func Name() string { return "test" }
`
	if err := os.WriteFile(filepath.Join(dir, "plugin.go"), []byte(source), 0644); err != nil {
		t.Fatal(err)
	}

	v := NewSubmissionValidator()
	result, err := v.ValidateDirectory(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Valid {
		t.Error("expected invalid result for no test files")
	}

	// Verify the tests_exist check specifically failed
	for _, check := range result.Checks {
		if check.Name == "tests_exist" && check.Passed {
			t.Error("tests_exist check should have failed")
		}
	}
}

func TestReviewChecklist(t *testing.T) {
	m := validManifest()
	rc := NewReviewChecklist(m)

	if rc.PluginName != "test-plugin" {
		t.Errorf("PluginName = %q, want %q", rc.PluginName, "test-plugin")
	}
	if rc.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", rc.Version, "1.0.0")
	}
	if len(rc.Items) == 0 {
		t.Error("expected non-empty items")
	}

	// Initially nothing is passed
	if rc.PassedRequired() {
		t.Error("expected PassedRequired to be false initially")
	}

	// Pass all required items
	for i := range rc.Items {
		if rc.Items[i].Required {
			rc.Items[i].Passed = true
		}
	}
	if !rc.PassedRequired() {
		t.Error("expected PassedRequired to be true after passing all required")
	}
}

func TestReviewChecklistSummary(t *testing.T) {
	m := validManifest()
	rc := NewReviewChecklist(m)

	summary := rc.Summary()
	if summary == "" {
		t.Error("expected non-empty summary")
	}

	// Mark all required as passed and check APPROVED
	for i := range rc.Items {
		if rc.Items[i].Required {
			rc.Items[i].Passed = true
		}
	}
	summary = rc.Summary()
	if !contains(summary, "APPROVED") {
		t.Error("expected APPROVED in summary when all required pass")
	}

	// Add a note to one item
	rc.Items[0].Notes = "looks good"
	summary = rc.Summary()
	if !contains(summary, "looks good") {
		t.Error("expected notes in summary")
	}
}

func TestReviewChecklistNeedsRevision(t *testing.T) {
	m := validManifest()
	rc := NewReviewChecklist(m)

	summary := rc.Summary()
	if !contains(summary, "NEEDS REVISION") {
		t.Error("expected NEEDS REVISION in summary when required checks fail")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := range len(s) - len(substr) + 1 {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
