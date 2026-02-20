package oidc

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Config holds OIDC provider configuration.
type Config struct {
	Issuer       string   `json:"issuer" yaml:"issuer"`
	ClientID     string   `json:"client_id" yaml:"client_id"`
	ClientSecret string   `json:"client_secret" yaml:"client_secret"` //nolint:gosec // G117: OIDC config field
	RedirectURI  string   `json:"redirect_uri" yaml:"redirect_uri"`
	Scopes       []string `json:"scopes" yaml:"scopes"`
}

// Validate checks that required configuration fields are set.
func (c Config) Validate() error {
	if c.Issuer == "" {
		return errors.New("oidc: issuer is required")
	}
	if c.ClientID == "" {
		return errors.New("oidc: client_id is required")
	}
	return nil
}

// DiscoveryDocument represents the OpenID Connect discovery response.
type DiscoveryDocument struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	UserInfoEndpoint      string   `json:"userinfo_endpoint"`
	JWKSURI               string   `json:"jwks_uri"`
	ScopesSupported       []string `json:"scopes_supported"`
}

// Claims represents the standard claims extracted from an ID token.
type Claims struct {
	Subject       string   `json:"sub"`
	Email         string   `json:"email"`
	EmailVerified bool     `json:"email_verified"`
	Name          string   `json:"name"`
	Groups        []string `json:"groups,omitempty"`
	Issuer        string   `json:"iss"`
	Audience      string   `json:"aud"`
	ExpiresAt     int64    `json:"exp"`
	IssuedAt      int64    `json:"iat"`
}

// TokenResponse represents the response from the token endpoint.
type TokenResponse struct {
	AccessToken  string `json:"access_token"` //nolint:gosec // G117: OIDC token response field
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"` //nolint:gosec // G117: OIDC token response field
	IDToken      string `json:"id_token,omitempty"`
}

// HTTPClient is the interface for making HTTP requests (allows testing).
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Provider handles OIDC authentication flows.
type Provider struct {
	config    Config
	discovery *DiscoveryDocument
	client    HTTPClient
	mu        sync.RWMutex
}

// NewProvider creates a new OIDC provider with the given configuration.
func NewProvider(cfg Config, client HTTPClient) (*Provider, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{"openid", "profile", "email"}
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &Provider{
		config: cfg,
		client: client,
	}, nil
}

// Discover fetches the OIDC discovery document from the issuer.
func (p *Provider) Discover(ctx context.Context) (*DiscoveryDocument, error) {
	p.mu.RLock()
	if p.discovery != nil {
		d := p.discovery
		p.mu.RUnlock()
		return d, nil
	}
	p.mu.RUnlock()

	url := strings.TrimRight(p.config.Issuer, "/") + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("oidc: failed to create discovery request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oidc: discovery request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oidc: discovery endpoint returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("oidc: failed to read discovery response: %w", err)
	}

	var doc DiscoveryDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("oidc: failed to parse discovery document: %w", err)
	}

	p.mu.Lock()
	p.discovery = &doc
	p.mu.Unlock()

	return &doc, nil
}

// AuthorizationURL builds the URL to redirect users for authentication.
func (p *Provider) AuthorizationURL(ctx context.Context, state string) (string, error) {
	doc, err := p.Discover(ctx)
	if err != nil {
		return "", err
	}

	params := fmt.Sprintf(
		"%s?client_id=%s&redirect_uri=%s&response_type=code&scope=%s&state=%s",
		doc.AuthorizationEndpoint,
		p.config.ClientID,
		p.config.RedirectURI,
		strings.Join(p.config.Scopes, "+"),
		state,
	)
	return params, nil
}

// ExchangeCode exchanges an authorization code for tokens.
func (p *Provider) ExchangeCode(ctx context.Context, code string) (*TokenResponse, error) {
	doc, err := p.Discover(ctx)
	if err != nil {
		return nil, err
	}

	body := fmt.Sprintf(
		"grant_type=authorization_code&code=%s&redirect_uri=%s&client_id=%s&client_secret=%s",
		code,
		p.config.RedirectURI,
		p.config.ClientID,
		p.config.ClientSecret,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, doc.TokenEndpoint, strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("oidc: failed to create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oidc: token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oidc: token endpoint returned status %d", resp.StatusCode)
	}

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("oidc: failed to read token response: %w", err)
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return nil, fmt.Errorf("oidc: failed to parse token response: %w", err)
	}

	return &tokenResp, nil
}

// ParseIDTokenUnverified extracts claims from an ID token without cryptographic
// verification. Use this only when you have already validated the token via
// the token endpoint response.
func ParseIDTokenUnverified(idToken string) (*Claims, error) {
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	token, _, err := parser.ParseUnverified(idToken, jwt.MapClaims{})
	if err != nil {
		return nil, fmt.Errorf("oidc: failed to parse ID token: %w", err)
	}

	mapClaims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("oidc: unexpected claims type")
	}

	claims := &Claims{}
	if sub, ok := mapClaims["sub"].(string); ok {
		claims.Subject = sub
	}
	if email, ok := mapClaims["email"].(string); ok {
		claims.Email = email
	}
	if verified, ok := mapClaims["email_verified"].(bool); ok {
		claims.EmailVerified = verified
	}
	if name, ok := mapClaims["name"].(string); ok {
		claims.Name = name
	}
	if iss, ok := mapClaims["iss"].(string); ok {
		claims.Issuer = iss
	}
	if aud, ok := mapClaims["aud"].(string); ok {
		claims.Audience = aud
	}
	if exp, ok := mapClaims["exp"].(float64); ok {
		claims.ExpiresAt = int64(exp)
	}
	if iat, ok := mapClaims["iat"].(float64); ok {
		claims.IssuedAt = int64(iat)
	}
	if groups, ok := mapClaims["groups"].([]any); ok {
		for _, g := range groups {
			if s, ok := g.(string); ok {
				claims.Groups = append(claims.Groups, s)
			}
		}
	}

	return claims, nil
}

// GenerateState produces a cryptographically random state parameter.
func GenerateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("oidc: failed to generate state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// CallbackHandler returns an HTTP handler that processes the OIDC authorization
// code callback. On success it calls onSuccess with the extracted claims.
func (p *Provider) CallbackHandler(onSuccess func(w http.ResponseWriter, r *http.Request, claims *Claims, tokens *TokenResponse)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing authorization code", http.StatusBadRequest)
			return
		}

		errParam := r.URL.Query().Get("error")
		if errParam != "" {
			desc := r.URL.Query().Get("error_description")
			http.Error(w, fmt.Sprintf("authorization error: %s: %s", errParam, desc), http.StatusBadRequest)
			return
		}

		tokens, err := p.ExchangeCode(r.Context(), code)
		if err != nil {
			http.Error(w, "failed to exchange authorization code", http.StatusInternalServerError)
			return
		}

		if tokens.IDToken == "" {
			http.Error(w, "no ID token in response", http.StatusInternalServerError)
			return
		}

		claims, err := ParseIDTokenUnverified(tokens.IDToken)
		if err != nil {
			http.Error(w, "failed to parse ID token", http.StatusInternalServerError)
			return
		}

		onSuccess(w, r, claims, tokens)
	}
}

// Config returns the provider's configuration.
func (p *Provider) Config() Config {
	return p.config
}
