package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
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
