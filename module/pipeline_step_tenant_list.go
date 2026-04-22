package module

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// TenantListStep lists tenants from the registry with optional filtering.
type TenantListStep struct {
	name       string
	activeOnly bool
	domain     string // static domain filter (optional)
	slug       string // static slug filter (optional)
	limit      int
	offset     int
	app        modular.Application
}

// NewTenantListStepFactory returns a StepFactory for step.tenant_list.
func NewTenantListStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		activeOnly, _ := config["active_only"].(bool)
		domain, _ := config["domain"].(string)
		slug, _ := config["slug"].(string)
		limit, _ := config["limit"].(int)
		offset, _ := config["offset"].(int)
		return &TenantListStep{
			name:       name,
			activeOnly: activeOnly,
			domain:     domain,
			slug:       slug,
			limit:      limit,
			offset:     offset,
			app:        app,
		}, nil
	}
}

// Name returns the step name.
func (s *TenantListStep) Name() string { return s.name }

// Execute calls TenantRegistry.List and writes results to the pipeline context.
func (s *TenantListStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	reg, err := resolveTenantRegistry(s.app)
	if err != nil {
		return nil, fmt.Errorf("tenant_list step %q: %w", s.name, err)
	}

	filter := interfaces.TenantFilter{
		ActiveOnly: s.activeOnly,
		Domain:     s.domain,
		Slug:       s.slug,
		Limit:      s.limit,
		Offset:     s.offset,
	}
	tenants, err := reg.List(filter)
	if err != nil {
		return nil, fmt.Errorf("tenant_list step %q: %w", s.name, err)
	}

	return &StepResult{
		Output: map[string]any{
			"tenants": tenants,
			"count":   len(tenants),
		},
	}, nil
}
