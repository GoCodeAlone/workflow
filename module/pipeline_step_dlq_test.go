package module

import (
	"context"
	"encoding/json"
	"testing"
)

// dlqMockProducer captures messages sent to it.
type dlqMockProducer struct {
	messages []struct {
		topic string
		data  []byte
	}
}

func (p *dlqMockProducer) SendMessage(topic string, data []byte) error {
	p.messages = append(p.messages, struct {
		topic string
		data  []byte
	}{topic, data})
	return nil
}

// dlqMockBroker wraps a dlqMockProducer as a MessageBroker.
type dlqMockBroker struct {
	producer *dlqMockProducer
}

func (b *dlqMockBroker) Producer() MessageProducer                      { return b.producer }
func (b *dlqMockBroker) Consumer() MessageConsumer                      { return nil }
func (b *dlqMockBroker) Subscribe(_ string, _ MessageHandler) error     { return nil }
func (b *dlqMockBroker) Start(_ context.Context) error                  { return nil }
func (b *dlqMockBroker) Stop(_ context.Context) error                   { return nil }

// newAppWithDLQBroker creates a MockApplication with the broker registered under "test-broker".
func newAppWithDLQBroker() (*MockApplication, *dlqMockProducer) {
	producer := &dlqMockProducer{}
	broker := &dlqMockBroker{producer: producer}
	app := NewMockApplication()
	app.Services["test-broker"] = MessageBroker(broker)
	return app, producer
}

// ---- DLQSendStep tests ----

func TestDLQSendStepFactory_RequiresTopic(t *testing.T) {
	factory := NewDLQSendStepFactory()
	_, err := factory("dlq", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error when topic is missing")
	}
}

func TestDLQSendStepFactory_Defaults(t *testing.T) {
	factory := NewDLQSendStepFactory()
	step, err := factory("dlq", map[string]any{"topic": "dead.letters"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if step.Name() != "dlq" {
		t.Errorf("expected name %q, got %q", "dlq", step.Name())
	}
}

func TestDLQSendStep_SendsEnvelopeViaBroker(t *testing.T) {
	app, producer := newAppWithDLQBroker()

	factory := NewDLQSendStepFactory()
	step, err := factory("dlq", map[string]any{
		"topic":          "dead.letters",
		"original_topic": "orders",
		"error":          "processing failed",
		"broker":         "test-broker",
	}, app)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	pc := NewPipelineContext(map[string]any{"order_id": "123"}, nil)

	result, err := step.Execute(ctx, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Output["dlq_sent"] != true {
		t.Error("expected dlq_sent=true")
	}
	if result.Output["topic"] != "dead.letters" {
		t.Errorf("expected topic %q, got %v", "dead.letters", result.Output["topic"])
	}

	if len(producer.messages) != 1 {
		t.Fatalf("expected 1 message sent, got %d", len(producer.messages))
	}
	if producer.messages[0].topic != "dead.letters" {
		t.Errorf("expected topic %q, got %q", "dead.letters", producer.messages[0].topic)
	}

	var envelope map[string]any
	if err := json.Unmarshal(producer.messages[0].data, &envelope); err != nil {
		t.Fatalf("failed to unmarshal envelope: %v", err)
	}
	if envelope["original_topic"] != "orders" {
		t.Errorf("expected original_topic %q, got %v", "orders", envelope["original_topic"])
	}
	if envelope["error"] != "processing failed" {
		t.Errorf("expected error field, got %v", envelope["error"])
	}
	if envelope["sent_at"] == nil {
		t.Error("expected sent_at to be set")
	}
}

func TestDLQSendStep_UsesCurrentWhenNoPayload(t *testing.T) {
	app, producer := newAppWithDLQBroker()

	factory := NewDLQSendStepFactory()
	step, err := factory("dlq", map[string]any{
		"topic":  "dead.letters",
		"broker": "test-broker",
	}, app)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	pc := NewPipelineContext(map[string]any{"key": "value"}, nil)
	_, err = step.Execute(ctx, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var envelope map[string]any
	if err := json.Unmarshal(producer.messages[0].data, &envelope); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	payload, ok := envelope["payload"].(map[string]any)
	if !ok {
		t.Fatalf("expected payload map, got %T", envelope["payload"])
	}
	if payload["key"] != "value" {
		t.Errorf("expected payload.key=value, got %v", payload["key"])
	}
}

// ---- DLQReplayStep tests ----

func TestDLQReplayStepFactory_RequiresDlqTopic(t *testing.T) {
	factory := NewDLQReplayStepFactory()
	_, err := factory("replay", map[string]any{"target_topic": "orders"}, nil)
	if err == nil {
		t.Fatal("expected error when dlq_topic is missing")
	}
}

func TestDLQReplayStepFactory_RequiresTargetTopic(t *testing.T) {
	factory := NewDLQReplayStepFactory()
	_, err := factory("replay", map[string]any{"dlq_topic": "dead.letters"}, nil)
	if err == nil {
		t.Fatal("expected error when target_topic is missing")
	}
}

func TestDLQReplayStepFactory_Defaults(t *testing.T) {
	factory := NewDLQReplayStepFactory()
	step, err := factory("replay", map[string]any{
		"dlq_topic":    "dead.letters",
		"target_topic": "orders",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if step.(*DLQReplayStep).maxMessages != 100 {
		t.Errorf("expected maxMessages=100, got %d", step.(*DLQReplayStep).maxMessages)
	}
}

func TestDLQReplayStep_SingleMessage(t *testing.T) {
	app, producer := newAppWithDLQBroker()

	factory := NewDLQReplayStepFactory()
	step, err := factory("replay", map[string]any{
		"dlq_topic":    "dead.letters",
		"target_topic": "orders",
		"broker":       "test-broker",
	}, app)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	// DLQ envelope with payload field
	pc := NewPipelineContext(map[string]any{
		"payload": map[string]any{"order_id": "123"},
	}, nil)

	result, err := step.Execute(ctx, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Output["replayed"] != 1 {
		t.Errorf("expected replayed=1, got %v", result.Output["replayed"])
	}
	if len(producer.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(producer.messages))
	}
	if producer.messages[0].topic != "orders" {
		t.Errorf("expected topic=orders, got %q", producer.messages[0].topic)
	}

	var payload map[string]any
	if err := json.Unmarshal(producer.messages[0].data, &payload); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if payload["order_id"] != "123" {
		t.Errorf("expected order_id=123, got %v", payload["order_id"])
	}
}

func TestDLQReplayStep_BatchMessages(t *testing.T) {
	app, producer := newAppWithDLQBroker()

	factory := NewDLQReplayStepFactory()
	step, err := factory("replay", map[string]any{
		"dlq_topic":    "dead.letters",
		"target_topic": "orders",
		"max_messages": 2,
		"broker":       "test-broker",
	}, app)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	pc := NewPipelineContext(map[string]any{
		"messages": []any{
			map[string]any{"payload": map[string]any{"id": "1"}},
			map[string]any{"payload": map[string]any{"id": "2"}},
			map[string]any{"payload": map[string]any{"id": "3"}}, // truncated
		},
	}, nil)

	result, err := step.Execute(ctx, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Output["replayed"] != 2 {
		t.Errorf("expected replayed=2, got %v", result.Output["replayed"])
	}
	if len(producer.messages) != 2 {
		t.Errorf("expected 2 messages sent, got %d", len(producer.messages))
	}
}

func TestDLQReplayStep_EmptyMessages(t *testing.T) {
	app := NewMockApplication()

	factory := NewDLQReplayStepFactory()
	step, err := factory("replay", map[string]any{
		"dlq_topic":    "dead.letters",
		"target_topic": "orders",
	}, app)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	pc := NewPipelineContext(map[string]any{"messages": []any{}}, nil)

	result, err := step.Execute(ctx, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output["replayed"] != 0 {
		t.Errorf("expected replayed=0, got %v", result.Output["replayed"])
	}
}
