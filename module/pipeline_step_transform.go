package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
)

// TransformStep applies a DataTransformer to the pipeline context's current data.
type TransformStep struct {
	name        string
	transformer string // service name to look up
	pipeline    string // named pipeline within the transformer
	operations  []TransformOperation
	app         modular.Application
}

// NewTransformStepFactory returns a StepFactory that creates TransformStep instances.
func NewTransformStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		step := &TransformStep{
			name: name,
			app:  app,
		}

		step.transformer, _ = config["transformer"].(string)
		step.pipeline, _ = config["pipeline"].(string)

		// Parse inline operations
		if opsRaw, ok := config["operations"].([]any); ok {
			for i, opRaw := range opsRaw {
				opMap, ok := opRaw.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("transform step %q: operation %d must be a map", name, i)
				}
				opType, _ := opMap["type"].(string)
				if opType == "" {
					return nil, fmt.Errorf("transform step %q: operation %d missing 'type'", name, i)
				}
				opConfig, _ := opMap["config"].(map[string]any)
				step.operations = append(step.operations, TransformOperation{
					Type:   opType,
					Config: opConfig,
				})
			}
		}

		// Validate that at least one mode is configured
		if step.transformer == "" && len(step.operations) == 0 {
			return nil, fmt.Errorf("transform step %q: must specify 'transformer' service or inline 'operations'", name)
		}

		return step, nil
	}
}

// Name returns the step name.
func (s *TransformStep) Name() string { return s.name }

// Execute runs the transformation and returns the result under the "data" key.
func (s *TransformStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	var result any
	var err error

	switch {
	case s.transformer != "":
		// Look up the DataTransformer service
		var dt *DataTransformer
		if svcErr := s.app.GetService(s.transformer, &dt); svcErr != nil {
			return nil, fmt.Errorf("transform step %q: service %q not found: %w", s.name, s.transformer, svcErr)
		}

		switch {
		case s.pipeline != "":
			result, err = dt.Transform(ctx, s.pipeline, pc.Current)
		case len(s.operations) > 0:
			result, err = dt.TransformWithOps(ctx, s.operations, pc.Current)
		default:
			return nil, fmt.Errorf("transform step %q: transformer service specified but no pipeline or operations", s.name)
		}
	default:
		// Use a temporary DataTransformer for inline operations
		dt := NewDataTransformer(s.name + ".inline")
		result, err = dt.TransformWithOps(ctx, s.operations, pc.Current)
	}

	if err != nil {
		return nil, fmt.Errorf("transform step %q: %w", s.name, err)
	}

	// Wrap the result in a map under "data"
	output := map[string]any{"data": result}
	return &StepResult{Output: output}, nil
}
