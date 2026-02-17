// Package openai provides an AIProvider implementation for the OpenAI API.
package openai

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
	defaultModel   = "gpt-4o"
	defaultBaseURL = "https://api.openai.com/v1"
)

// Config holds configuration for the OpenAI provider.
type Config struct {
	APIKey  string // Defaults to OPENAI_API_KEY env var
	Model   string // Defaults to gpt-4o
	BaseURL string // Defaults to https://api.openai.com/v1
}

// Provider implements ai.AIProvider for OpenAI models.
type Provider struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient *http.Client
}

// New creates a new OpenAI AIProvider.
func New(cfg Config) (*Provider, error) {
	apiKey := cfg.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("openai: OPENAI_API_KEY not set")
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

func (p *Provider) Name() string { return "openai" }

func (p *Provider) Models() []ai.ModelInfo {
	return []ai.ModelInfo{
		{
			ID:              "gpt-4o",
			Name:            "GPT-4o",
			Provider:        "openai",
			ContextWindow:   128000,
			MaxOutput:       16384,
			SupportsTools:   true,
			SupportsVision:  true,
			CostPer1KInput:  0.005,
			CostPer1KOutput: 0.015,
			Overridable:     []string{"maxTokens", "temperature"},
		},
		{
			ID:              "gpt-4o-mini",
			Name:            "GPT-4o Mini",
			Provider:        "openai",
			ContextWindow:   128000,
			MaxOutput:       16384,
			SupportsTools:   true,
			SupportsVision:  true,
			CostPer1KInput:  0.00015,
			CostPer1KOutput: 0.0006,
			Overridable:     []string{"maxTokens", "temperature"},
		},
	}
}

func (p *Provider) SupportsToolUse() bool { return true }

// -- OpenAI API types --

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
		return nil, fmt.Errorf("openai: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openai: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai: API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var chatResp chatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("openai: parse response: %w", err)
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

func (p *Provider) Complete(ctx context.Context, req ai.CompletionRequest) (*ai.CompletionResponse, error) {
	model := req.Model
	if model == "" {
		model = p.model
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
		return nil, fmt.Errorf("openai: no choices in response")
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
	return nil, fmt.Errorf("openai: streaming not yet implemented")
}

func (p *Provider) ToolComplete(ctx context.Context, req ai.ToolCompletionRequest) (*ai.ToolCompletionResponse, error) {
	model := req.Model
	if model == "" {
		model = p.model
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
		return nil, fmt.Errorf("openai: no choices in response")
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
