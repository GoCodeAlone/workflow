package interfaces

import "github.com/CrisisTextLine/modular"

// Trigger defines what can start a workflow execution.
// Moving this interface here breaks the engine→module import dependency while
// preserving backward compatibility via the type alias in the module package.
//
// *module.HTTPTrigger, *module.ScheduleTrigger, and other concrete trigger
// implementations all satisfy this interface.
type Trigger interface {
	modular.Module
	modular.Startable
	modular.Stoppable

	// Configure sets up the trigger from configuration.
	Configure(app modular.Application, triggerConfig any) error
}

// TriggerRegistrar manages registered triggers.
// *module.TriggerRegistry satisfies this interface.
type TriggerRegistrar interface {
	// RegisterTrigger adds a trigger to the registry.
	RegisterTrigger(trigger Trigger)
}
