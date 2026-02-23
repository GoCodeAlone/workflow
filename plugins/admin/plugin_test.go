package admin

import (
	"testing"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/plugin"
)

func TestPluginImplementsEnginePlugin(t *testing.T) {
	p := New()
	var _ plugin.EnginePlugin = p
}

func TestPluginManifest(t *testing.T) {
	p := New()
	m := p.EngineManifest()

	if err := m.Validate(); err != nil {
		t.Fatalf("manifest validation failed: %v", err)
	}
	if m.Name != "admin" {
		t.Errorf("expected name %q, got %q", "admin", m.Name)
	}
	if len(m.ModuleTypes) != 2 {
		t.Errorf("expected 2 module types, got %d", len(m.ModuleTypes))
	}
}

func TestPluginCapabilities(t *testing.T) {
	p := New()
	caps := p.Capabilities()
	if len(caps) != 2 {
		t.Fatalf("expected 2 capabilities, got %d", len(caps))
	}
	names := map[string]bool{}
	for _, c := range caps {
		names[c.Name] = true
	}
	for _, expected := range []string{"admin-ui", "admin-config"} {
		if !names[expected] {
			t.Errorf("missing capability %q", expected)
		}
	}
}

func TestModuleFactories(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()

	expectedTypes := []string{"admin.dashboard", "admin.config_loader"}
	for _, typ := range expectedTypes {
		factory, ok := factories[typ]
		if !ok {
			t.Errorf("missing factory for %q", typ)
			continue
		}
		mod := factory("test-"+typ, map[string]any{})
		if mod == nil {
			t.Errorf("factory for %q returned nil", typ)
		}
	}
}

func TestDashboardFactoryWithRoot(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()

	mod := factories["admin.dashboard"]("dash", map[string]any{
		"root": "/tmp/admin-ui",
	})
	if mod == nil {
		t.Fatal("admin.dashboard factory returned nil")
	}
	if mod.Name() != "dash" {
		t.Errorf("expected name %q, got %q", "dash", mod.Name())
	}
}

func TestDashboardFactoryWithUIDir(t *testing.T) {
	p := New().WithUIDir("/custom/ui")
	factories := p.ModuleFactories()

	mod := factories["admin.dashboard"]("dash-override", map[string]any{
		"root": "/should-be-overridden",
	})
	if mod == nil {
		t.Fatal("admin.dashboard factory returned nil with UIDir override")
	}
}

func TestConfigLoaderFactory(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()

	mod := factories["admin.config_loader"]("loader", map[string]any{})
	if mod == nil {
		t.Fatal("admin.config_loader factory returned nil")
	}
	if mod.Name() != "loader" {
		t.Errorf("expected name %q, got %q", "loader", mod.Name())
	}
}

func TestModuleSchemas(t *testing.T) {
	p := New()
	schemas := p.ModuleSchemas()
	if len(schemas) != 2 {
		t.Fatalf("expected 2 module schemas, got %d", len(schemas))
	}

	types := map[string]bool{}
	for _, s := range schemas {
		types[s.Type] = true
	}
	for _, expected := range []string{"admin.dashboard", "admin.config_loader"} {
		if !types[expected] {
			t.Errorf("missing schema for %q", expected)
		}
	}
}

func TestWiringHooks(t *testing.T) {
	p := New()
	hooks := p.WiringHooks()
	if len(hooks) != 1 {
		t.Fatalf("expected 1 wiring hook, got %d", len(hooks))
	}
	if hooks[0].Name != "admin-config-merge" {
		t.Errorf("expected hook name %q, got %q", "admin-config-merge", hooks[0].Name)
	}
}

func TestWiringHookMergesAdminConfig(t *testing.T) {
	p := New()
	hooks := p.WiringHooks()

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "my-server", Type: "http.server"},
		},
	}

	err := hooks[0].Hook(nil, cfg)
	if err != nil {
		t.Fatalf("wiring hook failed: %v", err)
	}

	// After merge, admin modules should be present
	found := false
	for _, m := range cfg.Modules {
		if m.Name == "admin-server" {
			found = true
			break
		}
	}
	if !found {
		t.Error("admin-server module not found after wiring hook merge")
	}
}

func TestWiringHookSkipsIfAlreadyPresent(t *testing.T) {
	p := New()
	hooks := p.WiringHooks()

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "admin-server", Type: "http.server", Config: map[string]any{"address": ":8081"}},
		},
	}

	originalLen := len(cfg.Modules)
	err := hooks[0].Hook(nil, cfg)
	if err != nil {
		t.Fatalf("wiring hook failed: %v", err)
	}

	// Should not duplicate admin modules
	if len(cfg.Modules) != originalLen {
		t.Errorf("expected %d modules (no duplicates), got %d", originalLen, len(cfg.Modules))
	}
}

func TestWiringHookInjectsUIDir(t *testing.T) {
	p := New().WithUIDir("/custom/admin-ui")
	hooks := p.WiringHooks()

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "admin-server", Type: "http.server"},
			{Name: "admin-ui", Type: "static.fileserver", Config: map[string]any{"root": "ui/dist"}},
		},
	}

	err := hooks[0].Hook(nil, cfg)
	if err != nil {
		t.Fatalf("wiring hook failed: %v", err)
	}

	// The static.fileserver should have the overridden root
	for _, m := range cfg.Modules {
		if m.Type == "static.fileserver" {
			if m.Config["root"] != "/custom/admin-ui" {
				t.Errorf("expected root %q, got %q", "/custom/admin-ui", m.Config["root"])
			}
		}
	}
}

func TestWiringHookInjectsUIDirForAdminDashboard(t *testing.T) {
	p := New().WithUIDir("/custom/admin-ui")
	hooks := p.WiringHooks()

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "admin-server", Type: "http.server"},
			{Name: "admin-dashboard", Type: "admin.dashboard", Config: map[string]any{"root": "ui/dist"}},
		},
	}

	err := hooks[0].Hook(nil, cfg)
	if err != nil {
		t.Fatalf("wiring hook failed: %v", err)
	}

	// The admin.dashboard should have the overridden root
	for _, m := range cfg.Modules {
		if m.Type == "admin.dashboard" {
			if m.Config["root"] != "/custom/admin-ui" {
				t.Errorf("expected root %q, got %q", "/custom/admin-ui", m.Config["root"])
			}
		}
	}
}

func TestWithLoggerChaining(t *testing.T) {
	p := New()
	p2 := p.WithUIDir("/dir").WithLogger(nil)
	if p2 != p {
		t.Error("expected chained With* calls to return the same *Plugin")
	}
}

func TestPluginNameAndVersion(t *testing.T) {
	p := New()
	if p.Name() != "admin" {
		t.Errorf("expected name %q, got %q", "admin", p.Name())
	}
	if p.Version() != "1.0.0" {
		t.Errorf("expected version %q, got %q", "1.0.0", p.Version())
	}
}

func TestConfigLoaderModuleInterface(t *testing.T) {
	m := newConfigLoaderModule("test-loader")
	if m.Name() != "test-loader" {
		t.Errorf("expected name %q, got %q", "test-loader", m.Name())
	}
	if deps := m.Dependencies(); deps != nil {
		t.Errorf("expected nil dependencies, got %v", deps)
	}
	if err := m.RegisterConfig(nil); err != nil {
		t.Errorf("RegisterConfig returned error: %v", err)
	}
	if err := m.Init(nil); err != nil {
		t.Errorf("Init returned error: %v", err)
	}
	if err := m.Start(nil); err != nil {
		t.Errorf("Start returned error: %v", err)
	}
	if err := m.Stop(nil); err != nil {
		t.Errorf("Stop returned error: %v", err)
	}
}
