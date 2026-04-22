package module

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/modular"
)

// TenantDisableStep soft-deletes a tenant (sets is_active=false).
type TenantDisableStep struct {
	name  string
	idKey string // key in pipeline context holding the tenant ID
	app   modular.Application
}

// NewTenantDisableStepFactory returns a StepFactory for step.tenant_disable.
func NewTenantDisableStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		idKey, _ := config["id_key"].(string)
		if idKey == "" {
			idKey = "tenant_id"
		}
		return &TenantDisableStep{
			name:  name,
			idKey: idKey,
			app:   app,
		}, nil
	}
}

// Name returns the step name.
func (s *TenantDisableStep) Name() string { return s.name }

// Execute calls TenantRegistry.Disable with the tenant ID from the pipeline context.
func (s *TenantDisableStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	reg, err := resolveTenantRegistry(s.app)
	if err != nil {
		return nil, fmt.Errorf("tenant_disable step %q: %w", s.name, err)
	}

	id, _ := pc.Current[s.idKey].(string)
	if id == "" {
		return nil, fmt.Errorf("tenant_disable step %q: id key %q not found or empty in pipeline context", s.name, s.idKey)
	}

	if err := reg.Disable(id); err != nil {
		return nil, fmt.Errorf("tenant_disable step %q: %w", s.name, err)
	}

	return &StepResult{
		Output: map[string]any{
			"disabled": true,
			"id":       id,
		},
	}, nil
}
