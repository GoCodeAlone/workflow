package statemachine

import (
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/handlers"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/schema"
)

// Plugin provides state machine workflow capabilities: statemachine.engine,
// state.tracker, state.connector modules and the statemachine workflow handler.
type Plugin struct {
	plugin.BaseEnginePlugin
}

// New creates a new StateMachine plugin.
func New() *Plugin {
	return &Plugin{
		BaseEnginePlugin: plugin.BaseEnginePlugin{
			BaseNativePlugin: plugin.BaseNativePlugin{
				PluginName:        "statemachine",
				PluginVersion:     "1.0.0",
				PluginDescription: "State machine engine, tracker, connector modules and workflow handler",
			},
			Manifest: plugin.PluginManifest{
				Name:        "statemachine",
				Version:     "1.0.0",
				Author:      "GoCodeAlone",
				Description: "State machine engine, tracker, connector modules and workflow handler",
				Tier:        plugin.TierCore,
				ModuleTypes: []string{
					"statemachine.engine",
					"state.tracker",
					"state.connector",
				},
				WorkflowTypes: []string{"statemachine"},
				Capabilities: []plugin.CapabilityDecl{
					{Name: "state-machine", Role: "provider", Priority: 10},
					{Name: "state-tracking", Role: "provider", Priority: 10},
				},
			},
		},
	}
}

// Capabilities returns the capability contracts this plugin defines.
func (p *Plugin) Capabilities() []capability.Contract {
	return []capability.Contract{
		{
			Name:        "state-machine",
			Description: "State machine engine for workflow state transitions and lifecycle management",
		},
		{
			Name:        "state-tracking",
			Description: "Tracks and persists workflow instance state across transitions",
		},
	}
}

// ModuleFactories returns factories for statemachine.engine, state.tracker, and state.connector.
func (p *Plugin) ModuleFactories() map[string]plugin.ModuleFactory {
	return map[string]plugin.ModuleFactory{
		"statemachine.engine": func(name string, config map[string]any) modular.Module {
			smEngine := module.NewStateMachineEngine(name)
			if maxInst, ok := config["maxInstances"].(float64); ok {
				smEngine.SetMaxInstances(int(maxInst))
			}
			if ttl, ok := config["instanceTTL"].(string); ok {
				if d, err := time.ParseDuration(ttl); err == nil {
					smEngine.SetInstanceTTL(d)
				}
			}
			return smEngine
		},
		"state.tracker": func(name string, config map[string]any) modular.Module {
			tracker := module.NewStateTracker(name)
			if rd, ok := config["retentionDays"].(float64); ok {
				tracker.SetRetentionDays(int(rd))
			}
			return tracker
		},
		"state.connector": func(name string, _ map[string]any) modular.Module {
			return module.NewStateMachineStateConnector(name)
		},
	}
}

// WorkflowHandlers returns the statemachine workflow handler factory.
func (p *Plugin) WorkflowHandlers() map[string]plugin.WorkflowHandlerFactory {
	return map[string]plugin.WorkflowHandlerFactory{
		"statemachine": func() any {
			return handlers.NewStateMachineWorkflowHandler()
		},
	}
}

// ModuleSchemas returns UI schema definitions for state machine module types.
func (p *Plugin) ModuleSchemas() []*schema.ModuleSchema {
	return []*schema.ModuleSchema{
		{
			Type:        "statemachine.engine",
			Label:       "State Machine Engine",
			Category:    "statemachine",
			Description: "Manages workflow state transitions and lifecycle",
			Inputs:      []schema.ServiceIODef{{Name: "event", Type: "Event", Description: "Event triggering a state transition"}},
			Outputs:     []schema.ServiceIODef{{Name: "transition", Type: "Transition", Description: "Completed state transition result"}},
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "maxInstances", Label: "Max Instances", Type: schema.FieldTypeNumber, DefaultValue: 1000, Description: "Maximum concurrent workflow instances"},
				{Key: "instanceTTL", Label: "Instance TTL", Type: schema.FieldTypeDuration, DefaultValue: "24h", Description: "TTL for idle workflow instances", Placeholder: "24h"},
			},
			DefaultConfig: map[string]any{"maxInstances": 1000, "instanceTTL": "24h"},
		},
		{
			Type:        "state.tracker",
			Label:       "State Tracker",
			Category:    "statemachine",
			Description: "Tracks and persists workflow instance state",
			Inputs:      []schema.ServiceIODef{{Name: "state", Type: "State", Description: "State update to track"}},
			Outputs:     []schema.ServiceIODef{{Name: "tracked", Type: "State", Description: "Tracked state with persistence"}},
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "retentionDays", Label: "Retention Days", Type: schema.FieldTypeNumber, DefaultValue: 30, Description: "State history retention in days"},
			},
			DefaultConfig: map[string]any{"retentionDays": 30},
		},
		{
			Type:         "state.connector",
			Label:        "State Connector",
			Category:     "statemachine",
			Description:  "Connects state machine engine to state tracker for persistence",
			Inputs:       []schema.ServiceIODef{{Name: "state", Type: "State", Description: "State from engine to connect"}},
			Outputs:      []schema.ServiceIODef{{Name: "connected", Type: "State", Description: "Connected state bridging engine and tracker"}},
			ConfigFields: []schema.ConfigFieldDef{},
		},
	}
}
