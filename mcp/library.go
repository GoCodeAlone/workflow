package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

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
type InProcessServer struct {
	server      *Server
	tools       map[string]ToolHandlerFunc
	auditLogger *slog.Logger
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

	return &InProcessServer{
		server:      s,
		tools:       s.toolHandlers,
		auditLogger: cfg.auditLogger,
	}
}

// ListTools returns the names of all registered tools.
func (p *InProcessServer) ListTools() []string {
	names := make([]string, 0, len(p.tools))
	for name := range p.tools {
		names = append(names, name)
	}
	return names
}

// CallTool invokes the named tool with the given arguments.
// Returns the tool result, which may be of any type, or an error if the
// tool is not found or invocation fails.
func (p *InProcessServer) CallTool(ctx context.Context, name string, args map[string]any) (any, error) {
	handler, ok := p.tools[name]
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", name)
	}

	argsJSON, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("marshal args: %w", err)
	}

	var req mcp.CallToolRequest
	req.Params.Name = name
	req.Params.Arguments = make(map[string]any)
	if err := json.Unmarshal(argsJSON, &req.Params.Arguments); err != nil {
		return nil, fmt.Errorf("unmarshal args: %w", err)
	}

	result, callErr := handler(ctx, req)
	if p.auditLogger != nil {
		p.auditLogger.Info("mcp tool call",
			"tool", name,
			"error", callErr,
		)
	}
	if callErr != nil {
		return nil, callErr
	}

	for _, c := range result.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			return tc.Text, nil
		}
	}
	return result, nil
}
