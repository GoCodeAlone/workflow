package schema

import "sort"

// ConfigFieldType represents the type of a configuration field.
type ConfigFieldType string

const (
	FieldTypeString   ConfigFieldType = "string"
	FieldTypeNumber   ConfigFieldType = "number"
	FieldTypeBool     ConfigFieldType = "boolean"
	FieldTypeSelect   ConfigFieldType = "select"
	FieldTypeJSON     ConfigFieldType = "json"
	FieldTypeDuration ConfigFieldType = "duration"
	FieldTypeArray    ConfigFieldType = "array"
	FieldTypeMap      ConfigFieldType = "map"
	FieldTypeFilePath ConfigFieldType = "filepath"
	FieldTypeSQL      ConfigFieldType = "sql"
)

// ConfigFieldDef describes a single configuration field for a module type.
type ConfigFieldDef struct {
	Key           string          `json:"key"`
	Label         string          `json:"label"`
	Type          ConfigFieldType `json:"type"`
	Description   string          `json:"description,omitempty"`
	Required      bool            `json:"required,omitempty"`
	DefaultValue  any             `json:"defaultValue,omitempty"`
	Options       []string        `json:"options,omitempty"` // for select type
	Placeholder   string          `json:"placeholder,omitempty"`
	Group         string          `json:"group,omitempty"`         // field grouping in UI
	ArrayItemType string          `json:"arrayItemType,omitempty"` // element type for array fields ("string", "number", etc.)
	MapValueType  string          `json:"mapValueType,omitempty"`  // value type for map fields ("string", "number", etc.)
	InheritFrom   string          `json:"inheritFrom,omitempty"`   // "{edgeType}.{sourceField}" pattern for config inheritance from connected nodes
	Sensitive     bool            `json:"sensitive,omitempty"`     // when true, the UI renders this as a password field with visibility toggle
}

// ServiceIODef describes a single input or output service port for a module type.
type ServiceIODef struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

// ModuleSchema describes the full configuration schema for a module type.
type ModuleSchema struct {
	Type          string           `json:"type"`
	Label         string           `json:"label"`
	Category      string           `json:"category"`
	Description   string           `json:"description,omitempty"`
	Inputs        []ServiceIODef   `json:"inputs,omitempty"`
	Outputs       []ServiceIODef   `json:"outputs,omitempty"`
	ConfigFields  []ConfigFieldDef `json:"configFields"`
	DefaultConfig map[string]any   `json:"defaultConfig,omitempty"`
	MaxIncoming   *int             `json:"maxIncoming,omitempty"` // nil=unlimited, 0=none, N=limit
	MaxOutgoing   *int             `json:"maxOutgoing,omitempty"`
}

// ModuleSchemaRegistry holds all known module configuration schemas.
type ModuleSchemaRegistry struct {
	schemas map[string]*ModuleSchema
}

// NewModuleSchemaRegistry creates a new registry with all built-in module schemas pre-registered.
func NewModuleSchemaRegistry() *ModuleSchemaRegistry {
	r := &ModuleSchemaRegistry{schemas: make(map[string]*ModuleSchema)}
	r.registerBuiltins()
	return r
}

// Register adds or replaces a module schema.
func (r *ModuleSchemaRegistry) Register(s *ModuleSchema) {
	r.schemas[s.Type] = s
}

// Unregister removes a module schema by type. Intended for cleanup during testing.
func (r *ModuleSchemaRegistry) Unregister(moduleType string) {
	delete(r.schemas, moduleType)
}

// Get returns the schema for a module type, or nil if not found.
func (r *ModuleSchemaRegistry) Get(moduleType string) *ModuleSchema {
	return r.schemas[moduleType]
}

// All returns all registered schemas as a slice.
func (r *ModuleSchemaRegistry) All() []*ModuleSchema {
	out := make([]*ModuleSchema, 0, len(r.schemas))
	for _, s := range r.schemas {
		out = append(out, s)
	}
	return out
}

// AllMap returns all registered schemas as a map keyed by module type.
func (r *ModuleSchemaRegistry) AllMap() map[string]*ModuleSchema {
	out := make(map[string]*ModuleSchema, len(r.schemas))
	for k, v := range r.schemas {
		out[k] = v
	}
	return out
}

// Types returns a sorted list of all registered module type identifiers.
// This is the dynamic equivalent of KnownModuleTypes — it includes both
// built-in types and any types registered at runtime by plugins.
func (r *ModuleSchemaRegistry) Types() []string {
	types := make([]string, 0, len(r.schemas))
	for t := range r.schemas {
		types = append(types, t)
	}
	sort.Strings(types)
	return types
}

func intPtr(v int) *int { return &v }

func (r *ModuleSchemaRegistry) registerBuiltins() {
	// ---- HTTP Category ----

	r.Register(&ModuleSchema{
		Type:        "http.server",
		Label:       "HTTP Server",
		Category:    "http",
		Description: "Standard HTTP server that listens on a network address",
		Outputs:     []ServiceIODef{{Name: "request", Type: "http.Request", Description: "Incoming HTTP requests"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "address", Label: "Listen Address", Type: FieldTypeString, Required: true, Description: "Host:port to listen on (e.g. :8080, 0.0.0.0:80)", DefaultValue: ":8080", Placeholder: ":8080"},
		},
		DefaultConfig: map[string]any{"address": ":8080"},
		MaxIncoming:   intPtr(0),
	})

	r.Register(&ModuleSchema{
		Type:         "http.router",
		Label:        "HTTP Router",
		Category:     "http",
		Description:  "Routes HTTP requests to handlers based on path and method",
		Inputs:       []ServiceIODef{{Name: "request", Type: "http.Request", Description: "Incoming HTTP request to route"}},
		Outputs:      []ServiceIODef{{Name: "routed", Type: "http.Request", Description: "Routed HTTP request dispatched to handler"}},
		ConfigFields: []ConfigFieldDef{},
	})

	r.Register(&ModuleSchema{
		Type:        "http.handler",
		Label:       "HTTP Handler",
		Category:    "http",
		Description: "Handles HTTP requests and produces responses",
		Inputs:      []ServiceIODef{{Name: "request", Type: "http.Request", Description: "HTTP request to handle"}},
		Outputs:     []ServiceIODef{{Name: "response", Type: "http.Response", Description: "HTTP response"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "contentType", Label: "Content Type", Type: FieldTypeString, Description: "Response content type", DefaultValue: "application/json", Placeholder: "application/json"},
		},
		DefaultConfig: map[string]any{"contentType": "application/json"},
	})

	r.Register(&ModuleSchema{
		Type:         "http.proxy",
		Label:        "HTTP Proxy",
		Category:     "http",
		Description:  "Reverse proxy using the GoCodeAlone/modular reverseproxy module",
		Inputs:       []ServiceIODef{{Name: "request", Type: "http.Request", Description: "HTTP request to proxy"}},
		Outputs:      []ServiceIODef{{Name: "proxied", Type: "http.Response", Description: "Proxied HTTP response"}},
		ConfigFields: []ConfigFieldDef{},
	})

	r.Register(&ModuleSchema{
		Type:         "reverseproxy",
		Label:        "Reverse Proxy",
		Category:     "http",
		Description:  "Reverse proxy using the GoCodeAlone/modular reverseproxy module",
		Inputs:       []ServiceIODef{{Name: "request", Type: "http.Request", Description: "HTTP request to proxy"}},
		Outputs:      []ServiceIODef{{Name: "proxied", Type: "http.Response", Description: "Proxied HTTP response"}},
		ConfigFields: []ConfigFieldDef{},
	})

	r.Register(&ModuleSchema{
		Type:        "http.simple_proxy",
		Label:       "Simple Proxy",
		Category:    "http",
		Description: "Simple reverse proxy with prefix-based target routing",
		Inputs:      []ServiceIODef{{Name: "request", Type: "http.Request", Description: "HTTP request to proxy"}},
		Outputs:     []ServiceIODef{{Name: "proxied", Type: "http.Response", Description: "Proxied HTTP response"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "targets", Label: "Targets", Type: FieldTypeMap, MapValueType: "string", Description: "Map of URL prefix to backend URL (e.g. /api -> http://localhost:3000)", Placeholder: "/api=http://backend:8080"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "static.fileserver",
		Label:       "Static File Server",
		Category:    "http",
		Description: "Serves static files from a directory with optional SPA fallback",
		Inputs:      []ServiceIODef{{Name: "request", Type: "http.Request", Description: "HTTP request for static file"}},
		Outputs:     []ServiceIODef{{Name: "file", Type: "http.Response", Description: "Static file response"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "root", Label: "Root Directory", Type: FieldTypeString, Required: true, Description: "Path to the directory containing static files", Placeholder: "./ui/dist"},
			{Key: "prefix", Label: "URL Prefix", Type: FieldTypeString, DefaultValue: "/", Description: "URL path prefix to serve files under", Placeholder: "/"},
			{Key: "spaFallback", Label: "SPA Fallback", Type: FieldTypeBool, DefaultValue: true, Description: "When enabled, serves index.html for unmatched paths (for single-page apps)"},
			{Key: "cacheMaxAge", Label: "Cache Max-Age (sec)", Type: FieldTypeNumber, DefaultValue: 3600, Description: "Cache-Control max-age in seconds for static assets"},
			{Key: "router", Label: "Router Name", Type: FieldTypeString, Description: "Explicit router module name to register on (auto-detected if omitted)", Placeholder: "my-router", InheritFrom: "dependency.name"},
		},
		DefaultConfig: map[string]any{"prefix": "/", "spaFallback": true, "cacheMaxAge": 3600},
	})

	// ---- API Category ----

	r.Register(&ModuleSchema{
		Type:        "api.handler",
		Label:       "REST API Handler",
		Category:    "http",
		Description: "Full REST API handler for a resource, with optional state machine integration",
		Inputs:      []ServiceIODef{{Name: "request", Type: "http.Request", Description: "HTTP request for resource CRUD"}},
		Outputs:     []ServiceIODef{{Name: "response", Type: "JSON", Description: "JSON response with resource data"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "resourceName", Label: "Resource Name", Type: FieldTypeString, Description: "Name of the resource to manage (e.g. orders, users)", DefaultValue: "resources", Placeholder: "orders"},
			{Key: "workflowType", Label: "Workflow Type", Type: FieldTypeString, Description: "Workflow type for state machine operations", Placeholder: "order-processing"},
			{Key: "workflowEngine", Label: "Workflow Engine", Type: FieldTypeString, Description: "Name of the workflow engine service to use", Placeholder: "statemachine-engine", InheritFrom: "dependency.name"},
			{Key: "initialTransition", Label: "Initial Transition", Type: FieldTypeString, Description: "State transition to trigger after resource creation", Placeholder: "submit"},
			{Key: "seedFile", Label: "Seed Data File", Type: FieldTypeString, Description: "Path to a JSON file with initial resource data", Placeholder: "data/seed.json"},
			{Key: "sourceResourceName", Label: "Source Resource", Type: FieldTypeString, Description: "Alternative resource name to read from (for derived views)"},
			{Key: "stateFilter", Label: "State Filter", Type: FieldTypeString, Description: "Only show resources in this state", Placeholder: "active"},
			{Key: "fieldMapping", Label: "Field Mapping", Type: FieldTypeMap, MapValueType: "string", Description: "Custom field name mapping (e.g. id -> order_id, status -> state)", Group: "advanced"},
			{Key: "transitionMap", Label: "Transition Map", Type: FieldTypeMap, MapValueType: "string", Description: "Map of sub-action names to state transitions (e.g. approve -> approved)", Group: "advanced"},
			{Key: "summaryFields", Label: "Summary Fields", Type: FieldTypeArray, ArrayItemType: "string", Description: "Field names to include in list/summary responses", Group: "advanced"},
		},
		DefaultConfig: map[string]any{"resourceName": "resources"},
	})

	// ---- CQRS API Category ----

	r.Register(&ModuleSchema{
		Type:        "api.query",
		Label:       "Query Handler",
		Category:    "http",
		Description: "Dispatches GET requests to named read-only query functions",
		Inputs:      []ServiceIODef{{Name: "request", Type: "http.Request", Description: "HTTP GET request to dispatch"}},
		Outputs:     []ServiceIODef{{Name: "response", Type: "JSON", Description: "JSON query result"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "delegate", Label: "Delegate Service", Type: FieldTypeString, Description: "Name of a service (implementing http.Handler) to delegate unmatched requests to", Placeholder: "my-service-name", InheritFrom: "dependency.name"},
			{Key: "routes", Label: "Route Pipelines", Type: FieldTypeArray, Description: "Per-route processing pipelines with composable steps (validate, transform, http_call, etc.)", Group: "routes"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "api.command",
		Label:       "Command Handler",
		Category:    "http",
		Description: "Dispatches POST/PUT/DELETE requests to named state-changing command functions",
		Inputs:      []ServiceIODef{{Name: "request", Type: "http.Request", Description: "HTTP request for state-changing operation"}},
		Outputs:     []ServiceIODef{{Name: "response", Type: "JSON", Description: "JSON command result"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "delegate", Label: "Delegate Service", Type: FieldTypeString, Description: "Name of a service (implementing http.Handler) to delegate unmatched requests to", Placeholder: "my-service-name", InheritFrom: "dependency.name"},
			{Key: "routes", Label: "Route Pipelines", Type: FieldTypeArray, Description: "Per-route processing pipelines with composable steps (validate, transform, http_call, etc.)", Group: "routes"},
		},
	})

	// ---- Middleware Category ----

	r.Register(&ModuleSchema{
		Type:        "http.middleware.auth",
		Label:       "Auth Middleware",
		Category:    "middleware",
		Description: "Authentication middleware that validates tokens on incoming requests",
		Inputs:      []ServiceIODef{{Name: "request", Type: "http.Request", Description: "Unauthenticated HTTP request"}},
		Outputs:     []ServiceIODef{{Name: "authed", Type: "http.Request", Description: "Authenticated HTTP request with claims"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "authType", Label: "Auth Type", Type: FieldTypeSelect, Options: []string{"Bearer", "Basic", "ApiKey"}, DefaultValue: "Bearer", Description: "Authentication scheme to enforce"},
		},
		DefaultConfig: map[string]any{"authType": "Bearer"},
	})

	r.Register(&ModuleSchema{
		Type:        "http.middleware.logging",
		Label:       "Logging Middleware",
		Category:    "middleware",
		Description: "HTTP request/response logging middleware",
		Inputs:      []ServiceIODef{{Name: "request", Type: "http.Request", Description: "HTTP request to log"}},
		Outputs:     []ServiceIODef{{Name: "logged", Type: "http.Request", Description: "HTTP request (passed through after logging)"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "logLevel", Label: "Log Level", Type: FieldTypeSelect, Options: []string{"debug", "info", "warn", "error"}, DefaultValue: "info", Description: "Minimum log level for request logging"},
		},
		DefaultConfig: map[string]any{"logLevel": "info"},
	})

	r.Register(&ModuleSchema{
		Type:        "http.middleware.ratelimit",
		Label:       "Rate Limiter",
		Category:    "middleware",
		Description: "Rate limiting middleware to control request throughput",
		Inputs:      []ServiceIODef{{Name: "request", Type: "http.Request", Description: "HTTP request to rate-limit"}},
		Outputs:     []ServiceIODef{{Name: "limited", Type: "http.Request", Description: "HTTP request (passed through if within limit)"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "requestsPerMinute", Label: "Requests Per Minute", Type: FieldTypeNumber, DefaultValue: 60, Description: "Maximum number of requests per minute per client"},
			{Key: "burstSize", Label: "Burst Size", Type: FieldTypeNumber, DefaultValue: 10, Description: "Maximum burst of requests allowed above the rate limit"},
		},
		DefaultConfig: map[string]any{"requestsPerMinute": 60, "burstSize": 10},
	})

	r.Register(&ModuleSchema{
		Type:        "http.middleware.cors",
		Label:       "CORS Middleware",
		Category:    "middleware",
		Description: "Cross-Origin Resource Sharing (CORS) middleware",
		Inputs:      []ServiceIODef{{Name: "request", Type: "http.Request", Description: "HTTP request needing CORS headers"}},
		Outputs:     []ServiceIODef{{Name: "cors", Type: "http.Request", Description: "HTTP request with CORS headers applied"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "allowedOrigins", Label: "Allowed Origins", Type: FieldTypeArray, ArrayItemType: "string", DefaultValue: []string{"*"}, Description: "Allowed origins (e.g. https://example.com, http://localhost:3000)"},
			{Key: "allowedMethods", Label: "Allowed Methods", Type: FieldTypeArray, ArrayItemType: "string", DefaultValue: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}, Description: "Allowed HTTP methods"},
		},
		DefaultConfig: map[string]any{
			"allowedOrigins": []string{"*"},
			"allowedMethods": []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		},
	})

	r.Register(&ModuleSchema{
		Type:         "http.middleware.requestid",
		Label:        "Request ID Middleware",
		Category:     "middleware",
		Description:  "Adds a unique request ID header to each request for tracing",
		Inputs:       []ServiceIODef{{Name: "request", Type: "http.Request", Description: "HTTP request without request ID"}},
		Outputs:      []ServiceIODef{{Name: "tagged", Type: "http.Request", Description: "HTTP request with X-Request-ID header"}},
		ConfigFields: []ConfigFieldDef{},
	})

	r.Register(&ModuleSchema{
		Type:        "http.middleware.securityheaders",
		Label:       "Security Headers",
		Category:    "middleware",
		Description: "Adds security-related HTTP headers to responses",
		Inputs:      []ServiceIODef{{Name: "request", Type: "http.Request", Description: "HTTP request to add security headers"}},
		Outputs:     []ServiceIODef{{Name: "secured", Type: "http.Request", Description: "HTTP request with security headers"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "contentSecurityPolicy", Label: "Content Security Policy", Type: FieldTypeString, Description: "CSP header value", Placeholder: "default-src 'self'", Group: "headers"},
			{Key: "frameOptions", Label: "X-Frame-Options", Type: FieldTypeSelect, Options: []string{"DENY", "SAMEORIGIN"}, DefaultValue: "DENY", Description: "Controls whether the page can be embedded in frames", Group: "headers"},
			{Key: "contentTypeOptions", Label: "X-Content-Type-Options", Type: FieldTypeString, DefaultValue: "nosniff", Description: "Prevents MIME type sniffing", Group: "headers"},
			{Key: "hstsMaxAge", Label: "HSTS Max-Age (sec)", Type: FieldTypeNumber, DefaultValue: 31536000, Description: "HTTP Strict Transport Security max-age in seconds", Group: "headers"},
			{Key: "referrerPolicy", Label: "Referrer Policy", Type: FieldTypeSelect, Options: []string{"no-referrer", "no-referrer-when-downgrade", "origin", "origin-when-cross-origin", "same-origin", "strict-origin", "strict-origin-when-cross-origin", "unsafe-url"}, DefaultValue: "strict-origin-when-cross-origin", Description: "Controls the Referer header sent with requests", Group: "headers"},
			{Key: "permissionsPolicy", Label: "Permissions Policy", Type: FieldTypeString, DefaultValue: "camera=(), microphone=(), geolocation=()", Description: "Controls which browser features are allowed", Group: "headers"},
		},
		DefaultConfig: map[string]any{
			"frameOptions":       "DENY",
			"contentTypeOptions": "nosniff",
			"hstsMaxAge":         31536000,
			"referrerPolicy":     "strict-origin-when-cross-origin",
			"permissionsPolicy":  "camera=(), microphone=(), geolocation=()",
		},
	})

	// ---- Messaging Category ----

	r.Register(&ModuleSchema{
		Type:        "messaging.broker",
		Label:       "In-Memory Message Broker",
		Category:    "messaging",
		Description: "Simple in-memory message broker for local pub/sub",
		Inputs:      []ServiceIODef{{Name: "message", Type: "[]byte", Description: "Message to publish"}},
		Outputs:     []ServiceIODef{{Name: "message", Type: "[]byte", Description: "Delivered message to subscriber"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "maxQueueSize", Label: "Max Queue Size", Type: FieldTypeNumber, DefaultValue: 10000, Description: "Maximum message queue size per topic"},
			{Key: "deliveryTimeout", Label: "Delivery Timeout", Type: FieldTypeDuration, DefaultValue: "30s", Description: "Message delivery timeout", Placeholder: "30s"},
		},
		DefaultConfig: map[string]any{"maxQueueSize": 10000, "deliveryTimeout": "30s"},
	})

	r.Register(&ModuleSchema{
		Type:         "messaging.broker.eventbus",
		Label:        "EventBus Bridge",
		Category:     "messaging",
		Description:  "Bridges the modular EventBus to the messaging subsystem",
		Inputs:       []ServiceIODef{{Name: "event", Type: "Event", Description: "CloudEvent from the EventBus"}},
		Outputs:      []ServiceIODef{{Name: "message", Type: "[]byte", Description: "Message forwarded to messaging subsystem"}},
		ConfigFields: []ConfigFieldDef{},
	})

	r.Register(&ModuleSchema{
		Type:         "messaging.handler",
		Label:        "Message Handler",
		Category:     "messaging",
		Description:  "Handles messages from topics/queues",
		Inputs:       []ServiceIODef{{Name: "message", Type: "[]byte", Description: "Incoming message from topic/queue"}},
		Outputs:      []ServiceIODef{{Name: "result", Type: "[]byte", Description: "Processed message result"}},
		ConfigFields: []ConfigFieldDef{},
	})

	r.Register(&ModuleSchema{
		Type:        "messaging.nats",
		Label:       "NATS Broker",
		Category:    "messaging",
		Description: "NATS message broker for distributed pub/sub",
		Inputs:      []ServiceIODef{{Name: "message", Type: "[]byte", Description: "Message to publish via NATS"}},
		Outputs:     []ServiceIODef{{Name: "message", Type: "[]byte", Description: "Message received from NATS subscription"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "url", Label: "NATS URL", Type: FieldTypeString, DefaultValue: "nats://localhost:4222", Description: "NATS server connection URL", Placeholder: "nats://localhost:4222"},
		},
		DefaultConfig: map[string]any{"url": "nats://localhost:4222"},
	})

	r.Register(&ModuleSchema{
		Type:        "messaging.kafka",
		Label:       "Kafka Broker",
		Category:    "messaging",
		Description: "Apache Kafka message broker for high-throughput streaming",
		Inputs:      []ServiceIODef{{Name: "message", Type: "[]byte", Description: "Message to produce to Kafka"}},
		Outputs:     []ServiceIODef{{Name: "message", Type: "[]byte", Description: "Message consumed from Kafka"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "brokers", Label: "Broker Addresses", Type: FieldTypeArray, ArrayItemType: "string", Description: "Kafka broker addresses (e.g. localhost:9092)", Placeholder: "localhost:9092"},
			{Key: "groupId", Label: "Consumer Group ID", Type: FieldTypeString, Description: "Kafka consumer group identifier", Placeholder: "my-consumer-group"},
		},
	})

	// ---- State Machine Category ----

	r.Register(&ModuleSchema{
		Type:        "statemachine.engine",
		Label:       "State Machine Engine",
		Category:    "statemachine",
		Description: "Manages workflow state transitions and lifecycle",
		Inputs:      []ServiceIODef{{Name: "event", Type: "Event", Description: "Event triggering a state transition"}},
		Outputs:     []ServiceIODef{{Name: "transition", Type: "Transition", Description: "Completed state transition result"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "maxInstances", Label: "Max Instances", Type: FieldTypeNumber, DefaultValue: 1000, Description: "Maximum concurrent workflow instances"},
			{Key: "instanceTTL", Label: "Instance TTL", Type: FieldTypeDuration, DefaultValue: "24h", Description: "TTL for idle workflow instances", Placeholder: "24h"},
		},
		DefaultConfig: map[string]any{"maxInstances": 1000, "instanceTTL": "24h"},
	})

	r.Register(&ModuleSchema{
		Type:        "state.tracker",
		Label:       "State Tracker",
		Category:    "statemachine",
		Description: "Tracks and persists workflow instance state",
		Inputs:      []ServiceIODef{{Name: "state", Type: "State", Description: "State update to track"}},
		Outputs:     []ServiceIODef{{Name: "tracked", Type: "State", Description: "Tracked state with persistence"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "retentionDays", Label: "Retention Days", Type: FieldTypeNumber, DefaultValue: 30, Description: "State history retention in days"},
		},
		DefaultConfig: map[string]any{"retentionDays": 30},
	})

	r.Register(&ModuleSchema{
		Type:         "state.connector",
		Label:        "State Connector",
		Category:     "statemachine",
		Description:  "Connects state machine engine to state tracker for persistence",
		Inputs:       []ServiceIODef{{Name: "state", Type: "State", Description: "State from engine to connect"}},
		Outputs:      []ServiceIODef{{Name: "connected", Type: "State", Description: "Connected state bridging engine and tracker"}},
		ConfigFields: []ConfigFieldDef{},
	})

	// ---- Infrastructure / Modular Framework Category ----

	r.Register(&ModuleSchema{
		Type:         "scheduler.modular",
		Label:        "Scheduler",
		Category:     "scheduling",
		Description:  "GoCodeAlone/modular scheduler for cron-based job execution",
		Inputs:       []ServiceIODef{{Name: "job", Type: "func()", Description: "Job function to schedule"}},
		Outputs:      []ServiceIODef{{Name: "scheduler", Type: "Scheduler", Description: "Scheduler service for registering cron jobs"}},
		ConfigFields: []ConfigFieldDef{},
		MaxIncoming:  intPtr(0),
	})

	r.Register(&ModuleSchema{
		Type:         "cache.modular",
		Label:        "Cache",
		Category:     "infrastructure",
		Description:  "GoCodeAlone/modular caching module (use config feeders for provider settings)",
		Inputs:       []ServiceIODef{{Name: "key", Type: "string", Description: "Cache key for get/set operations"}},
		Outputs:      []ServiceIODef{{Name: "cache", Type: "Cache", Description: "Cache service for key-value storage"}},
		ConfigFields: []ConfigFieldDef{},
	})

	r.Register(&ModuleSchema{
		Type:        "config.provider",
		Label:       "Config Provider",
		Category:    "infrastructure",
		Description: "Application configuration registry with schema validation, defaults, and source layering. Provides {{config \"key\"}} template references.",
		Outputs:     []ServiceIODef{{Name: "config", Type: "JSON", Description: "Configuration values accessible via {{config \"key\"}} templates"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "sources", Label: "Sources", Type: FieldTypeArray, Required: true, Description: "Ordered list of config sources (later overrides earlier). Supported types: defaults, env"},
			{Key: "schema", Label: "Schema", Type: FieldTypeMap, Required: true, Description: "Map of config key definitions with env, required, default, sensitive, desc fields"},
		},
		MaxIncoming: intPtr(0),
	})

	r.Register(&ModuleSchema{
		Type:         "jsonschema.modular",
		Label:        "JSON Schema Validator",
		Category:     "validation",
		Description:  "GoCodeAlone/modular JSON Schema validation module",
		Inputs:       []ServiceIODef{{Name: "data", Type: "any", Description: "Data to validate against schema"}},
		Outputs:      []ServiceIODef{{Name: "validator", Type: "JSONSchemaService", Description: "JSON Schema validation service"}},
		ConfigFields: []ConfigFieldDef{},
	})

	// ---- Database Category ----

	r.Register(&ModuleSchema{
		Type:        "database.workflow",
		Label:       "Workflow Database",
		Category:    "database",
		Description: "SQL database for workflow state persistence (supports PostgreSQL, MySQL, SQLite)",
		Inputs:      []ServiceIODef{{Name: "query", Type: "SQL", Description: "SQL query to execute"}},
		Outputs:     []ServiceIODef{{Name: "database", Type: "sql.DB", Description: "SQL database connection pool"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "driver", Label: "Driver", Type: FieldTypeSelect, Options: []string{"postgres", "mysql", "sqlite3"}, Required: true, Description: "Database driver to use"},
			{Key: "dsn", Label: "DSN", Type: FieldTypeString, Required: true, Description: "Data source name / connection string", Placeholder: "postgres://user:pass@localhost/db?sslmode=disable", Sensitive: true}, //nolint:gosec // G101: placeholder DSN example in schema documentation
			{Key: "maxOpenConns", Label: "Max Open Connections", Type: FieldTypeNumber, DefaultValue: 25, Description: "Maximum number of open database connections"},
			{Key: "maxIdleConns", Label: "Max Idle Connections", Type: FieldTypeNumber, DefaultValue: 5, Description: "Maximum number of idle connections in the pool"},
		},
		DefaultConfig: map[string]any{"maxOpenConns": 25, "maxIdleConns": 5},
	})

	r.Register(&ModuleSchema{
		Type:        "database.partitioned",
		Label:       "Partitioned Database",
		Category:    "database",
		Description: "PostgreSQL partitioned database for multi-tenant data isolation. Supports LIST and RANGE partitions with configurable naming format and optional source-table-driven auto-partition creation. Use partitionKey/tables for a single partition config, or partitions[] for multiple independent partition key configurations.",
		Inputs:      []ServiceIODef{{Name: "query", Type: "SQL", Description: "SQL query to execute"}},
		Outputs:     []ServiceIODef{{Name: "database", Type: "sql.DB", Description: "SQL database connection pool"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "driver", Label: "Driver", Type: FieldTypeSelect, Options: []string{"pgx", "pgx/v5", "postgres"}, Required: true, Description: "PostgreSQL database driver"},
			{Key: "dsn", Label: "DSN", Type: FieldTypeString, Required: true, Description: "Data source name / connection string", Placeholder: "postgres://user:pass@localhost/db?sslmode=disable", Sensitive: true}, //nolint:gosec // G101: placeholder DSN example in schema documentation
			{Key: "partitionKey", Label: "Partition Key", Type: FieldTypeString, Description: "Column name used for partitioning in single-partition mode (e.g. tenant_id). Ignored when 'partitions' is set.", Placeholder: "tenant_id"},
			{Key: "tables", Label: "Tables", Type: FieldTypeArray, ArrayItemType: "string", Description: "Tables to manage partitions for in single-partition mode. Ignored when 'partitions' is set.", Placeholder: "forms"},
			{Key: "partitionType", Label: "Partition Type", Type: FieldTypeSelect, Options: []string{"list", "range"}, DefaultValue: "list", Description: "PostgreSQL partition type for single-partition mode: list (FOR VALUES IN) or range (FOR VALUES FROM/TO). Ignored when 'partitions' is set."},
			{Key: "partitionNameFormat", Label: "Partition Name Format", Type: FieldTypeString, DefaultValue: "{table}_{tenant}", Description: "Template for partition table names in single-partition mode. Supports {table} and {tenant} placeholders. Ignored when 'partitions' is set.", Placeholder: "{table}_{tenant}"},
			{Key: "sourceTable", Label: "Source Table", Type: FieldTypeString, Description: "Table containing all tenant IDs for auto-partition sync in single-partition mode. Ignored when 'partitions' is set.", Placeholder: "tenants"},
			{Key: "sourceColumn", Label: "Source Column", Type: FieldTypeString, Description: "Column in source table to query for tenant values in single-partition mode. Defaults to partitionKey.", Placeholder: "id"},
			{Key: "partitions", Label: "Partitions", Type: FieldTypeArray, ArrayItemType: "object", Description: "List of independent partition key configurations. When set, overrides the single-partition fields. Each entry supports: partitionKey, tables, partitionType, partitionNameFormat, sourceTable, sourceColumn."},
			{Key: "maxOpenConns", Label: "Max Open Connections", Type: FieldTypeNumber, DefaultValue: 25, Description: "Maximum number of open database connections"},
			{Key: "maxIdleConns", Label: "Max Idle Connections", Type: FieldTypeNumber, DefaultValue: 5, Description: "Maximum number of idle connections in the pool"},
		},
		DefaultConfig: map[string]any{"maxOpenConns": 25, "maxIdleConns": 5, "partitionType": "list", "partitionNameFormat": "{table}_{tenant}"},
	})

	r.Register(&ModuleSchema{
		Type:        "persistence.store",
		Label:       "Persistence Store",
		Category:    "database",
		Description: "Persistence layer that uses a database service for storage",
		Inputs:      []ServiceIODef{{Name: "data", Type: "any", Description: "Data to persist or retrieve"}},
		Outputs:     []ServiceIODef{{Name: "persistence", Type: "PersistenceStore", Description: "Persistence service for CRUD operations"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "database", Label: "Database Service", Type: FieldTypeString, DefaultValue: "database", Description: "Name of the database module to use for storage", Placeholder: "database", InheritFrom: "dependency.name"},
		},
		DefaultConfig: map[string]any{"database": "database"},
	})

	// ---- Observability Category ----

	r.Register(&ModuleSchema{
		Type:        "metrics.collector",
		Label:       "Metrics Collector",
		Category:    "observability",
		Description: "Collects and exposes application metrics",
		Outputs:     []ServiceIODef{{Name: "metrics", Type: "prometheus.Metrics", Description: "Prometheus metrics endpoint"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "namespace", Label: "Namespace", Type: FieldTypeString, DefaultValue: "workflow", Description: "Prometheus metric namespace prefix", Placeholder: "workflow"},
			{Key: "subsystem", Label: "Subsystem", Type: FieldTypeString, Description: "Prometheus metric subsystem", Placeholder: "api"},
			{Key: "metricsPath", Label: "Metrics Path", Type: FieldTypeString, DefaultValue: "/metrics", Description: "Endpoint path for Prometheus scraping", Placeholder: "/metrics"},
			{Key: "enabledMetrics", Label: "Enabled Metrics", Type: FieldTypeArray, ArrayItemType: "string", DefaultValue: []string{"workflow", "http", "module", "active_workflows"}, Description: "Which metric groups to register (workflow, http, module, active_workflows)"},
		},
		DefaultConfig: map[string]any{"namespace": "workflow", "metricsPath": "/metrics", "enabledMetrics": []string{"workflow", "http", "module", "active_workflows"}},
	})

	r.Register(&ModuleSchema{
		Type:        "health.checker",
		Label:       "Health Checker",
		Category:    "observability",
		Description: "Health check endpoint for liveness/readiness probes",
		Outputs:     []ServiceIODef{{Name: "health", Type: "HealthStatus", Description: "Health check status endpoint"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "healthPath", Label: "Health Path", Type: FieldTypeString, DefaultValue: "/healthz", Description: "Health check endpoint path", Placeholder: "/healthz"},
			{Key: "readyPath", Label: "Ready Path", Type: FieldTypeString, DefaultValue: "/readyz", Description: "Readiness probe endpoint path", Placeholder: "/readyz"},
			{Key: "livePath", Label: "Live Path", Type: FieldTypeString, DefaultValue: "/livez", Description: "Liveness probe endpoint path", Placeholder: "/livez"},
			{Key: "checkTimeout", Label: "Check Timeout", Type: FieldTypeDuration, DefaultValue: "5s", Description: "Per-check timeout duration", Placeholder: "5s"},
			{Key: "autoDiscover", Label: "Auto-Discover", Type: FieldTypeBool, DefaultValue: true, Description: "Automatically discover HealthCheckable services"},
		},
		DefaultConfig: map[string]any{"healthPath": "/healthz", "readyPath": "/readyz", "livePath": "/livez", "checkTimeout": "5s", "autoDiscover": true},
	})

	r.Register(&ModuleSchema{
		Type:        "observability.otel",
		Label:       "OpenTelemetry",
		Category:    "observability",
		Description: "OpenTelemetry tracing integration for distributed tracing",
		Inputs:      []ServiceIODef{{Name: "span", Type: "trace.Span", Description: "Trace spans from instrumented code"}},
		Outputs:     []ServiceIODef{{Name: "tracer", Type: "trace.Tracer", Description: "OpenTelemetry tracer for distributed tracing"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "endpoint", Label: "OTLP Endpoint", Type: FieldTypeString, DefaultValue: "localhost:4318", Description: "OpenTelemetry collector endpoint", Placeholder: "localhost:4318"},
			{Key: "serviceName", Label: "Service Name", Type: FieldTypeString, DefaultValue: "workflow", Description: "Service name for trace attribution", Placeholder: "workflow"},
		},
		DefaultConfig: map[string]any{"endpoint": "localhost:4318", "serviceName": "workflow"},
	})

	r.Register(&ModuleSchema{
		Type:        "log.collector",
		Label:       "Log Collector",
		Category:    "observability",
		Description: "Centralized log collection from all modules, auto-wires to the first available router at /logs",
		Inputs:      []ServiceIODef{{Name: "logEntry", Type: "LogEntry", Description: "Log entries from modules"}},
		Outputs:     []ServiceIODef{{Name: "logs", Type: "[]LogEntry", Description: "Aggregated log entries"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "logLevel", Label: "Log Level", Type: FieldTypeSelect, Options: []string{"debug", "info", "warn", "error"}, DefaultValue: "info", Description: "Minimum log level to collect"},
			{Key: "outputFormat", Label: "Output Format", Type: FieldTypeSelect, Options: []string{"json", "text"}, DefaultValue: "json", Description: "Format for log output"},
			{Key: "retentionDays", Label: "Retention Days", Type: FieldTypeNumber, DefaultValue: 7, Description: "Number of days to retain log entries"},
		},
		DefaultConfig: map[string]any{"logLevel": "info", "outputFormat": "json", "retentionDays": 7},
	})

	// ---- Authentication Category ----

	r.Register(&ModuleSchema{
		Type:        "auth.jwt",
		Label:       "JWT Auth",
		Category:    "middleware",
		Description: "JWT-based authentication with token signing, verification, and user management",
		Inputs:      []ServiceIODef{{Name: "credentials", Type: "Credentials", Description: "Login credentials or JWT token to verify"}},
		Outputs:     []ServiceIODef{{Name: "auth", Type: "AuthService", Description: "JWT authentication service with token signing and verification"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "secret", Label: "JWT Secret", Type: FieldTypeString, Required: true, Description: "Secret key for signing JWT tokens (supports $ENV_VAR expansion)", Placeholder: "$JWT_SECRET", Sensitive: true},
			{Key: "tokenExpiry", Label: "Token Expiry", Type: FieldTypeDuration, DefaultValue: "24h", Description: "Token expiration duration (e.g. 1h, 24h, 7d)", Placeholder: "24h"},
			{Key: "issuer", Label: "Issuer", Type: FieldTypeString, DefaultValue: "workflow", Description: "Token issuer claim", Placeholder: "workflow"},
			{Key: "seedFile", Label: "Seed Users File", Type: FieldTypeString, Description: "Path to JSON file with initial user accounts", Placeholder: "data/users.json"},
			{Key: "responseFormat", Label: "Response Format", Type: FieldTypeSelect, Options: []string{"standard", "oauth2"}, Description: "Format of authentication response payloads"},
			{Key: "allowRegistration", Label: "Allow Open Registration", Type: FieldTypeBool, DefaultValue: false, Description: "When true, any visitor may register without admin intervention"},
		},
		DefaultConfig: map[string]any{"tokenExpiry": "24h", "issuer": "workflow"},
	})

	// ---- Integration Category ----

	r.Register(&ModuleSchema{
		Type:         "data.transformer",
		Label:        "Data Transformer",
		Category:     "integration",
		Description:  "Transforms data between formats using configurable pipelines",
		Inputs:       []ServiceIODef{{Name: "input", Type: "any", Description: "Input data to transform"}},
		Outputs:      []ServiceIODef{{Name: "output", Type: "any", Description: "Transformed output data"}},
		ConfigFields: []ConfigFieldDef{},
	})

	r.Register(&ModuleSchema{
		Type:        "webhook.sender",
		Label:       "Webhook Sender",
		Category:    "integration",
		Description: "Sends HTTP webhooks with retry and exponential backoff",
		Inputs:      []ServiceIODef{{Name: "payload", Type: "JSON", Description: "Webhook payload to send"}},
		Outputs:     []ServiceIODef{{Name: "response", Type: "http.Response", Description: "HTTP response from webhook target"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "maxRetries", Label: "Max Retries", Type: FieldTypeNumber, DefaultValue: 3, Description: "Maximum number of retry attempts on failure"},
		},
		DefaultConfig: map[string]any{"maxRetries": 3},
	})

	r.Register(&ModuleSchema{
		Type:        "notification.slack",
		Label:       "Slack Notification",
		Category:    "integration",
		Description: "Sends notifications to Slack channels via webhooks",
		Inputs:      []ServiceIODef{{Name: "message", Type: "string", Description: "Message text to send to Slack"}},
		Outputs:     []ServiceIODef{{Name: "sent", Type: "SlackResponse", Description: "Slack API response"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "webhookURL", Label: "Webhook URL", Type: FieldTypeString, Required: true, Description: "Slack incoming webhook URL", Placeholder: "https://hooks.slack.com/services/...", Sensitive: true},
			{Key: "channel", Label: "Channel", Type: FieldTypeString, Description: "Slack channel to post to", Placeholder: "#general"},
			{Key: "username", Label: "Username", Type: FieldTypeString, DefaultValue: "workflow-bot", Description: "Bot username for messages"},
		},
		DefaultConfig: map[string]any{"username": "workflow-bot"},
	})

	r.Register(&ModuleSchema{
		Type:        "storage.s3",
		Label:       "S3 Storage",
		Category:    "integration",
		Description: "Amazon S3 compatible object storage integration",
		Inputs:      []ServiceIODef{{Name: "object", Type: "[]byte", Description: "Object data to store or retrieve"}},
		Outputs:     []ServiceIODef{{Name: "storage", Type: "ObjectStore", Description: "S3-compatible object storage service"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "bucket", Label: "Bucket", Type: FieldTypeString, Required: true, Description: "S3 bucket name", Placeholder: "my-bucket"},
			{Key: "region", Label: "Region", Type: FieldTypeString, DefaultValue: "us-east-1", Description: "AWS region", Placeholder: "us-east-1"},
			{Key: "endpoint", Label: "Endpoint", Type: FieldTypeString, Description: "Custom S3 endpoint (for MinIO, etc.)", Placeholder: "http://localhost:9000"},
		},
		DefaultConfig: map[string]any{"region": "us-east-1"},
	})

	// ---- Dynamic Component ----

	r.Register(&ModuleSchema{
		Type:        "dynamic.component",
		Label:       "Dynamic Component",
		Category:    "infrastructure",
		Description: "Loads a dynamic component from the registry or source file at runtime",
		Inputs:      []ServiceIODef{{Name: "input", Type: "any", Description: "Input data for the dynamic component"}},
		Outputs:     []ServiceIODef{{Name: "output", Type: "any", Description: "Output from the dynamic component"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "componentId", Label: "Component ID", Type: FieldTypeString, Description: "ID to look up in the dynamic component registry (defaults to module name)"},
			{Key: "source", Label: "Source File", Type: FieldTypeString, Description: "Path to Go source file to load dynamically", Placeholder: "components/my_processor.go"},
			{Key: "provides", Label: "Provides Services", Type: FieldTypeArray, ArrayItemType: "string", Description: "Service names this component provides", Placeholder: "my-service"},
			{Key: "requires", Label: "Requires Services", Type: FieldTypeArray, ArrayItemType: "string", Description: "Service names this component depends on", Placeholder: "database"},
		},
	})

	// ---- Processing Category ----

	r.Register(&ModuleSchema{
		Type:        "processing.step",
		Label:       "Processing Step",
		Category:    "statemachine",
		Description: "Executes a component as a processing step in a workflow, with retry and compensation",
		Inputs:      []ServiceIODef{{Name: "input", Type: "any", Description: "Input data for the processing step"}},
		Outputs: []ServiceIODef{
			{Name: "result", Type: "any", Description: "Processing result on success"},
			{Name: "transition", Type: "string", Description: "State transition triggered (success or compensate)"},
		},
		ConfigFields: []ConfigFieldDef{
			{Key: "componentId", Label: "Component ID", Type: FieldTypeString, Required: true, Description: "Service name of the component to execute", InheritFrom: "dependency.name"},
			{Key: "successTransition", Label: "Success Transition", Type: FieldTypeString, Description: "State transition to trigger on success", Placeholder: "completed"},
			{Key: "compensateTransition", Label: "Compensate Transition", Type: FieldTypeString, Description: "State transition to trigger on failure for compensation", Placeholder: "failed"},
			{Key: "maxRetries", Label: "Max Retries", Type: FieldTypeNumber, DefaultValue: 2, Description: "Maximum number of retry attempts"},
			{Key: "retryBackoffMs", Label: "Retry Backoff (ms)", Type: FieldTypeNumber, DefaultValue: 1000, Description: "Base backoff duration in milliseconds between retries"},
			{Key: "timeoutSeconds", Label: "Timeout (sec)", Type: FieldTypeNumber, DefaultValue: 30, Description: "Maximum execution time per attempt in seconds"},
		},
		DefaultConfig: map[string]any{"maxRetries": 2, "retryBackoffMs": 1000, "timeoutSeconds": 30},
	})

	// ---- Storage Category ----

	r.Register(&ModuleSchema{
		Type:        "storage.local",
		Label:       "Local Storage",
		Category:    "integration",
		Description: "Local filesystem storage provider for workspace files",
		Inputs:      []ServiceIODef{{Name: "file", Type: "[]byte", Description: "File data to store or retrieve"}},
		Outputs:     []ServiceIODef{{Name: "storage", Type: "FileStore", Description: "Local filesystem storage service"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "rootDir", Label: "Root Directory", Type: FieldTypeString, Required: true, Description: "Filesystem path for the storage root", Placeholder: "./data/storage"},
		},
		DefaultConfig: map[string]any{"rootDir": "./data/storage"},
	})

	r.Register(&ModuleSchema{
		Type:        "storage.gcs",
		Label:       "GCS Storage",
		Category:    "integration",
		Description: "Google Cloud Storage integration",
		Inputs:      []ServiceIODef{{Name: "object", Type: "[]byte", Description: "Object data to store or retrieve"}},
		Outputs:     []ServiceIODef{{Name: "storage", Type: "ObjectStore", Description: "GCS-compatible object storage service"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "bucket", Label: "Bucket", Type: FieldTypeString, Required: true, Description: "GCS bucket name", Placeholder: "my-bucket"},
			{Key: "project", Label: "GCP Project", Type: FieldTypeString, Description: "Google Cloud project ID", Placeholder: "my-project"},
			{Key: "credentialsFile", Label: "Credentials File", Type: FieldTypeFilePath, Description: "Path to service account JSON key file", Placeholder: "credentials/gcs-key.json", Sensitive: true},
		},
	})

	// ---- Secrets Category ----

	r.Register(&ModuleSchema{
		Type:        "secrets.vault",
		Label:       "Vault Secrets",
		Category:    "infrastructure",
		Description: "HashiCorp Vault secret provider for secure secret storage and retrieval",
		Outputs:     []ServiceIODef{{Name: "secrets", Type: "SecretProvider", Description: "Secret provider service backed by HashiCorp Vault"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "address", Label: "Vault Address", Type: FieldTypeString, Required: true, Description: "HashiCorp Vault server address", Placeholder: "https://vault.example.com:8200"},
			{Key: "token", Label: "Vault Token", Type: FieldTypeString, Required: true, Description: "Authentication token for Vault access", Placeholder: "${VAULT_TOKEN}", Sensitive: true},
			{Key: "mountPath", Label: "Mount Path", Type: FieldTypeString, DefaultValue: "secret", Description: "KV v2 secrets engine mount path", Placeholder: "secret"},
			{Key: "namespace", Label: "Namespace", Type: FieldTypeString, Description: "Vault namespace (Enterprise only)", Placeholder: "admin"},
		},
		DefaultConfig: map[string]any{"mountPath": "secret"},
	})

	r.Register(&ModuleSchema{
		Type:        "secrets.aws",
		Label:       "AWS Secrets Manager",
		Category:    "infrastructure",
		Description: "AWS Secrets Manager provider for cloud-native secret management",
		Outputs:     []ServiceIODef{{Name: "secrets", Type: "SecretProvider", Description: "Secret provider service backed by AWS Secrets Manager"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "region", Label: "AWS Region", Type: FieldTypeString, DefaultValue: "us-east-1", Description: "AWS region for Secrets Manager", Placeholder: "us-east-1"},
			{Key: "accessKeyId", Label: "Access Key ID", Type: FieldTypeString, Description: "AWS access key ID (uses default credential chain if empty)", Placeholder: "${AWS_ACCESS_KEY_ID}", Sensitive: true},
			{Key: "secretAccessKey", Label: "Secret Access Key", Type: FieldTypeString, Description: "AWS secret access key (uses default credential chain if empty)", Placeholder: "${AWS_SECRET_ACCESS_KEY}", Sensitive: true},
		},
		DefaultConfig: map[string]any{"region": "us-east-1"},
	})

	// ---- Admin Infrastructure Category ----

	r.Register(&ModuleSchema{
		Type:        "storage.sqlite",
		Label:       "SQLite Storage",
		Category:    "database",
		Description: "SQLite database connection provided as a service for other modules",
		Outputs:     []ServiceIODef{{Name: "database", Type: "sql.DB", Description: "SQLite database connection"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "dbPath", Label: "Database Path", Type: FieldTypeString, DefaultValue: "data/workflow.db", Description: "Path to the SQLite database file", Placeholder: "data/workflow.db"},
			{Key: "maxConnections", Label: "Max Connections", Type: FieldTypeNumber, DefaultValue: 5, Description: "Maximum number of open database connections"},
			{Key: "walMode", Label: "WAL Mode", Type: FieldTypeBool, DefaultValue: true, Description: "Enable Write-Ahead Logging for better concurrent read performance"},
		},
		DefaultConfig: map[string]any{"dbPath": "data/workflow.db", "maxConnections": 5, "walMode": true},
	})

	r.Register(&ModuleSchema{
		Type:         "auth.user-store",
		Label:        "User Store",
		Category:     "infrastructure",
		Description:  "In-memory user store with optional persistence write-through for user CRUD operations",
		Inputs:       []ServiceIODef{{Name: "credentials", Type: "Credentials", Description: "User credentials for CRUD operations"}},
		Outputs:      []ServiceIODef{{Name: "user-store", Type: "UserStore", Description: "User storage service for auth modules"}},
		ConfigFields: []ConfigFieldDef{},
	})

	r.Register(&ModuleSchema{
		Type:        "workflow.registry",
		Label:       "Workflow Registry",
		Category:    "infrastructure",
		Description: "SQLite-backed registry for companies, organizations, projects, and workflows",
		Inputs:      []ServiceIODef{{Name: "storageBackend", Type: "SQLiteStorage", Description: "Optional shared SQLite storage service name"}},
		Outputs:     []ServiceIODef{{Name: "registry", Type: "WorkflowRegistry", Description: "Workflow data registry service"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "storageBackend", Label: "Storage Backend", Type: FieldTypeString, DefaultValue: "", Description: "Name of a storage.sqlite module to share its DB connection (leave empty for standalone DB)", Placeholder: "admin-db", InheritFrom: "dependency.name"},
		},
		DefaultConfig: map[string]any{"storageBackend": ""},
	})

	// ---- OpenAPI Category ----

	r.Register(&ModuleSchema{
		Type:        "openapi",
		Label:       "OpenAPI Spec Server",
		Category:    "integration",
		Description: "Parses an OpenAPI v3 spec file and auto-generates HTTP routes with request validation and optional Swagger UI",
		Inputs:      []ServiceIODef{{Name: "router", Type: "HTTPRouter", Description: "HTTP router to register generated routes on"}},
		Outputs:     []ServiceIODef{{Name: "routes", Type: "OpenAPIRoutes", Description: "Auto-generated HTTP routes from the spec"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "spec_file", Label: "Spec File", Type: FieldTypeFilePath, Required: true, Description: "Path to the OpenAPI v3 spec file (JSON or YAML)", Placeholder: "specs/petstore.yaml"},
			{Key: "base_path", Label: "Base Path", Type: FieldTypeString, Description: "Base path prefix for all generated routes", Placeholder: "/api/v1"},
			{Key: "router", Label: "Router Module", Type: FieldTypeString, Description: "Name of the http.router module to register routes on (auto-detected if omitted)", Placeholder: "my-router"},
			{Key: "register_routes", Label: "Register Routes", Type: FieldTypeBool, DefaultValue: true, Description: "When false, skip registering spec-path routes (only serve spec endpoints and Swagger UI); default true"},
			{Key: "validation", Label: "Validation", Type: FieldTypeMap, Description: "Request/response validation settings (request, response booleans)"},
			{Key: "swagger_ui", Label: "Swagger UI", Type: FieldTypeMap, Description: "Swagger UI settings (enabled bool, path string)"},
			{Key: "max_body_bytes", Label: "Max Body Bytes", Type: FieldTypeNumber, Description: "Maximum allowed request body size in bytes for validated OpenAPI operations (leave empty to use module default)", Placeholder: "1048576"},
		},
		DefaultConfig: map[string]any{"register_routes": true, "validation": map[string]any{"request": true, "response": false}, "swagger_ui": map[string]any{"enabled": false, "path": "/docs"}},
	})

	r.Register(&ModuleSchema{
		Type:        "openapi.generator",
		Label:       "OpenAPI Generator",
		Category:    "integration",
		Description: "Scans workflow route definitions to generate an OpenAPI 3.0 spec, served at /api/openapi.json and /api/openapi.yaml",
		Inputs:      []ServiceIODef{{Name: "routes", Type: "RouteConfig", Description: "Workflow route definitions to scan"}},
		Outputs:     []ServiceIODef{{Name: "spec", Type: "OpenAPISpec", Description: "Generated OpenAPI 3.0 specification"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "title", Label: "API Title", Type: FieldTypeString, DefaultValue: "Workflow API", Description: "Title for the OpenAPI spec", Placeholder: "My API"},
			{Key: "version", Label: "API Version", Type: FieldTypeString, DefaultValue: "1.0.0", Description: "Version string for the OpenAPI spec", Placeholder: "1.0.0"},
			{Key: "description", Label: "Description", Type: FieldTypeString, Description: "Description of the API", Placeholder: "API generated from workflow routes"},
			{Key: "servers", Label: "Server URLs", Type: FieldTypeArray, ArrayItemType: "string", Description: "List of server URLs to include in the spec", Placeholder: "http://localhost:8080"},
		},
		DefaultConfig: map[string]any{"title": "Workflow API", "version": "1.0.0"},
	})

	r.Register(&ModuleSchema{
		Type:        "openapi.consumer",
		Label:       "OpenAPI Consumer",
		Category:    "integration",
		Description: "Parses an external OpenAPI spec and provides a typed HTTP client for calling its operations",
		Inputs:      []ServiceIODef{{Name: "spec", Type: "OpenAPISpec", Description: "External OpenAPI specification to consume"}},
		Outputs:     []ServiceIODef{{Name: "client", Type: "ExternalAPIClient", Description: "HTTP client with operations matching the spec"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "specUrl", Label: "Spec URL", Type: FieldTypeString, Description: "URL to fetch the OpenAPI spec from", Placeholder: "https://api.example.com/openapi.json"},
			{Key: "specFile", Label: "Spec File", Type: FieldTypeFilePath, Description: "Local file path to the OpenAPI spec (JSON or YAML)", Placeholder: "specs/external-api.json"},
			{Key: "fieldMapping", Label: "Field Mapping", Type: FieldTypeMap, MapValueType: "string", Description: "Custom field name mapping between local workflow data and external API schemas", Group: "advanced"},
		},
	})

	// ---- Pipeline Steps Category ----

	r.Register(&ModuleSchema{
		Type:        "step.validate",
		Label:       "Validate",
		Category:    "pipeline",
		Description: "Validates pipeline data using JSON Schema or required fields check",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context with current data to validate"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "Validation result (pass-through on success, error on failure)"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "strategy", Label: "Strategy", Type: FieldTypeSelect, Options: []string{"json_schema", "required_fields"}, DefaultValue: "required_fields", Description: "Validation strategy to use"},
			{Key: "schema", Label: "JSON Schema", Type: FieldTypeMap, Description: "JSON Schema definition for validation (when strategy is json_schema)"},
			{Key: "required_fields", Label: "Required Fields", Type: FieldTypeArray, ArrayItemType: "string", Description: "List of required field names (when strategy is required_fields)"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.transform",
		Label:       "Transform",
		Category:    "pipeline",
		Description: "Transforms pipeline data using extract, map, filter, and convert operations",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context with data to transform"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "Transformed data merged back into pipeline context"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "transformer", Label: "Transformer Service", Type: FieldTypeString, Description: "Name of a DataTransformer service to use", Placeholder: "my-transformer", InheritFrom: "dependency.name"},
			{Key: "pipeline", Label: "Pipeline Name", Type: FieldTypeString, Description: "Named pipeline within the transformer", Placeholder: "normalize"},
			{Key: "operations", Label: "Operations", Type: FieldTypeArray, Description: "Inline transformation operations (alternative to transformer+pipeline)"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.conditional",
		Label:       "Conditional",
		Category:    "pipeline",
		Description: "Routes to different pipeline steps based on a field value",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context with field to evaluate for routing"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "Routing decision with target step name"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "field", Label: "Field", Type: FieldTypeString, Required: true, Description: "Field path to evaluate for routing", Placeholder: "event_type"},
			{Key: "routes", Label: "Routes", Type: FieldTypeMap, MapValueType: "string", Required: true, Description: "Map of field values to target step names"},
			{Key: "default", Label: "Default Step", Type: FieldTypeString, Description: "Step name to route to when no match is found"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.publish",
		Label:       "Publish Event",
		Category:    "pipeline",
		Description: "Publishes pipeline data to an EventBus topic or message broker",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context with data to publish"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "Publish confirmation with topic and message ID"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "topic", Label: "Topic", Type: FieldTypeString, Required: true, Description: "Topic name to publish to", Placeholder: "order.created"},
			{Key: "payload", Label: "Payload", Type: FieldTypeMap, Description: "Custom payload template (uses {{ .field }} expressions). Defaults to pipeline context."},
			{Key: "broker", Label: "Broker Service", Type: FieldTypeString, Description: "Message broker service name (optional, defaults to EventBus)", InheritFrom: "dependency.name"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.set",
		Label:       "Set Values",
		Category:    "pipeline",
		Description: "Sets or overrides values in the pipeline context",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context to update with new values"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "Updated pipeline context with set values"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "values", Label: "Values", Type: FieldTypeMap, MapValueType: "string", Required: true, Description: "Key-value pairs to set (values support {{ .field }} templates)"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.log",
		Label:       "Log",
		Category:    "pipeline",
		Description: "Logs a message at a specified level during pipeline execution",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context with data for message template resolution"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "Pass-through result (logging is a side effect)"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "level", Label: "Level", Type: FieldTypeSelect, Options: []string{"debug", "info", "warn", "error"}, DefaultValue: "info", Description: "Log level"},
			{Key: "message", Label: "Message", Type: FieldTypeString, Required: true, Description: "Message template (supports {{ .field }} expressions)", Placeholder: "Processing {{ .event_type }}"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.http_call",
		Label:       "HTTP Call",
		Category:    "pipeline",
		Description: "Makes an outbound HTTP request and returns the response",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context with data for URL/body template resolution"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "HTTP response body parsed as JSON and merged into pipeline context"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "url", Label: "URL", Type: FieldTypeString, Required: true, Description: "Request URL (supports {{ .field }} templates; {{ .instance_url }} is available when OAuth2 client_credentials auth uses a token endpoint that returns instance_url)", Placeholder: "https://api.example.com/{{ .resource }}"},
			{Key: "method", Label: "Method", Type: FieldTypeSelect, Options: []string{"GET", "POST", "PUT", "PATCH", "DELETE"}, DefaultValue: "GET", Description: "HTTP method"},
			{Key: "headers", Label: "Headers", Type: FieldTypeMap, MapValueType: "string", Description: "Request headers (values support templates)"},
			{Key: "body", Label: "Body", Type: FieldTypeMap, Description: "Request body (supports templates). For POST/PUT without body, sends pipeline context."},
			{Key: "timeout", Label: "Timeout", Type: FieldTypeString, DefaultValue: "30s", Description: "Request timeout duration", Placeholder: "30s"},
			{Key: "oauth2", Label: "OAuth2", Type: FieldTypeMap, Description: "OAuth2 client_credentials configuration (grant_type, token_url, client_id, client_secret, scopes). Tokens are cached and refreshed automatically."},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.http_proxy",
		Label:       "HTTP Proxy",
		Category:    "pipeline",
		Description: "Forwards the original HTTP request to a dynamically resolved backend URL and writes the response directly to the client",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context with _http_request and _http_response_writer metadata"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "Proxy response status and target URL; Stop is always true"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "backend_url_key", Label: "Backend URL Key", Type: FieldTypeString, DefaultValue: "backend_url", Description: "Dot-path in pc.Current for the backend URL (e.g. backend_url or steps.resolve.url)"},
			{Key: "resource_key", Label: "Resource Key", Type: FieldTypeString, DefaultValue: "path_params.resource", Description: "Dot-path in pc.Current for the resource path suffix"},
			{Key: "forward_headers", Label: "Forward Headers", Type: FieldTypeArray, ArrayItemType: "string", Description: "Header names to copy from the original request to the proxy request"},
			{Key: "timeout", Label: "Timeout", Type: FieldTypeString, DefaultValue: "30s", Description: "Proxy request timeout duration", Placeholder: "30s"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.delegate",
		Label:       "Delegate",
		Category:    "pipeline",
		Description: "Forwards the request to a named service implementing http.Handler",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context with HTTP request metadata"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "Delegate service handles the full HTTP response"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "service", Label: "Service", Type: FieldTypeString, Required: true, Description: "Name of the service to delegate to (must implement http.Handler)", Placeholder: "my-service", InheritFrom: "dependency.name"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.request_parse",
		Label:       "Request Parse",
		Category:    "pipeline",
		Description: "Parses HTTP request path parameters, query parameters, and body from pipeline metadata",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context with _http_request and _route_pattern metadata"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "Parsed path_params, query, and body maps"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "path_params", Label: "Path Parameters", Type: FieldTypeArray, ArrayItemType: "string", Description: "Parameter names to extract from URL path (e.g., id, companyId)"},
			{Key: "query_params", Label: "Query Parameters", Type: FieldTypeArray, ArrayItemType: "string", Description: "Query string parameter names to extract"},
			{Key: "parse_body", Label: "Parse Body", Type: FieldTypeBool, Description: "Whether to parse the JSON request body"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.db_query",
		Label:       "Database Query",
		Category:    "pipeline",
		Description: "Executes a parameterized SQL SELECT query against a named database service",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context for template parameter resolution"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "Query results as rows/count (list mode) or row/found (single mode)"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "database", Label: "Database", Type: FieldTypeString, Required: true, Description: "Name of the database service (must implement DBProvider)", Placeholder: "admin-db", InheritFrom: "dependency.name"},
			{Key: "query", Label: "SQL Query", Type: FieldTypeSQL, Required: true, Description: "Parameterized SQL SELECT query (use ? for placeholders). Template expressions are forbidden unless allow_dynamic_sql is true.", Placeholder: "SELECT id, name FROM companies WHERE id = ?"},
			{Key: "params", Label: "Parameters", Type: FieldTypeArray, ArrayItemType: "string", Description: "Template-resolved parameter values for ? placeholders in query"},
			{Key: "mode", Label: "Mode", Type: FieldTypeSelect, Options: []string{"list", "single"}, DefaultValue: "list", Description: "Result mode: 'list' returns rows/count, 'single' returns row/found"},
			{Key: "tenantKey", Label: "Tenant Key", Type: FieldTypeString, Description: "Dot-path in pipeline context to resolve the tenant value for automatic scoping (requires database.partitioned)", Placeholder: "steps.auth.tenant_id"},
			{Key: "allow_dynamic_sql", Label: "Allow Dynamic SQL", Type: FieldTypeBool, DefaultValue: "false", Description: "When true, template expressions in 'query' are resolved at runtime. Each resolved value must contain only letters, digits, underscores and hyphens to prevent SQL injection."},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.db_query_cached",
		Label:       "Database Query (Cached)",
		Category:    "pipeline",
		Description: "Executes a parameterized SQL SELECT query and caches the result in-process with TTL. On subsequent calls the cached value is returned until the TTL expires.",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context for template parameter and cache key resolution"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "Query results as rows/count (list mode) or row/found (single mode), plus cache_hit boolean"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "database", Label: "Database", Type: FieldTypeString, Required: true, Description: "Name of the database service (must implement DBProvider)", Placeholder: "db", InheritFrom: "dependency.name"},
			{Key: "query", Label: "SQL Query", Type: FieldTypeSQL, Required: true, Description: "Parameterized SQL SELECT query using $N placeholders (e.g. $1, $2); automatically converted to ? for SQLite drivers. Template expressions are forbidden unless allow_dynamic_sql is true.", Placeholder: "SELECT backend_url, settings FROM routing_config WHERE tenant_id = $1 LIMIT 1"},
			{Key: "params", Label: "Parameters", Type: FieldTypeArray, ArrayItemType: "string", Description: "Template-resolved parameter values for query placeholders"},
			{Key: "mode", Label: "Mode", Type: FieldTypeSelect, Options: []string{"single", "list"}, DefaultValue: "single", Description: "Result mode: 'single' returns row/found, 'list' returns rows/count"},
			{Key: "cache_key", Label: "Cache Key", Type: FieldTypeString, Required: true, Description: "Template-resolved key used to store/retrieve the cached result", Placeholder: "tenant_config:{{.steps.parse.headers.X-Tenant-Id}}"},
			{Key: "cache_ttl", Label: "Cache TTL", Type: FieldTypeString, DefaultValue: "5m", Description: "Duration string for how long to cache the result (e.g. '5m', '30s', '1h')", Placeholder: "5m"},
			{Key: "scan_fields", Label: "Scan Fields", Type: FieldTypeArray, ArrayItemType: "string", Description: "Column names to include in the output (omit to include all columns)"},
			{Key: "allow_dynamic_sql", Label: "Allow Dynamic SQL", Type: FieldTypeBool, DefaultValue: "false", Description: "When true, template expressions in 'query' are resolved at runtime. Each resolved value must contain only letters, digits, and underscores to prevent SQL injection."},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.db_exec",
		Label:       "Database Execute",
		Category:    "pipeline",
		Description: "Executes a parameterized SQL INSERT/UPDATE/DELETE against a named database service",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context for template parameter resolution"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "Execution result with affected_rows and last_id"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "database", Label: "Database", Type: FieldTypeString, Required: true, Description: "Name of the database service (must implement DBProvider)", Placeholder: "admin-db", InheritFrom: "dependency.name"},
			{Key: "query", Label: "SQL Statement", Type: FieldTypeSQL, Required: true, Description: "Parameterized SQL INSERT/UPDATE/DELETE statement (use ? for placeholders). Template expressions are forbidden unless allow_dynamic_sql is true.", Placeholder: "INSERT INTO companies (id, name) VALUES (?, ?)"},
			{Key: "params", Label: "Parameters", Type: FieldTypeArray, ArrayItemType: "string", Description: "Template-resolved parameter values for ? placeholders"},
			{Key: "tenantKey", Label: "Tenant Key", Type: FieldTypeString, Description: "Dot-path in pipeline context to resolve the tenant value for automatic scoping. Supported for UPDATE/DELETE only (requires database.partitioned)", Placeholder: "steps.auth.tenant_id"},
			{Key: "allow_dynamic_sql", Label: "Allow Dynamic SQL", Type: FieldTypeBool, DefaultValue: "false", Description: "When true, template expressions in 'query' are resolved at runtime. Each resolved value must contain only letters, digits, underscores and hyphens to prevent SQL injection."},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.db_create_partition",
		Label:       "Create Database Partition",
		Category:    "pipeline",
		Description: "Creates a PostgreSQL partition for a tenant on all tables managed by a database.partitioned module. Supports both LIST and RANGE partition types. Idempotent — safe to call when a partition may already exist.",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context for tenant key resolution"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "Partition creation result with tenant and partition fields"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "database", Label: "Database", Type: FieldTypeString, Required: true, Description: "Name of a database.partitioned service", Placeholder: "db", InheritFrom: "dependency.name"},
			{Key: "tenantKey", Label: "Tenant Key", Type: FieldTypeString, Required: true, Description: "Dot-path in pipeline context to resolve the tenant value (e.g. the new tenant's ID)", Placeholder: "steps.body.tenant_id"},
			{Key: "partitionKey", Label: "Partition Key", Type: FieldTypeString, Description: "Target a specific partition config by its partitionKey. Required when the database has multiple partition configs. Omit to use the primary (first) partition config.", Placeholder: "tenant_id"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.db_sync_partitions",
		Label:       "Sync Database Partitions",
		Category:    "pipeline",
		Description: "Synchronizes partitions from the configured source table in a database.partitioned module. Queries all distinct tenant values from the source table and creates missing partitions for all managed tables.",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context (not used but required for step interface)"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "Sync result with synced boolean"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "database", Label: "Database", Type: FieldTypeString, Required: true, Description: "Name of a database.partitioned service with sourceTable configured", Placeholder: "db", InheritFrom: "dependency.name"},
			{Key: "partitionKey", Label: "Partition Key", Type: FieldTypeString, Description: "Target a specific partition config by its partitionKey for syncing. Omit to sync all configured partition groups.", Placeholder: "tenant_id"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.json_response",
		Label:       "JSON Response",
		Category:    "pipeline",
		Description: "Writes an HTTP JSON response with custom status code and stops the pipeline",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context with _http_response_writer metadata"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "Response status (always sets Stop: true)"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "status", Label: "Status Code", Type: FieldTypeNumber, DefaultValue: "200", Description: "HTTP status code for the response"},
			{Key: "status_from", Label: "Status From", Type: FieldTypeString, Description: "Dotted path to resolve HTTP status code dynamically (e.g., steps.call_upstream.status_code). Takes precedence over 'status' when resolved to a valid HTTP status code (100-599).", Placeholder: "steps.call_upstream.status_code"},
			{Key: "headers", Label: "Headers", Type: FieldTypeMap, MapValueType: "string", Description: "Additional response headers"},
			{Key: "body", Label: "Body", Type: FieldTypeMap, Description: "Response body as JSON (supports template expressions)"},
			{Key: "body_from", Label: "Body From", Type: FieldTypeString, Description: "Dotted path to resolve body from step outputs (e.g., steps.get-company.row)", Placeholder: "steps.get-company.row"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.jq",
		Label:       "JQ Transform",
		Category:    "pipeline",
		Description: "Applies JQ expressions to pipeline data for complex transformations (field access, pipes, map/select, object construction, arithmetic, conditionals)",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context with data to transform using JQ expression"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "JQ expression result merged into pipeline context"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "expression", Label: "JQ Expression", Type: FieldTypeString, Required: true, Description: "JQ expression to apply (supports full JQ syntax: field access, pipes, map, select, object construction, arithmetic, conditionals)", Placeholder: "{name: .user.name, total: [.items[].price] | add}"},
			{Key: "input_from", Label: "Input From", Type: FieldTypeString, Description: "Dotted path to resolve input from (e.g., steps.fetch.data). Defaults to full pipeline context.", Placeholder: "steps.fetch-orders.orders"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.base64_decode",
		Label:       "Base64 Decode",
		Category:    "pipeline",
		Description: "Decodes base64-encoded content (raw or data-URI), validates MIME type and size, and returns structured metadata",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context containing the encoded data at the path specified by input_from"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "Decoded content metadata: content_type, extension, size_bytes, data (base64), valid, reason (on failure)"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "input_from", Label: "Input From", Type: FieldTypeString, Required: true, Description: "Dotted path to the encoded data in the pipeline context (e.g., steps.upload.file_data)", Placeholder: "steps.upload.file_data"},
			{Key: "format", Label: "Format", Type: FieldTypeSelect, Options: []string{"data_uri", "raw_base64"}, DefaultValue: "data_uri", Description: "Encoding format: 'data_uri' expects a data:mime/type;base64,... string; 'raw_base64' expects plain base64"},
			{Key: "allowed_types", Label: "Allowed MIME Types", Type: FieldTypeArray, ArrayItemType: "string", Description: "Whitelist of allowed MIME types (e.g., [\"image/jpeg\", \"image/png\"]). Omit to allow all types."},
			{Key: "max_size_bytes", Label: "Max Size (bytes)", Type: FieldTypeNumber, Description: "Maximum allowed decoded size in bytes. 0 means unlimited."},
			{Key: "validate_magic_bytes", Label: "Validate Magic Bytes", Type: FieldTypeBool, DefaultValue: "false", Description: "When true, verifies the decoded content matches the MIME type claimed in the data-URI header"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.branch",
		Label:       "Branch",
		Category:    "pipeline",
		Description: "Switch/case routing: evaluates a field, executes only the matched branch's sub-steps inline, then jumps to merge_step. Unlike step.conditional, skipped branches never execute.",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context containing the field to evaluate"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "Branch result with matched_value and branch name"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "field", Label: "Field", Type: FieldTypeString, Required: true, Description: "Dot-path field to evaluate for branch selection"},
			{Key: "branches", Label: "Branches", Type: FieldTypeMap, Required: true, Description: "Map of field values to lists of inline step configs"},
			{Key: "default", Label: "Default", Type: FieldTypeArray, Description: "Sub-steps to run when no branch matches"},
			{Key: "merge_step", Label: "Merge Step", Type: FieldTypeString, Description: "Step name to jump to after the branch completes (empty = continue sequentially)"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.s3_upload",
		Label:       "S3 Upload",
		Category:    "pipeline",
		Description: "Uploads base64-encoded binary data from the pipeline context to AWS S3 or S3-compatible storage (MinIO, LocalStack). Returns the public URL, resolved object key, and bucket name.",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context with binary data (base64-encoded) and optional MIME type"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "Upload result with url, key, and bucket fields"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "bucket", Label: "Bucket", Type: FieldTypeString, Required: true, Description: "S3 bucket name (supports ${ENV_VAR} expansion)", Placeholder: "${AVATAR_BUCKET}"},
			{Key: "region", Label: "Region", Type: FieldTypeString, Required: true, Description: "AWS region (supports ${ENV_VAR} expansion)", Placeholder: "${AWS_REGION}"},
			{Key: "key", Label: "Object Key", Type: FieldTypeString, Required: true, Description: "S3 object key (supports {{ .field }} templates and {{ uuid }})", Placeholder: "avatars/{{ .user_id }}/{{ uuid }}.{{ .ext }}"},
			{Key: "body_from", Label: "Body From", Type: FieldTypeString, Required: true, Description: "Dot-path to the base64-encoded binary data in the pipeline context (e.g. steps.parse.image_data)", Placeholder: "steps.parse.image_data"},
			{Key: "content_type_from", Label: "Content Type From", Type: FieldTypeString, Description: "Dot-path to the MIME type in the pipeline context (takes precedence over content_type)", Placeholder: "steps.parse.mime_type"},
			{Key: "content_type", Label: "Content Type", Type: FieldTypeString, Description: "Static MIME type for the uploaded object (e.g. image/png)", Placeholder: "image/png"},
			{Key: "endpoint", Label: "Endpoint", Type: FieldTypeString, Description: "Custom S3 endpoint URL for MinIO, LocalStack, or other S3-compatible storage (supports ${ENV_VAR} expansion). Leave empty for AWS.", Placeholder: "http://localhost:9000"},
		},
	})

	// ---- CI/CD Pipeline Steps Category ----

	r.Register(&ModuleSchema{
		Type:        "step.shell_exec",
		Label:       "Shell Exec",
		Category:    "cicd",
		Description: "Executes shell commands inside a Docker container, optionally collecting output artifacts",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context with execution metadata"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "Command outputs and collected artifacts"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "image", Label: "Docker Image", Type: FieldTypeString, Required: true, Description: "Docker image to run commands in", Placeholder: "ubuntu:22.04"},
			{Key: "commands", Label: "Commands", Type: FieldTypeArray, ArrayItemType: "string", Required: true, Description: "Shell commands to execute sequentially"},
			{Key: "work_dir", Label: "Working Directory", Type: FieldTypeString, Description: "Working directory inside the container", Placeholder: "/workspace"},
			{Key: "timeout", Label: "Timeout", Type: FieldTypeDuration, Description: "Maximum execution time for all commands", Placeholder: "5m"},
			{Key: "env", Label: "Environment Variables", Type: FieldTypeMap, MapValueType: "string", Description: "Environment variables to set in the container"},
			{Key: "artifacts_out", Label: "Output Artifacts", Type: FieldTypeArray, Description: "Artifacts to collect after execution (array of {key, path})"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.artifact_pull",
		Label:       "Artifact Pull",
		Category:    "cicd",
		Description: "Retrieves an artifact from a previous execution, URL, or S3 and writes it to a destination path",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context with artifact store metadata"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "Downloaded artifact metadata (source, key, dest, size)"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "source", Label: "Source", Type: FieldTypeSelect, Options: []string{"previous_execution", "url", "s3"}, Required: true, Description: "Artifact source type"},
			{Key: "dest", Label: "Destination Path", Type: FieldTypeString, Required: true, Description: "Local file path to write the artifact to", Placeholder: "/workspace/artifact.tar.gz"},
			{Key: "key", Label: "Artifact Key", Type: FieldTypeString, Description: "Artifact key (required for previous_execution and s3 sources)"},
			{Key: "execution_id", Label: "Execution ID", Type: FieldTypeString, Description: "Execution ID to pull from (defaults to current execution)"},
			{Key: "url", Label: "URL", Type: FieldTypeString, Description: "URL to download artifact from (required for url source)", Placeholder: "https://example.com/artifact.tar.gz"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.artifact_push",
		Label:       "Artifact Push",
		Category:    "cicd",
		Description: "Reads a local file and stores it in the artifact store with a checksum",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context with artifact store metadata"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "Stored artifact metadata (key, size, checksum)"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "source_path", Label: "Source Path", Type: FieldTypeString, Required: true, Description: "Local file path to read and push", Placeholder: "/workspace/build/output.tar.gz"},
			{Key: "key", Label: "Artifact Key", Type: FieldTypeString, Required: true, Description: "Unique key for the artifact in the store", Placeholder: "build-output"},
			{Key: "dest", Label: "Destination", Type: FieldTypeString, DefaultValue: "artifact_store", Description: "Destination store identifier"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.docker_build",
		Label:       "Docker Build",
		Category:    "cicd",
		Description: "Builds a Docker image from a context directory and Dockerfile using the Docker SDK",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "Built image ID and tags"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "context", Label: "Build Context", Type: FieldTypeString, Required: true, Description: "Path to the Docker build context directory", Placeholder: "."},
			{Key: "dockerfile", Label: "Dockerfile", Type: FieldTypeString, DefaultValue: "Dockerfile", Description: "Path to Dockerfile relative to context"},
			{Key: "tags", Label: "Image Tags", Type: FieldTypeArray, ArrayItemType: "string", Description: "Tags for the built image", Placeholder: "myapp:latest"},
			{Key: "build_args", Label: "Build Args", Type: FieldTypeMap, MapValueType: "string", Description: "Docker build arguments"},
			{Key: "cache_from", Label: "Cache From", Type: FieldTypeArray, ArrayItemType: "string", Description: "Images to use as cache sources"},
		},
		DefaultConfig: map[string]any{"dockerfile": "Dockerfile"},
	})

	r.Register(&ModuleSchema{
		Type:        "step.docker_push",
		Label:       "Docker Push",
		Category:    "cicd",
		Description: "Pushes a Docker image to a remote registry",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "Push result with image digest"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "image", Label: "Image", Type: FieldTypeString, Required: true, Description: "Image name to push", Placeholder: "myapp:latest"},
			{Key: "registry", Label: "Registry", Type: FieldTypeString, Description: "Registry hostname (prepended to image name)", Placeholder: "ghcr.io/myorg"},
			{Key: "auth_provider", Label: "Auth Provider", Type: FieldTypeString, Description: "Authentication provider for the registry"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.docker_run",
		Label:       "Docker Run",
		Category:    "cicd",
		Description: "Runs a command inside a Docker container using the sandbox",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "Container exit code, stdout, and stderr"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "image", Label: "Docker Image", Type: FieldTypeString, Required: true, Description: "Docker image to run", Placeholder: "alpine:latest"},
			{Key: "command", Label: "Command", Type: FieldTypeArray, ArrayItemType: "string", Description: "Command to execute in the container"},
			{Key: "env", Label: "Environment Variables", Type: FieldTypeMap, MapValueType: "string", Description: "Environment variables for the container"},
			{Key: "wait_for_exit", Label: "Wait For Exit", Type: FieldTypeBool, DefaultValue: true, Description: "Whether to wait for the container to exit"},
			{Key: "timeout", Label: "Timeout", Type: FieldTypeDuration, Description: "Maximum execution time", Placeholder: "5m"},
		},
		DefaultConfig: map[string]any{"wait_for_exit": true},
	})

	// ---- Security Scan Steps Category ----

	r.Register(&ModuleSchema{
		Type:        "step.scan_sast",
		Label:       "SAST Scan",
		Category:    "security",
		Description: "Runs a SAST (Static Application Security Testing) scanner and evaluates findings against a severity gate",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "Scan result with findings and gate evaluation"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "scanner", Label: "Scanner", Type: FieldTypeString, Required: true, Description: "SAST scanner to use (e.g., semgrep)", Placeholder: "semgrep"},
			{Key: "image", Label: "Scanner Image", Type: FieldTypeString, DefaultValue: "semgrep/semgrep:latest", Description: "Docker image for the scanner"},
			{Key: "source_path", Label: "Source Path", Type: FieldTypeString, DefaultValue: "/workspace", Description: "Path to source code to scan"},
			{Key: "rules", Label: "Rules", Type: FieldTypeArray, ArrayItemType: "string", Description: "Scanner rule configurations"},
			{Key: "fail_on_severity", Label: "Fail on Severity", Type: FieldTypeSelect, Options: []string{"critical", "high", "medium", "low", "info"}, DefaultValue: "error", Description: "Minimum severity level to fail the gate"},
			{Key: "output_format", Label: "Output Format", Type: FieldTypeSelect, Options: []string{"sarif", "json"}, DefaultValue: "sarif", Description: "Scan output format"},
		},
		DefaultConfig: map[string]any{"image": "semgrep/semgrep:latest", "source_path": "/workspace", "fail_on_severity": "error", "output_format": "sarif"},
	})

	r.Register(&ModuleSchema{
		Type:        "step.scan_container",
		Label:       "Container Scan",
		Category:    "security",
		Description: "Runs a container vulnerability scanner (e.g., Trivy) against a target image",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "Scan result with vulnerabilities and gate evaluation"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "scanner", Label: "Scanner", Type: FieldTypeString, DefaultValue: "trivy", Description: "Container scanner to use"},
			{Key: "image", Label: "Scanner Image", Type: FieldTypeString, DefaultValue: "aquasec/trivy:latest", Description: "Docker image for the scanner"},
			{Key: "target_image", Label: "Target Image", Type: FieldTypeString, Required: true, Description: "Docker image to scan for vulnerabilities", Placeholder: "myapp:latest"},
			{Key: "severity_threshold", Label: "Severity Threshold", Type: FieldTypeSelect, Options: []string{"CRITICAL", "HIGH", "MEDIUM", "LOW"}, DefaultValue: "HIGH", Description: "Minimum severity to report"},
			{Key: "ignore_unfixed", Label: "Ignore Unfixed", Type: FieldTypeBool, Description: "Skip vulnerabilities without available fixes"},
			{Key: "output_format", Label: "Output Format", Type: FieldTypeSelect, Options: []string{"sarif", "json"}, DefaultValue: "sarif", Description: "Scan output format"},
		},
		DefaultConfig: map[string]any{"scanner": "trivy", "image": "aquasec/trivy:latest", "severity_threshold": "HIGH", "output_format": "sarif"},
	})

	r.Register(&ModuleSchema{
		Type:        "step.scan_deps",
		Label:       "Dependency Scan",
		Category:    "security",
		Description: "Runs a dependency vulnerability scanner (e.g., Grype) against source code",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "Scan result with dependency vulnerabilities and gate evaluation"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "scanner", Label: "Scanner", Type: FieldTypeString, DefaultValue: "grype", Description: "Dependency scanner to use"},
			{Key: "image", Label: "Scanner Image", Type: FieldTypeString, DefaultValue: "anchore/grype:latest", Description: "Docker image for the scanner"},
			{Key: "source_path", Label: "Source Path", Type: FieldTypeString, DefaultValue: "/workspace", Description: "Path to source code to scan for dependencies"},
			{Key: "fail_on_severity", Label: "Fail on Severity", Type: FieldTypeSelect, Options: []string{"critical", "high", "medium", "low", "info"}, DefaultValue: "high", Description: "Minimum severity level to fail the gate"},
			{Key: "output_format", Label: "Output Format", Type: FieldTypeSelect, Options: []string{"sarif", "json"}, DefaultValue: "sarif", Description: "Scan output format"},
		},
		DefaultConfig: map[string]any{"scanner": "grype", "image": "anchore/grype:latest", "source_path": "/workspace", "fail_on_severity": "high", "output_format": "sarif"},
	})

	// ---- Deployment Steps Category ----

	r.Register(&ModuleSchema{
		Type:        "step.deploy",
		Label:       "Deploy",
		Category:    "deployment",
		Description: "Executes a deployment through a cloud provider using a specified strategy (rolling, blue-green, canary)",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context with deploy executor metadata"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "Deployment result with deploy ID, status, and provider info"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "environment", Label: "Environment", Type: FieldTypeString, Required: true, Description: "Target deployment environment", Placeholder: "production"},
			{Key: "strategy", Label: "Strategy", Type: FieldTypeSelect, Options: []string{"rolling", "blue_green", "canary"}, Required: true, Description: "Deployment strategy to use"},
			{Key: "image", Label: "Image", Type: FieldTypeString, Required: true, Description: "Docker image to deploy", Placeholder: "myapp:v1.2.3"},
			{Key: "provider", Label: "Provider", Type: FieldTypeSelect, Options: []string{"aws", "gcp", "azure", "digitalocean"}, Description: "Cloud provider to deploy to"},
			{Key: "rollback_on_failure", Label: "Rollback on Failure", Type: FieldTypeBool, Description: "Automatically rollback if deployment fails"},
			{Key: "health_check", Label: "Health Check", Type: FieldTypeMap, Description: "Health check configuration (path, interval, timeout, thresholds)"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.gate",
		Label:       "Approval Gate",
		Category:    "deployment",
		Description: "Implements an approval gate supporting manual, automated, and scheduled gate types",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context for condition evaluation"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "Gate result (passed/failed with reason)"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "type", Label: "Gate Type", Type: FieldTypeSelect, Options: []string{"manual", "automated", "scheduled"}, Required: true, Description: "Type of approval gate"},
			{Key: "approvers", Label: "Approvers", Type: FieldTypeArray, ArrayItemType: "string", Description: "List of approver identifiers (for manual gates)"},
			{Key: "timeout", Label: "Timeout", Type: FieldTypeDuration, DefaultValue: "24h", Description: "Maximum time to wait for approval", Placeholder: "24h"},
			{Key: "auto_approve_conditions", Label: "Auto-Approve Conditions", Type: FieldTypeArray, ArrayItemType: "string", Description: "Conditions for automated approval (key.path == value format)"},
			{Key: "schedule", Label: "Schedule Window", Type: FieldTypeMap, Description: "Time window for scheduled gates (weekdays, start_hour, end_hour)"},
		},
		DefaultConfig: map[string]any{"timeout": "24h"},
	})

	r.Register(&ModuleSchema{
		Type:        "step.build_ui",
		Label:       "Build UI",
		Category:    "deployment",
		Description: "Executes a UI build pipeline natively (npm install + build), producing static assets for static.fileserver",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context with build parameters"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "Build result with output path and file count"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "source_dir", Label: "Source Directory", Type: FieldTypeString, Required: true, Description: "UI source directory containing package.json", Placeholder: "ui"},
			{Key: "output_dir", Label: "Output Directory", Type: FieldTypeString, Required: true, Description: "Where to place built assets (for static.fileserver root)", Placeholder: "ui_dist"},
			{Key: "install_cmd", Label: "Install Command", Type: FieldTypeString, DefaultValue: "npm install --silent", Description: "Dependency install command"},
			{Key: "build_cmd", Label: "Build Command", Type: FieldTypeString, DefaultValue: "npm run build", Description: "Build command to generate static assets"},
			{Key: "env", Label: "Environment Variables", Type: FieldTypeMap, Description: "Extra environment variables for the build process"},
			{Key: "timeout", Label: "Timeout", Type: FieldTypeDuration, DefaultValue: "5m", Description: "Maximum time for the build", Placeholder: "5m"},
		},
		DefaultConfig: map[string]any{"install_cmd": "npm install --silent", "build_cmd": "npm run build", "timeout": "5m"},
	})

	// -----------------------------------------------------------------------
	// API Gateway module
	// -----------------------------------------------------------------------

	r.Register(&ModuleSchema{
		Type:        "api.gateway",
		Label:       "API Gateway",
		Category:    "http",
		Description: "Composable API gateway combining routing, auth, rate limiting, CORS, and reverse proxying into a single module",
		Inputs:      []ServiceIODef{{Name: "http_request", Type: "http.Request", Description: "Incoming HTTP request"}},
		Outputs:     []ServiceIODef{{Name: "http_response", Type: "http.Response", Description: "Proxied response from backend"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "routes", Label: "Routes", Type: FieldTypeArray, Required: true, Description: "Array of route definitions with pathPrefix, backend, methods, rateLimit, auth, timeout"},
			{Key: "globalRateLimit", Label: "Global Rate Limit", Type: FieldTypeMap, Description: "Global rate limit applied to all routes (requestsPerMinute, burstSize)"},
			{Key: "cors", Label: "CORS Config", Type: FieldTypeMap, Description: "CORS settings (allowOrigins, allowMethods, allowHeaders, maxAge)"},
			{Key: "auth", Label: "Auth Config", Type: FieldTypeMap, Description: "Authentication settings (type: bearer/api_key/basic, header)"},
		},
	})

	// -----------------------------------------------------------------------
	// Gateway & resilience pipeline steps
	// -----------------------------------------------------------------------

	r.Register(&ModuleSchema{
		Type:        "step.rate_limit",
		Label:       "Rate Limit",
		Category:    "resilience",
		Description: "Enforces rate limiting using a token bucket algorithm, rejecting requests that exceed the limit",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context with request data"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "Pass-through on success, error when rate limited"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "requests_per_minute", Label: "Requests/Min", Type: FieldTypeNumber, Required: true, Description: "Maximum requests per minute", Placeholder: "60"},
			{Key: "burst_size", Label: "Burst Size", Type: FieldTypeNumber, Description: "Maximum burst size (defaults to requests_per_minute)"},
			{Key: "key_from", Label: "Key Template", Type: FieldTypeString, Description: "Template for per-client rate limit key (e.g. {{.client_ip}})"},
		},
		DefaultConfig: map[string]any{"requests_per_minute": 60},
	})

	r.Register(&ModuleSchema{
		Type:        "step.circuit_breaker",
		Label:       "Circuit Breaker",
		Category:    "resilience",
		Description: "Implements the circuit breaker pattern, tracking failures per service and preventing calls when the circuit is open",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "Pass-through when closed, error when open"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "service", Label: "Service Name", Type: FieldTypeString, Required: true, Description: "Service identifier for circuit tracking"},
			{Key: "failure_threshold", Label: "Failure Threshold", Type: FieldTypeNumber, DefaultValue: "5", Description: "Consecutive failures before opening circuit"},
			{Key: "success_threshold", Label: "Success Threshold", Type: FieldTypeNumber, DefaultValue: "2", Description: "Consecutive successes in half-open before closing"},
			{Key: "timeout", Label: "Recovery Timeout", Type: FieldTypeDuration, DefaultValue: "30s", Description: "Time to wait before trying half-open"},
		},
		DefaultConfig: map[string]any{"failure_threshold": 5, "success_threshold": 2, "timeout": "30s"},
	})

	// -----------------------------------------------------------------------
	// Plugin workflow composition step
	// -----------------------------------------------------------------------

	r.Register(&ModuleSchema{
		Type:        "step.sub_workflow",
		Label:       "Sub-Workflow",
		Category:    "composition",
		Description: "Invokes a registered plugin workflow as a sub-workflow with input/output mapping",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context with input data"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "Sub-workflow execution result with mapped outputs"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "workflow", Label: "Workflow", Type: FieldTypeString, Required: true, Description: "Qualified workflow name (plugin:workflow-name)", Placeholder: "my-plugin:payment-flow"},
			{Key: "input_mapping", Label: "Input Mapping", Type: FieldTypeMap, Description: "Map of sub-workflow input keys to template expressions"},
			{Key: "output_mapping", Label: "Output Mapping", Type: FieldTypeMap, Description: "Map of parent context keys to sub-workflow output paths"},
			{Key: "timeout", Label: "Timeout", Type: FieldTypeDuration, DefaultValue: "30s", Description: "Maximum execution time for the sub-workflow"},
		},
		DefaultConfig: map[string]any{"timeout": "30s"},
	})

	// -----------------------------------------------------------------------
	// Cross-workflow call step (multi-workflow composition)
	// -----------------------------------------------------------------------

	r.Register(&ModuleSchema{
		Type:        "step.workflow_call",
		Label:       "Workflow Call",
		Category:    "composition",
		Description: "Invokes another pipeline registered in the same engine application. Supports sync (call & wait) and async (fire-and-forget) modes with input/output mapping.",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context with input data"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "Called workflow result with mapped outputs (sync mode) or dispatch confirmation (async mode)"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "workflow", Label: "Workflow", Type: FieldTypeString, Required: true, Description: "Name of the target pipeline to call (must be registered in the same engine)", Placeholder: "queue-assignment"},
			{Key: "mode", Label: "Mode", Type: FieldTypeSelect, Options: []string{"sync", "async"}, DefaultValue: "sync", Description: "Execution mode: 'sync' waits for result, 'async' fires and returns immediately"},
			{Key: "input", Label: "Input Mapping", Type: FieldTypeMap, Description: "Map of target pipeline input keys to template expressions from the current context. If omitted, all current context data is passed through."},
			{Key: "output_mapping", Label: "Output Mapping", Type: FieldTypeMap, Description: "Map of parent context keys to target pipeline output paths. If omitted, all outputs are returned under 'result'."},
			{Key: "timeout", Label: "Timeout", Type: FieldTypeDuration, DefaultValue: "30s", Description: "Maximum execution time for the called workflow (applies to both sync and async modes)"},
		},
		DefaultConfig: map[string]any{"mode": "sync", "timeout": "30s"},
	})

	// -----------------------------------------------------------------------
	// AI pipeline steps
	// -----------------------------------------------------------------------

	r.Register(&ModuleSchema{
		Type:        "step.ai_complete",
		Label:       "AI Complete",
		Category:    "ai",
		Description: "Sends a prompt to an AI provider and returns the completion text",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context with input text"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "Completion text and token usage metadata"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "provider", Label: "Provider", Type: FieldTypeString, Description: "AI provider name (or 'auto' for registry default)", Placeholder: "anthropic"},
			{Key: "model", Label: "Model", Type: FieldTypeString, Description: "Model identifier", Placeholder: "claude-sonnet-4-20250514"},
			{Key: "system_prompt", Label: "System Prompt", Type: FieldTypeString, Description: "System prompt to guide the AI"},
			{Key: "input_from", Label: "Input From", Type: FieldTypeString, Description: "Template expression for input text (e.g. {{.steps.parse.body.text}})"},
			{Key: "max_tokens", Label: "Max Tokens", Type: FieldTypeNumber, DefaultValue: "1024", Description: "Maximum output tokens"},
			{Key: "temperature", Label: "Temperature", Type: FieldTypeNumber, Description: "Sampling temperature (0.0 - 1.0)"},
		},
		DefaultConfig: map[string]any{"max_tokens": 1024, "temperature": 0.7},
	})

	r.Register(&ModuleSchema{
		Type:        "step.ai_classify",
		Label:       "AI Classify",
		Category:    "ai",
		Description: "Classifies input text into one of the provided categories using an AI provider",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context with input text"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "Classification result with category and confidence"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "provider", Label: "Provider", Type: FieldTypeString, Description: "AI provider name"},
			{Key: "model", Label: "Model", Type: FieldTypeString, Description: "Model identifier"},
			{Key: "categories", Label: "Categories", Type: FieldTypeArray, ArrayItemType: "string", Required: true, Description: "List of classification categories"},
			{Key: "input_from", Label: "Input From", Type: FieldTypeString, Description: "Template expression for input text"},
			{Key: "max_tokens", Label: "Max Tokens", Type: FieldTypeNumber, DefaultValue: "256", Description: "Maximum output tokens"},
			{Key: "temperature", Label: "Temperature", Type: FieldTypeNumber, Description: "Sampling temperature"},
		},
		DefaultConfig: map[string]any{"max_tokens": 256, "temperature": 0.3},
	})

	r.Register(&ModuleSchema{
		Type:        "step.ai_extract",
		Label:       "AI Extract",
		Category:    "ai",
		Description: "Extracts structured data from input text using an AI provider with a defined schema",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context with input text"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "Extracted structured data matching the schema"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "provider", Label: "Provider", Type: FieldTypeString, Description: "AI provider name"},
			{Key: "model", Label: "Model", Type: FieldTypeString, Description: "Model identifier"},
			{Key: "schema", Label: "Extraction Schema", Type: FieldTypeMap, Required: true, Description: "JSON schema defining the fields to extract"},
			{Key: "input_from", Label: "Input From", Type: FieldTypeString, Description: "Template expression for input text"},
			{Key: "max_tokens", Label: "Max Tokens", Type: FieldTypeNumber, DefaultValue: "1024", Description: "Maximum output tokens"},
			{Key: "temperature", Label: "Temperature", Type: FieldTypeNumber, Description: "Sampling temperature"},
		},
		DefaultConfig: map[string]any{"max_tokens": 1024, "temperature": 0.3},
	})

	// ---- Feature Flags ----

	r.Register(&ModuleSchema{
		Type:        "featureflag.service",
		Label:       "Feature Flag Service",
		Category:    "infrastructure",
		Description: "Feature flag management service with targeting rules, overrides, and real-time SSE updates",
		Outputs:     []ServiceIODef{{Name: "featureflag.Service", Type: "featureflag.Service", Description: "Feature flag evaluation service"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "provider", Label: "Provider", Type: FieldTypeSelect, Options: []string{"generic", "launchdarkly"}, DefaultValue: "generic", Description: "Feature flag backend provider"},
			{Key: "cache_ttl", Label: "Cache TTL", Type: FieldTypeDuration, DefaultValue: "1m", Description: "Duration to cache flag evaluations", Placeholder: "1m"},
			{Key: "sse_enabled", Label: "SSE Enabled", Type: FieldTypeBool, DefaultValue: true, Description: "Enable Server-Sent Events for real-time flag change notifications"},
			{Key: "store_path", Label: "Store Path", Type: FieldTypeString, Description: "Path for the flag definition store (file-based provider)", Placeholder: "data/flags.json"},
			{Key: "launchdarkly_sdk_key", Label: "LaunchDarkly SDK Key", Type: FieldTypeString, Sensitive: true, Description: "LaunchDarkly server-side SDK key (required when provider is launchdarkly)", Group: "LaunchDarkly"},
		},
		DefaultConfig: map[string]any{"provider": "generic", "cache_ttl": "1m", "sse_enabled": true},
		MaxIncoming:   intPtr(0),
	})

	// ---- Event Store ----

	r.Register(&ModuleSchema{
		Type:        "eventstore.service",
		Label:       "Event Store Service",
		Category:    "infrastructure",
		Description: "SQLite-backed event store for execution event persistence, timeline, and replay features",
		Outputs:     []ServiceIODef{{Name: "EventStore", Type: "store.SQLiteEventStore", Description: "Execution event store"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "db_path", Label: "Database Path", Type: FieldTypeString, DefaultValue: "data/events.db", Description: "Path to the SQLite database file for event storage", Placeholder: "data/events.db"},
			{Key: "retention_days", Label: "Retention Days", Type: FieldTypeNumber, DefaultValue: 90, Description: "Number of days to retain execution events"},
		},
		DefaultConfig: map[string]any{"db_path": "data/events.db", "retention_days": 90},
		MaxIncoming:   intPtr(0),
	})

	// ---- Timeline / Replay ----

	r.Register(&ModuleSchema{
		Type:        "timeline.service",
		Label:       "Timeline & Replay Service",
		Category:    "infrastructure",
		Description: "Provides execution timeline visualization and request replay HTTP endpoints",
		Inputs:      []ServiceIODef{{Name: "EventStore", Type: "store.EventStore", Description: "Event store dependency for timeline and replay data"}},
		Outputs: []ServiceIODef{
			{Name: "TimelineMux", Type: "http.Handler", Description: "HTTP handler for timeline endpoints"},
			{Name: "ReplayMux", Type: "http.Handler", Description: "HTTP handler for replay endpoints"},
			{Name: "BackfillMux", Type: "http.Handler", Description: "HTTP handler for backfill/mock/diff endpoints"},
		},
		ConfigFields: []ConfigFieldDef{
			{Key: "event_store", Label: "Event Store Service", Type: FieldTypeString, DefaultValue: "admin-event-store", Description: "Name of the event store service module to use", Placeholder: "admin-event-store"},
		},
		DefaultConfig: map[string]any{"event_store": "admin-event-store"},
		MaxIncoming:   intPtr(1),
	})

	// ---- DLQ (Dead Letter Queue) ----

	r.Register(&ModuleSchema{
		Type:        "dlq.service",
		Label:       "Dead Letter Queue Service",
		Category:    "infrastructure",
		Description: "In-memory dead letter queue for failed message management with retry, discard, and purge",
		Outputs:     []ServiceIODef{{Name: "DLQHandler", Type: "http.Handler", Description: "HTTP handler for DLQ management endpoints"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "max_retries", Label: "Max Retries", Type: FieldTypeNumber, DefaultValue: 3, Description: "Maximum number of retry attempts for failed messages"},
			{Key: "retention_days", Label: "Retention Days", Type: FieldTypeNumber, DefaultValue: 30, Description: "Number of days to retain resolved/discarded DLQ entries"},
		},
		DefaultConfig: map[string]any{"max_retries": 3, "retention_days": 30},
		MaxIncoming:   intPtr(0),
	})

	r.Register(&ModuleSchema{
		Type:        "step.feature_flag",
		Label:       "Feature Flag Check",
		Category:    "steps",
		Description: "Evaluates a feature flag and stores the result in the pipeline context",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context with user/group info"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "Flag evaluation result"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "flag", Label: "Flag Key", Type: FieldTypeString, Required: true, Description: "Feature flag key to evaluate", Placeholder: "feature.my-flag"},
			{Key: "user_from", Label: "User From", Type: FieldTypeString, Description: "Template expression to extract user identifier from context", Placeholder: "{{.request.user_id}}"},
			{Key: "group_from", Label: "Group From", Type: FieldTypeString, Description: "Template expression to extract group identifier from context", Placeholder: "{{.request.group}}"},
			{Key: "output_key", Label: "Output Key", Type: FieldTypeString, DefaultValue: "flag_value", Description: "Key to store the flag value in pipeline context", Placeholder: "flag_value"},
		},
		DefaultConfig: map[string]any{"output_key": "flag_value"},
	})

	r.Register(&ModuleSchema{
		Type:        "step.ff_gate",
		Label:       "Feature Flag Gate",
		Category:    "steps",
		Description: "Gates pipeline execution based on a feature flag evaluation — routes to different branches when enabled vs disabled",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context with user/group info"}},
		Outputs: []ServiceIODef{
			{Name: "enabled", Type: "PipelineContext", Description: "Output when flag is enabled"},
			{Name: "disabled", Type: "PipelineContext", Description: "Output when flag is disabled"},
		},
		ConfigFields: []ConfigFieldDef{
			{Key: "flag", Label: "Flag Key", Type: FieldTypeString, Required: true, Description: "Feature flag key to evaluate", Placeholder: "feature.my-flag"},
			{Key: "on_enabled", Label: "On Enabled", Type: FieldTypeString, Description: "Branch or step to execute when flag is enabled"},
			{Key: "on_disabled", Label: "On Disabled", Type: FieldTypeString, Description: "Branch or step to execute when flag is disabled"},
			{Key: "user_from", Label: "User From", Type: FieldTypeString, Description: "Template expression to extract user identifier from context", Placeholder: "{{.request.user_id}}"},
			{Key: "group_from", Label: "Group From", Type: FieldTypeString, Description: "Template expression to extract group identifier from context", Placeholder: "{{.request.group}}"},
		},
	})

	// ---- Platform Category ----

	r.Register(&ModuleSchema{
		Type:        "platform.provider",
		Label:       "Platform Provider",
		Category:    "platform",
		Description: "Infrastructure provider for the platform abstraction layer (e.g., AWS, Docker Compose, GCP)",
		Outputs:     []ServiceIODef{{Name: "provider", Type: "platform.Provider", Description: "Infrastructure provider instance"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "name", Label: "Provider Name", Type: FieldTypeString, Required: true, Description: "Provider identifier (e.g., aws, docker-compose, gcp)", Placeholder: "aws"},
			{Key: "config", Label: "Provider Config", Type: FieldTypeMap, MapValueType: "string", Description: "Provider-specific configuration (credentials, region, etc.)"},
			{Key: "tiers", Label: "Tier Configuration", Type: FieldTypeMap, Description: "Three-tier infrastructure layout (infrastructure, shared_primitives, application)"},
		},
		MaxIncoming: intPtr(0),
	})

	r.Register(&ModuleSchema{
		Type:        "platform.resource",
		Label:       "Platform Resource",
		Category:    "platform",
		Description: "A capability-based resource declaration managed by the platform abstraction layer",
		Inputs:      []ServiceIODef{{Name: "provider", Type: "platform.Provider", Description: "The infrastructure provider managing this resource"}},
		Outputs:     []ServiceIODef{{Name: "output", Type: "platform.ResourceOutput", Description: "Provisioned resource outputs (endpoint, credentials, properties)"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "name", Label: "Resource Name", Type: FieldTypeString, Required: true, Description: "Unique identifier for this resource within its tier", Placeholder: "web-cluster"},
			{Key: "type", Label: "Capability Type", Type: FieldTypeString, Required: true, Description: "Abstract capability type (e.g., container_runtime, database, message_queue)", Placeholder: "container_runtime"},
			{Key: "tier", Label: "Infrastructure Tier", Type: FieldTypeSelect, Options: []string{"infrastructure", "shared_primitive", "application"}, DefaultValue: "application", Description: "Which infrastructure tier this resource belongs to"},
			{Key: "capabilities", Label: "Capabilities", Type: FieldTypeMap, Description: "Provider-agnostic capability properties (replicas, memory, ports, etc.)"},
			{Key: "constraints", Label: "Constraints", Type: FieldTypeMap, Description: "Hard limits imposed by parent tiers"},
		},
		DefaultConfig: map[string]any{"tier": "application"},
	})

	r.Register(&ModuleSchema{
		Type:        "platform.context",
		Label:       "Platform Context",
		Category:    "platform",
		Description: "Hierarchical context for platform operations carrying org, environment, and tier information",
		Outputs:     []ServiceIODef{{Name: "context", Type: "platform.PlatformContext", Description: "Resolved platform context with parent tier outputs and constraints"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "org", Label: "Organization", Type: FieldTypeString, Required: true, Description: "Organization identifier", Placeholder: "acme-corp"},
			{Key: "environment", Label: "Environment", Type: FieldTypeString, Required: true, Description: "Deployment environment (e.g., production, staging, dev)", Placeholder: "production"},
			{Key: "tier", Label: "Tier", Type: FieldTypeSelect, Options: []string{"infrastructure", "shared_primitive", "application"}, DefaultValue: "application", Description: "Infrastructure tier for this context"},
		},
		DefaultConfig: map[string]any{"tier": "application"},
		MaxIncoming:   intPtr(0),
	})

	// ---- Platform Pipeline Steps ----

	r.Register(&ModuleSchema{
		Type:        "step.platform_plan",
		Label:       "Platform Plan",
		Category:    "pipeline_steps",
		Description: "Generates an execution plan by mapping capability declarations through a platform provider",
		ConfigFields: []ConfigFieldDef{
			{Key: "provider_service", Label: "Provider Service", Type: FieldTypeString, Description: "Service name to look up the platform provider"},
			{Key: "resources_from", Label: "Resources From", Type: FieldTypeString, Description: "Key in pipeline context containing resource declarations", DefaultValue: "resource_declarations"},
			{Key: "tier", Label: "Tier", Type: FieldTypeSelect, Options: []string{"1", "2", "3"}, DefaultValue: "3", Description: "Infrastructure tier for this plan"},
			{Key: "dry_run", Label: "Dry Run", Type: FieldTypeBool, DefaultValue: "false", Description: "If true, plan only without preparing for apply"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.platform_apply",
		Label:       "Platform Apply",
		Category:    "pipeline_steps",
		Description: "Applies a previously generated platform plan to provision or update resources",
		ConfigFields: []ConfigFieldDef{
			{Key: "provider_service", Label: "Provider Service", Type: FieldTypeString, Description: "Service name to look up the platform provider"},
			{Key: "plan_from", Label: "Plan From", Type: FieldTypeString, Description: "Key in pipeline context containing the plan to apply", DefaultValue: "platform_plan"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.platform_destroy",
		Label:       "Platform Destroy",
		Category:    "pipeline_steps",
		Description: "Destroys platform resources in reverse dependency order",
		ConfigFields: []ConfigFieldDef{
			{Key: "provider_service", Label: "Provider Service", Type: FieldTypeString, Description: "Service name to look up the platform provider"},
			{Key: "resources_from", Label: "Resources From", Type: FieldTypeString, Description: "Key in pipeline context containing resources to destroy", DefaultValue: "applied_resources"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.drift_check",
		Label:       "Drift Check",
		Category:    "pipeline_steps",
		Description: "Compares desired vs actual resource state to detect configuration drift",
		ConfigFields: []ConfigFieldDef{
			{Key: "provider_service", Label: "Provider Service", Type: FieldTypeString, Description: "Service name to look up the platform provider"},
			{Key: "resources_from", Label: "Resources From", Type: FieldTypeString, Description: "Key in pipeline context containing resources to check", DefaultValue: "applied_resources"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.constraint_check",
		Label:       "Constraint Check",
		Category:    "pipeline_steps",
		Description: "Validates resource specs against tier constraints before provisioning",
		ConfigFields: []ConfigFieldDef{
			{Key: "constraints", Label: "Constraints", Type: FieldTypeArray, Description: "List of constraint definitions (field, operator, value)"},
			{Key: "resources_from", Label: "Resources From", Type: FieldTypeString, Description: "Key in pipeline context containing resources to validate", DefaultValue: "resource_declarations"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.platform_template",
		Label:       "Platform Template",
		Category:    "pipeline_steps",
		Description: "Resolves a platform template with parameters to produce resource declarations",
		ConfigFields: []ConfigFieldDef{
			{Key: "template_name", Label: "Template Name", Type: FieldTypeString, Required: true, Description: "Name of the template to resolve"},
			{Key: "template_version", Label: "Template Version", Type: FieldTypeString, Description: "Specific version to use (empty for latest)"},
			{Key: "parameters", Label: "Parameters", Type: FieldTypeMap, MapValueType: "string", Description: "Template parameter values"},
		},
	})

	// ---- Additional Pipeline Steps ----

	r.Register(&ModuleSchema{
		Type:        "step.foreach",
		Label:       "For Each",
		Category:    "pipeline_steps",
		Description: "Iterates over a collection and executes a sub-pipeline step for each item",
		ConfigFields: []ConfigFieldDef{
			{Key: "collection", Label: "Collection", Type: FieldTypeString, Required: true, Description: "Dotted path to the collection to iterate over", Placeholder: "steps.fetch.items"},
			{Key: "item_var", Label: "Item Variable", Type: FieldTypeString, Description: "Context variable name for the current item (defaults to 'item')", DefaultValue: "item"},
			{Key: "item_key", Label: "Item Key (legacy)", Type: FieldTypeString, Description: "Legacy alias for item_var"},
			{Key: "index_key", Label: "Index Key", Type: FieldTypeString, Description: "Context variable name for the current index (defaults to 'index')", DefaultValue: "index"},
			{Key: "step", Label: "Step", Type: FieldTypeMap, Description: "Single step map to execute for each item (mutually exclusive with steps); must include 'type' key"},
			{Key: "steps", Label: "Steps", Type: FieldTypeArray, Description: "Array of step maps to execute for each item (mutually exclusive with step)"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.parallel",
		Label:       "Parallel",
		Category:    "pipeline_steps",
		Description: "Execute multiple named sub-steps concurrently and collect results",
		ConfigFields: []ConfigFieldDef{
			{Key: "steps", Label: "Steps", Type: FieldTypeArray, Required: true, Description: "List of sub-steps to run concurrently. Each must have a unique 'name'."},
			{Key: "error_strategy", Label: "Error Strategy", Type: FieldTypeSelect, Description: "fail_fast: cancel on first error. collect_errors: run all, collect partial results.", Options: []string{"fail_fast", "collect_errors"}, DefaultValue: "fail_fast"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.webhook_verify",
		Label:       "Webhook Verify",
		Category:    "pipeline_steps",
		Description: "Verifies incoming webhook request signatures (supports HMAC-SHA1, HMAC-SHA256)",
		ConfigFields: []ConfigFieldDef{
			{Key: "scheme", Label: "Scheme", Type: FieldTypeSelect, Options: []string{"hmac-sha1", "hmac-sha256", "hmac-sha256-hex"}, Description: "HMAC signature scheme to use (preferred over provider)"},
			{Key: "provider", Label: "Provider", Type: FieldTypeSelect, Options: []string{"github", "stripe", "generic"}, Description: "Webhook provider (legacy; prefer scheme)"},
			{Key: "secret", Label: "Secret", Type: FieldTypeString, Sensitive: true, Description: "Webhook signing secret"},
			{Key: "secret_from", Label: "Secret From", Type: FieldTypeString, Description: "Context key containing the secret at runtime (scheme mode only)"},
			{Key: "signature_header", Label: "Signature Header", Type: FieldTypeString, Description: "HTTP header containing the signature (scheme mode only)", Placeholder: "X-Hub-Signature-256"},
			{Key: "header", Label: "Signature Header (legacy)", Type: FieldTypeString, Description: "HTTP header containing the signature (provider/legacy mode)", Placeholder: "X-Hub-Signature-256"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.cache_get",
		Label:       "Cache Get",
		Category:    "pipeline_steps",
		Description: "Retrieves a value from the cache by key",
		ConfigFields: []ConfigFieldDef{
			{Key: "key", Label: "Key", Type: FieldTypeString, Required: true, Description: "Cache key (supports template expressions)", Placeholder: "user:{{.user_id}}"},
			{Key: "cache", Label: "Cache Module", Type: FieldTypeString, Required: true, Description: "Name of the cache module to use"},
			{Key: "output", Label: "Output Key", Type: FieldTypeString, Description: "Context key to store the retrieved value", DefaultValue: "value"},
			{Key: "miss_ok", Label: "Allow Cache Miss", Type: FieldTypeBool, Description: "If true, do not fail when the cache key is missing (default: true)"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.cache_set",
		Label:       "Cache Set",
		Category:    "pipeline_steps",
		Description: "Stores a value in the cache with optional TTL",
		ConfigFields: []ConfigFieldDef{
			{Key: "key", Label: "Key", Type: FieldTypeString, Required: true, Description: "Cache key (supports template expressions)", Placeholder: "user:{{.user_id}}"},
			{Key: "value", Label: "Value", Type: FieldTypeString, Required: true, Description: "Value to cache (supports template expressions, e.g. {{.field}})"},
			{Key: "cache", Label: "Cache Module", Type: FieldTypeString, Required: true, Description: "Name of the cache module to use"},
			{Key: "ttl", Label: "TTL", Type: FieldTypeDuration, Description: "Cache entry time-to-live", Placeholder: "5m"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.cache_delete",
		Label:       "Cache Delete",
		Category:    "pipeline_steps",
		Description: "Removes a value from the cache by key",
		ConfigFields: []ConfigFieldDef{
			{Key: "key", Label: "Key", Type: FieldTypeString, Required: true, Description: "Cache key to delete (supports template expressions)", Placeholder: "user:{{.user_id}}"},
			{Key: "cache", Label: "Cache Module", Type: FieldTypeString, Required: true, Description: "Name of the cache module to use"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.event_publish",
		Label:       "Event Publish",
		Category:    "pipeline_steps",
		Description: "Publishes a structured event in CloudEvents format to a messaging broker, EventPublisher, or event bus",
		ConfigFields: []ConfigFieldDef{
			{Key: "topic", Label: "Topic", Type: FieldTypeString, Description: "Topic or channel to publish the event to (also accepts 'stream' alias)", Placeholder: "user-events"},
			{Key: "stream", Label: "Stream", Type: FieldTypeString, Description: "Alias for 'topic' — name of the stream to publish to (e.g., Kinesis stream name)", Placeholder: "messaging.texter-messages"},
			{Key: "payload", Label: "Payload", Type: FieldTypeMap, Description: "Event payload (supports template expressions); defaults to current pipeline context"},
			{Key: "data", Label: "Data", Type: FieldTypeMap, Description: "Alias for 'payload' — event data fields (supports template expressions)"},
			{Key: "headers", Label: "Headers", Type: FieldTypeMap, Description: "Additional headers/metadata to include with the event"},
			{Key: "event_type", Label: "Event Type", Type: FieldTypeString, Description: "CloudEvents type identifier (e.g., messaging.texter-message.received)", Placeholder: "user.created"},
			{Key: "source", Label: "Source", Type: FieldTypeString, Description: "CloudEvents source URI identifying the event producer (supports template expressions)", Placeholder: "/chimera/messaging"},
			{Key: "broker", Label: "Broker", Type: FieldTypeString, Description: "Name of the messaging broker module to use (falls back to eventbus if not set)"},
			{Key: "provider", Label: "Provider", Type: FieldTypeString, Description: "Alias for 'broker' — name of the EventPublisher or MessageBroker service (e.g., kinesis, bento-output)"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.validate_path_param",
		Label:       "Validate Path Param",
		Category:    "pipeline_steps",
		Description: "Validates URL path parameters are present and optionally conform to a format (e.g. UUID)",
		ConfigFields: []ConfigFieldDef{
			{Key: "params", Label: "Parameter Names", Type: FieldTypeArray, Required: true, ArrayItemType: "string", Description: "List of path parameter names to validate", Placeholder: "id"},
			{Key: "format", Label: "Format", Type: FieldTypeString, Description: "Validation format to apply to each parameter (e.g. 'uuid')"},
			{Key: "source", Label: "Source Path", Type: FieldTypeString, Description: "Dotted path within the context to read path parameters from (e.g. 'steps.parse-request.path_params')"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.validate_pagination",
		Label:       "Validate Pagination",
		Category:    "pipeline_steps",
		Description: "Validates and normalizes pagination query parameters (page, limit, offset)",
		ConfigFields: []ConfigFieldDef{
			{Key: "default_page", Label: "Default Page", Type: FieldTypeNumber, DefaultValue: 1, Description: "Default page number when none is provided"},
			{Key: "default_limit", Label: "Default Limit", Type: FieldTypeNumber, DefaultValue: 20, Description: "Default number of items to return when no limit is provided"},
			{Key: "max_limit", Label: "Max Limit", Type: FieldTypeNumber, DefaultValue: 100, Description: "Maximum allowed number of items to return per request"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.validate_request_body",
		Label:       "Validate Request Body",
		Category:    "pipeline_steps",
		Description: "Parses the HTTP request body and validates required fields are present",
		ConfigFields: []ConfigFieldDef{
			{Key: "required_fields", Label: "Required Fields", Type: FieldTypeArray, ArrayItemType: "string", Description: "List of required top-level field names in the request body"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.dlq_send",
		Label:       "DLQ Send",
		Category:    "pipeline_steps",
		Description: "Sends a failed message to the dead letter topic for later replay",
		ConfigFields: []ConfigFieldDef{
			{Key: "topic", Label: "DLQ Topic", Type: FieldTypeString, Required: true, Description: "Dead letter topic to publish failed messages to"},
			{Key: "original_topic", Label: "Original Topic", Type: FieldTypeString, Description: "Optional name of the original topic the message came from"},
			{Key: "error", Label: "Error", Type: FieldTypeString, Description: "Optional error message or template expression containing the failure reason"},
			{Key: "payload", Label: "Payload", Type: FieldTypeMap, Description: "Message payload to send to the DLQ (defaults to current pipeline context)"},
			{Key: "broker", Label: "Broker", Type: FieldTypeString, Description: "Optional name of the messaging broker module to use (falls back to eventbus if not set)"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.dlq_replay",
		Label:       "DLQ Replay",
		Category:    "pipeline_steps",
		Description: "Replays messages from a dead letter topic back to the original target topic",
		ConfigFields: []ConfigFieldDef{
			{Key: "dlq_topic", Label: "DLQ Topic", Type: FieldTypeString, Required: true, Description: "Dead letter topic name to replay messages from"},
			{Key: "target_topic", Label: "Target Topic", Type: FieldTypeString, Required: true, Description: "Target topic to publish replayed messages to"},
			{Key: "max_messages", Label: "Max Messages", Type: FieldTypeNumber, DefaultValue: 100, Description: "Maximum number of messages to replay"},
			{Key: "broker", Label: "Broker", Type: FieldTypeString, Description: "Name of the messaging broker module to use for replay (falls back to eventbus if not set)"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.retry_with_backoff",
		Label:       "Retry With Backoff",
		Category:    "pipeline_steps",
		Description: "Wraps a sub-step with automatic retry logic using exponential backoff",
		ConfigFields: []ConfigFieldDef{
			{Key: "step", Label: "Step", Type: FieldTypeMap, Required: true, Description: "Sub-step map to retry; must include 'type' key with inline step configuration"},
			{Key: "max_retries", Label: "Max Retries", Type: FieldTypeNumber, DefaultValue: 3, Description: "Maximum number of retry attempts"},
			{Key: "initial_delay", Label: "Initial Delay", Type: FieldTypeDuration, DefaultValue: "1s", Description: "Initial delay before first retry"},
			{Key: "max_delay", Label: "Max Delay", Type: FieldTypeDuration, DefaultValue: "30s", Description: "Maximum delay between retries"},
			{Key: "multiplier", Label: "Backoff Multiplier", Type: FieldTypeNumber, DefaultValue: 2.0, Description: "Multiplier applied to the delay for each retry (exponential backoff factor)"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.resilient_circuit_breaker",
		Label:       "Resilient Circuit Breaker",
		Category:    "pipeline_steps",
		Description: "Wraps a sub-step with circuit breaker pattern to prevent cascading failures",
		ConfigFields: []ConfigFieldDef{
			{Key: "step", Label: "Step", Type: FieldTypeMap, Required: true, Description: "Sub-step map to protect; must include 'type' key with inline step configuration"},
			{Key: "failure_threshold", Label: "Failure Threshold", Type: FieldTypeNumber, DefaultValue: 5, Description: "Number of consecutive failures to open the circuit"},
			{Key: "reset_timeout", Label: "Reset Timeout", Type: FieldTypeDuration, DefaultValue: "60s", Description: "Duration the circuit remains open before attempting a half-open state"},
			{Key: "fallback", Label: "Fallback Step", Type: FieldTypeMap, Description: "Optional fallback step map executed when the circuit is open; must include 'type' key"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.ui_scaffold",
		Label:       "UI Scaffold",
		Category:    "pipeline_steps",
		Description: "Generates a Vite+React+TypeScript UI scaffold from an OpenAPI spec (read from the request body or context) and returns a ZIP archive",
		ConfigFields: []ConfigFieldDef{
			{Key: "title", Label: "Title", Type: FieldTypeString, Description: "Title to use for the generated UI"},
			{Key: "theme", Label: "Theme", Type: FieldTypeString, Description: "UI theme or design system to target"},
			{Key: "auth", Label: "Auth", Type: FieldTypeBool, Description: "Whether to generate authentication UI components"},
			{Key: "filename", Label: "Filename", Type: FieldTypeString, DefaultValue: "scaffold.zip", Description: "Filename for the generated ZIP archive"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.ui_scaffold_analyze",
		Label:       "UI Scaffold Analyze",
		Category:    "pipeline_steps",
		Description: "Analyzes an OpenAPI spec (read from the request body or context) to produce scaffold analysis metadata",
		ConfigFields: []ConfigFieldDef{
			{Key: "title", Label: "Title", Type: FieldTypeString, Description: "Title to use for the generated scaffold analysis"},
			{Key: "theme", Label: "Theme", Type: FieldTypeString, Description: "Visual theme or design system to target when generating scaffold analysis"},
		},
	})

	// ---- License ----

	r.Register(&ModuleSchema{
		Type:        "license.validator",
		Label:       "License Validator",
		Category:    "infrastructure",
		Description: "Validates license keys against a remote server with local caching and offline grace period",
		Outputs: []ServiceIODef{
			{Name: "license-validator", Type: "LicenseValidator", Description: "License validation service for feature gating"},
		},
		ConfigFields: []ConfigFieldDef{
			{Key: "server_url", Label: "License Server URL", Type: FieldTypeString, Description: "URL of the license validation server (leave empty for offline/starter mode)", Placeholder: "https://license.gocodalone.com/api/v1"},
			{Key: "license_key", Label: "License Key", Type: FieldTypeString, Description: "License key (supports $ENV_VAR expansion; also reads WORKFLOW_LICENSE_KEY env var)", Placeholder: "$WORKFLOW_LICENSE_KEY", Sensitive: true},
			{Key: "cache_ttl", Label: "Cache TTL", Type: FieldTypeDuration, DefaultValue: "1h", Description: "How long to cache a valid license result before re-validating", Placeholder: "1h"},
			{Key: "grace_period", Label: "Grace Period", Type: FieldTypeDuration, DefaultValue: "72h", Description: "How long to allow operation when the license server is unreachable", Placeholder: "72h"},
			{Key: "refresh_interval", Label: "Refresh Interval", Type: FieldTypeDuration, DefaultValue: "1h", Description: "How often the background goroutine re-validates the license", Placeholder: "1h"},
		},
		DefaultConfig: map[string]any{"cache_ttl": "1h", "grace_period": "72h", "refresh_interval": "1h"},
		MaxIncoming:   intPtr(0),
	})

	// ---- Hash ----

	r.Register(&ModuleSchema{
		Type:        "step.hash",
		Label:       "Hash",
		Category:    "pipeline",
		Description: "Computes a cryptographic hash (md5, sha256, sha512) of a template-resolved input string",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context for template resolution"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "Hash digest and algorithm name"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "algorithm", Label: "Algorithm", Type: FieldTypeString, DefaultValue: "sha256", Description: "Hash algorithm: md5, sha256, or sha512"},
			{Key: "input", Label: "Input", Type: FieldTypeString, Required: true, Description: "Input string to hash (template expressions supported)"},
		},
		DefaultConfig: map[string]any{"algorithm": "sha256"},
	})

	// ---- Regex Match ----

	r.Register(&ModuleSchema{
		Type:        "step.regex_match",
		Label:       "Regex Match",
		Category:    "pipeline",
		Description: "Matches a regular expression against a template-resolved input string and returns match results",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context for template resolution"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "Match result with matched (bool), match (string), and groups ([]string)"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "pattern", Label: "Pattern", Type: FieldTypeString, Required: true, Description: "Regular expression pattern (compiled at config time)"},
			{Key: "input", Label: "Input", Type: FieldTypeString, Required: true, Description: "Input string to match against (template expressions supported)"},
		},
	})

	// ---- Static File ----

	r.Register(&ModuleSchema{
		Type:        "step.static_file",
		Label:       "Static File",
		Category:    "pipeline",
		Description: "Serves a static file from disk as an HTTP response; file is read at init time for performance",
		Inputs:      []ServiceIODef{{Name: "context", Type: "PipelineContext", Description: "Pipeline context (HTTP response writer)"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "StepResult", Description: "Writes an HTTP response with file content and stops the pipeline; if no HTTP writer is available, returns the file content in StepResult.Output as body"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "file", Label: "File Path", Type: FieldTypeString, Required: true, Description: "Path to the file to serve; resolved relative to the config file directory"},
			{Key: "content_type", Label: "Content-Type", Type: FieldTypeString, Required: true, Description: "MIME type of the file (e.g. application/yaml, text/html)"},
			{Key: "cache_control", Label: "Cache-Control", Type: FieldTypeString, Description: "Optional Cache-Control header value (e.g. public, max-age=3600)"},
		},
	})

	// ---- Auth Required ----

	r.Register(&ModuleSchema{
		Type:        "step.auth_required",
		Label:       "Auth Required",
		Category:    "pipeline",
		Description: "Validates JWT or API key authentication; returns 401 if not authenticated",
		ConfigFields: []ConfigFieldDef{
			{Key: "roles", Label: "Required Roles", Type: FieldTypeArray, Description: "Required roles (any match grants access)"},
			{Key: "scopes", Label: "Required Scopes", Type: FieldTypeArray, Description: "Required OAuth2 scopes"},
		},
	})

	// ---- Auth Validate ----

	r.Register(&ModuleSchema{
		Type:        "step.auth_validate",
		Label:       "Auth Validate",
		Category:    "pipeline",
		Description: "Validates a Bearer token against a registered AuthProvider module and outputs claims",
		ConfigFields: []ConfigFieldDef{
			{Key: "auth_module", Label: "Auth Module", Type: FieldTypeString, Required: true, Description: "Service name of AuthProvider module"},
			{Key: "token_source", Label: "Token Source", Type: FieldTypeString, Required: true, Description: "Dot-path to Bearer token in pipeline context"},
			{Key: "subject_field", Label: "Subject Field", Type: FieldTypeString, Description: "Output field name for 'sub' claim"},
		},
	})

	// ---- Authz Check ----

	r.Register(&ModuleSchema{
		Type:        "step.authz_check",
		Label:       "Authz Check",
		Category:    "pipeline",
		Description: "Checks authorization using the configured authz module; returns 403 if denied",
		ConfigFields: []ConfigFieldDef{
			{Key: "module", Label: "Module", Type: FieldTypeString, Required: true, Description: "Authorization module name"},
			{Key: "subject", Label: "Subject", Type: FieldTypeString, Required: true, Description: "Subject (template expression)"},
			{Key: "object", Label: "Object", Type: FieldTypeString, Required: true, Description: "Object (template expression)"},
			{Key: "action", Label: "Action", Type: FieldTypeString, Required: true, Description: "Action (template expression)"},
		},
	})

	// ---- CLI Invoke ----

	r.Register(&ModuleSchema{
		Type:        "step.cli_invoke",
		Label:       "CLI Invoke",
		Category:    "pipeline",
		Description: "Calls a registered Go CLI command function from the CLICommandRegistry",
		ConfigFields: []ConfigFieldDef{
			{Key: "command", Label: "Command", Type: FieldTypeString, Required: true, Description: "Registered command name"},
		},
	})

	// ---- CLI Print ----

	r.Register(&ModuleSchema{
		Type:        "step.cli_print",
		Label:       "CLI Print",
		Category:    "pipeline",
		Description: "Writes a template-resolved message to stdout or stderr",
		ConfigFields: []ConfigFieldDef{
			{Key: "message", Label: "Message", Type: FieldTypeString, Required: true, Description: "Message template"},
			{Key: "newline", Label: "Newline", Type: FieldTypeBool, Description: "Append trailing newline"},
			{Key: "target", Label: "Target", Type: FieldTypeSelect, Options: []string{"stdout", "stderr"}, Description: "Output destination"},
		},
	})

	// ---- Event Decrypt ----

	r.Register(&ModuleSchema{
		Type:        "step.event_decrypt",
		Label:       "Event Decrypt",
		Category:    "pipeline",
		Description: "Decrypts field-level encryption applied by step.event_publish using CloudEvents extension attributes",
		ConfigFields: []ConfigFieldDef{
			{Key: "key_id", Label: "Key ID", Type: FieldTypeString, Description: "Key ID override"},
		},
	})

	// ---- Field Re-encrypt ----

	r.Register(&ModuleSchema{
		Type:        "step.field_reencrypt",
		Label:       "Field Re-encrypt",
		Category:    "pipeline",
		Description: "Re-encrypts pipeline context data with the latest key version using a ProtectedFieldManager",
		ConfigFields: []ConfigFieldDef{
			{Key: "module", Label: "Module", Type: FieldTypeString, Required: true, Description: "Service name of ProtectedFieldManager"},
			{Key: "tenant_id", Label: "Tenant ID", Type: FieldTypeString, Description: "Template expression for tenant ID"},
		},
	})

	// ---- GraphQL ----

	r.Register(&ModuleSchema{
		Type:        "step.graphql",
		Label:       "GraphQL",
		Category:    "pipeline",
		Description: "Executes GraphQL queries or mutations with support for pagination, batch requests, and OAuth2 authentication",
		ConfigFields: []ConfigFieldDef{
			{Key: "url", Label: "URL", Type: FieldTypeString, Required: true, Description: "GraphQL endpoint URL"},
			{Key: "query", Label: "Query", Type: FieldTypeString, Description: "GraphQL query or mutation"},
			{Key: "variables", Label: "Variables", Type: FieldTypeMap, Description: "Query variables"},
			{Key: "data_path", Label: "Data Path", Type: FieldTypeString, Description: "Dot-path to extract nested data"},
			{Key: "headers", Label: "Headers", Type: FieldTypeMap, Description: "Custom HTTP headers"},
			{Key: "timeout", Label: "Timeout", Type: FieldTypeDuration, Description: "Request timeout"},
			{Key: "auth", Label: "Auth", Type: FieldTypeMap, Description: "Authentication config"},
		},
	})

	// ---- JSON Parse ----

	r.Register(&ModuleSchema{
		Type:        "step.json_parse",
		Label:       "JSON Parse",
		Category:    "pipeline",
		Description: "Parses a JSON string from pipeline context into a structured value",
		ConfigFields: []ConfigFieldDef{
			{Key: "source", Label: "Source", Type: FieldTypeString, Required: true, Description: "Dot-path to JSON string value"},
			{Key: "target", Label: "Target", Type: FieldTypeString, Description: "Output key name for parsed result"},
		},
	})

	// ---- M2M Token ----

	r.Register(&ModuleSchema{
		Type:        "step.m2m_token",
		Label:       "M2M Token",
		Category:    "pipeline",
		Description: "Generates or validates a machine-to-machine (M2M) token with custom claims",
		ConfigFields: []ConfigFieldDef{
			{Key: "action", Label: "Action", Type: FieldTypeSelect, Options: []string{"generate", "validate", "revoke", "introspect"}, Required: true},
			{Key: "claims", Label: "Claims", Type: FieldTypeMap, Description: "Custom claims"},
			{Key: "ttl", Label: "TTL", Type: FieldTypeDuration, Description: "Token time-to-live"},
		},
	})

	// ---- NoSQL Get ----

	r.Register(&ModuleSchema{
		Type:        "step.nosql_get",
		Label:       "NoSQL Get",
		Category:    "pipeline",
		Description: "Retrieves a document from a NoSQL store by key",
		ConfigFields: []ConfigFieldDef{
			{Key: "store", Label: "Store", Type: FieldTypeString, Required: true, Description: "NoSQL store module name"},
			{Key: "key", Label: "Key", Type: FieldTypeString, Required: true, Description: "Document key"},
		},
	})

	// ---- NoSQL Put ----

	r.Register(&ModuleSchema{
		Type:        "step.nosql_put",
		Label:       "NoSQL Put",
		Category:    "pipeline",
		Description: "Stores a document in a NoSQL store by key",
		ConfigFields: []ConfigFieldDef{
			{Key: "store", Label: "Store", Type: FieldTypeString, Required: true, Description: "NoSQL store module name"},
			{Key: "key", Label: "Key", Type: FieldTypeString, Required: true, Description: "Document key"},
			{Key: "item", Label: "Item", Type: FieldTypeMap, Description: "Document to store"},
		},
	})

	// ---- NoSQL Query ----

	r.Register(&ModuleSchema{
		Type:        "step.nosql_query",
		Label:       "NoSQL Query",
		Category:    "pipeline",
		Description: "Queries documents from a NoSQL store by key prefix",
		ConfigFields: []ConfigFieldDef{
			{Key: "store", Label: "Store", Type: FieldTypeString, Required: true, Description: "NoSQL store module name"},
			{Key: "prefix", Label: "Prefix", Type: FieldTypeString, Description: "Key prefix to filter documents"},
		},
	})

	// ---- OIDC Auth URL ----

	r.Register(&ModuleSchema{
		Type:        "step.oidc_auth_url",
		Label:       "OIDC Auth URL",
		Category:    "pipeline",
		Description: "Generates an OIDC/OAuth2 authorization URL and redirects the user",
		ConfigFields: []ConfigFieldDef{
			{Key: "provider", Label: "Provider", Type: FieldTypeString, Required: true, Description: "OIDC provider name"},
			{Key: "redirect_uri", Label: "Redirect URI", Type: FieldTypeString, Required: true, Description: "Redirect URI after authentication"},
			{Key: "scopes", Label: "Scopes", Type: FieldTypeArray, Description: "OAuth2 scopes to request"},
		},
	})

	// ---- OIDC Callback ----

	r.Register(&ModuleSchema{
		Type:        "step.oidc_callback",
		Label:       "OIDC Callback",
		Category:    "pipeline",
		Description: "Handles the OIDC/OAuth2 callback, exchanges the code for tokens, and extracts user info",
		ConfigFields: []ConfigFieldDef{
			{Key: "provider", Label: "Provider", Type: FieldTypeString, Required: true, Description: "OIDC provider name"},
			{Key: "redirect_uri", Label: "Redirect URI", Type: FieldTypeString, Required: true, Description: "Redirect URI (must match auth URL)"},
		},
	})

	// ---- Raw Response ----

	r.Register(&ModuleSchema{
		Type:        "step.raw_response",
		Label:       "Raw Response",
		Category:    "pipeline",
		Description: "Writes a non-JSON HTTP response with custom content type and stops pipeline execution",
		ConfigFields: []ConfigFieldDef{
			{Key: "content_type", Label: "Content-Type", Type: FieldTypeString, Required: true, Description: "Content-Type header"},
			{Key: "status", Label: "Status", Type: FieldTypeNumber, Description: "HTTP status code"},
			{Key: "headers", Label: "Headers", Type: FieldTypeMap, Description: "Custom response headers"},
			{Key: "body", Label: "Body", Type: FieldTypeString, Description: "Response body"},
			{Key: "body_from", Label: "Body From", Type: FieldTypeString, Description: "Dot-path to body value"},
		},
	})

	// ---- Pipeline Output ----

	r.Register(&ModuleSchema{
		Type:        "step.pipeline_output",
		Label:       "Pipeline Output",
		Category:    "pipeline",
		Description: "Marks structured data as the pipeline's return value for extraction by engine.ExecutePipeline() or the HTTP trigger fallback",
		ConfigFields: []ConfigFieldDef{
			{Key: "source", Label: "Source", Type: FieldTypeString, Description: "Dot-path to step output (e.g. steps.fetch or steps.fetch.row)"},
			{Key: "values", Label: "Values", Type: FieldTypeMap, Description: "Template map of key-value pairs to include in output"},
		},
	})

	// ---- Sandbox Exec ----

	r.Register(&ModuleSchema{
		Type:        "step.sandbox_exec",
		Label:       "Sandbox Exec",
		Category:    "pipeline",
		Description: "Runs a command in a hardened Docker sandbox container with resource limits",
		ConfigFields: []ConfigFieldDef{
			{Key: "image", Label: "Image", Type: FieldTypeString, Description: "Container image URI"},
			{Key: "command", Label: "Command", Type: FieldTypeArray, Description: "Command to execute"},
			{Key: "security_profile", Label: "Security Profile", Type: FieldTypeSelect, Options: []string{"strict", "standard", "permissive"}, Description: "Security profile"},
			{Key: "memory_limit", Label: "Memory Limit", Type: FieldTypeString, Description: "Memory limit (e.g. 128m)"},
			{Key: "timeout", Label: "Timeout", Type: FieldTypeDuration, Description: "Execution timeout"},
			{Key: "env", Label: "Environment", Type: FieldTypeMap, Description: "Environment variables"},
		},
	})

	// ---- Secret Fetch ----

	r.Register(&ModuleSchema{
		Type:        "step.secret_fetch",
		Label:       "Secret Fetch",
		Category:    "pipeline",
		Description: "Fetches one or more secrets from a named secrets module",
		ConfigFields: []ConfigFieldDef{
			{Key: "module", Label: "Module", Type: FieldTypeString, Required: true, Description: "Secrets module name"},
			{Key: "secrets", Label: "Secrets", Type: FieldTypeMap, Required: true, Description: "Map of output key to secret ID/ARN"},
		},
	})

	// ---- Secret Set ----

	r.Register(&ModuleSchema{
		Type:        "step.secret_set",
		Label:       "Secret Set",
		Category:    "pipeline",
		Description: "Writes one or more secrets to a named secrets module",
		ConfigFields: []ConfigFieldDef{
			{Key: "module", Label: "Module", Type: FieldTypeString, Required: true, Description: "Secrets module name"},
			{Key: "secrets", Label: "Secrets", Type: FieldTypeMap, Required: true, Description: "Map of secret key to value (supports template expressions)"},
		},
	})

	// ---- State Machine Get ----

	r.Register(&ModuleSchema{
		Type:        "step.statemachine_get",
		Label:       "State Machine Get",
		Category:    "pipeline",
		Description: "Retrieves the current state of a state machine instance",
		ConfigFields: []ConfigFieldDef{
			{Key: "engine", Label: "Engine", Type: FieldTypeString, Required: true, Description: "State machine engine module name"},
			{Key: "instanceId", Label: "Instance ID", Type: FieldTypeString, Required: true, Description: "State machine instance ID"},
		},
	})

	// ---- State Machine Transition ----

	r.Register(&ModuleSchema{
		Type:        "step.statemachine_transition",
		Label:       "State Machine Transition",
		Category:    "pipeline",
		Description: "Triggers a state machine transition for a given instance",
		ConfigFields: []ConfigFieldDef{
			{Key: "engine", Label: "Engine", Type: FieldTypeString, Required: true, Description: "State machine engine module name"},
			{Key: "instanceId", Label: "Instance ID", Type: FieldTypeString, Required: true, Description: "State machine instance ID"},
			{Key: "transition", Label: "Transition", Type: FieldTypeString, Required: true, Description: "Transition name to trigger"},
		},
	})

	// ---- Token Revoke ----

	r.Register(&ModuleSchema{
		Type:        "step.token_revoke",
		Label:       "Token Revoke",
		Category:    "pipeline",
		Description: "Revokes a JWT by extracting its JTI claim and adding it to a token blacklist",
		ConfigFields: []ConfigFieldDef{
			{Key: "blacklist_module", Label: "Blacklist Module", Type: FieldTypeString, Required: true, Description: "TokenBlacklist module name"},
			{Key: "token_source", Label: "Token Source", Type: FieldTypeString, Required: true, Description: "Dot-path to Bearer token"},
		},
	})

	// ---- Actor System ----

	r.Register(&ModuleSchema{
		Type:     "actor.system",
		Label:    "Actor System",
		Category: "infrastructure",
		Description: "Actor runtime for stateful, message-driven services. " +
			"Actors are lightweight, isolated units of computation that communicate through messages.",
		Outputs: []ServiceIODef{{Name: "system", Type: "any", Description: "Actor runtime system for message-driven actor pools"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "shutdownTimeout", Label: "Shutdown Timeout", Type: FieldTypeDuration, Description: "How long to wait for in-flight messages to drain before forcing shutdown", DefaultValue: "30s"},
			{Key: "defaultRecovery", Label: "Default Recovery Policy", Type: FieldTypeMap, Description: "What happens when any actor in this system crashes"},
		},
	})

	// ---- Actor Pool ----

	r.Register(&ModuleSchema{
		Type:        "actor.pool",
		Label:       "Actor Pool",
		Category:    "infrastructure",
		Description: "Defines a group of actors that handle the same type of work with configurable lifecycle and routing.",
		Inputs:      []ServiceIODef{{Name: "message", Type: "JSON", Description: "Message to dispatch to an actor in the pool"}},
		Outputs:     []ServiceIODef{{Name: "result", Type: "JSON", Description: "Actor response message"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "system", Label: "Actor Cluster", Type: FieldTypeString, Required: true, Description: "Name of the actor.system module this pool belongs to"},
			{Key: "mode", Label: "Lifecycle Mode", Type: FieldTypeSelect, Options: []string{"auto-managed", "permanent"}, DefaultValue: "auto-managed", Description: "'auto-managed': actors activate on first message; 'permanent': fixed pool"},
			{Key: "idleTimeout", Label: "Idle Timeout", Type: FieldTypeDuration, DefaultValue: "10m", Description: "How long an auto-managed actor stays idle before deactivating"},
			{Key: "poolSize", Label: "Pool Size", Type: FieldTypeNumber, DefaultValue: 10, Description: "Number of actors in a permanent pool"},
			{Key: "routing", Label: "Load Balancing", Type: FieldTypeSelect, Options: []string{"round-robin", "random", "broadcast", "sticky"}, DefaultValue: "round-robin", Description: "How messages are distributed among actors"},
			{Key: "routingKey", Label: "Sticky Routing Key", Type: FieldTypeString, Description: "Message field used for sticky routing"},
			{Key: "recovery", Label: "Recovery Policy", Type: FieldTypeMap, Description: "What happens when an actor crashes"},
		},
	})

	// ---- App Container ----

	r.Register(&ModuleSchema{
		Type:        "app.container",
		Label:       "App Container",
		Category:    "infrastructure",
		Description: "Application deployment abstraction that translates high-level config into platform-specific resources (Kubernetes Deployment+Service or ECS task definition)",
		Outputs:     []ServiceIODef{{Name: "container", Type: "JSON", Description: "Deployment output with service endpoint and status"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "environment", Label: "Environment", Type: FieldTypeString, Required: true, Description: "Name of the platform.kubernetes or platform.ecs module to deploy to"},
			{Key: "image", Label: "Container Image", Type: FieldTypeString, Required: true, Description: "Container image reference (e.g. registry.example.com/my-api:v1.0.0)"},
			{Key: "replicas", Label: "Replicas", Type: FieldTypeNumber, Description: "Desired replica count"},
			{Key: "ports", Label: "Ports", Type: FieldTypeArray, Description: "List of container port numbers"},
			{Key: "cpu", Label: "CPU", Type: FieldTypeString, Description: "CPU request/limit (e.g. 500m)"},
			{Key: "memory", Label: "Memory", Type: FieldTypeString, Description: "Memory request/limit (e.g. 512Mi)"},
			{Key: "env", Label: "Environment Variables", Type: FieldTypeMap, Description: "Environment variables injected into the container"},
			{Key: "health_path", Label: "Health Path", Type: FieldTypeString, Description: "HTTP health check path"},
			{Key: "health_port", Label: "Health Port", Type: FieldTypeNumber, Description: "HTTP health check port"},
		},
	})

	// ---- Argo Workflows ----

	r.Register(&ModuleSchema{
		Type:        "argo.workflows",
		Label:       "Argo Workflows",
		Category:    "cicd",
		Description: "Manages Argo Workflows submissions and status on a Kubernetes cluster",
		Outputs:     []ServiceIODef{{Name: "workflow", Type: "JSON", Description: "Argo Workflows service for workflow submission and status"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "namespace", Label: "Namespace", Type: FieldTypeString, Description: "Kubernetes namespace for Argo Workflows"},
			{Key: "server", Label: "Argo Server", Type: FieldTypeString, Description: "Argo Workflows server URL"},
			{Key: "token", Label: "Auth Token", Type: FieldTypeString, Description: "Bearer token for Argo server authentication", Sensitive: true},
		},
	})

	// ---- Auth Token Blacklist ----

	r.Register(&ModuleSchema{
		Type:        "auth.token-blacklist",
		Label:       "Token Blacklist",
		Category:    "security",
		Description: "JWT token blacklist for revocation support (memory or Redis backend)",
		Inputs:      []ServiceIODef{{Name: "token", Type: "Token", Description: "JWT token to check or add to the blacklist"}},
		Outputs:     []ServiceIODef{{Name: "blacklist", Type: "PersistenceStore", Description: "Token blacklist service for revocation checks"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "backend", Label: "Backend", Type: FieldTypeSelect, Options: []string{"memory", "redis"}, DefaultValue: "memory", Description: "Storage backend for the blacklist"},
			{Key: "redis_url", Label: "Redis URL", Type: FieldTypeString, Description: "Redis connection URL (redis backend only)", Placeholder: "redis://localhost:6379"},
			{Key: "cleanup_interval", Label: "Cleanup Interval", Type: FieldTypeDuration, DefaultValue: "5m", Description: "How often to purge expired tokens"},
		},
	})

	// ---- AWS CodeBuild ----

	r.Register(&ModuleSchema{
		Type:        "aws.codebuild",
		Label:       "AWS CodeBuild",
		Category:    "cicd",
		Description: "AWS CodeBuild integration for running build projects in the cloud",
		Outputs:     []ServiceIODef{{Name: "build", Type: "JSON", Description: "AWS CodeBuild service for running and monitoring build projects"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "region", Label: "AWS Region", Type: FieldTypeString, Description: "AWS region (e.g. us-east-1)"},
			{Key: "account", Label: "Cloud Account", Type: FieldTypeString, Description: "Name of the cloud.account module"},
			{Key: "role_arn", Label: "IAM Role ARN", Type: FieldTypeString, Description: "IAM role ARN for CodeBuild service role"},
		},
	})

	// ---- Cache Redis ----

	r.Register(&ModuleSchema{
		Type:        "cache.redis",
		Label:       "Redis Cache",
		Category:    "infrastructure",
		Description: "Redis-backed key/value cache for pipeline step data",
		Inputs:      []ServiceIODef{{Name: "key", Type: "string", Description: "Cache key for get/set operations"}},
		Outputs:     []ServiceIODef{{Name: "cache", Type: "Cache", Description: "Redis cache service"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "address", Label: "Address", Type: FieldTypeString, DefaultValue: "localhost:6379", Description: "Redis server address (host:port)"},
			{Key: "password", Label: "Password", Type: FieldTypeString, Description: "Redis password (optional)", Sensitive: true},
			{Key: "db", Label: "Database", Type: FieldTypeNumber, DefaultValue: 0, Description: "Redis database number"},
			{Key: "prefix", Label: "Key Prefix", Type: FieldTypeString, DefaultValue: "wf:", Description: "Prefix applied to all cache keys"},
			{Key: "defaultTTL", Label: "Default TTL", Type: FieldTypeString, DefaultValue: "1h", Description: "Default time-to-live for cached values"},
		},
	})

	// ---- Cloud Account ----

	r.Register(&ModuleSchema{
		Type:        "cloud.account",
		Label:       "Cloud Account",
		Category:    "infrastructure",
		Description: "Cloud provider credentials (AWS, GCP, Azure, DigitalOcean, Kubernetes, or mock)",
		Outputs:     []ServiceIODef{{Name: "credentials", Type: "Credentials", Description: "Cloud provider credentials for downstream infrastructure modules"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "provider", Label: "Provider", Type: FieldTypeString, Required: true, Description: "Cloud provider: aws, gcp, azure, digitalocean, kubernetes, mock"},
			{Key: "region", Label: "Region", Type: FieldTypeString, Description: "Primary region (e.g. us-east-1, us-central1, eastus, nyc3)"},
			{Key: "credentials", Label: "Credentials", Type: FieldTypeMap, Description: "Credential configuration (type, keys, paths)"},
			{Key: "project_id", Label: "GCP Project ID", Type: FieldTypeString, Description: "GCP project ID"},
			{Key: "subscription_id", Label: "Azure Subscription ID", Type: FieldTypeString, Description: "Azure subscription ID"},
		},
	})

	// ---- GitLab Client ----

	r.Register(&ModuleSchema{
		Type:        "gitlab.client",
		Label:       "GitLab Client",
		Category:    "integration",
		Description: "GitLab API client for triggering pipelines, managing MRs, and querying pipeline status",
		Outputs:     []ServiceIODef{{Name: "client", Type: "ExternalAPIClient", Description: "GitLab API client for pipeline and MR operations"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "url", Label: "GitLab URL", Type: FieldTypeString, DefaultValue: "https://gitlab.com", Description: "GitLab instance URL"},
			{Key: "token", Label: "Access Token", Type: FieldTypeString, Required: true, Description: "GitLab personal or project access token", Sensitive: true},
		},
	})

	// ---- GitLab Webhook ----

	r.Register(&ModuleSchema{
		Type:        "gitlab.webhook",
		Label:       "GitLab Webhook",
		Category:    "integration",
		Description: "GitLab webhook receiver that parses and validates incoming GitLab events",
		Inputs:      []ServiceIODef{{Name: "event", Type: "http.Request", Description: "Incoming GitLab webhook HTTP request"}},
		Outputs:     []ServiceIODef{{Name: "event", Type: "Event", Description: "Parsed and validated GitLab event"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "secret", Label: "Webhook Secret", Type: FieldTypeString, Description: "GitLab webhook secret token for request validation", Sensitive: true},
		},
	})

	// ---- HTTP Middleware OTEL ----

	r.Register(&ModuleSchema{
		Type:        "http.middleware.otel",
		Label:       "OTEL HTTP Middleware",
		Category:    "observability",
		Description: "Instruments HTTP requests with OpenTelemetry tracing spans",
		Inputs:      []ServiceIODef{{Name: "request", Type: "http.Request", Description: "HTTP request to instrument with tracing"}},
		Outputs:     []ServiceIODef{{Name: "request", Type: "http.Request", Description: "HTTP request with OpenTelemetry span attached"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "serverName", Label: "Server Name", Type: FieldTypeString, DefaultValue: "workflow-http", Description: "Server name used as the span operation name"},
		},
	})

	// ---- IaC State ----

	r.Register(&ModuleSchema{
		Type:        "iac.state",
		Label:       "IaC State Store",
		Category:    "infrastructure",
		Description: "Tracks infrastructure provisioning state (memory or filesystem backend)",
		Inputs:      []ServiceIODef{{Name: "state", Type: "JSON", Description: "Infrastructure state snapshot to store or update"}},
		Outputs:     []ServiceIODef{{Name: "state", Type: "PersistenceStore", Description: "IaC state persistence service"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "backend", Label: "Backend", Type: FieldTypeString, Description: "memory or filesystem"},
			{Key: "directory", Label: "Directory", Type: FieldTypeString, Description: "State directory (filesystem backend only)"},
		},
	})

	// ---- NoSQL DynamoDB ----

	r.Register(&ModuleSchema{
		Type:        "nosql.dynamodb",
		Label:       "DynamoDB",
		Category:    "database",
		Description: "AWS DynamoDB NoSQL store",
		Inputs:      []ServiceIODef{{Name: "key", Type: "string", Description: "Document key for get/put operations"}},
		Outputs:     []ServiceIODef{{Name: "store", Type: "PersistenceStore", Description: "DynamoDB NoSQL persistence store"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "region", Label: "Region", Type: FieldTypeString, Description: "AWS region"},
			{Key: "table", Label: "Table", Type: FieldTypeString, Required: true, Description: "DynamoDB table name"},
			{Key: "endpoint", Label: "Endpoint", Type: FieldTypeString, Description: "Custom endpoint (for local DynamoDB)"},
		},
	})

	// ---- NoSQL Memory ----

	r.Register(&ModuleSchema{
		Type:         "nosql.memory",
		Label:        "In-Memory NoSQL",
		Category:     "database",
		Description:  "In-memory NoSQL store for testing and development",
		Inputs:       []ServiceIODef{{Name: "key", Type: "string", Description: "Document key for get/put operations"}},
		Outputs:      []ServiceIODef{{Name: "store", Type: "PersistenceStore", Description: "In-memory NoSQL persistence store"}},
		ConfigFields: []ConfigFieldDef{},
	})

	// ---- NoSQL MongoDB ----

	r.Register(&ModuleSchema{
		Type:        "nosql.mongodb",
		Label:       "MongoDB",
		Category:    "database",
		Description: "MongoDB NoSQL document store",
		Inputs:      []ServiceIODef{{Name: "key", Type: "string", Description: "Document key for get/put operations"}},
		Outputs:     []ServiceIODef{{Name: "store", Type: "PersistenceStore", Description: "MongoDB NoSQL persistence store"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "uri", Label: "Connection URI", Type: FieldTypeString, Required: true, Description: "MongoDB connection URI", Placeholder: "mongodb://localhost:27017"},
			{Key: "database", Label: "Database", Type: FieldTypeString, Required: true, Description: "MongoDB database name"},
			{Key: "collection", Label: "Collection", Type: FieldTypeString, Description: "Default collection name"},
		},
	})

	// ---- NoSQL Redis ----

	r.Register(&ModuleSchema{
		Type:        "nosql.redis",
		Label:       "Redis NoSQL",
		Category:    "database",
		Description: "Redis as a NoSQL key/value store",
		Inputs:      []ServiceIODef{{Name: "key", Type: "string", Description: "Document key for get/put operations"}},
		Outputs:     []ServiceIODef{{Name: "store", Type: "PersistenceStore", Description: "Redis NoSQL persistence store"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "address", Label: "Address", Type: FieldTypeString, DefaultValue: "localhost:6379", Description: "Redis server address"},
			{Key: "password", Label: "Password", Type: FieldTypeString, Description: "Redis password", Sensitive: true},
			{Key: "db", Label: "Database", Type: FieldTypeNumber, DefaultValue: 0, Description: "Redis database number"},
		},
	})

	// ---- Platform API Gateway ----

	r.Register(&ModuleSchema{
		Type:        "platform.apigateway",
		Label:       "API Gateway",
		Category:    "infrastructure",
		Description: "Manages API gateway provisioning with routes, stages, and rate limiting (mock or AWS API Gateway v2)",
		Outputs:     []ServiceIODef{{Name: "gateway", Type: "JSON", Description: "Provisioned API gateway endpoint and configuration"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "account", Label: "Cloud Account", Type: FieldTypeString, Description: "Name of the cloud.account module"},
			{Key: "provider", Label: "Provider", Type: FieldTypeString, Description: "mock | aws"},
			{Key: "name", Label: "Gateway Name", Type: FieldTypeString, Required: true, Description: "API gateway name"},
			{Key: "stage", Label: "Stage", Type: FieldTypeString, Description: "Deployment stage (dev, staging, prod)"},
			{Key: "cors", Label: "CORS Config", Type: FieldTypeMap, Description: "CORS configuration (allowedOrigins, allowedMethods, allowedHeaders)"},
			{Key: "routes", Label: "Routes", Type: FieldTypeArray, Description: "Route definitions"},
		},
	})

	// ---- Platform Autoscaling ----

	r.Register(&ModuleSchema{
		Type:        "platform.autoscaling",
		Label:       "Autoscaling Policies",
		Category:    "infrastructure",
		Description: "Manages autoscaling policies (target tracking, step, scheduled) for AWS or mock resources",
		Outputs:     []ServiceIODef{{Name: "policies", Type: "JSON", Description: "Configured autoscaling policies"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "account", Label: "Cloud Account", Type: FieldTypeString, Description: "Name of the cloud.account module"},
			{Key: "provider", Label: "Provider", Type: FieldTypeString, Description: "mock | aws"},
			{Key: "policies", Label: "Policies", Type: FieldTypeArray, Required: true, Description: "Scaling policy definitions"},
		},
	})

	// ---- Platform DNS ----

	r.Register(&ModuleSchema{
		Type:        "platform.dns",
		Label:       "DNS Zone Manager",
		Category:    "infrastructure",
		Description: "Manages DNS zones and records (mock or Route53/aws backend)",
		Outputs:     []ServiceIODef{{Name: "zone", Type: "JSON", Description: "Provisioned DNS zone and record set"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "account", Label: "Cloud Account", Type: FieldTypeString, Description: "Name of the cloud.account module"},
			{Key: "provider", Label: "Provider", Type: FieldTypeString, Description: "mock | aws"},
			{Key: "zone", Label: "Zone Config", Type: FieldTypeMap, Required: true, Description: "Zone configuration (name, comment, private, vpcId)"},
			{Key: "records", Label: "DNS Records", Type: FieldTypeArray, Description: "List of DNS record definitions"},
		},
	})

	// ---- Platform DigitalOcean App ----

	r.Register(&ModuleSchema{
		Type:        "platform.do_app",
		Label:       "DigitalOcean App Platform",
		Category:    "infrastructure",
		Description: "Deploys containerized apps to DigitalOcean App Platform (mock or real DO backend)",
		Outputs:     []ServiceIODef{{Name: "app", Type: "JSON", Description: "Deployed app endpoint and status on DigitalOcean App Platform"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "account", Label: "Cloud Account", Type: FieldTypeString, Description: "Name of the cloud.account module"},
			{Key: "provider", Label: "Provider", Type: FieldTypeString, Description: "mock | digitalocean"},
			{Key: "name", Label: "App Name", Type: FieldTypeString, Description: "App Platform application name"},
			{Key: "region", Label: "Region", Type: FieldTypeString, Description: "DO region slug (e.g. nyc)"},
			{Key: "image", Label: "Container Image", Type: FieldTypeString, Description: "Container image reference"},
			{Key: "instances", Label: "Instances", Type: FieldTypeNumber, Description: "Number of instances"},
			{Key: "http_port", Label: "HTTP Port", Type: FieldTypeNumber, Description: "Container HTTP port"},
			{Key: "envs", Label: "Environment Variables", Type: FieldTypeMap, Description: "Environment variables for the app"},
		},
	})

	// ---- Platform DigitalOcean Database ----

	r.Register(&ModuleSchema{
		Type:        "platform.do_database",
		Label:       "DigitalOcean Managed Database",
		Category:    "infrastructure",
		Description: "Manages DigitalOcean Managed Databases (PostgreSQL, MySQL, Redis, MongoDB, Kafka)",
		Outputs:     []ServiceIODef{{Name: "database", Type: "sql.DB", Description: "Managed database connection for DigitalOcean database cluster"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "account", Label: "Cloud Account", Type: FieldTypeString, Description: "Name of the cloud.account module"},
			{Key: "provider", Label: "Provider", Type: FieldTypeString, Description: "mock | digitalocean"},
			{Key: "engine", Label: "Engine", Type: FieldTypeString, Description: "Database engine: pg | mysql | redis | mongodb | kafka"},
			{Key: "version", Label: "Version", Type: FieldTypeString, Description: "Engine version"},
			{Key: "size", Label: "Size", Type: FieldTypeString, Description: "Droplet size slug"},
			{Key: "region", Label: "Region", Type: FieldTypeString, Description: "DO region slug"},
			{Key: "num_nodes", Label: "Node Count", Type: FieldTypeNumber, Description: "Number of nodes"},
			{Key: "name", Label: "Cluster Name", Type: FieldTypeString, Description: "Database cluster name"},
		},
	})

	// ---- Platform DigitalOcean DNS ----

	r.Register(&ModuleSchema{
		Type:        "platform.do_dns",
		Label:       "DigitalOcean DNS",
		Category:    "infrastructure",
		Description: "Manages DigitalOcean domains and DNS records (mock or real DO backend)",
		Outputs:     []ServiceIODef{{Name: "zone", Type: "JSON", Description: "Provisioned DigitalOcean DNS zone and records"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "account", Label: "Cloud Account", Type: FieldTypeString, Description: "Name of the cloud.account module"},
			{Key: "provider", Label: "Provider", Type: FieldTypeString, Description: "mock | digitalocean"},
			{Key: "domain", Label: "Domain", Type: FieldTypeString, Required: true, Description: "Domain name (e.g. example.com)"},
			{Key: "records", Label: "Records", Type: FieldTypeArray, Description: "List of DNS record definitions"},
		},
	})

	// ---- Platform DigitalOcean Networking ----

	r.Register(&ModuleSchema{
		Type:        "platform.do_networking",
		Label:       "DigitalOcean VPC & Firewalls",
		Category:    "infrastructure",
		Description: "Manages DigitalOcean VPCs, firewalls, and load balancers (mock or real DO backend)",
		Outputs:     []ServiceIODef{{Name: "vpc", Type: "JSON", Description: "Provisioned DigitalOcean VPC and firewall configuration"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "account", Label: "Cloud Account", Type: FieldTypeString, Description: "Name of the cloud.account module"},
			{Key: "provider", Label: "Provider", Type: FieldTypeString, Description: "mock | digitalocean"},
			{Key: "vpc", Label: "VPC Config", Type: FieldTypeMap, Required: true, Description: "VPC configuration (name, region, ip_range)"},
			{Key: "firewalls", Label: "Firewalls", Type: FieldTypeArray, Description: "List of firewall definitions"},
		},
	})

	// ---- Platform DOKS ----

	r.Register(&ModuleSchema{
		Type:        "platform.doks",
		Label:       "DigitalOcean Kubernetes (DOKS)",
		Category:    "infrastructure",
		Description: "Manages DigitalOcean Kubernetes Service clusters (mock or real DO backend)",
		Outputs:     []ServiceIODef{{Name: "cluster", Type: "JSON", Description: "Provisioned DOKS cluster endpoint and kubeconfig"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "account", Label: "Cloud Account", Type: FieldTypeString, Description: "Name of the cloud.account module"},
			{Key: "cluster_name", Label: "Cluster Name", Type: FieldTypeString, Description: "DOKS cluster name"},
			{Key: "region", Label: "Region", Type: FieldTypeString, Description: "DO region slug (e.g. nyc3)"},
			{Key: "version", Label: "Kubernetes Version", Type: FieldTypeString, Description: "Kubernetes version slug"},
			{Key: "node_pool", Label: "Node Pool", Type: FieldTypeMap, Description: "Node pool config"},
		},
	})

	// ---- Platform ECS ----

	r.Register(&ModuleSchema{
		Type:        "platform.ecs",
		Label:       "ECS Fargate Service",
		Category:    "infrastructure",
		Description: "AWS ECS/Fargate service with task definitions and ALB target group config",
		Outputs:     []ServiceIODef{{Name: "service", Type: "JSON", Description: "Provisioned ECS Fargate service ARN and endpoint"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "account", Label: "Cloud Account", Type: FieldTypeString, Description: "Name of the cloud.account module"},
			{Key: "cluster", Label: "ECS Cluster", Type: FieldTypeString, Required: true, Description: "ECS cluster name"},
			{Key: "region", Label: "AWS Region", Type: FieldTypeString, Description: "AWS region (e.g. us-east-1)"},
			{Key: "launch_type", Label: "Launch Type", Type: FieldTypeString, Description: "FARGATE or EC2"},
			{Key: "desired_count", Label: "Desired Count", Type: FieldTypeString, Description: "Number of tasks to run"},
			{Key: "vpc_subnets", Label: "VPC Subnets", Type: FieldTypeArray, ArrayItemType: "string", Description: "List of subnet IDs"},
			{Key: "security_groups", Label: "Security Groups", Type: FieldTypeArray, ArrayItemType: "string", Description: "List of security group IDs"},
		},
	})

	// ---- Platform Kubernetes ----

	r.Register(&ModuleSchema{
		Type:        "platform.kubernetes",
		Label:       "Kubernetes Cluster",
		Category:    "infrastructure",
		Description: "Managed Kubernetes cluster (kind/k3s for local, EKS/GKE/AKS stubs for cloud)",
		Outputs:     []ServiceIODef{{Name: "cluster", Type: "JSON", Description: "Kubernetes cluster endpoint and kubeconfig"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "account", Label: "Cloud Account", Type: FieldTypeString, Description: "Name of the cloud.account module"},
			{Key: "type", Label: "Cluster Type", Type: FieldTypeString, Required: true, Description: "eks | gke | aks | kind | k3s"},
			{Key: "version", Label: "Kubernetes Version", Type: FieldTypeString, Description: "e.g. 1.29"},
			{Key: "nodeGroups", Label: "Node Groups", Type: FieldTypeArray, Description: "Node group definitions"},
		},
	})

	// ---- Platform Networking ----

	r.Register(&ModuleSchema{
		Type:        "platform.networking",
		Label:       "VPC Networking",
		Category:    "infrastructure",
		Description: "Manages VPC, subnets, NAT gateway, and security groups (mock or AWS backend)",
		Outputs:     []ServiceIODef{{Name: "vpc", Type: "JSON", Description: "Provisioned VPC with subnets and security groups"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "account", Label: "Cloud Account", Type: FieldTypeString, Description: "Name of the cloud.account module"},
			{Key: "provider", Label: "Provider", Type: FieldTypeString, Description: "mock | aws"},
			{Key: "vpc", Label: "VPC Config", Type: FieldTypeMap, Required: true, Description: "VPC configuration (cidr, name)"},
			{Key: "subnets", Label: "Subnets", Type: FieldTypeArray, Description: "List of subnet definitions"},
			{Key: "nat_gateway", Label: "NAT Gateway", Type: FieldTypeBool, Description: "Provision a NAT gateway"},
			{Key: "security_groups", Label: "Security Groups", Type: FieldTypeArray, Description: "List of security group definitions"},
		},
	})

	// ---- Platform Region ----

	r.Register(&ModuleSchema{
		Type:        "platform.region",
		Label:       "Multi-Region Deployment",
		Category:    "infrastructure",
		Description: "Manages multi-region tenant deployments with failover, health checking, and traffic weight routing",
		Outputs:     []ServiceIODef{{Name: "regions", Type: "JSON", Description: "Multi-region deployment status and endpoint map"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "provider", Label: "Provider", Type: FieldTypeString, Description: "mock (default)"},
			{Key: "regions", Label: "Regions", Type: FieldTypeArray, Required: true, Description: "List of region definitions (name, provider, endpoint, priority, health_check)"},
		},
	})

	// ---- Platform Region Router ----

	r.Register(&ModuleSchema{
		Type:        "platform.region_router",
		Label:       "Region Router",
		Category:    "infrastructure",
		Description: "Routes traffic between regions based on weights, health, and failover policies",
		Inputs:      []ServiceIODef{{Name: "request", Type: "http.Request", Description: "Incoming HTTP request to route to a region"}},
		Outputs:     []ServiceIODef{{Name: "routed", Type: "http.Request", Description: "Request routed to the selected region's endpoint"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "region", Label: "Region Module", Type: FieldTypeString, Required: true, Description: "Name of the platform.region module to route"},
			{Key: "strategy", Label: "Routing Strategy", Type: FieldTypeSelect, Options: []string{"weighted", "failover", "active-active"}, DefaultValue: "weighted", Description: "Traffic routing strategy"},
		},
	})

	// ---- Policy Mock ----

	r.Register(&ModuleSchema{
		Type:        "policy.mock",
		Label:       "Mock Policy Engine",
		Category:    "security",
		Description: "In-memory mock policy engine for testing. Denies if any loaded policy contains the word 'deny'.",
		Inputs:      []ServiceIODef{{Name: "request", Type: "JSON", Description: "Authorization request to evaluate against policies"}},
		Outputs:     []ServiceIODef{{Name: "decision", Type: "JSON", Description: "Policy decision (allow/deny) with reason"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "policies", Label: "Pre-loaded Policies", Type: FieldTypeArray, Description: "List of policies to load at startup"},
		},
	})

	// ---- Security Field Protection ----

	r.Register(&ModuleSchema{
		Type:        "security.field-protection",
		Label:       "Field Protection",
		Category:    "security",
		Description: "Field-level encryption/masking for sensitive data in pipeline responses",
		Inputs:      []ServiceIODef{{Name: "data", Type: "JSON", Description: "JSON data with fields to protect via encryption or masking"}},
		Outputs:     []ServiceIODef{{Name: "protected", Type: "JSON", Description: "JSON data with sensitive fields encrypted or masked"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "fields", Label: "Protected Fields", Type: FieldTypeArray, Description: "List of field protection rules (path, action: mask|encrypt|redact)"},
			{Key: "key", Label: "Encryption Key", Type: FieldTypeString, Description: "Key used for field encryption", Sensitive: true},
		},
	})

	// ---- Security Scanner ----

	r.Register(&ModuleSchema{
		Type:        "security.scanner",
		Label:       "Security Scanner",
		Category:    "security",
		Description: "Security scanner provider supporting mock, semgrep, trivy, and grype backends",
		Outputs:     []ServiceIODef{{Name: "scanner", Type: "JSON", Description: "Security scanner service for SAST, container, and dependency scans"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "mode", Label: "Mode", Type: FieldTypeSelect, Options: []string{"mock", "cli"}, DefaultValue: "mock", Description: "Scanner mode: 'mock' for testing or 'cli' for real tools"},
			{Key: "semgrepBinary", Label: "Semgrep Binary", Type: FieldTypeString, DefaultValue: "semgrep", Description: "Path to semgrep binary"},
			{Key: "trivyBinary", Label: "Trivy Binary", Type: FieldTypeString, DefaultValue: "trivy", Description: "Path to trivy binary"},
			{Key: "grypeBinary", Label: "Grype Binary", Type: FieldTypeString, DefaultValue: "grype", Description: "Path to grype binary"},
			{Key: "mockFindings", Label: "Mock Findings", Type: FieldTypeMap, Description: "Mock findings to return (keyed by scan type: sast, container, deps)"},
		},
	})

	// ---- Storage Artifact ----

	r.Register(&ModuleSchema{
		Type:        "storage.artifact",
		Label:       "Artifact Store",
		Category:    "infrastructure",
		Description: "Named artifact storage with metadata support (filesystem or S3)",
		Inputs:      []ServiceIODef{{Name: "artifact", Type: "[]byte", Description: "Binary artifact data to store or retrieve"}},
		Outputs:     []ServiceIODef{{Name: "storage", Type: "FileStore", Description: "Artifact file storage service with metadata support"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "backend", Label: "Backend", Type: FieldTypeSelect, Options: []string{"filesystem", "s3"}, DefaultValue: "filesystem", Description: "Storage backend to use"},
			{Key: "basePath", Label: "Base Path", Type: FieldTypeString, DefaultValue: "./data/artifacts", Description: "Root directory for filesystem backend"},
			{Key: "maxSize", Label: "Max Size (bytes)", Type: FieldTypeNumber, Description: "Maximum artifact size in bytes (0 = unlimited)"},
			{Key: "bucket", Label: "S3 Bucket", Type: FieldTypeString, Description: "S3 bucket name (s3 backend only)"},
			{Key: "region", Label: "S3 Region", Type: FieldTypeString, Description: "AWS region (s3 backend only)"},
			{Key: "endpoint", Label: "S3 Endpoint", Type: FieldTypeString, Description: "Custom S3 endpoint"},
		},
	})

	// ---- Tracing Propagation ----

	r.Register(&ModuleSchema{
		Type:        "tracing.propagation",
		Label:       "Trace Propagation",
		Category:    "observability",
		Description: "Propagates trace context across async boundaries (Kafka, EventBridge, webhooks, HTTP)",
		Inputs:      []ServiceIODef{{Name: "span", Type: "trace.Span", Description: "Trace span to propagate across async boundaries"}},
		Outputs:     []ServiceIODef{{Name: "propagation", Type: "trace.Span", Description: "Trace span with propagation context injected or extracted"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "format", Label: "Propagation Format", Type: FieldTypeSelect, Options: []string{"w3c", "b3", "composite"}, DefaultValue: "w3c", Description: "Trace context propagation format"},
		},
	})

	// ---- Auth M2M ----

	r.Register(&ModuleSchema{
		Type:        "auth.m2m",
		Label:       "M2M Auth",
		Category:    "security",
		Description: "Machine-to-machine OAuth2 auth: client_credentials grant, JWT-bearer assertion, ES256/HS256 token issuance, and JWKS endpoint",
		Inputs:      []ServiceIODef{{Name: "credentials", Type: "Credentials", Description: "Client credentials for M2M OAuth2 token request"}},
		Outputs:     []ServiceIODef{{Name: "auth", Type: "AuthService", Description: "M2M authentication service for token issuance and validation"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "secret", Label: "HMAC Secret", Type: FieldTypeString, Description: "Secret for HS256 token signing", Sensitive: true},
			{Key: "algorithm", Label: "Signing Algorithm", Type: FieldTypeSelect, Options: []string{"HS256", "ES256"}, DefaultValue: "ES256", Description: "JWT signing algorithm"},
			{Key: "tokenExpiry", Label: "Token Expiry", Type: FieldTypeDuration, DefaultValue: "1h", Description: "Access token expiration duration"},
			{Key: "issuer", Label: "Issuer", Type: FieldTypeString, DefaultValue: "workflow", Description: "Token issuer claim"},
			{Key: "clients", Label: "Registered Clients", Type: FieldTypeArray, Description: "List of OAuth2 clients"},
		},
	})

	// ---- Auth OAuth2 ----

	r.Register(&ModuleSchema{
		Type:        "auth.oauth2",
		Label:       "OAuth2",
		Category:    "security",
		Description: "OAuth2 authorization code flow supporting Google, GitHub, and generic OIDC providers",
		Inputs:      []ServiceIODef{{Name: "credentials", Type: "Credentials", Description: "OAuth2 credentials (client ID/secret or authorization code)"}},
		Outputs:     []ServiceIODef{{Name: "auth", Type: "AuthService", Description: "OAuth2 authentication service for authorization code flow"}},
		ConfigFields: []ConfigFieldDef{
			{Key: "providers", Label: "Providers", Type: FieldTypeArray, Required: true, Description: "List of OAuth2 provider configurations"},
		},
	})

	// ---- Step types (pipeline steps registered as module types) ----

	for _, stepType := range []struct {
		t, label, desc string
	}{
		{"step.actor_send", "Actor Send", "Send a message to an actor without waiting for a response"},
		{"step.actor_ask", "Actor Ask", "Send a message to an actor and wait for a response"},
		{"step.apigw_apply", "API Gateway Apply", "Applies API gateway configuration"},
		{"step.apigw_destroy", "API Gateway Destroy", "Destroys a provisioned API gateway"},
		{"step.apigw_plan", "API Gateway Plan", "Plans API gateway changes without applying them"},
		{"step.apigw_status", "API Gateway Status", "Gets the current status of an API gateway"},
		{"step.app_deploy", "App Deploy", "Deploys an application container"},
		{"step.app_rollback", "App Rollback", "Rolls back an application to a previous version"},
		{"step.app_status", "App Status", "Gets the deployment status of an application"},
		{"step.argo_delete", "Argo Delete", "Deletes an Argo Workflow"},
		{"step.argo_list", "Argo List", "Lists Argo Workflows"},
		{"step.argo_logs", "Argo Logs", "Retrieves logs from an Argo Workflow"},
		{"step.argo_status", "Argo Status", "Gets the status of an Argo Workflow"},
		{"step.argo_submit", "Argo Submit", "Submits an Argo Workflow"},
		{"step.artifact_delete", "Artifact Delete", "Deletes an artifact from the artifact store"},
		{"step.artifact_download", "Artifact Download", "Downloads an artifact from the artifact store"},
		{"step.artifact_list", "Artifact List", "Lists artifacts in the artifact store"},
		{"step.artifact_upload", "Artifact Upload", "Uploads a file as an artifact"},
		{"step.build_binary", "Build Binary", "Builds a Go binary from source"},
		{"step.build_from_config", "Build From Config", "Builds using workflow engine config as build spec"},
		{"step.cloud_validate", "Cloud Validate", "Validates cloud provider credentials"},
		{"step.codebuild_create_project", "CodeBuild Create Project", "Creates an AWS CodeBuild project"},
		{"step.codebuild_delete_project", "CodeBuild Delete Project", "Deletes an AWS CodeBuild project"},
		{"step.codebuild_list_builds", "CodeBuild List Builds", "Lists builds for a CodeBuild project"},
		{"step.codebuild_logs", "CodeBuild Logs", "Retrieves logs for a CodeBuild build"},
		{"step.codebuild_start", "CodeBuild Start", "Starts an AWS CodeBuild build"},
		{"step.codebuild_status", "CodeBuild Status", "Gets the status of a CodeBuild build"},
		{"step.dns_apply", "DNS Apply", "Applies DNS zone and record changes"},
		{"step.dns_plan", "DNS Plan", "Plans DNS changes without applying them"},
		{"step.dns_status", "DNS Status", "Gets the current status of a DNS zone"},
		{"step.do_deploy", "DO Deploy", "Deploys to DigitalOcean App Platform"},
		{"step.do_destroy", "DO Destroy", "Destroys a DigitalOcean App Platform application"},
		{"step.do_logs", "DO Logs", "Retrieves logs from DigitalOcean App Platform"},
		{"step.do_scale", "DO Scale", "Scales a DigitalOcean App Platform application"},
		{"step.do_status", "DO Status", "Gets the status of a DigitalOcean App Platform application"},
		{"step.ecs_apply", "ECS Apply", "Applies ECS Fargate service deployment"},
		{"step.ecs_destroy", "ECS Destroy", "Destroys an ECS Fargate service"},
		{"step.ecs_plan", "ECS Plan", "Plans ECS service deployment changes"},
		{"step.ecs_status", "ECS Status", "Gets the status of an ECS Fargate service"},
		{"step.git_checkout", "Git Checkout", "Checks out a Git branch, tag, or commit"},
		{"step.git_clone", "Git Clone", "Clones a Git repository"},
		{"step.git_commit", "Git Commit", "Creates a Git commit"},
		{"step.git_push", "Git Push", "Pushes commits to a remote repository"},
		{"step.git_tag", "Git Tag", "Creates a Git tag"},
		{"step.gitlab_create_mr", "GitLab Create MR", "Creates a GitLab merge request"},
		{"step.gitlab_mr_comment", "GitLab MR Comment", "Adds a comment to a GitLab merge request"},
		{"step.gitlab_parse_webhook", "GitLab Parse Webhook", "Parses and validates a GitLab webhook"},
		{"step.gitlab_pipeline_status", "GitLab Pipeline Status", "Gets the status of a GitLab pipeline"},
		{"step.gitlab_trigger_pipeline", "GitLab Trigger Pipeline", "Triggers a GitLab CI/CD pipeline"},
		{"step.iac_apply", "IaC Apply", "Applies infrastructure changes"},
		{"step.iac_destroy", "IaC Destroy", "Destroys IaC-managed infrastructure"},
		{"step.iac_drift_detect", "IaC Drift Detect", "Detects IaC configuration drift"},
		{"step.iac_plan", "IaC Plan", "Plans infrastructure changes without applying"},
		{"step.iac_status", "IaC Status", "Gets IaC provisioning status"},
		{"step.k8s_apply", "K8s Apply", "Applies Kubernetes manifests"},
		{"step.k8s_destroy", "K8s Destroy", "Deletes Kubernetes resources"},
		{"step.k8s_plan", "K8s Plan", "Diffs Kubernetes manifests against cluster state"},
		{"step.k8s_status", "K8s Status", "Gets the status of Kubernetes resources"},
		{"step.marketplace_detail", "Marketplace Detail", "Gets details about a marketplace plugin"},
		{"step.marketplace_install", "Marketplace Install", "Installs a marketplace plugin"},
		{"step.marketplace_installed", "Marketplace Installed", "Lists installed marketplace plugins"},
		{"step.marketplace_search", "Marketplace Search", "Searches the plugin marketplace"},
		{"step.marketplace_uninstall", "Marketplace Uninstall", "Uninstalls a marketplace plugin"},
		{"step.marketplace_update", "Marketplace Update", "Updates an installed marketplace plugin"},
		{"step.network_apply", "Network Apply", "Applies VPC networking changes"},
		{"step.network_plan", "Network Plan", "Plans VPC networking changes"},
		{"step.network_status", "Network Status", "Gets VPC networking status"},
		{"step.nosql_delete", "NoSQL Delete", "Deletes an item from a NoSQL store"},
		{"step.policy_evaluate", "Policy Evaluate", "Evaluates input against a policy"},
		{"step.policy_list", "Policy List", "Lists loaded policies"},
		{"step.policy_load", "Policy Load", "Loads a policy at runtime"},
		{"step.policy_test", "Policy Test", "Tests a policy against cases"},
		{"step.region_deploy", "Region Deploy", "Deploys to a specific region"},
		{"step.region_failover", "Region Failover", "Triggers regional failover"},
		{"step.region_promote", "Region Promote", "Promotes a region to primary"},
		{"step.region_status", "Region Status", "Gets multi-region health status"},
		{"step.region_sync", "Region Sync", "Syncs state across regions"},
		{"step.region_weight", "Region Weight", "Sets traffic weight for a region"},
		{"step.scaling_apply", "Scaling Apply", "Applies autoscaling policies"},
		{"step.scaling_destroy", "Scaling Destroy", "Removes autoscaling policies"},
		{"step.scaling_plan", "Scaling Plan", "Plans autoscaling changes"},
		{"step.scaling_status", "Scaling Status", "Gets autoscaling status"},
		{"step.secret_rotate", "Secret Rotate", "Rotates a secret"},
		{"step.trace_annotate", "Trace Annotate", "Adds attributes to the current trace span"},
		{"step.trace_extract", "Trace Extract", "Extracts trace context from incoming headers"},
		{"step.trace_inject", "Trace Inject", "Injects trace context into outgoing headers"},
		{"step.trace_link", "Trace Link", "Links the current span to another span"},
		{"step.trace_start", "Trace Start", "Starts a new trace span"},
	} {
		r.Register(&ModuleSchema{
			Type:         stepType.t,
			Label:        stepType.label,
			Category:     "pipeline",
			Description:  stepType.desc,
			ConfigFields: []ConfigFieldDef{},
		})
	}

	// ---- Deployment steps (cicd plugin) ----

	r.Register(&ModuleSchema{
		Type:        "step.container_build",
		Label:       "Container Build",
		Category:    "deployment",
		Description: "Builds a container image using docker/podman and pushes it to a registry",
		ConfigFields: []ConfigFieldDef{
			{Key: "context", Label: "Build Context", Type: FieldTypeFilePath, Required: true, Description: "Build context path"},
			{Key: "tag", Label: "Image Tag", Type: FieldTypeString, Required: true, Description: "Image tag (template expressions supported)"},
			{Key: "registry", Label: "Registry", Type: FieldTypeString, Required: true, Description: "ContainerRegistry service name"},
			{Key: "dockerfile", Label: "Dockerfile", Type: FieldTypeString, Description: "Dockerfile path relative to context", DefaultValue: "Dockerfile"},
			{Key: "build_args", Label: "Build Args", Type: FieldTypeMap, Description: "Build-time variables"},
			{Key: "builder", Label: "Builder", Type: FieldTypeSelect, Options: []string{"docker", "podman"}, Description: "Container builder binary", DefaultValue: "docker"},
			{Key: "dry_run", Label: "Dry Run", Type: FieldTypeBool, Description: "Print build command without executing"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.deploy_blue_green",
		Label:       "Deploy Blue/Green",
		Category:    "deployment",
		Description: "Deploys using blue/green strategy: creates green environment, verifies it, switches traffic, destroys blue",
		ConfigFields: []ConfigFieldDef{
			{Key: "service", Label: "Service", Type: FieldTypeString, Required: true, Description: "BlueGreenDriver service name"},
			{Key: "image", Label: "Image", Type: FieldTypeString, Required: true, Description: "Docker image to deploy"},
			{Key: "health_check", Label: "Health Check", Type: FieldTypeMap, Description: "Health check config: {path, timeout}"},
			{Key: "traffic_switch", Label: "Traffic Switch", Type: FieldTypeSelect, Options: []string{"dns", "lb"}, Description: "Traffic switch mechanism", DefaultValue: "lb"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.deploy_canary",
		Label:       "Deploy Canary",
		Category:    "deployment",
		Description: "Gradually shifts traffic to a new image via configurable stages with optional metric gates",
		ConfigFields: []ConfigFieldDef{
			{Key: "service", Label: "Service", Type: FieldTypeString, Required: true, Description: "CanaryDriver service name"},
			{Key: "image", Label: "Image", Type: FieldTypeString, Required: true, Description: "Docker image to deploy"},
			{Key: "stages", Label: "Stages", Type: FieldTypeArray, Description: "Canary stages: [{percent, duration, metric_gate}]"},
			{Key: "rollback_on_failure", Label: "Rollback on Failure", Type: FieldTypeBool, Description: "Automatically rollback if a stage fails"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.deploy_rollback",
		Label:       "Deploy Rollback",
		Category:    "deployment",
		Description: "Rolls back a service to a previous image version using deployment history",
		ConfigFields: []ConfigFieldDef{
			{Key: "service", Label: "Service", Type: FieldTypeString, Required: true, Description: "DeployDriver service name"},
			{Key: "history_store", Label: "History Store", Type: FieldTypeString, Required: true, Description: "DeployHistoryStore service name"},
			{Key: "target_version", Label: "Target Version", Type: FieldTypeString, Description: "Version to roll back to", DefaultValue: "previous"},
			{Key: "health_check", Label: "Health Check", Type: FieldTypeMap, Description: "Health check config: {path, timeout}"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.deploy_rolling",
		Label:       "Deploy Rolling",
		Category:    "deployment",
		Description: "Deploys an image using rolling update strategy, replacing instances one-by-one with health checks",
		ConfigFields: []ConfigFieldDef{
			{Key: "service", Label: "Service", Type: FieldTypeString, Required: true, Description: "DeployDriver service name"},
			{Key: "image", Label: "Image", Type: FieldTypeString, Required: true, Description: "Docker image to deploy"},
			{Key: "max_surge", Label: "Max Surge", Type: FieldTypeNumber, Description: "Maximum instances above desired count", DefaultValue: 1},
			{Key: "max_unavailable", Label: "Max Unavailable", Type: FieldTypeNumber, Description: "Maximum unavailable instances during update", DefaultValue: 1},
			{Key: "health_check", Label: "Health Check", Type: FieldTypeMap, Description: "Health check config: {path, interval, timeout}"},
			{Key: "rollback_on_failure", Label: "Rollback on Failure", Type: FieldTypeBool, Description: "Automatically rollback if health checks fail"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.deploy_verify",
		Label:       "Deploy Verify",
		Category:    "deployment",
		Description: "Runs HTTP and/or metrics checks against a service to confirm it is healthy after a deployment",
		ConfigFields: []ConfigFieldDef{
			{Key: "service", Label: "Service", Type: FieldTypeString, Required: true, Description: "DeployDriver service name"},
			{Key: "checks", Label: "Checks", Type: FieldTypeArray, Required: true, Description: "Verification checks: [{type, path, expected_status, threshold, window}]"},
		},
	})
}
