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

	"github.com/GoCodeAlone/modular"
)

// globalOAuthCache is a process-wide registry of OAuth2 token cache entries, shared across all
// HTTPCallStep instances. Entries are keyed by a credential fingerprint (token URL + client ID +
// client secret + scopes), so each distinct set of credentials (i.e. each tenant) gets its own
// isolated entry.
var globalOAuthCache = &oauthTokenCache{ //nolint:gochecknoglobals // intentional process-wide cache
	entries: make(map[string]*oauthCacheEntry),
	stopCh:  make(chan struct{}),
}

// oauthTokenCache is a registry of per-credential token cache entries.
type oauthTokenCache struct {
	mu        sync.RWMutex
	entries   map[string]*oauthCacheEntry
	startOnce sync.Once
	stopCh    chan struct{}
}

// getOrCreate returns the existing cache entry for key, or creates and stores a new one.
// On first call it starts a background goroutine that evicts expired entries every 5 minutes.
func (c *oauthTokenCache) getOrCreate(key string) *oauthCacheEntry {
	c.startOnce.Do(func() { go c.cleanupLoop() })

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

// cleanupLoop evicts expired entries from the cache every 5 minutes.
func (c *oauthTokenCache) cleanupLoop() {
	defer func() { recover() }() //nolint:errcheck
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.evictExpired()
		case <-c.stopCh:
			return
		}
	}
}

// evictExpired removes entries whose tokens have expired.
func (c *oauthTokenCache) evictExpired() {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, entry := range c.entries {
		entry.mu.Lock()
		expired := entry.accessToken == "" || now.After(entry.expiry)
		entry.mu.Unlock()
		if expired {
			delete(c.entries, key)
		}
	}
}

// oauthCacheEntry holds a cached OAuth2 access token with expiry. A singleflight.Group is
// embedded to ensure at most one concurrent token fetch per credential set.
type oauthCacheEntry struct {
	mu          sync.Mutex
	accessToken string
	instanceURL string // optional; populated when the token endpoint returns instance_url (Salesforce pattern)
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

// getInstanceURL returns the cached instance_url (may be empty if the token endpoint did not return one).
func (e *oauthCacheEntry) getInstanceURL() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.instanceURL
}

// set stores a token and optional instance_url with the given TTL.
func (e *oauthCacheEntry) set(token, instanceURL string, ttl time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.accessToken = token
	e.instanceURL = instanceURL
	e.expiry = time.Now().Add(ttl)
}

// invalidate clears the cached token and instance_url.
func (e *oauthCacheEntry) invalidate() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.accessToken = ""
	e.instanceURL = ""
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
	bodyFrom   string // dot-path into pc.Current or a prior step result via "steps.<name>..."; if set, the resolved value is used as the request body (strings/[]byte sent as-is, other types JSON-marshaled)
	timeout    time.Duration
	tmpl       *TemplateEngine
	auth       *oauthConfig
	oauthEntry *oauthCacheEntry // shared entry from globalOAuthCache; nil when no auth configured
	httpClient *http.Client     // timeout is enforced via the context passed to each request
	clientRef  string           // service name for an HTTPClient registered in the service registry
	app        modular.Application
}

// NewHTTPCallStepFactory returns a StepFactory that creates HTTPCallStep instances.
func NewHTTPCallStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		rawURL, _ := config["url"].(string)
		if rawURL == "" {
			return nil, fmt.Errorf("http_call step %q: 'url' is required", name)
		}

		method, _ := config["method"].(string)
		if method == "" {
			method = "GET"
		}

		clientRef, _ := config["client"].(string)

		// Parse-time mutual exclusion: client: ref owns authentication; combining it with
		// inline auth or oauth2 blocks is a configuration mistake.
		if clientRef != "" {
			if _, hasAuth := config["auth"]; hasAuth {
				return nil, fmt.Errorf("http_call step %q: 'client' and 'auth' are mutually exclusive; the referenced http.client module owns authentication", name)
			}
			if _, hasOAuth2 := config["oauth2"]; hasOAuth2 {
				return nil, fmt.Errorf("http_call step %q: 'client' and 'oauth2' are mutually exclusive; the referenced http.client module owns authentication", name)
			}
		}

		step := &HTTPCallStep{
			name:       name,
			url:        rawURL,
			method:     method,
			timeout:    30 * time.Second,
			tmpl:       NewTemplateEngine(),
			httpClient: http.DefaultClient,
			clientRef:  clientRef,
			app:        app,
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

		if bodyFrom, ok := config["body_from"].(string); ok {
			step.bodyFrom = bodyFrom
		}

		if timeout, ok := config["timeout"].(string); ok && timeout != "" {
			if d, err := time.ParseDuration(timeout); err == nil {
				step.timeout = d
			}
		}

		if authCfg, ok := config["auth"].(map[string]any); ok {
			authType, _ := authCfg["type"].(string)
			if authType == "oauth2_client_credentials" {
				cfg, oauthErr := buildOAuthConfig(name, "auth", authCfg)
				if oauthErr != nil {
					return nil, oauthErr
				}
				step.auth = cfg
				step.oauthEntry = globalOAuthCache.getOrCreate(cfg.cacheKey)
			}
		}

		// Support top-level "oauth2" key as an alternative to "auth" with type=oauth2_client_credentials.
		// This follows the syntax proposed in the issue and is more idiomatic for Salesforce-style configs:
		//   oauth2:
		//     grant_type: client_credentials  (optional, defaults to client_credentials)
		//     token_url: "..."
		//     client_id: "..."
		//     client_secret: "..."
		//     scopes: ["api"]
		// Note: if the "auth" block is also present, it takes precedence and "oauth2" is ignored.
		if oauth2Cfg, ok := config["oauth2"].(map[string]any); ok && step.auth == nil {
			grantType, _ := oauth2Cfg["grant_type"].(string)
			if grantType == "" {
				grantType = "client_credentials"
			}
			if grantType != "client_credentials" {
				return nil, fmt.Errorf("http_call step %q: oauth2.grant_type must be 'client_credentials'", name)
			}
			cfg, oauthErr := buildOAuthConfig(name, "oauth2", oauth2Cfg)
			if oauthErr != nil {
				return nil, oauthErr
			}
			step.auth = cfg
			step.oauthEntry = globalOAuthCache.getOrCreate(cfg.cacheKey)
		}

		return step, nil
	}
}

// buildOAuthConfig parses OAuth2 client_credentials fields from a config map and returns an
// oauthConfig. The prefix parameter ("auth" or "oauth2") is used in error messages.
func buildOAuthConfig(stepName, prefix string, cfg map[string]any) (*oauthConfig, error) {
	tokenURL, _ := cfg["token_url"].(string)
	if tokenURL == "" {
		return nil, fmt.Errorf("http_call step %q: %s.token_url is required", stepName, prefix)
	}
	clientID, _ := cfg["client_id"].(string)
	if clientID == "" {
		return nil, fmt.Errorf("http_call step %q: %s.client_id is required", stepName, prefix)
	}
	clientSecret, _ := cfg["client_secret"].(string)
	if clientSecret == "" {
		return nil, fmt.Errorf("http_call step %q: %s.client_secret is required", stepName, prefix)
	}

	var scopes []string
	if raw, ok := cfg["scopes"]; ok {
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
	return &oauthConfig{
		tokenURL:     tokenURL,
		clientID:     clientID,
		clientSecret: clientSecret,
		scopes:       scopes,
		cacheKey:     cacheKey,
	}, nil
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
		InstanceURL string  `json:"instance_url"` // Salesforce pattern: base URL for subsequent API calls
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
	s.oauthEntry.set(tokenResp.AccessToken, tokenResp.InstanceURL, ttl)

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
func (s *HTTPCallStep) buildBodyReader(pc *PipelineContext) (io.Reader, bool, error) {
	if s.bodyFrom != "" {
		val := resolveBodyFrom(s.bodyFrom, pc)
		switch v := val.(type) {
		case []byte:
			return bytes.NewReader(v), true, nil
		case string:
			return strings.NewReader(v), true, nil
		case nil:
			return nil, true, nil
		default:
			data, marshalErr := json.Marshal(v)
			if marshalErr != nil {
				return nil, false, fmt.Errorf("http_call step %q: failed to marshal body_from value: %w", s.name, marshalErr)
			}
			return bytes.NewReader(data), false, nil
		}
	}
	if s.body != nil {
		resolvedBody, resolveErr := s.tmpl.ResolveMap(s.body, pc)
		if resolveErr != nil {
			return nil, false, fmt.Errorf("http_call step %q: failed to resolve body: %w", s.name, resolveErr)
		}
		data, marshalErr := json.Marshal(resolvedBody)
		if marshalErr != nil {
			return nil, false, fmt.Errorf("http_call step %q: failed to marshal body: %w", s.name, marshalErr)
		}
		return bytes.NewReader(data), false, nil
	}
	if s.method != "GET" && s.method != "HEAD" {
		data, marshalErr := json.Marshal(pc.Current)
		if marshalErr != nil {
			return nil, false, fmt.Errorf("http_call step %q: failed to marshal current data: %w", s.name, marshalErr)
		}
		return bytes.NewReader(data), false, nil
	}
	return nil, false, nil
}

// buildRequest constructs the HTTP request with resolved headers and optional bearer token.
// rawBody, when true, indicates that the request body is a raw value (string/[]byte/nil,
// typically provided via body_from) and should not have its Content-Type automatically
// overridden with application/json.
func (s *HTTPCallStep) buildRequest(ctx context.Context, resolvedURL string, bodyReader io.Reader, rawBody bool, pc *PipelineContext, bearerToken string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, s.method, resolvedURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("http_call step %q: failed to create request: %w", s.name, err)
	}

	if bodyReader != nil && !rawBody {
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

// resolveClientRef looks up the HTTPClient service from the service registry when clientRef is set.
// Returns the effective *http.Client to use and an error if the service cannot be resolved.
// If clientRef is empty, returns nil (caller uses s.httpClient unchanged).
func (s *HTTPCallStep) resolveClientRef() (HTTPClient, error) {
	if s.clientRef == "" {
		return nil, nil
	}
	if s.app == nil {
		return nil, fmt.Errorf("http_call step %q: client %q requested but no application context available", s.name, s.clientRef)
	}
	svc, ok := s.app.SvcRegistry()[s.clientRef]
	if !ok {
		return nil, fmt.Errorf("http_call step %q: client service %q not found in service registry", s.name, s.clientRef)
	}
	hc, ok := svc.(HTTPClient)
	if !ok {
		return nil, fmt.Errorf("http_call step %q: service %q does not implement HTTPClient", s.name, s.clientRef)
	}
	return hc, nil
}

// resolveURL applies base-URL resolution when the step URL is relative (no scheme) and a
// clientRef's base URL is available. Absolute URLs pass through unchanged.
func resolveStepURL(rawURL, baseURL string) (string, error) {
	if baseURL == "" {
		return rawURL, nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	// If the URL already has a scheme it is absolute — do not prefix with BaseURL.
	if parsed.IsAbs() {
		return rawURL, nil
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	return base.ResolveReference(parsed).String(), nil
}

// Execute performs the HTTP request and returns the response.
func (s *HTTPCallStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	// Resolve client: ref if configured. This replaces s.httpClient for this execution.
	activeClient := s.httpClient
	var clientBaseURL string
	if s.clientRef != "" {
		hc, err := s.resolveClientRef()
		if err != nil {
			return nil, err
		}
		activeClient = hc.Client()
		clientBaseURL = hc.BaseURL()
	}

	// Obtain OAuth2 bearer token first so that instance_url is available for URL template resolution.
	var bearerToken string
	var err error
	if s.auth != nil {
		bearerToken, err = s.getToken(ctx)
		if err != nil {
			return nil, err
		}
		// Inject instance_url into the pipeline context so URL/header templates can reference it
		// as {{ .instance_url }}. This is a Salesforce pattern where the token endpoint returns the
		// org-specific base URL alongside the access token.
		if instanceURL := s.oauthEntry.getInstanceURL(); instanceURL != "" {
			pc.Current["instance_url"] = instanceURL
		}
	}

	// Resolve URL template
	resolvedURL, err := s.tmpl.Resolve(s.url, pc)
	if err != nil {
		return nil, fmt.Errorf("http_call step %q: failed to resolve url: %w", s.name, err)
	}

	// Apply base-URL resolution for relative URLs when a client: ref is in use.
	if clientBaseURL != "" {
		resolvedURL, err = resolveStepURL(resolvedURL, clientBaseURL)
		if err != nil {
			return nil, fmt.Errorf("http_call step %q: failed to resolve url against base: %w", s.name, err)
		}
	}

	bodyReader, rawBody, err := s.buildBodyReader(pc)
	if err != nil {
		return nil, err
	}

	req, err := s.buildRequest(ctx, resolvedURL, bodyReader, rawBody, pc, bearerToken)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	resp, err := activeClient.Do(req) //nolint:gosec // G107: URL is user-configured
	if err != nil {
		return nil, fmt.Errorf("http_call step %q: request failed: %w", s.name, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	elapsedMS := time.Since(start).Milliseconds()
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

		// After a token refresh, instance_url may have changed (Salesforce can rotate it).
		// Re-inject it into pc.Current and re-resolve the URL template so the retry
		// hits the correct host.
		if instanceURL := s.oauthEntry.getInstanceURL(); instanceURL != "" {
			pc.Current["instance_url"] = instanceURL
		}
		retryURL, resolveErr := s.tmpl.Resolve(s.url, pc)
		if resolveErr != nil {
			return nil, fmt.Errorf("http_call step %q: failed to resolve url for retry: %w", s.name, resolveErr)
		}

		retryBody, rawBody2, buildErr := s.buildBodyReader(pc)
		if buildErr != nil {
			return nil, buildErr
		}
		retryReq, buildErr := s.buildRequest(ctx, retryURL, retryBody, rawBody2, pc, newToken)
		if buildErr != nil {
			return nil, buildErr
		}

		retryStart := time.Now()
		retryResp, doErr := activeClient.Do(retryReq) //nolint:gosec // G107: URL is user-configured
		if doErr != nil {
			return nil, fmt.Errorf("http_call step %q: retry request failed: %w", s.name, doErr)
		}
		defer retryResp.Body.Close()

		respBody, err = io.ReadAll(retryResp.Body)
		retryElapsedMS := time.Since(retryStart).Milliseconds()
		if err != nil {
			return nil, fmt.Errorf("http_call step %q: failed to read retry response: %w", s.name, err)
		}

		output := parseHTTPResponse(retryResp, respBody)
		output["elapsed_ms"] = retryElapsedMS
		if instanceURL := s.oauthEntry.getInstanceURL(); instanceURL != "" {
			output["instance_url"] = instanceURL
		}
		if retryResp.StatusCode >= 400 {
			return nil, fmt.Errorf("http_call step %q: HTTP %d: %s", s.name, retryResp.StatusCode, string(respBody))
		}
		return &StepResult{Output: output}, nil
	}

	output := parseHTTPResponse(resp, respBody)
	output["elapsed_ms"] = elapsedMS
	if s.auth != nil {
		if instanceURL := s.oauthEntry.getInstanceURL(); instanceURL != "" {
			output["instance_url"] = instanceURL
		}
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("http_call step %q: HTTP %d: %s", s.name, resp.StatusCode, string(respBody))
	}

	return &StepResult{Output: output}, nil
}
