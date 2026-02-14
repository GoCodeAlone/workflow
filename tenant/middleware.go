package tenant

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

type contextKey string

const (
	// TenantIDKey is the context key for the tenant ID.
	TenantIDKey contextKey = "tenant_id"

	// TenantHeaderName is the default HTTP header for the tenant ID.
	TenantHeaderName = "X-Tenant-ID"
)

// TenantFromContext extracts the tenant ID from the context.
func TenantFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(TenantIDKey).(string); ok {
		return v
	}
	return ""
}

// ContextWithTenant returns a context with the tenant ID set.
func ContextWithTenant(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, TenantIDKey, tenantID)
}

// TenantIsolation is an HTTP middleware that extracts the tenant ID from the
// request header and injects it into the request context. It rejects requests
// without a valid tenant ID.
type TenantIsolation struct {
	HeaderName      string
	AllowedTenants  map[string]bool // nil means all tenants are allowed
	RequireTenantID bool
}

// NewTenantIsolation creates a new tenant isolation middleware.
func NewTenantIsolation() *TenantIsolation {
	return &TenantIsolation{
		HeaderName:      TenantHeaderName,
		RequireTenantID: true,
	}
}

// SetAllowedTenants configures the set of allowed tenant IDs.
func (t *TenantIsolation) SetAllowedTenants(tenants []string) {
	t.AllowedTenants = make(map[string]bool, len(tenants))
	for _, id := range tenants {
		t.AllowedTenants[id] = true
	}
}

// Process wraps an HTTP handler with tenant isolation.
func (t *TenantIsolation) Process(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenantID := strings.TrimSpace(r.Header.Get(t.HeaderName))

		if tenantID == "" && t.RequireTenantID {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "missing tenant ID in header " + t.HeaderName,
			})
			return
		}

		if tenantID != "" && t.AllowedTenants != nil && !t.AllowedTenants[tenantID] {
			writeJSON(w, http.StatusForbidden, map[string]string{
				"error": "tenant not allowed",
			})
			return
		}

		ctx := ContextWithTenant(r.Context(), tenantID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// QuotaEnforcer is an HTTP middleware that enforces per-tenant rate limits
// and concurrency quotas using the QuotaRegistry.
type QuotaEnforcer struct {
	Registry *QuotaRegistry
}

// NewQuotaEnforcer creates a new quota enforcer middleware.
func NewQuotaEnforcer(registry *QuotaRegistry) *QuotaEnforcer {
	return &QuotaEnforcer{Registry: registry}
}

// Process wraps an HTTP handler with quota enforcement.
func (q *QuotaEnforcer) Process(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenantID := TenantFromContext(r.Context())
		if tenantID == "" {
			// No tenant in context — pass through (tenant isolation should run first)
			next.ServeHTTP(w, r)
			return
		}

		// Check API rate limit
		if err := q.Registry.CheckAPIRate(tenantID); err != nil {
			w.Header().Set("Retry-After", "60")
			writeJSON(w, http.StatusTooManyRequests, map[string]string{
				"error": err.Error(),
			})
			return
		}

		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
