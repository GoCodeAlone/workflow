package module

import (
	"context"
	"net/http"

	"github.com/CrisisTextLine/modular"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// PipelineTracePropagator defines the interface for propagating trace context
// across asynchronous messaging boundaries (Kafka, EventBridge, webhooks, HTTP).
type PipelineTracePropagator interface {
	// Inject injects the trace context from ctx into the carrier.
	Inject(ctx context.Context, carrier propagation.TextMapCarrier) error
	// Extract extracts trace context from the carrier and returns an updated context.
	Extract(ctx context.Context, carrier propagation.TextMapCarrier) context.Context
}

// MapCarrier wraps a map[string]string as a TextMapCarrier for use with OTEL propagators.
type MapCarrier struct {
	m map[string]string
}

// NewMapCarrier creates a MapCarrier backed by the given map.
// If m is nil, an empty map is allocated.
func NewMapCarrier(m map[string]string) MapCarrier {
	if m == nil {
		m = make(map[string]string)
	}
	return MapCarrier{m: m}
}

func (c MapCarrier) Get(key string) string { return c.m[key] }
func (c MapCarrier) Set(key, value string) { c.m[key] = value }
func (c MapCarrier) Keys() []string {
	keys := make([]string, 0, len(c.m))
	for k := range c.m {
		keys = append(keys, k)
	}
	return keys
}

// GetMap returns the underlying map.
func (c MapCarrier) GetMap() map[string]string { return c.m }

// EventBridgeCarrier wraps a map[string]any as a TextMapCarrier.
// Only string values are propagated; non-string values are ignored on Get.
type EventBridgeCarrier struct {
	Detail map[string]any
}

func (c EventBridgeCarrier) Get(key string) string {
	if v, ok := c.Detail[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func (c EventBridgeCarrier) Set(key, value string) { c.Detail[key] = value }

func (c EventBridgeCarrier) Keys() []string {
	keys := make([]string, 0, len(c.Detail))
	for k := range c.Detail {
		keys = append(keys, k)
	}
	return keys
}

// HTTPTracePropagator propagates trace context via W3C TraceContext HTTP headers.
type HTTPTracePropagator struct {
	propagator propagation.TextMapPropagator
}

// NewHTTPTracePropagator creates an HTTP trace propagator using the global OTEL propagator.
func NewHTTPTracePropagator() *HTTPTracePropagator {
	return &HTTPTracePropagator{propagator: otel.GetTextMapPropagator()}
}

func (p *HTTPTracePropagator) Inject(ctx context.Context, carrier propagation.TextMapCarrier) error {
	p.propagator.Inject(ctx, carrier)
	return nil
}

func (p *HTTPTracePropagator) Extract(ctx context.Context, carrier propagation.TextMapCarrier) context.Context {
	return p.propagator.Extract(ctx, carrier)
}

// InjectHeaders injects trace context into an http.Header.
func (p *HTTPTracePropagator) InjectHeaders(ctx context.Context, headers http.Header) error {
	p.propagator.Inject(ctx, propagation.HeaderCarrier(headers))
	return nil
}

// ExtractHeaders extracts trace context from an http.Header.
func (p *HTTPTracePropagator) ExtractHeaders(ctx context.Context, headers http.Header) context.Context {
	return p.propagator.Extract(ctx, propagation.HeaderCarrier(headers))
}

// KafkaTracePropagator propagates trace context via Kafka message headers (map[string]string).
type KafkaTracePropagator struct {
	propagator propagation.TextMapPropagator
}

// NewKafkaTracePropagator creates a Kafka trace propagator using the global OTEL propagator.
func NewKafkaTracePropagator() *KafkaTracePropagator {
	return &KafkaTracePropagator{propagator: otel.GetTextMapPropagator()}
}

func (p *KafkaTracePropagator) Inject(ctx context.Context, carrier propagation.TextMapCarrier) error {
	p.propagator.Inject(ctx, carrier)
	return nil
}

func (p *KafkaTracePropagator) Extract(ctx context.Context, carrier propagation.TextMapCarrier) context.Context {
	return p.propagator.Extract(ctx, carrier)
}

// InjectMap injects trace context into a map[string]string (Kafka headers).
func (p *KafkaTracePropagator) InjectMap(ctx context.Context, headers map[string]string) error {
	p.propagator.Inject(ctx, NewMapCarrier(headers))
	return nil
}

// ExtractMap extracts trace context from a map[string]string (Kafka headers).
func (p *KafkaTracePropagator) ExtractMap(ctx context.Context, headers map[string]string) context.Context {
	return p.propagator.Extract(ctx, NewMapCarrier(headers))
}

// EventBridgeTracePropagator propagates trace context in EventBridge event detail metadata.
type EventBridgeTracePropagator struct {
	propagator propagation.TextMapPropagator
}

// NewEventBridgeTracePropagator creates an EventBridge trace propagator.
func NewEventBridgeTracePropagator() *EventBridgeTracePropagator {
	return &EventBridgeTracePropagator{propagator: otel.GetTextMapPropagator()}
}

func (p *EventBridgeTracePropagator) Inject(ctx context.Context, carrier propagation.TextMapCarrier) error {
	p.propagator.Inject(ctx, carrier)
	return nil
}

func (p *EventBridgeTracePropagator) Extract(ctx context.Context, carrier propagation.TextMapCarrier) context.Context {
	return p.propagator.Extract(ctx, carrier)
}

// InjectDetail injects trace context into an EventBridge detail map.
func (p *EventBridgeTracePropagator) InjectDetail(ctx context.Context, detail map[string]any) error {
	p.propagator.Inject(ctx, EventBridgeCarrier{Detail: detail})
	return nil
}

// ExtractDetail extracts trace context from an EventBridge detail map.
func (p *EventBridgeTracePropagator) ExtractDetail(ctx context.Context, detail map[string]any) context.Context {
	return p.propagator.Extract(ctx, EventBridgeCarrier{Detail: detail})
}

// WebhookTracePropagator propagates trace context via outbound webhook HTTP headers.
type WebhookTracePropagator struct {
	propagator propagation.TextMapPropagator
}

// NewWebhookTracePropagator creates a webhook trace propagator.
func NewWebhookTracePropagator() *WebhookTracePropagator {
	return &WebhookTracePropagator{propagator: otel.GetTextMapPropagator()}
}

func (p *WebhookTracePropagator) Inject(ctx context.Context, carrier propagation.TextMapCarrier) error {
	p.propagator.Inject(ctx, carrier)
	return nil
}

func (p *WebhookTracePropagator) Extract(ctx context.Context, carrier propagation.TextMapCarrier) context.Context {
	return p.propagator.Extract(ctx, carrier)
}

// InjectRequest injects trace context into an outbound *http.Request.
func (p *WebhookTracePropagator) InjectRequest(ctx context.Context, req *http.Request) error {
	p.propagator.Inject(ctx, propagation.HeaderCarrier(req.Header))
	return nil
}

// PipelineTracingMiddleware wraps a PipelineStep with OTEL span creation.
// It creates a child span for each step execution, recording errors automatically.
type PipelineTracingMiddleware struct {
	step   PipelineStep
	tracer trace.Tracer
}

// NewPipelineTracingMiddleware wraps the given step with span instrumentation.
// If tracer is nil, the global tracer provider is used.
func NewPipelineTracingMiddleware(step PipelineStep, tracer trace.Tracer) *PipelineTracingMiddleware {
	if tracer == nil {
		tracer = otel.GetTracerProvider().Tracer("workflow.pipeline")
	}
	return &PipelineTracingMiddleware{step: step, tracer: tracer}
}

func (m *PipelineTracingMiddleware) Name() string { return m.step.Name() }

func (m *PipelineTracingMiddleware) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	ctx, span := m.tracer.Start(ctx, "pipeline.step."+m.step.Name(),
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(attribute.String("pipeline.step.name", m.step.Name())),
	)
	defer span.End()

	result, err := m.step.Execute(ctx, pc)
	if err != nil {
		span.RecordError(err)
	}
	return result, err
}

// TracePropagationModule provides trace propagation configuration as a workflow module.
type TracePropagationModule struct {
	name   string
	format string // propagation format: "w3c", "b3", "composite"
}

// NewTracePropagationModule creates a new trace propagation module.
func NewTracePropagationModule(name string, cfg map[string]any) *TracePropagationModule {
	format := "w3c"
	if v, ok := cfg["format"].(string); ok && v != "" {
		format = v
	}
	return &TracePropagationModule{name: name, format: format}
}

func (m *TracePropagationModule) Name() string { return m.name }

func (m *TracePropagationModule) Init(_ modular.Application) error { return nil }

func (m *TracePropagationModule) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        m.name,
			Description: "Distributed Trace Propagation",
			Instance:    m,
		},
	}
}

func (m *TracePropagationModule) RequiresServices() []modular.ServiceDependency { return nil }

func (m *TracePropagationModule) Start(_ context.Context) error { return nil }

func (m *TracePropagationModule) Stop(_ context.Context) error { return nil }

// HTTPPropagator returns a new HTTPTracePropagator configured for this module.
func (m *TracePropagationModule) HTTPPropagator() *HTTPTracePropagator {
	return NewHTTPTracePropagator()
}

// KafkaPropagator returns a new KafkaTracePropagator configured for this module.
func (m *TracePropagationModule) KafkaPropagator() *KafkaTracePropagator {
	return NewKafkaTracePropagator()
}

// EventBridgePropagator returns a new EventBridgeTracePropagator configured for this module.
func (m *TracePropagationModule) EventBridgePropagator() *EventBridgeTracePropagator {
	return NewEventBridgeTracePropagator()
}

// WebhookPropagator returns a new WebhookTracePropagator configured for this module.
func (m *TracePropagationModule) WebhookPropagator() *WebhookTracePropagator {
	return NewWebhookTracePropagator()
}

// Format returns the configured propagation format.
func (m *TracePropagationModule) Format() string { return m.format }
