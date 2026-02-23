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
		Description:  "Reverse proxy using the CrisisTextLine/modular reverseproxy module",
		Inputs:       []ServiceIODef{{Name: "request", Type: "http.Request", Description: "HTTP request to proxy"}},
		Outputs:      []ServiceIODef{{Name: "proxied", Type: "http.Response", Description: "Proxied HTTP response"}},
		ConfigFields: []ConfigFieldDef{},
	})

	r.Register(&ModuleSchema{
		Type:         "reverseproxy",
		Label:        "Reverse Proxy",
		Category:     "http",
		Description:  "Reverse proxy using the CrisisTextLine/modular reverseproxy module",
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
			{Key: "routes", Label: "Route Pipelines", Type: FieldTypeJSON, Description: "Per-route processing pipelines with composable steps (validate, transform, http_call, etc.)", Group: "routes"},
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
			{Key: "routes", Label: "Route Pipelines", Type: FieldTypeJSON, Description: "Per-route processing pipelines with composable steps (validate, transform, http_call, etc.)", Group: "routes"},
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
		Description:  "CrisisTextLine/modular scheduler for cron-based job execution",
		Inputs:       []ServiceIODef{{Name: "job", Type: "func()", Description: "Job function to schedule"}},
		Outputs:      []ServiceIODef{{Name: "scheduler", Type: "Scheduler", Description: "Scheduler service for registering cron jobs"}},
		ConfigFields: []ConfigFieldDef{},
		MaxIncoming:  intPtr(0),
	})

	r.Register(&ModuleSchema{
		Type:         "cache.modular",
		Label:        "Cache",
		Category:     "infrastructure",
		Description:  "CrisisTextLine/modular caching module (use config feeders for provider settings)",
		Inputs:       []ServiceIODef{{Name: "key", Type: "string", Description: "Cache key for get/set operations"}},
		Outputs:      []ServiceIODef{{Name: "cache", Type: "Cache", Description: "Cache service for key-value storage"}},
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
			{Key: "schema", Label: "JSON Schema", Type: FieldTypeJSON, Description: "JSON Schema definition for validation (when strategy is json_schema)"},
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
			{Key: "operations", Label: "Operations", Type: FieldTypeJSON, Description: "Inline transformation operations (alternative to transformer+pipeline)"},
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
			{Key: "payload", Label: "Payload", Type: FieldTypeJSON, Description: "Custom payload template (uses {{ .field }} expressions). Defaults to pipeline context."},
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
			{Key: "url", Label: "URL", Type: FieldTypeString, Required: true, Description: "Request URL (supports {{ .field }} templates)", Placeholder: "https://api.example.com/{{ .resource }}"},
			{Key: "method", Label: "Method", Type: FieldTypeSelect, Options: []string{"GET", "POST", "PUT", "PATCH", "DELETE"}, DefaultValue: "GET", Description: "HTTP method"},
			{Key: "headers", Label: "Headers", Type: FieldTypeMap, MapValueType: "string", Description: "Request headers (values support templates)"},
			{Key: "body", Label: "Body", Type: FieldTypeJSON, Description: "Request body (supports templates). For POST/PUT without body, sends pipeline context."},
			{Key: "timeout", Label: "Timeout", Type: FieldTypeString, DefaultValue: "30s", Description: "Request timeout duration", Placeholder: "30s"},
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
			{Key: "query", Label: "SQL Query", Type: FieldTypeSQL, Required: true, Description: "Parameterized SQL SELECT query (use ? for placeholders, no template expressions allowed)", Placeholder: "SELECT id, name FROM companies WHERE id = ?"},
			{Key: "params", Label: "Parameters", Type: FieldTypeArray, ArrayItemType: "string", Description: "Template-resolved parameter values for ? placeholders in query"},
			{Key: "mode", Label: "Mode", Type: FieldTypeSelect, Options: []string{"list", "single"}, DefaultValue: "list", Description: "Result mode: 'list' returns rows/count, 'single' returns row/found"},
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
			{Key: "query", Label: "SQL Statement", Type: FieldTypeSQL, Required: true, Description: "Parameterized SQL INSERT/UPDATE/DELETE statement (use ? for placeholders)", Placeholder: "INSERT INTO companies (id, name) VALUES (?, ?)"},
			{Key: "params", Label: "Parameters", Type: FieldTypeArray, ArrayItemType: "string", Description: "Template-resolved parameter values for ? placeholders"},
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
			{Key: "headers", Label: "Headers", Type: FieldTypeMap, MapValueType: "string", Description: "Additional response headers"},
			{Key: "body", Label: "Body", Type: FieldTypeJSON, Description: "Response body as JSON (supports template expressions)"},
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
			{Key: "artifacts_out", Label: "Output Artifacts", Type: FieldTypeJSON, Description: "Artifacts to collect after execution (array of {key, path})"},
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
			{Key: "health_check", Label: "Health Check", Type: FieldTypeJSON, Description: "Health check configuration (path, interval, timeout, thresholds)"},
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
			{Key: "schedule", Label: "Schedule Window", Type: FieldTypeJSON, Description: "Time window for scheduled gates (weekdays, start_hour, end_hour)"},
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
			{Key: "env", Label: "Environment Variables", Type: FieldTypeJSON, Description: "Extra environment variables for the build process"},
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
			{Key: "routes", Label: "Routes", Type: FieldTypeJSON, Required: true, Description: "Array of route definitions with pathPrefix, backend, methods, rateLimit, auth, timeout"},
			{Key: "globalRateLimit", Label: "Global Rate Limit", Type: FieldTypeJSON, Description: "Global rate limit applied to all routes (requestsPerMinute, burstSize)"},
			{Key: "cors", Label: "CORS Config", Type: FieldTypeJSON, Description: "CORS settings (allowOrigins, allowMethods, allowHeaders, maxAge)"},
			{Key: "auth", Label: "Auth Config", Type: FieldTypeJSON, Description: "Authentication settings (type: bearer/api_key/basic, header)"},
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
			{Key: "input_mapping", Label: "Input Mapping", Type: FieldTypeJSON, Description: "Map of sub-workflow input keys to template expressions"},
			{Key: "output_mapping", Label: "Output Mapping", Type: FieldTypeJSON, Description: "Map of parent context keys to sub-workflow output paths"},
			{Key: "timeout", Label: "Timeout", Type: FieldTypeDuration, DefaultValue: "30s", Description: "Maximum execution time for the sub-workflow"},
		},
		DefaultConfig: map[string]any{"timeout": "30s"},
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
			{Key: "schema", Label: "Extraction Schema", Type: FieldTypeJSON, Required: true, Description: "JSON schema defining the fields to extract"},
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
			{Key: "tiers", Label: "Tier Configuration", Type: FieldTypeJSON, Description: "Three-tier infrastructure layout (infrastructure, shared_primitives, application)"},
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
			{Key: "capabilities", Label: "Capabilities", Type: FieldTypeJSON, Description: "Provider-agnostic capability properties (replicas, memory, ports, etc.)"},
			{Key: "constraints", Label: "Constraints", Type: FieldTypeJSON, Description: "Hard limits imposed by parent tiers"},
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
			{Key: "constraints", Label: "Constraints", Type: FieldTypeJSON, Description: "List of constraint definitions (field, operator, value)"},
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
}
