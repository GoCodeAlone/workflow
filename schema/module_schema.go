package schema

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
		Type:         "messaging.broker",
		Label:        "In-Memory Message Broker",
		Category:     "messaging",
		Description:  "Simple in-memory message broker for local pub/sub",
		Inputs:       []ServiceIODef{{Name: "message", Type: "[]byte", Description: "Message to publish"}},
		Outputs:      []ServiceIODef{{Name: "message", Type: "[]byte", Description: "Delivered message to subscriber"}},
		ConfigFields: []ConfigFieldDef{},
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
		Type:         "statemachine.engine",
		Label:        "State Machine Engine",
		Category:     "statemachine",
		Description:  "Manages workflow state transitions and lifecycle",
		Inputs:       []ServiceIODef{{Name: "event", Type: "Event", Description: "Event triggering a state transition"}},
		Outputs:      []ServiceIODef{{Name: "transition", Type: "Transition", Description: "Completed state transition result"}},
		ConfigFields: []ConfigFieldDef{},
	})

	r.Register(&ModuleSchema{
		Type:         "state.tracker",
		Label:        "State Tracker",
		Category:     "statemachine",
		Description:  "Tracks and persists workflow instance state",
		Inputs:       []ServiceIODef{{Name: "state", Type: "State", Description: "State update to track"}},
		Outputs:      []ServiceIODef{{Name: "tracked", Type: "State", Description: "Tracked state with persistence"}},
		ConfigFields: []ConfigFieldDef{},
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
		Type:         "httpserver.modular",
		Label:        "Modular HTTP Server",
		Category:     "infrastructure",
		Description:  "CrisisTextLine/modular HTTP server module (use config feeders for settings)",
		Outputs:      []ServiceIODef{{Name: "http-server", Type: "net.Listener", Description: "HTTP server accepting connections"}},
		ConfigFields: []ConfigFieldDef{},
	})

	r.Register(&ModuleSchema{
		Type:         "scheduler.modular",
		Label:        "Scheduler",
		Category:     "scheduling",
		Description:  "CrisisTextLine/modular scheduler for cron-based job execution",
		Inputs:       []ServiceIODef{{Name: "job", Type: "func()", Description: "Job function to schedule"}},
		Outputs:      []ServiceIODef{{Name: "scheduler", Type: "Scheduler", Description: "Scheduler service for registering cron jobs"}},
		ConfigFields: []ConfigFieldDef{},
	})

	r.Register(&ModuleSchema{
		Type:         "auth.modular",
		Label:        "Auth Service",
		Category:     "infrastructure",
		Description:  "CrisisTextLine/modular authentication service module",
		Inputs:       []ServiceIODef{{Name: "credentials", Type: "Credentials", Description: "User credentials to authenticate"}},
		Outputs:      []ServiceIODef{{Name: "auth-service", Type: "AuthService", Description: "Authentication and authorization service"}},
		ConfigFields: []ConfigFieldDef{},
	})

	r.Register(&ModuleSchema{
		Type:         "eventbus.modular",
		Label:        "Event Bus",
		Category:     "events",
		Description:  "CrisisTextLine/modular in-process event bus for pub/sub",
		Inputs:       []ServiceIODef{{Name: "event", Type: "CloudEvent", Description: "CloudEvent to publish"}},
		Outputs:      []ServiceIODef{{Name: "eventbus", Type: "EventBus", Description: "In-process event bus for pub/sub"}},
		ConfigFields: []ConfigFieldDef{},
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

	r.Register(&ModuleSchema{
		Type:         "chimux.router",
		Label:        "Chi Mux Router",
		Category:     "http",
		Description:  "CrisisTextLine/modular Chi-based HTTP router",
		Inputs:       []ServiceIODef{{Name: "request", Type: "http.Request", Description: "HTTP request to route via Chi mux"}},
		Outputs:      []ServiceIODef{{Name: "routed", Type: "http.Request", Description: "Routed HTTP request dispatched to handler"}},
		ConfigFields: []ConfigFieldDef{},
	})

	r.Register(&ModuleSchema{
		Type:         "eventlogger.modular",
		Label:        "Event Logger",
		Category:     "events",
		Description:  "CrisisTextLine/modular event logging module",
		Inputs:       []ServiceIODef{{Name: "event", Type: "CloudEvent", Description: "CloudEvent to log"}},
		Outputs:      []ServiceIODef{{Name: "logged", Type: "LogEntry", Description: "Logged event record"}},
		ConfigFields: []ConfigFieldDef{},
	})

	r.Register(&ModuleSchema{
		Type:         "httpclient.modular",
		Label:        "HTTP Client",
		Category:     "integration",
		Description:  "CrisisTextLine/modular HTTP client for outbound requests",
		Inputs:       []ServiceIODef{{Name: "request", Type: "http.Request", Description: "Outbound HTTP request to send"}},
		Outputs:      []ServiceIODef{{Name: "http-client", Type: "http.Client", Description: "HTTP client for making outbound requests"}},
		ConfigFields: []ConfigFieldDef{},
	})

	r.Register(&ModuleSchema{
		Type:         "database.modular",
		Label:        "Database",
		Category:     "database",
		Description:  "CrisisTextLine/modular database module (use config feeders for driver/DSN)",
		Inputs:       []ServiceIODef{{Name: "query", Type: "SQL", Description: "SQL query to execute"}},
		Outputs:      []ServiceIODef{{Name: "database", Type: "sql.DB", Description: "Database connection pool"}},
		ConfigFields: []ConfigFieldDef{},
	})

	r.Register(&ModuleSchema{
		Type:         "jsonschema.modular",
		Label:        "JSON Schema Validator",
		Category:     "infrastructure",
		Description:  "CrisisTextLine/modular JSON Schema validation module",
		Inputs:       []ServiceIODef{{Name: "document", Type: "JSON", Description: "JSON document to validate"}},
		Outputs:      []ServiceIODef{{Name: "validator", Type: "SchemaValidator", Description: "JSON Schema validation service"}},
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
			{Key: "dsn", Label: "DSN", Type: FieldTypeString, Required: true, Description: "Data source name / connection string", Placeholder: "postgres://user:pass@localhost/db?sslmode=disable", Sensitive: true},
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
		Type:         "metrics.collector",
		Label:        "Metrics Collector",
		Category:     "observability",
		Description:  "Collects and exposes application metrics",
		Outputs:      []ServiceIODef{{Name: "metrics", Type: "prometheus.Metrics", Description: "Prometheus metrics endpoint"}},
		ConfigFields: []ConfigFieldDef{},
	})

	r.Register(&ModuleSchema{
		Type:         "health.checker",
		Label:        "Health Checker",
		Category:     "observability",
		Description:  "Health check endpoint for liveness/readiness probes",
		Outputs:      []ServiceIODef{{Name: "health", Type: "HealthStatus", Description: "Health check status endpoint"}},
		ConfigFields: []ConfigFieldDef{},
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
			{Key: "credentialsFile", Label: "Credentials File", Type: FieldTypeFilePath, Description: "Path to service account JSON key file", Placeholder: "credentials/gcs-key.json"},
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
		ConfigFields: []ConfigFieldDef{
			{Key: "values", Label: "Values", Type: FieldTypeMap, MapValueType: "string", Required: true, Description: "Key-value pairs to set (values support {{ .field }} templates)"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "step.log",
		Label:       "Log",
		Category:    "pipeline",
		Description: "Logs a message at a specified level during pipeline execution",
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
		ConfigFields: []ConfigFieldDef{
			{Key: "url", Label: "URL", Type: FieldTypeString, Required: true, Description: "Request URL (supports {{ .field }} templates)", Placeholder: "https://api.example.com/{{ .resource }}"},
			{Key: "method", Label: "Method", Type: FieldTypeSelect, Options: []string{"GET", "POST", "PUT", "PATCH", "DELETE"}, DefaultValue: "GET", Description: "HTTP method"},
			{Key: "headers", Label: "Headers", Type: FieldTypeMap, MapValueType: "string", Description: "Request headers (values support templates)"},
			{Key: "body", Label: "Body", Type: FieldTypeJSON, Description: "Request body (supports templates). For POST/PUT without body, sends pipeline context."},
			{Key: "timeout", Label: "Timeout", Type: FieldTypeString, DefaultValue: "30s", Description: "Request timeout duration", Placeholder: "30s"},
		},
	})
}
