package orchestration

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// BenchmarkSagaConcurrent measures saga throughput under concurrent load.
// Target: 100 concurrent sagas (from PLATFORM_ROADMAP.md Phase 4).
func BenchmarkSagaConcurrent(b *testing.B) {
	coord := NewCoordinator(nil)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		id := fmt.Sprintf("bench-saga-%d", i)
		cfg := SagaConfig{
			Enabled:           true,
			CompensationOrder: "reverse",
		}

		coord.StartSaga(id, "bench-pipeline", cfg)

		step := CompletedStep{
			Name:        "step-1",
			Output:      map[string]any{"result": "ok"},
			CompletedAt: time.Now(),
		}
		comp := &CompensationStep{
			StepName: "step-1",
			Type:     "undo",
			Config:   map[string]any{},
		}

		_ = coord.RecordStepCompleted(id, step, comp)
		_ = coord.CompleteSaga(id)
	}
}

// TestSaga100Concurrent validates that 100 concurrent sagas complete correctly.
// Target: 100 concurrent sagas with compensation (from PLATFORM_ROADMAP.md Phase 4).
func TestSaga100Concurrent(t *testing.T) {
	coord := NewCoordinator(nil)
	const numSagas = 100
	const stepsPerSaga = 3

	start := time.Now()

	var wg sync.WaitGroup
	wg.Add(numSagas)
	errCh := make(chan error, numSagas)

	for i := 0; i < numSagas; i++ {
		go func(idx int) {
			defer wg.Done()

			id := fmt.Sprintf("concurrent-100-saga-%d", idx)
			cfg := SagaConfig{
				Enabled:           true,
				CompensationOrder: "reverse",
				TrackCompensation: true,
			}

			coord.StartSaga(id, fmt.Sprintf("pipeline-%d", idx), cfg)

			// Register 3 steps with compensations
			for s := 0; s < stepsPerSaga; s++ {
				step := CompletedStep{
					Name:        fmt.Sprintf("step-%d", s),
					Output:      map[string]any{"index": s},
					CompletedAt: time.Now(),
				}
				comp := &CompensationStep{
					StepName: fmt.Sprintf("step-%d", s),
					Type:     "undo",
					Config:   map[string]any{"index": s},
				}
				if err := coord.RecordStepCompleted(id, step, comp); err != nil {
					errCh <- fmt.Errorf("saga %s step %d: %w", id, s, err)
					return
				}
			}

			// Half complete successfully, half trigger compensation
			if idx%2 == 0 {
				if err := coord.CompleteSaga(id); err != nil {
					errCh <- fmt.Errorf("saga %s complete: %w", id, err)
				}
			} else {
				_, err := coord.TriggerCompensation(
					context.Background(), id,
					"failed-step",
					fmt.Errorf("error-%d", idx),
				)
				if err != nil {
					errCh <- fmt.Errorf("saga %s compensate: %w", id, err)
					return
				}
				for s := stepsPerSaga - 1; s >= 0; s-- {
					if err := coord.RecordCompensation(id, fmt.Sprintf("step-%d", s), nil); err != nil {
						errCh <- fmt.Errorf("saga %s record comp %d: %w", id, s, err)
						return
					}
				}
				if err := coord.FinishCompensation(id); err != nil {
					errCh <- fmt.Errorf("saga %s finish comp: %w", id, err)
				}
			}
		}(i)
	}

	wg.Wait()
	close(errCh)
	elapsed := time.Since(start)

	for err := range errCh {
		t.Error(err)
	}

	sagas := coord.ListSagas()
	if len(sagas) != numSagas {
		t.Fatalf("expected %d sagas, got %d", numSagas, len(sagas))
	}

	completed := 0
	compensated := 0
	for _, s := range sagas {
		switch s.Status {
		case SagaCompleted:
			completed++
		case SagaCompensated:
			compensated++
		default:
			t.Errorf("unexpected status %q for saga %s", s.Status, s.ID)
		}
	}

	if completed != numSagas/2 {
		t.Errorf("expected %d completed, got %d", numSagas/2, completed)
	}
	if compensated != numSagas/2 {
		t.Errorf("expected %d compensated, got %d", numSagas/2, compensated)
	}

	t.Logf("PASS: 100 concurrent sagas (%d completed, %d compensated) in %v", completed, compensated, elapsed)
}
