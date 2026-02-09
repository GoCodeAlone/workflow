package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTools(t *testing.T) {
	tools := Tools()
	if len(tools) != 4 {
		t.Fatalf("expected 4 tools, got %d", len(tools))
	}

	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Name] = true
		if tool.Description == "" {
			t.Errorf("tool %q has empty description", tool.Name)
		}
		if tool.InputSchema == nil {
			t.Errorf("tool %q has nil input schema", tool.Name)
		}
	}

	expected := []string{"list_components", "get_component_schema", "validate_config", "get_example_workflow"}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("missing tool: %s", name)
		}
	}
}

func TestHandleListComponents(t *testing.T) {
	result, err := HandleToolCall("list_components", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "http.server") {
		t.Error("result missing http.server")
	}
	if !strings.Contains(result, "messaging.broker") {
		t.Error("result missing messaging.broker")
	}
}

func TestHandleGetComponentSchema(t *testing.T) {
	tests := []struct {
		name       string
		moduleType string
		want       string
		wantErr    bool
	}{
		{"http server", "http.server", "address", false},
		{"messaging broker", "messaging.broker", "description", false},
		{"unknown type", "nonexistent.type", "error", false},
		{"modular type", "cache.modular", "note", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input, _ := json.Marshal(map[string]string{"module_type": tt.moduleType})
			result, err := HandleToolCall("get_component_schema", input)
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr = %v", err, tt.wantErr)
			}
			if !strings.Contains(result, tt.want) {
				t.Errorf("result %q missing %q", result, tt.want)
			}
		})
	}
}

func TestHandleValidateConfig(t *testing.T) {
	tests := []struct {
		name      string
		yaml      string
		wantValid bool
	}{
		{
			name: "valid config",
			yaml: `modules:
  - name: server
    type: http.server
    config:
      address: ":8080"
  - name: router
    type: http.router
    dependsOn:
      - server
workflows:
  http:
    routes:
      - method: GET
        path: /health
        handler: server`,
			wantValid: true,
		},
		{
			name:      "empty modules",
			yaml:      `modules: []`,
			wantValid: false,
		},
		{
			name: "duplicate names",
			yaml: `modules:
  - name: server
    type: http.server
  - name: server
    type: http.router`,
			wantValid: false,
		},
		{
			name: "missing dependency",
			yaml: `modules:
  - name: router
    type: http.router
    dependsOn:
      - nonexistent`,
			wantValid: false,
		},
		{
			name:      "invalid yaml",
			yaml:      `{not yaml: [`,
			wantValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input, _ := json.Marshal(map[string]string{"config_yaml": tt.yaml})
			result, err := HandleToolCall("validate_config", input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantValid && !strings.Contains(result, `"valid": true`) {
				t.Errorf("expected valid config, got: %s", result)
			}
			if !tt.wantValid && strings.Contains(result, `"valid": true`) {
				t.Errorf("expected invalid config, got: %s", result)
			}
		})
	}
}

func TestHandleGetExampleWorkflow(t *testing.T) {
	categories := []string{"http", "messaging", "statemachine", "event", "trigger"}

	for _, cat := range categories {
		t.Run(cat, func(t *testing.T) {
			input, _ := json.Marshal(map[string]string{"category": cat})
			result, err := HandleToolCall("get_example_workflow", input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result == "" {
				t.Error("empty result")
			}
			// Check for JSON error response, not just the word "error" in YAML content
			if strings.Contains(result, `"error"`) && strings.Contains(result, "Unknown category") {
				t.Errorf("got error response: %s", result)
			}
		})
	}

	// Unknown category
	input, _ := json.Marshal(map[string]string{"category": "unknown"})
	result, err := HandleToolCall("get_example_workflow", input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "error") {
		t.Error("expected error for unknown category")
	}
}

func TestHandleUnknownTool(t *testing.T) {
	result, err := HandleToolCall("nonexistent_tool", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty result for unknown tool, got: %s", result)
	}
}

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{
			name: "json in markdown",
			text: "Here is the result:\n```json\n{\"key\": \"value\"}\n```\n",
			want: `{"key": "value"}`,
		},
		{
			name: "raw json",
			text: `Some text {"key": "value"} more text`,
			want: `{"key": "value"}`,
		},
		{
			name: "json array",
			text: `Result: [{"a": 1}, {"b": 2}]`,
			want: `[{"a": 1}, {"b": 2}]`,
		},
		{
			name: "no json",
			text: "Just plain text",
			want: "",
		},
		{
			name: "nested json",
			text: `{"outer": {"inner": "value"}}`,
			want: `{"outer": {"inner": "value"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractJSON(tt.text)
			if got != tt.want {
				t.Errorf("ExtractJSON() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractCode(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{
			name: "go code block",
			text: "```go\npackage main\n\nfunc main() {}\n```",
			want: "package main\n\nfunc main() {}",
		},
		{
			name: "generic code block",
			text: "```\npackage main\n```",
			want: "package main",
		},
		{
			name: "no code block",
			text: "package main\n\nfunc main() {}",
			want: "package main\n\nfunc main() {}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractCode(tt.text)
			if got != tt.want {
				t.Errorf("ExtractCode() = %q, want %q", got, tt.want)
			}
		})
	}
}
