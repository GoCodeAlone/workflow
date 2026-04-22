package module

import (
	"net/http"
	"strings"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// sessionTenantIDContextKey is the context key for the tenant ID stored in a
// session by the SessionSelector or session middleware.
type sessionContextKey string

const sessionTenantIDContextKey sessionContextKey = "session_tenant_id"

// Compile-time checks that each selector implements interfaces.Selector.
var (
	_ interfaces.Selector = (*HostSelector)(nil)
	_ interfaces.Selector = (*SubdomainSelector)(nil)
	_ interfaces.Selector = (*HeaderSelector)(nil)
	_ interfaces.Selector = (*CookieSelector)(nil)
	_ interfaces.Selector = (*JWTClaimSelector)(nil)
	_ interfaces.Selector = (*SessionSelector)(nil)
	_ interfaces.Selector = (*StaticSelector)(nil)
)

// HostSelector returns the request's Host header (without port) as the tenant key.
type HostSelector struct{}

func (s *HostSelector) Match(r *http.Request) (string, bool, error) {
	host := r.Host
	if host == "" {
		return "", false, nil
	}
	// Strip port if present.
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		host = host[:idx]
	}
	if host == "" {
		return "", false, nil
	}
	return strings.ToLower(host), true, nil
}

// SubdomainSelector extracts the leftmost subdomain under a configured root domain.
// For root "example.com" and host "acme.example.com", returns "acme".
type SubdomainSelector struct {
	RootDomain string // e.g. "example.com"
}

func (s *SubdomainSelector) Match(r *http.Request) (string, bool, error) {
	host := r.Host
	// Strip port.
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		host = host[:idx]
	}
	suffix := "." + s.RootDomain
	if !strings.HasSuffix(host, suffix) {
		return "", false, nil
	}
	subdomain := strings.TrimSuffix(host, suffix)
	// Reject empty subdomain (root domain match) or multi-level subdomain.
	if subdomain == "" || strings.Contains(subdomain, ".") {
		return "", false, nil
	}
	return subdomain, true, nil
}

// HeaderSelector returns the value of a named HTTP header as the tenant key.
type HeaderSelector struct {
	Header string // e.g. "X-Tenant-ID"
}

func (s *HeaderSelector) Match(r *http.Request) (string, bool, error) {
	v := r.Header.Get(s.Header)
	if v == "" {
		return "", false, nil
	}
	return v, true, nil
}

// CookieSelector returns the value of a named cookie as the tenant key.
type CookieSelector struct {
	Cookie string // e.g. "tenant_id"
}

func (s *CookieSelector) Match(r *http.Request) (string, bool, error) {
	c, err := r.Cookie(s.Cookie)
	if err != nil {
		// http.ErrNoCookie — not an error worth surfacing.
		return "", false, nil
	}
	if c.Value == "" {
		return "", false, nil
	}
	return c.Value, true, nil
}

// JWTClaimSelector extracts a named claim from the JWT claims already stored in
// the request context by the auth middleware. Requires the auth middleware to
// have run before this selector is invoked.
type JWTClaimSelector struct {
	Claim string // e.g. "tenant_id"
}

func (s *JWTClaimSelector) Match(r *http.Request) (string, bool, error) {
	claims, ok := r.Context().Value(authClaimsContextKey).(map[string]any)
	if !ok || claims == nil {
		return "", false, nil
	}
	v, ok := claims[s.Claim]
	if !ok {
		return "", false, nil
	}
	sv, ok := v.(string)
	if !ok || sv == "" {
		return "", false, nil
	}
	return sv, true, nil
}

// SessionSelector reads a tenant ID that was previously stored in the session
// context (e.g., by a session middleware). The tenant ID is stored under the
// key sessionTenantIDContextKey.
type SessionSelector struct{}

func (s *SessionSelector) Match(r *http.Request) (string, bool, error) {
	v, ok := r.Context().Value(sessionTenantIDContextKey).(string)
	if !ok || v == "" {
		return "", false, nil
	}
	return v, true, nil
}

// StaticSelector always returns the same configured tenant ID. Useful for
// single-tenant deployments or as a fallback/default in resolver chains.
type StaticSelector struct {
	TenantID string
}

func (s *StaticSelector) Match(_ *http.Request) (string, bool, error) {
	if s.TenantID == "" {
		return "", false, nil
	}
	return s.TenantID, true, nil
}
