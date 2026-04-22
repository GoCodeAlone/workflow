package module_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/module"
)

// stubSelector always returns a fixed key and matched state.
type stubSelector struct {
	key     string
	matched bool
	err     error
}

func (s *stubSelector) Match(_ *http.Request) (string, bool, error) {
	return s.key, s.matched, s.err
}

// stubRegistry returns a fake tenant by slug.
type stubRegistry struct {
	tenants map[string]interfaces.Tenant
}

func (r *stubRegistry) GetBySlug(slug string) (interfaces.Tenant, error) {
	if t, ok := r.tenants[slug]; ok {
		return t, nil
	}
	return interfaces.Tenant{}, interfaces.ErrResourceNotFound
}

func (r *stubRegistry) Ensure(spec interfaces.TenantSpec) (interfaces.Tenant, error) {
	return interfaces.Tenant{}, nil
}
func (r *stubRegistry) GetByID(_ string) (interfaces.Tenant, error) {
	return interfaces.Tenant{}, nil
}
func (r *stubRegistry) GetByDomain(d string) (interfaces.Tenant, error) {
	if t, ok := r.tenants[d]; ok {
		return t, nil
	}
	return interfaces.Tenant{}, interfaces.ErrResourceNotFound
}
func (r *stubRegistry) List(_ interfaces.TenantFilter) ([]interfaces.Tenant, error) {
	return nil, nil
}
func (r *stubRegistry) Update(_ string, _ interfaces.TenantPatch) (interfaces.Tenant, error) {
	return interfaces.Tenant{}, nil
}
func (r *stubRegistry) Disable(_ string) error { return nil }

func makeReq() *http.Request {
	return httptest.NewRequest(http.MethodGet, "/", nil)
}

func TestTenantContextResolver_FirstMatch(t *testing.T) {
	reg := &stubRegistry{tenants: map[string]interfaces.Tenant{
		"acme": {ID: "1", Slug: "acme", IsActive: true},
		"beta": {ID: "2", Slug: "beta", IsActive: true},
	}}
	resolver := module.NewTenantContextResolver(module.TenantContextResolverConfig{
		Mode:     "first_match",
		Registry: reg,
		Selectors: []interfaces.Selector{
			&stubSelector{key: "acme", matched: true},
			&stubSelector{key: "beta", matched: true},
		},
	})

	tenant, err := resolver.Resolve(context.Background(), makeReq())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tenant.Slug != "acme" {
		t.Errorf("first_match: expected 'acme', got %q", tenant.Slug)
	}
}

func TestTenantContextResolver_FirstMatch_NoMatch(t *testing.T) {
	reg := &stubRegistry{tenants: map[string]interfaces.Tenant{}}
	resolver := module.NewTenantContextResolver(module.TenantContextResolverConfig{
		Mode:     "first_match",
		Registry: reg,
		Selectors: []interfaces.Selector{
			&stubSelector{key: "", matched: false},
		},
	})

	tenant, err := resolver.Resolve(context.Background(), makeReq())
	if err != nil {
		t.Fatalf("unexpected error on no-match: %v", err)
	}
	if !tenant.IsZero() {
		t.Error("expected zero tenant when no selector matches")
	}
}

func TestTenantContextResolver_AllMustMatch_Agree(t *testing.T) {
	reg := &stubRegistry{tenants: map[string]interfaces.Tenant{
		"acme": {ID: "1", Slug: "acme", IsActive: true},
	}}
	resolver := module.NewTenantContextResolver(module.TenantContextResolverConfig{
		Mode:     "all_must_match",
		Registry: reg,
		Selectors: []interfaces.Selector{
			&stubSelector{key: "acme", matched: true},
			&stubSelector{key: "acme", matched: true},
		},
	})

	tenant, err := resolver.Resolve(context.Background(), makeReq())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tenant.Slug != "acme" {
		t.Errorf("all_must_match agree: expected 'acme', got %q", tenant.Slug)
	}
}

func TestTenantContextResolver_AllMustMatch_Disagree(t *testing.T) {
	reg := &stubRegistry{tenants: map[string]interfaces.Tenant{
		"acme": {ID: "1", Slug: "acme", IsActive: true},
		"beta": {ID: "2", Slug: "beta", IsActive: true},
	}}
	resolver := module.NewTenantContextResolver(module.TenantContextResolverConfig{
		Mode:     "all_must_match",
		Registry: reg,
		Selectors: []interfaces.Selector{
			&stubSelector{key: "acme", matched: true},
			&stubSelector{key: "beta", matched: true},
		},
	})

	_, err := resolver.Resolve(context.Background(), makeReq())
	if err == nil {
		t.Error("all_must_match with disagreement should return error")
	}
}

func TestTenantContextResolver_Consensus_Majority(t *testing.T) {
	reg := &stubRegistry{tenants: map[string]interfaces.Tenant{
		"acme": {ID: "1", Slug: "acme", IsActive: true},
		"beta": {ID: "2", Slug: "beta", IsActive: true},
	}}
	resolver := module.NewTenantContextResolver(module.TenantContextResolverConfig{
		Mode:     "consensus",
		Registry: reg,
		Selectors: []interfaces.Selector{
			&stubSelector{key: "acme", matched: true},
			&stubSelector{key: "acme", matched: true},
			&stubSelector{key: "beta", matched: true},
		},
	})

	tenant, err := resolver.Resolve(context.Background(), makeReq())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tenant.Slug != "acme" {
		t.Errorf("consensus: expected 'acme' (2/3 votes), got %q", tenant.Slug)
	}
}

func TestTenantContextKey(t *testing.T) {
	expected := interfaces.Tenant{ID: "t1", Slug: "acme"}
	ctx := module.WithTenant(context.Background(), expected)
	got := module.TenantFromContext(ctx)
	if got.ID != expected.ID {
		t.Errorf("context round-trip: got %q, want %q", got.ID, expected.ID)
	}
}
