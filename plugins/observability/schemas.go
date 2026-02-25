package observability

import "github.com/GoCodeAlone/workflow/schema"

// moduleSchemas returns the UI schema definitions for observability module types.
func moduleSchemas() []*schema.ModuleSchema {
	return []*schema.ModuleSchema{
		{
			Type:        "metrics.collector",
			Label:       "Metrics Collector",
			Category:    "observability",
			Description: "Collects and exposes application metrics",
			Outputs:     []schema.ServiceIODef{{Name: "metrics", Type: "prometheus.Metrics", Description: "Prometheus metrics endpoint"}},
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "namespace", Label: "Namespace", Type: schema.FieldTypeString, DefaultValue: "workflow", Description: "Prometheus metric namespace prefix", Placeholder: "workflow"},
				{Key: "subsystem", Label: "Subsystem", Type: schema.FieldTypeString, Description: "Prometheus metric subsystem", Placeholder: "api"},
				{Key: "metricsPath", Label: "Metrics Path", Type: schema.FieldTypeString, DefaultValue: "/metrics", Description: "Endpoint path for Prometheus scraping", Placeholder: "/metrics"},
				{Key: "enabledMetrics", Label: "Enabled Metrics", Type: schema.FieldTypeArray, ArrayItemType: "string", DefaultValue: []string{"workflow", "http", "module", "active_workflows"}, Description: "Which metric groups to register (workflow, http, module, active_workflows)"},
			},
			DefaultConfig: map[string]any{"namespace": "workflow", "metricsPath": "/metrics", "enabledMetrics": []string{"workflow", "http", "module", "active_workflows"}},
		},
		{
			Type:        "health.checker",
			Label:       "Health Checker",
			Category:    "observability",
			Description: "Health check endpoint for liveness/readiness probes",
			Outputs:     []schema.ServiceIODef{{Name: "health", Type: "HealthStatus", Description: "Health check status endpoint"}},
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "healthPath", Label: "Health Path", Type: schema.FieldTypeString, DefaultValue: "/healthz", Description: "Health check endpoint path", Placeholder: "/healthz"},
				{Key: "readyPath", Label: "Ready Path", Type: schema.FieldTypeString, DefaultValue: "/readyz", Description: "Readiness probe endpoint path", Placeholder: "/readyz"},
				{Key: "livePath", Label: "Live Path", Type: schema.FieldTypeString, DefaultValue: "/livez", Description: "Liveness probe endpoint path", Placeholder: "/livez"},
				{Key: "checkTimeout", Label: "Check Timeout", Type: schema.FieldTypeDuration, DefaultValue: "5s", Description: "Per-check timeout duration", Placeholder: "5s"},
				{Key: "autoDiscover", Label: "Auto-Discover", Type: schema.FieldTypeBool, DefaultValue: true, Description: "Automatically discover HealthCheckable services"},
			},
			DefaultConfig: map[string]any{"healthPath": "/healthz", "readyPath": "/readyz", "livePath": "/livez", "checkTimeout": "5s", "autoDiscover": true},
		},
		{
			Type:        "observability.otel",
			Label:       "OpenTelemetry",
			Category:    "observability",
			Description: "OpenTelemetry tracing integration for distributed tracing",
			Inputs:      []schema.ServiceIODef{{Name: "span", Type: "trace.Span", Description: "Trace spans from instrumented code"}},
			Outputs:     []schema.ServiceIODef{{Name: "tracer", Type: "trace.Tracer", Description: "OpenTelemetry tracer for distributed tracing"}},
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "endpoint", Label: "OTLP Endpoint", Type: schema.FieldTypeString, DefaultValue: "localhost:4318", Description: "OpenTelemetry collector endpoint", Placeholder: "localhost:4318"},
				{Key: "serviceName", Label: "Service Name", Type: schema.FieldTypeString, DefaultValue: "workflow", Description: "Service name for trace attribution", Placeholder: "workflow"},
			},
			DefaultConfig: map[string]any{"endpoint": "localhost:4318", "serviceName": "workflow"},
		},
		{
			Type:        "log.collector",
			Label:       "Log Collector",
			Category:    "observability",
			Description: "Centralized log collection from all modules, auto-wires to the first available router at /logs",
			Inputs:      []schema.ServiceIODef{{Name: "logEntry", Type: "LogEntry", Description: "Log entries from modules"}},
			Outputs:     []schema.ServiceIODef{{Name: "logs", Type: "[]LogEntry", Description: "Aggregated log entries"}},
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "logLevel", Label: "Log Level", Type: schema.FieldTypeSelect, Options: []string{"debug", "info", "warn", "error"}, DefaultValue: "info", Description: "Minimum log level to collect"},
				{Key: "outputFormat", Label: "Output Format", Type: schema.FieldTypeSelect, Options: []string{"json", "text"}, DefaultValue: "json", Description: "Format for log output"},
				{Key: "retentionDays", Label: "Retention Days", Type: schema.FieldTypeNumber, DefaultValue: 7, Description: "Number of days to retain log entries"},
			},
			DefaultConfig: map[string]any{"logLevel": "info", "outputFormat": "json", "retentionDays": 7},
		},
		{
			Type:        "openapi.generator",
			Label:       "OpenAPI Generator",
			Category:    "integration",
			Description: "Scans workflow route definitions to generate an OpenAPI 3.0 spec, served at /api/openapi.json and /api/openapi.yaml",
			Inputs:      []schema.ServiceIODef{{Name: "routes", Type: "RouteConfig", Description: "Workflow route definitions to scan"}},
			Outputs:     []schema.ServiceIODef{{Name: "spec", Type: "OpenAPISpec", Description: "Generated OpenAPI 3.0 specification"}},
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "title", Label: "API Title", Type: schema.FieldTypeString, DefaultValue: "Workflow API", Description: "Title for the OpenAPI spec", Placeholder: "My API"},
				{Key: "version", Label: "API Version", Type: schema.FieldTypeString, DefaultValue: "1.0.0", Description: "Version string for the OpenAPI spec", Placeholder: "1.0.0"},
				{Key: "description", Label: "Description", Type: schema.FieldTypeString, Description: "Description of the API", Placeholder: "API generated from workflow routes"},
				{Key: "servers", Label: "Server URLs", Type: schema.FieldTypeArray, ArrayItemType: "string", Description: "List of server URLs to include in the spec", Placeholder: "http://localhost:8080"},
			},
			DefaultConfig: map[string]any{"title": "Workflow API", "version": "1.0.0"},
		},
		{
			Type:        "http.middleware.otel",
			Label:       "OTEL HTTP Middleware",
			Category:    "observability",
			Description: "Instruments HTTP requests with OpenTelemetry tracing spans",
			ConfigFields: []schema.ConfigFieldDef{
				{Key: "serverName", Label: "Server Name", Type: schema.FieldTypeString, DefaultValue: "workflow-http", Description: "Server name used as the span operation name", Placeholder: "workflow-http"},
			},
			DefaultConfig: map[string]any{"serverName": "workflow-http"},
		},
	}
}
