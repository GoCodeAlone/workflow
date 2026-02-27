// Package pipelinesteps provides a plugin that registers generic pipeline step
// types: validate, transform, conditional, set, log, delegate, jq, publish,
// http_call, request_parse, db_query, db_exec, json_response,
// validate_path_param, validate_pagination, validate_request_body,
// foreach, webhook_verify, base64_decode, ui_scaffold, ui_scaffold_analyze,
// dlq_send, dlq_replay, retry_with_backoff, circuit_breaker (wrapping).
// It also provides the PipelineWorkflowHandler for composable pipelines.
package pipelinesteps

import (
	"log/slog"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/handlers"
	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
)

// PipelineHandlerServiceName is the service name under which the
// PipelineWorkflowHandler is registered in the app's service registry.
// External components can look it up to call SetEventRecorder after startup.
const PipelineHandlerServiceName = "pipeline-workflow-handler"

// Plugin registers generic pipeline step factories and the pipeline workflow handler.
type Plugin struct {
	plugin.BaseEnginePlugin
	// pipelineHandler is retained so the wiring hook can inject dependencies.
	pipelineHandler *handlers.PipelineWorkflowHandler
	// stepRegistry and logger are injected by the engine via optional setter interfaces.
	stepRegistry         interfaces.StepRegistryProvider
	concreteStepRegistry *module.StepRegistry
	logger               *slog.Logger
}

// New creates a new pipeline-steps plugin.
func New() *Plugin {
	return &Plugin{
		BaseEnginePlugin: plugin.BaseEnginePlugin{
			BaseNativePlugin: plugin.BaseNativePlugin{
				PluginName:        "pipeline-steps",
				PluginVersion:     "1.0.0",
				PluginDescription: "Generic pipeline step types (validate, transform, conditional, set, log, delegate, jq, base64_decode, validate_path_param, validate_pagination, validate_request_body, foreach, webhook_verify, etc.)",
			},
			Manifest: plugin.PluginManifest{
				Name:        "pipeline-steps",
				Version:     "1.0.0",
				Author:      "GoCodeAlone",
				Description: "Generic pipeline step types, pre-processing validators, and pipeline workflow handler (including base64_decode)",
				Tier:        plugin.TierCore,
				StepTypes: []string{
					"step.validate",
					"step.transform",
					"step.conditional",
					"step.set",
					"step.log",
					"step.delegate",
					"step.jq",
					"step.publish",
					"step.event_publish",
					"step.http_call",
					"step.request_parse",
					"step.db_query",
					"step.db_exec",
					"step.json_response",
					"step.workflow_call",
					"step.validate_path_param",
					"step.validate_pagination",
					"step.validate_request_body",
					"step.foreach",
					"step.webhook_verify",
					"step.base64_decode",
					"step.cache_get",
					"step.cache_set",
					"step.cache_delete",
					"step.ui_scaffold",
					"step.ui_scaffold_analyze",
					"step.dlq_send",
					"step.dlq_replay",
					"step.retry_with_backoff",
					"step.resilient_circuit_breaker",
				},
				WorkflowTypes: []string{"pipeline"},
				Capabilities: []plugin.CapabilityDecl{
					{Name: "pipeline-steps", Role: "provider", Priority: 50},
				},
			},
		},
	}
}

// Capabilities returns the capability contracts defined by this plugin.
func (p *Plugin) Capabilities() []capability.Contract {
	return []capability.Contract{
		{
			Name:        "pipeline-steps",
			Description: "Generic pipeline step operations: validate, transform, conditional, set, log, delegate, jq, foreach, webhook_verify, etc.",
		},
	}
}

// StepFactories returns the step factories provided by this plugin.
func (p *Plugin) StepFactories() map[string]plugin.StepFactory {
	return map[string]plugin.StepFactory{
		"step.validate":              wrapStepFactory(module.NewValidateStepFactory()),
		"step.transform":             wrapStepFactory(module.NewTransformStepFactory()),
		"step.conditional":           wrapStepFactory(module.NewConditionalStepFactory()),
		"step.set":                   wrapStepFactory(module.NewSetStepFactory()),
		"step.log":                   wrapStepFactory(module.NewLogStepFactory()),
		"step.delegate":              wrapStepFactory(module.NewDelegateStepFactory()),
		"step.jq":                    wrapStepFactory(module.NewJQStepFactory()),
		"step.publish":               wrapStepFactory(module.NewPublishStepFactory()),
		"step.event_publish":          wrapStepFactory(module.NewEventPublishStepFactory()),
		"step.http_call":             wrapStepFactory(module.NewHTTPCallStepFactory()),
		"step.request_parse":         wrapStepFactory(module.NewRequestParseStepFactory()),
		"step.db_query":              wrapStepFactory(module.NewDBQueryStepFactory()),
		"step.db_exec":               wrapStepFactory(module.NewDBExecStepFactory()),
		"step.json_response":         wrapStepFactory(module.NewJSONResponseStepFactory()),
		"step.validate_path_param":   wrapStepFactory(module.NewValidatePathParamStepFactory()),
		"step.validate_pagination":   wrapStepFactory(module.NewValidatePaginationStepFactory()),
		"step.validate_request_body": wrapStepFactory(module.NewValidateRequestBodyStepFactory()),
		// step.foreach uses a lazy registry getter so it can reference any registered step type,
		// including types registered by other plugins loaded after this one.
		"step.foreach": wrapStepFactory(module.NewForEachStepFactory(func() *module.StepRegistry {
			return p.concreteStepRegistry
		})),
		"step.webhook_verify": wrapStepFactory(module.NewWebhookVerifyStepFactory()),
		"step.base64_decode":  wrapStepFactory(module.NewBase64DecodeStepFactory()),
		"step.cache_get":           wrapStepFactory(module.NewCacheGetStepFactory()),
		"step.cache_set":           wrapStepFactory(module.NewCacheSetStepFactory()),
		"step.cache_delete":        wrapStepFactory(module.NewCacheDeleteStepFactory()),
		"step.ui_scaffold":         wrapStepFactory(module.NewScaffoldStepFactory()),
		"step.ui_scaffold_analyze": wrapStepFactory(module.NewScaffoldAnalyzeStepFactory()),
		// DLQ steps use a lazy registry getter so sub-steps can reference any registered type.
		"step.dlq_send":   wrapStepFactory(module.NewDLQSendStepFactory()),
		"step.dlq_replay": wrapStepFactory(module.NewDLQReplayStepFactory()),
		"step.retry_with_backoff": wrapStepFactory(module.NewRetryWithBackoffStepFactory(func() *module.StepRegistry {
			return p.concreteStepRegistry
		})),
		"step.resilient_circuit_breaker": wrapStepFactory(module.NewResilienceCircuitBreakerStepFactory(func() *module.StepRegistry {
			return p.concreteStepRegistry
		})),
	}
}

// WorkflowHandlers returns the pipeline workflow handler factory.
func (p *Plugin) WorkflowHandlers() map[string]plugin.WorkflowHandlerFactory {
	return map[string]plugin.WorkflowHandlerFactory{
		"pipeline": func() any {
			p.pipelineHandler = handlers.NewPipelineWorkflowHandler()
			return p.pipelineHandler
		},
	}
}

// SetStepRegistry is called by the engine (via optional-interface detection in LoadPlugin)
// to inject the step registry after all step factories have been registered.
// It also stores the concrete *module.StepRegistry so that step.foreach can build
// sub-steps using the full registry at step-creation time.
func (p *Plugin) SetStepRegistry(registry interfaces.StepRegistryProvider) {
	p.stepRegistry = registry
	// Type-assert to the concrete registry so step.foreach can call Create().
	// The engine always passes *module.StepRegistry; this is safe.
	if concrete, ok := registry.(*module.StepRegistry); ok {
		p.concreteStepRegistry = concrete
	}
}

// SetLogger is called by the engine (via optional-interface detection in LoadPlugin)
// to inject the application logger.
func (p *Plugin) SetLogger(logger *slog.Logger) {
	p.logger = logger
}

// WiringHooks returns a hook that wires the injected step registry and logger into
// the PipelineWorkflowHandler and registers the handler as a named service so that
// other components (e.g. the server) can look it up without reaching into the plugin.
func (p *Plugin) WiringHooks() []plugin.WiringHook {
	return []plugin.WiringHook{
		{
			Name:     "pipeline-handler-wiring",
			Priority: 50,
			Hook: func(app modular.Application, _ *config.WorkflowConfig) error {
				if p.pipelineHandler == nil {
					return nil
				}
				if p.stepRegistry != nil {
					p.pipelineHandler.SetStepRegistry(p.stepRegistry)
				}
				if p.logger != nil {
					p.pipelineHandler.SetLogger(p.logger)
				}
				// Register the handler as a service so callers can discover it
				// (e.g. to wire SetEventRecorder post-start) without a plugin-specific getter.
				_ = app.RegisterService(PipelineHandlerServiceName, p.pipelineHandler)
				return nil
			},
		},
	}
}

// wrapStepFactory converts a module.StepFactory to a plugin.StepFactory,
// threading the modular.Application through so steps like db_exec and
// db_query can access the service registry.
func wrapStepFactory(f module.StepFactory) plugin.StepFactory {
	return func(name string, cfg map[string]any, app modular.Application) (any, error) {
		return f(name, cfg, app)
	}
}
