package iam

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// testKeyPair holds an RSA key pair and its JWKS kid for tests.
type testKeyPair struct {
	private *rsa.PrivateKey
	kid     string
}

func newTestKeyPair(t *testing.T, kid string) *testKeyPair {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	return &testKeyPair{private: priv, kid: kid}
}

// makeJWKS returns a serialised JWKS document containing the public key.
func (kp *testKeyPair) makeJWKS(t *testing.T) []byte {
	t.Helper()
	pub := &kp.private.PublicKey
	nBytes := pub.N.Bytes()
	eBytes := big.NewInt(int64(pub.E)).Bytes()
	doc := jwksDocument{
		Keys: []jwkKey{
			{
				Kty: "RSA",
				Kid: kp.kid,
				N:   base64.RawURLEncoding.EncodeToString(nBytes),
				E:   base64.RawURLEncoding.EncodeToString(eBytes),
				Alg: "RS256",
				Use: "sig",
			},
		},
	}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal JWKS: %v", err)
	}
	return b
}

// signToken signs a JWT with the given claims using RS256.
func (kp *testKeyPair) signToken(t *testing.T, issuer, audience, subject string, expiry time.Time) string {
	t.Helper()
	claims := jwt.MapClaims{
		"iss": issuer,
		"aud": []string{audience},
		"sub": subject,
		"exp": expiry.Unix(),
		"iat": time.Now().Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kp.kid
	signed, err := tok.SignedString(kp.private)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

// oidcTestServer starts an httptest server that serves OIDC discovery and JWKS.
type oidcTestServer struct {
	server    *httptest.Server
	jwksBytes []byte
}

func newOIDCTestServer(t *testing.T, kp *testKeyPair) *oidcTestServer {
	t.Helper()
	ts := &oidcTestServer{jwksBytes: kp.makeJWKS(t)}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		issuer := "http://" + r.Host
		doc := discoveryDoc{
			Issuer:  issuer,
			JWKSURI: issuer + "/jwks",
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(doc)
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(ts.jwksBytes)
	})

	ts.server = httptest.NewServer(mux)
	t.Cleanup(ts.server.Close)
	return ts
}

func (ts *oidcTestServer) issuer() string { return ts.server.URL }

// newOIDCProviderForTest returns an OIDCProvider wired to the test server's HTTP client.
func newOIDCProviderForTest(ts *oidcTestServer) *OIDCProvider {
	return &OIDCProvider{HTTPClient: ts.server.Client()}
}

// --- TestConnection ---

func TestOIDCProvider_TestConnection_Success(t *testing.T) {
	kp := newTestKeyPair(t, "key1")
	ts := newOIDCTestServer(t, kp)
	p := newOIDCProviderForTest(ts)

	cfg, _ := json.Marshal(OIDCConfig{Issuer: ts.issuer(), ClientID: "client"})
	if err := p.TestConnection(context.Background(), cfg); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestOIDCProvider_TestConnection_InvalidDiscovery(t *testing.T) {
	// Server that returns 404 for discovery.
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	p := &OIDCProvider{HTTPClient: srv.Client()}

	cfg, _ := json.Marshal(OIDCConfig{Issuer: srv.URL, ClientID: "client"})
	if err := p.TestConnection(context.Background(), cfg); err == nil {
		t.Fatal("expected error for unreachable discovery endpoint")
	}
}

func TestOIDCProvider_TestConnection_InvalidConfig(t *testing.T) {
	p := &OIDCProvider{}
	cfg := json.RawMessage(`{"issuer":""}`)
	if err := p.TestConnection(context.Background(), cfg); err == nil {
		t.Fatal("expected error for missing issuer")
	}
}

// --- ResolveIdentities with JWT ---

func TestOIDCProvider_ResolveIdentities_ValidToken(t *testing.T) {
	kp := newTestKeyPair(t, "key1")
	ts := newOIDCTestServer(t, kp)
	p := newOIDCProviderForTest(ts)

	issuer := ts.issuer()
	token := kp.signToken(t, issuer, "my-client", "user-42", time.Now().Add(time.Hour))

	cfg, _ := json.Marshal(OIDCConfig{Issuer: issuer, ClientID: "my-client"})
	creds := map[string]string{"id_token": token}

	ids, err := p.ResolveIdentities(context.Background(), cfg, creds)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("expected 1 identity, got %d", len(ids))
	}
	if ids[0].Identifier != "user-42" {
		t.Errorf("expected identifier 'user-42', got %s", ids[0].Identifier)
	}
}

func TestOIDCProvider_ResolveIdentities_CustomClaim_Token(t *testing.T) {
	kp := newTestKeyPair(t, "key1")
	ts := newOIDCTestServer(t, kp)
	p := newOIDCProviderForTest(ts)

	issuer := ts.issuer()
	// Sign a token that includes an "email" claim.
	claims := jwt.MapClaims{
		"iss":   issuer,
		"aud":   []string{"my-client"},
		"sub":   "user-42",
		"email": "user@example.com",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"iat":   time.Now().Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kp.kid
	token, err := tok.SignedString(kp.private)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	cfg, _ := json.Marshal(OIDCConfig{Issuer: issuer, ClientID: "my-client", ClaimKey: "email"})
	creds := map[string]string{"id_token": token}

	ids, err := p.ResolveIdentities(context.Background(), cfg, creds)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if ids[0].Identifier != "user@example.com" {
		t.Errorf("expected 'user@example.com', got %s", ids[0].Identifier)
	}
}

func TestOIDCProvider_ResolveIdentities_ExpiredToken(t *testing.T) {
	kp := newTestKeyPair(t, "key1")
	ts := newOIDCTestServer(t, kp)
	p := newOIDCProviderForTest(ts)

	issuer := ts.issuer()
	token := kp.signToken(t, issuer, "my-client", "user-42", time.Now().Add(-time.Hour))

	cfg, _ := json.Marshal(OIDCConfig{Issuer: issuer, ClientID: "my-client"})
	creds := map[string]string{"id_token": token}

	if _, err := p.ResolveIdentities(context.Background(), cfg, creds); err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestOIDCProvider_ResolveIdentities_WrongAudience(t *testing.T) {
	kp := newTestKeyPair(t, "key1")
	ts := newOIDCTestServer(t, kp)
	p := newOIDCProviderForTest(ts)

	issuer := ts.issuer()
	token := kp.signToken(t, issuer, "wrong-client", "user-42", time.Now().Add(time.Hour))

	cfg, _ := json.Marshal(OIDCConfig{Issuer: issuer, ClientID: "my-client"})
	creds := map[string]string{"id_token": token}

	if _, err := p.ResolveIdentities(context.Background(), cfg, creds); err == nil {
		t.Fatal("expected error for wrong audience")
	}
}

func TestOIDCProvider_ResolveIdentities_WrongIssuer(t *testing.T) {
	kp := newTestKeyPair(t, "key1")
	ts := newOIDCTestServer(t, kp)
	p := newOIDCProviderForTest(ts)

	issuer := ts.issuer()
	token := kp.signToken(t, issuer, "my-client", "user-42", time.Now().Add(time.Hour))

	// Configure with a different issuer.
	cfg, _ := json.Marshal(OIDCConfig{
		Issuer:   "https://wrong-issuer.example.com",
		ClientID: "my-client",
		JWKSURL:  issuer + "/jwks",
	})
	creds := map[string]string{"id_token": token}

	if _, err := p.ResolveIdentities(context.Background(), cfg, creds); err == nil {
		t.Fatal("expected error for wrong issuer")
	}
}

func TestOIDCProvider_ResolveIdentities_InvalidSignature(t *testing.T) {
	kp := newTestKeyPair(t, "key1")
	ts := newOIDCTestServer(t, kp)
	p := newOIDCProviderForTest(ts)

	issuer := ts.issuer()
	// Sign with a different key.
	otherKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	claims := jwt.MapClaims{
		"iss": issuer,
		"aud": []string{"my-client"},
		"sub": "user-42",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kp.kid // claim to be the known kid but signed with different key
	token, _ := tok.SignedString(otherKey)

	cfg, _ := json.Marshal(OIDCConfig{Issuer: issuer, ClientID: "my-client"})
	creds := map[string]string{"id_token": token}

	if _, err := p.ResolveIdentities(context.Background(), cfg, creds); err == nil {
		t.Fatal("expected error for invalid signature")
	}
}

// TestOIDCProvider_ResolveIdentities_KeyRotation tests that a new key (different kid)
// is accepted after the JWKS is refreshed.
func TestOIDCProvider_ResolveIdentities_KeyRotation(t *testing.T) {
	kp1 := newTestKeyPair(t, "key1")
	kp2 := newTestKeyPair(t, "key2")

	// Start server initially serving kp1's JWKS.
	var jwksBytes []byte
	jwksBytes = kp1.makeJWKS(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		issuer := "http://" + r.Host
		doc := discoveryDoc{Issuer: issuer, JWKSURI: issuer + "/jwks"}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(doc)
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jwksBytes)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := &OIDCProvider{HTTPClient: srv.Client()}
	issuer := srv.URL

	// First request with key1 succeeds.
	token1 := kp1.signToken(t, issuer, "client", "user-1", time.Now().Add(time.Hour))
	cfg, _ := json.Marshal(OIDCConfig{Issuer: issuer, ClientID: "client"})
	if _, err := p.ResolveIdentities(context.Background(), cfg, map[string]string{"id_token": token1}); err != nil {
		t.Fatalf("expected key1 token to succeed: %v", err)
	}

	// Rotate to key2 on the server.
	jwksBytes = kp2.makeJWKS(t)

	// Token signed with key2 should succeed (triggers JWKS re-fetch on unknown kid).
	token2 := kp2.signToken(t, issuer, "client", "user-2", time.Now().Add(time.Hour))
	ids, err := p.ResolveIdentities(context.Background(), cfg, map[string]string{"id_token": token2})
	if err != nil {
		t.Fatalf("expected key2 token to succeed after rotation: %v", err)
	}
	if ids[0].Identifier != "user-2" {
		t.Errorf("expected 'user-2', got %s", ids[0].Identifier)
	}
}

// --- Fallback (no id_token) – existing behaviour ---

func TestOIDCProvider_ResolveIdentities_NoToken_FallbackSub(t *testing.T) {
	p := &OIDCProvider{}
	cfg, _ := json.Marshal(OIDCConfig{Issuer: "https://example.com", ClientID: "client"})
	creds := map[string]string{"sub": "user-123"}

	ids, err := p.ResolveIdentities(context.Background(), cfg, creds)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if ids[0].Identifier != "user-123" {
		t.Errorf("expected 'user-123', got %s", ids[0].Identifier)
	}
}
