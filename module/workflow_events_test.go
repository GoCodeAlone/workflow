package module

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/CrisisTextLine/modular/modules/eventbus/v2"
)

// --- helpers -----------------------------------------------------------------

// setupEventBus creates a real EventBusModule backed by the in-memory engine,
// starts it, and registers it with a MockApplication. The caller should defer
// the returned cleanup function.
func setupEventBus(t *testing.T) (*MockApplication, *eventbus.EventBusModule, func()) {
	t.Helper()

	app := NewMockApplication()

	// Build a real EventBusModule with in-memory engine.
	ebMod := eventbus.NewModule()
	ebModule := ebMod.(*eventbus.EventBusModule)

	// Register default config section expected by EventBusModule.Init.
	defaultCfg := &eventbus.EventBusConfig{
		Engine:                 "memory",
		MaxEventQueueSize:      1000,
		DefaultEventBufferSize: 10,
		WorkerCount:            5,
		EventTTL:               3600 * time.Second,
		RetentionDays:          7,
	}

	// Create a real lightweight app for the eventbus module lifecycle.
	logger := &MockLogger{}
	realApp := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)
	realApp.RegisterConfigSection("eventbus", modular.NewStdConfigProvider(defaultCfg))

	if err := ebModule.Init(realApp); err != nil {
		t.Fatalf("eventbus Init: %v", err)
	}
	ctx := context.Background()
	if err := ebModule.Start(ctx); err != nil {
		t.Fatalf("eventbus Start: %v", err)
	}

	// Register the running module as a service in our mock app.
	if err := app.RegisterService("eventbus.provider", ebModule); err != nil {
		t.Fatalf("RegisterService: %v", err)
	}

	cleanup := func() {
		_ = ebModule.Stop(ctx)
	}

	return app, ebModule, cleanup
}

// collected is a thread-safe collector for events received via subscriptions.
type collected struct {
	mu     sync.Mutex
	events []eventbus.Event
}

func (c *collected) handler(_ context.Context, ev eventbus.Event) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, ev)
	return nil
}

func (c *collected) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.events)
}

// --- WS2 tests ---------------------------------------------------------------

func TestWorkflowTopic(t *testing.T) {
	tests := []struct {
		wfType, lifecycle, want string
	}{
		{"order", "started", "workflow.order.started"},
		{"user-onboard", "completed", "workflow.user-onboard.completed"},
		{"report", "failed", "workflow.report.failed"},
	}
	for _, tc := range tests {
		got := WorkflowTopic(tc.wfType, tc.lifecycle)
		if got != tc.want {
			t.Errorf("WorkflowTopic(%q, %q) = %q; want %q", tc.wfType, tc.lifecycle, got, tc.want)
		}
	}
}

func TestStepTopic(t *testing.T) {
	tests := []struct {
		wfType, step, lifecycle, want string
	}{
		{"order", "validate", "started", "workflow.order.step.validate.started"},
		{"user-onboard", "send-email", "completed", "workflow.user-onboard.step.send-email.completed"},
		{"report", "generate-pdf", "failed", "workflow.report.step.generate-pdf.failed"},
	}
	for _, tc := range tests {
		got := StepTopic(tc.wfType, tc.step, tc.lifecycle)
		if got != tc.want {
			t.Errorf("StepTopic(%q, %q, %q) = %q; want %q", tc.wfType, tc.step, tc.lifecycle, got, tc.want)
		}
	}
}

func TestWorkflowLifecycleEvent_JSON(t *testing.T) {
	ev := WorkflowLifecycleEvent{
		WorkflowType: "order",
		Action:       "create",
		Status:       LifecycleStarted,
		Timestamp:    time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Data:         map[string]any{"key": "value"},
	}
	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded WorkflowLifecycleEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.WorkflowType != ev.WorkflowType {
		t.Errorf("WorkflowType mismatch: %q vs %q", decoded.WorkflowType, ev.WorkflowType)
	}
	if decoded.Status != ev.Status {
		t.Errorf("Status mismatch: %q vs %q", decoded.Status, ev.Status)
	}
}

func TestStepLifecycleEvent_JSON(t *testing.T) {
	ev := StepLifecycleEvent{
		WorkflowType: "order",
		StepName:     "validate",
		Connector:    "http",
		Action:       "post",
		Status:       LifecycleCompleted,
		Timestamp:    time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Duration:     500 * time.Millisecond,
		Results:      map[string]any{"ok": true},
	}
	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded StepLifecycleEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.StepName != ev.StepName {
		t.Errorf("StepName mismatch: %q vs %q", decoded.StepName, ev.StepName)
	}
	if decoded.Duration != ev.Duration {
		t.Errorf("Duration mismatch: %v vs %v", decoded.Duration, ev.Duration)
	}
}

func TestEmitter_NilEventBus_NoPanic(t *testing.T) {
	// An emitter without an EventBus should silently no-op.
	app := NewMockApplication() // no eventbus.provider registered
	emitter := NewWorkflowEventEmitter(app)

	ctx := context.Background()
	// None of these should panic.
	emitter.EmitWorkflowStarted(ctx, "wf", "act", nil)
	emitter.EmitWorkflowCompleted(ctx, "wf", "act", time.Second, nil)
	emitter.EmitWorkflowFailed(ctx, "wf", "act", time.Second, errors.New("boom"))
	emitter.EmitStepStarted(ctx, "wf", "step", "conn", "act")
	emitter.EmitStepCompleted(ctx, "wf", "step", "conn", "act", time.Second, nil)
	emitter.EmitStepFailed(ctx, "wf", "step", "conn", "act", time.Second, errors.New("oops"))
}

func TestEmitter_WorkflowStarted(t *testing.T) {
	app, eb, cleanup := setupEventBus(t)
	defer cleanup()

	col := &collected{}
	ctx := context.Background()
	sub, err := eb.Subscribe(ctx, WorkflowTopic("order", LifecycleStarted), col.handler)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer func() { _ = sub.Cancel() }()

	emitter := NewWorkflowEventEmitter(app)
	emitter.EmitWorkflowStarted(ctx, "order", "create", map[string]any{"item": "widget"})

	// Give synchronous delivery a moment.
	time.Sleep(50 * time.Millisecond)

	if col.len() != 1 {
		t.Fatalf("expected 1 event, got %d", col.len())
	}
}

func TestEmitter_WorkflowCompleted(t *testing.T) {
	app, eb, cleanup := setupEventBus(t)
	defer cleanup()

	col := &collected{}
	ctx := context.Background()
	sub, err := eb.Subscribe(ctx, WorkflowTopic("order", LifecycleCompleted), col.handler)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer func() { _ = sub.Cancel() }()

	emitter := NewWorkflowEventEmitter(app)
	emitter.EmitWorkflowCompleted(ctx, "order", "create", 2*time.Second, map[string]any{"count": 5})

	time.Sleep(50 * time.Millisecond)

	if col.len() != 1 {
		t.Fatalf("expected 1 event, got %d", col.len())
	}
}

func TestEmitter_WorkflowFailed(t *testing.T) {
	app, eb, cleanup := setupEventBus(t)
	defer cleanup()

	col := &collected{}
	ctx := context.Background()
	sub, err := eb.Subscribe(ctx, WorkflowTopic("order", LifecycleFailed), col.handler)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer func() { _ = sub.Cancel() }()

	emitter := NewWorkflowEventEmitter(app)
	emitter.EmitWorkflowFailed(ctx, "order", "create", time.Second, errors.New("timeout"))

	time.Sleep(50 * time.Millisecond)

	if col.len() != 1 {
		t.Fatalf("expected 1 event, got %d", col.len())
	}
}

func TestEmitter_StepStarted(t *testing.T) {
	app, eb, cleanup := setupEventBus(t)
	defer cleanup()

	col := &collected{}
	ctx := context.Background()
	sub, err := eb.Subscribe(ctx, StepTopic("order", "validate", LifecycleStarted), col.handler)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer func() { _ = sub.Cancel() }()

	emitter := NewWorkflowEventEmitter(app)
	emitter.EmitStepStarted(ctx, "order", "validate", "http", "post")

	time.Sleep(50 * time.Millisecond)

	if col.len() != 1 {
		t.Fatalf("expected 1 event, got %d", col.len())
	}
}

func TestEmitter_StepCompleted(t *testing.T) {
	app, eb, cleanup := setupEventBus(t)
	defer cleanup()

	col := &collected{}
	ctx := context.Background()
	sub, err := eb.Subscribe(ctx, StepTopic("order", "validate", LifecycleCompleted), col.handler)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer func() { _ = sub.Cancel() }()

	emitter := NewWorkflowEventEmitter(app)
	emitter.EmitStepCompleted(ctx, "order", "validate", "http", "post", 100*time.Millisecond, map[string]any{"valid": true})

	time.Sleep(50 * time.Millisecond)

	if col.len() != 1 {
		t.Fatalf("expected 1 event, got %d", col.len())
	}
}

func TestEmitter_StepFailed(t *testing.T) {
	app, eb, cleanup := setupEventBus(t)
	defer cleanup()

	col := &collected{}
	ctx := context.Background()
	sub, err := eb.Subscribe(ctx, StepTopic("order", "validate", LifecycleFailed), col.handler)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer func() { _ = sub.Cancel() }()

	emitter := NewWorkflowEventEmitter(app)
	emitter.EmitStepFailed(ctx, "order", "validate", "http", "post", 100*time.Millisecond, errors.New("bad request"))

	time.Sleep(50 * time.Millisecond)

	if col.len() != 1 {
		t.Fatalf("expected 1 event, got %d", col.len())
	}
}
