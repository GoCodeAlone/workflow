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
