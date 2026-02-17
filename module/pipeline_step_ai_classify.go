package module

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/ai"
)

// AIClassifyStep takes input text and a list of categories, then uses an AI
// provider to classify the text into one of the categories.
type AIClassifyStep struct {
	name         string
	providerName string
	model        string
	categories   []string
	inputFrom    string
	maxTokens    int
	temperature  float64
	registry     *ai.AIModelRegistry
	tmpl         *TemplateEngine
}

// NewAIClassifyStepFactory returns a StepFactory that creates AIClassifyStep instances.
func NewAIClassifyStepFactory(registry *ai.AIModelRegistry) StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		step := &AIClassifyStep{
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

		// Parse categories
		if cats, ok := config["categories"].([]any); ok {
			for _, c := range cats {
				if s, ok := c.(string); ok {
					step.categories = append(step.categories, s)
				}
			}
		}
		if len(step.categories) == 0 {
			return nil, fmt.Errorf("ai_classify step %q: 'categories' is required and must be non-empty", name)
		}

		switch v := config["max_tokens"].(type) {
		case int:
			step.maxTokens = v
		case float64:
			step.maxTokens = int(v)
		}
		if step.maxTokens == 0 {
			step.maxTokens = 256
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

func (s *AIClassifyStep) Name() string { return s.name }

func (s *AIClassifyStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	if s.registry == nil {
		return nil, fmt.Errorf("ai_classify step %q: no AI model registry configured", s.name)
	}

	inputText, err := s.resolveInput(pc)
	if err != nil {
		return nil, fmt.Errorf("ai_classify step %q: %w", s.name, err)
	}

	provider, err := s.resolveProvider()
	if err != nil {
		return nil, fmt.Errorf("ai_classify step %q: %w", s.name, err)
	}

	categoriesStr := strings.Join(s.categories, ", ")
	systemPrompt := fmt.Sprintf(
		"You are a text classifier. Classify the given text into exactly one of these categories: %s.\n"+
			"Respond with ONLY a JSON object in this format: {\"category\": \"<category>\", \"confidence\": <0.0-1.0>, \"reasoning\": \"<brief explanation>\"}",
		categoriesStr,
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
		return nil, fmt.Errorf("ai_classify step %q: completion failed: %w", s.name, err)
	}

	// Parse the classification result
	result := parseClassification(resp.Content, s.categories)

	output := map[string]any{
		"category":   result.Category,
		"confidence": result.Confidence,
		"reasoning":  result.Reasoning,
		"raw":        resp.Content,
		"model":      resp.Model,
		"usage": map[string]any{
			"input_tokens":  resp.Usage.InputTokens,
			"output_tokens": resp.Usage.OutputTokens,
		},
	}

	return &StepResult{Output: output}, nil
}

type classificationResult struct {
	Category   string  `json:"category"`
	Confidence float64 `json:"confidence"`
	Reasoning  string  `json:"reasoning"`
}

func parseClassification(content string, validCategories []string) classificationResult {
	var result classificationResult

	// Try JSON parsing first
	if err := json.Unmarshal([]byte(content), &result); err == nil && result.Category != "" {
		return result
	}

	// Try extracting JSON from the content
	if idx := strings.Index(content, "{"); idx != -1 {
		if end := strings.LastIndex(content, "}"); end > idx {
			if err := json.Unmarshal([]byte(content[idx:end+1]), &result); err == nil && result.Category != "" {
				return result
			}
		}
	}

	// Fall back: look for a category mentioned in the text
	lower := strings.ToLower(content)
	for _, cat := range validCategories {
		if strings.Contains(lower, strings.ToLower(cat)) {
			return classificationResult{
				Category:   cat,
				Confidence: 0.5,
				Reasoning:  "extracted from response text",
			}
		}
	}

	return classificationResult{
		Category:   content,
		Confidence: 0.0,
		Reasoning:  "could not parse classification",
	}
}

func (s *AIClassifyStep) resolveInput(pc *PipelineContext) (string, error) {
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

func (s *AIClassifyStep) resolveProvider() (ai.AIProvider, error) {
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
