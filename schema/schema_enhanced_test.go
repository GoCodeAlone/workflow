package schema

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestGenerateWorkflowSchema_ValidJSON verifies the schema produces valid JSON.
func TestGenerateWorkflowSchema_ValidJSON(t *testing.T) {
	s := GenerateWorkflowSchema()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal schema: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("schema JSON is empty")
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
}

// TestGenerateWorkflowSchema_HTTPServerIfThen checks that the http.server module
// type has an if/then conditional with config field validation.
func TestGenerateWorkflowSchema_HTTPServerIfThen(t *testing.T) {
	s := GenerateWorkflowSchema()
	modules := s.Properties["modules"]
	if modules == nil {
		t.Fatal("modules property missing")
	}
	items := modules.Items
	if items == nil {
		t.Fatal("modules items missing")
	}

	// Find the http.server if/then in allOf.
	var httpServerThen *Schema
	for _, cond := range items.AllOf {
		if cond.If == nil {
			continue
		}
		typeProp, ok := cond.If.Properties["type"]
		if !ok {
			continue
		}
		for _, e := range typeProp.Enum {
			if e == "http.server" {
				httpServerThen = cond.Then
				break
			}
		}
	}

	if httpServerThen == nil {
		t.Fatal("http.server if/then not found in allOf")
	}

	configProp := httpServerThen.Properties["config"]
	if configProp == nil {
		t.Fatal("http.server then.config missing")
	}

	addressProp := configProp.Properties["address"]
	if addressProp == nil {
		t.Fatal("http.server config.address schema missing")
	}
	if addressProp.Type != "string" {
		t.Errorf("address type should be string, got %q", addressProp.Type)
	}
}

// TestGenerateWorkflowSchema_PipelinesSection checks the pipelines section.
func TestGenerateWorkflowSchema_PipelinesSection(t *testing.T) {
	s := GenerateWorkflowSchema()
	pipelines := s.Properties["pipelines"]
	if pipelines == nil {
		t.Fatal("pipelines property missing")
	}
	if pipelines.Type != "object" {
		t.Errorf("pipelines should be object, got %q", pipelines.Type)
	}
}

// TestGenerateWorkflowSchema_RequiresSection checks the requires section.
func TestGenerateWorkflowSchema_RequiresSection(t *testing.T) {
	s := GenerateWorkflowSchema()
	requires := s.Properties["requires"]
	if requires == nil {
		t.Fatal("requires property missing")
	}
	if requires.Type != "object" {
		t.Errorf("requires should be object, got %q", requires.Type)
	}
	if requires.Properties["plugins"] == nil {
		t.Error("requires.plugins missing")
	}
	if requires.Properties["version"] == nil {
		t.Error("requires.version missing")
	}
}

// TestGenerateWorkflowSchema_ImportsSection checks the imports section.
func TestGenerateWorkflowSchema_ImportsSection(t *testing.T) {
	s := GenerateWorkflowSchema()
	imports := s.Properties["imports"]
	if imports == nil {
		t.Fatal("imports property missing")
	}
	if imports.Type != "array" {
		t.Errorf("imports should be array, got %q", imports.Type)
	}
	if imports.Items == nil || imports.Items.Type != "string" {
		t.Error("imports items should be string")
	}
}

// TestGenerateApplicationSchema checks the application schema top-level structure.
func TestGenerateApplicationSchema(t *testing.T) {
	s := GenerateApplicationSchema()
	if s.Schema != "https://json-schema.org/draft/2020-12/schema" {
		t.Errorf("unexpected schema URI: %q", s.Schema)
	}
	if s.Properties["engine"] == nil {
		t.Error("application schema should have engine property")
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("failed to marshal application schema: %v", err)
	}
	if !strings.Contains(string(data), "modules") {
		t.Error("application schema JSON should contain modules")
	}
}

// TestConfigFieldDefToSchema checks conversion of ConfigFieldDef to Schema.
func TestConfigFieldDefToSchema(t *testing.T) {
	cases := []struct {
		def      ConfigFieldDef
		wantType string
	}{
		{ConfigFieldDef{Type: FieldTypeString}, "string"},
		{ConfigFieldDef{Type: FieldTypeNumber}, "number"},
		{ConfigFieldDef{Type: FieldTypeBool}, "boolean"},
		{ConfigFieldDef{Type: FieldTypeArray, ArrayItemType: "string"}, "array"},
		{ConfigFieldDef{Type: FieldTypeMap}, "object"},
		{ConfigFieldDef{Type: FieldTypeJSON}, "object"},
		{ConfigFieldDef{Type: FieldTypeDuration}, "string"},
		{ConfigFieldDef{Type: FieldTypeFilePath}, "string"},
		{ConfigFieldDef{Type: FieldTypeSQL}, "string"},
		{ConfigFieldDef{Type: FieldTypeSelect, Options: []string{"a", "b"}}, "string"},
	}

	for _, tc := range cases {
		s := configFieldDefToSchema(tc.def)
		if s.Type != tc.wantType {
			t.Errorf("field type %q: got schema type %q, want %q", tc.def.Type, s.Type, tc.wantType)
		}
	}
}

// TestKnownStepTypes checks the schema package's step type set.
func TestKnownStepTypes(t *testing.T) {
	types := KnownStepTypes()
	if len(types) == 0 {
		t.Fatal("no step types returned")
	}
	// Spot-check some core types.
	for _, expected := range []string{"step.set", "step.http_call", "step.json_response", "step.validate"} {
		if !types[expected] {
			t.Errorf("missing step type %q", expected)
		}
	}
}
