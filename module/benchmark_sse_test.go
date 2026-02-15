package module

import (
	"log/slog"
	"testing"
	"time"
)

// BenchmarkSSEPublishDelivery measures SSE event delivery latency.
// Target: <100ms event delivery (from PLATFORM_ROADMAP.md Phase 4).
func BenchmarkSSEPublishDelivery(b *testing.B) {
	tracer := NewSSETracer(nil)

	ch, unsub := tracer.Subscribe("exec-bench")
	defer unsub()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		event := SSEEvent{
			ID:    "evt-bench",
			Event: "step.completed",
			Data:  `{"step":"transform","status":"ok"}`,
		}
		tracer.Publish("exec-bench", event)

		// Drain the channel
		<-ch
	}
}

// TestSSEDeliveryLatency measures actual wall-clock delivery time.
// Target: <100ms event delivery (from PLATFORM_ROADMAP.md Phase 4).
func TestSSEDeliveryLatency(t *testing.T) {
	tracer := NewSSETracer(slog.Default())

	ch, unsub := tracer.Subscribe("exec-latency")
	defer unsub()

	const iterations = 1000
	var totalLatency time.Duration
	var maxLatency time.Duration

	for i := 0; i < iterations; i++ {
		start := time.Now()

		tracer.Publish("exec-latency", SSEEvent{
			ID:    "evt-latency",
			Event: "step.completed",
			Data:  `{"step":"transform","status":"ok"}`,
		})

		select {
		case <-ch:
			latency := time.Since(start)
			totalLatency += latency
			if latency > maxLatency {
				maxLatency = latency
			}
		case <-time.After(time.Second):
			t.Fatalf("event delivery timed out at iteration %d", i)
		}
	}

	avgLatency := totalLatency / iterations

	t.Logf("SSE delivery latency over %d iterations:", iterations)
	t.Logf("  Average: %v", avgLatency)
	t.Logf("  Max:     %v", maxLatency)

	// Target: <100ms
	if maxLatency > 100*time.Millisecond {
		t.Errorf("FAIL: max SSE delivery latency %v exceeds 100ms target", maxLatency)
	} else {
		t.Logf("PASS: SSE delivery latency within 100ms target")
	}
}
