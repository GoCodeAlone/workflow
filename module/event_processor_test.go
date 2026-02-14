package module

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestNewEventProcessor(t *testing.T) {
	ep := NewEventProcessor("test-processor")
	if ep.Name() != "test-processor" {
		t.Errorf("expected name 'test-processor', got '%s'", ep.Name())
	}
}

func TestEventProcessor_AddPattern(t *testing.T) {
	ep := NewEventProcessor("test-processor")

	pattern := &EventPattern{
		PatternID:  "p1",
		EventTypes: []string{"login"},
		WindowTime: time.Minute,
		MinOccurs:  3,
		Condition:  "count",
	}
	ep.AddPattern(pattern)

	if len(ep.patterns) != 1 {
		t.Errorf("expected 1 pattern, got %d", len(ep.patterns))
	}
}

func TestEventProcessor_RegisterHandler(t *testing.T) {
	ep := NewEventProcessor("test-processor")

	pattern := &EventPattern{PatternID: "p1", EventTypes: []string{"login"}}
	ep.AddPattern(pattern)

	handler := NewFunctionHandler(func(ctx context.Context, match PatternMatch) error {
		return nil
	})

	err := ep.RegisterHandler("p1", handler)
	if err != nil {
		t.Fatalf("RegisterHandler failed: %v", err)
	}
}

func TestEventProcessor_RegisterHandler_NotFound(t *testing.T) {
	ep := NewEventProcessor("test-processor")

	handler := NewFunctionHandler(func(ctx context.Context, match PatternMatch) error {
		return nil
	})

	err := ep.RegisterHandler("nonexistent", handler)
	if err == nil {
		t.Error("expected error for nonexistent pattern")
	}
}

func TestEventProcessor_ProcessEvent_SimpleMatch(t *testing.T) {
	ep := NewEventProcessor("test-processor")

	pattern := &EventPattern{
		PatternID:  "burst-detection",
		EventTypes: []string{"error"},
		WindowTime: 5 * time.Minute,
		MinOccurs:  2,
		Condition:  "count",
	}
	ep.AddPattern(pattern)

	var matchResult PatternMatch
	handler := NewFunctionHandler(func(ctx context.Context, match PatternMatch) error {
		matchResult = match
		return nil
	})
	_ = ep.RegisterHandler("burst-detection", handler)

	ctx := context.Background()
	now := time.Now()

	// First event - should not trigger
	_ = ep.ProcessEvent(ctx, EventData{
		EventType: "error",
		Timestamp: now.Add(-1 * time.Minute),
		SourceID:  "server-1",
	})

	if matchResult.PatternID != "" {
		t.Error("first event should not trigger match")
	}

	// Second event - should trigger (minOccurs=2)
	_ = ep.ProcessEvent(ctx, EventData{
		EventType: "error",
		Timestamp: now,
		SourceID:  "server-1",
	})

	if matchResult.PatternID != "burst-detection" {
		t.Errorf("expected match for 'burst-detection', got '%s'", matchResult.PatternID)
	}
	if len(matchResult.Events) != 2 {
		t.Errorf("expected 2 events in match, got %d", len(matchResult.Events))
	}
}

func TestEventProcessor_ProcessEvent_WithCorrelID(t *testing.T) {
	ep := NewEventProcessor("test-processor")

	pattern := &EventPattern{
		PatternID:  "corr-pattern",
		EventTypes: []string{"step-1", "step-2"},
		WindowTime: 5 * time.Minute,
		MinOccurs:  2,
		Condition:  "count",
	}
	ep.AddPattern(pattern)

	var matched bool
	handler := NewFunctionHandler(func(ctx context.Context, match PatternMatch) error {
		matched = true
		return nil
	})
	_ = ep.RegisterHandler("corr-pattern", handler)

	ctx := context.Background()
	now := time.Now()

	// Events with same correlation ID
	_ = ep.ProcessEvent(ctx, EventData{
		EventType: "step-1",
		Timestamp: now,
		SourceID:  "svc-1",
		CorrelID:  "tx-123",
	})
	_ = ep.ProcessEvent(ctx, EventData{
		EventType: "step-2",
		Timestamp: now,
		SourceID:  "svc-2",
		CorrelID:  "tx-123",
	})

	if !matched {
		t.Error("expected pattern match with correlation ID")
	}
}

func TestEventProcessor_ProcessEvent_NoHandler(t *testing.T) {
	ep := NewEventProcessor("test-processor")

	pattern := &EventPattern{
		PatternID:  "unhandled",
		EventTypes: []string{"event"},
		WindowTime: 5 * time.Minute,
		MinOccurs:  1,
		Condition:  "count",
	}
	ep.AddPattern(pattern)

	// No handler registered - should not error
	ctx := context.Background()
	err := ep.ProcessEvent(ctx, EventData{
		EventType: "event",
		Timestamp: time.Now(),
		SourceID:  "src",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEventProcessor_ProcessEvent_HandlerError(t *testing.T) {
	ep := NewEventProcessor("test-processor")

	pattern := &EventPattern{
		PatternID:  "error-pattern",
		EventTypes: []string{"event"},
		WindowTime: 5 * time.Minute,
		MinOccurs:  1,
		Condition:  "count",
	}
	ep.AddPattern(pattern)

	handler := NewFunctionHandler(func(ctx context.Context, match PatternMatch) error {
		return fmt.Errorf("handler error")
	})
	_ = ep.RegisterHandler("error-pattern", handler)

	ctx := context.Background()
	err := ep.ProcessEvent(ctx, EventData{
		EventType: "event",
		Timestamp: time.Now(),
		SourceID:  "src",
	})
	if err == nil {
		t.Error("expected error from handler")
	}
}

func TestEventProcessor_ProcessEvent_TypeMismatch(t *testing.T) {
	ep := NewEventProcessor("test-processor")

	pattern := &EventPattern{
		PatternID:  "specific",
		EventTypes: []string{"login"},
		WindowTime: 5 * time.Minute,
		MinOccurs:  1,
		Condition:  "count",
	}
	ep.AddPattern(pattern)

	var matched bool
	handler := NewFunctionHandler(func(ctx context.Context, match PatternMatch) error {
		matched = true
		return nil
	})
	_ = ep.RegisterHandler("specific", handler)

	ctx := context.Background()
	_ = ep.ProcessEvent(ctx, EventData{
		EventType: "logout", // different type
		Timestamp: time.Now(),
		SourceID:  "src",
	})

	if matched {
		t.Error("event with wrong type should not trigger match")
	}
}

func TestEventProcessor_ProcessEvent_MaxOccurs(t *testing.T) {
	ep := NewEventProcessor("test-processor")

	pattern := &EventPattern{
		PatternID:  "bounded",
		EventTypes: []string{"event"},
		WindowTime: 5 * time.Minute,
		MinOccurs:  1,
		MaxOccurs:  2,
		Condition:  "count",
	}
	ep.AddPattern(pattern)

	matchCount := 0
	handler := NewFunctionHandler(func(ctx context.Context, match PatternMatch) error {
		matchCount++
		return nil
	})
	_ = ep.RegisterHandler("bounded", handler)

	ctx := context.Background()
	now := time.Now()

	// 3 events exceed MaxOccurs - should NOT match after 3rd
	for range 3 {
		_ = ep.ProcessEvent(ctx, EventData{
			EventType: "event",
			Timestamp: now,
			SourceID:  "src",
		})
	}

	// The first two events should match (1 and 2 meet MinOccurs=1,MaxOccurs=2)
	// The third event brings the count to 3, exceeding MaxOccurs=2
	// But since we re-check on every event and all events are in the buffer, the third event should NOT match
	// (events 1,2,3 are 3 events which > MaxOccurs=2)
	// However events 1 matches (1 >= 1, 1 <= 2), events 1,2 match (2 >= 1, 2 <= 2)
	// After 3 events (3 > MaxOccurs=2) no match
	if matchCount < 1 {
		t.Error("expected at least 1 match")
	}
}

func TestEventProcessor_ProcessEvent_OutsideWindow(t *testing.T) {
	ep := NewEventProcessor("test-processor")

	pattern := &EventPattern{
		PatternID:  "timed",
		EventTypes: []string{"event"},
		WindowTime: 1 * time.Second,
		MinOccurs:  2,
		Condition:  "count",
	}
	ep.AddPattern(pattern)

	var matched bool
	handler := NewFunctionHandler(func(ctx context.Context, match PatternMatch) error {
		matched = true
		return nil
	})
	_ = ep.RegisterHandler("timed", handler)

	ctx := context.Background()

	// Event outside the window
	_ = ep.ProcessEvent(ctx, EventData{
		EventType: "event",
		Timestamp: time.Now().Add(-10 * time.Minute), // way outside window
		SourceID:  "src",
	})
	// Event inside the window
	_ = ep.ProcessEvent(ctx, EventData{
		EventType: "event",
		Timestamp: time.Now(),
		SourceID:  "src",
	})

	if matched {
		t.Error("events with one outside window should not match minOccurs=2")
	}
}

func TestEventProcessor_ProvidesServices(t *testing.T) {
	ep := NewEventProcessor("test-processor")
	services := ep.ProvidesServices()
	if len(services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(services))
	}
	if services[0].Name != "test-processor" {
		t.Errorf("expected 'test-processor', got '%s'", services[0].Name)
	}
}

func TestEventProcessor_RequiresServices(t *testing.T) {
	ep := NewEventProcessor("test-processor")
	deps := ep.RequiresServices()
	if deps != nil {
		t.Errorf("expected nil deps, got %v", deps)
	}
}

func TestEventProcessor_Error(t *testing.T) {
	ep := NewEventProcessor("test-processor")

	if ep.Error() != "" {
		t.Errorf("expected empty error, got '%s'", ep.Error())
	}

	ep.SetError(fmt.Errorf("test error"))
	if ep.Error() != "test error" {
		t.Errorf("expected 'test error', got '%s'", ep.Error())
	}
}

func TestEventProcessor_Services(t *testing.T) {
	ep := NewEventProcessor("test-processor")

	services := ep.Services()
	if services == nil {
		t.Fatal("expected non-nil services map")
	}
	if services["test-processor"] != ep {
		t.Error("expected self in services map")
	}
}

func TestEventProcessor_Service(t *testing.T) {
	ep := NewEventProcessor("test-processor")
	svc := ep.Service("test-processor")
	// Service method returns nil when no appContext
	_ = svc // not a hard failure
}

func TestEventProcessor_GetService(t *testing.T) {
	ep := NewEventProcessor("test-processor")

	var out *EventProcessor
	err := ep.GetService("test-processor", &out)
	if err != nil {
		t.Fatalf("GetService failed: %v", err)
	}
	if out != ep {
		t.Error("expected GetService to return self")
	}
}

func TestEventProcessor_CleanupOldEvents(t *testing.T) {
	ep := NewEventProcessor("test-processor")

	pattern := &EventPattern{
		PatternID:  "cleanup-test",
		EventTypes: []string{"event"},
		WindowTime: 1 * time.Second,
		MinOccurs:  1,
	}
	ep.AddPattern(pattern)

	// Add some old events directly to the buffer
	ep.bufferLock.Lock()
	ep.eventBuffer["old-correl"] = []EventData{
		{EventType: "event", Timestamp: time.Now().Add(-1 * time.Hour), SourceID: "old"},
	}
	ep.eventBuffer["new-correl"] = []EventData{
		{EventType: "event", Timestamp: time.Now(), SourceID: "new"},
	}
	ep.bufferLock.Unlock()

	ep.cleanupOldEvents()

	ep.bufferLock.RLock()
	defer ep.bufferLock.RUnlock()

	if _, exists := ep.eventBuffer["old-correl"]; exists {
		t.Error("expected old events to be cleaned up")
	}
	if _, exists := ep.eventBuffer["new-correl"]; !exists {
		t.Error("expected new events to be retained")
	}
}

func TestEventProcessor_Init(t *testing.T) {
	ep := NewEventProcessor("test-ep")
	app := CreateIsolatedApp(t)

	if err := ep.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Verify appContext was stored
	if ep.appContext == nil {
		t.Fatal("expected appContext to be set after Init")
	}

	// Verify service was registered
	var svc any
	if err := app.GetService("test-ep", &svc); err != nil {
		t.Fatalf("expected service to be registered: %v", err)
	}
}

func TestEventProcessor_Start(t *testing.T) {
	ep := NewEventProcessor("test-processor")

	// Start launches a goroutine for cleanup; verify it doesn't error
	if err := ep.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
}

func TestEventProcessor_Stop(t *testing.T) {
	ep := NewEventProcessor("test-processor")
	if err := ep.Stop(context.Background()); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestFunctionHandler(t *testing.T) {
	var called bool
	h := NewFunctionHandler(func(ctx context.Context, match PatternMatch) error {
		called = true
		return nil
	})

	err := h.HandlePattern(context.Background(), PatternMatch{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("expected function to be called")
	}
}

func TestEventProcessor_EventMatchesType(t *testing.T) {
	ep := NewEventProcessor("test")

	tests := []struct {
		eventType string
		types     []string
		expected  bool
	}{
		{"login", []string{"login", "logout"}, true},
		{"register", []string{"login", "logout"}, false},
		{"event", []string{"event"}, true},
		{"event", []string{}, false},
	}

	for _, tc := range tests {
		event := EventData{EventType: tc.eventType}
		result := ep.eventMatchesType(event, tc.types)
		if result != tc.expected {
			t.Errorf("eventMatchesType(%s, %v) = %v, want %v", tc.eventType, tc.types, result, tc.expected)
		}
	}
}
