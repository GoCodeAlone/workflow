package module

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/oauth2"

	"github.com/GoCodeAlone/workflow/secrets"
)

// ---------------------------------------------------------------------------
// oauth2_client_credentials
// ---------------------------------------------------------------------------

// buildOAuth2ClientCredentialsClient constructs an *http.Client that automatically
// fetches and caches an OAuth2 client_credentials token.  The implementation
// intentionally does NOT use golang.org/x/oauth2/clientcredentials so that the
// token cache and 401-retry behaviour are consistent with the rest of this package.
func buildOAuth2ClientCredentialsClient(_ context.Context, auth *HTTPClientAuthConfig, timeout time.Duration) (*http.Client, error) {
	if auth.TokenURL == "" {
		return nil, fmt.Errorf("oauth2_client_credentials: token_url is required")
	}
	if auth.ClientID == "" {
		return nil, fmt.Errorf("oauth2_client_credentials: client_id is required")
	}
	if auth.ClientCredential == "" {
		return nil, fmt.Errorf("oauth2_client_credentials: client_secret is required")
	}

	ts := &clientCredentialsTokenSource{
		tokenURL:         auth.TokenURL,
		clientID:         auth.ClientID,
		clientCredential: auth.ClientCredential, //nolint:gosec // G117: credential passed through to token source
		scopes:           append([]string(nil), auth.Scopes...),
		base:             http.DefaultTransport,
	}
	reuseTS := oauth2.ReuseTokenSource(nil, ts)
	oauth2TR := &oauth2.Transport{Source: reuseTS, Base: http.DefaultTransport}

	return &http.Client{
		Timeout: timeout,
		Transport: &retryOn401Transport{
			underlying: ts,
			oauth2TR:   oauth2TR,
			base:       http.DefaultTransport,
		},
	}, nil
}

// clientCredentialsTokenSource fetches OAuth2 client_credentials tokens.
// It is intentionally stateless; caching is handled by oauth2.ReuseTokenSource.
type clientCredentialsTokenSource struct {
	tokenURL         string
	clientID         string
	clientCredential string //nolint:gosec // G117: credential field in token source
	scopes           []string
	base             http.RoundTripper
}

// Token performs the client_credentials grant and returns the resulting token.
func (ts *clientCredentialsTokenSource) Token() (*oauth2.Token, error) {
	params := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {ts.clientID},
		"client_secret": {ts.clientCredential},
	}
	if len(ts.scopes) > 0 {
		params.Set("scope", strings.Join(ts.scopes, " "))
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, ts.tokenURL,
		strings.NewReader(params.Encode()))
	if err != nil {
		return nil, fmt.Errorf("http.client: failed to build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	transport := ts.base
	if transport == nil {
		transport = http.DefaultTransport
	}
	resp, err := transport.RoundTrip(req)
	if err != nil {
		return nil, fmt.Errorf("http.client: token request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("http.client: failed to read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &oauth2.RetrieveError{
			Response: resp,
			Body:     body,
		}
	}

	var tokenResp struct {
		AccessToken string  `json:"access_token"` //nolint:gosec // G117: parsing OAuth2 response
		ExpiresIn   float64 `json:"expires_in"`
		TokenType   string  `json:"token_type"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("http.client: failed to parse token response: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("http.client: token response missing access_token")
	}

	expiry := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	if tokenResp.ExpiresIn <= 0 {
		expiry = time.Now().Add(3600 * time.Second)
	}

	return &oauth2.Token{
		AccessToken: tokenResp.AccessToken,
		TokenType:   tokenResp.TokenType,
		Expiry:      expiry,
	}, nil
}

// ---------------------------------------------------------------------------
// oauth2_refresh_token
// ---------------------------------------------------------------------------

// buildOAuth2RefreshTokenClient constructs an *http.Client backed by a
// secretsBackedTokenSource.  The module starts cleanly even when tokenProvider
// is nil or has no stored token — the error surfaces on the first HTTP request.
func buildOAuth2RefreshTokenClient(_ context.Context, auth *HTTPClientAuthConfig, tokenProvider secrets.Provider, timeout time.Duration) (*http.Client, error) {
	if auth.TokenURL == "" {
		return nil, fmt.Errorf("oauth2_refresh_token: token_url is required")
	}

	cfg := &oauth2.Config{
		ClientID:     auth.ClientID,
		ClientSecret: auth.ClientCredential, //nolint:gosec // G117: OAuth2 config DTO
		Endpoint:     oauth2.Endpoint{TokenURL: auth.TokenURL},
		Scopes:       append([]string(nil), auth.Scopes...),
	}

	providerKey := auth.TokenProviderKey
	if providerKey == "" {
		providerKey = "oauth_token"
	}

	ts := &secretsBackedTokenSource{
		cfg:         cfg,
		provider:    tokenProvider,
		providerKey: providerKey,
	}
	reuseTS := oauth2.ReuseTokenSource(nil, ts)
	oauth2TR := &oauth2.Transport{Source: reuseTS, Base: http.DefaultTransport}

	return &http.Client{
		Timeout: timeout,
		Transport: &retryOn401Transport{
			underlying: ts,
			oauth2TR:   oauth2TR,
			base:       http.DefaultTransport,
		},
	}, nil
}

// secretsBackedTokenSource implements oauth2.TokenSource.  Each Token() call:
//  1. Reads the serialised oauth2.Token JSON from the secrets provider.
//  2. If the token is still valid (and forceRefresh is not set), returns it as-is.
//  3. If expired (or forceRefresh is set) and a refresh_token exists, calls the
//     token endpoint to refresh, then persists the rotated token back to the provider.
//  4. If not found (secrets.ErrNotFound), returns an *oauth2.RetrieveError with
//     HTTP 401 — the module started cleanly; credentials arrive later.
type secretsBackedTokenSource struct {
	mu           sync.Mutex
	cfg          *oauth2.Config
	provider     secrets.Provider
	providerKey  string
	forceRefresh atomic.Bool // set by invalidate(); cleared after next successful Token()
}

// invalidate marks the token as requiring a refresh on the next Token() call.
// Called by retryOn401Transport when the upstream rejects the current access token.
func (ts *secretsBackedTokenSource) invalidate() {
	ts.forceRefresh.Store(true)
}

// Token satisfies oauth2.TokenSource.
func (ts *secretsBackedTokenSource) Token() (*oauth2.Token, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if ts.provider == nil {
		return nil, noTokenError()
	}

	raw, err := ts.provider.Get(context.Background(), ts.providerKey)
	if err != nil {
		if errors.Is(err, secrets.ErrNotFound) {
			return nil, noTokenError()
		}
		return nil, fmt.Errorf("http.client: reading token from provider: %w", err)
	}

	var stored oauth2.Token
	if unmarshalErr := json.Unmarshal([]byte(raw), &stored); unmarshalErr != nil {
		return nil, fmt.Errorf("http.client: parsing stored token JSON: %w", unmarshalErr)
	}

	// Token still valid and no forced refresh requested — return immediately.
	forced := ts.forceRefresh.Swap(false)
	if stored.Valid() && !forced {
		return &stored, nil
	}

	// Expired or invalidated — attempt refresh if we have a refresh_token.
	if stored.RefreshToken == "" {
		return nil, noTokenError()
	}

	// When forcing refresh, clear the access token to ensure the oauth2 library
	// issues a refresh_token grant rather than returning the cached value.
	if forced {
		stored.AccessToken = ""
		stored.Expiry = time.Time{}
	}

	newTok, refreshErr := ts.cfg.TokenSource(context.Background(), &stored).Token()
	if refreshErr != nil {
		return nil, fmt.Errorf("http.client: refreshing token: %w", refreshErr)
	}

	// Persist rotated token back to the provider.
	if persistErr := ts.persistToken(newTok); persistErr != nil {
		// Log but do not fail — we still have a valid token.
		_ = persistErr // caller has no logger; persistence failure is non-fatal
	}

	return newTok, nil
}

// persistToken serialises tok and writes it to the secrets provider.
func (ts *secretsBackedTokenSource) persistToken(tok *oauth2.Token) error {
	b, err := json.Marshal(tok)
	if err != nil {
		return fmt.Errorf("http.client: marshalling token for persistence: %w", err)
	}
	return ts.provider.Set(context.Background(), ts.providerKey, string(b))
}

// noTokenError returns an *oauth2.RetrieveError signalling that no credentials
// are available.  StatusCode 401 is chosen so callers (and tests) can use
// errors.As to distinguish missing-token from network errors.
func noTokenError() *oauth2.RetrieveError {
	body := []byte(`{"error":"no_token","error_description":"no OAuth2 token available in secrets provider"}`)
	return &oauth2.RetrieveError{
		Response: &http.Response{
			StatusCode: http.StatusUnauthorized,
			Status:     "401 Unauthorized",
			Body:       io.NopCloser(bytes.NewReader(body)),
			Header:     make(http.Header),
		},
		Body:             body,
		ErrorCode:        "no_token",
		ErrorDescription: "no OAuth2 token available in secrets provider",
	}
}

// ---------------------------------------------------------------------------
// retryOn401Transport — 401-retry wrapper
// ---------------------------------------------------------------------------

// retryOn401Transport sits above oauth2.Transport in the middleware stack.
// On a 401 response it:
//  1. Calls invalidate() on the underlying secretsBackedTokenSource (if applicable),
//     which marks the stored token as requiring a refresh on the next call.
//  2. Replaces the ReuseTokenSource with a fresh one so the cached token is dropped.
//  3. Retries the request exactly once.
//
// This allows externally-rotated credentials (via step.secret_set) to be
// picked up without restarting the module.
//
// Stack layout (outermost → innermost):
//
//	http.Client{Transport: retryOn401Transport}
//	  └─ oauth2.Transport{Source: reuseTS, Base: http.DefaultTransport}
//	       └─ underlying secretsBackedTokenSource / clientCredentialsTokenSource
type retryOn401Transport struct {
	mu         sync.Mutex
	underlying oauth2.TokenSource // the raw (non-reuse) source
	oauth2TR   *oauth2.Transport  // pointer to the oauth2.Transport — we swap its Source
	base       http.RoundTripper  // final transport that actually sends the request
}

// RoundTrip implements http.RoundTripper.
func (t *retryOn401Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Buffer the body so it can be replayed on retry.
	var bodyBytes []byte
	if req.Body != nil && req.Body != http.NoBody {
		var readErr error
		bodyBytes, readErr = io.ReadAll(req.Body)
		if readErr != nil {
			return nil, fmt.Errorf("http.client: reading request body for 401-retry: %w", readErr)
		}
		req.Body.Close()
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}

	// First attempt through the full oauth2 middleware.
	resp, err := t.oauth2TR.RoundTrip(req.Clone(req.Context()))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}

	// 401 — drain and discard the response.
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	// Mark the underlying source as needing a refresh (for secretsBackedTokenSource).
	// This forces the next Token() call to go to the token endpoint even if the
	// stored token timestamp looks valid.
	if sbts, ok := t.underlying.(*secretsBackedTokenSource); ok {
		sbts.invalidate()
	}

	// Replace the ReuseTokenSource with a fresh one so the cached access token
	// is dropped.  The next Token() call will invoke the underlying source.
	t.mu.Lock()
	newReuseTS := oauth2.ReuseTokenSource(nil, t.underlying)
	t.oauth2TR.Source = newReuseTS
	t.mu.Unlock()

	// Rebuild retry request with the buffered body.
	retryReq := req.Clone(req.Context())
	if len(bodyBytes) > 0 {
		retryReq.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		retryReq.ContentLength = int64(len(bodyBytes))
	}

	return t.oauth2TR.RoundTrip(retryReq) //nolint:gosec // G107: URL is user-configured
}
