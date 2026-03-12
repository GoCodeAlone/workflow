package module

import (
	"context"
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

	"github.com/GoCodeAlone/modular"
	"github.com/golang-jwt/jwt/v5"
)

// TokenRevocationStore is an optional persistence backend for token revocations.
// Implementations can persist revoked JTIs (e.g., in a relational database) so
// that revocations survive process restarts.
//
// Both methods receive a context so implementations can honour timeouts and
// propagate cancellations.
type TokenRevocationStore interface {
	// RevokeToken persists the revocation of the given JTI.
	// expiry is the token's exp time; implementations should use it to avoid
	// accumulating entries for tokens that have already expired naturally.
	RevokeToken(ctx context.Context, jti string, expiry time.Time) error
	// IsRevoked reports whether the given JTI has been revoked.
	IsRevoked(ctx context.Context, jti string) (bool, error)
}

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
	ClientID     string         `json:"clientId"`
	ClientSecret string         `json:"clientSecret"` //nolint:gosec // G117: config DTO field
	Description  string         `json:"description,omitempty"`
	Scopes       []string       `json:"scopes,omitempty"`
	Claims       map[string]any `json:"claims,omitempty"`
}

// M2MEndpointPaths configures the URL path suffixes for the OAuth2 endpoints
// exposed by the M2M auth module. Each field is matched using strings.HasSuffix
// against the incoming request path, so a prefix such as /api/v1 is allowed.
//
// The zero value is not useful; use DefaultM2MEndpointPaths() to obtain defaults.
type M2MEndpointPaths struct {
	// Token is the path suffix for the token endpoint (default: /oauth/token).
	Token string
	// Revoke is the path suffix for the revocation endpoint (default: /oauth/revoke).
	Revoke string
	// Introspect is the path suffix for the introspection endpoint (default: /oauth/introspect).
	Introspect string
	// JWKS is the path suffix for the JWKS endpoint (default: /oauth/jwks).
	JWKS string
}

// DefaultM2MEndpointPaths returns the default OAuth2 endpoint path suffixes.
func DefaultM2MEndpointPaths() M2MEndpointPaths {
	return M2MEndpointPaths{ //nolint:gosec // G101: These are URL paths, not credentials.
		Token:      "/oauth/token",
		Revoke:     "/oauth/revoke",
		Introspect: "/oauth/introspect",
		JWKS:       "/oauth/jwks",
	}
}

// TrustedKeyConfig holds the configuration for a trusted external JWT issuer.
// It is used to register trusted keys for the JWT-bearer grant via YAML configuration.
type TrustedKeyConfig struct {
	// Issuer is the expected `iss` claim value (e.g. "https://legacy-platform.example.com").
	Issuer string `json:"issuer" yaml:"issuer"`
	// Algorithm is the expected signing algorithm (e.g. "ES256"). Currently only ES256 is supported.
	Algorithm string `json:"algorithm,omitempty" yaml:"algorithm,omitempty"`
	// PublicKeyPEM is the PEM-encoded EC public key for the trusted issuer.
	// Literal `\n` sequences (common in Docker/Kubernetes env vars) are normalised to newlines.
	PublicKeyPEM string `json:"publicKeyPEM,omitempty" yaml:"publicKeyPEM,omitempty"` //nolint:gosec // G117: config DTO field
	// Audiences is an optional list of accepted audience values.
	// When non-empty, the assertion's `aud` claim must contain at least one of these values.
	Audiences []string `json:"audiences,omitempty" yaml:"audiences,omitempty"`
	// ClaimMapping renames claims from the external assertion before they are included in the
	// issued token.  The map key is the external claim name; the value is the local claim name.
	// For example {"user_id": "sub"} promotes the external `user_id` claim to `sub`.
	ClaimMapping map[string]string `json:"claimMapping,omitempty" yaml:"claimMapping,omitempty"`
}

// trustedKeyEntry is the internal representation of a trusted external JWT issuer.
type trustedKeyEntry struct {
	pubKey       *ecdsa.PublicKey
	audiences    []string
	claimMapping map[string]string
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
	trustedKeys map[string]*trustedKeyEntry

	// Registered clients
	mu           sync.RWMutex
	clients      map[string]*M2MClient // keyed by ClientID
	jtiBlacklist map[string]time.Time  // revoked token JTIs → expiry time

	// Optional pluggable persistence for token revocations.
	revocationStore TokenRevocationStore

	// Configurable OAuth2 endpoint path suffixes.
	endpointPaths M2MEndpointPaths

	// Introspection access-control policy (see SetIntrospectPolicy).
	introspectAllowOthers      bool   // if true, authenticated callers may inspect any token
	introspectRequiredScope    string // scope required in caller's token to inspect others
	introspectRequiredClaim    string // claim key required in caller's token to inspect others
	introspectRequiredClaimVal string // expected value for introspectRequiredClaim (empty = key only)
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
		name:          name,
		algorithm:     SigningAlgHS256,
		issuer:        issuer,
		tokenExpiry:   tokenExpiry,
		hmacSecret:    []byte(hmacSecret),
		trustedKeys:   make(map[string]*trustedKeyEntry),
		clients:       make(map[string]*M2MClient),
		jtiBlacklist:  make(map[string]time.Time),
		endpointPaths: DefaultM2MEndpointPaths(),
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
	m.trustedKeys[keyID] = &trustedKeyEntry{pubKey: pubKey}
}

// AddTrustedKeyFromPEM parses a PEM-encoded EC public key and registers it as a trusted
// key for JWT-bearer assertion validation.  Literal `\n` sequences in the PEM string are
// normalised to real newlines so that env-var-injected keys (Docker/Kubernetes) work without
// additional preprocessing by the caller.
//
// audiences is an optional list; when non-empty the assertion's `aud` claim must match at
// least one entry.  claimMapping renames external claims before they are forwarded into the
// issued token (map key = external name, map value = local name).
func (m *M2MAuthModule) AddTrustedKeyFromPEM(issuer, publicKeyPEM string, audiences []string, claimMapping map[string]string) error {
	// Normalise escaped newlines that are common in Docker/Kubernetes env vars.
	normalised := strings.ReplaceAll(publicKeyPEM, `\n`, "\n")

	block, _ := pem.Decode([]byte(normalised))
	if block == nil {
		return fmt.Errorf("auth.m2m: failed to decode PEM block for issuer %q", issuer)
	}

	pubAny, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return fmt.Errorf("auth.m2m: parse public key for issuer %q: %w", issuer, err)
	}
	ecKey, ok := pubAny.(*ecdsa.PublicKey)
	if !ok {
		return fmt.Errorf("auth.m2m: public key for issuer %q is not an ECDSA key", issuer)
	}
	if ecKey.Curve != elliptic.P256() {
		return fmt.Errorf("auth.m2m: public key for issuer %q must use P-256 (ES256) curve", issuer)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.trustedKeys[issuer] = &trustedKeyEntry{
		pubKey:       ecKey,
		audiences:    audiences,
		claimMapping: claimMapping,
	}
	return nil
}

// RegisterClient registers a new OAuth2 client.
func (m *M2MAuthModule) RegisterClient(client M2MClient) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clients[client.ClientID] = &client
}

// SetIntrospectPolicy configures access control for the POST /oauth/introspect endpoint.
//
// By default (allowOthers=false) only self-inspection is permitted: a caller may only
// introspect its own token (the token being inspected must have the same sub as the
// authenticated caller's identity).
//
// When allowOthers=true, any authenticated caller may introspect any token. Two optional
// prerequisites can narrow this further when the caller authenticates with a Bearer token:
//   - requiredScope: the caller's token must contain this scope (e.g. "introspect:admin").
//   - requiredClaim / requiredClaimVal: the caller's token must have this claim, and if
//     requiredClaimVal is non-empty, the claim value must equal that string.
//
// Callers authenticating via HTTP Basic Auth (client_id + client_secret) are always
// considered admin-level and satisfy any scope/claim requirement when allowOthers=true.
func (m *M2MAuthModule) SetIntrospectPolicy(allowOthers bool, requiredScope, requiredClaim, requiredClaimVal string) {
	m.introspectAllowOthers = allowOthers
	m.introspectRequiredScope = requiredScope
	m.introspectRequiredClaim = requiredClaim
	m.introspectRequiredClaimVal = requiredClaimVal
}

// SetRevocationStore configures an optional persistent backend for token revocations.
// When set, every call to POST /oauth/revoke will also call store.RevokeToken.
// Revocation checks consult the store in addition to the in-memory blacklist, allowing
// revocations to survive process restarts.
//
// The store is called with a background context. Pass nil to remove a previously set store.
func (m *M2MAuthModule) SetRevocationStore(store TokenRevocationStore) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.revocationStore = store
}

// SetEndpoints overrides the URL path suffixes used by Handle() to route incoming
// requests to the token, revocation, introspection, and JWKS sub-handlers.
// Any empty field in paths is left at its current value (defaulting to the standard
// paths set by NewM2MAuthModule).
//
// Example – to match Fosite/Auth0-style paths:
//
//	m.SetEndpoints(M2MEndpointPaths{
//	    Revoke:     "/oauth/token/revoke",
//	    Introspect: "/oauth/token/introspect",
//	})
func (m *M2MAuthModule) SetEndpoints(paths M2MEndpointPaths) {
	if paths.Token != "" {
		m.endpointPaths.Token = paths.Token
	}
	if paths.Revoke != "" {
		m.endpointPaths.Revoke = paths.Revoke
	}
	if paths.Introspect != "" {
		m.endpointPaths.Introspect = paths.Introspect
	}
	if paths.JWKS != "" {
		m.endpointPaths.JWKS = paths.JWKS
	}
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
// Routes (path suffixes are configurable via SetEndpoints):
//
//	POST <endpoints.Token>      — token endpoint (client_credentials + jwt-bearer grants)
//	POST <endpoints.Revoke>     — token revocation (RFC 7009)
//	POST <endpoints.Introspect> — token introspection (RFC 7662)
//	GET  <endpoints.JWKS>       — JSON Web Key Set (ES256 public key)
func (m *M2MAuthModule) Handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	path := r.URL.Path
	switch {
	case r.Method == http.MethodPost && strings.HasSuffix(path, m.endpointPaths.Token):
		m.handleToken(w, r)
	case r.Method == http.MethodPost && strings.HasSuffix(path, m.endpointPaths.Revoke):
		m.handleRevoke(w, r)
	case r.Method == http.MethodPost && strings.HasSuffix(path, m.endpointPaths.Introspect):
		m.handleIntrospect(w, r)
	case r.Method == http.MethodGet && strings.HasSuffix(path, m.endpointPaths.JWKS):
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

	token, err := m.issueToken(clientID, grantedScopes, client.Claims)
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

// handleRevoke implements token revocation per RFC 7009.
// It adds the token's JTI to the in-memory blacklist so that subsequent
// calls to Authenticate or handleIntrospect will treat the token as invalid.
//
// Per RFC 7009 §2.1, the revocation endpoint MUST require client authentication.
// Callers must authenticate via HTTP Basic Auth or form-encoded client_id/client_secret.
// Per RFC 7009 §2.2, if the token is valid and recognised, 200 OK is returned;
// if it is unrecognised or already invalid, 200 OK is still returned.
func (m *M2MAuthModule) handleRevoke(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(oauthError("invalid_request", "failed to parse form"))
		return
	}

	// RFC 7009 §2.1: client authentication is required.
	clientID, clientSecret, hasCredentials := m.extractClientCredentials(r)
	if !hasCredentials {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(oauthError("invalid_client", "client authentication required"))
		return
	}
	if _, err := m.authenticateClient(clientID, clientSecret); err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(oauthError("invalid_client", "invalid client credentials"))
		return
	}

	tokenStr := r.FormValue("token")
	if tokenStr == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(oauthError("invalid_request", "token is required"))
		return
	}
	// Per RFC 7009 §2.2: if the token is unrecognized or already invalid, still return 200.
	// We attempt to parse it; if valid, add its JTI to the blacklist.
	if claims, ok := m.parseTokenClaims(tokenStr); ok {
		if jti, _ := claims["jti"].(string); jti != "" {
			var expiry time.Time
			if expRaw, ok2 := claims["exp"]; ok2 {
				switch v := expRaw.(type) {
				case float64:
					expiry = time.Unix(int64(v), 0)
				case json.Number:
					if n, e := v.Int64(); e == nil {
						expiry = time.Unix(n, 0)
					}
				}
			}
			if expiry.IsZero() {
				expiry = time.Now().Add(m.tokenExpiry)
			}

			m.mu.Lock()
			m.purgeExpiredJTIsLocked()
			m.jtiBlacklist[jti] = expiry
			store := m.revocationStore
			m.mu.Unlock()

			// Persist to external store if configured.
			if store != nil {
				_ = store.RevokeToken(r.Context(), jti, expiry)
			}
		}
	}
	w.WriteHeader(http.StatusOK)
}

// handleIntrospect implements token introspection per RFC 7662.
//
// The caller MUST authenticate before the token under inspection is revealed.
// Two authentication methods are supported:
//   - HTTP Basic Auth (client_id + client_secret): always treated as admin-level.
//   - Bearer token (Authorization: Bearer <token>): the caller's own valid token.
//
// By default (allowOthers=false) a caller may only introspect its own token (the
// token's sub must match the caller's identity). Set the introspect policy via
// SetIntrospectPolicy to allow cross-token inspection with optional scope/claim guards.
func (m *M2MAuthModule) handleIntrospect(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(oauthError("invalid_request", "failed to parse form"))
		return
	}
	tokenStr := r.FormValue("token")
	if tokenStr == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(oauthError("invalid_request", "token is required"))
		return
	}

	// --- Authenticate the caller ---
	callerID, callerClaims, authed := m.authenticateIntrospectCaller(r)
	if !authed {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(oauthError("unauthorized_client", "authentication required to use the introspection endpoint"))
		return
	}

	// --- Validate the token being introspected ---
	claims, ok := m.parseTokenClaims(tokenStr)
	if !ok {
		_ = json.NewEncoder(w).Encode(map[string]any{"active": false})
		return
	}

	// Check JTI blacklist (in-memory + optional persistent store).
	if jti, _ := claims["jti"].(string); jti != "" {
		if m.isJTIRevoked(r.Context(), jti) {
			_ = json.NewEncoder(w).Encode(map[string]any{"active": false})
			return
		}
	}

	// --- Authorization check ---
	tokenSub, _ := claims["sub"].(string)
	if !m.introspectAllowOthers {
		// Default: self-inspection only.
		if callerID != tokenSub {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(oauthError("access_denied", "not authorized to introspect this token"))
			return
		}
	} else {
		// Allow-others mode: enforce optional scope/claim prerequisites.
		if !m.callerMeetsIntrospectPolicy(callerID, callerClaims, tokenSub) {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(oauthError("access_denied", "not authorized to introspect this token"))
			return
		}
	}

	resp := map[string]any{
		"active": true,
	}
	if v, ok2 := claims["sub"].(string); ok2 {
		resp["client_id"] = v
	}
	if v, ok2 := claims["scope"].(string); ok2 {
		resp["scope"] = v
	}
	if v, ok2 := claims["exp"]; ok2 {
		resp["exp"] = v
	}
	if v, ok2 := claims["iat"]; ok2 {
		resp["iat"] = v
	}
	if v, ok2 := claims["iss"].(string); ok2 {
		resp["iss"] = v
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// --- token issuance ---

// issueToken creates and signs a JWT access token.
// extraClaims are merged in (e.g., from a jwt-bearer assertion).
func (m *M2MAuthModule) issueToken(subject string, scopes []string, extraClaims map[string]any) (string, error) {
	now := time.Now()
	jti, err := generateJTI()
	if err != nil {
		return "", fmt.Errorf("generate JTI: %w", err)
	}
	claims := jwt.MapClaims{
		"iss": m.issuer,
		"sub": subject,
		"iat": now.Unix(),
		"exp": now.Add(m.tokenExpiry).Unix(),
		"jti": jti,
	}
	if len(scopes) > 0 {
		claims["scope"] = strings.Join(scopes, " ")
	}
	// Merge extra claims, but never let them override standard fields.
	for k, v := range extraClaims {
		switch k {
		case "iss", "sub", "iat", "exp", "scope", "jti":
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
	var selectedEntry *trustedKeyEntry
	if kid != "" {
		selectedEntry = m.trustedKeys[kid]
	}
	if selectedEntry == nil && iss != "" {
		selectedEntry = m.trustedKeys[iss]
	}
	hmacSecret := m.hmacSecret
	m.mu.RUnlock()

	// Try EC key if found.
	if selectedEntry != nil && selectedEntry.pubKey != nil {
		k := selectedEntry.pubKey
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

		// Validate audience if configured.
		if len(selectedEntry.audiences) > 0 {
			if err := validateAssertionAudience(claims, selectedEntry.audiences); err != nil {
				return nil, err
			}
		}

		// Apply claim mapping if configured.
		if len(selectedEntry.claimMapping) > 0 {
			claims = applyAssertionClaimMapping(claims, selectedEntry.claimMapping)
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
// Tokens whose JTI has been revoked via the /oauth/revoke endpoint are rejected.
func (m *M2MAuthModule) Authenticate(tokenStr string) (bool, map[string]any, error) {
	if m.algorithm == SigningAlgES256 && m.publicKey == nil {
		return false, nil, fmt.Errorf("no ECDSA public key configured")
	}

	claims, ok := m.parseTokenClaims(tokenStr)
	if !ok {
		return false, nil, nil
	}

	// Check JTI blacklist (in-memory + optional persistent store).
	if jti, _ := claims["jti"].(string); jti != "" {
		if m.isJTIRevoked(context.Background(), jti) {
			return false, nil, nil
		}
	}

	result := make(map[string]any, len(claims))
	for k, v := range claims {
		result[k] = v
	}
	return true, result, nil
}

// parseTokenClaims parses and cryptographically validates a token string,
// returning the claims and whether the token is valid.
// It does NOT check the JTI blacklist; callers must do that separately.
func (m *M2MAuthModule) parseTokenClaims(tokenStr string) (jwt.MapClaims, bool) {
	var (
		token *jwt.Token
		err   error
	)

	switch m.algorithm {
	case SigningAlgES256:
		if m.publicKey == nil {
			return nil, false
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
		return nil, false
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, false
	}
	return claims, true
}

// --- JWKS helpers ---

// authenticateIntrospectCaller authenticates the caller of the /oauth/introspect endpoint.
// It tries HTTP Basic Auth (client_id + client_secret) first, then Bearer token.
//
// Returns:
//   - callerID: the authenticated client_id (Basic Auth) or sub claim (Bearer token).
//   - callerClaims: the JWT claims of the caller's Bearer token, or nil for Basic Auth callers.
//   - ok: whether authentication succeeded.
//
// If HTTP Basic Auth credentials are present but invalid, authentication fails immediately
// without falling back to Bearer token.
func (m *M2MAuthModule) authenticateIntrospectCaller(r *http.Request) (callerID string, callerClaims jwt.MapClaims, ok bool) {
	// Try HTTP Basic Auth (client_id + client_secret).
	if clientID, clientSecret, hasBasic := r.BasicAuth(); hasBasic && clientID != "" {
		if _, err := m.authenticateClient(clientID, clientSecret); err == nil {
			return clientID, nil, true
		}
		// Credentials provided but invalid — reject immediately.
		return "", nil, false
	}

	// Try Bearer token in Authorization header.
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
		if claims, valid := m.parseTokenClaims(tokenStr); valid {
			// Reject revoked caller tokens.
			if jti, _ := claims["jti"].(string); jti != "" {
				if m.isJTIRevoked(r.Context(), jti) {
					return "", nil, false
				}
			}
			if sub, _ := claims["sub"].(string); sub != "" {
				return sub, claims, true
			}
		}
		return "", nil, false
	}

	return "", nil, false
}

// callerMeetsIntrospectPolicy reports whether the caller is permitted to inspect a
// token with the given subject under the "allow-others" policy.
//
// Self-inspection (callerID == tokenSub) is always allowed.
// HTTP Basic Auth callers (callerClaims == nil) are treated as admin-level and bypass
// scope/claim prerequisites.
// Bearer token callers must satisfy any configured requiredScope and/or requiredClaim.
func (m *M2MAuthModule) callerMeetsIntrospectPolicy(callerID string, callerClaims jwt.MapClaims, tokenSub string) bool {
	// Self-inspection is always permitted.
	if callerID == tokenSub {
		return true
	}
	// HTTP Basic Auth callers are admin-level.
	if callerClaims == nil {
		return true
	}
	// Bearer token callers: enforce scope prerequisite.
	if m.introspectRequiredScope != "" {
		scopeStr, _ := callerClaims["scope"].(string)
		if !containsScope(scopeStr, m.introspectRequiredScope) {
			return false
		}
	}
	// Enforce claim prerequisite.
	if m.introspectRequiredClaim != "" {
		claimVal, exists := callerClaims[m.introspectRequiredClaim]
		if !exists {
			return false
		}
		if m.introspectRequiredClaimVal != "" && fmt.Sprintf("%v", claimVal) != m.introspectRequiredClaimVal {
			return false
		}
	}
	return true
}

// containsScope reports whether scopeStr (a space-separated list of scopes) contains target.
func containsScope(scopeStr, target string) bool {
	for _, s := range strings.Fields(scopeStr) {
		if s == target {
			return true
		}
	}
	return false
}

// isJTIRevoked checks whether the given JTI has been revoked.
// It consults the in-memory blacklist first (after pruning expired entries),
// then falls back to the optional persistent store.
func (m *M2MAuthModule) isJTIRevoked(ctx context.Context, jti string) bool {
	m.mu.Lock()
	m.purgeExpiredJTIsLocked()
	expiry, inMemory := m.jtiBlacklist[jti]
	store := m.revocationStore
	m.mu.Unlock()

	if inMemory && time.Now().Before(expiry) {
		return true
	}
	if store != nil {
		revoked, err := store.IsRevoked(ctx, jti)
		if err == nil && revoked {
			return true
		}
	}
	return false
}

// purgeExpiredJTIsLocked removes JTIs from the in-memory blacklist whose
// tokens have already expired naturally. It MUST be called with m.mu held for writing.
func (m *M2MAuthModule) purgeExpiredJTIsLocked() {
	now := time.Now()
	for jti, expiry := range m.jtiBlacklist {
		if now.After(expiry) {
			delete(m.jtiBlacklist, jti)
		}
	}
}

// generateJTI generates a random 16-byte JWT ID encoded as base64url.
func generateJTI() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

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

// validateAssertionAudience checks that the JWT claims contain at least one of the
// required audience values.  The `aud` claim can be a single string or a JSON array.
func validateAssertionAudience(claims jwt.MapClaims, requiredAudiences []string) error {
	aud := claims["aud"]
	if aud == nil {
		return fmt.Errorf("assertion missing aud claim, expected one of %v", requiredAudiences)
	}
	var tokenAuds []string
	switch v := aud.(type) {
	case string:
		tokenAuds = []string{v}
	case []any:
		for _, a := range v {
			if s, ok := a.(string); ok {
				tokenAuds = append(tokenAuds, s)
			}
		}
	}
	for _, required := range requiredAudiences {
		for _, tokenAud := range tokenAuds {
			if tokenAud == required {
				return nil
			}
		}
	}
	return fmt.Errorf("assertion audience %v does not include required audience %v", tokenAuds, requiredAudiences)
}

// applyAssertionClaimMapping renames claims from an external assertion before they are
// forwarded into the issued token.  The mapping key is the external claim name; the
// value is the local claim name.  The original claim is removed when the names differ.
func applyAssertionClaimMapping(claims jwt.MapClaims, mapping map[string]string) jwt.MapClaims {
	result := make(jwt.MapClaims, len(claims))
	for k, v := range claims {
		result[k] = v
	}
	for externalKey, localKey := range mapping {
		if val, exists := claims[externalKey]; exists {
			result[localKey] = val
			if externalKey != localKey {
				delete(result, externalKey)
			}
		}
	}
	return result
}
