package module

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/CrisisTextLine/modular"
)

// globalOAuthCache is a process-wide registry of OAuth2 token cache entries, shared across all
// HTTPCallStep instances. Entries are keyed by a credential fingerprint (token URL + client ID +
// client secret + scopes), so each distinct set of credentials (i.e. each tenant) gets its own
// isolated entry.
var globalOAuthCache = &oauthTokenCache{ //nolint:gochecknoglobals // intentional process-wide cache
	entries: make(map[string]*oauthCacheEntry),
}

// oauthTokenCache is a registry of per-credential token cache entries.
type oauthTokenCache struct {
	mu      sync.RWMutex
	entries map[string]*oauthCacheEntry
}

// getOrCreate returns the existing cache entry for key, or creates and stores a new one.
func (c *oauthTokenCache) getOrCreate(key string) *oauthCacheEntry {
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()
	if ok {
		return entry
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok = c.entries[key]; ok {
		return entry
	}
	entry = &oauthCacheEntry{}
	c.entries[key] = entry
	return entry
}

// oauthCacheEntry holds a cached OAuth2 access token with expiry. A singleflight.Group is
// embedded to ensure at most one concurrent token fetch per credential set.
type oauthCacheEntry struct {
	mu          sync.Mutex
	accessToken string
	expiry      time.Time
	sfGroup     singleflight.Group
}

// get returns the cached token if still valid, or an empty string.
func (e *oauthCacheEntry) get() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.accessToken != "" && time.Now().Before(e.expiry) {
		return e.accessToken
	}
	return ""
}

// set stores a token with the given TTL.
func (e *oauthCacheEntry) set(token string, ttl time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.accessToken = token
	e.expiry = time.Now().Add(ttl)
}

// invalidate clears the cached token.
func (e *oauthCacheEntry) invalidate() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.accessToken = ""
	e.expiry = time.Time{}
}

// oauthConfig holds OAuth2 client_credentials configuration.
type oauthConfig struct {
	tokenURL     string
	clientID     string
	clientSecret string
	scopes       []string
	cacheKey     string // derived from credentials; used for per-tenant cache isolation
}

// HTTPCallStep makes an HTTP request as a pipeline step.
type HTTPCallStep struct {
	name       string
	url        string
	method     string
	headers    map[string]string
	body       map[string]any
	timeout    time.Duration
	tmpl       *TemplateEngine
	auth       *oauthConfig
	oauthEntry *oauthCacheEntry // shared entry from globalOAuthCache; nil when no auth configured
	httpClient *http.Client     // timeout is enforced via the context passed to each request
}

// NewHTTPCallStepFactory returns a StepFactory that creates HTTPCallStep instances.
func NewHTTPCallStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		rawURL, _ := config["url"].(string)
		if rawURL == "" {
			return nil, fmt.Errorf("http_call step %q: 'url' is required", name)
		}

		method, _ := config["method"].(string)
		if method == "" {
			method = "GET"
		}

		step := &HTTPCallStep{
			name:       name,
			url:        rawURL,
			method:     method,
			timeout:    30 * time.Second,
			tmpl:       NewTemplateEngine(),
			httpClient: http.DefaultClient,
		}

		if headers, ok := config["headers"].(map[string]any); ok {
			step.headers = make(map[string]string, len(headers))
			for k, v := range headers {
				if s, ok := v.(string); ok {
					step.headers[k] = s
				}
			}
		}

		if body, ok := config["body"].(map[string]any); ok {
			step.body = body
		}

		if timeout, ok := config["timeout"].(string); ok && timeout != "" {
			if d, err := time.ParseDuration(timeout); err == nil {
				step.timeout = d
			}
		}

		if authCfg, ok := config["auth"].(map[string]any); ok {
			authType, _ := authCfg["type"].(string)
			if authType == "oauth2_client_credentials" {
				tokenURL, _ := authCfg["token_url"].(string)
				if tokenURL == "" {
					return nil, fmt.Errorf("http_call step %q: auth.token_url is required for oauth2_client_credentials", name)
				}
				clientID, _ := authCfg["client_id"].(string)
				if clientID == "" {
					return nil, fmt.Errorf("http_call step %q: auth.client_id is required for oauth2_client_credentials", name)
				}
				clientSecret, _ := authCfg["client_secret"].(string)
				if clientSecret == "" {
					return nil, fmt.Errorf("http_call step %q: auth.client_secret is required for oauth2_client_credentials", name)
				}

				var scopes []string
				if raw, ok := authCfg["scopes"]; ok {
					switch v := raw.(type) {
					case []string:
						scopes = v
					case []any:
						for _, s := range v {
							if str, ok := s.(string); ok {
								scopes = append(scopes, str)
							}
						}
					}
				}

				// Cache key incorporates all credential fields so each distinct tenant/client
				// gets its own isolated token cache entry.
				cacheKey := tokenURL + "\x00" + clientID + "\x00" + clientSecret + "\x00" + strings.Join(scopes, " ")
				step.auth = &oauthConfig{
					tokenURL:     tokenURL,
					clientID:     clientID,
					clientSecret: clientSecret,
					scopes:       scopes,
					cacheKey:     cacheKey,
				}
				step.oauthEntry = globalOAuthCache.getOrCreate(cacheKey)
			}
		}

		return step, nil
	}
}

// Name returns the step name.
func (s *HTTPCallStep) Name() string { return s.name }

// doFetchToken performs the actual HTTP call to the token endpoint, caches the result, and returns
// the new access token. It is called either via getToken (through singleflight) or directly on
// the 401-retry path where an unconditional refresh is needed.
func (s *HTTPCallStep) doFetchToken(ctx context.Context) (string, error) {
	params := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {s.auth.clientID},
		"client_secret": {s.auth.clientSecret},
	}
	if len(s.auth.scopes) > 0 {
		params.Set("scope", strings.Join(s.auth.scopes, " "))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.auth.tokenURL,
		strings.NewReader(params.Encode()))
	if err != nil {
		return "", fmt.Errorf("http_call step %q: failed to create token request: %w", s.name, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("http_call step %q: token request failed: %w", s.name, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("http_call step %q: failed to read token response: %w", s.name, err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("http_call step %q: token endpoint returned HTTP %d: %s", s.name, resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken string  `json:"access_token"` //nolint:gosec // G117: parsing OAuth2 token response, not a secret exposure
		ExpiresIn   float64 `json:"expires_in"`
		TokenType   string  `json:"token_type"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("http_call step %q: failed to parse token response: %w", s.name, err)
	}
	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("http_call step %q: token response missing access_token", s.name)
	}

	ttl := time.Duration(tokenResp.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = 3600 * time.Second
	}
	// Subtract a small buffer to avoid using a token that is about to expire
	if ttl > 10*time.Second {
		ttl -= 10 * time.Second
	}
	s.oauthEntry.set(tokenResp.AccessToken, ttl)

	return tokenResp.AccessToken, nil
}

// getToken returns a valid OAuth2 token from the shared cache. If the cache is empty or expired,
// a single network fetch is performed; concurrent callers for the same credential set are
// coalesced via singleflight so the token endpoint is called at most once.
func (s *HTTPCallStep) getToken(ctx context.Context) (string, error) {
	// Fast path: valid token already in the shared cache.
	if token := s.oauthEntry.get(); token != "" {
		return token, nil
	}

	// Slow path: coalesce concurrent fetches so only one goroutine calls the token endpoint.
	val, err, _ := s.oauthEntry.sfGroup.Do("fetch", func() (any, error) {
		// Double-check inside the group so we don't fetch again if a concurrent goroutine
		// already populated the cache while we were waiting.
		if token := s.oauthEntry.get(); token != "" {
			return token, nil
		}
		return s.doFetchToken(ctx)
	})
	if err != nil {
		return "", err
	}
	return val.(string), nil
}

// buildBodyReader constructs the request body reader from the step configuration.
func (s *HTTPCallStep) buildBodyReader(pc *PipelineContext) (io.Reader, error) {
	if s.body != nil {
		resolvedBody, resolveErr := s.tmpl.ResolveMap(s.body, pc)
		if resolveErr != nil {
			return nil, fmt.Errorf("http_call step %q: failed to resolve body: %w", s.name, resolveErr)
		}
		data, marshalErr := json.Marshal(resolvedBody)
		if marshalErr != nil {
			return nil, fmt.Errorf("http_call step %q: failed to marshal body: %w", s.name, marshalErr)
		}
		return bytes.NewReader(data), nil
	}
	if s.method != "GET" && s.method != "HEAD" {
		data, marshalErr := json.Marshal(pc.Current)
		if marshalErr != nil {
			return nil, fmt.Errorf("http_call step %q: failed to marshal current data: %w", s.name, marshalErr)
		}
		return bytes.NewReader(data), nil
	}
	return nil, nil
}

// buildRequest constructs the HTTP request with resolved headers and optional bearer token.
func (s *HTTPCallStep) buildRequest(ctx context.Context, resolvedURL string, bodyReader io.Reader, pc *PipelineContext, bearerToken string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, s.method, resolvedURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("http_call step %q: failed to create request: %w", s.name, err)
	}

	if bodyReader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range s.headers {
		resolved, resolveErr := s.tmpl.Resolve(v, pc)
		if resolveErr != nil {
			req.Header.Set(k, v)
		} else {
			req.Header.Set(k, resolved)
		}
	}
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}

	return req, nil
}

// parseResponse converts an HTTP response into a StepResult output map.
func parseHTTPResponse(resp *http.Response, respBody []byte) map[string]any {
	respHeaders := make(map[string]any, len(resp.Header))
	for k, v := range resp.Header {
		if len(v) == 1 {
			respHeaders[k] = v[0]
		} else {
			vals := make([]any, len(v))
			for i, hv := range v {
				vals[i] = hv
			}
			respHeaders[k] = vals
		}
	}

	output := map[string]any{
		"status_code": resp.StatusCode,
		"status":      resp.Status,
		"headers":     respHeaders,
	}

	var jsonResp any
	if json.Unmarshal(respBody, &jsonResp) == nil {
		output["body"] = jsonResp
	} else {
		output["body"] = string(respBody)
	}

	return output
}

// Execute performs the HTTP request and returns the response.
func (s *HTTPCallStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	// Resolve URL template
	resolvedURL, err := s.tmpl.Resolve(s.url, pc)
	if err != nil {
		return nil, fmt.Errorf("http_call step %q: failed to resolve url: %w", s.name, err)
	}

	bodyReader, err := s.buildBodyReader(pc)
	if err != nil {
		return nil, err
	}

	// Obtain OAuth2 bearer token if auth is configured
	var bearerToken string
	if s.auth != nil {
		bearerToken, err = s.getToken(ctx)
		if err != nil {
			return nil, err
		}
	}

	req, err := s.buildRequest(ctx, resolvedURL, bodyReader, pc, bearerToken)
	if err != nil {
		return nil, err
	}

	resp, err := s.httpClient.Do(req) //nolint:gosec // G107: URL is user-configured
	if err != nil {
		return nil, fmt.Errorf("http_call step %q: request failed: %w", s.name, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("http_call step %q: failed to read response: %w", s.name, err)
	}

	// On 401, invalidate the shared cache and fetch a fresh token directly (bypassing
	// singleflight so the refresh is not coalesced with an in-progress normal fetch).
	if resp.StatusCode == http.StatusUnauthorized && s.auth != nil {
		s.oauthEntry.invalidate()

		newToken, tokenErr := s.doFetchToken(ctx)
		if tokenErr != nil {
			return nil, tokenErr
		}

		retryBody, buildErr := s.buildBodyReader(pc)
		if buildErr != nil {
			return nil, buildErr
		}
		retryReq, buildErr := s.buildRequest(ctx, resolvedURL, retryBody, pc, newToken)
		if buildErr != nil {
			return nil, buildErr
		}

		retryResp, doErr := s.httpClient.Do(retryReq) //nolint:gosec // G107: URL is user-configured
		if doErr != nil {
			return nil, fmt.Errorf("http_call step %q: retry request failed: %w", s.name, doErr)
		}
		defer retryResp.Body.Close()

		respBody, err = io.ReadAll(retryResp.Body)
		if err != nil {
			return nil, fmt.Errorf("http_call step %q: failed to read retry response: %w", s.name, err)
		}

		output := parseHTTPResponse(retryResp, respBody)
		if retryResp.StatusCode >= 400 {
			return nil, fmt.Errorf("http_call step %q: HTTP %d: %s", s.name, retryResp.StatusCode, string(respBody))
		}
		return &StepResult{Output: output}, nil
	}

	output := parseHTTPResponse(resp, respBody)

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("http_call step %q: HTTP %d: %s", s.name, resp.StatusCode, string(respBody))
	}

	return &StepResult{Output: output}, nil
}
