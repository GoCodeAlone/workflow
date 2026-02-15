package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
)

// ConditionalStep routes pipeline execution to different steps based on a
// field value in pc.Current.
type ConditionalStep struct {
	name         string
	field        string
	routes       map[string]string
	defaultRoute string
	tmpl         *TemplateEngine
}

// NewConditionalStepFactory returns a StepFactory that creates ConditionalStep instances.
func NewConditionalStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		field, _ := config["field"].(string)
		if field == "" {
			return nil, fmt.Errorf("conditional step %q: 'field' is required", name)
		}

		routesRaw, _ := config["routes"].(map[string]any)
		if len(routesRaw) == 0 {
			return nil, fmt.Errorf("conditional step %q: 'routes' map is required and must not be empty", name)
		}

		routes := make(map[string]string, len(routesRaw))
		for k, v := range routesRaw {
			s, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("conditional step %q: route value for %q must be a string", name, k)
			}
			routes[k] = s
		}

		defaultRoute, _ := config["default"].(string)

		return &ConditionalStep{
			name:         name,
			field:        field,
			routes:       routes,
			defaultRoute: defaultRoute,
			tmpl:         NewTemplateEngine(),
		}, nil
	}
}

// Name returns the step name.
func (s *ConditionalStep) Name() string { return s.name }

// Execute resolves the field value and determines the next step.
func (s *ConditionalStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	// Use the template engine to resolve the field expression.
	// Wrap the field in {{ }} so the engine evaluates it as a Go template path.
	tmplExpr := "{{." + s.field + "}}"
	resolved, err := s.tmpl.Resolve(tmplExpr, pc)
	if err != nil {
		return nil, fmt.Errorf("conditional step %q: failed to resolve field %q: %w", s.name, s.field, err)
	}

	// Look up the resolved value in the routes map
	if nextStep, ok := s.routes[resolved]; ok {
		return &StepResult{
			Output:   map[string]any{"matched_value": resolved, "next_step": nextStep},
			NextStep: nextStep,
		}, nil
	}

	// Fall back to default
	if s.defaultRoute != "" {
		return &StepResult{
			Output:   map[string]any{"matched_value": resolved, "next_step": s.defaultRoute, "used_default": true},
			NextStep: s.defaultRoute,
		}, nil
	}

	return nil, fmt.Errorf("conditional step %q: value %q not found in routes and no default configured", s.name, resolved)
}
