package module

import (
	"context"
	"fmt"
	"sync"

	"github.com/GoCodeAlone/modular"
)

// Standard module name constants
const (
	InMemoryMessageBrokerName = "messaging.broker.memory"
)

// InMemoryMessageBroker provides a simple in-memory implementation of MessageBroker
type InMemoryMessageBroker struct {
	name          string
	namespace     ModuleNamespaceProvider
	subscriptions map[string][]MessageHandler
	mu            sync.RWMutex
	producer      *inMemoryProducer
	consumer      *inMemoryConsumer
}

// NewInMemoryMessageBroker creates a new in-memory message broker
func NewInMemoryMessageBroker(name string) *InMemoryMessageBroker {
	return NewInMemoryMessageBrokerWithNamespace(name, nil)
}

// NewInMemoryMessageBrokerWithNamespace creates a new in-memory message broker with namespace support
func NewInMemoryMessageBrokerWithNamespace(name string, namespace ModuleNamespaceProvider) *InMemoryMessageBroker {
	// Default to standard namespace if none provided
	if namespace == nil {
		namespace = NewStandardNamespace("", "")
	}

	// Format the name using the namespace
	formattedName := namespace.FormatName(name)

	broker := &InMemoryMessageBroker{
		name:          formattedName,
		namespace:     namespace,
		subscriptions: make(map[string][]MessageHandler),
	}
	broker.producer = &inMemoryProducer{broker: broker}
	broker.consumer = &inMemoryConsumer{broker: broker}
	return broker
}

// NewStandardInMemoryMessageBroker creates an in-memory message broker with the standard name
func NewStandardInMemoryMessageBroker(namespace ModuleNamespaceProvider) *InMemoryMessageBroker {
	return NewInMemoryMessageBrokerWithNamespace(InMemoryMessageBrokerName, namespace)
}

// Name returns the unique identifier for this module
func (b *InMemoryMessageBroker) Name() string {
	return b.name
}

// Init initializes the module with the application context
func (b *InMemoryMessageBroker) Init(app modular.Application) error {
	return nil
}

// Producer returns the message producer interface
func (b *InMemoryMessageBroker) Producer() MessageProducer {
	return b.producer
}

// Consumer returns the message consumer interface
func (b *InMemoryMessageBroker) Consumer() MessageConsumer {
	return b.consumer
}

// Subscribe is a convenience method to subscribe a handler to a topic
func (b *InMemoryMessageBroker) Subscribe(topic string, handler MessageHandler) error {
	return b.consumer.Subscribe(topic, handler)
}

// SendMessage is a convenience method to send a message to a topic
func (b *InMemoryMessageBroker) SendMessage(topic string, message []byte) error {
	return b.producer.SendMessage(topic, message)
}

// Start starts the message broker
func (b *InMemoryMessageBroker) Start(ctx context.Context) error {
	fmt.Println("In-memory message broker started")
	return nil
}

// Stop stops the message broker
func (b *InMemoryMessageBroker) Stop(ctx context.Context) error {
	fmt.Println("In-memory message broker stopped")
	return nil
}

// ProvidesServices returns a list of services provided by this module
func (b *InMemoryMessageBroker) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        b.name,
			Description: "In-Memory Message Broker",
			Instance:    b,
		},
		{
			Name:        b.namespace.ResolveServiceName(b.name + ".producer"),
			Description: "Message Producer",
			Instance:    b.producer,
		},
		{
			Name:        b.namespace.ResolveServiceName(b.name + ".consumer"),
			Description: "Message Consumer",
			Instance:    b.consumer,
		},
	}
}

// RequiresServices returns a list of services required by this module
func (b *InMemoryMessageBroker) RequiresServices() []modular.ServiceDependency {
	// No required services
	return nil
}

// inMemoryProducer implements MessageProducer
type inMemoryProducer struct {
	broker *InMemoryMessageBroker
}

// SendMessage sends a message to a topic
func (p *inMemoryProducer) SendMessage(topic string, message []byte) error {
	p.broker.mu.RLock()
	defer p.broker.mu.RUnlock()

	handlers, exists := p.broker.subscriptions[topic]
	if !exists {
		return nil // No subscribers for this topic
	}

	// Deliver message to all subscribers
	for _, handler := range handlers {
		if err := handler.HandleMessage(message); err != nil {
			fmt.Printf("Error handling message on topic %s: %v\n", topic, err)
		}
	}

	return nil
}

// inMemoryConsumer implements MessageConsumer
type inMemoryConsumer struct {
	broker *InMemoryMessageBroker
}

// Subscribe subscribes a handler to a topic
func (c *inMemoryConsumer) Subscribe(topic string, handler MessageHandler) error {
	c.broker.mu.Lock()
	defer c.broker.mu.Unlock()

	c.broker.subscriptions[topic] = append(c.broker.subscriptions[topic], handler)
	fmt.Printf("Handler subscribed to topic: %s\n", topic)
	return nil
}

// Unsubscribe removes all handlers for a topic
func (c *inMemoryConsumer) Unsubscribe(topic string) error {
	c.broker.mu.Lock()
	defer c.broker.mu.Unlock()

	delete(c.broker.subscriptions, topic)
	fmt.Printf("All handlers unsubscribed from topic: %s\n", topic)
	return nil
}
