package module

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// TenantUpdateStep applies a partial patch to an existing tenant.
type TenantUpdateStep struct {
	name    string
	idKey   string // key in pipeline context holding the tenant ID
	nameKey string // key in pipeline context holding the new name (optional)
	app     modular.Application
}

// NewTenantUpdateStepFactory returns a StepFactory for step.tenant_update.
func NewTenantUpdateStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		idKey, _ := config["id_key"].(string)
		if idKey == "" {
			idKey = "tenant_id"
		}
		nameKey, _ := config["name_key"].(string)
		return &TenantUpdateStep{
			name:    name,
			idKey:   idKey,
			nameKey: nameKey,
			app:     app,
		}, nil
	}
}

// Name returns the step name.
func (s *TenantUpdateStep) Name() string { return s.name }

// Execute calls TenantRegistry.Update with the patch derived from the pipeline context.
func (s *TenantUpdateStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	reg, err := resolveTenantRegistry(s.app)
	if err != nil {
		return nil, fmt.Errorf("tenant_update step %q: %w", s.name, err)
	}

	id, _ := pc.Current[s.idKey].(string)
	if id == "" {
		return nil, fmt.Errorf("tenant_update step %q: id key %q not found in pipeline context", s.name, s.idKey)
	}

	var patch interfaces.TenantPatch
	if s.nameKey != "" {
		if newName, ok := pc.Current[s.nameKey].(string); ok && newName != "" {
			patch.Name = &newName
		}
	}

	tenant, err := reg.Update(id, patch)
	if err != nil {
		return nil, fmt.Errorf("tenant_update step %q: %w", s.name, err)
	}

	return &StepResult{
		Output: map[string]any{
			"tenant": tenantToMap(tenant),
		},
	}, nil
}
