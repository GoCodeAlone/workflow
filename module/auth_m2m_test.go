package module

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
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

	if stored == nil {
		t.Error("expected key to be stored")
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
