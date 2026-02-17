// Package anthropic provides an AIProvider implementation for the Anthropic Claude API.
// It wraps the HTTP patterns from ai/llm/client.go into the pluggable AIProvider interface.
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/GoCodeAlone/workflow/ai"
)

const (
	defaultModel   = "claude-sonnet-4-20250514"
	defaultBaseURL = "https://api.anthropic.com"
	apiVersion     = "2023-06-01"
)

// Config holds configuration for the Anthropic provider.
type Config struct {
	APIKey  string // Defaults to ANTHROPIC_API_KEY env var
	Model   string // Defaults to claude-sonnet-4-20250514
	BaseURL string // Defaults to https://api.anthropic.com
}

// Provider implements ai.AIProvider for Anthropic's Claude models.
type Provider struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient *http.Client
}

// New creates a new Anthropic AIProvider.
func New(cfg Config) (*Provider, error) {
	apiKey := cfg.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("anthropic: ANTHROPIC_API_KEY not set")
	}

	model := cfg.Model
	if model == "" {
		model = defaultModel
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	return &Provider{
		apiKey:     apiKey,
		model:      model,
		baseURL:    baseURL,
		httpClient: &http.Client{},
	}, nil
}

func (p *Provider) Name() string { return "anthropic" }

func (p *Provider) Models() []ai.ModelInfo {
	return []ai.ModelInfo{
		{
			ID:              "claude-sonnet-4-20250514",
			Name:            "Claude Sonnet 4",
			Provider:        "anthropic",
			ContextWindow:   200000,
			MaxOutput:       8192,
			SupportsTools:   true,
			SupportsVision:  true,
			CostPer1KInput:  0.003,
			CostPer1KOutput: 0.015,
			Overridable:     []string{"maxTokens", "temperature"},
		},
		{
			ID:              "claude-opus-4-20250514",
			Name:            "Claude Opus 4",
			Provider:        "anthropic",
			ContextWindow:   200000,
			MaxOutput:       8192,
			SupportsTools:   true,
			SupportsVision:  true,
			CostPer1KInput:  0.015,
			CostPer1KOutput: 0.075,
			Overridable:     []string{"maxTokens", "temperature"},
		},
		{
			ID:              "claude-haiku-3-20250307",
			Name:            "Claude Haiku 3",
			Provider:        "anthropic",
			ContextWindow:   200000,
			MaxOutput:       4096,
			SupportsTools:   true,
			SupportsVision:  true,
			CostPer1KInput:  0.00025,
			CostPer1KOutput: 0.00125,
			Overridable:     []string{"maxTokens", "temperature"},
		},
	}
}

func (p *Provider) SupportsToolUse() bool { return true }

// -- Anthropic API types --

type apiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type apiToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type apiRequest struct {
	Model       string       `json:"model"`
	MaxTokens   int          `json:"max_tokens"`
	System      string       `json:"system,omitempty"`
	Messages    []apiMessage `json:"messages"`
	Tools       []apiToolDef `json:"tools,omitempty"`
	Temperature *float64     `json:"temperature,omitempty"`
}

type contentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type apiUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type apiResponse struct {
	ID         string         `json:"id"`
	Model      string         `json:"model"`
	Content    []contentBlock `json:"content"`
	StopReason string         `json:"stop_reason"`
	Usage      apiUsage       `json:"usage"`
}

func (p *Provider) doRequest(ctx context.Context, req apiRequest) (*apiResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("anthropic: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", apiVersion)

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("anthropic: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic: API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var apiResp apiResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("anthropic: parse response: %w", err)
	}
	return &apiResp, nil
}

func (p *Provider) Complete(ctx context.Context, req ai.CompletionRequest) (*ai.CompletionResponse, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 4096
	}

	msgs := make([]apiMessage, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = apiMessage{Role: m.Role, Content: m.Content}
	}

	apiReq := apiRequest{
		Model:     model,
		MaxTokens: maxTokens,
		System:    req.SystemPrompt,
		Messages:  msgs,
	}
	if req.Temperature > 0 {
		apiReq.Temperature = &req.Temperature
	}

	resp, err := p.doRequest(ctx, apiReq)
	if err != nil {
		return nil, err
	}

	var content string
	for _, block := range resp.Content {
		if block.Type == "text" {
			content += block.Text
		}
	}

	return &ai.CompletionResponse{
		ID:           resp.ID,
		Model:        resp.Model,
		Content:      content,
		Usage:        ai.TokenUsage{InputTokens: resp.Usage.InputTokens, OutputTokens: resp.Usage.OutputTokens},
		FinishReason: resp.StopReason,
	}, nil
}

func (p *Provider) CompleteStream(_ context.Context, _ ai.CompletionRequest) (<-chan ai.StreamChunk, error) {
	// Streaming requires SSE parsing; return a single-chunk fallback for now.
	return nil, fmt.Errorf("anthropic: streaming not yet implemented")
}

func (p *Provider) ToolComplete(ctx context.Context, req ai.ToolCompletionRequest) (*ai.ToolCompletionResponse, error) {
	model := req.Model
	if model == "" {
		model = p.model
	}
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = 4096
	}

	msgs := make([]apiMessage, len(req.Messages))
	for i, m := range req.Messages {
		msgs[i] = apiMessage{Role: m.Role, Content: m.Content}
	}

	tools := make([]apiToolDef, len(req.Tools))
	for i, t := range req.Tools {
		tools[i] = apiToolDef{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		}
	}

	apiReq := apiRequest{
		Model:     model,
		MaxTokens: maxTokens,
		System:    req.SystemPrompt,
		Messages:  msgs,
		Tools:     tools,
	}
	if req.Temperature > 0 {
		apiReq.Temperature = &req.Temperature
	}

	resp, err := p.doRequest(ctx, apiReq)
	if err != nil {
		return nil, err
	}

	var content string
	var toolCalls []ai.ToolCall
	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			content += block.Text
		case "tool_use":
			var input map[string]any
			if len(block.Input) > 0 {
				_ = json.Unmarshal(block.Input, &input)
			}
			toolCalls = append(toolCalls, ai.ToolCall{
				ID:    block.ID,
				Name:  block.Name,
				Input: input,
			})
		}
	}

	return &ai.ToolCompletionResponse{
		CompletionResponse: ai.CompletionResponse{
			ID:           resp.ID,
			Model:        resp.Model,
			Content:      content,
			Usage:        ai.TokenUsage{InputTokens: resp.Usage.InputTokens, OutputTokens: resp.Usage.OutputTokens},
			FinishReason: resp.StopReason,
		},
		ToolCalls: toolCalls,
	}, nil
}
