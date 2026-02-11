package module

import (
	"context"
	"sync"
	"testing"
	"time"
)

// --- mock engine for eventbus trigger tests ----------------------------------

type ebTriggerCall struct {
	workflowType string
	action       string
	data         map[string]interface{}
}

type mockEBWorkflowEngine struct {
	mu        sync.Mutex
	triggered []ebTriggerCall
}

func (m *mockEBWorkflowEngine) TriggerWorkflow(_ context.Context, wfType, action string, data map[string]interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.triggered = append(m.triggered, ebTriggerCall{wfType, action, data})
	return nil
}

func (m *mockEBWorkflowEngine) calls() []ebTriggerCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]ebTriggerCall, len(m.triggered))
	copy(out, m.triggered)
	return out
}

// --- tests -------------------------------------------------------------------

func TestEventBusTrigger_Name(t *testing.T) {
	trigger := NewEventBusTrigger()
	if trigger.Name() != EventBusTriggerName {
		t.Errorf("Name() = %q; want %q", trigger.Name(), EventBusTriggerName)
	}
}

func TestEventBusTrigger_NameWithNamespace(t *testing.T) {
	ns := NewStandardNamespace("ns", "")
	trigger := NewEventBusTriggerWithNamespace(ns)
	want := "ns-" + EventBusTriggerName
	if trigger.Name() != want {
		t.Errorf("Name() = %q; want %q", trigger.Name(), want)
	}
}

func TestEventBusTrigger_Configure(t *testing.T) {
	app, eb, cleanup := setupEventBus(t)
	defer cleanup()

	engine := &mockEBWorkflowEngine{}
	if err := app.RegisterService("workflowEngine", engine); err != nil {
		t.Fatalf("RegisterService: %v", err)
	}
	// Also register the eventbus so GetService can find it by pointer.
	_ = eb // already registered by setupEventBus

	trigger := NewEventBusTrigger()

	config := map[string]interface{}{
		"subscriptions": []interface{}{
			map[string]interface{}{
				"topic":    "order.created",
				"workflow": "order-pipeline",
				"action":   "process",
			},
			map[string]interface{}{
				"topic":    "user.events",
				"event":    "user.registered",
				"workflow": "onboard",
				"action":   "start",
				"async":    true,
				"params":   map[string]interface{}{"source": "eventbus"},
			},
		},
	}

	if err := trigger.Configure(app, config); err != nil {
		t.Fatalf("Configure: %v", err)
	}

	if len(trigger.subscriptions) != 2 {
		t.Fatalf("expected 2 subscriptions, got %d", len(trigger.subscriptions))
	}

	// Verify first subscription.
	s0 := trigger.subscriptions[0]
	if s0.Topic != "order.created" || s0.Workflow != "order-pipeline" || s0.Action != "process" {
		t.Errorf("unexpected sub[0]: %+v", s0)
	}
	if s0.Async {
		t.Error("sub[0] should not be async")
	}

	// Verify second subscription.
	s1 := trigger.subscriptions[1]
	if s1.Topic != "user.events" || s1.Event != "user.registered" || !s1.Async {
		t.Errorf("unexpected sub[1]: %+v", s1)
	}
	if s1.Params["source"] != "eventbus" {
		t.Errorf("sub[1] params mismatch: %v", s1.Params)
	}
}

func TestEventBusTrigger_Configure_InvalidFormat(t *testing.T) {
	trigger := NewEventBusTrigger()
	err := trigger.Configure(NewMockApplication(), "bad config")
	if err == nil {
		t.Fatal("expected error for invalid config format")
	}
}

func TestEventBusTrigger_Configure_MissingSubscriptions(t *testing.T) {
	trigger := NewEventBusTrigger()
	err := trigger.Configure(NewMockApplication(), map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error for missing subscriptions")
	}
}

func TestEventBusTrigger_StartAndPublish(t *testing.T) {
	_, eb, cleanup := setupEventBus(t)
	defer cleanup()

	engine := &mockEBWorkflowEngine{}

	trigger := NewEventBusTrigger()
	trigger.subscriptions = []EventBusTriggerSubscription{
		{
			Topic:    "test.topic",
			Workflow: "my-workflow",
			Action:   "run",
		},
	}
	trigger.SetEventBusAndEngine(eb, engine)

	ctx := context.Background()
	if err := trigger.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Publish an event on the topic.
	payload := map[string]interface{}{"key": "value"}
	if err := eb.Publish(ctx, "test.topic", payload); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	calls := engine.calls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 trigger call, got %d", len(calls))
	}
	if calls[0].workflowType != "my-workflow" || calls[0].action != "run" {
		t.Errorf("unexpected call: %+v", calls[0])
	}
	if calls[0].data["key"] != "value" {
		t.Errorf("expected data key=value, got %v", calls[0].data)
	}

	if err := trigger.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestEventBusTrigger_EventTypeFiltering(t *testing.T) {
	_, eb, cleanup := setupEventBus(t)
	defer cleanup()

	engine := &mockEBWorkflowEngine{}

	trigger := NewEventBusTrigger()
	trigger.subscriptions = []EventBusTriggerSubscription{
		{
			Topic:    "events",
			Event:    "order.placed",
			Workflow: "order-wf",
			Action:   "start",
		},
	}
	trigger.SetEventBusAndEngine(eb, engine)

	ctx := context.Background()
	if err := trigger.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Publish non-matching event type.
	if err := eb.Publish(ctx, "events", map[string]interface{}{
		"type": "order.cancelled",
		"id":   "1",
	}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	if len(engine.calls()) != 0 {
		t.Errorf("expected 0 triggers for non-matching event, got %d", len(engine.calls()))
	}

	// Publish matching event type.
	if err := eb.Publish(ctx, "events", map[string]interface{}{
		"type": "order.placed",
		"id":   "2",
	}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	if len(engine.calls()) != 1 {
		t.Fatalf("expected 1 trigger for matching event, got %d", len(engine.calls()))
	}
	if engine.calls()[0].data["id"] != "2" {
		t.Errorf("unexpected data: %v", engine.calls()[0].data)
	}

	_ = trigger.Stop(ctx)
}

func TestEventBusTrigger_EventTypeFilteringWithEventType(t *testing.T) {
	_, eb, cleanup := setupEventBus(t)
	defer cleanup()

	engine := &mockEBWorkflowEngine{}

	trigger := NewEventBusTrigger()
	trigger.subscriptions = []EventBusTriggerSubscription{
		{
			Topic:    "events",
			Event:    "user.login",
			Workflow: "auth-wf",
			Action:   "audit",
		},
	}
	trigger.SetEventBusAndEngine(eb, engine)

	ctx := context.Background()
	if err := trigger.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Publish with "eventType" field instead of "type".
	if err := eb.Publish(ctx, "events", map[string]interface{}{
		"eventType": "user.login",
		"user":      "alice",
	}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	calls := engine.calls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 trigger for eventType match, got %d", len(calls))
	}
	if calls[0].data["user"] != "alice" {
		t.Errorf("unexpected data: %v", calls[0].data)
	}

	_ = trigger.Stop(ctx)
}

func TestEventBusTrigger_ParamsMerged(t *testing.T) {
	_, eb, cleanup := setupEventBus(t)
	defer cleanup()

	engine := &mockEBWorkflowEngine{}

	trigger := NewEventBusTrigger()
	trigger.subscriptions = []EventBusTriggerSubscription{
		{
			Topic:    "test.params",
			Workflow: "wf",
			Action:   "act",
			Params:   map[string]interface{}{"env": "production", "source": "trigger"},
		},
	}
	trigger.SetEventBusAndEngine(eb, engine)

	ctx := context.Background()
	if err := trigger.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := eb.Publish(ctx, "test.params", map[string]interface{}{
		"id": "42",
	}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	calls := engine.calls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	data := calls[0].data
	if data["env"] != "production" {
		t.Errorf("expected env=production, got %v", data["env"])
	}
	if data["source"] != "trigger" {
		t.Errorf("expected source=trigger, got %v", data["source"])
	}
	if data["id"] != "42" {
		t.Errorf("expected id=42, got %v", data["id"])
	}

	_ = trigger.Stop(ctx)
}

func TestEventBusTrigger_AsyncSubscription(t *testing.T) {
	_, eb, cleanup := setupEventBus(t)
	defer cleanup()

	engine := &mockEBWorkflowEngine{}

	trigger := NewEventBusTrigger()
	trigger.subscriptions = []EventBusTriggerSubscription{
		{
			Topic:    "async.topic",
			Workflow: "async-wf",
			Action:   "go",
			Async:    true,
		},
	}
	trigger.SetEventBusAndEngine(eb, engine)

	ctx := context.Background()
	if err := trigger.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := eb.Publish(ctx, "async.topic", map[string]interface{}{
		"msg": "hello",
	}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// Async processing may take a moment.
	time.Sleep(200 * time.Millisecond)

	calls := engine.calls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 async trigger call, got %d", len(calls))
	}
	if calls[0].data["msg"] != "hello" {
		t.Errorf("unexpected data: %v", calls[0].data)
	}

	_ = trigger.Stop(ctx)
}

func TestEventBusTrigger_StopCancelsSubscriptions(t *testing.T) {
	_, eb, cleanup := setupEventBus(t)
	defer cleanup()

	engine := &mockEBWorkflowEngine{}

	trigger := NewEventBusTrigger()
	trigger.subscriptions = []EventBusTriggerSubscription{
		{Topic: "stop.test", Workflow: "wf", Action: "act"},
	}
	trigger.SetEventBusAndEngine(eb, engine)

	ctx := context.Background()
	if err := trigger.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if len(trigger.activeSubs) != 1 {
		t.Fatalf("expected 1 active sub, got %d", len(trigger.activeSubs))
	}

	if err := trigger.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if len(trigger.activeSubs) != 0 {
		t.Errorf("expected 0 active subs after Stop, got %d", len(trigger.activeSubs))
	}

	// Publishing after stop should not trigger workflow (subscription was cancelled).
	if err := eb.Publish(ctx, "stop.test", map[string]interface{}{"x": 1}); err != nil {
		t.Fatalf("Publish after stop: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	if len(engine.calls()) != 0 {
		t.Errorf("expected 0 calls after Stop, got %d", len(engine.calls()))
	}
}

func TestEventBusTrigger_Init(t *testing.T) {
	app := NewMockApplication()
	trigger := NewEventBusTrigger()

	if err := trigger.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}

	if _, exists := app.Services[EventBusTriggerName]; !exists {
		t.Error("trigger not registered in service registry")
	}
}

func TestEventBusTrigger_StartWithoutEventBus(t *testing.T) {
	trigger := NewEventBusTrigger()
	trigger.engine = &mockEBWorkflowEngine{}
	trigger.subscriptions = []EventBusTriggerSubscription{{Topic: "test"}}
	err := trigger.Start(context.Background())
	if err == nil {
		t.Fatal("expected error when starting without eventbus")
	}
}

func TestEventBusTrigger_StartWithoutEngine(t *testing.T) {
	_, eb, cleanup := setupEventBus(t)
	defer cleanup()

	trigger := NewEventBusTrigger()
	trigger.eventBus = eb
	trigger.subscriptions = []EventBusTriggerSubscription{{Topic: "test"}}
	err := trigger.Start(context.Background())
	if err == nil {
		t.Fatal("expected error when starting without engine")
	}
}

func TestEventBusTrigger_Configure_Incomplete(t *testing.T) {
	app, _, cleanup := setupEventBus(t)
	defer cleanup()

	engine := &mockEBWorkflowEngine{}
	if err := app.RegisterService("workflowEngine", engine); err != nil {
		t.Fatalf("RegisterService: %v", err)
	}

	trigger := NewEventBusTrigger()

	// Subscription with empty topic should fail
	config := map[string]interface{}{
		"subscriptions": []interface{}{
			map[string]interface{}{
				"topic":    "",
				"workflow": "wf",
				"action":   "act",
			},
		},
	}

	err := trigger.Configure(app, config)
	if err == nil {
		t.Fatal("expected error for incomplete subscription (empty topic)")
	}
}

func TestEventBusTrigger_Configure_InvalidEntry(t *testing.T) {
	app, _, cleanup := setupEventBus(t)
	defer cleanup()

	engine := &mockEBWorkflowEngine{}
	if err := app.RegisterService("workflowEngine", engine); err != nil {
		t.Fatalf("RegisterService: %v", err)
	}

	trigger := NewEventBusTrigger()

	// Non-map subscription entry
	config := map[string]interface{}{
		"subscriptions": []interface{}{
			"not a map",
		},
	}

	err := trigger.Configure(app, config)
	if err == nil {
		t.Fatal("expected error for invalid subscription entry (non-map)")
	}
}

func TestEventBusTrigger_Configure_NoEventBus(t *testing.T) {
	app := NewMockApplication()

	trigger := NewEventBusTrigger()

	config := map[string]interface{}{
		"subscriptions": []interface{}{
			map[string]interface{}{
				"topic":    "t",
				"workflow": "wf",
				"action":   "act",
			},
		},
	}

	err := trigger.Configure(app, config)
	if err == nil {
		t.Fatal("expected error when eventbus.provider service is missing")
	}
}

func TestEventBusTrigger_Configure_NoEngine(t *testing.T) {
	app, _, cleanup := setupEventBus(t)
	defer cleanup()

	trigger := NewEventBusTrigger()

	config := map[string]interface{}{
		"subscriptions": []interface{}{
			map[string]interface{}{
				"topic":    "t",
				"workflow": "wf",
				"action":   "act",
			},
		},
	}

	err := trigger.Configure(app, config)
	if err == nil {
		t.Fatal("expected error when workflowEngine service is missing")
	}
}
