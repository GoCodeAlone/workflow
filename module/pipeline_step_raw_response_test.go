package module

import (
	"context"
	"io"
	"net/http/httptest"
	"testing"
)

func TestRawResponseStep_BasicXML(t *testing.T) {
	factory := NewRawResponseStepFactory()
	step, err := factory("respond", map[string]any{
		"content_type": "text/xml",
		"body":         `<?xml version="1.0" encoding="UTF-8"?><Response></Response>`,
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
	if ct := resp.Header.Get("Content-Type"); ct != "text/xml" {
		t.Errorf("expected Content-Type text/xml, got %q", ct)
	}

	body, _ := io.ReadAll(resp.Body)
	expected := `<?xml version="1.0" encoding="UTF-8"?><Response></Response>`
	if string(body) != expected {
		t.Errorf("expected body %q, got %q", expected, string(body))
	}

	if pc.Metadata["_response_handled"] != true {
		t.Error("expected _response_handled=true")
	}
}

func TestRawResponseStep_CustomStatus(t *testing.T) {
	factory := NewRawResponseStepFactory()
	step, err := factory("health", map[string]any{
		"status":       503,
		"content_type": "text/plain",
		"body":         "Service Unavailable",
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
	if resp.StatusCode != 503 {
		t.Errorf("expected status 503, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "Service Unavailable" {
		t.Errorf("expected body 'Service Unavailable', got %q", string(body))
	}
}

func TestRawResponseStep_CustomHeaders(t *testing.T) {
	factory := NewRawResponseStepFactory()
	step, err := factory("with-headers", map[string]any{
		"content_type": "text/html",
		"headers": map[string]any{
			"X-Custom":      "test-value",
			"Cache-Control": "no-cache",
		},
		"body": "<html><body>OK</body></html>",
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

	if recorder.Header().Get("Content-Type") != "text/html" {
		t.Errorf("expected Content-Type text/html, got %q", recorder.Header().Get("Content-Type"))
	}
	if recorder.Header().Get("X-Custom") != "test-value" {
		t.Errorf("expected X-Custom header, got %q", recorder.Header().Get("X-Custom"))
	}
	if recorder.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("expected Cache-Control header, got %q", recorder.Header().Get("Cache-Control"))
	}
}

func TestRawResponseStep_TemplateBody(t *testing.T) {
	factory := NewRawResponseStepFactory()
	step, err := factory("templated", map[string]any{
		"content_type": "text/xml",
		"body":         `<Response><Id>{{ .steps.prepare.id }}</Id></Response>`,
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

	body, _ := io.ReadAll(recorder.Body)
	expected := `<Response><Id>new-id-123</Id></Response>`
	if string(body) != expected {
		t.Errorf("expected body %q, got %q", expected, string(body))
	}
}

func TestRawResponseStep_BodyFrom(t *testing.T) {
	factory := NewRawResponseStepFactory()
	step, err := factory("from-step", map[string]any{
		"content_type": "text/plain",
		"body_from":    "steps.generate.content",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	recorder := httptest.NewRecorder()
	pc := NewPipelineContext(nil, map[string]any{
		"_http_response_writer": recorder,
	})
	pc.MergeStepOutput("generate", map[string]any{
		"content": "Hello from pipeline context",
	})

	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	body, _ := io.ReadAll(recorder.Body)
	if string(body) != "Hello from pipeline context" {
		t.Errorf("expected body 'Hello from pipeline context', got %q", string(body))
	}
}

func TestRawResponseStep_NoWriter(t *testing.T) {
	factory := NewRawResponseStepFactory()
	step, err := factory("no-writer", map[string]any{
		"content_type": "text/plain",
		"body":         "test body",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, map[string]any{})
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if !result.Stop {
		t.Error("expected Stop=true even without writer")
	}
	if result.Output["status"] != 200 {
		t.Errorf("expected status=200, got %v", result.Output["status"])
	}
	if result.Output["content_type"] != "text/plain" {
		t.Errorf("expected content_type=text/plain, got %v", result.Output["content_type"])
	}
	if result.Output["body"] != "test body" {
		t.Errorf("expected body='test body', got %v", result.Output["body"])
	}
}

func TestRawResponseStep_DefaultStatus(t *testing.T) {
	factory := NewRawResponseStepFactory()
	step, err := factory("default-status", map[string]any{
		"content_type": "text/plain",
		"body":         "ok",
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

func TestRawResponseStep_MissingContentType(t *testing.T) {
	factory := NewRawResponseStepFactory()
	_, err := factory("bad", map[string]any{
		"body": "test",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing content_type")
	}
}

func TestRawResponseStep_EmptyBody(t *testing.T) {
	factory := NewRawResponseStepFactory()
	step, err := factory("empty", map[string]any{
		"status":       204,
		"content_type": "text/plain",
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
	if resp.StatusCode != 204 {
		t.Errorf("expected status 204, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if len(body) != 0 {
		t.Errorf("expected empty body, got %q", string(body))
	}
}
