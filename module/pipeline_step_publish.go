package module

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/CrisisTextLine/modular"
	"github.com/CrisisTextLine/modular/modules/eventbus/v2"
)

// PublishStep publishes data to an EventBus topic or a MessageBroker.
type PublishStep struct {
	name    string
	topic   string
	payload map[string]any
	broker  string // optional service name for a MessageBroker
	app     modular.Application
	tmpl    *TemplateEngine
}

// NewPublishStepFactory returns a StepFactory that creates PublishStep instances.
func NewPublishStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		topic, _ := config["topic"].(string)
		if topic == "" {
			return nil, fmt.Errorf("publish step %q: 'topic' is required", name)
		}

		payload, _ := config["payload"].(map[string]any)
		broker, _ := config["broker"].(string)

		return &PublishStep{
			name:    name,
			topic:   topic,
			payload: payload,
			broker:  broker,
			app:     app,
			tmpl:    NewTemplateEngine(),
		}, nil
	}
}

// Name returns the step name.
func (s *PublishStep) Name() string { return s.name }

// Execute resolves the payload templates and publishes to the configured target.
func (s *PublishStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	// Resolve the topic template
	resolvedTopic, err := s.tmpl.Resolve(s.topic, pc)
	if err != nil {
		return nil, fmt.Errorf("publish step %q: failed to resolve topic: %w", s.name, err)
	}

	// Resolve the payload, defaulting to pc.Current if no payload configured
	var resolvedPayload map[string]any
	if s.payload != nil {
		resolvedPayload, err = s.tmpl.ResolveMap(s.payload, pc)
		if err != nil {
			return nil, fmt.Errorf("publish step %q: failed to resolve payload: %w", s.name, err)
		}
	} else {
		resolvedPayload = pc.Current
	}

	// Try broker first if specified
	if s.broker != "" {
		return s.publishViaBroker(ctx, resolvedTopic, resolvedPayload)
	}

	// Try EventBus
	return s.publishViaEventBus(ctx, resolvedTopic, resolvedPayload)
}

// publishViaBroker sends a message through a MessageBroker service.
func (s *PublishStep) publishViaBroker(_ context.Context, topic string, payload map[string]any) (*StepResult, error) {
	var broker MessageBroker
	if err := s.app.GetService(s.broker, &broker); err != nil {
		slog.Warn("publish step: broker service not found, skipping publish",
			"step", s.name, "broker", s.broker, "error", err)
		return &StepResult{Output: map[string]any{"published": false, "reason": "broker not found"}}, nil
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("publish step %q: failed to marshal payload: %w", s.name, err)
	}

	if err := broker.Producer().SendMessage(topic, data); err != nil {
		return nil, fmt.Errorf("publish step %q: failed to publish via broker: %w", s.name, err)
	}

	return &StepResult{Output: map[string]any{"published": true, "topic": topic}}, nil
}

// publishViaEventBus sends an event through the modular EventBus.
func (s *PublishStep) publishViaEventBus(ctx context.Context, topic string, payload map[string]any) (*StepResult, error) {
	var eb *eventbus.EventBusModule
	if err := s.app.GetService("eventbus.provider", &eb); err != nil || eb == nil {
		slog.Warn("publish step: eventbus not available, skipping publish",
			"step", s.name, "error", err)
		return &StepResult{Output: map[string]any{"published": false, "reason": "eventbus not available"}}, nil
	}

	if err := eb.Publish(ctx, topic, payload); err != nil {
		return nil, fmt.Errorf("publish step %q: failed to publish to eventbus: %w", s.name, err)
	}

	return &StepResult{Output: map[string]any{"published": true, "topic": topic}}, nil
}
