package telemetry_test

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/plugin/external/sdk"
	"github.com/GoCodeAlone/workflow/telemetry"
)

type sdkMetricEmitter struct{}

func (sdkMetricEmitter) EmitMetrics(_ context.Context, r sdk.TelemetryMetricRecorder) error {
	r.Counter("sdk_requests_total", 1, sdk.TelemetryAttrs{"source": "sdk"})
	return nil
}

func TestSDKTelemetryAliasesMatchCoreContracts(t *testing.T) {
	var _ telemetry.MetricEmitter = sdkMetricEmitter{}
	var _ sdk.TelemetryMetricEmitter = sdkMetricEmitter{}

	rec := telemetry.NewSnapshotRecorder()
	if err := (sdkMetricEmitter{}).EmitMetrics(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	got := rec.Metrics()
	if len(got) != 1 || got[0].Name != "sdk_requests_total" {
		t.Fatalf("metrics = %#v", got)
	}

	requireSDKMetricRecord(telemetry.MetricRecord{})
	requireSDKLogRecord(telemetry.LogRecord{})
	requireSDKSpanEvent(telemetry.SpanEvent{})
}

func requireSDKMetricRecord(sdk.TelemetryMetricRecord) {}
func requireSDKLogRecord(sdk.TelemetryLogRecord)       {}
func requireSDKSpanEvent(sdk.TelemetrySpanEvent)       {}
