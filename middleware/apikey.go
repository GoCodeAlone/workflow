package middleware

import (
	"context"
	"errors"
	"net/http"

	"github.com/GoCodeAlone/workflow/store"
	"github.com/google/uuid"
)

// apiKeyContextKey is a private type for context keys to avoid collisions.
type apiKeyContextKey struct{}

// APIKeyContextKey is the context key for the authenticated API key.
var APIKeyContextKey = apiKeyContextKey{}

// TenantContext holds the tenant scoping information extracted from an API key.
type TenantContext struct {
	CompanyID   uuid.UUID
	OrgID       *uuid.UUID
	ProjectID   *uuid.UUID
	Permissions []string
	APIKeyID    uuid.UUID
}

// tenantContextKey is a private type for the tenant context key.
type tenantContextKey struct{}

// TenantContextKey is the context key for the tenant context.
var TenantContextKey = tenantContextKey{}

// TenantFromContext extracts the TenantContext from the request context, if present.
func TenantFromContext(ctx context.Context) (*TenantContext, bool) {
	tc, ok := ctx.Value(TenantContextKey).(*TenantContext)
	return tc, ok
}

// APIKeyAuth returns HTTP middleware that authenticates requests using the X-API-Key header.
//
// If the header is present, the middleware validates the key against the provided store.
// On success it sets TenantContext and the APIKey in the request context and calls next.
// On failure (invalid, expired, inactive key) it returns 401 Unauthorized.
//
// If the header is absent, the middleware calls next without modification,
// allowing downstream handlers or other auth middleware (e.g., JWT) to handle authentication.
func APIKeyAuth(apiKeyStore store.APIKeyStore, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawKey := r.Header.Get("X-API-Key")
		if rawKey == "" {
			// No API key header; fall through to other auth mechanisms.
			next.ServeHTTP(w, r)
			return
		}

		apiKey, err := apiKeyStore.Validate(r.Context(), rawKey)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				http.Error(w, "invalid API key", http.StatusUnauthorized)
				return
			}
			if errors.Is(err, store.ErrKeyExpired) {
				http.Error(w, "API key expired", http.StatusUnauthorized)
				return
			}
			if errors.Is(err, store.ErrKeyInactive) {
				http.Error(w, "API key inactive", http.StatusUnauthorized)
				return
			}
			http.Error(w, "authentication failed", http.StatusUnauthorized)
			return
		}

		// Update last used time asynchronously (best effort).
		go func() {
			_ = apiKeyStore.UpdateLastUsed(context.Background(), apiKey.ID)
		}()

		// Build tenant context from the API key scoping.
		tc := &TenantContext{
			CompanyID:   apiKey.CompanyID,
			OrgID:       apiKey.OrgID,
			ProjectID:   apiKey.ProjectID,
			Permissions: apiKey.Permissions,
			APIKeyID:    apiKey.ID,
		}

		// Set both the raw APIKey and the TenantContext in request context.
		ctx := r.Context()
		ctx = context.WithValue(ctx, APIKeyContextKey, apiKey)
		ctx = context.WithValue(ctx, TenantContextKey, tc)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
