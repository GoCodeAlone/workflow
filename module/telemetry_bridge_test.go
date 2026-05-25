package module

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/telemetry"
)

type telemetryBridgeMetricEmitter struct{}

func (telemetryBridgeMetricEmitter) EmitMetrics(_ context.Context, r telemetry.MetricRecorder) error {
	r.Counter("requests_total", 1, nil)
	return nil
}

type telemetryBridgeRecordingSink struct {
	mu      sync.Mutex
	metrics []telemetry.MetricRecord
}

func (s *telemetryBridgeRecordingSink) RecordMetrics(_ context.Context, records []telemetry.MetricRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metrics = append(s.metrics, records...)
	return nil
}

func (s *telemetryBridgeRecordingSink) RecordLogs(context.Context, []telemetry.LogRecord) error {
	return nil
}

func (s *telemetryBridgeRecordingSink) RecordSpanEvents(context.Context, []telemetry.SpanEvent) error {
	return nil
}

func (s *telemetryBridgeRecordingSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.metrics)
}

func TestTelemetryBridgeModuleCollectsOnInterval(t *testing.T) {
	app := NewMockApplication()
	if err := app.RegisterService("emitter", telemetryBridgeMetricEmitter{}); err != nil {
		t.Fatal(err)
	}
	sink := &telemetryBridgeRecordingSink{}
	mod := NewTelemetryBridge("telemetry-bridge", sink, TelemetryBridgeConfig{
		Interval: 10 * time.Millisecond,
		Timeout:  time.Second,
	})
	if err := mod.Init(app); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := mod.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := mod.Stop(context.Background()); err != nil {
			t.Fatalf("Stop: %v", err)
		}
	})

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if sink.count() > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("bridge did not collect metrics")
}
