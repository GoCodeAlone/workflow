package orchestration

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"
)

func newTestLogger() *slog.Logger {
	return slog.Default()
}

// --- TestSagaStartAndComplete ---
// Full successful saga: start, record three steps, complete.
func TestSagaStartAndComplete(t *testing.T) {
	coord := NewCoordinator(newTestLogger())

	cfg := SagaConfig{
		Enabled:           true,
		Timeout:           30 * time.Second,
		CompensationOrder: "reverse",
		TrackCompensation: true,
	}

	state := coord.StartSaga("saga-1", "order-pipeline", cfg)
	if state.ID != "saga-1" {
		t.Fatalf("expected saga ID %q, got %q", "saga-1", state.ID)
	}
	if state.Status != SagaRunning {
		t.Fatalf("expected status %q, got %q", SagaRunning, state.Status)
	}
	if state.PipelineName != "order-pipeline" {
		t.Fatalf("expected pipeline name %q, got %q", "order-pipeline", state.PipelineName)
	}

	// Record three steps
	steps := []CompletedStep{
		{Name: "validate", Output: map[string]any{"valid": true}, CompletedAt: time.Now()},
		{Name: "charge", Output: map[string]any{"tx_id": "tx-123"}, CompletedAt: time.Now()},
		{Name: "fulfill", Output: map[string]any{"tracking": "TRACK-456"}, CompletedAt: time.Now()},
	}

	for _, step := range steps {
		if err := coord.RecordStepCompleted("saga-1", step, nil); err != nil {
			t.Fatalf("unexpected error recording step %q: %v", step.Name, err)
		}
	}

	// Complete the saga
	if err := coord.CompleteSaga("saga-1"); err != nil {
		t.Fatalf("unexpected error completing saga: %v", err)
	}

	// Verify final state
	final, err := coord.GetState("saga-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if final.Status != SagaCompleted {
		t.Errorf("expected status %q, got %q", SagaCompleted, final.Status)
	}
	if final.CompletedAt == nil {
		t.Error("expected CompletedAt to be set")
	}
	if len(final.CompletedSteps) != 3 {
		t.Errorf("expected 3 completed steps, got %d", len(final.CompletedSteps))
	}
	if final.FailedStep != "" {
		t.Errorf("expected no failed step, got %q", final.FailedStep)
	}
}

// --- TestSagaCompensationReverse ---
// 3 steps complete, step 3 fails, compensate step 2 then step 1 (reverse order).
func TestSagaCompensationReverse(t *testing.T) {
	coord := NewCoordinator(newTestLogger())

	cfg := SagaConfig{
		Enabled:           true,
		CompensationOrder: "reverse",
		TrackCompensation: true,
	}

	coord.StartSaga("saga-rev", "payment-pipeline", cfg)

	// Steps 1 and 2 succeed with compensations registered.
	step1 := CompletedStep{Name: "reserve", Output: map[string]any{"reservation": "R1"}, CompletedAt: time.Now()}
	comp1 := &CompensationStep{StepName: "reserve", Type: "cancel_reservation", Config: map[string]any{"action": "cancel"}}

	step2 := CompletedStep{Name: "charge", Output: map[string]any{"tx_id": "TX-99"}, CompletedAt: time.Now()}
	comp2 := &CompensationStep{StepName: "charge", Type: "refund", Config: map[string]any{"action": "refund"}}

	if err := coord.RecordStepCompleted("saga-rev", step1, comp1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := coord.RecordStepCompleted("saga-rev", step2, comp2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Step 3 fails
	plan, err := coord.TriggerCompensation(context.Background(), "saga-rev", "ship", fmt.Errorf("shipping unavailable"))
	if err != nil {
		t.Fatalf("unexpected error triggering compensation: %v", err)
	}

	// Verify compensation plan order: charge first (index 1), then reserve (index 0)
	if len(plan.Steps) != 2 {
		t.Fatalf("expected 2 compensation steps, got %d", len(plan.Steps))
	}
	if plan.Steps[0].StepName != "charge" {
		t.Errorf("expected first compensation for %q, got %q", "charge", plan.Steps[0].StepName)
	}
	if plan.Steps[1].StepName != "reserve" {
		t.Errorf("expected second compensation for %q, got %q", "reserve", plan.Steps[1].StepName)
	}

	// Verify original outputs are passed through
	if plan.Steps[0].StepOutput["tx_id"] != "TX-99" {
		t.Errorf("expected charge output in compensation action")
	}
	if plan.Steps[1].StepOutput["reservation"] != "R1" {
		t.Errorf("expected reserve output in compensation action")
	}

	// Verify saga state
	state, _ := coord.GetState("saga-rev")
	if state.Status != SagaCompensating {
		t.Errorf("expected status %q, got %q", SagaCompensating, state.Status)
	}
	if state.FailedStep != "ship" {
		t.Errorf("expected failed step %q, got %q", "ship", state.FailedStep)
	}
	if state.FailureError != "shipping unavailable" {
		t.Errorf("expected failure error %q, got %q", "shipping unavailable", state.FailureError)
	}

	// Record successful compensations
	if err := coord.RecordCompensation("saga-rev", "charge", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := coord.RecordCompensation("saga-rev", "reserve", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := coord.FinishCompensation("saga-rev"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	state, _ = coord.GetState("saga-rev")
	if state.Status != SagaCompensated {
		t.Errorf("expected status %q, got %q", SagaCompensated, state.Status)
	}
	if len(state.CompensatedSteps) != 2 {
		t.Errorf("expected 2 compensated steps, got %d", len(state.CompensatedSteps))
	}
}

// --- TestSagaCompensationForward ---
// Compensate in forward order (same order as execution).
func TestSagaCompensationForward(t *testing.T) {
	coord := NewCoordinator(newTestLogger())

	cfg := SagaConfig{
		Enabled:           true,
		CompensationOrder: "forward",
		TrackCompensation: true,
	}

	coord.StartSaga("saga-fwd", "cleanup-pipeline", cfg)

	step1 := CompletedStep{Name: "alloc", Output: map[string]any{"resource": "A"}, CompletedAt: time.Now()}
	comp1 := &CompensationStep{StepName: "alloc", Type: "dealloc", Config: map[string]any{}}

	step2 := CompletedStep{Name: "configure", Output: map[string]any{"config_id": "C1"}, CompletedAt: time.Now()}
	comp2 := &CompensationStep{StepName: "configure", Type: "reset_config", Config: map[string]any{}}

	step3 := CompletedStep{Name: "activate", Output: map[string]any{"active": true}, CompletedAt: time.Now()}
	comp3 := &CompensationStep{StepName: "activate", Type: "deactivate", Config: map[string]any{}}

	for _, pair := range []struct {
		step CompletedStep
		comp *CompensationStep
	}{
		{step1, comp1},
		{step2, comp2},
		{step3, comp3},
	} {
		if err := coord.RecordStepCompleted("saga-fwd", pair.step, pair.comp); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	plan, err := coord.TriggerCompensation(context.Background(), "saga-fwd", "deploy", fmt.Errorf("deploy failed"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Forward order: alloc, configure, activate
	if len(plan.Steps) != 3 {
		t.Fatalf("expected 3 compensation steps, got %d", len(plan.Steps))
	}
	expectedOrder := []string{"alloc", "configure", "activate"}
	for i, expected := range expectedOrder {
		if plan.Steps[i].StepName != expected {
			t.Errorf("compensation step %d: expected %q, got %q", i, expected, plan.Steps[i].StepName)
		}
	}
}

// --- TestSagaTimeout ---
// Saga exceeds timeout; verify IsTimedOut and TimeoutSaga.
func TestSagaTimeout(t *testing.T) {
	coord := NewCoordinator(newTestLogger())

	cfg := SagaConfig{
		Enabled:           true,
		Timeout:           10 * time.Millisecond,
		CompensationOrder: "reverse",
	}

	coord.StartSaga("saga-timeout", "slow-pipeline", cfg)

	step1 := CompletedStep{Name: "step1", Output: map[string]any{"done": true}, CompletedAt: time.Now()}
	comp1 := &CompensationStep{StepName: "step1", Type: "undo_step1", Config: map[string]any{}}
	if err := coord.RecordStepCompleted("saga-timeout", step1, comp1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Not timed out yet (might be, but let's check the mechanism)
	// Wait long enough to guarantee timeout.
	time.Sleep(20 * time.Millisecond)

	timedOut, err := coord.IsTimedOut("saga-timeout")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !timedOut {
		t.Error("expected saga to be timed out")
	}

	// Trigger timeout compensation
	plan, err := coord.TimeoutSaga(context.Background(), "saga-timeout")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(plan.Steps) != 1 {
		t.Fatalf("expected 1 compensation step, got %d", len(plan.Steps))
	}
	if plan.Steps[0].StepName != "step1" {
		t.Errorf("expected compensation for %q, got %q", "step1", plan.Steps[0].StepName)
	}

	state, _ := coord.GetState("saga-timeout")
	if state.Status != SagaCompensating {
		t.Errorf("expected status %q, got %q", SagaCompensating, state.Status)
	}
	if state.FailureError == "" {
		t.Error("expected failure error to be set for timeout")
	}
}

// --- TestSagaTimeout_NoTimeout ---
// Saga with zero timeout should never be considered timed out.
func TestSagaTimeout_NoTimeout(t *testing.T) {
	coord := NewCoordinator(newTestLogger())

	cfg := SagaConfig{
		Enabled: true,
		Timeout: 0,
	}

	coord.StartSaga("saga-no-timeout", "fast-pipeline", cfg)

	timedOut, err := coord.IsTimedOut("saga-no-timeout")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if timedOut {
		t.Error("expected saga with zero timeout to not be timed out")
	}
}

// --- TestSagaStateTracking ---
// Verify all state transitions through the full lifecycle.
func TestSagaStateTracking(t *testing.T) {
	coord := NewCoordinator(newTestLogger())

	cfg := SagaConfig{
		Enabled:           true,
		CompensationOrder: "reverse",
		TrackCompensation: true,
	}

	// State: running
	coord.StartSaga("saga-track", "tracking-pipeline", cfg)
	state, _ := coord.GetState("saga-track")
	if state.Status != SagaRunning {
		t.Errorf("expected %q, got %q", SagaRunning, state.Status)
	}
	if state.StartedAt.IsZero() {
		t.Error("expected StartedAt to be set")
	}

	// Record a step
	step := CompletedStep{Name: "init", Output: map[string]any{"ready": true}, CompletedAt: time.Now()}
	comp := &CompensationStep{StepName: "init", Type: "teardown", Config: map[string]any{}}
	if err := coord.RecordStepCompleted("saga-track", step, comp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	state, _ = coord.GetState("saga-track")
	if len(state.CompletedSteps) != 1 {
		t.Errorf("expected 1 completed step, got %d", len(state.CompletedSteps))
	}
	if state.CompletedSteps[0].Name != "init" {
		t.Errorf("expected step name %q, got %q", "init", state.CompletedSteps[0].Name)
	}

	// State: compensating
	_, err := coord.TriggerCompensation(context.Background(), "saga-track", "process", fmt.Errorf("process error"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	state, _ = coord.GetState("saga-track")
	if state.Status != SagaCompensating {
		t.Errorf("expected %q, got %q", SagaCompensating, state.Status)
	}

	// Record compensation
	if err := coord.RecordCompensation("saga-track", "init", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// State: compensated
	if err := coord.FinishCompensation("saga-track"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	state, _ = coord.GetState("saga-track")
	if state.Status != SagaCompensated {
		t.Errorf("expected %q, got %q", SagaCompensated, state.Status)
	}
	if state.CompletedAt == nil {
		t.Error("expected CompletedAt to be set after compensation")
	}
}

// --- TestSagaConcurrent ---
// Multiple concurrent sagas with race detector.
func TestSagaConcurrent(t *testing.T) {
	coord := NewCoordinator(newTestLogger())
	const numSagas = 50

	var wg sync.WaitGroup
	wg.Add(numSagas)

	for i := 0; i < numSagas; i++ {
		go func(idx int) {
			defer wg.Done()

			id := fmt.Sprintf("concurrent-saga-%d", idx)
			cfg := SagaConfig{
				Enabled:           true,
				CompensationOrder: "reverse",
			}

			coord.StartSaga(id, fmt.Sprintf("pipeline-%d", idx), cfg)

			step := CompletedStep{
				Name:        fmt.Sprintf("step-%d", idx),
				Output:      map[string]any{"index": idx},
				CompletedAt: time.Now(),
			}
			comp := &CompensationStep{
				StepName: fmt.Sprintf("step-%d", idx),
				Type:     "undo",
				Config:   map[string]any{"index": idx},
			}

			if err := coord.RecordStepCompleted(id, step, comp); err != nil {
				t.Errorf("saga %s: unexpected error recording step: %v", id, err)
				return
			}

			// Half succeed, half fail
			if idx%2 == 0 {
				if err := coord.CompleteSaga(id); err != nil {
					t.Errorf("saga %s: unexpected error completing: %v", id, err)
				}
			} else {
				_, err := coord.TriggerCompensation(
					context.Background(), id,
					fmt.Sprintf("failed-step-%d", idx),
					fmt.Errorf("error-%d", idx),
				)
				if err != nil {
					t.Errorf("saga %s: unexpected error triggering compensation: %v", id, err)
					return
				}
				if err := coord.RecordCompensation(id, fmt.Sprintf("step-%d", idx), nil); err != nil {
					t.Errorf("saga %s: unexpected error recording compensation: %v", id, err)
					return
				}
				if err := coord.FinishCompensation(id); err != nil {
					t.Errorf("saga %s: unexpected error finishing compensation: %v", id, err)
				}
			}
		}(i)
	}

	wg.Wait()

	sagas := coord.ListSagas()
	if len(sagas) != numSagas {
		t.Errorf("expected %d sagas, got %d", numSagas, len(sagas))
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
			t.Errorf("unexpected saga status %q for %s", s.Status, s.ID)
		}
	}

	if completed != numSagas/2 {
		t.Errorf("expected %d completed sagas, got %d", numSagas/2, completed)
	}
	if compensated != numSagas/2 {
		t.Errorf("expected %d compensated sagas, got %d", numSagas/2, compensated)
	}
}

// --- TestCompensationPlanOrder ---
// Verify compensation order matches config for both forward and reverse.
func TestCompensationPlanOrder(t *testing.T) {
	stepNames := []string{"alpha", "beta", "gamma", "delta"}

	tests := []struct {
		name          string
		order         string
		expectedOrder []string
	}{
		{
			name:          "reverse order",
			order:         "reverse",
			expectedOrder: []string{"delta", "gamma", "beta", "alpha"},
		},
		{
			name:          "forward order",
			order:         "forward",
			expectedOrder: []string{"alpha", "beta", "gamma", "delta"},
		},
		{
			name:          "default (empty) is reverse",
			order:         "",
			expectedOrder: []string{"delta", "gamma", "beta", "alpha"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			coord := NewCoordinator(newTestLogger())

			cfg := SagaConfig{
				Enabled:           true,
				CompensationOrder: tc.order,
			}

			sagaID := fmt.Sprintf("order-test-%s", tc.name)
			coord.StartSaga(sagaID, "test-pipeline", cfg)

			for _, name := range stepNames {
				step := CompletedStep{
					Name:        name,
					Output:      map[string]any{"step": name},
					CompletedAt: time.Now(),
				}
				comp := &CompensationStep{
					StepName: name,
					Type:     fmt.Sprintf("undo_%s", name),
					Config:   map[string]any{},
				}
				if err := coord.RecordStepCompleted(sagaID, step, comp); err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}

			plan, err := coord.TriggerCompensation(context.Background(), sagaID, "epsilon", fmt.Errorf("failure"))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(plan.Steps) != len(tc.expectedOrder) {
				t.Fatalf("expected %d compensation steps, got %d", len(tc.expectedOrder), len(plan.Steps))
			}

			for i, expected := range tc.expectedOrder {
				if plan.Steps[i].StepName != expected {
					t.Errorf("step %d: expected %q, got %q", i, expected, plan.Steps[i].StepName)
				}
			}
		})
	}
}

// --- TestSagaPartialCompensation ---
// Some compensations succeed, some fail -- saga should end in SagaFailed.
func TestSagaPartialCompensation(t *testing.T) {
	coord := NewCoordinator(newTestLogger())

	cfg := SagaConfig{
		Enabled:           true,
		CompensationOrder: "reverse",
		TrackCompensation: true,
	}

	coord.StartSaga("saga-partial", "partial-pipeline", cfg)

	// Register three steps with compensations
	for _, name := range []string{"step1", "step2", "step3"} {
		step := CompletedStep{Name: name, Output: map[string]any{}, CompletedAt: time.Now()}
		comp := &CompensationStep{StepName: name, Type: "undo_" + name, Config: map[string]any{}}
		if err := coord.RecordStepCompleted("saga-partial", step, comp); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	// Trigger compensation
	plan, err := coord.TriggerCompensation(context.Background(), "saga-partial", "step4", fmt.Errorf("step4 broke"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Simulate: step3 compensation succeeds, step2 fails, step1 succeeds
	if err := coord.RecordCompensation("saga-partial", plan.Steps[0].StepName, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := coord.RecordCompensation("saga-partial", plan.Steps[1].StepName, fmt.Errorf("compensation failed for step2")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := coord.RecordCompensation("saga-partial", plan.Steps[2].StepName, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Finish
	if err := coord.FinishCompensation("saga-partial"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	state, _ := coord.GetState("saga-partial")
	if state.Status != SagaFailed {
		t.Errorf("expected status %q, got %q", SagaFailed, state.Status)
	}

	// Verify compensated steps are recorded
	if len(state.CompensatedSteps) != 3 {
		t.Fatalf("expected 3 compensated steps, got %d", len(state.CompensatedSteps))
	}

	// step2's compensation should have the error
	foundError := false
	for _, cs := range state.CompensatedSteps {
		if cs.Name == plan.Steps[1].StepName && cs.Error != "" {
			foundError = true
			break
		}
	}
	if !foundError {
		t.Error("expected to find a compensated step with error")
	}
}

// --- Error case tests ---

func TestSagaNotFound(t *testing.T) {
	coord := NewCoordinator(newTestLogger())

	_, err := coord.GetState("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent saga")
	}

	err = coord.RecordStepCompleted("nonexistent", CompletedStep{Name: "x"}, nil)
	if err == nil {
		t.Fatal("expected error for nonexistent saga")
	}

	_, err = coord.TriggerCompensation(context.Background(), "nonexistent", "x", nil)
	if err == nil {
		t.Fatal("expected error for nonexistent saga")
	}

	err = coord.CompleteSaga("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent saga")
	}
}

func TestSagaDoubleComplete(t *testing.T) {
	coord := NewCoordinator(newTestLogger())

	cfg := SagaConfig{Enabled: true}
	coord.StartSaga("saga-double", "test", cfg)

	if err := coord.CompleteSaga("saga-double"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Second complete should fail
	if err := coord.CompleteSaga("saga-double"); err == nil {
		t.Fatal("expected error on double complete")
	}
}

func TestRecordStepOnCompletedSaga(t *testing.T) {
	coord := NewCoordinator(newTestLogger())

	cfg := SagaConfig{Enabled: true}
	coord.StartSaga("saga-closed", "test", cfg)

	if err := coord.CompleteSaga("saga-closed"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Recording a step on a completed saga should fail
	err := coord.RecordStepCompleted("saga-closed", CompletedStep{Name: "late"}, nil)
	if err == nil {
		t.Fatal("expected error recording step on completed saga")
	}
}

func TestCompensationOnCompletedSaga(t *testing.T) {
	coord := NewCoordinator(newTestLogger())

	cfg := SagaConfig{Enabled: true}
	coord.StartSaga("saga-comp-closed", "test", cfg)

	if err := coord.CompleteSaga("saga-comp-closed"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Triggering compensation on a completed saga should fail
	_, err := coord.TriggerCompensation(context.Background(), "saga-comp-closed", "x", nil)
	if err == nil {
		t.Fatal("expected error triggering compensation on completed saga")
	}
}

func TestStepsWithoutCompensation(t *testing.T) {
	coord := NewCoordinator(newTestLogger())

	cfg := SagaConfig{
		Enabled:           true,
		CompensationOrder: "reverse",
	}

	coord.StartSaga("saga-no-comp", "test", cfg)

	// Step 1 has compensation, step 2 does not
	step1 := CompletedStep{Name: "with_comp", Output: map[string]any{}, CompletedAt: time.Now()}
	comp1 := &CompensationStep{StepName: "with_comp", Type: "undo", Config: map[string]any{}}

	step2 := CompletedStep{Name: "without_comp", Output: map[string]any{}, CompletedAt: time.Now()}

	if err := coord.RecordStepCompleted("saga-no-comp", step1, comp1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := coord.RecordStepCompleted("saga-no-comp", step2, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	plan, err := coord.TriggerCompensation(context.Background(), "saga-no-comp", "step3", fmt.Errorf("fail"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only step 1 should be in the plan (step 2 had no compensation)
	if len(plan.Steps) != 1 {
		t.Fatalf("expected 1 compensation step, got %d", len(plan.Steps))
	}
	if plan.Steps[0].StepName != "with_comp" {
		t.Errorf("expected compensation for %q, got %q", "with_comp", plan.Steps[0].StepName)
	}
}

func TestListSagas(t *testing.T) {
	coord := NewCoordinator(newTestLogger())

	cfg := SagaConfig{Enabled: true}
	coord.StartSaga("list-1", "p1", cfg)
	coord.StartSaga("list-2", "p2", cfg)
	coord.StartSaga("list-3", "p3", cfg)

	sagas := coord.ListSagas()
	if len(sagas) != 3 {
		t.Errorf("expected 3 sagas, got %d", len(sagas))
	}

	// Verify all are present (order not guaranteed from map)
	ids := make(map[string]bool)
	for _, s := range sagas {
		ids[s.ID] = true
	}
	for _, expected := range []string{"list-1", "list-2", "list-3"} {
		if !ids[expected] {
			t.Errorf("expected saga %q in list", expected)
		}
	}
}

func TestNewCoordinatorNilLogger(t *testing.T) {
	// Should not panic with nil logger
	coord := NewCoordinator(nil)
	if coord == nil {
		t.Fatal("expected non-nil coordinator")
	}

	// Should still function
	cfg := SagaConfig{Enabled: true}
	state := coord.StartSaga("nil-logger", "test", cfg)
	if state == nil {
		t.Fatal("expected non-nil state")
	}
}
