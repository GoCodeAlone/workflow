package telemetry_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/telemetry"
)

type bridgeMetricEmitter struct{}

func (bridgeMetricEmitter) EmitMetrics(_ context.Context, r telemetry.MetricRecorder) error {
	r.Counter("requests_total", 2, telemetry.Attrs{"tenant": "acme"})
	r.Gauge("active_sessions", 3, nil)
	r.Histogram("request_duration_seconds", 0.15, telemetry.Attrs{"route": "/"})
	return nil
}

type bridgeLogEmitter struct{}

func (bridgeLogEmitter) DrainTelemetryLogs(_ context.Context) []telemetry.LogRecord {
	return []telemetry.LogRecord{{Level: "info", Message: "ok", Module: "test"}}
}

type bridgeTraceAnnotator struct{}

func (bridgeTraceAnnotator) AnnotateSpan(_ context.Context, r telemetry.SpanRecorder) {
	r.Event("cache.hit", telemetry.Attrs{"cache": "users"})
	r.Attribute("tenant", "acme")
}

type recordingSink struct {
	metrics []telemetry.MetricRecord
	logs    []telemetry.LogRecord
	events  []telemetry.SpanEvent
}

func (s *recordingSink) RecordMetrics(_ context.Context, records []telemetry.MetricRecord) error {
	s.metrics = append(s.metrics, records...)
	return nil
}

func (s *recordingSink) RecordLogs(_ context.Context, records []telemetry.LogRecord) error {
	s.logs = append(s.logs, records...)
	return nil
}

func (s *recordingSink) RecordSpanEvents(_ context.Context, records []telemetry.SpanEvent) error {
	s.events = append(s.events, records...)
	return nil
}

type failingSink struct{}

func (failingSink) RecordMetrics(context.Context, []telemetry.MetricRecord) error {
	return errors.New("metrics down")
}

func (failingSink) RecordLogs(context.Context, []telemetry.LogRecord) error {
	return nil
}

func (failingSink) RecordSpanEvents(context.Context, []telemetry.SpanEvent) error {
	return nil
}

func TestBridgeCollectsEmitters(t *testing.T) {
	app := module.NewMockApplication()
	if err := app.RegisterService("metrics", bridgeMetricEmitter{}); err != nil {
		t.Fatal(err)
	}
	if err := app.RegisterService("logs", bridgeLogEmitter{}); err != nil {
		t.Fatal(err)
	}
	if err := app.RegisterService("traces", bridgeTraceAnnotator{}); err != nil {
		t.Fatal(err)
	}

	sink := &recordingSink{}
	bridge := telemetry.NewBridge(sink, telemetry.BridgeConfig{Timeout: time.Second})
	if err := bridge.Collect(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	if len(sink.metrics) != 3 {
		t.Fatalf("metric count = %d, want 3", len(sink.metrics))
	}
	if len(sink.logs) != 1 {
		t.Fatalf("log count = %d, want 1", len(sink.logs))
	}
	if len(sink.events) != 2 {
		t.Fatalf("span event count = %d, want 2", len(sink.events))
	}
}

func TestBridgeSinkFailureIsDiagnostic(t *testing.T) {
	app := module.NewMockApplication()
	if err := app.RegisterService("metrics", bridgeMetricEmitter{}); err != nil {
		t.Fatal(err)
	}

	bridge := telemetry.NewBridge(failingSink{}, telemetry.BridgeConfig{Timeout: time.Second})
	if err := bridge.Collect(context.Background(), app); err == nil {
		t.Fatal("expected diagnostic error")
	}
}

func TestNoopSinkKeepsEmittersInert(t *testing.T) {
	app := module.NewMockApplication()
	if err := app.RegisterService("metrics", bridgeMetricEmitter{}); err != nil {
		t.Fatal(err)
	}

	bridge := telemetry.NewBridge(telemetry.NoopSink{}, telemetry.BridgeConfig{Timeout: time.Second})
	if err := bridge.Collect(context.Background(), app); err != nil {
		t.Fatal(err)
	}
}

func TestServiceInvokerSinkConvertsRecords(t *testing.T) {
	invoker := &recordingInvoker{}
	sink := telemetry.NewServiceInvokerSink(invoker)
	err := sink.RecordMetrics(context.Background(), []telemetry.MetricRecord{{
		Name:      "requests_total",
		Kind:      telemetry.MetricCounter,
		Value:     1,
		Attrs:     telemetry.Attrs{"tenant": "acme"},
		Timestamp: time.Unix(1, 0).UTC(),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if invoker.method != "recordMetrics" {
		t.Fatalf("method = %q, want recordMetrics", invoker.method)
	}
	metrics, ok := invoker.args["metrics"].([]map[string]any)
	if !ok || len(metrics) != 1 {
		t.Fatalf("metrics args = %#v", invoker.args["metrics"])
	}
	if metrics[0]["timestamp"] != "1970-01-01T00:00:01Z" {
		t.Fatalf("timestamp = %#v", metrics[0]["timestamp"])
	}
}

type recordingInvoker struct {
	method string
	args   map[string]any
}

func (i *recordingInvoker) InvokeServiceContext(_ context.Context, method string, args map[string]any) (map[string]any, error) {
	i.method = method
	i.args = args
	return map[string]any{"accepted": true}, nil
}
