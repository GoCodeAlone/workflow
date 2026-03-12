# step.graphql Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a full-featured `step.graphql` pipeline step to the workflow engine, then migrate BMW's 5 BwP plugin steps to use it.

**Architecture:** New standalone step type in `module/pipeline_step_graphql.go` that reuses the existing OAuth2 token cache (`globalOAuthCache` in `pipeline_step_http_call.go`). The step sends GraphQL queries/mutations over HTTP POST, parses GraphQL-specific responses (data/errors/extensions), and supports pagination, batching, APQ, introspection, and fragments.

**Tech Stack:** Go, `net/http`, `encoding/json`, `crypto/sha256`, `golang.org/x/sync/singleflight` (already a dependency), `gopkg.in/yaml.v3` (for config), Go templates via `TemplateEngine`.

**Design doc:** `docs/plans/2026-03-12-step-graphql-design.md`

---

## Task 1: Core GraphQL Step — Basic Query/Mutation

**Files:**
- Create: `module/pipeline_step_graphql.go`
- Test: `module/pipeline_step_graphql_test.go`
- Modify: `plugins/pipelinesteps/plugin.go` (add registration)

### Step 1: Write failing tests for basic query execution

Create `module/pipeline_step_graphql_test.go` with tests for:
- Basic query with variables
- Missing `url` returns factory error
- Missing `query` returns factory error

```go
package module

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGraphQLStep_BasicQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected application/json content-type, got %s", ct)
		}

		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.Variables["id"] != "user-123" {
			t.Errorf("expected variable id=user-123, got %v", req.Variables["id"])
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"user": map[string]any{
					"name":  "Alice",
					"email": "alice@example.com",
				},
			},
		})
	}))
	defer server.Close()

	factory := NewGraphQLStepFactory()
	step, err := factory("test_query", map[string]any{
		"url": server.URL,
		"query": `query GetUser($id: ID!) {
			user(id: $id) { name email }
		}`,
		"variables": map[string]any{
			"id": "user-123",
		},
		"data_path": "user",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	pc := &PipelineContext{
		Current: map[string]any{},
		Steps:   map[string]map[string]any{},
	}
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatal(err)
	}

	data, ok := result.Output["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data to be map, got %T: %v", result.Output["data"], result.Output["data"])
	}
	if data["name"] != "Alice" {
		t.Errorf("expected name=Alice, got %v", data["name"])
	}
	if result.Output["has_errors"] != false {
		t.Errorf("expected has_errors=false, got %v", result.Output["has_errors"])
	}
	if result.Output["status_code"] != 200 {
		t.Errorf("expected status_code=200, got %v", result.Output["status_code"])
	}
}

func TestGraphQLStep_FactoryValidation(t *testing.T) {
	factory := NewGraphQLStepFactory()

	_, err := factory("no_url", map[string]any{
		"query": "{ users { id } }",
	}, nil)
	if err == nil {
		t.Error("expected error for missing url")
	}

	_, err = factory("no_query", map[string]any{
		"url": "http://example.com/graphql",
	}, nil)
	if err == nil {
		t.Error("expected error for missing query")
	}
}
```

### Step 2: Run tests to verify they fail

Run: `cd /Users/jon/workspace/workflow && go test ./module/ -run TestGraphQL -v`
Expected: compilation error — `NewGraphQLStepFactory` not found

### Step 3: Implement core GraphQLStep

Create `module/pipeline_step_graphql.go`:

```go
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
```

### Step 4: Run tests to verify they pass

Run: `cd /Users/jon/workspace/workflow && go test ./module/ -run TestGraphQL -v`
Expected: PASS

### Step 5: Register the step in the plugin

In `plugins/pipelinesteps/plugin.go`, add to the `StepFactories()` map:

```go
"step.graphql": wrapStepFactory(module.NewGraphQLStepFactory()),
```

### Step 6: Run full test suite

Run: `cd /Users/jon/workspace/workflow && go test ./... -count=1 2>&1 | tail -20`
Expected: PASS (or only pre-existing failures)

### Step 7: Commit

```bash
cd /Users/jon/workspace/workflow
git add module/pipeline_step_graphql.go module/pipeline_step_graphql_test.go plugins/pipelinesteps/plugin.go
git commit -m "feat: add step.graphql — generic GraphQL pipeline step

Supports query/mutation execution with variables, data_path extraction,
GraphQL error handling, OAuth2 client_credentials auth (reuses global
token cache), custom headers, and fragment prepending."
```

---

## Task 2: GraphQL Error Handling Tests

**Files:**
- Modify: `module/pipeline_step_graphql_test.go`

### Step 1: Write tests for GraphQL error scenarios

Add these tests to `module/pipeline_step_graphql_test.go`:

```go
func TestGraphQLStep_GraphQLErrors_FailByDefault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"data": nil,
			"errors": []map[string]any{
				{"message": "User not found", "locations": []map[string]any{{"line": 1, "column": 3}}},
			},
		})
	}))
	defer server.Close()

	factory := NewGraphQLStepFactory()
	step, _ := factory("err_test", map[string]any{
		"url":   server.URL,
		"query": "{ user(id: \"bad\") { name } }",
	}, nil)

	pc := &PipelineContext{Current: map[string]any{}, Steps: map[string]map[string]any{}}
	_, err := step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error for GraphQL errors with fail_on_graphql_errors=true")
	}
	if !strings.Contains(err.Error(), "User not found") {
		t.Errorf("expected error message to contain 'User not found', got: %s", err.Error())
	}
}

func TestGraphQLStep_GraphQLErrors_PartialData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"user": map[string]any{"name": "Alice"}},
			"errors": []map[string]any{
				{"message": "email field requires elevated permissions"},
			},
		})
	}))
	defer server.Close()

	factory := NewGraphQLStepFactory()
	step, _ := factory("partial_test", map[string]any{
		"url":                    server.URL,
		"query":                  "{ user(id: \"1\") { name email } }",
		"data_path":              "user",
		"fail_on_graphql_errors": false,
	}, nil)

	pc := &PipelineContext{Current: map[string]any{}, Steps: map[string]map[string]any{}}
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("unexpected error with fail_on_graphql_errors=false: %v", err)
	}
	if result.Output["has_errors"] != true {
		t.Error("expected has_errors=true")
	}
	data := result.Output["data"].(map[string]any)
	if data["name"] != "Alice" {
		t.Errorf("expected partial data name=Alice, got %v", data["name"])
	}
	errors := result.Output["errors"].([]any)
	if len(errors) != 1 {
		t.Errorf("expected 1 error, got %d", len(errors))
	}
}

func TestGraphQLStep_DataPathExtraction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"organization": map[string]any{
					"users": []any{
						map[string]any{"name": "Alice"},
						map[string]any{"name": "Bob"},
					},
				},
			},
		})
	}))
	defer server.Close()

	factory := NewGraphQLStepFactory()
	step, _ := factory("path_test", map[string]any{
		"url":       server.URL,
		"query":     "{ organization { users { name } } }",
		"data_path": "organization.users",
	}, nil)

	pc := &PipelineContext{Current: map[string]any{}, Steps: map[string]map[string]any{}}
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatal(err)
	}
	data, ok := result.Output["data"].([]any)
	if !ok {
		t.Fatalf("expected data to be array, got %T", result.Output["data"])
	}
	if len(data) != 2 {
		t.Errorf("expected 2 users, got %d", len(data))
	}
}
```

(Add `"strings"` to the import block if not already present.)

### Step 2: Run tests

Run: `cd /Users/jon/workspace/workflow && go test ./module/ -run TestGraphQLStep_ -v`
Expected: PASS

### Step 3: Commit

```bash
cd /Users/jon/workspace/workflow
git add module/pipeline_step_graphql_test.go
git commit -m "test: add GraphQL error handling and data_path extraction tests"
```

---

## Task 3: OAuth2 Authentication

**Files:**
- Modify: `module/pipeline_step_graphql_test.go`

### Step 1: Write OAuth2 test

```go
func TestGraphQLStep_OAuth2ClientCredentials(t *testing.T) {
	tokenCalled := 0
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenCalled++
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("grant_type") != "client_credentials" {
			t.Errorf("expected grant_type=client_credentials, got %s", r.Form.Get("grant_type"))
		}
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "test-token-123",
			"expires_in":   3600,
			"token_type":   "Bearer",
		})
	}))
	defer tokenServer.Close()

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token-123" {
			w.WriteHeader(401)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"ok": true},
		})
	}))
	defer apiServer.Close()

	factory := NewGraphQLStepFactory()
	step, err := factory("oauth_test", map[string]any{
		"url":   apiServer.URL,
		"query": "{ status }",
		"auth": map[string]any{
			"type":          "oauth2_client_credentials",
			"token_url":     tokenServer.URL,
			"client_id":     "test-client",
			"client_secret": "test-secret",
			"scopes":        []any{"api.read"},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	pc := &PipelineContext{Current: map[string]any{}, Steps: map[string]map[string]any{}}
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatal(err)
	}
	if result.Output["has_errors"] != false {
		t.Error("expected no errors")
	}
	if tokenCalled != 1 {
		t.Errorf("expected 1 token call, got %d", tokenCalled)
	}
}
```

### Step 2: Run test

Run: `cd /Users/jon/workspace/workflow && go test ./module/ -run TestGraphQLStep_OAuth2 -v`
Expected: PASS (already implemented in Task 1)

### Step 3: Commit

```bash
cd /Users/jon/workspace/workflow
git add module/pipeline_step_graphql_test.go
git commit -m "test: add OAuth2 client_credentials test for step.graphql"
```

---

## Task 4: Pagination (Cursor + Offset)

**Files:**
- Modify: `module/pipeline_step_graphql.go`
- Modify: `module/pipeline_step_graphql_test.go`

### Step 1: Write failing pagination test

```go
func TestGraphQLStep_CursorPagination(t *testing.T) {
	page := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Variables map[string]any `json:"variables"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		page++
		var resp map[string]any
		switch page {
		case 1:
			if req.Variables["after"] != nil {
				t.Error("first page should have no cursor")
			}
			resp = map[string]any{
				"data": map[string]any{
					"users": map[string]any{
						"edges": []any{
							map[string]any{"node": map[string]any{"name": "Alice"}},
							map[string]any{"node": map[string]any{"name": "Bob"}},
						},
						"pageInfo": map[string]any{
							"hasNextPage": true,
							"endCursor":   "cursor-abc",
						},
					},
				},
			}
		case 2:
			if req.Variables["after"] != "cursor-abc" {
				t.Errorf("expected cursor cursor-abc, got %v", req.Variables["after"])
			}
			resp = map[string]any{
				"data": map[string]any{
					"users": map[string]any{
						"edges": []any{
							map[string]any{"node": map[string]any{"name": "Charlie"}},
						},
						"pageInfo": map[string]any{
							"hasNextPage": false,
							"endCursor":   "cursor-def",
						},
					},
				},
			}
		default:
			t.Fatal("too many requests")
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	factory := NewGraphQLStepFactory()
	step, err := factory("paginate_test", map[string]any{
		"url":       server.URL,
		"query":     `query Users($after: String) { users(first: 2, after: $after) { edges { node { name } } pageInfo { hasNextPage endCursor } } }`,
		"data_path": "users.edges",
		"pagination": map[string]any{
			"strategy":        "cursor",
			"page_info_path":  "users.pageInfo",
			"cursor_variable": "after",
			"has_next_field":  "hasNextPage",
			"cursor_field":    "endCursor",
			"max_pages":       10,
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	pc := &PipelineContext{Current: map[string]any{}, Steps: map[string]map[string]any{}}
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatal(err)
	}

	data, ok := result.Output["data"].([]any)
	if !ok {
		t.Fatalf("expected merged array, got %T: %v", result.Output["data"], result.Output["data"])
	}
	if len(data) != 3 {
		t.Errorf("expected 3 merged items, got %d", len(data))
	}
	if result.Output["page_count"] != 2 {
		t.Errorf("expected page_count=2, got %v", result.Output["page_count"])
	}
}
```

### Step 2: Run test to verify it fails

Run: `cd /Users/jon/workspace/workflow && go test ./module/ -run TestGraphQLStep_CursorPagination -v`
Expected: FAIL — pagination config not parsed

### Step 3: Implement pagination

Add pagination config struct and parsing to `pipeline_step_graphql.go`:

```go
// paginationConfig holds cursor or offset pagination settings.
type paginationConfig struct {
	strategy       string // "cursor" or "offset"
	pageInfoPath   string
	cursorVariable string
	hasNextField   string
	cursorField    string
	maxPages       int
}
```

Add field to `GraphQLStep`:
```go
pagination *paginationConfig
```

Add parsing in factory after fragments parsing:
```go
if pagCfg, ok := config["pagination"].(map[string]any); ok {
	pc := &paginationConfig{
		strategy: "cursor",
		maxPages: 10,
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
	if v, ok := pagCfg["max_pages"]; ok {
		switch val := v.(type) {
		case int:
			pc.maxPages = val
		case float64:
			pc.maxPages = int(val)
		}
	}
	step.pagination = pc
}
```

Modify `Execute` to check for pagination mode and delegate:
```go
// In Execute, before the single-query path:
if s.pagination != nil {
	return s.executePaginated(ctx, pc)
}
```

Add the paginated execution method:
```go
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
		if cursor != nil {
			vars[s.pagination.cursorVariable] = cursor
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

		// Extract page data via data_path
		rawData := output["raw"]
		var gqlData any
		if rawResp, ok := rawData.(struct {
			Data       any   `json:"data"`
			Errors     []any `json:"errors"`
			Extensions any   `json:"extensions"`
		}); ok {
			gqlData = gqlData
			_ = gqlData
			_ = gqlResp
		}
		// Re-extract from raw response
		if rawMap, ok := rawData.(map[string]any); ok {
			gqlData = rawMap["data"]
		} else {
			// output["data"] already extracted
			// For pagination we need to navigate the full data tree
			gqlData = output["data"]
			if s.dataPath != "" {
				// data was already extracted, but we need full response data for pageInfo
				// Re-parse: output["raw"] has full response
			}
		}

		// Get full response data for pageInfo navigation
		var fullData any
		if rawMap, ok := rawData.(map[string]any); ok {
			fullData = rawMap["data"]
		}
		if fullData == nil {
			fullData = output["data"]
		}

		// Extract items from data_path
		pageData := fullData
		if s.dataPath != "" {
			pageData = extractDataPath(fullData, s.dataPath)
		}
		if arr, ok := pageData.([]any); ok {
			allData = append(allData, arr...)
		}

		// Check pagination: extract pageInfo
		pageInfo := extractDataPath(fullData, s.pagination.pageInfoPath)
		pageInfoMap, ok := pageInfo.(map[string]any)
		if !ok {
			break
		}

		hasNext, _ := pageInfoMap[s.pagination.hasNextField].(bool)
		if !hasNext {
			break
		}

		cursor = pageInfoMap[s.pagination.cursorField]
		if cursor == nil {
			break
		}
	}

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
```

**Note:** The `executePaginated` method accesses the full GraphQL `data` response (before `data_path` extraction) to navigate `pageInfo`. The `doRequest` method applies `data_path` to the `data` output key. For pagination, we need both: `data_path` for item extraction and `page_info_path` for cursor navigation. So `doRequest` stores the full response in `raw` — pagination uses `raw.data` for navigation and `data_path` for item collection.

The implementation above extracts `fullData` from the `raw` response map (which `doRequest` stores as the full `{data, errors, extensions}` struct). It navigates `page_info_path` against `fullData` and `data_path` against `fullData` separately.

**Offset pagination:** In the `executePaginated` method, add an offset branch. When `strategy == "offset"`, instead of extracting cursor from pageInfo, increment an `offset` variable by the page result count and inject it into the `offset_variable` (config field, defaults to `"offset"`). Also add a `limit_variable` config field (defaults to `"limit"`) so the step knows which variable to check against result count. Add `offset_variable` and `limit_variable` fields to `paginationConfig` struct.

Offset loop logic:
```go
if s.pagination.strategy == "offset" {
	// Extract result array from data_path
	pageData := extractDataPath(fullData, s.dataPath)
	arr, ok := pageData.([]any)
	if !ok || len(arr) == 0 {
		break
	}
	allData = append(allData, arr...)
	// If result count < limit, we've reached the end
	limit := s.pagination.maxPerPage
	if len(arr) < limit {
		break
	}
	offset += len(arr)
	// Inject offset into variables for next iteration
}
```

Add `max_per_page` (int, default 100) and `offset_variable` (string, default `"offset"`) to `paginationConfig`.

### Step 4: Write offset pagination test

```go
func TestGraphQLStep_OffsetPagination(t *testing.T) {
	page := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Variables map[string]any `json:"variables"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		page++

		offset := 0
		if v, ok := req.Variables["offset"]; ok {
			if f, ok := v.(float64); ok {
				offset = int(f)
			}
		}

		var items []any
		switch offset {
		case 0:
			items = []any{
				map[string]any{"name": "A"},
				map[string]any{"name": "B"},
			}
		case 2:
			items = []any{
				map[string]any{"name": "C"},
			}
		}

		json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"items": items},
		})
	}))
	defer server.Close()

	factory := NewGraphQLStepFactory()
	step, err := factory("offset_test", map[string]any{
		"url":       server.URL,
		"query":     `query Items($offset: Int!, $limit: Int!) { items(offset: $offset, limit: $limit) { name } }`,
		"data_path": "items",
		"variables": map[string]any{
			"limit": 2,
		},
		"pagination": map[string]any{
			"strategy":        "offset",
			"offset_variable": "offset",
			"max_per_page":    2,
			"max_pages":       10,
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	pc := &PipelineContext{Current: map[string]any{}, Steps: map[string]map[string]any{}}
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatal(err)
	}

	data, ok := result.Output["data"].([]any)
	if !ok {
		t.Fatalf("expected merged array, got %T", result.Output["data"])
	}
	if len(data) != 3 {
		t.Errorf("expected 3 items, got %d", len(data))
	}
	if result.Output["page_count"] != 2 {
		t.Errorf("expected page_count=2, got %v", result.Output["page_count"])
	}
}
```

### Step 5: Run pagination tests

Run: `cd /Users/jon/workspace/workflow && go test ./module/ -run TestGraphQLStep_.*Pagination -v`
Expected: PASS

### Step 6: Commit

```bash
cd /Users/jon/workspace/workflow
git add module/pipeline_step_graphql.go module/pipeline_step_graphql_test.go
git commit -m "feat(step.graphql): add cursor and offset pagination support"
```

---

## Task 5: Batch Queries

**Files:**
- Modify: `module/pipeline_step_graphql.go`
- Modify: `module/pipeline_step_graphql_test.go`

### Step 1: Write failing batch test

```go
func TestGraphQLStep_BatchQueries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqs []map[string]any
		if err := json.NewDecoder(r.Body).Decode(&reqs); err != nil {
			t.Fatal(err)
		}
		if len(reqs) != 2 {
			t.Fatalf("expected 2 batch queries, got %d", len(reqs))
		}

		results := make([]map[string]any, len(reqs))
		for i := range reqs {
			results[i] = map[string]any{
				"data": map[string]any{"index": i},
			}
		}
		json.NewEncoder(w).Encode(results)
	}))
	defer server.Close()

	factory := NewGraphQLStepFactory()
	step, err := factory("batch_test", map[string]any{
		"url": server.URL,
		"batch": map[string]any{
			"queries": []any{
				map[string]any{"query": "{ users { name } }"},
				map[string]any{"query": "{ posts { title } }"},
			},
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	pc := &PipelineContext{Current: map[string]any{}, Steps: map[string]map[string]any{}}
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatal(err)
	}

	results, ok := result.Output["results"].([]any)
	if !ok {
		t.Fatalf("expected results array, got %T", result.Output["results"])
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
}
```

### Step 2: Run test to verify it fails

Run: `cd /Users/jon/workspace/workflow && go test ./module/ -run TestGraphQLStep_Batch -v`
Expected: FAIL

### Step 3: Implement batch queries

Add batch config to `GraphQLStep`:
```go
type batchQuery struct {
	query     string
	variables map[string]any
}

// Add to GraphQLStep struct:
batch []batchQuery
```

Parse in factory:
```go
if batchCfg, ok := config["batch"].(map[string]any); ok {
	if queries, ok := batchCfg["queries"].([]any); ok {
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
```

Add batch execution in `Execute` (before pagination check):
```go
if len(s.batch) > 0 {
	return s.executeBatch(ctx, pc)
}
```

Implement `executeBatch`:
```go
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

	// Build batch request body (array of {query, variables})
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

	output := map[string]any{
		"results":     batchResp,
		"status_code": resp.StatusCode,
	}
	return &StepResult{Output: output}, nil
}
```

### Step 4: Run test

Run: `cd /Users/jon/workspace/workflow && go test ./module/ -run TestGraphQLStep_Batch -v`
Expected: PASS

### Step 5: Commit

```bash
cd /Users/jon/workspace/workflow
git add module/pipeline_step_graphql.go module/pipeline_step_graphql_test.go
git commit -m "feat(step.graphql): add batch query support"
```

---

## Task 6: Automatic Persisted Queries (APQ)

**Files:**
- Modify: `module/pipeline_step_graphql.go`
- Modify: `module/pipeline_step_graphql_test.go`

### Step 1: Write failing APQ test

```go
func TestGraphQLStep_APQ(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)

		ext, _ := req["extensions"].(map[string]any)
		pq, _ := ext["persistedQuery"].(map[string]any)

		if callCount == 1 {
			// First call: hash only, no query body — return PersistedQueryNotFound
			if req["query"] != nil {
				t.Error("first APQ call should not include query body")
			}
			if pq == nil {
				t.Fatal("expected persistedQuery extension")
			}
			json.NewEncoder(w).Encode(map[string]any{
				"errors": []map[string]any{
					{"message": "PersistedQueryNotFound"},
				},
			})
			return
		}

		// Second call: hash + query body
		if req["query"] == nil {
			t.Error("retry should include query body")
		}
		json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"ok": true},
		})
	}))
	defer server.Close()

	factory := NewGraphQLStepFactory()
	step, err := factory("apq_test", map[string]any{
		"url":   server.URL,
		"query": "{ status }",
		"persisted_query": map[string]any{
			"enabled": true,
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	pc := &PipelineContext{Current: map[string]any{}, Steps: map[string]map[string]any{}}
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatal(err)
	}
	if result.Output["has_errors"] != false {
		t.Error("expected no errors after APQ negotiation")
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls (hash miss + retry), got %d", callCount)
	}
}
```

### Step 2: Run test to verify it fails

Run: `cd /Users/jon/workspace/workflow && go test ./module/ -run TestGraphQLStep_APQ -v`
Expected: FAIL

### Step 3: Implement APQ

Add to imports: `"crypto/sha256"`, `"encoding/hex"`

Add to `GraphQLStep` struct:
```go
apqEnabled bool
apqSHA256  string // pre-computed, or auto-computed at execution time
```

Parse in factory:
```go
if pqCfg, ok := config["persisted_query"].(map[string]any); ok {
	if enabled, ok := pqCfg["enabled"].(bool); ok && enabled {
		step.apqEnabled = true
		if hash, ok := pqCfg["sha256"].(string); ok && hash != "" {
			step.apqSHA256 = hash
		}
	}
}
```

Modify `Execute` to handle APQ (add before the standard request path, after variable resolution):

```go
if s.apqEnabled && s.pagination == nil && len(s.batch) == 0 {
	return s.executeAPQ(ctx, pc)
}
```

Implement:
```go
func (s *GraphQLStep) executeAPQ(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
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

	output, _, err := s.doRequest(ctx, resolvedURL, reqBody, bearerToken)
	if err == nil && !isPersistedQueryNotFound(output) {
		return &StepResult{Output: output}, nil
	}

	// Retry with full query
	reqBody["query"] = fullQuery
	output, _, err = s.doRequest(ctx, resolvedURL, reqBody, bearerToken)
	if err != nil {
		return nil, err
	}
	return &StepResult{Output: output}, nil
}

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
```

**Important:** For APQ, `doRequest` with `fail_on_graphql_errors=true` would fail on the first attempt's `PersistedQueryNotFound` error. The APQ path must handle errors internally, so temporarily override error handling. The cleanest approach: `doRequest` always returns the output map (even with errors) and only returns a Go error for HTTP-level failures. The GraphQL error check happens in the caller. Looking at the existing `doRequest` implementation, it already returns an error when `fail_on_graphql_errors` is true. For APQ, the `executeAPQ` method should set `s.failOnGraphQLErrors = false` for the first attempt, then restore it for the retry. Alternatively, add a `ignoreGraphQLErrors` parameter to `doRequest`. The simplest approach: the `executeAPQ` method catches the Go error from the first attempt and checks if it's a PersistedQueryNotFound before retrying.

### Step 4: Run test

Run: `cd /Users/jon/workspace/workflow && go test ./module/ -run TestGraphQLStep_APQ -v`
Expected: PASS

### Step 5: Commit

```bash
cd /Users/jon/workspace/workflow
git add module/pipeline_step_graphql.go module/pipeline_step_graphql_test.go
git commit -m "feat(step.graphql): add automatic persisted queries (APQ) support"
```

---

## Task 7: Introspection Query

**Files:**
- Modify: `module/pipeline_step_graphql.go`
- Modify: `module/pipeline_step_graphql_test.go`

### Step 1: Write failing introspection test

```go
func TestGraphQLStep_Introspection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query string `json:"query"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		if !strings.Contains(req.Query, "__schema") {
			t.Error("expected introspection query with __schema")
		}

		json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"__schema": map[string]any{
					"types": []any{
						map[string]any{"name": "Query", "kind": "OBJECT"},
						map[string]any{"name": "User", "kind": "OBJECT"},
					},
				},
			},
		})
	}))
	defer server.Close()

	factory := NewGraphQLStepFactory()
	step, err := factory("introspect_test", map[string]any{
		"url": server.URL,
		"introspection": map[string]any{
			"enabled": true,
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	pc := &PipelineContext{Current: map[string]any{}, Steps: map[string]map[string]any{}}
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatal(err)
	}

	if result.Output["schema"] == nil {
		t.Error("expected schema in output")
	}
	types, ok := result.Output["types"].([]any)
	if !ok {
		t.Fatalf("expected types array, got %T", result.Output["types"])
	}
	if len(types) != 2 {
		t.Errorf("expected 2 types, got %d", len(types))
	}
}
```

### Step 2: Run test to verify it fails

Run: `cd /Users/jon/workspace/workflow && go test ./module/ -run TestGraphQLStep_Introspection -v`
Expected: FAIL

### Step 3: Implement introspection

Add to `GraphQLStep` struct:
```go
introspection bool
```

Parse in factory:
```go
if introCfg, ok := config["introspection"].(map[string]any); ok {
	if enabled, ok := introCfg["enabled"].(bool); ok && enabled {
		step.introspection = true
	}
}
```

Add introspection query constant:
```go
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
```

Add to `Execute`, before batch/pagination/APQ checks:
```go
if s.introspection {
	return s.executeIntrospection(ctx, pc)
}
```

Implement:
```go
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

	// Extract schema and types for convenience
	if data, ok := output["data"].(map[string]any); ok {
		if schema, ok := data.(map[string]any)["__schema"]; ok {
			output["schema"] = schema
			if schemaMap, ok := schema.(map[string]any); ok {
				output["types"] = schemaMap["types"]
			}
		}
	}
	// Also check if data_path already extracted __schema
	if output["schema"] == nil {
		if raw, ok := output["raw"].(map[string]any); ok {
			if rawData, ok := raw["data"].(map[string]any); ok {
				if schema, ok := rawData["__schema"]; ok {
					output["schema"] = schema
					if schemaMap, ok := schema.(map[string]any); ok {
						output["types"] = schemaMap["types"]
					}
				}
			}
		}
	}

	return &StepResult{Output: output}, nil
}
```

### Step 4: Run test

Run: `cd /Users/jon/workspace/workflow && go test ./module/ -run TestGraphQLStep_Introspection -v`
Expected: PASS

### Step 5: Commit

```bash
cd /Users/jon/workspace/workflow
git add module/pipeline_step_graphql.go module/pipeline_step_graphql_test.go
git commit -m "feat(step.graphql): add introspection query support"
```

---

## Task 8: Template Resolution & Fragments Test

**Files:**
- Modify: `module/pipeline_step_graphql_test.go`

### Step 1: Write test for template-resolved variables and fragments

```go
func TestGraphQLStep_TemplateVariables(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		if req.Variables["user_id"] != "from-current" {
			t.Errorf("expected resolved variable, got %v", req.Variables["user_id"])
		}
		if !strings.Contains(req.Query, "fragment UserFields") {
			t.Error("expected fragment to be prepended to query")
		}

		json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"user": map[string]any{"name": "Test"}},
		})
	}))
	defer server.Close()

	factory := NewGraphQLStepFactory()
	step, err := factory("tmpl_test", map[string]any{
		"url": server.URL,
		"query": `query GetUser($user_id: ID!) {
			user(id: $user_id) { ...UserFields }
		}`,
		"variables": map[string]any{
			"user_id": "{{ .user_id }}",
		},
		"fragments": []any{
			"fragment UserFields on User { name email }",
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	pc := &PipelineContext{
		Current: map[string]any{"user_id": "from-current"},
		Steps:   map[string]map[string]any{},
	}
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatal(err)
	}
	if result.Output["status_code"] != 200 {
		t.Errorf("expected 200, got %v", result.Output["status_code"])
	}
}
```

### Step 2: Write timeout test

```go
func TestGraphQLStep_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"ok": true}})
	}))
	defer server.Close()

	factory := NewGraphQLStepFactory()
	step, err := factory("timeout_test", map[string]any{
		"url":     server.URL,
		"query":   "{ status }",
		"timeout": "50ms",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	pc := &PipelineContext{Current: map[string]any{}, Steps: map[string]map[string]any{}}
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "context deadline exceeded") && !strings.Contains(err.Error(), "request failed") {
		t.Errorf("expected timeout-related error, got: %v", err)
	}
}
```

### Step 3: Write retry_on_network_error test

```go
func TestGraphQLStep_RetryOnNetworkError(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// Force connection close to simulate network error
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Fatal("server doesn't support hijacking")
			}
			conn, _, _ := hj.Hijack()
			conn.Close()
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"ok": true}})
	}))
	defer server.Close()

	factory := NewGraphQLStepFactory()
	step, err := factory("retry_test", map[string]any{
		"url":                    server.URL,
		"query":                  "{ status }",
		"retry_on_network_error": true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	pc := &PipelineContext{Current: map[string]any{}, Steps: map[string]map[string]any{}}
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("expected retry to succeed, got: %v", err)
	}
	if result.Output["has_errors"] != false {
		t.Error("expected no errors after retry")
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls (fail + retry), got %d", callCount)
	}
}
```

### Step 4: Run tests

Run: `cd /Users/jon/workspace/workflow && go test ./module/ -run "TestGraphQLStep_(Template|Timeout|Retry)" -v`
Expected: PASS

### Step 5: Commit

```bash
cd /Users/jon/workspace/workflow
git add module/pipeline_step_graphql_test.go
git commit -m "test: add template, timeout, and retry tests for step.graphql"
```

---

## Task 9: Update Documentation

**Files:**
- Modify: `DOCUMENTATION.md`

### Step 1: Add step.graphql to the pipeline steps table

Find the pipeline steps table in `DOCUMENTATION.md` and add:

```markdown
| step.graphql | Execute GraphQL queries/mutations with data extraction, pagination, batching, APQ |
```

### Step 2: Add step.graphql configuration section

Add a new section under the pipeline steps documentation:

```markdown
### step.graphql

Executes GraphQL queries and mutations over HTTP POST. Supports OAuth2 authentication
(reuses the same token cache as step.http_call), response data path extraction, cursor and
offset pagination, batch queries, automatic persisted queries (APQ), introspection, and
fragment prepending.

**Config:**

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| url | string | yes | | GraphQL endpoint URL (template-resolved) |
| query | string | yes* | | GraphQL query or mutation (* not required if introspection or batch is configured) |
| variables | map | no | | Query variables (template-resolved values) |
| data_path | string | no | | Dot-path to extract from response data (e.g., "user.posts") |
| auth | map | no | | Authentication config (oauth2_client_credentials, bearer, api_key) |
| headers | map | no | | Custom HTTP headers |
| fragments | list | no | | GraphQL fragments prepended to query |
| pagination | map | no | | Pagination config (strategy, page_info_path, cursor_variable, etc.) |
| batch | map | no | | Batch query config (queries array) |
| persisted_query | map | no | | APQ config (enabled, sha256) |
| introspection | map | no | | Introspection config (enabled) |
| fail_on_graphql_errors | bool | no | true | Whether to fail the step on GraphQL errors |
| timeout | string | no | "30s" | Request timeout duration |

**Output:**

| Key | Type | Description |
|-----|------|-------------|
| data | any | Extracted response data (via data_path if set) |
| errors | array | GraphQL errors (empty array if none) |
| has_errors | bool | Whether GraphQL errors were present |
| raw | map | Full GraphQL response (data + errors + extensions) |
| status_code | int | HTTP status code |
| extensions | any | GraphQL extensions object |
```

### Step 3: Commit

```bash
cd /Users/jon/workspace/workflow
git add DOCUMENTATION.md
git commit -m "docs: add step.graphql to pipeline steps documentation"
```

---

## Task 10: Migrate BMW BwP Steps — Delivery Preview

**Files:**
- Modify: `/Users/jon/workspace/buymywishlist-phase3/app.yaml`

### Step 1: Find the `check_bwp` step in `fulfill-single-item` pipeline

Current:
```yaml
- name: check_bwp
  type: step.bmw.bwp_delivery_preview
  config: {}
```

Replace with:
```yaml
- name: check_bwp
  type: step.graphql
  config:
    url: '{{ config "bwp_api_url" }}'
    auth:
      type: oauth2_client_credentials
      token_url: '{{ config "bwp_token_url" }}'
      client_id: '{{ config "bwp_client_id" }}'
      client_secret: '{{ config "bwp_client_secret" }}'
    query: |
      query DeliveryPreview($asin: String!) {
        deliveryPreview(asin: $asin) {
          eligible
          estimatedDeliveryDate
          shippingCost { amount currency }
        }
      }
    variables:
      asin: '{{ .asin }}'
    data_path: deliveryPreview
    fail_on_graphql_errors: false
```

**Note:** The output keys change from the plugin's custom mapping (`eligible`, `estimated_delivery_date`, `shipping_cost`) to GraphQL field names (`eligible`, `estimatedDeliveryDate`, `shippingCost`). Update downstream references in `route_fulfillment` conditional:
- `steps.check_bwp.eligible` → `steps.check_bwp.data.eligible` (since data_path extracts to `data` key)

### Step 2: Update downstream conditional references

The conditional step `route_fulfillment` uses `field: "steps.check_bwp.eligible"`. With step.graphql, the extracted data is in `output["data"]`, so the field path becomes `steps.check_bwp.data.eligible`.

Similarly, `check_price` references `steps.check_bwp.price_within_10pct` — this field was computed by the plugin step. With the generic step, this logic needs to move to a `step.set` that computes it from the raw GraphQL response.

**Add a step.set after check_bwp to flatten the output:**
```yaml
- name: flatten_bwp
  type: step.set
  config:
    values:
      eligible: '{{ index .steps "check_bwp" "data" "eligible" }}'
      estimated_delivery_date: '{{ index .steps "check_bwp" "data" "estimatedDeliveryDate" }}'
      shipping_cost: '{{ index .steps "check_bwp" "data" "shippingCost" "amount" }}'
```

Update conditionals to reference `steps.flatten_bwp.eligible` and compute price comparison in a template.

### Step 3: Commit

```bash
cd /Users/jon/workspace/buymywishlist-phase3
git add app.yaml
git commit -m "refactor: migrate bwp_delivery_preview to step.graphql in fulfill-single-item"
```

---

## Task 11: Migrate BMW BwP Steps — Create Order

**Files:**
- Modify: `/Users/jon/workspace/buymywishlist-phase3/app.yaml`

### Step 1: Replace auto_order step

Current:
```yaml
- name: auto_order
  type: step.bmw.bwp_create_order
  config: {}
```

Replace with:
```yaml
- name: auto_order
  type: step.graphql
  config:
    url: '{{ config "bwp_api_url" }}'
    auth:
      type: oauth2_client_credentials
      token_url: '{{ config "bwp_token_url" }}'
      client_id: '{{ config "bwp_client_id" }}'
      client_secret: '{{ config "bwp_client_secret" }}'
    query: |
      mutation CreateOrder($asin: String!, $quantity: Int!, $shippingAddress: ShippingAddressInput!) {
        createOrder(input: { asin: $asin, quantity: $quantity, shippingAddress: $shippingAddress }) {
          orderId
          status
          error
        }
      }
    variables:
      asin: '{{ .asin }}'
      quantity: 1
      shippingAddress: '{{ .shipping_address }}'
    data_path: createOrder
```

Update `record_auto` reference from `index .steps "auto_order" "order_id"` to `index .steps "auto_order" "data" "orderId"` (GraphQL uses camelCase).

### Step 2: Commit

```bash
cd /Users/jon/workspace/buymywishlist-phase3
git add app.yaml
git commit -m "refactor: migrate bwp_create_order to step.graphql in fulfill-single-item"
```

---

## Task 12: Migrate BMW BwP Steps — Order Status & Cancel Order

**Files:**
- Modify: `/Users/jon/workspace/buymywishlist-phase3/app.yaml`

### Step 1: Migrate order status in cron-order-tracking

Find the `step.bmw.bwp_order_status` in the `check_each` foreach loop and replace with `step.graphql`:

```yaml
- name: check_status
  type: step.graphql
  config:
    url: '{{ config "bwp_api_url" }}'
    auth:
      type: oauth2_client_credentials
      token_url: '{{ config "bwp_token_url" }}'
      client_id: '{{ config "bwp_client_id" }}'
      client_secret: '{{ config "bwp_client_secret" }}'
    query: |
      query OrderStatus($orderId: String!) {
        order(orderId: $orderId) {
          status
          trackingNumber
          estimatedDelivery
          milestones { name timestamp }
        }
      }
    variables:
      orderId: '{{ .order_id }}'
    data_path: order
    fail_on_graphql_errors: false
```

### Step 2: Migrate cancel order in stripe-webhook-chargeback

Find `try_bwp_cancel` step and replace:

```yaml
- name: try_bwp_cancel
  type: step.graphql
  config:
    url: '{{ config "bwp_api_url" }}'
    auth:
      type: oauth2_client_credentials
      token_url: '{{ config "bwp_token_url" }}'
      client_id: '{{ config "bwp_client_id" }}'
      client_secret: '{{ config "bwp_client_secret" }}'
    query: |
      mutation CancelOrder($orderId: String!) {
        cancelOrder(orderId: $orderId) {
          cancelled
          error
        }
      }
    variables:
      orderId: '{{ index .steps "check_cancel_order" "row" "amazon_order_id" }}'
    data_path: cancelOrder
    fail_on_graphql_errors: false
```

**Note:** This also fixes the data flow issue identified in research — the previous plugin step read `order_id` from current, but the actual data is in `steps.check_cancel_order.row.amazon_order_id`.

### Step 3: Commit

```bash
cd /Users/jon/workspace/buymywishlist-phase3
git add app.yaml
git commit -m "refactor: migrate bwp_order_status and bwp_cancel_order to step.graphql"
```

---

## Task 13: Wire Up Missing Returns Pipeline

**Files:**
- Modify: `/Users/jon/workspace/buymywishlist-phase3/app.yaml`

### Step 1: Add the returns pipeline

Add a new pipeline to app.yaml:

```yaml
  process-return:
    trigger:
      type: http
      config:
        path: /api/v1/fulfillments/{fulfillment_id}/return
        method: POST
    steps:
      - name: auth
        type: step.auth_validate
        config:
          auth_module: auth_jwt
      - name: parse_request
        type: step.request_parse
        config:
          parse_body: true
      - name: lookup_fulfillment
        type: step.db_query
        config:
          database: db
          query: >
            SELECT f.id, f.amazon_order_id, f.status, f.fulfillment_method,
                   wi.title, wi.price, w.user_id
            FROM fulfillments f
            JOIN wishlist_items wi ON f.item_id = wi.id
            JOIN wishlists w ON wi.wishlist_id = w.id
            WHERE f.id = $1 AND w.user_id = $2
            AND f.status IN ('ordered', 'shipped', 'delivered')
          params:
            - '{{ .fulfillment_id }}'
            - '{{ index .steps "auth" "sub" }}'
          mode: single
      - name: check_found
        type: step.conditional
        config:
          field: steps.lookup_fulfillment.found
          routes:
            "false": not_found
          default: check_method
      - name: check_method
        type: step.conditional
        config:
          field: steps.lookup_fulfillment.row.fulfillment_method
          routes:
            automated: process_bwp_return
          default: process_manual_return
      - name: process_bwp_return
        type: step.graphql
        config:
          url: '{{ config "bwp_api_url" }}'
          auth:
            type: oauth2_client_credentials
            token_url: '{{ config "bwp_token_url" }}'
            client_id: '{{ config "bwp_client_id" }}'
            client_secret: '{{ config "bwp_client_secret" }}'
          query: |
            mutation ProcessReturn($orderId: String!, $reason: String!) {
              processReturn(orderId: $orderId, reason: $reason) {
                returnId
                status
                error
              }
            }
          variables:
            orderId: '{{ index .steps "lookup_fulfillment" "row" "amazon_order_id" }}'
            reason: '{{ .body.reason | default "Customer requested return" }}'
          data_path: processReturn
          fail_on_graphql_errors: false
      - name: record_return
        type: step.db_exec
        config:
          database: db
          query: >
            UPDATE fulfillments SET status = 'return_requested',
            routing_reason = $2
            WHERE id = $1
          params:
            - '{{ .fulfillment_id }}'
            - '{{ index .steps "process_bwp_return" "data" "returnId" | default "manual" }}'
      - name: respond_ok
        type: step.json_response
        config:
          status: 200
          body:
            success: true
            return_id: '{{ index .steps "process_bwp_return" "data" "returnId" | default "" }}'
            status: '{{ index .steps "process_bwp_return" "data" "status" | default "pending" }}'
      - name: process_manual_return
        type: step.db_exec
        config:
          database: db
          query: >
            UPDATE fulfillments SET status = 'return_requested',
            routing_reason = 'manual_return'
            WHERE id = $1
          params:
            - '{{ .fulfillment_id }}'
          next: respond_manual
      - name: respond_manual
        type: step.json_response
        config:
          status: 200
          body:
            success: true
            status: manual_review
      - name: not_found
        type: step.json_response
        config:
          status: 404
          body:
            error: Fulfillment not found or not eligible for return
```

### Step 2: Commit

```bash
cd /Users/jon/workspace/buymywishlist-phase3
git add app.yaml
git commit -m "feat: add process-return pipeline using step.graphql for BwP returns"
```

---

## Task 14: Add BwP Config Provider Entries

**Files:**
- Modify: `/Users/jon/workspace/buymywishlist-phase3/app.yaml`

### Step 1: Verify config.provider has BwP entries

Check the config provider section in app.yaml for these keys:
- `bwp_api_url`
- `bwp_token_url`
- `bwp_client_id`
- `bwp_client_secret`

If missing, add them to the config provider's `defaults` source:

```yaml
bwp_api_url: ${BWP_API_URL}
bwp_token_url: ${BWP_TOKEN_URL}
bwp_client_id: ${BWP_CLIENT_ID}
bwp_client_secret: ${BWP_CLIENT_SECRET}
```

### Step 2: Commit (if changes needed)

```bash
cd /Users/jon/workspace/buymywishlist-phase3
git add app.yaml
git commit -m "config: add BwP credential entries to config provider"
```

---

## Task 15: Remove BwP Plugin Steps

**Files:**
- Modify: `/Users/jon/workspace/buymywishlist-phase3/bmwplugin/step_bwp.go`
- Modify: `/Users/jon/workspace/buymywishlist-phase3/bmwplugin/plugin.go` (remove BwP step registrations)

### Step 1: Remove BwP step registrations from plugin.go

Find where `step.bmw.bwp_delivery_preview`, `step.bmw.bwp_create_order`, `step.bmw.bwp_order_status`, `step.bmw.bwp_cancel_order`, and `step.bmw.bwp_process_return` are registered in `plugin.go` and remove those entries.

### Step 2: Delete step_bwp.go

The entire file can be removed since all 5 BwP step types have been migrated to step.graphql in app.yaml.

### Step 3: Verify plugin builds

Run: `cd /Users/jon/workspace/buymywishlist-phase3 && go build ./cmd/bmw-plugin`
Expected: builds successfully

### Step 4: Commit

```bash
cd /Users/jon/workspace/buymywishlist-phase3
git add bmwplugin/step_bwp.go bmwplugin/plugin.go
git commit -m "refactor: remove BwP plugin steps, now handled by step.graphql in app.yaml"
```

---

## Task 16: Update Workflow Engine Version in BMW

**Files:**
- Modify: `/Users/jon/workspace/buymywishlist-phase3/go.mod`

### Step 1: Tag new workflow version with step.graphql

```bash
cd /Users/jon/workspace/workflow
git tag v0.3.31
git push origin v0.3.31
```

### Step 2: Update BMW's go.mod

```bash
cd /Users/jon/workspace/buymywishlist-phase3
go get github.com/GoCodeAlone/workflow@v0.3.31
go mod tidy
```

### Step 3: Verify build

```bash
go build ./cmd/bmw-plugin
```

### Step 4: Commit

```bash
cd /Users/jon/workspace/buymywishlist-phase3
git add go.mod go.sum
git commit -m "deps: update workflow engine to v0.3.31 (step.graphql support)"
```

---

## Task 17: Build, Deploy & Smoke Test

**Files:** None (operational)

### Step 1: Build workflow-server with step.graphql

```bash
cd /Users/jon/workspace/workflow
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o /Users/jon/workspace/buymywishlist-phase3/workflow-server ./cmd/server
```

### Step 2: Build BMW plugin

```bash
cd /Users/jon/workspace/buymywishlist-phase3
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o bmw-plugin ./cmd/bmw-plugin
```

### Step 3: Build UI and Docker image

```bash
cd /Users/jon/workspace/buymywishlist-phase3
cd ui && npm run build && cd ..
rm -rf dist && cp -r ui/dist dist
cp bmwplugin/plugin.json plugin.json
docker build -f Dockerfile.prebuilt -t bmw-app:v71 .
```

### Step 4: Deploy to minikube

```bash
minikube image load bmw-app:v71
kubectl set image deployment/app app=bmw-app:v71
kubectl rollout status deployment/app --timeout=60s
```

### Step 5: Verify deployment

```bash
kubectl logs deployment/app --tail=50 | grep -i "error\|graphql\|started"
```

### Step 6: Run smoke test

```bash
# Port-forward
kubectl port-forward deployment/app 18080:8080 &

# Test a basic endpoint
curl -s http://localhost:18080/api/v1/health | jq .
```

Expected: Server running, no startup errors related to step.graphql or missing BwP steps.
