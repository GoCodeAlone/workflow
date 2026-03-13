package module

import (
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/ai"
)

func TestAIExtractStep_MissingSchema(t *testing.T) {
	registry := ai.NewAIModelRegistry()
	_, err := NewAIExtractStepFactory(registry)("extract", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error for missing schema")
	}
	if !strings.Contains(err.Error(), "'schema' is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAIExtractStep_ValidConfig(t *testing.T) {
	registry := ai.NewAIModelRegistry()
	step, err := NewAIExtractStepFactory(registry)("extract", map[string]any{
		"schema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":  map[string]any{"type": "string"},
				"email": map[string]any{"type": "string"},
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if step.Name() != "extract" {
		t.Errorf("expected name 'extract', got %q", step.Name())
	}
}

func TestAIExtractStep_WithAllOptions(t *testing.T) {
	registry := ai.NewAIModelRegistry()
	step, err := NewAIExtractStepFactory(registry)("extract", map[string]any{
		"schema": map[string]any{
			"type": "object",
		},
		"provider":   "anthropic",
		"model":      "claude-3-haiku",
		"input_from": ".body.text",
		"max_tokens": 256,
		"temperature": 0.0,
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if step.Name() != "extract" {
		t.Errorf("expected name 'extract', got %q", step.Name())
	}
}

func TestAIExtractStep_DefaultMaxTokens(t *testing.T) {
	registry := ai.NewAIModelRegistry()
	step, err := NewAIExtractStepFactory(registry)("extract", map[string]any{
		"schema": map[string]any{"type": "object"},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := step.(*AIExtractStep)
	if s.maxTokens != 1024 {
		t.Errorf("expected default maxTokens 1024, got %d", s.maxTokens)
	}
}
