package module

import (
	"context"
	"fmt"
	"sync"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/pkg/tlsutil"
	"github.com/nats-io/nats.go"
)

// NATSBroker implements the MessageBroker interface using NATS.
type NATSBroker struct {
	name          string
	url           string
	conn          *nats.Conn
	subscriptions map[string]*nats.Subscription
	handlers      map[string]MessageHandler
	mu            sync.RWMutex
	producer      *natsProducer
	consumer      *natsConsumer
	logger        modular.Logger
	tlsCfg        tlsutil.TLSConfig
}

// NewNATSBroker creates a new NATS message broker.
func NewNATSBroker(name string) *NATSBroker {
	broker := &NATSBroker{
		name:          name,
		url:           nats.DefaultURL,
		subscriptions: make(map[string]*nats.Subscription),
		handlers:      make(map[string]MessageHandler),
		logger:        &noopLogger{},
	}
	broker.producer = &natsProducer{broker: broker}
	broker.consumer = &natsConsumer{broker: broker}
	return broker
}

// Name returns the module name.
func (b *NATSBroker) Name() string {
	return b.name
}

// Init initializes the module with the application context.
func (b *NATSBroker) Init(app modular.Application) error {
	b.logger = app.Logger()
	return nil
}

// ProvidesServices returns the services provided by this module.
func (b *NATSBroker) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        b.name,
			Description: "NATS Message Broker",
			Instance:    b,
		},
		{
			Name:        b.name + ".producer",
			Description: "NATS Message Producer",
			Instance:    b.producer,
		},
		{
			Name:        b.name + ".consumer",
			Description: "NATS Message Consumer",
			Instance:    b.consumer,
		},
	}
}

// RequiresServices returns the services required by this module.
func (b *NATSBroker) RequiresServices() []modular.ServiceDependency {
	return nil
}

// SetURL sets the NATS server URL.
func (b *NATSBroker) SetURL(url string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.url = url
}

// SetTLSConfig configures TLS for the NATS broker connection.
func (b *NATSBroker) SetTLSConfig(cfg tlsutil.TLSConfig) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tlsCfg = cfg
}

// Producer returns the message producer interface.
func (b *NATSBroker) Producer() MessageProducer {
	return b.producer
}

// Consumer returns the message consumer interface.
func (b *NATSBroker) Consumer() MessageConsumer {
	return b.consumer
}

// Subscribe is a convenience method to subscribe a handler to a topic.
func (b *NATSBroker) Subscribe(topic string, handler MessageHandler) error {
	return b.consumer.Subscribe(topic, handler)
}

// Start connects to NATS and activates any pending subscriptions.
func (b *NATSBroker) Start(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	var opts []nats.Option
	if b.tlsCfg.Enabled {
		tlsCfg, tlsErr := tlsutil.LoadTLSConfig(b.tlsCfg)
		if tlsErr != nil {
			return fmt.Errorf("nats broker %q: TLS config: %w", b.name, tlsErr)
		}
		opts = append(opts, nats.Secure(tlsCfg))
	}

	conn, err := nats.Connect(b.url, opts...)
	if err != nil {
		return fmt.Errorf("failed to connect to NATS at %s: %w", b.url, err)
	}
	b.conn = conn

	// Activate pending subscriptions
	for topic, handler := range b.handlers {
		h := handler // capture for closure
		sub, subErr := conn.Subscribe(topic, func(msg *nats.Msg) {
			if handleErr := h.HandleMessage(msg.Data); handleErr != nil {
				b.logger.Error("Error handling NATS message", "topic", topic, "error", handleErr)
			}
		})
		if subErr != nil {
			return fmt.Errorf("failed to subscribe to topic %q: %w", topic, subErr)
		}
		b.subscriptions[topic] = sub
	}

	b.logger.Info("NATS broker started", "url", b.url)
	return nil
}

// Stop disconnects from NATS.
func (b *NATSBroker) Stop(_ context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	for topic, sub := range b.subscriptions {
		if err := sub.Unsubscribe(); err != nil {
			b.logger.Error("Failed to unsubscribe", "topic", topic, "error", err)
		}
		delete(b.subscriptions, topic)
	}

	if b.conn != nil {
		b.conn.Close()
		b.conn = nil
	}

	b.logger.Info("NATS broker stopped")
	return nil
}

// natsProducer implements MessageProducer for NATS.
type natsProducer struct {
	broker *NATSBroker
}

// SendMessage publishes a message to a NATS topic.
func (p *natsProducer) SendMessage(topic string, message []byte) error {
	p.broker.mu.RLock()
	conn := p.broker.conn
	p.broker.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("NATS connection not established; call Start first")
	}

	if err := conn.Publish(topic, message); err != nil {
		return fmt.Errorf("failed to publish to topic %q: %w", topic, err)
	}

	p.broker.logger.Info("Message published to NATS", "topic", topic)
	return nil
}

// natsConsumer implements MessageConsumer for NATS.
type natsConsumer struct {
	broker *NATSBroker
}

// Subscribe registers a handler for a topic.
// If the broker is already connected, the subscription is activated immediately.
func (c *natsConsumer) Subscribe(topic string, handler MessageHandler) error {
	c.broker.mu.Lock()
	defer c.broker.mu.Unlock()

	c.broker.handlers[topic] = handler

	// If already connected, subscribe immediately
	if c.broker.conn != nil {
		h := handler
		sub, err := c.broker.conn.Subscribe(topic, func(msg *nats.Msg) {
			if handleErr := h.HandleMessage(msg.Data); handleErr != nil {
				c.broker.logger.Error("Error handling NATS message", "topic", topic, "error", handleErr)
			}
		})
		if err != nil {
			return fmt.Errorf("failed to subscribe to topic %q: %w", topic, err)
		}
		c.broker.subscriptions[topic] = sub
	}

	c.broker.logger.Info("Handler registered for NATS topic", "topic", topic)
	return nil
}

// Unsubscribe removes the handler for a topic.
func (c *natsConsumer) Unsubscribe(topic string) error {
	c.broker.mu.Lock()
	defer c.broker.mu.Unlock()

	if sub, ok := c.broker.subscriptions[topic]; ok {
		if err := sub.Unsubscribe(); err != nil {
			return fmt.Errorf("failed to unsubscribe from topic %q: %w", topic, err)
		}
		delete(c.broker.subscriptions, topic)
	}

	delete(c.broker.handlers, topic)
	c.broker.logger.Info("Handler unregistered from NATS topic", "topic", topic)
	return nil
}
