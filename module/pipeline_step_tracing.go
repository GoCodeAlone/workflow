package module

import (
	"context"

	"github.com/CrisisTextLine/modular"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ─── trace_start ──────────────────────────────────────────────────────────────

// TraceStartStep starts a new trace span and records its IDs in the pipeline context.
type TraceStartStep struct {
	name       string
	spanName   string
	attributes map[string]string
}

// NewTraceStartStepFactory returns a StepFactory for step.trace_start.
func NewTraceStartStepFactory() StepFactory {
	return func(name string, cfg map[string]any, _ modular.Application) (PipelineStep, error) {
		spanName, _ := cfg["span_name"].(string)
		if spanName == "" {
			spanName = name
		}
		attrs := make(map[string]string)
		if raw, ok := cfg["attributes"].(map[string]any); ok {
			for k, v := range raw {
				if s, ok := v.(string); ok {
					attrs[k] = s
				}
			}
		}
		return &TraceStartStep{name: name, spanName: spanName, attributes: attrs}, nil
	}
}

func (s *TraceStartStep) Name() string { return s.name }

func (s *TraceStartStep) Execute(ctx context.Context, _ *PipelineContext) (*StepResult, error) {
	tracer := otel.GetTracerProvider().Tracer("workflow.pipeline")

	otelAttrs := make([]attribute.KeyValue, 0, len(s.attributes))
	for k, v := range s.attributes {
		otelAttrs = append(otelAttrs, attribute.String(k, v))
	}

	_, span := tracer.Start(ctx, s.spanName,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(otelAttrs...),
	)
	defer span.End()

	sc := span.SpanContext()
	return &StepResult{Output: map[string]any{
		"trace_id": sc.TraceID().String(),
		"span_id":  sc.SpanID().String(),
	}}, nil
}

// ─── trace_inject ─────────────────────────────────────────────────────────────

// TraceInjectStep injects the current trace context into an outbound carrier stored
// in the pipeline context under carrier_field.
type TraceInjectStep struct {
	name         string
	carrierField string
	carrierType  string // "http", "kafka", "eventbridge"
}

// NewTraceInjectStepFactory returns a StepFactory for step.trace_inject.
func NewTraceInjectStepFactory() StepFactory {
	return func(name string, cfg map[string]any, _ modular.Application) (PipelineStep, error) {
		carrierField, _ := cfg["carrier_field"].(string)
		if carrierField == "" {
			carrierField = "trace_headers"
		}
		carrierType, _ := cfg["carrier_type"].(string)
		if carrierType == "" {
			carrierType = "http"
		}
		return &TraceInjectStep{
			name:         name,
			carrierField: carrierField,
			carrierType:  carrierType,
		}, nil
	}
}

func (s *TraceInjectStep) Name() string { return s.name }

func (s *TraceInjectStep) Execute(ctx context.Context, _ *PipelineContext) (*StepResult, error) {
	headers := make(map[string]string)
	carrier := NewMapCarrier(headers)
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	return &StepResult{Output: map[string]any{s.carrierField: carrier.GetMap()}}, nil
}

// ─── trace_extract ────────────────────────────────────────────────────────────

// TraceExtractStep extracts trace context from an inbound carrier stored
// in the pipeline context under carrier_field, and records the extracted IDs.
type TraceExtractStep struct {
	name         string
	carrierField string
	carrierType  string
}

// NewTraceExtractStepFactory returns a StepFactory for step.trace_extract.
func NewTraceExtractStepFactory() StepFactory {
	return func(name string, cfg map[string]any, _ modular.Application) (PipelineStep, error) {
		carrierField, _ := cfg["carrier_field"].(string)
		if carrierField == "" {
			carrierField = "trace_headers"
		}
		carrierType, _ := cfg["carrier_type"].(string)
		if carrierType == "" {
			carrierType = "http"
		}
		return &TraceExtractStep{
			name:         name,
			carrierField: carrierField,
			carrierType:  carrierType,
		}, nil
	}
}

func (s *TraceExtractStep) Name() string { return s.name }

func (s *TraceExtractStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	headers := make(map[string]string)
	if raw, ok := pc.Current[s.carrierField]; ok {
		switch v := raw.(type) {
		case map[string]string:
			headers = v
		case map[string]any:
			for k, val := range v {
				if str, ok := val.(string); ok {
					headers[k] = str
				}
			}
		}
	}

	carrier := NewMapCarrier(headers)
	extracted := otel.GetTextMapPropagator().Extract(ctx, carrier)
	sc := trace.SpanFromContext(extracted).SpanContext()

	return &StepResult{Output: map[string]any{
		"extracted_trace_id": sc.TraceID().String(),
		"extracted_span_id":  sc.SpanID().String(),
		"trace_valid":        sc.IsValid(),
	}}, nil
}

// ─── trace_annotate ───────────────────────────────────────────────────────────

// TraceAnnotateStep adds events and attributes to the current span from context.
type TraceAnnotateStep struct {
	name       string
	eventName  string
	attributes map[string]string
}

// NewTraceAnnotateStepFactory returns a StepFactory for step.trace_annotate.
func NewTraceAnnotateStepFactory() StepFactory {
	return func(name string, cfg map[string]any, _ modular.Application) (PipelineStep, error) {
		eventName, _ := cfg["event_name"].(string)
		attrs := make(map[string]string)
		if raw, ok := cfg["attributes"].(map[string]any); ok {
			for k, v := range raw {
				if str, ok := v.(string); ok {
					attrs[k] = str
				}
			}
		}
		return &TraceAnnotateStep{name: name, eventName: eventName, attributes: attrs}, nil
	}
}

func (s *TraceAnnotateStep) Name() string { return s.name }

func (s *TraceAnnotateStep) Execute(ctx context.Context, _ *PipelineContext) (*StepResult, error) {
	span := trace.SpanFromContext(ctx)

	otelAttrs := make([]attribute.KeyValue, 0, len(s.attributes))
	for k, v := range s.attributes {
		otelAttrs = append(otelAttrs, attribute.String(k, v))
	}
	span.SetAttributes(otelAttrs...)

	if s.eventName != "" {
		span.AddEvent(s.eventName, trace.WithAttributes(otelAttrs...))
	}

	return &StepResult{Output: map[string]any{"annotated": true}}, nil
}

// ─── trace_link ───────────────────────────────────────────────────────────────

// TraceLinkStep links the current trace to a parent trace across service boundaries.
// The parent trace context is read from pipeline context under parent_field as a
// map[string]string of W3C traceparent/tracestate headers.
type TraceLinkStep struct {
	name        string
	parentField string
}

// NewTraceLinkStepFactory returns a StepFactory for step.trace_link.
func NewTraceLinkStepFactory() StepFactory {
	return func(name string, cfg map[string]any, _ modular.Application) (PipelineStep, error) {
		parentField, _ := cfg["parent_field"].(string)
		if parentField == "" {
			parentField = "parent_trace_headers"
		}
		return &TraceLinkStep{name: name, parentField: parentField}, nil
	}
}

func (s *TraceLinkStep) Name() string { return s.name }

func (s *TraceLinkStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	headers := make(map[string]string)
	if raw, ok := pc.Current[s.parentField]; ok {
		switch v := raw.(type) {
		case map[string]string:
			headers = v
		case map[string]any:
			for k, val := range v {
				if str, ok := val.(string); ok {
					headers[k] = str
				}
			}
		}
	}

	if len(headers) == 0 {
		return &StepResult{Output: map[string]any{
			"linked": false,
			"reason": "no parent headers",
		}}, nil
	}

	carrier := NewMapCarrier(headers)
	parentCtx := otel.GetTextMapPropagator().Extract(context.Background(), carrier)
	parentSpanCtx := trace.SpanFromContext(parentCtx).SpanContext()

	if !parentSpanCtx.IsValid() {
		return &StepResult{Output: map[string]any{
			"linked": false,
			"reason": "invalid parent span context",
		}}, nil
	}

	tracer := otel.GetTracerProvider().Tracer("workflow.pipeline")
	_, span := tracer.Start(ctx, "trace.link",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithLinks(trace.Link{SpanContext: parentSpanCtx}),
	)
	defer span.End()

	return &StepResult{Output: map[string]any{
		"linked":          true,
		"parent_trace_id": parentSpanCtx.TraceID().String(),
		"parent_span_id":  parentSpanCtx.SpanID().String(),
	}}, nil
}
