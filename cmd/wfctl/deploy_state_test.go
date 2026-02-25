package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/config"
)

// --- SaveState / LoadState round-trip ---

func TestSaveAndLoadState(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deployment.state.json")

	original := &DeploymentState{
		Version:       "1",
		ConfigHash:    "sha256:abc123",
		DeployedAt:    time.Now().UTC().Truncate(time.Second),
		ConfigFile:    "app.yaml",
		SchemaVersion: 1,
		Migrations:    []string{"001_initial"},
		Resources: DeployedResources{
			Modules: map[string]DeployedModuleState{
				"orders-db": {
					Type:       "storage.sqlite",
					Stateful:   true,
					ResourceID: "database/orders-db",
					Config:     map[string]any{"dbPath": "/data/orders.db"},
				},
			},
			Pipelines: map[string]DeployedPipelineState{
				"create-order": {Trigger: "http", Path: "/api/v1/orders", Method: "POST"},
			},
		},
	}

	if err := SaveState(original, path); err != nil {
		t.Fatalf("SaveState failed: %v", err)
	}

	loaded, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}

	if loaded.Version != original.Version {
		t.Errorf("Version: got %q, want %q", loaded.Version, original.Version)
	}
	if loaded.ConfigHash != original.ConfigHash {
		t.Errorf("ConfigHash: got %q, want %q", loaded.ConfigHash, original.ConfigHash)
	}
	if loaded.SchemaVersion != original.SchemaVersion {
		t.Errorf("SchemaVersion: got %d, want %d", loaded.SchemaVersion, original.SchemaVersion)
	}
	if len(loaded.Migrations) != 1 || loaded.Migrations[0] != "001_initial" {
		t.Errorf("Migrations: got %v, want [001_initial]", loaded.Migrations)
	}

	mod, ok := loaded.Resources.Modules["orders-db"]
	if !ok {
		t.Fatal("expected orders-db module in loaded state")
	}
	if !mod.Stateful {
		t.Error("expected orders-db to be stateful")
	}
	if mod.ResourceID != "database/orders-db" {
		t.Errorf("ResourceID: got %q, want %q", mod.ResourceID, "database/orders-db")
	}

	pl, ok := loaded.Resources.Pipelines["create-order"]
	if !ok {
		t.Fatal("expected create-order pipeline in loaded state")
	}
	if pl.Method != "POST" {
		t.Errorf("Pipeline method: got %q, want POST", pl.Method)
	}
}

func TestLoadStateMissingFile(t *testing.T) {
	_, err := LoadState("/nonexistent/path/deployment.state.json")
	if err == nil {
		t.Fatal("expected error for missing state file")
	}
}

func TestLoadStateInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("not json {{{"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadState(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// --- BuildStateFromConfig ---

func TestBuildStateFromConfigModules(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "orders-db", Type: "storage.sqlite", Config: map[string]any{"dbPath": "/data/orders.db"}},
			{Name: "event-broker", Type: "messaging.broker"},
			{Name: "http-server", Type: "http.server", Config: map[string]any{"address": ":8080"}},
			{Name: "cache", Type: "cache.redis"},
		},
		Pipelines: map[string]any{},
	}

	state, err := BuildStateFromConfig(cfg, "", "prod", nil)
	if err != nil {
		t.Fatalf("BuildStateFromConfig failed: %v", err)
	}

	if state.Version != "1" {
		t.Errorf("Version: got %q, want 1", state.Version)
	}
	if state.SchemaVersion != 1 {
		t.Errorf("SchemaVersion: got %d, want 1", state.SchemaVersion)
	}

	ordersDB := state.Resources.Modules["orders-db"]
	if !ordersDB.Stateful {
		t.Error("orders-db should be stateful")
	}
	if ordersDB.ResourceID != "database/prod-orders-db" {
		t.Errorf("orders-db ResourceID: got %q, want %q", ordersDB.ResourceID, "database/prod-orders-db")
	}

	broker := state.Resources.Modules["event-broker"]
	if !broker.Stateful {
		t.Error("event-broker should be stateful")
	}

	server := state.Resources.Modules["http-server"]
	if server.Stateful {
		t.Error("http-server should NOT be stateful")
	}
	if server.ResourceID != "" {
		t.Errorf("http-server should have no ResourceID, got %q", server.ResourceID)
	}

	cache := state.Resources.Modules["cache"]
	if cache.Stateful {
		t.Error("cache.redis should NOT be stateful (ephemeral by default)")
	}
}

func TestBuildStateFromConfigPipelines(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{},
		Pipelines: map[string]any{
			"create-order": map[string]any{
				"trigger": map[string]any{
					"type": "http",
					"config": map[string]any{
						"path":   "/api/v1/orders",
						"method": "POST",
					},
				},
				"steps": []any{},
			},
			"list-orders": map[string]any{
				"trigger": map[string]any{
					"type": "http",
					"config": map[string]any{
						"path":   "/api/v1/orders",
						"method": "GET",
					},
				},
				"steps": []any{},
			},
		},
	}

	state, err := BuildStateFromConfig(cfg, "", "", nil)
	if err != nil {
		t.Fatalf("BuildStateFromConfig failed: %v", err)
	}

	if len(state.Resources.Pipelines) != 2 {
		t.Fatalf("expected 2 pipelines, got %d", len(state.Resources.Pipelines))
	}

	createOrder := state.Resources.Pipelines["create-order"]
	if createOrder.Trigger != "http" {
		t.Errorf("create-order Trigger: got %q, want http", createOrder.Trigger)
	}
	if createOrder.Path != "/api/v1/orders" {
		t.Errorf("create-order Path: got %q, want /api/v1/orders", createOrder.Path)
	}
	if createOrder.Method != "POST" {
		t.Errorf("create-order Method: got %q, want POST", createOrder.Method)
	}
}

func TestBuildStateFromConfigWithFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "app.yaml")
	if err := os.WriteFile(cfgPath, []byte("modules: []\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.WorkflowConfig{
		Modules:   []config.ModuleConfig{},
		Pipelines: map[string]any{},
	}

	state, err := BuildStateFromConfig(cfg, cfgPath, "", nil)
	if err != nil {
		t.Fatalf("BuildStateFromConfig failed: %v", err)
	}

	if state.ConfigFile != cfgPath {
		t.Errorf("ConfigFile: got %q, want %q", state.ConfigFile, cfgPath)
	}
	if state.ConfigHash == "" {
		t.Error("expected non-empty ConfigHash when config file exists")
	}
	if len(state.ConfigHash) < 8 || state.ConfigHash[:7] != "sha256:" {
		t.Errorf("ConfigHash should start with sha256:, got %q", state.ConfigHash)
	}
}

func TestBuildStateFromConfigMigrations(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules:   []config.ModuleConfig{},
		Pipelines: map[string]any{},
	}
	migrations := []string{"001_initial", "002_add_index"}
	state, err := BuildStateFromConfig(cfg, "", "", migrations)
	if err != nil {
		t.Fatalf("BuildStateFromConfig failed: %v", err)
	}
	if len(state.Migrations) != 2 {
		t.Fatalf("expected 2 migrations, got %d", len(state.Migrations))
	}
}

func TestBuildStateConfigCopyIsolation(t *testing.T) {
	originalConfig := map[string]any{"dbPath": "/data/orders.db"}
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "db", Type: "storage.sqlite", Config: originalConfig},
		},
		Pipelines: map[string]any{},
	}

	state, err := BuildStateFromConfig(cfg, "", "", nil)
	if err != nil {
		t.Fatalf("BuildStateFromConfig failed: %v", err)
	}

	// Mutate the original config — the state copy should be unaffected.
	originalConfig["dbPath"] = "/data/mutated.db"

	snapshotPath, _ := state.Resources.Modules["db"].Config["dbPath"].(string)
	if snapshotPath != "/data/orders.db" {
		t.Errorf("state config was mutated: got %q, want /data/orders.db", snapshotPath)
	}
}

func TestSaveStateIsValidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "deployment.state.json")

	state := &DeploymentState{
		Version:       "1",
		SchemaVersion: 1,
		Resources: DeployedResources{
			Modules:   make(map[string]DeployedModuleState),
			Pipelines: make(map[string]DeployedPipelineState),
		},
	}

	if err := SaveState(state, path); err != nil {
		t.Fatalf("SaveState failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("saved state is not valid JSON: %v", err)
	}
}
