package tracing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func setupTestProvider(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})
	return exporter
}

func TestSpanMiddleware_CreatesSpan(t *testing.T) {
	exporter := setupTestProvider(t)

	handler := SpanMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Name != "GET /api/test" {
		t.Errorf("expected span name 'GET /api/test', got %q", spans[0].Name)
	}
}

func TestSpanMiddleware_CapturesStatusCode(t *testing.T) {
	exporter := setupTestProvider(t)

	handler := SpanMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	req := httptest.NewRequest(http.MethodPost, "/missing", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	// Check that the span has an error attribute for 4xx
	found := false
	for _, attr := range spans[0].Attributes {
		if string(attr.Key) == "error" && attr.Value.AsBool() {
			found = true
		}
	}
	if !found {
		t.Error("expected error attribute on 404 span")
	}
}

func TestSpanMiddleware_DefaultStatusOK(t *testing.T) {
	setupTestProvider(t)

	var capturedCode int
	handler := SpanMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Write body without explicit WriteHeader
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	capturedCode = rec.Code
	if capturedCode != http.StatusOK {
		t.Errorf("expected 200, got %d", capturedCode)
	}
}

func TestResponseWriter_WriteHeaderOnce(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: rec, statusCode: http.StatusOK}

	rw.WriteHeader(http.StatusCreated)
	rw.WriteHeader(http.StatusBadRequest) // should be ignored

	if rw.statusCode != http.StatusCreated {
		t.Errorf("expected status 201, got %d", rw.statusCode)
	}
}

func TestScheme(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	if s := scheme(req); s != "http" {
		t.Errorf("expected http, got %s", s)
	}
}
