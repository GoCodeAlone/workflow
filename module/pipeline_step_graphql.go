package module

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/GoCodeAlone/modular"
)

// batchQuery holds a single query in a batch request.
type batchQuery struct {
	query     string
	variables map[string]any
}

// paginationConfig holds cursor or offset pagination settings.
type paginationConfig struct {
	strategy       string // "cursor" or "offset"
	pageInfoPath   string
	cursorVariable string
	hasNextField   string
	cursorField    string
	maxPages       int
	maxPerPage     int
	offsetVariable string
}

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
	pagination          *paginationConfig
	batch               []batchQuery
	apqEnabled          bool
	apqSHA256           string
	introspection       bool

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

		if batchCfgParsed, ok := config["batch"].(map[string]any); ok {
			if queries, ok := batchCfgParsed["queries"].([]any); ok {
				for _, q := range queries {
					qMap, ok := q.(map[string]any)
					if !ok {
						continue
					}
					bq := batchQuery{}
					bq.query, _ = qMap["query"].(string)
					if vars, ok := qMap["variables"].(map[string]any); ok {
						bq.variables = vars
					}
					step.batch = append(step.batch, bq)
				}
			}
		}

		if pqCfg, ok := config["persisted_query"].(map[string]any); ok {
			if enabled, ok := pqCfg["enabled"].(bool); ok && enabled {
				step.apqEnabled = true
				if hash, ok := pqCfg["sha256"].(string); ok && hash != "" {
					step.apqSHA256 = hash
				}
			}
		}

		if introCfg, ok := config["introspection"].(map[string]any); ok {
			if enabled, ok := introCfg["enabled"].(bool); ok && enabled {
				step.introspection = true
			}
		}

		if pagCfg, ok := config["pagination"].(map[string]any); ok {
			pc := &paginationConfig{
				strategy:       "cursor",
				maxPages:       10,
				maxPerPage:     100,
				offsetVariable: "offset",
			}
			if s, ok := pagCfg["strategy"].(string); ok {
				pc.strategy = s
			}
			if s, ok := pagCfg["page_info_path"].(string); ok {
				pc.pageInfoPath = s
			}
			if s, ok := pagCfg["cursor_variable"].(string); ok {
				pc.cursorVariable = s
			}
			if s, ok := pagCfg["has_next_field"].(string); ok {
				pc.hasNextField = s
			}
			if s, ok := pagCfg["cursor_field"].(string); ok {
				pc.cursorField = s
			}
			if s, ok := pagCfg["offset_variable"].(string); ok {
				pc.offsetVariable = s
			}
			if v, ok := pagCfg["max_pages"]; ok {
				switch val := v.(type) {
				case int:
					pc.maxPages = val
				case float64:
					pc.maxPages = int(val)
				}
			}
			if v, ok := pagCfg["max_per_page"]; ok {
				switch val := v.(type) {
				case int:
					pc.maxPerPage = val
				case float64:
					pc.maxPerPage = int(val)
				}
			}
			step.pagination = pc
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
	if s.introspection {
		return s.executeIntrospection(ctx, pc)
	}

	if len(s.batch) > 0 {
		return s.executeBatch(ctx, pc)
	}

	if s.pagination != nil {
		return s.executePaginated(ctx, pc)
	}

	if s.apqEnabled {
		return s.executeAPQ(ctx, pc)
	}

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

// executePaginated handles cursor and offset pagination, collecting all pages.
func (s *GraphQLStep) executePaginated(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	if s.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.timeout)
		defer cancel()
	}

	resolvedURL, err := s.tmpl.Resolve(s.url, pc)
	if err != nil {
		return nil, fmt.Errorf("graphql step %q: failed to resolve url: %w", s.name, err)
	}

	bearerToken, err := s.getBearerToken(ctx)
	if err != nil {
		return nil, err
	}

	fullQuery := s.query
	if len(s.fragments) > 0 {
		fullQuery = strings.Join(s.fragments, "\n") + "\n" + s.query
	}
	fullQuery, err = s.tmpl.Resolve(fullQuery, pc)
	if err != nil {
		return nil, fmt.Errorf("graphql step %q: failed to resolve query: %w", s.name, err)
	}

	var allData []any
	var allErrors []any
	pageCount := 0
	var cursor any
	offset := 0

	for page := 0; page < s.pagination.maxPages; page++ {
		vars := make(map[string]any)
		if s.variables != nil {
			resolved, resolveErr := s.tmpl.ResolveMap(s.variables, pc)
			if resolveErr != nil {
				return nil, fmt.Errorf("graphql step %q: failed to resolve variables: %w", s.name, resolveErr)
			}
			for k, v := range resolved {
				vars[k] = v
			}
		}

		switch s.pagination.strategy {
		case "cursor":
			if cursor != nil {
				vars[s.pagination.cursorVariable] = cursor
			}
		case "offset":
			vars[s.pagination.offsetVariable] = offset
		}

		reqBody := map[string]any{"query": fullQuery}
		if len(vars) > 0 {
			reqBody["variables"] = vars
		}

		output, _, reqErr := s.doRequest(ctx, resolvedURL, reqBody, bearerToken)
		if reqErr != nil {
			return nil, reqErr
		}

		// Collect errors
		if errs, ok := output["errors"].([]any); ok && len(errs) > 0 {
			allErrors = append(allErrors, errs...)
		}

		pageCount++

		// Get full response data (before data_path extraction) from "full_data" key
		fullData := output["full_data"]

		switch s.pagination.strategy {
		case "cursor":
			// Extract page items via data_path
			pageData := fullData
			if s.dataPath != "" {
				pageData = extractDataPath(fullData, s.dataPath)
			}
			if arr, ok := pageData.([]any); ok {
				allData = append(allData, arr...)
			}

			// Check for next page via pageInfo
			pageInfo := extractDataPath(fullData, s.pagination.pageInfoPath)
			pageInfoMap, ok := pageInfo.(map[string]any)
			if !ok {
				goto done
			}
			hasNext, _ := pageInfoMap[s.pagination.hasNextField].(bool)
			if !hasNext {
				goto done
			}
			cursor = pageInfoMap[s.pagination.cursorField]
			if cursor == nil {
				goto done
			}

		case "offset":
			// Extract page items via data_path
			pageData := fullData
			if s.dataPath != "" {
				pageData = extractDataPath(fullData, s.dataPath)
			}
			arr, ok := pageData.([]any)
			if !ok || len(arr) == 0 {
				goto done
			}
			allData = append(allData, arr...)
			if len(arr) < s.pagination.maxPerPage {
				goto done
			}
			offset += len(arr)
		}
	}

done:
	result := map[string]any{
		"data":        allData,
		"errors":      allErrors,
		"has_errors":  len(allErrors) > 0,
		"page_count":  pageCount,
		"total_items": len(allData),
		"status_code": 200,
	}
	if allErrors == nil {
		result["errors"] = []any{}
	}

	return &StepResult{Output: result}, nil
}

const introspectionQuery = `query IntrospectionQuery {
  __schema {
    queryType { name }
    mutationType { name }
    subscriptionType { name }
    types {
      kind name description
      fields(includeDeprecated: true) {
        name description
        args { name description type { kind name ofType { kind name ofType { kind name } } } defaultValue }
        type { kind name ofType { kind name ofType { kind name ofType { kind name } } } }
        isDeprecated deprecationReason
      }
      inputFields { name description type { kind name ofType { kind name } } defaultValue }
      interfaces { kind name }
      enumValues(includeDeprecated: true) { name description isDeprecated deprecationReason }
      possibleTypes { kind name }
    }
    directives {
      name description locations
      args { name description type { kind name ofType { kind name } } defaultValue }
    }
  }
}`

// executeIntrospection sends the standard introspection query.
func (s *GraphQLStep) executeIntrospection(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	if s.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.timeout)
		defer cancel()
	}

	resolvedURL, err := s.tmpl.Resolve(s.url, pc)
	if err != nil {
		return nil, fmt.Errorf("graphql step %q: failed to resolve url: %w", s.name, err)
	}

	bearerToken, err := s.getBearerToken(ctx)
	if err != nil {
		return nil, err
	}

	reqBody := map[string]any{"query": introspectionQuery}
	output, _, err := s.doRequest(ctx, resolvedURL, reqBody, bearerToken)
	if err != nil {
		return nil, err
	}

	// Extract __schema and types for convenience
	if fullData, ok := output["full_data"].(map[string]any); ok {
		if schema, ok := fullData["__schema"]; ok {
			output["schema"] = schema
			if schemaMap, ok := schema.(map[string]any); ok {
				output["types"] = schemaMap["types"]
			}
		}
	}

	return &StepResult{Output: output}, nil
}

// executeAPQ sends an Automatic Persisted Query: first hash-only, then with full query on cache miss.
func (s *GraphQLStep) executeAPQ(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	if s.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.timeout)
		defer cancel()
	}

	resolvedURL, err := s.tmpl.Resolve(s.url, pc)
	if err != nil {
		return nil, fmt.Errorf("graphql step %q: failed to resolve url: %w", s.name, err)
	}

	fullQuery := s.query
	if len(s.fragments) > 0 {
		fullQuery = strings.Join(s.fragments, "\n") + "\n" + s.query
	}
	fullQuery, err = s.tmpl.Resolve(fullQuery, pc)
	if err != nil {
		return nil, fmt.Errorf("graphql step %q: failed to resolve query: %w", s.name, err)
	}

	var resolvedVars map[string]any
	if s.variables != nil {
		resolvedVars, err = s.tmpl.ResolveMap(s.variables, pc)
		if err != nil {
			return nil, fmt.Errorf("graphql step %q: failed to resolve variables: %w", s.name, err)
		}
	}

	hash := s.apqSHA256
	if hash == "" {
		h := sha256.Sum256([]byte(fullQuery))
		hash = hex.EncodeToString(h[:])
	}

	bearerToken, err := s.getBearerToken(ctx)
	if err != nil {
		return nil, err
	}

	// First attempt: send hash only (no query body)
	reqBody := map[string]any{
		"extensions": map[string]any{
			"persistedQuery": map[string]any{
				"version":    1,
				"sha256Hash": hash,
			},
		},
	}
	if resolvedVars != nil {
		reqBody["variables"] = resolvedVars
	}

	// Temporarily disable fail_on_graphql_errors so we can inspect PersistedQueryNotFound
	origFail := s.failOnGraphQLErrors
	s.failOnGraphQLErrors = false
	output, _, firstErr := s.doRequest(ctx, resolvedURL, reqBody, bearerToken)
	s.failOnGraphQLErrors = origFail

	if firstErr == nil && !isPersistedQueryNotFound(output) {
		return &StepResult{Output: output}, nil
	}

	// Retry with full query body
	reqBody["query"] = fullQuery
	output, _, err = s.doRequest(ctx, resolvedURL, reqBody, bearerToken)
	if err != nil {
		return nil, err
	}
	return &StepResult{Output: output}, nil
}

// isPersistedQueryNotFound checks if the output contains a PersistedQueryNotFound error.
func isPersistedQueryNotFound(output map[string]any) bool {
	errors, ok := output["errors"].([]any)
	if !ok || len(errors) == 0 {
		return false
	}
	for _, e := range errors {
		if errMap, ok := e.(map[string]any); ok {
			if msg, ok := errMap["message"].(string); ok && strings.Contains(msg, "PersistedQueryNotFound") {
				return true
			}
		}
	}
	return false
}

// executeBatch sends all batch queries in a single HTTP request.
func (s *GraphQLStep) executeBatch(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	if s.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.timeout)
		defer cancel()
	}

	resolvedURL, err := s.tmpl.Resolve(s.url, pc)
	if err != nil {
		return nil, fmt.Errorf("graphql step %q: failed to resolve url: %w", s.name, err)
	}

	bearerToken, err := s.getBearerToken(ctx)
	if err != nil {
		return nil, err
	}

	batchBody := make([]map[string]any, len(s.batch))
	for i, bq := range s.batch {
		query := bq.query
		if len(s.fragments) > 0 {
			query = strings.Join(s.fragments, "\n") + "\n" + query
		}
		entry := map[string]any{"query": query}
		if bq.variables != nil {
			resolved, resolveErr := s.tmpl.ResolveMap(bq.variables, pc)
			if resolveErr != nil {
				return nil, fmt.Errorf("graphql step %q: batch query %d: failed to resolve variables: %w", s.name, i, resolveErr)
			}
			entry["variables"] = resolved
		}
		batchBody[i] = entry
	}

	bodyBytes, err := json.Marshal(batchBody)
	if err != nil {
		return nil, fmt.Errorf("graphql step %q: failed to marshal batch request: %w", s.name, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, resolvedURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("graphql step %q: failed to create batch request: %w", s.name, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	for k, v := range s.headers {
		req.Header.Set(k, v)
	}
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("graphql step %q: batch request failed: %w", s.name, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("graphql step %q: failed to read batch response: %w", s.name, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("graphql step %q: batch HTTP %d: %s", s.name, resp.StatusCode, string(respBody))
	}

	var batchResp []any
	if err := json.Unmarshal(respBody, &batchResp); err != nil {
		return nil, fmt.Errorf("graphql step %q: failed to parse batch response: %w", s.name, err)
	}

	return &StepResult{Output: map[string]any{
		"results":     batchResp,
		"status_code": resp.StatusCode,
	}}, nil
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
// Output includes "full_data" (raw data before data_path extraction) for pagination.
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

	// Parse GraphQL response into a map so full_data is accessible as map[string]any
	var rawMap map[string]any
	if err := json.Unmarshal(respBody, &rawMap); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("graphql step %q: failed to parse response JSON: %w", s.name, err)
	}

	var gqlErrors []any
	if errs, ok := rawMap["errors"].([]any); ok {
		gqlErrors = errs
	}
	gqlData := rawMap["data"]
	gqlExtensions := rawMap["extensions"]

	hasErrors := len(gqlErrors) > 0

	if hasErrors && s.failOnGraphQLErrors {
		errMsg := "graphql error"
		if errMap, ok := gqlErrors[0].(map[string]any); ok {
			if msg, ok := errMap["message"].(string); ok {
				errMsg = msg
			}
		}
		return nil, resp.StatusCode, fmt.Errorf("graphql step %q: %s", s.name, errMsg)
	}

	// Extract data via data_path
	extractedData := gqlData
	if s.dataPath != "" && gqlData != nil {
		extractedData = extractDataPath(gqlData, s.dataPath)
	}

	output := map[string]any{
		"data":        extractedData,
		"full_data":   gqlData, // full data before data_path extraction (used by pagination)
		"errors":      gqlErrors,
		"raw":         rawMap,
		"status_code": resp.StatusCode,
		"has_errors":  hasErrors,
		"extensions":  gqlExtensions,
	}
	if gqlErrors == nil {
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
