package mcp

import "context"

// MCPProvider is the interface for invoking MCP tools in-process without
// HTTP or subprocess overhead.
type MCPProvider interface {
	// ListTools returns the names of all registered tools.
	ListTools() []string
	// ListToolSchemas returns all tools with their parameter schemas.
	ListToolSchemas() []ToolSchema
	// CallTool invokes the named tool with the given arguments.
	// Returns the tool result, which may be of any type, or an error.
	CallTool(ctx context.Context, name string, args map[string]any) (any, error)
}
