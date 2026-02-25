package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func TestRunCompatMissingSubcommand(t *testing.T) {
	err := runCompat([]string{})
	if err == nil {
		t.Fatal("expected error when no subcommand given")
	}
}

func TestRunCompatUnknownSubcommand(t *testing.T) {
	err := runCompat([]string{"unknown"})
	if err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
}

func TestRunCompatCheckMissingConfig(t *testing.T) {
	err := runCompatCheck([]string{})
	if err == nil {
		t.Fatal("expected error when no config given")
	}
}

func TestRunCompatCheckValidConfig(t *testing.T) {
	dir := t.TempDir()
	configContent := `
modules:
  - name: server
    type: http.server
    config:
      address: ":8080"
  - name: router
    type: http.router
  - name: auth
    type: auth.jwt
    config:
      secret: test-secret
`
	configPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	if err := runCompatCheck([]string{configPath}); err != nil {
		t.Fatalf("expected compatible config to pass, got: %v", err)
	}
}

func TestRunCompatCheckUnknownModule(t *testing.T) {
	dir := t.TempDir()
	configContent := `
modules:
  - name: unknown-thing
    type: not.a.real.module.type
`
	configPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	err := runCompatCheck([]string{configPath})
	if err == nil {
		t.Fatal("expected error for unknown module type")
	}
	if !strings.Contains(err.Error(), "compatibility check failed") {
		t.Errorf("expected compatibility check failed, got: %v", err)
	}
}

func TestRunCompatCheckJSON(t *testing.T) {
	dir := t.TempDir()
	configContent := `
modules:
  - name: server
    type: http.server
`
	configPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	if err := runCompatCheck([]string{"-format", "json", configPath}); err != nil {
		t.Fatalf("expected JSON output to work, got: %v", err)
	}
}

func TestCheckCompatibilityAllKnown(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "server", Type: "http.server"},
			{Name: "db", Type: "storage.sqlite"},
			{Name: "auth", Type: "auth.jwt"},
		},
	}

	result := checkCompatibility(cfg)
	if !result.Compatible {
		t.Errorf("expected compatible config, got issues: %v", result.Issues)
	}
	if len(result.RequiredModules) != 3 {
		t.Errorf("expected 3 required modules, got %d", len(result.RequiredModules))
	}
	for _, m := range result.RequiredModules {
		if !m.Available {
			t.Errorf("expected module %q to be available", m.Type)
		}
	}
}

func TestCheckCompatibilityUnknownModule(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "server", Type: "http.server"},
			{Name: "unknown", Type: "future.engine.feature"},
		},
	}

	result := checkCompatibility(cfg)
	if result.Compatible {
		t.Error("expected incompatible result for unknown module type")
	}
	if len(result.Issues) == 0 {
		t.Error("expected issues to be populated")
	}

	// Check that the unknown module appears as not available
	found := false
	for _, m := range result.RequiredModules {
		if m.Type == "future.engine.feature" && !m.Available {
			found = true
		}
	}
	if !found {
		t.Error("expected future.engine.feature to be marked as not available")
	}
}

func TestCheckCompatibilityWithSteps(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Pipelines: map[string]any{
			"my-pipeline": map[string]any{
				"trigger": map[string]any{
					"type": "http",
					"config": map[string]any{
						"path":   "/test",
						"method": "GET",
					},
				},
				"steps": []any{
					map[string]any{"type": "step.validate"},
					map[string]any{"type": "step.json_response"},
					map[string]any{"type": "step.db_query"},
				},
			},
		},
	}

	result := checkCompatibility(cfg)
	if !result.Compatible {
		t.Errorf("expected compatible config, got issues: %v", result.Issues)
	}
	if len(result.RequiredSteps) != 3 {
		t.Errorf("expected 3 required steps, got %d", len(result.RequiredSteps))
	}
}

func TestCheckCompatibilityUnknownStep(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Pipelines: map[string]any{
			"my-pipeline": map[string]any{
				"steps": []any{
					map[string]any{"type": "step.future_step_type"},
				},
			},
		},
	}

	result := checkCompatibility(cfg)
	if result.Compatible {
		t.Error("expected incompatible result for unknown step type")
	}
}

func TestCheckCompatibilityEngineVersion(t *testing.T) {
	cfg := &config.WorkflowConfig{}
	result := checkCompatibility(cfg)
	if result.EngineVersion == "" {
		t.Error("expected engine version to be set")
	}
}

func TestCheckCompatibilityDeduplicate(t *testing.T) {
	// Same module type used multiple times
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "server1", Type: "http.server"},
			{Name: "server2", Type: "http.server"},
			{Name: "db", Type: "storage.sqlite"},
		},
	}

	result := checkCompatibility(cfg)
	// After deduplication, should have 2 unique types
	if len(result.RequiredModules) != 2 {
		t.Errorf("expected 2 deduplicated module types, got %d", len(result.RequiredModules))
	}
}

func TestSortCompatItems(t *testing.T) {
	items := []compatItem{
		{Type: "z.module", Available: true},
		{Type: "a.module", Available: true},
		{Type: "m.module", Available: false},
	}
	sortCompatItems(items)
	if items[0].Type != "a.module" {
		t.Errorf("expected first item to be a.module, got %q", items[0].Type)
	}
	if items[2].Type != "z.module" {
		t.Errorf("expected last item to be z.module, got %q", items[2].Type)
	}
}

func TestDeduplicateCompatItems(t *testing.T) {
	items := []compatItem{
		{Type: "http.server", Available: true},
		{Type: "http.router", Available: true},
		{Type: "http.server", Available: true},
	}
	result := deduplicateCompatItems(items)
	if len(result) != 2 {
		t.Errorf("expected 2 unique items, got %d", len(result))
	}
}
