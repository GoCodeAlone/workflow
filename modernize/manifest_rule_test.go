package modernize

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// parseYAMLNode is a test helper that parses YAML into a yaml.Node.
func parseYAMLNode(t *testing.T, input string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(input), &doc); err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}
	return &doc
}

// TestManifestRule_Validate covers rule validation.
func TestManifestRule_Validate(t *testing.T) {
	tests := []struct {
		name    string
		rule    ManifestRule
		wantErr bool
	}{
		{
			name:    "missing ID",
			rule:    ManifestRule{Description: "desc", OldModuleType: "a", NewModuleType: "b"},
			wantErr: true,
		},
		{
			name:    "missing Description",
			rule:    ManifestRule{ID: "x", OldModuleType: "a", NewModuleType: "b"},
			wantErr: true,
		},
		{
			name:    "module type rename missing newModuleType",
			rule:    ManifestRule{ID: "x", Description: "d", OldModuleType: "a"},
			wantErr: true,
		},
		{
			name:    "step type rename missing newStepType",
			rule:    ManifestRule{ID: "x", Description: "d", OldStepType: "a"},
			wantErr: true,
		},
		{
			name:    "config key rename missing newKey",
			rule:    ManifestRule{ID: "x", Description: "d", ModuleType: "my.mod", OldKey: "old"},
			wantErr: true,
		},
		{
			name:    "no rule kind configured",
			rule:    ManifestRule{ID: "x", Description: "d"},
			wantErr: true,
		},
		{
			name:    "valid module type rename",
			rule:    ManifestRule{ID: "x", Description: "d", OldModuleType: "a", NewModuleType: "b"},
			wantErr: false,
		},
		{
			name:    "valid step type rename",
			rule:    ManifestRule{ID: "x", Description: "d", OldStepType: "a", NewStepType: "b"},
			wantErr: false,
		},
		{
			name:    "valid module config key rename",
			rule:    ManifestRule{ID: "x", Description: "d", ModuleType: "my.mod", OldKey: "old", NewKey: "new"},
			wantErr: false,
		},
		{
			name:    "valid step config key rename",
			rule:    ManifestRule{ID: "x", Description: "d", StepType: "step.foo", OldKey: "old", NewKey: "new"},
			wantErr: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.rule.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() error = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

// TestManifestRule_ModuleTypeRename verifies detection and fixing of a module
// type rename rule.
func TestManifestRule_ModuleTypeRename(t *testing.T) {
	mr := ManifestRule{
		ID:            "test-module-rename",
		Description:   "Rename old.module to new.module",
		Severity:      "error",
		OldModuleType: "old.module",
		NewModuleType: "new.module",
	}

	input := `
modules:
  - name: my-server
    type: old.module
    config:
      port: 8080
  - name: other
    type: other.module
    config:
      key: value
`
	rule, err := mr.ToRule()
	if err != nil {
		t.Fatalf("ToRule() error: %v", err)
	}
	if rule.ID != "test-module-rename" {
		t.Errorf("unexpected rule ID: %s", rule.ID)
	}
	if rule.Severity != "error" {
		t.Errorf("unexpected severity: %s", rule.Severity)
	}

	doc := parseYAMLNode(t, input)
	findings := rule.Check(doc, []byte(input))
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %v", len(findings), findings)
	}
	if findings[0].RuleID != "test-module-rename" {
		t.Errorf("unexpected finding RuleID: %s", findings[0].RuleID)
	}
	if !findings[0].Fixable {
		t.Error("finding should be fixable")
	}
	if !strings.Contains(findings[0].Message, "old.module") {
		t.Errorf("message should mention old type, got: %s", findings[0].Message)
	}

	// Apply fix.
	changes := rule.Fix(doc)
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}

	out, _ := yaml.Marshal(doc)
	result := string(out)
	if strings.Contains(result, "old.module") {
		t.Error("old.module should have been renamed")
	}
	if !strings.Contains(result, "new.module") {
		t.Error("new.module should appear after rename")
	}
	// other.module should be untouched
	if !strings.Contains(result, "other.module") {
		t.Error("other.module should remain")
	}
}

// TestManifestRule_ModuleTypeRename_DefaultSeverity verifies that severity
// defaults to "warning" when not specified.
func TestManifestRule_ModuleTypeRename_DefaultSeverity(t *testing.T) {
	mr := ManifestRule{
		ID:            "test-default-sev",
		Description:   "desc",
		OldModuleType: "old",
		NewModuleType: "new",
	}
	rule, err := mr.ToRule()
	if err != nil {
		t.Fatalf("ToRule() error: %v", err)
	}
	if rule.Severity != "warning" {
		t.Errorf("expected default severity 'warning', got %q", rule.Severity)
	}
}

// TestManifestRule_ModuleTypeRename_CustomMessage verifies custom message override.
func TestManifestRule_ModuleTypeRename_CustomMessage(t *testing.T) {
	mr := ManifestRule{
		ID:            "test-custom-msg",
		Description:   "desc",
		OldModuleType: "old",
		NewModuleType: "new",
		Message:       "Please migrate now!",
	}
	rule, err := mr.ToRule()
	if err != nil {
		t.Fatalf("ToRule() error: %v", err)
	}

	input := `modules:
  - name: x
    type: old
`
	doc := parseYAMLNode(t, input)
	findings := rule.Check(doc, []byte(input))
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Message != "Please migrate now!" {
		t.Errorf("expected custom message, got: %s", findings[0].Message)
	}
}

// TestManifestRule_StepTypeRename verifies detection and fixing of a step type
// rename rule.
func TestManifestRule_StepTypeRename(t *testing.T) {
	mr := ManifestRule{
		ID:          "test-step-rename",
		Description: "Rename old.step to new.step",
		OldStepType: "step.old_action",
		NewStepType: "step.new_action",
	}

	input := `
pipelines:
  main:
    steps:
      - name: do_thing
        type: step.old_action
        config:
          key: value
      - name: do_other
        type: step.keep
        config:
          key: value
`
	rule, err := mr.ToRule()
	if err != nil {
		t.Fatalf("ToRule() error: %v", err)
	}

	doc := parseYAMLNode(t, input)
	findings := rule.Check(doc, []byte(input))
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %v", len(findings), findings)
	}
	if !findings[0].Fixable {
		t.Error("step type rename should be fixable")
	}

	changes := rule.Fix(doc)
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}

	out, _ := yaml.Marshal(doc)
	result := string(out)
	if strings.Contains(result, "step.old_action") {
		t.Error("old step type should have been renamed")
	}
	if !strings.Contains(result, "step.new_action") {
		t.Error("new step type should appear after rename")
	}
	// Unaffected step should remain.
	if !strings.Contains(result, "step.keep") {
		t.Error("step.keep should be unaffected")
	}
}

// TestManifestRule_ModuleConfigKeyRename verifies config key renaming within a
// specific module type.
func TestManifestRule_ModuleConfigKeyRename(t *testing.T) {
	mr := ManifestRule{
		ID:          "test-module-key",
		Description: "Rename old_field to new_field in my.module",
		ModuleType:  "my.module",
		OldKey:      "old_field",
		NewKey:      "new_field",
	}

	input := `
modules:
  - name: m1
    type: my.module
    config:
      old_field: value1
      other_field: value2
  - name: m2
    type: other.module
    config:
      old_field: should_not_be_touched
`
	rule, err := mr.ToRule()
	if err != nil {
		t.Fatalf("ToRule() error: %v", err)
	}

	doc := parseYAMLNode(t, input)
	findings := rule.Check(doc, []byte(input))
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding (only in my.module), got %d: %v", len(findings), findings)
	}
	if !findings[0].Fixable {
		t.Error("config key rename should be fixable")
	}

	changes := rule.Fix(doc)
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}

	out, _ := yaml.Marshal(doc)
	result := string(out)

	// The my.module config should use new_field.
	if strings.Contains(result, "old_field: value1") {
		t.Error("my.module old_field should have been renamed to new_field")
	}
	if !strings.Contains(result, "new_field: value1") {
		t.Error("new_field with value1 should appear in my.module")
	}
	// other.module's old_field should be untouched.
	if !strings.Contains(result, "old_field: should_not_be_touched") {
		t.Error("other.module old_field should remain unchanged")
	}
}

// TestManifestRule_StepConfigKeyRename verifies config key renaming within a
// specific step type.
func TestManifestRule_StepConfigKeyRename(t *testing.T) {
	mr := ManifestRule{
		ID:          "test-step-key",
		Description: "Rename deprecated_field to current_field in step.my_step",
		StepType:    "step.my_step",
		OldKey:      "deprecated_field",
		NewKey:      "current_field",
	}

	input := `
pipelines:
  main:
    steps:
      - name: run_step
        type: step.my_step
        config:
          deprecated_field: hello
      - name: other
        type: step.other
        config:
          deprecated_field: should_stay
`
	rule, err := mr.ToRule()
	if err != nil {
		t.Fatalf("ToRule() error: %v", err)
	}

	doc := parseYAMLNode(t, input)
	findings := rule.Check(doc, []byte(input))
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding (only in step.my_step), got %d: %v", len(findings), findings)
	}

	changes := rule.Fix(doc)
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}

	out, _ := yaml.Marshal(doc)
	result := string(out)

	if strings.Contains(result, "deprecated_field: hello") {
		t.Error("step.my_step deprecated_field should have been renamed")
	}
	if !strings.Contains(result, "current_field: hello") {
		t.Error("current_field should appear after rename")
	}
	// step.other should be unchanged.
	if !strings.Contains(result, "deprecated_field: should_stay") {
		t.Error("step.other deprecated_field should remain")
	}
}

// TestManifestRule_NoFindings verifies that rules don't emit false positives.
func TestManifestRule_NoFindings(t *testing.T) {
	mr := ManifestRule{
		ID:            "no-match",
		Description:   "Should not match",
		OldModuleType: "nonexistent.type",
		NewModuleType: "new.type",
	}
	rule, err := mr.ToRule()
	if err != nil {
		t.Fatalf("ToRule() error: %v", err)
	}

	input := `
modules:
  - name: x
    type: http.server
    config:
      address: :8080
`
	doc := parseYAMLNode(t, input)
	findings := rule.Check(doc, []byte(input))
	if len(findings) != 0 {
		t.Errorf("expected no findings, got %d", len(findings))
	}
	changes := rule.Fix(doc)
	if len(changes) != 0 {
		t.Errorf("expected no changes, got %d", len(changes))
	}
}

// TestLoadRulesFromDir_NonexistentDir verifies graceful error on missing directory.
func TestLoadRulesFromDir_NonexistentDir(t *testing.T) {
	_, err := LoadRulesFromDir("/nonexistent/plugin/dir")
	if err == nil {
		t.Error("expected error for nonexistent directory")
	}
}

// TestLoadRulesFromDir_Empty verifies that an empty plugin directory returns
// no rules without error.
func TestLoadRulesFromDir_Empty(t *testing.T) {
	dir := t.TempDir()
	rules, err := LoadRulesFromDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 0 {
		t.Errorf("expected 0 rules, got %d", len(rules))
	}
}

// TestLoadRulesFromDir_SkipsMalformedManifest verifies that malformed plugin.json
// files are silently skipped.
func TestLoadRulesFromDir_SkipsMalformedManifest(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "bad-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte("not valid json{{"), 0o644); err != nil {
		t.Fatal(err)
	}

	rules, err := LoadRulesFromDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 0 {
		t.Errorf("expected 0 rules from malformed manifest, got %d", len(rules))
	}
}

// TestLoadRulesFromDir_LoadsRulesFromManifest simulates an external plugin
// directory with a plugin.json that declares modernize rules, and verifies
// that the rules are loaded and work correctly.
func TestLoadRulesFromDir_LoadsRulesFromManifest(t *testing.T) {
	dir := t.TempDir()

	// Simulate an external plugin "my-vendor-plugin" that has migrated its
	// module type name and renamed a config field.
	pluginDir := filepath.Join(dir, "my-vendor-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}

	manifest := map[string]any{
		"name":        "my-vendor-plugin",
		"version":     "2.0.0",
		"author":      "Vendor Inc.",
		"description": "My vendor plugin",
		"moduleTypes": []string{"vendor.new_connector"},
		"modernizeRules": []map[string]any{
			{
				"id":            "vendor-rename-module-type",
				"description":   "Rename vendor.old_connector to vendor.new_connector (v2.0 migration)",
				"severity":      "error",
				"oldModuleType": "vendor.old_connector",
				"newModuleType": "vendor.new_connector",
			},
			{
				"id":          "vendor-rename-config-key",
				"description": "Rename apiEndpoint to endpoint in vendor.new_connector config",
				"moduleType":  "vendor.new_connector",
				"oldKey":      "apiEndpoint",
				"newKey":      "endpoint",
			},
		},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	rules, err := LoadRulesFromDir(dir)
	if err != nil {
		t.Fatalf("LoadRulesFromDir() error: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}

	// Verify rule IDs.
	ruleIDs := map[string]bool{}
	for _, r := range rules {
		ruleIDs[r.ID] = true
	}
	if !ruleIDs["vendor-rename-module-type"] {
		t.Error("missing rule vendor-rename-module-type")
	}
	if !ruleIDs["vendor-rename-config-key"] {
		t.Error("missing rule vendor-rename-config-key")
	}

	// Run the rules against a config that uses the old module type.
	input := `
modules:
  - name: my-connector
    type: vendor.old_connector
    config:
      apiEndpoint: https://api.example.com
`
	doc := parseYAMLNode(t, input)
	var findings []Finding
	for _, r := range rules {
		findings = append(findings, r.Check(doc, []byte(input))...)
	}
	// Expect 1 finding: old module type (the config key rule won't fire because
	// the module still has the old type — type matches new_connector, not old).
	if len(findings) != 1 {
		t.Errorf("expected 1 finding for old module type, got %d: %v", len(findings), findings)
	}
	if findings[0].RuleID != "vendor-rename-module-type" {
		t.Errorf("expected vendor-rename-module-type finding, got %s", findings[0].RuleID)
	}
}

// TestLoadRulesFromDir_MultiplePlugins verifies that rules from multiple plugins
// are all collected.
func TestLoadRulesFromDir_MultiplePlugins(t *testing.T) {
	dir := t.TempDir()

	// Plugin A: one module type rename rule.
	pluginA := filepath.Join(dir, "plugin-a")
	if err := os.MkdirAll(pluginA, 0o755); err != nil {
		t.Fatal(err)
	}
	manifestA := map[string]any{
		"name":        "plugin-a",
		"version":     "1.0.0",
		"author":      "Dev",
		"description": "Plugin A",
		"modernizeRules": []map[string]any{
			{
				"id":            "plugin-a-rule",
				"description":   "Plugin A rename",
				"oldModuleType": "a.old",
				"newModuleType": "a.new",
			},
		},
	}
	writeJSON(t, filepath.Join(pluginA, "plugin.json"), manifestA)

	// Plugin B: one step type rename rule.
	pluginB := filepath.Join(dir, "plugin-b")
	if err := os.MkdirAll(pluginB, 0o755); err != nil {
		t.Fatal(err)
	}
	manifestB := map[string]any{
		"name":        "plugin-b",
		"version":     "1.0.0",
		"author":      "Dev",
		"description": "Plugin B",
		"modernizeRules": []map[string]any{
			{
				"id":          "plugin-b-rule",
				"description": "Plugin B step rename",
				"oldStepType": "step.b_old",
				"newStepType": "step.b_new",
			},
		},
	}
	writeJSON(t, filepath.Join(pluginB, "plugin.json"), manifestB)

	rules, err := LoadRulesFromDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 2 {
		t.Errorf("expected 2 rules from 2 plugins, got %d", len(rules))
	}
}

// TestLoadRulesFromDir_SkipsInvalidRules verifies that invalid rule descriptors
// within an otherwise-valid manifest are silently skipped.
func TestLoadRulesFromDir_SkipsInvalidRules(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "bad-rules-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{
		"name":        "bad-rules-plugin",
		"version":     "1.0.0",
		"author":      "Dev",
		"description": "Plugin with an invalid rule",
		"modernizeRules": []map[string]any{
			// Missing newModuleType — should be skipped.
			{
				"id":            "bad-rule",
				"description":   "Incomplete rule",
				"oldModuleType": "a.type",
			},
			// Valid rule.
			{
				"id":            "good-rule",
				"description":   "Complete rule",
				"oldModuleType": "b.old",
				"newModuleType": "b.new",
			},
		},
	}
	writeJSON(t, filepath.Join(pluginDir, "plugin.json"), manifest)

	rules, err := LoadRulesFromDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only the valid rule should be loaded.
	if len(rules) != 1 {
		t.Errorf("expected 1 valid rule, got %d", len(rules))
	}
	if rules[0].ID != "good-rule" {
		t.Errorf("expected good-rule, got %s", rules[0].ID)
	}
}

// TestLoadRulesFromDir_NoModernizeRulesField verifies that a plugin.json without
// a modernizeRules field contributes 0 rules without error.
func TestLoadRulesFromDir_NoModernizeRulesField(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "standard-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{
		"name":        "standard-plugin",
		"version":     "1.0.0",
		"author":      "Dev",
		"description": "A plugin without modernize rules",
		"moduleTypes": []string{"my.type"},
	}
	writeJSON(t, filepath.Join(dir, "standard-plugin", "plugin.json"), manifest)

	rules, err := LoadRulesFromDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 0 {
		t.Errorf("expected 0 rules for plugin without modernizeRules, got %d", len(rules))
	}
}

// TestManifestRule_JSONRoundTrip verifies that ManifestRule serialises and
// deserialises correctly.
func TestManifestRule_JSONRoundTrip(t *testing.T) {
	original := ManifestRule{
		ID:            "my-rule",
		Description:   "Rename old to new",
		Severity:      "error",
		Message:       "Please update",
		OldModuleType: "my.old",
		NewModuleType: "my.new",
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}
	var decoded ManifestRule
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal error: %v", err)
	}
	if decoded.ID != original.ID ||
		decoded.Description != original.Description ||
		decoded.Severity != original.Severity ||
		decoded.Message != original.Message ||
		decoded.OldModuleType != original.OldModuleType ||
		decoded.NewModuleType != original.NewModuleType {
		t.Errorf("round-trip mismatch: got %+v, want %+v", decoded, original)
	}
}

// writeJSON is a test helper to write a JSON-marshalled value to a file.
func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}
