package module

import (
	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/pipeline"
)

// SetStep sets template-resolved values in the pipeline context.
// Aliased from pipeline.SetStep for backwards compatibility.
type SetStep = pipeline.SetStep

// NewSetStepFactory returns a StepFactory that creates SetStep instances.
// Delegates to pipeline.NewSetStepFactory for backwards compatibility.
func NewSetStepFactory() StepFactory {
	pf := pipeline.NewSetStepFactory()
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		return pf(name, config, app)
	}
}
