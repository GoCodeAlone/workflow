package debug

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func newTestManager() *BreakpointManager {
	return NewBreakpointManager(slog.Default())
}

func TestSetAndRemoveBreakpoint(t *testing.T) {
	m := newTestManager()

	bp := m.SetBreakpoint("order-pipeline", "validate", "")
	if bp == nil {
		t.Fatal("expected non-nil breakpoint")
	}
	if bp.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if bp.PipelineName != "order-pipeline" {
		t.Errorf("expected pipeline_name order-pipeline, got %s", bp.PipelineName)
	}
	if bp.StepName != "validate" {
		t.Errorf("expected step_name validate, got %s", bp.StepName)
	}
	if !bp.Enabled {
		t.Error("expected breakpoint to be enabled by default")
	}
	if bp.HitCount != 0 {
		t.Errorf("expected hit count 0, got %d", bp.HitCount)
	}

	bps := m.ListBreakpoints()
	if len(bps) != 1 {
		t.Fatalf("expected 1 breakpoint, got %d", len(bps))
	}

	// Remove it
	removed := m.RemoveBreakpoint("order-pipeline", "validate")
	if !removed {
		t.Error("expected removal to succeed")
	}
	if len(m.ListBreakpoints()) != 0 {
		t.Error("expected 0 breakpoints after removal")
	}

	// Remove nonexistent
	removed = m.RemoveBreakpoint("order-pipeline", "validate")
	if removed {
		t.Error("expected removal of nonexistent breakpoint to return false")
	}
}

func TestEnableDisableBreakpoint(t *testing.T) {
	m := newTestManager()

	m.SetBreakpoint("pipeline-a", "step-1", "")

	// Disable
	ok := m.DisableBreakpoint("pipeline-a", "step-1")
	if !ok {
		t.Fatal("expected disable to succeed")
	}

	// Verify disabled
	bps := m.ListBreakpoints()
	if len(bps) != 1 {
		t.Fatalf("expected 1 breakpoint, got %d", len(bps))
	}
	if bps[0].Enabled {
		t.Error("expected breakpoint to be disabled")
	}

	// CheckBreakpoint should return false when disabled
	if m.CheckBreakpoint("pipeline-a", "step-1", nil) {
		t.Error("disabled breakpoint should not trigger")
	}

	// Re-enable
	ok = m.EnableBreakpoint("pipeline-a", "step-1")
	if !ok {
		t.Fatal("expected enable to succeed")
	}

	bps = m.ListBreakpoints()
	if !bps[0].Enabled {
		t.Error("expected breakpoint to be re-enabled")
	}

	// CheckBreakpoint should return true when enabled
	if !m.CheckBreakpoint("pipeline-a", "step-1", nil) {
		t.Error("enabled breakpoint should trigger")
	}

	// Enable/disable nonexistent
	if m.EnableBreakpoint("nope", "nope") {
		t.Error("expected enable of nonexistent breakpoint to return false")
	}
	if m.DisableBreakpoint("nope", "nope") {
		t.Error("expected disable of nonexistent breakpoint to return false")
	}
}

func TestCheckPipelineBreakpoint(t *testing.T) {
	m := newTestManager()

	m.SetBreakpoint("pipeline-a", "step-1", "")

	// Match
	if !m.CheckBreakpoint("pipeline-a", "step-1", nil) {
		t.Error("expected breakpoint to fire")
	}

	// No match - wrong pipeline
	if m.CheckBreakpoint("pipeline-b", "step-1", nil) {
		t.Error("should not fire for wrong pipeline")
	}

	// No match - wrong step
	if m.CheckBreakpoint("pipeline-a", "step-2", nil) {
		t.Error("should not fire for wrong step")
	}

	// No match - no breakpoint at all
	if m.CheckBreakpoint("none", "none", nil) {
		t.Error("should not fire when no breakpoint exists")
	}
}

func TestCheckBreakpointWithCondition(t *testing.T) {
	m := newTestManager()

	// Set breakpoint with condition key "debug_mode"
	m.SetBreakpoint("pipeline-a", "step-1", "debug_mode")

	// Should not fire: key missing from context
	if m.CheckBreakpoint("pipeline-a", "step-1", map[string]any{}) {
		t.Error("should not fire when condition key is missing")
	}

	// Should not fire: key present but falsy (false)
	if m.CheckBreakpoint("pipeline-a", "step-1", map[string]any{"debug_mode": false}) {
		t.Error("should not fire when condition is false")
	}

	// Should not fire: key present but falsy (0)
	if m.CheckBreakpoint("pipeline-a", "step-1", map[string]any{"debug_mode": 0}) {
		t.Error("should not fire when condition is 0")
	}

	// Should not fire: key present but falsy (empty string)
	if m.CheckBreakpoint("pipeline-a", "step-1", map[string]any{"debug_mode": ""}) {
		t.Error("should not fire when condition is empty string")
	}

	// Should not fire: key present but falsy (nil)
	if m.CheckBreakpoint("pipeline-a", "step-1", map[string]any{"debug_mode": nil}) {
		t.Error("should not fire when condition is nil")
	}

	// Should fire: key present and truthy (true)
	if !m.CheckBreakpoint("pipeline-a", "step-1", map[string]any{"debug_mode": true}) {
		t.Error("should fire when condition is true")
	}

	// Should fire: key present and truthy (non-zero int)
	if !m.CheckBreakpoint("pipeline-a", "step-1", map[string]any{"debug_mode": 1}) {
		t.Error("should fire when condition is 1")
	}

	// Should fire: key present and truthy (non-empty string)
	if !m.CheckBreakpoint("pipeline-a", "step-1", map[string]any{"debug_mode": "yes"}) {
		t.Error("should fire when condition is non-empty string")
	}

	// Unconditional breakpoint always fires regardless of context
	m.SetBreakpoint("pipeline-b", "step-2", "")
	if !m.CheckBreakpoint("pipeline-b", "step-2", map[string]any{}) {
		t.Error("unconditional breakpoint should always fire")
	}
}

func TestPipelinePauseAndResume(t *testing.T) {
	m := newTestManager()

	var wg sync.WaitGroup
	var gotAction ResumeAction

	wg.Add(1)
	go func() {
		defer wg.Done()
		ch := m.Pause("exec-1", "order-pipeline", "validate", 0, map[string]any{"order_id": "123"})
		gotAction = <-ch
	}()

	// Wait for the execution to appear in paused list
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(m.ListPaused()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	paused := m.ListPaused()
	if len(paused) != 1 {
		t.Fatalf("expected 1 paused execution, got %d", len(paused))
	}
	if paused[0].ID != "exec-1" {
		t.Errorf("expected execution ID exec-1, got %s", paused[0].ID)
	}
	if paused[0].PipelineName != "order-pipeline" {
		t.Errorf("expected pipeline order-pipeline, got %s", paused[0].PipelineName)
	}
	if paused[0].StepName != "validate" {
		t.Errorf("expected step validate, got %s", paused[0].StepName)
	}
	if paused[0].Context["order_id"] != "123" {
		t.Error("expected context snapshot to contain order_id")
	}

	// Resume with continue
	err := m.Resume("exec-1", ResumeAction{Action: "continue"})
	if err != nil {
		t.Fatalf("resume failed: %v", err)
	}
	wg.Wait()

	if gotAction.Action != "continue" {
		t.Errorf("expected continue action, got %s", gotAction.Action)
	}

	// Verify it's removed from paused list
	if len(m.ListPaused()) != 0 {
		t.Error("expected 0 paused executions after resume")
	}
}

func TestPauseAndSkip(t *testing.T) {
	m := newTestManager()

	var wg sync.WaitGroup
	var gotAction ResumeAction

	wg.Add(1)
	go func() {
		defer wg.Done()
		ch := m.Pause("exec-2", "pipeline", "step-a", 1, nil)
		gotAction = <-ch
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(m.ListPaused()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	err := m.Resume("exec-2", ResumeAction{Action: "skip"})
	if err != nil {
		t.Fatalf("resume failed: %v", err)
	}
	wg.Wait()

	if gotAction.Action != "skip" {
		t.Errorf("expected skip action, got %s", gotAction.Action)
	}
}

func TestPauseAndAbort(t *testing.T) {
	m := newTestManager()

	var wg sync.WaitGroup
	var gotAction ResumeAction

	wg.Add(1)
	go func() {
		defer wg.Done()
		ch := m.Pause("exec-3", "pipeline", "step-b", 2, nil)
		gotAction = <-ch
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(m.ListPaused()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	err := m.Resume("exec-3", ResumeAction{Action: "abort"})
	if err != nil {
		t.Fatalf("resume failed: %v", err)
	}
	wg.Wait()

	if gotAction.Action != "abort" {
		t.Errorf("expected abort action, got %s", gotAction.Action)
	}
}

func TestResumeWithModifiedData(t *testing.T) {
	m := newTestManager()

	var wg sync.WaitGroup
	var gotAction ResumeAction

	wg.Add(1)
	go func() {
		defer wg.Done()
		ch := m.Pause("exec-4", "pipeline", "step-c", 0, map[string]any{"x": 1})
		gotAction = <-ch
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(m.ListPaused()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	injectedData := map[string]any{"x": 42, "injected": true}
	err := m.Resume("exec-4", ResumeAction{Action: "continue", Data: injectedData})
	if err != nil {
		t.Fatalf("resume failed: %v", err)
	}
	wg.Wait()

	if gotAction.Action != "continue" {
		t.Errorf("expected continue, got %s", gotAction.Action)
	}
	if gotAction.Data["x"] != 42 {
		t.Errorf("expected injected x=42, got %v", gotAction.Data["x"])
	}
	if gotAction.Data["injected"] != true {
		t.Error("expected injected=true in data")
	}
}

func TestResumeNonexistent(t *testing.T) {
	m := newTestManager()

	err := m.Resume("nonexistent", ResumeAction{Action: "continue"})
	if err == nil {
		t.Error("expected error resuming nonexistent execution")
	}
}

func TestListPaused(t *testing.T) {
	m := newTestManager()

	// No paused executions initially
	if len(m.ListPaused()) != 0 {
		t.Error("expected 0 paused executions initially")
	}

	// Pause two executions
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		ch := m.Pause("exec-a", "p1", "s1", 0, nil)
		<-ch
	}()
	go func() {
		defer wg.Done()
		ch := m.Pause("exec-b", "p2", "s2", 1, nil)
		<-ch
	}()

	// Wait for both to appear
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(m.ListPaused()) >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	paused := m.ListPaused()
	if len(paused) != 2 {
		t.Fatalf("expected 2 paused executions, got %d", len(paused))
	}

	// Cleanup
	_ = m.Resume("exec-a", ResumeAction{Action: "abort"})
	_ = m.Resume("exec-b", ResumeAction{Action: "abort"})
	wg.Wait()
}

func TestGetPaused(t *testing.T) {
	m := newTestManager()

	// Not found
	_, ok := m.GetPaused("exec-1")
	if ok {
		t.Error("expected not found for nonexistent execution")
	}

	// Pause and find
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ch := m.Pause("exec-1", "pipeline", "step", 0, map[string]any{"key": "value"})
		<-ch
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := m.GetPaused("exec-1"); ok {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	pe, ok := m.GetPaused("exec-1")
	if !ok {
		t.Fatal("expected to find paused execution")
	}
	if pe.ID != "exec-1" {
		t.Errorf("expected ID exec-1, got %s", pe.ID)
	}
	if pe.Context["key"] != "value" {
		t.Error("expected context to contain key=value")
	}

	_ = m.Resume("exec-1", ResumeAction{Action: "abort"})
	wg.Wait()
}

func TestClearAll(t *testing.T) {
	m := newTestManager()

	// Add some breakpoints
	m.SetBreakpoint("p1", "s1", "")
	m.SetBreakpoint("p2", "s2", "")
	m.SetBreakpoint("p3", "s3", "cond")

	if len(m.ListBreakpoints()) != 3 {
		t.Fatalf("expected 3 breakpoints, got %d", len(m.ListBreakpoints()))
	}

	// Pause an execution
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ch := m.Pause("exec-1", "p1", "s1", 0, nil)
		<-ch // will receive abort from ClearAll
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(m.ListPaused()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Clear all
	m.ClearAll()
	wg.Wait()

	if len(m.ListBreakpoints()) != 0 {
		t.Errorf("expected 0 breakpoints after clear, got %d", len(m.ListBreakpoints()))
	}
	if len(m.ListPaused()) != 0 {
		t.Errorf("expected 0 paused executions after clear, got %d", len(m.ListPaused()))
	}
}

func TestConcurrentBreakpoints(t *testing.T) {
	m := newTestManager()

	var wg sync.WaitGroup
	// Concurrently set many breakpoints
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			pipeline := "pipeline"
			step := "step-" + string(rune('a'+idx%26))
			m.SetBreakpoint(pipeline, step, "")
		}(i)
	}
	wg.Wait()

	bps := m.ListBreakpoints()
	if len(bps) == 0 {
		t.Error("expected some breakpoints to be set")
	}

	// Concurrent checks
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			step := "step-" + string(rune('a'+idx%26))
			m.CheckBreakpoint("pipeline", step, nil)
		}(i)
	}
	wg.Wait()

	// Concurrent pause/resume
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			execID := "concurrent-exec-" + string(rune('0'+idx))
			ch := m.Pause(execID, "pipeline", "step-a", 0, nil)
			// A separate goroutine will resume
			go func() {
				time.Sleep(10 * time.Millisecond)
				_ = m.Resume(execID, ResumeAction{Action: "continue"})
			}()
			<-ch
		}(i)
	}
	wg.Wait()
}

func TestPipelineBreakpointHitCount(t *testing.T) {
	m := newTestManager()

	m.SetBreakpoint("pipeline", "step-1", "")

	// Hit it 5 times
	for i := 0; i < 5; i++ {
		m.CheckBreakpoint("pipeline", "step-1", nil)
	}

	bps := m.ListBreakpoints()
	if len(bps) != 1 {
		t.Fatalf("expected 1 breakpoint, got %d", len(bps))
	}
	if bps[0].HitCount != 5 {
		t.Errorf("expected hit count 5, got %d", bps[0].HitCount)
	}
}

func TestBreakpointOverwrite(t *testing.T) {
	m := newTestManager()

	bp1 := m.SetBreakpoint("pipeline", "step", "cond1")
	bp2 := m.SetBreakpoint("pipeline", "step", "cond2")

	// Should overwrite, only 1 breakpoint
	bps := m.ListBreakpoints()
	if len(bps) != 1 {
		t.Fatalf("expected 1 breakpoint after overwrite, got %d", len(bps))
	}
	if bps[0].Condition != "cond2" {
		t.Errorf("expected condition cond2, got %s", bps[0].Condition)
	}
	if bp1.ID == bp2.ID {
		t.Error("expected different IDs for overwritten breakpoint")
	}
}

func TestBreakpointInterceptorInterface(t *testing.T) {
	m := newTestManager()

	// Verify BreakpointManager satisfies BreakpointInterceptor
	var interceptor BreakpointInterceptor = m
	_ = interceptor

	m.SetBreakpoint("pipeline", "step", "")

	// ShouldPause
	if !m.ShouldPause("pipeline", "step", nil) {
		t.Error("ShouldPause should return true for matching breakpoint")
	}
	if m.ShouldPause("pipeline", "other", nil) {
		t.Error("ShouldPause should return false for non-matching step")
	}
}

func TestWaitForResume(t *testing.T) {
	m := newTestManager()

	var wg sync.WaitGroup
	var gotAction ResumeAction
	var gotErr error

	wg.Add(1)
	go func() {
		defer wg.Done()
		gotAction, gotErr = m.WaitForResume("exec-w", "pipeline", "step", 0, map[string]any{"x": 1})
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(m.ListPaused()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	err := m.Resume("exec-w", ResumeAction{Action: "step_over", Data: map[string]any{"y": 2}})
	if err != nil {
		t.Fatalf("resume failed: %v", err)
	}
	wg.Wait()

	if gotErr != nil {
		t.Fatalf("WaitForResume returned error: %v", gotErr)
	}
	if gotAction.Action != "step_over" {
		t.Errorf("expected step_over, got %s", gotAction.Action)
	}
	if gotAction.Data["y"] != 2 {
		t.Error("expected data to contain y=2")
	}
}

func TestContextSnapshotIsolation(t *testing.T) {
	m := newTestManager()

	original := map[string]any{"key": "original"}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ch := m.Pause("exec-iso", "pipeline", "step", 0, original)
		<-ch
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(m.ListPaused()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Modify the original map after pausing
	original["key"] = "modified"

	pe, ok := m.GetPaused("exec-iso")
	if !ok {
		t.Fatal("expected to find paused execution")
	}

	// Snapshot should be isolated
	if pe.Context["key"] != "original" {
		t.Errorf("expected snapshot to be 'original', got %v", pe.Context["key"])
	}

	_ = m.Resume("exec-iso", ResumeAction{Action: "abort"})
	wg.Wait()
}

// --- HTTP Handler Tests ---

func setupBreakpointTestHandler() (*BreakpointHandler, *http.ServeMux) {
	m := NewBreakpointManager(slog.Default())
	h := NewBreakpointHandler(m, slog.Default())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return h, mux
}

func TestHTTPListBreakpoints(t *testing.T) {
	_, mux := setupBreakpointTestHandler()

	req := httptest.NewRequest("GET", "/api/v1/debug/breakpoints", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var bps []*PipelineBreakpoint
	if err := json.NewDecoder(w.Body).Decode(&bps); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if len(bps) != 0 {
		t.Errorf("expected 0 breakpoints, got %d", len(bps))
	}
}

func TestHTTPSetBreakpoint(t *testing.T) {
	_, mux := setupBreakpointTestHandler()

	body := `{"pipeline":"order-pipeline","step":"validate","condition":"debug_mode"}`
	req := httptest.NewRequest("POST", "/api/v1/debug/breakpoints", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var bp PipelineBreakpoint
	if err := json.NewDecoder(w.Body).Decode(&bp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if bp.ID == "" {
		t.Error("expected non-empty ID")
	}
	if bp.PipelineName != "order-pipeline" {
		t.Errorf("expected pipeline order-pipeline, got %s", bp.PipelineName)
	}
	if bp.StepName != "validate" {
		t.Errorf("expected step validate, got %s", bp.StepName)
	}
	if bp.Condition != "debug_mode" {
		t.Errorf("expected condition debug_mode, got %s", bp.Condition)
	}

	// Verify it shows up in list
	req = httptest.NewRequest("GET", "/api/v1/debug/breakpoints", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var bps []*PipelineBreakpoint
	_ = json.NewDecoder(w.Body).Decode(&bps)
	if len(bps) != 1 {
		t.Errorf("expected 1 breakpoint in list, got %d", len(bps))
	}
}

func TestHTTPSetBreakpointValidation(t *testing.T) {
	_, mux := setupBreakpointTestHandler()

	tests := []struct {
		name string
		body string
		code int
	}{
		{"missing pipeline", `{"step":"s"}`, http.StatusBadRequest},
		{"missing step", `{"pipeline":"p"}`, http.StatusBadRequest},
		{"empty body", `{}`, http.StatusBadRequest},
		{"invalid json", `not json`, http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/v1/debug/breakpoints", bytes.NewBufferString(tt.body))
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tt.code {
				t.Errorf("expected %d, got %d: %s", tt.code, w.Code, w.Body.String())
			}
		})
	}
}

func TestHTTPRemoveBreakpoint(t *testing.T) {
	_, mux := setupBreakpointTestHandler()

	// Set a breakpoint first
	body := `{"pipeline":"p1","step":"s1"}`
	req := httptest.NewRequest("POST", "/api/v1/debug/breakpoints", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// Remove it
	req = httptest.NewRequest("DELETE", "/api/v1/debug/breakpoints/p1/s1", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Remove nonexistent
	req = httptest.NewRequest("DELETE", "/api/v1/debug/breakpoints/p1/s1", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHTTPEnableDisableBreakpoint(t *testing.T) {
	_, mux := setupBreakpointTestHandler()

	// Set a breakpoint
	body := `{"pipeline":"p1","step":"s1"}`
	req := httptest.NewRequest("POST", "/api/v1/debug/breakpoints", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// Disable
	req = httptest.NewRequest("PUT", "/api/v1/debug/breakpoints/p1/s1/disable", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify disabled
	req = httptest.NewRequest("GET", "/api/v1/debug/breakpoints", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	var bps []*PipelineBreakpoint
	_ = json.NewDecoder(w.Body).Decode(&bps)
	if len(bps) != 1 || bps[0].Enabled {
		t.Error("expected breakpoint to be disabled")
	}

	// Enable
	req = httptest.NewRequest("PUT", "/api/v1/debug/breakpoints/p1/s1/enable", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Verify enabled
	req = httptest.NewRequest("GET", "/api/v1/debug/breakpoints", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	bps = nil
	_ = json.NewDecoder(w.Body).Decode(&bps)
	if len(bps) != 1 || !bps[0].Enabled {
		t.Error("expected breakpoint to be enabled")
	}

	// Enable/disable nonexistent
	req = httptest.NewRequest("PUT", "/api/v1/debug/breakpoints/nope/nope/enable", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for enable nonexistent, got %d", w.Code)
	}

	req = httptest.NewRequest("PUT", "/api/v1/debug/breakpoints/nope/nope/disable", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for disable nonexistent, got %d", w.Code)
	}
}

func TestHTTPClearAll(t *testing.T) {
	_, mux := setupBreakpointTestHandler()

	// Set breakpoints
	for _, s := range []string{"s1", "s2", "s3"} {
		body := `{"pipeline":"p","step":"` + s + `"}`
		req := httptest.NewRequest("POST", "/api/v1/debug/breakpoints", bytes.NewBufferString(body))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
	}

	// Clear all
	req := httptest.NewRequest("DELETE", "/api/v1/debug/breakpoints", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Verify empty
	req = httptest.NewRequest("GET", "/api/v1/debug/breakpoints", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	var bps []*PipelineBreakpoint
	_ = json.NewDecoder(w.Body).Decode(&bps)
	if len(bps) != 0 {
		t.Errorf("expected 0 breakpoints after clear, got %d", len(bps))
	}
}

func TestHTTPListPaused(t *testing.T) {
	_, mux := setupBreakpointTestHandler()

	req := httptest.NewRequest("GET", "/api/v1/debug/paused", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var paused []*PausedExecution
	if err := json.NewDecoder(w.Body).Decode(&paused); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if len(paused) != 0 {
		t.Errorf("expected 0 paused, got %d", len(paused))
	}
}

func TestHTTPGetPausedNotFound(t *testing.T) {
	_, mux := setupBreakpointTestHandler()

	req := httptest.NewRequest("GET", "/api/v1/debug/paused/nonexistent", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHTTPPauseAndResume(t *testing.T) {
	m := NewBreakpointManager(slog.Default())
	h := NewBreakpointHandler(m, slog.Default())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Pause an execution in the background
	var wg sync.WaitGroup
	var gotAction ResumeAction

	wg.Add(1)
	go func() {
		defer wg.Done()
		ch := m.Pause("exec-http", "pipeline", "step", 0, map[string]any{"data": "test"})
		gotAction = <-ch
	}()

	// Wait for it to appear
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(m.ListPaused()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Get paused execution via API
	req := httptest.NewRequest("GET", "/api/v1/debug/paused/exec-http", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var pe PausedExecution
	_ = json.NewDecoder(w.Body).Decode(&pe)
	if pe.ID != "exec-http" {
		t.Errorf("expected ID exec-http, got %s", pe.ID)
	}

	// Resume via API
	body := `{"action":"continue","data":{"injected":true}}`
	req = httptest.NewRequest("POST", "/api/v1/debug/paused/exec-http/resume", bytes.NewBufferString(body))
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	wg.Wait()

	if gotAction.Action != "continue" {
		t.Errorf("expected continue, got %s", gotAction.Action)
	}
	if gotAction.Data["injected"] != true {
		t.Error("expected injected data")
	}
}

func TestHTTPResumeValidation(t *testing.T) {
	_, mux := setupBreakpointTestHandler()

	tests := []struct {
		name string
		body string
		code int
	}{
		{"invalid action", `{"action":"invalid"}`, http.StatusBadRequest},
		{"empty action", `{"action":""}`, http.StatusBadRequest},
		{"invalid json", `not json`, http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/v1/debug/paused/some-id/resume", bytes.NewBufferString(tt.body))
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != tt.code {
				t.Errorf("expected %d, got %d: %s", tt.code, w.Code, w.Body.String())
			}
		})
	}
}

func TestHTTPResumeNotFound(t *testing.T) {
	_, mux := setupBreakpointTestHandler()

	body := `{"action":"continue"}`
	req := httptest.NewRequest("POST", "/api/v1/debug/paused/nonexistent/resume", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHTTPFullWorkflow(t *testing.T) {
	// End-to-end test: set breakpoint, pause execution, inspect, resume
	m := NewBreakpointManager(slog.Default())
	h := NewBreakpointHandler(m, slog.Default())
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// 1. Set a breakpoint via API
	body := `{"pipeline":"order-pipeline","step":"process"}`
	req := httptest.NewRequest("POST", "/api/v1/debug/breakpoints", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("set breakpoint: expected 201, got %d", w.Code)
	}

	// 2. Simulate pipeline checking the breakpoint
	if !m.CheckBreakpoint("order-pipeline", "process", nil) {
		t.Fatal("expected breakpoint to fire")
	}

	// 3. Pipeline pauses (simulated in goroutine)
	var wg sync.WaitGroup
	var action ResumeAction

	wg.Add(1)
	go func() {
		defer wg.Done()
		action, _ = m.WaitForResume("exec-full", "order-pipeline", "process", 1, map[string]any{"order": "xyz"})
	}()

	// Wait for it to appear
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(m.ListPaused()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// 4. List paused via API
	req = httptest.NewRequest("GET", "/api/v1/debug/paused", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	var paused []*PausedExecution
	_ = json.NewDecoder(w.Body).Decode(&paused)
	if len(paused) != 1 {
		t.Fatalf("expected 1 paused, got %d", len(paused))
	}

	// 5. Inspect paused execution via API
	req = httptest.NewRequest("GET", "/api/v1/debug/paused/exec-full", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get paused: expected 200, got %d", w.Code)
	}

	// 6. Resume via API with skip action
	body = `{"action":"skip"}`
	req = httptest.NewRequest("POST", "/api/v1/debug/paused/exec-full/resume", bytes.NewBufferString(body))
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("resume: expected 200, got %d", w.Code)
	}

	wg.Wait()

	if action.Action != "skip" {
		t.Errorf("expected skip action, got %s", action.Action)
	}

	// 7. Verify hit count increased
	req = httptest.NewRequest("GET", "/api/v1/debug/breakpoints", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	var bps []*PipelineBreakpoint
	_ = json.NewDecoder(w.Body).Decode(&bps)
	if len(bps) != 1 {
		t.Fatalf("expected 1 breakpoint, got %d", len(bps))
	}
	if bps[0].HitCount != 1 {
		t.Errorf("expected hit count 1, got %d", bps[0].HitCount)
	}
}
