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

	"github.com/GoCodeAlone/modular"
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
		clientCredential: auth.ClientCredential, //nolint:gosec // G101: credential passed through to token source
		scopes:           append([]string(nil), auth.Scopes...),
		base:             http.DefaultTransport,
	}
	reuseTS := oauth2.ReuseTokenSource(nil, ts)
	tr := &retryOn401Transport{
		underlying: ts,
		base:       http.DefaultTransport,
	}
	tr.oauth2TR.Store(&oauth2.Transport{Source: reuseTS, Base: http.DefaultTransport})

	return &http.Client{
		Timeout:   timeout,
		Transport: tr,
	}, nil
}

// clientCredentialsTokenSource fetches OAuth2 client_credentials tokens.
// It is intentionally stateless; caching is handled by oauth2.ReuseTokenSource.
type clientCredentialsTokenSource struct {
	tokenURL         string
	clientID         string
	clientCredential string //nolint:gosec // G101: credential field in token source
	scopes           []string
	base             http.RoundTripper
}

// Token implements oauth2.TokenSource. Uses context.Background() because the
// oauth2.TokenSource interface does not accept a per-call context; this means
// a token refresh cannot be cancelled by a cancelled request.
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
		AccessToken string  `json:"access_token"` //nolint:gosec // G101: parsing OAuth2 response
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
func buildOAuth2RefreshTokenClient(_ context.Context, auth *HTTPClientAuthConfig, tokenProvider secrets.Provider, timeout time.Duration, logger modular.Logger) (*http.Client, error) {
	if auth.TokenURL == "" {
		return nil, fmt.Errorf("oauth2_refresh_token: 'token_url' is required")
	}
	if auth.ClientID == "" {
		return nil, fmt.Errorf("oauth2_refresh_token: 'client_id' or 'client_id_from_secret' is required")
	}
	if auth.ClientCredential == "" {
		return nil, fmt.Errorf("oauth2_refresh_token: 'client_secret' or 'client_secret_from_secret' is required")
	}

	cfg := &oauth2.Config{
		ClientID:     auth.ClientID,
		ClientSecret: auth.ClientCredential, //nolint:gosec // G101: OAuth2 config DTO
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
		logger:      logger,
	}
	reuseTS := oauth2.ReuseTokenSource(nil, ts)
	tr := &retryOn401Transport{
		underlying: ts,
		base:       http.DefaultTransport,
	}
	tr.oauth2TR.Store(&oauth2.Transport{Source: reuseTS, Base: http.DefaultTransport})

	return &http.Client{
		Timeout:   timeout,
		Transport: tr,
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
	logger       modular.Logger
	forceRefresh atomic.Bool // set by invalidate(); cleared after next successful Token()
}

// invalidate marks the token as requiring a refresh on the next Token() call.
// Called by retryOn401Transport when the upstream rejects the current access token.
func (ts *secretsBackedTokenSource) invalidate() {
	ts.forceRefresh.Store(true)
}

// Token implements oauth2.TokenSource. Uses context.Background() because the
// oauth2.TokenSource interface does not accept a per-call context; this means
// a token refresh cannot be cancelled by a cancelled request.
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
		if ts.logger != nil {
			ts.logger.Warn("http.client: failed to persist rotated token; token still valid for this session",
				"error", persistErr)
		}
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
//
// Thread-safety: oauth2TR is an atomic.Pointer so concurrent RoundTrip calls
// always read a consistent snapshot.  The mu mutex serialises the swap-on-401
// path so only one goroutine rebuilds the transport at a time.
type retryOn401Transport struct {
	mu         sync.Mutex
	underlying oauth2.TokenSource          // the raw (non-reuse) source
	oauth2TR   atomic.Pointer[oauth2.Transport] // atomic read; mu-protected write
	base       http.RoundTripper           // final transport that actually sends the request
}

// maxRetryBodySize is the upper bound for buffering a request body when
// req.GetBody is nil and the body is not nil.  Requests with bodies larger than
// this cannot be retried because we have no way to replay them without risking
// an unbounded memory allocation.
const maxRetryBodySize = 1 << 20 // 1 MiB

// RoundTrip implements http.RoundTripper.
//
// Body replay strategy (evaluated before the first attempt):
//
//  1. req.GetBody is set — preferred path; http.NewRequest populates this for
//     bodies backed by bytes/strings readers.  We call it to get a fresh reader
//     for the retry; the original body is consumed by the first attempt.
//  2. Body is nil or http.NoBody — trivial; no buffering needed.
//  3. Body present, GetBody nil — buffer up to maxRetryBodySize before the
//     first attempt so the retry can replay the same bytes.  Bodies that exceed
//     the cap are forwarded as-is but not retried on 401.
func (t *retryOn401Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Determine how we will replay the body on retry, before the first attempt
	// consumes it.
	type replayStrategy int
	const (
		replayGetBody  replayStrategy = iota // call req.GetBody()
		replayNilBody                        // no body to replay
		replayBuffered                       // pre-buffered bytes
		replaySkip                           // body too large; skip retry on 401
	)

	var (
		strategy    replayStrategy
		buffered    []byte
	)

	switch {
	case req.GetBody != nil:
		strategy = replayGetBody

	case req.Body == nil || req.Body == http.NoBody:
		strategy = replayNilBody

	default:
		// Read body upfront so the first attempt can still send it.
		buf, readErr := io.ReadAll(io.LimitReader(req.Body, maxRetryBodySize+1))
		if readErr != nil {
			return nil, fmt.Errorf("http.client: reading request body for 401-retry: %w", readErr)
		}
		if len(buf) > maxRetryBodySize {
			// Too large — forward the already-read bytes on the first attempt;
			// skip retry if we get a 401.
			strategy = replaySkip
			// Restore body for first attempt from the (over-limit) read.
			req = req.Clone(req.Context())
			req.Body = io.NopCloser(bytes.NewReader(buf))
		} else {
			strategy = replayBuffered
			buffered = buf
			req = req.Clone(req.Context())
			req.Body = io.NopCloser(bytes.NewReader(buffered))
			req.ContentLength = int64(len(buffered))
		}
	}

	// First attempt through the full oauth2 middleware stack.
	resp, err := t.oauth2TR.Load().RoundTrip(req.Clone(req.Context()))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}

	// 401 — drain and discard the response body before retrying.
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	if strategy == replaySkip {
		// Body too large to replay safely — return the 401 to the caller.
		return resp, nil
	}

	// Build the retry request.
	var retryReq *http.Request
	switch strategy {
	case replayGetBody:
		newBody, getErr := req.GetBody()
		if getErr != nil {
			// GetBody failed; we cannot replay the body.
			// Return the 401 response to the caller — suppressing getErr
			// intentionally because the caller cares about the HTTP outcome,
			// not the internal replay failure.
			_ = getErr
			return resp, nil //nolint:nilerr // intentional: return HTTP 401 when body replay fails
		}
		retryReq = req.Clone(req.Context())
		retryReq.Body = newBody

	case replayNilBody:
		retryReq = req.Clone(req.Context())

	case replayBuffered:
		retryReq = req.Clone(req.Context())
		retryReq.Body = io.NopCloser(bytes.NewReader(buffered))
		retryReq.ContentLength = int64(len(buffered))
	}

	// Mark the underlying source as needing a refresh (for secretsBackedTokenSource).
	// This forces the next Token() call to go to the token endpoint even if the
	// stored token timestamp looks valid.
	if sbts, ok := t.underlying.(*secretsBackedTokenSource); ok {
		sbts.invalidate()
	}

	// Replace the ReuseTokenSource with a fresh one so the cached access token
	// is dropped.  The next Token() call will invoke the underlying source.
	// mu serialises concurrent 401 swaps; Load() above is always safe to read.
	t.mu.Lock()
	newReuseTS := oauth2.ReuseTokenSource(nil, t.underlying)
	newTR := &oauth2.Transport{Source: newReuseTS, Base: http.DefaultTransport}
	t.oauth2TR.Store(newTR)
	t.mu.Unlock()

	return t.oauth2TR.Load().RoundTrip(retryReq) //nolint:gosec // G107: URL is user-configured
}
