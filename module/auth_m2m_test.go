package module

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	_ "modernc.org/sqlite"
)

// --- helpers ---

// newM2MHS256 creates an M2MAuthModule configured with HS256 and a test client.
func newM2MHS256(t *testing.T) *M2MAuthModule {
	t.Helper()
	m := NewM2MAuthModule("m2m", "this-is-a-valid-secret-32-bytes!", time.Hour, "test-issuer")
	m.RegisterClient(M2MClient{
		ClientID:     "test-client",
		ClientSecret: "test-secret", //nolint:gosec // test credential
		Scopes:       []string{"read", "write"},
	})
	return m
}

// newM2MES256 creates an M2MAuthModule configured with ES256 and a test client.
func newM2MES256(t *testing.T) *M2MAuthModule {
	t.Helper()
	m := NewM2MAuthModule("m2m", "", time.Hour, "test-issuer")
	if err := m.GenerateECDSAKey(); err != nil {
		t.Fatalf("GenerateECDSAKey: %v", err)
	}
	m.RegisterClient(M2MClient{
		ClientID:     "es256-client",
		ClientSecret: "es256-secret", //nolint:gosec // test credential
		Scopes:       []string{"api"},
	})
	return m
}

// postToken is a test helper that sends a form-encoded POST to /oauth/token.
func postToken(t *testing.T, m *M2MAuthModule, params url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/oauth/token",
		strings.NewReader(params.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	m.Handle(w, req)
	return w
}

// --- Name / Init ---

func TestM2M_Name(t *testing.T) {
	m := NewM2MAuthModule("my-m2m", "secret-must-be-at-least-32bytes!!", time.Hour, "")
	if m.Name() != "my-m2m" {
		t.Errorf("expected 'my-m2m', got %q", m.Name())
	}
}

func TestM2M_InitHS256_ValidSecret(t *testing.T) {
	m := NewM2MAuthModule("m2m", "this-is-a-valid-secret-32-bytes!", time.Hour, "issuer")
	if err := m.Init(nil); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestM2M_InitHS256_ShortSecret(t *testing.T) {
	m := NewM2MAuthModule("m2m", "short", time.Hour, "issuer")
	if err := m.Init(nil); err == nil {
		t.Error("expected error for short HMAC secret")
	}
}

func TestM2M_InitES256_NoKey(t *testing.T) {
	m := &M2MAuthModule{
		name:      "m2m",
		algorithm: SigningAlgES256,
		issuer:    "issuer",
	}
	if err := m.Init(nil); err == nil {
		t.Error("expected error when ES256 key not set")
	}
}

func TestM2M_InitES256_WithKey(t *testing.T) {
	m := newM2MES256(t)
	if err := m.Init(nil); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestM2M_ProvidesServices(t *testing.T) {
	m := newM2MHS256(t)
	svcs := m.ProvidesServices()
	if len(svcs) != 1 {
		t.Fatalf("expected 1 service, got %d", len(svcs))
	}
	if svcs[0].Name != "m2m" {
		t.Errorf("expected service name 'm2m', got %q", svcs[0].Name)
	}
}

// --- client_credentials grant ---

func TestM2M_ClientCredentials_FormParams(t *testing.T) {
	m := newM2MHS256(t)

	params := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {"test-client"},
		"client_secret": {"test-secret"},
	}
	w := postToken(t, m, params)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["access_token"] == nil || resp["access_token"] == "" {
		t.Error("expected non-empty access_token")
	}
	if resp["token_type"] != "Bearer" {
		t.Errorf("expected token_type=Bearer, got %v", resp["token_type"])
	}
	if resp["expires_in"] == nil {
		t.Error("expected expires_in in response")
	}
}

func TestM2M_ClientCredentials_BasicAuth(t *testing.T) {
	m := newM2MHS256(t)

	req := httptest.NewRequest(http.MethodPost, "/oauth/token",
		strings.NewReader("grant_type=client_credentials"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("test-client", "test-secret")
	w := httptest.NewRecorder()
	m.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["access_token"] == nil {
		t.Error("expected access_token in response")
	}
}

func TestM2M_ClientCredentials_WrongSecret(t *testing.T) {
	m := newM2MHS256(t)

	params := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {"test-client"},
		"client_secret": {"wrong-secret"},
	}
	w := postToken(t, m, params)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for wrong secret, got %d", w.Code)
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"] != "invalid_client" {
		t.Errorf("expected error=invalid_client, got %q", resp["error"])
	}
}

func TestM2M_ClientCredentials_UnknownClient(t *testing.T) {
	m := newM2MHS256(t)

	params := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {"unknown"},
		"client_secret": {"secret"},
	}
	w := postToken(t, m, params)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for unknown client, got %d", w.Code)
	}
}

func TestM2M_ClientCredentials_MissingCredentials(t *testing.T) {
	m := newM2MHS256(t)

	params := url.Values{
		"grant_type": {"client_credentials"},
	}
	w := postToken(t, m, params)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 when no credentials, got %d", w.Code)
	}
}

func TestM2M_ClientCredentials_ScopeGranted(t *testing.T) {
	m := newM2MHS256(t)

	params := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {"test-client"},
		"client_secret": {"test-secret"},
		"scope":         {"read"},
	}
	w := postToken(t, m, params)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["scope"] != "read" {
		t.Errorf("expected scope=read, got %v", resp["scope"])
	}
}

func TestM2M_ClientCredentials_ScopeNotPermitted(t *testing.T) {
	m := newM2MHS256(t)

	params := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {"test-client"},
		"client_secret": {"test-secret"},
		"scope":         {"admin"},
	}
	w := postToken(t, m, params)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for forbidden scope, got %d", w.Code)
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"] != "invalid_scope" {
		t.Errorf("expected error=invalid_scope, got %q", resp["error"])
	}
}

func TestM2M_ClientCredentials_NoScopeGrantsAll(t *testing.T) {
	m := newM2MHS256(t)

	params := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {"test-client"},
		"client_secret": {"test-secret"},
		// no scope param → grant all client scopes
	}
	w := postToken(t, m, params)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	scopeVal, _ := resp["scope"].(string)
	if !strings.Contains(scopeVal, "read") || !strings.Contains(scopeVal, "write") {
		t.Errorf("expected all client scopes, got %q", scopeVal)
	}
}

// --- ES256 token issuance ---

func TestM2M_ES256_ClientCredentials_IssuesToken(t *testing.T) {
	m := newM2MES256(t)

	params := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {"es256-client"},
		"client_secret": {"es256-secret"},
	}
	w := postToken(t, m, params)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	tokenStr, _ := resp["access_token"].(string)
	if tokenStr == "" {
		t.Fatal("expected non-empty access_token")
	}

	// Parse the token header to confirm ES256 algorithm.
	tok, _, err := new(jwt.Parser).ParseUnverified(tokenStr, jwt.MapClaims{})
	if err != nil {
		t.Fatalf("parse token: %v", err)
	}
	if tok.Method.Alg() != "ES256" {
		t.Errorf("expected ES256 algorithm, got %q", tok.Method.Alg())
	}
}

func TestM2M_ES256_TokenVerifiable(t *testing.T) {
	m := newM2MES256(t)

	params := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {"es256-client"},
		"client_secret": {"es256-secret"},
	}
	w := postToken(t, m, params)

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	tokenStr, _ := resp["access_token"].(string)

	// Verify the token using the module's Authenticate method.
	valid, claims, err := m.Authenticate(tokenStr)
	if err != nil {
		t.Fatalf("authenticate error: %v", err)
	}
	if !valid {
		t.Error("expected token to be valid")
	}
	if claims["sub"] != "es256-client" {
		t.Errorf("expected sub=es256-client, got %v", claims["sub"])
	}
}

func TestM2M_ES256_GenerateKey(t *testing.T) {
	m := NewM2MAuthModule("m2m", "", time.Hour, "issuer")
	if err := m.GenerateECDSAKey(); err != nil {
		t.Fatalf("GenerateECDSAKey: %v", err)
	}
	if m.privateKey == nil {
		t.Error("expected private key to be set")
	}
	if m.publicKey == nil {
		t.Error("expected public key to be set")
	}
	if m.algorithm != SigningAlgES256 {
		t.Errorf("expected algorithm ES256, got %v", m.algorithm)
	}
}

func TestM2M_SetECDSAKey_ValidPEM(t *testing.T) {
	// Generate a key to export as PEM.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: der,
	})

	m := NewM2MAuthModule("m2m", "", time.Hour, "issuer")
	if err := m.SetECDSAKey(string(pemBytes)); err != nil {
		t.Fatalf("SetECDSAKey: %v", err)
	}
	if m.algorithm != SigningAlgES256 {
		t.Error("expected algorithm to be ES256 after SetECDSAKey")
	}
}

func TestM2M_SetECDSAKey_InvalidPEM(t *testing.T) {
	m := NewM2MAuthModule("m2m", "", time.Hour, "issuer")
	if err := m.SetECDSAKey("not a pem"); err == nil {
		t.Error("expected error for invalid PEM")
	}
}

func TestM2M_SetECDSAKey_NonP256Rejected(t *testing.T) {
	// Generate a P-384 key (not P-256) and verify it is rejected.
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatalf("generate P-384 key: %v", err)
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
	m := NewM2MAuthModule("m2m", "", time.Hour, "issuer")
	if err := m.SetECDSAKey(string(pemBytes)); err == nil {
		t.Error("expected error for non-P256 key")
	} else if !strings.Contains(err.Error(), "P-256") {
		t.Errorf("expected P-256 mention in error, got %q", err.Error())
	}
}

func TestM2M_InitErr_SurfacedInInit(t *testing.T) {
	m := NewM2MAuthModule("m2m", "", time.Hour, "issuer")
	m.SetInitErr(fmt.Errorf("injected key error"))
	if err := m.Init(nil); err == nil {
		t.Error("expected init error to surface")
	}
}

// --- JWKS endpoint ---

func TestM2M_JWKS_ES256(t *testing.T) {
	m := newM2MES256(t)

	req := httptest.NewRequest(http.MethodGet, "/oauth/jwks", nil)
	w := httptest.NewRecorder()
	m.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for JWKS, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode JWKS: %v", err)
	}
	keys, ok := resp["keys"].([]any)
	if !ok || len(keys) == 0 {
		t.Fatal("expected non-empty keys array")
	}
	jwk, _ := keys[0].(map[string]any)
	if jwk["kty"] != "EC" {
		t.Errorf("expected kty=EC, got %v", jwk["kty"])
	}
	if jwk["crv"] != "P-256" {
		t.Errorf("expected crv=P-256, got %v", jwk["crv"])
	}
	if jwk["alg"] != "ES256" {
		t.Errorf("expected alg=ES256, got %v", jwk["alg"])
	}
}

func TestM2M_JWKS_HS256_NotAvailable(t *testing.T) {
	m := newM2MHS256(t)

	req := httptest.NewRequest(http.MethodGet, "/oauth/jwks", nil)
	w := httptest.NewRecorder()
	m.Handle(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for HS256 JWKS, got %d", w.Code)
	}
}

func TestM2M_JWKS_RoundTrip(t *testing.T) {
	m := newM2MES256(t)

	// Get JWKS.
	req := httptest.NewRequest(http.MethodGet, "/oauth/jwks", nil)
	w := httptest.NewRecorder()
	m.Handle(w, req)

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	keys := resp["keys"].([]any)
	jwk := keys[0].(map[string]any)

	// Reconstruct the public key from the JWK.
	pub, err := jwkToECPublicKey(jwk)
	if err != nil {
		t.Fatalf("jwkToECPublicKey: %v", err)
	}

	// Compare via ECDH byte representation to avoid using deprecated X/Y fields.
	origBytes, err := m.publicKey.ECDH()
	if err != nil {
		t.Fatalf("original key ECDH: %v", err)
	}
	reconBytes, err := pub.ECDH()
	if err != nil {
		t.Fatalf("reconstructed key ECDH: %v", err)
	}
	if string(origBytes.Bytes()) != string(reconBytes.Bytes()) {
		t.Error("reconstructed key does not match original")
	}
}

// --- JWT-bearer grant ---

func TestM2M_JWTBearer_ES256_Valid(t *testing.T) {
	// Server M2M module
	server := newM2MES256(t)

	// Client generates its own key pair.
	clientKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate client key: %v", err)
	}

	// Register client's public key as trusted by the server.
	server.AddTrustedKey("client-service", &clientKey.PublicKey)

	// Client creates a JWT assertion.
	claims := jwt.MapClaims{
		"iss": "client-service",
		"sub": "client-service",
		"aud": "test-issuer",
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(5 * time.Minute).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	assertion, err := tok.SignedString(clientKey)
	if err != nil {
		t.Fatalf("sign assertion: %v", err)
	}

	params := url.Values{
		"grant_type": {GrantTypeJWTBearer},
		"assertion":  {assertion},
		"scope":      {"api"},
	}
	w := postToken(t, server, params)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["access_token"] == nil || resp["access_token"] == "" {
		t.Error("expected non-empty access_token")
	}
}

func TestM2M_JWTBearer_HS256_Valid(t *testing.T) {
	m := newM2MHS256(t)

	// Create a JWT assertion signed with the module's own HMAC secret.
	claims := jwt.MapClaims{
		"iss": "internal-service",
		"sub": "internal-service",
		"aud": "test-issuer",
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(5 * time.Minute).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	assertion, err := tok.SignedString([]byte("this-is-a-valid-secret-32-bytes!"))
	if err != nil {
		t.Fatalf("sign assertion: %v", err)
	}

	params := url.Values{
		"grant_type": {GrantTypeJWTBearer},
		"assertion":  {assertion},
	}
	w := postToken(t, m, params)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestM2M_JWTBearer_MissingAssertion(t *testing.T) {
	m := newM2MHS256(t)

	params := url.Values{
		"grant_type": {GrantTypeJWTBearer},
	}
	w := postToken(t, m, params)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing assertion, got %d", w.Code)
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"] != "invalid_request" {
		t.Errorf("expected error=invalid_request, got %q", resp["error"])
	}
}

func TestM2M_JWTBearer_InvalidSignature(t *testing.T) {
	m := newM2MHS256(t)

	// Sign with a different secret.
	claims := jwt.MapClaims{
		"sub": "service",
		"exp": time.Now().Add(5 * time.Minute).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	badAssertion, _ := tok.SignedString([]byte("wrong-secret-that-is-long-enough-x"))

	params := url.Values{
		"grant_type": {GrantTypeJWTBearer},
		"assertion":  {badAssertion},
	}
	w := postToken(t, m, params)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for bad assertion, got %d", w.Code)
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"] != "invalid_grant" {
		t.Errorf("expected error=invalid_grant, got %q", resp["error"])
	}
}

func TestM2M_JWTBearer_ExpiredAssertion(t *testing.T) {
	m := newM2MHS256(t)

	claims := jwt.MapClaims{
		"sub": "service",
		"exp": time.Now().Add(-time.Minute).Unix(), // expired
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	expired, _ := tok.SignedString([]byte("this-is-a-valid-secret-32-bytes!"))

	params := url.Values{
		"grant_type": {GrantTypeJWTBearer},
		"assertion":  {expired},
	}
	w := postToken(t, m, params)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for expired assertion, got %d", w.Code)
	}
}

func TestM2M_JWTBearer_MissingSub(t *testing.T) {
	m := newM2MHS256(t)

	claims := jwt.MapClaims{
		// no "sub"
		"exp": time.Now().Add(5 * time.Minute).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	assertion, _ := tok.SignedString([]byte("this-is-a-valid-secret-32-bytes!"))

	params := url.Values{
		"grant_type": {GrantTypeJWTBearer},
		"assertion":  {assertion},
	}
	w := postToken(t, m, params)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing sub, got %d", w.Code)
	}
}

func TestM2M_JWTBearer_UntrustedKey(t *testing.T) {
	// Module with ES256 but no trusted keys (and no hmac secret).
	m := newM2MES256(t)

	clientKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	// Note: key is NOT added to trusted keys.

	claims := jwt.MapClaims{
		"sub": "unknown-service",
		"exp": time.Now().Add(5 * time.Minute).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	assertion, _ := tok.SignedString(clientKey)

	params := url.Values{
		"grant_type": {GrantTypeJWTBearer},
		"assertion":  {assertion},
	}
	w := postToken(t, m, params)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 when no trusted key matches, got %d", w.Code)
	}
}

// TestM2M_JWTBearer_KeySelectedByIss verifies that validation selects the key
// that matches the assertion's iss claim, not an arbitrary trusted key.
func TestM2M_JWTBearer_KeySelectedByIss(t *testing.T) {
	server := newM2MES256(t)

	// Register two different keys for two different issuers.
	keyA, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	keyB, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	server.AddTrustedKey("service-a", &keyA.PublicKey)
	server.AddTrustedKey("service-b", &keyB.PublicKey)

	// Build an assertion claiming iss=service-a but signed with keyB (mismatch).
	badClaims := jwt.MapClaims{
		"iss": "service-a",
		"sub": "service-a",
		"exp": time.Now().Add(5 * time.Minute).Unix(),
	}
	badTok := jwt.NewWithClaims(jwt.SigningMethodES256, badClaims)
	badAssertion, _ := badTok.SignedString(keyB) // signed by keyB but iss=service-a

	params := url.Values{
		"grant_type": {GrantTypeJWTBearer},
		"assertion":  {badAssertion},
	}
	w := postToken(t, server, params)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 when assertion iss/key mismatch, got %d; body: %s", w.Code, w.Body.String())
	}
}

// TestM2M_JWTBearer_KeySelectedByKid verifies that the kid header is used for key lookup.
func TestM2M_JWTBearer_KeySelectedByKid(t *testing.T) {
	server := newM2MES256(t)

	clientKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	server.AddTrustedKey("my-kid", &clientKey.PublicKey)

	claims := jwt.MapClaims{
		"iss": "some-service",
		"sub": "some-service",
		"exp": time.Now().Add(5 * time.Minute).Unix(),
	}
	// Set kid in header; server should find key by kid.
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	tok.Header["kid"] = "my-kid"
	assertion, err := tok.SignedString(clientKey)
	if err != nil {
		t.Fatalf("sign assertion: %v", err)
	}

	params := url.Values{
		"grant_type": {GrantTypeJWTBearer},
		"assertion":  {assertion},
	}
	w := postToken(t, server, params)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for kid-based key lookup, got %d; body: %s", w.Code, w.Body.String())
	}
}

// --- unsupported grant type ---

func TestM2M_UnsupportedGrantType(t *testing.T) {
	m := newM2MHS256(t)

	params := url.Values{
		"grant_type": {"authorization_code"},
		"code":       {"abc"},
	}
	w := postToken(t, m, params)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unsupported grant, got %d", w.Code)
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"] != "unsupported_grant_type" {
		t.Errorf("expected error=unsupported_grant_type, got %q", resp["error"])
	}
}

// --- not found / unknown route ---

func TestM2M_UnknownRoute(t *testing.T) {
	m := newM2MHS256(t)

	req := httptest.NewRequest(http.MethodGet, "/oauth/unknown", nil)
	w := httptest.NewRecorder()
	m.Handle(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// --- Token revocation (RFC 7009) ---

// postRevoke is a test helper that sends a form-encoded POST to /oauth/revoke,
// authenticated via HTTP Basic Auth (client_id + client_secret).
func postRevoke(t *testing.T, m *M2MAuthModule, params url.Values) *httptest.ResponseRecorder {
	t.Helper()
	return postRevokeAs(t, m, params, "test-client", "test-secret")
}

// postRevokeAs sends a POST to /oauth/revoke with the given client credentials.
func postRevokeAs(t *testing.T, m *M2MAuthModule, params url.Values, clientID, clientSecret string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/oauth/revoke",
		strings.NewReader(params.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if clientID != "" {
		req.SetBasicAuth(clientID, clientSecret)
	}
	w := httptest.NewRecorder()
	m.Handle(w, req)
	return w
}

// postIntrospect is a test helper that sends a form-encoded POST to /oauth/introspect
// authenticated with a Bearer token (the caller's own token for self-inspection, or a
// different token for cross-inspection when the policy allows it).
func postIntrospect(t *testing.T, m *M2MAuthModule, params url.Values, bearerToken string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/oauth/introspect",
		strings.NewReader(params.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	w := httptest.NewRecorder()
	m.Handle(w, req)
	return w
}

// postIntrospectBasic is a test helper that authenticates the introspect endpoint
// using HTTP Basic Auth (client_id + client_secret).
func postIntrospectBasic(t *testing.T, m *M2MAuthModule, params url.Values, clientID, clientSecret string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/oauth/introspect",
		strings.NewReader(params.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(clientID, clientSecret)
	w := httptest.NewRecorder()
	m.Handle(w, req)
	return w
}

// issueTestToken is a test helper that obtains an access token via client_credentials.
func issueTestToken(t *testing.T, m *M2MAuthModule, clientID, clientSecret string) string {
	t.Helper()
	params := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
	}
	w := postToken(t, m, params)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 issuing token, got %d; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	tok, _ := resp["access_token"].(string)
	if tok == "" {
		t.Fatal("expected non-empty access_token")
	}
	return tok
}

func TestM2M_Revoke_ValidToken_Returns200(t *testing.T) {
	m := newM2MHS256(t)
	tokenStr := issueTestToken(t, m, "test-client", "test-secret")

	w := postRevoke(t, m, url.Values{"token": {tokenStr}})
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for revoke, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestM2M_Revoke_NoClientAuth_Returns401(t *testing.T) {
	// RFC 7009 §2.1: client authentication is required.
	m := newM2MHS256(t)
	tokenStr := issueTestToken(t, m, "test-client", "test-secret")
	// Send revoke with no credentials.
	w := postRevokeAs(t, m, url.Values{"token": {tokenStr}}, "", "")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 when no client auth provided, got %d", w.Code)
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"] != "invalid_client" {
		t.Errorf("expected error=invalid_client, got %q", resp["error"])
	}
}

func TestM2M_Revoke_WrongClientCredentials_Returns401(t *testing.T) {
	m := newM2MHS256(t)
	tokenStr := issueTestToken(t, m, "test-client", "test-secret")
	w := postRevokeAs(t, m, url.Values{"token": {tokenStr}}, "test-client", "wrong-secret")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for wrong credentials, got %d", w.Code)
	}
}

func TestM2M_Revoke_InvalidToken_StillReturns200(t *testing.T) {
	// RFC 7009 §2.2: revocation of an unknown/invalid token must return 200.
	m := newM2MHS256(t)
	w := postRevoke(t, m, url.Values{"token": {"not.a.valid.jwt"}})
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 even for invalid token, got %d", w.Code)
	}
}

func TestM2M_Revoke_MissingToken_Returns400(t *testing.T) {
	m := newM2MHS256(t)
	w := postRevoke(t, m, url.Values{})
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 when token param missing, got %d", w.Code)
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"] != "invalid_request" {
		t.Errorf("expected error=invalid_request, got %q", resp["error"])
	}
}

func TestM2M_Revoke_BlacklistsToken_AuthenticateFails(t *testing.T) {
	m := newM2MHS256(t)
	tokenStr := issueTestToken(t, m, "test-client", "test-secret")

	// Token is valid before revocation.
	valid, _, _ := m.Authenticate(tokenStr)
	if !valid {
		t.Fatal("expected token to be valid before revocation")
	}

	// Revoke the token.
	w := postRevoke(t, m, url.Values{"token": {tokenStr}})
	if w.Code != http.StatusOK {
		t.Fatalf("revoke failed with %d", w.Code)
	}

	// Token must now be rejected by Authenticate.
	valid, _, _ = m.Authenticate(tokenStr)
	if valid {
		t.Error("expected token to be invalid after revocation")
	}
}

func TestM2M_Revoke_ES256_BlacklistsToken(t *testing.T) {
	m := newM2MES256(t)
	tokenStr := issueTestToken(t, m, "es256-client", "es256-secret")

	valid, _, _ := m.Authenticate(tokenStr)
	if !valid {
		t.Fatal("expected token to be valid before revocation")
	}

	w := postRevokeAs(t, m, url.Values{"token": {tokenStr}}, "es256-client", "es256-secret")
	if w.Code != http.StatusOK {
		t.Fatalf("revoke failed with %d", w.Code)
	}

	valid, _, _ = m.Authenticate(tokenStr)
	if valid {
		t.Error("expected ES256 token to be invalid after revocation")
	}
}

// --- Token introspection (RFC 7662) ---

func TestM2M_Introspect_ValidToken_ActiveTrue(t *testing.T) {
	m := newM2MHS256(t)
	tokenStr := issueTestToken(t, m, "test-client", "test-secret")

	// Self-inspection: caller authenticates with its own token.
	w := postIntrospect(t, m, url.Values{"token": {tokenStr}}, tokenStr)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["active"] != true {
		t.Errorf("expected active=true, got %v", resp["active"])
	}
	if resp["client_id"] != "test-client" {
		t.Errorf("expected client_id=test-client, got %v", resp["client_id"])
	}
	if resp["iss"] != "test-issuer" {
		t.Errorf("expected iss=test-issuer, got %v", resp["iss"])
	}
	if resp["exp"] == nil {
		t.Error("expected exp in introspect response")
	}
	if resp["iat"] == nil {
		t.Error("expected iat in introspect response")
	}
}

func TestM2M_Introspect_InvalidToken_ActiveFalse(t *testing.T) {
	m := newM2MHS256(t)
	// Authenticate with a valid token but try to introspect an invalid one (allowOthers=false → 403, not active=false).
	// Use Basic Auth to show the token is just invalid (not a policy error).
	w := postIntrospectBasic(t, m, url.Values{"token": {"not.a.valid.jwt"}}, "test-client", "test-secret")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["active"] != false {
		t.Errorf("expected active=false for invalid token, got %v", resp["active"])
	}
}

func TestM2M_Introspect_RevokedToken_ActiveFalse(t *testing.T) {
	m := newM2MHS256(t)
	m.SetIntrospectPolicy(true, "", "", "") // allow others so we can still introspect after revoke
	tokenStr := issueTestToken(t, m, "test-client", "test-secret")

	// Revoke then introspect (self-inspect: same caller, same token).
	postRevoke(t, m, url.Values{"token": {tokenStr}})

	// Use Basic Auth since the token is now revoked (can't use it as Bearer).
	w := postIntrospectBasic(t, m, url.Values{"token": {tokenStr}}, "test-client", "test-secret")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["active"] != false {
		t.Errorf("expected active=false for revoked token, got %v", resp["active"])
	}
}

func TestM2M_Introspect_MissingToken_Returns400(t *testing.T) {
	m := newM2MHS256(t)
	// Authenticate via Basic Auth; the missing `token` param is what triggers the 400.
	w := postIntrospectBasic(t, m, url.Values{}, "test-client", "test-secret")
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 when token param missing, got %d", w.Code)
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"] != "invalid_request" {
		t.Errorf("expected error=invalid_request, got %q", resp["error"])
	}
}

func TestM2M_Introspect_ES256_ValidToken(t *testing.T) {
	m := newM2MES256(t)
	tokenStr := issueTestToken(t, m, "es256-client", "es256-secret")

	w := postIntrospect(t, m, url.Values{"token": {tokenStr}}, tokenStr)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["active"] != true {
		t.Errorf("expected active=true, got %v", resp["active"])
	}
	if resp["client_id"] != "es256-client" {
		t.Errorf("expected client_id=es256-client, got %v", resp["client_id"])
	}
}

func TestM2M_Introspect_ScopeIncluded(t *testing.T) {
	m := newM2MHS256(t)
	params := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {"test-client"},
		"client_secret": {"test-secret"},
		"scope":         {"read"},
	}
	w := postToken(t, m, params)
	var tokenResp map[string]any
	json.NewDecoder(w.Body).Decode(&tokenResp)
	tokenStr, _ := tokenResp["access_token"].(string)

	// Self-inspection.
	introspectResp := postIntrospect(t, m, url.Values{"token": {tokenStr}}, tokenStr)
	var resp map[string]any
	json.NewDecoder(introspectResp.Body).Decode(&resp)
	if resp["scope"] != "read" {
		t.Errorf("expected scope=read in introspect response, got %v", resp["scope"])
	}
}

// Verify that issued tokens include a jti claim.
func TestM2M_IssuedToken_HasJTI(t *testing.T) {
	m := newM2MHS256(t)
	tokenStr := issueTestToken(t, m, "test-client", "test-secret")

	_, claims, err := m.Authenticate(tokenStr)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	jti, _ := claims["jti"].(string)
	if jti == "" {
		t.Error("expected non-empty jti claim in issued token")
	}
}

// Verify that two tokens issued for the same client have different JTIs.
func TestM2M_IssuedTokens_UniqueJTIs(t *testing.T) {
	m := newM2MHS256(t)
	tok1 := issueTestToken(t, m, "test-client", "test-secret")
	tok2 := issueTestToken(t, m, "test-client", "test-secret")

	_, claims1, _ := m.Authenticate(tok1)
	_, claims2, _ := m.Authenticate(tok2)
	jti1, _ := claims1["jti"].(string)
	jti2, _ := claims2["jti"].(string)
	if jti1 == "" || jti2 == "" {
		t.Fatal("expected non-empty jti claims")
	}
	if jti1 == jti2 {
		t.Error("expected unique JTIs for different tokens")
	}
}

// --- Introspect access-control ---

// TestM2M_Introspect_NoAuth_Returns401 verifies that unauthenticated requests are rejected.
func TestM2M_Introspect_NoAuth_Returns401(t *testing.T) {
	m := newM2MHS256(t)
	tokenStr := issueTestToken(t, m, "test-client", "test-secret")

	// No Authorization header, no Basic Auth.
	w := postIntrospect(t, m, url.Values{"token": {tokenStr}}, "")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for unauthenticated introspect, got %d; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"] != "unauthorized_client" {
		t.Errorf("expected error=unauthorized_client, got %q", resp["error"])
	}
}

// TestM2M_Introspect_BasicAuth_SelfToken verifies that Basic Auth allows self-inspection.
func TestM2M_Introspect_BasicAuth_SelfToken(t *testing.T) {
	m := newM2MHS256(t)
	tokenStr := issueTestToken(t, m, "test-client", "test-secret")

	w := postIntrospectBasic(t, m, url.Values{"token": {tokenStr}}, "test-client", "test-secret")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for Basic Auth self-inspect, got %d; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["active"] != true {
		t.Errorf("expected active=true, got %v", resp["active"])
	}
}

// TestM2M_Introspect_BasicAuth_InvalidCredentials_Returns401 verifies that wrong
// Basic Auth credentials are rejected.
func TestM2M_Introspect_BasicAuth_InvalidCredentials_Returns401(t *testing.T) {
	m := newM2MHS256(t)
	tokenStr := issueTestToken(t, m, "test-client", "test-secret")

	w := postIntrospectBasic(t, m, url.Values{"token": {tokenStr}}, "test-client", "wrong-secret")
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for bad Basic Auth, got %d", w.Code)
	}
}

// TestM2M_Introspect_SelfOnly_CrossTokenForbidden verifies that in default self-only
// mode a caller cannot introspect another client's token.
func TestM2M_Introspect_SelfOnly_CrossTokenForbidden(t *testing.T) {
	m := NewM2MAuthModule("m2m", "this-is-a-valid-secret-32-bytes!", time.Hour, "test-issuer")
	m.RegisterClient(M2MClient{ClientID: "client-a", ClientSecret: "secret-a", Scopes: []string{"read"}}) //nolint:gosec // test credential
	m.RegisterClient(M2MClient{ClientID: "client-b", ClientSecret: "secret-b", Scopes: []string{"read"}}) //nolint:gosec // test credential

	tokenA := issueTestToken(t, m, "client-a", "secret-a")
	tokenB := issueTestToken(t, m, "client-b", "secret-b")

	// client-a tries to introspect client-b's token (default policy = self-only).
	w := postIntrospect(t, m, url.Values{"token": {tokenB}}, tokenA)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 when self-only policy prevents cross-token inspect, got %d; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"] != "access_denied" {
		t.Errorf("expected error=access_denied, got %q", resp["error"])
	}
}

// TestM2M_Introspect_AllowOthers_BasicAuth verifies that HTTP Basic Auth callers
// can inspect any token when allowOthers=true.
func TestM2M_Introspect_AllowOthers_BasicAuth(t *testing.T) {
	m := NewM2MAuthModule("m2m", "this-is-a-valid-secret-32-bytes!", time.Hour, "test-issuer")
	m.RegisterClient(M2MClient{ClientID: "client-a", ClientSecret: "secret-a", Scopes: []string{"read"}}) //nolint:gosec // test credential
	m.RegisterClient(M2MClient{ClientID: "client-b", ClientSecret: "secret-b", Scopes: []string{"read"}}) //nolint:gosec // test credential
	m.SetIntrospectPolicy(true, "", "", "")

	tokenB := issueTestToken(t, m, "client-b", "secret-b")

	// client-a authenticates via Basic Auth and inspects client-b's token.
	w := postIntrospectBasic(t, m, url.Values{"token": {tokenB}}, "client-a", "secret-a")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for Basic Auth cross-token inspect, got %d; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["active"] != true {
		t.Errorf("expected active=true, got %v", resp["active"])
	}
}

// TestM2M_Introspect_AllowOthers_BearerWithRequiredScope verifies that a Bearer
// token caller can inspect another token when it has the required scope.
func TestM2M_Introspect_AllowOthers_BearerWithRequiredScope(t *testing.T) {
	m := NewM2MAuthModule("m2m", "this-is-a-valid-secret-32-bytes!", time.Hour, "test-issuer")
	m.RegisterClient(M2MClient{ClientID: "admin", ClientSecret: "admin-secret-long!", Scopes: []string{"read", "introspect:admin"}}) //nolint:gosec // test credential
	m.RegisterClient(M2MClient{ClientID: "worker", ClientSecret: "worker-secret-long", Scopes: []string{"read"}})                    //nolint:gosec // test credential
	m.SetIntrospectPolicy(true, "introspect:admin", "", "")

	adminToken := issueTestToken(t, m, "admin", "admin-secret-long!")
	workerToken := issueTestToken(t, m, "worker", "worker-secret-long")

	// admin (has introspect:admin scope) inspects worker's token.
	w := postIntrospect(t, m, url.Values{"token": {workerToken}}, adminToken)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["active"] != true {
		t.Errorf("expected active=true, got %v", resp["active"])
	}
	if resp["client_id"] != "worker" {
		t.Errorf("expected client_id=worker, got %v", resp["client_id"])
	}
}

// TestM2M_Introspect_AllowOthers_BearerMissingScope verifies that a Bearer token
// caller is forbidden from inspecting another token when it lacks the required scope.
func TestM2M_Introspect_AllowOthers_BearerMissingScope(t *testing.T) {
	m := NewM2MAuthModule("m2m", "this-is-a-valid-secret-32-bytes!", time.Hour, "test-issuer")
	m.RegisterClient(M2MClient{ClientID: "client-a", ClientSecret: "secret-a-long-enough!", Scopes: []string{"read"}}) //nolint:gosec // test credential
	m.RegisterClient(M2MClient{ClientID: "client-b", ClientSecret: "secret-b-long-enough!", Scopes: []string{"read"}}) //nolint:gosec // test credential
	m.SetIntrospectPolicy(true, "introspect:admin", "", "")

	tokenA := issueTestToken(t, m, "client-a", "secret-a-long-enough!")
	tokenB := issueTestToken(t, m, "client-b", "secret-b-long-enough!")

	// client-a lacks introspect:admin → forbidden.
	w := postIntrospect(t, m, url.Values{"token": {tokenB}}, tokenA)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 when caller missing required scope, got %d", w.Code)
	}
}

// TestM2M_Introspect_AllowOthers_BearerWithRequiredClaim verifies that a claim-based
// prerequisite is enforced for cross-token inspection.
func TestM2M_Introspect_AllowOthers_BearerWithRequiredClaim(t *testing.T) {
	m := NewM2MAuthModule("m2m", "this-is-a-valid-secret-32-bytes!", time.Hour, "test-issuer")
	m.RegisterClient(M2MClient{
		ClientID:     "admin",
		ClientSecret: "admin-secret-long!", //nolint:gosec // test credential
		Scopes:       []string{"read"},
		Claims:       map[string]any{"role": "admin"},
	})
	m.RegisterClient(M2MClient{ClientID: "worker", ClientSecret: "worker-secret-long", Scopes: []string{"read"}}) //nolint:gosec // test credential
	m.SetIntrospectPolicy(true, "", "role", "admin")

	adminToken := issueTestToken(t, m, "admin", "admin-secret-long!")
	workerToken := issueTestToken(t, m, "worker", "worker-secret-long")

	// admin (has role=admin claim) can inspect worker's token.
	w := postIntrospect(t, m, url.Values{"token": {workerToken}}, adminToken)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["active"] != true {
		t.Errorf("expected active=true, got %v", resp["active"])
	}
}

// TestM2M_Introspect_AllowOthers_BearerMissingClaim verifies that a caller without
// the required claim value is forbidden from cross-token inspection.
func TestM2M_Introspect_AllowOthers_BearerMissingClaim(t *testing.T) {
	m := NewM2MAuthModule("m2m", "this-is-a-valid-secret-32-bytes!", time.Hour, "test-issuer")
	m.RegisterClient(M2MClient{ClientID: "client-a", ClientSecret: "secret-a-long-enough!", Scopes: []string{"read"}}) //nolint:gosec // test credential
	m.RegisterClient(M2MClient{ClientID: "client-b", ClientSecret: "secret-b-long-enough!", Scopes: []string{"read"}}) //nolint:gosec // test credential
	m.SetIntrospectPolicy(true, "", "role", "admin")                                                                   // role=admin required

	tokenA := issueTestToken(t, m, "client-a", "secret-a-long-enough!")
	tokenB := issueTestToken(t, m, "client-b", "secret-b-long-enough!")

	// client-a has no role=admin claim → forbidden.
	w := postIntrospect(t, m, url.Values{"token": {tokenB}}, tokenA)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 when caller missing required claim, got %d", w.Code)
	}
}

// TestM2M_Introspect_SelfAlwaysAllowed verifies that self-inspection is always
// permitted even when allowOthers=true has scope/claim prerequisites.
func TestM2M_Introspect_SelfAlwaysAllowed(t *testing.T) {
	m := NewM2MAuthModule("m2m", "this-is-a-valid-secret-32-bytes!", time.Hour, "test-issuer")
	m.RegisterClient(M2MClient{ClientID: "worker", ClientSecret: "worker-secret-long", Scopes: []string{"read"}}) //nolint:gosec // test credential
	m.SetIntrospectPolicy(true, "introspect:admin", "role", "admin")

	workerToken := issueTestToken(t, m, "worker", "worker-secret-long")

	// worker inspects its own token — must succeed even without the admin scope/claim.
	w := postIntrospect(t, m, url.Values{"token": {workerToken}}, workerToken)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for self-inspection, got %d; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["active"] != true {
		t.Errorf("expected active=true for self-inspection, got %v", resp["active"])
	}
}

// TestM2M_Introspect_RevokedCallerToken_Returns401 verifies that a revoked Bearer
// token cannot be used to authenticate an introspect call.
func TestM2M_Introspect_RevokedCallerToken_Returns401(t *testing.T) {
	m := newM2MHS256(t)
	callerToken := issueTestToken(t, m, "test-client", "test-secret")
	targetToken := issueTestToken(t, m, "test-client", "test-secret")

	// Revoke the caller's token.
	postRevoke(t, m, url.Values{"token": {callerToken}})

	// Attempt introspect with the now-revoked caller token.
	w := postIntrospect(t, m, url.Values{"token": {targetToken}}, callerToken)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 when caller token is revoked, got %d", w.Code)
	}
}

// --- Authenticate (AuthProvider interface) ---

func TestM2M_Authenticate_HS256_Valid(t *testing.T) {
	m := newM2MHS256(t)

	// Get a token.
	params := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {"test-client"},
		"client_secret": {"test-secret"},
	}
	w := postToken(t, m, params)
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	tokenStr, _ := resp["access_token"].(string)

	valid, claims, err := m.Authenticate(tokenStr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !valid {
		t.Error("expected token to be valid")
	}
	if claims["sub"] != "test-client" {
		t.Errorf("expected sub=test-client, got %v", claims["sub"])
	}
}

func TestM2M_Authenticate_HS256_Invalid(t *testing.T) {
	m := newM2MHS256(t)

	valid, _, err := m.Authenticate("not.a.jwt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if valid {
		t.Error("expected invalid token to not authenticate")
	}
}

func TestM2M_Authenticate_ES256_Valid(t *testing.T) {
	m := newM2MES256(t)

	params := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {"es256-client"},
		"client_secret": {"es256-secret"},
	}
	w := postToken(t, m, params)
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	tokenStr, _ := resp["access_token"].(string)

	valid, claims, err := m.Authenticate(tokenStr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !valid {
		t.Error("expected token to be valid")
	}
	if claims["iss"] != "test-issuer" {
		t.Errorf("expected iss=test-issuer, got %v", claims["iss"])
	}
}

func TestM2M_Authenticate_ES256_NoPublicKey(t *testing.T) {
	m := &M2MAuthModule{
		name:      "m2m",
		algorithm: SigningAlgES256,
		// no publicKey set
	}
	_, _, err := m.Authenticate("some.jwt.token")
	if err == nil {
		t.Error("expected error when no public key configured")
	}
}

// --- JWK helpers ---

func TestM2M_ecPublicKeyToJWK(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	jwk, err := ecPublicKeyToJWK(&key.PublicKey, "test-key")
	if err != nil {
		t.Fatalf("ecPublicKeyToJWK: %v", err)
	}
	if jwk["kty"] != "EC" {
		t.Errorf("expected kty=EC, got %v", jwk["kty"])
	}
	if jwk["crv"] != "P-256" {
		t.Errorf("expected crv=P-256, got %v", jwk["crv"])
	}
	if jwk["kid"] != "test-key" {
		t.Errorf("expected kid=test-key, got %v", jwk["kid"])
	}
}

func TestM2M_jwkToECPublicKey_InvalidKty(t *testing.T) {
	_, err := jwkToECPublicKey(map[string]any{"kty": "RSA"})
	if err == nil {
		t.Error("expected error for kty=RSA")
	}
}

func TestM2M_jwkToECPublicKey_InvalidCrv(t *testing.T) {
	_, err := jwkToECPublicKey(map[string]any{"kty": "EC", "crv": "P-384"})
	if err == nil {
		t.Error("expected error for crv=P-384")
	}
}

func TestM2M_jwkToECPublicKey_MissingCoords(t *testing.T) {
	_, err := jwkToECPublicKey(map[string]any{"kty": "EC", "crv": "P-256"})
	if err == nil {
		t.Error("expected error for missing x/y")
	}
}

func TestM2M_jwkThumbprint_Deterministic(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	a := jwkThumbprint(&key.PublicKey)
	b := jwkThumbprint(&key.PublicKey)
	if a != b {
		t.Error("thumbprint must be deterministic")
	}
}

func TestM2M_AddTrustedKey(t *testing.T) {
	m := newM2MES256(t)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	m.AddTrustedKey("svc", &key.PublicKey)

	m.mu.RLock()
	stored := m.trustedKeys["svc"]
	m.mu.RUnlock()

	if stored == nil || stored.pubKey == nil {
		t.Error("expected key to be stored")
	}
}

// ecPublicKeyToPEM marshals an ECDSA public key to a PEM-encoded string.
func ecPublicKeyToPEM(t *testing.T, pub *ecdsa.PublicKey) string {
	t.Helper()
	pkixBytes, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pkixBytes}))
}

func TestM2M_AddTrustedKeyFromPEM_Valid(t *testing.T) {
	m := newM2MES256(t)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	pemStr := ecPublicKeyToPEM(t, &key.PublicKey)

	if err := m.AddTrustedKeyFromPEM("issuer-a", pemStr, nil, nil); err != nil {
		t.Fatalf("AddTrustedKeyFromPEM: %v", err)
	}

	m.mu.RLock()
	stored := m.trustedKeys["issuer-a"]
	m.mu.RUnlock()

	if stored == nil || stored.pubKey == nil {
		t.Error("expected key to be stored")
	}
}

func TestM2M_AddTrustedKeyFromPEM_EscapedNewlines(t *testing.T) {
	m := newM2MES256(t)
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	pemStr := ecPublicKeyToPEM(t, &key.PublicKey)
	// Simulate Docker/Kubernetes env var with literal \n instead of real newlines.
	escapedPEM := strings.ReplaceAll(pemStr, "\n", `\n`)

	if err := m.AddTrustedKeyFromPEM("issuer-b", escapedPEM, nil, nil); err != nil {
		t.Fatalf("AddTrustedKeyFromPEM with escaped newlines: %v", err)
	}

	m.mu.RLock()
	stored := m.trustedKeys["issuer-b"]
	m.mu.RUnlock()

	if stored == nil || stored.pubKey == nil {
		t.Error("expected key to be stored after escaped-newline normalisation")
	}
}

func TestM2M_AddTrustedKeyFromPEM_Invalid(t *testing.T) {
	m := newM2MES256(t)
	err := m.AddTrustedKeyFromPEM("issuer-bad", "not-a-pem", nil, nil)
	if err == nil {
		t.Error("expected error for invalid PEM, got nil")
	}
}

func TestM2M_AddTrustedKeyFromPEM_NonP256Rejected(t *testing.T) {
	m := newM2MES256(t)
	// Generate a P-384 key, which should be rejected.
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatalf("generate P-384 key: %v", err)
	}
	pkixBytes, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey: %v", err)
	}
	pemStr := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pkixBytes}))

	if err := m.AddTrustedKeyFromPEM("issuer-p384", pemStr, nil, nil); err == nil {
		t.Error("expected error for P-384 key, got nil")
	}
}

func TestM2M_JWTBearer_AudienceValid(t *testing.T) {
	server := newM2MES256(t)
	clientKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	pemStr := ecPublicKeyToPEM(t, &clientKey.PublicKey)

	if err := server.AddTrustedKeyFromPEM("client-svc", pemStr, []string{"test-issuer"}, nil); err != nil {
		t.Fatalf("AddTrustedKeyFromPEM: %v", err)
	}

	claims := jwt.MapClaims{
		"iss": "client-svc",
		"sub": "client-svc",
		"aud": "test-issuer",
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(5 * time.Minute).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	assertion, err := tok.SignedString(clientKey)
	if err != nil {
		t.Fatalf("sign assertion: %v", err)
	}

	params := url.Values{
		"grant_type": {GrantTypeJWTBearer},
		"assertion":  {assertion},
	}
	w := postToken(t, server, params)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 with valid audience, got %d: %s", w.Code, w.Body.String())
	}
}

func TestM2M_JWTBearer_AudienceMismatch(t *testing.T) {
	server := newM2MES256(t)
	clientKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	pemStr := ecPublicKeyToPEM(t, &clientKey.PublicKey)

	// Require audience "test-issuer" but assertion will have "wrong-audience".
	if err := server.AddTrustedKeyFromPEM("client-svc", pemStr, []string{"test-issuer"}, nil); err != nil {
		t.Fatalf("AddTrustedKeyFromPEM: %v", err)
	}

	claims := jwt.MapClaims{
		"iss": "client-svc",
		"sub": "client-svc",
		"aud": "wrong-audience",
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(5 * time.Minute).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	assertion, err := tok.SignedString(clientKey)
	if err != nil {
		t.Fatalf("sign assertion: %v", err)
	}

	params := url.Values{
		"grant_type": {GrantTypeJWTBearer},
		"assertion":  {assertion},
	}
	w := postToken(t, server, params)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for audience mismatch, got %d", w.Code)
	}
}

func TestM2M_JWTBearer_ClaimMapping(t *testing.T) {
	server := newM2MES256(t)
	clientKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	pemStr := ecPublicKeyToPEM(t, &clientKey.PublicKey)

	// Map external claim "user_id" → local claim "ext_user".
	claimMapping := map[string]string{"user_id": "ext_user"}
	if err := server.AddTrustedKeyFromPEM("client-svc", pemStr, nil, claimMapping); err != nil {
		t.Fatalf("AddTrustedKeyFromPEM: %v", err)
	}

	claims := jwt.MapClaims{
		"iss":     "client-svc",
		"sub":     "client-svc",
		"aud":     "test-issuer",
		"iat":     time.Now().Unix(),
		"exp":     time.Now().Add(5 * time.Minute).Unix(),
		"user_id": "u-42",
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	assertion, err := tok.SignedString(clientKey)
	if err != nil {
		t.Fatalf("sign assertion: %v", err)
	}

	params := url.Values{
		"grant_type": {GrantTypeJWTBearer},
		"assertion":  {assertion},
	}
	w := postToken(t, server, params)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Parse the issued access token to verify claim mapping was applied.
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	accessToken, _ := resp["access_token"].(string)
	if accessToken == "" {
		t.Fatal("no access_token in response")
	}

	// Parse unverified to inspect claims.
	parser := new(jwt.Parser)
	parsed, _, err := parser.ParseUnverified(accessToken, jwt.MapClaims{})
	if err != nil {
		t.Fatalf("parse issued token: %v", err)
	}
	issuedClaims, _ := parsed.Claims.(jwt.MapClaims)

	if issuedClaims["ext_user"] != "u-42" {
		t.Errorf("expected ext_user=u-42 in issued token, got %v", issuedClaims["ext_user"])
	}
	if _, exists := issuedClaims["user_id"]; exists {
		t.Error("expected user_id to be removed by claim mapping")
	}
}

// --- DefaultExpiry / issuer defaults ---

func TestM2M_DefaultExpiry(t *testing.T) {
	m := NewM2MAuthModule("m2m", "this-is-a-valid-secret-32-bytes!", 0, "")
	if m.tokenExpiry != time.Hour {
		t.Errorf("expected default 1h expiry, got %v", m.tokenExpiry)
	}
	if m.issuer != "workflow" {
		t.Errorf("expected default issuer 'workflow', got %q", m.issuer)
	}
}

// --- token claims ---

func TestM2M_TokenClaims_Issuer(t *testing.T) {
	m := newM2MHS256(t)

	params := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {"test-client"},
		"client_secret": {"test-secret"},
	}
	w := postToken(t, m, params)
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	tokenStr, _ := resp["access_token"].(string)

	_, claims, _ := m.Authenticate(tokenStr)
	if claims["iss"] != "test-issuer" {
		t.Errorf("expected iss=test-issuer, got %v", claims["iss"])
	}
}

func TestM2M_RegisterClient(t *testing.T) {
	m := newM2MHS256(t)
	m.RegisterClient(M2MClient{
		ClientID:     "new-client",
		ClientSecret: "new-secret-long-enough", //nolint:gosec // test credential
		Scopes:       []string{"read"},
	})

	m.mu.RLock()
	c, ok := m.clients["new-client"]
	m.mu.RUnlock()

	if !ok {
		t.Fatal("expected new client to be registered")
	}
	if c.ClientID != "new-client" {
		t.Errorf("expected clientID 'new-client', got %q", c.ClientID)
	}
}

// --- JWK thumbprint used for key ID ---

func TestM2M_JWKSKeyID(t *testing.T) {
	m := newM2MES256(t)

	req := httptest.NewRequest(http.MethodGet, "/oauth/jwks", nil)
	w := httptest.NewRecorder()
	m.Handle(w, req)

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	keys := resp["keys"].([]any)
	jwk := keys[0].(map[string]any)

	kid, _ := jwk["kid"].(string)
	if kid == "" {
		t.Error("expected non-empty kid in JWK")
	}
}

// --- base64url encoding sanity check ---

func TestM2M_ecPublicKeyToJWK_CoordinatesDecodable(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	jwk, err := ecPublicKeyToJWK(&key.PublicKey, "kid")
	if err != nil {
		t.Fatalf("ecPublicKeyToJWK: %v", err)
	}

	xStr, _ := jwk["x"].(string)
	yStr, _ := jwk["y"].(string)

	xBytes, err := base64.RawURLEncoding.DecodeString(xStr)
	if err != nil {
		t.Fatalf("decode x: %v", err)
	}
	yBytes, err := base64.RawURLEncoding.DecodeString(yStr)
	if err != nil {
		t.Fatalf("decode y: %v", err)
	}
	if len(xBytes) != 32 {
		t.Errorf("expected x to be 32 bytes, got %d", len(xBytes))
	}
	if len(yBytes) != 32 {
		t.Errorf("expected y to be 32 bytes, got %d", len(yBytes))
	}

	// Reconstructed key should match original via ECDH bytes.
	pub, err := jwkToECPublicKey(jwk)
	if err != nil {
		t.Fatalf("jwkToECPublicKey: %v", err)
	}
	origECDH, err := key.PublicKey.ECDH()
	if err != nil {
		t.Fatalf("original key ECDH: %v", err)
	}
	reconECDH, err := pub.ECDH()
	if err != nil {
		t.Fatalf("reconstructed key ECDH: %v", err)
	}
	if string(origECDH.Bytes()) != string(reconECDH.Bytes()) {
		t.Error("round-trip key mismatch")
	}
}

// Test that oauthError returns the expected structure.
func TestM2M_oauthError(t *testing.T) {
	e := oauthError("invalid_client", "bad creds")
	if e["error"] != "invalid_client" {
		t.Errorf("expected error=invalid_client, got %q", e["error"])
	}
	if e["error_description"] != "bad creds" {
		t.Errorf("expected error_description='bad creds', got %q", e["error_description"])
	}
}

// Test that issueToken doesn't override iss/sub with extraClaims.
func TestM2M_IssueToken_ProtectedClaims(t *testing.T) {
	m := newM2MHS256(t)
	extra := map[string]any{
		"iss":    "evil-issuer",
		"sub":    "evil-sub",
		"custom": "value",
	}
	tokenStr, err := m.issueToken("legit-subject", nil, extra)
	if err != nil {
		t.Fatalf("issueToken: %v", err)
	}
	_, claims, _ := m.Authenticate(tokenStr)
	if claims["iss"] != "test-issuer" {
		t.Errorf("iss should not be overridable, got %v", claims["iss"])
	}
	if claims["sub"] != "legit-subject" {
		t.Errorf("sub should not be overridable, got %v", claims["sub"])
	}
	if claims["custom"] != "value" {
		t.Errorf("expected custom claim to be passed through, got %v", claims["custom"])
	}
}

// Verify the JWT-bearer grant passes through extra claims from the assertion.
func TestM2M_JWTBearer_ExtraClaimsPassedThrough(t *testing.T) {
	m := newM2MHS256(t)

	claims := jwt.MapClaims{
		"sub":      "svc",
		"exp":      time.Now().Add(5 * time.Minute).Unix(),
		"team":     "platform",
		"tenantId": "acme",
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	assertion, _ := tok.SignedString([]byte("this-is-a-valid-secret-32-bytes!"))

	params := url.Values{
		"grant_type": {GrantTypeJWTBearer},
		"assertion":  {assertion},
	}
	w := postToken(t, m, params)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	tokenStr, _ := resp["access_token"].(string)

	_, issuedClaims, _ := m.Authenticate(tokenStr)
	if issuedClaims["team"] != "platform" {
		t.Errorf("expected team=platform, got %v", issuedClaims["team"])
	}
	if issuedClaims["tenantId"] != "acme" {
		t.Errorf("expected tenantId=acme, got %v", issuedClaims["tenantId"])
	}
}

// Test RequiresServices returns nil.
func TestM2M_RequiresServices(t *testing.T) {
	m := newM2MHS256(t)
	if deps := m.RequiresServices(); deps != nil {
		t.Errorf("expected nil deps, got %v", deps)
	}
}

// Test that the JWKS response has content-type application/json.
func TestM2M_Handle_ContentTypeJSON(t *testing.T) {
	m := newM2MES256(t)
	req := httptest.NewRequest(http.MethodGet, "/oauth/jwks", nil)
	w := httptest.NewRecorder()
	m.Handle(w, req)

	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("expected application/json content-type, got %q", ct)
	}
}

// Test a client with no scopes gets empty scope in response.
func TestM2M_ClientCredentials_NoClientScopes(t *testing.T) {
	m := NewM2MAuthModule("m2m", "this-is-a-valid-secret-32-bytes!", time.Hour, "issuer")
	m.RegisterClient(M2MClient{
		ClientID:     "no-scope-client",
		ClientSecret: "no-scope-secret", //nolint:gosec // test credential
		Scopes:       nil,               // no scopes configured
	})

	params := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {"no-scope-client"},
		"client_secret": {"no-scope-secret"},
	}
	w := postToken(t, m, params)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	// No scopes → empty scope value.
	scopeVal, _ := resp["scope"].(string)
	if scopeVal != "" {
		t.Errorf("expected empty scope for no-scope client, got %q", scopeVal)
	}
}

// Verify that the issued token sub matches the client_id.
func TestM2M_ClientCredentials_SubMatchesClientID(t *testing.T) {
	m := newM2MHS256(t)

	params := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {"test-client"},
		"client_secret": {"test-secret"},
	}
	w := postToken(t, m, params)
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	tokenStr, _ := resp["access_token"].(string)

	_, claims, _ := m.Authenticate(tokenStr)
	if claims["sub"] != "test-client" {
		t.Errorf("expected sub=test-client, got %v", claims["sub"])
	}
}

// --- per-client custom claims ---

// TestM2M_ClientCredentials_CustomClaimsInToken verifies that a client's Claims
// map is included in the issued access token.
func TestM2M_ClientCredentials_CustomClaimsInToken(t *testing.T) {
	m := NewM2MAuthModule("m2m", "this-is-a-valid-secret-32-bytes!", time.Hour, "test-issuer")
	m.RegisterClient(M2MClient{
		ClientID:     "org-alpha",
		ClientSecret: "secret-org-alpha", //nolint:gosec // test credential
		Scopes:       []string{"read"},
		Claims:       map[string]any{"tenant_id": "alpha"},
	})

	params := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {"org-alpha"},
		"client_secret": {"secret-org-alpha"},
	}
	w := postToken(t, m, params)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	tokenStr, _ := resp["access_token"].(string)

	_, claims, err := m.Authenticate(tokenStr)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if claims["tenant_id"] != "alpha" {
		t.Errorf("expected tenant_id=alpha, got %v", claims["tenant_id"])
	}
}

// TestM2M_ClientCredentials_MultipleCustomClaims verifies that multiple custom
// claims are all present in the issued token.
func TestM2M_ClientCredentials_MultipleCustomClaims(t *testing.T) {
	m := NewM2MAuthModule("m2m", "this-is-a-valid-secret-32-bytes!", time.Hour, "test-issuer")
	m.RegisterClient(M2MClient{
		ClientID:     "org-beta",
		ClientSecret: "secret-org-beta", //nolint:gosec // test credential
		Scopes:       []string{"read", "write"},
		Claims: map[string]any{
			"tenant_id":    "beta",
			"affiliate_id": "partner-42",
		},
	})

	params := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {"org-beta"},
		"client_secret": {"secret-org-beta"},
	}
	w := postToken(t, m, params)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	tokenStr, _ := resp["access_token"].(string)

	_, claims, err := m.Authenticate(tokenStr)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if claims["tenant_id"] != "beta" {
		t.Errorf("expected tenant_id=beta, got %v", claims["tenant_id"])
	}
	if claims["affiliate_id"] != "partner-42" {
		t.Errorf("expected affiliate_id=partner-42, got %v", claims["affiliate_id"])
	}
}

// TestM2M_ClientCredentials_CustomClaimsDoNotOverrideStandard verifies that
// custom claims on a client cannot override standard JWT claims.
func TestM2M_ClientCredentials_CustomClaimsDoNotOverrideStandard(t *testing.T) {
	m := NewM2MAuthModule("m2m", "this-is-a-valid-secret-32-bytes!", time.Hour, "trusted-issuer")
	m.RegisterClient(M2MClient{
		ClientID:     "attacker",
		ClientSecret: "attacker-secret-here", //nolint:gosec // test credential
		Scopes:       []string{"read"},
		Claims: map[string]any{
			"iss": "evil-issuer",
			"sub": "admin",
		},
	})

	params := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {"attacker"},
		"client_secret": {"attacker-secret-here"},
	}
	w := postToken(t, m, params)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	tokenStr, _ := resp["access_token"].(string)

	_, claims, err := m.Authenticate(tokenStr)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	// Standard claims must not be overridden by client.Claims.
	if claims["iss"] != "trusted-issuer" {
		t.Errorf("iss must not be overridable via client claims, got %v", claims["iss"])
	}
	if claims["sub"] != "attacker" {
		t.Errorf("sub must not be overridable via client claims, got %v", claims["sub"])
	}
}

// TestM2M_ClientCredentials_NilClaimsOK verifies that a client with nil Claims
// still issues tokens without error.
func TestM2M_ClientCredentials_NilClaimsOK(t *testing.T) {
	m := NewM2MAuthModule("m2m", "this-is-a-valid-secret-32-bytes!", time.Hour, "test-issuer")
	m.RegisterClient(M2MClient{
		ClientID:     "plain-client",
		ClientSecret: "plain-client-secret!", //nolint:gosec // test credential
		Scopes:       []string{"read"},
		Claims:       nil,
	})

	params := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {"plain-client"},
		"client_secret": {"plain-client-secret!"},
	}
	w := postToken(t, m, params)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
}

// --- JTI blacklist expiry and GC ---

// TestM2M_JTIBlacklist_ExpiredEntryPurged verifies that the in-memory blacklist
// automatically removes JTI entries once the token's expiry has passed.
func TestM2M_JTIBlacklist_ExpiredEntryPurged(t *testing.T) {
	m := newM2MHS256(t) // 1-hour token expiry

	tokenStr := issueTestToken(t, m, "test-client", "test-secret")

	// Revoke the token via the endpoint (adds JTI with real future expiry).
	w := postRevoke(t, m, url.Values{"token": {tokenStr}})
	if w.Code != http.StatusOK {
		t.Fatalf("revoke failed: got %d; body: %s", w.Code, w.Body.String())
	}

	// Artificially backdate the entry to simulate the token having expired.
	m.mu.Lock()
	for jti := range m.jtiBlacklist {
		m.jtiBlacklist[jti] = time.Now().Add(-time.Hour) // already expired
	}
	m.mu.Unlock()

	// Revoke another token — the next write purges the expired entry.
	newTok := issueTestToken(t, m, "test-client", "test-secret")
	w2 := postRevoke(t, m, url.Values{"token": {newTok}})
	if w2.Code != http.StatusOK {
		t.Fatalf("second revoke failed: got %d", w2.Code)
	}

	m.mu.RLock()
	blacklistLen := len(m.jtiBlacklist)
	m.mu.RUnlock()

	// Only the freshly revoked token should remain; the backdated one was purged.
	if blacklistLen != 1 {
		t.Errorf("expected 1 blacklist entry after GC, got %d", blacklistLen)
	}
}

// TestM2M_JTIBlacklist_GrowsBoundedByExpiry verifies that the blacklist does not
// accumulate stale entries: each write purges any JTIs whose expiry has passed.
func TestM2M_JTIBlacklist_GrowsBoundedByExpiry(t *testing.T) {
	m := newM2MHS256(t) // 1-hour token expiry

	const numTokens = 5
	for i := 0; i < numTokens; i++ {
		tok := issueTestToken(t, m, "test-client", "test-secret")
		w := postRevoke(t, m, url.Values{"token": {tok}})
		if w.Code != http.StatusOK {
			t.Fatalf("revoke[%d] failed: got %d", i, w.Code)
		}
	}

	// All 5 should be in the blacklist (not yet expired).
	m.mu.RLock()
	sizeBeforeExpiry := len(m.jtiBlacklist)
	m.mu.RUnlock()
	if sizeBeforeExpiry != numTokens {
		t.Errorf("expected %d blacklist entries before expiry, got %d", numTokens, sizeBeforeExpiry)
	}

	// Backdate all existing entries to simulate having expired.
	m.mu.Lock()
	for jti := range m.jtiBlacklist {
		m.jtiBlacklist[jti] = time.Now().Add(-time.Hour)
	}
	m.mu.Unlock()

	// Revoking one more token triggers purge.
	newTok := issueTestToken(t, m, "test-client", "test-secret")
	w := postRevoke(t, m, url.Values{"token": {newTok}})
	if w.Code != http.StatusOK {
		t.Fatalf("final revoke failed: got %d", w.Code)
	}

	m.mu.RLock()
	sizeAfterExpiry := len(m.jtiBlacklist)
	m.mu.RUnlock()

	// Only the freshly revoked token (future expiry) should remain.
	if sizeAfterExpiry != 1 {
		t.Errorf("expected 1 blacklist entry after GC, got %d", sizeAfterExpiry)
	}
}

// --- DB-backed TokenRevocationStore ---

// sqliteRevocationStore is an example TokenRevocationStore backed by a SQL
// database (SQLite here; swap the driver and DDL for PostgreSQL or MySQL in
// production). It demonstrates how to implement the TokenRevocationStore
// interface so that revocations survive process restarts.
type sqliteRevocationStore struct {
	mu sync.Mutex
	db *sql.DB
}

// newSQLiteRevocationStore opens an in-memory SQLite database and creates the
// revoked_tokens table. Cleanup is registered via t.Cleanup.
func newSQLiteRevocationStore(t *testing.T) *sqliteRevocationStore {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.ExecContext(context.Background(), `
		CREATE TABLE IF NOT EXISTS revoked_tokens (
			jti     TEXT    PRIMARY KEY,
			expires INTEGER NOT NULL  -- Unix timestamp
		)`)
	if err != nil {
		t.Fatalf("create revoked_tokens table: %v", err)
	}
	return &sqliteRevocationStore{db: db}
}

// RevokeToken inserts the JTI into the database.
// If it already exists the insert is silently ignored (idempotent).
func (s *sqliteRevocationStore) RevokeToken(_ context.Context, jti string, expiry time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO revoked_tokens (jti, expires) VALUES (?, ?)`,
		jti, expiry.Unix(),
	)
	return err
}

// IsRevoked reports whether the JTI is present and its stored expiry is still
// in the future (i.e. the token could still be used if it weren't revoked).
func (s *sqliteRevocationStore) IsRevoked(_ context.Context, jti string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var expires int64
	err := s.db.QueryRow(
		`SELECT expires FROM revoked_tokens WHERE jti = ?`, jti,
	).Scan(&expires)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	// Only treat as revoked if the token hasn't already expired naturally.
	return time.Now().Unix() < expires, nil
}

// revokedJTIs returns all JTIs currently stored in the SQLite database (test helper).
func (s *sqliteRevocationStore) revokedJTIs(t *testing.T) []string {
	t.Helper()
	rows, err := s.db.Query(`SELECT jti FROM revoked_tokens`)
	if err != nil {
		t.Fatalf("query revoked_tokens: %v", err)
	}
	defer rows.Close()
	var jtis []string
	for rows.Next() {
		var jti string
		if err := rows.Scan(&jti); err != nil {
			t.Fatalf("scan jti: %v", err)
		}
		jtis = append(jtis, jti)
	}
	return jtis
}

// TestM2M_Revoke_DBStore_PersistsRevocation demonstrates that when a
// TokenRevocationStore is attached, POST /oauth/revoke also writes the JTI to
// the database.
func TestM2M_Revoke_DBStore_PersistsRevocation(t *testing.T) {
	m := newM2MHS256(t)
	store := newSQLiteRevocationStore(t)
	m.SetRevocationStore(store)

	tokenStr := issueTestToken(t, m, "test-client", "test-secret")

	// Confirm the token is valid before revocation.
	valid, claims, err := m.Authenticate(tokenStr)
	if err != nil || !valid {
		t.Fatalf("expected valid token before revocation")
	}
	jti, _ := claims["jti"].(string)
	if jti == "" {
		t.Fatal("expected non-empty jti in token")
	}

	// Revoke via the HTTP endpoint.
	w := postRevoke(t, m, url.Values{"token": {tokenStr}})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	// Verify the JTI was written to the database.
	storedJTIs := store.revokedJTIs(t)
	if len(storedJTIs) != 1 || storedJTIs[0] != jti {
		t.Errorf("expected JTI %q in DB, got %v", jti, storedJTIs)
	}

	// Authenticate must now reject the token.
	valid, _, _ = m.Authenticate(tokenStr)
	if valid {
		t.Error("expected token to be invalid after revocation")
	}
}

// TestM2M_Revoke_DBStore_ReloadedModuleRespects demonstrates that a fresh
// M2MAuthModule (simulating a process restart with an empty in-memory blacklist)
// still rejects previously revoked tokens when backed by the same persistent store.
func TestM2M_Revoke_DBStore_ReloadedModuleRespects(t *testing.T) {
	store := newSQLiteRevocationStore(t)

	// First module instance: issues and revokes a token.
	m1 := newM2MHS256(t)
	m1.SetRevocationStore(store)

	tokenStr := issueTestToken(t, m1, "test-client", "test-secret")
	w := postRevoke(t, m1, url.Values{"token": {tokenStr}})
	if w.Code != http.StatusOK {
		t.Fatalf("revoke failed: got %d", w.Code)
	}

	// Second module instance — same secret and DB store, but a fresh empty
	// in-memory blacklist, as would occur after a process restart.
	m2 := NewM2MAuthModule("m2m", "this-is-a-valid-secret-32-bytes!", time.Hour, "test-issuer")
	m2.RegisterClient(M2MClient{
		ClientID:     "test-client",
		ClientSecret: "test-secret", //nolint:gosec // test credential
		Scopes:       []string{"read", "write"},
	})
	m2.SetRevocationStore(store)

	m2.mu.RLock()
	inMemLen := len(m2.jtiBlacklist)
	m2.mu.RUnlock()
	if inMemLen != 0 {
		t.Fatalf("expected empty in-memory blacklist on fresh module, got %d entries", inMemLen)
	}

	// Authenticate via the fresh module must still reject the revoked token.
	valid, _, authErr := m2.Authenticate(tokenStr)
	if authErr != nil {
		t.Fatalf("unexpected error: %v", authErr)
	}
	if valid {
		t.Error("expected token to be rejected by fresh module using DB-backed store")
	}
}

// TestM2M_Revoke_DBStore_MultipleTokens verifies that multiple revocations each
// produce a distinct database row.
func TestM2M_Revoke_DBStore_MultipleTokens(t *testing.T) {
	m := newM2MHS256(t)
	store := newSQLiteRevocationStore(t)
	m.SetRevocationStore(store)

	const numTokens = 3
	for i := 0; i < numTokens; i++ {
		tok := issueTestToken(t, m, "test-client", "test-secret")
		w := postRevoke(t, m, url.Values{"token": {tok}})
		if w.Code != http.StatusOK {
			t.Fatalf("revoke[%d] failed: got %d", i, w.Code)
		}
	}

	storedJTIs := store.revokedJTIs(t)
	if len(storedJTIs) != numTokens {
		t.Errorf("expected %d DB rows, got %d", numTokens, len(storedJTIs))
	}
}

// --- Custom endpoint path configuration ---

func TestM2M_DefaultEndpointPaths(t *testing.T) {
	defaults := DefaultM2MEndpointPaths()
	if defaults.Token != "/oauth/token" {
		t.Errorf("expected Token=/oauth/token, got %q", defaults.Token)
	}
	if defaults.Revoke != "/oauth/revoke" {
		t.Errorf("expected Revoke=/oauth/revoke, got %q", defaults.Revoke)
	}
	if defaults.Introspect != "/oauth/introspect" {
		t.Errorf("expected Introspect=/oauth/introspect, got %q", defaults.Introspect)
	}
	if defaults.JWKS != "/oauth/jwks" {
		t.Errorf("expected JWKS=/oauth/jwks, got %q", defaults.JWKS)
	}
}

func TestM2M_SetEndpoints_CustomPaths(t *testing.T) {
	m := newM2MHS256(t)
	if err := m.SetEndpoints(M2MEndpointPaths{
		Token:      "/v2/oauth/token",
		Revoke:     "/oauth/token/revoke",
		Introspect: "/oauth/token/introspect",
		JWKS:       "/v2/oauth/jwks",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m.mu.RLock()
	ep := m.endpointPaths
	m.mu.RUnlock()
	if ep.Token != "/v2/oauth/token" {
		t.Errorf("expected /v2/oauth/token, got %q", ep.Token)
	}
	if ep.Revoke != "/oauth/token/revoke" {
		t.Errorf("expected /oauth/token/revoke, got %q", ep.Revoke)
	}
	if ep.Introspect != "/oauth/token/introspect" {
		t.Errorf("expected /oauth/token/introspect, got %q", ep.Introspect)
	}
	if ep.JWKS != "/v2/oauth/jwks" {
		t.Errorf("expected /v2/oauth/jwks, got %q", ep.JWKS)
	}
}

func TestM2M_SetEndpoints_EmptyFieldsPreserveDefaults(t *testing.T) {
	m := newM2MHS256(t)
	// Only override Revoke; other paths should remain at defaults.
	if err := m.SetEndpoints(M2MEndpointPaths{
		Revoke: "/oauth/token/revoke",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m.mu.RLock()
	ep := m.endpointPaths
	m.mu.RUnlock()
	if ep.Token != "/oauth/token" {
		t.Errorf("Token should remain default, got %q", ep.Token)
	}
	if ep.Revoke != "/oauth/token/revoke" {
		t.Errorf("expected /oauth/token/revoke, got %q", ep.Revoke)
	}
	if ep.Introspect != "/oauth/introspect" {
		t.Errorf("Introspect should remain default, got %q", ep.Introspect)
	}
}

func TestM2M_SetEndpoints_ValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		paths   M2MEndpointPaths
		wantErr string
	}{
		{
			name:    "missing leading slash",
			paths:   M2MEndpointPaths{Token: "oauth/token"},
			wantErr: "must start with '/'",
		},
		{
			name: "duplicate paths",
			paths: M2MEndpointPaths{
				Token:      "/oauth/token",
				Revoke:     "/oauth/token",
				Introspect: "/oauth/introspect",
				JWKS:       "/oauth/jwks",
			},
			wantErr: "share the same path",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newM2MHS256(t)
			err := m.SetEndpoints(tc.paths)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("expected error containing %q, got %q", tc.wantErr, err.Error())
			}
		})
	}
}

func TestM2M_SetEndpoints_InvalidPath_DoesNotMutateState(t *testing.T) {
	m := newM2MHS256(t)
	// Attempt an invalid override (no leading slash).
	err := m.SetEndpoints(M2MEndpointPaths{Token: "bad-path"})
	if err == nil {
		t.Fatal("expected validation error")
	}
	// Existing defaults must be intact.
	m.mu.RLock()
	ep := m.endpointPaths
	m.mu.RUnlock()
	if ep.Token != "/oauth/token" {
		t.Errorf("expected original default /oauth/token to be preserved, got %q", ep.Token)
	}
}

func TestM2M_Init_ValidatesEndpointPaths(t *testing.T) {
	// Directly corrupt endpointPaths to simulate a misconfiguration that
	// bypassed SetEndpoints (e.g. zero-value struct).
	m := newM2MHS256(t)
	m.endpointPaths.Token = "no-leading-slash"
	if err := m.Init(nil); err == nil {
		t.Error("expected Init to reject invalid endpoint path")
	}
}

func TestM2M_CustomTokenPath_Issues_Token(t *testing.T) {
	m := newM2MHS256(t)
	if err := m.SetEndpoints(M2MEndpointPaths{Token: "/v2/oauth/token"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v2/oauth/token",
		strings.NewReader(url.Values{
			"grant_type":    {"client_credentials"},
			"client_id":     {"test-client"},
			"client_secret": {"test-secret"},
		}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	m.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestM2M_OldTokenPath_Returns404_WhenOverridden(t *testing.T) {
	m := newM2MHS256(t)
	if err := m.SetEndpoints(M2MEndpointPaths{Token: "/v2/oauth/token"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/oauth/token",
		strings.NewReader(url.Values{
			"grant_type":    {"client_credentials"},
			"client_id":     {"test-client"},
			"client_secret": {"test-secret"},
		}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	m.Handle(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 on old path after override, got %d", w.Code)
	}
}

func TestM2M_CustomRevokePath_Fosite_Style(t *testing.T) {
	m := newM2MHS256(t)
	if err := m.SetEndpoints(M2MEndpointPaths{
		Revoke:     "/oauth/token/revoke",
		Introspect: "/oauth/token/introspect",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tok := issueTestToken(t, m, "test-client", "test-secret")

	// Revoke via Fosite-style path.
	req := httptest.NewRequest(http.MethodPost, "/oauth/token/revoke",
		strings.NewReader(url.Values{"token": {tok}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("test-client", "test-secret")
	w := httptest.NewRecorder()
	m.Handle(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on revoke, got %d; body: %s", w.Code, w.Body.String())
	}

	// Old /oauth/revoke should now return 404.
	req2 := httptest.NewRequest(http.MethodPost, "/oauth/revoke",
		strings.NewReader(url.Values{"token": {tok}}.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req2.SetBasicAuth("test-client", "test-secret")
	w2 := httptest.NewRecorder()
	m.Handle(w2, req2)
	if w2.Code != http.StatusNotFound {
		t.Errorf("expected 404 on old /oauth/revoke, got %d", w2.Code)
	}
}

func TestM2M_CustomIntrospectPath_Fosite_Style(t *testing.T) {
	m := newM2MHS256(t)
	if err := m.SetEndpoints(M2MEndpointPaths{
		Introspect: "/oauth/token/introspect",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m.SetIntrospectPolicy(true, "", "", "")

	tok := issueTestToken(t, m, "test-client", "test-secret")

	req := httptest.NewRequest(http.MethodPost, "/oauth/token/introspect",
		strings.NewReader(url.Values{"token": {tok}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("test-client", "test-secret")
	w := httptest.NewRecorder()
	m.Handle(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if active, _ := resp["active"].(bool); !active {
		t.Errorf("expected active=true, got %v", resp)
	}
}
