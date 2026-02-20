// Package messaging provides an EnginePlugin that registers all messaging-related
// module types, trigger types, workflow handlers, and schemas.
package messaging

import (
	"reflect"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/handlers"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/schema"
)

// Plugin is the messaging EnginePlugin.
type Plugin struct {
	plugin.BaseEnginePlugin
}

// New creates a new messaging plugin with a valid manifest.
func New() *Plugin {
	return &Plugin{
		BaseEnginePlugin: plugin.BaseEnginePlugin{
			BaseNativePlugin: plugin.BaseNativePlugin{
				PluginName:        "messaging",
				PluginVersion:     "1.0.0",
				PluginDescription: "Messaging subsystem: brokers, handlers, triggers, and workflows",
			},
			Manifest: plugin.PluginManifest{
				Name:        "messaging",
				Version:     "1.0.0",
				Author:      "GoCodeAlone",
				Description: "Messaging subsystem: brokers, handlers, triggers, and workflows",
				ModuleTypes: []string{
					"messaging.broker",
					"messaging.broker.eventbus",
					"messaging.handler",
					"messaging.nats",
					"messaging.kafka",
					"notification.slack",
					"webhook.sender",
				},
				TriggerTypes:  []string{"event", "eventbus"},
				WorkflowTypes: []string{"messaging"},
			},
		},
	}
}

// Capabilities returns the capability contracts this plugin defines.
func (p *Plugin) Capabilities() []capability.Contract {
	return []capability.Contract{
		{
			Name:          "message-broker",
			Description:   "Publish/subscribe message broker",
			InterfaceType: reflect.TypeOf((*module.MessageBroker)(nil)).Elem(),
		},
		{
			Name:          "message-handler",
			Description:   "Handles messages from topics/queues",
			InterfaceType: reflect.TypeOf((*module.MessageHandler)(nil)).Elem(),
		},
	}
}

// ModuleFactories returns factories for all messaging module types.
func (p *Plugin) ModuleFactories() map[string]plugin.ModuleFactory {
	return map[string]plugin.ModuleFactory{
		"messaging.broker": func(name string, cfg map[string]any) modular.Module {
			broker := module.NewInMemoryMessageBroker(name)
			if maxQ, ok := cfg["maxQueueSize"].(float64); ok {
				broker.SetMaxQueueSize(int(maxQ))
			}
			if timeout, ok := cfg["deliveryTimeout"].(string); ok {
				if d, err := time.ParseDuration(timeout); err == nil {
					broker.SetDeliveryTimeout(d)
				}
			}
			return broker
		},
		"messaging.broker.eventbus": func(name string, _ map[string]any) modular.Module {
			return module.NewEventBusBridge(name)
		},
		"messaging.handler": func(name string, _ map[string]any) modular.Module {
			return module.NewSimpleMessageHandler(name)
		},
		"messaging.nats": func(name string, _ map[string]any) modular.Module {
			return module.NewNATSBroker(name)
		},
		"messaging.kafka": func(name string, cfg map[string]any) modular.Module {
			kb := module.NewKafkaBroker(name)
			if brokers, ok := cfg["brokers"].([]any); ok {
				bs := make([]string, 0, len(brokers))
				for _, b := range brokers {
					if s, ok := b.(string); ok {
						bs = append(bs, s)
					}
				}
				if len(bs) > 0 {
					kb.SetBrokers(bs)
				}
			}
			if groupID, ok := cfg["groupId"].(string); ok && groupID != "" {
				kb.SetGroupID(groupID)
			}
			return kb
		},
		"notification.slack": func(name string, _ map[string]any) modular.Module {
			return module.NewSlackNotification(name)
		},
		"webhook.sender": func(name string, cfg map[string]any) modular.Module {
			webhookConfig := module.WebhookConfig{}
			if mr, ok := cfg["maxRetries"].(float64); ok {
				webhookConfig.MaxRetries = int(mr)
			}
			return module.NewWebhookSender(name, webhookConfig)
		},
	}
}

// TriggerFactories returns trigger constructors for messaging-related triggers.
func (p *Plugin) TriggerFactories() map[string]plugin.TriggerFactory {
	return map[string]plugin.TriggerFactory{
		"event": func() any {
			return module.NewEventTrigger()
		},
		"eventbus": func() any {
			return module.NewEventBusTrigger()
		},
	}
}

// WorkflowHandlers returns workflow handler factories for messaging workflows.
func (p *Plugin) WorkflowHandlers() map[string]plugin.WorkflowHandlerFactory {
	return map[string]plugin.WorkflowHandlerFactory{
		"messaging": func() any {
			return handlers.NewMessagingWorkflowHandler()
		},
	}
}

// PipelineTriggerConfigWrappers returns config wrappers that convert flat
// pipeline trigger config into the messaging trigger's native format.
func (p *Plugin) PipelineTriggerConfigWrappers() map[string]plugin.TriggerConfigWrapperFunc {
	return map[string]plugin.TriggerConfigWrapperFunc{
		"event": func(pipelineName string, cfg map[string]any) map[string]any {
			sub := map[string]any{
				"workflow": "pipeline:" + pipelineName,
			}
			if t, ok := cfg["topic"]; ok {
				sub["topic"] = t
			}
			if ev, ok := cfg["event"]; ok {
				sub["event"] = ev
			}
			return map[string]any{
				"subscriptions": []any{sub},
			}
		},
	}
}

// ModuleSchemas returns UI schema definitions for this plugin's module types.
func (p *Plugin) ModuleSchemas() []*schema.ModuleSchema {
	return []*schema.ModuleSchema{
		{
			Type:        "messaging.broker",
			Label:       "In-Memory Message Broker",
			Category:    "messaging",
			Description: "Simple in-memory message broker for local pub/sub",
			Inputs:      []schema.ServiceIODef{{Name: "message", Type: "[]byte", Description: "Message to publish"}},
			Outputs:     []schema.ServiceIODef{{Name: "message", Type: "[]byte", Description: "Delivered message to subscriber"}},
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "maxQueueSize", Label: "Max Queue Size", Type: schema.FieldTypeNumber, DefaultValue: 10000, Description: "Maximum message queue size per topic"},
				{Key: "deliveryTimeout", Label: "Delivery Timeout", Type: schema.FieldTypeDuration, DefaultValue: "30s", Description: "Message delivery timeout", Placeholder: "30s"},
			},
			DefaultConfig: map[string]any{"maxQueueSize": 10000, "deliveryTimeout": "30s"},
		},
		{
			Type:         "messaging.broker.eventbus",
			Label:        "EventBus Bridge",
			Category:     "messaging",
			Description:  "Bridges the modular EventBus to the messaging subsystem",
			Inputs:       []schema.ServiceIODef{{Name: "event", Type: "Event", Description: "CloudEvent from the EventBus"}},
			Outputs:      []schema.ServiceIODef{{Name: "message", Type: "[]byte", Description: "Message forwarded to messaging subsystem"}},
			ConfigFields: []schema.ConfigFieldDef{},
		},
		{
			Type:         "messaging.handler",
			Label:        "Message Handler",
			Category:     "messaging",
			Description:  "Handles messages from topics/queues",
			Inputs:       []schema.ServiceIODef{{Name: "message", Type: "[]byte", Description: "Incoming message from topic/queue"}},
			Outputs:      []schema.ServiceIODef{{Name: "result", Type: "[]byte", Description: "Processed message result"}},
			ConfigFields: []schema.ConfigFieldDef{},
		},
		{
			Type:        "messaging.nats",
			Label:       "NATS Broker",
			Category:    "messaging",
			Description: "NATS message broker for distributed pub/sub",
			Inputs:      []schema.ServiceIODef{{Name: "message", Type: "[]byte", Description: "Message to publish via NATS"}},
			Outputs:     []schema.ServiceIODef{{Name: "message", Type: "[]byte", Description: "Message received from NATS subscription"}},
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "url", Label: "NATS URL", Type: schema.FieldTypeString, DefaultValue: "nats://localhost:4222", Description: "NATS server connection URL", Placeholder: "nats://localhost:4222"},
			},
			DefaultConfig: map[string]any{"url": "nats://localhost:4222"},
		},
		{
			Type:        "messaging.kafka",
			Label:       "Kafka Broker",
			Category:    "messaging",
			Description: "Apache Kafka message broker for high-throughput streaming",
			Inputs:      []schema.ServiceIODef{{Name: "message", Type: "[]byte", Description: "Message to produce to Kafka"}},
			Outputs:     []schema.ServiceIODef{{Name: "message", Type: "[]byte", Description: "Message consumed from Kafka"}},
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "brokers", Label: "Broker Addresses", Type: schema.FieldTypeArray, ArrayItemType: "string", Description: "Kafka broker addresses (e.g. localhost:9092)", Placeholder: "localhost:9092"},
				{Key: "groupId", Label: "Consumer Group ID", Type: schema.FieldTypeString, Description: "Kafka consumer group identifier", Placeholder: "my-consumer-group"},
			},
		},
		{
			Type:        "notification.slack",
			Label:       "Slack Notification",
			Category:    "integration",
			Description: "Sends notifications to Slack channels via webhooks",
			Inputs:      []schema.ServiceIODef{{Name: "message", Type: "string", Description: "Message text to send to Slack"}},
			Outputs:     []schema.ServiceIODef{{Name: "sent", Type: "SlackResponse", Description: "Slack API response"}},
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "webhookURL", Label: "Webhook URL", Type: schema.FieldTypeString, Required: true, Description: "Slack incoming webhook URL", Placeholder: "https://hooks.slack.com/services/...", Sensitive: true},
				{Key: "channel", Label: "Channel", Type: schema.FieldTypeString, Description: "Slack channel to post to", Placeholder: "#general"},
				{Key: "username", Label: "Username", Type: schema.FieldTypeString, DefaultValue: "workflow-bot", Description: "Bot username for messages"},
			},
			DefaultConfig: map[string]any{"username": "workflow-bot"},
		},
		{
			Type:        "webhook.sender",
			Label:       "Webhook Sender",
			Category:    "integration",
			Description: "Sends HTTP webhooks with retry and exponential backoff",
			Inputs:      []schema.ServiceIODef{{Name: "payload", Type: "JSON", Description: "Webhook payload to send"}},
			Outputs:     []schema.ServiceIODef{{Name: "response", Type: "http.Response", Description: "HTTP response from webhook target"}},
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "maxRetries", Label: "Max Retries", Type: schema.FieldTypeNumber, DefaultValue: 3, Description: "Maximum number of retry attempts on failure"},
			},
			DefaultConfig: map[string]any{"maxRetries": 3},
		},
	}
}
