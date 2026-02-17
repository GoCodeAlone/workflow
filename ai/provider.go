package ai

import "context"

// AIProvider defines a pluggable AI model provider that can be registered
// with the AIModelRegistry. Providers supply completion and streaming APIs
// with optional tool-use support.
type AIProvider interface {
	// Name returns the provider's unique identifier (e.g., "anthropic", "openai").
	Name() string

	// Models returns metadata about all models available from this provider.
	Models() []ModelInfo

	// Complete sends a completion request and returns the full response.
	Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error)

	// CompleteStream sends a completion request and returns a channel of streaming chunks.
	CompleteStream(ctx context.Context, req CompletionRequest) (<-chan StreamChunk, error)

	// SupportsToolUse returns whether this provider supports function/tool calling.
	SupportsToolUse() bool

	// ToolComplete sends a completion request with tool definitions and returns the response.
	ToolComplete(ctx context.Context, req ToolCompletionRequest) (*ToolCompletionResponse, error)
}

// ModelInfo describes a model's capabilities and pricing.
type ModelInfo struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Provider        string   `json:"provider"`
	ContextWindow   int      `json:"contextWindow"`
	MaxOutput       int      `json:"maxOutput"`
	SupportsTools   bool     `json:"supportsTools"`
	SupportsVision  bool     `json:"supportsVision"`
	CostPer1KInput  float64  `json:"costPer1KInput"`
	CostPer1KOutput float64  `json:"costPer1KOutput"`
	Overridable     []string `json:"overridable"`
}

// CompletionRequest is the input for a non-streaming completion call.
type CompletionRequest struct {
	Model        string         `json:"model"`
	Messages     []Message      `json:"messages"`
	MaxTokens    int            `json:"maxTokens"`
	Temperature  float64        `json:"temperature"`
	SystemPrompt string         `json:"systemPrompt,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

// Message is a single message in a conversation.
type Message struct {
	Role    string `json:"role"` // "user", "assistant", "system"
	Content string `json:"content"`
}

// CompletionResponse is the output of a completion call.
type CompletionResponse struct {
	ID           string     `json:"id"`
	Model        string     `json:"model"`
	Content      string     `json:"content"`
	Usage        TokenUsage `json:"usage"`
	FinishReason string     `json:"finishReason"`
}

// TokenUsage tracks input and output token counts.
type TokenUsage struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
}

// StreamChunk is one piece of a streaming completion response.
type StreamChunk struct {
	Content string `json:"content,omitempty"`
	Done    bool   `json:"done"`
	Error   error  `json:"-"`
}

// ToolDefinition describes a tool that the model can invoke.
type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// ToolCompletionRequest extends CompletionRequest with tool definitions.
type ToolCompletionRequest struct {
	CompletionRequest
	Tools []ToolDefinition `json:"tools"`
}

// ToolCall represents a single tool invocation requested by the model.
type ToolCall struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

// ToolCompletionResponse extends CompletionResponse with tool calls.
type ToolCompletionResponse struct {
	CompletionResponse
	ToolCalls []ToolCall `json:"toolCalls,omitempty"`
}
