package module

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/CrisisTextLine/modular"
)

// resilienceFailStep is a PipelineStep that always returns an error.
type resilienceFailStep struct {
	name    string
	callCnt int
}

func (s *resilienceFailStep) Name() string { return s.name }
func (s *resilienceFailStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	s.callCnt++
	return nil, errors.New("step failed")
}

// resilienceSucceedStep is a PipelineStep that always succeeds.
type resilienceSucceedStep struct {
	name    string
	callCnt int
}

func (s *resilienceSucceedStep) Name() string { return s.name }
func (s *resilienceSucceedStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	s.callCnt++
	return &StepResult{Output: map[string]any{"ok": true}}, nil
}

// resilienceCountingStep succeeds after a configurable number of failures.
type resilienceCountingStep struct {
	name      string
	failUntil int
	callCnt   int
}

func (s *resilienceCountingStep) Name() string { return s.name }
func (s *resilienceCountingStep) Execute(_ context.Context, _ *PipelineContext) (*StepResult, error) {
	s.callCnt++
	if s.callCnt <= s.failUntil {
		return nil, errors.New("not yet")
	}
	return &StepResult{Output: map[string]any{"call": s.callCnt}}, nil
}

// buildResilienceRegistry creates a StepRegistry with a pre-registered step.
func buildResilienceRegistry(stepType string, step PipelineStep) func() *StepRegistry {
	reg := NewStepRegistry()
	reg.Register(stepType, func(_ string, _ map[string]any, _ modular.Application) (PipelineStep, error) {
		return step, nil
	})
	return func() *StepRegistry { return reg }
}

// ---- RetryWithBackoffStep tests ----

func TestRetryWithBackoffStep_RequiresStepConfig(t *testing.T) {
	reg := NewStepRegistry()
	factory := NewRetryWithBackoffStepFactory(func() *StepRegistry { return reg })
	_, err := factory("retry", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error when step config is missing")
	}
}

func TestRetryWithBackoffStep_InvalidInitialDelay(t *testing.T) {
	reg := NewStepRegistry()
	factory := NewRetryWithBackoffStepFactory(func() *StepRegistry { return reg })
	_, err := factory("retry", map[string]any{
		"initial_delay": "not-a-duration",
		"step":          map[string]any{"type": "step.log"},
	}, nil)
	if err == nil {
		t.Fatal("expected error for invalid initial_delay")
	}
}

func TestRetryWithBackoffStep_InvalidMaxDelay(t *testing.T) {
	reg := NewStepRegistry()
	factory := NewRetryWithBackoffStepFactory(func() *StepRegistry { return reg })
	_, err := factory("retry", map[string]any{
		"max_delay": "not-a-duration",
		"step":      map[string]any{"type": "step.log"},
	}, nil)
	if err == nil {
		t.Fatal("expected error for invalid max_delay")
	}
}

func TestRetryWithBackoffStep_SuccessOnFirstAttempt(t *testing.T) {
	inner := &resilienceSucceedStep{name: "inner"}
	registryFn := buildResilienceRegistry("step.inner", inner)

	factory := NewRetryWithBackoffStepFactory(registryFn)
	step, err := factory("retry", map[string]any{
		"max_retries":   2,
		"initial_delay": "1ms",
		"step":          map[string]any{"type": "step.inner"},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(ctx, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Output["retry_attempts"] != 0 {
		t.Errorf("expected retry_attempts=0, got %v", result.Output["retry_attempts"])
	}
	if inner.callCnt != 1 {
		t.Errorf("expected 1 call, got %d", inner.callCnt)
	}
}

func TestRetryWithBackoffStep_RetriesAndSucceeds(t *testing.T) {
	inner := &resilienceCountingStep{name: "inner", failUntil: 2}
	registryFn := buildResilienceRegistry("step.inner", inner)

	factory := NewRetryWithBackoffStepFactory(registryFn)
	step, err := factory("retry", map[string]any{
		"max_retries":   5,
		"initial_delay": "1ms",
		"max_delay":     "5ms",
		"step":          map[string]any{"type": "step.inner"},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(ctx, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Output["retry_attempts"] != 2 {
		t.Errorf("expected retry_attempts=2, got %v", result.Output["retry_attempts"])
	}
	if inner.callCnt != 3 {
		t.Errorf("expected 3 calls (2 failures + 1 success), got %d", inner.callCnt)
	}
}

func TestRetryWithBackoffStep_ExhaustsRetries(t *testing.T) {
	inner := &resilienceFailStep{name: "inner"}
	registryFn := buildResilienceRegistry("step.inner", inner)

	factory := NewRetryWithBackoffStepFactory(registryFn)
	step, err := factory("retry", map[string]any{
		"max_retries":   2,
		"initial_delay": "1ms",
		"step":          map[string]any{"type": "step.inner"},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(ctx, pc)
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}

	if inner.callCnt != 3 {
		t.Errorf("expected 3 calls (initial + 2 retries), got %d", inner.callCnt)
	}
}

func TestRetryWithBackoffStep_DefaultValues(t *testing.T) {
	inner := &resilienceSucceedStep{name: "inner"}
	reg := NewStepRegistry()
	reg.Register("step.inner", func(_ string, _ map[string]any, _ modular.Application) (PipelineStep, error) {
		return inner, nil
	})

	factory := NewRetryWithBackoffStepFactory(func() *StepRegistry { return reg })
	step, err := factory("retry", map[string]any{
		"step": map[string]any{"type": "step.inner"},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	retryStep := step.(*RetryWithBackoffStep)
	if retryStep.maxRetries != 3 {
		t.Errorf("expected maxRetries=3, got %d", retryStep.maxRetries)
	}
	if retryStep.initialDelay != time.Second {
		t.Errorf("expected initialDelay=1s, got %v", retryStep.initialDelay)
	}
	if retryStep.maxDelay != 30*time.Second {
		t.Errorf("expected maxDelay=30s, got %v", retryStep.maxDelay)
	}
	if retryStep.multiplier != 2.0 {
		t.Errorf("expected multiplier=2.0, got %v", retryStep.multiplier)
	}
}

// ---- ResilienceCircuitBreakerStep tests ----

func TestResilienceCircuitBreakerStep_RequiresStepConfig(t *testing.T) {
	reg := NewStepRegistry()
	factory := NewResilienceCircuitBreakerStepFactory(func() *StepRegistry { return reg })
	_, err := factory("cb", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error when step config is missing")
	}
}

func TestResilienceCircuitBreakerStep_InvalidThreshold(t *testing.T) {
	reg := NewStepRegistry()
	factory := NewResilienceCircuitBreakerStepFactory(func() *StepRegistry { return reg })
	_, err := factory("cb", map[string]any{
		"failure_threshold": 0,
		"step":              map[string]any{"type": "step.inner"},
	}, nil)
	if err == nil {
		t.Fatal("expected error for zero failure_threshold")
	}
}

func TestResilienceCircuitBreakerStep_InvalidResetTimeout(t *testing.T) {
	inner := &resilienceSucceedStep{name: "inner"}
	reg := NewStepRegistry()
	reg.Register("step.inner", func(_ string, _ map[string]any, _ modular.Application) (PipelineStep, error) {
		return inner, nil
	})
	factory := NewResilienceCircuitBreakerStepFactory(func() *StepRegistry { return reg })
	_, err := factory("cb", map[string]any{
		"reset_timeout": "not-a-duration",
		"step":          map[string]any{"type": "step.inner"},
	}, nil)
	if err == nil {
		t.Fatal("expected error for invalid reset_timeout")
	}
}

func TestResilienceCircuitBreakerStep_ClosedAllows(t *testing.T) {
	inner := &resilienceSucceedStep{name: "inner"}
	registryFn := buildResilienceRegistry("step.inner", inner)

	factory := NewResilienceCircuitBreakerStepFactory(registryFn)
	step, err := factory("cb", map[string]any{
		"failure_threshold": 3,
		"step":              map[string]any{"type": "step.inner"},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(ctx, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Output["circuit_breaker_open"] != false {
		t.Errorf("expected circuit_breaker_open=false, got %v", result.Output["circuit_breaker_open"])
	}
}

func TestResilienceCircuitBreakerStep_OpensAfterThreshold(t *testing.T) {
	inner := &resilienceFailStep{name: "inner"}
	registryFn := buildResilienceRegistry("step.inner", inner)

	factory := NewResilienceCircuitBreakerStepFactory(registryFn)
	step, err := factory("cb", map[string]any{
		"failure_threshold": 3,
		"reset_timeout":     "1h",
		"step":              map[string]any{"type": "step.inner"},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	pc := NewPipelineContext(nil, nil)

	// Trigger failures up to threshold
	for i := 0; i < 3; i++ {
		_, _ = step.Execute(ctx, pc)
	}

	// Next call should be rejected (circuit open)
	_, err = step.Execute(ctx, pc)
	if err == nil {
		t.Fatal("expected error when circuit is open")
	}
}

func TestResilienceCircuitBreakerStep_FallbackWhenOpen(t *testing.T) {
	inner := &resilienceFailStep{name: "inner"}
	fallback := &resilienceSucceedStep{name: "fallback"}

	reg := NewStepRegistry()
	reg.Register("step.inner", func(_ string, _ map[string]any, _ modular.Application) (PipelineStep, error) {
		return inner, nil
	})
	reg.Register("step.fallback", func(_ string, _ map[string]any, _ modular.Application) (PipelineStep, error) {
		return fallback, nil
	})

	factory := NewResilienceCircuitBreakerStepFactory(func() *StepRegistry { return reg })
	step, err := factory("cb", map[string]any{
		"failure_threshold": 2,
		"reset_timeout":     "1h",
		"step":              map[string]any{"type": "step.inner"},
		"fallback":          map[string]any{"type": "step.fallback"},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	pc := NewPipelineContext(nil, nil)

	// Open the circuit
	for i := 0; i < 2; i++ {
		_, _ = step.Execute(ctx, pc)
	}

	// Should execute fallback
	result, err := step.Execute(ctx, pc)
	if err != nil {
		t.Fatalf("expected fallback to succeed: %v", err)
	}
	if result.Output["circuit_breaker_open"] != true {
		t.Errorf("expected circuit_breaker_open=true in fallback result, got %v", result.Output["circuit_breaker_open"])
	}
	if fallback.callCnt != 1 {
		t.Errorf("expected fallback called once, got %d", fallback.callCnt)
	}
}

func TestResilienceCircuitBreakerStep_ResetsAfterTimeout(t *testing.T) {
	inner := &resilienceCountingStep{name: "inner", failUntil: 2}
	registryFn := buildResilienceRegistry("step.inner", inner)

	factory := NewResilienceCircuitBreakerStepFactory(registryFn)
	step, err := factory("cb", map[string]any{
		"failure_threshold": 2,
		"reset_timeout":     "5ms",
		"step":              map[string]any{"type": "step.inner"},
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	pc := NewPipelineContext(nil, nil)

	// Open the circuit
	for i := 0; i < 2; i++ {
		_, _ = step.Execute(ctx, pc)
	}

	// Wait for reset timeout
	time.Sleep(10 * time.Millisecond)

	// Circuit should be half-open and allow through (inner will succeed since callCnt > failUntil)
	result, err := step.Execute(ctx, pc)
	if err != nil {
		t.Fatalf("expected success after reset: %v", err)
	}
	if result.Output["circuit_breaker_open"] != false {
		t.Errorf("expected circuit_breaker_open=false after reset, got %v", result.Output["circuit_breaker_open"])
	}
}
