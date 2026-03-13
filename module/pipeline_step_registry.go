package module

import (
	"fmt"

	"github.com/GoCodeAlone/modular"
)

// StepFactory creates a PipelineStep from its name and config.
type StepFactory func(name string, config map[string]any, app modular.Application) (PipelineStep, error)

// StepRegistry maps step type strings to factory functions.
type StepRegistry struct {
	factories map[string]StepFactory
}

// NewStepRegistry creates an empty StepRegistry.
func NewStepRegistry() *StepRegistry {
	return &StepRegistry{
		factories: make(map[string]StepFactory),
	}
}

// Register adds a step factory for the given type string.
func (r *StepRegistry) Register(stepType string, factory StepFactory) {
	r.factories[stepType] = factory
}

// Create instantiates a PipelineStep of the given type.
// app must be a modular.Application; it is typed as any to satisfy
// the interfaces.StepRegistrar interface without an import cycle.
func (r *StepRegistry) Create(stepType, name string, config map[string]any, app any) (PipelineStep, error) {
	factory, ok := r.factories[stepType]
	if !ok {
		return nil, fmt.Errorf("unknown step type: %s", stepType)
	}
	a, _ := app.(modular.Application)
	return factory(name, config, a)
}

// Types returns all registered step type names.
func (r *StepRegistry) Types() []string {
	types := make([]string, 0, len(r.factories))
	for t := range r.factories {
		types = append(types, t)
	}
	return types
}
