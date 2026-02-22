package http

import (
	"reflect"

	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/schema"
)

// HTTPPlugin provides all HTTP-related module types, middleware, triggers,
// workflow handlers, and wiring hooks.
type HTTPPlugin struct {
	plugin.BaseEnginePlugin
}

// New creates a new HTTPPlugin instance.
func New() *HTTPPlugin {
	return &HTTPPlugin{
		BaseEnginePlugin: plugin.BaseEnginePlugin{
			BaseNativePlugin: plugin.BaseNativePlugin{
				PluginName:        "workflow-plugin-http",
				PluginVersion:     "1.0.0",
				PluginDescription: "HTTP server, router, handlers, middleware, proxy, and static file serving",
			},
			Manifest: plugin.PluginManifest{
				Name:        "workflow-plugin-http",
				Version:     "1.0.0",
				Author:      "GoCodeAlone",
				Description: "HTTP server, router, handlers, middleware, proxy, and static file serving",
				Tier:        plugin.TierCore,
				ModuleTypes: []string{
					"http.server",
					"http.router",
					"http.handler",
					"http.proxy",
					"reverseproxy",
					"http.simple_proxy",
					"static.fileserver",
					"http.middleware.auth",
					"http.middleware.logging",
					"http.middleware.ratelimit",
					"http.middleware.cors",
					"http.middleware.requestid",
					"http.middleware.securityheaders",
				},
				StepTypes: []string{
					"step.rate_limit",
					"step.circuit_breaker",
				},
				TriggerTypes:  []string{"http"},
				WorkflowTypes: []string{"http"},
				WiringHooks: []string{
					"http-auth-provider-wiring",
					"http-static-fileserver-registration",
					"http-health-endpoint-registration",
					"http-metrics-endpoint-registration",
					"http-log-endpoint-registration",
					"http-openapi-endpoint-registration",
				},
				Capabilities: []plugin.CapabilityDecl{
					{Name: "http-server", Role: "provider", Priority: 10},
					{Name: "http-router", Role: "provider", Priority: 10},
					{Name: "http-handler", Role: "provider", Priority: 10},
					{Name: "http-middleware", Role: "provider", Priority: 10},
					{Name: "http-proxy", Role: "provider", Priority: 10},
					{Name: "static-files", Role: "provider", Priority: 10},
				},
			},
		},
	}
}

// Capabilities returns contracts for HTTP-related capabilities.
func (p *HTTPPlugin) Capabilities() []capability.Contract {
	return []capability.Contract{
		{
			Name:          "http-server",
			Description:   "HTTP server that listens on a network address and dispatches to routers",
			InterfaceType: reflect.TypeOf((*module.HTTPServer)(nil)).Elem(),
			RequiredMethods: []capability.MethodSignature{
				{Name: "AddRouter", Params: []string{"HTTPRouter"}, Returns: nil},
				{Name: "Start", Params: []string{"context.Context"}, Returns: []string{"error"}},
				{Name: "Stop", Params: []string{"context.Context"}, Returns: []string{"error"}},
			},
		},
		{
			Name:          "http-router",
			Description:   "Routes HTTP requests to handlers based on method and path",
			InterfaceType: reflect.TypeOf((*module.HTTPRouter)(nil)).Elem(),
			RequiredMethods: []capability.MethodSignature{
				{Name: "AddRoute", Params: []string{"string", "string", "HTTPHandler"}, Returns: nil},
			},
		},
		{
			Name:          "http-handler",
			Description:   "Handles HTTP requests and produces responses",
			InterfaceType: reflect.TypeOf((*module.HTTPHandler)(nil)).Elem(),
			RequiredMethods: []capability.MethodSignature{
				{Name: "Handle", Params: []string{"http.ResponseWriter", "*http.Request"}, Returns: nil},
			},
		},
		{
			Name:          "http-middleware",
			Description:   "Middleware that wraps HTTP handlers for cross-cutting concerns",
			InterfaceType: reflect.TypeOf((*module.HTTPMiddleware)(nil)).Elem(),
			RequiredMethods: []capability.MethodSignature{
				{Name: "Process", Params: []string{"http.Handler"}, Returns: []string{"http.Handler"}},
			},
		},
		{
			Name:        "http-proxy",
			Description: "Reverse proxy that forwards HTTP requests to backend services",
		},
		{
			Name:        "static-files",
			Description: "Serves static files from a directory with optional SPA fallback",
		},
	}
}

// ModuleFactories returns factory functions for all HTTP module types.
func (p *HTTPPlugin) ModuleFactories() map[string]plugin.ModuleFactory {
	return moduleFactories()
}

// StepFactories returns pipeline step factories for HTTP-related steps.
func (p *HTTPPlugin) StepFactories() map[string]plugin.StepFactory {
	return stepFactories()
}

// TriggerFactories returns trigger constructors for HTTP triggers.
func (p *HTTPPlugin) TriggerFactories() map[string]plugin.TriggerFactory {
	return triggerFactories()
}

// WorkflowHandlers returns workflow handler factories for HTTP workflows.
func (p *HTTPPlugin) WorkflowHandlers() map[string]plugin.WorkflowHandlerFactory {
	return workflowHandlerFactories()
}

// ModuleSchemas returns UI schema definitions for all HTTP module types.
func (p *HTTPPlugin) ModuleSchemas() []*schema.ModuleSchema {
	return moduleSchemas()
}

// WiringHooks returns post-init wiring functions for HTTP-related cross-module integrations.
func (p *HTTPPlugin) WiringHooks() []plugin.WiringHook {
	return wiringHooks()
}

// PipelineTriggerConfigWrappers returns config wrappers that convert flat
// pipeline trigger config into the HTTP trigger's native format.
func (p *HTTPPlugin) PipelineTriggerConfigWrappers() map[string]plugin.TriggerConfigWrapperFunc {
	return map[string]plugin.TriggerConfigWrapperFunc{
		"http": func(pipelineName string, cfg map[string]any) map[string]any {
			route := map[string]any{
				"workflow": "pipeline:" + pipelineName,
			}
			if path, ok := cfg["path"]; ok {
				route["path"] = path
			}
			if method, ok := cfg["method"]; ok {
				route["method"] = method
			}
			return map[string]any{
				"routes": []any{route},
			}
		},
	}
}
