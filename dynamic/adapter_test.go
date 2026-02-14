package dynamic

import (
	"context"
	"testing"

	"github.com/CrisisTextLine/modular"
)

func TestNewModuleAdapter(t *testing.T) {
	pool := NewInterpreterPool()
	comp := NewDynamicComponent("test-comp", pool)
	adapter := NewModuleAdapter(comp)

	if adapter.Name() != "test-comp" {
		t.Errorf("expected name 'test-comp', got %q", adapter.Name())
	}
}

func TestModuleAdapter_SetProvidesRequires(t *testing.T) {
	pool := NewInterpreterPool()
	comp := NewDynamicComponent("test-comp", pool)
	adapter := NewModuleAdapter(comp)

	adapter.SetProvides([]string{"svc-a", "svc-b"})
	adapter.SetRequires([]string{"svc-c"})

	if len(adapter.provides) != 2 {
		t.Errorf("expected 2 provides, got %d", len(adapter.provides))
	}
	if len(adapter.requires) != 1 {
		t.Errorf("expected 1 requires, got %d", len(adapter.requires))
	}
}

func TestModuleAdapter_Init(t *testing.T) {
	pool := NewInterpreterPool()
	comp := NewDynamicComponent("test-comp", pool)

	// Load minimal source so init works
	src := `package component

func Name() string { return "test-comp" }
func Init(services map[string]interface{}) error { return nil }
`
	if err := comp.LoadFromSource(src); err != nil {
		t.Fatalf("failed to load source: %v", err)
	}

	adapter := NewModuleAdapter(comp)

	logger := &testLogger{}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)
	if err := app.Init(); err != nil {
		t.Fatalf("failed to init app: %v", err)
	}

	if err := adapter.Init(app); err != nil {
		t.Fatalf("adapter Init failed: %v", err)
	}
}

func TestModuleAdapter_InitWithProvides(t *testing.T) {
	pool := NewInterpreterPool()
	comp := NewDynamicComponent("test-comp", pool)

	src := `package component

func Name() string { return "test-comp" }
func Init(services map[string]interface{}) error { return nil }
`
	if err := comp.LoadFromSource(src); err != nil {
		t.Fatalf("failed to load source: %v", err)
	}

	adapter := NewModuleAdapter(comp)
	adapter.SetProvides([]string{"my-service"})

	logger := &testLogger{}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)
	if err := app.Init(); err != nil {
		t.Fatalf("failed to init app: %v", err)
	}

	if err := adapter.Init(app); err != nil {
		t.Fatalf("adapter Init failed: %v", err)
	}

	// Verify the service was registered
	var svc any
	if err := app.GetService("my-service", &svc); err != nil {
		t.Fatalf("expected service 'my-service' to be registered: %v", err)
	}
}

func TestModuleAdapter_Execute(t *testing.T) {
	pool := NewInterpreterPool()
	comp := NewDynamicComponent("test-comp", pool)

	src := `package component

import "context"

func Name() string { return "test-comp" }

func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{"result": "ok"}, nil
}
`
	if err := comp.LoadFromSource(src); err != nil {
		t.Fatalf("failed to load source: %v", err)
	}

	adapter := NewModuleAdapter(comp)
	result, err := adapter.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result["result"] != "ok" {
		t.Errorf("expected result 'ok', got %v", result["result"])
	}
}

func TestModuleAdapter_ProvidesServices(t *testing.T) {
	pool := NewInterpreterPool()
	comp := NewDynamicComponent("test-comp", pool)
	adapter := NewModuleAdapter(comp)
	adapter.SetProvides([]string{"svc-a", "svc-b"})

	svcs := adapter.ProvidesServices()
	if len(svcs) != 2 {
		t.Fatalf("expected 2 services, got %d", len(svcs))
	}
	if svcs[0].Name != "svc-a" {
		t.Errorf("expected first service 'svc-a', got %q", svcs[0].Name)
	}
}

func TestModuleAdapter_RequiresServices(t *testing.T) {
	pool := NewInterpreterPool()
	comp := NewDynamicComponent("test-comp", pool)
	adapter := NewModuleAdapter(comp)
	adapter.SetRequires([]string{"svc-c"})

	deps := adapter.RequiresServices()
	if len(deps) != 1 {
		t.Fatalf("expected 1 dependency, got %d", len(deps))
	}
	if deps[0].Name != "svc-c" {
		t.Errorf("expected dependency 'svc-c', got %q", deps[0].Name)
	}
}

func TestRegistry_ListNames(t *testing.T) {
	reg := NewComponentRegistry()
	pool := NewInterpreterPool()

	comp1 := NewDynamicComponent("alpha", pool)
	comp2 := NewDynamicComponent("beta", pool)

	_ = reg.Register("alpha", comp1)
	_ = reg.Register("beta", comp2)

	names := reg.ListNames()
	if len(names) != 2 {
		t.Fatalf("expected 2 names, got %d", len(names))
	}

	found := map[string]bool{}
	for _, n := range names {
		found[n] = true
	}
	if !found["alpha"] || !found["beta"] {
		t.Errorf("expected alpha and beta in names, got %v", names)
	}
}

func TestRegistry_ListNames_Empty(t *testing.T) {
	reg := NewComponentRegistry()
	names := reg.ListNames()
	if len(names) != 0 {
		t.Errorf("expected 0 names, got %d", len(names))
	}
}

// testLogger is a simple logger for testing
type testLogger struct{}

func (l *testLogger) Debug(msg string, args ...any) {}
func (l *testLogger) Info(msg string, args ...any)  {}
func (l *testLogger) Warn(msg string, args ...any)  {}
func (l *testLogger) Error(msg string, args ...any) {}
func (l *testLogger) Fatal(msg string, args ...any) {}
