package schema

import (
	"encoding/json"
	"os"
	"testing"
)

type editorSchemasGolden struct {
	ModuleSchemas map[string]*ModuleSchema `json:"moduleSchemas"`
	CoercionRules map[string][]string      `json:"coercionRules"`
}

func TestEditorSchemasGoldenFile(t *testing.T) {
	moduleReg := NewModuleSchemaRegistry()
	coercionReg := NewTypeCoercionRegistry()

	current := editorSchemasGolden{
		ModuleSchemas: moduleReg.AllMap(),
		CoercionRules: coercionReg.Rules(),
	}

	currentJSON, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		t.Fatalf("marshal current: %v", err)
	}

	goldenPath := "testdata/editor-schemas.golden.json"

	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenPath, append(currentJSON, '\n'), 0644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Log("Golden file updated")
		return
	}

	golden, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden file (run UPDATE_GOLDEN=1 go test ./schema/ -run TestEditorSchemasGoldenFile to create): %v", err)
	}

	var goldenParsed editorSchemasGolden
	if err := json.Unmarshal(golden, &goldenParsed); err != nil {
		t.Fatalf("parse golden: %v", err)
	}
	goldenNorm, _ := json.MarshalIndent(goldenParsed, "", "  ")

	if string(currentJSON) != string(goldenNorm) {
		t.Fatalf("editor schemas have changed — update golden file with:\n  UPDATE_GOLDEN=1 go test ./schema/ -run TestEditorSchemasGoldenFile\n\nThen commit the updated golden file and re-run the workflow-editor sync.")
	}
}
