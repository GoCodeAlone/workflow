// Package observability provides an EnginePlugin that contributes all
// observability-related module types: metrics collector, health checker,
// log collector, OpenTelemetry tracing, OpenAPI generator/consumer,
// and distributed trace propagation.
package observability

import (
	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/schema"
)

// ObservabilityPlugin provides metrics, health checking, log collection,
// distributed tracing (OpenTelemetry), and OpenAPI spec generation/consumption.
type ObservabilityPlugin struct {
	plugin.BaseEnginePlugin
}

// New creates a new ObservabilityPlugin.
func New() *ObservabilityPlugin {
	p := &ObservabilityPlugin{}
	p.BaseEnginePlugin = plugin.BaseEnginePlugin{
		BaseNativePlugin: plugin.BaseNativePlugin{
			PluginName:        "observability",
			PluginVersion:     "1.0.0",
			PluginDescription: "Metrics, health checks, log collection, OpenTelemetry tracing, and OpenAPI spec generation/consumption",
		},
		Manifest: plugin.PluginManifest{
			Name:        "observability",
			Version:     "1.0.0",
			Author:      "GoCodeAlone",
			Description: "Metrics, health checks, log collection, OpenTelemetry tracing, and OpenAPI spec generation/consumption",
			Tier:        plugin.TierCore,
			ModuleTypes: []string{
				"metrics.collector",
				"health.checker",
				"log.collector",
				"observability.otel",
				"openapi.generator",
				"http.middleware.otel",
				"tracing.propagation",
			},
			StepTypes: []string{
				"step.trace_start",
				"step.trace_inject",
				"step.trace_extract",
				"step.trace_annotate",
				"step.trace_link",
			},
			WiringHooks: []string{
				"observability.otel-middleware",
				"observability.health-endpoints",
				"observability.metrics-endpoint",
				"observability.log-endpoint",
				"observability.openapi-endpoints",
			},
		},
	}
	return p
}

// Capabilities returns the capability contracts this plugin defines.
func (p *ObservabilityPlugin) Capabilities() []capability.Contract {
	return []capability.Contract{
		{
			Name:        "metrics",
			Description: "Application metrics collection and exposition (Prometheus)",
		},
		{
			Name:        "health-check",
			Description: "Health, readiness, and liveness endpoint probes",
		},
		{
			Name:        "logging",
			Description: "Centralized log collection from modules",
		},
		{
			Name:        "tracing",
			Description: "Distributed tracing via OpenTelemetry",
		},
		{
			Name:        "openapi",
			Description: "OpenAPI 3.0 spec generation and external API consumption",
		},
	}
}

// ModuleFactories returns factories for all observability module types.
func (p *ObservabilityPlugin) ModuleFactories() map[string]plugin.ModuleFactory {
	return moduleFactories()
}

// ModuleSchemas returns the UI schema definitions for observability module types.
func (p *ObservabilityPlugin) ModuleSchemas() []*schema.ModuleSchema {
	return moduleSchemas()
}

// StepFactories returns the tracing pipeline step factories.
func (p *ObservabilityPlugin) StepFactories() map[string]plugin.StepFactory {
	return map[string]plugin.StepFactory{
		"step.trace_start": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewTraceStartStepFactory()(name, cfg, app)
		},
		"step.trace_inject": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewTraceInjectStepFactory()(name, cfg, app)
		},
		"step.trace_extract": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewTraceExtractStepFactory()(name, cfg, app)
		},
		"step.trace_annotate": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewTraceAnnotateStepFactory()(name, cfg, app)
		},
		"step.trace_link": func(name string, cfg map[string]any, app modular.Application) (any, error) {
			return module.NewTraceLinkStepFactory()(name, cfg, app)
		},
	}
}

// WiringHooks returns post-init wiring functions that connect observability
// modules to the HTTP router.
func (p *ObservabilityPlugin) WiringHooks() []plugin.WiringHook {
	return wiringHooks()
}
