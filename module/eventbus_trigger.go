package module

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"

	"github.com/CrisisTextLine/modular"
	"github.com/CrisisTextLine/modular/modules/eventbus"
)

const (
	// EventBusTriggerName is the standard name for the EventBus trigger.
	EventBusTriggerName = "trigger.eventbus"
)

// EventBusTriggerSubscription defines a single subscription that the trigger
// listens to on the EventBus and maps to a workflow execution.
type EventBusTriggerSubscription struct {
	Topic    string         `json:"topic" yaml:"topic"`
	Event    string         `json:"event,omitempty" yaml:"event,omitempty"`
	Workflow string         `json:"workflow" yaml:"workflow"`
	Action   string         `json:"action" yaml:"action"`
	Async    bool           `json:"async,omitempty" yaml:"async,omitempty"`
	Params   map[string]any `json:"params,omitempty" yaml:"params,omitempty"`
}

// EventBusTrigger implements the Trigger interface and starts workflows in
// response to events published on the EventBus.
type EventBusTrigger struct {
	name          string
	namespace     ModuleNamespaceProvider
	subscriptions []EventBusTriggerSubscription
	eventBus      *eventbus.EventBusModule
	engine        WorkflowEngine
	activeSubs    []eventbus.Subscription
}

// NewEventBusTrigger creates a new EventBus trigger with default namespace.
func NewEventBusTrigger() *EventBusTrigger {
	return NewEventBusTriggerWithNamespace(nil)
}

// NewEventBusTriggerWithNamespace creates a new EventBus trigger with namespace support.
func NewEventBusTriggerWithNamespace(namespace ModuleNamespaceProvider) *EventBusTrigger {
	if namespace == nil {
		namespace = NewStandardNamespace("", "")
	}
	return &EventBusTrigger{
		name:          namespace.FormatName(EventBusTriggerName),
		namespace:     namespace,
		subscriptions: make([]EventBusTriggerSubscription, 0),
		activeSubs:    make([]eventbus.Subscription, 0),
	}
}

// Name returns the trigger name.
func (t *EventBusTrigger) Name() string {
	return t.name
}

// Init registers the trigger as a service.
func (t *EventBusTrigger) Init(app modular.Application) error {
	return app.RegisterService(t.name, t)
}

// Configure parses the trigger config and resolves the EventBus and engine
// services from the application.
func (t *EventBusTrigger) Configure(app modular.Application, triggerConfig any) error {
	config, ok := triggerConfig.(map[string]any)
	if !ok {
		return fmt.Errorf("invalid eventbus trigger configuration format")
	}

	subsConfig, ok := config["subscriptions"].([]any)
	if !ok {
		return fmt.Errorf("subscriptions not found in eventbus trigger configuration")
	}

	// Resolve EventBusModule.
	var eb *eventbus.EventBusModule
	if err := app.GetService("eventbus.provider", &eb); err != nil || eb == nil {
		return fmt.Errorf("eventbus.provider service not found")
	}
	t.eventBus = eb

	// Resolve WorkflowEngine.
	var engine WorkflowEngine
	engineNames := []string{"workflowEngine", "engine"}
	for _, name := range engineNames {
		var svc any
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
	t.engine = engine

	// Parse subscriptions.
	for i, sc := range subsConfig {
		subMap, ok := sc.(map[string]any)
		if !ok {
			return fmt.Errorf("invalid subscription configuration at index %d", i)
		}

		topic, _ := subMap["topic"].(string)
		event, _ := subMap["event"].(string)
		workflow, _ := subMap["workflow"].(string)
		action, _ := subMap["action"].(string)
		async, _ := subMap["async"].(bool)
		params, _ := subMap["params"].(map[string]any)

		if topic == "" || workflow == "" || action == "" {
			return fmt.Errorf("incomplete subscription at index %d: topic, workflow, and action are required", i)
		}

		t.subscriptions = append(t.subscriptions, EventBusTriggerSubscription{
			Topic:    topic,
			Event:    event,
			Workflow: workflow,
			Action:   action,
			Async:    async,
			Params:   params,
		})
	}

	return nil
}

// Start subscribes to the configured EventBus topics.
func (t *EventBusTrigger) Start(ctx context.Context) error {
	// If no subscriptions are configured, nothing to do
	if len(t.subscriptions) == 0 {
		return nil
	}

	if t.eventBus == nil {
		return fmt.Errorf("event bus not configured for eventbus trigger")
	}
	if t.engine == nil {
		return fmt.Errorf("workflow engine not configured for eventbus trigger")
	}

	for _, sub := range t.subscriptions {
		handler := t.createHandler(sub)

		var subscription eventbus.Subscription
		var err error
		if sub.Async {
			subscription, err = t.eventBus.SubscribeAsync(ctx, sub.Topic, handler)
		} else {
			subscription, err = t.eventBus.Subscribe(ctx, sub.Topic, handler)
		}
		if err != nil {
			return fmt.Errorf("failed to subscribe to topic %q: %w", sub.Topic, err)
		}
		t.activeSubs = append(t.activeSubs, subscription)
	}

	return nil
}

// Stop cancels all active EventBus subscriptions.
func (t *EventBusTrigger) Stop(_ context.Context) error {
	for _, sub := range t.activeSubs {
		_ = sub.Cancel()
	}
	t.activeSubs = t.activeSubs[:0]
	return nil
}

// SetEventBusAndEngine allows directly setting the EventBus and engine for testing.
func (t *EventBusTrigger) SetEventBusAndEngine(eb *eventbus.EventBusModule, engine WorkflowEngine) {
	t.eventBus = eb
	t.engine = engine
}

// createHandler returns an EventHandler for the given subscription.
func (t *EventBusTrigger) createHandler(sub EventBusTriggerSubscription) eventbus.EventHandler {
	return func(ctx context.Context, ev eventbus.Event) error {
		// Extract payload as map[string]interface{}.
		data, ok := ev.Payload.(map[string]any)
		if !ok {
			// JSON round-trip for non-map payloads.
			raw, err := json.Marshal(ev.Payload)
			if err != nil {
				return fmt.Errorf("failed to marshal event payload: %w", err)
			}
			data = make(map[string]any)
			if err := json.Unmarshal(raw, &data); err != nil {
				return fmt.Errorf("failed to unmarshal event payload: %w", err)
			}
		}

		// Event type filtering.
		if sub.Event != "" {
			eventType, _ := data["type"].(string)
			if eventType == "" {
				eventType, _ = data["eventType"].(string)
			}
			if eventType != sub.Event {
				return nil // not a match
			}
		}

		// Merge static params.
		maps.Copy(data, sub.Params)

		return t.engine.TriggerWorkflow(ctx, sub.Workflow, sub.Action, data)
	}
}
