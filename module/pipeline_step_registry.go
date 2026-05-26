package module

import (
	"fmt"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/internal/legacydo"
)

// StepFactory creates a PipelineStep from its name and config.
type StepFactory func(name string, config map[string]any, app modular.Application) (PipelineStep, error)

// StepRegistry maps step type strings to factory functions.
type StepRegistry struct {
	factories         map[string]StepFactory
	iacProviderLoaded bool // set by SetIaCProviderLoaded; consumed by Create
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

// SetIaCProviderLoaded is called by the engine after module factory registration
// is complete and before pipeline construction. Per-registry state — no global —
// so parallel test runs that build independent StepRegistry instances do not
// share or race the flag.
func (r *StepRegistry) SetIaCProviderLoaded(loaded bool) {
	r.iacProviderLoaded = loaded
}

// Create instantiates a PipelineStep of the given type.
// app must be a modular.Application; it is typed as any to satisfy
// the interfaces.StepRegistrar interface without an import cycle.
func (r *StepRegistry) Create(stepType, name string, config map[string]any, app any) (PipelineStep, error) {
	factory, ok := r.factories[stepType]
	if !ok {
		if legacydo.IsStepType(stepType) {
			return nil, legacydo.FormatStepError(stepType, r.iacProviderLoaded)
		}
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
