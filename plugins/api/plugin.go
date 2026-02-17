package api

import (
	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/schema"
)

// Plugin provides REST API and CQRS capabilities: api.query, api.command,
// api.handler, api.gateway, workflow.registry, data.transformer,
// and processing.step modules.
type Plugin struct {
	plugin.BaseEnginePlugin
}

// New creates a new API plugin.
func New() *Plugin {
	return &Plugin{
		BaseEnginePlugin: plugin.BaseEnginePlugin{
			BaseNativePlugin: plugin.BaseNativePlugin{
				PluginName:        "api",
				PluginVersion:     "1.0.0",
				PluginDescription: "REST API handlers, CQRS query/command, API gateway, and data transformation",
			},
			Manifest: plugin.PluginManifest{
				Name:        "api",
				Version:     "1.0.0",
				Author:      "GoCodeAlone",
				Description: "REST API handlers, CQRS query/command, API gateway, and data transformation",
				ModuleTypes: []string{
					"api.query",
					"api.command",
					"api.handler",
					"api.gateway",
					"workflow.registry",
					"data.transformer",
					"processing.step",
				},
				Capabilities: []plugin.CapabilityDecl{
					{Name: "rest-api", Role: "provider", Priority: 10},
					{Name: "cqrs", Role: "provider", Priority: 10},
					{Name: "api-gateway", Role: "provider", Priority: 10},
				},
			},
		},
	}
}

// Capabilities returns the capability contracts this plugin defines.
func (p *Plugin) Capabilities() []capability.Contract {
	return []capability.Contract{
		{
			Name:        "rest-api",
			Description: "REST API handler for resource CRUD with optional state machine integration",
		},
		{
			Name:        "cqrs",
			Description: "CQRS query and command handlers for read/write separation",
		},
		{
			Name:        "api-gateway",
			Description: "API gateway with routing, auth, rate limiting, CORS, and reverse proxying",
		},
	}
}

// getStringConfig extracts a string value from a config map with a default.
func getStringConfig(cfg map[string]any, key, defaultVal string) string {
	if v, ok := cfg[key].(string); ok {
		return v
	}
	return defaultVal
}

// getIntConfig extracts an int value from a config map with a default.
// Handles both int and float64 (YAML numbers are decoded as float64).
func getIntConfig(cfg map[string]any, key string, defaultVal int) int {
	if v, ok := cfg[key].(int); ok {
		return v
	}
	if v, ok := cfg[key].(float64); ok {
		return int(v)
	}
	return defaultVal
}

// ModuleFactories returns factories for all API module types.
func (p *Plugin) ModuleFactories() map[string]plugin.ModuleFactory {
	return map[string]plugin.ModuleFactory{
		"api.query": func(name string, cfg map[string]any) modular.Module {
			qh := module.NewQueryHandler(name)
			if delegate, ok := cfg["delegate"].(string); ok && delegate != "" {
				qh.SetDelegate(delegate)
			}
			return qh
		},
		"api.command": func(name string, cfg map[string]any) modular.Module {
			ch := module.NewCommandHandler(name)
			if delegate, ok := cfg["delegate"].(string); ok && delegate != "" {
				ch.SetDelegate(delegate)
			}
			return ch
		},
		"api.handler": func(name string, cfg map[string]any) modular.Module {
			resourceName := "resources"
			if rn, ok := cfg["resourceName"].(string); ok {
				resourceName = rn
			}
			handler := module.NewRESTAPIHandler(name, resourceName)
			if wt, ok := cfg["workflowType"].(string); ok && wt != "" {
				handler.SetWorkflowType(wt)
			}
			if we, ok := cfg["workflowEngine"].(string); ok && we != "" {
				handler.SetWorkflowEngine(we)
			}
			if it, ok := cfg["initialTransition"].(string); ok && it != "" {
				handler.SetInitialTransition(it)
			}
			if sf, ok := cfg["seedFile"].(string); ok && sf != "" {
				handler.SetSeedFile(sf)
			}
			if src, ok := cfg["sourceResourceName"].(string); ok && src != "" {
				handler.SetSourceResourceName(src)
			}
			if stf, ok := cfg["stateFilter"].(string); ok && stf != "" {
				handler.SetStateFilter(stf)
			}
			// Dynamic field mapping (optional YAML override of default field names)
			if fmCfg, ok := cfg["fieldMapping"].(map[string]any); ok {
				override := module.FieldMappingFromConfig(fmCfg)
				defaults := module.DefaultRESTFieldMapping()
				defaults.Merge(override)
				handler.SetFieldMapping(defaults)
			}
			// Custom sub-action to transition mapping
			if tmCfg, ok := cfg["transitionMap"].(map[string]any); ok {
				tm := module.DefaultTransitionMap()
				for action, trans := range tmCfg {
					if t, ok := trans.(string); ok {
						tm[action] = t
					}
				}
				handler.SetTransitionMap(tm)
			}
			// Custom summary fields
			if sfCfg, ok := cfg["summaryFields"].([]any); ok {
				fields := make([]string, 0, len(sfCfg))
				for _, f := range sfCfg {
					if s, ok := f.(string); ok {
						fields = append(fields, s)
					}
				}
				if len(fields) > 0 {
					handler.SetSummaryFields(fields)
				}
			}
			return handler
		},
		"api.gateway": func(name string, cfg map[string]any) modular.Module {
			gw := module.NewAPIGateway(name)
			// Parse routes
			if routesCfg, ok := cfg["routes"].([]any); ok {
				var routes []module.GatewayRoute
				for _, rc := range routesCfg {
					if rm, ok := rc.(map[string]any); ok {
						route := module.GatewayRoute{}
						if v, ok := rm["pathPrefix"].(string); ok {
							route.PathPrefix = v
						}
						if v, ok := rm["backend"].(string); ok {
							route.Backend = v
						}
						if v, ok := rm["stripPrefix"].(bool); ok {
							route.StripPrefix = v
						}
						if v, ok := rm["auth"].(bool); ok {
							route.Auth = v
						}
						if v, ok := rm["timeout"].(string); ok {
							route.Timeout = v
						}
						if methods, ok := rm["methods"].([]any); ok {
							for _, m := range methods {
								if s, ok := m.(string); ok {
									route.Methods = append(route.Methods, s)
								}
							}
						}
						if rlCfg, ok := rm["rateLimit"].(map[string]any); ok {
							rl := &module.RateLimitConfig{}
							if v, ok := rlCfg["requestsPerMinute"].(float64); ok {
								rl.RequestsPerMinute = int(v)
							}
							if v, ok := rlCfg["burstSize"].(float64); ok {
								rl.BurstSize = int(v)
							}
							route.RateLimit = rl
						}
						routes = append(routes, route)
					}
				}
				_ = gw.SetRoutes(routes)
			}
			// Global rate limit
			if glCfg, ok := cfg["globalRateLimit"].(map[string]any); ok {
				rl := &module.RateLimitConfig{}
				if v, ok := glCfg["requestsPerMinute"].(float64); ok {
					rl.RequestsPerMinute = int(v)
				}
				if v, ok := glCfg["burstSize"].(float64); ok {
					rl.BurstSize = int(v)
				}
				gw.SetGlobalRateLimit(rl)
			}
			// CORS
			if corsCfg, ok := cfg["cors"].(map[string]any); ok {
				cors := &module.CORSConfig{}
				if origins, ok := corsCfg["allowOrigins"].([]any); ok {
					for _, o := range origins {
						if s, ok := o.(string); ok {
							cors.AllowOrigins = append(cors.AllowOrigins, s)
						}
					}
				}
				if methods, ok := corsCfg["allowMethods"].([]any); ok {
					for _, m := range methods {
						if s, ok := m.(string); ok {
							cors.AllowMethods = append(cors.AllowMethods, s)
						}
					}
				}
				if headers, ok := corsCfg["allowHeaders"].([]any); ok {
					for _, h := range headers {
						if s, ok := h.(string); ok {
							cors.AllowHeaders = append(cors.AllowHeaders, s)
						}
					}
				}
				if v, ok := corsCfg["maxAge"].(float64); ok {
					cors.MaxAge = int(v)
				}
				gw.SetCORS(cors)
			}
			// Auth
			if authCfg, ok := cfg["auth"].(map[string]any); ok {
				ac := &module.AuthConfig{}
				if v, ok := authCfg["type"].(string); ok {
					ac.Type = v
				}
				if v, ok := authCfg["header"].(string); ok {
					ac.Header = v
				}
				gw.SetAuth(ac)
			}
			return gw
		},
		"workflow.registry": func(name string, cfg map[string]any) modular.Module {
			storageBackend := ""
			if sb, ok := cfg["storageBackend"].(string); ok && sb != "" {
				storageBackend = sb
			}
			return module.NewWorkflowRegistry(name, storageBackend)
		},
		"data.transformer": func(name string, _ map[string]any) modular.Module {
			return module.NewDataTransformer(name)
		},
		"processing.step": func(name string, cfg map[string]any) modular.Module {
			stepConfig := module.ProcessingStepConfig{
				ComponentID:          getStringConfig(cfg, "componentId", ""),
				SuccessTransition:    getStringConfig(cfg, "successTransition", ""),
				CompensateTransition: getStringConfig(cfg, "compensateTransition", ""),
				MaxRetries:           getIntConfig(cfg, "maxRetries", 2),
				RetryBackoffMs:       getIntConfig(cfg, "retryBackoffMs", 1000),
				TimeoutSeconds:       getIntConfig(cfg, "timeoutSeconds", 30),
			}
			return module.NewProcessingStep(name, stepConfig)
		},
	}
}

// ModuleSchemas returns UI schema definitions for API module types.
func (p *Plugin) ModuleSchemas() []*schema.ModuleSchema {
	return []*schema.ModuleSchema{
		{
			Type:        "api.query",
			Label:       "Query Handler",
			Category:    "http",
			Description: "Dispatches GET requests to named read-only query functions",
			Inputs:      []schema.ServiceIODef{{Name: "request", Type: "http.Request", Description: "HTTP GET request to dispatch"}},
			Outputs:     []schema.ServiceIODef{{Name: "response", Type: "JSON", Description: "JSON query result"}},
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "delegate", Label: "Delegate Service", Type: schema.FieldTypeString, Description: "Name of a service (implementing http.Handler) to delegate unmatched requests to", Placeholder: "my-service-name", InheritFrom: "dependency.name"},
				{Key: "routes", Label: "Route Pipelines", Type: schema.FieldTypeJSON, Description: "Per-route processing pipelines with composable steps (validate, transform, http_call, etc.)", Group: "routes"},
			},
		},
		{
			Type:        "api.command",
			Label:       "Command Handler",
			Category:    "http",
			Description: "Dispatches POST/PUT/DELETE requests to named state-changing command functions",
			Inputs:      []schema.ServiceIODef{{Name: "request", Type: "http.Request", Description: "HTTP request for state-changing operation"}},
			Outputs:     []schema.ServiceIODef{{Name: "response", Type: "JSON", Description: "JSON command result"}},
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "delegate", Label: "Delegate Service", Type: schema.FieldTypeString, Description: "Name of a service (implementing http.Handler) to delegate unmatched requests to", Placeholder: "my-service-name", InheritFrom: "dependency.name"},
				{Key: "routes", Label: "Route Pipelines", Type: schema.FieldTypeJSON, Description: "Per-route processing pipelines with composable steps (validate, transform, http_call, etc.)", Group: "routes"},
			},
		},
		{
			Type:        "api.handler",
			Label:       "REST API Handler",
			Category:    "http",
			Description: "Full REST API handler for a resource, with optional state machine integration",
			Inputs:      []schema.ServiceIODef{{Name: "request", Type: "http.Request", Description: "HTTP request for resource CRUD"}},
			Outputs:     []schema.ServiceIODef{{Name: "response", Type: "JSON", Description: "JSON response with resource data"}},
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "resourceName", Label: "Resource Name", Type: schema.FieldTypeString, Description: "Name of the resource to manage (e.g. orders, users)", DefaultValue: "resources", Placeholder: "orders"},
				{Key: "workflowType", Label: "Workflow Type", Type: schema.FieldTypeString, Description: "Workflow type for state machine operations", Placeholder: "order-processing"},
				{Key: "workflowEngine", Label: "Workflow Engine", Type: schema.FieldTypeString, Description: "Name of the workflow engine service to use", Placeholder: "statemachine-engine", InheritFrom: "dependency.name"},
				{Key: "initialTransition", Label: "Initial Transition", Type: schema.FieldTypeString, Description: "State transition to trigger after resource creation", Placeholder: "submit"},
				{Key: "seedFile", Label: "Seed Data File", Type: schema.FieldTypeString, Description: "Path to a JSON file with initial resource data", Placeholder: "data/seed.json"},
				{Key: "sourceResourceName", Label: "Source Resource", Type: schema.FieldTypeString, Description: "Alternative resource name to read from (for derived views)"},
				{Key: "stateFilter", Label: "State Filter", Type: schema.FieldTypeString, Description: "Only show resources in this state", Placeholder: "active"},
				{Key: "fieldMapping", Label: "Field Mapping", Type: schema.FieldTypeMap, MapValueType: "string", Description: "Custom field name mapping (e.g. id -> order_id, status -> state)", Group: "advanced"},
				{Key: "transitionMap", Label: "Transition Map", Type: schema.FieldTypeMap, MapValueType: "string", Description: "Map of sub-action names to state transitions (e.g. approve -> approved)", Group: "advanced"},
				{Key: "summaryFields", Label: "Summary Fields", Type: schema.FieldTypeArray, ArrayItemType: "string", Description: "Field names to include in list/summary responses", Group: "advanced"},
			},
			DefaultConfig: map[string]any{"resourceName": "resources"},
		},
		{
			Type:        "api.gateway",
			Label:       "API Gateway",
			Category:    "http",
			Description: "Composable API gateway combining routing, auth, rate limiting, CORS, and reverse proxying into a single module",
			Inputs:      []schema.ServiceIODef{{Name: "http_request", Type: "http.Request", Description: "Incoming HTTP request"}},
			Outputs:     []schema.ServiceIODef{{Name: "http_response", Type: "http.Response", Description: "Proxied response from backend"}},
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "routes", Label: "Routes", Type: schema.FieldTypeJSON, Required: true, Description: "Array of route definitions with pathPrefix, backend, methods, rateLimit, auth, timeout"},
				{Key: "globalRateLimit", Label: "Global Rate Limit", Type: schema.FieldTypeJSON, Description: "Global rate limit applied to all routes (requestsPerMinute, burstSize)"},
				{Key: "cors", Label: "CORS Config", Type: schema.FieldTypeJSON, Description: "CORS settings (allowOrigins, allowMethods, allowHeaders, maxAge)"},
				{Key: "auth", Label: "Auth Config", Type: schema.FieldTypeJSON, Description: "Authentication settings (type: bearer/api_key/basic, header)"},
			},
		},
		{
			Type:        "workflow.registry",
			Label:       "Workflow Registry",
			Category:    "infrastructure",
			Description: "SQLite-backed registry for companies, organizations, projects, and workflows",
			Inputs:      []schema.ServiceIODef{{Name: "storageBackend", Type: "SQLiteStorage", Description: "Optional shared SQLite storage service name"}},
			Outputs:     []schema.ServiceIODef{{Name: "registry", Type: "WorkflowRegistry", Description: "Workflow data registry service"}},
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "storageBackend", Label: "Storage Backend", Type: schema.FieldTypeString, DefaultValue: "", Description: "Name of a storage.sqlite module to share its DB connection (leave empty for standalone DB)", Placeholder: "admin-db", InheritFrom: "dependency.name"},
			},
			DefaultConfig: map[string]any{"storageBackend": ""},
		},
		{
			Type:         "data.transformer",
			Label:        "Data Transformer",
			Category:     "integration",
			Description:  "Transforms data between formats using configurable pipelines",
			Inputs:       []schema.ServiceIODef{{Name: "input", Type: "any", Description: "Input data to transform"}},
			Outputs:      []schema.ServiceIODef{{Name: "output", Type: "any", Description: "Transformed output data"}},
			ConfigFields: []schema.ConfigFieldDef{},
		},
		{
			Type:        "processing.step",
			Label:       "Processing Step",
			Category:    "statemachine",
			Description: "Executes a component as a processing step in a workflow, with retry and compensation",
			Inputs:      []schema.ServiceIODef{{Name: "input", Type: "any", Description: "Input data for the processing step"}},
			Outputs: []schema.ServiceIODef{
				{Name: "result", Type: "any", Description: "Processing result on success"},
				{Name: "transition", Type: "string", Description: "State transition triggered (success or compensate)"},
			},
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "componentId", Label: "Component ID", Type: schema.FieldTypeString, Required: true, Description: "Service name of the component to execute", InheritFrom: "dependency.name"},
				{Key: "successTransition", Label: "Success Transition", Type: schema.FieldTypeString, Description: "State transition to trigger on success", Placeholder: "completed"},
				{Key: "compensateTransition", Label: "Compensate Transition", Type: schema.FieldTypeString, Description: "State transition to trigger on failure for compensation", Placeholder: "failed"},
				{Key: "maxRetries", Label: "Max Retries", Type: schema.FieldTypeNumber, DefaultValue: 2, Description: "Maximum number of retry attempts"},
				{Key: "retryBackoffMs", Label: "Retry Backoff (ms)", Type: schema.FieldTypeNumber, DefaultValue: 1000, Description: "Base backoff duration in milliseconds between retries"},
				{Key: "timeoutSeconds", Label: "Timeout (sec)", Type: schema.FieldTypeNumber, DefaultValue: 30, Description: "Maximum execution time per attempt in seconds"},
			},
			DefaultConfig: map[string]any{"maxRetries": 2, "retryBackoffMs": 1000, "timeoutSeconds": 30},
		},
	}
}
