package workflow

import (
	"io"
	"log/slog"
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

// rawSliceRecorderModule retains the EXACT slice reference passed to
// SetDependencies — no defensive copy on the recording side. Used by
// TestEngine_BuildFromConfig_PlumbsDefensiveCopy to prove the engine
// itself copies; if the engine just passed modCfg.DependsOn through,
// mutating the original yaml-derived slice would mutate this recorded
// reference too, and the test would fail.
type rawSliceRecorderModule struct {
	name        string
	rawRecorded []string // nil until SetDependencies fires; then the exact slice ref
	callCount   int
}

func (m *rawSliceRecorderModule) Name() string                                  { return m.name }
func (m *rawSliceRecorderModule) Init(_ modular.Application) error              { return nil }
func (m *rawSliceRecorderModule) RegisterConfig(_ modular.Application) error    { return nil }
func (m *rawSliceRecorderModule) Dependencies() []string                        { return nil }
func (m *rawSliceRecorderModule) ProvidesServices() []modular.ServiceProvider   { return nil }
func (m *rawSliceRecorderModule) RequiresServices() []modular.ServiceDependency { return nil }
func (m *rawSliceRecorderModule) SetDependencies(deps []string) {
	m.rawRecorded = deps // INTENTIONAL: no copy. Asserts engine-side copy.
	m.callCount++
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
//
// The fake here records the EXACT slice reference passed to
// SetDependencies (no defensive copy on the recording side) so a
// regression where the engine passes modCfg.DependsOn through verbatim
// would let a post-build mutation appear in the recorded value and fail
// the assertion.
func TestEngine_BuildFromConfig_PlumbsDefensiveCopy(t *testing.T) {
	app := newMockApplication()
	engine := NewStdEngine(app, app.Logger())

	var capturedChild *rawSliceRecorderModule
	engine.AddModuleType("test.rawmod", func(name string, _ map[string]any) modular.Module {
		m := &rawSliceRecorderModule{name: name}
		if name == "child" {
			capturedChild = m
		}
		return m
	})

	deps := []string{"root-a", "root-b"}
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "root-a", Type: "test.rawmod"},
			{Name: "root-b", Type: "test.rawmod"},
			{Name: "child", Type: "test.rawmod", DependsOn: deps},
		},
		Workflows: map[string]any{},
		Triggers:  map[string]any{},
	}

	if err := engine.BuildFromConfig(cfg); err != nil {
		t.Fatalf("BuildFromConfig: %v", err)
	}

	if capturedChild == nil {
		t.Fatal("child factory was not invoked")
	}
	if capturedChild.callCount != 1 {
		t.Fatalf("child SetDependencies call count = %d, want 1", capturedChild.callCount)
	}

	// Mutate the original yaml-derived slice. The child's recorded
	// reference must NOT see this change, because the engine should have
	// passed a defensive copy. If the engine passed `deps` directly, this
	// mutation would change capturedChild.rawRecorded[0] too and the
	// assertion below would fail.
	deps[0] = "MUTATED"
	if capturedChild.rawRecorded[0] != "root-a" {
		t.Errorf("engine did NOT defensively copy DependsOn — mutating the yaml slice from %q to %q changed the recorded value (now %q)",
			"root-a", deps[0], capturedChild.rawRecorded[0])
	}
}

// initOrderRecorderModule appends its name to a shared slice during Init().
// Used by TestEngine_BuildFromConfig_RealModularHonoursDependsOn to assert
// the order modular's real Init walker fires modules in — proving that
// SetDependencies plumbed by BuildFromConfig actually drives modular's
// dependency-aware sort, not just that the recorded values look right.
type initOrderRecorderModule struct {
	name      string
	deps      []string
	initOrder *[]string // shared pointer; appended to on Init
}

func (m *initOrderRecorderModule) Name() string { return m.name }
func (m *initOrderRecorderModule) Init(_ modular.Application) error {
	*m.initOrder = append(*m.initOrder, m.name)
	return nil
}
func (m *initOrderRecorderModule) RegisterConfig(_ modular.Application) error    { return nil }
func (m *initOrderRecorderModule) Dependencies() []string                        { return m.deps }
func (m *initOrderRecorderModule) ProvidesServices() []modular.ServiceProvider   { return nil }
func (m *initOrderRecorderModule) RequiresServices() []modular.ServiceDependency { return nil }
func (m *initOrderRecorderModule) SetDependencies(deps []string) {
	cp := make([]string, len(deps))
	copy(cp, deps)
	m.deps = cp
}

// stubConfigProvider is a minimal modular.ConfigProvider for NewStdApplication.
type stubConfigProvider struct{}

func (stubConfigProvider) GetConfig() any { return nil }

// TestEngine_BuildFromConfig_RealModularHonoursDependsOn proves the
// fix end-to-end against the *real* modular framework — not the test
// mockApplication whose Init() is a no-op. A consumer module declared
// FIRST in cfg.Modules with dependsOn:[broker] must initialise AFTER
// the broker, even though the broker is declared SECOND. This pins
// the actual production behaviour the BMW PR #279/#280 race depends on.
func TestEngine_BuildFromConfig_RealModularHonoursDependsOn(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	app := modular.NewStdApplication(stubConfigProvider{}, logger)
	engine := NewStdEngine(app, logger)

	var initOrder []string
	engine.AddModuleType("test.depinit", func(name string, _ map[string]any) modular.Module {
		return &initOrderRecorderModule{name: name, initOrder: &initOrder}
	})

	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			// Consumer declared FIRST, depends on broker.
			{Name: "consumer", Type: "test.depinit", DependsOn: []string{"broker"}},
			// Broker declared SECOND.
			{Name: "broker", Type: "test.depinit"},
		},
		Workflows: map[string]any{},
		Triggers:  map[string]any{},
	}

	if err := engine.BuildFromConfig(cfg); err != nil {
		t.Fatalf("BuildFromConfig: %v", err)
	}

	// Without the SetDependencies plumbing, modular's Init walker would
	// see Dependencies()=nil for both modules and fall back to alphabetical
	// (broker before consumer) — which would actually pass this assertion
	// by coincidence. To prove the plumbing matters, declare the broker
	// with a name that sorts AFTER the consumer alphabetically. With
	// SetDependencies wired, modular still inits broker first (because
	// consumer.Dependencies() now returns ["broker"]). Without it, modular
	// would init "consumer" then "z-broker" (alphabetical) and the
	// assertion would fail.
	//
	// Reset + re-run with that shape.
	initOrder = initOrder[:0]
	app2 := modular.NewStdApplication(stubConfigProvider{}, logger)
	engine2 := NewStdEngine(app2, logger)
	engine2.AddModuleType("test.depinit", func(name string, _ map[string]any) modular.Module {
		return &initOrderRecorderModule{name: name, initOrder: &initOrder}
	})
	cfg2 := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "consumer", Type: "test.depinit", DependsOn: []string{"z-broker"}},
			{Name: "z-broker", Type: "test.depinit"},
		},
		Workflows: map[string]any{},
		Triggers:  map[string]any{},
	}
	if err := engine2.BuildFromConfig(cfg2); err != nil {
		t.Fatalf("BuildFromConfig (z-broker shape): %v", err)
	}
	if len(initOrder) != 2 {
		t.Fatalf("initOrder = %v, want 2 entries", initOrder)
	}
	if initOrder[0] != "z-broker" || initOrder[1] != "consumer" {
		t.Errorf("modular Init order = %v, want [z-broker consumer] (broker must precede dependent even when alphabetically later)",
			initOrder)
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
