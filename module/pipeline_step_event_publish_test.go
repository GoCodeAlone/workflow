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
	// Without source, only event_type is insufficient for a full CloudEvents envelope.
	// The step wraps as {data: payload} without CloudEvents-required fields.
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
	// Without source, CloudEvents required fields (specversion, id, time, type, source) are not added.
	if _, ok := envelope["specversion"]; ok {
		t.Error("expected no specversion when source is not set")
	}
	if _, ok := envelope["type"]; ok {
		t.Error("expected no type when source is not set (incomplete CloudEvents config)")
	}
	// But the payload is still wrapped under "data"
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

func TestEventPublishStep_CloudEventsEnvelope(t *testing.T) {
	broker := newMockBroker()
	app := mockAppWithBroker("bus", broker)

	factory := NewEventPublishStepFactory()
	step, err := factory("pub-cloudevents", map[string]any{
		"topic":      "messaging.texter-messages",
		"broker":     "bus",
		"event_type": "messaging.texter-message.received",
		"source":     "/chimera/messaging",
		"payload": map[string]any{
			"messageId": "msg-1",
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
	if envelope["specversion"] != "1.0" {
		t.Errorf("expected specversion=1.0, got %v", envelope["specversion"])
	}
	if envelope["type"] != "messaging.texter-message.received" {
		t.Errorf("expected type=messaging.texter-message.received, got %v", envelope["type"])
	}
	if envelope["source"] != "/chimera/messaging" {
		t.Errorf("expected source=/chimera/messaging, got %v", envelope["source"])
	}
	if _, ok := envelope["id"].(string); !ok || envelope["id"] == "" {
		t.Errorf("expected non-empty id string, got %v", envelope["id"])
	}
	if _, ok := envelope["time"].(string); !ok || envelope["time"] == "" {
		t.Errorf("expected non-empty time string, got %v", envelope["time"])
	}
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatal("expected data field in envelope")
	}
	if data["messageId"] != "msg-1" {
		t.Errorf("expected messageId=msg-1, got %v", data["messageId"])
	}
}

func TestEventPublishStep_StreamAlias(t *testing.T) {
	broker := newMockBroker()
	app := mockAppWithBroker("bus", broker)

	factory := NewEventPublishStepFactory()
	step, err := factory("pub-stream", map[string]any{
		"stream": "messaging.texter-messages",
		"broker": "bus",
		"payload": map[string]any{
			"id": "1",
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
	if result.Output["topic"] != "messaging.texter-messages" {
		t.Errorf("expected topic=messaging.texter-messages, got %v", result.Output["topic"])
	}
}

func TestEventPublishStep_DataAlias(t *testing.T) {
	broker := newMockBroker()
	app := mockAppWithBroker("bus", broker)

	factory := NewEventPublishStepFactory()
	step, err := factory("pub-data", map[string]any{
		"topic":  "events",
		"broker": "bus",
		"data": map[string]any{
			"texterId": "42",
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

	var payload map[string]any
	if err := json.Unmarshal(broker.producer.published[0].message, &payload); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if payload["texterId"] != "42" {
		t.Errorf("expected texterId=42, got %v", payload["texterId"])
	}
}

func TestEventPublishStep_ProviderAlias(t *testing.T) {
	broker := newMockBroker()
	app := mockAppWithBroker("kinesis-provider", broker)

	factory := NewEventPublishStepFactory()
	step, err := factory("pub-provider", map[string]any{
		"topic":    "events",
		"provider": "kinesis-provider",
		"payload": map[string]any{
			"id": "1",
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
}

// mockEventPublisher implements EventPublisher for testing.
type mockEventPublisher struct {
	published []struct {
		topic string
		event map[string]any
	}
	publishErr error
}

func (p *mockEventPublisher) PublishEvent(_ context.Context, topic string, event map[string]any) error {
	if p.publishErr != nil {
		return p.publishErr
	}
	p.published = append(p.published, struct {
		topic string
		event map[string]any
	}{topic, event})
	return nil
}

func TestEventPublishStep_EventPublisherInterface(t *testing.T) {
	pub := &mockEventPublisher{}
	app := NewMockApplication()
	app.Services["bento-output"] = pub

	factory := NewEventPublishStepFactory()
	step, err := factory("pub-ep", map[string]any{
		"topic":      "events.processed",
		"provider":   "bento-output",
		"event_type": "order.shipped",
		"source":     "/api/orders",
		"payload": map[string]any{
			"orderId": "ORD-99",
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
		t.Errorf("expected published=true")
	}

	if len(pub.published) != 1 {
		t.Fatalf("expected 1 published event, got %d", len(pub.published))
	}
	if pub.published[0].topic != "events.processed" {
		t.Errorf("expected topic=events.processed, got %v", pub.published[0].topic)
	}
	event := pub.published[0].event
	if event["specversion"] != "1.0" {
		t.Errorf("expected specversion=1.0, got %v", event["specversion"])
	}
	if event["type"] != "order.shipped" {
		t.Errorf("expected type=order.shipped, got %v", event["type"])
	}
	if event["source"] != "/api/orders" {
		t.Errorf("expected source=/api/orders, got %v", event["source"])
	}
	data, ok := event["data"].(map[string]any)
	if !ok {
		t.Fatal("expected data in event")
	}
	if data["orderId"] != "ORD-99" {
		t.Errorf("expected orderId=ORD-99, got %v", data["orderId"])
	}
}

func TestEventPublishStep_SourceTemplateResolution(t *testing.T) {
	broker := newMockBroker()
	app := mockAppWithBroker("bus", broker)

	factory := NewEventPublishStepFactory()
	step, err := factory("pub-src-tmpl", map[string]any{
		"topic":      "events",
		"broker":     "bus",
		"event_type": "test.event",
		"source":     "/api/{{ .service }}",
		"payload": map[string]any{
			"id": "1",
		},
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{"service": "orders"}, nil)
	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	var envelope map[string]any
	if err := json.Unmarshal(broker.producer.published[0].message, &envelope); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if envelope["source"] != "/api/orders" {
		t.Errorf("expected source=/api/orders, got %v", envelope["source"])
	}
}

func TestEventPublishStep_SourceTemplateError(t *testing.T) {
	broker := newMockBroker()
	app := mockAppWithBroker("bus", broker)

	factory := NewEventPublishStepFactory()
	step, err := factory("pub-src-err", map[string]any{
		"topic":      "events",
		"broker":     "bus",
		"event_type": "test.event",
		"source":     "/api/{{ .service", // malformed template
		"payload": map[string]any{
			"id": "1",
		},
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error when source template resolution fails")
	}
	if !strings.Contains(err.Error(), "failed to resolve source") {
		t.Errorf("expected 'failed to resolve source' error, got: %v", err)
	}
}
