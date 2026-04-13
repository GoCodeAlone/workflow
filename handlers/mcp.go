package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/GoCodeAlone/modular"
	workflowmodule "github.com/GoCodeAlone/workflow/module"
)

// MCPHandlerRoute maps an MCP tool name to a pipeline.
type MCPHandlerRoute struct {
	Pipeline    string `yaml:"pipeline" json:"pipeline"`
	Description string `yaml:"description" json:"description"`
}

// MCPHandlerConfig configures an MCP workflow handler that exposes
// a group of pipelines as MCP tools under a named server.
type MCPHandlerConfig struct {
	ServerName   string                     `yaml:"server_name" json:"server_name"`
	LogToolCalls bool                       `yaml:"log_tool_calls" json:"log_tool_calls"`
	Routes       map[string]MCPHandlerRoute `yaml:"routes" json:"routes"`
}

// ToToolDefinitions converts all routes to MCP tool definitions.
func (c *MCPHandlerConfig) ToToolDefinitions() []workflowmodule.MCPToolDefinition {
	defs := make([]workflowmodule.MCPToolDefinition, 0, len(c.Routes))
	for name, route := range c.Routes {
		defs = append(defs, workflowmodule.MCPToolDefinition{
			Name:        name,
			Description: route.Description,
			InputSchema: workflowmodule.MCPToolInputSchema{
				Type:       "object",
				Properties: map[string]workflowmodule.MCPToolPropDef{},
			},
		})
	}
	return defs
}

// MCPWorkflowHandler handles MCP-type workflows by routing tool calls to pipelines.
type MCPWorkflowHandler struct{}

// NewMCPWorkflowHandler creates a new MCP workflow handler.
func NewMCPWorkflowHandler() *MCPWorkflowHandler {
	return &MCPWorkflowHandler{}
}

// CanHandle returns true for the "mcp" workflow type and any "mcp-" prefixed types.
func (h *MCPWorkflowHandler) CanHandle(workflowType string) bool {
	return workflowType == "mcp" || strings.HasPrefix(workflowType, "mcp-")
}

// ConfigureWorkflow sets up the MCP workflow from configuration.
func (h *MCPWorkflowHandler) ConfigureWorkflow(_ modular.Application, workflowConfig any) error {
	_, ok := workflowConfig.(map[string]any)
	if !ok {
		return fmt.Errorf("invalid MCP workflow configuration format")
	}
	return nil
}

// ExecuteWorkflow executes an MCP tool call by routing the action to a pipeline.
func (h *MCPWorkflowHandler) ExecuteWorkflow(_ context.Context, _ string, _ string, _ map[string]any) (map[string]any, error) {
	// MCP tool calls are dispatched through the MCPToolTrigger, not this handler.
	// This handler's ConfigureWorkflow is invoked at startup to validate the workflow config.
	return map[string]any{}, nil
}
