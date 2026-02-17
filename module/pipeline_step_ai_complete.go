package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/ai"
)

// AICompleteStep invokes an AI provider to produce a text completion.
type AICompleteStep struct {
	name         string
	providerName string
	model        string
	systemPrompt string
	inputFrom    string
	maxTokens    int
	temperature  float64
	registry     *ai.AIModelRegistry
	tmpl         *TemplateEngine
}

// NewAICompleteStepFactory returns a StepFactory that creates AICompleteStep instances.
func NewAICompleteStepFactory(registry *ai.AIModelRegistry) StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		step := &AICompleteStep{
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
		if v, ok := config["system_prompt"].(string); ok {
			step.systemPrompt = v
		}
		if v, ok := config["input_from"].(string); ok {
			step.inputFrom = v
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

func (s *AICompleteStep) Name() string { return s.name }

func (s *AICompleteStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	if s.registry == nil {
		return nil, fmt.Errorf("ai_complete step %q: no AI model registry configured", s.name)
	}

	// Resolve input text
	inputText, err := s.resolveInput(pc)
	if err != nil {
		return nil, fmt.Errorf("ai_complete step %q: %w", s.name, err)
	}

	// Find the provider
	provider, err := s.resolveProvider()
	if err != nil {
		return nil, fmt.Errorf("ai_complete step %q: %w", s.name, err)
	}

	// Resolve system prompt template
	systemPrompt := s.systemPrompt
	if systemPrompt != "" {
		resolved, tmplErr := s.tmpl.Resolve(systemPrompt, pc)
		if tmplErr == nil {
			systemPrompt = resolved
		}
	}

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
		return nil, fmt.Errorf("ai_complete step %q: completion failed: %w", s.name, err)
	}

	output := map[string]any{
		"content":       resp.Content,
		"model":         resp.Model,
		"finish_reason": resp.FinishReason,
		"usage": map[string]any{
			"input_tokens":  resp.Usage.InputTokens,
			"output_tokens": resp.Usage.OutputTokens,
		},
	}

	return &StepResult{Output: output}, nil
}

func (s *AICompleteStep) resolveInput(pc *PipelineContext) (string, error) {
	if s.inputFrom != "" {
		resolved, err := s.tmpl.Resolve("{{"+s.inputFrom+"}}", pc)
		if err != nil {
			return "", fmt.Errorf("failed to resolve input_from %q: %w", s.inputFrom, err)
		}
		if resolved != "" {
			return resolved, nil
		}
	}

	// Fall back to current context as JSON
	if text, ok := pc.Current["text"].(string); ok {
		return text, nil
	}
	if body, ok := pc.Current["body"].(string); ok {
		return body, nil
	}

	return fmt.Sprintf("%v", pc.Current), nil
}

func (s *AICompleteStep) resolveProvider() (ai.AIProvider, error) {
	if s.providerName != "" {
		p, ok := s.registry.GetProvider(s.providerName)
		if !ok {
			return nil, fmt.Errorf("provider %q not found in registry", s.providerName)
		}
		return p, nil
	}

	// If model is specified, find the provider that owns it
	if s.model != "" {
		p, ok := s.registry.ProviderForModel(s.model)
		if ok {
			return p, nil
		}
	}

	// Fall back to first registered provider
	providers := s.registry.ListProviders()
	if len(providers) == 0 {
		return nil, fmt.Errorf("no AI providers registered")
	}
	p, _ := s.registry.GetProvider(providers[0])
	return p, nil
}
