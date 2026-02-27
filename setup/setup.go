// Package setup bridges the root workflow package with the handlers and
// module packages to register default workflow handlers and triggers.
// It exists to break the import cycle that would occur if the root package
// directly imported handlers (which has test files that import the root package).
//
// Typical usage is to blank-import this package so that its init function
// registers the default handler and trigger factories with the workflow
// engine, and then build an engine using the workflow package:
//
//	import (
//		"github.com/GoCodeAlone/workflow"
//		_ "github.com/GoCodeAlone/workflow/setup"
//	)
//
//	engine, err := workflow.NewEngineBuilder().
//		WithAllDefaults().
//		Build()
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
