package schema

// Snippet represents a code snippet for IDE support.
// Body uses ${N:placeholder} VSCode-style syntax.
type Snippet struct {
	Name        string   // human-readable name
	Prefix      string   // trigger prefix / keyword
	Description string   // short description shown in IDE
	Body        []string // lines of the snippet body
}

// GetSnippets returns the canonical set of workflow configuration snippets.
func GetSnippets() []Snippet {
	return []Snippet{
		// ---------------------------------------------------------------
		// Module snippets (10)
		// ---------------------------------------------------------------
		{
			Name:        "HTTP Server Module",
			Prefix:      "mod-http-server",
			Description: "HTTP server module listening on a configurable address",
			Body: []string{
				"- name: ${1:server}",
				"  type: http.server",
				"  config:",
				"    address: ${2::8080}",
			},
		},
		{
			Name:        "HTTP Router Module",
			Prefix:      "mod-http-router",
			Description: "HTTP router module that dispatches requests to handlers",
			Body: []string{
				"- name: ${1:router}",
				"  type: http.router",
				"  dependsOn:",
				"    - ${2:server}",
			},
		},
		{
			Name:        "SQLite Storage Module",
			Prefix:      "mod-sqlite",
			Description: "SQLite database storage module",
			Body: []string{
				"- name: ${1:db}",
				"  type: storage.sqlite",
				"  config:",
				"    dbPath: ${2:data/app.db}",
				"    walMode: ${3:true}",
			},
		},
		{
			Name:        "NoSQL Memory Module",
			Prefix:      "mod-nosql",
			Description: "In-memory NoSQL document store",
			Body: []string{
				"- name: ${1:store}",
				"  type: nosql.memory",
				"  config:",
				"    collection: ${2:documents}",
			},
		},
		{
			Name:        "JWT Auth Module",
			Prefix:      "mod-jwt",
			Description: "JWT authentication module",
			Body: []string{
				"- name: ${1:auth}",
				"  type: auth.jwt",
				"  config:",
				"    secret: ${2:my-secret-key}",
				"    tokenExpiry: ${3:24h}",
				"    issuer: ${4:my-app}",
			},
		},
		{
			Name:        "Messaging Broker Module",
			Prefix:      "mod-broker",
			Description: "In-process messaging broker for pub/sub",
			Body: []string{
				"- name: ${1:broker}",
				"  type: messaging.broker",
				"  config:",
				"    maxQueueSize: ${2:1000}",
			},
		},
		{
			Name:        "State Machine Module",
			Prefix:      "mod-statemachine",
			Description: "State machine engine for workflow orchestration",
			Body: []string{
				"- name: ${1:state-engine}",
				"  type: statemachine.engine",
				"  config:",
				"    maxInstances: ${2:100}",
				"    instanceTTL: ${3:24h}",
			},
		},
		{
			Name:        "OTEL Observability Module",
			Prefix:      "mod-otel",
			Description: "OpenTelemetry observability module",
			Body: []string{
				"- name: ${1:otel}",
				"  type: observability.otel",
				"  config:",
				"    endpoint: ${2:localhost:4317}",
				"    serviceName: ${3:my-service}",
			},
		},
		{
			Name:        "Cache Modular Module",
			Prefix:      "mod-cache",
			Description: "Modular cache adapter",
			Body: []string{
				"- name: ${1:cache}",
				"  type: cache.modular",
			},
		},
		{
			Name:        "Secrets Vault Module",
			Prefix:      "mod-secrets",
			Description: "HashiCorp Vault secrets module",
			Body: []string{
				"- name: ${1:secrets}",
				"  type: secrets.vault",
				"  config:",
				"    mode: ${2:dev}",
				"    address: ${3:http://localhost:8200}",
				"    token: ${4:root}",
				"    mountPath: ${5:secret}",
			},
		},

		// ---------------------------------------------------------------
		// Pipeline scaffold (1)
		// ---------------------------------------------------------------
		{
			Name:        "Pipeline Scaffold",
			Prefix:      "pipeline",
			Description: "Full pipeline definition with trigger and steps",
			Body: []string{
				"${1:my-pipeline}:",
				"  trigger:",
				"    type: ${2:http}",
				"    config:",
				"      path: ${3:/api/v1/resource}",
				"      method: ${4:POST}",
				"  steps:",
				"    - type: step.validate",
				"      config:",
				"        required:",
				"          - ${5:field}",
				"    - type: step.json_response",
				"      config:",
				"        status: ${6:200}",
				"        body: ${7:{ \"ok\": true }}",
			},
		},

		// ---------------------------------------------------------------
		// Step snippets (12+)
		// ---------------------------------------------------------------
		{
			Name:        "Step: Set Variable",
			Prefix:      "step-set",
			Description: "Set a named variable in the pipeline context",
			Body: []string{
				"- type: step.set",
				"  config:",
				"    values:",
				"      ${1:key}: ${2:value}",
			},
		},
		{
			Name:        "Step: HTTP Call",
			Prefix:      "step-http-call",
			Description: "Make an outbound HTTP request",
			Body: []string{
				"- type: step.http_call",
				"  config:",
				"    url: ${1:https://api.example.com/endpoint}",
				"    method: ${2:POST}",
				"    headers:",
				"      Content-Type: application/json",
				"    body: ${3:{{ json . }}}",
				"    timeout: ${4:30s}",
			},
		},
		{
			Name:        "Step: JSON Response",
			Prefix:      "step-json-response",
			Description: "Return a JSON HTTP response",
			Body: []string{
				"- type: step.json_response",
				"  config:",
				"    status: ${1:200}",
				"    body:",
				"      ${2:message}: ${3:ok}",
			},
		},
		{
			Name:        "Step: Validate",
			Prefix:      "step-validate",
			Description: "Validate request data against rules",
			Body: []string{
				"- type: step.validate",
				"  config:",
				"    required:",
				"      - ${1:field1}",
				"    rules:",
				"      ${2:field1}: ${3:string}",
			},
		},
		{
			Name:        "Step: Transform",
			Prefix:      "step-transform",
			Description: "Transform data using a mapping template",
			Body: []string{
				"- type: step.transform",
				"  config:",
				"    mapping:",
				"      ${1:output_field}: ${2:{{ .input_field }}}",
			},
		},
		{
			Name:        "Step: DB Query",
			Prefix:      "step-db-query",
			Description: "Execute a read-only SQL query",
			Body: []string{
				"- type: step.db_query",
				"  config:",
				"    database: ${1:db}",
				"    query: ${2:SELECT * FROM ${3:table} WHERE id = ?}",
				"    params:",
				"      - ${4:{{ .id }}}",
			},
		},
		{
			Name:        "Step: DB Exec",
			Prefix:      "step-db-exec",
			Description: "Execute a SQL write statement",
			Body: []string{
				"- type: step.db_exec",
				"  config:",
				"    database: ${1:db}",
				"    query: ${2:INSERT INTO ${3:table} (col) VALUES (?)}",
				"    params:",
				"      - ${4:{{ .value }}}",
			},
		},
		{
			Name:        "Step: Auth Required",
			Prefix:      "step-auth",
			Description: "Require authentication and optional role/scope",
			Body: []string{
				"- type: step.auth_required",
				"  config:",
				"    roles:",
				"      - ${1:admin}",
			},
		},
		{
			Name:        "Step: Cache Get",
			Prefix:      "step-cache-get",
			Description: "Get a value from the cache",
			Body: []string{
				"- type: step.cache_get",
				"  config:",
				"    cache: ${1:cache}",
				"    key: ${2:{{ .id }}}",
				"    output: ${3:cached_result}",
			},
		},
		{
			Name:        "Step: Cache Set",
			Prefix:      "step-cache-set",
			Description: "Store a value in the cache with optional TTL",
			Body: []string{
				"- type: step.cache_set",
				"  config:",
				"    cache: ${1:cache}",
				"    key: ${2:{{ .id }}}",
				"    value: ${3:{{ .result }}}",
				"    ttl: ${4:5m}",
			},
		},
		{
			Name:        "Step: Event Publish",
			Prefix:      "step-event-publish",
			Description: "Publish an event to the message broker",
			Body: []string{
				"- type: step.event_publish",
				"  config:",
				"    broker: ${1:broker}",
				"    topic: ${2:events.created}",
				"    event_type: ${3:resource.created}",
				"    payload: ${4:{{ json . }}}",
			},
		},
		{
			Name:        "Step: Log",
			Prefix:      "step-log",
			Description: "Log a message at the specified level",
			Body: []string{
				"- type: step.log",
				"  config:",
				"    level: ${1:info}",
				"    message: ${2:Processing request: {{ .id }}}",
			},
		},

		// ---------------------------------------------------------------
		// Trigger snippets (3)
		// ---------------------------------------------------------------
		{
			Name:        "Trigger: HTTP",
			Prefix:      "trigger-http",
			Description: "HTTP trigger for a specific path and method",
			Body: []string{
				"trigger:",
				"  type: http",
				"  config:",
				"    path: ${1:/api/v1/${2:resource}}",
				"    method: ${3:GET}",
			},
		},
		{
			Name:        "Trigger: Schedule",
			Prefix:      "trigger-schedule",
			Description: "Cron-based schedule trigger",
			Body: []string{
				"trigger:",
				"  type: schedule",
				"  config:",
				"    cron: ${1:0 * * * *}",
				"    timezone: ${2:UTC}",
			},
		},
		{
			Name:        "Trigger: Event",
			Prefix:      "trigger-event",
			Description: "Event-driven trigger subscribing to a topic",
			Body: []string{
				"trigger:",
				"  type: event",
				"  config:",
				"    topic: ${1:events.created}",
				"    broker: ${2:broker}",
			},
		},

		// ---------------------------------------------------------------
		// Workflow snippets (3)
		// ---------------------------------------------------------------
		{
			Name:        "Workflow: HTTP",
			Prefix:      "workflow-http",
			Description: "HTTP workflow handler with route definitions",
			Body: []string{
				"workflows:",
				"  http:",
				"    routes:",
				"      - path: ${1:/api/v1/${2:resource}}",
				"        method: ${3:GET}",
				"        pipeline: ${4:get-resources}",
			},
		},
		{
			Name:        "Workflow: Messaging",
			Prefix:      "workflow-messaging",
			Description: "Messaging workflow handler with topic subscriptions",
			Body: []string{
				"workflows:",
				"  messaging:",
				"    subscriptions:",
				"      - topic: ${1:events.created}",
				"        pipeline: ${2:handle-created}",
			},
		},
		{
			Name:        "Workflow: State Machine",
			Prefix:      "workflow-statemachine",
			Description: "State machine workflow handler",
			Body: []string{
				"workflows:",
				"  statemachine:",
				"    engine: ${1:state-engine}",
				"    states:",
				"      - name: ${2:pending}",
				"        transitions:",
				"          - name: ${3:submit}",
				"            to: ${4:active}",
			},
		},

		// ---------------------------------------------------------------
		// Structural snippets (3)
		// ---------------------------------------------------------------
		{
			Name:        "App Structure",
			Prefix:      "app",
			Description: "Full application config skeleton",
			Body: []string{
				"modules:",
				"  - name: ${1:server}",
				"    type: http.server",
				"    config:",
				"      address: ${2::8080}",
				"  - name: ${3:router}",
				"    type: http.router",
				"    dependsOn:",
				"      - ${1:server}",
				"",
				"workflows:",
				"  http:",
				"    routes: []",
				"",
				"triggers:",
				"  http:",
				"    port: ${4:8080}",
			},
		},
		{
			Name:        "Requires Section",
			Prefix:      "requires",
			Description: "Plugin and version dependency declarations",
			Body: []string{
				"requires:",
				"  version: ${1:v0.2.0}",
				"  plugins:",
				"    - ${2:storage}",
				"    - ${3:auth}",
			},
		},
		{
			Name:        "Imports Section",
			Prefix:      "imports",
			Description: "Import external configuration files",
			Body: []string{
				"imports:",
				"  - ${1:config/modules.yaml}",
				"  - ${2:config/workflows.yaml}",
			},
		},
	}
}
