package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

// --- diffConfigs tests ---

func TestDiffConfigsModuleAdded(t *testing.T) {
	oldCfg := cfgWithModules(
		config.ModuleConfig{Name: "server", Type: "http.server"},
	)
	newCfg := cfgWithModules(
		config.ModuleConfig{Name: "server", Type: "http.server"},
		config.ModuleConfig{Name: "new-cache", Type: "cache.redis"},
	)

	result := diffConfigs(oldCfg, newCfg, nil)

	mod := findModuleDiff(t, result, "new-cache")
	if mod.Status != DiffStatusAdded {
		t.Errorf("new-cache status: got %q, want %q", mod.Status, DiffStatusAdded)
	}
}

func TestDiffConfigsModuleRemoved(t *testing.T) {
	oldCfg := cfgWithModules(
		config.ModuleConfig{Name: "server", Type: "http.server"},
		config.ModuleConfig{Name: "old-module", Type: "http.middleware.cors"},
	)
	newCfg := cfgWithModules(
		config.ModuleConfig{Name: "server", Type: "http.server"},
	)

	result := diffConfigs(oldCfg, newCfg, nil)

	mod := findModuleDiff(t, result, "old-module")
	if mod.Status != DiffStatusRemoved {
		t.Errorf("old-module status: got %q, want %q", mod.Status, DiffStatusRemoved)
	}
	if !strings.Contains(mod.Detail, "stateless") {
		t.Errorf("expected 'stateless' in detail for cors module, got: %s", mod.Detail)
	}
}

func TestDiffConfigsStatefulModuleRemoved(t *testing.T) {
	oldCfg := cfgWithModules(
		config.ModuleConfig{Name: "orders-db", Type: "storage.sqlite"},
	)
	newCfg := cfgWithModules()

	result := diffConfigs(oldCfg, newCfg, nil)

	mod := findModuleDiff(t, result, "orders-db")
	if mod.Status != DiffStatusRemoved {
		t.Errorf("orders-db status: got %q, want %q", mod.Status, DiffStatusRemoved)
	}
	if !mod.Stateful {
		t.Error("expected orders-db to be flagged as stateful")
	}
	if !strings.Contains(mod.Detail, "WARNING") {
		t.Errorf("expected WARNING in detail for stateful removal, got: %s", mod.Detail)
	}
}

func TestDiffConfigsModuleUnchanged(t *testing.T) {
	mod := config.ModuleConfig{Name: "server", Type: "http.server", Config: map[string]any{"address": ":8080"}}
	oldCfg := cfgWithModules(mod)
	newCfg := cfgWithModules(mod)

	result := diffConfigs(oldCfg, newCfg, nil)

	diff := findModuleDiff(t, result, "server")
	if diff.Status != DiffStatusUnchanged {
		t.Errorf("server status: got %q, want %q", diff.Status, DiffStatusUnchanged)
	}
}

func TestDiffConfigsModuleConfigChanged(t *testing.T) {
	oldCfg := cfgWithModules(
		config.ModuleConfig{Name: "server", Type: "http.server", Config: map[string]any{"address": ":8080"}},
	)
	newCfg := cfgWithModules(
		config.ModuleConfig{Name: "server", Type: "http.server", Config: map[string]any{"address": ":9090"}},
	)

	result := diffConfigs(oldCfg, newCfg, nil)

	diff := findModuleDiff(t, result, "server")
	if diff.Status != DiffStatusChanged {
		t.Errorf("server status: got %q, want %q", diff.Status, DiffStatusChanged)
	}
}

func TestDiffConfigsStatefulModuleConfigChanged(t *testing.T) {
	oldCfg := cfgWithModules(
		config.ModuleConfig{
			Name:   "orders-db",
			Type:   "storage.sqlite",
			Config: map[string]any{"dbPath": "/data/old.db"},
		},
	)
	newCfg := cfgWithModules(
		config.ModuleConfig{
			Name:   "orders-db",
			Type:   "storage.sqlite",
			Config: map[string]any{"dbPath": "/data/new.db"},
		},
	)

	result := diffConfigs(oldCfg, newCfg, nil)

	diff := findModuleDiff(t, result, "orders-db")
	if diff.Status != DiffStatusChanged {
		t.Errorf("orders-db status: got %q, want %q", diff.Status, DiffStatusChanged)
	}
	if len(diff.BreakingChanges) == 0 {
		t.Error("expected breaking changes for stateful module config change")
	}
	if len(result.BreakingChanges) == 0 {
		t.Error("expected top-level breaking changes")
	}
}

func TestDiffConfigsModuleTypeChanged(t *testing.T) {
	oldCfg := cfgWithModules(
		config.ModuleConfig{Name: "db", Type: "storage.sqlite"},
	)
	newCfg := cfgWithModules(
		config.ModuleConfig{Name: "db", Type: "database.workflow"},
	)

	result := diffConfigs(oldCfg, newCfg, nil)

	diff := findModuleDiff(t, result, "db")
	if diff.Status != DiffStatusChanged {
		t.Errorf("db status: got %q, want %q", diff.Status, DiffStatusChanged)
	}
	if !strings.Contains(diff.Detail, "TYPE CHANGED") {
		t.Errorf("expected TYPE CHANGED in detail, got: %s", diff.Detail)
	}
}

// --- Pipeline diff tests ---

func TestDiffConfigsPipelineAdded(t *testing.T) {
	oldCfg := cfgWithPipelines()
	newCfg := cfgWithPipelines(
		"create-order", httpPipeline("POST", "/api/v1/orders", 3),
	)

	result := diffConfigs(oldCfg, newCfg, nil)

	pl := findPipelineDiff(t, result, "create-order")
	if pl.Status != DiffStatusAdded {
		t.Errorf("create-order status: got %q, want %q", pl.Status, DiffStatusAdded)
	}
}

func TestDiffConfigsPipelineRemoved(t *testing.T) {
	oldCfg := cfgWithPipelines(
		"legacy", httpPipeline("GET", "/api/v1/legacy", 1),
	)
	newCfg := cfgWithPipelines()

	result := diffConfigs(oldCfg, newCfg, nil)

	pl := findPipelineDiff(t, result, "legacy")
	if pl.Status != DiffStatusRemoved {
		t.Errorf("legacy status: got %q, want %q", pl.Status, DiffStatusRemoved)
	}
}

func TestDiffConfigsPipelineUnchanged(t *testing.T) {
	p := httpPipeline("GET", "/api/v1/orders", 2)
	oldCfg := cfgWithPipelines("list-orders", p)
	newCfg := cfgWithPipelines("list-orders", p)

	result := diffConfigs(oldCfg, newCfg, nil)

	pl := findPipelineDiff(t, result, "list-orders")
	if pl.Status != DiffStatusUnchanged {
		t.Errorf("list-orders status: got %q, want %q", pl.Status, DiffStatusUnchanged)
	}
}

func TestDiffConfigsPipelineStepsChanged(t *testing.T) {
	oldCfg := cfgWithPipelines("create-order", httpPipeline("POST", "/api/v1/orders", 3))
	newCfg := cfgWithPipelines("create-order", httpPipeline("POST", "/api/v1/orders", 5))

	result := diffConfigs(oldCfg, newCfg, nil)

	pl := findPipelineDiff(t, result, "create-order")
	if pl.Status != DiffStatusChanged {
		t.Errorf("create-order status: got %q, want %q", pl.Status, DiffStatusChanged)
	}
	if !strings.Contains(pl.Detail, "STEPS CHANGED") {
		t.Errorf("expected STEPS CHANGED in detail, got: %s", pl.Detail)
	}
	if !strings.Contains(pl.Detail, "3 → 5") {
		t.Errorf("expected step counts 3 → 5 in detail, got: %s", pl.Detail)
	}
}

func TestDiffConfigsPipelineTriggerChanged(t *testing.T) {
	oldCfg := cfgWithPipelines("my-pipeline", httpPipeline("GET", "/old-path", 2))
	newCfg := cfgWithPipelines("my-pipeline", httpPipeline("GET", "/new-path", 2))

	result := diffConfigs(oldCfg, newCfg, nil)

	pl := findPipelineDiff(t, result, "my-pipeline")
	if pl.Status != DiffStatusChanged {
		t.Errorf("my-pipeline status: got %q, want %q", pl.Status, DiffStatusChanged)
	}
	if !strings.Contains(pl.Detail, "TRIGGER CHANGED") {
		t.Errorf("expected TRIGGER CHANGED in detail, got: %s", pl.Detail)
	}
}

// --- State correlation ---

func TestDiffConfigsStateCorrelation(t *testing.T) {
	state := &DeploymentState{
		Resources: DeployedResources{
			Modules: map[string]DeployedModuleState{
				"orders-db": {
					Type:       "storage.sqlite",
					Stateful:   true,
					ResourceID: "database/prod-orders-db",
				},
			},
		},
	}

	oldCfg := cfgWithModules(
		config.ModuleConfig{Name: "orders-db", Type: "storage.sqlite"},
	)
	newCfg := cfgWithModules(
		config.ModuleConfig{Name: "orders-db", Type: "storage.sqlite"},
	)

	result := diffConfigs(oldCfg, newCfg, state)

	diff := findModuleDiff(t, result, "orders-db")
	if diff.ResourceID != "database/prod-orders-db" {
		t.Errorf("ResourceID: got %q, want database/prod-orders-db", diff.ResourceID)
	}
}

// --- runDiff integration ---

func TestRunDiffMissingArgs(t *testing.T) {
	err := runDiff([]string{})
	if err == nil {
		t.Fatal("expected error when no args given")
	}
}

func TestRunDiffMissingSecondArg(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.yaml")
	writeTestConfigFile(t, p, simpleConfig())
	err := runDiff([]string{p})
	if err == nil {
		t.Fatal("expected error when second config missing")
	}
}

func TestRunDiffText(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.yaml")
	newPath := filepath.Join(dir, "new.yaml")

	writeTestConfigFile(t, oldPath, `
modules:
  - name: server
    type: http.server
  - name: orders-db
    type: storage.sqlite
    config:
      dbPath: /data/orders.db
`)
	writeTestConfigFile(t, newPath, `
modules:
  - name: server
    type: http.server
  - name: orders-db
    type: storage.sqlite
    config:
      dbPath: /data/new.db
  - name: new-cache
    type: cache.redis
`)

	if err := runDiff([]string{oldPath, newPath}); err != nil {
		t.Fatalf("runDiff failed: %v", err)
	}
}

func TestRunDiffJSON(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.yaml")
	newPath := filepath.Join(dir, "new.yaml")

	writeTestConfigFile(t, oldPath, simpleConfig())
	writeTestConfigFile(t, newPath, simpleConfig())

	// Redirect stdout capture is tricky; just verify it runs without error.
	if err := runDiff([]string{"-format", "json", oldPath, newPath}); err != nil {
		t.Fatalf("runDiff -format json failed: %v", err)
	}
}

func TestRunDiffCheckBreaking(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.yaml")
	newPath := filepath.Join(dir, "new.yaml")

	writeTestConfigFile(t, oldPath, `
modules:
  - name: orders-db
    type: storage.sqlite
    config:
      dbPath: /data/old.db
`)
	writeTestConfigFile(t, newPath, `
modules:
  - name: orders-db
    type: storage.sqlite
    config:
      dbPath: /data/new.db
`)

	err := runDiff([]string{"-check-breaking", oldPath, newPath})
	if err == nil {
		t.Fatal("expected error from -check-breaking with breaking changes")
	}
	if !strings.Contains(err.Error(), "breaking change") {
		t.Errorf("expected breaking change error, got: %v", err)
	}
}

func TestRunDiffNoBreakingChanges(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.yaml")
	newPath := filepath.Join(dir, "new.yaml")

	writeTestConfigFile(t, oldPath, simpleConfig())
	writeTestConfigFile(t, newPath, simpleConfig())

	// With -check-breaking and no breaking changes, should succeed.
	if err := runDiff([]string{"-check-breaking", oldPath, newPath}); err != nil {
		t.Fatalf("expected no error when no breaking changes, got: %v", err)
	}
}

func TestRunDiffMissingStateFile(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.yaml")
	newPath := filepath.Join(dir, "new.yaml")
	writeTestConfigFile(t, oldPath, simpleConfig())
	writeTestConfigFile(t, newPath, simpleConfig())

	err := runDiff([]string{"-state", "/nonexistent/state.json", oldPath, newPath})
	if err == nil {
		t.Fatal("expected error for missing state file")
	}
}

// --- describePipelineTrigger ---

func TestDescribePipelineTrigger(t *testing.T) {
	p := httpPipeline("POST", "/api/v1/orders", 2)
	got := describePipelineTrigger(p)
	if !strings.Contains(got, "http") {
		t.Errorf("expected 'http' in trigger description, got: %s", got)
	}
	if !strings.Contains(got, "POST") {
		t.Errorf("expected 'POST' in trigger description, got: %s", got)
	}
	if !strings.Contains(got, "/api/v1/orders") {
		t.Errorf("expected path in trigger description, got: %s", got)
	}
}

func TestDescribePipelineTriggerNil(t *testing.T) {
	got := describePipelineTrigger(nil)
	if got == "" {
		t.Error("expected non-empty description for nil pipeline")
	}
}

// --- JSON roundtrip for DiffResult ---

func TestDiffResultJSONRoundtrip(t *testing.T) {
	result := DiffResult{
		OldConfig: "old.yaml",
		NewConfig: "new.yaml",
		Modules: []ModuleDiff{
			{Name: "server", Status: DiffStatusUnchanged, Type: "http.server"},
		},
		Pipelines: []PipelineDiff{
			{Name: "create-order", Status: DiffStatusAdded, Trigger: "http POST /orders"},
		},
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal DiffResult: %v", err)
	}

	var decoded DiffResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal DiffResult: %v", err)
	}

	if decoded.Modules[0].Name != "server" {
		t.Errorf("expected server module, got %q", decoded.Modules[0].Name)
	}
	if decoded.Pipelines[0].Status != DiffStatusAdded {
		t.Errorf("expected added pipeline, got %q", decoded.Pipelines[0].Status)
	}
}

// --- Helpers ---

func cfgWithModules(mods ...config.ModuleConfig) *config.WorkflowConfig {
	return &config.WorkflowConfig{
		Modules:   mods,
		Pipelines: map[string]any{},
		Workflows: map[string]any{},
		Triggers:  map[string]any{},
	}
}

func cfgWithPipelines(pairs ...any) *config.WorkflowConfig {
	pipelines := map[string]any{}
	for i := 0; i+1 < len(pairs); i += 2 {
		name, _ := pairs[i].(string)
		val := pairs[i+1]
		pipelines[name] = val
	}
	return &config.WorkflowConfig{
		Modules:   []config.ModuleConfig{},
		Pipelines: pipelines,
		Workflows: map[string]any{},
		Triggers:  map[string]any{},
	}
}

// httpPipeline builds a raw pipeline map with an HTTP trigger and n stub steps.
func httpPipeline(method, path string, n int) map[string]any {
	steps := make([]any, n)
	for i := range steps {
		steps[i] = map[string]any{"name": "step", "type": "step.noop"}
	}
	return map[string]any{
		"trigger": map[string]any{
			"type": "http",
			"config": map[string]any{
				"method": method,
				"path":   path,
			},
		},
		"steps": steps,
	}
}

func findModuleDiff(t *testing.T, result DiffResult, name string) ModuleDiff {
	t.Helper()
	for _, m := range result.Modules {
		if m.Name == name {
			return m
		}
	}
	t.Fatalf("module %q not found in diff result", name)
	return ModuleDiff{}
}

func findPipelineDiff(t *testing.T, result DiffResult, name string) PipelineDiff {
	t.Helper()
	for _, p := range result.Pipelines {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("pipeline %q not found in diff result", name)
	return PipelineDiff{}
}

func writeTestConfigFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write test config %q: %v", path, err)
	}
}

func simpleConfig() string {
	return `
modules:
  - name: server
    type: http.server
    config:
      address: ":8080"
`
}
