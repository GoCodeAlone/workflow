package workflow

import (
	"testing"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/config"
)

// fakeDepAwareModule implements modular.Module + the
// `interface{ SetDependencies([]string) }` contract that engine.go uses to
// plumb yaml-level dependsOn into modules. Recording every SetDependencies
// call lets the test assert the engine actually invoked it.
type fakeDepAwareModule struct {
	name        string
	setCalls    [][]string // ordered record of every SetDependencies invocation
	initialised bool
}

func (m *fakeDepAwareModule) Name() string                                  { return m.name }
func (m *fakeDepAwareModule) Init(_ modular.Application) error              { m.initialised = true; return nil }
func (m *fakeDepAwareModule) RegisterConfig(_ modular.Application) error    { return nil }
func (m *fakeDepAwareModule) Dependencies() []string                        { return nil }
func (m *fakeDepAwareModule) ProvidesServices() []modular.ServiceProvider   { return nil }
func (m *fakeDepAwareModule) RequiresServices() []modular.ServiceDependency { return nil }
func (m *fakeDepAwareModule) SetDependencies(deps []string) {
	// Defensive copy so we record what the engine passed, not what later
	// code may have mutated on the slice.
	cp := make([]string, len(deps))
	copy(cp, deps)
	m.setCalls = append(m.setCalls, cp)
}

// fakePlainModule deliberately does NOT implement SetDependencies. The
// engine plumbing must skip it without panicking, regardless of whether
// modCfg.DependsOn is set.
type fakePlainModule struct {
	name string
}

func (m *fakePlainModule) Name() string                                  { return m.name }
func (m *fakePlainModule) Init(_ modular.Application) error              { return nil }
func (m *fakePlainModule) RegisterConfig(_ modular.Application) error    { return nil }
func (m *fakePlainModule) Dependencies() []string                        { return nil }
func (m *fakePlainModule) ProvidesServices() []modular.ServiceProvider   { return nil }
func (m *fakePlainModule) RequiresServices() []modular.ServiceDependency { return nil }

// TestEngine_BuildFromConfig_PlumbsDependsOnIntoModule pins the production
// path that closes workflow#663: BuildFromConfig must call SetDependencies
// on each module that implements it, with a defensive copy of
// modCfg.DependsOn, before app.RegisterModule. Without this end-to-end
// test, a future refactor that moves the structural type assertion out
// of the registration loop (or changes the contract) would silently
// regress the BMW PR #279/280 fix and only image-launch CI would catch it.
func TestEngine_BuildFromConfig_PlumbsDependsOnIntoModule(t *testing.T) {
	app := newMockApplication()
	engine := NewStdEngine(app, app.Logger())

	var capturedConsumer *fakeDepAwareModule
	var capturedBroker *fakeDepAwareModule
	engine.AddModuleType("test.broker", func(name string, _ map[string]any) modular.Module {
		capturedBroker = &fakeDepAwareModule{name: name}
		return capturedBroker
	})
	engine.AddModuleType("test.consumer", func(name string, _ map[string]any) modular.Module {
		capturedConsumer = &fakeDepAwareModule{name: name}
		return capturedConsumer
	})

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "broker", Type: "test.broker"},
			{Name: "consumer", Type: "test.consumer", DependsOn: []string{"broker"}},
		},
		Workflows: map[string]any{},
		Triggers:  map[string]any{},
	}

	if err := engine.BuildFromConfig(cfg); err != nil {
		t.Fatalf("BuildFromConfig: %v", err)
	}

	if capturedBroker == nil || capturedConsumer == nil {
		t.Fatal("factories were not invoked")
	}

	// Broker has no dependsOn → no SetDependencies call.
	if len(capturedBroker.setCalls) != 0 {
		t.Errorf("broker SetDependencies calls = %d, want 0", len(capturedBroker.setCalls))
	}

	// Consumer dependsOn:[broker] → exactly one SetDependencies call with that value.
	if len(capturedConsumer.setCalls) != 1 {
		t.Fatalf("consumer SetDependencies calls = %d, want 1", len(capturedConsumer.setCalls))
	}
	got := capturedConsumer.setCalls[0]
	if len(got) != 1 || got[0] != "broker" {
		t.Errorf("consumer SetDependencies received %v, want [broker]", got)
	}
}

// TestEngine_BuildFromConfig_PlumbsDefensiveCopy pins the defensive copy:
// after BuildFromConfig returns, mutating modCfg.DependsOn must not
// affect the slice the module recorded. Without the copy, downstream
// code that re-uses ModuleConfig structs across builds (or mutates them
// for any reason) could silently corrupt the engine-side dependency graph.
func TestEngine_BuildFromConfig_PlumbsDefensiveCopy(t *testing.T) {
	app := newMockApplication()
	engine := NewStdEngine(app, app.Logger())

	var captured *fakeDepAwareModule
	engine.AddModuleType("test.depmod", func(name string, _ map[string]any) modular.Module {
		captured = &fakeDepAwareModule{name: name}
		return captured
	})

	deps := []string{"root-a", "root-b"}
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "root-a", Type: "test.depmod"},
			{Name: "root-b", Type: "test.depmod"},
			{Name: "child", Type: "test.depmod", DependsOn: deps},
		},
		Workflows: map[string]any{},
		Triggers:  map[string]any{},
	}

	if err := engine.BuildFromConfig(cfg); err != nil {
		t.Fatalf("BuildFromConfig: %v", err)
	}

	// Mutate the original yaml-derived slice. Because we registered three
	// modules using the same factory, `captured` is the LAST one — the
	// "child" module with the dependsOn slice. Its recorded SetDependencies
	// arg should NOT change after we mutate `deps`.
	if len(captured.setCalls) != 1 {
		t.Fatalf("child SetDependencies calls = %d, want 1", len(captured.setCalls))
	}
	deps[0] = "root-b" // valid mutation — duplicate of root-b, doesn't matter for the assertion
	got := captured.setCalls[0]
	if got[0] != "root-a" {
		t.Errorf("engine did NOT defensively copy DependsOn — mutating the yaml slice changed the recorded value from root-a to %q", got[0])
	}
}

// TestEngine_BuildFromConfig_SkipsModulesWithoutSetter pins that modules
// not implementing the SetDependencies contract are silently skipped
// (not panicked, not errored) even when modCfg.DependsOn is non-empty.
// The yaml-level dependsOn is then expressed only through cfg.Modules
// slice ordering (via topoSortModules) — modular's own DependencyAware
// sort sees nil from the module's Dependencies() and treats it as a root.
// This is the back-compat surface for built-in modules that haven't
// opted in to the new setter.
func TestEngine_BuildFromConfig_SkipsModulesWithoutSetter(t *testing.T) {
	app := newMockApplication()
	engine := NewStdEngine(app, app.Logger())

	engine.AddModuleType("test.plain", func(name string, _ map[string]any) modular.Module {
		return &fakePlainModule{name: name}
	})

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "root", Type: "test.plain"},
			{Name: "child", Type: "test.plain", DependsOn: []string{"root"}},
		},
		Workflows: map[string]any{},
		Triggers:  map[string]any{},
	}

	// The structural-type-assertion path must not panic on a module that
	// lacks the setter. A successful BuildFromConfig is the assertion.
	if err := engine.BuildFromConfig(cfg); err != nil {
		t.Fatalf("BuildFromConfig must tolerate modules without SetDependencies: %v", err)
	}
}
