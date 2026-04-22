package module

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/modular"
)

// TenantGetByDomainStep looks up a tenant by domain from the pipeline context.
type TenantGetByDomainStep struct {
	name      string
	domainKey string // key in pipeline context holding the domain value
	app       modular.Application
}

// NewTenantGetByDomainStepFactory returns a StepFactory for step.tenant_get_by_domain.
func NewTenantGetByDomainStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		domainKey, _ := config["domain_key"].(string)
		if domainKey == "" {
			domainKey = "domain"
		}
		return &TenantGetByDomainStep{
			name:      name,
			domainKey: domainKey,
			app:       app,
		}, nil
	}
}

// Name returns the step name.
func (s *TenantGetByDomainStep) Name() string { return s.name }

// Execute calls TenantRegistry.GetByDomain with the domain from the pipeline context.
func (s *TenantGetByDomainStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	reg, err := resolveTenantRegistry(s.app)
	if err != nil {
		return nil, fmt.Errorf("tenant_get_by_domain step %q: %w", s.name, err)
	}

	domain, _ := pc.Current[s.domainKey].(string)
	if domain == "" {
		return nil, fmt.Errorf("tenant_get_by_domain step %q: domain key %q not found in pipeline context", s.name, s.domainKey)
	}

	tenant, err := reg.GetByDomain(domain)
	if err != nil {
		return nil, fmt.Errorf("tenant_get_by_domain step %q: %w", s.name, err)
	}

	return &StepResult{
		Output: map[string]any{
			"tenant": tenantToMap(tenant),
		},
	}, nil
}
