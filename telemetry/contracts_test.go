package telemetry

import (
	"context"
	"testing"
	"time"
)

type testMetricEmitter struct{}

func (testMetricEmitter) EmitMetrics(_ context.Context, r MetricRecorder) error {
	r.Counter("requests_total", 2, Attrs{"tenant": "acme"})
	r.Gauge("active_sessions", 3, nil)
	r.Histogram("request_duration_seconds", 0.15, Attrs{"route": "/"})
	return nil
}

func TestSnapshotRecorderCapturesMetrics(t *testing.T) {
	rec := NewSnapshotRecorder()
	if err := (testMetricEmitter{}).EmitMetrics(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	got := rec.Metrics()
	if len(got) != 3 {
		t.Fatalf("metric count = %d, want 3", len(got))
	}
	if got[0].Name != "requests_total" || got[0].Kind != MetricCounter || got[0].Value != 2 {
		t.Fatalf("first metric = %#v", got[0])
	}
	if got[0].Attrs["tenant"] != "acme" {
		t.Fatalf("tenant attr = %q, want acme", got[0].Attrs["tenant"])
	}
	if got[0].Timestamp.IsZero() {
		t.Fatal("metric timestamp is zero")
	}
}

func TestSnapshotRecorderCopiesAttrs(t *testing.T) {
	rec := NewSnapshotRecorder()
	attrs := Attrs{"tenant": "acme"}
	rec.Counter("requests_total", 1, attrs)
	attrs["tenant"] = "other"
	got := rec.Metrics()
	got[0].Attrs["tenant"] = "mutated"

	got = rec.Metrics()
	if got[0].Attrs["tenant"] != "acme" {
		t.Fatalf("stored attr = %q, want acme", got[0].Attrs["tenant"])
	}
}

func TestLogRecordDefaults(t *testing.T) {
	now := time.Now()
	rec := LogRecord{Timestamp: now, Level: "info", Message: "ok"}
	if rec.Timestamp.IsZero() || rec.Level != "info" || rec.Message != "ok" {
		t.Fatalf("bad log record: %#v", rec)
	}
}
