package module

import (
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/golang-jwt/jwt/v5"
)

// GrantType constants for OAuth2 M2M flows.
const (
	GrantTypeClientCredentials = "client_credentials"
	//nolint:gosec // G101: This is an OAuth2 grant type name, not a credential value.
	GrantTypeJWTBearer = "urn:ietf:params:oauth:grant-type:jwt-bearer"
)

// SigningAlgorithm defines the JWT signing algorithm for the M2M module.
type SigningAlgorithm string

const (
	// SigningAlgHS256 uses HMAC-SHA256 (symmetric).
	SigningAlgHS256 SigningAlgorithm = "HS256"
	// SigningAlgES256 uses ECDSA P-256 (asymmetric).
	SigningAlgES256 SigningAlgorithm = "ES256"
)

// M2MClient represents a registered machine-to-machine OAuth2 client.
type M2MClient struct {
	ClientID     string   `json:"clientId"`
	ClientSecret string   `json:"clientSecret"` //nolint:gosec // G117: config DTO field
	Description  string   `json:"description,omitempty"`
	Scopes       []string `json:"scopes,omitempty"`
}

// M2MAuthModule provides machine-to-machine (server-to-server) OAuth2 authentication.
// It supports the client_credentials grant and the JWT-bearer grant, and can issue
// tokens signed with either HS256 (shared secret) or ES256 (ECDSA P-256).
type M2MAuthModule struct {
	name        string
	algorithm   SigningAlgorithm
	issuer      string
	tokenExpiry time.Duration

	// initErr holds an error from factory-time key setup (e.g. SetECDSAKey/GenerateECDSAKey),
	// which is surfaced in Init() since module factories cannot return errors.
	initErr error

	// HS256 fields
	hmacSecret []byte

	// ES256 fields
	privateKey *ecdsa.PrivateKey
	publicKey  *ecdsa.PublicKey

	// Trusted public keys for JWT-bearer grant (keyed by key ID or issuer)
	trustedKeys map[string]*ecdsa.PublicKey

	// Registered clients
	mu      sync.RWMutex
	clients map[string]*M2MClient // keyed by ClientID
}

// NewM2MAuthModule creates a new M2MAuthModule with HS256 signing.
// Use SetECDSAKey or GenerateECDSAKey to switch to ES256 signing.
func NewM2MAuthModule(name string, hmacSecret string, tokenExpiry time.Duration, issuer string) *M2MAuthModule {
	if tokenExpiry <= 0 {
		tokenExpiry = time.Hour
	}
	if issuer == "" {
		issuer = "workflow"
	}
	m := &M2MAuthModule{
		name:        name,
		algorithm:   SigningAlgHS256,
		issuer:      issuer,
		tokenExpiry: tokenExpiry,
		hmacSecret:  []byte(hmacSecret),
		trustedKeys: make(map[string]*ecdsa.PublicKey),
		clients:     make(map[string]*M2MClient),
	}
	return m
}

// GenerateECDSAKey generates a new P-256 key pair and switches the module to ES256 signing.
func (m *M2MAuthModule) GenerateECDSAKey() error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate ECDSA key: %w", err)
	}
	m.privateKey = key
	m.publicKey = &key.PublicKey
	m.algorithm = SigningAlgES256
	return nil
}

// SetECDSAKey loads a PEM-encoded EC private key and switches the module to ES256 signing.
// Only P-256 keys are accepted; other curves are rejected.
func (m *M2MAuthModule) SetECDSAKey(pemKey string) error {
	block, _ := pem.Decode([]byte(pemKey))
	if block == nil {
		return fmt.Errorf("failed to decode PEM block")
	}
	key, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("parse EC private key: %w", err)
	}
	if key.Curve != elliptic.P256() {
		return fmt.Errorf("unsupported ECDSA curve: got %s, want P-256", key.Curve.Params().Name)
	}
	m.privateKey = key
	m.publicKey = &key.PublicKey
	m.algorithm = SigningAlgES256
	return nil
}

// SetInitErr stores a deferred initialization error to be returned by Init().
// This is used by factory functions which cannot return errors directly.
func (m *M2MAuthModule) SetInitErr(err error) {
	m.initErr = err
}

// AddTrustedKey registers a trusted ECDSA public key for JWT-bearer assertion validation.
// The keyID is used to look up the key; it can be an issuer name or any unique identifier.
func (m *M2MAuthModule) AddTrustedKey(keyID string, pubKey *ecdsa.PublicKey) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.trustedKeys[keyID] = pubKey
}

// RegisterClient registers a new OAuth2 client.
func (m *M2MAuthModule) RegisterClient(client M2MClient) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clients[client.ClientID] = &client
}

// Name returns the module name.
func (m *M2MAuthModule) Name() string { return m.name }

// Init validates the module configuration. It also surfaces any key-setup error
// that occurred in the factory (stored in initErr).
func (m *M2MAuthModule) Init(_ modular.Application) error {
	if m.initErr != nil {
		return fmt.Errorf("M2M auth: key setup failed: %w", m.initErr)
	}
	if m.algorithm == SigningAlgHS256 && len(m.hmacSecret) < 32 {
		return fmt.Errorf("M2M auth: HMAC secret must be at least 32 bytes for HS256")
	}
	if m.algorithm == SigningAlgES256 && m.privateKey == nil {
		return fmt.Errorf("M2M auth: ECDSA private key required for ES256")
	}
	return nil
}

// ProvidesServices returns the services provided by this module.
func (m *M2MAuthModule) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        m.name,
			Description: "Machine-to-machine OAuth2 auth module (client_credentials + jwt-bearer)",
			Instance:    m,
		},
	}
}

// RequiresServices returns an empty list (no external dependencies).
func (m *M2MAuthModule) RequiresServices() []modular.ServiceDependency { return nil }

// Handle routes M2M OAuth2 requests.
//
// Routes:
//
//	POST /oauth/token  — token endpoint (client_credentials + jwt-bearer grants)
//	GET  /oauth/jwks   — JSON Web Key Set (ES256 public key)
func (m *M2MAuthModule) Handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	path := r.URL.Path
	switch {
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/oauth/token"):
		m.handleToken(w, r)
	case r.Method == http.MethodGet && strings.HasSuffix(path, "/oauth/jwks"):
		m.handleJWKS(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "not found"})
	}
}

// handleToken implements RFC 6749 § 4.4 (client_credentials) and
// RFC 7523 § 2.1 (jwt-bearer assertion) token endpoints.
func (m *M2MAuthModule) handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(oauthError("invalid_request", "failed to parse form"))
		return
	}

	grantType := r.FormValue("grant_type")
	switch grantType {
	case GrantTypeClientCredentials:
		m.handleClientCredentials(w, r)
	case GrantTypeJWTBearer:
		m.handleJWTBearer(w, r)
	default:
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(oauthError("unsupported_grant_type",
			fmt.Sprintf("grant_type %q not supported; use %q or %q",
				grantType, GrantTypeClientCredentials, GrantTypeJWTBearer)))
	}
}

// handleClientCredentials processes the OAuth2 client_credentials grant.
// Clients send client_id + client_secret (either as form params or HTTP Basic auth)
// and receive a signed access token.
func (m *M2MAuthModule) handleClientCredentials(w http.ResponseWriter, r *http.Request) {
	clientID, clientSecret, ok := m.extractClientCredentials(r)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(oauthError("invalid_client", "client credentials required"))
		return
	}

	client, err := m.authenticateClient(clientID, clientSecret)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(oauthError("invalid_client", err.Error()))
		return
	}

	// Validate requested scopes against client's allowed scopes.
	requestedScope := r.FormValue("scope")
	grantedScopes, err := m.validateScopes(client, requestedScope)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(oauthError("invalid_scope", err.Error()))
		return
	}

	token, err := m.issueToken(clientID, grantedScopes, nil)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(oauthError("server_error", "failed to issue token"))
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": token,
		"token_type":   "Bearer",
		"expires_in":   int(m.tokenExpiry.Seconds()),
		"scope":        strings.Join(grantedScopes, " "),
	})
}

// handleJWTBearer processes the JWT-bearer grant (RFC 7523).
// The client sends a signed JWT assertion; if the signature is valid and the
// assertion is trusted, an access token is returned.
func (m *M2MAuthModule) handleJWTBearer(w http.ResponseWriter, r *http.Request) {
	assertion := r.FormValue("assertion")
	if assertion == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(oauthError("invalid_request", "assertion is required"))
		return
	}

	claims, err := m.validateJWTAssertion(assertion)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(oauthError("invalid_grant", err.Error()))
		return
	}

	// The subject (sub) becomes the client identity in the issued token.
	subject, _ := claims["sub"].(string)
	if subject == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(oauthError("invalid_grant", "assertion missing sub claim"))
		return
	}

	requestedScope := r.FormValue("scope")
	var grantedScopes []string
	if requestedScope != "" {
		grantedScopes = strings.Fields(requestedScope)
	}

	token, err := m.issueToken(subject, grantedScopes, claims)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(oauthError("server_error", "failed to issue token"))
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": token,
		"token_type":   "Bearer",
		"expires_in":   int(m.tokenExpiry.Seconds()),
		"scope":        strings.Join(grantedScopes, " "),
	})
}

// handleJWKS returns the JSON Web Key Set containing the module's public key(s).
// Only available when the module is configured for ES256.
func (m *M2MAuthModule) handleJWKS(w http.ResponseWriter, _ *http.Request) {
	if m.algorithm != SigningAlgES256 || m.publicKey == nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "JWKS not available: algorithm must be ES256 with a configured public key",
		})
		return
	}

	jwk, err := ecPublicKeyToJWK(m.publicKey, m.name+"-key")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(oauthError("server_error", "failed to generate JWK for ES256 public key"))
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"keys": []any{jwk},
	})
}

// --- token issuance ---

// issueToken creates and signs a JWT access token.
// extraClaims are merged in (e.g., from a jwt-bearer assertion).
func (m *M2MAuthModule) issueToken(subject string, scopes []string, extraClaims map[string]any) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"iss": m.issuer,
		"sub": subject,
		"iat": now.Unix(),
		"exp": now.Add(m.tokenExpiry).Unix(),
	}
	if len(scopes) > 0 {
		claims["scope"] = strings.Join(scopes, " ")
	}
	// Merge extra claims, but never let them override standard fields.
	for k, v := range extraClaims {
		switch k {
		case "iss", "sub", "iat", "exp", "scope":
			// protected — skip
		default:
			claims[k] = v
		}
	}

	switch m.algorithm {
	case SigningAlgES256:
		tok := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
		return tok.SignedString(m.privateKey)
	default: // HS256
		tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		return tok.SignedString(m.hmacSecret)
	}
}

// --- authentication helpers ---

// extractClientCredentials returns client_id and client_secret from either
// HTTP Basic Auth or the request form body (per RFC 6749 § 2.3).
func (m *M2MAuthModule) extractClientCredentials(r *http.Request) (string, string, bool) {
	// Prefer HTTP Basic Auth.
	if clientID, clientSecret, ok := r.BasicAuth(); ok && clientID != "" {
		return clientID, clientSecret, true
	}
	// Fall back to form params.
	clientID := r.FormValue("client_id")
	clientSecret := r.FormValue("client_secret")
	if clientID != "" && clientSecret != "" {
		return clientID, clientSecret, true
	}
	return "", "", false
}

// authenticateClient looks up and verifies a client by ID and secret.
func (m *M2MAuthModule) authenticateClient(clientID, clientSecret string) (*M2MClient, error) {
	m.mu.RLock()
	client, ok := m.clients[clientID]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("client not found")
	}

	// Compare fixed-length SHA-256 hashes to keep the comparison constant-time
	// regardless of whether the provided secret length differs from the stored one,
	// since subtle.ConstantTimeCompare returns early when lengths differ.
	storedHash := sha256.Sum256([]byte(client.ClientSecret))
	providedHash := sha256.Sum256([]byte(clientSecret))
	if subtle.ConstantTimeCompare(storedHash[:], providedHash[:]) != 1 {
		return nil, fmt.Errorf("invalid client secret")
	}

	return client, nil
}

// validateScopes checks that all requested scopes are permitted for the client.
// If no scopes are requested, the client's full scope list is granted.
func (m *M2MAuthModule) validateScopes(client *M2MClient, requestedScope string) ([]string, error) {
	if requestedScope == "" {
		return client.Scopes, nil
	}

	requested := strings.Fields(requestedScope)
	allowed := make(map[string]bool, len(client.Scopes))
	for _, s := range client.Scopes {
		allowed[s] = true
	}

	for _, s := range requested {
		if !allowed[s] {
			return nil, fmt.Errorf("scope %q not permitted for this client", s)
		}
	}
	return requested, nil
}

// validateJWTAssertion parses and validates a JWT bearer assertion (RFC 7523).
// It first parses the assertion unverified to extract the `iss` claim and the
// `kid` header, then selects the matching trusted key, and verifies the signature
// with that specific key.  This prevents a holder of any trusted key from
// impersonating an arbitrary subject.
func (m *M2MAuthModule) validateJWTAssertion(assertion string) (jwt.MapClaims, error) {
	// Parse unverified to extract iss/kid for key selection.
	unverified, _, err := new(jwt.Parser).ParseUnverified(assertion, jwt.MapClaims{})
	if err != nil {
		return nil, fmt.Errorf("malformed assertion: %w", err)
	}
	uClaims, ok := unverified.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("malformed assertion claims")
	}
	iss, _ := uClaims["iss"].(string)
	kid, _ := unverified.Header["kid"].(string)

	m.mu.RLock()
	// Try kid first, then iss.
	var selectedKey *ecdsa.PublicKey
	if kid != "" {
		selectedKey = m.trustedKeys[kid]
	}
	if selectedKey == nil && iss != "" {
		selectedKey = m.trustedKeys[iss]
	}
	hmacSecret := m.hmacSecret
	m.mu.RUnlock()

	// Try EC key if found.
	if selectedKey != nil {
		k := selectedKey
		token, err := jwt.Parse(assertion, func(token *jwt.Token) (any, error) {
			if _, ok := token.Method.(*jwt.SigningMethodECDSA); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return k, nil
		}, jwt.WithExpirationRequired())
		if err != nil {
			return nil, fmt.Errorf("invalid assertion: %w", err)
		}
		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok || !token.Valid {
			return nil, fmt.Errorf("invalid assertion claims")
		}
		return claims, nil
	}

	// Fall back to HS256 using the module's own secret (for internal/testing use).
	// The assertion must be signed with the module's exact secret.
	if len(hmacSecret) >= 32 {
		token, err := jwt.Parse(assertion, func(token *jwt.Token) (any, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return hmacSecret, nil
		}, jwt.WithExpirationRequired())
		if err != nil {
			return nil, fmt.Errorf("invalid assertion: %w", err)
		}
		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok || !token.Valid {
			return nil, fmt.Errorf("invalid assertion claims")
		}
		return claims, nil
	}

	return nil, fmt.Errorf("no trusted key found for assertion issuer %q", iss)
}

// Authenticate implements the AuthProvider interface so M2MAuthModule can be
// used as a provider in AuthMiddleware.  It validates the token's signature
// using the configured algorithm and returns the embedded claims.
func (m *M2MAuthModule) Authenticate(tokenStr string) (bool, map[string]any, error) {
	var token *jwt.Token
	var err error

	switch m.algorithm {
	case SigningAlgES256:
		if m.publicKey == nil {
			return false, nil, fmt.Errorf("no ECDSA public key configured")
		}
		token, err = jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodECDSA); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return m.publicKey, nil
		})
	default: // HS256
		token, err = jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return m.hmacSecret, nil
		})
	}

	if err != nil {
		return false, nil, nil //nolint:nilerr // Invalid token is a failed auth, not an error
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return false, nil, nil
	}

	result := make(map[string]any, len(claims))
	for k, v := range claims {
		result[k] = v
	}
	return true, result, nil
}

// --- JWKS helpers ---

// ecPublicKeyToJWK converts an ECDSA P-256 public key to a JWK (RFC 7517) map.
// It uses the ecdh package to extract the uncompressed point bytes, avoiding
// the deprecated ecdsa.PublicKey.X / .Y big.Int fields.
// Returns an error if the key cannot be converted.
func ecPublicKeyToJWK(pub *ecdsa.PublicKey, kid string) (map[string]any, error) {
	ecdhPub, err := pub.ECDH()
	if err != nil {
		return nil, fmt.Errorf("convert to ECDH key: %w", err)
	}
	// Uncompressed point format for P-256: 0x04 || x (32 bytes) || y (32 bytes) = 65 bytes.
	b := ecdhPub.Bytes()
	if len(b) != 65 || b[0] != 0x04 {
		return nil, fmt.Errorf("unexpected uncompressed point length %d or prefix 0x%02x (want 65, 0x04)", len(b), b[0])
	}
	x := b[1:33]
	y := b[33:65]
	return map[string]any{
		"kty": "EC",
		"crv": "P-256",
		"alg": "ES256",
		"use": "sig",
		"kid": kid,
		"x":   base64.RawURLEncoding.EncodeToString(x),
		"y":   base64.RawURLEncoding.EncodeToString(y),
	}, nil
}

// jwkThumbprint computes the JWK thumbprint (RFC 7638) for an EC P-256 key.
// This is useful for deriving deterministic key IDs.
func jwkThumbprint(pub *ecdsa.PublicKey) string {
	ecdhPub, err := pub.ECDH()
	if err != nil {
		return ""
	}
	b := ecdhPub.Bytes()
	if len(b) != 65 || b[0] != 0x04 {
		return ""
	}
	x := base64.RawURLEncoding.EncodeToString(b[1:33])
	y := base64.RawURLEncoding.EncodeToString(b[33:65])
	// RFC 7638: lexicographic JSON of required members.
	//nolint:gocritic // sprintfQuotedString: %s is required here; %q would add extra escaping
	raw := fmt.Sprintf(`{"crv":"P-256","kty":"EC","x":"%s","y":"%s"}`, x, y)
	h := sha256.Sum256([]byte(raw))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// jwkToECPublicKey converts a JWK map (EC P-256) back to an *ecdsa.PublicKey.
// Returns an error if the JWK is not a valid EC P-256 key.
func jwkToECPublicKey(jwk map[string]any) (*ecdsa.PublicKey, error) {
	kty, _ := jwk["kty"].(string)
	if kty != "EC" {
		return nil, fmt.Errorf("expected kty=EC, got %q", kty)
	}
	crv, _ := jwk["crv"].(string)
	if crv != "P-256" {
		return nil, fmt.Errorf("expected crv=P-256, got %q", crv)
	}
	xStr, _ := jwk["x"].(string)
	yStr, _ := jwk["y"].(string)
	if xStr == "" || yStr == "" {
		return nil, fmt.Errorf("missing x or y coordinate in JWK")
	}
	xBytes, err := base64.RawURLEncoding.DecodeString(xStr)
	if err != nil {
		return nil, fmt.Errorf("decode x: %w", err)
	}
	yBytes, err := base64.RawURLEncoding.DecodeString(yStr)
	if err != nil {
		return nil, fmt.Errorf("decode y: %w", err)
	}
	if len(xBytes) != 32 || len(yBytes) != 32 {
		return nil, fmt.Errorf("invalid P-256 coordinate length: x=%d y=%d", len(xBytes), len(yBytes))
	}
	// Construct uncompressed point: 0x04 || x || y
	uncompressed := make([]byte, 65)
	uncompressed[0] = 0x04
	copy(uncompressed[1:33], xBytes)
	copy(uncompressed[33:65], yBytes)

	// Parse via ecdh, then convert to ecdsa via PKIX round-trip.
	ecdhPub, err := ecdh.P256().NewPublicKey(uncompressed)
	if err != nil {
		return nil, fmt.Errorf("parse uncompressed point: %w", err)
	}
	pkixBytes, err := x509.MarshalPKIXPublicKey(ecdhPub)
	if err != nil {
		return nil, fmt.Errorf("marshal PKIX: %w", err)
	}
	pub, err := x509.ParsePKIXPublicKey(pkixBytes)
	if err != nil {
		return nil, fmt.Errorf("parse PKIX: %w", err)
	}
	ecdsaPub, ok := pub.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("unexpected key type %T", pub)
	}
	return ecdsaPub, nil
}

// oauthError builds an RFC 6749-compliant error response body.
func oauthError(code, description string) map[string]string {
	return map[string]string{
		"error":             code,
		"error_description": description,
	}
}
