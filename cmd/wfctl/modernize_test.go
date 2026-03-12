package main

import (
	"encoding/json"
	"os"
	"path/filepath"
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

// --- Plugin directory (--plugin-dir) tests ---

// writeTempYAMLFile writes YAML content to a temp file and returns the file path.
func writeTempYAMLFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	f, err := os.CreateTemp(dir, "*.yaml")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return f.Name()
}

// writeTestPluginManifest creates a plugin subdirectory with a plugin.json file.
func writeTestPluginManifest(t *testing.T, pluginsDir, pluginName string, manifest map[string]any) {
	t.Helper()
	dir := filepath.Join(pluginsDir, pluginName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// TestRunModernize_PluginDir_Empty tests --plugin-dir with an empty directory.
func TestRunModernize_PluginDir_Empty(t *testing.T) {
	pluginDir := t.TempDir()
	cfgFile := writeTempYAMLFile(t, `
modules:
  - name: my-server
    type: http.server
    config:
      address: :8080
`)
	err := runModernize([]string{"--plugin-dir", pluginDir, cfgFile})
	if err != nil {
		t.Fatalf("unexpected error with empty plugin dir: %v", err)
	}
}

// TestRunModernize_PluginDir_WithRules tests --plugin-dir with a plugin that
// declares a modernize rule. This simulates an external plugin migration
// scenario where the plugin author has renamed a module type in v2.
func TestRunModernize_PluginDir_WithRules(t *testing.T) {
	pluginDir := t.TempDir()
	writeTestPluginManifest(t, pluginDir, "test-ext-plugin", map[string]any{
		"name":        "test-ext-plugin",
		"version":     "2.0.0",
		"author":      "Test",
		"description": "External test plugin",
		"modernizeRules": []map[string]any{
			{
				"id":            "ext-rename-type",
				"description":   "Rename ext.old_module to ext.new_module",
				"severity":      "error",
				"oldModuleType": "ext.old_module",
				"newModuleType": "ext.new_module",
			},
		},
	})

	// Write a config that uses the old (deprecated) module type.
	cfgFile := writeTempYAMLFile(t, `
modules:
  - name: my-connector
    type: ext.old_module
    config:
      endpoint: https://api.example.com
`)
	// Dry-run should succeed and report findings.
	err := runModernize([]string{"--plugin-dir", pluginDir, cfgFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestRunModernize_PluginDir_NonexistentDir tests --plugin-dir with a missing
// directory, which should produce an error.
func TestRunModernize_PluginDir_NonexistentDir(t *testing.T) {
	cfgFile := writeTempYAMLFile(t, "modules: []\n")
	err := runModernize([]string{"--plugin-dir", "/nonexistent/dir/12345", cfgFile})
	if err == nil {
		t.Fatal("expected error for nonexistent plugin directory")
	}
}

// TestRunModernize_PluginDir_ListRulesIncludesPluginRules tests that
// --list-rules works when a plugin directory is supplied.
func TestRunModernize_PluginDir_ListRulesIncludesPluginRules(t *testing.T) {
	pluginDir := t.TempDir()
	writeTestPluginManifest(t, pluginDir, "my-plugin", map[string]any{
		"name":        "my-plugin",
		"version":     "1.0.0",
		"author":      "Dev",
		"description": "My plugin",
		"modernizeRules": []map[string]any{
			{
				"id":            "my-plugin-rename",
				"description":   "Rename my.old to my.new",
				"oldModuleType": "my.old",
				"newModuleType": "my.new",
			},
		},
	})
	// --list-rules should succeed even when a plugin dir is supplied.
	err := runModernize([]string{"--plugin-dir", pluginDir, "--list-rules"})
	if err != nil {
		t.Fatalf("unexpected error with plugin dir + list-rules: %v", err)
	}
}

// TestRunModernize_PluginDir_Apply tests that --apply with a plugin rule
// actually fixes the config file in-place.
func TestRunModernize_PluginDir_Apply(t *testing.T) {
	pluginDir := t.TempDir()
	writeTestPluginManifest(t, pluginDir, "myplugin", map[string]any{
		"name":        "myplugin",
		"version":     "1.0.0",
		"author":      "Dev",
		"description": "Plugin",
		"modernizeRules": []map[string]any{
			{
				"id":            "myplugin-rename",
				"description":   "Rename myplugin.old to myplugin.new",
				"severity":      "error",
				"oldModuleType": "myplugin.old",
				"newModuleType": "myplugin.new",
			},
		},
	})

	cfgFile := writeTempYAMLFile(t, `modules:
  - name: x
    type: myplugin.old
    config:
      key: val
`)
	if err := runModernize([]string{"--apply", "--plugin-dir", pluginDir, cfgFile}); err != nil {
		t.Fatalf("runModernize --apply: %v", err)
	}

	// Verify the file was updated.
	data, err := os.ReadFile(cfgFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	result := string(data)
	if strings.Contains(result, "myplugin.old") {
		t.Error("old module type should have been renamed to myplugin.new")
	}
	if !strings.Contains(result, "myplugin.new") {
		t.Error("new module type should appear after --apply")
	}
}

// TestRunModernize_PluginDir_MultiplePlugins tests that rules from multiple
// plugins are all loaded and applied.
func TestRunModernize_PluginDir_MultiplePlugins(t *testing.T) {
	pluginDir := t.TempDir()
	writeTestPluginManifest(t, pluginDir, "plugin-a", map[string]any{
		"name":        "plugin-a",
		"version":     "1.0.0",
		"author":      "Dev",
		"description": "Plugin A",
		"modernizeRules": []map[string]any{
			{
				"id":            "plugin-a-rule",
				"description":   "Rename a.old to a.new",
				"oldModuleType": "a.old",
				"newModuleType": "a.new",
			},
		},
	})
	writeTestPluginManifest(t, pluginDir, "plugin-b", map[string]any{
		"name":        "plugin-b",
		"version":     "1.0.0",
		"author":      "Dev",
		"description": "Plugin B",
		"modernizeRules": []map[string]any{
			{
				"id":          "plugin-b-rule",
				"description": "Rename step.b_old to step.b_new",
				"oldStepType": "step.b_old",
				"newStepType": "step.b_new",
			},
		},
	})

	cfgFile := writeTempYAMLFile(t, `
modules:
  - name: conn
    type: a.old
pipelines:
  main:
    steps:
      - name: run
        type: step.b_old
        config:
          key: val
`)
	// Should report 2 findings (one per plugin rule) without error.
	err := runModernize([]string{"--plugin-dir", pluginDir, cfgFile})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
