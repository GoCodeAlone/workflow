package module

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/ai"
)

// AIExtractStep takes input text and an extraction schema, then uses an AI
// provider with tool use to extract structured data from the text.
type AIExtractStep struct {
	name         string
	providerName string
	model        string
	schema       map[string]any
	inputFrom    string
	maxTokens    int
	temperature  float64
	registry     *ai.AIModelRegistry
	tmpl         *TemplateEngine
}

// NewAIExtractStepFactory returns a StepFactory that creates AIExtractStep instances.
func NewAIExtractStepFactory(registry *ai.AIModelRegistry) StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		step := &AIExtractStep{
			name:     name,
			registry: registry,
			tmpl:     NewTemplateEngine(),
		}

		if v, ok := config["provider"].(string); ok {
			step.providerName = v
		}
		if v, ok := config["model"].(string); ok {
			step.model = v
		}
		if v, ok := config["input_from"].(string); ok {
			step.inputFrom = v
		}

		// Parse extraction schema
		if schema, ok := config["schema"].(map[string]any); ok {
			step.schema = schema
		}
		if step.schema == nil {
			return nil, fmt.Errorf("ai_extract step %q: 'schema' is required", name)
		}

		switch v := config["max_tokens"].(type) {
		case int:
			step.maxTokens = v
		case float64:
			step.maxTokens = int(v)
		}
		if step.maxTokens == 0 {
			step.maxTokens = 1024
		}

		switch v := config["temperature"].(type) {
		case float64:
			step.temperature = v
		case int:
			step.temperature = float64(v)
		}

		return step, nil
	}
}

func (s *AIExtractStep) Name() string { return s.name }

func (s *AIExtractStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	if s.registry == nil {
		return nil, fmt.Errorf("ai_extract step %q: no AI model registry configured", s.name)
	}

	inputText, err := s.resolveInput(pc)
	if err != nil {
		return nil, fmt.Errorf("ai_extract step %q: %w", s.name, err)
	}

	provider, err := s.resolveProvider()
	if err != nil {
		return nil, fmt.Errorf("ai_extract step %q: %w", s.name, err)
	}

	// If the provider supports tool use, use tool calling for structured extraction
	if provider.SupportsToolUse() {
		return s.executeWithTools(ctx, provider, inputText)
	}

	// Fall back to prompt-based extraction
	return s.executeWithPrompt(ctx, provider, inputText)
}

func (s *AIExtractStep) executeWithTools(ctx context.Context, provider ai.AIProvider, inputText string) (*StepResult, error) {
	schemaJSON, err := json.Marshal(s.schema)
	if err != nil {
		return nil, fmt.Errorf("ai_extract step %q: marshal schema: %w", s.name, err)
	}

	tool := ai.ToolDefinition{
		Name:        "extract_data",
		Description: "Extract structured data from the provided text according to the schema.",
		InputSchema: s.schema,
	}

	systemPrompt := "You are a data extraction assistant. Extract the requested information from the text and call the extract_data tool with the results. " +
		"The extraction schema is: " + string(schemaJSON)

	req := ai.ToolCompletionRequest{
		CompletionRequest: ai.CompletionRequest{
			Model:        s.model,
			MaxTokens:    s.maxTokens,
			Temperature:  s.temperature,
			SystemPrompt: systemPrompt,
			Messages: []ai.Message{
				{Role: "user", Content: inputText},
			},
		},
		Tools: []ai.ToolDefinition{tool},
	}

	resp, err := provider.ToolComplete(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("ai_extract step %q: tool completion failed: %w", s.name, err)
	}

	output := map[string]any{
		"model": resp.Model,
		"usage": map[string]any{
			"input_tokens":  resp.Usage.InputTokens,
			"output_tokens": resp.Usage.OutputTokens,
		},
	}

	// Extract data from tool calls
	switch {
	case len(resp.ToolCalls) > 0:
		output["extracted"] = resp.ToolCalls[0].Input
		output["method"] = "tool_use"
	case resp.Content != "":
		// Model responded with text instead of tool call; try parsing as JSON
		extracted := parseExtraction(resp.Content)
		output["extracted"] = extracted
		output["method"] = "text_parse"
	default:
		output["extracted"] = map[string]any{}
		output["method"] = "empty"
	}

	return &StepResult{Output: output}, nil
}

func (s *AIExtractStep) executeWithPrompt(ctx context.Context, provider ai.AIProvider, inputText string) (*StepResult, error) {
	schemaJSON, err := json.Marshal(s.schema)
	if err != nil {
		return nil, fmt.Errorf("ai_extract step %q: marshal schema: %w", s.name, err)
	}

	systemPrompt := fmt.Sprintf(
		"You are a data extraction assistant. Extract the requested information from the text.\n"+
			"Respond with ONLY a JSON object matching this schema:\n%s",
		string(schemaJSON),
	)

	req := ai.CompletionRequest{
		Model:        s.model,
		MaxTokens:    s.maxTokens,
		Temperature:  s.temperature,
		SystemPrompt: systemPrompt,
		Messages: []ai.Message{
			{Role: "user", Content: inputText},
		},
	}

	resp, err := provider.Complete(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("ai_extract step %q: completion failed: %w", s.name, err)
	}

	extracted := parseExtraction(resp.Content)

	output := map[string]any{
		"extracted": extracted,
		"method":    "prompt",
		"raw":       resp.Content,
		"model":     resp.Model,
		"usage": map[string]any{
			"input_tokens":  resp.Usage.InputTokens,
			"output_tokens": resp.Usage.OutputTokens,
		},
	}

	return &StepResult{Output: output}, nil
}

// parseExtraction tries to parse JSON from the model's text response.
func parseExtraction(content string) map[string]any {
	var result map[string]any

	// Try direct parse
	if err := json.Unmarshal([]byte(content), &result); err == nil {
		return result
	}

	// Try extracting JSON from the content
	if idx := strings.Index(content, "{"); idx != -1 {
		if end := strings.LastIndex(content, "}"); end > idx {
			if err := json.Unmarshal([]byte(content[idx:end+1]), &result); err == nil {
				return result
			}
		}
	}

	// Try extracting from markdown code block
	if idx := strings.Index(content, "```json"); idx != -1 {
		start := idx + len("```json")
		if end := strings.Index(content[start:], "```"); end != -1 {
			if err := json.Unmarshal([]byte(strings.TrimSpace(content[start:start+end])), &result); err == nil {
				return result
			}
		}
	}

	return map[string]any{"raw": content}
}

func (s *AIExtractStep) resolveInput(pc *PipelineContext) (string, error) {
	if s.inputFrom != "" {
		resolved, err := s.tmpl.Resolve("{{"+s.inputFrom+"}}", pc)
		if err != nil {
			return "", fmt.Errorf("failed to resolve input_from %q: %w", s.inputFrom, err)
		}
		if resolved != "" {
			return resolved, nil
		}
	}

	if text, ok := pc.Current["text"].(string); ok {
		return text, nil
	}
	if body, ok := pc.Current["body"].(string); ok {
		return body, nil
	}

	return fmt.Sprintf("%v", pc.Current), nil
}

func (s *AIExtractStep) resolveProvider() (ai.AIProvider, error) {
	if s.providerName != "" {
		p, ok := s.registry.GetProvider(s.providerName)
		if !ok {
			return nil, fmt.Errorf("provider %q not found in registry", s.providerName)
		}
		return p, nil
	}

	if s.model != "" {
		p, ok := s.registry.ProviderForModel(s.model)
		if ok {
			return p, nil
		}
	}

	providers := s.registry.ListProviders()
	if len(providers) == 0 {
		return nil, fmt.Errorf("no AI providers registered")
	}
	p, _ := s.registry.GetProvider(providers[0])
	return p, nil
}
