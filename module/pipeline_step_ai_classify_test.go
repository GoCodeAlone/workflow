package module

import (
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/ai"
)

func TestAIClassifyStep_MissingCategories(t *testing.T) {
	registry := ai.NewAIModelRegistry()
	_, err := NewAIClassifyStepFactory(registry)("classify", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error for missing categories")
	}
	if !strings.Contains(err.Error(), "'categories' is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAIClassifyStep_EmptyCategories(t *testing.T) {
	registry := ai.NewAIModelRegistry()
	_, err := NewAIClassifyStepFactory(registry)("classify", map[string]any{
		"categories": []any{},
	}, nil)
	if err == nil {
		t.Fatal("expected error for empty categories")
	}
	if !strings.Contains(err.Error(), "'categories' is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestAIClassifyStep_ValidConfig(t *testing.T) {
	registry := ai.NewAIModelRegistry()
	step, err := NewAIClassifyStepFactory(registry)("classify", map[string]any{
		"categories": []any{"positive", "negative", "neutral"},
		"model":      "claude-3",
		"max_tokens": 128,
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if step.Name() != "classify" {
		t.Errorf("expected name 'classify', got %q", step.Name())
	}
}

func TestAIClassifyStep_NonStringCategoryIgnored(t *testing.T) {
	registry := ai.NewAIModelRegistry()
	// Non-string items are silently skipped; if all skipped, error
	_, err := NewAIClassifyStepFactory(registry)("classify", map[string]any{
		"categories": []any{42, true},
	}, nil)
	if err == nil {
		t.Fatal("expected error when all category items are non-string")
	}
}

func TestAIClassifyStep_WithValidStringCategories(t *testing.T) {
	registry := ai.NewAIModelRegistry()
	step, err := NewAIClassifyStepFactory(registry)("classify", map[string]any{
		"categories": []any{"spam", "not_spam"},
		"provider":   "anthropic",
		"input_from": ".body.text",
		"temperature": 0.2,
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if step.Name() != "classify" {
		t.Errorf("expected name 'classify', got %q", step.Name())
	}
}
