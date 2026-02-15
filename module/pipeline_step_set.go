package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
)

// SetStep sets template-resolved values in the pipeline context.
type SetStep struct {
	name   string
	values map[string]any
	tmpl   *TemplateEngine
}

// NewSetStepFactory returns a StepFactory that creates SetStep instances.
func NewSetStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		values, _ := config["values"].(map[string]any)
		if len(values) == 0 {
			return nil, fmt.Errorf("set step %q: 'values' map is required and must not be empty", name)
		}

		return &SetStep{
			name:   name,
			values: values,
			tmpl:   NewTemplateEngine(),
		}, nil
	}
}

// Name returns the step name.
func (s *SetStep) Name() string { return s.name }

// Execute resolves template expressions in the configured values and returns
// them as the step output.
func (s *SetStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	resolved, err := s.tmpl.ResolveMap(s.values, pc)
	if err != nil {
		return nil, fmt.Errorf("set step %q: failed to resolve values: %w", s.name, err)
	}
	return &StepResult{Output: resolved}, nil
}
