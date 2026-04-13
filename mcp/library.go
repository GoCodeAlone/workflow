package mcp

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
)

// InProcessServer exposes the workflow MCP tools for direct in-process
// invocation without HTTP or subprocess overhead.
type InProcessServer struct {
	s *Server
}

// NewInProcessServer creates an InProcessServer with all workflow tools registered.
func NewInProcessServer() *InProcessServer {
	return &InProcessServer{s: NewServer("")}
}

// ListTools returns the names of all registered tools.
func (p *InProcessServer) ListTools() []string {
	toolMap := p.s.MCPServer().ListTools()
	names := make([]string, 0, len(toolMap))
	for name := range toolMap {
		names = append(names, name)
	}
	return names
}

// CallTool invokes the named tool with the given arguments.
// Returns the text content of the result as a string, or an error if the
// tool is not found or invocation fails.
func (p *InProcessServer) CallTool(ctx context.Context, name string, args map[string]any) (any, error) {
	toolMap := p.s.MCPServer().ListTools()
	st, ok := toolMap[name]
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", name)
	}

	var req mcp.CallToolRequest
	req.Params.Name = name
	req.Params.Arguments = args

	result, err := st.Handler(ctx, req)
	if err != nil {
		return nil, err
	}

	for _, c := range result.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			return tc.Text, nil
		}
	}
	return result, nil
}
