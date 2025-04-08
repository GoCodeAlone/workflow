package module

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/GoCodeAlone/modular"
)

const (
	// EventTriggerName is the standard name for event triggers
	EventTriggerName = "trigger.event"
)

// EventTriggerConfig represents the configuration for an event trigger
type EventTriggerConfig struct {
	Subscriptions []EventTriggerSubscription `json:"subscriptions" yaml:"subscriptions"`
}

// EventTriggerSubscription represents a subscription to a message topic
type EventTriggerSubscription struct {
	Topic    string                 `json:"topic" yaml:"topic"`
	Event    string                 `json:"event" yaml:"event"`
	Workflow string                 `json:"workflow" yaml:"workflow"`
	Action   string                 `json:"action" yaml:"action"`
	Params   map[string]interface{} `json:"params,omitempty" yaml:"params,omitempty"`
}

// EventTrigger implements a trigger that starts workflows from messaging events
type EventTrigger struct {
	name          string
	namespace     ModuleNamespaceProvider
	subscriptions []EventTriggerSubscription
	broker        MessageBroker
	engine        WorkflowEngine
}

// NewEventTrigger creates a new event trigger
func NewEventTrigger() *EventTrigger {
	return NewEventTriggerWithNamespace(nil)
}

// NewEventTriggerWithNamespace creates a new event trigger with namespace support
func NewEventTriggerWithNamespace(namespace ModuleNamespaceProvider) *EventTrigger {
	// Default to standard namespace if none provided
	if namespace == nil {
		namespace = NewStandardNamespace("", "")
	}

	return &EventTrigger{
		name:          namespace.FormatName(EventTriggerName),
		namespace:     namespace,
		subscriptions: make([]EventTriggerSubscription, 0),
	}
}

// Name returns the name of this trigger
func (t *EventTrigger) Name() string {
	return t.name
}

// Init initializes the trigger
func (t *EventTrigger) Init(app modular.Application) error {
	return app.RegisterService(t.name, t)
}

// Start starts the trigger
func (t *EventTrigger) Start(ctx context.Context) error {
	// If no broker is set, we can't start
	if t.broker == nil {
		return fmt.Errorf("message broker not configured for event trigger")
	}

	// If no engine is set, we can't start
	if t.engine == nil {
		return fmt.Errorf("workflow engine not configured for event trigger")
	}

	// Subscribe to all topics
	for _, sub := range t.subscriptions {
		handler := t.createHandler(sub)
		if err := t.broker.Subscribe(sub.Topic, handler); err != nil {
			return fmt.Errorf("failed to subscribe to topic '%s': %w", sub.Topic, err)
		}
	}

	return nil
}

// Stop stops the trigger
func (t *EventTrigger) Stop(ctx context.Context) error {
	// Nothing specific to do here as the message broker will handle unsubscribing
	return nil
}

// Configure sets up the trigger from configuration
func (t *EventTrigger) Configure(app modular.Application, triggerConfig interface{}) error {
	// Convert the generic config to event trigger config
	config, ok := triggerConfig.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid event trigger configuration format")
	}

	// Extract subscriptions from configuration
	subsConfig, ok := config["subscriptions"].([]interface{})
	if !ok {
		return fmt.Errorf("subscriptions not found in event trigger configuration")
	}

	// Find the message broker
	var broker MessageBroker
	brokerNames := []string{"messageBroker", "eventBroker", "broker"}

	for _, name := range brokerNames {
		var svc interface{}
		if err := app.GetService(name, &svc); err == nil && svc != nil {
			if b, ok := svc.(MessageBroker); ok {
				broker = b
				break
			}
		}
	}

	if broker == nil {
		return fmt.Errorf("message broker not found")
	}

	// Find the workflow engine
	var engine WorkflowEngine
	engineNames := []string{"workflowEngine", "engine"}

	for _, name := range engineNames {
		var svc interface{}
		if err := app.GetService(name, &svc); err == nil && svc != nil {
			if e, ok := svc.(WorkflowEngine); ok {
				engine = e
				break
			}
		}
	}

	if engine == nil {
		return fmt.Errorf("workflow engine not found")
	}

	// Store broker and engine references
	t.broker = broker
	t.engine = engine

	// Parse subscriptions
	for i, sc := range subsConfig {
		subMap, ok := sc.(map[string]interface{})
		if !ok {
			return fmt.Errorf("invalid subscription configuration at index %d", i)
		}

		topic, _ := subMap["topic"].(string)
		event, _ := subMap["event"].(string)
		workflow, _ := subMap["workflow"].(string)
		action, _ := subMap["action"].(string)

		if topic == "" || workflow == "" || action == "" {
			return fmt.Errorf("incomplete subscription configuration at index %d: topic, workflow, and action are required", i)
		}

		// Get optional params
		params, _ := subMap["params"].(map[string]interface{})

		// Add the subscription
		t.subscriptions = append(t.subscriptions, EventTriggerSubscription{
			Topic:    topic,
			Event:    event,
			Workflow: workflow,
			Action:   action,
			Params:   params,
		})
	}

	return nil
}

// SetBrokerAndEngine allows directly setting the broker and engine for testing
func (t *EventTrigger) SetBrokerAndEngine(broker MessageBroker, engine WorkflowEngine) {
	t.broker = broker
	t.engine = engine
}

// createHandler creates a message handler for a specific subscription
func (t *EventTrigger) createHandler(sub EventTriggerSubscription) MessageHandler {
	// Create a handler function that will be called when a message is received
	handlerFn := func(msg []byte) error {
		// Parse the message
		var eventData map[string]interface{}
		if err := json.Unmarshal(msg, &eventData); err != nil {
			return fmt.Errorf("failed to parse message: %w", err)
		}

		// Check if this matches the expected event type
		if sub.Event != "" {
			eventType, _ := eventData["type"].(string)
			if eventType == "" {
				eventType, _ = eventData["eventType"].(string)
			}

			// Skip if event type doesn't match
			if eventType != sub.Event {
				return nil
			}
		}

		// Create the data to pass to the workflow
		data := make(map[string]interface{})

		// Include all event data
		for k, v := range eventData {
			data[k] = v
		}

		// Include the original message
		data["originalMessage"] = string(msg)

		// Add any static params from the subscription configuration
		for k, v := range sub.Params {
			data[k] = v
		}

		// Call the workflow engine to trigger the workflow
		ctx := context.Background()
		return t.engine.TriggerWorkflow(ctx, sub.Workflow, sub.Action, data)
	}

	// Create a message handler from our function
	return &MessageHandlerAdapter{handlerFn}
}

// MessageHandlerAdapter adapts a function to the MessageHandler interface
type MessageHandlerAdapter struct {
	handlerFunc func([]byte) error
}

// HandleMessage implements the MessageHandler interface
func (a *MessageHandlerAdapter) HandleMessage(msg []byte) error {
	return a.handlerFunc(msg)
}
