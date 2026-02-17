package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/platform"
)

// ConstraintCheckStep implements a pipeline step that validates capability
// declarations against a set of constraints. It uses the platform package's
// ConstraintValidator to check each declaration and outputs any violations.
type ConstraintCheckStep struct {
	name          string
	resourcesFrom string
	constraints   []platform.Constraint
}

// NewConstraintCheckStepFactory returns a StepFactory that creates ConstraintCheckStep instances.
func NewConstraintCheckStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		resourcesFrom, _ := config["resources_from"].(string)
		if resourcesFrom == "" {
			resourcesFrom = "resources"
		}

		var constraints []platform.Constraint
		if rawConstraints, ok := config["constraints"].([]any); ok {
			for i, item := range rawConstraints {
				cMap, ok := item.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("constraint_check step %q: constraints[%d] must be a map", name, i)
				}
				c := platform.Constraint{
					Field:    stringFromMap(cMap, "field"),
					Operator: stringFromMap(cMap, "operator"),
					Value:    cMap["value"],
					Source:   stringFromMap(cMap, "source"),
				}
				if c.Field == "" || c.Operator == "" {
					return nil, fmt.Errorf("constraint_check step %q: constraints[%d] requires 'field' and 'operator'", name, i)
				}
				constraints = append(constraints, c)
			}
		}

		return &ConstraintCheckStep{
			name:          name,
			resourcesFrom: resourcesFrom,
			constraints:   constraints,
		}, nil
	}
}

// stringFromMap extracts a string value from a map by key, returning empty string if missing.
func stringFromMap(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

// Name returns the step name.
func (s *ConstraintCheckStep) Name() string { return s.name }

// Execute validates each capability declaration against the configured constraints.
func (s *ConstraintCheckStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	declarations, err := s.resolveDeclarations(pc)
	if err != nil {
		return nil, fmt.Errorf("constraint_check step %q: %w", s.name, err)
	}

	validator := platform.NewConstraintValidator()
	var allViolations []map[string]any

	// Merge step-level constraints with per-declaration constraints
	for _, decl := range declarations {
		constraints := make([]platform.Constraint, 0, len(s.constraints)+len(decl.Constraints))
		constraints = append(constraints, s.constraints...)
		constraints = append(constraints, decl.Constraints...)
		violations := validator.Validate(decl.Properties, constraints)
		for _, v := range violations {
			allViolations = append(allViolations, map[string]any{
				"resource": decl.Name,
				"field":    v.Constraint.Field,
				"operator": v.Constraint.Operator,
				"limit":    v.Constraint.Value,
				"actual":   v.Actual,
				"source":   v.Constraint.Source,
				"message":  v.Message,
			})
		}
	}

	passed := len(allViolations) == 0
	result := &StepResult{
		Output: map[string]any{
			"constraint_violations": allViolations,
			"constraint_summary": map[string]any{
				"passed":          passed,
				"total_checked":   len(declarations),
				"violation_count": len(allViolations),
			},
		},
	}

	if !passed {
		result.Stop = true
	}

	return result, nil
}

// resolveDeclarations reads the list of CapabilityDeclarations from the pipeline context.
func (s *ConstraintCheckStep) resolveDeclarations(pc *PipelineContext) ([]platform.CapabilityDeclaration, error) {
	raw, ok := pc.Current[s.resourcesFrom]
	if !ok {
		return nil, fmt.Errorf("resources key %q not found in pipeline context", s.resourcesFrom)
	}

	switch v := raw.(type) {
	case []platform.CapabilityDeclaration:
		return v, nil
	case []any:
		var decls []platform.CapabilityDeclaration
		for i, item := range v {
			decl, ok := item.(platform.CapabilityDeclaration)
			if !ok {
				return nil, fmt.Errorf("resources[%d] is not a CapabilityDeclaration", i)
			}
			decls = append(decls, decl)
		}
		return decls, nil
	default:
		return nil, fmt.Errorf("resources key %q has unexpected type %T", s.resourcesFrom, raw)
	}
}
