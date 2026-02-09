package module

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/CrisisTextLine/modular"
)

func TestInMemoryMessageBroker(t *testing.T) {
	// Create broker with a proper logger initialization
	broker := NewInMemoryMessageBroker("test-broker")

	// Initialize the broker with a mock application to set the logger
	mockApp := NewMockApplication()
	err := broker.Init(mockApp)
	if err != nil {
		t.Fatalf("Failed to initialize broker: %v", err)
	}

	// Test message sending with no subscribers
	err = broker.Producer().SendMessage("test-topic", []byte("test message"))
	if err != nil {
		t.Errorf("failed to send message to empty topic: %v", err)
	}

	// Create a test handler
	messageReceived := false
	var receivedMessage []byte
	var messageWg sync.WaitGroup
	messageWg.Add(1)

	testHandler := &SimpleMessageHandler{
		name: "test-handler",
		handleFunc: func(message []byte) error {
			receivedMessage = message
			messageReceived = true
			messageWg.Done()
			return nil
		},
		logger: mockApp.Logger(), // Add logger to avoid nil pointer dereference
	}

	// Subscribe handler to topic
	err = broker.Consumer().Subscribe("test-topic", testHandler)
	if err != nil {
		t.Errorf("failed to subscribe handler: %v", err)
	}

	// Send a message
	testMessage := []byte("hello world")
	err = broker.Producer().SendMessage("test-topic", testMessage)
	if err != nil {
		t.Errorf("failed to send message: %v", err)
	}

	// Wait for message to be processed (with timeout)
	done := make(chan struct{})
	go func() {
		messageWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success, message was processed
	case <-time.After(time.Second):
		t.Error("timeout waiting for message to be processed")
	}

	// Check message was received
	if !messageReceived {
		t.Error("message was not received by handler")
	}

	// Check message content
	if string(receivedMessage) != string(testMessage) {
		t.Errorf("expected message '%s', got '%s'", string(testMessage), string(receivedMessage))
	}

	// Test unsubscribing
	err = broker.Consumer().Unsubscribe("test-topic")
	if err != nil {
		t.Errorf("failed to unsubscribe: %v", err)
	}

	// Send another message (should not trigger handler)
	messageReceived = false
	err = broker.Producer().SendMessage("test-topic", []byte("message after unsubscribe"))
	if err != nil {
		t.Errorf("failed to send message after unsubscribe: %v", err)
	}

	// Wait briefly to ensure message wasn't processed
	time.Sleep(100 * time.Millisecond)
	if messageReceived {
		t.Error("handler received message after unsubscribe")
	}
}

// Add mockMessageProducer implementation before TestSimpleMessageHandler
type mockMessageProducer struct {
	sendFunc func(topic string, message []byte) error
}

func (m *mockMessageProducer) SendMessage(topic string, message []byte) error {
	if m.sendFunc != nil {
		return m.sendFunc(topic, message)
	}
	return nil
}

func TestSimpleMessageHandler(t *testing.T) {
	// Create handler with a mock logger
	handler := NewSimpleMessageHandler("test-handler")
	handler.logger = &mockLogger{entries: make([]string, 0)} // Add logger to avoid nil pointer dereference

	// Test default handler implementation
	err := handler.HandleMessage([]byte("test message"))
	if err != nil {
		t.Errorf("default handler implementation failed: %v", err)
	}

	// Test custom handler function
	messageProcessed := false
	handler.SetHandleFunc(func(message []byte) error {
		messageProcessed = true
		if string(message) != "custom message" {
			t.Errorf("expected message 'custom message', got '%s'", string(message))
		}
		return nil
	})

	err = handler.HandleMessage([]byte("custom message"))
	if err != nil {
		t.Errorf("custom handler function failed: %v", err)
	}

	if !messageProcessed {
		t.Error("custom handler function was not called")
	}

	// Test message forwarding
	mockProducer := &mockMessageProducer{
		sendFunc: func(topic string, message []byte) error {
			if topic != "target-topic" {
				t.Errorf("expected topic 'target-topic', got '%s'", topic)
			}
			if string(message) != "forward message" {
				t.Errorf("expected message 'forward message', got '%s'", string(message))
			}
			return nil
		},
	}

	handler.SetHandleFunc(nil) // Reset to default handler
	handler.SetProducer(mockProducer)
	handler.SetTargetTopics([]string{"target-topic"})

	err = handler.HandleMessage([]byte("forward message"))
	if err != nil {
		t.Errorf("message forwarding failed: %v", err)
	}
}

func createIsolatedAppForMessagingTest(t *testing.T) modular.Application {
	t.Helper()

	// Create a completely fresh application for each test
	uniqueSuffix := fmt.Sprintf("%d", time.Now().UnixNano())
	t.Setenv("APP_NAME", fmt.Sprintf("test-messaging-app-%s", uniqueSuffix))
	t.Setenv("APP_VERSION", "1.0.0")

	// Use a mock logger to avoid nil pointer issues
	logger := &mockLogger{entries: make([]string, 0)}

	configProvider := modular.NewStdConfigProvider(&minCfg{})
	app := modular.NewStdApplication(configProvider, logger)

	err := app.Init()
	if err != nil {
		t.Fatalf("Failed to initialize app: %v", err)
	}

	return app
}

// TestMessagingModulesIntegration tests messaging modules working together
func TestMessagingModulesIntegration(t *testing.T) {
	// Skip if running in short mode
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Use truly unique names with a random component
	broker1Id := fmt.Sprintf("%d-%d", time.Now().UnixNano(), time.Now().UnixNano()%10000)
	brokerName := fmt.Sprintf("msg-broker-part1-%s", broker1Id)
	handler1Name := fmt.Sprintf("handler1-part1-%s", broker1Id)
	handler2Name := fmt.Sprintf("handler2-part1-%s", broker1Id)

	// Create isolated app for this test
	app := createIsolatedAppForMessagingTest(t)

	// Create messaging broker with unique name
	broker := NewInMemoryMessageBroker(brokerName)

	// Create message handlers with unique names
	handler1 := NewSimpleMessageHandler(handler1Name)
	handler2 := NewSimpleMessageHandler(handler2Name)

	// Initialize loggers immediately to avoid nil pointer dereference
	mockLogger := &mockLogger{entries: make([]string, 0)}
	handler1.logger = mockLogger
	handler2.logger = mockLogger

	// Set the broker dependencies for the handlers to match our unique broker name
	handler1.SetBrokerDependencies([]string{brokerName})
	handler2.SetBrokerDependencies([]string{brokerName})

	// Create channels to wait for message processing
	handler1Called := make(chan struct{})
	handler2Called := make(chan struct{})

	// Set handler functions
	handler1.SetHandleFunc(func(message []byte) error {
		if string(message) != "test message 1" {
			t.Errorf("handler1: expected message 'test message 1', got '%s'", string(message))
		}
		close(handler1Called)
		return nil
	})

	handler2.SetHandleFunc(func(message []byte) error {
		if string(message) != "test message 2" {
			t.Errorf("handler2: expected message 'test message 2', got '%s'", string(message))
		}
		close(handler2Called)
		return nil
	})

	// Register modules
	app.RegisterModule(broker)
	app.RegisterModule(handler1)
	app.RegisterModule(handler2)

	// Initialize modules
	if err := app.Init(); err != nil {
		t.Fatalf("Failed to initialize modules: %v", err)
	}

	// Subscribe handlers to topics
	if err := broker.Consumer().Subscribe("topic-1", handler1); err != nil {
		t.Fatalf("Failed to subscribe handler1: %v", err)
	}

	if err := broker.Consumer().Subscribe("topic-2", handler2); err != nil {
		t.Fatalf("Failed to subscribe handler2: %v", err)
	}

	// Start modules
	if err := app.Start(); err != nil {
		t.Fatalf("Failed to start modules: %v", err)
	}

	// Send messages
	if err := broker.Producer().SendMessage("topic-1", []byte("test message 1")); err != nil {
		t.Fatalf("Failed to send message to topic-1: %v", err)
	}

	if err := broker.Producer().SendMessage("topic-2", []byte("test message 2")); err != nil {
		t.Fatalf("Failed to send message to topic-2: %v", err)
	}

	// Wait for messages to be processed with timeout
	timeout := time.After(2 * time.Second)
	for i := 0; i < 2; i++ {
		select {
		case <-handler1Called:
			// Handler 1 received the message
		case <-handler2Called:
			// Handler 2 received the message
		case <-timeout:
			t.Fatal("Timeout waiting for handlers to process messages")
		}
	}

	// Stop modules
	if err := app.Stop(); err != nil {
		t.Fatalf("Failed to stop modules: %v", err)
	}

	// Create a completely separate test for forwarding
	TestMessageForwarding(t)
}

// TestMessageForwarding is a separate test for message forwarding functionality
func TestMessageForwarding(t *testing.T) {
	// Create a completely new isolated app for this test
	app := createIsolatedAppForMessagingTest(t)

	// Create truly unique names for this test
	broker2Id := fmt.Sprintf("%d-%d", time.Now().UnixNano(), time.Now().UnixNano()%10000+1000)
	forwarderBrokerName := fmt.Sprintf("msg-broker-part2-%s", broker2Id)
	forwarderName := fmt.Sprintf("forwarder-part2-%s", broker2Id)
	receiverName := fmt.Sprintf("receiver-part2-%s", broker2Id)

	// Create broker and handlers with the unique names
	broker := NewInMemoryMessageBroker(forwarderBrokerName)
	forwarder := NewSimpleMessageHandler(forwarderName)
	receiver := NewSimpleMessageHandler(receiverName)

	// Initialize loggers immediately to avoid nil pointer dereference
	mockLogger := &mockLogger{entries: make([]string, 0)}
	broker.logger = mockLogger
	forwarder.logger = mockLogger
	receiver.logger = mockLogger

	// Set the broker dependencies for the forwarding handlers
	forwarder.SetBrokerDependencies([]string{forwarderBrokerName})
	receiver.SetBrokerDependencies([]string{forwarderBrokerName})

	receiverCalled := make(chan struct{})
	receiver.SetHandleFunc(func(message []byte) error {
		if string(message) != "forwarded message" {
			t.Errorf("receiver: expected message 'forwarded message', got '%s'", string(message))
		}
		close(receiverCalled)
		return nil
	})

	// Configure forwarder to forward to another topic
	forwarder.SetProducer(broker.Producer())
	forwarder.SetTargetTopics([]string{"forward-destination"})

	// Register modules
	app.RegisterModule(broker)
	app.RegisterModule(forwarder)
	app.RegisterModule(receiver)

	// Initialize modules
	if err := app.Init(); err != nil {
		t.Fatalf("Failed to initialize modules: %v", err)
	}

	// Subscribe handlers to topics
	if err := broker.Consumer().Subscribe("source-topic", forwarder); err != nil {
		t.Fatalf("Failed to subscribe forwarder: %v", err)
	}

	if err := broker.Consumer().Subscribe("forward-destination", receiver); err != nil {
		t.Fatalf("Failed to subscribe receiver: %v", err)
	}

	// Start modules
	if err := app.Start(); err != nil {
		t.Fatalf("Failed to start modules: %v", err)
	}

	// Send message to source topic
	if err := broker.Producer().SendMessage("source-topic", []byte("forwarded message")); err != nil {
		t.Fatalf("Failed to send message to source topic: %v", err)
	}

	// Wait for message to be forwarded and received
	select {
	case <-receiverCalled:
		// Success - message was forwarded and received
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for message forwarding")
	}

	// Stop modules
	if err := app.Stop(); err != nil {
		t.Fatalf("Failed to stop modules: %v", err)
	}
}
