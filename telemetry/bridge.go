package telemetry

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/GoCodeAlone/modular"
)

const defaultBridgeTimeout = 2 * time.Second

type TelemetrySink interface {
	RecordMetrics(context.Context, []MetricRecord) error
	RecordLogs(context.Context, []LogRecord) error
	RecordSpanEvents(context.Context, []SpanEvent) error
}

type BridgeConfig struct {
	Timeout time.Duration
}

type Bridge struct {
	sink   TelemetrySink
	config BridgeConfig
}

func NewBridge(sink TelemetrySink, config BridgeConfig) *Bridge {
	if sink == nil {
		sink = NoopSink{}
	}
	if config.Timeout <= 0 {
		config.Timeout = defaultBridgeTimeout
	}
	return &Bridge{sink: sink, config: config}
}

func (b *Bridge) Collect(ctx context.Context, app modular.Application) error {
	if app == nil {
		return nil
	}

	var joined error
	var metrics []MetricRecord
	var logs []LogRecord
	var events []SpanEvent

	for name, svc := range app.SvcRegistry() {
		if emitter, ok := svc.(MetricEmitter); ok {
			rec := NewSnapshotRecorder()
			if err := emitter.EmitMetrics(ctx, rec); err != nil {
				joined = errors.Join(joined, fmt.Errorf("collect metrics from %s: %w", name, err))
			}
			metrics = append(metrics, rec.Metrics()...)
		}
		if emitter, ok := svc.(LogEmitter); ok {
			logs = append(logs, emitter.DrainTelemetryLogs(ctx)...)
		}
		if annotator, ok := svc.(TraceAnnotator); ok {
			rec := newSnapshotSpanRecorder()
			annotator.AnnotateSpan(ctx, rec)
			events = append(events, rec.Events()...)
		}
	}

	sinkCtx := ctx
	var cancel context.CancelFunc
	if b.config.Timeout > 0 {
		sinkCtx, cancel = context.WithTimeout(ctx, b.config.Timeout)
		defer cancel()
	}
	if len(metrics) > 0 {
		if err := b.sink.RecordMetrics(sinkCtx, metrics); err != nil {
			joined = errors.Join(joined, fmt.Errorf("record metrics: %w", err))
		}
	}
	if len(logs) > 0 {
		if err := b.sink.RecordLogs(sinkCtx, logs); err != nil {
			joined = errors.Join(joined, fmt.Errorf("record logs: %w", err))
		}
	}
	if len(events) > 0 {
		if err := b.sink.RecordSpanEvents(sinkCtx, events); err != nil {
			joined = errors.Join(joined, fmt.Errorf("record span events: %w", err))
		}
	}
	return joined
}

type NoopSink struct{}

func (NoopSink) RecordMetrics(context.Context, []MetricRecord) error {
	return nil
}

func (NoopSink) RecordLogs(context.Context, []LogRecord) error {
	return nil
}

func (NoopSink) RecordSpanEvents(context.Context, []SpanEvent) error {
	return nil
}

type ContextServiceInvoker interface {
	InvokeServiceContext(context.Context, string, map[string]any) (map[string]any, error)
}

type ServiceInvokerSink struct {
	invoker ContextServiceInvoker
}

func NewServiceInvokerSink(invoker ContextServiceInvoker) *ServiceInvokerSink {
	return &ServiceInvokerSink{invoker: invoker}
}

func (s *ServiceInvokerSink) RecordMetrics(ctx context.Context, records []MetricRecord) error {
	if s == nil || s.invoker == nil {
		return nil
	}
	_, err := s.invoker.InvokeServiceContext(ctx, "recordMetrics", map[string]any{
		"metrics": metricRecordsToArgs(records),
	})
	return err
}

func (s *ServiceInvokerSink) RecordLogs(ctx context.Context, records []LogRecord) error {
	if s == nil || s.invoker == nil {
		return nil
	}
	_, err := s.invoker.InvokeServiceContext(ctx, "recordLogs", map[string]any{
		"logs": logRecordsToArgs(records),
	})
	return err
}

func (s *ServiceInvokerSink) RecordSpanEvents(ctx context.Context, records []SpanEvent) error {
	if s == nil || s.invoker == nil {
		return nil
	}
	_, err := s.invoker.InvokeServiceContext(ctx, "recordSpanEvents", map[string]any{
		"events": spanEventsToArgs(records),
	})
	return err
}

type snapshotSpanRecorder struct {
	events []SpanEvent
}

func newSnapshotSpanRecorder() *snapshotSpanRecorder {
	return &snapshotSpanRecorder{}
}

func (r *snapshotSpanRecorder) Event(name string, attrs Attrs) {
	r.events = append(r.events, SpanEvent{
		Name:      name,
		Attrs:     copyAttrs(attrs),
		Timestamp: time.Now(),
	})
}

func (r *snapshotSpanRecorder) Attribute(key, value string) {
	r.Event("attribute", Attrs{key: value})
}

func (r *snapshotSpanRecorder) Events() []SpanEvent {
	out := make([]SpanEvent, len(r.events))
	for i, event := range r.events {
		event.Attrs = copyAttrs(event.Attrs)
		out[i] = event
	}
	return out
}

func metricRecordsToArgs(records []MetricRecord) []map[string]any {
	out := make([]map[string]any, 0, len(records))
	for _, record := range records {
		out = append(out, map[string]any{
			"name":      record.Name,
			"kind":      string(record.Kind),
			"value":     record.Value,
			"attrs":     mapString(record.Attrs),
			"timestamp": formatTimestamp(record.Timestamp),
		})
	}
	return out
}

func logRecordsToArgs(records []LogRecord) []map[string]any {
	out := make([]map[string]any, 0, len(records))
	for _, record := range records {
		out = append(out, map[string]any{
			"timestamp": formatTimestamp(record.Timestamp),
			"level":     record.Level,
			"message":   record.Message,
			"module":    record.Module,
			"attrs":     mapString(record.Attrs),
		})
	}
	return out
}

func spanEventsToArgs(records []SpanEvent) []map[string]any {
	out := make([]map[string]any, 0, len(records))
	for _, record := range records {
		out = append(out, map[string]any{
			"name":      record.Name,
			"attrs":     mapString(record.Attrs),
			"timestamp": formatTimestamp(record.Timestamp),
		})
	}
	return out
}

func mapString(attrs Attrs) map[string]string {
	if len(attrs) == 0 {
		return nil
	}
	out := make(map[string]string, len(attrs))
	for k, v := range attrs {
		out[k] = v
	}
	return out
}

func formatTimestamp(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.UTC().Format(time.RFC3339Nano)
}
