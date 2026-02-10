package module

import (
	"testing"
)

func TestNATSBrokerName(t *testing.T) {
	b := NewNATSBroker("nats-test")
	if b.Name() != "nats-test" {
		t.Errorf("expected name 'nats-test', got %q", b.Name())
	}
}

func TestNATSBrokerModuleInterface(t *testing.T) {
	b := NewNATSBroker("nats-test")

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
	if services[0].Name != "nats-test" {
		t.Errorf("expected service name 'nats-test', got %q", services[0].Name)
	}
	if services[1].Name != "nats-test.producer" {
		t.Errorf("expected service name 'nats-test.producer', got %q", services[1].Name)
	}
	if services[2].Name != "nats-test.consumer" {
		t.Errorf("expected service name 'nats-test.consumer', got %q", services[2].Name)
	}

	// Test RequiresServices
	deps := b.RequiresServices()
	if len(deps) != 0 {
		t.Errorf("expected no dependencies, got %d", len(deps))
	}
}

func TestNATSBrokerInterfaceCompliance(t *testing.T) {
	b := NewNATSBroker("nats-test")

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

func TestNATSBrokerSetURL(t *testing.T) {
	b := NewNATSBroker("nats-test")

	// Test default URL (nats.DefaultURL = "nats://127.0.0.1:4222")
	if b.url != "nats://127.0.0.1:4222" {
		t.Errorf("expected default URL 'nats://127.0.0.1:4222', got %q", b.url)
	}

	// Test setter
	b.SetURL("nats://custom:4222")
	if b.url != "nats://custom:4222" {
		t.Errorf("expected URL 'nats://custom:4222', got %q", b.url)
	}
}

func TestNATSBrokerSubscribeBeforeConnect(t *testing.T) {
	b := NewNATSBroker("nats-test")
	app, _ := NewTestApplication()
	_ = b.Init(app)

	handler := &SimpleMessageHandler{name: "test", logger: &noopLogger{}}

	// Subscribe before Start should store handler for later activation
	err := b.Subscribe("test-topic", handler)
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	if _, ok := b.handlers["test-topic"]; !ok {
		t.Error("handler should be registered in handlers map")
	}
}

func TestNATSBrokerProducerWithoutConnection(t *testing.T) {
	b := NewNATSBroker("nats-test")

	err := b.Producer().SendMessage("test", []byte("hello"))
	if err == nil {
		t.Error("expected error when sending without connection")
	}
}
