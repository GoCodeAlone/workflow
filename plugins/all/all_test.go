package all

import (
	"testing"

	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/plugin"
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
