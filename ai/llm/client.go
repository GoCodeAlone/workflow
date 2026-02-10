package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/GoCodeAlone/workflow/ai"
	"github.com/GoCodeAlone/workflow/config"
	"gopkg.in/yaml.v3"
)

const (
	defaultModel   = "claude-sonnet-4-20250514"
	defaultBaseURL = "https://api.anthropic.com"
	apiVersion     = "2023-06-01"
	maxTokens      = 4096
)

// ClientConfig holds configuration for the Anthropic LLM client.
type ClientConfig struct {
	APIKey  string // Defaults to ANTHROPIC_API_KEY env var
	Model   string // Defaults to claude-sonnet-4-20250514
	BaseURL string // Defaults to https://api.anthropic.com
}

// Client implements ai.WorkflowGenerator using the Anthropic Claude API.
type Client struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new Anthropic LLM client.
func NewClient(cfg ClientConfig) (*Client, error) {
	apiKey := cfg.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY not set")
	}

	model := cfg.Model
	if model == "" {
		model = defaultModel
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	return &Client{
		apiKey:     apiKey,
		model:      model,
		baseURL:    baseURL,
		httpClient: &http.Client{},
	}, nil
}

// -- Anthropic API types --

type message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type toolDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

type apiRequest struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	System    string    `json:"system,omitempty"`
	Messages  []message `json:"messages"`
	Tools     []toolDef `json:"tools,omitempty"`
}

type contentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type apiResponse struct {
	ID         string         `json:"id"`
	Content    []contentBlock `json:"content"`
	StopReason string         `json:"stop_reason"`
}

func (c *Client) call(ctx context.Context, system string, messages []message, tools []toolDef) (*apiResponse, error) {
	req := apiRequest{
		Model:     c.model,
		MaxTokens: maxTokens,
		System:    system,
		Messages:  messages,
		Tools:     tools,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.apiKey)
	httpReq.Header.Set("anthropic-version", apiVersion)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var apiResp apiResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return &apiResp, nil
}

// callWithToolLoop sends messages to the API and handles tool_use loops until
// the model returns a final text response.
func (c *Client) callWithToolLoop(ctx context.Context, system string, userContent string) (string, error) {
	tools := Tools()
	apiTools := make([]toolDef, len(tools))
	for i, t := range tools {
		apiTools[i] = toolDef(t)
	}

	userMsg, _ := json.Marshal(userContent)
	messages := []message{
		{Role: "user", Content: userMsg},
	}

	for i := 0; i < 10; i++ { // max 10 tool call rounds
		resp, err := c.call(ctx, system, messages, apiTools)
		if err != nil {
			return "", err
		}

		if resp.StopReason != "tool_use" {
			// Collect text blocks
			var texts []string
			for _, block := range resp.Content {
				if block.Type == "text" {
					texts = append(texts, block.Text)
				}
			}
			return strings.Join(texts, "\n"), nil
		}

		// Marshal the assistant content blocks as the assistant message
		assistantContent, _ := json.Marshal(resp.Content)
		messages = append(messages, message{Role: "assistant", Content: assistantContent})

		// Build tool result message content
		var resultBlocks []map[string]interface{}
		for _, block := range resp.Content {
			if block.Type != "tool_use" {
				continue
			}
			result, err := HandleToolCall(block.Name, block.Input)
			if err != nil {
				result = fmt.Sprintf(`{"error": "%s"}`, err.Error())
			}
			resultBlocks = append(resultBlocks, map[string]interface{}{
				"type":        "tool_result",
				"tool_use_id": block.ID,
				"content":     result,
			})
		}
		toolContent, _ := json.Marshal(resultBlocks)
		messages = append(messages, message{Role: "user", Content: toolContent})
	}

	return "", fmt.Errorf("exceeded maximum tool call rounds")
}

// -- WorkflowGenerator implementation --

// GenerateWorkflow creates a workflow config from a natural language request.
func (c *Client) GenerateWorkflow(ctx context.Context, req ai.GenerateRequest) (*ai.GenerateResponse, error) {
	prompt := ai.GeneratePrompt(req)
	system := ai.SystemPrompt()

	text, err := c.callWithToolLoop(ctx, system, prompt)
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %w", err)
	}

	return parseGenerateResponse(text)
}

// GenerateComponent generates Go source code for a component specification.
func (c *Client) GenerateComponent(ctx context.Context, spec ai.ComponentSpec) (string, error) {
	prompt := ai.ComponentPrompt(spec)
	system := ai.SystemPrompt()

	text, err := c.callWithToolLoop(ctx, system, prompt)
	if err != nil {
		return "", fmt.Errorf("LLM call failed: %w", err)
	}

	// Extract code from markdown fences if present
	return ExtractCode(text), nil
}

// SuggestWorkflow returns workflow suggestions for a use case.
func (c *Client) SuggestWorkflow(ctx context.Context, useCase string) ([]ai.WorkflowSuggestion, error) {
	prompt := ai.SuggestPrompt(useCase)
	system := ai.SystemPrompt()

	text, err := c.callWithToolLoop(ctx, system, prompt)
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %w", err)
	}

	return parseSuggestions(text)
}

// IdentifyMissingComponents analyzes a config for non-built-in module types.
func (c *Client) IdentifyMissingComponents(ctx context.Context, cfg *config.WorkflowConfig) ([]ai.ComponentSpec, error) {
	var types []string
	for _, mod := range cfg.Modules {
		types = append(types, mod.Type)
	}

	prompt := ai.MissingComponentsPrompt(types)
	system := ai.SystemPrompt()

	text, err := c.callWithToolLoop(ctx, system, prompt)
	if err != nil {
		return nil, fmt.Errorf("LLM call failed: %w", err)
	}

	return parseMissingComponents(text)
}

// -- Response parsers --

func parseGenerateResponse(text string) (*ai.GenerateResponse, error) {
	jsonStr := ExtractJSON(text)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON found in response")
	}

	// Try parsing as GenerateResponse with embedded YAML
	var raw struct {
		Workflow    json.RawMessage  `json:"workflow"`
		Components []ai.ComponentSpec `json:"components"`
		Explanation string           `json:"explanation"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return nil, fmt.Errorf("failed to parse response JSON: %w", err)
	}

	resp := &ai.GenerateResponse{
		Components:  raw.Components,
		Explanation: raw.Explanation,
	}

	// Parse workflow - could be a WorkflowConfig object or YAML string
	if len(raw.Workflow) > 0 {
		var cfg config.WorkflowConfig
		// Try direct JSON unmarshal first
		if err := json.Unmarshal(raw.Workflow, &cfg); err != nil {
			// Try as YAML string
			var yamlStr string
			if err2 := json.Unmarshal(raw.Workflow, &yamlStr); err2 == nil {
				if err3 := yaml.Unmarshal([]byte(yamlStr), &cfg); err3 != nil {
					return nil, fmt.Errorf("failed to parse workflow config: %w", err3)
				}
			} else {
				return nil, fmt.Errorf("failed to parse workflow config: %w", err)
			}
		}
		resp.Workflow = &cfg
	}

	return resp, nil
}

func parseSuggestions(text string) ([]ai.WorkflowSuggestion, error) {
	jsonStr := ExtractJSON(text)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON found in response")
	}

	var suggestions []ai.WorkflowSuggestion
	if err := json.Unmarshal([]byte(jsonStr), &suggestions); err != nil {
		return nil, fmt.Errorf("failed to parse suggestions: %w", err)
	}
	return suggestions, nil
}

func parseMissingComponents(text string) ([]ai.ComponentSpec, error) {
	jsonStr := ExtractJSON(text)
	if jsonStr == "" {
		// No JSON means no missing components
		return nil, nil
	}

	var specs []ai.ComponentSpec
	if err := json.Unmarshal([]byte(jsonStr), &specs); err != nil {
		return nil, fmt.Errorf("failed to parse missing components: %w", err)
	}
	return specs, nil
}

// ExtractJSON finds the first JSON object or array in text.
func ExtractJSON(text string) string {
	// Try to find JSON in markdown code blocks first
	if idx := strings.Index(text, "```json"); idx != -1 {
		start := idx + len("```json")
		if end := strings.Index(text[start:], "```"); end != -1 {
			return strings.TrimSpace(text[start : start+end])
		}
	}
	if idx := strings.Index(text, "```"); idx != -1 {
		start := idx + len("```")
		if end := strings.Index(text[start:], "```"); end != -1 {
			candidate := strings.TrimSpace(text[start : start+end])
			if len(candidate) > 0 && (candidate[0] == '{' || candidate[0] == '[') {
				return candidate
			}
		}
	}

	// Find raw JSON
	for i, ch := range text {
		if ch == '{' || ch == '[' {
			closing := byte('}')
			if ch == '[' {
				closing = ']'
			}
			depth := 0
			inString := false
			escape := false
			for j := i; j < len(text); j++ {
				if escape {
					escape = false
					continue
				}
				if text[j] == '\\' && inString {
					escape = true
					continue
				}
				if text[j] == '"' {
					inString = !inString
					continue
				}
				if inString {
					continue
				}
				if text[j] == byte(ch) {
					depth++
				} else if text[j] == closing {
					depth--
					if depth == 0 {
						return text[i : j+1]
					}
				}
			}
		}
	}
	return ""
}

// ExtractCode strips markdown code fences from generated Go code.
func ExtractCode(text string) string {
	if idx := strings.Index(text, "```go"); idx != -1 {
		start := idx + len("```go")
		if end := strings.Index(text[start:], "```"); end != -1 {
			return strings.TrimSpace(text[start : start+end])
		}
	}
	if idx := strings.Index(text, "```"); idx != -1 {
		start := idx + len("```")
		if end := strings.Index(text[start:], "```"); end != -1 {
			return strings.TrimSpace(text[start : start+end])
		}
	}
	return strings.TrimSpace(text)
}
