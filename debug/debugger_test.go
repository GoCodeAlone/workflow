package debug

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestNewDebugger(t *testing.T) {
	d := New()
	if d == nil {
		t.Fatal("expected non-nil debugger")
	}
	state := d.State()
	if state.Status != "idle" {
		t.Errorf("expected idle status, got %s", state.Status)
	}
}

func TestAddRemoveBreakpoint(t *testing.T) {
	d := New()

	id := d.AddBreakpoint(BreakOnModule, "my-module", "")
	if id == "" {
		t.Fatal("expected non-empty breakpoint ID")
	}

	bps := d.ListBreakpoints()
	if len(bps) != 1 {
		t.Fatalf("expected 1 breakpoint, got %d", len(bps))
	}
	if bps[0].ID != id {
		t.Errorf("expected ID %s, got %s", id, bps[0].ID)
	}
	if bps[0].Type != BreakOnModule {
		t.Errorf("expected type module, got %s", bps[0].Type)
	}
	if bps[0].Target != "my-module" {
		t.Errorf("expected target my-module, got %s", bps[0].Target)
	}
	if !bps[0].Enabled {
		t.Error("expected breakpoint to be enabled")
	}

	if err := d.RemoveBreakpoint(id); err != nil {
		t.Fatalf("failed to remove breakpoint: %v", err)
	}
	if len(d.ListBreakpoints()) != 0 {
		t.Error("expected 0 breakpoints after removal")
	}
}

func TestRemoveBreakpointNotFound(t *testing.T) {
	d := New()
	if err := d.RemoveBreakpoint("nonexistent"); err == nil {
		t.Error("expected error removing nonexistent breakpoint")
	}
}

func TestCheckBreakpoint(t *testing.T) {
	d := New()
	d.AddBreakpoint(BreakOnModule, "processor", "")

	shouldPause, bpID := d.CheckBreakpoint("processor", BreakOnModule)
	if !shouldPause {
		t.Error("expected pause on matching breakpoint")
	}
	if bpID == "" {
		t.Error("expected non-empty breakpoint ID")
	}

	// Non-matching
	shouldPause, _ = d.CheckBreakpoint("other-module", BreakOnModule)
	if shouldPause {
		t.Error("did not expect pause on non-matching module")
	}

	// Wrong type
	shouldPause, _ = d.CheckBreakpoint("processor", BreakOnTrigger)
	if shouldPause {
		t.Error("did not expect pause on wrong type")
	}
}

func TestCheckBreakpointDisabled(t *testing.T) {
	d := New()
	id := d.AddBreakpoint(BreakOnModule, "processor", "")

	// Disable the breakpoint
	d.mu.Lock()
	d.breakpoints[id].Enabled = false
	d.mu.Unlock()

	shouldPause, _ := d.CheckBreakpoint("processor", BreakOnModule)
	if shouldPause {
		t.Error("disabled breakpoint should not cause pause")
	}
}

func TestBreakpointHitCount(t *testing.T) {
	d := New()
	id := d.AddBreakpoint(BreakOnWorkflow, "http", "")

	d.CheckBreakpoint("http", BreakOnWorkflow)
	d.CheckBreakpoint("http", BreakOnWorkflow)
	d.CheckBreakpoint("http", BreakOnWorkflow)

	d.mu.Lock()
	bp := d.breakpoints[id]
	count := bp.HitCount
	d.mu.Unlock()

	if count != 3 {
		t.Errorf("expected hit count 3, got %d", count)
	}
}

func TestRecordStep(t *testing.T) {
	d := New()
	d.SetRunning("http", "handle")
	d.RecordStep("module-a", "module", 100*time.Millisecond, map[string]any{"key": "val"}, nil)
	d.RecordStep("module-b", "module", 50*time.Millisecond, nil, nil)

	state := d.State()
	if len(state.StepHistory) != 2 {
		t.Fatalf("expected 2 step records, got %d", len(state.StepHistory))
	}
	if state.StepHistory[0].Step != "module-a" {
		t.Errorf("expected step module-a, got %s", state.StepHistory[0].Step)
	}
	if state.StepHistory[0].Data["key"] != "val" {
		t.Error("expected step data to be preserved")
	}
}

func TestSetRunningAndIdle(t *testing.T) {
	d := New()
	d.SetRunning("messaging", "send")

	state := d.State()
	if state.Status != "running" {
		t.Errorf("expected running, got %s", state.Status)
	}
	if state.WorkflowType != "messaging" {
		t.Errorf("expected workflow type messaging, got %s", state.WorkflowType)
	}

	d.SetIdle()
	state = d.State()
	if state.Status != "idle" {
		t.Errorf("expected idle, got %s", state.Status)
	}
}

func TestPauseAndResume(t *testing.T) {
	d := New()
	d.SetRunning("http", "request")

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = d.Pause(context.Background(), "step-1", "bp-1", map[string]any{"x": 1})
	}()

	// Wait for the debugger to reach paused state
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if d.State().Status == "paused" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if d.State().Status != "paused" {
		t.Fatal("expected paused status")
	}

	state := d.State()
	if state.CurrentStep != "step-1" {
		t.Errorf("expected current step step-1, got %s", state.CurrentStep)
	}
	if state.BreakpointID != "bp-1" {
		t.Errorf("expected breakpoint ID bp-1, got %s", state.BreakpointID)
	}

	// Resume with Continue
	if err := d.Continue(); err != nil {
		t.Fatalf("continue failed: %v", err)
	}
	wg.Wait()
}

func TestPauseCancelledByContext(t *testing.T) {
	d := New()
	d.SetRunning("http", "request")

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	var pauseErr error
	go func() {
		defer wg.Done()
		pauseErr = d.Pause(ctx, "step-1", "", nil)
	}()

	// Wait for pause state
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if d.State().Status == "paused" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	wg.Wait()

	if pauseErr != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", pauseErr)
	}
	if d.State().Status != "stopped" {
		t.Errorf("expected stopped status after cancel, got %s", d.State().Status)
	}
}

func TestStepWhenNotPaused(t *testing.T) {
	d := New()
	if err := d.Step(); err == nil {
		t.Error("expected error calling Step when not paused")
	}
}

func TestContinueWhenNotPaused(t *testing.T) {
	d := New()
	if err := d.Continue(); err == nil {
		t.Error("expected error calling Continue when not paused")
	}
}

func TestReset(t *testing.T) {
	d := New()
	d.AddBreakpoint(BreakOnModule, "test", "")
	d.SetRunning("http", "test")
	d.RecordStep("step1", "module", time.Millisecond, nil, nil)

	d.Reset()

	state := d.State()
	if state.Status != "idle" {
		t.Errorf("expected idle after reset, got %s", state.Status)
	}
	if len(d.ListBreakpoints()) != 0 {
		t.Error("expected 0 breakpoints after reset")
	}
	if len(state.StepHistory) != 0 {
		t.Error("expected empty step history after reset")
	}
}

func TestMultipleBreakpoints(t *testing.T) {
	d := New()
	d.AddBreakpoint(BreakOnModule, "module-a", "")
	d.AddBreakpoint(BreakOnModule, "module-b", "")
	d.AddBreakpoint(BreakOnTrigger, "http", "")

	bps := d.ListBreakpoints()
	if len(bps) != 3 {
		t.Errorf("expected 3 breakpoints, got %d", len(bps))
	}

	shouldPause, _ := d.CheckBreakpoint("module-a", BreakOnModule)
	if !shouldPause {
		t.Error("expected pause on module-a")
	}

	shouldPause, _ = d.CheckBreakpoint("http", BreakOnTrigger)
	if !shouldPause {
		t.Error("expected pause on trigger http")
	}

	shouldPause, _ = d.CheckBreakpoint("module-c", BreakOnModule)
	if !shouldPause {
		// module-c has no breakpoint
	}
}
