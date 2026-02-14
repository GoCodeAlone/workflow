package tracing

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Endpoint != "localhost:4318" {
		t.Errorf("expected default endpoint localhost:4318, got %s", cfg.Endpoint)
	}
	if cfg.ServiceName != "workflow" {
		t.Errorf("expected default service name workflow, got %s", cfg.ServiceName)
	}
	if cfg.SampleRate != 1.0 {
		t.Errorf("expected default sample rate 1.0, got %f", cfg.SampleRate)
	}
	if !cfg.Insecure {
		t.Error("expected default insecure to be true")
	}
}

func TestProvider_ShutdownNil(t *testing.T) {
	p := &Provider{}
	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown of nil provider should not error: %v", err)
	}
}

func TestProvider_Tracer(t *testing.T) {
	// Use an in-memory exporter to avoid network calls.
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
	)

	p := &Provider{
		tp:     tp,
		tracer: tp.Tracer("test"),
	}

	tr := p.Tracer()
	if tr == nil {
		t.Fatal("expected non-nil tracer")
	}

	if p.TracerProvider() != tp {
		t.Error("TracerProvider() should return the underlying provider")
	}

	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}
}

func TestProvider_GlobalTracerSet(t *testing.T) {
	// Use an in-memory exporter to avoid network calls.
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
	)

	// Simulate what NewProvider does for the global provider.
	otel.SetTracerProvider(tp)
	defer func() {
		_ = tp.Shutdown(context.Background())
	}()

	got := otel.GetTracerProvider()
	if got != tp {
		t.Error("expected global tracer provider to match")
	}
}
