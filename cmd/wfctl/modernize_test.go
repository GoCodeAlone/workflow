package main

import (
	"os"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/modernize"
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
	rules := []modernize.Rule{
		{ID: "rule-a", Description: "A"},
		{ID: "rule-b", Description: "B"},
	}

	// No filters — all rules returned
	result := modernize.FilterRules(rules, "", "")
	if len(result) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(result))
	}
}

func TestFilterRules_Include(t *testing.T) {
	rules := []modernize.Rule{
		{ID: "rule-a", Description: "A"},
		{ID: "rule-b", Description: "B"},
	}

	result := modernize.FilterRules(rules, "rule-a", "")
	if len(result) != 1 || result[0].ID != "rule-a" {
		t.Fatalf("expected only rule-a, got %v", result)
	}
}

func TestFilterRules_Exclude(t *testing.T) {
	rules := []modernize.Rule{
		{ID: "rule-a", Description: "A"},
		{ID: "rule-b", Description: "B"},
	}

	result := modernize.FilterRules(rules, "", "rule-b")
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

	findings, fixes, err := modernizeFile(tmpFile, []modernize.Rule{}, false)
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

	_, _, err := modernizeFile(tmpFile, []modernize.Rule{}, false)
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
func findRule(id string) *modernize.Rule {
	for _, r := range modernize.AllRules() {
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
	rules := modernize.AllRules()
	var rule modernize.Rule
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
	rules := modernize.AllRules()
	var rule modernize.Rule
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

func TestDbQueryModeCheck(t *testing.T) {
	input := `
pipelines:
  test:
    steps:
      - name: fetch_user
        type: step.db_query
        config:
          database: my-db
          query: "SELECT * FROM users WHERE id = ?"
      - name: respond
        type: step.json_response
        config:
          body:
            name: '{{ index .steps "fetch_user" "row" "name" }}'
`
	rule := findRule("db-query-mode")
	if rule == nil {
		t.Fatal("db-query-mode rule not found")
	}

	doc := parseTestYAML(t, input)
	findings := rule.Check(doc, []byte(input))
	if len(findings) == 0 {
		t.Fatal("expected findings for missing mode:single")
	}
}

func TestDbQueryModeNoFalsePositive(t *testing.T) {
	input := `
pipelines:
  test:
    steps:
      - name: fetch_user
        type: step.db_query
        config:
          database: my-db
          query: "SELECT * FROM users WHERE id = ?"
          mode: single
      - name: respond
        type: step.json_response
        config:
          body:
            name: '{{ index .steps "fetch_user" "row" "name" }}'
`
	rule := findRule("db-query-mode")
	doc := parseTestYAML(t, input)
	findings := rule.Check(doc, []byte(input))
	if len(findings) != 0 {
		t.Errorf("expected no findings when mode:single is set, got: %v", findings)
	}
}

func TestDbQueryModeFix(t *testing.T) {
	input := `
pipelines:
  test:
    steps:
      - name: fetch_user
        type: step.db_query
        config:
          database: my-db
          query: "SELECT * FROM users WHERE id = ?"
      - name: respond
        type: step.json_response
        config:
          body:
            found: "{{ .steps.fetch_user.found }}"
`
	rule := findRule("db-query-mode")
	doc := parseTestYAML(t, input)
	changes := rule.Fix(doc)
	if len(changes) == 0 {
		t.Fatal("expected changes from fix")
	}

	out, _ := yaml.Marshal(doc)
	result := string(out)

	if !strings.Contains(result, "mode: single") {
		t.Errorf("expected mode: single to be added, got:\n%s", result)
	}
}

func TestDbQueryIndexCheck(t *testing.T) {
	input := `
pipelines:
  test:
    steps:
      - name: respond
        type: step.json_response
        config:
          body:
            name: "{{ .steps.fetch_user.row.name }}"
`
	rule := findRule("db-query-index")
	if rule == nil {
		t.Fatal("db-query-index rule not found")
	}

	doc := parseTestYAML(t, input)
	findings := rule.Check(doc, []byte(input))
	if len(findings) == 0 {
		t.Fatal("expected findings for .row. dot-access")
	}
}

func TestDbQueryIndexFix(t *testing.T) {
	input := `
pipelines:
  test:
    steps:
      - name: respond
        type: step.json_response
        config:
          body:
            name: "{{ .steps.fetch_user.row.name }}"
            email: "{{ .steps.fetch_user.row.email }}"
`
	rule := findRule("db-query-index")
	doc := parseTestYAML(t, input)
	changes := rule.Fix(doc)
	if len(changes) == 0 {
		t.Fatal("expected changes from fix")
	}

	out, _ := yaml.Marshal(doc)
	result := string(out)

	if strings.Contains(result, ".steps.fetch_user.row.name") {
		t.Errorf("expected dot-access to be replaced, got:\n%s", result)
	}
	// yaml.Marshal may escape inner quotes; check for the index call pattern
	if !strings.Contains(result, `index .steps`) || !strings.Contains(result, `fetch_user`) {
		t.Errorf("expected index syntax, got:\n%s", result)
	}
}

func TestAbsoluteDbPathCheck(t *testing.T) {
	input := `
modules:
  - name: my-db
    type: storage.sqlite
    config:
      dbPath: /data/myapp.db
`
	rule := findRule("absolute-dbpath")
	if rule == nil {
		t.Fatal("absolute-dbpath rule not found")
	}
	doc := parseTestYAML(t, input)
	findings := rule.Check(doc, []byte(input))
	if len(findings) == 0 {
		t.Fatal("expected warning for absolute dbPath")
	}
}

func TestAbsoluteDbPathNoFalsePositive(t *testing.T) {
	input := `
modules:
  - name: my-db
    type: storage.sqlite
    config:
      dbPath: data.db
`
	rule := findRule("absolute-dbpath")
	doc := parseTestYAML(t, input)
	findings := rule.Check(doc, []byte(input))
	if len(findings) != 0 {
		t.Errorf("expected no findings for relative dbPath, got: %v", findings)
	}
}

func TestEmptyRoutesCheck(t *testing.T) {
	input := `
pipelines:
  test:
    steps:
      - name: route
        type: step.conditional
        config:
          field: steps.check.matched
          routes: {}
          default: next
`
	rule := findRule("empty-routes")
	if rule == nil {
		t.Fatal("empty-routes rule not found")
	}
	doc := parseTestYAML(t, input)
	findings := rule.Check(doc, []byte(input))
	if len(findings) == 0 {
		t.Fatal("expected findings for empty routes")
	}
}

func TestCamelCaseConfigCheck(t *testing.T) {
	input := `
modules:
  - name: my-server
    type: http.server
    config:
      max_connections: 10
      listen_address: ":8080"
`
	rule := findRule("camelcase-config")
	if rule == nil {
		t.Fatal("camelcase-config rule not found")
	}
	doc := parseTestYAML(t, input)
	findings := rule.Check(doc, []byte(input))
	if len(findings) == 0 {
		t.Fatal("expected findings for snake_case config keys")
	}
}

func TestCamelCaseConfigCheck_SchemaDefinedKeysNotFlagged(t *testing.T) {
	// openapi module uses snake_case keys (spec_file, register_routes, swagger_ui,
	// max_body_bytes) defined in its own schema — these must NOT be flagged.
	input := `
modules:
  - name: api-docs
    type: openapi
    config:
      spec_file: ./specs/api.yaml
      register_routes: false
      swagger_ui:
        enabled: true
        path: /docs
      max_body_bytes: 1048576
`
	rule := findRule("camelcase-config")
	if rule == nil {
		t.Fatal("camelcase-config rule not found")
	}
	doc := parseTestYAML(t, input)
	findings := rule.Check(doc, []byte(input))
	if len(findings) != 0 {
		t.Errorf("expected no findings for schema-defined snake_case keys, got %d: %v", len(findings), findings)
	}
}

func TestCamelCaseConfigCheck_UnknownModuleTypeStillFlagged(t *testing.T) {
	// For a module type not in the schema registry, snake_case keys should still be flagged.
	input := `
modules:
  - name: my-thing
    type: custom.unknown
    config:
      snake_key: value
`
	rule := findRule("camelcase-config")
	if rule == nil {
		t.Fatal("camelcase-config rule not found")
	}
	doc := parseTestYAML(t, input)
	findings := rule.Check(doc, []byte(input))
	if len(findings) == 0 {
		t.Fatal("expected findings for snake_case config keys on unknown module type")
	}
	found := false
	for _, f := range findings {
		if strings.Contains(f.Message, `"snake_key"`) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a finding mentioning key \"snake_key\", got: %v", findings)
	}
}

func TestModernizeAllRulesRegistered(t *testing.T) {
	rules := modernize.AllRules()
	expectedIDs := []string{
		"hyphen-steps",
		"conditional-field",
		"db-query-mode",
		"db-query-index",
		"absolute-dbpath",
		"empty-routes",
		"camelcase-config",
		"request-parse-config",
	}
	if len(rules) != len(expectedIDs) {
		t.Errorf("expected %d rules, got %d", len(expectedIDs), len(rules))
	}
	ruleMap := make(map[string]bool)
	for _, r := range rules {
		ruleMap[r.ID] = true
	}
	for _, id := range expectedIDs {
		if !ruleMap[id] {
			t.Errorf("missing rule: %s", id)
		}
	}
}

func TestFilterRulesIntegration(t *testing.T) {
	rules := modernize.AllRules()

	// Include filter
	filtered := modernize.FilterRules(rules, "hyphen-steps,empty-routes", "")
	if len(filtered) != 2 {
		t.Errorf("expected 2 rules with include filter, got %d", len(filtered))
	}

	// Exclude filter
	filtered = modernize.FilterRules(rules, "", "camelcase-config")
	if len(filtered) != len(rules)-1 {
		t.Errorf("expected %d rules with exclude filter, got %d", len(rules)-1, len(filtered))
	}
}

func TestModernizeFullPipeline(t *testing.T) {
	// A config with multiple issues
	input := `
name: test-app
pipelines:
  check:
    steps:
      - name: check-input
        type: step.regex_match
        config:
          pattern: "test"
          input: "{{ .body.data }}"
      - name: route-check
        type: step.conditional
        config:
          field: "{{ .steps.check-input.matched }}"
          routes:
            "true": block
          default: allow
      - name: fetch
        type: step.db_query
        config:
          database: my-db
          query: "SELECT name FROM users WHERE id = ?"
      - name: respond
        type: step.json_response
        config:
          body:
            name: "{{ .steps.fetch.row.name }}"
`
	rules := modernize.AllRules()
	doc := parseTestYAML(t, input)

	// Check phase
	var allFindings []modernize.Finding
	for _, r := range rules {
		allFindings = append(allFindings, r.Check(doc, []byte(input))...)
	}
	if len(allFindings) < 4 {
		t.Errorf("expected at least 4 findings, got %d: %v", len(allFindings), allFindings)
	}

	// Fix phase
	totalChanges := 0
	for _, r := range rules {
		if r.Fix == nil {
			continue
		}
		changes := r.Fix(doc)
		totalChanges += len(changes)
	}
	if totalChanges == 0 {
		t.Fatal("expected changes from fixes")
	}

	out, _ := yaml.Marshal(doc)
	result := string(out)

	// Verify fixes applied
	if strings.Contains(result, "check-input") {
		t.Error("hyphen-steps: check-input not renamed")
	}
	if strings.Contains(result, `field: "{{ .steps`) {
		t.Error("conditional-field: template not converted")
	}
}
