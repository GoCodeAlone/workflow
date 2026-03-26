package schema

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStepSchemaRegistry_Builtins(t *testing.T) {
	reg := NewStepSchemaRegistry()
	// Verify key step types are registered.
	for _, st := range []string{"step.set", "step.db_query", "step.http_call", "step.json_response", "step.conditional"} {
		s := reg.Get(st)
		if s == nil {
			t.Errorf("missing built-in schema for %s", st)
			continue
		}
		if len(s.ConfigFields) == 0 {
			t.Errorf("%s has no config fields", st)
		}
		if s.Description == "" {
			t.Errorf("%s has no description", st)
		}
	}
}

func TestStepSchemaRegistry_Outputs(t *testing.T) {
	reg := NewStepSchemaRegistry()
	// Verify that key step types have output definitions.
	for _, st := range []string{"step.db_query", "step.http_call", "step.hash", "step.cache_get"} {
		s := reg.Get(st)
		if s == nil {
			t.Errorf("missing built-in schema for %s", st)
			continue
		}
		if len(s.Outputs) == 0 {
			t.Errorf("%s has no outputs", st)
		}
	}
}

func TestStepSchemaRegistry_RegisterCustom(t *testing.T) {
	reg := NewStepSchemaRegistry()
	custom := &StepSchema{
		Type:        "step.custom_plugin",
		Description: "A custom plugin step",
		ConfigFields: []ConfigFieldDef{
			{Key: "endpoint", Type: FieldTypeString, Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "result", Type: "any", Description: "The result"},
		},
	}
	reg.Register(custom)
	got := reg.Get("step.custom_plugin")
	if got == nil {
		t.Fatal("custom schema not registered")
	}
	if got.Description != custom.Description {
		t.Error("custom schema description mismatch")
	}
	if len(got.Outputs) != 1 || got.Outputs[0].Key != "result" {
		t.Error("custom schema outputs mismatch")
	}
}

func TestStepSchemaRegistry_Unregister(t *testing.T) {
	reg := NewStepSchemaRegistry()
	if reg.Get("step.set") == nil {
		t.Fatal("step.set should exist before unregister")
	}
	reg.Unregister("step.set")
	if reg.Get("step.set") != nil {
		t.Error("step.set should be nil after unregister")
	}
}

func TestStepSchemaRegistry_Types(t *testing.T) {
	reg := NewStepSchemaRegistry()
	types := reg.Types()
	if len(types) == 0 {
		t.Fatal("expected non-empty types list")
	}
	// Should be sorted.
	for i := 1; i < len(types); i++ {
		if types[i] < types[i-1] {
			t.Errorf("types not sorted: %q before %q", types[i-1], types[i])
		}
	}
}

func TestStepSchemaRegistry_AllMap(t *testing.T) {
	reg := NewStepSchemaRegistry()
	m := reg.AllMap()
	if len(m) == 0 {
		t.Fatal("AllMap returned empty")
	}
	if _, ok := m["step.db_query"]; !ok {
		t.Error("step.db_query not in AllMap")
	}
}

func TestGetStepSchemaRegistry_Singleton(t *testing.T) {
	reg1 := GetStepSchemaRegistry()
	reg2 := GetStepSchemaRegistry()
	if reg1 != reg2 {
		t.Error("GetStepSchemaRegistry should return same instance")
	}
	if reg1.Get("step.set") == nil {
		t.Error("singleton registry should have built-in step.set")
	}
}

func TestLoadPluginStepSchemasFromDir(t *testing.T) {
	// Create a temp plugin directory with a fake plugin manifest.
	tmpDir := t.TempDir()
	pluginDir := filepath.Join(tmpDir, "my-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{
		"name": "my-plugin",
		"stepSchemas": [
			{
				"type": "step.my_plugin_action",
				"description": "A custom plugin action",
				"configFields": [
					{"key": "target", "type": "string", "description": "Target URL", "required": true}
				],
				"outputs": [
					{"key": "result", "type": "any", "description": "Action result"}
				]
			}
		]
	}`
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	reg := GetStepSchemaRegistry()
	// Ensure not already registered.
	if reg.Get("step.my_plugin_action") != nil {
		t.Fatal("step.my_plugin_action should not exist before loading")
	}

	LoadPluginStepSchemasFromDir(tmpDir)

	s := reg.Get("step.my_plugin_action")
	if s == nil {
		t.Fatal("step.my_plugin_action should be registered after loading")
	}
	if s.Description != "A custom plugin action" {
		t.Errorf("unexpected description: %q", s.Description)
	}
	if len(s.ConfigFields) != 1 || s.ConfigFields[0].Key != "target" {
		t.Error("config fields not loaded correctly")
	}
	if len(s.Outputs) != 1 || s.Outputs[0].Key != "result" {
		t.Error("outputs not loaded correctly")
	}

	// Clean up the global registry.
	reg.Unregister("step.my_plugin_action")
}

func TestLoadPluginStepSchemasFromDir_EmptyDir(t *testing.T) {
	// Should not panic on empty or nonexistent directory.
	LoadPluginStepSchemasFromDir("")
	LoadPluginStepSchemasFromDir(t.TempDir())
	LoadPluginStepSchemasFromDir("/nonexistent/path")
}

func TestInferStepOutputs_Set(t *testing.T) {
	reg := NewStepSchemaRegistry()
	outputs := reg.InferStepOutputs("step.set", map[string]any{
		"values": map[string]any{"user_id": "123", "status": "active"},
	})
	if len(outputs) != 2 {
		t.Fatalf("expected 2 outputs, got %d", len(outputs))
	}
	keys := map[string]bool{}
	for _, o := range outputs {
		keys[o.Key] = true
	}
	if !keys["user_id"] || !keys["status"] {
		t.Errorf("expected user_id and status keys, got %v", outputs)
	}
}

func TestInferStepOutputs_DBQuery_Single(t *testing.T) {
	reg := NewStepSchemaRegistry()
	outputs := reg.InferStepOutputs("step.db_query", map[string]any{
		"mode": "single",
	})
	keys := map[string]bool{}
	for _, o := range outputs {
		keys[o.Key] = true
	}
	if !keys["row"] || !keys["found"] {
		t.Errorf("expected row and found, got %v", outputs)
	}
}

func TestInferStepOutputs_DBQuery_List(t *testing.T) {
	reg := NewStepSchemaRegistry()
	outputs := reg.InferStepOutputs("step.db_query", map[string]any{
		"mode": "list",
	})
	keys := map[string]bool{}
	for _, o := range outputs {
		keys[o.Key] = true
	}
	if !keys["rows"] || !keys["count"] {
		t.Errorf("expected rows and count, got %v", outputs)
	}
}

func TestInferStepOutputs_DBQueryCached(t *testing.T) {
	reg := NewStepSchemaRegistry()
	outputs := reg.InferStepOutputs("step.db_query_cached", map[string]any{
		"mode": "list",
	})
	keys := map[string]bool{}
	for _, o := range outputs {
		keys[o.Key] = true
	}
	if !keys["rows"] || !keys["count"] || !keys["cache_hit"] {
		t.Errorf("expected rows, count, cache_hit, got %v", outputs)
	}
}

func TestInferStepOutputs_RequestParse(t *testing.T) {
	reg := NewStepSchemaRegistry()
	outputs := reg.InferStepOutputs("step.request_parse", map[string]any{
		"path_params":   []any{"id", "slug"},
		"query_params":  []any{"page"},
		"parse_body":    true,
		"parse_headers": []any{"Authorization"},
	})
	keys := map[string]bool{}
	for _, o := range outputs {
		keys[o.Key] = true
	}
	for _, expected := range []string{"path_params", "query", "body", "headers"} {
		if !keys[expected] {
			t.Errorf("missing expected output key %q", expected)
		}
	}
}

func TestInferStepOutputs_Fallback(t *testing.T) {
	reg := NewStepSchemaRegistry()
	outputs := reg.InferStepOutputs("step.http_call", nil)
	keys := map[string]bool{}
	for _, o := range outputs {
		keys[o.Key] = true
	}
	if !keys["status"] || !keys["body"] || !keys["headers"] || !keys["elapsed_ms"] {
		t.Errorf("expected static outputs for step.http_call, got %v", outputs)
	}
}

func TestInferStepOutputs_Unknown(t *testing.T) {
	reg := NewStepSchemaRegistry()
	outputs := reg.InferStepOutputs("step.nonexistent", nil)
	if outputs != nil {
		t.Errorf("expected nil for unknown step type, got %v", outputs)
	}
}

func TestInferStepOutputs_Validate_JsonSchema(t *testing.T) {
	reg := NewStepSchemaRegistry()
	outputs := reg.InferStepOutputs("step.validate", map[string]any{
		"strategy": "json_schema",
		"schema": map[string]any{
			"properties": map[string]any{
				"email": map[string]any{"type": "string"},
				"name":  map[string]any{"type": "string"},
			},
		},
	})
	if len(outputs) == 0 {
		t.Fatal("expected outputs from json_schema strategy")
	}
	keys := map[string]bool{}
	for _, o := range outputs {
		keys[o.Key] = true
	}
	if !keys["valid"] {
		t.Error("expected 'valid' output to always be present")
	}
	if !keys["email"] || !keys["name"] {
		t.Errorf("expected email and name from schema properties, got %v", outputs)
	}
}

func TestInferStepOutputs_Validate_Fallback(t *testing.T) {
	reg := NewStepSchemaRegistry()
	outputs := reg.InferStepOutputs("step.validate", map[string]any{
		"rules": map[string]any{"field": "required"},
	})
	keys := map[string]bool{}
	for _, o := range outputs {
		keys[o.Key] = true
	}
	if !keys["valid"] {
		t.Errorf("expected valid output from fallback strategy, got %v", outputs)
	}
}

func TestInferStepOutputs_NoSQLGet(t *testing.T) {
	reg := NewStepSchemaRegistry()
	outputs := reg.InferStepOutputs("step.nosql_get", map[string]any{
		"store": "mystore",
		"key":   "{{.user_id}}",
	})
	keys := map[string]bool{}
	for _, o := range outputs {
		keys[o.Key] = true
	}
	if !keys["item"] || !keys["found"] {
		t.Errorf("expected item and found, got %v", outputs)
	}
}

func TestInferStepOutputs_NoSQLQuery(t *testing.T) {
	reg := NewStepSchemaRegistry()
	outputs := reg.InferStepOutputs("step.nosql_query", map[string]any{
		"store":  "mystore",
		"prefix": "users/",
	})
	keys := map[string]bool{}
	for _, o := range outputs {
		keys[o.Key] = true
	}
	if !keys["items"] || !keys["count"] {
		t.Errorf("expected items and count, got %v", outputs)
	}
}

func TestInferStepOutputs_NoSQLPut(t *testing.T) {
	reg := NewStepSchemaRegistry()
	outputs := reg.InferStepOutputs("step.nosql_put", map[string]any{
		"store": "mystore",
		"key":   "{{.id}}",
	})
	keys := map[string]bool{}
	for _, o := range outputs {
		keys[o.Key] = true
	}
	if !keys["stored"] || !keys["key"] {
		t.Errorf("expected stored and key, got %v", outputs)
	}
}
