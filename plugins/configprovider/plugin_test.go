package configprovider

import (
	"testing"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/module"
)

func TestPluginMetadata(t *testing.T) {
	p := New()
	if p.Name() != "configprovider" {
		t.Fatalf("expected name 'configprovider', got %q", p.Name())
	}
	if p.Version() != "1.0.0" {
		t.Fatalf("expected version '1.0.0', got %q", p.Version())
	}
	manifest := p.EngineManifest()
	if len(manifest.ModuleTypes) != 1 || manifest.ModuleTypes[0] != "config.provider" {
		t.Fatalf("unexpected module types: %v", manifest.ModuleTypes)
	}
}

func TestPluginModuleFactories(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()
	if _, ok := factories["config.provider"]; !ok {
		t.Fatal("expected config.provider factory")
	}
	mod := factories["config.provider"]("test-config", map[string]any{})
	if mod == nil {
		t.Fatal("expected non-nil module")
	}
	if mod.Name() != "test-config" {
		t.Fatalf("expected module name 'test-config', got %q", mod.Name())
	}
}

func TestPluginConfigTransformHooks(t *testing.T) {
	p := New()
	hooks := p.ConfigTransformHooks()
	if len(hooks) != 1 {
		t.Fatalf("expected 1 hook, got %d", len(hooks))
	}
	if hooks[0].Name != "config-provider-expansion" {
		t.Fatalf("unexpected hook name: %q", hooks[0].Name)
	}
	if hooks[0].Priority != 1000 {
		t.Fatalf("expected priority 1000, got %d", hooks[0].Priority)
	}
}

func TestConfigTransformHookNoProvider(t *testing.T) {
	// When there's no config.provider module, hook is a no-op
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "server", Type: "http.server", Config: map[string]any{"port": "8080"}},
		},
	}
	p := New()
	hooks := p.ConfigTransformHooks()
	if err := hooks[0].Hook(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConfigTransformHookBasic(t *testing.T) {
	module.GetConfigRegistry().Reset()

	t.Setenv("HOOK_TEST_DB_DSN", "postgres://test/db")

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name: "app-config",
				Type: "config.provider",
				Config: map[string]any{
					"sources": []any{
						map[string]any{"type": "defaults"},
						map[string]any{"type": "env"},
					},
					"schema": map[string]any{
						"db_dsn": map[string]any{
							"env":       "HOOK_TEST_DB_DSN",
							"required":  true,
							"sensitive": true,
						},
						"port": map[string]any{
							"env":     "HOOK_TEST_PORT",
							"default": "8080",
						},
					},
				},
			},
			{
				Name: "db",
				Type: "database.workflow",
				Config: map[string]any{
					"driver":  "postgres",
					"dsn":     `{{config "db_dsn"}}`,
					"address": `0.0.0.0:{{config "port"}}`,
				},
			},
		},
	}

	p := New()
	hooks := p.ConfigTransformHooks()
	if err := hooks[0].Hook(cfg); err != nil {
		t.Fatalf("hook error: %v", err)
	}

	// Verify the db module's config was expanded
	dbCfg := cfg.Modules[1].Config
	if dbCfg["dsn"] != "postgres://test/db" {
		t.Fatalf("dsn not expanded: %q", dbCfg["dsn"])
	}
	if dbCfg["address"] != "0.0.0.0:8080" {
		t.Fatalf("address not expanded: %q", dbCfg["address"])
	}
	if dbCfg["driver"] != "postgres" {
		t.Fatalf("driver changed unexpectedly: %q", dbCfg["driver"])
	}
}

func TestConfigTransformHookMissingRequired(t *testing.T) {
	module.GetConfigRegistry().Reset()

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name: "app-config",
				Type: "config.provider",
				Config: map[string]any{
					"sources": []any{
						map[string]any{"type": "defaults"},
						map[string]any{"type": "env"},
					},
					"schema": map[string]any{
						"required_key": map[string]any{
							"env":      "NEVER_SET_THIS_KEY_12345",
							"required": true,
						},
					},
				},
			},
		},
	}

	p := New()
	hooks := p.ConfigTransformHooks()
	err := hooks[0].Hook(cfg)
	if err == nil {
		t.Fatal("expected error for missing required key")
	}
}

func TestConfigTransformHookWorkflowExpansion(t *testing.T) {
	module.GetConfigRegistry().Reset()

	t.Setenv("HOOK_WF_REGION", "eu-west-1")

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name: "app-config",
				Type: "config.provider",
				Config: map[string]any{
					"sources": []any{
						map[string]any{"type": "defaults"},
						map[string]any{"type": "env"},
					},
					"schema": map[string]any{
						"region": map[string]any{
							"env":     "HOOK_WF_REGION",
							"default": "us-east-1",
						},
					},
				},
			},
		},
		Workflows: map[string]any{
			"http": map[string]any{
				"region": `{{config "region"}}`,
			},
		},
		Triggers: map[string]any{
			"main": map[string]any{
				"region": `{{config "region"}}`,
			},
		},
		Pipelines: map[string]any{
			"pipeline1": map[string]any{
				"region": `{{config "region"}}`,
			},
		},
	}

	p := New()
	hooks := p.ConfigTransformHooks()
	if err := hooks[0].Hook(cfg); err != nil {
		t.Fatalf("hook error: %v", err)
	}

	wf := cfg.Workflows["http"].(map[string]any)
	if wf["region"] != "eu-west-1" {
		t.Fatalf("workflow region not expanded: %q", wf["region"])
	}
	tr := cfg.Triggers["main"].(map[string]any)
	if tr["region"] != "eu-west-1" {
		t.Fatalf("trigger region not expanded: %q", tr["region"])
	}
	pl := cfg.Pipelines["pipeline1"].(map[string]any)
	if pl["region"] != "eu-west-1" {
		t.Fatalf("pipeline region not expanded: %q", pl["region"])
	}
}

func TestConfigTransformHookMissingSchema(t *testing.T) {
	module.GetConfigRegistry().Reset()

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name: "app-config",
				Type: "config.provider",
				Config: map[string]any{
					"sources": []any{map[string]any{"type": "defaults"}},
				},
			},
		},
	}

	p := New()
	hooks := p.ConfigTransformHooks()
	err := hooks[0].Hook(cfg)
	if err == nil {
		t.Fatal("expected error for missing schema")
	}
}

func TestConfigTransformHookMissingSources(t *testing.T) {
	module.GetConfigRegistry().Reset()

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{
				Name: "app-config",
				Type: "config.provider",
				Config: map[string]any{
					"schema": map[string]any{
						"key": map[string]any{"default": "val"},
					},
				},
			},
		},
	}

	p := New()
	hooks := p.ConfigTransformHooks()
	err := hooks[0].Hook(cfg)
	if err == nil {
		t.Fatal("expected error for missing sources")
	}
}
