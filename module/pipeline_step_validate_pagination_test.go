package module

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidatePaginationStepFactory(t *testing.T) {
	factory := NewValidatePaginationStepFactory()

	t.Run("defaults", func(t *testing.T) {
		step, err := factory("test", map[string]any{}, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if step.Name() != "test" {
			t.Fatalf("expected name 'test', got %q", step.Name())
		}
	})

	t.Run("invalid max_limit", func(t *testing.T) {
		_, err := factory("test", map[string]any{"max_limit": 0}, nil)
		if err == nil {
			t.Fatal("expected error for zero max_limit")
		}
	})
}

func TestValidatePaginationStep_Execute(t *testing.T) {
	factory := NewValidatePaginationStepFactory()

	t.Run("defaults applied", func(t *testing.T) {
		step, _ := factory("test", map[string]any{}, nil)
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		pc := &PipelineContext{
			Metadata: map[string]any{"_http_request": req},
			Current:  map[string]any{},
		}

		result, err := step.Execute(context.Background(), pc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Output["page"] != 1 {
			t.Fatalf("expected page=1, got %v", result.Output["page"])
		}
		if result.Output["limit"] != 20 {
			t.Fatalf("expected limit=20, got %v", result.Output["limit"])
		}
		if result.Output["offset"] != 0 {
			t.Fatalf("expected offset=0, got %v", result.Output["offset"])
		}
	})

	t.Run("custom values", func(t *testing.T) {
		step, _ := factory("test", map[string]any{"max_limit": 50}, nil)
		req := httptest.NewRequest(http.MethodGet, "/test?page=3&limit=10", nil)
		pc := &PipelineContext{
			Metadata: map[string]any{"_http_request": req},
			Current:  map[string]any{},
		}

		result, err := step.Execute(context.Background(), pc)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Output["page"] != 3 {
			t.Fatalf("expected page=3, got %v", result.Output["page"])
		}
		if result.Output["limit"] != 10 {
			t.Fatalf("expected limit=10, got %v", result.Output["limit"])
		}
		if result.Output["offset"] != 20 {
			t.Fatalf("expected offset=20, got %v", result.Output["offset"])
		}
	})

	t.Run("invalid page", func(t *testing.T) {
		step, _ := factory("test", map[string]any{}, nil)
		req := httptest.NewRequest(http.MethodGet, "/test?page=-1", nil)
		pc := &PipelineContext{
			Metadata: map[string]any{"_http_request": req},
			Current:  map[string]any{},
		}

		_, err := step.Execute(context.Background(), pc)
		if err == nil {
			t.Fatal("expected error for negative page")
		}
	})

	t.Run("limit exceeds max", func(t *testing.T) {
		step, _ := factory("test", map[string]any{"max_limit": 50}, nil)
		req := httptest.NewRequest(http.MethodGet, "/test?limit=200", nil)
		pc := &PipelineContext{
			Metadata: map[string]any{"_http_request": req},
			Current:  map[string]any{},
		}

		_, err := step.Execute(context.Background(), pc)
		if err == nil {
			t.Fatal("expected error for limit exceeding max")
		}
	})

	t.Run("non-numeric limit", func(t *testing.T) {
		step, _ := factory("test", map[string]any{}, nil)
		req := httptest.NewRequest(http.MethodGet, "/test?limit=abc", nil)
		pc := &PipelineContext{
			Metadata: map[string]any{"_http_request": req},
			Current:  map[string]any{},
		}

		_, err := step.Execute(context.Background(), pc)
		if err == nil {
			t.Fatal("expected error for non-numeric limit")
		}
	})
}
