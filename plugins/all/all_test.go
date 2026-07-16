package all

import (
	"strings"
	"testing"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/plugin/builder"
	plugincicd "github.com/GoCodeAlone/workflow/plugins/cicd"
	plugindatastores "github.com/GoCodeAlone/workflow/plugins/datastores"
	"github.com/GoCodeAlone/workflow/schema"
)

func TestDefaultPlugins_NotEmpty(t *testing.T) {
	plugins := DefaultPlugins()
	if len(plugins) == 0 {
		t.Fatal("DefaultPlugins() returned empty slice")
	}
}

func TestDefaultPlugins_AllNonNil(t *testing.T) {
	for i, p := range DefaultPlugins() {
		if p == nil {
			t.Errorf("DefaultPlugins()[%d] is nil", i)
		}
	}
}

func TestDefaultPlugins_UniqueNames(t *testing.T) {
	seen := make(map[string]bool)
	for _, p := range DefaultPlugins() {
		name := p.Name()
		if seen[name] {
			t.Errorf("duplicate plugin name %q in DefaultPlugins()", name)
		}
		seen[name] = true
	}
}

// TestRemovedBuiltins_NotInDefaultPlugins asserts that the superseded built-in
// plugins (gitlab, scanner) have been removed from DefaultPlugins and must now
// be installed as external plugins. This is the TDD red-before-green guard
// for the builtin->external migration.
func TestRemovedBuiltins_NotInDefaultPlugins(t *testing.T) {
	removed := map[string]bool{
		"gitlab":  true,
		"scanner": true,
	}
	for _, p := range DefaultPlugins() {
		name := p.Name()
		if removed[name] {
			t.Errorf("DefaultPlugins() still returns removed built-in plugin %q — it must be installed as an external plugin now", name)
		}
	}
}

func TestDefaultPlugins_IndependentSlices(t *testing.T) {
	a := DefaultPlugins()
	b := DefaultPlugins()
	if len(a) != len(b) {
		t.Fatalf("successive calls returned different lengths: %d vs %d", len(a), len(b))
	}
	// Modifying one slice must not affect the other.
	a[0] = nil
	if b[0] == nil {
		t.Error("modifying slice returned by DefaultPlugins() affected a separate call")
	}
}

// stubEngine is a minimal PluginLoader used in tests to avoid importing the
// workflow package (which would create a circular dependency).
type stubEngine struct {
	loaded []string
	errOn  string // if non-empty, return an error when this plugin name is loaded
}

func (s *stubEngine) LoadPlugin(p plugin.EnginePlugin) error {
	if s.errOn != "" && p.Name() == s.errOn {
		return &testError{p.Name()}
	}
	s.loaded = append(s.loaded, p.Name())
	return nil
}

type testError struct{ name string }

func (e *testError) Error() string { return "test error for " + e.name }

func TestLoadAll_LoadsAllPlugins(t *testing.T) {
	eng := &stubEngine{}
	if err := LoadAll(eng); err != nil {
		t.Fatalf("LoadAll() unexpected error: %v", err)
	}

	want := len(DefaultPlugins())
	if len(eng.loaded) != want {
		t.Errorf("LoadAll() loaded %d plugins, want %d", len(eng.loaded), want)
	}
}

func TestLoadAll_ReturnsFirstError(t *testing.T) {
	plugins := DefaultPlugins()
	if len(plugins) == 0 {
		t.Skip("no plugins")
	}
	// Trigger an error on the second plugin to verify early return.
	targetName := plugins[1].Name()
	eng := &stubEngine{errOn: targetName}

	err := LoadAll(eng)
	if err == nil {
		t.Fatal("LoadAll() expected error, got nil")
	}

	// Only the first plugin should have been loaded (the second triggered the error).
	if len(eng.loaded) != 1 {
		t.Errorf("LoadAll() loaded %d plugins before error, want 1", len(eng.loaded))
	}
}

// TestLoadAll_WithRealLoader verifies that all default plugins can be loaded
// into a real PluginLoader without conflicts.
func TestLoadAll_WithRealLoader(t *testing.T) {
	capReg := capability.NewRegistry()
	schemaReg := schema.NewModuleSchemaRegistry()
	loader := plugin.NewPluginLoader(capReg, schemaReg)

	for _, p := range DefaultPlugins() {
		if err := loader.LoadPlugin(p); err != nil {
			t.Fatalf("LoadPlugin(%q) error: %v", p.Name(), err)
		}
	}

	// All plugins were loaded — there should be module, step, and trigger factories.
	if len(loader.ModuleFactories()) == 0 {
		t.Error("no module factories registered after loading all plugins")
	}
	if len(loader.StepFactories()) == 0 {
		t.Error("no step factories registered after loading all plugins")
	}
	if len(loader.TriggerFactories()) == 0 {
		t.Error("no trigger factories registered after loading all plugins")
	}
}

func TestPluginLoader_ProviderBuiltinCoexistence(t *testing.T) {
	loader := plugin.NewPluginLoader(capability.NewRegistry(), schema.NewModuleSchemaRegistry())
	for _, builtin := range []plugin.EnginePlugin{plugincicd.New(), plugindatastores.New()} {
		if err := loader.LoadPlugin(builtin); err != nil {
			t.Fatalf("load builtin %q: %v", builtin.Name(), err)
		}
	}
	loaded := loader.LoadedPlugins()
	if len(loaded) != 2 || loaded[0].Name() != "cicd" || loaded[1].Name() != "datastores" {
		t.Fatalf("builtins did not load first in order: %v", pluginNames(loaded))
	}

	wantOverridable := map[string]bool{
		"aws.codebuild":                 true,
		"step.codebuild_create_project": true,
		"step.codebuild_start":          true,
		"step.codebuild_status":         true,
		"step.codebuild_logs":           true,
		"step.codebuild_delete_project": true,
		"step.codebuild_list_builds":    true,
		"nosql.dynamodb":                true,
	}
	gotOverridable := loader.OverridableTypes()
	if len(gotOverridable) != len(wantOverridable) {
		t.Fatalf("overridable types = %v, want exactly %v", gotOverridable, wantOverridable)
	}
	for typeName := range wantOverridable {
		if !gotOverridable[typeName] {
			t.Errorf("type %q is not overridable", typeName)
		}
	}

	externalCICD := providerReplacementPlugin("external-cicd", []string{"aws.codebuild"}, []string{
		"step.codebuild_create_project",
		"step.codebuild_start",
		"step.codebuild_status",
		"step.codebuild_logs",
		"step.codebuild_delete_project",
		"step.codebuild_list_builds",
	})
	if err := loader.LoadPlugin(externalCICD); err != nil {
		t.Fatalf("replace CodeBuild builtin types: %v", err)
	}
	externalDatastore := providerReplacementPlugin("external-aws", []string{"nosql.dynamodb"}, nil)
	if err := loader.LoadPlugin(externalDatastore); err != nil {
		t.Fatalf("replace DynamoDB builtin type: %v", err)
	}

	if got := loader.ModuleFactories()["aws.codebuild"]("test", nil); got != nil {
		t.Fatalf("aws.codebuild factory was not replaced: got %T", got)
	}
	if got := loader.ModuleFactories()["nosql.dynamodb"]("test", nil); got != nil {
		t.Fatalf("nosql.dynamodb factory was not replaced: got %T", got)
	}
	for typeName := range externalCICD.StepFactories() {
		got, err := loader.StepFactories()[typeName]("test", nil, nil)
		if err != nil {
			t.Fatalf("invoke replacement %q: %v", typeName, err)
		}
		if got != "external-cicd" {
			t.Errorf("step %q was not replaced: got %v", typeName, got)
		}
	}

	unrelated := providerReplacementPlugin("unrelated-cicd", nil, []string{"step.shell_exec"})
	err := loader.LoadPlugin(unrelated)
	if err == nil || !strings.Contains(err.Error(), `step type "step.shell_exec" already registered`) {
		t.Fatalf("unrelated duplicate error = %v", err)
	}
}

func providerReplacementPlugin(name string, moduleTypes, stepTypes []string) *providerReplacement {
	p := &providerReplacement{
		BaseEnginePlugin: plugin.BaseEnginePlugin{
			BaseNativePlugin: plugin.BaseNativePlugin{PluginName: name, PluginVersion: "1.0.0", PluginDescription: "provider replacement fixture"},
			Manifest:         plugin.PluginManifest{Name: name, Version: "1.0.0", Author: "test", Description: "provider replacement fixture"},
		},
		modules: make(map[string]plugin.ModuleFactory, len(moduleTypes)),
		steps:   make(map[string]plugin.StepFactory, len(stepTypes)),
	}
	for _, typeName := range moduleTypes {
		p.modules[typeName] = func(string, map[string]any) modular.Module { return nil }
	}
	for _, typeName := range stepTypes {
		p.steps[typeName] = func(string, map[string]any, modular.Application) (any, error) { return name, nil }
	}
	return p
}

type providerReplacement struct {
	plugin.BaseEnginePlugin
	modules map[string]plugin.ModuleFactory
	steps   map[string]plugin.StepFactory
}

func (p *providerReplacement) ModuleFactories() map[string]plugin.ModuleFactory { return p.modules }
func (p *providerReplacement) StepFactories() map[string]plugin.StepFactory     { return p.steps }

func pluginNames(plugins []plugin.EnginePlugin) []string {
	names := make([]string, 0, len(plugins))
	for _, p := range plugins {
		names = append(names, p.Name())
	}
	return names
}

func TestAllBuilderPluginsRegistered(t *testing.T) {
	// Importing plugins/all triggers init() in each builder plugin,
	// which registers them in the builder registry.
	for _, name := range []string{"go", "nodejs", "custom"} {
		if _, ok := builder.Get(name); !ok {
			t.Errorf("builder %q not registered after importing plugins/all", name)
		}
	}
}
