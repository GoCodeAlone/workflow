package telemetry

import (
	"context"
	"sync"
	"time"
)

type Attrs map[string]string

type MetricKind string

const (
	MetricCounter   MetricKind = "counter"
	MetricGauge     MetricKind = "gauge"
	MetricHistogram MetricKind = "histogram"
)

type MetricRecord struct {
	Name      string
	Kind      MetricKind
	Value     float64
	Attrs     Attrs
	Timestamp time.Time
}

type MetricRecorder interface {
	Counter(name string, value float64, attrs Attrs)
	Gauge(name string, value float64, attrs Attrs)
	Histogram(name string, value float64, attrs Attrs)
}

type MetricEmitter interface {
	EmitMetrics(context.Context, MetricRecorder) error
}

type LogRecord struct {
	Timestamp time.Time
	Level     string
	Message   string
	Module    string
	Attrs     Attrs
}

type LogEmitter interface {
	DrainTelemetryLogs(context.Context) []LogRecord
}

type SpanEvent struct {
	Name      string
	Attrs     Attrs
	Timestamp time.Time
}

type SpanRecorder interface {
	Event(name string, attrs Attrs)
	Attribute(key, value string)
}

type TraceAnnotator interface {
	AnnotateSpan(context.Context, SpanRecorder)
}

type SnapshotRecorder struct {
	mu      sync.Mutex
	metrics []MetricRecord
}

func NewSnapshotRecorder() *SnapshotRecorder {
	return &SnapshotRecorder{}
}

func (r *SnapshotRecorder) Counter(name string, value float64, attrs Attrs) {
	r.record(MetricCounter, name, value, attrs)
}

func (r *SnapshotRecorder) Gauge(name string, value float64, attrs Attrs) {
	r.record(MetricGauge, name, value, attrs)
}

func (r *SnapshotRecorder) Histogram(name string, value float64, attrs Attrs) {
	r.record(MetricHistogram, name, value, attrs)
}

func (r *SnapshotRecorder) Metrics() []MetricRecord {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]MetricRecord, len(r.metrics))
	for i, metric := range r.metrics {
		metric.Attrs = copyAttrs(metric.Attrs)
		out[i] = metric
	}
	return out
}

func (r *SnapshotRecorder) record(kind MetricKind, name string, value float64, attrs Attrs) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.metrics = append(r.metrics, MetricRecord{
		Name:      name,
		Kind:      kind,
		Value:     value,
		Attrs:     copyAttrs(attrs),
		Timestamp: time.Now(),
	})
}

func copyAttrs(attrs Attrs) Attrs {
	if len(attrs) == 0 {
		return nil
	}
	copied := make(Attrs, len(attrs))
	for k, v := range attrs {
		copied[k] = v
	}
	return copied
}
