// Package generic provides an AIProvider implementation for any OpenAI-compatible API.
// This can be used with providers like Ollama, Together AI, Fireworks, vLLM,
// or any other service that implements the OpenAI chat completions API format.
package generic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/GoCodeAlone/workflow/ai"
)

// Config holds configuration for a generic OpenAI-compatible provider.
type Config struct {
	// Name is the unique provider name (e.g., "ollama", "together", "fireworks").
	Name string

	// BaseURL is the base URL for the API (e.g., "http://localhost:11434/v1").
	BaseURL string

	// APIKey is the bearer token for authentication (optional for local providers).
	APIKey string //nolint:gosec // G117: config field

	// Models lists the models available from this provider.
	Models []ai.ModelInfo

	// Headers adds extra HTTP headers to every request.
	Headers map[string]string

	// SupportsTools indicates whether this provider supports function/tool calling.
	SupportsTools bool
}

// Provider implements ai.AIProvider for any OpenAI-compatible API.
type Provider struct {
	name          string
	baseURL       string
	apiKey        string
	models        []ai.ModelInfo
	headers       map[string]string
	supportsTools bool
	httpClient    *http.Client
}

// New creates a new generic OpenAI-compatible provider.
func New(cfg Config) (*Provider, error) {
	if cfg.Name == "" {
		return nil, fmt.Errorf("generic: provider name is required")
	}
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("generic: base URL is required")
	}

	// Set provider field on all models
	models := make([]ai.ModelInfo, len(cfg.Models))
	for i, m := range cfg.Models {
		m.Provider = cfg.Name
		models[i] = m
	}

	return &Provider{
		name:          cfg.Name,
		baseURL:       cfg.BaseURL,
		apiKey:        cfg.APIKey,
		models:        models,
		headers:       cfg.Headers,
		supportsTools: cfg.SupportsTools,
		httpClient:    &http.Client{},
	}, nil
}

func (p *Provider) Name() string           { return p.name }
func (p *Provider) Models() []ai.ModelInfo { return p.models }
func (p *Provider) SupportsToolUse() bool  { return p.supportsTools }

// -- OpenAI-compatible API types (same format as OpenAI) --

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type functionDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters"`
}

type toolDef struct {
	Type     string      `json:"type"`
	Function functionDef `json:"function"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Temperature *float64      `json:"temperature,omitempty"`
	Tools       []toolDef     `json:"tools,omitempty"`
}

type toolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type responseToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function toolCallFunction `json:"function"`
}

type chatChoice struct {
	Message struct {
		Role      string             `json:"role"`
		Content   *string            `json:"content"`
		ToolCalls []responseToolCall `json:"tool_calls,omitempty"`
	} `json:"message"`
	FinishReason string `json:"finish_reason"`
}

type chatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

type chatResponse struct {
	ID      string       `json:"id"`
	Model   string       `json:"model"`
	Choices []chatChoice `json:"choices"`
	Usage   chatUsage    `json:"usage"`
}

func (p *Provider) doRequest(ctx context.Context, req chatRequest) (*chatResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("%s: marshal request: %w", p.name, err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%s: create request: %w", p.name, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	for k, v := range p.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := p.httpClient.Do(httpReq) //nolint:gosec // G704: URL from configured provider endpoint
	if err != nil {
		return nil, fmt.Errorf("%s: request failed: %w", p.name, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%s: read response: %w", p.name, err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: API error (status %d): %s", p.name, resp.StatusCode, string(respBody))
	}

	var chatResp chatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("%s: parse response: %w", p.name, err)
	}
	return &chatResp, nil
}

func (p *Provider) buildMessages(req ai.CompletionRequest) []chatMessage {
	var msgs []chatMessage
	if req.SystemPrompt != "" {
		msgs = append(msgs, chatMessage{Role: "system", Content: req.SystemPrompt})
	}
	for _, m := range req.Messages {
		msgs = append(msgs, chatMessage{Role: m.Role, Content: m.Content})
	}
	return msgs
}

func (p *Provider) defaultModel() string {
	if len(p.models) > 0 {
		return p.models[0].ID
	}
	return ""
}

func (p *Provider) Complete(ctx context.Context, req ai.CompletionRequest) (*ai.CompletionResponse, error) {
	model := req.Model
	if model == "" {
		model = p.defaultModel()
	}

	chatReq := chatRequest{
		Model:     model,
		Messages:  p.buildMessages(req),
		MaxTokens: req.MaxTokens,
	}
	if req.Temperature > 0 {
		chatReq.Temperature = &req.Temperature
	}

	resp, err := p.doRequest(ctx, chatReq)
	if err != nil {
		return nil, err
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("%s: no choices in response", p.name)
	}

	choice := resp.Choices[0]
	content := ""
	if choice.Message.Content != nil {
		content = *choice.Message.Content
	}

	return &ai.CompletionResponse{
		ID:           resp.ID,
		Model:        resp.Model,
		Content:      content,
		Usage:        ai.TokenUsage{InputTokens: resp.Usage.PromptTokens, OutputTokens: resp.Usage.CompletionTokens},
		FinishReason: choice.FinishReason,
	}, nil
}

func (p *Provider) CompleteStream(_ context.Context, _ ai.CompletionRequest) (<-chan ai.StreamChunk, error) {
	return nil, fmt.Errorf("%s: streaming not yet implemented", p.name)
}

func (p *Provider) ToolComplete(ctx context.Context, req ai.ToolCompletionRequest) (*ai.ToolCompletionResponse, error) {
	if !p.supportsTools {
		return nil, fmt.Errorf("%s: tool use not supported", p.name)
	}

	model := req.Model
	if model == "" {
		model = p.defaultModel()
	}

	tools := make([]toolDef, len(req.Tools))
	for i, t := range req.Tools {
		tools[i] = toolDef{
			Type: "function",
			Function: functionDef{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		}
	}

	chatReq := chatRequest{
		Model:     model,
		Messages:  p.buildMessages(req.CompletionRequest),
		MaxTokens: req.MaxTokens,
		Tools:     tools,
	}
	if req.Temperature > 0 {
		chatReq.Temperature = &req.Temperature
	}

	resp, err := p.doRequest(ctx, chatReq)
	if err != nil {
		return nil, err
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("%s: no choices in response", p.name)
	}

	choice := resp.Choices[0]
	content := ""
	if choice.Message.Content != nil {
		content = *choice.Message.Content
	}

	var toolCalls []ai.ToolCall
	for _, tc := range choice.Message.ToolCalls {
		var input map[string]any
		if tc.Function.Arguments != "" {
			_ = json.Unmarshal([]byte(tc.Function.Arguments), &input)
		}
		toolCalls = append(toolCalls, ai.ToolCall{
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: input,
		})
	}

	return &ai.ToolCompletionResponse{
		CompletionResponse: ai.CompletionResponse{
			ID:           resp.ID,
			Model:        resp.Model,
			Content:      content,
			Usage:        ai.TokenUsage{InputTokens: resp.Usage.PromptTokens, OutputTokens: resp.Usage.CompletionTokens},
			FinishReason: choice.FinishReason,
		},
		ToolCalls: toolCalls,
	}, nil
}
