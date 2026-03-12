package module

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/modular/modules/eventbus/v2"
	"github.com/google/uuid"
)

// EventEncryptionConfig holds the field-level encryption configuration for event publishing.
type EventEncryptionConfig struct {
	// Provider is the encryption provider: "aes" or "envelope" (default: "aes").
	Provider string
	// KeyID is the encryption key identifier. Supports "${ENV_VAR}" env-var references.
	KeyID string
	// Fields lists the payload field names to encrypt.
	Fields []string
	// Algorithm is the encryption algorithm (currently only "AES-256-GCM" is supported).
	Algorithm string
}

// EventPublishStep publishes events to a messaging broker, EventPublisher, or EventBus
// from pipeline execution. It supports CloudEvents envelope format and multiple
// provider backends including external plugins (e.g., Bento).
type EventPublishStep struct {
	name       string
	topic      string
	payload    map[string]any
	headers    map[string]string
	eventType  string
	source     string
	broker     string // service name for a MessageBroker or EventPublisher
	app        modular.Application
	tmpl       *TemplateEngine
	encryption *EventEncryptionConfig
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

		// Parse optional encryption config block.
		if encCfg, ok := config["encryption"].(map[string]any); ok {
			enc := &EventEncryptionConfig{
				Provider:  "aes",
				Algorithm: "AES-256-GCM",
			}
			if p, ok := encCfg["provider"].(string); ok && p != "" {
				enc.Provider = p
			}
			if k, ok := encCfg["key_id"].(string); ok {
				enc.KeyID = k
			}
			if a, ok := encCfg["algorithm"].(string); ok && a != "" {
				enc.Algorithm = a
			}
			if fields, ok := encCfg["fields"].([]any); ok {
				for _, f := range fields {
					if fs, ok := f.(string); ok && fs != "" {
						enc.Fields = append(enc.Fields, fs)
					}
				}
			} else if fields, ok := encCfg["fields"].([]string); ok {
				enc.Fields = fields
			}
			if len(enc.Fields) > 0 && enc.KeyID != "" {
				step.encryption = enc
			}
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
			return nil, fmt.Errorf("event_publish step %q: failed to resolve source: %w", s.name, err)
		}
	}

	// Apply field-level encryption if configured.
	var encMeta *eventEncryptionMeta
	if s.encryption != nil {
		resolvedPayload, encMeta, err = applyEventFieldEncryption(resolvedPayload, s.encryption)
		if err != nil {
			return nil, fmt.Errorf("event_publish step %q: encryption failed: %w", s.name, err)
		}
	}

	// Build event envelope for broker/EventPublisher paths
	event := s.buildEventEnvelope(resolvedPayload, resolvedHeaders, resolvedSource, encMeta)

	if s.broker != "" {
		// Try EventPublisher interface first (supports external plugins like Bento)
		if pub := s.tryGetEventPublisher(); pub != nil {
			return s.publishViaEventPublisher(ctx, resolvedTopic, event, pub)
		}
		// Fall back to MessageBroker interface
		return s.publishViaBroker(resolvedTopic, event)
	}

	// The EventBus module builds its own CloudEvents envelope internally,
	// so pass the resolved payload directly (not the pre-built envelope).
	return s.publishViaEventBus(ctx, resolvedTopic, resolvedPayload)
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

// buildEventEnvelope wraps the payload in an envelope for publishing.
// When both event_type and source are configured, a full CloudEvents 1.0-compatible
// envelope is emitted with specversion, type, source, id, time, and data fields.
// When only headers are provided (without event_type/source), the payload is
// wrapped as {data, headers} without adding CloudEvents-required attributes.
// Encryption metadata (if present) is added as CloudEvents extension attributes.
func (s *EventPublishStep) buildEventEnvelope(payload map[string]any, headers map[string]string, resolvedSource string, encMeta *eventEncryptionMeta) map[string]any {
	if s.eventType == "" && resolvedSource == "" && len(headers) == 0 && encMeta == nil {
		return payload
	}
	envelope := map[string]any{
		"data": payload,
	}
	// Only emit a CloudEvents envelope when both required attributes are present.
	if s.eventType != "" && resolvedSource != "" {
		envelope["specversion"] = "1.0"
		envelope["id"] = uuid.New().String()
		envelope["time"] = time.Now().UTC().Format(time.RFC3339)
		envelope["type"] = s.eventType
		envelope["source"] = resolvedSource
	}
	if len(headers) > 0 {
		envelope["headers"] = headers
	}
	// Embed encryption metadata as CloudEvents extension attributes.
	if encMeta != nil {
		envelope["encryption"] = encMeta.Algorithm
		envelope["keyid"] = encMeta.KeyID
		envelope["encrypteddek"] = encMeta.EncryptedDEK
		envelope["encryptedfields"] = strings.Join(encMeta.EncryptedFields, ",")
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
