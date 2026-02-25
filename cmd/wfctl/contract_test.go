package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func TestRunContractMissingSubcommand(t *testing.T) {
	err := runContract([]string{})
	if err == nil {
		t.Fatal("expected error when no subcommand given")
	}
}

func TestRunContractUnknownSubcommand(t *testing.T) {
	err := runContract([]string{"unknown"})
	if err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
}

func TestRunContractTestMissingConfig(t *testing.T) {
	err := runContractTest([]string{})
	if err == nil {
		t.Fatal("expected error when no config given")
	}
}

const contractTestConfig = `
pipelines:
  list-items:
    trigger:
      type: http
      config:
        path: /api/items
        method: GET
    steps:
      - name: parse
        type: step.request_parse
      - name: respond
        type: step.json_response
        config:
          status: 200

  create-item:
    trigger:
      type: http
      config:
        path: /api/items
        method: POST
    steps:
      - name: auth
        type: step.auth_required
      - name: validate
        type: step.validate
      - name: respond
        type: step.json_response
        config:
          status: 201

  process-event:
    trigger:
      type: event
      config:
        topic: item.created
    steps:
      - name: log
        type: step.log
      - name: publish
        type: step.publish
        config:
          topic: item.processed

modules:
  - name: db
    type: storage.sqlite
    config:
      dbPath: data/test.db
`

func TestRunContractTestGenerate(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(configPath, []byte(contractTestConfig), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	if err := runContractTest([]string{configPath}); err != nil {
		t.Fatalf("expected contract generation to succeed, got: %v", err)
	}
}

func TestRunContractTestGenerateJSON(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(configPath, []byte(contractTestConfig), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	if err := runContractTest([]string{"-format", "json", configPath}); err != nil {
		t.Fatalf("expected json output to work, got: %v", err)
	}
}

func TestRunContractTestWriteOutput(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "workflow.yaml")
	outputPath := filepath.Join(dir, "contract.json")

	if err := os.WriteFile(configPath, []byte(contractTestConfig), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	if err := runContractTest([]string{"-output", outputPath, configPath}); err != nil {
		t.Fatalf("expected contract generation to succeed, got: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}

	var contract Contract
	if err := json.Unmarshal(data, &contract); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if contract.ConfigHash == "" {
		t.Error("expected ConfigHash to be set")
	}
	if contract.GeneratedAt == "" {
		t.Error("expected GeneratedAt to be set")
	}
}

func TestGenerateContractEndpoints(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Pipelines: map[string]any{
			"get-items": map[string]any{
				"trigger": map[string]any{
					"type": "http",
					"config": map[string]any{
						"path":   "/api/items",
						"method": "GET",
					},
				},
				"steps": []any{},
			},
			"create-item": map[string]any{
				"trigger": map[string]any{
					"type": "http",
					"config": map[string]any{
						"path":   "/api/items",
						"method": "POST",
					},
				},
				"steps": []any{
					map[string]any{
						"name": "auth",
						"type": "step.auth_required",
					},
				},
			},
		},
	}

	contract := generateContract(cfg)

	if len(contract.Endpoints) != 2 {
		t.Fatalf("expected 2 endpoints, got %d", len(contract.Endpoints))
	}

	// Find GET endpoint
	var getEP, postEP *EndpointContract
	for i := range contract.Endpoints {
		if contract.Endpoints[i].Method == "GET" {
			getEP = &contract.Endpoints[i]
		}
		if contract.Endpoints[i].Method == "POST" {
			postEP = &contract.Endpoints[i]
		}
	}

	if getEP == nil {
		t.Error("expected GET endpoint")
	} else if getEP.AuthRequired {
		t.Error("expected GET endpoint to not require auth")
	}

	if postEP == nil {
		t.Error("expected POST endpoint")
	} else if !postEP.AuthRequired {
		t.Error("expected POST endpoint to require auth")
	}
}

func TestGenerateContractModules(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "db", Type: "storage.sqlite"},
			{Name: "cache", Type: "cache.redis"},
		},
	}

	contract := generateContract(cfg)

	if len(contract.Modules) != 2 {
		t.Fatalf("expected 2 modules, got %d", len(contract.Modules))
	}

	// Find sqlite module and check stateful
	var sqliteMod *ModuleContract
	for i := range contract.Modules {
		if contract.Modules[i].Type == "storage.sqlite" {
			sqliteMod = &contract.Modules[i]
		}
	}
	if sqliteMod == nil {
		t.Fatal("expected storage.sqlite module")
	}
	if !sqliteMod.Stateful {
		t.Error("expected storage.sqlite to be stateful")
	}
}

func TestGenerateContractEvents(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Pipelines: map[string]any{
			"publisher": map[string]any{
				"trigger": map[string]any{
					"type": "http",
					"config": map[string]any{
						"path":   "/publish",
						"method": "POST",
					},
				},
				"steps": []any{
					map[string]any{
						"type": "step.publish",
						"config": map[string]any{
							"topic": "order.created",
						},
					},
				},
			},
			"subscriber": map[string]any{
				"trigger": map[string]any{
					"type": "event",
					"config": map[string]any{
						"topic": "order.created",
					},
				},
				"steps": []any{},
			},
		},
	}

	contract := generateContract(cfg)

	if len(contract.Events) != 2 {
		t.Fatalf("expected 2 events, got %d; events: %v", len(contract.Events), contract.Events)
	}

	foundPublish := false
	foundSubscribe := false
	for _, e := range contract.Events {
		if e.Topic == "order.created" && e.Direction == "publish" {
			foundPublish = true
		}
		if e.Topic == "order.created" && e.Direction == "subscribe" {
			foundSubscribe = true
		}
	}
	if !foundPublish {
		t.Error("expected publish event for order.created")
	}
	if !foundSubscribe {
		t.Error("expected subscribe event for order.created")
	}
}

func TestCompareContractsNoChanges(t *testing.T) {
	cfg := &config.WorkflowConfig{
		Pipelines: map[string]any{
			"get-items": map[string]any{
				"trigger": map[string]any{
					"type": "http",
					"config": map[string]any{
						"path":   "/api/items",
						"method": "GET",
					},
				},
				"steps": []any{},
			},
		},
		Modules: []config.ModuleConfig{
			{Name: "db", Type: "storage.sqlite"},
		},
	}

	base := generateContract(cfg)
	current := generateContract(cfg)

	comp := compareContracts(base, current)
	if comp.BreakingCount != 0 {
		t.Errorf("expected no breaking changes, got %d", comp.BreakingCount)
	}
}

func TestCompareContractsEndpointAdded(t *testing.T) {
	baseCfg := &config.WorkflowConfig{
		Pipelines: map[string]any{
			"get-items": map[string]any{
				"trigger": map[string]any{
					"type": "http",
					"config": map[string]any{
						"path":   "/api/items",
						"method": "GET",
					},
				},
				"steps": []any{},
			},
		},
	}

	currentCfg := &config.WorkflowConfig{
		Pipelines: map[string]any{
			"get-items": map[string]any{
				"trigger": map[string]any{
					"type": "http",
					"config": map[string]any{
						"path":   "/api/items",
						"method": "GET",
					},
				},
				"steps": []any{},
			},
			"create-item": map[string]any{
				"trigger": map[string]any{
					"type": "http",
					"config": map[string]any{
						"path":   "/api/items",
						"method": "POST",
					},
				},
				"steps": []any{},
			},
		},
	}

	base := generateContract(baseCfg)
	current := generateContract(currentCfg)
	comp := compareContracts(base, current)

	// Adding an endpoint is not breaking
	if comp.BreakingCount != 0 {
		t.Errorf("expected 0 breaking changes for added endpoint, got %d", comp.BreakingCount)
	}

	// Find the added endpoint
	found := false
	for _, ec := range comp.Endpoints {
		if ec.Method == "POST" && ec.Path == "/api/items" && ec.Change == changeAdded {
			found = true
		}
	}
	if !found {
		t.Error("expected POST /api/items to appear as ADDED")
	}
}

func TestCompareContractsEndpointRemoved(t *testing.T) {
	baseCfg := &config.WorkflowConfig{
		Pipelines: map[string]any{
			"get-items": map[string]any{
				"trigger": map[string]any{
					"type": "http",
					"config": map[string]any{
						"path":   "/api/items",
						"method": "GET",
					},
				},
				"steps": []any{},
			},
			"legacy": map[string]any{
				"trigger": map[string]any{
					"type": "http",
					"config": map[string]any{
						"path":   "/api/legacy",
						"method": "GET",
					},
				},
				"steps": []any{},
			},
		},
	}

	currentCfg := &config.WorkflowConfig{
		Pipelines: map[string]any{
			"get-items": map[string]any{
				"trigger": map[string]any{
					"type": "http",
					"config": map[string]any{
						"path":   "/api/items",
						"method": "GET",
					},
				},
				"steps": []any{},
			},
		},
	}

	base := generateContract(baseCfg)
	current := generateContract(currentCfg)
	comp := compareContracts(base, current)

	// Removing an endpoint is breaking
	if comp.BreakingCount == 0 {
		t.Error("expected breaking change for removed endpoint")
	}

	found := false
	for _, ec := range comp.Endpoints {
		if ec.Path == "/api/legacy" && ec.Change == changeRemoved && ec.IsBreaking {
			found = true
		}
	}
	if !found {
		t.Error("expected /api/legacy to appear as REMOVED and breaking")
	}
}

func TestCompareContractsAuthAdded(t *testing.T) {
	baseCfg := &config.WorkflowConfig{
		Pipelines: map[string]any{
			"get-items": map[string]any{
				"trigger": map[string]any{
					"type": "http",
					"config": map[string]any{
						"path":   "/api/items",
						"method": "GET",
					},
				},
				"steps": []any{},
			},
		},
	}

	currentCfg := &config.WorkflowConfig{
		Pipelines: map[string]any{
			"get-items": map[string]any{
				"trigger": map[string]any{
					"type": "http",
					"config": map[string]any{
						"path":   "/api/items",
						"method": "GET",
					},
				},
				"steps": []any{
					map[string]any{"type": "step.auth_required"},
				},
			},
		},
	}

	base := generateContract(baseCfg)
	current := generateContract(currentCfg)
	comp := compareContracts(base, current)

	// Adding auth to a public endpoint is breaking
	if comp.BreakingCount == 0 {
		t.Error("expected breaking change for auth added to public endpoint")
	}

	found := false
	for _, ec := range comp.Endpoints {
		if ec.Path == "/api/items" && ec.Change == changeChanged && ec.IsBreaking {
			if strings.Contains(ec.Detail, "auth") {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected GET /api/items to appear as CHANGED with auth breaking change")
	}
}

func TestRunContractTestWithBaseline(t *testing.T) {
	dir := t.TempDir()

	// Write config
	configPath := filepath.Join(dir, "workflow.yaml")
	if err := os.WriteFile(configPath, []byte(contractTestConfig), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	// Generate baseline
	baselinePath := filepath.Join(dir, "baseline.json")
	if err := runContractTest([]string{"-output", baselinePath, configPath}); err != nil {
		t.Fatalf("failed to generate baseline: %v", err)
	}

	// Compare against itself (no changes)
	if err := runContractTest([]string{"-baseline", baselinePath, configPath}); err != nil {
		t.Fatalf("expected no breaking changes comparing to same config: %v", err)
	}
}

func TestRunContractTestBreakingChange(t *testing.T) {
	dir := t.TempDir()

	// Original config with public endpoint
	originalConfig := `
pipelines:
  list-items:
    trigger:
      type: http
      config:
        path: /api/items
        method: GET
    steps: []
`
	// Updated config removing the endpoint
	updatedConfig := `
pipelines:
  different-endpoint:
    trigger:
      type: http
      config:
        path: /api/other
        method: GET
    steps: []
`
	originalPath := filepath.Join(dir, "original.yaml")
	updatedPath := filepath.Join(dir, "updated.yaml")
	baselinePath := filepath.Join(dir, "baseline.json")

	if err := os.WriteFile(originalPath, []byte(originalConfig), 0644); err != nil {
		t.Fatalf("failed to write original config: %v", err)
	}
	if err := os.WriteFile(updatedPath, []byte(updatedConfig), 0644); err != nil {
		t.Fatalf("failed to write updated config: %v", err)
	}

	// Generate baseline from original
	if err := runContractTest([]string{"-output", baselinePath, originalPath}); err != nil {
		t.Fatalf("failed to generate baseline: %v", err)
	}

	// Compare updated against baseline - should detect breaking change
	err := runContractTest([]string{"-baseline", baselinePath, updatedPath})
	if err == nil {
		t.Fatal("expected breaking change error")
	}
	if !strings.Contains(err.Error(), "breaking") {
		t.Errorf("expected 'breaking' in error message, got: %v", err)
	}
}
