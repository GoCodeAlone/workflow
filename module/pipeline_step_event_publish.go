package module

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/CrisisTextLine/modular"
	"github.com/CrisisTextLine/modular/modules/eventbus/v2"
)

// EventPublishStep publishes events to a messaging broker or EventBus from pipeline execution.
type EventPublishStep struct {
	name      string
	topic     string
	payload   map[string]any
	headers   map[string]string
	eventType string
	broker    string // optional service name for a MessageBroker
	app       modular.Application
	tmpl      *TemplateEngine
}

// NewEventPublishStepFactory returns a StepFactory that creates EventPublishStep instances.
func NewEventPublishStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		topic, _ := config["topic"].(string)
		if topic == "" {
			return nil, fmt.Errorf("event_publish step %q: 'topic' is required", name)
		}

		step := &EventPublishStep{
			name:  name,
			topic: topic,
			app:   app,
			tmpl:  NewTemplateEngine(),
		}

		if payload, ok := config["payload"].(map[string]any); ok {
			step.payload = payload
		}

		if headers, ok := config["headers"].(map[string]any); ok {
			step.headers = make(map[string]string, len(headers))
			for k, v := range headers {
				if s, ok := v.(string); ok {
					step.headers[k] = s
				}
			}
		}

		step.eventType, _ = config["event_type"].(string)
		step.broker, _ = config["broker"].(string)

		return step, nil
	}
}

// Name returns the step name.
func (s *EventPublishStep) Name() string { return s.name }

// Execute resolves templates in topic, payload, and headers then publishes the event.
func (s *EventPublishStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	resolvedTopic, err := s.tmpl.Resolve(s.topic, pc)
	if err != nil {
		return nil, fmt.Errorf("event_publish step %q: failed to resolve topic: %w", s.name, err)
	}

	var resolvedPayload map[string]any
	if s.payload != nil {
		resolvedPayload, err = s.tmpl.ResolveMap(s.payload, pc)
		if err != nil {
			return nil, fmt.Errorf("event_publish step %q: failed to resolve payload: %w", s.name, err)
		}
	} else {
		resolvedPayload = pc.Current
	}

	resolvedHeaders := make(map[string]string, len(s.headers))
	for k, v := range s.headers {
		resolved, resolveErr := s.tmpl.Resolve(v, pc)
		if resolveErr != nil {
			resolvedHeaders[k] = v
		} else {
			resolvedHeaders[k] = resolved
		}
	}

	// Build event envelope when event_type or headers are present
	event := s.buildEventEnvelope(resolvedPayload, resolvedHeaders)

	if s.broker != "" {
		return s.publishViaBroker(resolvedTopic, event)
	}

	return s.publishViaEventBus(ctx, resolvedTopic, event)
}

// buildEventEnvelope wraps the payload with event_type and headers metadata when present.
func (s *EventPublishStep) buildEventEnvelope(payload map[string]any, headers map[string]string) map[string]any {
	if s.eventType == "" && len(headers) == 0 {
		return payload
	}
	envelope := map[string]any{
		"data": payload,
	}
	if s.eventType != "" {
		envelope["type"] = s.eventType
	}
	if len(headers) > 0 {
		envelope["headers"] = headers
	}
	return envelope
}

func (s *EventPublishStep) publishViaBroker(topic string, payload map[string]any) (*StepResult, error) {
	var broker MessageBroker
	if err := s.app.GetService(s.broker, &broker); err != nil {
		return nil, fmt.Errorf("event_publish step %q: broker service %q not found: %w", s.name, s.broker, err)
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("event_publish step %q: failed to marshal payload: %w", s.name, err)
	}

	if err := broker.Producer().SendMessage(topic, data); err != nil {
		return nil, fmt.Errorf("event_publish step %q: failed to publish via broker: %w", s.name, err)
	}

	return &StepResult{Output: map[string]any{"published": true, "topic": topic}}, nil
}

func (s *EventPublishStep) publishViaEventBus(ctx context.Context, topic string, payload map[string]any) (*StepResult, error) {
	var eb *eventbus.EventBusModule
	if err := s.app.GetService("eventbus.provider", &eb); err != nil || eb == nil {
		return nil, fmt.Errorf("event_publish step %q: no broker configured and eventbus not available", s.name)
	}

	if err := eb.Publish(ctx, topic, payload); err != nil {
		return nil, fmt.Errorf("event_publish step %q: failed to publish to eventbus: %w", s.name, err)
	}

	return &StepResult{Output: map[string]any{"published": true, "topic": topic}}, nil
}
