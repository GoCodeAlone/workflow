package http

import (
	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
)

// stepFactories returns pipeline step factories for HTTP-specific steps.
// Note: step.http_call, step.request_parse, and step.json_response are
// provided by the pipelinesteps plugin to avoid duplicate registrations.
func stepFactories() map[string]plugin.StepFactory {
	return map[string]plugin.StepFactory{
		"step.rate_limit": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			factory := module.NewRateLimitStepFactory()
			return factory(name, cfg, app)
		},
		"step.circuit_breaker": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			factory := module.NewCircuitBreakerStepFactory()
			return factory(name, cfg, app)
		},
	}
}
