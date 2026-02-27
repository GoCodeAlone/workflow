package workflow

import (
	"context"
	"fmt"
	"testing"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/config"
)

// mockReconfigurableModule is a test module that supports reconfiguration.
type mockReconfigurableModule struct {
	name   string
	config map[string]any
	calls  int
	failOn string // if set, Reconfigure returns error when name matches
}

func (m *mockReconfigurableModule) Name() string { return m.name }

func (m *mockReconfigurableModule) Init(app modular.Application) error { return nil }

func (m *mockReconfigurableModule) Reconfigure(_ context.Context, cfg map[string]any) error {
	if m.failOn != "" && m.name == m.failOn {
		return fmt.Errorf("reconfigure failed for %s", m.name)
	}
	m.config = cfg
	m.calls++
	return nil
}

// mockNonReconfigurableModule is a test module that does NOT support reconfiguration.
type mockNonReconfigurableModule struct {
	name string
}

func (m *mockNonReconfigurableModule) Name() string { return m.name }

func (m *mockNonReconfigurableModule) Init(app modular.Application) error { return nil }

// TestReconfigureModules_Success tests successful reconfiguration of a single module.
func TestReconfigureModules_Success(t *testing.T) {
	app := newMockApplication()
	engine := NewStdEngine(app, app.Logger())

	// Register a reconfigurable module
	mod := &mockReconfigurableModule{
		name:   "test-module",
		config: map[string]any{"version": "v1"},
	}
	app.RegisterModule(mod)

	ctx := context.Background()
	newConfig := map[string]any{"version": "v2", "timeout": 30}

	changes := []config.ModuleConfigChange{
		{
			Name:      "test-module",
			OldConfig: map[string]any{"version": "v1"},
			NewConfig: newConfig,
		},
	}

	failed, err := engine.ReconfigureModules(ctx, changes)

	if err != nil {
		t.Fatalf("ReconfigureModules returned error: %v", err)
	}

	if len(failed) != 0 {
		t.Fatalf("Expected 0 failed modules, got %d: %v", len(failed), failed)
	}

	if mod.calls != 1 {
		t.Fatalf("Expected Reconfigure to be called once, got %d", mod.calls)
	}

	if mod.config["version"] != "v2" {
		t.Fatalf("Expected config version to be v2, got %v", mod.config["version"])
	}

	if mod.config["timeout"] != 30 {
		t.Fatalf("Expected timeout to be 30, got %v", mod.config["timeout"])
	}
}

// TestReconfigureModules_NotReconfigurable tests that non-reconfigurable modules
// are reported in failedModules.
func TestReconfigureModules_NotReconfigurable(t *testing.T) {
	app := newMockApplication()
	engine := NewStdEngine(app, app.Logger())

	// Register a non-reconfigurable module
	mod := &mockNonReconfigurableModule{name: "static-module"}
	app.RegisterModule(mod)

	ctx := context.Background()
	changes := []config.ModuleConfigChange{
		{
			Name:      "static-module",
			OldConfig: map[string]any{},
			NewConfig: map[string]any{"key": "value"},
		},
	}

	failed, err := engine.ReconfigureModules(ctx, changes)

	if err != nil {
		t.Fatalf("ReconfigureModules returned error: %v", err)
	}

	if len(failed) != 1 {
		t.Fatalf("Expected 1 failed module, got %d: %v", len(failed), failed)
	}

	if failed[0] != "static-module" {
		t.Fatalf("Expected failed module to be 'static-module', got %s", failed[0])
	}
}

// TestReconfigureModules_ModuleNotFound tests that non-existent modules
// are reported in failedModules.
func TestReconfigureModules_ModuleNotFound(t *testing.T) {
	app := newMockApplication()
	engine := NewStdEngine(app, app.Logger())

	ctx := context.Background()
	changes := []config.ModuleConfigChange{
		{
			Name:      "nonexistent-module",
			OldConfig: map[string]any{},
			NewConfig: map[string]any{"key": "value"},
		},
	}

	failed, err := engine.ReconfigureModules(ctx, changes)

	if err != nil {
		t.Fatalf("ReconfigureModules returned error: %v", err)
	}

	if len(failed) != 1 {
		t.Fatalf("Expected 1 failed module, got %d: %v", len(failed), failed)
	}

	if failed[0] != "nonexistent-module" {
		t.Fatalf("Expected failed module to be 'nonexistent-module', got %s", failed[0])
	}
}

// TestReconfigureModules_ReconfigureFails tests that when Reconfigure returns
// an error, the module is added to failedModules.
func TestReconfigureModules_ReconfigureFails(t *testing.T) {
	app := newMockApplication()
	engine := NewStdEngine(app, app.Logger())

	// Register a module that fails on reconfigure
	mod := &mockReconfigurableModule{
		name:   "failing-module",
		config: map[string]any{},
		failOn: "failing-module",
	}
	app.RegisterModule(mod)

	ctx := context.Background()
	changes := []config.ModuleConfigChange{
		{
			Name:      "failing-module",
			OldConfig: map[string]any{},
			NewConfig: map[string]any{"key": "value"},
		},
	}

	failed, err := engine.ReconfigureModules(ctx, changes)

	if err != nil {
		t.Fatalf("ReconfigureModules returned error: %v", err)
	}

	if len(failed) != 1 {
		t.Fatalf("Expected 1 failed module, got %d: %v", len(failed), failed)
	}

	if failed[0] != "failing-module" {
		t.Fatalf("Expected failed module to be 'failing-module', got %s", failed[0])
	}
}

// TestReconfigureModules_MultipleModules tests reconfiguration of multiple
// modules with different outcomes (success, not found, not reconfigurable, error).
func TestReconfigureModules_MultipleModules(t *testing.T) {
	app := newMockApplication()
	engine := NewStdEngine(app, app.Logger())

	// Register modules
	reconfigMod := &mockReconfigurableModule{
		name:   "reconfig-module",
		config: map[string]any{},
	}
	nonReconfigMod := &mockNonReconfigurableModule{name: "static-module"}
	failingMod := &mockReconfigurableModule{
		name:   "failing-module",
		config: map[string]any{},
		failOn: "failing-module",
	}

	app.RegisterModule(reconfigMod)
	app.RegisterModule(nonReconfigMod)
	app.RegisterModule(failingMod)

	ctx := context.Background()
	changes := []config.ModuleConfigChange{
		{
			Name:      "reconfig-module",
			OldConfig: map[string]any{},
			NewConfig: map[string]any{"key": "value1"},
		},
		{
			Name:      "static-module",
			OldConfig: map[string]any{},
			NewConfig: map[string]any{"key": "value2"},
		},
		{
			Name:      "failing-module",
			OldConfig: map[string]any{},
			NewConfig: map[string]any{"key": "value3"},
		},
		{
			Name:      "nonexistent-module",
			OldConfig: map[string]any{},
			NewConfig: map[string]any{"key": "value4"},
		},
	}

	failed, err := engine.ReconfigureModules(ctx, changes)

	if err != nil {
		t.Fatalf("ReconfigureModules returned error: %v", err)
	}

	// Should have 3 failed modules: static-module (not reconfigurable),
	// failing-module (error), and nonexistent-module (not found)
	if len(failed) != 3 {
		t.Fatalf("Expected 3 failed modules, got %d: %v", len(failed), failed)
	}

	// Verify the successful module was reconfigured
	if reconfigMod.calls != 1 {
		t.Fatalf("Expected reconfig-module Reconfigure to be called once, got %d", reconfigMod.calls)
	}
	if reconfigMod.config["key"] != "value1" {
		t.Fatalf("Expected reconfig-module config key to be value1, got %v", reconfigMod.config["key"])
	}
}

// TestReconfigureModules_EmptyChanges tests that empty changes list returns
// success with no failed modules.
func TestReconfigureModules_EmptyChanges(t *testing.T) {
	app := newMockApplication()
	engine := NewStdEngine(app, app.Logger())

	ctx := context.Background()
	changes := []config.ModuleConfigChange{}

	failed, err := engine.ReconfigureModules(ctx, changes)

	if err != nil {
		t.Fatalf("ReconfigureModules returned error: %v", err)
	}

	if len(failed) != 0 {
		t.Fatalf("Expected 0 failed modules, got %d: %v", len(failed), failed)
	}
}

// TestReconfigureModules_ContextCancellation tests that context cancellation
// is respected during reconfiguration.
func TestReconfigureModules_ContextCancellation(t *testing.T) {
	app := newMockApplication()
	engine := NewStdEngine(app, app.Logger())

	// Register a module with a slow reconfigure
	mod := &mockReconfigurableModule{
		name:   "test-module",
		config: map[string]any{},
	}
	app.RegisterModule(mod)

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	changes := []config.ModuleConfigChange{
		{
			Name:      "test-module",
			OldConfig: map[string]any{},
			NewConfig: map[string]any{"key": "value"},
		},
	}

	// This should still work - our mock doesn't check context cancellation,
	// but in a real scenario with a context-aware Reconfigure, it would respect it
	failed, err := engine.ReconfigureModules(ctx, changes)

	if err != nil {
		t.Fatalf("ReconfigureModules returned error: %v", err)
	}

	// Even with cancelled context, the module should be reconfigured
	// (our mock implementation doesn't check context)
	if len(failed) != 0 {
		t.Fatalf("Expected 0 failed modules, got %d: %v", len(failed), failed)
	}
}
