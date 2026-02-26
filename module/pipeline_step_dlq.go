package module

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/CrisisTextLine/modular/modules/eventbus/v2"
)

// DLQSendStep sends a failed message to a dead letter queue topic.
type DLQSendStep struct {
	name          string
	topic         string
	originalTopic string
	errTemplate   string
	payload       map[string]any
	broker        string
	app           modular.Application
	tmpl          *TemplateEngine
}

// NewDLQSendStepFactory returns a StepFactory that creates DLQSendStep instances.
func NewDLQSendStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		topic, _ := config["topic"].(string)
		if topic == "" {
			return nil, fmt.Errorf("dlq_send step %q: 'topic' is required", name)
		}

		step := &DLQSendStep{
			name:  name,
			topic: topic,
			app:   app,
			tmpl:  NewTemplateEngine(),
		}

		step.originalTopic, _ = config["original_topic"].(string)
		step.errTemplate, _ = config["error"].(string)
		step.broker, _ = config["broker"].(string)

		if payload, ok := config["payload"].(map[string]any); ok {
			step.payload = payload
		}

		return step, nil
	}
}

// Name returns the step name.
func (s *DLQSendStep) Name() string { return s.name }

// Execute sends the current message to the DLQ topic with error metadata.
func (s *DLQSendStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	resolvedTopic, err := s.tmpl.Resolve(s.topic, pc)
	if err != nil {
		return nil, fmt.Errorf("dlq_send step %q: failed to resolve topic: %w", s.name, err)
	}

	var resolvedPayload map[string]any
	if s.payload != nil {
		resolvedPayload, err = s.tmpl.ResolveMap(s.payload, pc)
		if err != nil {
			return nil, fmt.Errorf("dlq_send step %q: failed to resolve payload: %w", s.name, err)
		}
	} else {
		resolvedPayload = pc.Current
	}

	errMsg := ""
	if s.errTemplate != "" {
		errMsg, _ = s.tmpl.Resolve(s.errTemplate, pc)
	}

	envelope := map[string]any{
		"payload": resolvedPayload,
		"sent_at": time.Now().UTC().Format(time.RFC3339),
	}
	if s.originalTopic != "" {
		envelope["original_topic"] = s.originalTopic
	}
	if errMsg != "" {
		envelope["error"] = errMsg
	}

	if s.broker != "" {
		return s.sendViaBroker(resolvedTopic, envelope)
	}
	return s.sendViaEventBus(ctx, resolvedTopic, envelope)
}

func (s *DLQSendStep) sendViaBroker(topic string, envelope map[string]any) (*StepResult, error) {
	var broker MessageBroker
	if err := s.app.GetService(s.broker, &broker); err != nil {
		return nil, fmt.Errorf("dlq_send step %q: broker service %q not found: %w", s.name, s.broker, err)
	}

	data, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("dlq_send step %q: failed to marshal envelope: %w", s.name, err)
	}

	if err := broker.Producer().SendMessage(topic, data); err != nil {
		return nil, fmt.Errorf("dlq_send step %q: failed to send to DLQ via broker: %w", s.name, err)
	}

	return &StepResult{Output: map[string]any{"dlq_sent": true, "topic": topic}}, nil
}

func (s *DLQSendStep) sendViaEventBus(ctx context.Context, topic string, envelope map[string]any) (*StepResult, error) {
	var eb *eventbus.EventBusModule
	if err := s.app.GetService("eventbus.provider", &eb); err != nil || eb == nil {
		return nil, fmt.Errorf("dlq_send step %q: no broker configured and eventbus not available", s.name)
	}

	if err := eb.Publish(ctx, topic, envelope); err != nil {
		return nil, fmt.Errorf("dlq_send step %q: failed to publish to eventbus: %w", s.name, err)
	}

	return &StepResult{Output: map[string]any{"dlq_sent": true, "topic": topic}}, nil
}

// DLQReplayStep replays messages from a DLQ topic back to the original topic.
// When used in a pipeline triggered by a DLQ consumer, pc.Current holds the DLQ
// message (or a "messages" array for batch replay).
type DLQReplayStep struct {
	name        string
	dlqTopic    string
	targetTopic string
	maxMessages int
	broker      string
	app         modular.Application
	tmpl        *TemplateEngine
}

// NewDLQReplayStepFactory returns a StepFactory that creates DLQReplayStep instances.
func NewDLQReplayStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		dlqTopic, _ := config["dlq_topic"].(string)
		if dlqTopic == "" {
			return nil, fmt.Errorf("dlq_replay step %q: 'dlq_topic' is required", name)
		}

		targetTopic, _ := config["target_topic"].(string)
		if targetTopic == "" {
			return nil, fmt.Errorf("dlq_replay step %q: 'target_topic' is required", name)
		}

		maxMessages := 100
		if v, ok := config["max_messages"]; ok {
			switch val := v.(type) {
			case int:
				maxMessages = val
			case float64:
				maxMessages = int(val)
			}
		}
		if maxMessages <= 0 {
			maxMessages = 100
		}

		broker, _ := config["broker"].(string)

		return &DLQReplayStep{
			name:        name,
			dlqTopic:    dlqTopic,
			targetTopic: targetTopic,
			maxMessages: maxMessages,
			broker:      broker,
			app:         app,
			tmpl:        NewTemplateEngine(),
		}, nil
	}
}

// Name returns the step name.
func (s *DLQReplayStep) Name() string { return s.name }

// Execute replays messages from the DLQ to the target topic.
func (s *DLQReplayStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	resolvedTarget, err := s.tmpl.Resolve(s.targetTopic, pc)
	if err != nil {
		return nil, fmt.Errorf("dlq_replay step %q: failed to resolve target_topic: %w", s.name, err)
	}

	messages := s.collectMessages(pc)
	if len(messages) > s.maxMessages {
		messages = messages[:s.maxMessages]
	}

	if len(messages) == 0 {
		return &StepResult{Output: map[string]any{"replayed": 0, "target_topic": resolvedTarget}}, nil
	}

	for i, msg := range messages {
		if err := s.publishMessage(ctx, resolvedTarget, msg); err != nil {
			return nil, fmt.Errorf("dlq_replay step %q: failed to replay message %d: %w", s.name, i, err)
		}
	}

	return &StepResult{Output: map[string]any{
		"replayed":     len(messages),
		"target_topic": resolvedTarget,
		"dlq_topic":    s.dlqTopic,
	}}, nil
}

// collectMessages gathers messages from the pipeline context.
// Handles both batch ("messages" array) and single-message contexts.
func (s *DLQReplayStep) collectMessages(pc *PipelineContext) []map[string]any {
	if msgs, ok := pc.Current["messages"]; ok {
		if msgSlice, ok := msgs.([]any); ok {
			result := make([]map[string]any, 0, len(msgSlice))
			for _, m := range msgSlice {
				if mMap, ok := m.(map[string]any); ok {
					if payload, ok := mMap["payload"].(map[string]any); ok {
						result = append(result, payload)
					} else {
						result = append(result, mMap)
					}
				}
			}
			return result
		}
	}

	// Single DLQ envelope — extract original payload if present
	if payload, ok := pc.Current["payload"].(map[string]any); ok {
		return []map[string]any{payload}
	}

	return []map[string]any{pc.Current}
}

func (s *DLQReplayStep) publishMessage(ctx context.Context, topic string, payload map[string]any) error {
	if s.broker != "" {
		var broker MessageBroker
		if err := s.app.GetService(s.broker, &broker); err != nil {
			return fmt.Errorf("broker service %q not found: %w", s.broker, err)
		}
		data, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal payload: %w", err)
		}
		return broker.Producer().SendMessage(topic, data)
	}

	var eb *eventbus.EventBusModule
	if err := s.app.GetService("eventbus.provider", &eb); err != nil || eb == nil {
		return fmt.Errorf("no broker configured and eventbus not available")
	}
	return eb.Publish(ctx, topic, payload)
}
