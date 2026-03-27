package schema

import (
	"testing"
)

// --- test structs ---

type allTagsStruct struct {
	Driver    string `json:"driver" editor:"type=select,options=postgres|mysql|sqlite,required,description=Database driver"`
	DSN       string `json:"dsn" editor:"type=string,required,sensitive,description=Connection string,placeholder=postgres://user:pass@host/db"`
	MaxConns  int    `json:"maxConns" editor:"type=number,description=Max connections,default=25"`
	Enabled   bool   `json:"enabled" editor:"type=boolean,description=Enable feature,default=true"`
	Tags      []string `json:"tags" editor:"type=array,arrayItemType=string"`
	Meta      map[string]string `json:"meta" editor:"type=map,mapValueType=string"`
}

type inferredTypesStruct struct {
	Name    string  `json:"name" editor:"description=Name"`
	Count   int     `json:"count" editor:"description=Count"`
	Ratio   float64 `json:"ratio" editor:"description=Ratio"`
	Active  bool    `json:"active" editor:"description=Active"`
	Items   []string `json:"items" editor:"description=Items"`
	Labels  map[string]string `json:"labels" editor:"description=Labels"`
}

type noEditorTagsStruct struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

type nestedStruct struct {
	Name  string      `json:"name" editor:"type=string,description=Name"`
	Inner innerStruct `json:"inner" editor:"type=string,description=Inner"`
}

type innerStruct struct {
	Value string `json:"value" editor:"type=string"`
}

type sensitiveStruct struct {
	Password string `json:"password" editor:"type=string,sensitive,required"`
}

type labelOverrideStruct struct {
	Foo string `json:"foo" editor:"type=string,label=Custom Label"`
}

type groupStruct struct {
	Host string `json:"host" editor:"type=string,group=connection"`
	Port int    `json:"port" editor:"type=number,group=connection"`
}

type defaultValueStruct struct {
	Count   int     `json:"count" editor:"type=number,default=42"`
	Ratio   float64 `json:"ratio" editor:"type=number,default=3.14"`
	Active  bool    `json:"active" editor:"type=boolean,default=true"`
	Name    string  `json:"name" editor:"type=string,default=hello"`
}

// --- tests ---

func TestGenerateConfigFields_AllTags(t *testing.T) {
	fields := GenerateConfigFields(allTagsStruct{})
	if len(fields) != 6 {
		t.Fatalf("expected 6 fields, got %d", len(fields))
	}

	driver := fields[0]
	if driver.Key != "driver" {
		t.Errorf("expected key=driver, got %q", driver.Key)
	}
	if driver.Type != FieldTypeSelect {
		t.Errorf("expected type=select, got %q", driver.Type)
	}
	if !driver.Required {
		t.Error("expected required=true")
	}
	if driver.Description != "Database driver" {
		t.Errorf("unexpected description: %q", driver.Description)
	}
	if len(driver.Options) != 3 || driver.Options[0] != "postgres" || driver.Options[1] != "mysql" || driver.Options[2] != "sqlite" {
		t.Errorf("unexpected options: %v", driver.Options)
	}

	dsn := fields[1]
	if !dsn.Sensitive {
		t.Error("expected dsn.Sensitive=true")
	}
	if dsn.Placeholder != "postgres://user:pass@host/db" {
		t.Errorf("unexpected placeholder: %q", dsn.Placeholder)
	}

	maxConns := fields[2]
	if maxConns.DefaultValue != 25 {
		t.Errorf("expected defaultValue=25 (int), got %v (%T)", maxConns.DefaultValue, maxConns.DefaultValue)
	}

	enabled := fields[3]
	if enabled.DefaultValue != true {
		t.Errorf("expected defaultValue=true, got %v", enabled.DefaultValue)
	}

	tags := fields[4]
	if tags.ArrayItemType != "string" {
		t.Errorf("expected arrayItemType=string, got %q", tags.ArrayItemType)
	}

	meta := fields[5]
	if meta.MapValueType != "string" {
		t.Errorf("expected mapValueType=string, got %q", meta.MapValueType)
	}
}

func TestGenerateConfigFields_InferredTypes(t *testing.T) {
	fields := GenerateConfigFields(inferredTypesStruct{})
	if len(fields) != 6 {
		t.Fatalf("expected 6 fields, got %d", len(fields))
	}

	cases := []struct {
		key      string
		wantType ConfigFieldType
	}{
		{"name", FieldTypeString},
		{"count", FieldTypeNumber},
		{"ratio", FieldTypeNumber},
		{"active", FieldTypeBool},
		{"items", FieldTypeArray},
		{"labels", FieldTypeMap},
	}

	for i, tc := range cases {
		if fields[i].Key != tc.key {
			t.Errorf("[%d] expected key=%q, got %q", i, tc.key, fields[i].Key)
		}
		if fields[i].Type != tc.wantType {
			t.Errorf("[%d] key=%q: expected type=%q, got %q", i, tc.key, tc.wantType, fields[i].Type)
		}
	}
}

func TestGenerateConfigFields_NoEditorTags(t *testing.T) {
	fields := GenerateConfigFields(noEditorTagsStruct{})
	if len(fields) != 0 {
		t.Errorf("expected 0 fields, got %d", len(fields))
	}
}

func TestGenerateConfigFields_PointerInput(t *testing.T) {
	fields := GenerateConfigFields(&allTagsStruct{})
	if len(fields) != 6 {
		t.Fatalf("expected 6 fields from pointer input, got %d", len(fields))
	}
}

func TestGenerateConfigFields_NonStructInput(t *testing.T) {
	if fields := GenerateConfigFields("not a struct"); fields != nil {
		t.Errorf("expected nil for non-struct, got %v", fields)
	}
	if fields := GenerateConfigFields(42); fields != nil {
		t.Errorf("expected nil for int, got %v", fields)
	}
}

func TestGenerateConfigFields_NilInput(t *testing.T) {
	// nil interface{} — TypeOf returns nil
	var v interface{}
	fields := GenerateConfigFields(v)
	if fields != nil {
		t.Errorf("expected nil for nil input, got %v", fields)
	}
}

func TestGenerateConfigFields_SensitiveFlag(t *testing.T) {
	fields := GenerateConfigFields(sensitiveStruct{})
	if len(fields) != 1 {
		t.Fatalf("expected 1 field, got %d", len(fields))
	}
	if !fields[0].Sensitive {
		t.Error("expected Sensitive=true")
	}
	if !fields[0].Required {
		t.Error("expected Required=true")
	}
}

func TestGenerateConfigFields_LabelOverride(t *testing.T) {
	fields := GenerateConfigFields(labelOverrideStruct{})
	if len(fields) != 1 {
		t.Fatalf("expected 1 field, got %d", len(fields))
	}
	if fields[0].Label != "Custom Label" {
		t.Errorf("expected label=%q, got %q", "Custom Label", fields[0].Label)
	}
}

func TestGenerateConfigFields_Group(t *testing.T) {
	fields := GenerateConfigFields(groupStruct{})
	if len(fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(fields))
	}
	for _, f := range fields {
		if f.Group != "connection" {
			t.Errorf("field %q: expected group=connection, got %q", f.Key, f.Group)
		}
	}
}

func TestGenerateConfigFields_DefaultValues(t *testing.T) {
	fields := GenerateConfigFields(defaultValueStruct{})
	if len(fields) != 4 {
		t.Fatalf("expected 4 fields, got %d", len(fields))
	}

	if fields[0].DefaultValue != 42 {
		t.Errorf("expected int 42, got %v (%T)", fields[0].DefaultValue, fields[0].DefaultValue)
	}
	if fields[1].DefaultValue != 3.14 {
		t.Errorf("expected float 3.14, got %v (%T)", fields[1].DefaultValue, fields[1].DefaultValue)
	}
	if fields[2].DefaultValue != true {
		t.Errorf("expected bool true, got %v (%T)", fields[2].DefaultValue, fields[2].DefaultValue)
	}
	if fields[3].DefaultValue != "hello" {
		t.Errorf("expected string 'hello', got %v (%T)", fields[3].DefaultValue, fields[3].DefaultValue)
	}
}

func TestGenerateConfigFields_OptionsWithPipeSeparator(t *testing.T) {
	fields := GenerateConfigFields(allTagsStruct{})
	driver := fields[0]
	if len(driver.Options) != 3 {
		t.Fatalf("expected 3 options, got %d", len(driver.Options))
	}
	expected := []string{"postgres", "mysql", "sqlite"}
	for i, e := range expected {
		if driver.Options[i] != e {
			t.Errorf("options[%d]: expected %q, got %q", i, e, driver.Options[i])
		}
	}
}

func TestToLabel(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"driver", "Driver"},
		{"maxOpenConns", "Max Open Conns"},
		{"dsn", "Dsn"},
		{"myFieldName", "My Field Name"},
		{"name", "Name"},
	}
	for _, tc := range cases {
		got := toLabel(tc.input)
		if got != tc.want {
			t.Errorf("toLabel(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestParseDefault(t *testing.T) {
	cases := []struct {
		input string
		want  any
	}{
		{"true", true},
		{"false", false},
		{"42", 42},
		{"3.14", 3.14},
		{"hello", "hello"},
	}
	for _, tc := range cases {
		got := parseDefault(tc.input)
		if got != tc.want {
			t.Errorf("parseDefault(%q) = %v (%T), want %v (%T)", tc.input, got, got, tc.want, tc.want)
		}
	}
}
