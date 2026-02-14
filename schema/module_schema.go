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
)

// ConfigFieldDef describes a single configuration field for a module type.
type ConfigFieldDef struct {
	Key          string          `json:"key"`
	Label        string          `json:"label"`
	Type         ConfigFieldType `json:"type"`
	Description  string          `json:"description,omitempty"`
	Required     bool            `json:"required,omitempty"`
	DefaultValue any             `json:"defaultValue,omitempty"`
	Options      []string        `json:"options,omitempty"` // for select type
	Placeholder  string          `json:"placeholder,omitempty"`
	Group        string          `json:"group,omitempty"` // field grouping in UI
}

// ModuleSchema describes the full configuration schema for a module type.
type ModuleSchema struct {
	Type          string           `json:"type"`
	Label         string           `json:"label"`
	Category      string           `json:"category"`
	Description   string           `json:"description,omitempty"`
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
		ConfigFields: []ConfigFieldDef{},
	})

	r.Register(&ModuleSchema{
		Type:        "http.handler",
		Label:       "HTTP Handler",
		Category:    "http",
		Description: "Handles HTTP requests and produces responses",
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
		ConfigFields: []ConfigFieldDef{},
	})

	r.Register(&ModuleSchema{
		Type:         "reverseproxy",
		Label:        "Reverse Proxy",
		Category:     "http",
		Description:  "Reverse proxy using the CrisisTextLine/modular reverseproxy module",
		ConfigFields: []ConfigFieldDef{},
	})

	r.Register(&ModuleSchema{
		Type:        "http.simple_proxy",
		Label:       "Simple Proxy",
		Category:    "http",
		Description: "Simple reverse proxy with prefix-based target routing",
		ConfigFields: []ConfigFieldDef{
			{Key: "targets", Label: "Targets", Type: FieldTypeJSON, Description: "Map of URL prefix to backend URL (e.g. {\"/api\": \"http://localhost:3000\"})", Placeholder: "{\"/api\": \"http://backend:8080\"}"},
		},
	})

	r.Register(&ModuleSchema{
		Type:        "static.fileserver",
		Label:       "Static File Server",
		Category:    "http",
		Description: "Serves static files from a directory with optional SPA fallback",
		ConfigFields: []ConfigFieldDef{
			{Key: "root", Label: "Root Directory", Type: FieldTypeString, Required: true, Description: "Path to the directory containing static files", Placeholder: "./ui/dist"},
			{Key: "prefix", Label: "URL Prefix", Type: FieldTypeString, DefaultValue: "/", Description: "URL path prefix to serve files under", Placeholder: "/"},
			{Key: "spaFallback", Label: "SPA Fallback", Type: FieldTypeBool, DefaultValue: true, Description: "When enabled, serves index.html for unmatched paths (for single-page apps)"},
			{Key: "cacheMaxAge", Label: "Cache Max-Age (sec)", Type: FieldTypeNumber, DefaultValue: 3600, Description: "Cache-Control max-age in seconds for static assets"},
			{Key: "router", Label: "Router Name", Type: FieldTypeString, Description: "Explicit router module name to register on (auto-detected if omitted)", Placeholder: "my-router"},
		},
		DefaultConfig: map[string]any{"prefix": "/", "spaFallback": true, "cacheMaxAge": 3600},
	})

	// ---- API Category ----

	r.Register(&ModuleSchema{
		Type:        "api.handler",
		Label:       "REST API Handler",
		Category:    "http",
		Description: "Full REST API handler for a resource, with optional state machine integration",
		ConfigFields: []ConfigFieldDef{
			{Key: "resourceName", Label: "Resource Name", Type: FieldTypeString, Description: "Name of the resource to manage (e.g. orders, users)", DefaultValue: "resources", Placeholder: "orders"},
			{Key: "workflowType", Label: "Workflow Type", Type: FieldTypeString, Description: "Workflow type for state machine operations", Placeholder: "order-processing"},
			{Key: "workflowEngine", Label: "Workflow Engine", Type: FieldTypeString, Description: "Name of the workflow engine service to use", Placeholder: "statemachine-engine"},
			{Key: "initialTransition", Label: "Initial Transition", Type: FieldTypeString, Description: "State transition to trigger after resource creation", Placeholder: "submit"},
			{Key: "seedFile", Label: "Seed Data File", Type: FieldTypeString, Description: "Path to a JSON file with initial resource data", Placeholder: "data/seed.json"},
			{Key: "sourceResourceName", Label: "Source Resource", Type: FieldTypeString, Description: "Alternative resource name to read from (for derived views)"},
			{Key: "stateFilter", Label: "State Filter", Type: FieldTypeString, Description: "Only show resources in this state", Placeholder: "active"},
			{Key: "fieldMapping", Label: "Field Mapping", Type: FieldTypeJSON, Description: "Custom field name mapping (e.g. {\"id\": \"order_id\", \"status\": \"state\"})", Group: "advanced"},
			{Key: "transitionMap", Label: "Transition Map", Type: FieldTypeJSON, Description: "Map of sub-action names to state transitions (e.g. {\"approve\": \"approved\"})", Group: "advanced"},
			{Key: "summaryFields", Label: "Summary Fields", Type: FieldTypeJSON, Description: "Array of field names to include in list/summary responses (e.g. [\"name\",\"status\"])", Group: "advanced"},
		},
		DefaultConfig: map[string]any{"resourceName": "resources"},
	})

	// ---- Middleware Category ----

	r.Register(&ModuleSchema{
		Type:        "http.middleware.auth",
		Label:       "Auth Middleware",
		Category:    "middleware",
		Description: "Authentication middleware that validates tokens on incoming requests",
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
		ConfigFields: []ConfigFieldDef{
			{Key: "allowedOrigins", Label: "Allowed Origins", Type: FieldTypeJSON, DefaultValue: []string{"*"}, Description: "Array of allowed origins (e.g. [\"https://example.com\", \"http://localhost:3000\"])", Placeholder: "[\"*\"]"},
			{Key: "allowedMethods", Label: "Allowed Methods", Type: FieldTypeJSON, DefaultValue: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}, Description: "Array of allowed HTTP methods", Placeholder: "[\"GET\",\"POST\",\"PUT\",\"DELETE\",\"OPTIONS\"]"},
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
		ConfigFields: []ConfigFieldDef{},
	})

	r.Register(&ModuleSchema{
		Type:        "http.middleware.securityheaders",
		Label:       "Security Headers",
		Category:    "middleware",
		Description: "Adds security-related HTTP headers to responses",
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
		ConfigFields: []ConfigFieldDef{},
	})

	r.Register(&ModuleSchema{
		Type:         "messaging.broker.eventbus",
		Label:        "EventBus Bridge",
		Category:     "messaging",
		Description:  "Bridges the modular EventBus to the messaging subsystem",
		ConfigFields: []ConfigFieldDef{},
	})

	r.Register(&ModuleSchema{
		Type:         "messaging.handler",
		Label:        "Message Handler",
		Category:     "messaging",
		Description:  "Handles messages from topics/queues",
		ConfigFields: []ConfigFieldDef{},
	})

	r.Register(&ModuleSchema{
		Type:         "messaging.nats",
		Label:        "NATS Broker",
		Category:     "messaging",
		Description:  "NATS message broker for distributed pub/sub",
		ConfigFields: []ConfigFieldDef{},
	})

	r.Register(&ModuleSchema{
		Type:        "messaging.kafka",
		Label:       "Kafka Broker",
		Category:    "messaging",
		Description: "Apache Kafka message broker for high-throughput streaming",
		ConfigFields: []ConfigFieldDef{
			{Key: "brokers", Label: "Broker Addresses", Type: FieldTypeJSON, Description: "Array of Kafka broker addresses", Placeholder: "[\"localhost:9092\"]"},
			{Key: "groupId", Label: "Consumer Group ID", Type: FieldTypeString, Description: "Kafka consumer group identifier", Placeholder: "my-consumer-group"},
		},
	})

	// ---- State Machine Category ----

	r.Register(&ModuleSchema{
		Type:         "statemachine.engine",
		Label:        "State Machine Engine",
		Category:     "statemachine",
		Description:  "Manages workflow state transitions and lifecycle",
		ConfigFields: []ConfigFieldDef{},
	})

	r.Register(&ModuleSchema{
		Type:         "state.tracker",
		Label:        "State Tracker",
		Category:     "statemachine",
		Description:  "Tracks and persists workflow instance state",
		ConfigFields: []ConfigFieldDef{},
	})

	r.Register(&ModuleSchema{
		Type:         "state.connector",
		Label:        "State Connector",
		Category:     "statemachine",
		Description:  "Connects state machine engine to state tracker for persistence",
		ConfigFields: []ConfigFieldDef{},
	})

	// ---- Infrastructure / Modular Framework Category ----

	r.Register(&ModuleSchema{
		Type:         "httpserver.modular",
		Label:        "Modular HTTP Server",
		Category:     "infrastructure",
		Description:  "CrisisTextLine/modular HTTP server module (use config feeders for settings)",
		ConfigFields: []ConfigFieldDef{},
	})

	r.Register(&ModuleSchema{
		Type:         "scheduler.modular",
		Label:        "Scheduler",
		Category:     "scheduling",
		Description:  "CrisisTextLine/modular scheduler for cron-based job execution",
		ConfigFields: []ConfigFieldDef{},
	})

	r.Register(&ModuleSchema{
		Type:         "auth.modular",
		Label:        "Auth Service",
		Category:     "infrastructure",
		Description:  "CrisisTextLine/modular authentication service module",
		ConfigFields: []ConfigFieldDef{},
	})

	r.Register(&ModuleSchema{
		Type:         "eventbus.modular",
		Label:        "Event Bus",
		Category:     "events",
		Description:  "CrisisTextLine/modular in-process event bus for pub/sub",
		ConfigFields: []ConfigFieldDef{},
	})

	r.Register(&ModuleSchema{
		Type:         "cache.modular",
		Label:        "Cache",
		Category:     "infrastructure",
		Description:  "CrisisTextLine/modular caching module (use config feeders for provider settings)",
		ConfigFields: []ConfigFieldDef{},
	})

	r.Register(&ModuleSchema{
		Type:         "chimux.router",
		Label:        "Chi Mux Router",
		Category:     "http",
		Description:  "CrisisTextLine/modular Chi-based HTTP router",
		ConfigFields: []ConfigFieldDef{},
	})

	r.Register(&ModuleSchema{
		Type:         "eventlogger.modular",
		Label:        "Event Logger",
		Category:     "events",
		Description:  "CrisisTextLine/modular event logging module",
		ConfigFields: []ConfigFieldDef{},
	})

	r.Register(&ModuleSchema{
		Type:         "httpclient.modular",
		Label:        "HTTP Client",
		Category:     "integration",
		Description:  "CrisisTextLine/modular HTTP client for outbound requests",
		ConfigFields: []ConfigFieldDef{},
	})

	r.Register(&ModuleSchema{
		Type:         "database.modular",
		Label:        "Database",
		Category:     "database",
		Description:  "CrisisTextLine/modular database module (use config feeders for driver/DSN)",
		ConfigFields: []ConfigFieldDef{},
	})

	r.Register(&ModuleSchema{
		Type:         "jsonschema.modular",
		Label:        "JSON Schema Validator",
		Category:     "infrastructure",
		Description:  "CrisisTextLine/modular JSON Schema validation module",
		ConfigFields: []ConfigFieldDef{},
	})

	// ---- Database Category ----

	r.Register(&ModuleSchema{
		Type:        "database.workflow",
		Label:       "Workflow Database",
		Category:    "database",
		Description: "SQL database for workflow state persistence (supports PostgreSQL, MySQL, SQLite)",
		ConfigFields: []ConfigFieldDef{
			{Key: "driver", Label: "Driver", Type: FieldTypeSelect, Options: []string{"postgres", "mysql", "sqlite3"}, Required: true, Description: "Database driver to use"},
			{Key: "dsn", Label: "DSN", Type: FieldTypeString, Required: true, Description: "Data source name / connection string", Placeholder: "postgres://user:pass@localhost/db?sslmode=disable"},
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
		ConfigFields: []ConfigFieldDef{
			{Key: "database", Label: "Database Service", Type: FieldTypeString, DefaultValue: "database", Description: "Name of the database module to use for storage", Placeholder: "database"},
		},
		DefaultConfig: map[string]any{"database": "database"},
	})

	// ---- Observability Category ----

	r.Register(&ModuleSchema{
		Type:         "metrics.collector",
		Label:        "Metrics Collector",
		Category:     "observability",
		Description:  "Collects and exposes application metrics",
		ConfigFields: []ConfigFieldDef{},
	})

	r.Register(&ModuleSchema{
		Type:         "health.checker",
		Label:        "Health Checker",
		Category:     "observability",
		Description:  "Health check endpoint for liveness/readiness probes",
		ConfigFields: []ConfigFieldDef{},
	})

	r.Register(&ModuleSchema{
		Type:         "observability.otel",
		Label:        "OpenTelemetry",
		Category:     "observability",
		Description:  "OpenTelemetry tracing integration for distributed tracing",
		ConfigFields: []ConfigFieldDef{},
	})

	// ---- Authentication Category ----

	r.Register(&ModuleSchema{
		Type:        "auth.jwt",
		Label:       "JWT Auth",
		Category:    "middleware",
		Description: "JWT-based authentication with token signing, verification, and user management",
		ConfigFields: []ConfigFieldDef{
			{Key: "secret", Label: "JWT Secret", Type: FieldTypeString, Required: true, Description: "Secret key for signing JWT tokens (supports $ENV_VAR expansion)", Placeholder: "$JWT_SECRET"},
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
		ConfigFields: []ConfigFieldDef{},
	})

	r.Register(&ModuleSchema{
		Type:        "webhook.sender",
		Label:       "Webhook Sender",
		Category:    "integration",
		Description: "Sends HTTP webhooks with retry and exponential backoff",
		ConfigFields: []ConfigFieldDef{
			{Key: "maxRetries", Label: "Max Retries", Type: FieldTypeNumber, DefaultValue: 3, Description: "Maximum number of retry attempts on failure"},
		},
		DefaultConfig: map[string]any{"maxRetries": 3},
	})

	r.Register(&ModuleSchema{
		Type:         "notification.slack",
		Label:        "Slack Notification",
		Category:     "integration",
		Description:  "Sends notifications to Slack channels via webhooks",
		ConfigFields: []ConfigFieldDef{},
	})

	r.Register(&ModuleSchema{
		Type:         "storage.s3",
		Label:        "S3 Storage",
		Category:     "integration",
		Description:  "Amazon S3 compatible object storage integration",
		ConfigFields: []ConfigFieldDef{},
	})

	// ---- Dynamic Component ----

	r.Register(&ModuleSchema{
		Type:        "dynamic.component",
		Label:       "Dynamic Component",
		Category:    "infrastructure",
		Description: "Loads a dynamic component from the registry or source file at runtime",
		ConfigFields: []ConfigFieldDef{
			{Key: "componentId", Label: "Component ID", Type: FieldTypeString, Description: "ID to look up in the dynamic component registry (defaults to module name)"},
			{Key: "source", Label: "Source File", Type: FieldTypeString, Description: "Path to Go source file to load dynamically", Placeholder: "components/my_processor.go"},
			{Key: "provides", Label: "Provides Services", Type: FieldTypeJSON, Description: "Array of service names this component provides", Placeholder: "[\"my-service\"]"},
			{Key: "requires", Label: "Requires Services", Type: FieldTypeJSON, Description: "Array of service names this component depends on", Placeholder: "[\"database\"]"},
		},
	})

	// ---- Processing Category ----

	r.Register(&ModuleSchema{
		Type:        "processing.step",
		Label:       "Processing Step",
		Category:    "statemachine",
		Description: "Executes a component as a processing step in a workflow, with retry and compensation",
		ConfigFields: []ConfigFieldDef{
			{Key: "componentId", Label: "Component ID", Type: FieldTypeString, Required: true, Description: "Service name of the component to execute"},
			{Key: "successTransition", Label: "Success Transition", Type: FieldTypeString, Description: "State transition to trigger on success", Placeholder: "completed"},
			{Key: "compensateTransition", Label: "Compensate Transition", Type: FieldTypeString, Description: "State transition to trigger on failure for compensation", Placeholder: "failed"},
			{Key: "maxRetries", Label: "Max Retries", Type: FieldTypeNumber, DefaultValue: 2, Description: "Maximum number of retry attempts"},
			{Key: "retryBackoffMs", Label: "Retry Backoff (ms)", Type: FieldTypeNumber, DefaultValue: 1000, Description: "Base backoff duration in milliseconds between retries"},
			{Key: "timeoutSeconds", Label: "Timeout (sec)", Type: FieldTypeNumber, DefaultValue: 30, Description: "Maximum execution time per attempt in seconds"},
		},
		DefaultConfig: map[string]any{"maxRetries": 2, "retryBackoffMs": 1000, "timeoutSeconds": 30},
	})
}
