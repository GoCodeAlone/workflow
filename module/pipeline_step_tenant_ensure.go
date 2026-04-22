package module

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// TenantRegistryServiceName is the well-known service name for interfaces.TenantRegistry
// in the application service registry.
const TenantRegistryServiceName = "tenantRegistry"

// tenantToMap converts a Tenant struct to a plain map for pipeline output.
func tenantToMap(t interfaces.Tenant) map[string]any {
	return map[string]any{
		"id":        t.ID,
		"name":      t.Name,
		"slug":      t.Slug,
		"domains":   t.Domains,
		"metadata":  t.Metadata,
		"is_active": t.IsActive,
	}
}

// resolveTenantRegistry looks up the TenantRegistry service from the app.
func resolveTenantRegistry(app modular.Application) (interfaces.TenantRegistry, error) {
	var reg interfaces.TenantRegistry
	if err := app.GetService(TenantRegistryServiceName, &reg); err != nil {
		return nil, fmt.Errorf("tenant registry service %q not found: %w", TenantRegistryServiceName, err)
	}
	return reg, nil
}

// ---- step.tenant_ensure ----

// TenantEnsureStep creates a tenant if it does not exist, or returns the existing one.
type TenantEnsureStep struct {
	name    string
	nameKey string // key in pipeline context holding the tenant name
	slugKey string // key in pipeline context holding the tenant slug
	app     modular.Application
}

// NewTenantEnsureStepFactory returns a StepFactory for step.tenant_ensure.
func NewTenantEnsureStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		nameKey, _ := config["name_key"].(string)
		if nameKey == "" {
			nameKey = "tenant_name"
		}
		slugKey, _ := config["slug_key"].(string)
		if slugKey == "" {
			slugKey = "tenant_slug"
		}
		return &TenantEnsureStep{
			name:    name,
			nameKey: nameKey,
			slugKey: slugKey,
			app:     app,
		}, nil
	}
}

// Name returns the step name.
func (s *TenantEnsureStep) Name() string { return s.name }

// Execute calls TenantRegistry.Ensure and writes the result to the pipeline context.
func (s *TenantEnsureStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	reg, err := resolveTenantRegistry(s.app)
	if err != nil {
		return nil, fmt.Errorf("tenant_ensure step %q: %w", s.name, err)
	}

	tenantName, _ := pc.Current[s.nameKey].(string)
	tenantSlug, _ := pc.Current[s.slugKey].(string)

	spec := interfaces.TenantSpec{
		Name: tenantName,
		Slug: tenantSlug,
	}
	tenant, err := reg.Ensure(spec)
	if err != nil {
		return nil, fmt.Errorf("tenant_ensure step %q: %w", s.name, err)
	}

	return &StepResult{
		Output: map[string]any{
			"tenant": tenantToMap(tenant),
		},
	}, nil
}
