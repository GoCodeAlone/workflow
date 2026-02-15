package module

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestJSONResponseStep_BasicResponse(t *testing.T) {
	factory := NewJSONResponseStepFactory()
	step, err := factory("respond", map[string]any{
		"status": 200,
		"body": map[string]any{
			"message": "success",
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	recorder := httptest.NewRecorder()
	pc := NewPipelineContext(nil, map[string]any{
		"_http_response_writer": recorder,
	})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if !result.Stop {
		t.Error("expected Stop=true")
	}

	resp := recorder.Result()
	if resp.StatusCode != 200 {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)
	if body["message"] != "success" {
		t.Errorf("expected message='success', got %v", body["message"])
	}

	if pc.Metadata["_response_handled"] != true {
		t.Error("expected _response_handled=true")
	}
}

func TestJSONResponseStep_CustomStatus(t *testing.T) {
	factory := NewJSONResponseStepFactory()
	step, err := factory("not-found", map[string]any{
		"status": 404,
		"body": map[string]any{
			"error": "not found",
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	recorder := httptest.NewRecorder()
	pc := NewPipelineContext(nil, map[string]any{
		"_http_response_writer": recorder,
	})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if !result.Stop {
		t.Error("expected Stop=true")
	}

	resp := recorder.Result()
	if resp.StatusCode != 404 {
		t.Errorf("expected status 404, got %d", resp.StatusCode)
	}
}

func TestJSONResponseStep_CustomHeaders(t *testing.T) {
	factory := NewJSONResponseStepFactory()
	step, err := factory("with-headers", map[string]any{
		"status": 201,
		"headers": map[string]any{
			"X-Custom": "test-value",
		},
		"body": map[string]any{"ok": true},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	recorder := httptest.NewRecorder()
	pc := NewPipelineContext(nil, map[string]any{
		"_http_response_writer": recorder,
	})

	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if recorder.Header().Get("X-Custom") != "test-value" {
		t.Errorf("expected X-Custom header, got %q", recorder.Header().Get("X-Custom"))
	}
	if recorder.Header().Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type: application/json")
	}
}

func TestJSONResponseStep_BodyFrom(t *testing.T) {
	factory := NewJSONResponseStepFactory()
	step, err := factory("from-step", map[string]any{
		"status":    200,
		"body_from": "steps.get-company.row",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	recorder := httptest.NewRecorder()
	pc := NewPipelineContext(nil, map[string]any{
		"_http_response_writer": recorder,
	})
	pc.MergeStepOutput("get-company", map[string]any{
		"row": map[string]any{
			"id":   "c1",
			"name": "Acme Corp",
		},
		"found": true,
	})

	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	var body map[string]any
	json.NewDecoder(recorder.Body).Decode(&body)
	if body["id"] != "c1" {
		t.Errorf("expected id='c1', got %v", body["id"])
	}
	if body["name"] != "Acme Corp" {
		t.Errorf("expected name='Acme Corp', got %v", body["name"])
	}
}

func TestJSONResponseStep_TemplateBody(t *testing.T) {
	factory := NewJSONResponseStepFactory()
	step, err := factory("templated", map[string]any{
		"status": 201,
		"body": map[string]any{
			"id":      "{{ .steps.prepare.id }}",
			"message": "created",
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	recorder := httptest.NewRecorder()
	pc := NewPipelineContext(nil, map[string]any{
		"_http_response_writer": recorder,
	})
	pc.MergeStepOutput("prepare", map[string]any{"id": "new-id-123"})

	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	var body map[string]any
	json.NewDecoder(recorder.Body).Decode(&body)
	if body["id"] != "new-id-123" {
		t.Errorf("expected id='new-id-123', got %v", body["id"])
	}
}

func TestJSONResponseStep_NoWriter(t *testing.T) {
	factory := NewJSONResponseStepFactory()
	step, err := factory("no-writer", map[string]any{
		"status": 200,
		"body": map[string]any{
			"data": "test",
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, map[string]any{})
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	// Should still return Stop=true and have output
	if !result.Stop {
		t.Error("expected Stop=true even without writer")
	}
	if result.Output["status"] != 200 {
		t.Errorf("expected status=200, got %v", result.Output["status"])
	}
}

func TestJSONResponseStep_BodyFromRows(t *testing.T) {
	factory := NewJSONResponseStepFactory()
	step, err := factory("list-response", map[string]any{
		"status":    200,
		"body_from": "steps.list-companies.rows",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	recorder := httptest.NewRecorder()
	pc := NewPipelineContext(nil, map[string]any{
		"_http_response_writer": recorder,
	})
	pc.MergeStepOutput("list-companies", map[string]any{
		"rows": []map[string]any{
			{"id": "c1", "name": "Acme"},
			{"id": "c2", "name": "Beta"},
		},
		"count": 2,
	})

	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	var body []any
	json.NewDecoder(recorder.Body).Decode(&body)
	if len(body) != 2 {
		t.Errorf("expected 2 items in response, got %d", len(body))
	}
}

func TestJSONResponseStep_DefaultStatus(t *testing.T) {
	factory := NewJSONResponseStepFactory()
	step, err := factory("default-status", map[string]any{
		"body": map[string]any{"ok": true},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	recorder := httptest.NewRecorder()
	pc := NewPipelineContext(nil, map[string]any{
		"_http_response_writer": recorder,
	})

	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	resp := recorder.Result()
	if resp.StatusCode != 200 {
		t.Errorf("expected default status 200, got %d", resp.StatusCode)
	}
}
