package module

import (
	"context"
	"fmt"
	"maps"

	"github.com/GoCodeAlone/modular"
)

// PipelineOutputStep marks structured data as the pipeline's return value.
// The data is stored in pc.Metadata["_pipeline_output"] for extraction by
// engine.ExecutePipeline() or the HTTP trigger fallback handler.
type PipelineOutputStep struct {
	name   string
	source string            // dot-path to step output (e.g. "steps.fetch")
	values map[string]string // template map (e.g. {"gameId": "{{ .gameId }}"})
	tmpl   *TemplateEngine
}

// NewPipelineOutputStepFactory returns a StepFactory for step.pipeline_output.
func NewPipelineOutputStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		source, _ := config["source"].(string)
		var values map[string]string
		if v, ok := config["values"].(map[string]any); ok {
			values = make(map[string]string, len(v))
			for k, val := range v {
				if s, ok := val.(string); ok {
					values[k] = s
				}
			}
		}

		if source == "" && len(values) == 0 {
			return nil, fmt.Errorf("pipeline_output step %q: 'source' or 'values' is required", name)
		}
		if source != "" && len(values) > 0 {
			return nil, fmt.Errorf("pipeline_output step %q: 'source' and 'values' are mutually exclusive", name)
		}

		return &PipelineOutputStep{
			name:   name,
			source: source,
			values: values,
			tmpl:   NewTemplateEngine(),
		}, nil
	}
}

func (s *PipelineOutputStep) Name() string { return s.name }

func (s *PipelineOutputStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	var output map[string]any

	if s.source != "" {
		// Resolve from step outputs using the existing resolveBodyFrom helper
		resolved := resolveBodyFrom(s.source, pc)
		if m, ok := resolved.(map[string]any); ok {
			output = m
		} else {
			// Source didn't resolve to a map — return empty
			output = make(map[string]any)
		}
	} else {
		// Resolve template values
		output = make(map[string]any, len(s.values))
		for k, tmplExpr := range s.values {
			resolved, err := s.tmpl.Resolve(tmplExpr, pc)
			if err != nil {
				return nil, fmt.Errorf("pipeline_output step %q: failed to resolve value %q: %w", s.name, k, err)
			}
			output[k] = resolved
		}
	}

	// Store in metadata for extraction by ExecutePipeline()
	pc.Metadata["_pipeline_output"] = output

	// Also include _pipeline_output in Output so it propagates into Current
	// via MergeStepOutput. This makes it visible to the HTTP trigger's
	// resultHolder (which reads from Pipeline.Run() → pc.Current).
	stepOutput := make(map[string]any, len(output)+1)
	maps.Copy(stepOutput, output)
	stepOutput["_pipeline_output"] = output

	return &StepResult{Output: stepOutput, Stop: true}, nil
}

var _ PipelineStep = (*PipelineOutputStep)(nil)
