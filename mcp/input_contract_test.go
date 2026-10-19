package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const inputContractYAML = "modules:\n  - name: web\n    type: http.server\n    config:\n      address: \":8080\"\n"

// Exercise Workflow's actual SDK dispatcher and serialized response, rather
// than invoking a handler around the server's input-validation options.
func dispatchInputContract(t *testing.T, srv *Server, tool string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	request, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": tool, "arguments": args},
	})
	if err != nil {
		t.Fatal(err)
	}
	response := srv.MCPServer().HandleMessage(t.Context(), request)
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var reply struct {
		Result *mcp.CallToolResult      `json:"result"`
		Error  *mcp.JSONRPCErrorDetails `json:"error"`
	}
	if err := json.Unmarshal(encoded, &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Error != nil || reply.Result == nil {
		t.Fatalf("expected tool result, got %s", encoded)
	}
	return reply.Result
}

func TestMCPInputContract(t *testing.T) {
	tests := []struct {
		name  string
		tool  string
		args  map[string]any
		field string
	}{
		{"missing required", "validate_config", map[string]any{}, "yaml_content"},
		{"wrong YAML type", "validate_config", map[string]any{"yaml_content": 73}, "yaml_content"},
		{"string strict flag", "validate_config", map[string]any{"yaml_content": inputContractYAML, "strict": "false"}, "strict"},
		{"string skip flag", "validate_config", map[string]any{"yaml_content": inputContractYAML, "skip_unknown_types": "true"}, "skip_unknown_types"},
	}
	for _, array := range []struct {
		tool  string
		field string
		base  map[string]any
	}{
		{"get_config_skeleton", "module_types", nil},
		{"scaffold_ci", "environments", map[string]any{"description": "Public sample API"}},
		{"scaffold_environment", "environments", map[string]any{"provider": "docker"}},
	} {
		for _, value := range []struct {
			name string
			data any
		}{
			{"scalar", "staging"},
			{"null", nil},
			{"mixed", []any{"http.server", 73}},
		} {
			args := map[string]any{array.field: value.data}
			for key, val := range array.base {
				args[key] = val
			}
			tests = append(tests, struct {
				name  string
				tool  string
				args  map[string]any
				field string
			}{array.tool + "/" + value.name, array.tool, args, array.field})
		}
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := dispatchInputContract(t, NewServer(""), tt.tool, tt.args)
			text := extractText(t, result)
			if !result.IsError {
				t.Fatalf("schema-invalid %s accepted by %s: %s", tt.field, tt.tool, text)
			}
			if !strings.Contains(text, tt.field) {
				t.Errorf("error must identify %s, got %q", tt.field, text)
			}
		})
	}
}

func TestMCPInputContractValid(t *testing.T) {
	for _, tt := range []struct {
		name string
		tool string
		args map[string]any
		want string
	}{
		{"required YAML", "validate_config", map[string]any{"yaml_content": inputContractYAML}, `"valid": true`},
		{"typed booleans", "validate_config", map[string]any{"yaml_content": inputContractYAML, "strict": true, "skip_unknown_types": false}, `"valid": true`},
		{"module strings", "get_config_skeleton", map[string]any{"module_types": []string{"http.server", "http.router"}}, "type: http.router"},
		{"CI environments", "scaffold_ci", map[string]any{"description": "Public sample API", "environments": []string{"staging"}}, "staging"},
		{"provider environments", "scaffold_environment", map[string]any{"provider": "docker", "environments": []string{"staging"}}, "staging"},
		{"omitted optional array", "scaffold_environment", map[string]any{"provider": "docker"}, "production"},
		{"extra argument remains allowed", "validate_config", map[string]any{"yaml_content": inputContractYAML, "future_client_hint": "ignored"}, `"valid": true`},
		{"negative validation is a report", "validate_config", map[string]any{"yaml_content": "modules:\n  - name: missing\n    type: task1.unknown\n"}, `"valid": false`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			result := dispatchInputContract(t, NewServer(""), tt.tool, tt.args)
			text := extractText(t, result)
			if result.IsError || !strings.Contains(text, tt.want) {
				t.Fatalf("valid invocation changed: IsError=%v, expected %q in %q", result.IsError, tt.want, text)
			}
		})
	}
}

// This engine is only a counter: malformed arguments must never reach it.
type inputContractEngine struct {
	mockEngine
	calls int
}

func (e *inputContractEngine) TriggerWorkflow(_ context.Context, _ string, _ string, _ map[string]any) error {
	e.calls++
	return nil
}

func TestMCPInputContractBeforeEngine(t *testing.T) {
	for _, tt := range []struct {
		name string
		args map[string]any
	}{
		{"missing action", map[string]any{"workflow_type": "http"}},
		{"wrong workflow type", map[string]any{"workflow_type": 73, "action": "public_test"}},
		{"wrong data type", map[string]any{"workflow_type": "http", "action": "public_test", "data": "not an object"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			engine := &inputContractEngine{}
			result := dispatchInputContract(t, NewServer("", WithEngine(engine)), "run_workflow", tt.args)
			if !result.IsError || engine.calls != 0 {
				t.Fatalf("malformed engine invocation: IsError=%v, engine calls=%d", result.IsError, engine.calls)
			}
		})
	}
	t.Run("valid typed call", func(t *testing.T) {
		engine := &inputContractEngine{}
		result := dispatchInputContract(t, NewServer("", WithEngine(engine)), "run_workflow", map[string]any{
			"workflow_type": "http", "action": "public_test", "data": map[string]any{"value": "public"},
		})
		if result.IsError || engine.calls != 1 || !strings.Contains(extractText(t, result), `"success": true`) {
			t.Fatalf("valid engine invocation changed: IsError=%v, engine calls=%d", result.IsError, engine.calls)
		}
	})
}

func TestMCPInputContractSkeletonAllOrNothing(t *testing.T) {
	for _, bad := range []any{73, nil, map[string]any{"type": "http.router"}} {
		result, err := NewServer("").handleGetConfigSkeleton(t.Context(), makeCallToolRequest(map[string]any{
			"module_types": []any{"http.server", bad},
		}))
		if err != nil {
			t.Fatal(err)
		}
		if !result.IsError || !strings.Contains(extractText(t, result), "module_types[1]") {
			t.Errorf("mixed module_types must fail without partial skeleton: IsError=%v, %s", result.IsError, extractText(t, result))
		}
	}
}

// The SDK skips validation when a tool schema fails to compile. Keep every
// Workflow-owned definition compile-valid and self-contained, including tools
// registered only when an engine is attached.
func TestMCPSchemaInventory(t *testing.T) {
	for _, variant := range []struct {
		name string
		srv  *Server
	}{
		{"stdio", NewServer("")},
		{"engine", NewServer("", WithEngine(&inputContractEngine{}))},
	} {
		t.Run(variant.name, func(t *testing.T) {
			for name, registered := range variant.srv.MCPServer().ListTools() {
				t.Run(name, func(t *testing.T) {
					data, err := json.Marshal(registered.Tool.InputSchema)
					if len(registered.Tool.RawInputSchema) != 0 {
						data = registered.Tool.RawInputSchema
					}
					if err != nil {
						t.Fatal(err)
					}
					if err := compileInputContractSchema(data); err != nil {
						t.Fatalf("invalid or nonlocal input schema: %v", err)
					}
					for field, property := range registered.Tool.InputSchema.Properties {
						definition, ok := property.(map[string]any)
						if !ok || definition["type"] != "array" {
							continue
						}
						items, ok := definition["items"].(map[string]any)
						if !ok || items["type"] != "string" {
							t.Errorf("%s requires items.type:string", field)
						}
					}
				})
			}
		})
	}
}

func TestMCPSchemaInventoryGuard(t *testing.T) {
	for _, tt := range []struct {
		name string
		raw  string
		bad  bool
	}{
		{"invalid schema", `{"type":"not-a-schema-type"}`, true},
		{"HTTP ref", `{"$ref":"https://example.invalid/schema.json"}`, true},
		{"file ref", `{"properties":{"nested":{"$ref":"file:///never-read.json"}}}`, true},
		{"relative ref", `{"$ref":"other.json"}`, true},
		{"dynamic HTTP ref", `{"$dynamicRef":"https://example.invalid/schema.json"}`, true},
		{"fragment ref", `{"$defs":{"value":{"type":"string"}},"properties":{"value":{"$ref":"#/$defs/value"}}}`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := compileInputContractSchema([]byte(tt.raw)); (err != nil) != tt.bad {
				t.Fatalf("offline schema guard error=%v, want rejection=%v", err, tt.bad)
			}
		})
	}
}

func compileInputContractSchema(raw []byte) error {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	if err := checkInputContractRefs(doc); err != nil {
		return err
	}
	compiler := jsonschema.NewCompiler()
	// No URL loader: only this in-memory resource and its fragments can resolve.
	const resource = "mem:///workflow/input-contract.json"
	if err := compiler.AddResource(resource, doc); err != nil {
		return err
	}
	_, err = compiler.Compile(resource)
	return err
}

func checkInputContractRefs(value any) error {
	switch node := value.(type) {
	case map[string]any:
		for key, child := range node {
			if key == "$ref" || key == "$dynamicRef" || key == "$recursiveRef" {
				ref, ok := child.(string)
				if !ok || !strings.HasPrefix(ref, "#") {
					return fmt.Errorf("%s must be fragment-local", key)
				}
			}
			if err := checkInputContractRefs(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range node {
			if err := checkInputContractRefs(child); err != nil {
				return err
			}
		}
	}
	return nil
}
