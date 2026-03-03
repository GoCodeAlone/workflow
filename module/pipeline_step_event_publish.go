package module

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/CrisisTextLine/modular/modules/eventbus/v2"
	"github.com/google/uuid"
)

// EventPublishStep publishes events to a messaging broker, EventPublisher, or EventBus
// from pipeline execution. It supports CloudEvents envelope format and multiple
// provider backends including external plugins (e.g., Bento).
type EventPublishStep struct {
	name      string
	topic     string
	payload   map[string]any
	headers   map[string]string
	eventType string
	source    string
	broker    string // service name for a MessageBroker or EventPublisher
	app       modular.Application
	tmpl      *TemplateEngine
}

// NewEventPublishStepFactory returns a StepFactory that creates EventPublishStep instances.
func NewEventPublishStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		// Support "stream" as alias for "topic"
		topic, _ := config["topic"].(string)
		if topic == "" {
			topic, _ = config["stream"].(string)
		}
		if topic == "" {
			return nil, fmt.Errorf("event_publish step %q: 'topic' is required", name)
		}

		step := &EventPublishStep{
			name:  name,
			topic: topic,
			app:   app,
			tmpl:  NewTemplateEngine(),
		}

		// Support "data" as alias for "payload"
		if payload, ok := config["payload"].(map[string]any); ok {
			step.payload = payload
		} else if data, ok := config["data"].(map[string]any); ok {
			step.payload = data
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
		step.source, _ = config["source"].(string)

		// Support "provider" as alias for "broker"
		step.broker, _ = config["broker"].(string)
		if step.broker == "" {
			step.broker, _ = config["provider"].(string)
		}

		return step, nil
	}
}

// Name returns the step name.
func (s *EventPublishStep) Name() string { return s.name }

// Execute resolves templates in topic, payload, source, and headers then publishes the event.
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

	// Resolve source template if configured
	var resolvedSource string
	if s.source != "" {
		resolvedSource, err = s.tmpl.Resolve(s.source, pc)
		if err != nil {
			resolvedSource = s.source
		}
	}

	// Build CloudEvents envelope when event_type, source, or headers are present
	event := s.buildEventEnvelope(resolvedPayload, resolvedHeaders, resolvedSource)

	if s.broker != "" {
		// Try EventPublisher interface first (supports external plugins like Bento)
		if pub := s.tryGetEventPublisher(); pub != nil {
			return s.publishViaEventPublisher(ctx, resolvedTopic, event, pub)
		}
		// Fall back to MessageBroker interface
		return s.publishViaBroker(resolvedTopic, event)
	}

	return s.publishViaEventBus(ctx, resolvedTopic, event)
}

// tryGetEventPublisher attempts to resolve the broker service as an EventPublisher.
// Returns nil if the service does not implement EventPublisher.
func (s *EventPublishStep) tryGetEventPublisher() (pub EventPublisher) {
	defer func() {
		if r := recover(); r != nil {
			pub = nil
		}
	}()
	if err := s.app.GetService(s.broker, &pub); err != nil || pub == nil {
		return nil
	}
	return pub
}

// buildEventEnvelope wraps the payload in a CloudEvents-compatible envelope
// when event_type or source is configured. The envelope includes specversion,
// type, source, id, time, and data fields per the CloudEvents specification.
func (s *EventPublishStep) buildEventEnvelope(payload map[string]any, headers map[string]string, resolvedSource string) map[string]any {
	if s.eventType == "" && resolvedSource == "" && len(headers) == 0 {
		return payload
	}
	envelope := map[string]any{
		"data": payload,
	}
	if s.eventType != "" || resolvedSource != "" {
		envelope["specversion"] = "1.0"
		envelope["id"] = uuid.New().String()
		envelope["time"] = time.Now().UTC().Format(time.RFC3339)
	}
	if s.eventType != "" {
		envelope["type"] = s.eventType
	}
	if resolvedSource != "" {
		envelope["source"] = resolvedSource
	}
	if len(headers) > 0 {
		envelope["headers"] = headers
	}
	return envelope
}

func (s *EventPublishStep) publishViaEventPublisher(ctx context.Context, topic string, event map[string]any, pub EventPublisher) (*StepResult, error) {
	if err := pub.PublishEvent(ctx, topic, event); err != nil {
		return nil, fmt.Errorf("event_publish step %q: failed to publish via provider: %w", s.name, err)
	}
	return &StepResult{Output: map[string]any{"published": true, "topic": topic}}, nil
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
