package module

import (
	"github.com/CrisisTextLine/modular"
)

// Trigger defines what can start a workflow execution
type Trigger interface {
	modular.Module
	modular.Startable
	modular.Stoppable

	// Configure sets up the trigger from configuration
	Configure(app modular.Application, triggerConfig any) error
}

// TriggerRegistry manages registered triggers and allows finding them by name
type TriggerRegistry struct {
	triggers map[string]Trigger
}

// NewTriggerRegistry creates a new trigger registry
func NewTriggerRegistry() *TriggerRegistry {
	return &TriggerRegistry{
		triggers: make(map[string]Trigger),
	}
}

// RegisterTrigger adds a trigger to the registry
func (r *TriggerRegistry) RegisterTrigger(trigger Trigger) {
	r.triggers[trigger.Name()] = trigger
}

// GetTrigger returns a trigger by name
func (r *TriggerRegistry) GetTrigger(name string) (Trigger, bool) {
	trigger, ok := r.triggers[name]
	return trigger, ok
}

// GetAllTriggers returns all registered triggers
func (r *TriggerRegistry) GetAllTriggers() map[string]Trigger {
	return r.triggers
}
