package plugin

import (
	"testing"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/schema"
)

func newTestEngineLoader() *PluginLoader {
	return NewPluginLoader(capability.NewRegistry(), schema.NewModuleSchemaRegistry())
}

// makeEnginePlugin builds a minimal EnginePlugin for testing.
func makeEnginePlugin(name, version string, deps []Dependency) *BaseEnginePlugin {
	return &BaseEnginePlugin{
		BaseNativePlugin: BaseNativePlugin{
			PluginName:        name,
			PluginVersion:     version,
			PluginDescription: "test plugin " + name,
		},
		Manifest: PluginManifest{
			Name:         name,
			Version:      version,
			Author:       "test",
			Description:  "test plugin " + name,
			Dependencies: deps,
		},
	}
}

func TestPluginLoader_LoadSinglePlugin(t *testing.T) {
	loader := newTestEngineLoader()

	p := &modulePlugin{
		BaseEnginePlugin: *makeEnginePlugin("my-plugin", "1.0.0", nil),
		modules: map[string]ModuleFactory{
			"my.module": func(name string, cfg map[string]any) modular.Module {
				return nil
			},
		},
		steps: map[string]StepFactory{
			"my.step": func(name string, cfg map[string]any, _ modular.Application) (any, error) {
				return nil, nil
			},
		},
		triggers: map[string]TriggerFactory{
			"my-trigger": func() any { return nil },
		},
		handlers: map[string]WorkflowHandlerFactory{
			"my-handler": func() any { return nil },
		},
	}

	if err := loader.LoadPlugin(p); err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	if got := len(loader.ModuleFactories()); got != 1 {
		t.Errorf("expected 1 module factory, got %d", got)
	}
	if _, ok := loader.ModuleFactories()["my.module"]; !ok {
		t.Error("expected my.module factory to be registered")
	}

	if got := len(loader.StepFactories()); got != 1 {
		t.Errorf("expected 1 step factory, got %d", got)
	}

	if got := len(loader.TriggerFactories()); got != 1 {
		t.Errorf("expected 1 trigger factory, got %d", got)
	}

	if got := len(loader.WorkflowHandlerFactories()); got != 1 {
		t.Errorf("expected 1 handler factory, got %d", got)
	}

	if got := len(loader.LoadedPlugins()); got != 1 {
		t.Errorf("expected 1 loaded plugin, got %d", got)
	}
}

func TestPluginLoader_LoadPluginsWithDependencies(t *testing.T) {
	loader := newTestEngineLoader()

	base := &modulePlugin{
		BaseEnginePlugin: *makeEnginePlugin("base-plugin", "1.0.0", nil),
		modules: map[string]ModuleFactory{
			"base.module": func(name string, cfg map[string]any) modular.Module {
				return nil
			},
		},
	}

	dependent := &modulePlugin{
		BaseEnginePlugin: *makeEnginePlugin("dep-plugin", "1.0.0", []Dependency{
			{Name: "base-plugin", Constraint: ">=1.0.0"},
		}),
		modules: map[string]ModuleFactory{
			"dep.module": func(name string, cfg map[string]any) modular.Module {
				return nil
			},
		},
	}

	// Load in reverse order — topo sort should fix it.
	if err := loader.LoadPlugins([]EnginePlugin{dependent, base}); err != nil {
		t.Fatalf("LoadPlugins failed: %v", err)
	}

	loaded := loader.LoadedPlugins()
	if len(loaded) != 2 {
		t.Fatalf("expected 2 loaded plugins, got %d", len(loaded))
	}
	if loaded[0].EngineManifest().Name != "base-plugin" {
		t.Errorf("expected base-plugin first, got %s", loaded[0].EngineManifest().Name)
	}
	if loaded[1].EngineManifest().Name != "dep-plugin" {
		t.Errorf("expected dep-plugin second, got %s", loaded[1].EngineManifest().Name)
	}
}

func TestPluginLoader_CircularDependency(t *testing.T) {
	loader := newTestEngineLoader()

	a := makeEnginePlugin("plugin-a", "1.0.0", []Dependency{
		{Name: "plugin-b", Constraint: ">=1.0.0"},
	})
	b := makeEnginePlugin("plugin-b", "1.0.0", []Dependency{
		{Name: "plugin-a", Constraint: ">=1.0.0"},
	})

	err := loader.LoadPlugins([]EnginePlugin{a, b})
	if err == nil {
		t.Fatal("expected circular dependency error")
	}
}

func TestPluginLoader_DuplicateModuleTypeConflict(t *testing.T) {
	loader := newTestEngineLoader()

	p1 := &modulePlugin{
		BaseEnginePlugin: *makeEnginePlugin("plugin-one", "1.0.0", nil),
		modules: map[string]ModuleFactory{
			"shared.type": func(name string, cfg map[string]any) modular.Module {
				return nil
			},
		},
	}
	p2 := &modulePlugin{
		BaseEnginePlugin: *makeEnginePlugin("plugin-two", "1.0.0", nil),
		modules: map[string]ModuleFactory{
			"shared.type": func(name string, cfg map[string]any) modular.Module {
				return nil
			},
		},
	}

	if err := loader.LoadPlugin(p1); err != nil {
		t.Fatalf("first load should succeed: %v", err)
	}
	if err := loader.LoadPlugin(p2); err == nil {
		t.Fatal("expected duplicate module type error")
	}
}

func TestPluginLoader_WiringHooksSortedByPriority(t *testing.T) {
	loader := newTestEngineLoader()

	p := &hookPlugin{
		BaseEnginePlugin: *makeEnginePlugin("hook-plugin", "1.0.0", nil),
		hooks: []WiringHook{
			{Name: "low", Priority: 10, Hook: func(_ modular.Application, _ *config.WorkflowConfig) error { return nil }},
			{Name: "high", Priority: 100, Hook: func(_ modular.Application, _ *config.WorkflowConfig) error { return nil }},
			{Name: "mid", Priority: 50, Hook: func(_ modular.Application, _ *config.WorkflowConfig) error { return nil }},
		},
	}

	if err := loader.LoadPlugin(p); err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	hooks := loader.WiringHooks()
	if len(hooks) != 3 {
		t.Fatalf("expected 3 hooks, got %d", len(hooks))
	}
	if hooks[0].Name != "high" {
		t.Errorf("expected high priority first, got %s", hooks[0].Name)
	}
	if hooks[1].Name != "mid" {
		t.Errorf("expected mid priority second, got %s", hooks[1].Name)
	}
	if hooks[2].Name != "low" {
		t.Errorf("expected low priority third, got %s", hooks[2].Name)
	}
}

func TestPluginLoader_EmptyBasePluginLoads(t *testing.T) {
	loader := newTestEngineLoader()

	p := makeEnginePlugin("empty-plugin", "1.0.0", nil)
	if err := loader.LoadPlugin(p); err != nil {
		t.Fatalf("LoadPlugin of empty plugin should succeed: %v", err)
	}
	if got := len(loader.ModuleFactories()); got != 0 {
		t.Errorf("expected 0 module factories, got %d", got)
	}
	if got := len(loader.LoadedPlugins()); got != 1 {
		t.Errorf("expected 1 loaded plugin, got %d", got)
	}
}

// -- helper plugin types for tests --

// modulePlugin embeds BaseEnginePlugin and overrides factory methods.
type modulePlugin struct {
	BaseEnginePlugin
	modules  map[string]ModuleFactory
	steps    map[string]StepFactory
	triggers map[string]TriggerFactory
	handlers map[string]WorkflowHandlerFactory
}

func (p *modulePlugin) ModuleFactories() map[string]ModuleFactory           { return p.modules }
func (p *modulePlugin) StepFactories() map[string]StepFactory               { return p.steps }
func (p *modulePlugin) TriggerFactories() map[string]TriggerFactory         { return p.triggers }
func (p *modulePlugin) WorkflowHandlers() map[string]WorkflowHandlerFactory { return p.handlers }

// hookPlugin embeds BaseEnginePlugin and overrides WiringHooks.
type hookPlugin struct {
	BaseEnginePlugin
	hooks []WiringHook
}

func (p *hookPlugin) WiringHooks() []WiringHook { return p.hooks }
