package module

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/platform"
)

func TestConstraintCheckStep_AllPass(t *testing.T) {
	declarations := []platform.CapabilityDeclaration{
		{
			Name: "web-app",
			Type: "container_runtime",
			Properties: map[string]any{
				"replicas": 3,
				"memory":   "256Mi",
			},
		},
	}

	pc := NewPipelineContext(map[string]any{
		"resources": declarations,
	}, nil)

	factory := NewConstraintCheckStepFactory()
	step, err := factory("constraint-check", map[string]any{
		"constraints": []any{
			map[string]any{
				"field":    "replicas",
				"operator": "<=",
				"value":    10,
				"source":   "tier1-limit",
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("failed to create step: %v", err)
	}

	if step.Name() != "constraint-check" {
		t.Errorf("expected name %q, got %q", "constraint-check", step.Name())
	}

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	violations, ok := result.Output["constraint_violations"].([]map[string]any)
	if ok && len(violations) > 0 {
		t.Errorf("expected no violations, got %d", len(violations))
	}

	summary := result.Output["constraint_summary"].(map[string]any)
	if summary["passed"] != true {
		t.Error("expected passed to be true")
	}

	if result.Stop {
		t.Error("expected Stop to be false when all constraints pass")
	}
}

func TestConstraintCheckStep_ViolationDetected(t *testing.T) {
	declarations := []platform.CapabilityDeclaration{
		{
			Name: "web-app",
			Type: "container_runtime",
			Properties: map[string]any{
				"replicas": 20,
			},
		},
	}

	pc := NewPipelineContext(map[string]any{
		"resources": declarations,
	}, nil)

	factory := NewConstraintCheckStepFactory()
	step, err := factory("constraint-check", map[string]any{
		"constraints": []any{
			map[string]any{
				"field":    "replicas",
				"operator": "<=",
				"value":    10,
				"source":   "tier1-limit",
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("failed to create step: %v", err)
	}

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	violations := result.Output["constraint_violations"].([]map[string]any)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}

	if violations[0]["resource"] != "web-app" {
		t.Errorf("expected resource %q, got %v", "web-app", violations[0]["resource"])
	}
	if violations[0]["field"] != "replicas" {
		t.Errorf("expected field %q, got %v", "replicas", violations[0]["field"])
	}

	summary := result.Output["constraint_summary"].(map[string]any)
	if summary["passed"] != false {
		t.Error("expected passed to be false")
	}

	if !result.Stop {
		t.Error("expected Stop to be true when violations found")
	}
}

func TestConstraintCheckStep_PerDeclarationConstraints(t *testing.T) {
	declarations := []platform.CapabilityDeclaration{
		{
			Name: "web-app",
			Type: "container_runtime",
			Properties: map[string]any{
				"replicas": 5,
			},
			Constraints: []platform.Constraint{
				{Field: "replicas", Operator: "<=", Value: 3, Source: "parent-tier"},
			},
		},
	}

	pc := NewPipelineContext(map[string]any{
		"resources": declarations,
	}, nil)

	factory := NewConstraintCheckStepFactory()
	step, err := factory("constraint-check", map[string]any{}, nil)
	if err != nil {
		t.Fatalf("failed to create step: %v", err)
	}

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	violations := result.Output["constraint_violations"].([]map[string]any)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation from per-declaration constraint, got %d", len(violations))
	}
}

func TestConstraintCheckStep_MultipleDeclarations(t *testing.T) {
	declarations := []platform.CapabilityDeclaration{
		{
			Name:       "good-svc",
			Properties: map[string]any{"replicas": 2},
		},
		{
			Name:       "bad-svc",
			Properties: map[string]any{"replicas": 15},
		},
	}

	pc := NewPipelineContext(map[string]any{
		"resources": declarations,
	}, nil)

	factory := NewConstraintCheckStepFactory()
	step, err := factory("constraint-check", map[string]any{
		"constraints": []any{
			map[string]any{
				"field":    "replicas",
				"operator": "<=",
				"value":    10,
				"source":   "limit",
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("failed to create step: %v", err)
	}

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	violations := result.Output["constraint_violations"].([]map[string]any)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0]["resource"] != "bad-svc" {
		t.Errorf("expected violation on %q, got %v", "bad-svc", violations[0]["resource"])
	}
}

func TestConstraintCheckStep_MissingResources(t *testing.T) {
	pc := NewPipelineContext(map[string]any{}, nil)

	factory := NewConstraintCheckStepFactory()
	step, err := factory("constraint-check", map[string]any{}, nil)
	if err != nil {
		t.Fatalf("failed to create step: %v", err)
	}

	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error for missing resources")
	}
}

func TestConstraintCheckStepFactory_InvalidConstraint(t *testing.T) {
	factory := NewConstraintCheckStepFactory()

	// Missing field
	_, err := factory("check", map[string]any{
		"constraints": []any{
			map[string]any{
				"operator": "<=",
				"value":    10,
			},
		},
	}, nil)
	if err == nil {
		t.Fatal("expected error for constraint missing 'field'")
	}

	// Non-map constraint
	_, err = factory("check", map[string]any{
		"constraints": []any{"not-a-map"},
	}, nil)
	if err == nil {
		t.Fatal("expected error for non-map constraint")
	}
}

func TestConstraintCheckStep_NoConstraints(t *testing.T) {
	declarations := []platform.CapabilityDeclaration{
		{
			Name:       "svc",
			Properties: map[string]any{"replicas": 100},
		},
	}

	pc := NewPipelineContext(map[string]any{
		"resources": declarations,
	}, nil)

	factory := NewConstraintCheckStepFactory()
	step, err := factory("constraint-check", map[string]any{}, nil)
	if err != nil {
		t.Fatalf("failed to create step: %v", err)
	}

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	summary := result.Output["constraint_summary"].(map[string]any)
	if summary["passed"] != true {
		t.Error("expected passed to be true with no constraints")
	}
}
