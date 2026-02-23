// Package openapi provides the OpenAPI module plugin for the workflow engine.
// It registers the "openapi" module type which parses OpenAPI v3 specifications,
// generates HTTP routes, validates requests, and optionally serves Swagger UI.
package openapi

import (
	"log/slog"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/schema"
)

// Plugin provides the "openapi" module type and its wiring hook.
type Plugin struct {
	plugin.BaseEnginePlugin
}

// New creates a new OpenAPI plugin.
func New() *Plugin {
	return &Plugin{
		BaseEnginePlugin: plugin.BaseEnginePlugin{
			BaseNativePlugin: plugin.BaseNativePlugin{
				PluginName:        "workflow-plugin-openapi",
				PluginVersion:     "1.0.0",
				PluginDescription: "OpenAPI v3 spec-driven HTTP route generation with request validation and Swagger UI",
			},
			Manifest: plugin.PluginManifest{
				Name:        "workflow-plugin-openapi",
				Version:     "1.0.0",
				Author:      "GoCodeAlone",
				Description: "OpenAPI v3 spec-driven HTTP route generation with request validation and Swagger UI",
				Tier:        plugin.TierCore,
				ModuleTypes: []string{"openapi"},
				WiringHooks: []string{"openapi-route-registration"},
				Capabilities: []plugin.CapabilityDecl{
					{Name: "openapi", Role: "provider", Priority: 10},
				},
			},
		},
	}
}

// Capabilities returns the capability contracts this plugin defines.
func (p *Plugin) Capabilities() []capability.Contract {
	return []capability.Contract{
		{
			Name:        "openapi",
			Description: "OpenAPI v3 spec-driven HTTP route generation with request validation and Swagger UI",
		},
	}
}

// ModuleFactories returns the factory for the "openapi" module type.
func (p *Plugin) ModuleFactories() map[string]plugin.ModuleFactory {
	return map[string]plugin.ModuleFactory{
		"openapi": func(name string, cfg map[string]any) modular.Module {
			oacfg := module.OpenAPIConfig{
				// Default: enable request validation unless explicitly overridden.
				Validation: module.OpenAPIValidationConfig{Request: true},
			}

			// NOTE: spec_file existence is not validated here at configuration time.
			// Path resolution is performed by ResolvePathInConfig (relative to the
			// config file directory via the _config_dir key), but file existence and
			// readability are checked lazily during Init(). Errors will surface at
			// engine startup, after all modules have been constructed.
			if v, ok := cfg["spec_file"].(string); ok {
				oacfg.SpecFile = config.ResolvePathInConfig(cfg, v)
			}
			if v, ok := cfg["base_path"].(string); ok {
				oacfg.BasePath = v
			}
			if v, ok := cfg["router"].(string); ok {
				oacfg.RouterName = v
			}

			if valCfg, ok := cfg["validation"].(map[string]any); ok {
				if v, ok2 := valCfg["request"].(bool); ok2 {
					oacfg.Validation.Request = v
				}
				if v, ok2 := valCfg["response"].(bool); ok2 {
					oacfg.Validation.Response = v
				}
			}

			if uiCfg, ok := cfg["swagger_ui"].(map[string]any); ok {
				if v, ok2 := uiCfg["enabled"].(bool); ok2 {
					oacfg.SwaggerUI.Enabled = v
				}
				if v, ok2 := uiCfg["path"].(string); ok2 {
					oacfg.SwaggerUI.Path = v
				}
			}

			return module.NewOpenAPIModule(name, oacfg)
		},
	}
}

// ModuleSchemas returns the UI schema definition for the "openapi" module type.
func (p *Plugin) ModuleSchemas() []*schema.ModuleSchema {
	return []*schema.ModuleSchema{
		{
			Type:        "openapi",
			Label:       "OpenAPI",
			Category:    "http",
			Description: "Generates HTTP routes from an OpenAPI v3 spec with optional request validation and Swagger UI",
			Inputs:      []schema.ServiceIODef{{Name: "request", Type: "http.Request", Description: "Incoming HTTP requests matched against spec paths"}},
			Outputs:     []schema.ServiceIODef{{Name: "response", Type: "http.Response", Description: "Validated HTTP response or 501 stub"}},
			ConfigFields: []schema.ConfigFieldDef{
				{
					Key:         "spec_file",
					Label:       "Spec File",
					Type:        schema.FieldTypeFilePath,
					Required:    true,
					Description: "Path to the OpenAPI v3 YAML or JSON spec file",
					Placeholder: "specs/petstore.yaml",
				},
				{
					Key:         "base_path",
					Label:       "Base Path",
					Type:        schema.FieldTypeString,
					Description: "URL path prefix prepended to all spec routes (e.g. /api/v1)",
					Placeholder: "/api/v1",
				},
				{
					Key:         "router",
					Label:       "Router Name",
					Type:        schema.FieldTypeString,
					Description: "Explicit router module name to attach routes to (auto-detected if omitted)",
					Placeholder: "my-router",
					InheritFrom: "dependency.name",
				},
				{
					Key:          "validation",
					Label:        "Validation",
					Type:         schema.FieldTypeJSON,
					Description:  "Request/response validation settings: {request: true, response: false}",
					DefaultValue: map[string]any{"request": true, "response": false},
					Group:        "validation",
				},
				{
					Key:          "swagger_ui",
					Label:        "Swagger UI",
					Type:         schema.FieldTypeJSON,
					Description:  "Swagger UI settings: {enabled: true, path: /docs}",
					DefaultValue: map[string]any{"enabled": false, "path": "/docs"},
					Group:        "swagger_ui",
				},
			},
			DefaultConfig: map[string]any{
				"base_path":  "/api/v1",
				"validation": map[string]any{"request": true, "response": false},
				"swagger_ui": map[string]any{"enabled": false, "path": "/docs"},
			},
		},
	}
}

// WiringHooks returns the post-init wiring function that registers OpenAPI routes
// on the appropriate HTTP router.
func (p *Plugin) WiringHooks() []plugin.WiringHook {
	return []plugin.WiringHook{
		{
			Name:     "openapi-route-registration",
			Priority: 45, // run after auth wiring (90) and after static files (50)
			Hook:     wireOpenAPIRoutes,
		},
	}
}

// wireOpenAPIRoutes finds all OpenAPIModule instances registered as services and
// registers their routes on the best matching HTTPRouter.
func wireOpenAPIRoutes(app modular.Application, cfg *config.WorkflowConfig) error {
	// Build name→router lookup from config dependsOn
	routerNames := make(map[string]bool)
	openAPIDeps := make(map[string][]string) // openapi module name → dependsOn
	for _, modCfg := range cfg.Modules {
		if modCfg.Type == "http.router" {
			routerNames[modCfg.Name] = true
		}
		if modCfg.Type == "openapi" {
			openAPIDeps[modCfg.Name] = modCfg.DependsOn
		}
	}

	// Build router lookup map and capture the first available router in a single pass.
	// This reduces subsequent lookups from O(n) each to O(1).
	routers := make(map[string]module.HTTPRouter)
	var firstRouter module.HTTPRouter
	for svcName, svc := range app.SvcRegistry() {
		if router, ok := svc.(module.HTTPRouter); ok {
			routers[svcName] = router
			if firstRouter == nil {
				firstRouter = router
			}
		}
	}

	for _, svc := range app.SvcRegistry() {
		oaMod, ok := svc.(*module.OpenAPIModule)
		if !ok {
			continue
		}

		var targetRouter module.HTTPRouter

		// 1) Explicit router name from config
		if rName := oaMod.RouterName(); rName != "" {
			targetRouter = routers[rName]
		}

		// 2) dependsOn router reference
		if targetRouter == nil {
			for _, dep := range openAPIDeps[oaMod.Name()] {
				if routerNames[dep] {
					if router, found := routers[dep]; found {
						targetRouter = router
						break
					}
				}
			}
		}

		// 3) Fall back to first available router
		if targetRouter == nil {
			targetRouter = firstRouter
		}

		if targetRouter == nil {
			// No router found — log a warning and skip (not fatal; engine may be running without HTTP).
			slog.Warn("openapi: no HTTP router found; skipping route registration",
				"module", oaMod.Name(),
			)
			continue
		}

		oaMod.RegisterRoutes(targetRouter)
	}

	return nil
}
