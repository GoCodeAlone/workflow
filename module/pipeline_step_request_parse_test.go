package module

import (
	"bytes"
	"context"
	"net/http"
	"testing"
)

func TestRequestParseStep_PathParams(t *testing.T) {
	factory := NewRequestParseStepFactory()
	step, err := factory("parse-req", map[string]any{
		"path_params": []any{"id"},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	req, _ := http.NewRequest("GET", "/api/v1/admin/companies/abc-123", nil)
	pc := NewPipelineContext(nil, map[string]any{
		"_http_request":  req,
		"_route_pattern": "/api/v1/admin/companies/{id}",
	})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	pathParams, ok := result.Output["path_params"].(map[string]any)
	if !ok {
		t.Fatal("expected path_params in output")
	}
	if pathParams["id"] != "abc-123" {
		t.Errorf("expected id='abc-123', got %v", pathParams["id"])
	}
}

func TestRequestParseStep_QueryParams(t *testing.T) {
	factory := NewRequestParseStepFactory()
	step, err := factory("parse-query", map[string]any{
		"query_params": []any{"status", "page"},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	req, _ := http.NewRequest("GET", "/api/v1/companies?status=active&page=2", nil)
	pc := NewPipelineContext(nil, map[string]any{
		"_http_request": req,
	})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	query, ok := result.Output["query"].(map[string]any)
	if !ok {
		t.Fatal("expected query in output")
	}
	if query["status"] != "active" {
		t.Errorf("expected status='active', got %v", query["status"])
	}
	if query["page"] != "2" {
		t.Errorf("expected page='2', got %v", query["page"])
	}
}

func TestRequestParseStep_ParseBody(t *testing.T) {
	factory := NewRequestParseStepFactory()
	step, err := factory("parse-body", map[string]any{
		"parse_body": true,
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	body := bytes.NewBufferString(`{"name":"Acme Corp","slug":"acme"}`)
	req, _ := http.NewRequest("POST", "/api/v1/companies", body)
	req.Header.Set("Content-Type", "application/json")

	pc := NewPipelineContext(nil, map[string]any{
		"_http_request": req,
	})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	bodyData, ok := result.Output["body"].(map[string]any)
	if !ok {
		t.Fatal("expected body in output")
	}
	if bodyData["name"] != "Acme Corp" {
		t.Errorf("expected name='Acme Corp', got %v", bodyData["name"])
	}
}

func TestRequestParseStep_MultiplePathParams(t *testing.T) {
	factory := NewRequestParseStepFactory()
	step, err := factory("parse-multi", map[string]any{
		"path_params": []any{"companyId", "orgId"},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	req, _ := http.NewRequest("GET", "/api/v1/admin/companies/c1/organizations/o2", nil)
	pc := NewPipelineContext(nil, map[string]any{
		"_http_request":  req,
		"_route_pattern": "/api/v1/admin/companies/{companyId}/organizations/{orgId}",
	})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	pathParams, ok := result.Output["path_params"].(map[string]any)
	if !ok {
		t.Fatal("expected path_params in output")
	}
	if pathParams["companyId"] != "c1" {
		t.Errorf("expected companyId='c1', got %v", pathParams["companyId"])
	}
	if pathParams["orgId"] != "o2" {
		t.Errorf("expected orgId='o2', got %v", pathParams["orgId"])
	}
}

func TestRequestParseStep_EmptyConfig(t *testing.T) {
	factory := NewRequestParseStepFactory()
	step, err := factory("parse-empty", map[string]any{}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if len(result.Output) != 0 {
		t.Errorf("expected empty output, got %v", result.Output)
	}
}

func TestRequestParseStep_NoRequest(t *testing.T) {
	factory := NewRequestParseStepFactory()
	step, err := factory("parse-noreq", map[string]any{
		"path_params":  []any{"id"},
		"query_params": []any{"q"},
		"parse_body":   true,
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, map[string]any{})
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	// Should have empty path_params and query maps, no body
	pathParams, _ := result.Output["path_params"].(map[string]any)
	if len(pathParams) != 0 {
		t.Errorf("expected empty path_params, got %v", pathParams)
	}
}
