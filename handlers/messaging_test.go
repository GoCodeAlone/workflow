package handlers

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/CrisisTextLine/modular"
	workflowmodule "github.com/GoCodeAlone/workflow/module"
)

func TestNewMessagingWorkflowHandler(t *testing.T) {
	h := NewMessagingWorkflowHandler()
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
	if h.Name() != MessagingWorkflowHandlerName {
		t.Errorf("expected name '%s', got '%s'", MessagingWorkflowHandlerName, h.Name())
	}
}

func TestNewMessagingWorkflowHandlerWithNamespace(t *testing.T) {
	ns := workflowmodule.NewStandardNamespace("app", "")
	h := NewMessagingWorkflowHandlerWithNamespace(ns)
	expected := "app-" + MessagingWorkflowHandlerName
	if h.Name() != expected {
		t.Errorf("expected name '%s', got '%s'", expected, h.Name())
	}
}

func TestMessagingWorkflowHandler_CanHandle(t *testing.T) {
	h := NewMessagingWorkflowHandler()

	if !h.CanHandle("messaging") {
		t.Error("expected CanHandle('messaging') = true")
	}
	if h.CanHandle("http") {
		t.Error("expected CanHandle('http') = false")
	}
	if h.CanHandle("") {
		t.Error("expected CanHandle('') = false")
	}
}

func TestMessagingWorkflowHandler_ConfigureWorkflow_InvalidFormat(t *testing.T) {
	h := NewMessagingWorkflowHandler()
	app := CreateMockApplication()

	err := h.ConfigureWorkflow(app, "not a map")
	if err == nil {
		t.Error("expected error for invalid config format")
	}
}

func TestMessagingWorkflowHandler_ConfigureWorkflow_NoBroker(t *testing.T) {
	h := NewMessagingWorkflowHandler()
	app := CreateMockApplication()

	config := map[string]interface{}{
		"subscriptions": []interface{}{
			map[string]interface{}{
				"topic":   "events",
				"handler": "myhandler",
			},
		},
	}
	err := h.ConfigureWorkflow(app, config)
	if err == nil {
		t.Error("expected error for missing broker")
	}
}

func TestMessagingWorkflowHandler_ConfigureWorkflow_NoSubscriptions(t *testing.T) {
	h := NewMessagingWorkflowHandler()
	app := NewTestServiceRegistry()

	broker := workflowmodule.NewInMemoryMessageBroker("test-broker")
	app.services["test-broker"] = broker

	config := map[string]interface{}{
		"noSubscriptions": true,
	}
	err := h.ConfigureWorkflow(app, config)
	if err == nil {
		t.Error("expected error for missing subscriptions")
	}
}

func TestMessagingWorkflowHandler_ConfigureWorkflow_InvalidSubscription(t *testing.T) {
	h := NewMessagingWorkflowHandler()
	app := NewTestServiceRegistry()

	broker := workflowmodule.NewInMemoryMessageBroker("test-broker")
	app.services["test-broker"] = broker

	config := map[string]interface{}{
		"subscriptions": []interface{}{
			"not a map",
		},
	}
	err := h.ConfigureWorkflow(app, config)
	if err == nil {
		t.Error("expected error for invalid subscription")
	}
}

func TestMessagingWorkflowHandler_ConfigureWorkflow_IncompleteSubscription(t *testing.T) {
	h := NewMessagingWorkflowHandler()
	app := NewTestServiceRegistry()

	broker := workflowmodule.NewInMemoryMessageBroker("test-broker")
	app.services["test-broker"] = broker

	config := map[string]interface{}{
		"subscriptions": []interface{}{
			map[string]interface{}{
				"topic": "events",
				// missing handler
			},
		},
	}
	err := h.ConfigureWorkflow(app, config)
	if err == nil {
		t.Error("expected error for incomplete subscription")
	}
}

func TestMessagingWorkflowHandler_ExecuteWorkflow_NoApp(t *testing.T) {
	h := NewMessagingWorkflowHandler()

	_, err := h.ExecuteWorkflow(context.Background(), "messaging", "topic", nil)
	if err == nil {
		t.Error("expected error for missing application context")
	}
}

func TestMessagingWorkflowHandler_ExecuteWorkflow_NoBroker(t *testing.T) {
	h := NewMessagingWorkflowHandler()
	app := NewTestServiceRegistry()
	ctx := context.WithValue(context.Background(), applicationContextKey, app)

	_, err := h.ExecuteWorkflow(ctx, "messaging", "topic", nil)
	if err == nil {
		t.Error("expected error for missing broker")
	}
}

func TestMessagingWorkflowHandler_ExecuteWorkflow_EmptyTopic(t *testing.T) {
	h := NewMessagingWorkflowHandler()
	app := NewTestServiceRegistry()

	broker := workflowmodule.NewInMemoryMessageBroker("test-broker")
	app.services["test-broker"] = broker

	ctx := context.WithValue(context.Background(), applicationContextKey, app)

	_, err := h.ExecuteWorkflow(ctx, "messaging", "", nil)
	if err == nil {
		t.Error("expected error for empty topic")
	}
}

func TestMessagingWorkflowHandler_ExecuteWorkflow_SendMessage(t *testing.T) {
	h := NewMessagingWorkflowHandler()
	app := NewTestServiceRegistry()

	broker := workflowmodule.NewInMemoryMessageBroker("test-broker")
	brokerApp := createMinimalBrokerApp(t)
	_ = broker.Init(brokerApp)
	app.services["test-broker"] = broker

	// Subscribe a handler to capture the message
	var received []byte
	handler := &testMsgHandler{
		handleFunc: func(msg []byte) error {
			received = msg
			return nil
		},
	}
	_ = broker.Subscribe("test-topic", handler)

	ctx := context.WithValue(context.Background(), applicationContextKey, app)

	data := map[string]interface{}{
		"message": "hello world",
	}

	result, err := h.ExecuteWorkflow(ctx, "messaging", "test-topic", data)
	if err != nil {
		t.Fatalf("ExecuteWorkflow failed: %v", err)
	}
	if result["success"] != true {
		t.Errorf("expected success=true, got %v", result["success"])
	}
	if result["topic"] != "test-topic" {
		t.Errorf("expected topic 'test-topic', got '%v'", result["topic"])
	}
	if received == nil {
		t.Error("expected message to be received by subscriber")
	}
	if string(received) != "hello world" {
		t.Errorf("expected 'hello world', got '%s'", string(received))
	}
}

func TestMessagingWorkflowHandler_ExecuteWorkflow_SendJSONMessage(t *testing.T) {
	h := NewMessagingWorkflowHandler()
	app := NewTestServiceRegistry()

	broker := workflowmodule.NewInMemoryMessageBroker("test-broker")
	brokerApp := createMinimalBrokerApp(t)
	_ = broker.Init(brokerApp)
	app.services["test-broker"] = broker

	var received []byte
	handler := &testMsgHandler{
		handleFunc: func(msg []byte) error {
			received = msg
			return nil
		},
	}
	_ = broker.Subscribe("test-topic", handler)

	ctx := context.WithValue(context.Background(), applicationContextKey, app)

	data := map[string]interface{}{
		"message": map[string]interface{}{
			"key": "value",
		},
	}

	result, err := h.ExecuteWorkflow(ctx, "messaging", "test-topic", data)
	if err != nil {
		t.Fatalf("ExecuteWorkflow failed: %v", err)
	}
	if result["success"] != true {
		t.Errorf("expected success=true, got %v", result["success"])
	}

	if received == nil {
		t.Fatal("expected message")
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(received, &parsed); err != nil {
		t.Fatalf("received non-JSON: %s", string(received))
	}
	if parsed["key"] != "value" {
		t.Errorf("expected key='value', got '%v'", parsed["key"])
	}
}

func TestMessagingWorkflowHandler_ExecuteWorkflow_SendDataAsPayload(t *testing.T) {
	h := NewMessagingWorkflowHandler()
	app := NewTestServiceRegistry()

	broker := workflowmodule.NewInMemoryMessageBroker("test-broker")
	brokerApp := createMinimalBrokerApp(t)
	_ = broker.Init(brokerApp)
	app.services["test-broker"] = broker

	var received []byte
	handler := &testMsgHandler{
		handleFunc: func(msg []byte) error {
			received = msg
			return nil
		},
	}
	_ = broker.Subscribe("test-topic", handler)

	ctx := context.WithValue(context.Background(), applicationContextKey, app)

	// No "message" field - should use entire data as payload
	data := map[string]interface{}{
		"order_id": "123",
		"status":   "created",
	}

	_, err := h.ExecuteWorkflow(ctx, "messaging", "test-topic", data)
	if err != nil {
		t.Fatalf("ExecuteWorkflow failed: %v", err)
	}

	if received == nil {
		t.Fatal("expected message")
	}
	var parsed map[string]interface{}
	json.Unmarshal(received, &parsed)
	if parsed["order_id"] != "123" {
		t.Errorf("expected order_id='123', got '%v'", parsed["order_id"])
	}
}

func TestMessagingWorkflowHandler_ExecuteWorkflow_BrokerTopicFormat(t *testing.T) {
	h := NewMessagingWorkflowHandler()
	app := NewTestServiceRegistry()

	broker := workflowmodule.NewInMemoryMessageBroker("mybroker")
	brokerApp := createMinimalBrokerApp(t)
	_ = broker.Init(brokerApp)
	app.services["mybroker"] = broker

	var received []byte
	handler := &testMsgHandler{
		handleFunc: func(msg []byte) error {
			received = msg
			return nil
		},
	}
	_ = broker.Subscribe("events", handler)

	ctx := context.WithValue(context.Background(), applicationContextKey, app)

	// Use "broker:topic" format in action
	data := map[string]interface{}{"message": "test"}
	result, err := h.ExecuteWorkflow(ctx, "messaging", "mybroker:events", data)
	if err != nil {
		t.Fatalf("ExecuteWorkflow failed: %v", err)
	}
	if result["topic"] != "events" {
		t.Errorf("expected topic 'events', got '%v'", result["topic"])
	}
	if received == nil {
		t.Error("expected message to be received")
	}
}

func TestMessagingWorkflowHandler_ExecuteWorkflow_ByteMessage(t *testing.T) {
	h := NewMessagingWorkflowHandler()
	app := NewTestServiceRegistry()

	broker := workflowmodule.NewInMemoryMessageBroker("test-broker")
	brokerApp := createMinimalBrokerApp(t)
	_ = broker.Init(brokerApp)
	app.services["test-broker"] = broker

	var received []byte
	handler := &testMsgHandler{
		handleFunc: func(msg []byte) error {
			received = msg
			return nil
		},
	}
	_ = broker.Subscribe("test-topic", handler)

	ctx := context.WithValue(context.Background(), applicationContextKey, app)

	data := map[string]interface{}{
		"message": []byte("byte payload"),
	}

	_, err := h.ExecuteWorkflow(ctx, "messaging", "test-topic", data)
	if err != nil {
		t.Fatalf("ExecuteWorkflow failed: %v", err)
	}
	if string(received) != "byte payload" {
		t.Errorf("expected 'byte payload', got '%s'", string(received))
	}
}

// testMsgHandler is a simple message handler for testing
type testMsgHandler struct {
	handleFunc func(msg []byte) error
}

func (h *testMsgHandler) HandleMessage(message []byte) error {
	return h.handleFunc(message)
}

// createMinimalBrokerApp creates a minimal app for broker Init
func createMinimalBrokerApp(t *testing.T) modular.Application {
	t.Helper()
	logger := &testBrokerLogger{}
	app := modular.NewStdApplication(modular.NewStdConfigProvider(nil), logger)
	return app
}

type testBrokerLogger struct{}

func (l *testBrokerLogger) Debug(msg string, args ...interface{}) {}
func (l *testBrokerLogger) Info(msg string, args ...interface{})  {}
func (l *testBrokerLogger) Warn(msg string, args ...interface{})  {}
func (l *testBrokerLogger) Error(msg string, args ...interface{}) {}
func (l *testBrokerLogger) Fatal(msg string, args ...interface{}) {}
