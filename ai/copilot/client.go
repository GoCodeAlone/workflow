package copilotai

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/GoCodeAlone/workflow/ai"
	aillm "github.com/GoCodeAlone/workflow/ai/llm"
	"github.com/GoCodeAlone/workflow/config"
	copilot "github.com/github/copilot-sdk/go"
)

// ClientConfig holds configuration for the Copilot SDK client.
type ClientConfig struct {
	// CLIPath is the path to the Copilot CLI binary.
	CLIPath string
	// Model to use for sessions (e.g., "claude-sonnet-4-20250514").
	Model string
	// Provider configures BYOK (Bring Your Own Key) for custom model providers.
	Provider *copilot.ProviderConfig
}

// Client implements ai.WorkflowGenerator using the GitHub Copilot SDK.
type Client struct {
	cfg     ClientConfig
	wrapper ClientWrapper
}

// NewClient creates a new Copilot SDK client. The Copilot CLI must be available.
func NewClient(cfg ClientConfig) (*Client, error) {
	cliPath := cfg.CLIPath
	if cliPath == "" {
		cliPath = "copilot"
	}

	cli := copilot.NewClient(&copilot.ClientOptions{
		CLIPath: cliPath,
	})

	return &Client{
		cfg:     cfg,
		wrapper: &realClientWrapper{cli: cli},
	}, nil
}

// workflowTools returns the Copilot SDK tool definitions that mirror the LLM tools.
func workflowTools() []copilot.Tool {
	return []copilot.Tool{
		{
			Name:        "list_components",
			Description: "Lists all available built-in module types and their descriptions.",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			Handler: func(inv copilot.ToolInvocation) (copilot.ToolResult, error) {
				result, err := aillm.HandleToolCall("list_components", nil)
				if err != nil {
					return copilot.ToolResult{}, err
				}
				return copilot.ToolResult{TextResultForLLM: result, ResultType: "success"}, nil
			},
		},
		{
			Name:        "get_component_schema",
			Description: "Returns the configuration schema for a specific module type.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"module_type": map[string]any{
						"type":        "string",
						"description": "The module type to get the schema for",
					},
				},
				"required": []string{"module_type"},
			},
			Handler: func(inv copilot.ToolInvocation) (copilot.ToolResult, error) {
				input, _ := json.Marshal(inv.Arguments)
				result, err := aillm.HandleToolCall("get_component_schema", input)
				if err != nil {
					return copilot.ToolResult{}, err
				}
				return copilot.ToolResult{TextResultForLLM: result, ResultType: "success"}, nil
			},
		},
		{
			Name:        "validate_config",
			Description: "Validates a workflow configuration YAML string.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"config_yaml": map[string]any{
						"type":        "string",
						"description": "The workflow configuration as a YAML string",
					},
				},
				"required": []string{"config_yaml"},
			},
			Handler: func(inv copilot.ToolInvocation) (copilot.ToolResult, error) {
				input, _ := json.Marshal(inv.Arguments)
				result, err := aillm.HandleToolCall("validate_config", input)
				if err != nil {
					return copilot.ToolResult{}, err
				}
				return copilot.ToolResult{TextResultForLLM: result, ResultType: "success"}, nil
			},
		},
		{
			Name:        "get_example_workflow",
			Description: "Returns an example workflow configuration for a given category.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"category": map[string]any{
						"type":        "string",
						"description": "Category: 'http', 'messaging', 'statemachine', 'event', 'trigger'",
					},
				},
				"required": []string{"category"},
			},
			Handler: func(inv copilot.ToolInvocation) (copilot.ToolResult, error) {
				input, _ := json.Marshal(inv.Arguments)
				result, err := aillm.HandleToolCall("get_example_workflow", input)
				if err != nil {
					return copilot.ToolResult{}, err
				}
				return copilot.ToolResult{TextResultForLLM: result, ResultType: "success"}, nil
			},
		},
	}
}

func (c *Client) createSession(ctx context.Context) (SessionWrapper, error) {
	session, err := c.wrapper.CreateSession(ctx, &copilot.SessionConfig{
		Model: c.cfg.Model,
		Tools: workflowTools(),
		SystemMessage: &copilot.SystemMessageConfig{
			Mode:    "append",
			Content: ai.SystemPrompt(),
		},
		Provider: c.cfg.Provider,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Copilot session: %w", err)
	}
	return session, nil
}

// GenerateWorkflow creates a workflow config from a natural language request.
func (c *Client) GenerateWorkflow(ctx context.Context, req ai.GenerateRequest) (*ai.GenerateResponse, error) {
	session, err := c.createSession(ctx)
	if err != nil {
		return nil, err
	}
	defer session.Destroy()

	prompt := ai.GeneratePrompt(req)

	resp, err := session.SendAndWait(ctx, copilot.MessageOptions{Prompt: prompt})
	if err != nil {
		return nil, fmt.Errorf("Copilot request failed: %w", err)
	}

	if resp == nil || resp.Data.Content == nil {
		return nil, fmt.Errorf("empty response from Copilot")
	}

	text := *resp.Data.Content
	jsonStr := aillm.ExtractJSON(text)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON found in Copilot response")
	}

	var result ai.GenerateResponse
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return &result, nil
}

// GenerateComponent generates Go source code for a component.
func (c *Client) GenerateComponent(ctx context.Context, spec ai.ComponentSpec) (string, error) {
	session, err := c.createSession(ctx)
	if err != nil {
		return "", err
	}
	defer session.Destroy()

	prompt := ai.ComponentPrompt(spec)

	resp, err := session.SendAndWait(ctx, copilot.MessageOptions{Prompt: prompt})
	if err != nil {
		return "", fmt.Errorf("Copilot request failed: %w", err)
	}

	if resp == nil || resp.Data.Content == nil {
		return "", fmt.Errorf("empty response from Copilot")
	}

	return aillm.ExtractCode(*resp.Data.Content), nil
}

// SuggestWorkflow returns workflow suggestions for a use case.
func (c *Client) SuggestWorkflow(ctx context.Context, useCase string) ([]ai.WorkflowSuggestion, error) {
	session, err := c.createSession(ctx)
	if err != nil {
		return nil, err
	}
	defer session.Destroy()

	prompt := ai.SuggestPrompt(useCase)

	resp, err := session.SendAndWait(ctx, copilot.MessageOptions{Prompt: prompt})
	if err != nil {
		return nil, fmt.Errorf("Copilot request failed: %w", err)
	}

	if resp == nil || resp.Data.Content == nil {
		return nil, fmt.Errorf("empty response from Copilot")
	}

	text := *resp.Data.Content
	jsonStr := aillm.ExtractJSON(text)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON found in Copilot response")
	}

	var suggestions []ai.WorkflowSuggestion
	if err := json.Unmarshal([]byte(jsonStr), &suggestions); err != nil {
		return nil, fmt.Errorf("failed to parse suggestions: %w", err)
	}
	return suggestions, nil
}

// IdentifyMissingComponents analyzes a config for non-built-in module types.
func (c *Client) IdentifyMissingComponents(ctx context.Context, cfg *config.WorkflowConfig) ([]ai.ComponentSpec, error) {
	var types []string
	for _, mod := range cfg.Modules {
		types = append(types, mod.Type)
	}

	session, err := c.createSession(ctx)
	if err != nil {
		return nil, err
	}
	defer session.Destroy()

	prompt := ai.MissingComponentsPrompt(types)

	resp, err := session.SendAndWait(ctx, copilot.MessageOptions{Prompt: prompt})
	if err != nil {
		return nil, fmt.Errorf("Copilot request failed: %w", err)
	}

	if resp == nil || resp.Data.Content == nil {
		return nil, fmt.Errorf("empty response from Copilot")
	}

	text := *resp.Data.Content
	jsonStr := aillm.ExtractJSON(text)
	if jsonStr == "" {
		return nil, nil // no missing components
	}

	var specs []ai.ComponentSpec
	if err := json.Unmarshal([]byte(jsonStr), &specs); err != nil {
		return nil, fmt.Errorf("failed to parse missing components: %w", err)
	}
	return specs, nil
}
