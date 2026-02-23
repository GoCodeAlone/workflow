package module

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidateRequestBodyStepFactory(t *testing.T) {
	factory := NewValidateRequestBodyStepFactory()

	t.Run("no required fields", func(t *testing.T) {
		step, err := factory("test", map[string]any{}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if step.Name() != "test" {
			t.Fatalf("expected name 'test', got %q", step.Name())
		}
	})

	t.Run("with required fields", func(t *testing.T) {
		step, err := factory("test", map[string]any{
			"required_fields": []any{"config"},
		}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if step.Name() != "test" {
			t.Fatalf("expected name 'test', got %q", step.Name())
		}
	})
}

func TestValidateRequestBodyStep_Execute(t *testing.T) {
	factory := NewValidateRequestBodyStepFactory()

	t.Run("valid body from trigger data", func(t *testing.T) {
		step, _ := factory("test", map[string]any{
			"required_fields": []any{"config"},
		}, nil)

		pc := &PipelineContext{
			TriggerData: map[string]any{
				"body": map[string]any{"config": "test-data"},
			},
			Current: map[string]any{},
		}

		result, err := step.Execute(context.Background(), pc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		body, ok := result.Output["body"].(map[string]any)
		if !ok {
			t.Fatal("expected body in output")
		}
		if body["config"] != "test-data" {
			t.Fatalf("expected config='test-data', got %v", body["config"])
		}
	})

	t.Run("missing required field", func(t *testing.T) {
		step, _ := factory("test", map[string]any{
			"required_fields": []any{"config"},
		}, nil)

		pc := &PipelineContext{
			TriggerData: map[string]any{
				"body": map[string]any{"other": "value"},
			},
			Current: map[string]any{},
		}

		_, err := step.Execute(context.Background(), pc)
		if err == nil {
			t.Fatal("expected error for missing required field")
		}
	})

	t.Run("no body when required", func(t *testing.T) {
		step, _ := factory("test", map[string]any{
			"required_fields": []any{"config"},
		}, nil)

		pc := &PipelineContext{
			TriggerData: map[string]any{},
			Current:     map[string]any{},
			Metadata:    map[string]any{},
		}

		_, err := step.Execute(context.Background(), pc)
		if err == nil {
			t.Fatal("expected error for missing body")
		}
	})

	t.Run("body from HTTP request", func(t *testing.T) {
		step, _ := factory("test", map[string]any{
			"required_fields": []any{"name"},
		}, nil)

		body := `{"name": "test-config"}`
		req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		pc := &PipelineContext{
			TriggerData: map[string]any{},
			Current:     map[string]any{},
			Metadata:    map[string]any{"_http_request": req},
		}

		result, err := step.Execute(context.Background(), pc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		parsedBody, ok := result.Output["body"].(map[string]any)
		if !ok {
			t.Fatal("expected body in output")
		}
		if parsedBody["name"] != "test-config" {
			t.Fatalf("expected name='test-config', got %v", parsedBody["name"])
		}
	})

	t.Run("no required fields allows empty body", func(t *testing.T) {
		step, _ := factory("test", map[string]any{}, nil)

		pc := &PipelineContext{
			TriggerData: map[string]any{},
			Current:     map[string]any{},
			Metadata:    map[string]any{},
		}

		result, err := step.Execute(context.Background(), pc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("expected non-nil result")
		}
	})
}
