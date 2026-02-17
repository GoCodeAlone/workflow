package http

import (
	"github.com/GoCodeAlone/workflow/handlers"
	"github.com/GoCodeAlone/workflow/plugin"
)

// workflowHandlerFactories returns workflow handler factories for HTTP workflows.
func workflowHandlerFactories() map[string]plugin.WorkflowHandlerFactory {
	return map[string]plugin.WorkflowHandlerFactory{
		"http": func() any {
			return handlers.NewHTTPWorkflowHandler()
		},
	}
}
