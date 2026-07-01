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
	name      string
	provider  string
	refs      []interfaces.ResourceRef
	refsFrom  string
	resources []string
	app       modular.Application
}

// NewIaCProviderDestroyStepFactory returns a StepFactory for step.iac_provider_destroy.
func NewIaCProviderDestroyStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		providerName, _ := cfg["provider"].(string)
		if providerName == "" {
			return nil, fmt.Errorf("iac_provider_destroy step %q: 'provider' is required", name)
		}
		refsFrom, _ := cfg["refs_from"].(string)
		_, hasRefs := cfg["refs"]
		refs, err := parseResourceRefs(cfg["refs"])
		if err != nil {
			return nil, fmt.Errorf("iac_provider_destroy step %q: parse refs: %w", name, err)
		}
		rawResources, hasResourcesKey := cfg["resources"]
		resources, hasResources, err := parseResourceNames(rawResources, hasResourcesKey)
		if err != nil {
			return nil, fmt.Errorf("iac_provider_destroy step %q: parse resources: %w", name, err)
		}
		inputSources := 0
		if hasRefs {
			inputSources++
		}
		if refsFrom != "" {
			inputSources++
		}
		if hasResources {
			inputSources++
		}
		if inputSources > 1 {
			return nil, fmt.Errorf("iac_provider_destroy step %q: 'refs', 'refs_from', and 'resources' are mutually exclusive", name)
		}
		return &IaCProviderDestroyStep{
			name:      name,
			provider:  providerName,
			refs:      refs,
			refsFrom:  refsFrom,
			resources: resources,
			app:       app,
		}, nil
	}
}

func (s *IaCProviderDestroyStep) Name() string { return s.name }

func (s *IaCProviderDestroyStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	provider, err := resolveIaCProvider(s.app, s.provider, s.name, "iac_provider_destroy")
	if err != nil {
		return nil, err
	}

	refs := s.refs
	if s.refsFrom != "" {
		refs, err = resolveResourceRefsFrom(s.refsFrom, s.name, "iac_provider_destroy", pc)
		if err != nil {
			return nil, err
		}
	}
	if len(s.resources) > 0 {
		refs, err = resolveResourceRefs(s.app, s.resources)
		if err != nil {
			return nil, fmt.Errorf("iac_provider_destroy step %q: resolve resources: %w", s.name, err)
		}
	}

	result, err := provider.Destroy(ctx, refs)
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
