package module

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// ─── step.iac_provider_destroy ───────────────────────────────────────────────

// IaCProviderDestroyStep resolves an IaCProvider and destroys the specified
// resources by ref list.
type IaCProviderDestroyStep struct {
	name     string
	provider string
	refs     []interfaces.ResourceRef
	app      modular.Application
}

// NewIaCProviderDestroyStepFactory returns a StepFactory for step.iac_provider_destroy.
func NewIaCProviderDestroyStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		providerName, _ := cfg["provider"].(string)
		if providerName == "" {
			return nil, fmt.Errorf("iac_provider_destroy step %q: 'provider' is required", name)
		}
		refs, err := parseResourceRefs(cfg["refs"])
		if err != nil {
			return nil, fmt.Errorf("iac_provider_destroy step %q: parse refs: %w", name, err)
		}
		return &IaCProviderDestroyStep{
			name:     name,
			provider: providerName,
			refs:     refs,
			app:      app,
		}, nil
	}
}

func (s *IaCProviderDestroyStep) Name() string { return s.name }

func (s *IaCProviderDestroyStep) Execute(ctx context.Context, _ *PipelineContext) (*StepResult, error) {
	provider, err := resolveIaCProvider(s.app, s.provider, s.name, "iac_provider_destroy")
	if err != nil {
		return nil, err
	}

	result, err := provider.Destroy(ctx, s.refs)
	if err != nil {
		return nil, fmt.Errorf("iac_provider_destroy step %q: Destroy: %w", s.name, err)
	}

	var destroyed []string
	var destroyErrors []map[string]any
	if result != nil {
		destroyed = result.Destroyed
		destroyErrors = make([]map[string]any, 0, len(result.Errors))
		for _, e := range result.Errors {
			destroyErrors = append(destroyErrors, map[string]any{
				"resource": e.Resource,
				"action":   e.Action,
				"error":    e.Error,
			})
		}
	}

	return &StepResult{Output: map[string]any{
		"destroyed":      destroyed,
		"destroy_errors": destroyErrors,
		"provider":       s.provider,
	}}, nil
}
