package module

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// mockBrokerProducer is a simple in-memory MessageProducer for testing.
type mockBrokerProducer struct {
	published []struct {
		topic   string
		message []byte
	}
	sendErr error
}

func (p *mockBrokerProducer) SendMessage(topic string, message []byte) error {
	if p.sendErr != nil {
		return p.sendErr
	}
	p.published = append(p.published, struct {
		topic   string
		message []byte
	}{topic, message})
	return nil
}

// mockBroker is a minimal MessageBroker for testing event_publish steps.
type mockBroker struct {
	producer *mockBrokerProducer
}

func newMockBroker() *mockBroker {
	return &mockBroker{producer: &mockBrokerProducer{}}
}

func (b *mockBroker) Producer() MessageProducer                  { return b.producer }
func (b *mockBroker) Consumer() MessageConsumer                  { return nil }
func (b *mockBroker) Subscribe(_ string, _ MessageHandler) error { return nil }
func (b *mockBroker) Start(_ context.Context) error              { return nil }
func (b *mockBroker) Stop(_ context.Context) error               { return nil }

func mockAppWithBroker(name string, broker MessageBroker) *MockApplication {
	app := NewMockApplication()
	app.Services[name] = broker
	return app
}

func TestEventPublishStep_BasicPublish(t *testing.T) {
	broker := newMockBroker()
	app := mockAppWithBroker("my-broker", broker)

	factory := NewEventPublishStepFactory()
	step, err := factory("publish-event", map[string]any{
		"topic":  "orders.created",
		"broker": "my-broker",
		"payload": map[string]any{
			"order_id": "ORD-1",
			"status":   "created",
		},
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["published"] != true {
		t.Errorf("expected published=true, got %v", result.Output["published"])
	}
	if result.Output["topic"] != "orders.created" {
		t.Errorf("expected topic=orders.created, got %v", result.Output["topic"])
	}
	if len(broker.producer.published) != 1 {
		t.Fatalf("expected 1 published message, got %d", len(broker.producer.published))
	}

	var payload map[string]any
	if err := json.Unmarshal(broker.producer.published[0].message, &payload); err != nil {
		t.Fatalf("failed to unmarshal published message: %v", err)
	}
	if payload["order_id"] != "ORD-1" {
		t.Errorf("expected order_id=ORD-1, got %v", payload["order_id"])
	}
}

func TestEventPublishStep_TemplateEvaluation(t *testing.T) {
	broker := newMockBroker()
	app := mockAppWithBroker("bus", broker)

	factory := NewEventPublishStepFactory()
	step, err := factory("pub-tmpl", map[string]any{
		"topic":  "users.{{ .user_id }}.events",
		"broker": "bus",
		"payload": map[string]any{
			"id":   "{{ .user_id }}",
			"name": "{{ .name }}",
		},
		"headers": map[string]any{
			"x-source": "pipeline-{{ .user_id }}",
		},
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"user_id": "42",
		"name":    "Alice",
	}, nil)

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["published"] != true {
		t.Errorf("expected published=true")
	}
	if result.Output["topic"] != "users.42.events" {
		t.Errorf("expected resolved topic, got %v", result.Output["topic"])
	}

	// Message should contain headers envelope since headers were set
	var envelope map[string]any
	if err := json.Unmarshal(broker.producer.published[0].message, &envelope); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	headers, ok := envelope["headers"].(map[string]any)
	if !ok {
		t.Fatal("expected headers in envelope")
	}
	if headers["x-source"] != "pipeline-42" {
		t.Errorf("expected x-source=pipeline-42, got %v", headers["x-source"])
	}
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatal("expected data in envelope")
	}
	if data["id"] != "42" {
		t.Errorf("expected id=42, got %v", data["id"])
	}
}

func TestEventPublishStep_MissingBrokerError(t *testing.T) {
	app := NewMockApplication() // empty app, no broker registered

	factory := NewEventPublishStepFactory()
	step, err := factory("pub-no-broker", map[string]any{
		"topic":  "test.topic",
		"broker": "nonexistent-broker",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error when broker service not found")
	}
	if !strings.Contains(err.Error(), "broker service") {
		t.Errorf("expected error about broker service, got: %v", err)
	}
}

func TestEventPublishStep_MissingTopicError(t *testing.T) {
	factory := NewEventPublishStepFactory()
	_, err := factory("pub-no-topic", map[string]any{
		"broker": "my-broker",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing topic")
	}
	if !strings.Contains(err.Error(), "'topic' is required") {
		t.Errorf("expected 'topic is required' error, got: %v", err)
	}
}

func TestEventPublishStep_EventTypeEnvelope(t *testing.T) {
	broker := newMockBroker()
	app := mockAppWithBroker("bus", broker)

	factory := NewEventPublishStepFactory()
	step, err := factory("pub-typed", map[string]any{
		"topic":      "events",
		"broker":     "bus",
		"event_type": "order.created",
		"payload": map[string]any{
			"id": "123",
		},
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	var envelope map[string]any
	if err := json.Unmarshal(broker.producer.published[0].message, &envelope); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if envelope["type"] != "order.created" {
		t.Errorf("expected type=order.created, got %v", envelope["type"])
	}
	if _, ok := envelope["data"]; !ok {
		t.Error("expected data field in envelope")
	}
}

func TestEventPublishStep_DefaultsPayloadToCurrent(t *testing.T) {
	broker := newMockBroker()
	app := mockAppWithBroker("bus", broker)

	factory := NewEventPublishStepFactory()
	step, err := factory("pub-default", map[string]any{
		"topic":  "events",
		"broker": "bus",
		// no payload configured
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{"foo": "bar"}, nil)
	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	var published map[string]any
	if err := json.Unmarshal(broker.producer.published[0].message, &published); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if published["foo"] != "bar" {
		t.Errorf("expected foo=bar from current context, got %v", published["foo"])
	}
}

func TestEventPublishStep_NoBrokerNorEventBus(t *testing.T) {
	app := NewMockApplication() // no broker, no eventbus

	factory := NewEventPublishStepFactory()
	step, err := factory("pub-no-target", map[string]any{
		"topic": "test.topic",
		// no broker specified
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error when no broker or eventbus available")
	}
	if !strings.Contains(err.Error(), "eventbus not available") {
		t.Errorf("expected eventbus not available error, got: %v", err)
	}
}
