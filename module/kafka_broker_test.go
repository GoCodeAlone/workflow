package module

import (
	"testing"
)

func TestKafkaBrokerName(t *testing.T) {
	b := NewKafkaBroker("kafka-test")
	if b.Name() != "kafka-test" {
		t.Errorf("expected name 'kafka-test', got %q", b.Name())
	}
}

func TestKafkaBrokerModuleInterface(t *testing.T) {
	b := NewKafkaBroker("kafka-test")

	// Test Init
	app, _ := NewTestApplication()
	if err := b.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Test ProvidesServices
	services := b.ProvidesServices()
	if len(services) != 3 {
		t.Fatalf("expected 3 services, got %d", len(services))
	}
	if services[0].Name != "kafka-test" {
		t.Errorf("expected service name 'kafka-test', got %q", services[0].Name)
	}
	if services[1].Name != "kafka-test.producer" {
		t.Errorf("expected service name 'kafka-test.producer', got %q", services[1].Name)
	}
	if services[2].Name != "kafka-test.consumer" {
		t.Errorf("expected service name 'kafka-test.consumer', got %q", services[2].Name)
	}

	// Test RequiresServices
	deps := b.RequiresServices()
	if len(deps) != 0 {
		t.Errorf("expected no dependencies, got %d", len(deps))
	}
}

func TestKafkaBrokerInterfaceCompliance(t *testing.T) {
	b := NewKafkaBroker("kafka-test")

	// Verify it implements MessageBroker
	var _ MessageBroker = b

	// Verify producer and consumer are non-nil
	if b.Producer() == nil {
		t.Error("Producer should not be nil")
	}
	if b.Consumer() == nil {
		t.Error("Consumer should not be nil")
	}
}

func TestKafkaBrokerConfig(t *testing.T) {
	b := NewKafkaBroker("kafka-test")

	// Test defaults
	if len(b.brokers) != 1 || b.brokers[0] != "localhost:9092" {
		t.Errorf("expected default brokers ['localhost:9092'], got %v", b.brokers)
	}
	if b.groupID != "workflow-group" {
		t.Errorf("expected default groupID 'workflow-group', got %q", b.groupID)
	}

	// Test setters
	b.SetBrokers([]string{"broker1:9092", "broker2:9092"})
	if len(b.brokers) != 2 {
		t.Errorf("expected 2 brokers, got %d", len(b.brokers))
	}

	b.SetGroupID("custom-group")
	if b.groupID != "custom-group" {
		t.Errorf("expected groupID 'custom-group', got %q", b.groupID)
	}
}

func TestKafkaBrokerSubscribeBeforeStart(t *testing.T) {
	b := NewKafkaBroker("kafka-test")
	app, _ := NewTestApplication()
	_ = b.Init(app)

	handler := &SimpleMessageHandler{name: "test", logger: &noopLogger{}}

	err := b.Subscribe("test-topic", handler)
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	if _, ok := b.handlers["test-topic"]; !ok {
		t.Error("handler should be registered in handlers map")
	}
}

func TestKafkaBrokerProducerWithoutStart(t *testing.T) {
	b := NewKafkaBroker("kafka-test")

	err := b.Producer().SendMessage("test", []byte("hello"))
	if err == nil {
		t.Error("expected error when sending without connection")
	}
}

func TestKafkaBrokerUnsubscribe(t *testing.T) {
	b := NewKafkaBroker("kafka-test")
	app, _ := NewTestApplication()
	_ = b.Init(app)

	handler := &SimpleMessageHandler{name: "test", logger: &noopLogger{}}
	_ = b.Subscribe("test-topic", handler)

	err := b.Consumer().Unsubscribe("test-topic")
	if err != nil {
		t.Fatalf("Unsubscribe failed: %v", err)
	}

	if _, ok := b.handlers["test-topic"]; ok {
		t.Error("handler should be removed after unsubscribe")
	}
}
