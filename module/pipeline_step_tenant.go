package module

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/modular"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// TenantRegistryServiceName is the service registry key for interfaces.TenantRegistry.
const TenantRegistryServiceName = "tenant_registry"

// tenantAppProvider is a narrow interface satisfied by both modular.Application and
// lightweight test stubs. Step structs store this instead of the full
// modular.Application, enabling unit tests to inject a minimal fake.
type tenantAppProvider interface {
	GetService(name string, target any) error
}

// tenantToMap converts a Tenant to a plain map for pipeline output.
func tenantToMap(t interfaces.Tenant) map[string]any {
	m := map[string]any{
		"id":        t.ID,
		"name":      t.Name,
		"slug":      t.Slug,
		"is_active": t.IsActive,
	}
	if len(t.Domains) > 0 {
		m["domains"] = t.Domains
	}
	if len(t.Metadata) > 0 {
		m["metadata"] = t.Metadata
	}
	return m
}

// tenantRegistryFrom retrieves the TenantRegistry from app's service registry.
func tenantRegistryFrom(app tenantAppProvider) (interfaces.TenantRegistry, error) {
	var reg interfaces.TenantRegistry
	if err := app.GetService(TenantRegistryServiceName, &reg); err != nil {
		return nil, fmt.Errorf("tenant registry not available: %w", err)
	}
	if reg == nil {
		return nil, fmt.Errorf("tenant registry is nil")
	}
	return reg, nil
}

// ── tenant_ensure ──────────────────────────────────────────────────────────────

// TenantEnsureStep creates a tenant if it doesn't exist, or returns the existing one.
type TenantEnsureStep struct {
	name    string
	nameKey string
	slugKey string
	app     tenantAppProvider
}

// NewTenantEnsureStepFactory returns a factory for step.tenant_ensure.
// The returned function accepts any tenantAppProvider (including modular.Application
// and lightweight test fakes). To register in the StepFactory registry call
// the returned func directly — it accepts modular.Application too since
// modular.Application satisfies tenantAppProvider.
func NewTenantEnsureStepFactory() func(name string, config map[string]any, app tenantAppProvider) (PipelineStep, error) {
	return func(name string, config map[string]any, app tenantAppProvider) (PipelineStep, error) {
		nameKey, _ := config["name_key"].(string)
		slugKey, _ := config["slug_key"].(string)
		return &TenantEnsureStep{name: name, nameKey: nameKey, slugKey: slugKey, app: app}, nil
	}
}

// NewTenantEnsureStepFactoryStd wraps NewTenantEnsureStepFactory as a StepFactory
// for registration in the step registry.
func NewTenantEnsureStepFactoryStd() StepFactory {
	inner := NewTenantEnsureStepFactory()
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		return inner(name, config, app)
	}
}

func (s *TenantEnsureStep) Name() string { return s.name }

func (s *TenantEnsureStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	reg, err := tenantRegistryFrom(s.app)
	if err != nil {
		return nil, err
	}

	tenantName, _ := pc.Current[s.nameKey].(string)
	tenantSlug, _ := pc.Current[s.slugKey].(string)
	if tenantSlug == "" {
		return nil, fmt.Errorf("tenant ensure: %q is required but missing from pipeline context", s.slugKey)
	}

	tenant, err := reg.Ensure(interfaces.TenantSpec{Name: tenantName, Slug: tenantSlug})
	if err != nil {
		return nil, fmt.Errorf("tenant ensure %q: %w", tenantSlug, err)
	}

	return &StepResult{Output: map[string]any{"tenant": tenantToMap(tenant)}}, nil
}

// ── tenant_list ───────────────────────────────────────────────────────────────

// TenantListStep lists tenants matching a filter.
type TenantListStep struct {
	name       string
	activeOnly bool
	limit      int
	offset     int
	app        tenantAppProvider
}

// NewTenantListStepFactory returns a factory for step.tenant_list.
func NewTenantListStepFactory() func(name string, config map[string]any, app tenantAppProvider) (PipelineStep, error) {
	return func(name string, config map[string]any, app tenantAppProvider) (PipelineStep, error) {
		activeOnly, _ := config["active_only"].(bool)
		limit, _ := config["limit"].(int)
		offset, _ := config["offset"].(int)
		return &TenantListStep{
			name:       name,
			activeOnly: activeOnly,
			limit:      limit,
			offset:     offset,
			app:        app,
		}, nil
	}
}

// NewTenantListStepFactoryStd wraps NewTenantListStepFactory as a StepFactory.
func NewTenantListStepFactoryStd() StepFactory {
	inner := NewTenantListStepFactory()
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		return inner(name, config, app)
	}
}

func (s *TenantListStep) Name() string { return s.name }

func (s *TenantListStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	reg, err := tenantRegistryFrom(s.app)
	if err != nil {
		return nil, err
	}

	filter := interfaces.TenantFilter{
		ActiveOnly: s.activeOnly,
		Limit:      s.limit,
		Offset:     s.offset,
	}
	tenants, err := reg.List(filter)
	if err != nil {
		return nil, fmt.Errorf("tenant list: %w", err)
	}

	mapped := make([]map[string]any, len(tenants))
	for i, t := range tenants {
		mapped[i] = tenantToMap(t)
	}
	return &StepResult{Output: map[string]any{"tenants": mapped, "count": len(tenants)}}, nil
}

// ── tenant_get_by_domain ──────────────────────────────────────────────────────

// TenantGetByDomainStep looks up a tenant by a domain value from the pipeline context.
type TenantGetByDomainStep struct {
	name      string
	domainKey string
	app       tenantAppProvider
}

// NewTenantGetByDomainStepFactory returns a factory for step.tenant_get_by_domain.
func NewTenantGetByDomainStepFactory() func(name string, config map[string]any, app tenantAppProvider) (PipelineStep, error) {
	return func(name string, config map[string]any, app tenantAppProvider) (PipelineStep, error) {
		domainKey, _ := config["domain_key"].(string)
		return &TenantGetByDomainStep{name: name, domainKey: domainKey, app: app}, nil
	}
}

// NewTenantGetByDomainStepFactoryStd wraps as StepFactory.
func NewTenantGetByDomainStepFactoryStd() StepFactory {
	inner := NewTenantGetByDomainStepFactory()
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		return inner(name, config, app)
	}
}

func (s *TenantGetByDomainStep) Name() string { return s.name }

func (s *TenantGetByDomainStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	reg, err := tenantRegistryFrom(s.app)
	if err != nil {
		return nil, err
	}

	domain, _ := pc.Current[s.domainKey].(string)
	if domain == "" {
		return nil, fmt.Errorf("tenant get by domain: %q is required but missing from pipeline context", s.domainKey)
	}
	tenant, err := reg.GetByDomain(domain)
	if err != nil {
		return nil, fmt.Errorf("tenant get by domain %q: %w", domain, err)
	}

	return &StepResult{Output: map[string]any{"tenant": tenantToMap(tenant)}}, nil
}

// ── tenant_update ─────────────────────────────────────────────────────────────

// TenantUpdateStep applies a partial patch to an existing tenant.
type TenantUpdateStep struct {
	name    string
	idKey   string
	nameKey string
	app     tenantAppProvider
}

// NewTenantUpdateStepFactory returns a factory for step.tenant_update.
func NewTenantUpdateStepFactory() func(name string, config map[string]any, app tenantAppProvider) (PipelineStep, error) {
	return func(name string, config map[string]any, app tenantAppProvider) (PipelineStep, error) {
		idKey, _ := config["id_key"].(string)
		nameKey, _ := config["name_key"].(string)
		return &TenantUpdateStep{name: name, idKey: idKey, nameKey: nameKey, app: app}, nil
	}
}

// NewTenantUpdateStepFactoryStd wraps as StepFactory.
func NewTenantUpdateStepFactoryStd() StepFactory {
	inner := NewTenantUpdateStepFactory()
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		return inner(name, config, app)
	}
}

func (s *TenantUpdateStep) Name() string { return s.name }

func (s *TenantUpdateStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	reg, err := tenantRegistryFrom(s.app)
	if err != nil {
		return nil, err
	}

	id, _ := pc.Current[s.idKey].(string)
	if id == "" {
		return nil, fmt.Errorf("tenant update: %q is required but missing from pipeline context", s.idKey)
	}

	var patch interfaces.TenantPatch
	if s.nameKey != "" {
		if v, ok := pc.Current[s.nameKey].(string); ok {
			patch.Name = &v
		}
	}

	tenant, err := reg.Update(id, patch)
	if err != nil {
		return nil, fmt.Errorf("tenant update %q: %w", id, err)
	}

	return &StepResult{Output: map[string]any{"tenant": tenantToMap(tenant)}}, nil
}

// ── tenant_disable ────────────────────────────────────────────────────────────

// TenantDisableStep soft-deletes a tenant by ID.
type TenantDisableStep struct {
	name  string
	idKey string
	app   tenantAppProvider
}

// NewTenantDisableStepFactory returns a factory for step.tenant_disable.
func NewTenantDisableStepFactory() func(name string, config map[string]any, app tenantAppProvider) (PipelineStep, error) {
	return func(name string, config map[string]any, app tenantAppProvider) (PipelineStep, error) {
		idKey, _ := config["id_key"].(string)
		return &TenantDisableStep{name: name, idKey: idKey, app: app}, nil
	}
}

// NewTenantDisableStepFactoryStd wraps as StepFactory.
func NewTenantDisableStepFactoryStd() StepFactory {
	inner := NewTenantDisableStepFactory()
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		return inner(name, config, app)
	}
}

func (s *TenantDisableStep) Name() string { return s.name }

func (s *TenantDisableStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	reg, err := tenantRegistryFrom(s.app)
	if err != nil {
		return nil, err
	}

	id, _ := pc.Current[s.idKey].(string)
	if id == "" {
		return nil, fmt.Errorf("tenant disable: %q is required but missing from pipeline context", s.idKey)
	}

	if err := reg.Disable(id); err != nil {
		return nil, fmt.Errorf("tenant disable %q: %w", id, err)
	}

	return &StepResult{Output: map[string]any{"disabled": true, "id": id}}, nil
}
