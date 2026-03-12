package module

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/GoCodeAlone/modular"
)

// GraphQLStep executes GraphQL queries and mutations as a pipeline step.
type GraphQLStep struct {
	name                string
	url                 string
	query               string
	variables           map[string]any
	dataPath            string
	headers             map[string]string
	fragments           []string
	failOnGraphQLErrors bool
	timeout             time.Duration
	retryOnNetworkError bool
	tmpl                *TemplateEngine
	httpClient          *http.Client

	// OAuth2 (reuses globalOAuthCache from pipeline_step_http_call.go)
	auth       *oauthConfig
	oauthEntry *oauthCacheEntry
}

// NewGraphQLStepFactory returns a StepFactory for GraphQLStep.
func NewGraphQLStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		rawURL, _ := config["url"].(string)
		if rawURL == "" {
			return nil, fmt.Errorf("graphql step %q: 'url' is required", name)
		}

		query, _ := config["query"].(string)
		// query can be empty if introspection or batch mode is used
		introspectionCfg, _ := config["introspection"].(map[string]any)
		batchCfg, _ := config["batch"].(map[string]any)
		if query == "" && introspectionCfg == nil && batchCfg == nil {
			return nil, fmt.Errorf("graphql step %q: 'query' is required (unless introspection or batch is configured)", name)
		}

		step := &GraphQLStep{
			name:                name,
			url:                 rawURL,
			query:               query,
			failOnGraphQLErrors: true,
			timeout:             30 * time.Second,
			tmpl:                NewTemplateEngine(),
			httpClient:          http.DefaultClient,
		}

		if vars, ok := config["variables"].(map[string]any); ok {
			step.variables = vars
		}

		if dp, ok := config["data_path"].(string); ok {
			step.dataPath = dp
		}

		if headers, ok := config["headers"].(map[string]any); ok {
			step.headers = make(map[string]string, len(headers))
			for k, v := range headers {
				if s, ok := v.(string); ok {
					step.headers[k] = s
				}
			}
		}

		if frags, ok := config["fragments"].([]any); ok {
			for _, f := range frags {
				if s, ok := f.(string); ok {
					step.fragments = append(step.fragments, s)
				}
			}
		}

		if v, ok := config["fail_on_graphql_errors"]; ok {
			if b, ok := v.(bool); ok {
				step.failOnGraphQLErrors = b
			}
		}

		if s, ok := config["timeout"].(string); ok && s != "" {
			d, err := time.ParseDuration(s)
			if err != nil {
				return nil, fmt.Errorf("graphql step %q: invalid timeout %q: %w", name, s, err)
			}
			step.timeout = d
		}

		if v, ok := config["retry_on_network_error"].(bool); ok {
			step.retryOnNetworkError = v
		}

		// OAuth2 auth — reuses step.http_call patterns
		if authCfg, ok := config["auth"].(map[string]any); ok {
			authType, _ := authCfg["type"].(string)
			switch authType {
			case "oauth2_client_credentials":
				cfg, oauthErr := buildOAuthConfig(name, "auth", authCfg)
				if oauthErr != nil {
					return nil, oauthErr
				}
				step.auth = cfg
				step.oauthEntry = globalOAuthCache.getOrCreate(cfg.cacheKey)
			case "bearer":
				// bearer token stored as a simple header, resolved at execution time
				if token, ok := authCfg["token"].(string); ok {
					if step.headers == nil {
						step.headers = make(map[string]string)
					}
					step.headers["Authorization"] = "Bearer " + token
				}
			case "api_key":
				headerName, _ := authCfg["header"].(string)
				if headerName == "" {
					headerName = "X-API-Key"
				}
				apiKey, _ := authCfg["key"].(string)
				if step.headers == nil {
					step.headers = make(map[string]string)
				}
				step.headers[headerName] = apiKey
			case "basic":
				user, _ := authCfg["username"].(string)
				pass, _ := authCfg["password"].(string)
				if step.headers == nil {
					step.headers = make(map[string]string)
				}
				step.headers["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
			}
		}

		return step, nil
	}
}

// Name returns the step name.
func (s *GraphQLStep) Name() string { return s.name }

// Execute runs the GraphQL query/mutation and returns the result.
func (s *GraphQLStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	if s.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.timeout)
		defer cancel()
	}

	// Resolve URL template
	resolvedURL, err := s.tmpl.Resolve(s.url, pc)
	if err != nil {
		return nil, fmt.Errorf("graphql step %q: failed to resolve url: %w", s.name, err)
	}

	// Resolve variables
	var resolvedVars map[string]any
	if s.variables != nil {
		resolvedVars, err = s.tmpl.ResolveMap(s.variables, pc)
		if err != nil {
			return nil, fmt.Errorf("graphql step %q: failed to resolve variables: %w", s.name, err)
		}
	}

	// Build query with fragments prepended
	fullQuery := s.query
	if len(s.fragments) > 0 {
		fullQuery = strings.Join(s.fragments, "\n") + "\n" + s.query
	}

	// Resolve query template (allows dynamic queries)
	fullQuery, err = s.tmpl.Resolve(fullQuery, pc)
	if err != nil {
		return nil, fmt.Errorf("graphql step %q: failed to resolve query template: %w", s.name, err)
	}

	// Build request body
	reqBody := map[string]any{"query": fullQuery}
	if resolvedVars != nil {
		reqBody["variables"] = resolvedVars
	}

	// Get OAuth2 token if configured
	bearerToken, err := s.getBearerToken(ctx)
	if err != nil {
		return nil, err
	}

	// Execute request (with optional network error retry)
	output, statusCode, err := s.doRequest(ctx, resolvedURL, reqBody, bearerToken)
	if err != nil {
		// 401 retry with token refresh
		if statusCode == http.StatusUnauthorized && s.auth != nil {
			s.oauthEntry.invalidate()
			bearerToken, err = s.fetchTokenDirect(ctx)
			if err != nil {
				return nil, err
			}
			output, statusCode, err = s.doRequest(ctx, resolvedURL, reqBody, bearerToken)
			if err != nil {
				return nil, err
			}
		} else if s.retryOnNetworkError && statusCode == 0 {
			// Network-level error (no HTTP response) — retry once
			output, statusCode, err = s.doRequest(ctx, resolvedURL, reqBody, bearerToken)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	_ = statusCode
	return &StepResult{Output: output}, nil
}

// getBearerToken returns the OAuth2 bearer token if auth is configured.
func (s *GraphQLStep) getBearerToken(ctx context.Context) (string, error) {
	if s.auth == nil {
		return "", nil
	}
	if token := s.oauthEntry.get(); token != "" {
		return token, nil
	}
	val, err, _ := s.oauthEntry.sfGroup.Do("fetch", func() (any, error) {
		if token := s.oauthEntry.get(); token != "" {
			return token, nil
		}
		return s.fetchTokenDirect(ctx)
	})
	if err != nil {
		return "", err
	}
	return val.(string), nil
}

// fetchTokenDirect performs an OAuth2 client_credentials token fetch.
func (s *GraphQLStep) fetchTokenDirect(ctx context.Context) (string, error) {
	params := "grant_type=client_credentials&client_id=" + s.auth.clientID +
		"&client_secret=" + s.auth.clientSecret
	if len(s.auth.scopes) > 0 {
		params += "&scope=" + strings.Join(s.auth.scopes, " ")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.auth.tokenURL,
		strings.NewReader(params))
	if err != nil {
		return "", fmt.Errorf("graphql step %q: failed to create token request: %w", s.name, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("graphql step %q: token request failed: %w", s.name, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("graphql step %q: failed to read token response: %w", s.name, err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("graphql step %q: token endpoint returned HTTP %d: %s", s.name, resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken string  `json:"access_token"`
		ExpiresIn   float64 `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", fmt.Errorf("graphql step %q: failed to parse token response: %w", s.name, err)
	}
	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("graphql step %q: token response missing access_token", s.name)
	}

	ttl := time.Duration(tokenResp.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = 3600 * time.Second
	}
	if ttl > 10*time.Second {
		ttl -= 10 * time.Second
	}
	s.oauthEntry.set(tokenResp.AccessToken, "", ttl)
	return tokenResp.AccessToken, nil
}

// doRequest sends the GraphQL HTTP request and parses the response.
func (s *GraphQLStep) doRequest(ctx context.Context, url string, reqBody map[string]any, bearerToken string) (map[string]any, int, error) {
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("graphql step %q: failed to marshal request: %w", s.name, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, 0, fmt.Errorf("graphql step %q: failed to create request: %w", s.name, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// Apply custom headers (with template resolution)
	for k, v := range s.headers {
		resolved, resolveErr := s.tmpl.Resolve(v, nil)
		if resolveErr != nil {
			resolved = v
		}
		req.Header.Set(k, resolved)
	}

	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("graphql step %q: request failed: %w", s.name, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10MB limit
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("graphql step %q: failed to read response: %w", s.name, err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, resp.StatusCode, fmt.Errorf("graphql step %q: received 401 Unauthorized", s.name)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("graphql step %q: HTTP %d: %s", s.name, resp.StatusCode, string(respBody))
	}

	// Parse GraphQL response
	var gqlResp struct {
		Data       any   `json:"data"`
		Errors     []any `json:"errors"`
		Extensions any   `json:"extensions"`
	}
	if err := json.Unmarshal(respBody, &gqlResp); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("graphql step %q: failed to parse response JSON: %w", s.name, err)
	}

	hasErrors := len(gqlResp.Errors) > 0

	if hasErrors && s.failOnGraphQLErrors {
		errMsg := "graphql error"
		if len(gqlResp.Errors) > 0 {
			if errMap, ok := gqlResp.Errors[0].(map[string]any); ok {
				if msg, ok := errMap["message"].(string); ok {
					errMsg = msg
				}
			}
		}
		return nil, resp.StatusCode, fmt.Errorf("graphql step %q: %s", s.name, errMsg)
	}

	// Extract data via data_path
	extractedData := gqlResp.Data
	if s.dataPath != "" && gqlResp.Data != nil {
		extractedData = extractDataPath(gqlResp.Data, s.dataPath)
	}

	output := map[string]any{
		"data":        extractedData,
		"errors":      gqlResp.Errors,
		"raw":         gqlResp,
		"status_code": resp.StatusCode,
		"has_errors":  hasErrors,
		"extensions":  gqlResp.Extensions,
	}
	if gqlResp.Errors == nil {
		output["errors"] = []any{}
	}

	return output, resp.StatusCode, nil
}

// extractDataPath navigates a dot-separated path into a nested map.
func extractDataPath(data any, path string) any {
	parts := strings.Split(path, ".")
	current := data
	for _, part := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = m[part]
	}
	return current
}
