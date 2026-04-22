package module_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
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

// spyEmitter captures calls to EmitTenantMismatch for test assertions.
type spyEmitter struct {
	mu     sync.Mutex
	events []map[string]any
}

func (e *spyEmitter) EmitTenantMismatch(_ context.Context, data map[string]any) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, data)
	return nil
}

func (e *spyEmitter) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.events)
}

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
	spy := &spyEmitter{}
	resolver := module.NewTenantContextResolver(module.TenantContextResolverConfig{
		Mode:         "all_must_match",
		Registry:     reg,
		EventEmitter: spy,
		Selectors: []interfaces.Selector{
			&stubSelector{key: "acme", matched: true},
			&stubSelector{key: "beta", matched: true},
		},
	})

	_, err := resolver.Resolve(context.Background(), makeReq())
	if err == nil {
		t.Fatal("all_must_match with disagreement should return error")
	}
	if !errors.Is(err, module.ErrTenantMismatch) {
		t.Errorf("expected ErrTenantMismatch, got: %v", err)
	}
	if spy.count() != 1 {
		t.Errorf("expected 1 mismatch event, got %d", spy.count())
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

func TestTenantContextResolver_Consensus_MinVotes(t *testing.T) {
	reg := &stubRegistry{tenants: map[string]interfaces.Tenant{
		"acme": {ID: "1", Slug: "acme", IsActive: true},
	}}
	// Require 3 votes but only 2 agree — should return zero tenant.
	resolver := module.NewTenantContextResolver(module.TenantContextResolverConfig{
		Mode:     "consensus",
		MinVotes: 3,
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
	if !tenant.IsZero() {
		t.Errorf("expected zero tenant when votes < MinVotes, got %+v", tenant)
	}
}

// TestTenantMiddleware_SessionHijackEmulation verifies that when a request carries
// conflicting tenant signals (simulating a session-hijack attempt), the middleware:
//  1. Responds with HTTP 403 Forbidden
//  2. Emits a tenant.mismatch event via the EventEmitter
func TestTenantMiddleware_SessionHijackEmulation(t *testing.T) {
	reg := &stubRegistry{tenants: map[string]interfaces.Tenant{
		"acme": {ID: "1", Slug: "acme", IsActive: true},
		"evil": {ID: "2", Slug: "evil", IsActive: true},
	}}
	spy := &spyEmitter{}

	// Simulate two selectors disagreeing: cookie says "acme", JWT claim says "evil".
	resolver := module.NewTenantContextResolver(module.TenantContextResolverConfig{
		Mode:         "all_must_match",
		Registry:     reg,
		EventEmitter: spy,
		Selectors: []interfaces.Selector{
			&stubSelector{key: "acme", matched: true}, // cookie selector
			&stubSelector{key: "evil", matched: true}, // tampered JWT claim
		},
	})

	// Wrap a no-op handler with TenantMiddleware.
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})
	handler := module.TenantMiddleware(resolver, next)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, makeReq())

	// 1. Verify 403 Forbidden.
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden, got %d", w.Code)
	}

	// 2. Verify JSON error body.
	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	if body["error"] != "tenant.mismatch" {
		t.Errorf("expected error=tenant.mismatch, got %v", body)
	}

	// 3. Verify mismatch event was emitted.
	if spy.count() != 1 {
		t.Errorf("expected 1 mismatch event emitted, got %d", spy.count())
	}

	// 4. Verify next handler was NOT called.
	if nextCalled {
		t.Error("next handler should not be called on mismatch")
	}
}

// errorResolver always returns the configured error from Resolve.
type errorResolver struct{ err error }

func (e *errorResolver) Resolve(_ context.Context, _ *http.Request) (interfaces.Tenant, error) {
	return interfaces.Tenant{}, e.err
}

// TestTenantMiddleware_InfraError verifies that non-mismatch errors (e.g. registry
// timeouts, slug lookup failures) produce 500 Internal Server Error with plain text,
// NOT 403 + mismatch JSON — preventing infra failures from being misclassified as
// security events.
func TestTenantMiddleware_InfraError(t *testing.T) {
	infraErr := fmt.Errorf("postgres: connection refused")
	resolver := &errorResolver{err: infraErr}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler must not be called on infra error")
	})
	handler := module.TenantMiddleware(resolver, next)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, makeReq())

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 Internal Server Error, got %d", w.Code)
	}
	body := w.Body.String()
	if body == "" {
		t.Error("expected non-empty error body")
	}
	// Must NOT be a JSON mismatch response.
	var jsonBody map[string]string
	if json.Unmarshal([]byte(body), &jsonBody) == nil && jsonBody["error"] == "tenant.mismatch" {
		t.Error("infra error must not produce tenant.mismatch JSON body")
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
