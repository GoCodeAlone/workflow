package store

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
)

// BenchmarkEventStoreAppend measures event store append latency.
// Target: <5ms per append (from PLATFORM_ROADMAP.md Phase 1).
func BenchmarkEventStoreAppend_InMemory(b *testing.B) {
	s := NewInMemoryEventStore()
	execID := uuid.New()
	ctx := context.Background()

	// Prime with a started event
	_ = s.Append(ctx, execID, EventExecutionStarted, map[string]any{
		"pipeline":  "benchmark-pipeline",
		"tenant_id": "tenant-bench",
	})

	data := map[string]any{
		"step_name": "benchmark-step",
		"step_type": "http",
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := s.Append(ctx, execID, EventStepStarted, data); err != nil {
			b.Fatalf("Append failed: %v", err)
		}
	}
}

func BenchmarkEventStoreAppend_SQLite(b *testing.B) {
	dir := b.TempDir()
	s, err := NewSQLiteEventStore(dir + "/bench.db")
	if err != nil {
		b.Fatalf("NewSQLiteEventStore: %v", err)
	}
	defer s.Close()

	execID := uuid.New()
	ctx := context.Background()

	_ = s.Append(ctx, execID, EventExecutionStarted, map[string]any{
		"pipeline":  "benchmark-pipeline",
		"tenant_id": "tenant-bench",
	})

	data := map[string]any{
		"step_name": "benchmark-step",
		"step_type": "http",
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := s.Append(ctx, execID, EventStepStarted, data); err != nil {
			b.Fatalf("Append failed: %v", err)
		}
	}
}

// BenchmarkGetTimeline measures timeline query latency with increasing event counts.
// Target: <50ms for 1000 events (from PLATFORM_ROADMAP.md Phase 1).
func BenchmarkGetTimeline_InMemory(b *testing.B) {
	for _, eventCount := range []int{10, 50, 100, 500, 1000} {
		b.Run(fmt.Sprintf("events-%d", eventCount), func(b *testing.B) {
			s := NewInMemoryEventStore()
			execID := uuid.New()
			ctx := context.Background()

			// Seed events
			_ = s.Append(ctx, execID, EventExecutionStarted, map[string]any{
				"pipeline": "bench-pipeline",
			})
			for i := 1; i < eventCount-1; i++ {
				stepName := fmt.Sprintf("step-%d", i)
				if i%2 == 1 {
					_ = s.Append(ctx, execID, EventStepStarted, map[string]any{"step_name": stepName, "step_type": "http"})
				} else {
					_ = s.Append(ctx, execID, EventStepCompleted, map[string]any{"step_name": fmt.Sprintf("step-%d", i-1)})
				}
			}
			_ = s.Append(ctx, execID, EventExecutionCompleted, map[string]any{})

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_, err := s.GetTimeline(ctx, execID)
				if err != nil {
					b.Fatalf("GetTimeline failed: %v", err)
				}
			}
		})
	}
}

func BenchmarkGetTimeline_SQLite(b *testing.B) {
	for _, eventCount := range []int{10, 50, 100, 500, 1000} {
		b.Run(fmt.Sprintf("events-%d", eventCount), func(b *testing.B) {
			dir := b.TempDir()
			s, err := NewSQLiteEventStore(dir + "/bench.db")
			if err != nil {
				b.Fatalf("NewSQLiteEventStore: %v", err)
			}
			defer s.Close()

			execID := uuid.New()
			ctx := context.Background()

			_ = s.Append(ctx, execID, EventExecutionStarted, map[string]any{
				"pipeline": "bench-pipeline",
			})
			for i := 1; i < eventCount-1; i++ {
				stepName := fmt.Sprintf("step-%d", i)
				if i%2 == 1 {
					_ = s.Append(ctx, execID, EventStepStarted, map[string]any{"step_name": stepName, "step_type": "http"})
				} else {
					_ = s.Append(ctx, execID, EventStepCompleted, map[string]any{"step_name": fmt.Sprintf("step-%d", i-1)})
				}
			}
			_ = s.Append(ctx, execID, EventExecutionCompleted, map[string]any{})

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_, err := s.GetTimeline(ctx, execID)
				if err != nil {
					b.Fatalf("GetTimeline failed: %v", err)
				}
			}
		})
	}
}
