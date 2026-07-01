package module

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// resolveIaCProvider looks up an interfaces.IaCProvider from the service registry.
// stepType is the step type name (e.g. "iac_provider_list") for error messages.
func resolveIaCProvider(app modular.Application, providerName, stepName, stepType string) (interfaces.IaCProvider, error) {
	if app == nil {
		return nil, fmt.Errorf("%s step %q: no application context", stepType, stepName)
	}
	svc, ok := app.SvcRegistry()[providerName]
	if !ok {
		return nil, fmt.Errorf("%s step %q: provider service %q not registered", stepType, stepName, providerName)
	}
	provider, ok := svc.(interfaces.IaCProvider)
	if !ok {
		return nil, fmt.Errorf("%s step %q: service %q does not implement IaCProvider (got %T)", stepType, stepName, providerName, svc)
	}
	return provider, nil
}

// ─── step.iac_provider_list ──────────────────────────────────────────────────

// IaCProviderListStep resolves an IaCProvider and lists current resource statuses.
type IaCProviderListStep struct {
	name      string
	provider  string
	refs      []interfaces.ResourceRef
	resources []string
	app       modular.Application
}

// NewIaCProviderListStepFactory returns a StepFactory for step.iac_provider_list.
func NewIaCProviderListStepFactory() StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (PipelineStep, error) {
		providerName, _ := cfg["provider"].(string)
		if providerName == "" {
			return nil, fmt.Errorf("iac_provider_list step %q: 'provider' is required", name)
		}
		// Optional: list of refs to query; absent means pass nil to Status
		// (providers should return all resources when refs is nil/empty).
		// If "refs" is present but malformed (wrong type or wrong item shape), the
		// factory returns a config error — silently widening to list-all would mask a
		// misconfigured step that was intended to be a filtered query.
		var refs []interfaces.ResourceRef
		_, hasRefs := cfg["refs"]
		if rawRefs, ok := cfg["refs"]; ok {
			refList, ok := rawRefs.([]any)
			if !ok {
				return nil, fmt.Errorf("iac_provider_list step %q: 'refs' must be a list, got %T", name, rawRefs)
			}
			for i, r := range refList {
				rm, ok := r.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("iac_provider_list step %q: refs[%d] must be a map, got %T", name, i, r)
				}
				ref := interfaces.ResourceRef{}
				if n, ok := rm["name"].(string); ok {
					ref.Name = n
				}
				if t, ok := rm["type"].(string); ok {
					ref.Type = t
				}
				if pid, ok := rm["provider_id"].(string); ok {
					ref.ProviderID = pid
				}
				refs = append(refs, ref)
			}
		}
		resources, hasResources, err := parseResourceNames(cfg["resources"])
		if err != nil {
			return nil, fmt.Errorf("iac_provider_list step %q: parse resources: %w", name, err)
		}
		if hasRefs && hasResources {
			return nil, fmt.Errorf("iac_provider_list step %q: 'refs' and 'resources' are mutually exclusive", name)
		}
		return &IaCProviderListStep{
			name:      name,
			provider:  providerName,
			refs:      refs,
			resources: resources,
			app:       app,
		}, nil
	}
}

func (s *IaCProviderListStep) Name() string { return s.name }

func (s *IaCProviderListStep) Execute(ctx context.Context, _ *PipelineContext) (*StepResult, error) {
	provider, err := resolveIaCProvider(s.app, s.provider, s.name, "iac_provider_list")
	if err != nil {
		return nil, err
	}

	refs := s.refs
	if len(s.resources) > 0 {
		refs, err = resolveResourceRefs(s.app, s.resources)
		if err != nil {
			return nil, fmt.Errorf("iac_provider_list step %q: resolve resources: %w", s.name, err)
		}
	}

	statuses, err := provider.Status(ctx, refs)
	if err != nil {
		return nil, fmt.Errorf("iac_provider_list step %q: Status: %w", s.name, err)
	}

	resources := make([]map[string]any, 0, len(statuses))
	for _, st := range statuses {
		resources = append(resources, map[string]any{
			"name":        st.Name,
			"type":        st.Type,
			"provider_id": st.ProviderID,
			"status":      st.Status,
			"outputs":     st.Outputs,
		})
	}

	return &StepResult{Output: map[string]any{
		"provider":  s.provider,
		"resources": resources,
		"count":     len(resources),
	}}, nil
}
