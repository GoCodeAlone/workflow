package module

import (
	"context"
	"encoding/json"
	"testing"
)

func TestPublishStep_MissingTopic(t *testing.T) {
	factory := NewPublishStepFactory()
	_, err := factory("no-topic", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error for missing topic")
	}
}

func TestPublishStep_FactoryValid(t *testing.T) {
	factory := NewPublishStepFactory()
	_, err := factory("pub", map[string]any{"topic": "events.test"}, nil)
	if err != nil {
		t.Fatalf("unexpected factory error: %v", err)
	}
}

func TestPublishStep_ViaBroker(t *testing.T) {
	broker := newMockBroker()
	app := mockAppWithBroker("my-broker", broker)

	factory := NewPublishStepFactory()
	step, err := factory("pub", map[string]any{
		"topic":  "events.test",
		"broker": "my-broker",
		"payload": map[string]any{
			"key": "value",
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
	if result.Output["topic"] != "events.test" {
		t.Errorf("expected topic='events.test', got %v", result.Output["topic"])
	}

	if len(broker.producer.published) != 1 {
		t.Fatalf("expected 1 message published, got %d", len(broker.producer.published))
	}
	msg := broker.producer.published[0]
	if msg.topic != "events.test" {
		t.Errorf("expected topic='events.test', got %q", msg.topic)
	}

	var body map[string]any
	if err := json.Unmarshal(msg.message, &body); err != nil {
		t.Fatalf("failed to unmarshal message: %v", err)
	}
	if body["key"] != "value" {
		t.Errorf("expected key='value' in message, got %v", body["key"])
	}
}

func TestPublishStep_NoPayload_UsesCurrentContext(t *testing.T) {
	broker := newMockBroker()
	app := mockAppWithBroker("bus", broker)

	factory := NewPublishStepFactory()
	step, err := factory("pub", map[string]any{
		"topic":  "ctx.topic",
		"broker": "bus",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{"x": "y"}, nil)
	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if len(broker.producer.published) != 1 {
		t.Fatalf("expected 1 message published, got %d", len(broker.producer.published))
	}

	var body map[string]any
	if err := json.Unmarshal(broker.producer.published[0].message, &body); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if body["x"] != "y" {
		t.Errorf("expected x=y from context, got %v", body["x"])
	}
}

func TestPublishStep_NoBrokerNoEventBus_ReturnsNotPublished(t *testing.T) {
	// App has no broker and no eventbus service → should return published=false gracefully
	app := NewMockApplication()

	factory := NewPublishStepFactory()
	step, err := factory("pub", map[string]any{"topic": "orphan.topic"}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if result.Output["published"] != false {
		t.Errorf("expected published=false when no eventbus, got %v", result.Output["published"])
	}
}
