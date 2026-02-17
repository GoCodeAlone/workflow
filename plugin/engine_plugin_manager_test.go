package plugin

import (
	"testing"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/schema"
)

func newTestEngineManager() *EnginePluginManager {
	return NewEnginePluginManager(capability.NewRegistry(), schema.NewModuleSchemaRegistry())
}

func TestEnginePluginManager_RegisterAndEnable(t *testing.T) {
	mgr := newTestEngineManager()

	p := &modulePlugin{
		BaseEnginePlugin: *makeEnginePlugin("test-plugin", "1.0.0", nil),
		modules: map[string]ModuleFactory{
			"test.module": func(name string, cfg map[string]any) modular.Module {
				return nil
			},
		},
	}

	if err := mgr.Register(p); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	if mgr.IsEnabled("test-plugin") {
		t.Error("plugin should not be enabled before Enable()")
	}

	if err := mgr.Enable("test-plugin"); err != nil {
		t.Fatalf("Enable failed: %v", err)
	}

	if !mgr.IsEnabled("test-plugin") {
		t.Error("plugin should be enabled after Enable()")
	}

	if got := len(mgr.Loader().ModuleFactories()); got != 1 {
		t.Errorf("expected 1 module factory after enable, got %d", got)
	}
}

func TestEnginePluginManager_Disable(t *testing.T) {
	mgr := newTestEngineManager()

	p := &modulePlugin{
		BaseEnginePlugin: *makeEnginePlugin("test-plugin", "1.0.0", nil),
		modules: map[string]ModuleFactory{
			"test.module": func(name string, cfg map[string]any) modular.Module {
				return nil
			},
		},
	}

	if err := mgr.Register(p); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := mgr.Enable("test-plugin"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if err := mgr.Disable("test-plugin"); err != nil {
		t.Fatalf("Disable: %v", err)
	}

	if mgr.IsEnabled("test-plugin") {
		t.Error("plugin should not be enabled after Disable()")
	}
	if got := len(mgr.Loader().ModuleFactories()); got != 0 {
		t.Errorf("expected 0 module factories after disable, got %d", got)
	}
}

func TestEnginePluginManager_DuplicateRegister(t *testing.T) {
	mgr := newTestEngineManager()

	p := makeEnginePlugin("dup-plugin", "1.0.0", nil)
	if err := mgr.Register(p); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if err := mgr.Register(p); err == nil {
		t.Fatal("expected error on duplicate register")
	}
}

func TestEnginePluginManager_EnableUnregistered(t *testing.T) {
	mgr := newTestEngineManager()
	if err := mgr.Enable("nonexistent"); err == nil {
		t.Fatal("expected error enabling unregistered plugin")
	}
}

func TestEnginePluginManager_DisableNotEnabled(t *testing.T) {
	mgr := newTestEngineManager()

	p := makeEnginePlugin("inactive-plugin", "1.0.0", nil)
	if err := mgr.Register(p); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := mgr.Disable("inactive-plugin"); err == nil {
		t.Fatal("expected error disabling plugin that is not enabled")
	}
}

func TestEnginePluginManager_Get(t *testing.T) {
	mgr := newTestEngineManager()

	p := makeEnginePlugin("get-plugin", "1.0.0", nil)
	_ = mgr.Register(p)

	got, ok := mgr.Get("get-plugin")
	if !ok {
		t.Fatal("expected to find registered plugin")
	}
	if got.EngineManifest().Name != "get-plugin" {
		t.Errorf("expected name get-plugin, got %s", got.EngineManifest().Name)
	}

	_, ok = mgr.Get("missing")
	if ok {
		t.Error("expected not found for missing plugin")
	}
}

func TestEnginePluginManager_List(t *testing.T) {
	mgr := newTestEngineManager()

	_ = mgr.Register(makeEnginePlugin("alpha", "1.0.0", nil))
	_ = mgr.Register(makeEnginePlugin("beta", "1.0.0", nil))

	list := mgr.List()
	if len(list) != 2 {
		t.Errorf("expected 2 plugins, got %d", len(list))
	}
}

func TestEnginePluginManager_EnableAlreadyEnabled(t *testing.T) {
	mgr := newTestEngineManager()

	p := makeEnginePlugin("double-enable", "1.0.0", nil)
	_ = mgr.Register(p)
	_ = mgr.Enable("double-enable")

	if err := mgr.Enable("double-enable"); err == nil {
		t.Fatal("expected error on double enable")
	}
}
