package module

import (
	"context"
	"strings"
	"testing"
)

func TestValidateStep_RequiredFields_Passes(t *testing.T) {
	factory := NewValidateStepFactory()
	step, err := factory("check-fields", map[string]any{
		"strategy":        "required_fields",
		"required_fields": []any{"name", "email"},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	if step.Name() != "check-fields" {
		t.Errorf("expected step name 'check-fields', got %q", step.Name())
	}

	pc := NewPipelineContext(map[string]any{
		"name":  "Alice",
		"email": "alice@example.com",
		"extra": "field",
	}, nil)

	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("expected no error when all required fields present, got: %v", err)
	}
}

func TestValidateStep_RequiredFields_FailsMissing(t *testing.T) {
	factory := NewValidateStepFactory()
	step, err := factory("check-fields", map[string]any{
		"strategy":        "required_fields",
		"required_fields": []any{"name", "email", "phone"},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"name": "Alice",
	}, nil)

	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error for missing required fields")
	}

	if !strings.Contains(err.Error(), "email") {
		t.Errorf("expected error to mention 'email', got: %v", err)
	}
	if !strings.Contains(err.Error(), "phone") {
		t.Errorf("expected error to mention 'phone', got: %v", err)
	}
}

func TestValidateStep_RequiredFields_DefaultStrategy(t *testing.T) {
	factory := NewValidateStepFactory()

	// When strategy is omitted, default to required_fields
	step, err := factory("default-strat", map[string]any{
		"required_fields": []any{"name"},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{"name": "Bob"}, nil)
	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateStep_JSONSchema_ValidatesRequired(t *testing.T) {
	factory := NewValidateStepFactory()
	step, err := factory("schema-check", map[string]any{
		"strategy": "json_schema",
		"schema": map[string]any{
			"required": []any{"name", "age"},
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	// Passes when all required present
	pc := NewPipelineContext(map[string]any{"name": "Alice", "age": 30}, nil)
	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("expected no error with all required fields, got: %v", err)
	}

	// Fails when required missing
	pc2 := NewPipelineContext(map[string]any{"name": "Alice"}, nil)
	_, err = step.Execute(context.Background(), pc2)
	if err == nil {
		t.Fatal("expected error for missing 'age' field")
	}
	if !strings.Contains(err.Error(), "age") {
		t.Errorf("expected error to mention 'age', got: %v", err)
	}
}

func TestValidateStep_JSONSchema_ValidatesTypes(t *testing.T) {
	factory := NewValidateStepFactory()
	step, err := factory("type-check", map[string]any{
		"strategy": "json_schema",
		"schema": map[string]any{
			"properties": map[string]any{
				"name":    map[string]any{"type": "string"},
				"age":     map[string]any{"type": "number"},
				"active":  map[string]any{"type": "boolean"},
				"tags":    map[string]any{"type": "array"},
				"address": map[string]any{"type": "object"},
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	// Valid types
	pc := NewPipelineContext(map[string]any{
		"name":    "Alice",
		"age":     float64(30),
		"active":  true,
		"tags":    []any{"a", "b"},
		"address": map[string]any{"city": "NYC"},
	}, nil)
	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("expected no error with valid types, got: %v", err)
	}

	// Invalid type: name is an int instead of string
	pc2 := NewPipelineContext(map[string]any{
		"name": 42,
	}, nil)
	_, err = step.Execute(context.Background(), pc2)
	if err == nil {
		t.Fatal("expected error for wrong type")
	}
	if !strings.Contains(err.Error(), "expected string") {
		t.Errorf("expected 'expected string' in error, got: %v", err)
	}
}

func TestValidateStep_JSONSchema_RequiredAndTypes(t *testing.T) {
	factory := NewValidateStepFactory()
	step, err := factory("full-schema", map[string]any{
		"strategy": "json_schema",
		"schema": map[string]any{
			"required": []any{"name", "count"},
			"properties": map[string]any{
				"name":  map[string]any{"type": "string"},
				"count": map[string]any{"type": "integer"},
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	// Valid
	pc := NewPipelineContext(map[string]any{
		"name":  "test",
		"count": 5,
	}, nil)
	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateStep_JSONSchema_SkipsAbsentOptionalFields(t *testing.T) {
	factory := NewValidateStepFactory()
	step, err := factory("optional-check", map[string]any{
		"strategy": "json_schema",
		"schema": map[string]any{
			"properties": map[string]any{
				"optional_field": map[string]any{"type": "string"},
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	// Field is absent but not required -> should pass
	pc := NewPipelineContext(map[string]any{}, nil)
	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("expected no error for absent optional field, got: %v", err)
	}
}

func TestValidateStep_FactoryRejectsEmptyRequiredFields(t *testing.T) {
	factory := NewValidateStepFactory()

	_, err := factory("bad-validate", map[string]any{
		"strategy":        "required_fields",
		"required_fields": []any{},
	}, nil)
	if err == nil {
		t.Fatal("expected error for empty required_fields list")
	}
}

func TestValidateStep_FactoryRejectsMissingSchemaForJSONSchema(t *testing.T) {
	factory := NewValidateStepFactory()

	_, err := factory("bad-schema", map[string]any{
		"strategy": "json_schema",
	}, nil)
	if err == nil {
		t.Fatal("expected error when json_schema strategy has no schema")
	}
}

func TestValidateStep_FactoryRejectsUnknownStrategy(t *testing.T) {
	factory := NewValidateStepFactory()

	_, err := factory("bad-strat", map[string]any{
		"strategy": "unknown_strategy",
	}, nil)
	if err == nil {
		t.Fatal("expected error for unknown strategy")
	}
	if !strings.Contains(err.Error(), "unknown strategy") {
		t.Errorf("expected 'unknown strategy' in error, got: %v", err)
	}
}
