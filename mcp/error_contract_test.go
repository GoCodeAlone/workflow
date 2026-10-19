package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const errorContractYAML = "modules:\n  - name: absent\n    type: task2.uninstalled-module\n"

func TestMCPErrorContract(t *testing.T) {
	engine := &mockEngine{triggerErr: errors.New("PUBLIC_SYNTHETIC_ENGINE_FAILURE")}
	result := dispatchInputContract(t, NewServer("", WithEngine(engine)), "run_workflow", map[string]any{
		"workflow_type": "http", "action": "public_test",
	})
	var payload map[string]any
	if err := json.Unmarshal([]byte(extractText(t, result)), &payload); err != nil {
		t.Fatal(err)
	}
	if !result.IsError || payload["success"] != false || payload["error"] != engine.triggerErr.Error() {
		t.Fatalf("engine failure must retain its payload and IsError: IsError=%v, payload=%v", result.IsError, payload)
	}
}

func TestMCPErrorContractAnalyticalNegatives(t *testing.T) {
	for _, item := range []struct{ tool, field string }{{"validate_config", "valid"}, {"compat_check", "compatible"}} {
		t.Run(item.tool, func(t *testing.T) {
			result := dispatchInputContract(t, NewServer(""), item.tool, map[string]any{"yaml_content": errorContractYAML})
			var payload map[string]any
			if err := json.Unmarshal([]byte(extractText(t, result)), &payload); err != nil {
				t.Fatal(err)
			}
			if result.IsError || payload[item.field] != false {
				t.Fatal("analytical negatives must remain successful reports")
			}
		})
	}
}

func TestInProcessErrorContract(t *testing.T) {
	for _, item := range []struct {
		name, tool, field string
		args              map[string]any
	}{
		{"missing YAML", "validate_config", "yaml_content", map[string]any{}},
		{"wrong YAML type", "validate_config", "yaml_content", map[string]any{"yaml_content": 73}},
		{"wrong strict type", "validate_config", "strict", map[string]any{"yaml_content": inputContractYAML, "strict": "false"}},
		{"mixed array", "get_config_skeleton", "module_types", map[string]any{"module_types": []any{"http.server", 73}}},
	} {
		t.Run(item.name, func(t *testing.T) {
			_, err := NewInProcessServer().CallTool(t.Context(), item.tool, item.args)
			if err == nil {
				t.Fatalf("invalid invocation must return a Go error: %s", item.name)
			}
			if !strings.Contains(err.Error(), item.field) {
				t.Errorf("bounded feedback must identify %s: %v", item.field, err)
			}
		})
	}
}

func TestInProcessErrorContractSuccessParity(t *testing.T) {
	provider := NewInProcessServer()
	for _, item := range []struct {
		tool string
		args map[string]any
	}{
		{"list_module_types", nil},
		{"get_config_skeleton", map[string]any{"module_types": []string{"http.server"}}},
		{"validate_config", map[string]any{"yaml_content": inputContractYAML}},
		{"validate_config", map[string]any{"yaml_content": errorContractYAML}},
		{"compat_check", map[string]any{"yaml_content": errorContractYAML}},
	} {
		t.Run(item.tool+fmt.Sprint(item.args), func(t *testing.T) {
			result, err := provider.CallTool(t.Context(), item.tool, item.args)
			if err != nil {
				t.Fatal(err)
			}
			text, ok := result.(string)
			if !ok {
				t.Fatalf("successful result must remain string, got %T", result)
			}
			wire := dispatchInputContract(t, NewServer(""), item.tool, item.args)
			if wire.IsError || text != extractText(t, wire) {
				t.Fatal("adapter must preserve successful dispatcher text, including analytical negative reports")
			}
		})
	}
}

func TestInProcessErrorContractBoundedEngineFailure(t *testing.T) {
	engine := &mockEngine{triggerErr: errors.New(strings.Repeat("synthetic雪", 2000))}
	_, err := NewInProcessServer(WithInProcessEngine(engine)).CallTool(t.Context(), "run_workflow", map[string]any{"workflow_type": "http", "action": "public_test"})
	if err == nil {
		t.Fatal("engine failure must return a Go error")
	}
	if len(err.Error()) > 4200 || !utf8.ValidString(err.Error()) {
		t.Fatal("tool failure feedback must be bounded and valid UTF-8")
	}
}

func TestInProcessErrorContractAuditMetadataOnly(t *testing.T) {
	const argsCanary = "SYNTHETIC_ARGUMENT_CANARY"
	const engineCanary = "SYNTHETIC_ENGINE_ERROR_CANARY"
	const handlerCanary = "SYNTHETIC_HANDLER_ERROR_CANARY"
	const unknownCanary = "SYNTHETIC_UNKNOWN_TOOL_CANARY"
	var log bytes.Buffer
	provider := NewInProcessServer(WithInProcessEngine(&mockEngine{triggerErr: errors.New(engineCanary)}), WithInProcessAuditLog(slog.New(slog.NewJSONHandler(&log, nil))))
	_, _ = provider.CallTool(t.Context(), "run_workflow", map[string]any{"workflow_type": "http", "action": "public_test", "data": map[string]any{"secret": argsCanary}})
	_, _ = provider.CallTool(t.Context(), "validate_config", map[string]any{"yaml_content": argsCanary, "strict": true})
	_, err := provider.CallTool(t.Context(), strings.Repeat(unknownCanary, 500), nil)
	if err == nil || len(err.Error()) > 4200 {
		t.Error("unknown-tool feedback must be a bounded error")
	}
	_, err = provider.CallTool(t.Context(), "validate_config", map[string]any{"yaml_content": make(chan int)})
	if err == nil {
		t.Error("non-JSON arguments must return an error")
	}
	// Dependency double at the existing handler seam; no new production tool.
	registered := provider.server.MCPServer().ListTools()["list_module_types"]
	handler := func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return nil, errors.New(handlerCanary)
	}
	provider.tools["list_module_types"] = handler
	provider.server.MCPServer().AddTool(registered.Tool, handler)
	_, err = provider.CallTool(t.Context(), "list_module_types", nil)
	if err == nil {
		t.Error("SDK protocol error must become a Go error")
	}
	for _, canary := range []string{argsCanary, engineCanary, handlerCanary, unknownCanary} {
		if strings.Contains(log.String(), canary) {
			t.Errorf("audit exposed %s", canary)
		}
	}
	for _, line := range bytes.Split(bytes.TrimSpace(log.Bytes()), []byte("\n")) {
		var entry map[string]any
		if err := json.Unmarshal(line, &entry); err != nil {
			t.Fatal(err)
		}
		for key := range entry {
			if key != "time" && key != "level" && key != "msg" && key != "tool" && key != "status" {
				t.Errorf("audit must contain metadata only, found %q", key)
			}
		}
		if _, ok := entry["status"].(string); !ok {
			t.Error("audit missing bounded status metadata")
		}
		if name, ok := entry["tool"].(string); ok {
			if _, known := provider.GetToolSchema(name); !known {
				t.Error("audit exposed unknown user-controlled tool name")
			}
		}
	}
}

func TestInProcessErrorContractRequestIDs(t *testing.T) {
	provider := NewInProcessServer()
	var mu sync.Mutex
	var ids []string
	hooks := provider.server.MCPServer().GetHooks()
	if hooks == nil {
		hooks = &server.Hooks{}
		server.WithHooks(hooks)(provider.server.MCPServer())
	}
	hooks.AddBeforeCallTool(func(_ context.Context, id any, _ *mcp.CallToolRequest) {
		mu.Lock()
		defer mu.Unlock()
		ids = append(ids, fmt.Sprint(id))
	})
	for range 3 {
		if _, err := provider.CallTool(t.Context(), "list_module_types", nil); err != nil {
			t.Fatal(err)
		}
	}
	var calls sync.WaitGroup
	for range 40 {
		calls.Go(func() {
			if _, err := provider.CallTool(t.Context(), "list_module_types", nil); err != nil {
				t.Error(err)
			}
		})
	}
	calls.Wait()
	if len(ids) != 43 {
		t.Fatalf("all calls must cross SDK dispatcher, observed %d", len(ids))
	}
	seen := map[string]bool{}
	for _, id := range ids {
		value, err := strconv.ParseUint(id, 10, 64)
		if err != nil || value == 0 || value > 43 || seen[id] {
			t.Fatalf("request ID must be unique and monotonic per instance: %q", id)
		}
		seen[id] = true
	}
	if ids[0] != "1" || ids[1] != "2" || ids[2] != "3" {
		t.Fatalf("sequential request IDs are not monotonic: %v", ids[:3])
	}
}

// Controlled fake engine: it cannot start a real workflow. The entered and
// cancelled barriers prove cancellation reaches a currently blocked handler.
type errorContractEngine struct {
	mockEngine
	entered   chan struct{}
	cancelled chan struct{}
	successes atomic.Int32
}

func newErrorContractEngine() *errorContractEngine {
	return &errorContractEngine{entered: make(chan struct{}), cancelled: make(chan struct{})}
}
func (e *errorContractEngine) TriggerWorkflow(ctx context.Context, _ string, action string, _ map[string]any) error {
	if action == "block_until_cancelled" {
		close(e.entered)
		<-ctx.Done()
		close(e.cancelled)
		return ctx.Err()
	}
	e.successes.Add(1)
	return nil
}
func awaitErrorContract(t *testing.T, signal <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func TestInProcessErrorContractCancellation(t *testing.T) {
	engine := newErrorContractEngine()
	provider := NewInProcessServer(WithInProcessEngine(engine))
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	completed := make(chan error, 1)
	go func() {
		_, err := provider.CallTool(ctx, "run_workflow", map[string]any{"workflow_type": "http", "action": "block_until_cancelled"})
		completed <- err
	}()
	awaitErrorContract(t, engine.entered, "provider handler entry")
	cancel()
	awaitErrorContract(t, engine.cancelled, "provider ctx.Done")
	select {
	case err := <-completed:
		if err == nil {
			t.Error("cancelled engine invocation must return a Go error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled provider call did not complete")
	}
	if engine.successes.Load() != 0 {
		t.Fatal("cancelled operation recorded post-cancel success")
	}
	if _, err := provider.CallTool(t.Context(), "run_workflow", map[string]any{"workflow_type": "http", "action": "public_success"}); err != nil {
		t.Fatal(err)
	}
	if engine.successes.Load() != 1 {
		t.Fatal("subsequent successful request did not execute")
	}
}

func TestMCPErrorContractCancellationNotification(t *testing.T) {
	engine := newErrorContractEngine()
	srv := NewServer("", WithEngine(engine))
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	const requestID = "public-cancellation-request"
	request, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": requestID, "method": "tools/call", "params": map[string]any{"name": "run_workflow", "arguments": map[string]any{"workflow_type": "http", "action": "block_until_cancelled"}}})
	if err != nil {
		t.Fatal(err)
	}
	completed := make(chan mcp.JSONRPCMessage, 1)
	go func() { completed <- srv.MCPServer().HandleMessage(ctx, request) }()
	awaitErrorContract(t, engine.entered, "dispatcher handler entry")
	notification := []byte(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":"public-cancellation-request"}}`)
	if response := srv.MCPServer().HandleMessage(t.Context(), notification); response != nil {
		t.Error("cancellation notification must not produce a response")
	}
	awaitErrorContract(t, engine.cancelled, "dispatcher ctx.Done")
	if ctx.Err() != nil {
		t.Fatal("matching notification must cancel the handler before the parent operation deadline")
	}
	select {
	case response := <-completed:
		data, err := json.Marshal(response)
		if err != nil {
			t.Fatal(err)
		}
		var reply struct{ Result *mcp.CallToolResult }
		if err = json.Unmarshal(data, &reply); err != nil {
			t.Fatal(err)
		}
		if reply.Result == nil || !reply.Result.IsError || !strings.Contains(extractText(t, reply.Result), `"success": false`) {
			t.Fatal("cancelled engine invocation must retain error semantics")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled dispatcher call did not complete")
	}
	if engine.successes.Load() != 0 {
		t.Fatal("cancelled operation recorded post-cancel success")
	}
	result := dispatchInputContract(t, srv, "run_workflow", map[string]any{"workflow_type": "http", "action": "public_success"})
	if result.IsError || engine.successes.Load() != 1 {
		t.Fatal("dispatcher did not recover for a subsequent successful request")
	}
}
