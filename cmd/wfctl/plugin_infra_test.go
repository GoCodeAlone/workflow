package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func TestLoadPluginManifests_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	manifests, err := LoadPluginManifests(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(manifests) != 0 {
		t.Errorf("expected empty map, got %d entries", len(manifests))
	}
}

func TestLoadPluginManifests_NonExistentDir(t *testing.T) {
	manifests, err := LoadPluginManifests("/nonexistent/plugins/dir")
	if err != nil {
		t.Fatalf("unexpected error for missing dir: %v", err)
	}
	if len(manifests) != 0 {
		t.Errorf("expected empty map for missing dir, got %d entries", len(manifests))
	}
}

func TestLoadPluginManifests_ParsesManifests(t *testing.T) {
	dir := t.TempDir()

	// Create two plugin directories
	for _, plugin := range []struct {
		name     string
		manifest config.PluginManifestFile
	}{
		{
			name: "workflow-plugin-agent",
			manifest: config.PluginManifestFile{
				Name:    "workflow-plugin-agent",
				Version: "0.5.2",
				Capabilities: config.PluginCapabilities{
					ModuleTypes: []string{"agent.runner"},
					StepTypes:   []string{"step.agent_execute"},
				},
				ModuleInfraRequirements: config.PluginInfraRequirements{
					"agent.runner": {
						Requires: []config.InfraRequirement{
							{
								Type:        "database",
								Name:        "agent-db",
								Description: "SQLite for agent memory",
								Optional:    true,
							},
						},
					},
				},
			},
		},
		{
			name: "workflow-plugin-payments",
			manifest: config.PluginManifestFile{
				Name:    "workflow-plugin-payments",
				Version: "0.2.1",
				Capabilities: config.PluginCapabilities{
					ModuleTypes: []string{"payments.provider"},
					StepTypes:   []string{"step.payment_charge"},
				},
				ModuleInfraRequirements: config.PluginInfraRequirements{
					"payments.provider": {
						Requires: []config.InfraRequirement{
							{Type: "cache", Name: "payment-cache", Description: "Redis for idempotency"},
						},
					},
				},
			},
		},
	} {
		pluginDir := filepath.Join(dir, plugin.name)
		if err := os.MkdirAll(pluginDir, 0o755); err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(plugin.manifest)
		if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Also add a dir without plugin.json — should be silently skipped
	if err := os.MkdirAll(filepath.Join(dir, "no-manifest"), 0o755); err != nil {
		t.Fatal(err)
	}

	manifests, err := LoadPluginManifests(dir)
	if err != nil {
		t.Fatalf("LoadPluginManifests: %v", err)
	}
	if len(manifests) != 2 {
		t.Fatalf("expected 2 manifests, got %d", len(manifests))
	}

	agent, ok := manifests["workflow-plugin-agent"]
	if !ok {
		t.Fatal("expected workflow-plugin-agent")
	}
	if agent.Version != "0.5.2" {
		t.Errorf("version: got %q", agent.Version)
	}
	spec := agent.ModuleInfraRequirements["agent.runner"]
	if spec == nil || len(spec.Requires) != 1 {
		t.Fatal("expected agent.runner infra requirements")
	}
}

func TestDetectPluginInfraNeeds_Basic(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "my-agent", Type: "agent.runner"},
			{Name: "my-db", Type: "database.postgres"}, // no manifest for this
		},
	}
	manifests := map[string]*config.PluginManifestFile{
		"workflow-plugin-agent": {
			Name: "workflow-plugin-agent",
			ModuleInfraRequirements: config.PluginInfraRequirements{
				"agent.runner": {
					Requires: []config.InfraRequirement{
						{Type: "database", Name: "agent-db", Description: "SQLite"},
					},
				},
			},
		},
	}

	needs := DetectPluginInfraNeeds(cfg, manifests)
	if len(needs) != 1 {
		t.Fatalf("expected 1 need, got %d: %v", len(needs), needs)
	}
	if needs[0].Type != "database" || needs[0].Name != "agent-db" {
		t.Errorf("unexpected need: %+v", needs[0])
	}
}

func TestDetectPluginInfraNeeds_Deduplication(t *testing.T) {
	// Two modules of the same type → only one set of requirements
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "agent-1", Type: "agent.runner"},
			{Name: "agent-2", Type: "agent.runner"},
		},
	}
	manifests := map[string]*config.PluginManifestFile{
		"workflow-plugin-agent": {
			Name: "workflow-plugin-agent",
			ModuleInfraRequirements: config.PluginInfraRequirements{
				"agent.runner": {
					Requires: []config.InfraRequirement{
						{Type: "database", Name: "agent-db"},
					},
				},
			},
		},
	}

	needs := DetectPluginInfraNeeds(cfg, manifests)
	if len(needs) != 1 {
		t.Errorf("expected 1 deduplicated need, got %d", len(needs))
	}
}

func TestDetectPluginInfraNeeds_NoManifests(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "my-agent", Type: "agent.runner"},
		},
	}
	needs := DetectPluginInfraNeeds(cfg, nil)
	if len(needs) != 0 {
		t.Errorf("expected no needs with no manifests, got %d", len(needs))
	}
}

func TestDetectPluginInfraNeeds_ServiceModules(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Services: map[string]*config.ServiceConfig{
			"api": {
				Modules: []config.ModuleConfig{
					{Name: "payments", Type: "payments.provider"},
				},
			},
		},
	}
	manifests := map[string]*config.PluginManifestFile{
		"workflow-plugin-payments": {
			Name: "workflow-plugin-payments",
			ModuleInfraRequirements: config.PluginInfraRequirements{
				"payments.provider": {
					Requires: []config.InfraRequirement{
						{Type: "cache", Name: "payment-cache"},
					},
				},
			},
		},
	}

	needs := DetectPluginInfraNeeds(cfg, manifests)
	if len(needs) != 1 {
		t.Fatalf("expected 1 need from service module, got %d", len(needs))
	}
	if needs[0].Type != "cache" {
		t.Errorf("type: got %q", needs[0].Type)
	}
}
