package main

import (
	"os"
	"testing"
)

func TestRunModernize_ListRules(t *testing.T) {
	// --list-rules with no rules should succeed and not error
	err := runModernize([]string{"--list-rules"})
	if err != nil {
		t.Fatalf("expected no error from --list-rules, got: %v", err)
	}
}

func TestRunModernize_NoFiles(t *testing.T) {
	// No files and no --list-rules should return an error
	err := runModernize([]string{})
	if err == nil {
		t.Fatal("expected error when no files provided")
	}
}

func TestFilterRules_Empty(t *testing.T) {
	rules := []Rule{
		{ID: "rule-a", Description: "A"},
		{ID: "rule-b", Description: "B"},
	}

	// No filters — all rules returned
	result := filterRules(rules, "", "")
	if len(result) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(result))
	}
}

func TestFilterRules_Include(t *testing.T) {
	rules := []Rule{
		{ID: "rule-a", Description: "A"},
		{ID: "rule-b", Description: "B"},
	}

	result := filterRules(rules, "rule-a", "")
	if len(result) != 1 || result[0].ID != "rule-a" {
		t.Fatalf("expected only rule-a, got %v", result)
	}
}

func TestFilterRules_Exclude(t *testing.T) {
	rules := []Rule{
		{ID: "rule-a", Description: "A"},
		{ID: "rule-b", Description: "B"},
	}

	result := filterRules(rules, "", "rule-b")
	if len(result) != 1 || result[0].ID != "rule-a" {
		t.Fatalf("expected only rule-a, got %v", result)
	}
}

func TestModernizeFile_ValidYAML(t *testing.T) {
	// Create a temp YAML file and run modernizeFile with no rules
	tmpDir := t.TempDir()
	tmpFile := tmpDir + "/test.yaml"
	if err := writeTestFile(tmpFile, "key: value\n"); err != nil {
		t.Fatal(err)
	}

	findings, fixes, err := modernizeFile(tmpFile, []Rule{}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(findings))
	}
	if fixes != 0 {
		t.Fatalf("expected 0 fixes, got %d", fixes)
	}
}

func TestModernizeFile_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := tmpDir + "/bad.yaml"
	if err := writeTestFile(tmpFile, ":\n  :\n    - [invalid"); err != nil {
		t.Fatal(err)
	}

	_, _, err := modernizeFile(tmpFile, []Rule{}, false)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func writeTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}
