// Package setup provides convenience functions for initialising a workflow
// engine with sensible defaults. It bridges the root workflow package with
// the handlers and module packages so that consumers don't need to wire up
// all the built-in handlers and triggers manually.
//
// Typical usage:
//
//	engine, err := setup.NewDefaultEngine()
//	engine, err := setup.NewDefaultEngineFromConfig(cfg)
//	engine, err := setup.NewEngineBuilder().WithAllDefaults().Build()
package setup

import (
	"github.com/GoCodeAlone/workflow"
	"github.com/GoCodeAlone/workflow/handlers"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/module"
)

func init() {
	workflow.DefaultHandlerFactory = DefaultHandlers
	workflow.DefaultTriggerFactory = DefaultTriggers
}

// DefaultHandlers returns all built-in workflow handlers.
func DefaultHandlers() []workflow.WorkflowHandler {
	return []workflow.WorkflowHandler{
		handlers.NewHTTPWorkflowHandler(),
		handlers.NewMessagingWorkflowHandler(),
		handlers.NewStateMachineWorkflowHandler(),
		handlers.NewSchedulerWorkflowHandler(),
		handlers.NewIntegrationWorkflowHandler(),
		handlers.NewPipelineWorkflowHandler(),
		handlers.NewEventWorkflowHandler(),
		handlers.NewPlatformWorkflowHandler(),
	}
}

// DefaultTriggers returns all built-in triggers.
func DefaultTriggers() []interfaces.Trigger {
	return []interfaces.Trigger{
		module.NewHTTPTrigger(),
		module.NewEventTrigger(),
		module.NewScheduleTrigger(),
		module.NewEventBusTrigger(),
		module.NewReconciliationTrigger(),
	}
}
