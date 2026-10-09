package module

import (
	"context"
	"crypto/elliptic"
	"errors"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// MaxManagedTokenLifetime is the maximum approved agent access-token lifetime.
const MaxManagedTokenLifetime = 15 * time.Minute

var (
	ErrManagedTokenConfiguration = errors.New("managed token issuer unavailable")
	ErrManagedTokenRequest       = errors.New("invalid managed token request")
)

// ManagedTokenRequest carries a grant already approved by the host. This API
// signs it; it does not authenticate a caller, authorize scopes or query grants.
// The host must verify current grant version, tenant, expiry, revocation and
// approved scopes before calling and enforce them again on each resource use.
// Times must be UTC whole seconds; IssuedAt must be no more than 30 seconds old.
type ManagedTokenRequest struct {
	Subject      string
	Audience     string
	GrantID      string
	GrantVersion int64
	TenantID     int64
	Scopes       []string
	IssuedAt     time.Time
	ExpiresAt    time.Time
}

// SetManagedOnly disables the generic token, revocation and introspection HTTP
// handlers on this issuer. JWKS remains readable. The default is false for
// compatibility. Configure a dedicated instance before serving requests; do
// not share its signing key with a legacy issuer or re-enable generic handlers.
func (m *M2MAuthModule) SetManagedOnly(managedOnly bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.managedOnly = managedOnly
}

func (m *M2MAuthModule) refuseManagedEndpoint(w http.ResponseWriter) bool {
	m.mu.RLock()
	managedOnly := m.managedOnly
	m.mu.RUnlock()
	if !managedOnly {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte("{\"error\":\"access_denied\"}\n"))
	return true
}

// IssueManagedToken signs a fixed set of protected claims with the dedicated
// issuer's explicitly configured ES256 key. It does not expose arbitrary claims
// or inherit legacy client claims, grants, expiry or revocation behavior.
func (m *M2MAuthModule) IssueManagedToken(ctx context.Context, request ManagedTokenRequest) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	m.mu.RLock()
	key, publicKey := m.privateKey, m.publicKey
	issuer, keyID := m.issuer, m.name+"-key"
	configured := m.managedOnly && m.ecdsaKeyConfigured && m.algorithm == SigningAlgES256 && m.initErr == nil
	m.mu.RUnlock()
	if !configured || key == nil || publicKey == nil || key.Curve != elliptic.P256() || publicKey.Curve != elliptic.P256() || !key.PublicKey.Equal(publicKey) || !managedHTTPSURL(issuer) {
		return "", ErrManagedTokenConfiguration
	}
	now := time.Now().UTC()
	if !validManagedTokenRequest(request, now) {
		return "", ErrManagedTokenRequest
	}
	jti, err := generateJTI()
	if err != nil {
		return "", ErrManagedTokenConfiguration
	}
	claims := jwt.MapClaims{
		"iss":           issuer,
		"sub":           request.Subject,
		"aud":           request.Audience,
		"iat":           request.IssuedAt.Unix(),
		"exp":           request.ExpiresAt.Unix(),
		"jti":           jti,
		"token_use":     "agent_access",
		"grant_id":      request.GrantID,
		"grant_version": request.GrantVersion,
		"tenant_id":     request.TenantID,
		"scope":         strings.Join(request.Scopes, " "),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	token.Header["kid"] = keyID
	signed, err := token.SignedString(key)
	if err != nil {
		return "", ErrManagedTokenConfiguration
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return signed, nil
}

func validManagedTokenRequest(r ManagedTokenRequest, now time.Time) bool {
	if !managedIdentifier(r.Subject) || !managedIdentifier(r.GrantID) || !managedHTTPSURL(r.Audience) || r.GrantVersion <= 0 || r.TenantID <= 0 {
		return false
	}
	if r.IssuedAt.IsZero() || r.ExpiresAt.IsZero() || r.IssuedAt.Location() != time.UTC || r.ExpiresAt.Location() != time.UTC || r.IssuedAt.Nanosecond() != 0 || r.ExpiresAt.Nanosecond() != 0 || r.IssuedAt.After(now) || r.IssuedAt.Before(now.Add(-30*time.Second)) || !r.ExpiresAt.After(now) || !r.ExpiresAt.After(r.IssuedAt) || r.ExpiresAt.Sub(r.IssuedAt) > MaxManagedTokenLifetime {
		return false
	}
	if len(r.Scopes) == 0 || len(r.Scopes) > 64 {
		return false
	}
	seen := make(map[string]bool, len(r.Scopes))
	for _, scope := range r.Scopes {
		if len(scope) == 0 || len(scope) > 256 || seen[scope] || strings.Contains(scope, "*") {
			return false
		}
		// RFC 6749 scope-token excludes spaces, controls, quotes and backslashes.
		// Each scope is already approved by the host.
		for _, c := range []byte(scope) {
			if c < 0x21 || c > 0x7e || c == '"' || c == '\\' {
				return false
			}
		}
		seen[scope] = true
	}
	return true
}

func managedIdentifier(v string) bool {
	if len(v) == 0 || len(v) > 256 {
		return false
	}
	for _, c := range []byte(v) {
		if c < 0x21 || c > 0x7e || c == '*' {
			return false
		}
	}
	return true
}

func managedHTTPSURL(v string) bool {
	if len(v) == 0 || len(v) > 2048 || strings.ContainsAny(v, "*\\") {
		return false
	}
	u, err := url.Parse(v)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.Host == strings.ToLower(u.Host) && u.User == nil && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && u.RawPath == "" && u.EscapedPath() == u.Path && u.String() == v && (u.Path == "" || path.Clean(u.Path) == u.Path)
}
