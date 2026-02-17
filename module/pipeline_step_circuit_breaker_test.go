package module

import (
	"context"
	"testing"
	"time"
)

func TestCircuitBreakerStepFactory_Defaults(t *testing.T) {
	factory := NewCircuitBreakerStepFactory()
	step, err := factory("cb", map[string]any{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if step.Name() != "cb" {
		t.Errorf("expected name %q, got %q", "cb", step.Name())
	}

	cb := step.(*CircuitBreakerStep)
	if cb.failureThreshold != 5 {
		t.Errorf("expected failure_threshold 5, got %d", cb.failureThreshold)
	}
	if cb.successThreshold != 3 {
		t.Errorf("expected success_threshold 3, got %d", cb.successThreshold)
	}
	if cb.timeout != 30*time.Second {
		t.Errorf("expected timeout 30s, got %v", cb.timeout)
	}
	if cb.serviceName != "cb" {
		t.Errorf("expected service_name %q, got %q", "cb", cb.serviceName)
	}
}

func TestCircuitBreakerStepFactory_CustomConfig(t *testing.T) {
	factory := NewCircuitBreakerStepFactory()
	step, err := factory("cb", map[string]any{
		"failure_threshold": 3,
		"success_threshold": 2,
		"timeout":           "10s",
		"service_name":      "my-backend",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cb := step.(*CircuitBreakerStep)
	if cb.failureThreshold != 3 {
		t.Errorf("expected failure_threshold 3, got %d", cb.failureThreshold)
	}
	if cb.successThreshold != 2 {
		t.Errorf("expected success_threshold 2, got %d", cb.successThreshold)
	}
	if cb.timeout != 10*time.Second {
		t.Errorf("expected timeout 10s, got %v", cb.timeout)
	}
	if cb.serviceName != "my-backend" {
		t.Errorf("expected service_name %q, got %q", "my-backend", cb.serviceName)
	}
}

func TestCircuitBreakerStepFactory_FloatConfig(t *testing.T) {
	factory := NewCircuitBreakerStepFactory()
	step, err := factory("cb", map[string]any{
		"failure_threshold": float64(7),
		"success_threshold": float64(4),
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cb := step.(*CircuitBreakerStep)
	if cb.failureThreshold != 7 {
		t.Errorf("expected 7, got %d", cb.failureThreshold)
	}
	if cb.successThreshold != 4 {
		t.Errorf("expected 4, got %d", cb.successThreshold)
	}
}

func TestCircuitBreakerStepFactory_InvalidThreshold(t *testing.T) {
	factory := NewCircuitBreakerStepFactory()
	_, err := factory("cb", map[string]any{"failure_threshold": -1}, nil)
	if err == nil {
		t.Fatal("expected error for negative failure_threshold")
	}

	_, err = factory("cb", map[string]any{"success_threshold": 0}, nil)
	if err == nil {
		t.Fatal("expected error for zero success_threshold")
	}
}

func TestCircuitBreakerStepFactory_InvalidTimeout(t *testing.T) {
	factory := NewCircuitBreakerStepFactory()
	_, err := factory("cb", map[string]any{"timeout": "not-a-duration"}, nil)
	if err == nil {
		t.Fatal("expected error for invalid timeout")
	}
}

func TestCircuitBreakerStep_ClosedAllows(t *testing.T) {
	factory := NewCircuitBreakerStepFactory()
	step, _ := factory("cb", map[string]any{"failure_threshold": 3}, nil)
	cb := step.(*CircuitBreakerStep)

	ctx := context.Background()
	pc := NewPipelineContext(nil, nil)

	result, err := cb.Execute(ctx, pc)
	if err != nil {
		t.Fatalf("closed circuit should allow: %v", err)
	}

	cbOut := result.Output["circuit_breaker"].(map[string]any)
	if cbOut["state"] != string(CircuitClosed) {
		t.Errorf("expected state %q, got %q", CircuitClosed, cbOut["state"])
	}
	if !cbOut["allowed"].(bool) {
		t.Error("expected allowed=true")
	}
}

func TestCircuitBreakerStep_OpensAfterFailures(t *testing.T) {
	factory := NewCircuitBreakerStepFactory()
	step, _ := factory("cb", map[string]any{"failure_threshold": 3}, nil)
	cb := step.(*CircuitBreakerStep)

	// Record failures to open the circuit
	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}

	if cb.State() != CircuitOpen {
		t.Fatalf("expected state %q after %d failures, got %q", CircuitOpen, 3, cb.State())
	}

	ctx := context.Background()
	pc := NewPipelineContext(nil, nil)

	_, err := cb.Execute(ctx, pc)
	if err == nil {
		t.Fatal("open circuit should reject")
	}
}

func TestCircuitBreakerStep_TransitionsToHalfOpen(t *testing.T) {
	factory := NewCircuitBreakerStepFactory()
	step, _ := factory("cb", map[string]any{
		"failure_threshold": 2,
		"timeout":           "1ms",
	}, nil)
	cb := step.(*CircuitBreakerStep)

	// Open the circuit
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Fatalf("expected open, got %v", cb.State())
	}

	// Wait for timeout
	time.Sleep(5 * time.Millisecond)

	ctx := context.Background()
	pc := NewPipelineContext(nil, nil)

	result, err := cb.Execute(ctx, pc)
	if err != nil {
		t.Fatalf("should transition to half-open: %v", err)
	}

	cbOut := result.Output["circuit_breaker"].(map[string]any)
	if cbOut["state"] != string(CircuitHalfOpen) {
		t.Errorf("expected state %q, got %q", CircuitHalfOpen, cbOut["state"])
	}
	if !cbOut["transitioned"].(bool) {
		t.Error("expected transitioned=true")
	}
}

func TestCircuitBreakerStep_HalfOpenToClosedOnSuccess(t *testing.T) {
	factory := NewCircuitBreakerStepFactory()
	step, _ := factory("cb", map[string]any{
		"failure_threshold": 1,
		"success_threshold": 2,
		"timeout":           "1ms",
	}, nil)
	cb := step.(*CircuitBreakerStep)

	// Open and wait for half-open
	cb.RecordFailure()
	time.Sleep(5 * time.Millisecond)

	ctx := context.Background()
	pc := NewPipelineContext(nil, nil)
	_, _ = cb.Execute(ctx, pc) // transitions to half-open

	if cb.State() != CircuitHalfOpen {
		t.Fatalf("expected half-open, got %v", cb.State())
	}

	// Record successes
	cb.RecordSuccess()
	cb.RecordSuccess()

	if cb.State() != CircuitClosed {
		t.Errorf("expected closed after enough successes, got %v", cb.State())
	}
}

func TestCircuitBreakerStep_HalfOpenToOpenOnFailure(t *testing.T) {
	factory := NewCircuitBreakerStepFactory()
	step, _ := factory("cb", map[string]any{
		"failure_threshold": 1,
		"success_threshold": 3,
		"timeout":           "1ms",
	}, nil)
	cb := step.(*CircuitBreakerStep)

	// Open and wait for half-open
	cb.RecordFailure()
	time.Sleep(5 * time.Millisecond)

	ctx := context.Background()
	pc := NewPipelineContext(nil, nil)
	_, _ = cb.Execute(ctx, pc)

	if cb.State() != CircuitHalfOpen {
		t.Fatalf("expected half-open, got %v", cb.State())
	}

	// Failure in half-open goes back to open
	cb.RecordFailure()

	if cb.State() != CircuitOpen {
		t.Errorf("expected open after failure in half-open, got %v", cb.State())
	}
}

func TestCircuitBreakerStep_RecordSuccessResetsFails(t *testing.T) {
	factory := NewCircuitBreakerStepFactory()
	step, _ := factory("cb", map[string]any{"failure_threshold": 3}, nil)
	cb := step.(*CircuitBreakerStep)

	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordSuccess() // resets consecutive failures

	// Another failure shouldn't open the circuit (only 1 consecutive now)
	cb.RecordFailure()
	if cb.State() != CircuitClosed {
		t.Errorf("expected closed (success reset failures), got %v", cb.State())
	}
}
