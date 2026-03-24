package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/schema"
)

func TestRunTemplateValidateAllTemplates(t *testing.T) {
	err := runTemplateValidate([]string{})
	if err != nil {
		t.Fatalf("expected all templates to pass, got error: %v", err)
	}
}

func TestRunTemplateValidateSpecificTemplate(t *testing.T) {
	err := runTemplateValidate([]string{"-template", "api-service"})
	if err != nil {
		t.Fatalf("expected api-service template to pass, got error: %v", err)
	}
}

func TestRunTemplateValidateEventProcessor(t *testing.T) {
	err := runTemplateValidate([]string{"-template", "event-processor"})
	if err != nil {
		t.Fatalf("expected event-processor template to pass, got error: %v", err)
	}
}

func TestRunTemplateValidateFullStack(t *testing.T) {
	err := runTemplateValidate([]string{"-template", "full-stack"})
	if err != nil {
		t.Fatalf("expected full-stack template to pass, got error: %v", err)
	}
}

func TestRunTemplateValidateJsonOutput(t *testing.T) {
	err := runTemplateValidate([]string{"-format", "json", "-template", "api-service"})
	if err != nil {
		t.Fatalf("expected json output to work, got: %v", err)
	}
}

func TestRunTemplateValidateConfigFile(t *testing.T) {
	dir := t.TempDir()
	configContent := `
modules:
  - name: server
    type: http.server
    config:
      address: ":8080"
  - name: router
    type: http.router
    dependsOn:
      - server
`
	configPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	err := runTemplateValidate([]string{"-config", configPath})
	if err != nil {
		t.Fatalf("expected valid config to pass, got: %v", err)
	}
}

func TestRunTemplateValidateUnknownModuleType(t *testing.T) {
	dir := t.TempDir()
	configContent := `
modules:
  - name: my-thing
    type: unknown.module.type
`
	configPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	err := runTemplateValidate([]string{"-config", configPath})
	if err == nil {
		t.Fatal("expected error for unknown module type")
	}
	if !strings.Contains(err.Error(), "failed") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunTemplateValidateMissingDependency(t *testing.T) {
	dir := t.TempDir()
	configContent := `
modules:
  - name: router
    type: http.router
    dependsOn:
      - nonexistent-server
`
	configPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	err := runTemplateValidate([]string{"-config", configPath})
	if err == nil {
		t.Fatal("expected error for missing dependency")
	}
}

func TestRunTemplateValidateUnknownStepType(t *testing.T) {
	dir := t.TempDir()
	configContent := `
pipelines:
  my-pipeline:
    trigger:
      type: http
      config:
        path: /test
        method: GET
    steps:
      - name: bad-step
        type: step.nonexistent_step_type
`
	configPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	err := runTemplateValidate([]string{"-config", configPath})
	if err == nil {
		t.Fatal("expected error for unknown step type")
	}
}

func TestRunTemplateValidateStrictMode(t *testing.T) {
	dir := t.TempDir()
	// Valid module config but with an unknown config field (triggers warning)
	configContent := `
modules:
  - name: db
    type: storage.sqlite
    config:
      dbPath: data/test.db
      journalMode: WAL
`
	configPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	// Without strict: should pass (warning only)
	if err := runTemplateValidate([]string{"-config", configPath}); err != nil {
		t.Fatalf("expected pass without strict, got: %v", err)
	}

	// With strict: should fail on warning
	if err := runTemplateValidate([]string{"-strict", "-config", configPath}); err == nil {
		t.Fatal("expected failure in strict mode due to unknown config field")
	}
}

func TestTemplateVarCheck(t *testing.T) {
	content := `name: {{.Name}}-service\ntype: {{.Unknown}}`
	warnings := checkTemplateVars(content, "test")
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "Unknown") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected warning for unknown template variable {{.Unknown}}")
	}
}

func TestTemplateVarCheckKnownVars(t *testing.T) {
	content := `name: {{.Name}}-service\nauthor: {{.Author}}\ndesc: {{.Description}}\ncamel: {{.NameCamel}}`
	warnings := checkTemplateVars(content, "test")
	if len(warnings) > 0 {
		t.Errorf("expected no warnings for known vars, got: %v", warnings)
	}
}

func TestValidateWorkflowConfigEmpty(t *testing.T) {
	cfg := &config.WorkflowConfig{}
	knownModules := KnownModuleTypes()
	knownSteps := KnownStepTypes()
	knownTriggers := KnownTriggerTypes()

	result := validateWorkflowConfig("empty", cfg, knownModules, knownSteps, knownTriggers)
	if !result.pass() {
		t.Errorf("expected empty config to pass, got errors: %v", result.Errors)
	}
	if result.ModuleCount != 0 {
		t.Errorf("expected 0 modules, got %d", result.ModuleCount)
	}
}

func TestValidateWorkflowConfigValidModules(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "server", Type: "http.server"},
			{Name: "router", Type: "http.router", DependsOn: []string{"server"}},
		},
	}
	knownModules := KnownModuleTypes()
	knownSteps := KnownStepTypes()
	knownTriggers := KnownTriggerTypes()

	result := validateWorkflowConfig("test", cfg, knownModules, knownSteps, knownTriggers)
	if !result.pass() {
		t.Errorf("expected valid modules to pass, got errors: %v", result.Errors)
	}
	if result.ModuleCount != 2 {
		t.Errorf("expected 2 modules, got %d", result.ModuleCount)
	}
	if result.ModuleValid != 2 {
		t.Errorf("expected 2 valid modules, got %d", result.ModuleValid)
	}
}

func TestRunTemplateUsageMissingSubcommand(t *testing.T) {
	err := runTemplate([]string{})
	if err == nil {
		t.Fatal("expected error when no subcommand given")
	}
}

func TestRunTemplateUnknownSubcommand(t *testing.T) {
	err := runTemplate([]string{"unknown"})
	if err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
}

func TestValidateWorkflowConfig_SnakeCaseModuleField_Warning(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "server", Type: "http.server", Config: map[string]any{
				// snake_case form of "readTimeout" (known field)
				"read_timeout": "30s",
				// correct key
				"address": ":8080",
			}},
		},
	}
	knownModules := KnownModuleTypes()
	knownSteps := KnownStepTypes()
	knownTriggers := KnownTriggerTypes()

	result := validateWorkflowConfig("test", cfg, knownModules, knownSteps, knownTriggers)
	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "read_timeout") && strings.Contains(w, "readTimeout") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning for snake_case field 'read_timeout' suggesting 'readTimeout', got warnings: %v", result.Warnings)
	}
}

func TestValidateWorkflowConfig_SnakeCaseStepField_Warning(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Pipelines: map[string]any{
			"my-pipeline": map[string]any{
				"trigger": map[string]any{"type": "http"},
				"steps": []any{
					map[string]any{
						"name": "my-step",
						"type": "step.http_call",
						"config": map[string]any{
							// snake_case form of a known camelCase step config key
							"target_url": "http://example.com",
						},
					},
				},
			},
		},
	}
	knownModules := KnownModuleTypes()
	knownSteps := KnownStepTypes()
	knownTriggers := KnownTriggerTypes()

	result := validateWorkflowConfig("test", cfg, knownModules, knownSteps, knownTriggers)
	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "target_url") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning for snake_case step config field 'target_url', got warnings: %v", result.Warnings)
	}
}

func TestRunTemplateValidatePluginDir(t *testing.T) {
	pluginsDir := t.TempDir()
	// Create a fake plugin with a custom module type
	pluginSubdir := filepath.Join(pluginsDir, "my-external-plugin")
	if err := os.MkdirAll(pluginSubdir, 0755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"moduleTypes": ["custom.external.module"]}`
	if err := os.WriteFile(filepath.Join(pluginSubdir, "plugin.json"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}

	// Config that uses the external plugin module type
	dir := t.TempDir()
	configContent := `
modules:
  - name: ext-mod
    type: custom.external.module
`
	configPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	// Without --plugin-dir: should fail with unknown type
	err := runTemplateValidate([]string{"-config", configPath})
	if err == nil {
		t.Fatal("expected error for unknown external module type without --plugin-dir")
	}

	// With --plugin-dir: should pass
	if err := runTemplateValidate([]string{"-plugin-dir", pluginsDir, "-config", configPath}); err != nil {
		t.Errorf("expected valid config with --plugin-dir, got: %v", err)
	}
	t.Cleanup(func() {
		schema.UnregisterModuleType("custom.external.module")
	})
}

// --- Pipeline template expression linting tests ---

func TestValidateConfigWithValidStepRefs(t *testing.T) {
	dir := t.TempDir()
	configContent := `
pipelines:
  api:
    trigger:
      type: http
      config:
        path: /items/:id
        method: GET
    steps:
      - name: parse-request
        type: step.set
        config:
          values:
            item_id: "static-id"
      - name: db-query
        type: step.set
        config:
          values:
            query: "{{ .steps.parse-request.path_params.id }}"
`
	configPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	err := runTemplateValidate([]string{"-config", configPath})
	if err != nil {
		t.Fatalf("expected valid step refs to pass, got: %v", err)
	}
}

func TestValidateConfigWithMissingStepRef(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Pipelines: map[string]any{
			"api": map[string]any{
				"steps": []any{
					map[string]any{
						"name": "do-thing",
						"type": "step.set",
						"config": map[string]any{
							"values": map[string]any{
								"x": "{{ .steps.nonexistent.field }}",
							},
						},
					},
				},
			},
		},
	}
	knownModules := KnownModuleTypes()
	knownSteps := KnownStepTypes()
	knownTriggers := KnownTriggerTypes()

	result := validateWorkflowConfig("test", cfg, knownModules, knownSteps, knownTriggers)

	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "nonexistent") && strings.Contains(w, "does not exist") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning about nonexistent step reference, got warnings: %v", result.Warnings)
	}
}

func TestValidateConfigWithForwardStepRef(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Pipelines: map[string]any{
			"api": map[string]any{
				"steps": []any{
					map[string]any{
						"name": "first",
						"type": "step.set",
						"config": map[string]any{
							"values": map[string]any{
								"x": "{{ .steps.second.output }}",
							},
						},
					},
					map[string]any{
						"name": "second",
						"type": "step.set",
						"config": map[string]any{
							"values": map[string]any{
								"y": "hello",
							},
						},
					},
				},
			},
		},
	}
	knownModules := KnownModuleTypes()
	knownSteps := KnownStepTypes()
	knownTriggers := KnownTriggerTypes()

	result := validateWorkflowConfig("test", cfg, knownModules, knownSteps, knownTriggers)

	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "second") && strings.Contains(w, "has not executed yet") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning about forward step reference, got warnings: %v", result.Warnings)
	}
}

func TestValidateConfigWithStepFunction(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Pipelines: map[string]any{
			"api": map[string]any{
				"steps": []any{
					map[string]any{
						"name": "parse-request",
						"type": "step.set",
						"config": map[string]any{
							"values": map[string]any{
								"x": "raw-body",
							},
						},
					},
					map[string]any{
						"name": "process",
						"type": "step.set",
						"config": map[string]any{
							"values": map[string]any{
								"name": `{{ step "parse-request" "body" "name" }}`,
							},
						},
					},
				},
			},
		},
	}
	knownModules := KnownModuleTypes()
	knownSteps := KnownStepTypes()
	knownTriggers := KnownTriggerTypes()

	result := validateWorkflowConfig("test", cfg, knownModules, knownSteps, knownTriggers)

	// parse-request exists and is before process, so no warning about missing/forward ref
	for _, w := range result.Warnings {
		if strings.Contains(w, "parse-request") && (strings.Contains(w, "does not exist") || strings.Contains(w, "has not executed yet")) {
			t.Errorf("unexpected warning about parse-request: %s", w)
		}
	}
}

func TestValidateConfigWithSelfReference(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Pipelines: map[string]any{
			"api": map[string]any{
				"steps": []any{
					map[string]any{
						"name": "do-thing",
						"type": "step.set",
						"config": map[string]any{
							"values": map[string]any{
								"x": "{{ .steps.do-thing.output }}",
							},
						},
					},
				},
			},
		},
	}
	knownModules := KnownModuleTypes()
	knownSteps := KnownStepTypes()
	knownTriggers := KnownTriggerTypes()

	result := validateWorkflowConfig("test", cfg, knownModules, knownSteps, knownTriggers)

	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "references itself") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected self-reference warning, got warnings: %v", result.Warnings)
	}
}

func TestValidateConfigWithHyphenDotAccess(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Pipelines: map[string]any{
			"api": map[string]any{
				"steps": []any{
					map[string]any{
						"name": "my-step",
						"type": "step.set",
						"config": map[string]any{
							"values": map[string]any{
								"x": "hello",
							},
						},
					},
					map[string]any{
						"name": "consumer",
						"type": "step.set",
						"config": map[string]any{
							"values": map[string]any{
								"y": "{{ .steps.my-step.field }}",
							},
						},
					},
				},
			},
		},
	}
	knownModules := KnownModuleTypes()
	knownSteps := KnownStepTypes()
	knownTriggers := KnownTriggerTypes()

	result := validateWorkflowConfig("test", cfg, knownModules, knownSteps, knownTriggers)

	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "hyphenated dot-access") && strings.Contains(w, "prefer") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected informational warning about hyphenated dot-access, got warnings: %v", result.Warnings)
	}
}

// TestValidateStepOutputField_UndeclaredField_Warning checks that the validator
// warns when a template references a field that is not in the step type's
// declared output schema (Phase 1 static analysis).
func TestValidateStepOutputField_UndeclaredField_Warning(t *testing.T) {
	// step.db_query with mode:single declares outputs: row, found
	// Referencing .steps.query.rows (plural) should warn
	cfg := &config.WorkflowConfig{
		Pipelines: map[string]any{
			"api": map[string]any{
				"steps": []any{
					map[string]any{
						"name": "query",
						"type": "step.db_query",
						"config": map[string]any{
							"db":    "mydb",
							"query": "SELECT * FROM items WHERE id = 1",
							"mode":  "single",
						},
					},
					map[string]any{
						"name": "respond",
						"type": "step.json_response",
						"config": map[string]any{
							"status": 200,
							// "rows" is the list-mode output; single-mode declares "row"
							"body": `{{ json .steps.query.rows }}`,
						},
					},
				},
			},
		},
	}
	knownModules := KnownModuleTypes()
	knownSteps := KnownStepTypes()
	knownTriggers := KnownTriggerTypes()

	result := validateWorkflowConfig("test", cfg, knownModules, knownSteps, knownTriggers)

	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "rows") && strings.Contains(w, "query") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning for undeclared field 'rows' on step.db_query (single mode), got warnings: %v", result.Warnings)
	}
}

// TestValidateStepOutputField_DeclaredField_NoWarning checks that referencing a
// declared output field does not produce a warning.
func TestValidateStepOutputField_DeclaredField_NoWarning(t *testing.T) {
	// step.db_query with mode:single declares outputs: row, found
	// Referencing .steps.query.row should NOT warn
	cfg := &config.WorkflowConfig{
		Pipelines: map[string]any{
			"api": map[string]any{
				"steps": []any{
					map[string]any{
						"name": "query",
						"type": "step.db_query",
						"config": map[string]any{
							"db":    "mydb",
							"query": "SELECT * FROM items WHERE id = 1",
							"mode":  "single",
						},
					},
					map[string]any{
						"name": "respond",
						"type": "step.json_response",
						"config": map[string]any{
							"status": 200,
							"body":   `{{ json .steps.query.row }}`,
						},
					},
				},
			},
		},
	}
	knownModules := KnownModuleTypes()
	knownSteps := KnownStepTypes()
	knownTriggers := KnownTriggerTypes()

	result := validateWorkflowConfig("test", cfg, knownModules, knownSteps, knownTriggers)

	for _, w := range result.Warnings {
		if strings.Contains(w, "query.row") && strings.Contains(w, "declares outputs") {
			t.Errorf("unexpected warning for declared field 'row': %s", w)
		}
	}
}

// TestValidateStepOutputField_StepFuncSyntax_Warning verifies that the validator
// also checks field names in the step "NAME" "FIELD" function call syntax.
func TestValidateStepOutputField_StepFuncSyntax_Warning(t *testing.T) {
	// step.db_query with mode:list declares outputs: rows, count
	// Using step "query" "row" (singular) should warn
	cfg := &config.WorkflowConfig{
		Pipelines: map[string]any{
			"api": map[string]any{
				"steps": []any{
					map[string]any{
						"name": "query",
						"type": "step.db_query",
						"config": map[string]any{
							"mode": "list",
						},
					},
					map[string]any{
						"name": "respond",
						"type": "step.json_response",
						"config": map[string]any{
							"status": 200,
							// list mode declares "rows"/"count", not "row"
							"body": `{{ step "query" "row" }}`,
						},
					},
				},
			},
		},
	}
	knownModules := KnownModuleTypes()
	knownSteps := KnownStepTypes()
	knownTriggers := KnownTriggerTypes()

	result := validateWorkflowConfig("test", cfg, knownModules, knownSteps, knownTriggers)

	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "row") && strings.Contains(w, "query") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning for undeclared field 'row' on step.db_query (list mode) via step func, got warnings: %v", result.Warnings)
	}
}

// TestValidateStepOutputField_SetStep_NoWarning checks that step.set outputs
// inferred from config are validated correctly (declared key should not warn).
func TestValidateStepOutputField_SetStep_NoWarning(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Pipelines: map[string]any{
			"api": map[string]any{
				"steps": []any{
					map[string]any{
						"name": "setter",
						"type": "step.set",
						"config": map[string]any{
							"values": map[string]any{
								"user_id": "123",
							},
						},
					},
					map[string]any{
						"name": "respond",
						"type": "step.json_response",
						"config": map[string]any{
							"status": 200,
							"body":   `{{ .steps.setter.user_id }}`,
						},
					},
				},
			},
		},
	}
	knownModules := KnownModuleTypes()
	knownSteps := KnownStepTypes()
	knownTriggers := KnownTriggerTypes()

	result := validateWorkflowConfig("test", cfg, knownModules, knownSteps, knownTriggers)

	for _, w := range result.Warnings {
		if strings.Contains(w, "declares outputs") && strings.Contains(w, "setter") {
			t.Errorf("unexpected step output warning for step.set with declared key: %s", w)
		}
	}
}

// TestValidateStepOutputField_NoOutputSchema_NoWarning checks that steps with
// no declared outputs do not produce false-positive field warnings.
func TestValidateStepOutputField_NoOutputSchema_NoWarning(t *testing.T) {
	// An unknown/external step type has no schema — any field reference should be silently ignored
	cfg := &config.WorkflowConfig{
		Pipelines: map[string]any{
			"api": map[string]any{
				"steps": []any{
					map[string]any{
						"name": "external",
						"type": "step.external_custom_step", // not in registry
					},
					map[string]any{
						"name": "respond",
						"type": "step.json_response",
						"config": map[string]any{
							"status": 200,
							"body":   `{{ .steps.external.anything }}`,
						},
					},
				},
			},
		},
	}
	knownModules := KnownModuleTypes()
	knownSteps := KnownStepTypes()
	knownTriggers := KnownTriggerTypes()

	result := validateWorkflowConfig("test", cfg, knownModules, knownSteps, knownTriggers)

	for _, w := range result.Warnings {
		if strings.Contains(w, "declares outputs") && strings.Contains(w, "external") {
			t.Errorf("unexpected output field warning for step with no declared outputs: %s", w)
		}
	}
}

// TestValidateStepOutputFieldRegistry tests validateStepOutputField directly.
func TestValidateStepOutputFieldRegistry(t *testing.T) {
	reg := schema.NewStepSchemaRegistry()

	tests := []struct {
		name       string
		stepName   string
		stepType   string
		stepConfig map[string]any
		refField   string
		expectWarn bool
	}{
		{
			name:       "db_query single mode: valid field row",
			stepName:   "q",
			stepType:   "step.db_query",
			stepConfig: map[string]any{"mode": "single"},
			refField:   "row",
			expectWarn: false,
		},
		{
			name:       "db_query single mode: invalid field rows",
			stepName:   "q",
			stepType:   "step.db_query",
			stepConfig: map[string]any{"mode": "single"},
			refField:   "rows",
			expectWarn: true,
		},
		{
			name:       "db_query list mode: valid field rows",
			stepName:   "q",
			stepType:   "step.db_query",
			stepConfig: map[string]any{"mode": "list"},
			refField:   "rows",
			expectWarn: false,
		},
		{
			name:       "db_query list mode: invalid field row",
			stepName:   "q",
			stepType:   "step.db_query",
			stepConfig: map[string]any{"mode": "list"},
			refField:   "row",
			expectWarn: true,
		},
		{
			name:       "step.set: declared key is valid",
			stepName:   "s",
			stepType:   "step.set",
			stepConfig: map[string]any{"values": map[string]any{"my_field": "v"}},
			refField:   "my_field",
			expectWarn: false,
		},
		{
			name:       "step.set: undeclared key warns",
			stepName:   "s",
			stepType:   "step.set",
			stepConfig: map[string]any{"values": map[string]any{"my_field": "v"}},
			refField:   "other_field",
			expectWarn: true,
		},
		{
			name:       "unknown step type: no warning",
			stepName:   "s",
			stepType:   "step.nonexistent",
			stepConfig: nil,
			refField:   "anything",
			expectWarn: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := &templateValidationResult{}
			stepMeta := map[string]pipelineStepMeta{
				tc.stepName: {typ: tc.stepType, config: tc.stepConfig},
			}
			validateStepOutputField("pipeline", "current-step", tc.stepName, tc.refField, stepMeta, reg, result)
			hasWarn := len(result.Warnings) > 0
			if hasWarn != tc.expectWarn {
				t.Errorf("expectWarn=%v but warnings=%v", tc.expectWarn, result.Warnings)
			}
		})
	}
}

// --- Output field name validation tests ---

// TestValidateStepOutputField_KnownField checks that a reference to a known output
// field does NOT produce a warning.
func TestValidateStepOutputField_KnownField(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Pipelines: map[string]any{
			"api": map[string]any{
				"steps": []any{
					map[string]any{
						"name":   "query",
						"type":   "step.db_query",
						"config": map[string]any{"database": "db", "query": "SELECT id FROM users", "mode": "single"},
					},
					map[string]any{
						"name": "respond",
						"type": "step.set",
						"config": map[string]any{
							"values": map[string]any{
								// "found" is a known output of step.db_query single mode
								"ok": "{{ .steps.query.found }}",
							},
						},
					},
				},
			},
		},
	}
	knownModules := KnownModuleTypes()
	knownSteps := KnownStepTypes()
	knownTriggers := KnownTriggerTypes()

	result := validateWorkflowConfig("test", cfg, knownModules, knownSteps, knownTriggers)

	for _, w := range result.Warnings {
		if strings.Contains(w, "found") && strings.Contains(w, "not a known output") {
			t.Errorf("unexpected warning about known output field 'found': %s", w)
		}
	}
}

// TestValidateStepOutputField_UnknownField checks that a reference to an unknown
// output field produces a warning.
func TestValidateStepOutputField_UnknownField(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Pipelines: map[string]any{
			"api": map[string]any{
				"steps": []any{
					map[string]any{
						"name":   "query",
						"type":   "step.db_query",
						"config": map[string]any{"database": "db", "query": "SELECT id FROM users", "mode": "single"},
					},
					map[string]any{
						"name": "respond",
						"type": "step.set",
						"config": map[string]any{
							"values": map[string]any{
								// "nonexistent_column" is NOT an output of step.db_query
								"x": "{{ .steps.query.nonexistent_column }}",
							},
						},
					},
				},
			},
		},
	}
	knownModules := KnownModuleTypes()
	knownSteps := KnownStepTypes()
	knownTriggers := KnownTriggerTypes()

	result := validateWorkflowConfig("test", cfg, knownModules, knownSteps, knownTriggers)

	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "nonexistent_column") && strings.Contains(w, "not a known output") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning about unknown output field 'nonexistent_column', got warnings: %v", result.Warnings)
	}
}

// TestValidateStepOutputField_SQLAlias_Valid checks that a reference to a SQL column
// alias that IS present in the query does NOT produce a warning.
func TestValidateStepOutputField_SQLAlias_Valid(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Pipelines: map[string]any{
			"api": map[string]any{
				"steps": []any{
					map[string]any{
						"name": "load",
						"type": "step.db_query",
						"config": map[string]any{
							"database": "db",
							"query":    "SELECT auth_token, affiliate_id FROM integrations WHERE id = $1",
							"mode":     "single",
						},
					},
					map[string]any{
						"name": "verify",
						"type": "step.set",
						"config": map[string]any{
							"values": map[string]any{
								// auth_token IS selected by the SQL query
								"tok": "{{ .steps.load.row.auth_token }}",
							},
						},
					},
				},
			},
		},
	}
	knownModules := KnownModuleTypes()
	knownSteps := KnownStepTypes()
	knownTriggers := KnownTriggerTypes()

	result := validateWorkflowConfig("test", cfg, knownModules, knownSteps, knownTriggers)

	for _, w := range result.Warnings {
		if strings.Contains(w, "auth_token") && strings.Contains(w, "does not select") {
			t.Errorf("unexpected SQL alias warning for known alias 'auth_token': %s", w)
		}
	}
}

// TestValidateStepOutputField_SQLAlias_Invalid checks that a reference to a SQL
// column alias that is NOT present in the query produces a warning.
func TestValidateStepOutputField_SQLAlias_Invalid(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Pipelines: map[string]any{
			"api": map[string]any{
				"steps": []any{
					map[string]any{
						"name": "load",
						"type": "step.db_query",
						"config": map[string]any{
							"database": "db",
							// Query selects "auth_token" but not "token"
							"query": "SELECT auth_token FROM integrations WHERE id = $1",
							"mode":  "single",
						},
					},
					map[string]any{
						"name": "verify",
						"type": "step.set",
						"config": map[string]any{
							"values": map[string]any{
								// "token" is NOT a column in the query
								"tok": "{{ .steps.load.row.token }}",
							},
						},
					},
				},
			},
		},
	}
	knownModules := KnownModuleTypes()
	knownSteps := KnownStepTypes()
	knownTriggers := KnownTriggerTypes()

	result := validateWorkflowConfig("test", cfg, knownModules, knownSteps, knownTriggers)

	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "token") && strings.Contains(w, "does not select") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected SQL alias warning for missing column 'token', got warnings: %v", result.Warnings)
	}
}

// TestValidatePlainStepRef_SecretFrom checks that a secret_from value referencing
// a nonexistent step produces a warning.
func TestValidatePlainStepRef_SecretFrom(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Pipelines: map[string]any{
			"api": map[string]any{
				"steps": []any{
					map[string]any{
						"name": "verify",
						"type": "step.webhook_verify",
						"config": map[string]any{
							// references a step "load" that does not exist in this pipeline
							"secret_from": "steps.load.row.auth_token",
						},
					},
				},
			},
		},
	}
	knownModules := KnownModuleTypes()
	knownSteps := KnownStepTypes()
	knownTriggers := KnownTriggerTypes()

	result := validateWorkflowConfig("test", cfg, knownModules, knownSteps, knownTriggers)

	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "load") && strings.Contains(w, "does not exist") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning about nonexistent step in secret_from, got warnings: %v", result.Warnings)
	}
}

// TestValidatePlainStepRef_SecretFrom_Valid checks that a valid secret_from reference
// pointing to a known step output does NOT produce a warning.
func TestValidatePlainStepRef_SecretFrom_Valid(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Pipelines: map[string]any{
			"api": map[string]any{
				"steps": []any{
					map[string]any{
						"name": "load",
						"type": "step.db_query",
						"config": map[string]any{
							"database": "db",
							"query":    "SELECT auth_token FROM integrations WHERE id = $1",
							"mode":     "single",
						},
					},
					map[string]any{
						"name": "verify",
						"type": "step.webhook_verify",
						"config": map[string]any{
							// references load.row — "row" is a known output of step.db_query single mode
							"secret_from": "steps.load.row.auth_token",
						},
					},
				},
			},
		},
	}
	knownModules := KnownModuleTypes()
	knownSteps := KnownStepTypes()
	knownTriggers := KnownTriggerTypes()

	result := validateWorkflowConfig("test", cfg, knownModules, knownSteps, knownTriggers)

	for _, w := range result.Warnings {
		if strings.Contains(w, "load") && strings.Contains(w, "does not exist") {
			t.Errorf("unexpected warning about existing step 'load': %s", w)
		}
		if strings.Contains(w, "row") && strings.Contains(w, "not a known output") {
			t.Errorf("unexpected warning about known output field 'row': %s", w)
		}
	}
}

// TestValidatePlainStepRef_ConditionalField checks that a conditional step's
// "field" config value pointing to a nonexistent step output is warned about.
func TestValidatePlainStepRef_ConditionalField(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Pipelines: map[string]any{
			"api": map[string]any{
				"steps": []any{
					map[string]any{
						"name":   "query",
						"type":   "step.db_query",
						"config": map[string]any{"database": "db", "query": "SELECT id FROM t", "mode": "single"},
					},
					map[string]any{
						"name": "route",
						"type": "step.conditional",
						"config": map[string]any{
							// "found" is a known output of step.db_query single mode
							"field":   "steps.query.found",
							"routes":  map[string]any{"true": "a"},
							"default": "b",
						},
					},
				},
			},
		},
	}
	knownModules := KnownModuleTypes()
	knownSteps := KnownStepTypes()
	knownTriggers := KnownTriggerTypes()

	result := validateWorkflowConfig("test", cfg, knownModules, knownSteps, knownTriggers)

	for _, w := range result.Warnings {
		if strings.Contains(w, "found") && strings.Contains(w, "not a known output") {
			t.Errorf("unexpected warning about known output field 'found' in conditional field: %s", w)
		}
		if strings.Contains(w, "query") && strings.Contains(w, "does not exist") {
			t.Errorf("unexpected 'does not exist' warning for existing step 'query': %s", w)
		}
	}
}

// TestValidatePlainStepRef_ConditionalField_Unknown verifies that a conditional
// step's "field" referencing a step output that doesn't exist emits a warning.
func TestValidatePlainStepRef_ConditionalField_Unknown(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Pipelines: map[string]any{
			"api": map[string]any{
				"steps": []any{
					map[string]any{
						"name":   "query",
						"type":   "step.db_query",
						"config": map[string]any{"database": "db", "query": "SELECT id FROM t", "mode": "single"},
					},
					map[string]any{
						"name": "route",
						"type": "step.conditional",
						"config": map[string]any{
							// "nonexistent_output" is NOT an output of step.db_query
							"field":   "steps.query.nonexistent_output",
							"routes":  map[string]any{"true": "a"},
							"default": "b",
						},
					},
				},
			},
		},
	}
	knownModules := KnownModuleTypes()
	knownSteps := KnownStepTypes()
	knownTriggers := KnownTriggerTypes()

	result := validateWorkflowConfig("test", cfg, knownModules, knownSteps, knownTriggers)

	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "nonexistent_output") && strings.Contains(w, "not a known output") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning about unknown output field in conditional field, got warnings: %v", result.Warnings)
	}
}

// TestValidateStepOutputField_DynamicOutputSkipped verifies that steps with
// dynamic/wildcard placeholder outputs (e.g. "(key)" from step.secret_fetch,
// "(dynamic)" from step.set) do NOT generate false-positive warnings when
// arbitrary field names are accessed.
func TestValidateStepOutputField_DynamicOutputSkipped(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Pipelines: map[string]any{
			"api": map[string]any{
				"steps": []any{
					map[string]any{
						"name": "fetch",
						"type": "step.secret_fetch",
						"config": map[string]any{
							"secrets": map[string]any{
								"api_key": "env://API_KEY",
							},
						},
					},
					map[string]any{
						"name": "call",
						"type": "step.http_call",
						"config": map[string]any{
							// "api_key" is a dynamic output of step.secret_fetch (not statically declared)
							"url": "https://api.example.com",
							"headers": map[string]any{
								"Authorization": "Bearer {{ .steps.fetch.api_key }}",
							},
						},
					},
				},
			},
		},
	}
	knownModules := KnownModuleTypes()
	knownSteps := KnownStepTypes()
	knownTriggers := KnownTriggerTypes()

	result := validateWorkflowConfig("test", cfg, knownModules, knownSteps, knownTriggers)

	for _, w := range result.Warnings {
		if strings.Contains(w, "api_key") && strings.Contains(w, "not a known output") {
			t.Errorf("unexpected false-positive warning about dynamic output field 'api_key': %s", w)
		}
	}
}
