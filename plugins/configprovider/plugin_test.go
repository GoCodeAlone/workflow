package configprovider

import (
	"context"
	"log/slog"
	"testing"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/module"
)

func stepCredentialConfig(token, endpoint string) *config.WorkflowConfig {
	return &config.WorkflowConfig{
		Modules: []config.ModuleConfig{{Name: "settings", Type: "config.provider", Config: map[string]any{
			"schema": map[string]any{
				"compute_token": map[string]any{"default": token, "env": "STEP_CREDENTIAL_TEST_TOKEN"},
				"server_url":    map[string]any{"default": endpoint, "env": "STEP_CREDENTIAL_TEST_URL"},
			},
			"sources":          []any{map[string]any{"type": "defaults"}},
			"step_credentials": []any{map[string]any{"plugin": "test-plugin", "step_type": "step.capture", "step_name": "capture", "field": "auth_token_ref", "ref": "config:compute_token", "scope": "application"}},
		}}},
		Pipelines: map[string]any{"main": map[string]any{"steps": []any{map[string]any{
			"name": "capture", "type": "step.capture", "config": map[string]any{"auth_token_ref": "config:compute_token", "url": `{{config "server_url"}}`},
		}}}},
	}
}

func TestConfigProviderModuleApplicationOwned(t *testing.T) {
	t.Cleanup(module.GetConfigRegistry().Reset)
	a, b := New(), New()
	ca, cb := stepCredentialConfig("dummy-a", "http://app-a"), stepCredentialConfig("dummy-b", "http://app-b")
	if err := a.ConfigTransformHooks()[0].Hook(ca); err != nil {
		t.Fatal(err)
	}
	ma := a.ModuleFactories()["config.provider"]("settings", ca.Modules[0].Config).(*module.ConfigProviderModule)
	appA := modular.NewStdApplication(modular.NewStdConfigProvider(nil), slog.Default())
	appA.RegisterModule(ma)
	if err := appA.Init(); err != nil {
		t.Fatal(err)
	}
	if err := b.ConfigTransformHooks()[0].Hook(cb); err != nil {
		t.Fatal(err)
	}
	mb := b.ModuleFactories()["config.provider"]("settings", cb.Modules[0].Config).(*module.ConfigProviderModule)
	appB := modular.NewStdApplication(modular.NewStdConfigProvider(nil), slog.Default())
	appB.RegisterModule(mb)
	if err := appB.Init(); err != nil {
		t.Fatal(err)
	}
	var appRegistry *module.ConfigRegistry
	if err := appA.GetService("config.registry", &appRegistry); err != nil || appRegistry != ma.Registry() {
		t.Fatal("app DI lost exact registry")
	}
	if got, ok := ma.Registry().Get("compute_token"); !ok || got != "dummy-a" {
		t.Fatal("application A registry was replaced by application B")
	}
	if got, ok := mb.Registry().Get("compute_token"); !ok || got != "dummy-b" {
		t.Fatal("application B registry did not retain its source")
	}
	for _, c := range []struct {
		cfg *config.WorkflowConfig
		url string
	}{{ca, "http://app-a"}, {cb, "http://app-b"}} {
		step := c.cfg.Pipelines["main"].(map[string]any)["steps"].([]any)[0].(map[string]any)["config"].(map[string]any)
		if step["url"] != c.url {
			t.Fatal("application transform expansion crossed sources")
		}
	}
}

func TestConfigTransformApplicationOwned(t *testing.T) {
	t.Cleanup(module.GetConfigRegistry().Reset)
	t.Setenv("STEP_CREDENTIAL_TEST_TOKEN", "dummy-before")
	t.Setenv("STEP_CREDENTIAL_TEST_URL", "http://before")
	p := New()
	cfg := stepCredentialConfig("", "")
	cfg.Modules[0].Config["sources"] = []any{map[string]any{"type": "env"}}
	if err := p.ConfigTransformHooks()[0].Hook(cfg); err != nil {
		t.Fatal(err)
	}
	t.Setenv("STEP_CREDENTIAL_TEST_TOKEN", "dummy-after")
	t.Setenv("STEP_CREDENTIAL_TEST_URL", "http://after")
	b := New()
	if err := b.ConfigTransformHooks()[0].Hook(stepCredentialConfig("dummy-b", "http://app-b")); err != nil {
		t.Fatal(err)
	}
	m := p.ModuleFactories()["config.provider"]("settings", cfg.Modules[0].Config).(*module.ConfigProviderModule)
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), slog.Default())
	app.RegisterModule(m)
	if err := app.Init(); err != nil {
		t.Fatal(err)
	}
	var binder interfaces.StepCredentialBinder
	if err := app.GetService(module.StepCredentialsService, &binder); err != nil {
		t.Fatal(err)
	}
	bound, err := binder.BindStep(interfaces.StepCredentialTarget{Plugin: "test-plugin", StepType: "step.capture", StepName: "capture"}, map[string]any{"auth_token_ref": "config:compute_token"})
	if err != nil || bound == nil {
		t.Fatal("missing app binding")
	}
	values, err := bound.Resolve(context.Background())
	if err != nil || len(values) != 1 || values[0].Value != "dummy-before" {
		t.Fatal("Init reloaded token source")
	}
	step := cfg.Pipelines["main"].(map[string]any)["steps"].([]any)[0].(map[string]any)["config"].(map[string]any)
	if step["url"] != "http://before" {
		t.Fatal("transform did not use declared environment")
	}
	if got, ok := m.Registry().Get("server_url"); !ok || got != step["url"] {
		t.Fatal("module endpoint differs from transform snapshot")
	}
	if got, ok := m.Registry().Get("compute_token"); !ok || got != "dummy-before" {
		t.Fatal("module token differs from transform snapshot")
	}
}

func TestConfigTransformStepCredentialPreflight(t *testing.T) {
	t.Cleanup(module.GetConfigRegistry().Reset)
	for _, tc := range []struct {
		name   string
		change func(*config.WorkflowConfig)
	}{
		{"template alias", func(c *config.WorkflowConfig) {
			c.Modules[0].Config["schema"].(map[string]any)["alias"] = map[string]any{"default": "config:compute_token"}
			c.Pipelines["main"].(map[string]any)["steps"].([]any)[0].(map[string]any)["config"].(map[string]any)["auth_token_ref"] = `{{config "alias"}}`
		}},
		{"ref mutation", func(c *config.WorkflowConfig) {
			c.Pipelines["main"].(map[string]any)["steps"].([]any)[0].(map[string]any)["config"].(map[string]any)["auth_token_ref"] = "config:other"
		}},
		{"missing field", func(c *config.WorkflowConfig) {
			delete(c.Pipelines["main"].(map[string]any)["steps"].([]any)[0].(map[string]any)["config"].(map[string]any), "auth_token_ref")
		}},
		{"ambiguous target", func(c *config.WorkflowConfig) { c.Pipelines["other"] = c.Pipelines["main"] }},
		{"missing target", func(c *config.WorkflowConfig) { c.Pipelines = nil }},
		{"multiple providers", func(c *config.WorkflowConfig) {
			c.Modules = append(c.Modules, config.ModuleConfig{Name: "other", Type: "config.provider", Config: map[string]any{}})
		}},
		{"unknown grant field", func(c *config.WorkflowConfig) {
			c.Modules[0].Config["step_credentials"].([]any)[0].(map[string]any)["sensitive"] = true
		}},
		{"duplicates", func(c *config.WorkflowConfig) {
			gs := c.Modules[0].Config["step_credentials"].([]any)
			c.Modules[0].Config["step_credentials"] = append(gs, gs[0])
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := stepCredentialConfig("dummy", "http://app")
			tc.change(cfg)
			if err := New().ConfigTransformHooks()[0].Hook(cfg); err == nil {
				t.Fatal("invalid granted build accepted")
			}
		})
	}
}

func TestConfigTransformStepCredentialReuse(t *testing.T) {
	t.Cleanup(module.GetConfigRegistry().Reset)
	t.Run("normal reuse and empty grants", func(t *testing.T) {
		p := New()
		for i := 0; i < 2; i++ {
			cfg := stepCredentialConfig("dummy", "http://normal")
			cfg.Modules[0].Config["step_credentials"] = []any{}
			if err := p.ConfigTransformHooks()[0].Hook(cfg); err != nil {
				t.Fatal(err)
			}
			m := p.ModuleFactories()["config.provider"]("settings", cfg.Modules[0].Config).(*module.ConfigProviderModule)
			if m.Registry() != module.GetConfigRegistry() {
				t.Fatal("normal source behavior changed")
			}
			app := module.CreateIsolatedApp(t)
			if err := m.Init(app); err != nil {
				t.Fatal(err)
			}
			if _, exists := app.GetServiceEntry(module.StepCredentialsService); exists {
				t.Fatal("empty grants authorized private service")
			}
		}
	})
	t.Run("granted reuse fails before source reload", func(t *testing.T) {
		p := New()
		cfg := stepCredentialConfig("dummy-a", "http://a")
		if err := p.ConfigTransformHooks()[0].Hook(cfg); err != nil {
			t.Fatal(err)
		}
		if err := p.ConfigTransformHooks()[0].Hook(stepCredentialConfig("dummy-b", "http://b")); err == nil {
			t.Fatal("granted plugin reused")
		}
		if v, _ := module.GetConfigRegistry().Get("compute_token"); v != "dummy-a" {
			t.Fatal("reuse reloaded global")
		}
		m := p.ModuleFactories()["config.provider"]("settings", cfg.Modules[0].Config)
		if err := m.Init(module.CreateIsolatedApp(t)); err != nil {
			t.Fatal(err)
		}
		duplicate := p.ModuleFactories()["config.provider"]("settings", cfg.Modules[0].Config)
		if err := duplicate.Init(module.CreateIsolatedApp(t)); err == nil {
			t.Fatal("granted module constructed twice")
		}
	})
}

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
