package module

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// MCPToolTriggerName is the canonical name / service key for the MCP tool trigger.
const MCPToolTriggerName = "trigger.mcp_tool"

// MCPToolParameter defines a parameter for an MCP tool exposed via a pipeline trigger.
type MCPToolParameter struct {
	Name        string   `yaml:"name" json:"name"`
	Type        string   `yaml:"type" json:"type"` // string, integer, number, boolean, array, object
	Required    bool     `yaml:"required" json:"required"`
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	Enum        []string `yaml:"enum,omitempty" json:"enum,omitempty"`
}

// MCPToolTriggerConfig configures a pipeline to be exposed as an MCP tool.
type MCPToolTriggerConfig struct {
	ToolName    string             `yaml:"tool_name" json:"tool_name"`
	Description string             `yaml:"description" json:"description"`
	Parameters  []MCPToolParameter `yaml:"parameters,omitempty" json:"parameters,omitempty"`
}

// MCPToolPropDef is a single property definition in the JSON Schema for tool inputs.
type MCPToolPropDef struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

// MCPToolInputSchema is the JSON Schema for an MCP tool's parameters.
type MCPToolInputSchema struct {
	Type       string                    `json:"type"`
	Properties map[string]MCPToolPropDef `json:"properties"`
	Required   []string                  `json:"required,omitempty"`
}

// MCPToolDefinition is the MCP-compatible tool schema used for tool discovery.
type MCPToolDefinition struct {
	Name        string             `json:"name"`
	Description string             `json:"description"`
	InputSchema MCPToolInputSchema `json:"inputSchema"`
}

// ToToolDefinition converts the trigger config to an MCP tool definition.
func (c *MCPToolTriggerConfig) ToToolDefinition() MCPToolDefinition {
	props := make(map[string]MCPToolPropDef)
	var required []string
	for _, p := range c.Parameters {
		props[p.Name] = MCPToolPropDef{
			Type:        p.Type,
			Description: p.Description,
			Enum:        p.Enum,
		}
		if p.Required {
			required = append(required, p.Name)
		}
	}
	return MCPToolDefinition{
		Name:        c.ToolName,
		Description: c.Description,
		InputSchema: MCPToolInputSchema{
			Type:       "object",
			Properties: props,
			Required:   required,
		},
	}
}

// MCPToolTriggerRuntime bridges MCP tool calls to pipeline execution.
// It is created by MCPToolTrigger.Configure() for each registered pipeline tool.
type MCPToolTriggerRuntime struct {
	config   MCPToolTriggerConfig
	pipeline string
	executor interfaces.PipelineExecutor
}

// NewMCPToolTriggerRuntime creates a runtime bound to a specific pipeline.
func NewMCPToolTriggerRuntime(config MCPToolTriggerConfig, pipeline string, executor interfaces.PipelineExecutor) *MCPToolTriggerRuntime {
	return &MCPToolTriggerRuntime{
		config:   config,
		pipeline: pipeline,
		executor: executor,
	}
}

// HandleToolCall executes the bound pipeline with the given tool arguments.
func (r *MCPToolTriggerRuntime) HandleToolCall(ctx context.Context, args map[string]any) (any, error) {
	if r.executor == nil {
		return nil, fmt.Errorf("mcp_tool: no pipeline executor available")
	}
	result, err := r.executor.ExecutePipeline(ctx, r.pipeline, args)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// ToolName returns the MCP tool name for this runtime.
func (r *MCPToolTriggerRuntime) ToolName() string {
	return r.config.ToolName
}

// Definition returns the MCP tool definition for this runtime.
func (r *MCPToolTriggerRuntime) Definition() MCPToolDefinition {
	return r.config.ToToolDefinition()
}

// MCPToolTrigger implements interfaces.Trigger for the mcp_tool trigger type.
// It maps MCP tool calls to pipeline executions.
type MCPToolTrigger struct {
	name     string
	tools    map[string]*MCPToolTriggerRuntime
	executor interfaces.PipelineExecutor
}

// NewMCPToolTrigger creates a new MCPToolTrigger.
func NewMCPToolTrigger() *MCPToolTrigger {
	return &MCPToolTrigger{
		name:  MCPToolTriggerName,
		tools: make(map[string]*MCPToolTriggerRuntime),
	}
}

// Name returns the trigger's canonical name.
func (t *MCPToolTrigger) Name() string { return t.name }

// Dependencies returns nil — the MCP tool trigger has no module dependencies.
func (t *MCPToolTrigger) Dependencies() []string { return nil }

// Init is a no-op.
func (t *MCPToolTrigger) Init(_ modular.Application) error { return nil }

// Start is a no-op — MCP tool calls are dispatched synchronously.
func (t *MCPToolTrigger) Start(_ context.Context) error { return nil }

// Stop is a no-op.
func (t *MCPToolTrigger) Stop(_ context.Context) error { return nil }

// Configure processes a single tool→pipeline mapping from a pipeline's trigger config.
// The enriched config map must contain:
//
//	tool_name     — the MCP tool name (e.g. "analyze_logs")
//	workflowType  — pipeline identifier (e.g. "pipeline:analyze-logs"), injected by engine
//
// Optional fields: description, parameters.
func (t *MCPToolTrigger) Configure(app modular.Application, triggerConfig any) error {
	cfg, ok := triggerConfig.(map[string]any)
	if !ok {
		return fmt.Errorf("mcp_tool trigger: invalid config type %T (expected map[string]any)", triggerConfig)
	}

	// Register as a service (idempotent for same instance).
	if err := app.RegisterService(t.name, t); err != nil {
		// Check if the existing entry is this same instance.
		sameInstance := false
		for _, svc := range app.SvcRegistry() {
			if existing, ok := svc.(*MCPToolTrigger); ok && existing == t {
				sameInstance = true
				break
			}
		}
		if !sameInstance {
			return fmt.Errorf("mcp_tool trigger: registering service %q: %w", t.name, err)
		}
	}

	// Discover pipeline executor from app services.
	if t.executor == nil {
		for _, svc := range app.SvcRegistry() {
			if e, ok := svc.(interfaces.PipelineExecutor); ok {
				t.executor = e
				break
			}
		}
	}

	toolName, _ := cfg["tool_name"].(string)
	description, _ := cfg["description"].(string)
	workflowType, _ := cfg["workflowType"].(string)

	if toolName == "" {
		return fmt.Errorf("mcp_tool trigger: 'tool_name' is required in trigger config")
	}
	if workflowType == "" {
		return fmt.Errorf("mcp_tool trigger: 'workflowType' is required in trigger config (injected by the engine)")
	}

	// Parse parameters from config.
	var params []MCPToolParameter
	if rawParams, ok := cfg["parameters"].([]any); ok {
		for _, rp := range rawParams {
			pm, ok := rp.(map[string]any)
			if !ok {
				continue
			}
			p := MCPToolParameter{}
			if n, ok := pm["name"].(string); ok {
				p.Name = n
			}
			if typ, ok := pm["type"].(string); ok {
				p.Type = typ
			}
			if req, ok := pm["required"].(bool); ok {
				p.Required = req
			}
			if desc, ok := pm["description"].(string); ok {
				p.Description = desc
			}
			params = append(params, p)
		}
	}

	toolCfg := MCPToolTriggerConfig{
		ToolName:    toolName,
		Description: description,
		Parameters:  params,
	}

	t.tools[toolName] = NewMCPToolTriggerRuntime(toolCfg, workflowType, t.executor)
	return nil
}

// GetRuntime returns the runtime for the given tool name, if registered.
func (t *MCPToolTrigger) GetRuntime(toolName string) (*MCPToolTriggerRuntime, bool) {
	r, ok := t.tools[toolName]
	return r, ok
}

// ListTools returns MCP tool definitions for all registered tool pipelines.
func (t *MCPToolTrigger) ListTools() []MCPToolDefinition {
	defs := make([]MCPToolDefinition, 0, len(t.tools))
	for _, r := range t.tools {
		defs = append(defs, r.Definition())
	}
	return defs
}
