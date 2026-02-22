package iam

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/GoCodeAlone/workflow/store"
)

const (
	discoveryTTL = time.Hour
	jwksTTL      = time.Hour
)

// OIDCConfig holds configuration for the OIDC provider.
type OIDCConfig struct {
	Issuer       string `json:"issuer"`
	ClientID     string `json:"client_id"`
	ClaimKey     string `json:"claim_key,omitempty"` // Which claim to use as the external identifier (e.g. "sub", "email")
	JWKSURL      string `json:"jwks_url,omitempty"`
	DiscoveryURL string `json:"discovery_url,omitempty"`
}

// discoveryDoc represents an OIDC discovery document.
type discoveryDoc struct {
	Issuer                string `json:"issuer"`
	JWKSURI               string `json:"jwks_uri"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
}

// jwkKey represents a single key in a JWKS document.
type jwkKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
	Alg string `json:"alg"`
	Use string `json:"use"`
}

// jwksDocument represents a JWKS document.
type jwksDocument struct {
	Keys []jwkKey `json:"keys"`
}

// cachedDiscovery holds a cached discovery document with its fetch time.
type cachedDiscovery struct {
	doc       *discoveryDoc
	fetchedAt time.Time
}

// cachedKeys holds a cached set of JWKS public keys with their fetch time.
type cachedKeys struct {
	keys      map[string]*rsa.PublicKey
	fetchedAt time.Time
}

// OIDCProvider maps OIDC claims to roles using OIDC discovery and JWT validation.
type OIDCProvider struct {
	// HTTPClient is the HTTP client used for OIDC discovery and JWKS requests.
	// If nil, http.DefaultClient is used.
	HTTPClient *http.Client

	mu        sync.Mutex
	discCache map[string]cachedDiscovery
	jwksCache map[string]cachedKeys
}

func (p *OIDCProvider) httpClient() *http.Client {
	if p.HTTPClient != nil {
		return p.HTTPClient
	}
	return http.DefaultClient
}

func (p *OIDCProvider) Type() store.IAMProviderType {
	return store.IAMProviderOIDC
}

func (p *OIDCProvider) ValidateConfig(config json.RawMessage) error {
	var c OIDCConfig
	if err := json.Unmarshal(config, &c); err != nil {
		return fmt.Errorf("invalid oidc config: %w", err)
	}
	if c.Issuer == "" {
		return fmt.Errorf("issuer is required")
	}
	if c.ClientID == "" {
		return fmt.Errorf("client_id is required")
	}
	return nil
}

// discover fetches the OIDC discovery document for the given config, using cache when available.
func (p *OIDCProvider) discover(ctx context.Context, c OIDCConfig) (*discoveryDoc, error) {
	p.mu.Lock()
	if p.discCache == nil {
		p.discCache = make(map[string]cachedDiscovery)
	}
	cached, ok := p.discCache[c.Issuer]
	p.mu.Unlock()

	if ok && time.Since(cached.fetchedAt) < discoveryTTL {
		return cached.doc, nil
	}

	discURL := c.DiscoveryURL
	if discURL == "" {
		discURL = strings.TrimRight(c.Issuer, "/") + "/.well-known/openid-configuration"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create discovery request: %w", err)
	}

	resp, err := p.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch discovery document: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discovery request returned status %d", resp.StatusCode)
	}

	var doc discoveryDoc
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode discovery document: %w", err)
	}

	p.mu.Lock()
	p.discCache[c.Issuer] = cachedDiscovery{doc: &doc, fetchedAt: time.Now()}
	p.mu.Unlock()

	return &doc, nil
}

// fetchJWKS retrieves the JWKS from the given URL and parses RSA public keys.
func (p *OIDCProvider) fetchJWKS(ctx context.Context, jwksURL string) (map[string]*rsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create JWKS request: %w", err)
	}

	resp, err := p.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch JWKS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JWKS request returned status %d", resp.StatusCode)
	}

	var doc jwksDocument
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode JWKS: %w", err)
	}

	keys := make(map[string]*rsa.PublicKey)
	for _, k := range doc.Keys {
		if k.Kty != "RSA" {
			continue
		}
		pubKey, err := jwkToRSAPublicKey(k)
		if err != nil {
			continue
		}
		keys[k.Kid] = pubKey
	}

	return keys, nil
}

// getJWKS returns the cached JWKS for the given URL, fetching it if needed.
func (p *OIDCProvider) getJWKS(ctx context.Context, jwksURL string) (map[string]*rsa.PublicKey, error) {
	p.mu.Lock()
	if p.jwksCache == nil {
		p.jwksCache = make(map[string]cachedKeys)
	}
	cached, ok := p.jwksCache[jwksURL]
	p.mu.Unlock()

	if ok && time.Since(cached.fetchedAt) < jwksTTL {
		return cached.keys, nil
	}

	keys, err := p.fetchJWKS(ctx, jwksURL)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	p.jwksCache[jwksURL] = cachedKeys{keys: keys, fetchedAt: time.Now()}
	p.mu.Unlock()

	return keys, nil
}

// keyFunc returns a jwt.Keyfunc that looks up RSA public keys from the JWKS, with refresh on unknown kid.
func (p *OIDCProvider) keyFunc(ctx context.Context, jwksURL string) jwt.Keyfunc {
	return func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}

		kid, _ := token.Header["kid"].(string)

		keys, err := p.getJWKS(ctx, jwksURL)
		if err != nil {
			return nil, err
		}

		key, ok := keys[kid]
		if !ok {
			// Key not found – re-fetch to handle key rotation.
			keys, err = p.fetchJWKS(ctx, jwksURL)
			if err != nil {
				return nil, err
			}
			p.mu.Lock()
			p.jwksCache[jwksURL] = cachedKeys{keys: keys, fetchedAt: time.Now()}
			p.mu.Unlock()

			key, ok = keys[kid]
			if !ok {
				return nil, fmt.Errorf("key %q not found in JWKS", kid)
			}
		}

		return key, nil
	}
}

// resolveFromToken validates the JWT id_token and extracts the configured claim.
func (p *OIDCProvider) resolveFromToken(ctx context.Context, c OIDCConfig, idToken string) ([]ExternalIdentity, error) {
	jwksURL := c.JWKSURL
	if jwksURL == "" {
		doc, err := p.discover(ctx, c)
		if err != nil {
			return nil, fmt.Errorf("oidc discovery failed: %w", err)
		}
		jwksURL = doc.JWKSURI
	}

	parser := jwt.NewParser(
		jwt.WithAudience(c.ClientID),
		jwt.WithIssuer(c.Issuer),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
		jwt.WithValidMethods([]string{"RS256", "RS384", "RS512"}),
	)

	token, err := parser.ParseWithClaims(idToken, jwt.MapClaims{}, p.keyFunc(ctx, jwksURL))
	if err != nil {
		return nil, fmt.Errorf("validate token: %w", err)
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("invalid token claims")
	}

	claimKey := c.ClaimKey
	if claimKey == "" {
		claimKey = "sub"
	}

	claimValue, ok := claims[claimKey]
	if !ok {
		return nil, fmt.Errorf("claim %q not found in token", claimKey)
	}

	claimStr := fmt.Sprintf("%v", claimValue)

	return []ExternalIdentity{
		{
			Provider:   string(store.IAMProviderOIDC),
			Identifier: claimStr,
			Attributes: map[string]string{claimKey: claimStr},
		},
	}, nil
}

func (p *OIDCProvider) ResolveIdentities(ctx context.Context, config json.RawMessage, credentials map[string]string) ([]ExternalIdentity, error) {
	var c OIDCConfig
	if err := json.Unmarshal(config, &c); err != nil {
		return nil, fmt.Errorf("invalid oidc config: %w", err)
	}

	// If an id_token is present, perform full JWT validation.
	if idToken, ok := credentials["id_token"]; ok && idToken != "" {
		return p.resolveFromToken(ctx, c, idToken)
	}

	// Fallback: extract claims directly from the credentials map.
	claimKey := c.ClaimKey
	if claimKey == "" {
		claimKey = "sub"
	}

	claimValue, ok := credentials[claimKey]
	if !ok || claimValue == "" {
		return nil, fmt.Errorf("claim %q not found in credentials", claimKey)
	}

	return []ExternalIdentity{
		{
			Provider:   string(store.IAMProviderOIDC),
			Identifier: claimValue,
			Attributes: map[string]string{claimKey: claimValue},
		},
	}, nil
}

func (p *OIDCProvider) TestConnection(ctx context.Context, config json.RawMessage) error {
	if err := p.ValidateConfig(config); err != nil {
		return err
	}

	var c OIDCConfig
	if err := json.Unmarshal(config, &c); err != nil {
		return fmt.Errorf("invalid oidc config: %w", err)
	}

	// Perform OIDC discovery.
	doc, err := p.discover(ctx, c)
	if err != nil {
		return fmt.Errorf("oidc discovery failed: %w", err)
	}

	// Verify the JWKS endpoint is reachable.
	jwksURL := c.JWKSURL
	if jwksURL == "" {
		jwksURL = doc.JWKSURI
	}

	if _, err := p.getJWKS(ctx, jwksURL); err != nil {
		return fmt.Errorf("jwks endpoint unreachable: %w", err)
	}

	return nil
}

// jwkToRSAPublicKey converts a JWK key entry to an *rsa.PublicKey.
func jwkToRSAPublicKey(k jwkKey) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("decode JWK n: %w", err)
	}

	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("decode JWK e: %w", err)
	}

	n := new(big.Int).SetBytes(nBytes)
	e := new(big.Int).SetBytes(eBytes)

	return &rsa.PublicKey{
		N: n,
		E: int(e.Int64()),
	}, nil
}
