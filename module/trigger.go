package module

import (
	"github.com/GoCodeAlone/workflow/interfaces"
)

// Trigger is a type alias for interfaces.Trigger.
// The canonical definition lives in the interfaces package so that the engine
// and other packages can reference it without importing this module package.
// All existing code using module.Trigger is unaffected by this alias.
type Trigger = interfaces.Trigger

// TriggerRegistry manages registered triggers and allows finding them by name.
// It satisfies interfaces.TriggerRegistrar.
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
