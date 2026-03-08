package main

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
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

func parseTestYAML(t *testing.T, input string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(input), &doc); err != nil {
		t.Fatalf("parse YAML: %v", err)
	}
	return &doc
}

// findRule is a test helper that looks up a rule by ID.
func findRule(id string) *Rule {
	for _, r := range allModernizeRules() {
		if r.ID == id {
			return &r
		}
	}
	return nil
}

func TestConditionalFieldCheck(t *testing.T) {
	input := `
pipelines:
  test:
    steps:
      - name: route
        type: step.conditional
        config:
          field: "{{ .steps.check_xss.matched }}"
          routes:
            "true": deny
          default: allow
`
	rule := findRule("conditional-field")
	if rule == nil {
		t.Fatal("conditional-field rule not found")
	}

	doc := parseTestYAML(t, input)
	findings := rule.Check(doc, []byte(input))
	if len(findings) == 0 {
		t.Fatal("expected findings for template in conditional field")
	}
}

func TestConditionalFieldFix(t *testing.T) {
	input := `
pipelines:
  test:
    steps:
      - name: route
        type: step.conditional
        config:
          field: "{{ .steps.check_xss.matched }}"
          routes:
            "true": deny
          default: allow
`
	rule := findRule("conditional-field")
	doc := parseTestYAML(t, input)
	changes := rule.Fix(doc)
	if len(changes) == 0 {
		t.Fatal("expected changes from fix")
	}

	out, _ := yaml.Marshal(doc)
	result := string(out)

	if strings.Contains(result, "{{") {
		t.Errorf("expected template syntax to be removed, got:\n%s", result)
	}
	if !strings.Contains(result, "steps.check_xss.matched") {
		t.Errorf("expected dot-path field value, got:\n%s", result)
	}
}

func TestHyphenStepsCheck(t *testing.T) {
	input := `
pipelines:
  test:
    steps:
      - name: check-xss
        type: step.regex_match
      - name: route_xss
        type: step.conditional
        config:
          field: steps.check-xss.matched
`
	rules := allModernizeRules()
	var rule Rule
	for _, r := range rules {
		if r.ID == "hyphen-steps" {
			rule = r
			break
		}
	}
	if rule.ID == "" {
		t.Fatal("hyphen-steps rule not found")
	}

	doc := parseTestYAML(t, input)
	findings := rule.Check(doc, []byte(input))
	if len(findings) == 0 {
		t.Fatal("expected findings for hyphenated step name")
	}
	found := false
	for _, f := range findings {
		if strings.Contains(f.Message, "check-xss") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected finding for check-xss, got: %v", findings)
	}
}

func TestHyphenStepsFix(t *testing.T) {
	input := `
pipelines:
  test:
    steps:
      - name: check-xss
        type: step.regex_match
      - name: route-result
        type: step.conditional
        config:
          field: steps.check-xss.matched
      - name: respond
        type: step.json_response
        config:
          body:
            value: "{{ .steps.check-xss.result }}"
`
	rules := allModernizeRules()
	var rule Rule
	for _, r := range rules {
		if r.ID == "hyphen-steps" {
			rule = r
			break
		}
	}

	doc := parseTestYAML(t, input)
	changes := rule.Fix(doc)
	if len(changes) == 0 {
		t.Fatal("expected changes from fix")
	}

	out, _ := yaml.Marshal(doc)
	result := string(out)

	if strings.Contains(result, "check-xss") {
		t.Errorf("expected hyphens to be replaced, got:\n%s", result)
	}
	if !strings.Contains(result, "check_xss") {
		t.Errorf("expected underscored name, got:\n%s", result)
	}
	if strings.Contains(result, "route-result") {
		t.Errorf("expected route-result to be renamed, got:\n%s", result)
	}
	if !strings.Contains(result, "route_result") {
		t.Errorf("expected route_result, got:\n%s", result)
	}
	// Check that references in field paths and templates are updated
	if strings.Contains(result, "steps.check-xss") {
		t.Errorf("expected field reference to be updated, got:\n%s", result)
	}
}
