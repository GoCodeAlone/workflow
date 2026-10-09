package module

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func newManagedIssuer(t *testing.T) *M2MAuthModule {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	m := NewM2MAuthModule("managed-agent", "", time.Hour, "https://issuer.example.test")
	if err := m.SetECDSAKey(string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}))); err != nil {
		t.Fatal(err)
	}
	m.SetManagedOnly(true)
	return m
}

func managedRequest(now time.Time) ManagedTokenRequest {
	return ManagedTokenRequest{
		Subject: "agent:review-editor", Audience: "https://admin.example.test/api/agent",
		GrantID: "grant-1", GrantVersion: 3, TenantID: 234,
		Scopes:   []string{"pages:read", "pages:update"},
		IssuedAt: now.UTC().Truncate(time.Second), ExpiresAt: now.UTC().Truncate(time.Second).Add(5 * time.Minute),
	}
}

func TestM2MManagedProtectedClaimsAndSignature(t *testing.T) {
	m := newManagedIssuer(t)
	// Legacy client claims and expiry must never enter managed issuance.
	m.RegisterClient(M2MClient{ClientID: "agent:review-editor", Claims: map[string]any{
		"iss": "forged", "aud": "forged", "token_use": "owner", "grant_id": "forged", "grant_version": 999, "tenant_id": 999, "scope": "*", "role": "superadmin",
	}})
	r := managedRequest(time.Now())
	signed, err := m.IssueManagedToken(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	token, err := jwt.Parse(signed, func(*jwt.Token) (any, error) { return m.publicKey, nil }, jwt.WithValidMethods([]string{"ES256"}), jwt.WithIssuer(m.issuer), jwt.WithAudience(r.Audience), jwt.WithExpirationRequired(), jwt.WithIssuedAt())
	if err != nil || !token.Valid {
		t.Fatal("managed signature or registered claims refused")
	}
	c := token.Claims.(jwt.MapClaims)
	for name, want := range map[string]any{
		"iss": m.issuer, "sub": r.Subject, "aud": r.Audience, "token_use": "agent_access", "grant_id": r.GrantID,
		"grant_version": float64(r.GrantVersion), "tenant_id": float64(r.TenantID), "scope": "pages:read pages:update",
		"iat": float64(r.IssuedAt.Unix()), "exp": float64(r.ExpiresAt.Unix()),
	} {
		if c[name] != want {
			t.Fatalf("protected claim mismatch: %s", name)
		}
	}
	if c["role"] != nil || len(c) != 11 || c["jti"] == "" || token.Header["kid"] != m.name+"-key" {
		t.Fatal("unexpected claim inheritance or missing token identity")
	}
	second, err := m.IssueManagedToken(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	secondClaims := jwt.MapClaims{}
	if _, _, err := jwt.NewParser().ParseUnverified(second, secondClaims); err != nil || secondClaims["jti"] == c["jti"] {
		t.Fatal("managed token IDs reused")
	}
	if _, err := jwt.Parse(signed, func(*jwt.Token) (any, error) { return m.publicKey, nil }, jwt.WithAudience("https://preview.example.test")); err == nil {
		t.Fatal("token accepted for a distinct preview audience")
	}
}

func TestM2MManagedInvalidRequests(t *testing.T) {
	m := newManagedIssuer(t)
	changes := map[string]func(*ManagedTokenRequest){
		"empty subject":      func(r *ManagedTokenRequest) { r.Subject = "" },
		"spaced subject":     func(r *ManagedTokenRequest) { r.Subject = " secret subject " },
		"control subject":    func(r *ManagedTokenRequest) { r.Subject = "agent\nidentity" },
		"long subject":       func(r *ManagedTokenRequest) { r.Subject = strings.Repeat("a", 257) },
		"empty grant":        func(r *ManagedTokenRequest) { r.GrantID = "" },
		"long grant":         func(r *ManagedTokenRequest) { r.GrantID = strings.Repeat("a", 257) },
		"control grant":      func(r *ManagedTokenRequest) { r.GrantID = "grant\x00identity" },
		"wildcard grant":     func(r *ManagedTokenRequest) { r.GrantID = "*" },
		"zero version":       func(r *ManagedTokenRequest) { r.GrantVersion = 0 },
		"negative version":   func(r *ManagedTokenRequest) { r.GrantVersion = -1 },
		"zero tenant":        func(r *ManagedTokenRequest) { r.TenantID = 0 },
		"negative tenant":    func(r *ManagedTokenRequest) { r.TenantID = -1 },
		"no audience":        func(r *ManagedTokenRequest) { r.Audience = "" },
		"opaque audience":    func(r *ManagedTokenRequest) { r.Audience = "admin-agent" },
		"http audience":      func(r *ManagedTokenRequest) { r.Audience = "http://admin.example.test" },
		"wildcard audience":  func(r *ManagedTokenRequest) { r.Audience = "https://*.example.test" },
		"userinfo audience":  func(r *ManagedTokenRequest) { r.Audience = "https://secret@admin.example.test" },
		"query audience":     func(r *ManagedTokenRequest) { r.Audience += "?key=secret" },
		"fragment audience":  func(r *ManagedTokenRequest) { r.Audience += "#secret" },
		"host case alias":    func(r *ManagedTokenRequest) { r.Audience = "https://ADMIN.example.test" },
		"encoded path alias": func(r *ManagedTokenRequest) { r.Audience = "https://admin.example.test/%61pi" },
		"dot path alias":     func(r *ManagedTokenRequest) { r.Audience = "https://admin.example.test/api/../agent" },
		"empty issued":       func(r *ManagedTokenRequest) { r.IssuedAt = time.Time{} },
		"empty expiry":       func(r *ManagedTokenRequest) { r.ExpiresAt = time.Time{} },
		"future issued":      func(r *ManagedTokenRequest) { r.IssuedAt = r.IssuedAt.Add(time.Minute) },
		"old issued":         func(r *ManagedTokenRequest) { r.IssuedAt = r.IssuedAt.Add(-time.Minute) },
		"expired":            func(r *ManagedTokenRequest) { r.ExpiresAt = r.IssuedAt.Add(-time.Minute) },
		"equal expiry":       func(r *ManagedTokenRequest) { r.ExpiresAt = r.IssuedAt },
		"over lifetime":      func(r *ManagedTokenRequest) { r.ExpiresAt = r.IssuedAt.Add(MaxManagedTokenLifetime + time.Second) },
		"fractional issued":  func(r *ManagedTokenRequest) { r.IssuedAt = r.IssuedAt.Add(time.Nanosecond) },
		"fractional expiry":  func(r *ManagedTokenRequest) { r.ExpiresAt = r.ExpiresAt.Add(time.Nanosecond) },
		"non UTC issued":     func(r *ManagedTokenRequest) { r.IssuedAt = r.IssuedAt.In(time.FixedZone("offset", 3600)) },
		"non UTC expiry":     func(r *ManagedTokenRequest) { r.ExpiresAt = r.ExpiresAt.In(time.FixedZone("offset", 3600)) },
		"no scopes":          func(r *ManagedTokenRequest) { r.Scopes = nil },
		"too many scopes": func(r *ManagedTokenRequest) {
			r.Scopes = nil
			for i := 0; i < 65; i++ {
				r.Scopes = append(r.Scopes, "pages:"+strings.Repeat("x", i))
			}
		},
		"empty scope":     func(r *ManagedTokenRequest) { r.Scopes = []string{""} },
		"spaced scope":    func(r *ManagedTokenRequest) { r.Scopes = []string{"pages:read pages:update"} },
		"wildcard scope":  func(r *ManagedTokenRequest) { r.Scopes = []string{"pages:*"} },
		"duplicate scope": func(r *ManagedTokenRequest) { r.Scopes = []string{"pages:read", "pages:read"} },
		"control scope":   func(r *ManagedTokenRequest) { r.Scopes = []string{"pages:\nread"} },
		"quoted scope":    func(r *ManagedTokenRequest) { r.Scopes = []string{"pages:\"read"} },
		"escape scope":    func(r *ManagedTokenRequest) { r.Scopes = []string{"pages:\\read"} },
		"long scope":      func(r *ManagedTokenRequest) { r.Scopes = []string{strings.Repeat("a", 257)} },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			r := managedRequest(time.Now())
			change(&r)
			got, err := m.IssueManagedToken(context.Background(), r)
			if got != "" || !errors.Is(err, ErrManagedTokenRequest) || err.Error() != "invalid managed token request" {
				t.Fatal("invalid managed request not refused with fixed error")
			}
		})
	}
}

func TestM2MManagedClockBounds(t *testing.T) {
	now := time.Date(2030, 1, 1, 12, 0, 0, 0, time.UTC)
	r := managedRequest(now)
	r.ExpiresAt = r.IssuedAt.Add(MaxManagedTokenLifetime)
	if !validManagedTokenRequest(r, now) {
		t.Fatal("maximum approved lifetime refused")
	}
	r.IssuedAt = now.Add(-30 * time.Second)
	r.ExpiresAt = r.IssuedAt.Add(MaxManagedTokenLifetime)
	if !validManagedTokenRequest(r, now) {
		t.Fatal("clock age boundary refused")
	}
	r.IssuedAt = now.Add(-30*time.Second - time.Nanosecond)
	if validManagedTokenRequest(r, now) {
		t.Fatal("old clock accepted")
	}
	r = managedRequest(now)
	r.ExpiresAt = now
	if validManagedTokenRequest(r, now) {
		t.Fatal("already expired token accepted")
	}
}

func TestM2MManagedConfigurationRefusals(t *testing.T) {
	changes := map[string]func(*M2MAuthModule){
		"legacy mode":       func(m *M2MAuthModule) { m.SetManagedOnly(false) },
		"HS256":             func(m *M2MAuthModule) { m.algorithm = SigningAlgHS256 },
		"unknown algorithm": func(m *M2MAuthModule) { m.algorithm = "RS256" },
		"no private key":    func(m *M2MAuthModule) { m.privateKey = nil },
		"no public key":     func(m *M2MAuthModule) { m.publicKey = nil },
		"unconfigured key":  func(m *M2MAuthModule) { m.ecdsaKeyConfigured = false },
		"generated replacement": func(m *M2MAuthModule) {
			if err := m.GenerateECDSAKey(); err != nil {
				t.Fatal(err)
			}
		},
		"wrong curve":          func(m *M2MAuthModule) { m.privateKey.Curve = elliptic.P384() },
		"different public key": func(m *M2MAuthModule) { m.publicKey = newManagedIssuer(t).publicKey },
		"invalid issuer":       func(m *M2MAuthModule) { m.issuer = "secret issuer" },
		"http issuer":          func(m *M2MAuthModule) { m.issuer = "http://issuer.example.test" },
		"pending config error": func(m *M2MAuthModule) { m.SetInitErr(errors.New("private secret detail")) },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			m := newManagedIssuer(t)
			change(m)
			got, err := m.IssueManagedToken(context.Background(), managedRequest(time.Now()))
			if got != "" || !errors.Is(err, ErrManagedTokenConfiguration) || err.Error() != "managed token issuer unavailable" {
				t.Fatal("invalid issuer not refused with fixed error")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := newManagedIssuer(t).IssueManagedToken(ctx, managedRequest(time.Now())); got != "" || !errors.Is(err, context.Canceled) {
		t.Fatal("canceled issuance accepted")
	}
}

type managedUnreadBody struct{ reads int }

func (b *managedUnreadBody) Read([]byte) (int, error) { b.reads++; return 0, io.EOF }
func (b *managedUnreadBody) Close() error             { return nil }

func TestM2MManagedHTTPHandlersDenied(t *testing.T) {
	for _, custom := range []bool{false, true} {
		m := newManagedIssuer(t)
		if custom {
			if err := m.SetEndpoints(M2MEndpointPaths{Token: "/agent/token", Revoke: "/agent/revoke", Introspect: "/agent/introspect", JWKS: "/agent/jwks"}); err != nil {
				t.Fatal(err)
			}
		}
		ep := m.endpointPaths
		for _, target := range []string{ep.Token, ep.Revoke, ep.Introspect} {
			body := &managedUnreadBody{}
			w := httptest.NewRecorder()
			m.Handle(w, httptest.NewRequest(http.MethodPost, "/api/v1"+target, body))
			if w.Code != 403 || w.Header().Get("Cache-Control") != "no-store" || body.reads != 0 || strings.Contains(w.Body.String(), "access_token") {
				t.Fatal("managed handler read input or permitted generic operation")
			}
		}
		for _, handler := range []func(http.ResponseWriter, *http.Request){m.handleToken, m.handleRevoke, m.handleIntrospect} {
			body := &managedUnreadBody{}
			w := httptest.NewRecorder()
			handler(w, httptest.NewRequest(http.MethodPost, ep.Token, body))
			if w.Code != 403 || body.reads != 0 {
				t.Fatal("inner managed handler bypassed denial")
			}
		}
		w := httptest.NewRecorder()
		m.Handle(w, httptest.NewRequest(http.MethodGet, "/api/v1"+ep.JWKS, nil))
		var keys struct {
			Keys []map[string]any `json:"keys"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &keys) != nil || len(keys.Keys) != 1 || keys.Keys[0]["kid"] != m.name+"-key" {
			t.Fatal("managed JWKS unavailable or key ID mismatched")
		}
	}
}

func TestM2MManagedLegacyDefaultUnchanged(t *testing.T) {
	m := NewM2MAuthModule("legacy", strings.Repeat("x", 32), 2*time.Hour, "legacy-issuer")
	m.RegisterClient(M2MClient{ClientID: "client", ClientSecret: "synthetic-secret", Scopes: []string{"read"}})
	values := url.Values{"grant_type": {GrantTypeClientCredentials}, "client_id": {"client"}, "client_secret": {"synthetic-secret"}}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	m.Handle(w, r)
	var response struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &response) != nil || response.AccessToken == "" || response.ExpiresIn != 7200 {
		t.Fatal("legacy issuance or expiry changed")
	}
	ok, claims, err := m.Authenticate(response.AccessToken)
	if !ok || err != nil || claims["iss"] != "legacy-issuer" || claims["token_use"] != nil {
		t.Fatal("legacy claims or validation changed")
	}
	if _, err := m.IssueManagedToken(context.Background(), managedRequest(time.Now())); !errors.Is(err, ErrManagedTokenConfiguration) {
		t.Fatal("legacy issuer accepted managed issuance")
	}
}
