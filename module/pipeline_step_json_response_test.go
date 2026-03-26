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

func TestJSONResponseStep_BodyFromRef(t *testing.T) {
	factory := NewJSONResponseStepFactory()
	step, err := factory("from-ref", map[string]any{
		"status": 200,
		"body": map[string]any{
			"data": map[string]any{
				"_from": "steps.list-companies.rows",
			},
			"total": map[string]any{
				"_from": "steps.list-companies.count",
			},
		},
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

	var body map[string]any
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	rows, ok := body["data"].([]any)
	if !ok {
		t.Fatalf("expected data to be []any, got %T", body["data"])
	}
	if len(rows) != 2 {
		t.Errorf("expected 2 rows, got %d", len(rows))
	}

	// total should be a number (JSON numbers decode as float64)
	total, ok := body["total"].(float64)
	if !ok {
		t.Fatalf("expected total to be a number, got %T (%v)", body["total"], body["total"])
	}
	if total != 2 {
		t.Errorf("expected total=2, got %v", total)
	}
}

func TestJSONResponseStep_BodyFromRefComposite(t *testing.T) {
	factory := NewJSONResponseStepFactory()
	step, err := factory("composite-response", map[string]any{
		"status": 200,
		"body": map[string]any{
			"data": map[string]any{
				"_from": "steps.fetch.rows",
			},
			"meta": map[string]any{
				"total": map[string]any{
					"_from": "steps.fetch.count",
				},
				"page": 1,
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	recorder := httptest.NewRecorder()
	pc := NewPipelineContext(nil, map[string]any{
		"_http_response_writer": recorder,
	})
	pc.MergeStepOutput("fetch", map[string]any{
		"rows": []map[string]any{
			{"id": "r1", "name": "Row One"},
		},
		"count": 42,
	})

	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	var body map[string]any
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	rows, ok := body["data"].([]any)
	if !ok {
		t.Fatalf("expected data to be []any, got %T", body["data"])
	}
	if len(rows) != 1 {
		t.Errorf("expected 1 row, got %d", len(rows))
	}

	meta, ok := body["meta"].(map[string]any)
	if !ok {
		t.Fatalf("expected meta to be map, got %T", body["meta"])
	}
	if meta["total"] != float64(42) {
		t.Errorf("expected meta.total=42, got %v", meta["total"])
	}
	if meta["page"] != float64(1) {
		t.Errorf("expected meta.page=1, got %v", meta["page"])
	}
}

func TestJSONResponseStep_BodyFromRefMissingPath(t *testing.T) {
	factory := NewJSONResponseStepFactory()
	step, err := factory("missing-ref", map[string]any{
		"status": 200,
		"body": map[string]any{
			"data": map[string]any{
				"_from": "steps.nonexistent.rows",
			},
		},
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

	var body map[string]any
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	// Missing paths should resolve to nil; the key may be absent or null.
	_ = body["data"]
}

func TestJSONResponseStep_BodyFromRefInArray(t *testing.T) {
	factory := NewJSONResponseStepFactory()
	step, err := factory("array-from-ref", map[string]any{
		"status": 200,
		"body": map[string]any{
			"items": []any{
				map[string]any{"_from": "steps.first.data"},
				map[string]any{"_from": "steps.second.data"},
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	recorder := httptest.NewRecorder()
	pc := NewPipelineContext(nil, map[string]any{
		"_http_response_writer": recorder,
	})
	pc.MergeStepOutput("first", map[string]any{
		"data": map[string]any{"id": "a1", "label": "Alpha"},
	})
	pc.MergeStepOutput("second", map[string]any{
		"data": map[string]any{"id": "b2", "label": "Beta"},
	})

	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	var body map[string]any
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	items, ok := body["items"].([]any)
	if !ok {
		t.Fatalf("expected items to be []any, got %T", body["items"])
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	first, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("expected items[0] to be map, got %T", items[0])
	}
	if first["id"] != "a1" {
		t.Errorf("expected items[0].id='a1', got %v", first["id"])
	}

	second, ok := items[1].(map[string]any)
	if !ok {
		t.Fatalf("expected items[1] to be map, got %T", items[1])
	}
	if second["id"] != "b2" {
		t.Errorf("expected items[1].id='b2', got %v", second["id"])
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

func TestJSONResponseStep_StatusFrom(t *testing.T) {
	factory := NewJSONResponseStepFactory()
	step, err := factory("proxy-respond", map[string]any{
		"status_from": "steps.call_upstream.status_code",
		"body_from":   "steps.call_upstream.body",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	recorder := httptest.NewRecorder()
	pc := NewPipelineContext(nil, map[string]any{
		"_http_response_writer": recorder,
	})
	pc.MergeStepOutput("call_upstream", map[string]any{
		"status_code": 404,
		"body":        map[string]any{"error": "not found"},
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
		t.Errorf("expected status 404 from status_from, got %d", resp.StatusCode)
	}

	if result.Output["status"] != 404 {
		t.Errorf("expected output status=404, got %v", result.Output["status"])
	}
}

func TestJSONResponseStep_StatusFromFloat64(t *testing.T) {
	// JSON numbers are often decoded as float64; ensure conversion works.
	factory := NewJSONResponseStepFactory()
	step, err := factory("proxy-respond", map[string]any{
		"status_from": "steps.upstream.status_code",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	recorder := httptest.NewRecorder()
	pc := NewPipelineContext(nil, map[string]any{
		"_http_response_writer": recorder,
	})
	pc.MergeStepOutput("upstream", map[string]any{
		"status_code": float64(503),
	})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	resp := recorder.Result()
	if resp.StatusCode != 503 {
		t.Errorf("expected status 503 from float64 status_from, got %d", resp.StatusCode)
	}
	if result.Output["status"] != 503 {
		t.Errorf("expected output status=503, got %v", result.Output["status"])
	}
}

func TestJSONResponseStep_StatusFromFallback(t *testing.T) {
	// When status_from cannot be resolved, fall back to static status.
	factory := NewJSONResponseStepFactory()
	step, err := factory("fallback-respond", map[string]any{
		"status":      201,
		"status_from": "steps.nonexistent.status_code",
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

	resp := recorder.Result()
	if resp.StatusCode != 201 {
		t.Errorf("expected fallback status 201, got %d", resp.StatusCode)
	}
	if result.Output["status"] != 201 {
		t.Errorf("expected output status=201, got %v", result.Output["status"])
	}
}

func TestJSONResponseStep_StatusFromPrecedence(t *testing.T) {
	// status_from takes precedence over static status when resolved.
	factory := NewJSONResponseStepFactory()
	step, err := factory("precedence-respond", map[string]any{
		"status":      200,
		"status_from": "steps.upstream.status_code",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	recorder := httptest.NewRecorder()
	pc := NewPipelineContext(nil, map[string]any{
		"_http_response_writer": recorder,
	})
	pc.MergeStepOutput("upstream", map[string]any{
		"status_code": 422,
	})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	resp := recorder.Result()
	if resp.StatusCode != 422 {
		t.Errorf("expected status_from 422 to take precedence, got %d", resp.StatusCode)
	}
	if result.Output["status"] != 422 {
		t.Errorf("expected output status=422, got %v", result.Output["status"])
	}
}

func TestJSONResponseStep_StatusFromNoWriter(t *testing.T) {
	// Verify status_from is reflected in output even without an HTTP writer.
	factory := NewJSONResponseStepFactory()
	step, err := factory("no-writer-status-from", map[string]any{
		"status_from": "steps.upstream.status_code",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, map[string]any{})
	pc.MergeStepOutput("upstream", map[string]any{
		"status_code": 503,
	})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if !result.Stop {
		t.Error("expected Stop=true even without writer")
	}
	if result.Output["status"] != 503 {
		t.Errorf("expected status=503, got %v", result.Output["status"])
	}
}

func TestJSONResponseStep_StatusFromFractionalFloat(t *testing.T) {
	// A fractional float (e.g. 404.9) is not a valid HTTP status code; fall back.
	factory := NewJSONResponseStepFactory()
	step, err := factory("fractional-status", map[string]any{
		"status":      201,
		"status_from": "steps.upstream.status_code",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	recorder := httptest.NewRecorder()
	pc := NewPipelineContext(nil, map[string]any{
		"_http_response_writer": recorder,
	})
	pc.MergeStepOutput("upstream", map[string]any{
		"status_code": float64(404.9),
	})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	resp := recorder.Result()
	if resp.StatusCode != 201 {
		t.Errorf("expected fallback to static status 201 for fractional float, got %d", resp.StatusCode)
	}
	if result.Output["status"] != 201 {
		t.Errorf("expected output status=201, got %v", result.Output["status"])
	}
}

func TestJSONResponseStep_StatusFromOutOfRange(t *testing.T) {
	// Out-of-range status codes (< 100 or > 599) must fall back to static status.
	tests := []struct {
		name   string
		code   int
		static int
	}{
		{"zero", 0, 200},
		{"negative", -1, 200},
		{"too-large", 9999, 404},
		{"boundary-low", 99, 200},
		{"boundary-high", 600, 200},
	}

	factory := NewJSONResponseStepFactory()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			step, err := factory("out-of-range-status", map[string]any{
				"status":      tc.static,
				"status_from": "steps.upstream.code",
			}, nil)
			if err != nil {
				t.Fatalf("factory error: %v", err)
			}

			recorder := httptest.NewRecorder()
			pc := NewPipelineContext(nil, map[string]any{
				"_http_response_writer": recorder,
			})
			pc.MergeStepOutput("upstream", map[string]any{
				"code": tc.code,
			})

			result, err := step.Execute(context.Background(), pc)
			if err != nil {
				t.Fatalf("execute error: %v", err)
			}

			resp := recorder.Result()
			if resp.StatusCode != tc.static {
				t.Errorf("code %d: expected fallback to %d, got %d", tc.code, tc.static, resp.StatusCode)
			}
			if result.Output["status"] != tc.static {
				t.Errorf("code %d: expected output status=%d, got %v", tc.code, tc.static, result.Output["status"])
			}
		})
	}
}

func TestJSONResponseStep_StatusFromBoundaryValid(t *testing.T) {
	// Boundary values within valid HTTP range (100–599) should be accepted.
	tests := []struct {
		name string
		code int
	}{
		{"min", 100},
		{"max", 599},
		{"common", 200},
	}

	factory := NewJSONResponseStepFactory()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			step, err := factory("boundary-valid", map[string]any{
				"status":      200,
				"status_from": "steps.upstream.code",
			}, nil)
			if err != nil {
				t.Fatalf("factory error: %v", err)
			}

			recorder := httptest.NewRecorder()
			pc := NewPipelineContext(nil, map[string]any{
				"_http_response_writer": recorder,
			})
			pc.MergeStepOutput("upstream", map[string]any{
				"code": tc.code,
			})

			result, err := step.Execute(context.Background(), pc)
			if err != nil {
				t.Fatalf("execute error: %v", err)
			}

			resp := recorder.Result()
			if resp.StatusCode != tc.code {
				t.Errorf("code %d: expected status %d, got %d", tc.code, tc.code, resp.StatusCode)
			}
			if result.Output["status"] != tc.code {
				t.Errorf("code %d: expected output status=%d, got %v", tc.code, tc.code, result.Output["status"])
			}
		})
	}
}
