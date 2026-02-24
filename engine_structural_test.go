package workflow

import (
	"strings"
	"testing"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/mock"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/schema"
)

// --- Negative tests: core engine rejects unknown module types ---

// findPluginByName returns the plugin with the given name from allPlugins,
// or nil if no such plugin exists.
func findPluginByName(name string) plugin.EnginePlugin {
	for _, p := range allPlugins() {
		if p.Name() == name {
			return p
		}
	}
	return nil
}

// TestCoreRejectsUnknownModuleType verifies that the engine returns a clear
// error when a config references a module type that no plugin provides.
func TestCoreRejectsUnknownModuleType(t *testing.T) {
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), &mock.Logger{})
	engine := NewStdEngine(app, &mock.Logger{})
	// Deliberately do NOT load any plugins.

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "test-server", Type: "http.server", Config: map[string]any{"address": ":8080"}},
		},
		Workflows: map[string]any{},
		Triggers:  map[string]any{},
	}

	err := engine.BuildFromConfig(cfg)
	if err == nil {
		t.Fatal("expected error when using http.server without any plugins loaded, got nil")
	}
	if !strings.Contains(err.Error(), "unknown module type") {
		t.Fatalf("expected 'unknown module type' in error, got: %v", err)
	}
}

// TestCoreRejectsMultipleUnknownTypes verifies that the first unknown module
// type produces a clear error, not a panic or nil dereference.
func TestCoreRejectsMultipleUnknownTypes(t *testing.T) {
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), &mock.Logger{})
	engine := NewStdEngine(app, &mock.Logger{})

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "a", Type: "nonexistent.module.type", Config: map[string]any{}},
			{Name: "b", Type: "another.fake.type", Config: map[string]any{}},
		},
		Workflows: map[string]any{},
		Triggers:  map[string]any{},
	}

	err := engine.BuildFromConfig(cfg)
	if err == nil {
		t.Fatal("expected error for unknown module types, got nil")
	}
	if !strings.Contains(err.Error(), "nonexistent.module.type") {
		t.Fatalf("expected error to reference the unknown type, got: %v", err)
	}
}

// --- Capability / requires validation tests ---

// testCapabilityPlugin is a minimal plugin that registers a single capability.
type testCapabilityPlugin struct {
	plugin.BaseEnginePlugin
	caps []capability.Contract
}

func (p *testCapabilityPlugin) Capabilities() []capability.Contract { return p.caps }

func newTestCapPlugin(name string, caps []capability.Contract) *testCapabilityPlugin {
	// Build manifest capability declarations from contracts.
	var decls []plugin.CapabilityDecl
	for _, c := range caps {
		decls = append(decls, plugin.CapabilityDecl{
			Name: c.Name, Role: "provider", Priority: 10,
		})
	}
	return &testCapabilityPlugin{
		BaseEnginePlugin: plugin.BaseEnginePlugin{
			BaseNativePlugin: plugin.BaseNativePlugin{
				PluginName:        name,
				PluginVersion:     "1.0.0",
				PluginDescription: "test plugin",
			},
			Manifest: plugin.PluginManifest{
				Name:         name,
				Version:      "1.0.0",
				Author:       "test",
				Description:  "test plugin",
				Capabilities: decls,
			},
		},
		caps: caps,
	}
}

// TestRequiresValidation_MissingCapability verifies that BuildFromConfig fails
// when the config declares a required capability that no loaded plugin provides.
func TestRequiresValidation_MissingCapability(t *testing.T) {
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), &mock.Logger{})
	engine := NewStdEngine(app, &mock.Logger{})

	// Load a plugin that provides "http-server" but NOT "message-broker".
	p := newTestCapPlugin("test-http", []capability.Contract{
		{Name: "http-server", Description: "test capability"},
	})
	if err := engine.LoadPlugin(p); err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	cfg := &config.WorkflowConfig{
		Modules:   []config.ModuleConfig{},
		Workflows: map[string]any{},
		Triggers:  map[string]any{},
		Requires: &config.RequiresConfig{
			Capabilities: []string{"http-server", "message-broker"},
		},
	}

	err := engine.BuildFromConfig(cfg)
	if err == nil {
		t.Fatal("expected error for missing 'message-broker' capability, got nil")
	}
	if !strings.Contains(err.Error(), "message-broker") {
		t.Fatalf("expected error to reference missing capability, got: %v", err)
	}
}

// TestRequiresValidation_SatisfiedCapabilities verifies that BuildFromConfig
// succeeds when all required capabilities are provided.
func TestRequiresValidation_SatisfiedCapabilities(t *testing.T) {
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), &mock.Logger{})
	engine := NewStdEngine(app, &mock.Logger{})

	p := newTestCapPlugin("test-all", []capability.Contract{
		{Name: "http-server", Description: "test"},
		{Name: "message-broker", Description: "test"},
	})
	if err := engine.LoadPlugin(p); err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	cfg := &config.WorkflowConfig{
		Modules:   []config.ModuleConfig{},
		Workflows: map[string]any{},
		Triggers:  map[string]any{},
		Requires: &config.RequiresConfig{
			Capabilities: []string{"http-server", "message-broker"},
		},
	}

	err := engine.BuildFromConfig(cfg)
	if err != nil {
		t.Fatalf("expected no error when all capabilities are satisfied, got: %v", err)
	}
}

// TestRequiresValidation_MissingPlugin verifies that the engine rejects configs
// requiring a plugin that is not loaded.
func TestRequiresValidation_MissingPlugin(t *testing.T) {
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), &mock.Logger{})
	engine := NewStdEngine(app, &mock.Logger{})

	// Load a plugin with a known name.
	p := newTestCapPlugin("test-http", nil)
	if err := engine.LoadPlugin(p); err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	cfg := &config.WorkflowConfig{
		Modules:   []config.ModuleConfig{},
		Workflows: map[string]any{},
		Triggers:  map[string]any{},
		Requires: &config.RequiresConfig{
			Plugins: []config.PluginRequirement{
				{Name: "nonexistent-plugin"},
			},
		},
	}

	err := engine.BuildFromConfig(cfg)
	if err == nil {
		t.Fatal("expected error for missing plugin, got nil")
	}
	if !strings.Contains(err.Error(), "nonexistent-plugin") {
		t.Fatalf("expected error to reference missing plugin, got: %v", err)
	}
}

// --- Selective plugin loading tests ---

// TestSelectivePluginLoading_HTTPOnly verifies that loading only the HTTP
// plugin makes HTTP module types available while other types remain unknown.
func TestSelectivePluginLoading_HTTPOnly(t *testing.T) {
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), &mock.Logger{})
	engine := NewStdEngine(app, &mock.Logger{})

	// Load only the HTTP plugin.
	httpPlugin := findPluginByName("workflow-plugin-http")
	if httpPlugin == nil {
		t.Fatalf("HTTP plugin not found in allPlugins")
	}
	if err := engine.LoadPlugin(httpPlugin); err != nil {
		t.Fatalf("LoadPlugin(http) failed: %v", err)
	}

	// HTTP module should work.
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "server", Type: "http.server", Config: map[string]any{"address": ":0"}},
		},
		Workflows: map[string]any{},
		Triggers:  map[string]any{},
	}
	if err := engine.BuildFromConfig(cfg); err != nil {
		t.Fatalf("expected http.server to work with HTTP plugin loaded, got: %v", err)
	}

	// Messaging module should fail (plugin not loaded).
	app2 := modular.NewStdApplication(modular.NewStdConfigProvider(nil), &mock.Logger{})
	engine2 := NewStdEngine(app2, &mock.Logger{})
	if err := engine2.LoadPlugin(httpPlugin); err != nil {
		t.Fatalf("LoadPlugin(http) failed: %v", err)
	}
	cfgMsg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "broker", Type: "messaging.broker", Config: map[string]any{}},
		},
		Workflows: map[string]any{},
		Triggers:  map[string]any{},
	}
	err := engine2.BuildFromConfig(cfgMsg)
	if err == nil {
		t.Fatal("expected error for messaging.broker without messaging plugin")
	}
	if !strings.Contains(err.Error(), "unknown module type") {
		t.Fatalf("expected 'unknown module type' error, got: %v", err)
	}
}

// TestAllPluginsProvideFactories verifies that every plugin in allPlugins()
// has a non-empty Name and Version.
func TestAllPluginsProvideFactories(t *testing.T) {
	for _, p := range allPlugins() {
		if p.Name() == "" {
			t.Error("plugin with empty Name()")
		}
		if p.Version() == "" {
			t.Errorf("plugin %q has empty Version()", p.Name())
		}
		manifest := p.EngineManifest()
		if manifest == nil {
			t.Errorf("plugin %q returned nil EngineManifest()", p.Name())
		}
	}
}

// TestEngineFactoryMapPopulatedByPlugins verifies that loading all plugins
// populates the module factory map with all expected module types.
func TestEngineFactoryMapPopulatedByPlugins(t *testing.T) {
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), &mock.Logger{})
	engine := NewStdEngine(app, &mock.Logger{})
	loadAllPlugins(t, engine)

	// Spot-check a few well-known module types from different plugins.
	expectedTypes := []string{
		"http.server",
		"http.router",
		"messaging.broker",
		"statemachine.engine",
		"metrics.collector",
		"auth.jwt",
	}

	// Build a set of known module types from the public schema API.
	known := schema.KnownModuleTypes()
	knownSet := make(map[string]bool, len(known))
	for _, k := range known {
		knownSet[k] = true
	}

	for _, mt := range expectedTypes {
		if !knownSet[mt] {
			t.Errorf("module type %q not found in schema.KnownModuleTypes() after loading all plugins", mt)
		}
	}
}

func getEngineModuleTypes(engine *StdEngine) []string {
	types := make([]string, 0, len(engine.moduleFactories))
	for mt := range engine.moduleFactories {
		types = append(types, mt)
	}
	return types
}

// TestSchemaKnowsPluginModuleTypes verifies that schema.RegisterModuleType is
// called for each plugin's module types during LoadPlugin.
func TestSchemaKnowsPluginModuleTypes(t *testing.T) {
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), &mock.Logger{})
	engine := NewStdEngine(app, &mock.Logger{})
	loadAllPlugins(t, engine)

	known := schema.KnownModuleTypes()
	knownSet := make(map[string]bool, len(known))
	for _, k := range known {
		knownSet[k] = true
	}

	// Every module type in the factory map should be in the schema.
	for _, mt := range getEngineModuleTypes(engine) {
		if !knownSet[mt] {
			t.Errorf("module type %q is in factory map but not in schema.KnownModuleTypes()", mt)
		}
	}
}
