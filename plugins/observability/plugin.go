// Package observability provides an EnginePlugin that contributes all
// observability-related module types: metrics collector, health checker,
// log collector, OpenTelemetry tracing, and OpenAPI generator/consumer.
package observability

import (
	"github.com/GoCodeAlone/workflow/capability"
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
			},
			WiringHooks: []string{
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

// WiringHooks returns post-init wiring functions that connect observability
// modules to the HTTP router.
func (p *ObservabilityPlugin) WiringHooks() []plugin.WiringHook {
	return wiringHooks()
}
