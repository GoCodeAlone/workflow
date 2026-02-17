// Package middleware provides HTTP middleware for the platform abstraction layer.
package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/GoCodeAlone/workflow/platform"
)

// contextKey is an unexported type for context keys in this package.
type contextKey string

const (
	// RoleContextKey is the context key for the TierRole extracted from the request.
	RoleContextKey contextKey = "platform_role"
)

// TierBoundaryMiddleware enforces tier-based access control on HTTP requests.
// It extracts the role from the request context and the target tier from
// the URL path, then delegates authorization to a TierAuthorizer.
type TierBoundaryMiddleware struct {
	authorizer platform.TierAuthorizer
}

// NewTierBoundaryMiddleware creates a TierBoundaryMiddleware backed by the
// given TierAuthorizer.
func NewTierBoundaryMiddleware(authorizer platform.TierAuthorizer) *TierBoundaryMiddleware {
	return &TierBoundaryMiddleware{authorizer: authorizer}
}

// Wrap returns an http.Handler that performs tier boundary checks before
// delegating to next. If the request lacks a role in its context, a 401
// response is returned. If authorization fails, a 403 response with a
// structured JSON error body is returned.
func (m *TierBoundaryMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role, ok := RoleFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "missing role in request context")
			return
		}

		tier, err := TierFromPath(r.URL.Path)
		if err != nil {
			// Path does not contain a tier segment; pass through.
			next.ServeHTTP(w, r)
			return
		}

		operation := operationFromMethod(r.Method)

		if authErr := m.authorizer.Authorize(r.Context(), role, tier, operation); authErr != nil {
			writeJSONError(w, http.StatusForbidden, authErr.Error())
			return
		}

		next.ServeHTTP(w, r)
	})
}

// TierFromPath extracts a Tier from a URL path. It looks for segments
// matching "tier1", "tier2", or "tier3" in paths like
// "/api/v1/platform/tier1/resources".
func TierFromPath(path string) (platform.Tier, error) {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	for _, seg := range segments {
		lower := strings.ToLower(seg)
		switch lower {
		case "tier1", "infrastructure":
			return platform.TierInfrastructure, nil
		case "tier2", "shared_primitive", "shared-primitive":
			return platform.TierSharedPrimitive, nil
		case "tier3", "application":
			return platform.TierApplication, nil
		}
	}
	return 0, &platform.TierBoundaryError{
		Operation: "parse_path",
		Reason:    "no tier segment found in path: " + path,
	}
}

// RoleFromContext extracts the TierRole from the request context.
func RoleFromContext(ctx context.Context) (platform.TierRole, bool) {
	role, ok := ctx.Value(RoleContextKey).(platform.TierRole)
	return role, ok
}

// WithRole returns a new context with the given TierRole set.
func WithRole(ctx context.Context, role platform.TierRole) context.Context {
	return context.WithValue(ctx, RoleContextKey, role)
}

// operationFromMethod maps HTTP methods to platform operation names.
func operationFromMethod(method string) string {
	switch strings.ToUpper(method) {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return "read"
	case http.MethodPost:
		return "create"
	case http.MethodPut, http.MethodPatch:
		return "update"
	case http.MethodDelete:
		return "delete"
	default:
		return "read"
	}
}

// errorResponse is the JSON structure returned for authorization failures.
type errorResponse struct {
	Error   string `json:"error"`
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// writeJSONError writes a structured JSON error response.
func writeJSONError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(errorResponse{
		Error:   http.StatusText(code),
		Code:    code,
		Message: message,
	})
}
