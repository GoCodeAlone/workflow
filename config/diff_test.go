package config

import (
	"testing"
)

func makeConfig(modules []ModuleConfig, workflows, triggers, pipelines map[string]any) *WorkflowConfig {
	return &WorkflowConfig{
		Modules:   modules,
		Workflows: workflows,
		Triggers:  triggers,
		Pipelines: pipelines,
	}
}

func TestDiffModuleConfigs_NoChanges(t *testing.T) {
	modules := []ModuleConfig{
		{Name: "alpha", Type: "http.server", Config: map[string]any{"port": 8080}},
		{Name: "beta", Type: "http.router"},
	}
	old := makeConfig(modules, nil, nil, nil)
	new := makeConfig(modules, nil, nil, nil)

	diff := DiffModuleConfigs(old, new)
	if len(diff.Added) != 0 {
		t.Errorf("expected 0 added, got %d", len(diff.Added))
	}
	if len(diff.Removed) != 0 {
		t.Errorf("expected 0 removed, got %d", len(diff.Removed))
	}
	if len(diff.Modified) != 0 {
		t.Errorf("expected 0 modified, got %d", len(diff.Modified))
	}
	if len(diff.Unchanged) != 2 {
		t.Errorf("expected 2 unchanged, got %d", len(diff.Unchanged))
	}
}

func TestDiffModuleConfigs_AddedModule(t *testing.T) {
	old := makeConfig([]ModuleConfig{
		{Name: "alpha", Type: "http.server"},
	}, nil, nil, nil)
	new := makeConfig([]ModuleConfig{
		{Name: "alpha", Type: "http.server"},
		{Name: "beta", Type: "http.router"},
	}, nil, nil, nil)

	diff := DiffModuleConfigs(old, new)
	if len(diff.Added) != 1 {
		t.Fatalf("expected 1 added, got %d", len(diff.Added))
	}
	if diff.Added[0].Name != "beta" {
		t.Errorf("expected added module 'beta', got %q", diff.Added[0].Name)
	}
	if len(diff.Removed) != 0 {
		t.Errorf("expected 0 removed, got %d", len(diff.Removed))
	}
	if len(diff.Modified) != 0 {
		t.Errorf("expected 0 modified, got %d", len(diff.Modified))
	}
}

func TestDiffModuleConfigs_RemovedModule(t *testing.T) {
	old := makeConfig([]ModuleConfig{
		{Name: "alpha", Type: "http.server"},
		{Name: "beta", Type: "http.router"},
	}, nil, nil, nil)
	new := makeConfig([]ModuleConfig{
		{Name: "alpha", Type: "http.server"},
	}, nil, nil, nil)

	diff := DiffModuleConfigs(old, new)
	if len(diff.Removed) != 1 {
		t.Fatalf("expected 1 removed, got %d", len(diff.Removed))
	}
	if diff.Removed[0].Name != "beta" {
		t.Errorf("expected removed module 'beta', got %q", diff.Removed[0].Name)
	}
	if len(diff.Added) != 0 {
		t.Errorf("expected 0 added, got %d", len(diff.Added))
	}
}

func TestDiffModuleConfigs_ModifiedModule(t *testing.T) {
	old := makeConfig([]ModuleConfig{
		{Name: "alpha", Type: "http.server", Config: map[string]any{"port": 8080}},
	}, nil, nil, nil)
	new := makeConfig([]ModuleConfig{
		{Name: "alpha", Type: "http.server", Config: map[string]any{"port": 9090}},
	}, nil, nil, nil)

	diff := DiffModuleConfigs(old, new)
	if len(diff.Modified) != 1 {
		t.Fatalf("expected 1 modified, got %d", len(diff.Modified))
	}
	change := diff.Modified[0]
	if change.Name != "alpha" {
		t.Errorf("expected modified module 'alpha', got %q", change.Name)
	}
	if change.OldConfig["port"] != 8080 {
		t.Errorf("expected old port 8080, got %v", change.OldConfig["port"])
	}
	if change.NewConfig["port"] != 9090 {
		t.Errorf("expected new port 9090, got %v", change.NewConfig["port"])
	}
	if len(diff.Added) != 0 {
		t.Errorf("expected 0 added, got %d", len(diff.Added))
	}
	if len(diff.Removed) != 0 {
		t.Errorf("expected 0 removed, got %d", len(diff.Removed))
	}
	if len(diff.Unchanged) != 0 {
		t.Errorf("expected 0 unchanged, got %d", len(diff.Unchanged))
	}
}

func TestDiffModuleConfigs_Mixed(t *testing.T) {
	old := makeConfig([]ModuleConfig{
		{Name: "keep", Type: "http.server", Config: map[string]any{"port": 8080}},
		{Name: "modify", Type: "cache", Config: map[string]any{"ttl": 60}},
		{Name: "remove", Type: "db"},
	}, nil, nil, nil)
	new := makeConfig([]ModuleConfig{
		{Name: "keep", Type: "http.server", Config: map[string]any{"port": 8080}},
		{Name: "modify", Type: "cache", Config: map[string]any{"ttl": 120}},
		{Name: "add", Type: "queue"},
	}, nil, nil, nil)

	diff := DiffModuleConfigs(old, new)

	if len(diff.Added) != 1 || diff.Added[0].Name != "add" {
		t.Errorf("expected 1 added ('add'), got %v", diff.Added)
	}
	if len(diff.Removed) != 1 || diff.Removed[0].Name != "remove" {
		t.Errorf("expected 1 removed ('remove'), got %v", diff.Removed)
	}
	if len(diff.Modified) != 1 || diff.Modified[0].Name != "modify" {
		t.Errorf("expected 1 modified ('modify'), got %v", diff.Modified)
	}
	if len(diff.Unchanged) != 1 || diff.Unchanged[0] != "keep" {
		t.Errorf("expected 1 unchanged ('keep'), got %v", diff.Unchanged)
	}
}

func TestHasNonModuleChanges_NoChanges(t *testing.T) {
	wf := map[string]any{"flow1": map[string]any{"initial": "start"}}
	tr := map[string]any{"t1": map[string]any{"type": "http"}}
	pl := map[string]any{"p1": map[string]any{"steps": []any{}}}

	old := &WorkflowConfig{Workflows: wf, Triggers: tr, Pipelines: pl}
	new := &WorkflowConfig{Workflows: wf, Triggers: tr, Pipelines: pl}

	if HasNonModuleChanges(old, new) {
		t.Error("expected no non-module changes for identical configs")
	}
}

func TestHasNonModuleChanges_WorkflowChanged(t *testing.T) {
	old := &WorkflowConfig{
		Workflows: map[string]any{"flow1": map[string]any{"initial": "start"}},
	}
	new := &WorkflowConfig{
		Workflows: map[string]any{"flow1": map[string]any{"initial": "running"}},
	}

	if !HasNonModuleChanges(old, new) {
		t.Error("expected non-module changes when workflow differs")
	}
}

func TestHasNonModuleChanges_TriggerChanged(t *testing.T) {
	old := &WorkflowConfig{
		Triggers: map[string]any{"t1": map[string]any{"type": "http", "path": "/old"}},
	}
	new := &WorkflowConfig{
		Triggers: map[string]any{"t1": map[string]any{"type": "http", "path": "/new"}},
	}

	if !HasNonModuleChanges(old, new) {
		t.Error("expected non-module changes when trigger differs")
	}
}

func TestHasNonModuleChanges_PipelineChanged(t *testing.T) {
	old := &WorkflowConfig{
		Pipelines: map[string]any{"p1": map[string]any{"steps": []any{"a"}}},
	}
	new := &WorkflowConfig{
		Pipelines: map[string]any{"p1": map[string]any{"steps": []any{"a", "b"}}},
	}

	if !HasNonModuleChanges(old, new) {
		t.Error("expected non-module changes when pipeline differs")
	}
}
