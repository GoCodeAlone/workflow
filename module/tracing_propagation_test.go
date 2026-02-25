package module

import (
	"context"
	"net/http"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// setupTracingTest sets the global OTEL provider + W3C propagator for a test.
func setupTracingTest(t *testing.T) (*sdktrace.TracerProvider, *tracetest.InMemoryExporter) {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	return tp, exporter
}

// ─── MapCarrier ────────────────────────────────────────────────────────────────

func TestTracMapCarrier_GetSetKeys(t *testing.T) {
	m := make(map[string]string)
	c := NewMapCarrier(m)
	c.Set("traceparent", "00-abc-def-01")
	if got := c.Get("traceparent"); got != "00-abc-def-01" {
		t.Errorf("Get() = %q, want %q", got, "00-abc-def-01")
	}
	keys := c.Keys()
	if len(keys) != 1 || keys[0] != "traceparent" {
		t.Errorf("Keys() = %v, want [traceparent]", keys)
	}
}

func TestTracMapCarrier_NilMap(t *testing.T) {
	c := NewMapCarrier(nil)
	c.Set("k", "v")
	if c.Get("k") != "v" {
		t.Error("expected value after Set on nil-initialized carrier")
	}
	if c.GetMap() == nil {
		t.Error("GetMap() should not be nil")
	}
}

// ─── EventBridgeCarrier ────────────────────────────────────────────────────────

func TestTracEventBridgeCarrier_GetSetKeys(t *testing.T) {
	detail := map[string]any{}
	c := EventBridgeCarrier{Detail: detail}
	c.Set("traceparent", "00-abc-def-01")
	if got := c.Get("traceparent"); got != "00-abc-def-01" {
		t.Errorf("Get() = %q, want %q", got, "00-abc-def-01")
	}
	if len(c.Keys()) != 1 {
		t.Errorf("Keys() len = %d, want 1", len(c.Keys()))
	}
}

func TestTracEventBridgeCarrier_NonStringValue(t *testing.T) {
	detail := map[string]any{"count": 42}
	c := EventBridgeCarrier{Detail: detail}
	if got := c.Get("count"); got != "" {
		t.Errorf("Get() for non-string should return empty, got %q", got)
	}
}

// ─── HTTP propagator inject/extract round-trip ─────────────────────────────────

func TestTracHTTPPropagator_InjectExtract(t *testing.T) {
	setupTracingTest(t)

	tracer := otel.GetTracerProvider().Tracer("test")
	ctx, span := tracer.Start(context.Background(), "root")
	defer span.End()

	p := NewHTTPTracePropagator()
	m := make(map[string]string)
	carrier := NewMapCarrier(m)
	if err := p.Inject(ctx, carrier); err != nil {
		t.Fatalf("Inject() error: %v", err)
	}
	if _, ok := m["traceparent"]; !ok {
		t.Error("expected 'traceparent' header after HTTP inject")
	}

	ctx2 := p.Extract(context.Background(), carrier)
	sc := trace.SpanFromContext(ctx2).SpanContext()
	if !sc.IsValid() {
		t.Error("extracted span context should be valid")
	}
	if sc.TraceID() != span.SpanContext().TraceID() {
		t.Errorf("trace ID mismatch: got %s, want %s", sc.TraceID(), span.SpanContext().TraceID())
	}
}

func TestTracHTTPPropagator_InjectExtractHeaders(t *testing.T) {
	setupTracingTest(t)

	tracer := otel.GetTracerProvider().Tracer("test")
	ctx, span := tracer.Start(context.Background(), "root")
	defer span.End()

	p := NewHTTPTracePropagator()
	hdr := http.Header{}
	if err := p.InjectHeaders(ctx, hdr); err != nil {
		t.Fatalf("InjectHeaders() error: %v", err)
	}
	if hdr.Get("traceparent") == "" {
		t.Error("expected traceparent header after InjectHeaders")
	}

	ctx2 := p.ExtractHeaders(context.Background(), hdr)
	sc := trace.SpanFromContext(ctx2).SpanContext()
	if !sc.IsValid() {
		t.Error("extracted span context should be valid after ExtractHeaders")
	}
	if sc.TraceID() != span.SpanContext().TraceID() {
		t.Errorf("trace ID mismatch: got %s, want %s", sc.TraceID(), span.SpanContext().TraceID())
	}
}

// ─── Kafka propagator inject/extract round-trip ────────────────────────────────

func TestTracKafkaPropagator_InjectExtract(t *testing.T) {
	setupTracingTest(t)

	tracer := otel.GetTracerProvider().Tracer("test")
	ctx, span := tracer.Start(context.Background(), "root")
	defer span.End()

	p := NewKafkaTracePropagator()
	headers := make(map[string]string)
	if err := p.InjectMap(ctx, headers); err != nil {
		t.Fatalf("InjectMap() error: %v", err)
	}
	if _, ok := headers["traceparent"]; !ok {
		t.Error("expected 'traceparent' in Kafka headers after inject")
	}

	ctx2 := p.ExtractMap(context.Background(), headers)
	sc := trace.SpanFromContext(ctx2).SpanContext()
	if !sc.IsValid() {
		t.Error("extracted span context should be valid")
	}
	if sc.TraceID() != span.SpanContext().TraceID() {
		t.Errorf("trace ID mismatch after Kafka round-trip: got %s, want %s",
			sc.TraceID(), span.SpanContext().TraceID())
	}
}

func TestTracKafkaPropagator_ViaInterface(t *testing.T) {
	setupTracingTest(t)

	tracer := otel.GetTracerProvider().Tracer("test")
	ctx, span := tracer.Start(context.Background(), "root")
	defer span.End()

	var p PipelineTracePropagator = NewKafkaTracePropagator()
	m := make(map[string]string)
	carrier := NewMapCarrier(m)
	if err := p.Inject(ctx, carrier); err != nil {
		t.Fatalf("Inject() error: %v", err)
	}
	ctx2 := p.Extract(context.Background(), carrier)
	sc := trace.SpanFromContext(ctx2).SpanContext()
	if sc.TraceID() != span.SpanContext().TraceID() {
		t.Error("trace ID mismatch via PipelineTracePropagator interface")
	}
}

// ─── EventBridge propagator inject/extract ────────────────────────────────────

func TestTracEventBridgePropagator_InjectExtract(t *testing.T) {
	setupTracingTest(t)

	tracer := otel.GetTracerProvider().Tracer("test")
	ctx, span := tracer.Start(context.Background(), "root")
	defer span.End()

	p := NewEventBridgeTracePropagator()
	detail := make(map[string]any)
	if err := p.InjectDetail(ctx, detail); err != nil {
		t.Fatalf("InjectDetail() error: %v", err)
	}
	if _, ok := detail["traceparent"]; !ok {
		t.Error("expected 'traceparent' in EventBridge detail after inject")
	}

	ctx2 := p.ExtractDetail(context.Background(), detail)
	sc := trace.SpanFromContext(ctx2).SpanContext()
	if !sc.IsValid() {
		t.Error("extracted span context should be valid")
	}
	if sc.TraceID() != span.SpanContext().TraceID() {
		t.Errorf("trace ID mismatch: got %s, want %s", sc.TraceID(), span.SpanContext().TraceID())
	}
}

// ─── Webhook outbound header injection ────────────────────────────────────────

func TestTracWebhookPropagator_InjectRequest(t *testing.T) {
	setupTracingTest(t)

	tracer := otel.GetTracerProvider().Tracer("test")
	ctx, span := tracer.Start(context.Background(), "root")
	defer span.End()

	p := NewWebhookTracePropagator()
	req, _ := http.NewRequest("POST", "http://example.com/webhook", nil)
	if err := p.InjectRequest(ctx, req); err != nil {
		t.Fatalf("InjectRequest() error: %v", err)
	}
	if req.Header.Get("traceparent") == "" {
		t.Error("expected traceparent header in outbound webhook request")
	}
}

// ─── Span creation and attribute setting ──────────────────────────────────────

func TestTracPipelineTracingMiddleware_CreatesSpan(t *testing.T) {
	_, exporter := setupTracingTest(t)

	inner := &TraceAnnotateStep{name: "inner", eventName: "", attributes: map[string]string{}}
	mw := NewPipelineTracingMiddleware(inner, nil)
	if mw.Name() != "inner" {
		t.Errorf("Name() = %q, want %q", mw.Name(), "inner")
	}

	pc := NewPipelineContext(nil, nil)
	_, err := mw.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	spans := exporter.GetSpans()
	found := false
	for _, s := range spans {
		if s.Name == "pipeline.step.inner" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected span 'pipeline.step.inner', got %v", spanNames(spans))
	}
}

// ─── TracePropagationModule ────────────────────────────────────────────────────

func TestTracPropagationModule(t *testing.T) {
	m := NewTracePropagationModule("tp", map[string]any{"format": "w3c"})
	if m.Name() != "tp" {
		t.Errorf("Name() = %q, want %q", m.Name(), "tp")
	}
	if m.Format() != "w3c" {
		t.Errorf("Format() = %q, want %q", m.Format(), "w3c")
	}
	if err := m.Init(nil); err != nil {
		t.Errorf("Init() error: %v", err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Errorf("Start() error: %v", err)
	}
	if err := m.Stop(context.Background()); err != nil {
		t.Errorf("Stop() error: %v", err)
	}
	if m.HTTPPropagator() == nil {
		t.Error("HTTPPropagator() should not be nil")
	}
	if m.KafkaPropagator() == nil {
		t.Error("KafkaPropagator() should not be nil")
	}
	if m.EventBridgePropagator() == nil {
		t.Error("EventBridgePropagator() should not be nil")
	}
	if m.WebhookPropagator() == nil {
		t.Error("WebhookPropagator() should not be nil")
	}
	if len(m.ProvidesServices()) != 1 {
		t.Errorf("ProvidesServices() len = %d, want 1", len(m.ProvidesServices()))
	}
}

func TestTracPropagationModule_DefaultFormat(t *testing.T) {
	m := NewTracePropagationModule("tp2", map[string]any{})
	if m.Format() != "w3c" {
		t.Errorf("default Format() = %q, want %q", m.Format(), "w3c")
	}
}

// ─── Pipeline step factories ───────────────────────────────────────────────────

func TestTracTraceStartStep(t *testing.T) {
	_, exporter := setupTracingTest(t)

	factory := NewTraceStartStepFactory()
	step, err := factory("start", map[string]any{
		"span_name":  "my-span",
		"attributes": map[string]any{"env": "test"},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}
	if step.Name() != "start" {
		t.Errorf("Name() = %q, want %q", step.Name(), "start")
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if result.Output["trace_id"] == "" {
		t.Error("expected non-empty trace_id in output")
	}
	if result.Output["span_id"] == "" {
		t.Error("expected non-empty span_id in output")
	}

	spans := exporter.GetSpans()
	found := false
	for _, s := range spans {
		if s.Name == "my-span" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected span named 'my-span', got %v", spanNames(spans))
	}
}

func TestTracTraceStartStep_DefaultSpanName(t *testing.T) {
	setupTracingTest(t)
	factory := NewTraceStartStepFactory()
	step, _ := factory("default-name", map[string]any{}, nil)
	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if result.Output["trace_id"] == "" {
		t.Error("expected trace_id in output")
	}
}

func TestTracTraceInjectStep(t *testing.T) {
	setupTracingTest(t)

	tracer := otel.GetTracerProvider().Tracer("test")
	ctx, span := tracer.Start(context.Background(), "root")
	defer span.End()

	factory := NewTraceInjectStepFactory()
	step, err := factory("inject", map[string]any{
		"carrier_field": "headers",
		"carrier_type":  "http",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(ctx, pc)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	headers, ok := result.Output["headers"].(map[string]string)
	if !ok {
		t.Fatalf("expected map[string]string under 'headers', got %T", result.Output["headers"])
	}
	if _, ok := headers["traceparent"]; !ok {
		t.Error("expected 'traceparent' in injected headers")
	}
}

func TestTracTraceInjectStep_DefaultField(t *testing.T) {
	setupTracingTest(t)
	factory := NewTraceInjectStepFactory()
	step, _ := factory("inject", map[string]any{}, nil)

	tracer := otel.GetTracerProvider().Tracer("test")
	ctx, span := tracer.Start(context.Background(), "root")
	defer span.End()

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(ctx, pc)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if result.Output["trace_headers"] == nil {
		t.Error("expected 'trace_headers' in output for default carrier_field")
	}
}

func TestTracTraceExtractStep_MapStringAny(t *testing.T) {
	setupTracingTest(t)

	tracer := otel.GetTracerProvider().Tracer("test")
	ctx, span := tracer.Start(context.Background(), "root")
	headers := make(map[string]string)
	otel.GetTextMapPropagator().Inject(ctx, NewMapCarrier(headers))
	span.End()

	factory := NewTraceExtractStepFactory()
	step, err := factory("extract", map[string]any{"carrier_field": "incoming_headers"}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	headersAny := make(map[string]any)
	for k, v := range headers {
		headersAny[k] = v
	}
	pc := NewPipelineContext(map[string]any{"incoming_headers": headersAny}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if valid, ok := result.Output["trace_valid"].(bool); !ok || !valid {
		t.Errorf("expected trace_valid=true, got %v", result.Output["trace_valid"])
	}
	if result.Output["extracted_trace_id"] == "" {
		t.Error("expected non-empty extracted_trace_id")
	}
}

func TestTracTraceExtractStep_MapStringString(t *testing.T) {
	setupTracingTest(t)

	tracer := otel.GetTracerProvider().Tracer("test")
	ctx, span := tracer.Start(context.Background(), "root")
	headers := make(map[string]string)
	otel.GetTextMapPropagator().Inject(ctx, NewMapCarrier(headers))
	span.End()

	factory := NewTraceExtractStepFactory()
	step, _ := factory("extract", map[string]any{"carrier_field": "hdrs"}, nil)

	pc := NewPipelineContext(map[string]any{"hdrs": headers}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if valid, ok := result.Output["trace_valid"].(bool); !ok || !valid {
		t.Errorf("expected trace_valid=true, got %v", result.Output["trace_valid"])
	}
}

func TestTracTraceExtractStep_Empty(t *testing.T) {
	setupTracingTest(t)
	factory := NewTraceExtractStepFactory()
	step, _ := factory("extract", map[string]any{}, nil)

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if valid, ok := result.Output["trace_valid"].(bool); ok && valid {
		t.Error("expected trace_valid=false for empty carrier")
	}
}

func TestTracTraceAnnotateStep_WithEvent(t *testing.T) {
	_, exporter := setupTracingTest(t)

	tracer := otel.GetTracerProvider().Tracer("test")
	ctx, span := tracer.Start(context.Background(), "root")

	factory := NewTraceAnnotateStepFactory()
	step, err := factory("annotate", map[string]any{
		"event_name": "user.login",
		"attributes": map[string]any{"user.id": "42"},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(ctx, pc)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if result.Output["annotated"] != true {
		t.Error("expected annotated=true")
	}
	span.End()

	spans := exporter.GetSpans()
	found := false
	for _, s := range spans {
		if s.Name == "root" {
			for _, e := range s.Events {
				if e.Name == "user.login" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Error("expected event 'user.login' on the span")
	}
}

func TestTracTraceAnnotateStep_NoEvent(t *testing.T) {
	setupTracingTest(t)
	factory := NewTraceAnnotateStepFactory()
	step, _ := factory("annotate", map[string]any{
		"attributes": map[string]any{"key": "value"},
	}, nil)
	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if result.Output["annotated"] != true {
		t.Error("expected annotated=true even without event_name")
	}
}

func TestTracTraceLinkStep_CrossService(t *testing.T) {
	_, exporter := setupTracingTest(t)

	// Service A: start span, inject context
	tracer := otel.GetTracerProvider().Tracer("service-a")
	ctxA, spanA := tracer.Start(context.Background(), "service-a.handle")
	parentHeaders := make(map[string]string)
	otel.GetTextMapPropagator().Inject(ctxA, NewMapCarrier(parentHeaders))
	spanA.End()

	// Service B: link to parent via step.trace_link
	factory := NewTraceLinkStepFactory()
	step, err := factory("link", map[string]any{"parent_field": "parent_headers"}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{"parent_headers": parentHeaders}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if linked, ok := result.Output["linked"].(bool); !ok || !linked {
		t.Errorf("expected linked=true, got %v (%v)", result.Output["linked"], result.Output["reason"])
	}
	if result.Output["parent_trace_id"] != spanA.SpanContext().TraceID().String() {
		t.Errorf("parent_trace_id mismatch: got %v, want %v",
			result.Output["parent_trace_id"], spanA.SpanContext().TraceID().String())
	}

	spans := exporter.GetSpans()
	foundLink := false
	for _, s := range spans {
		if s.Name == "trace.link" && len(s.Links) > 0 {
			foundLink = true
			break
		}
	}
	if !foundLink {
		t.Error("expected 'trace.link' span with links")
	}
}

func TestTracTraceLinkStep_NoHeaders(t *testing.T) {
	setupTracingTest(t)
	factory := NewTraceLinkStepFactory()
	step, _ := factory("link", map[string]any{}, nil)
	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if linked, ok := result.Output["linked"].(bool); !ok || linked {
		t.Error("expected linked=false when no parent headers")
	}
}

func TestTracTraceLinkStep_InvalidParent(t *testing.T) {
	setupTracingTest(t)
	factory := NewTraceLinkStepFactory()
	step, _ := factory("link", map[string]any{"parent_field": "hdrs"}, nil)
	pc := NewPipelineContext(map[string]any{
		"hdrs": map[string]string{"traceparent": "invalid-value"},
	}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if linked, ok := result.Output["linked"].(bool); !ok || linked {
		t.Errorf("expected linked=false for invalid parent context, got %v", result.Output)
	}
}

// ─── helpers ──────────────────────────────────────────────────────────────────

func spanNames(spans tracetest.SpanStubs) []string {
	names := make([]string, len(spans))
	for i, s := range spans {
		names[i] = s.Name
	}
	return names
}
