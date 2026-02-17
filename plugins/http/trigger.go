package http

import (
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
)

// triggerFactories returns trigger constructors for HTTP triggers.
func triggerFactories() map[string]plugin.TriggerFactory {
	return map[string]plugin.TriggerFactory{
		"http": func() any {
			return module.NewHTTPTrigger()
		},
	}
}
