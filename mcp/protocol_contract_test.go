package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Literal wire keys deliberately avoid deriving the expected contract from the
// SDK constants whose serialized behavior these tests verify.
func protocolContractMeta() map[string]any {
	return map[string]any{
		"io.modelcontextprotocol/protocolVersion":    "2026-07-28",
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
	}
}

func dispatchProtocolContract(t *testing.T, ctx context.Context, srv *Server, params map[string]any) *mcp.CallToolResult {
	t.Helper()
	data, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "public-protocol-contract", "method": "tools/call", "params": params})
	if err != nil {
		t.Fatal(err)
	}
	data, err = json.Marshal(srv.MCPServer().HandleMessage(ctx, data))
	if err != nil {
		t.Fatal(err)
	}
	var reply struct {
		Result *mcp.CallToolResult      `json:"result"`
		Error  *mcp.JSONRPCErrorDetails `json:"error"`
	}
	if json.Unmarshal(data, &reply) != nil || reply.Error != nil || reply.Result == nil {
		t.Fatalf("expected serialized tool result, got %s", data)
	}
	return reply.Result
}

func TestMCPProtocolContractToolSemantics(t *testing.T) {
	for _, modern := range []bool{false, true} {
		era := "legacy"
		if modern {
			era = "July"
		}
		t.Run(era, func(t *testing.T) {
			const engineFailure = "PUBLIC_SYNTHETIC_ENGINE_FAILURE"
			engine := &mockEngine{triggerErr: errors.New(engineFailure)}
			srv := NewServer("", WithEngine(engine))
			for _, item := range []struct {
				name, tool, field string
				args              map[string]any
				isError           bool
				value             any
			}{
				{"wrong argument", "validate_config", "", map[string]any{"yaml_content": 73}, true, nil},
				{"wrong boolean", "validate_config", "", map[string]any{"yaml_content": inputContractYAML, "strict": "false"}, true, nil},
				{"mixed array", "get_config_skeleton", "", map[string]any{"module_types": []any{"http.server", 73}}, true, nil},
				{"engine failure", "run_workflow", "success", map[string]any{"workflow_type": "public", "action": "public_test"}, true, false},
				{"negative validation", "validate_config", "valid", map[string]any{"yaml_content": errorContractYAML}, false, false},
				{"negative compatibility", "compat_check", "compatible", map[string]any{"yaml_content": errorContractYAML}, false, false},
			} {
				t.Run(item.name, func(t *testing.T) {
					params := map[string]any{"name": item.tool, "arguments": item.args}
					if modern {
						params["_meta"] = protocolContractMeta()
					}
					result := dispatchProtocolContract(t, t.Context(), srv, params)
					if result.IsError != item.isError {
						t.Fatalf("IsError=%v, want %v", result.IsError, item.isError)
					}
					if item.field != "" {
						var payload map[string]any
						if json.Unmarshal([]byte(extractText(t, result)), &payload) != nil || payload[item.field] != item.value {
							t.Fatal("analytical/engine payload contract changed")
						}
						if item.tool == "run_workflow" && payload["error"] != engineFailure {
							t.Fatal("engine error payload lost")
						}
					}
					if modern && result.GetResultType() != "complete" {
						t.Fatal("July tool result lacks complete resultType")
					}
				})
			}
		})
	}
}

// This session double captures only SDK notification delivery. Existing
// Workflow tool handlers and the real SDK dispatcher run unchanged.
type protocolContractSession struct{ notifications chan mcp.JSONRPCNotification }

func (*protocolContractSession) Initialize()       {}
func (*protocolContractSession) Initialized() bool { return true }
func (*protocolContractSession) SessionID() string { return "public-protocol-session" }
func (s *protocolContractSession) NotificationChannel() chan<- mcp.JSONRPCNotification {
	return s.notifications
}

func TestMCPProtocolContractRequestIsolationAndLogging(t *testing.T) {
	srv := NewServer("")
	session := &protocolContractSession{notifications: make(chan mcp.JSONRPCNotification, 4)}
	ctx := srv.MCPServer().WithContext(t.Context(), session)
	type observed struct {
		modern, roots bool
		version       string
	}
	var requests []observed
	hooks := &server.Hooks{}
	hooks.AddBeforeCallTool(func(ctx context.Context, _ any, _ *mcp.CallToolRequest) {
		info := server.RequestProtocolInfoFromContext(ctx)
		item := observed{modern: server.IsModernRequest(ctx), version: server.RequestProtocolVersion(ctx)}
		if info != nil && info.ClientCapabilities != nil {
			item.roots = info.ClientCapabilities.Roots != nil
		}
		requests = append(requests, item)
		if item.modern {
			// An attempted SDK log must be suppressed without per-request logLevel.
			err := srv.MCPServer().SendLogMessageToClient(ctx, mcp.NewLoggingMessageNotification(mcp.LoggingLevelError, "public-contract", "SYNTHETIC_LOG_CANARY"))
			if err != nil {
				t.Error(err)
			}
		}
	})
	server.WithHooks(hooks)(srv.MCPServer())
	for _, meta := range []map[string]any{
		{"io.modelcontextprotocol/protocolVersion": "2026-07-28", "io.modelcontextprotocol/clientCapabilities": map[string]any{"roots": map[string]any{}}},
		nil,
		protocolContractMeta(),
		nil,
	} {
		params := map[string]any{"name": "list_module_types", "arguments": map[string]any{}}
		if meta != nil {
			params["_meta"] = meta
		}
		result := dispatchProtocolContract(t, ctx, srv, params)
		if result.IsError || (result.GetResultType() == "complete") != (meta != nil) {
			t.Fatal("modern result metadata leaked across requests")
		}
	}
	want := []observed{{true, true, "2026-07-28"}, {false, false, ""}, {true, false, "2026-07-28"}, {false, false, ""}}
	if len(requests) != len(want) {
		t.Fatal("missing real dispatcher hook observations")
	}
	for i := range want {
		if requests[i] != want[i] {
			t.Fatalf("request %d inherited protocol/capabilities: got %+v, want %+v", i, requests[i], want[i])
		}
	}
	select {
	case <-session.notifications:
		t.Fatal("modern log notification emitted without requested logLevel")
	default:
	}
	// Positive control proves the hook exercises live notification delivery,
	// followed by another missing-level request to detect log-level inheritance.
	meta := protocolContractMeta()
	meta["io.modelcontextprotocol/logLevel"] = "error"
	dispatchProtocolContract(t, ctx, srv, map[string]any{"name": "list_module_types", "_meta": meta})
	select {
	case notification := <-session.notifications:
		if notification.Method != "notifications/message" {
			t.Fatal("unexpected positive-control notification")
		}
	default:
		t.Fatal("requested log notification was not delivered")
	}
	dispatchProtocolContract(t, ctx, srv, map[string]any{"name": "list_module_types", "_meta": protocolContractMeta()})
	select {
	case <-session.notifications:
		t.Fatal("previous request logLevel leaked into next request")
	default:
	}
}

func TestMCPProtocolContractCooperativeCancellation(t *testing.T) {
	engine := newErrorContractEngine()
	srv := NewServer("", WithEngine(engine))
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	request, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "July-cancel", "method": "tools/call", "params": map[string]any{
		"name": "run_workflow", "arguments": map[string]any{"workflow_type": "public", "action": "block_until_cancelled"}, "_meta": protocolContractMeta(),
	}})
	if err != nil {
		t.Fatal(err)
	}
	completed := make(chan mcp.JSONRPCMessage, 1)
	go func() { completed <- srv.MCPServer().HandleMessage(ctx, request) }()
	awaitErrorContract(t, engine.entered, "July dispatcher handler entry")
	if reply := srv.MCPServer().HandleMessage(t.Context(), []byte(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":"July-cancel"}}`)); reply != nil {
		t.Fatal("cancellation notification must not produce a response")
	}
	awaitErrorContract(t, engine.cancelled, "July dispatcher ctx.Done")
	if ctx.Err() != nil {
		t.Fatal("matching notification must cancel before parent deadline")
	}
	select {
	case response := <-completed:
		data, err := json.Marshal(response)
		if err != nil {
			t.Fatal(err)
		}
		var reply struct {
			Result *mcp.CallToolResult      `json:"result"`
			Error  *mcp.JSONRPCErrorDetails `json:"error"`
		}
		if json.Unmarshal(data, &reply) != nil || reply.Error != nil || reply.Result == nil || !reply.Result.IsError || reply.Result.GetResultType() != "complete" {
			t.Fatal("cancelled July tool result lost error/complete semantics")
		}
		var payload struct {
			Success *bool  `json:"success"`
			Error   string `json:"error"`
		}
		if json.Unmarshal([]byte(extractText(t, reply.Result)), &payload) != nil || payload.Success == nil || *payload.Success || payload.Error != context.Canceled.Error() {
			t.Fatal("cancelled handler reported success")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled July dispatcher call did not complete")
	}
	if engine.successes.Load() != 0 {
		t.Fatal("cancelled handler recorded post-cancel success")
	}
	result := dispatchProtocolContract(t, t.Context(), srv, map[string]any{"name": "run_workflow", "arguments": map[string]any{"workflow_type": "public", "action": "public_success"}, "_meta": protocolContractMeta()})
	if result.IsError || engine.successes.Load() != 1 {
		t.Fatal("subsequent July request did not succeed")
	}
}
