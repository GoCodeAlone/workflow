package module

import (
	"testing"

	"github.com/GoCodeAlone/workflow/ai"
)

func TestAICompleteStep_ValidMinimalConfig(t *testing.T) {
	registry := ai.NewAIModelRegistry()
	step, err := NewAICompleteStepFactory(registry)("complete", map[string]any{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if step.Name() != "complete" {
		t.Errorf("expected name 'complete', got %q", step.Name())
	}
}

func TestAICompleteStep_WithSystemPrompt(t *testing.T) {
	registry := ai.NewAIModelRegistry()
	step, err := NewAICompleteStepFactory(registry)("complete", map[string]any{
		"system_prompt": "You are a helpful assistant.",
		"model":         "claude-3",
		"max_tokens":    512,
		"temperature":   0.7,
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if step.Name() != "complete" {
		t.Errorf("expected name 'complete', got %q", step.Name())
	}
}

func TestAICompleteStep_WithProvider(t *testing.T) {
	registry := ai.NewAIModelRegistry()
	step, err := NewAICompleteStepFactory(registry)("gen", map[string]any{
		"provider":   "anthropic",
		"model":      "claude-3-haiku",
		"input_from": ".body.prompt",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if step.Name() != "gen" {
		t.Errorf("expected name 'gen', got %q", step.Name())
	}
}

func TestAICompleteStep_DefaultMaxTokens(t *testing.T) {
	registry := ai.NewAIModelRegistry()
	step, err := NewAICompleteStepFactory(registry)("complete", map[string]any{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := step.(*AICompleteStep)
	if s.maxTokens != 1024 {
		t.Errorf("expected default maxTokens 1024, got %d", s.maxTokens)
	}
}
