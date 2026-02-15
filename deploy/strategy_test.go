package deploy

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

// --- Blue-Green Tests ---

func TestBlueGreenExecute(t *testing.T) {
	bg := NewBlueGreenStrategy(testLogger())

	plan := &DeploymentPlan{
		WorkflowID:  "wf-1",
		FromVersion: 1,
		ToVersion:   2,
		Strategy:    "blue-green",
		Config:      nil,
	}

	result, err := bg.Execute(context.Background(), plan)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Status != "success" {
		t.Errorf("expected success, got %s", result.Status)
	}
	if result.RolledBack {
		t.Error("should not be rolled back")
	}

	// Verify state: green should now be active (deployed to standby=green, then swapped).
	state, ok := bg.GetState("wf-1")
	if !ok {
		t.Fatal("expected state to exist")
	}
	if state.ActiveEnv != EnvGreen {
		t.Errorf("expected green active, got %s", state.ActiveEnv)
	}
	if state.ActiveVer != 2 {
		t.Errorf("expected active version 2, got %d", state.ActiveVer)
	}
}

func TestBlueGreenExecuteSecondDeploy(t *testing.T) {
	bg := NewBlueGreenStrategy(testLogger())

	// First deploy.
	plan1 := &DeploymentPlan{
		WorkflowID: "wf-1", FromVersion: 1, ToVersion: 2,
		Strategy: "blue-green",
	}
	_, _ = bg.Execute(context.Background(), plan1)

	// Second deploy should swap back.
	plan2 := &DeploymentPlan{
		WorkflowID: "wf-1", FromVersion: 2, ToVersion: 3,
		Strategy: "blue-green",
	}
	result, err := bg.Execute(context.Background(), plan2)
	if err != nil {
		t.Fatalf("second execute: %v", err)
	}
	if result.Status != "success" {
		t.Errorf("expected success, got %s", result.Status)
	}

	state, _ := bg.GetState("wf-1")
	if state.ActiveEnv != EnvBlue {
		t.Errorf("expected blue active after second deploy, got %s", state.ActiveEnv)
	}
	if state.ActiveVer != 3 {
		t.Errorf("expected active version 3, got %d", state.ActiveVer)
	}
}

func TestBlueGreenRollback(t *testing.T) {
	bg := NewBlueGreenStrategy(testLogger())

	// Deploy first.
	plan := &DeploymentPlan{
		WorkflowID: "wf-1", FromVersion: 1, ToVersion: 2,
		Strategy: "blue-green",
	}
	_, _ = bg.Execute(context.Background(), plan)

	// Rollback.
	result, err := bg.Rollback(context.Background(), "wf-1")
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if result.Status != "rolled_back" {
		t.Errorf("expected rolled_back, got %s", result.Status)
	}
	if !result.RolledBack {
		t.Error("expected RolledBack to be true")
	}

	state, _ := bg.GetState("wf-1")
	if state.ActiveEnv != EnvBlue {
		t.Errorf("expected blue after rollback, got %s", state.ActiveEnv)
	}
	if state.ActiveVer != 1 {
		t.Errorf("expected version 1 after rollback, got %d", state.ActiveVer)
	}
}

func TestBlueGreenRollbackNoState(t *testing.T) {
	bg := NewBlueGreenStrategy(testLogger())
	_, err := bg.Rollback(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for rollback without state")
	}
}

func TestBlueGreenExecuteNilPlan(t *testing.T) {
	bg := NewBlueGreenStrategy(testLogger())
	_, err := bg.Execute(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil plan")
	}
}

func TestBlueGreenExecuteCancelledContext(t *testing.T) {
	bg := NewBlueGreenStrategy(testLogger())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	plan := &DeploymentPlan{
		WorkflowID: "wf-1", FromVersion: 1, ToVersion: 2,
		Strategy: "blue-green",
	}
	result, err := bg.Execute(ctx, plan)
	if err == nil {
		t.Error("expected error for cancelled context")
	}
	if result.Status != "failed" {
		t.Errorf("expected failed status, got %s", result.Status)
	}
}

func TestBlueGreenValidate(t *testing.T) {
	bg := NewBlueGreenStrategy(testLogger())

	if err := bg.Validate(nil); err != nil {
		t.Errorf("nil config should be valid: %v", err)
	}
	if err := bg.Validate(map[string]any{"health_check_timeout": "10s"}); err != nil {
		t.Errorf("valid timeout should pass: %v", err)
	}
	if err := bg.Validate(map[string]any{"health_check_timeout": "invalid"}); err == nil {
		t.Error("expected error for invalid timeout")
	}
	if err := bg.Validate(map[string]any{"health_check_timeout": -1}); err == nil {
		t.Error("expected error for negative timeout")
	}
}

// --- Canary Tests ---

func TestCanaryExecute(t *testing.T) {
	cs := NewCanaryStrategy(testLogger())

	plan := &DeploymentPlan{
		WorkflowID:  "wf-canary",
		FromVersion: 1,
		ToVersion:   2,
		Strategy:    "canary",
		Config: map[string]any{
			"initial_percent": 20.0,
			"increment":       40.0,
			"interval":        "1ms", // Fast for testing.
			"error_threshold": 10.0,
		},
	}

	result, err := cs.Execute(context.Background(), plan)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Status != "success" {
		t.Errorf("expected success, got %s: %s", result.Status, result.Message)
	}

	split, ok := cs.GetSplit("wf-canary")
	if !ok {
		t.Fatal("expected split to exist")
	}
	if split.CanaryPercent != 100 {
		t.Errorf("expected 100%% canary, got %.0f%%", split.CanaryPercent)
	}
}

func TestCanaryTrafficProgression(t *testing.T) {
	cs := NewCanaryStrategy(testLogger())

	// Track the traffic percentages at each health check.
	var percentages []float64
	cs.SetHealthCheck(func(_ context.Context, _ string, _ int) (float64, error) {
		split, _ := cs.GetSplit("wf-progress")
		percentages = append(percentages, split.CanaryPercent)
		return 0, nil // Always healthy.
	})

	plan := &DeploymentPlan{
		WorkflowID:  "wf-progress",
		FromVersion: 1,
		ToVersion:   2,
		Strategy:    "canary",
		Config: map[string]any{
			"initial_percent": 10.0,
			"increment":       20.0,
			"interval":        "1ms",
		},
	}

	result, err := cs.Execute(context.Background(), plan)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Status != "success" {
		t.Fatalf("expected success, got %s: %s", result.Status, result.Message)
	}

	// With initial=10, increment=20: 10 -> 30 -> 50 -> 70 -> 90 -> 100 (breaks before health check at 100)
	// Health checks happen at: 10, 30, 50, 70, 90
	if len(percentages) < 3 {
		t.Errorf("expected at least 3 health checks, got %d: %v", len(percentages), percentages)
	}

	// Verify percentages are monotonically increasing.
	for i := 1; i < len(percentages); i++ {
		if percentages[i] <= percentages[i-1] {
			t.Errorf("percentages not increasing at index %d: %v", i, percentages)
		}
	}
}

func TestCanaryRollbackOnHighErrorRate(t *testing.T) {
	cs := NewCanaryStrategy(testLogger())

	callCount := 0
	cs.SetHealthCheck(func(_ context.Context, _ string, _ int) (float64, error) {
		callCount++
		if callCount >= 2 {
			return 15.0, nil // High error rate on second check.
		}
		return 1.0, nil
	})

	plan := &DeploymentPlan{
		WorkflowID:  "wf-rollback",
		FromVersion: 1,
		ToVersion:   2,
		Strategy:    "canary",
		Config: map[string]any{
			"initial_percent": 10.0,
			"increment":       10.0,
			"interval":        "1ms",
			"error_threshold": 5.0,
		},
	}

	result, err := cs.Execute(context.Background(), plan)
	if err != nil {
		t.Fatalf("should not return error on rollback: %v", err)
	}
	if result.Status != "rolled_back" {
		t.Errorf("expected rolled_back, got %s: %s", result.Status, result.Message)
	}
	if !result.RolledBack {
		t.Error("expected RolledBack to be true")
	}
}

func TestCanaryRollbackManual(t *testing.T) {
	cs := NewCanaryStrategy(testLogger())

	// Start a deployment that we'll manually rollback.
	// Use a blocked context so we can control execution.
	ctx, cancel := context.WithCancel(context.Background())

	// Deploy with very long interval so it blocks.
	go func() {
		plan := &DeploymentPlan{
			WorkflowID:  "wf-manual-rb",
			FromVersion: 1,
			ToVersion:   2,
			Strategy:    "canary",
			Config: map[string]any{
				"initial_percent": 50.0,
				"increment":       10.0,
				"interval":        "10m", // Very long.
			},
		}
		_, _ = cs.Execute(ctx, plan)
	}()

	// Wait for split to appear.
	time.Sleep(50 * time.Millisecond)

	result, err := cs.Rollback(context.Background(), "wf-manual-rb")
	cancel() // Cancel the blocked goroutine.

	if err != nil {
		t.Fatalf("manual rollback: %v", err)
	}
	if result.Status != "rolled_back" {
		t.Errorf("expected rolled_back, got %s", result.Status)
	}

	split, _ := cs.GetSplit("wf-manual-rb")
	if split.CanaryPercent != 0 {
		t.Errorf("expected 0%% canary after rollback, got %.0f%%", split.CanaryPercent)
	}
}

func TestCanaryRollbackNoState(t *testing.T) {
	cs := NewCanaryStrategy(testLogger())
	_, err := cs.Rollback(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for rollback without state")
	}
}

func TestCanaryExecuteNilPlan(t *testing.T) {
	cs := NewCanaryStrategy(testLogger())
	_, err := cs.Execute(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil plan")
	}
}

func TestCanaryValidate(t *testing.T) {
	cs := NewCanaryStrategy(testLogger())

	if err := cs.Validate(nil); err != nil {
		t.Errorf("nil config should be valid: %v", err)
	}

	valid := map[string]any{
		"initial_percent": 10.0,
		"increment":       20.0,
		"interval":        "5s",
		"error_threshold": 5.0,
	}
	if err := cs.Validate(valid); err != nil {
		t.Errorf("valid config should pass: %v", err)
	}

	invalid := map[string]any{"initial_percent": -1.0}
	if err := cs.Validate(invalid); err == nil {
		t.Error("expected error for negative initial_percent")
	}

	invalid2 := map[string]any{"increment": 0.0}
	if err := cs.Validate(invalid2); err == nil {
		t.Error("expected error for zero increment")
	}

	invalid3 := map[string]any{"interval": "not-a-duration"}
	if err := cs.Validate(invalid3); err == nil {
		t.Error("expected error for invalid interval")
	}

	invalid4 := map[string]any{"error_threshold": -1.0}
	if err := cs.Validate(invalid4); err == nil {
		t.Error("expected error for negative error_threshold")
	}
}

func TestCanaryDefaultConfig(t *testing.T) {
	cs := NewCanaryStrategy(testLogger())

	plan := &DeploymentPlan{
		WorkflowID:  "wf-defaults",
		FromVersion: 1,
		ToVersion:   2,
		Strategy:    "canary",
		Config:      nil, // Use all defaults.
	}

	// With nil health check (always healthy) and default config, the canary
	// should complete. Interval defaults to 30s, so we override just the interval
	// to keep tests fast.
	plan.Config = map[string]any{"interval": "1ms"}

	result, err := cs.Execute(context.Background(), plan)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Status != "success" {
		t.Errorf("expected success, got %s", result.Status)
	}
}

func TestCanaryHealthCheckError(t *testing.T) {
	cs := NewCanaryStrategy(testLogger())
	cs.SetHealthCheck(func(_ context.Context, _ string, _ int) (float64, error) {
		return 0, fmt.Errorf("health check unavailable")
	})

	plan := &DeploymentPlan{
		WorkflowID:  "wf-hc-error",
		FromVersion: 1,
		ToVersion:   2,
		Strategy:    "canary",
		Config: map[string]any{
			"interval": "1ms",
		},
	}

	result, err := cs.Execute(context.Background(), plan)
	if err == nil {
		t.Error("expected error when health check fails")
	}
	if result.Status != "failed" {
		t.Errorf("expected failed, got %s", result.Status)
	}
}

// --- Rolling Tests ---

func TestRollingExecute(t *testing.T) {
	rs := NewRollingStrategy(testLogger())

	plan := &DeploymentPlan{
		WorkflowID:  "wf-rolling",
		FromVersion: 1,
		ToVersion:   2,
		Strategy:    "rolling",
		Config: map[string]any{
			"batch_size": 2,
			"delay":      "1ms",
			"instances":  6,
		},
	}

	result, err := rs.Execute(context.Background(), plan)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Status != "success" {
		t.Errorf("expected success, got %s: %s", result.Status, result.Message)
	}
}

func TestRollingExecuteDefaults(t *testing.T) {
	rs := NewRollingStrategy(testLogger())

	plan := &DeploymentPlan{
		WorkflowID:  "wf-rolling-defaults",
		FromVersion: 1,
		ToVersion:   2,
		Strategy:    "rolling",
		Config: map[string]any{
			"delay": "1ms", // Override delay for test speed.
		},
	}

	result, err := rs.Execute(context.Background(), plan)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Status != "success" {
		t.Errorf("expected success, got %s", result.Status)
	}
}

func TestRollingExecuteNilPlan(t *testing.T) {
	rs := NewRollingStrategy(testLogger())
	_, err := rs.Execute(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil plan")
	}
}

func TestRollingExecuteCancelledContext(t *testing.T) {
	rs := NewRollingStrategy(testLogger())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	plan := &DeploymentPlan{
		WorkflowID: "wf-cancel", FromVersion: 1, ToVersion: 2,
		Strategy: "rolling",
		Config:   map[string]any{"instances": 10, "delay": "1ms"},
	}

	_, err := rs.Execute(ctx, plan)
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestRollingValidate(t *testing.T) {
	rs := NewRollingStrategy(testLogger())

	if err := rs.Validate(nil); err != nil {
		t.Errorf("nil config should be valid: %v", err)
	}
	if err := rs.Validate(map[string]any{"batch_size": 5, "delay": "1s"}); err != nil {
		t.Errorf("valid config should pass: %v", err)
	}
	if err := rs.Validate(map[string]any{"batch_size": 0}); err == nil {
		t.Error("expected error for zero batch_size")
	}
	if err := rs.Validate(map[string]any{"delay": "invalid"}); err == nil {
		t.Error("expected error for invalid delay")
	}
	if err := rs.Validate(map[string]any{"delay": -1}); err == nil {
		t.Error("expected error for negative delay")
	}
}

// --- Registry Tests ---

func TestRegistryGetAll(t *testing.T) {
	reg := NewStrategyRegistry(testLogger())

	names := reg.List()
	if len(names) != 3 {
		t.Fatalf("expected 3 built-in strategies, got %d: %v", len(names), names)
	}

	expected := []string{"blue-green", "canary", "rolling"}
	for i, name := range expected {
		if names[i] != name {
			t.Errorf("expected %s at index %d, got %s", name, i, names[i])
		}
	}

	for _, name := range expected {
		s, ok := reg.Get(name)
		if !ok {
			t.Errorf("strategy %q not found", name)
		}
		if s.Name() != name {
			t.Errorf("strategy name mismatch: %s vs %s", s.Name(), name)
		}
	}
}

func TestRegistryGetNotFound(t *testing.T) {
	reg := NewStrategyRegistry(testLogger())
	_, ok := reg.Get("nonexistent")
	if ok {
		t.Error("expected not found")
	}
}

func TestRegistryRegisterCustom(t *testing.T) {
	reg := NewStrategyRegistry(testLogger())
	custom := &mockStrategy{name: "custom"}
	reg.Register(custom)

	s, ok := reg.Get("custom")
	if !ok {
		t.Fatal("custom strategy not found")
	}
	if s.Name() != "custom" {
		t.Errorf("expected custom, got %s", s.Name())
	}
	if len(reg.List()) != 4 {
		t.Errorf("expected 4 strategies, got %d", len(reg.List()))
	}
}

func TestRegistryRegisterNil(t *testing.T) {
	reg := NewStrategyRegistry(testLogger())
	reg.Register(nil) // Should not panic.
	if len(reg.List()) != 3 {
		t.Errorf("expected 3 strategies, got %d", len(reg.List()))
	}
}

func TestRegistryExecute(t *testing.T) {
	reg := NewStrategyRegistry(testLogger())

	plan := &DeploymentPlan{
		WorkflowID:  "wf-reg",
		FromVersion: 1,
		ToVersion:   2,
		Strategy:    "rolling",
		Config:      map[string]any{"delay": "1ms"},
	}

	result, err := reg.Execute(plan)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Status != "success" {
		t.Errorf("expected success, got %s", result.Status)
	}
}

func TestRegistryExecuteNilPlan(t *testing.T) {
	reg := NewStrategyRegistry(testLogger())
	_, err := reg.Execute(nil)
	if err == nil {
		t.Error("expected error for nil plan")
	}
}

func TestRegistryExecuteUnknownStrategy(t *testing.T) {
	reg := NewStrategyRegistry(testLogger())
	plan := &DeploymentPlan{Strategy: "unknown"}
	_, err := reg.Execute(plan)
	if err == nil {
		t.Error("expected error for unknown strategy")
	}
}

func TestRegistryConcurrent(t *testing.T) {
	reg := NewStrategyRegistry(testLogger())
	var wg sync.WaitGroup

	// Concurrent reads.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = reg.List()
			_, _ = reg.Get("rolling")
			_, _ = reg.Get("blue-green")
			_, _ = reg.Get("canary")
		}()
	}

	// Concurrent writes.
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			reg.Register(&mockStrategy{name: fmt.Sprintf("custom-%d", n)})
		}(i)
	}

	wg.Wait()

	// Verify all custom strategies were registered.
	names := reg.List()
	if len(names) < 8 { // 3 built-in + 5 custom
		t.Errorf("expected at least 8 strategies, got %d: %v", len(names), names)
	}
}

// mockStrategy is a minimal DeploymentStrategy for testing.
type mockStrategy struct {
	name string
}

func (m *mockStrategy) Name() string { return m.name }
func (m *mockStrategy) Validate(_ map[string]any) error {
	return nil
}
func (m *mockStrategy) Execute(_ context.Context, plan *DeploymentPlan) (*DeploymentResult, error) {
	return &DeploymentResult{
		Status:      "success",
		StartedAt:   time.Now(),
		CompletedAt: time.Now(),
		Message:     fmt.Sprintf("mock deploy for %s", plan.WorkflowID),
	}, nil
}
