package mcp

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestNewServer(t *testing.T) {
	srv := NewServer("testdata/plugins")
	if srv == nil {
		t.Fatal("NewServer returned nil")
	}
	if srv.MCPServer() == nil {
		t.Fatal("MCPServer() returned nil")
	}
}

func TestListModuleTypes(t *testing.T) {
	srv := NewServer("")
	result, err := srv.handleListModuleTypes(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}

	text := extractText(t, result)
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse result JSON: %v", err)
	}

	types, ok := data["module_types"].([]any)
	if !ok {
		t.Fatal("module_types not found in result")
	}
	if len(types) == 0 {
		t.Fatal("expected at least one module type")
	}

	// Verify some known types are present
	typeSet := make(map[string]bool)
	for _, tt := range types {
		typeSet[tt.(string)] = true
	}
	for _, expected := range []string{"http.server", "http.router", "http.handler", "messaging.broker"} {
		if !typeSet[expected] {
			t.Errorf("expected module type %q not found", expected)
		}
	}
}

func TestListStepTypes(t *testing.T) {
	srv := NewServer("")
	result, err := srv.handleListStepTypes(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse result JSON: %v", err)
	}

	steps, ok := data["step_types"].([]any)
	if !ok {
		t.Fatal("step_types not found in result")
	}
	if len(steps) == 0 {
		t.Fatal("expected at least one step type")
	}

	// All step types should start with "step."
	for _, s := range steps {
		str := s.(string)
		if !strings.HasPrefix(str, "step.") {
			t.Errorf("step type %q does not start with 'step.'", str)
		}
	}
}

func TestListTriggerTypes(t *testing.T) {
	srv := NewServer("")
	result, err := srv.handleListTriggerTypes(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse result JSON: %v", err)
	}

	triggers, ok := data["trigger_types"].([]any)
	if !ok {
		t.Fatal("trigger_types not found in result")
	}
	if len(triggers) == 0 {
		t.Fatal("expected at least one trigger type")
	}

	typeSet := make(map[string]bool)
	for _, tt := range triggers {
		typeSet[tt.(string)] = true
	}
	if !typeSet["http"] || !typeSet["schedule"] {
		t.Error("expected http and schedule trigger types")
	}
}

func TestListWorkflowTypes(t *testing.T) {
	srv := NewServer("")
	result, err := srv.handleListWorkflowTypes(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse result JSON: %v", err)
	}

	workflows, ok := data["workflow_types"].([]any)
	if !ok {
		t.Fatal("workflow_types not found in result")
	}
	if len(workflows) == 0 {
		t.Fatal("expected at least one workflow type")
	}

	typeSet := make(map[string]bool)
	for _, wt := range workflows {
		typeSet[wt.(string)] = true
	}
	if !typeSet["http"] || !typeSet["messaging"] {
		t.Error("expected http and messaging workflow types")
	}
}

func TestGenerateSchema(t *testing.T) {
	srv := NewServer("")
	result, err := srv.handleGenerateSchema(context.Background(), mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse schema JSON: %v", err)
	}

	if data["$schema"] == nil {
		t.Error("schema should have $schema field")
	}
	if data["properties"] == nil {
		t.Error("schema should have properties field")
	}
}

func TestValidateConfig_Valid(t *testing.T) {
	srv := NewServer("")

	validYAML := `
modules:
  - name: webServer
    type: http.server
    config:
      address: ":8080"
  - name: router
    type: http.router
    dependsOn: [webServer]
`

	req := makeCallToolRequest(map[string]any{
		"yaml_content": validYAML,
	})

	result, err := srv.handleValidateConfig(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse result JSON: %v", err)
	}

	if data["valid"] != true {
		t.Errorf("expected valid=true, got %v", data["valid"])
	}
}

func TestValidateConfig_Invalid(t *testing.T) {
	srv := NewServer("")

	invalidYAML := `
modules:
  - name: ""
    type: http.server
`

	req := makeCallToolRequest(map[string]any{
		"yaml_content": invalidYAML,
		"strict":       true,
	})

	result, err := srv.handleValidateConfig(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse result JSON: %v", err)
	}

	if data["valid"] != false {
		t.Errorf("expected valid=false, got %v", data["valid"])
	}
}

func TestValidateConfig_MissingContent(t *testing.T) {
	srv := NewServer("")

	req := makeCallToolRequest(map[string]any{})
	result, err := srv.handleValidateConfig(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	if text == "" {
		t.Fatal("expected error message")
	}
}

func TestValidateConfig_MalformedYAML(t *testing.T) {
	srv := NewServer("")

	req := makeCallToolRequest(map[string]any{
		"yaml_content": "{{invalid yaml",
	})

	result, err := srv.handleValidateConfig(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	if text == "" {
		t.Fatal("expected error message for malformed YAML")
	}
}

func TestInspectConfig(t *testing.T) {
	srv := NewServer("")

	yaml := `
modules:
  - name: webServer
    type: http.server
    config:
      address: ":8080"
  - name: router
    type: http.router
    dependsOn: [webServer]
  - name: handler
    type: http.handler
    dependsOn: [router]

workflows:
  http:
    routes:
      - method: GET
        path: /api/health
        handler: handler

triggers:
  http:
    routes:
      - method: GET
        path: /api/health
`

	req := makeCallToolRequest(map[string]any{
		"yaml_content": yaml,
	})

	result, err := srv.handleInspectConfig(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse result JSON: %v", err)
	}

	if data["module_count"].(float64) != 3 {
		t.Errorf("expected 3 modules, got %v", data["module_count"])
	}
	if len(data["workflows"].([]any)) != 1 {
		t.Errorf("expected 1 workflow, got %v", len(data["workflows"].([]any)))
	}
	if len(data["triggers"].([]any)) != 1 {
		t.Errorf("expected 1 trigger, got %v", len(data["triggers"].([]any)))
	}
}

func TestInspectConfig_MissingContent(t *testing.T) {
	srv := NewServer("")

	req := makeCallToolRequest(map[string]any{})
	result, err := srv.handleInspectConfig(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	if text == "" {
		t.Fatal("expected error message")
	}
}

func TestListPlugins_NoDir(t *testing.T) {
	srv := NewServer("/nonexistent/path")

	req := makeCallToolRequest(map[string]any{})
	result, err := srv.handleListPlugins(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse result JSON: %v", err)
	}

	if data["count"].(float64) != 0 {
		t.Errorf("expected 0 plugins for nonexistent dir, got %v", data["count"])
	}
}

func TestListPlugins_WithPlugins(t *testing.T) {
	// Create a temp directory with a fake plugin
	dir := t.TempDir()
	pluginDir := dir + "/test-plugin"
	if err := createTestPlugin(pluginDir, "1.2.3"); err != nil {
		t.Fatal(err)
	}

	srv := NewServer(dir)
	req := makeCallToolRequest(map[string]any{})
	result, err := srv.handleListPlugins(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse result JSON: %v", err)
	}

	if data["count"].(float64) != 1 {
		t.Errorf("expected 1 plugin, got %v", data["count"])
	}

	plugins := data["plugins"].([]any)
	plugin := plugins[0].(map[string]any)
	if plugin["name"] != "test-plugin" {
		t.Errorf("expected plugin name 'test-plugin', got %v", plugin["name"])
	}
	if plugin["version"] != "1.2.3" {
		t.Errorf("expected version '1.2.3', got %v", plugin["version"])
	}
}

func TestGetConfigSkeleton(t *testing.T) {
	srv := NewServer("")

	req := makeCallToolRequest(map[string]any{
		"module_types": []any{"http.server", "http.router"},
	})

	result, err := srv.handleGetConfigSkeleton(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	if text == "" {
		t.Fatal("expected non-empty skeleton")
	}

	// Verify the skeleton contains expected content
	if !contains(text, "http.server") {
		t.Error("skeleton should contain http.server")
	}
	if !contains(text, "http.router") {
		t.Error("skeleton should contain http.router")
	}
	if !contains(text, "modules:") {
		t.Error("skeleton should contain modules: section")
	}
}

func TestGetConfigSkeleton_NoTypes(t *testing.T) {
	srv := NewServer("")

	req := makeCallToolRequest(map[string]any{
		"module_types": []any{},
	})

	result, err := srv.handleGetConfigSkeleton(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	if text == "" {
		t.Fatal("expected error message")
	}
}

func TestDocsOverview(t *testing.T) {
	srv := NewServer("")
	contents, err := srv.handleDocsOverview(context.Background(), mcp.ReadResourceRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(contents) != 1 {
		t.Fatalf("expected 1 resource content, got %d", len(contents))
	}
	text, ok := contents[0].(mcp.TextResourceContents)
	if !ok {
		t.Fatal("expected TextResourceContents")
	}
	if text.Text == "" {
		t.Fatal("expected non-empty overview text")
	}
	if !contains(text.Text, "Workflow Engine") {
		t.Error("overview should mention 'Workflow Engine'")
	}
}

func TestDocsYAMLSyntax(t *testing.T) {
	srv := NewServer("")
	contents, err := srv.handleDocsYAMLSyntax(context.Background(), mcp.ReadResourceRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(contents) != 1 {
		t.Fatalf("expected 1 resource content, got %d", len(contents))
	}
	text, ok := contents[0].(mcp.TextResourceContents)
	if !ok {
		t.Fatal("expected TextResourceContents")
	}
	if !contains(text.Text, "modules:") {
		t.Error("YAML syntax guide should contain 'modules:'")
	}
}

func TestDocsModuleReference(t *testing.T) {
	srv := NewServer("")
	contents, err := srv.handleDocsModuleReference(context.Background(), mcp.ReadResourceRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(contents) != 1 {
		t.Fatalf("expected 1 resource content, got %d", len(contents))
	}
	text, ok := contents[0].(mcp.TextResourceContents)
	if !ok {
		t.Fatal("expected TextResourceContents")
	}
	if !contains(text.Text, "http.server") {
		t.Error("module reference should contain 'http.server'")
	}
}

func TestGenerateConfigSkeleton(t *testing.T) {
	yaml := generateConfigSkeleton([]string{"http.server", "http.router"})
	if !contains(yaml, "http.server") {
		t.Error("skeleton should contain http.server")
	}
	if !contains(yaml, "http-server-1") {
		t.Error("skeleton should generate name 'http-server-1'")
	}
	if !contains(yaml, "http-router-2") {
		t.Error("skeleton should generate name 'http-router-2'")
	}
}

func TestGenerateModuleReference(t *testing.T) {
	ref := generateModuleReference()
	if ref == "" {
		t.Fatal("module reference should not be empty")
	}
	if !contains(ref, "Module Type Reference") {
		t.Error("reference should have title")
	}
}

// --- Test Helpers ---

func extractText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if result == nil {
		return ""
	}
	for _, c := range result.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}

func makeCallToolRequest(args map[string]any) mcp.CallToolRequest {
	req := mcp.CallToolRequest{}
	req.Params.Arguments = args
	return req
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func createTestPlugin(dir, version string) error {
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}
	data := []byte(`{"name":"test-plugin","version":"` + version + `"}`)
	return os.WriteFile(dir+"/plugin.json", data, 0640) //nolint:gosec // G306: test helper
}
