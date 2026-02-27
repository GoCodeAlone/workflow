package mcp

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// --- get_module_schema ---

func TestGetModuleSchema_KnownType(t *testing.T) {
	srv := NewServer("")
	req := makeCallToolRequest(map[string]any{
		"module_type": "http.server",
	})

	result, err := srv.handleGetModuleSchema(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse result JSON: %v", err)
	}

	if data["type"] != "http.server" {
		t.Errorf("expected type 'http.server', got %v", data["type"])
	}
	if data["description"] == nil || data["description"] == "" {
		t.Error("expected non-empty description")
	}
	configFields, ok := data["configFields"].([]any)
	if !ok {
		t.Fatal("expected configFields array")
	}
	if len(configFields) == 0 {
		t.Error("expected at least one config field for http.server")
	}
	// Verify address field is present.
	found := false
	for _, cf := range configFields {
		f := cf.(map[string]any)
		if f["key"] == "address" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'address' config field for http.server")
	}

	if data["example"] == nil || data["example"] == "" {
		t.Error("expected non-empty example")
	}
	if !contains(data["example"].(string), "http.server") {
		t.Error("example should mention http.server type")
	}
}

func TestGetModuleSchema_WithInputsOutputs(t *testing.T) {
	srv := NewServer("")
	req := makeCallToolRequest(map[string]any{
		"module_type": "http.router",
	})

	result, err := srv.handleGetModuleSchema(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse result JSON: %v", err)
	}

	// Router should have inputs (it receives requests).
	inputs, ok := data["inputs"].([]any)
	if !ok || len(inputs) == 0 {
		t.Error("expected at least one input for http.router")
	}
}

func TestGetModuleSchema_UnknownType(t *testing.T) {
	srv := NewServer("")
	req := makeCallToolRequest(map[string]any{
		"module_type": "unknown.type.xyz",
	})

	result, err := srv.handleGetModuleSchema(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	if !contains(text, "unknown module type") {
		t.Errorf("expected 'unknown module type' error, got %q", text)
	}
}

func TestGetModuleSchema_MissingType(t *testing.T) {
	srv := NewServer("")
	req := makeCallToolRequest(map[string]any{})

	result, err := srv.handleGetModuleSchema(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	if !contains(text, "required") {
		t.Errorf("expected 'required' error message, got %q", text)
	}
}

// --- get_step_schema ---

func TestGetStepSchema_KnownType(t *testing.T) {
	srv := NewServer("")
	req := makeCallToolRequest(map[string]any{
		"step_type": "step.set",
	})

	result, err := srv.handleGetStepSchema(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse result JSON: %v", err)
	}

	if data["type"] != "step.set" {
		t.Errorf("expected type 'step.set', got %v", data["type"])
	}
	if data["description"] == nil || data["description"] == "" {
		t.Error("expected non-empty description for step.set")
	}
	configKeys, ok := data["configKeys"].([]any)
	if !ok || len(configKeys) == 0 {
		t.Error("expected configKeys for step.set")
	}
	if data["example"] == nil || data["example"] == "" {
		t.Error("expected non-empty example")
	}
	if !contains(data["example"].(string), "step.set") {
		t.Error("example should mention step.set")
	}
}

func TestGetStepSchema_HTTPCall(t *testing.T) {
	srv := NewServer("")
	req := makeCallToolRequest(map[string]any{
		"step_type": "step.http_call",
	})

	result, err := srv.handleGetStepSchema(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse result JSON: %v", err)
	}

	if data["type"] != "step.http_call" {
		t.Errorf("expected type 'step.http_call', got %v", data["type"])
	}

	configDefs, ok := data["configDefs"].([]any)
	if !ok || len(configDefs) == 0 {
		t.Error("expected configDefs for step.http_call")
	}

	// Verify url field is present.
	found := false
	for _, cd := range configDefs {
		f := cd.(map[string]any)
		if f["key"] == "url" {
			found = true
			if f["required"] != true {
				t.Error("expected url to be required")
			}
		}
	}
	if !found {
		t.Error("expected 'url' in configDefs")
	}
}

func TestGetStepSchema_UnknownType(t *testing.T) {
	srv := NewServer("")
	req := makeCallToolRequest(map[string]any{
		"step_type": "step.nonexistent_xyz",
	})

	result, err := srv.handleGetStepSchema(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	if !contains(text, "unknown step type") {
		t.Errorf("expected 'unknown step type' error, got %q", text)
	}
}

func TestGetStepSchema_NotAStepType(t *testing.T) {
	srv := NewServer("")
	req := makeCallToolRequest(map[string]any{
		"step_type": "http.server",
	})

	result, err := srv.handleGetStepSchema(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	if !contains(text, "step.") {
		t.Errorf("expected error about step. prefix, got %q", text)
	}
}

func TestGetStepSchema_MissingType(t *testing.T) {
	srv := NewServer("")
	req := makeCallToolRequest(map[string]any{})

	result, err := srv.handleGetStepSchema(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	if !contains(text, "required") {
		t.Errorf("expected 'required' error, got %q", text)
	}
}

// --- get_template_functions ---

func TestGetTemplateFunctions(t *testing.T) {
	srv := NewServer("")
	result, err := srv.handleGetTemplateFunctions(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse result JSON: %v", err)
	}

	count, ok := data["count"].(float64)
	if !ok || count == 0 {
		t.Error("expected non-zero function count")
	}

	funcs, ok := data["functions"].([]any)
	if !ok || len(funcs) == 0 {
		t.Fatal("expected functions list")
	}

	// Verify expected function names are present.
	funcNames := make(map[string]bool)
	for _, f := range funcs {
		fn := f.(map[string]any)
		name, _ := fn["name"].(string)
		funcNames[name] = true

		// Each function should have name, signature, description, and example.
		if name == "" {
			t.Error("function should have a name")
		}
		if fn["signature"] == nil || fn["signature"] == "" {
			t.Errorf("function %q should have a signature", name)
		}
		if fn["description"] == nil || fn["description"] == "" {
			t.Errorf("function %q should have a description", name)
		}
		if fn["example"] == nil || fn["example"] == "" {
			t.Errorf("function %q should have an example", name)
		}
	}

	expectedFunctions := []string{"uuid", "uuidv4", "now", "lower", "default", "json", "step", "trigger"}
	for _, expected := range expectedFunctions {
		if !funcNames[expected] {
			t.Errorf("expected function %q not found in list", expected)
		}
	}
}

func TestGetTemplateFunctions_NowLayout(t *testing.T) {
	srv := NewServer("")
	result, err := srv.handleGetTemplateFunctions(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	// Verify the now function mentions layout.
	if !contains(text, "RFC3339") {
		t.Error("expected 'RFC3339' in template functions (as part of now function description)")
	}
}

// --- validate_template_expressions ---

func TestValidateTemplateExpressions_ForwardReference(t *testing.T) {
	srv := NewServer("")

	yaml := `
pipelines:
  test-pipeline:
    steps:
      - name: step-a
        type: step.set
        config:
          values:
            msg: "{{ .steps.step-b.result }}"
      - name: step-b
        type: step.set
        config:
          values:
            result: "hello"
`
	req := makeCallToolRequest(map[string]any{
		"yaml_content": yaml,
	})

	result, err := srv.handleValidateTemplateExpressions(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse result JSON: %v", err)
	}

	warnings, ok := data["warnings"].([]any)
	if !ok {
		t.Fatal("expected warnings array")
	}

	// Should detect a forward reference to step-b from step-a.
	foundForward := false
	for _, w := range warnings {
		if contains(w.(string), "forward reference") {
			foundForward = true
			break
		}
	}
	if !foundForward {
		t.Errorf("expected forward reference warning, got: %v", warnings)
	}
}

func TestValidateTemplateExpressions_HyphenatedDotAccess(t *testing.T) {
	srv := NewServer("")

	yaml := `
pipelines:
  test-pipeline:
    steps:
      - name: parse-request
        type: step.request_parse
        config: {}
      - name: process
        type: step.set
        config:
          values:
            id: "{{ .steps.parse-request.body.id }}"
`
	req := makeCallToolRequest(map[string]any{
		"yaml_content": yaml,
	})

	result, err := srv.handleValidateTemplateExpressions(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse result JSON: %v", err)
	}

	warnings, ok := data["warnings"].([]any)
	if !ok {
		t.Fatal("expected warnings array")
	}

	foundHyphen := false
	for _, w := range warnings {
		if contains(w.(string), "hyphenated step name") {
			foundHyphen = true
			break
		}
	}
	if !foundHyphen {
		t.Errorf("expected hyphenated dot-access warning, got: %v", warnings)
	}
}

func TestValidateTemplateExpressions_UndefinedStep(t *testing.T) {
	srv := NewServer("")

	yaml := `
pipelines:
  test-pipeline:
    steps:
      - name: process
        type: step.set
        config:
          values:
            val: "{{ .steps.nonexistent.field }}"
`
	req := makeCallToolRequest(map[string]any{
		"yaml_content": yaml,
	})

	result, err := srv.handleValidateTemplateExpressions(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse result JSON: %v", err)
	}

	warnings, ok := data["warnings"].([]any)
	if !ok {
		t.Fatal("expected warnings array")
	}

	foundUndef := false
	for _, w := range warnings {
		if contains(w.(string), "undefined step reference") {
			foundUndef = true
			break
		}
	}
	if !foundUndef {
		t.Errorf("expected undefined step reference warning, got: %v", warnings)
	}
}

func TestValidateTemplateExpressions_NoWarnings(t *testing.T) {
	srv := NewServer("")

	yaml := `
pipelines:
  clean-pipeline:
    steps:
      - name: step-one
        type: step.set
        config:
          values:
            x: "hello"
      - name: step-two
        type: step.log
        config:
          message: "done"
`
	req := makeCallToolRequest(map[string]any{
		"yaml_content": yaml,
	})

	result, err := srv.handleValidateTemplateExpressions(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse result JSON: %v", err)
	}

	count := data["warning_count"].(float64)
	if count != 0 {
		t.Errorf("expected 0 warnings for clean pipeline, got %v: %v", count, data["warnings"])
	}
}

func TestValidateTemplateExpressions_MissingContent(t *testing.T) {
	srv := NewServer("")
	req := makeCallToolRequest(map[string]any{})

	result, err := srv.handleValidateTemplateExpressions(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	if !contains(text, "required") {
		t.Errorf("expected 'required' error, got %q", text)
	}
}

func TestValidateTemplateExpressions_MalformedYAML(t *testing.T) {
	srv := NewServer("")
	req := makeCallToolRequest(map[string]any{
		"yaml_content": "{{not valid yaml}",
	})

	result, err := srv.handleValidateTemplateExpressions(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	if text == "" {
		t.Fatal("expected error message for malformed YAML")
	}
}

func TestValidateTemplateExpressions_NoPipelines(t *testing.T) {
	srv := NewServer("")

	yaml := `
modules:
  - name: web
    type: http.server
    config:
      address: ":8080"
`
	req := makeCallToolRequest(map[string]any{
		"yaml_content": yaml,
	})

	result, err := srv.handleValidateTemplateExpressions(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse result JSON: %v", err)
	}

	if data["pipelines_checked"].(float64) != 0 {
		t.Error("expected 0 pipelines_checked when no pipelines in config")
	}
}

// --- get_config_examples ---

func TestGetConfigExamples_List(t *testing.T) {
	// Create a temp dir with fake YAML files to simulate the example directory.
	dir := t.TempDir()
	files := []string{"api-server-config.yaml", "event-driven-workflow.yaml", "data-pipeline-config.yaml"}
	for _, f := range files {
		if err := os.WriteFile(dir+"/"+f, []byte("# test example\nmodules: []\n"), 0640); err != nil {
			t.Fatal(err)
		}
	}

	srv := &Server{pluginDir: ""}
	// Directly call listExamples (unit test the helper).
	examples, err := listExamples(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(examples) != 3 {
		t.Errorf("expected 3 examples, got %d", len(examples))
	}

	nameSet := make(map[string]bool)
	for _, ex := range examples {
		nameSet[ex.Name] = true
		if !strings.HasSuffix(ex.Filename, ".yaml") {
			t.Errorf("expected .yaml filename, got %q", ex.Filename)
		}
	}

	for _, f := range files {
		name := strings.TrimSuffix(f, ".yaml")
		if !nameSet[name] {
			t.Errorf("expected example %q in list", name)
		}
	}
	_ = srv
}

func TestGetConfigExamples_GetContent(t *testing.T) {
	dir := t.TempDir()
	content := "# Example config\nmodules:\n  - name: web\n    type: http.server\n"
	if err := os.WriteFile(dir+"/api-server-config.yaml", []byte(content), 0640); err != nil {
		t.Fatal(err)
	}

	gotContent, filename, err := readExampleFile(dir, "api-server-config")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotContent != content {
		t.Errorf("expected content %q, got %q", content, gotContent)
	}
	if filename != "api-server-config.yaml" {
		t.Errorf("expected filename 'api-server-config.yaml', got %q", filename)
	}
}

func TestGetConfigExamples_GetContentWithExtension(t *testing.T) {
	dir := t.TempDir()
	content := "modules: []\n"
	if err := os.WriteFile(dir+"/simple-workflow.yaml", []byte(content), 0640); err != nil {
		t.Fatal(err)
	}

	// Call with .yaml extension explicitly.
	gotContent, _, err := readExampleFile(dir, "simple-workflow.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotContent != content {
		t.Errorf("expected content %q, got %q", content, gotContent)
	}
}

func TestGetConfigExamples_NotFound(t *testing.T) {
	dir := t.TempDir()
	_, _, err := readExampleFile(dir, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent example")
	}
}

func TestGetConfigExamples_NoDir(t *testing.T) {
	examples, err := listExamples("/nonexistent/directory")
	if err != nil {
		t.Fatalf("unexpected error listing nonexistent dir: %v", err)
	}
	if len(examples) != 0 {
		t.Errorf("expected 0 examples for nonexistent dir, got %d", len(examples))
	}
}

func TestHandleGetConfigExamples_List(t *testing.T) {
	dir := t.TempDir()
	files := []string{"simple.yaml", "advanced.yaml"}
	for _, f := range files {
		if err := os.WriteFile(dir+"/"+f, []byte("modules: []\n"), 0640); err != nil {
			t.Fatal(err)
		}
	}

	srv := NewServer("")
	// Override exampleDir by using a server method call with the temp dir.
	// We test via the helper directly, since pluginDir-based lookup is environment-dependent.
	req := makeCallToolRequest(map[string]any{})
	result, err := srv.handleGetConfigExamples(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse result JSON: %v", err)
	}

	// Without a real example dir, count may be 0 (not a failure).
	if data["count"] == nil {
		t.Error("expected count field in result")
	}
	if data["examples"] == nil {
		t.Error("expected examples field in result")
	}
}

func TestHandleGetConfigExamples_SpecificName_NotFound(t *testing.T) {
	srv := NewServer("")
	req := makeCallToolRequest(map[string]any{
		"name": "this-does-not-exist-ever",
	})

	result, err := srv.handleGetConfigExamples(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	if !contains(text, "not found") {
		t.Errorf("expected 'not found' error, got %q", text)
	}
}

// --- helper unit tests ---

func TestGenerateModuleExample(t *testing.T) {
	// Unit test the module example generator via get_module_schema (integrated).
	srv := NewServer("")
	req := makeCallToolRequest(map[string]any{
		"module_type": "http.server",
	})

	result, err := srv.handleGetModuleSchema(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse JSON: %v", err)
	}

	example := data["example"].(string)
	if !contains(example, "modules:") {
		t.Error("example should contain 'modules:'")
	}
	if !contains(example, "type: http.server") {
		t.Error("example should contain 'type: http.server'")
	}
}

func TestGenerateStepExample(t *testing.T) {
	example := generateStepExample("step.http_call", []string{"url", "method"})
	if !contains(example, "step.http_call") {
		t.Error("step example should mention the step type")
	}
	if !contains(example, "pipelines:") {
		t.Error("step example should contain 'pipelines:'")
	}
}

func TestKnownStepTypeDescriptions_Coverage(t *testing.T) {
	descs := knownStepTypeDescriptions()
	if len(descs) == 0 {
		t.Fatal("expected non-empty step type descriptions")
	}

	// Spot-check some important types.
	for _, must := range []string{"step.set", "step.http_call", "step.foreach", "step.retry_with_backoff"} {
		if _, ok := descs[must]; !ok {
			t.Errorf("expected step type %q in descriptions", must)
		}
	}

	// All entries must have a non-empty description.
	for typ, info := range descs {
		if info.Description == "" {
			t.Errorf("step type %q has empty description", typ)
		}
		if info.Type != typ {
			t.Errorf("step type key %q doesn't match info.Type %q", typ, info.Type)
		}
	}
}

func TestTemplateFunctionDescriptions_AllHaveExamples(t *testing.T) {
	funcs := templateFunctionDescriptions()
	for _, f := range funcs {
		if f.Name == "" {
			t.Error("function has empty name")
		}
		if f.Signature == "" {
			t.Errorf("function %q has empty signature", f.Name)
		}
		if f.Description == "" {
			t.Errorf("function %q has empty description", f.Name)
		}
		if f.Example == "" {
			t.Errorf("function %q has empty example", f.Name)
		}
	}
}
