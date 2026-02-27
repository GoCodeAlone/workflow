package config

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"
)

// mockReconfigurer is a test double for ModuleReconfigurer.
type mockReconfigurer struct {
	called  [][]ModuleConfigChange
	failed  []string
	err     error
}

func (m *mockReconfigurer) ReconfigureModules(_ context.Context, changes []ModuleConfigChange) ([]string, error) {
	m.called = append(m.called, changes)
	return m.failed, m.err
}

func newTestReloader(t *testing.T, initial *WorkflowConfig, fullFn func(*WorkflowConfig) error, rec ModuleReconfigurer) *ConfigReloader {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	r, err := NewConfigReloader(initial, fullFn, rec, logger)
	if err != nil {
		t.Fatalf("NewConfigReloader: %v", err)
	}
	return r
}

func makeWorkflowConfig(modules []ModuleConfig, workflows map[string]any) *WorkflowConfig {
	return &WorkflowConfig{
		Modules:   modules,
		Workflows: workflows,
		Triggers:  make(map[string]any),
		Pipelines: make(map[string]any),
	}
}

func makeChangeEvent(cfg *WorkflowConfig) (ConfigChangeEvent, error) {
	hash, err := HashConfig(cfg)
	if err != nil {
		return ConfigChangeEvent{}, err
	}
	return ConfigChangeEvent{
		Source:  "test",
		OldHash: "oldhash",
		NewHash: hash,
		Config:  cfg,
		Time:    time.Now(),
	}, nil
}

func TestConfigReloader_FullReload_NonModuleChanges(t *testing.T) {
	initial := makeWorkflowConfig(
		[]ModuleConfig{{Name: "alpha", Type: "http.server"}},
		map[string]any{"flow1": map[string]any{"initial": "start"}},
	)

	var fullReloadCalled int
	var lastFullConfig *WorkflowConfig

	fullFn := func(cfg *WorkflowConfig) error {
		fullReloadCalled++
		lastFullConfig = cfg
		return nil
	}

	r := newTestReloader(t, initial, fullFn, nil)

	// New config: same modules, different workflow.
	newCfg := makeWorkflowConfig(
		[]ModuleConfig{{Name: "alpha", Type: "http.server"}},
		map[string]any{"flow1": map[string]any{"initial": "running"}},
	)
	evt, err := makeChangeEvent(newCfg)
	if err != nil {
		t.Fatalf("makeChangeEvent: %v", err)
	}

	if err := r.HandleChange(evt); err != nil {
		t.Fatalf("HandleChange: %v", err)
	}

	if fullReloadCalled != 1 {
		t.Errorf("expected 1 full reload, got %d", fullReloadCalled)
	}
	if lastFullConfig != newCfg {
		t.Error("expected full reload called with new config")
	}
}

func TestConfigReloader_FullReload_AddedModule(t *testing.T) {
	initial := makeWorkflowConfig(
		[]ModuleConfig{{Name: "alpha", Type: "http.server"}},
		nil,
	)

	var fullReloadCalled int
	fullFn := func(cfg *WorkflowConfig) error {
		fullReloadCalled++
		return nil
	}

	r := newTestReloader(t, initial, fullFn, nil)

	newCfg := makeWorkflowConfig(
		[]ModuleConfig{
			{Name: "alpha", Type: "http.server"},
			{Name: "beta", Type: "http.router"},
		},
		nil,
	)
	evt, err := makeChangeEvent(newCfg)
	if err != nil {
		t.Fatalf("makeChangeEvent: %v", err)
	}

	if err := r.HandleChange(evt); err != nil {
		t.Fatalf("HandleChange: %v", err)
	}
	if fullReloadCalled != 1 {
		t.Errorf("expected 1 full reload for added module, got %d", fullReloadCalled)
	}
}

func TestConfigReloader_FullReload_RemovedModule(t *testing.T) {
	initial := makeWorkflowConfig(
		[]ModuleConfig{
			{Name: "alpha", Type: "http.server"},
			{Name: "beta", Type: "http.router"},
		},
		nil,
	)

	var fullReloadCalled int
	fullFn := func(cfg *WorkflowConfig) error {
		fullReloadCalled++
		return nil
	}

	r := newTestReloader(t, initial, fullFn, nil)

	newCfg := makeWorkflowConfig(
		[]ModuleConfig{{Name: "alpha", Type: "http.server"}},
		nil,
	)
	evt, err := makeChangeEvent(newCfg)
	if err != nil {
		t.Fatalf("makeChangeEvent: %v", err)
	}

	if err := r.HandleChange(evt); err != nil {
		t.Fatalf("HandleChange: %v", err)
	}
	if fullReloadCalled != 1 {
		t.Errorf("expected 1 full reload for removed module, got %d", fullReloadCalled)
	}
}

func TestConfigReloader_PartialReconfigure(t *testing.T) {
	initial := makeWorkflowConfig(
		[]ModuleConfig{{Name: "alpha", Type: "cache", Config: map[string]any{"ttl": 60}}},
		nil,
	)

	var fullReloadCalled int
	fullFn := func(cfg *WorkflowConfig) error {
		fullReloadCalled++
		return nil
	}

	rec := &mockReconfigurer{}
	r := newTestReloader(t, initial, fullFn, rec)

	newCfg := makeWorkflowConfig(
		[]ModuleConfig{{Name: "alpha", Type: "cache", Config: map[string]any{"ttl": 120}}},
		nil,
	)
	evt, err := makeChangeEvent(newCfg)
	if err != nil {
		t.Fatalf("makeChangeEvent: %v", err)
	}

	if err := r.HandleChange(evt); err != nil {
		t.Fatalf("HandleChange: %v", err)
	}

	if fullReloadCalled != 0 {
		t.Errorf("expected 0 full reloads for module-only change, got %d", fullReloadCalled)
	}
	if len(rec.called) != 1 {
		t.Fatalf("expected ReconfigureModules called once, got %d", len(rec.called))
	}
	if len(rec.called[0]) != 1 || rec.called[0][0].Name != "alpha" {
		t.Errorf("expected ReconfigureModules called with 'alpha', got %v", rec.called[0])
	}
}

func TestConfigReloader_FallbackToFull(t *testing.T) {
	initial := makeWorkflowConfig(
		[]ModuleConfig{{Name: "alpha", Type: "db", Config: map[string]any{"dsn": "old"}}},
		nil,
	)

	var fullReloadCalled int
	fullFn := func(cfg *WorkflowConfig) error {
		fullReloadCalled++
		return nil
	}

	// Reconfigurer reports alpha as failed.
	rec := &mockReconfigurer{failed: []string{"alpha"}}
	r := newTestReloader(t, initial, fullFn, rec)

	newCfg := makeWorkflowConfig(
		[]ModuleConfig{{Name: "alpha", Type: "db", Config: map[string]any{"dsn": "new"}}},
		nil,
	)
	evt, err := makeChangeEvent(newCfg)
	if err != nil {
		t.Fatalf("makeChangeEvent: %v", err)
	}

	if err := r.HandleChange(evt); err != nil {
		t.Fatalf("HandleChange: %v", err)
	}

	if len(rec.called) != 1 {
		t.Errorf("expected ReconfigureModules called once, got %d", len(rec.called))
	}
	if fullReloadCalled != 1 {
		t.Errorf("expected 1 full reload after fallback, got %d", fullReloadCalled)
	}
}

func TestConfigReloader_NilReconfigurer_FallsBackToFullReload(t *testing.T) {
	initial := makeWorkflowConfig(
		[]ModuleConfig{{Name: "alpha", Type: "cache", Config: map[string]any{"ttl": 60}}},
		nil,
	)

	var fullReloadCalled int
	fullFn := func(cfg *WorkflowConfig) error {
		fullReloadCalled++
		return nil
	}

	// nil reconfigurer — module changes should still trigger full reload.
	r := newTestReloader(t, initial, fullFn, nil)

	newCfg := makeWorkflowConfig(
		[]ModuleConfig{{Name: "alpha", Type: "cache", Config: map[string]any{"ttl": 120}}},
		nil,
	)
	evt, err := makeChangeEvent(newCfg)
	if err != nil {
		t.Fatalf("makeChangeEvent: %v", err)
	}

	if err := r.HandleChange(evt); err != nil {
		t.Fatalf("HandleChange: %v", err)
	}

	if fullReloadCalled != 1 {
		t.Errorf("expected 1 full reload when reconfigurer is nil, got %d", fullReloadCalled)
	}
}

func TestConfigReloader_SetReconfigurer(t *testing.T) {
	initial := makeWorkflowConfig(
		[]ModuleConfig{{Name: "alpha", Type: "cache", Config: map[string]any{"ttl": 60}}},
		nil,
	)

	var fullReloadCalled int
	fullFn := func(cfg *WorkflowConfig) error {
		fullReloadCalled++
		return nil
	}

	// Start with nil reconfigurer.
	r := newTestReloader(t, initial, fullFn, nil)

	// Set a reconfigurer dynamically.
	rec := &mockReconfigurer{}
	r.SetReconfigurer(rec)

	newCfg := makeWorkflowConfig(
		[]ModuleConfig{{Name: "alpha", Type: "cache", Config: map[string]any{"ttl": 120}}},
		nil,
	)
	evt, err := makeChangeEvent(newCfg)
	if err != nil {
		t.Fatalf("makeChangeEvent: %v", err)
	}

	if err := r.HandleChange(evt); err != nil {
		t.Fatalf("HandleChange: %v", err)
	}

	// Should use partial reconfigure, not full reload.
	if fullReloadCalled != 0 {
		t.Errorf("expected 0 full reloads after SetReconfigurer, got %d", fullReloadCalled)
	}
	if len(rec.called) != 1 {
		t.Errorf("expected ReconfigureModules called once, got %d", len(rec.called))
	}
}

func TestConfigReloader_NoEffectiveChanges(t *testing.T) {
	initial := makeWorkflowConfig(
		[]ModuleConfig{{Name: "alpha", Type: "http.server", Config: map[string]any{"port": 8080}}},
		nil,
	)

	var fullReloadCalled int
	fullFn := func(cfg *WorkflowConfig) error {
		fullReloadCalled++
		return nil
	}

	rec := &mockReconfigurer{}
	r := newTestReloader(t, initial, fullFn, rec)

	// Identical config.
	sameCfg := makeWorkflowConfig(
		[]ModuleConfig{{Name: "alpha", Type: "http.server", Config: map[string]any{"port": 8080}}},
		nil,
	)
	evt, err := makeChangeEvent(sameCfg)
	if err != nil {
		t.Fatalf("makeChangeEvent: %v", err)
	}

	if err := r.HandleChange(evt); err != nil {
		t.Fatalf("HandleChange: %v", err)
	}

	if fullReloadCalled != 0 {
		t.Errorf("expected 0 full reloads for identical config, got %d", fullReloadCalled)
	}
	if len(rec.called) != 0 {
		t.Errorf("expected 0 ReconfigureModules calls for identical config, got %d", len(rec.called))
	}
}

func TestConfigReloader_FullReloadError(t *testing.T) {
	initial := makeWorkflowConfig(nil, nil)
	sentinel := errors.New("reload failed")

	fullFn := func(cfg *WorkflowConfig) error {
		return sentinel
	}

	r := newTestReloader(t, initial, fullFn, nil)

	newCfg := makeWorkflowConfig(nil, map[string]any{"flow1": "new"})
	evt, err := makeChangeEvent(newCfg)
	if err != nil {
		t.Fatalf("makeChangeEvent: %v", err)
	}

	err = r.HandleChange(evt)
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error from HandleChange, got %v", err)
	}
}
