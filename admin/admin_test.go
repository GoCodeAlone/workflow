package admin

import (
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func TestLoadConfigRaw(t *testing.T) {
	t.Parallel()

	data, err := LoadConfigRaw()
	if err != nil {
		t.Fatalf("LoadConfigRaw() error: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("LoadConfigRaw() returned empty data")
	}
}

func TestLoadConfig(t *testing.T) {
	t.Parallel()

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error: %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadConfig() returned nil config")
	}
	if len(cfg.Modules) == 0 {
		t.Error("expected at least one admin module")
	}
}

func TestMergeInto(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		primary *config.WorkflowConfig
		admin   *config.WorkflowConfig
		check   func(t *testing.T, result *config.WorkflowConfig)
	}{
		{
			name: "merge modules appended",
			primary: &config.WorkflowConfig{
				Modules: []config.ModuleConfig{
					{Name: "primary-mod", Type: "http.server"},
				},
				Workflows: map[string]any{"http": "primary-wf"},
				Triggers:  map[string]any{"http": "primary-trigger"},
			},
			admin: &config.WorkflowConfig{
				Modules: []config.ModuleConfig{
					{Name: "admin-mod", Type: "http.server"},
				},
				Workflows: map[string]any{"http-admin": "admin-wf"},
				Triggers:  map[string]any{"admin-trigger": "admin-cfg"},
			},
			check: func(t *testing.T, result *config.WorkflowConfig) {
				if len(result.Modules) != 2 {
					t.Errorf("expected 2 modules, got %d", len(result.Modules))
				}
				if result.Workflows["http-admin"] == nil {
					t.Error("admin workflow not merged")
				}
				if result.Triggers["admin-trigger"] == nil {
					t.Error("admin trigger not merged")
				}
			},
		},
		{
			name: "workflows not overwritten",
			primary: &config.WorkflowConfig{
				Modules:   nil,
				Workflows: map[string]any{"http": "primary"},
				Triggers:  nil,
			},
			admin: &config.WorkflowConfig{
				Modules:   nil,
				Workflows: map[string]any{"http": "admin-should-not-replace"},
				Triggers:  nil,
			},
			check: func(t *testing.T, result *config.WorkflowConfig) {
				if result.Workflows["http"] != "primary" {
					t.Errorf("primary workflow was overwritten: got %v", result.Workflows["http"])
				}
			},
		},
		{
			name: "nil primary workflows map initialized",
			primary: &config.WorkflowConfig{
				Workflows: nil,
				Triggers:  nil,
			},
			admin: &config.WorkflowConfig{
				Workflows: map[string]any{"admin-wf": "cfg"},
				Triggers:  nil,
			},
			check: func(t *testing.T, result *config.WorkflowConfig) {
				if result.Workflows == nil {
					t.Fatal("workflows map should have been initialized")
				}
				if result.Workflows["admin-wf"] == nil {
					t.Error("admin workflow not merged into nil map")
				}
			},
		},
		{
			name: "nil primary triggers map initialized",
			primary: &config.WorkflowConfig{
				Workflows: map[string]any{},
				Triggers:  nil,
			},
			admin: &config.WorkflowConfig{
				Workflows: nil,
				Triggers:  map[string]any{"admin-trig": "cfg"},
			},
			check: func(t *testing.T, result *config.WorkflowConfig) {
				if result.Triggers == nil {
					t.Fatal("triggers map should have been initialized")
				}
				if result.Triggers["admin-trig"] == nil {
					t.Error("admin trigger not merged into nil map")
				}
			},
		},
		{
			name: "triggers not overwritten",
			primary: &config.WorkflowConfig{
				Workflows: map[string]any{},
				Triggers:  map[string]any{"http": "primary"},
			},
			admin: &config.WorkflowConfig{
				Triggers: map[string]any{"http": "admin-should-not-replace"},
			},
			check: func(t *testing.T, result *config.WorkflowConfig) {
				if result.Triggers["http"] != "primary" {
					t.Errorf("primary trigger was overwritten: got %v", result.Triggers["http"])
				}
			},
		},
		{
			name: "empty admin triggers no-op",
			primary: &config.WorkflowConfig{
				Workflows: map[string]any{},
				Triggers:  map[string]any{"existing": "val"},
			},
			admin: &config.WorkflowConfig{
				Triggers: nil,
			},
			check: func(t *testing.T, result *config.WorkflowConfig) {
				if len(result.Triggers) != 1 {
					t.Errorf("expected 1 trigger, got %d", len(result.Triggers))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			MergeInto(tt.primary, tt.admin)
			tt.check(t, tt.primary)
		})
	}
}

func TestMergeInto_WithRealAdminConfig(t *testing.T) {
	t.Parallel()

	adminCfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	primary := config.NewEmptyWorkflowConfig()
	primary.Modules = []config.ModuleConfig{
		{Name: "user-server", Type: "http.server"},
	}

	initialModuleCount := len(primary.Modules)
	MergeInto(primary, adminCfg)

	if len(primary.Modules) <= initialModuleCount {
		t.Error("expected admin modules to be appended")
	}
}
