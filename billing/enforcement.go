package billing

import (
	"context"
	"encoding/json"
	"net/http"
)

// TenantIDFunc extracts a tenant ID from an incoming HTTP request.
// The caller provides this so that enforcement is not coupled to any
// specific authentication scheme.
type TenantIDFunc func(r *http.Request) string

// EnforcementMiddleware wraps an HTTP handler and rejects requests from
// tenants that have exceeded their plan's execution limit.
type EnforcementMiddleware struct {
	meter       UsageMeter
	getTenantID TenantIDFunc
}

// NewEnforcementMiddleware creates an EnforcementMiddleware. The meter is
// queried on every request; the getTenantID function is used to extract the
// current tenant from the request context or headers.
func NewEnforcementMiddleware(meter UsageMeter, getTenantID TenantIDFunc) *EnforcementMiddleware {
	return &EnforcementMiddleware{
		meter:       meter,
		getTenantID: getTenantID,
	}
}

// Wrap returns an http.Handler that enforces the tenant's execution limit
// before delegating to next. Requests without a resolvable tenant ID pass
// through without enforcement.
func (m *EnforcementMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenantID := m.getTenantID(r)
		if tenantID == "" {
			next.ServeHTTP(w, r)
			return
		}

		allowed, remaining, err := m.meter.CheckLimit(r.Context(), tenantID)
		if err != nil {
			http.Error(w, `{"error":"billing enforcement error"}`, http.StatusInternalServerError)
			return
		}

		if !allowed {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusPaymentRequired)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":     "execution limit exceeded for current billing period",
				"remaining": remaining,
			})
			return
		}

		next.ServeHTTP(w, r)
	})
}

// CheckLimit is a convenience wrapper for non-HTTP enforcement paths (e.g. gRPC
// handlers or internal pipeline runners). It returns an error when the tenant
// has exceeded their plan limit.
func CheckLimit(ctx context.Context, meter UsageMeter, tenantID string) error {
	allowed, _, err := meter.CheckLimit(ctx, tenantID)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrLimitExceeded
	}
	return nil
}

// ErrLimitExceeded is returned by CheckLimit when a tenant has exhausted
// their plan's execution quota for the current billing period.
var ErrLimitExceeded = billingError("execution limit exceeded for current billing period")

type billingError string

func (e billingError) Error() string { return string(e) }
