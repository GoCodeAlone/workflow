package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"sync/atomic"
	"unicode/utf8"

	"github.com/mark3labs/mcp-go/mcp"
)

// inProcessConfig holds the options for NewInProcessServer.
type inProcessConfig struct {
	pluginDir         string
	registryDir       string
	documentationFile string
	auditLogger       *slog.Logger
	engine            EngineProvider
}

// InProcessOption configures the in-process server.
type InProcessOption func(*inProcessConfig)

// WithInProcessPluginDir sets the plugin directory for type discovery.
func WithInProcessPluginDir(dir string) InProcessOption {
	return func(c *inProcessConfig) { c.pluginDir = dir }
}

// WithInProcessRegistryDir sets the registry directory for plugin search.
func WithInProcessRegistryDir(dir string) InProcessOption {
	return func(c *inProcessConfig) { c.registryDir = dir }
}

// WithInProcessDocFile sets an explicit path to DOCUMENTATION.md.
func WithInProcessDocFile(path string) InProcessOption {
	return func(c *inProcessConfig) { c.documentationFile = path }
}

// WithInProcessAuditLog enables audit logging for in-process tool calls.
func WithInProcessAuditLog(logger *slog.Logger) InProcessOption {
	return func(c *inProcessConfig) { c.auditLogger = logger }
}

// WithInProcessEngine attaches a workflow engine for run_workflow support.
func WithInProcessEngine(eng EngineProvider) InProcessOption {
	return func(c *inProcessConfig) { c.engine = eng }
}

// InProcessServer exposes the workflow MCP tools for direct in-process
// invocation without HTTP or subprocess overhead.
// ToolSchema describes an MCP tool's name, description, and parameter schema.
type ToolSchema struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema,omitempty"`
}

// InProcessServer wraps the MCP Server for direct in-process invocation
// without HTTP or subprocess overhead.
type InProcessServer struct {
	server      *Server
	tools       map[string]ToolHandlerFunc
	schemas     map[string]ToolSchema
	auditLogger *slog.Logger
	requestID   atomic.Uint64
}

// NewInProcessServer creates an InProcessServer with all workflow tools registered.
// All wfctl tools are available without HTTP or subprocess overhead.
func NewInProcessServer(opts ...InProcessOption) *InProcessServer {
	cfg := &inProcessConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	var serverOpts []ServerOption
	if cfg.engine != nil {
		serverOpts = append(serverOpts, WithEngine(cfg.engine))
	}

	s := NewServer(cfg.pluginDir, serverOpts...)
	if cfg.registryDir != "" {
		s.registryDir = cfg.registryDir
	}
	if cfg.documentationFile != "" {
		s.documentationFile = cfg.documentationFile
	}

	// Collect tool schemas from the MCP server's registered tools.
	schemas := make(map[string]ToolSchema)
	for name, st := range s.mcpServer.ListTools() {
		ts := ToolSchema{
			Name:        st.Tool.Name,
			Description: st.Tool.Description,
		}
		if st.Tool.InputSchema.Properties != nil {
			ts.InputSchema = map[string]any{
				"type":       "object",
				"properties": st.Tool.InputSchema.Properties,
			}
			if len(st.Tool.InputSchema.Required) > 0 {
				ts.InputSchema["required"] = st.Tool.InputSchema.Required
			}
		}
		schemas[name] = ts
	}

	return &InProcessServer{
		server:      s,
		tools:       s.toolHandlers,
		schemas:     schemas,
		auditLogger: cfg.auditLogger,
	}
}

// ListTools returns the names of all registered tools.
func (p *InProcessServer) ListTools() []string {
	names := make([]string, 0, len(p.tools))
	for name := range p.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ListToolSchemas returns all registered tools with their parameter schemas.
func (p *InProcessServer) ListToolSchemas() []ToolSchema {
	result := make([]ToolSchema, 0, len(p.schemas))
	for _, s := range p.schemas {
		result = append(result, s)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

// GetToolSchema returns the schema for a specific tool, or false if not found.
func (p *InProcessServer) GetToolSchema(name string) (ToolSchema, bool) {
	s, ok := p.schemas[name]
	return s, ok
}

// CallTool invokes the named tool with the given arguments.
// Returns the tool result, which may be of any type, or an error if the
// tool is not found or invocation fails.
func (p *InProcessServer) CallTool(ctx context.Context, name string, args map[string]any) (any, error) {
	_, known := p.schemas[name]
	status := "protocol_error"
	defer func() {
		if p.auditLogger != nil {
			attributes := []any{"status", status}
			if known {
				attributes = append(attributes, "tool", name)
			}
			p.auditLogger.Info("mcp tool call", attributes...)
		}
	}()

	// String IDs avoid JSON number precision loss while preserving per-instance
	// uniqueness across concurrent calls and SDK cancellation registration.
	id := strconv.FormatUint(p.requestID.Add(1), 10)
	request, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": id, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": args},
	})
	if err != nil {
		status = "invalid_arguments"
		return nil, errors.New("MCP arguments must be JSON-serializable")
	}
	// Use the same SDK dispatcher as stdio, including schema validation and
	// protocol/tool error semantics, rather than calling a raw handler.
	encoded, err := json.Marshal(p.server.MCPServer().HandleMessage(ctx, request))
	if err != nil {
		return nil, errors.New("MCP response could not be encoded")
	}
	var reply struct {
		JSONRPC string                   `json:"jsonrpc"`
		ID      string                   `json:"id"`
		Result  *mcp.CallToolResult      `json:"result"`
		Error   *mcp.JSONRPCErrorDetails `json:"error"`
	}
	if err = json.Unmarshal(encoded, &reply); err != nil || reply.JSONRPC != "2.0" || reply.ID != id {
		return nil, errors.New("invalid MCP response")
	}
	if reply.Error != nil {
		if !known {
			status = "unknown_tool"
		}
		// Protocol messages can contain user names or raw handler errors. The
		// error code supplies bounded feedback without echoing those details.
		return nil, fmt.Errorf("MCP protocol error (%d)", reply.Error.Code)
	}
	if reply.Result == nil {
		return nil, errors.New("missing MCP tool result")
	}
	if reply.Result.IsError {
		status = "tool_error"
		for _, content := range reply.Result.Content {
			if text, ok := content.(mcp.TextContent); ok {
				return nil, fmt.Errorf("MCP tool execution failed: %s", boundedToolFeedback(text.Text))
			}
		}
		return nil, errors.New("MCP tool execution failed")
	}
	status = "ok"
	for _, c := range reply.Result.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			return tc.Text, nil
		}
	}
	return reply.Result, nil
}

// Bound failure feedback only; successful tool text remains unchanged.
func boundedToolFeedback(text string) string {
	const limit = 4096
	if len(text) <= limit {
		return text
	}
	end := limit
	for !utf8.ValidString(text[:end]) {
		end--
	}
	return text[:end] + "…"
}
