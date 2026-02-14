package tracing

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func newTestTracer(t *testing.T) (*WorkflowTracer, *tracetest.InMemoryExporter) {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})
	tracer := NewWorkflowTracer(tp.Tracer("test"))
	return tracer, exporter
}

func TestWorkflowTracer_StartWorkflow(t *testing.T) {
	wt, exporter := newTestTracer(t)

	ctx, span := wt.StartWorkflow(context.Background(), "http", "create")
	if ctx == nil {
		t.Fatal("expected non-nil context")
	}
	span.End()

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Name != "workflow.execute" {
		t.Errorf("expected span name 'workflow.execute', got %q", spans[0].Name)
	}

	foundType, foundAction := false, false
	for _, attr := range spans[0].Attributes {
		if string(attr.Key) == "workflow.type" && attr.Value.AsString() == "http" {
			foundType = true
		}
		if string(attr.Key) == "workflow.action" && attr.Value.AsString() == "create" {
			foundAction = true
		}
	}
	if !foundType {
		t.Error("expected workflow.type attribute")
	}
	if !foundAction {
		t.Error("expected workflow.action attribute")
	}
}

func TestWorkflowTracer_StartStep(t *testing.T) {
	wt, exporter := newTestTracer(t)

	_, span := wt.StartStep(context.Background(), "validate", "validation")
	span.End()

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Name != "workflow.step.validate" {
		t.Errorf("unexpected span name: %q", spans[0].Name)
	}
}

func TestWorkflowTracer_StartTrigger(t *testing.T) {
	wt, exporter := newTestTracer(t)

	_, span := wt.StartTrigger(context.Background(), "http-trigger", "http")
	span.End()

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Name != "workflow.trigger.http-trigger" {
		t.Errorf("unexpected span name: %q", spans[0].Name)
	}
}

func TestWorkflowTracer_RecordError(t *testing.T) {
	wt, exporter := newTestTracer(t)

	_, span := wt.StartWorkflow(context.Background(), "test", "run")
	testErr := errors.New("something failed")
	wt.RecordError(span, testErr)
	span.End()

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Status.Code != codes.Error {
		t.Errorf("expected error status, got %v", spans[0].Status.Code)
	}
}

func TestWorkflowTracer_RecordError_Nil(t *testing.T) {
	wt, exporter := newTestTracer(t)

	_, span := wt.StartWorkflow(context.Background(), "test", "run")
	wt.RecordError(span, nil) // should not panic
	span.End()

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Status.Code == codes.Error {
		t.Error("expected non-error status for nil error")
	}
}

func TestWorkflowTracer_SetSuccess(t *testing.T) {
	wt, exporter := newTestTracer(t)

	_, span := wt.StartWorkflow(context.Background(), "test", "run")
	wt.SetSuccess(span)
	span.End()

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Status.Code != codes.Ok {
		t.Errorf("expected Ok status, got %v", spans[0].Status.Code)
	}
}

func TestNewWorkflowTracer_NilTracer(t *testing.T) {
	// Set up a global provider so the fallback works.
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	wt := NewWorkflowTracer(nil)
	if wt.tracer == nil {
		t.Fatal("expected non-nil tracer from global provider")
	}
}

func TestSpanFromContext_ReturnsNoopIfNone(t *testing.T) {
	span := SpanFromContext(context.Background())
	if span == nil {
		t.Fatal("SpanFromContext should never return nil")
	}
}

func TestContextWithSpan_RoundTrip(t *testing.T) {
	wt, _ := newTestTracer(t)
	ctx, span := wt.StartWorkflow(context.Background(), "test", "rt")

	ctx2 := ContextWithSpan(context.Background(), span)
	got := SpanFromContext(ctx2)

	// Both contexts should carry the same span
	if got.SpanContext().TraceID() != SpanFromContext(ctx).SpanContext().TraceID() {
		t.Error("expected same trace ID in round-tripped context")
	}
	span.End()
}
