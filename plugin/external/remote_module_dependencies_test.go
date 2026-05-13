package external

import (
	"testing"
)

// TestRemoteModule_Dependencies_DefaultsToNil pins that a freshly-constructed
// RemoteModule reports no dependencies — the same behaviour as before
// workflow#663. The DependencyAware contract still applies; modular's
// Init() walker just sees an empty list and treats the module as a root.
func TestRemoteModule_Dependencies_DefaultsToNil(t *testing.T) {
	m := &RemoteModule{name: "x"}
	if got := m.Dependencies(); got != nil {
		t.Errorf("Dependencies() default = %v, want nil", got)
	}
}

// TestRemoteModule_SetDependencies_PlumbsYamlDependsOn verifies the engine's
// post-factory plumb path: after the factory returns the module the engine
// calls SetDependencies with the yaml-level `dependsOn:` slice. Dependencies()
// must then return exactly that slice so modular's DependencyAware-driven
// Init() walker honours it. Closes the workflow#663 follow-up that surfaced
// on BMW PR #280's image-launch.
func TestRemoteModule_SetDependencies_PlumbsYamlDependsOn(t *testing.T) {
	m := &RemoteModule{name: "bmw-consumer-audit-appender"}
	m.SetDependencies([]string{"bmw-eventbus", "bmw-stream"})
	got := m.Dependencies()
	if len(got) != 2 || got[0] != "bmw-eventbus" || got[1] != "bmw-stream" {
		t.Errorf("Dependencies() = %v, want [bmw-eventbus bmw-stream]", got)
	}
}

// TestRemoteModule_SetDependencies_EmptySliceIsEmpty pins that passing an
// empty slice records an empty (non-nil) slice. modular treats a non-nil
// empty slice as "no dependencies declared" the same as nil, so the
// distinction is internal — but pinning it prevents accidental drift if a
// future refactor adds a nil-check that changes semantics.
func TestRemoteModule_SetDependencies_EmptySliceIsEmpty(t *testing.T) {
	m := &RemoteModule{name: "x"}
	m.SetDependencies([]string{})
	got := m.Dependencies()
	if got == nil || len(got) != 0 {
		t.Errorf("Dependencies() after SetDependencies([]) = %v, want empty non-nil slice", got)
	}
}

// TestRemoteModule_SetDependencies_Overwrites pins that a second call
// replaces the previous slice rather than appending. The engine calls
// SetDependencies exactly once per module per BuildFromConfig, but
// pinning the contract guards against future engine-side double-calls
// (e.g., a config-transform hook that mutates dependsOn post-registration).
func TestRemoteModule_SetDependencies_Overwrites(t *testing.T) {
	m := &RemoteModule{name: "x"}
	m.SetDependencies([]string{"a", "b"})
	m.SetDependencies([]string{"c"})
	got := m.Dependencies()
	if len(got) != 1 || got[0] != "c" {
		t.Errorf("Dependencies() after second SetDependencies = %v, want [c]", got)
	}
}

// TestRemoteModule_ImplementsDependencyAware pins that *RemoteModule
// satisfies a structurally-typed interface matching modular.DependencyAware.
// We can't import modular's interface here without a circular dep, but
// the structural check is sufficient — modular itself does the same
// type assertion at init time.
func TestRemoteModule_ImplementsDependencyAware(t *testing.T) {
	var _ interface {
		Dependencies() []string
	} = (*RemoteModule)(nil)
}

// TestRemoteModule_ImplementsDependencyTargetInterface pins the structural
// type that engine.go uses to find modules whose dependsOn it should plumb:
//
//	if dt, ok := mod.(interface{ SetDependencies([]string) }); ok { ... }
//
// If a future refactor drops SetDependencies the engine silently stops
// plumbing, which would re-introduce the workflow#663 race. This test
// fails loudly instead.
func TestRemoteModule_ImplementsDependencyTargetInterface(t *testing.T) {
	var _ interface {
		SetDependencies([]string)
	} = (*RemoteModule)(nil)
}
