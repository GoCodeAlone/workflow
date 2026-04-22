package module

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// tenantContextKeyType is the context key type for the resolved tenant.
type tenantContextKeyType struct{}

var tenantContextKey = tenantContextKeyType{}

// WithTenant stores t in ctx and returns the updated context.
func WithTenant(ctx context.Context, t interfaces.Tenant) context.Context {
	return context.WithValue(ctx, tenantContextKey, t)
}

// TenantFromContext retrieves the tenant stored by WithTenant.
// Returns the zero Tenant if none is set.
func TenantFromContext(ctx context.Context) interfaces.Tenant {
	t, _ := ctx.Value(tenantContextKey).(interfaces.Tenant)
	return t
}

// ErrTenantMismatch is returned by Resolve when selectors disagree in all_must_match mode.
var ErrTenantMismatch = errors.New("tenant.mismatch")

// TenantMismatchEmitter is an optional event sink for tenant mismatch events.
// Typically backed by a MessageBroker or engine event bus.
// All methods must be safe to call concurrently; a nil emitter is treated as a no-op.
type TenantMismatchEmitter interface {
	EmitTenantMismatch(ctx context.Context, data map[string]any) error
}

// TenantContextResolverConfig configures a TenantContextResolver.
type TenantContextResolverConfig struct {
	// Mode controls how multiple selectors are combined:
	//   "first_match"    – use the first selector that matches (default)
	//   "all_must_match" – all matching selectors must agree; error + mismatch event if they disagree
	//   "consensus"      – use the key with the most votes across all matching selectors
	Mode string

	// Registry is used to look up tenants by the key returned by selectors.
	Registry interfaces.TenantRegistry

	// Selectors is the ordered list of selectors to try.
	Selectors []interfaces.Selector

	// MinVotes is the minimum number of agreeing votes required in "consensus" mode.
	// Defaults to a simple majority: len(Selectors)/2 + 1.
	MinVotes int

	// EventEmitter receives tenant.mismatch events when selectors disagree.
	// If nil, mismatch events are silently dropped (no-op).
	EventEmitter TenantMismatchEmitter
}

// TenantContextResolver implements interfaces.TenantResolver using a configurable
// combination of Selectors and a TenantRegistry.
type TenantContextResolver struct {
	cfg TenantContextResolverConfig
}

// NewTenantContextResolver creates a new TenantContextResolver.
func NewTenantContextResolver(cfg TenantContextResolverConfig) *TenantContextResolver {
	if cfg.Mode == "" {
		cfg.Mode = "first_match"
	}
	return &TenantContextResolver{cfg: cfg}
}

// Resolve resolves the tenant for the given request.
// Returns the zero Tenant (and no error) when no tenant can be determined.
// Returns ErrTenantMismatch (wrapping the conflict detail) when all_must_match
// detects disagreeing selectors.
func (r *TenantContextResolver) Resolve(ctx context.Context, req *http.Request) (interfaces.Tenant, error) {
	switch r.cfg.Mode {
	case "all_must_match":
		return r.resolveAllMustMatch(ctx, req)
	case "consensus":
		return r.resolveConsensus(req)
	default: // "first_match"
		return r.resolveFirstMatch(req)
	}
}

func (r *TenantContextResolver) resolveFirstMatch(req *http.Request) (interfaces.Tenant, error) {
	for _, s := range r.cfg.Selectors {
		key, matched, err := s.Match(req)
		if err != nil {
			return interfaces.Tenant{}, fmt.Errorf("selector match: %w", err)
		}
		if !matched || key == "" {
			continue
		}
		return r.lookup(key)
	}
	return interfaces.Tenant{}, nil
}

func (r *TenantContextResolver) resolveAllMustMatch(ctx context.Context, req *http.Request) (interfaces.Tenant, error) {
	var agreedKey string
	matched := 0
	for _, s := range r.cfg.Selectors {
		key, ok, err := s.Match(req)
		if err != nil {
			return interfaces.Tenant{}, fmt.Errorf("selector match: %w", err)
		}
		if !ok || key == "" {
			continue
		}
		matched++
		if agreedKey == "" {
			agreedKey = key
		} else if key != agreedKey {
			// Selectors disagree — emit mismatch event then return the sentinel error.
			r.emitMismatch(ctx, agreedKey, key)
			return interfaces.Tenant{}, fmt.Errorf("%w: selectors disagree (%q vs %q)", ErrTenantMismatch, agreedKey, key)
		}
	}
	if matched == 0 || agreedKey == "" {
		return interfaces.Tenant{}, nil
	}
	return r.lookup(agreedKey)
}

func (r *TenantContextResolver) resolveConsensus(req *http.Request) (interfaces.Tenant, error) {
	votes := make(map[string]int)
	for _, s := range r.cfg.Selectors {
		key, ok, err := s.Match(req)
		if err != nil {
			return interfaces.Tenant{}, fmt.Errorf("selector match: %w", err)
		}
		if !ok || key == "" {
			continue
		}
		votes[key]++
	}
	if len(votes) == 0 {
		return interfaces.Tenant{}, nil
	}

	minVotes := r.cfg.MinVotes
	if minVotes <= 0 {
		minVotes = len(r.cfg.Selectors)/2 + 1
	}

	// Find the key with the most votes.
	bestKey := ""
	bestVotes := 0
	for k, v := range votes {
		if v > bestVotes {
			bestVotes = v
			bestKey = k
		}
	}

	if bestVotes < minVotes {
		// No key reached the required threshold — return zero tenant.
		return interfaces.Tenant{}, nil
	}

	return r.lookup(bestKey)
}

// emitMismatch fires the tenant.mismatch event to the configured EventEmitter, if any.
func (r *TenantContextResolver) emitMismatch(ctx context.Context, first, conflicting string) {
	if r.cfg.EventEmitter == nil {
		return
	}
	_ = r.cfg.EventEmitter.EmitTenantMismatch(ctx, map[string]any{
		"event":       "tenant.mismatch",
		"first":       first,
		"conflicting": conflicting,
	})
}

// lookup retrieves a tenant by key, trying slug first then domain.
// Returns zero tenant (no error) if not found.
func (r *TenantContextResolver) lookup(key string) (interfaces.Tenant, error) {
	t, err := r.cfg.Registry.GetBySlug(key)
	if err == nil {
		return t, nil
	}
	if !errors.Is(err, interfaces.ErrResourceNotFound) {
		return interfaces.Tenant{}, fmt.Errorf("registry lookup by slug %q: %w", key, err)
	}
	// Fall back to domain lookup.
	t, err = r.cfg.Registry.GetByDomain(key)
	if err == nil {
		return t, nil
	}
	if errors.Is(err, interfaces.ErrResourceNotFound) {
		return interfaces.Tenant{}, nil
	}
	return interfaces.Tenant{}, fmt.Errorf("registry lookup by domain %q: %w", key, err)
}

// TenantMiddleware returns an http.Handler that resolves the tenant and stores
// it in the request context via WithTenant before calling next.
// On ErrTenantMismatch the middleware responds with 403 Forbidden and a
// JSON body {"error":"tenant.mismatch"}.
func TenantMiddleware(resolver interfaces.TenantResolver, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenant, err := resolver.Resolve(r.Context(), r)
		if err != nil {
			status := http.StatusForbidden
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "tenant.mismatch"})
			return
		}
		if !tenant.IsZero() {
			r = r.WithContext(WithTenant(r.Context(), tenant))
		}
		next.ServeHTTP(w, r)
	})
}
