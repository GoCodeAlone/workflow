package module

import (
	"context"
	"testing"
)

func TestRegexMatchStep_SimpleMatch(t *testing.T) {
	factory := NewRegexMatchStepFactory()
	step, err := factory("regex-test", map[string]any{
		"pattern": `\d+`,
		"input":   "order-123",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["matched"] != true {
		t.Errorf("expected matched=true, got %v", result.Output["matched"])
	}
	if result.Output["match"] != "123" {
		t.Errorf("expected match='123', got %v", result.Output["match"])
	}
}

func TestRegexMatchStep_WithGroups(t *testing.T) {
	factory := NewRegexMatchStepFactory()
	step, err := factory("regex-groups", map[string]any{
		"pattern": `^(\w+)-(\d+)$`,
		"input":   "order-456",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["matched"] != true {
		t.Errorf("expected matched=true, got %v", result.Output["matched"])
	}
	if result.Output["match"] != "order-456" {
		t.Errorf("expected match='order-456', got %v", result.Output["match"])
	}

	groups, ok := result.Output["groups"].([]string)
	if !ok {
		t.Fatalf("expected groups to be []string, got %T", result.Output["groups"])
	}
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
	if groups[0] != "order" {
		t.Errorf("expected group[0]='order', got %q", groups[0])
	}
	if groups[1] != "456" {
		t.Errorf("expected group[1]='456', got %q", groups[1])
	}
}

func TestRegexMatchStep_NoMatch(t *testing.T) {
	factory := NewRegexMatchStepFactory()
	step, err := factory("regex-nomatch", map[string]any{
		"pattern": `^\d+$`,
		"input":   "not-a-number",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["matched"] != false {
		t.Errorf("expected matched=false, got %v", result.Output["matched"])
	}
	if result.Output["match"] != "" {
		t.Errorf("expected empty match, got %v", result.Output["match"])
	}
}

func TestRegexMatchStep_TemplateInput(t *testing.T) {
	factory := NewRegexMatchStepFactory()
	step, err := factory("regex-tmpl", map[string]any{
		"pattern": `^[a-f0-9-]+$`,
		"input":   "{{ .id }}",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{"id": "abc-123-def"}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["matched"] != true {
		t.Errorf("expected matched=true, got %v", result.Output["matched"])
	}
}

func TestRegexMatchStep_MissingPattern(t *testing.T) {
	factory := NewRegexMatchStepFactory()
	_, err := factory("bad", map[string]any{
		"input": "test",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing pattern")
	}
}

func TestRegexMatchStep_InvalidPattern(t *testing.T) {
	factory := NewRegexMatchStepFactory()
	_, err := factory("bad", map[string]any{
		"pattern": "[invalid",
		"input":   "test",
	}, nil)
	if err == nil {
		t.Fatal("expected error for invalid pattern")
	}
}

func TestRegexMatchStep_MissingInput(t *testing.T) {
	factory := NewRegexMatchStepFactory()
	_, err := factory("bad", map[string]any{
		"pattern": `\d+`,
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing input")
	}
}
