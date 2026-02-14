package tracing

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// WorkflowTracer provides convenience methods for creating spans around
// workflow execution lifecycle events.
type WorkflowTracer struct {
	tracer trace.Tracer
}

// NewWorkflowTracer creates a WorkflowTracer. If tracer is nil, the global
// tracer provider is used.
func NewWorkflowTracer(tracer trace.Tracer) *WorkflowTracer {
	if tracer == nil {
		tracer = otel.GetTracerProvider().Tracer("workflow.engine")
	}
	return &WorkflowTracer{tracer: tracer}
}

// StartWorkflow begins a new span for a workflow execution.
func (w *WorkflowTracer) StartWorkflow(ctx context.Context, workflowType, action string) (context.Context, trace.Span) {
	ctx, span := w.tracer.Start(ctx, "workflow.execute",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("workflow.type", workflowType),
			attribute.String("workflow.action", action),
		),
	)
	return ctx, span
}

// StartStep begins a child span for a workflow step.
func (w *WorkflowTracer) StartStep(ctx context.Context, stepName, stepType string) (context.Context, trace.Span) {
	ctx, span := w.tracer.Start(ctx, "workflow.step."+stepName,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("workflow.step.name", stepName),
			attribute.String("workflow.step.type", stepType),
		),
	)
	return ctx, span
}

// StartTrigger begins a span for a trigger invocation.
func (w *WorkflowTracer) StartTrigger(ctx context.Context, triggerName, triggerType string) (context.Context, trace.Span) {
	ctx, span := w.tracer.Start(ctx, "workflow.trigger."+triggerName,
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			attribute.String("workflow.trigger.name", triggerName),
			attribute.String("workflow.trigger.type", triggerType),
		),
	)
	return ctx, span
}

// RecordError records an error on the given span and sets the span status.
func (w *WorkflowTracer) RecordError(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
}

// SetSuccess marks a span as successful.
func (w *WorkflowTracer) SetSuccess(span trace.Span) {
	span.SetStatus(codes.Ok, "")
}

// SpanFromContext returns the current span from context, useful for adding
// attributes from within workflow handlers.
func SpanFromContext(ctx context.Context) trace.Span {
	return trace.SpanFromContext(ctx)
}

// ContextWithSpan wraps trace.ContextWithSpan for convenience.
func ContextWithSpan(ctx context.Context, span trace.Span) context.Context {
	return trace.ContextWithSpan(ctx, span)
}
