package module

import (
	"testing"
)

func TestExprEngine_FieldAccess(t *testing.T) {
	ee := NewExprEngine()
	pc := NewPipelineContext(map[string]any{"name": "Alice"}, nil)

	result, err := ee.Evaluate("name", pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Alice" {
		t.Errorf("expected 'Alice', got %q", result)
	}
}

func TestExprEngine_TriggerAccess(t *testing.T) {
	ee := NewExprEngine()
	pc := NewPipelineContext(map[string]any{"status": "active"}, nil)

	result, err := ee.Evaluate(`trigger["status"]`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "active" {
		t.Errorf("expected 'active', got %q", result)
	}
}

func TestExprEngine_BodyAlias(t *testing.T) {
	ee := NewExprEngine()
	pc := NewPipelineContext(map[string]any{"id": "42"}, nil)

	result, err := ee.Evaluate(`body["id"]`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "42" {
		t.Errorf("expected '42', got %q", result)
	}
}

func TestExprEngine_StepOutputs(t *testing.T) {
	ee := NewExprEngine()
	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("parse-request", map[string]any{"user_id": "u-99"})

	result, err := ee.Evaluate(`steps["parse-request"]["user_id"]`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "u-99" {
		t.Errorf("expected 'u-99', got %q", result)
	}
}

func TestExprEngine_BooleanComparison(t *testing.T) {
	ee := NewExprEngine()
	pc := NewPipelineContext(map[string]any{"status": "active"}, nil)

	result, err := ee.Evaluate(`status == "active"`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "true" {
		t.Errorf("expected 'true', got %q", result)
	}
}

func TestExprEngine_CompoundBoolean(t *testing.T) {
	ee := NewExprEngine()
	pc := NewPipelineContext(map[string]any{"x": "a", "y": 10}, nil)

	result, err := ee.Evaluate(`x == "a" && y > 5`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "true" {
		t.Errorf("expected 'true', got %q", result)
	}
}

func TestExprEngine_Arithmetic(t *testing.T) {
	ee := NewExprEngine()
	pc := NewPipelineContext(map[string]any{"price": 10, "qty": 3}, nil)

	result, err := ee.Evaluate(`price * qty`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "30" {
		t.Errorf("expected '30', got %q", result)
	}
}

func TestExprEngine_StringConcat(t *testing.T) {
	ee := NewExprEngine()
	pc := NewPipelineContext(map[string]any{"first": "Hello", "last": "World"}, nil)

	result, err := ee.Evaluate(`first + " " + last`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Hello World" {
		t.Errorf("expected 'Hello World', got %q", result)
	}
}

func TestExprEngine_NilHandling(t *testing.T) {
	ee := NewExprEngine()
	pc := NewPipelineContext(nil, nil)

	result, err := ee.Evaluate(`nil`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty string for nil, got %q", result)
	}
}

func TestExprEngine_NestedMapAccess(t *testing.T) {
	ee := NewExprEngine()
	pc := NewPipelineContext(map[string]any{
		"user": map[string]any{
			"profile": map[string]any{"age": 30},
		},
	}, nil)

	result, err := ee.Evaluate(`user["profile"]["age"]`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "30" {
		t.Errorf("expected '30', got %q", result)
	}
}

func TestExprEngine_MetaAccess(t *testing.T) {
	ee := NewExprEngine()
	pc := NewPipelineContext(nil, map[string]any{"pipeline": "order-flow"})

	result, err := ee.Evaluate(`meta["pipeline"]`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "order-flow" {
		t.Errorf("expected 'order-flow', got %q", result)
	}
}

// --- Function tests ---

func TestExprEngine_FuncUpper(t *testing.T) {
	ee := NewExprEngine()
	pc := NewPipelineContext(map[string]any{"name": "alice"}, nil)

	result, err := ee.Evaluate(`upper(name)`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ALICE" {
		t.Errorf("expected 'ALICE', got %q", result)
	}
}

func TestExprEngine_FuncLower(t *testing.T) {
	ee := NewExprEngine()
	pc := NewPipelineContext(map[string]any{"name": "BOB"}, nil)

	result, err := ee.Evaluate(`lower(name)`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "bob" {
		t.Errorf("expected 'bob', got %q", result)
	}
}

func TestExprEngine_FuncToString(t *testing.T) {
	ee := NewExprEngine()
	pc := NewPipelineContext(map[string]any{"count": 42}, nil)

	result, err := ee.Evaluate(`toString(count)`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "42" {
		t.Errorf("expected '42', got %q", result)
	}
}

func TestExprEngine_FuncDefault(t *testing.T) {
	ee := NewExprEngine()
	pc := NewPipelineContext(map[string]any{"val": ""}, nil)

	result, err := ee.Evaluate(`default("fallback", val)`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "fallback" {
		t.Errorf("expected 'fallback', got %q", result)
	}
}

// --- Resolve() integration tests ---

func TestResolve_PureExpr(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{"name": "Alice"}, nil)

	result, err := te.Resolve(`${ upper(name) }`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ALICE" {
		t.Errorf("expected 'ALICE', got %q", result)
	}
}

func TestResolve_PureGoTemplate(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{"name": "Alice"}, nil)

	result, err := te.Resolve(`{{ upper .name }}`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ALICE" {
		t.Errorf("expected 'ALICE', got %q", result)
	}
}

func TestResolve_Mixed(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{"first": "Alice", "last": "Smith"}, nil)

	result, err := te.Resolve(`${ first } {{ .last }}`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Alice Smith" {
		t.Errorf("expected 'Alice Smith', got %q", result)
	}
}

func TestResolve_ExprInMapValue(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{"env": "prod"}, nil)

	data := map[string]any{
		"stage":  `${ upper(env) }`,
		"static": "hello",
	}
	result, err := te.ResolveMap(data, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["stage"] != "PROD" {
		t.Errorf("expected 'PROD', got %q", result["stage"])
	}
	if result["static"] != "hello" {
		t.Errorf("expected 'hello', got %q", result["static"])
	}
}
