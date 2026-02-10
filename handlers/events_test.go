package handlers

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/GoCodeAlone/workflow/module"
)

func TestNewEventWorkflowHandler(t *testing.T) {
	h := NewEventWorkflowHandler()
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
}

func TestEventWorkflowHandler_CanHandle(t *testing.T) {
	h := NewEventWorkflowHandler()
	if !h.CanHandle("event") {
		t.Error("expected CanHandle('event') to be true")
	}
	if h.CanHandle("http") {
		t.Error("expected CanHandle('http') to be false")
	}
}

func TestEventWorkflowHandler_ConfigureWorkflow_InvalidFormat(t *testing.T) {
	h := NewEventWorkflowHandler()
	app := CreateMockApplication()
	err := h.ConfigureWorkflow(app, "invalid")
	if err == nil {
		t.Fatal("expected error for invalid format")
	}
}

func TestEventWorkflowHandler_ConfigureWorkflow_NoProcessor(t *testing.T) {
	h := NewEventWorkflowHandler()
	app := CreateMockApplication()
	err := h.ConfigureWorkflow(app, map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error for missing processor")
	}
}

func TestEventWorkflowHandler_ConfigureWorkflow_ProcessorNotFound(t *testing.T) {
	h := NewEventWorkflowHandler()
	app := CreateMockApplication()
	err := h.ConfigureWorkflow(app, map[string]interface{}{
		"processor": "my-processor",
	})
	if err == nil {
		t.Fatal("expected error for processor not found")
	}
}

func TestEventWorkflowHandler_ExecuteWorkflow_NoAppContext(t *testing.T) {
	h := NewEventWorkflowHandler()
	ctx := context.Background()
	_, err := h.ExecuteWorkflow(ctx, "event", "processor", map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error for missing application context")
	}
}

func TestEventWorkflowHandler_ExecuteWorkflow_ProcessorNotFound(t *testing.T) {
	h := NewEventWorkflowHandler()
	app := CreateMockApplication()
	ctx := context.WithValue(context.Background(), applicationContextKey, app)
	_, err := h.ExecuteWorkflow(ctx, "event", "my-processor", map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error for processor not found")
	}
}

func TestEventProcessorAdapter_HandleEvent_Map(t *testing.T) {
	processor := module.NewEventProcessor("test-processor")
	adapter := &EventProcessorAdapter{Processor: processor}

	ctx := context.Background()
	err := adapter.HandleEvent(ctx, map[string]interface{}{
		"eventType": "test.event",
		"sourceId":  "src-1",
		"data":      "value",
	})
	if err != nil {
		t.Fatalf("HandleEvent with map failed: %v", err)
	}
}

func TestEventProcessorAdapter_HandleEvent_Bytes(t *testing.T) {
	processor := module.NewEventProcessor("test-processor")
	adapter := &EventProcessorAdapter{Processor: processor}

	ctx := context.Background()
	payload, _ := json.Marshal(map[string]interface{}{
		"eventType": "test.event",
		"sourceId":  "src-1",
	})
	err := adapter.HandleEvent(ctx, payload)
	if err != nil {
		t.Fatalf("HandleEvent with bytes failed: %v", err)
	}
}

func TestEventProcessorAdapter_HandleEvent_String(t *testing.T) {
	processor := module.NewEventProcessor("test-processor")
	adapter := &EventProcessorAdapter{Processor: processor}

	ctx := context.Background()
	payload := `{"eventType":"test.event","sourceId":"src-1"}`
	err := adapter.HandleEvent(ctx, payload)
	if err != nil {
		t.Fatalf("HandleEvent with string failed: %v", err)
	}
}

func TestEventProcessorAdapter_HandleEvent_InvalidBytes(t *testing.T) {
	processor := module.NewEventProcessor("test-processor")
	adapter := &EventProcessorAdapter{Processor: processor}

	ctx := context.Background()
	err := adapter.HandleEvent(ctx, []byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON bytes")
	}
}

func TestEventProcessorAdapter_HandleEvent_InvalidString(t *testing.T) {
	processor := module.NewEventProcessor("test-processor")
	adapter := &EventProcessorAdapter{Processor: processor}

	ctx := context.Background()
	err := adapter.HandleEvent(ctx, "not json")
	if err == nil {
		t.Fatal("expected error for invalid JSON string")
	}
}

func TestEventProcessorAdapter_HandleEvent_UnsupportedType(t *testing.T) {
	processor := module.NewEventProcessor("test-processor")
	adapter := &EventProcessorAdapter{Processor: processor}

	ctx := context.Background()
	err := adapter.HandleEvent(ctx, 12345)
	if err == nil {
		t.Fatal("expected error for unsupported type")
	}
}

func TestRegisterEventProcessor(t *testing.T) {
	app := CreateMockApplication()
	processor := module.NewEventProcessor("event-proc")

	err := RegisterEventProcessor(app, processor)
	if err != nil {
		t.Fatalf("RegisterEventProcessor failed: %v", err)
	}
}

func TestEventProcessorAdapter_HandleEvent_MapWithUserID(t *testing.T) {
	processor := module.NewEventProcessor("test-processor")
	adapter := &EventProcessorAdapter{Processor: processor}

	ctx := context.Background()
	err := adapter.HandleEvent(ctx, map[string]interface{}{
		"userId": "user-123",
		"data":   "value",
	})
	if err != nil {
		t.Fatalf("HandleEvent with userId failed: %v", err)
	}
}
