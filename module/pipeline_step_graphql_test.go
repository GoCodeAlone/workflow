package module

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
