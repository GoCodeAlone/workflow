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
