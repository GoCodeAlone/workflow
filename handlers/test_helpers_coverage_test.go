package handlers

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/mock"
)

func TestTestServiceRegistry_Start(t *testing.T) {
	r := NewTestServiceRegistry()
	if err := r.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
}

func TestTestServiceRegistry_Stop(t *testing.T) {
	r := NewTestServiceRegistry()
	if err := r.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestTestServiceRegistry_Run(t *testing.T) {
	r := NewTestServiceRegistry()
	if err := r.Run(); err != nil {
		t.Fatalf("Run failed: %v", err)
	}
}

func TestTestServiceRegistry_Logger(t *testing.T) {
	r := NewTestServiceRegistry()
	logger := r.Logger()
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
}

func TestTestServiceRegistry_ConfigProvider(t *testing.T) {
	r := NewTestServiceRegistry()
	cp := r.ConfigProvider()
	if cp == nil {
		t.Fatal("expected non-nil config provider")
	}
}

func TestTestServiceRegistry_ConfigSections(t *testing.T) {
	r := NewTestServiceRegistry()
	sections := r.ConfigSections()
	if sections == nil {
		t.Fatal("expected non-nil config sections")
	}
}

func TestTestServiceRegistry_RegisterConfigSection(t *testing.T) {
	r := NewTestServiceRegistry()
	cp := &mock.ConfigProvider{ConfigData: map[string]any{"key": "val"}}
	r.RegisterConfigSection("test-section", cp)
	if r.configSections["test-section"] != cp {
		t.Error("expected config section to be registered")
	}
}

func TestTestServiceRegistry_GetConfigSection(t *testing.T) {
	r := NewTestServiceRegistry()
	cp := &mock.ConfigProvider{ConfigData: map[string]any{}}
	r.RegisterConfigSection("my-section", cp)
	got, err := r.GetConfigSection("my-section")
	if err != nil {
		t.Fatalf("GetConfigSection failed: %v", err)
	}
	if got != cp {
		t.Error("expected same config provider back")
	}
}

func TestTestServiceRegistry_IsVerboseConfig(t *testing.T) {
	r := NewTestServiceRegistry()
	if r.IsVerboseConfig() {
		t.Error("expected IsVerboseConfig to return false")
	}
}

func TestTestServiceRegistry_SetVerboseConfig(t *testing.T) {
	r := NewTestServiceRegistry()
	r.SetVerboseConfig(true) // no-op, should not panic
}

func TestTestServiceRegistry_SetLogger(t *testing.T) {
	r := NewTestServiceRegistry()
	newLogger := &mock.Logger{LogEntries: make([]string, 0)}
	r.SetLogger(newLogger)
	if r.logger != newLogger {
		t.Error("expected logger to be updated")
	}
}

func TestTestServiceRegistry_RegisterModule(t *testing.T) {
	r := NewTestServiceRegistry()
	r.RegisterModule(nil) // no-op, should not panic
}

func TestTestServiceRegistry_GetServicesByModule(t *testing.T) {
	r := NewTestServiceRegistry()
	svcs := r.GetServicesByModule("any")
	if len(svcs) != 0 {
		t.Errorf("expected empty, got %v", svcs)
	}
}

func TestTestServiceRegistry_GetServiceEntry(t *testing.T) {
	r := NewTestServiceRegistry()
	entry, found := r.GetServiceEntry("any")
	if entry != nil || found {
		t.Error("expected nil, false")
	}
}

func TestTestServiceRegistry_GetServicesByInterface(t *testing.T) {
	r := NewTestServiceRegistry()
	entries := r.GetServicesByInterface(nil)
	if entries != nil {
		t.Error("expected nil")
	}
}

func TestTestServiceRegistry_StartTime(t *testing.T) {
	r := NewTestServiceRegistry()
	st := r.StartTime()
	if !st.IsZero() {
		t.Error("expected zero time")
	}
}

func TestTestServiceRegistry_GetModule(t *testing.T) {
	r := NewTestServiceRegistry()
	m := r.GetModule("any")
	if m != nil {
		t.Error("expected nil module")
	}
}

func TestTestServiceRegistry_GetAllModules(t *testing.T) {
	r := NewTestServiceRegistry()
	mods := r.GetAllModules()
	if mods != nil {
		t.Error("expected nil")
	}
}

func TestTestServiceRegistry_OnConfigLoaded(t *testing.T) {
	r := NewTestServiceRegistry()
	r.OnConfigLoaded(nil) // no-op, should not panic
}

func TestSetMockConfig(t *testing.T) {
	r := NewTestServiceRegistry()
	newCP := &mock.ConfigProvider{ConfigData: map[string]any{"x": 1}}
	r.SetMockConfig(newCP)
	if r.config != newCP {
		t.Error("expected config to be updated")
	}
}

func TestSetMockLogger(t *testing.T) {
	r := NewTestServiceRegistry()
	newLogger := &mock.Logger{LogEntries: make([]string, 0)}
	r.SetMockLogger(newLogger)
	if r.logger != newLogger {
		t.Error("expected logger to be updated")
	}
}

func TestTestJob_Execute_NilFn(t *testing.T) {
	job := &TestJob{}
	err := job.Execute(context.Background())
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}

func TestNewTestEngine(t *testing.T) {
	app := CreateMockApplication()
	engine := NewTestEngine(app)
	if engine == nil {
		t.Fatal("expected non-nil engine")
	}
	if len(engine.handlers) != 3 {
		t.Errorf("expected 3 handlers, got %d", len(engine.handlers))
	}
}

func TestMockEngine_RegisterHandler(t *testing.T) {
	app := CreateMockApplication()
	engine := NewTestEngine(app)
	engine.RegisterHandler("custom", "handler-val")
	if engine.handlers["custom"] != "handler-val" {
		t.Error("expected handler to be registered")
	}
}

func TestMockEngine_Start(t *testing.T) {
	app := CreateMockApplication()
	engine := NewTestEngine(app)
	err := engine.Start(context.Background())
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}

func TestMockEngine_Stop(t *testing.T) {
	app := CreateMockApplication()
	engine := NewTestEngine(app)
	err := engine.Stop(context.Background())
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}

func TestMockEngine_BuildFromConfig(t *testing.T) {
	app := CreateMockApplication()
	engine := NewTestEngine(app)
	err := engine.BuildFromConfig(nil)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
}

func TestMockEngine_AddModuleType(t *testing.T) {
	app := CreateMockApplication()
	engine := NewTestEngine(app)
	engine.AddModuleType("test", nil) // no-op, should not panic
}
