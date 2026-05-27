package workflow

import (
	"testing"

	"github.com/GoCodeAlone/workflow/module"
)

// TestEngineFactory_InfraAdminRegistered pins T18: the engine-side
// infra.admin module factory MUST be registered by NewStdEngine
// so the host loads `type: infra.admin` without requiring a
// plugin. Per docs/plans/2026-05-27-infra-admin-dynamic.md Task 18.
//
// Reads the factory map via the public RegisteredModuleTypes
// surface AND constructs an instance to assert the factory
// actually produces a *module.InfraAdmin (not nil, not a fake
// type).
func TestEngineFactory_InfraAdminRegistered(t *testing.T) {
	e := NewStdEngine(nil, nopLogger{})
	types := e.RegisteredModuleTypes()
	found := false
	for _, ty := range types {
		if ty == "infra.admin" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("infra.admin not in RegisteredModuleTypes (got %v)", types)
	}

	// Construct an instance through the factory to verify the
	// factory closure produces a real module — not a panic, not nil.
	factory := e.moduleFactories["infra.admin"]
	if factory == nil {
		t.Fatal("moduleFactories[infra.admin] is nil even though listed")
	}
	mod := factory("test-infra-admin", map[string]any{})
	if mod == nil {
		t.Fatal("factory returned nil module")
	}
	if _, ok := mod.(*module.InfraAdmin); !ok {
		t.Errorf("factory returned %T, want *module.InfraAdmin", mod)
	}
	if mod.Name() != "test-infra-admin" {
		t.Errorf("module Name = %q, want test-infra-admin", mod.Name())
	}
}

// nopLogger is the minimum modular.Logger for engine tests.
// Production wires the slog adapter; we just want a non-nil
// logger so NewStdEngine doesn't crash.
type nopLogger struct{}

func (nopLogger) Debug(string, ...any) {}
func (nopLogger) Info(string, ...any)  {}
func (nopLogger) Warn(string, ...any)  {}
func (nopLogger) Error(string, ...any) {}
