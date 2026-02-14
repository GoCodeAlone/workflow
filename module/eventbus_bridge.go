package module

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/CrisisTextLine/modular"
	"github.com/CrisisTextLine/modular/modules/eventbus"
)

// EventBusBridgeName is the default service name for the EventBus bridge adapter.
const EventBusBridgeName = "messaging.broker.eventbus"

// EventBusBridge adapts the modular framework's EventBusModule to the workflow
// engine's MessageBroker interface. It allows the workflow engine to publish and
// subscribe to events through the EventBus using the existing MessageBroker API.
type EventBusBridge struct {
	name          string
	eventBus      *eventbus.EventBusModule
	subscriptions map[string]eventbus.Subscription
	mu            sync.RWMutex
}

// NewEventBusBridge creates a new EventBusBridge with the given name.
func NewEventBusBridge(name string) *EventBusBridge {
	return &EventBusBridge{
		name:          name,
		subscriptions: make(map[string]eventbus.Subscription),
	}
}

// Name returns the bridge's service name.
func (b *EventBusBridge) Name() string {
	return b.name
}

// Init registers the bridge as a service in the application's service registry.
// It does not look up the EventBus here; that is done via SetEventBus or
// InitFromApp after the application has been fully initialized.
func (b *EventBusBridge) Init(app modular.Application) error {
	reg := app.SvcRegistry()
	reg[b.name] = b
	return nil
}

// SetEventBus injects the EventBusModule directly. This is useful when the
// engine already has a reference to the EventBus after app.Init().
func (b *EventBusBridge) SetEventBus(eb *eventbus.EventBusModule) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.eventBus = eb
}

// InitFromApp looks up the EventBusModule from the application's service
// registry using the well-known service name "eventbus.provider".
func (b *EventBusBridge) InitFromApp(app modular.Application) error {
	var eb *eventbus.EventBusModule
	if err := app.GetService(eventbus.ServiceName, &eb); err != nil {
		return fmt.Errorf("looking up eventbus service: %w", err)
	}
	b.SetEventBus(eb)
	return nil
}

// Producer returns the bridge itself, which implements MessageProducer.
func (b *EventBusBridge) Producer() MessageProducer {
	return b
}

// Consumer returns the bridge itself, which implements MessageConsumer.
func (b *EventBusBridge) Consumer() MessageConsumer {
	return b
}

// SendMessage publishes a message to the EventBus. The message bytes are
// unmarshalled from JSON into an interface{} payload. If unmarshalling fails,
// the raw bytes are published as the payload. Returns nil (no-op) if no
// EventBus has been set.
func (b *EventBusBridge) SendMessage(topic string, message []byte) error {
	b.mu.RLock()
	eb := b.eventBus
	b.mu.RUnlock()

	if eb == nil {
		return nil
	}

	var payload any
	if err := json.Unmarshal(message, &payload); err != nil {
		payload = message
	}

	return eb.Publish(context.Background(), topic, payload)
}

// Subscribe registers a MessageHandler to receive events from the EventBus on
// the given topic. Events are marshalled to JSON before being passed to the
// handler. Returns nil (no-op) if no EventBus has been set.
func (b *EventBusBridge) Subscribe(topic string, handler MessageHandler) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.eventBus == nil {
		return nil
	}

	eventHandler := func(_ context.Context, event eventbus.Event) error {
		jsonBytes, err := json.Marshal(event.Payload)
		if err != nil {
			return fmt.Errorf("marshalling event payload: %w", err)
		}
		return handler.HandleMessage(jsonBytes)
	}

	sub, err := b.eventBus.Subscribe(context.Background(), topic, eventHandler)
	if err != nil {
		return fmt.Errorf("subscribing to eventbus topic %s: %w", topic, err)
	}

	b.subscriptions[topic] = sub
	return nil
}

// Unsubscribe cancels the subscription for the given topic and removes it.
func (b *EventBusBridge) Unsubscribe(topic string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	sub, exists := b.subscriptions[topic]
	if !exists {
		return nil
	}

	if err := sub.Cancel(); err != nil {
		return fmt.Errorf("cancelling subscription for topic %s: %w", topic, err)
	}

	delete(b.subscriptions, topic)
	return nil
}

// Start is a no-op; the EventBus lifecycle is managed externally.
func (b *EventBusBridge) Start(_ context.Context) error {
	return nil
}

// Stop cancels all active subscriptions and clears the subscription map.
func (b *EventBusBridge) Stop(_ context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	for topic, sub := range b.subscriptions {
		if err := sub.Cancel(); err != nil {
			return fmt.Errorf("cancelling subscription for topic %s during stop: %w", topic, err)
		}
	}
	b.subscriptions = make(map[string]eventbus.Subscription)
	return nil
}
