package module

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
		Current:     map[string]any{},
		StepOutputs: map[string]map[string]any{},
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

	pc := &PipelineContext{Current: map[string]any{}, StepOutputs: map[string]map[string]any{}}
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

	pc := &PipelineContext{Current: map[string]any{}, StepOutputs: map[string]map[string]any{}}
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

	pc := &PipelineContext{Current: map[string]any{}, StepOutputs: map[string]map[string]any{}}
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

	pc := &PipelineContext{Current: map[string]any{}, StepOutputs: map[string]map[string]any{}}
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

func TestGraphQLStep_OffsetPagination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Variables map[string]any `json:"variables"`
		}
		json.NewDecoder(r.Body).Decode(&req)

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

	pc := &PipelineContext{Current: map[string]any{}, StepOutputs: map[string]map[string]any{}}
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

	pc := &PipelineContext{Current: map[string]any{}, StepOutputs: map[string]map[string]any{}}
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

	pc := &PipelineContext{Current: map[string]any{}, StepOutputs: map[string]map[string]any{}}
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

	pc := &PipelineContext{Current: map[string]any{}, StepOutputs: map[string]map[string]any{}}
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
		Current:     map[string]any{"user_id": "from-current"},
		StepOutputs: map[string]map[string]any{},
	}
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatal(err)
	}
	if result.Output["status_code"] != 200 {
		t.Errorf("expected 200, got %v", result.Output["status_code"])
	}
}

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

	pc := &PipelineContext{Current: map[string]any{}, StepOutputs: map[string]map[string]any{}}
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "context deadline exceeded") && !strings.Contains(err.Error(), "request failed") {
		t.Errorf("expected timeout-related error, got: %v", err)
	}
}

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

	pc := &PipelineContext{Current: map[string]any{}, StepOutputs: map[string]map[string]any{}}
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

	pc := &PipelineContext{Current: map[string]any{}, StepOutputs: map[string]map[string]any{}}
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
