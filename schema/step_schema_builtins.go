package schema

func (r *StepSchemaRegistry) registerBuiltins() {
	r.Register(&StepSchema{
		Type:        "step.set",
		Plugin:      "pipelinesteps",
		Description: "Sets key/value pairs in the pipeline context. Values can contain template expressions.",
		ConfigFields: []ConfigFieldDef{
			{Key: "values", Type: FieldTypeMap, Description: "Map of key/value pairs to merge into the pipeline context", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "(dynamic)", Type: "any", Description: "Each key from 'values' becomes an output key with its resolved value"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.log",
		Plugin:      "pipelinesteps",
		Description: "Logs a message at the specified log level.",
		ConfigFields: []ConfigFieldDef{
			{Key: "message", Type: FieldTypeString, Description: "Message to log (template expressions supported)", Required: true},
			{Key: "level", Type: FieldTypeSelect, Description: "Log level", Options: []string{"debug", "info", "warn", "error"}, DefaultValue: "info"},
		},
		Outputs: []StepOutputDef{
			{Key: "logged", Type: "boolean", Description: "Always true when log succeeds"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.validate",
		Plugin:      "pipelinesteps",
		Description: "Validates pipeline context fields against rules.",
		ConfigFields: []ConfigFieldDef{
			{Key: "rules", Type: FieldTypeMap, Description: "Validation rules per field"},
			{Key: "required", Type: FieldTypeArray, Description: "List of required field names"},
			{Key: "schema", Type: FieldTypeString, Description: "JSON Schema for request body validation"},
		},
		Outputs: []StepOutputDef{
			{Key: "valid", Type: "boolean", Description: "Whether validation passed"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.transform",
		Plugin:      "pipelinesteps",
		Description: "Transforms pipeline context values using field mapping or template expressions.",
		ConfigFields: []ConfigFieldDef{
			{Key: "mapping", Type: FieldTypeMap, Description: "Field mapping from source to target keys"},
			{Key: "template", Type: FieldTypeString, Description: "Go template string for complex transformations"},
		},
		Outputs: []StepOutputDef{
			{Key: "(dynamic)", Type: "any", Description: "Output keys match the mapping target keys or template result"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.conditional",
		Plugin:      "pipelinesteps",
		Description: "Branches pipeline execution based on a field value. Uses 'field' with 'routes' map and 'default'.",
		ConfigFields: []ConfigFieldDef{
			{Key: "field", Type: FieldTypeString, Description: "Template expression to evaluate for routing", Required: true},
			{Key: "routes", Type: FieldTypeMap, Description: "Map of field values to step names for branching", Required: true},
			{Key: "default", Type: FieldTypeString, Description: "Default step name when no route matches", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "matched_route", Type: "string", Description: "The route value that was matched"},
			{Key: "next_step", Type: "string", Description: "Name of the next step to execute"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.http_call",
		Plugin:      "pipelinesteps",
		Description: "Makes an outbound HTTP request and stores the response in the pipeline context.",
		ConfigFields: []ConfigFieldDef{
			{Key: "url", Type: FieldTypeString, Description: "Request URL (template expressions supported)", Required: true},
			{Key: "method", Type: FieldTypeSelect, Description: "HTTP method", Options: []string{"GET", "POST", "PUT", "DELETE", "PATCH"}, Required: true},
			{Key: "headers", Type: FieldTypeMap, Description: "Request headers"},
			{Key: "body", Type: FieldTypeString, Description: "Request body (template expressions supported)"},
			{Key: "body_from", Type: FieldTypeString, Description: "Template expression to build body from step outputs"},
			{Key: "timeout", Type: FieldTypeDuration, Description: "Request timeout duration (e.g. 30s)", DefaultValue: "30s"},
			{Key: "auth", Type: FieldTypeMap, Description: "Authentication config (type, token, client_id, client_secret, token_url for OAuth2)"},
		},
		Outputs: []StepOutputDef{
			{Key: "status", Type: "number", Description: "HTTP response status code"},
			{Key: "body", Type: "any", Description: "Response body (parsed as JSON if Content-Type is application/json)"},
			{Key: "headers", Type: "map", Description: "Response headers"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.json_response",
		Plugin:      "pipelinesteps",
		Description: "Sends a JSON HTTP response and terminates pipeline execution.",
		ConfigFields: []ConfigFieldDef{
			{Key: "status", Type: FieldTypeNumber, Description: "HTTP status code", Required: true},
			{Key: "body", Type: FieldTypeJSON, Description: "Response body (static JSON object or template expression)"},
			{Key: "body_from", Type: FieldTypeString, Description: "Template expression to build body from step outputs (e.g. 'steps.query.rows')"},
			{Key: "headers", Type: FieldTypeMap, Description: "Additional response headers"},
		},
		Outputs: []StepOutputDef{
			{Key: "sent", Type: "boolean", Description: "Whether the response was sent successfully"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.request_parse",
		Plugin:      "pipelinesteps",
		Description: "Parses incoming HTTP request body (JSON or form-urlencoded), query params, and headers into the pipeline context.",
		ConfigFields: []ConfigFieldDef{
			{Key: "body_fields", Type: FieldTypeArray, Description: "Specific body fields to extract (default: all)"},
			{Key: "query_params", Type: FieldTypeArray, Description: "Specific query parameters to extract"},
			{Key: "headers", Type: FieldTypeArray, Description: "Specific headers to extract"},
		},
		Outputs: []StepOutputDef{
			{Key: "body", Type: "map", Description: "Parsed request body fields"},
			{Key: "query", Type: "map", Description: "Parsed query parameters"},
			{Key: "headers", Type: "map", Description: "Parsed request headers"},
			{Key: "content_type", Type: "string", Description: "Detected content type (application/json, application/x-www-form-urlencoded)"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.db_query",
		Plugin:      "pipelinesteps",
		Description: "Executes a database SELECT query and stores results in the pipeline context.",
		ConfigFields: []ConfigFieldDef{
			{Key: "database", Type: FieldTypeString, Description: "Database module name", Required: true},
			{Key: "query", Type: FieldTypeSQL, Description: "SQL query (template expressions supported)", Required: true},
			{Key: "params", Type: FieldTypeArray, Description: "Query parameters (positional $1, $2...)"},
			{Key: "mode", Type: FieldTypeSelect, Description: "Result mode", Options: []string{"single", "list"}, DefaultValue: "list"},
		},
		Outputs: []StepOutputDef{
			{Key: "row", Type: "map", Description: "First result row as key-value map (single mode)"},
			{Key: "rows", Type: "[]map", Description: "All result rows (list mode)"},
			{Key: "count", Type: "number", Description: "Number of rows returned (list mode)"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.db_exec",
		Plugin:      "pipelinesteps",
		Description: "Executes a database INSERT/UPDATE/DELETE statement.",
		ConfigFields: []ConfigFieldDef{
			{Key: "database", Type: FieldTypeString, Description: "Database module name", Required: true},
			{Key: "query", Type: FieldTypeSQL, Description: "SQL statement (template expressions supported)", Required: true},
			{Key: "params", Type: FieldTypeArray, Description: "Statement parameters (positional $1, $2...)"},
		},
		Outputs: []StepOutputDef{
			{Key: "rows_affected", Type: "number", Description: "Number of rows affected by the statement"},
			{Key: "last_insert_id", Type: "number", Description: "Last inserted row ID (if supported by driver)"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.db_query_cached",
		Plugin:      "pipelinesteps",
		Description: "Executes a cached database query. Returns cached results if available, otherwise runs query and caches.",
		ConfigFields: []ConfigFieldDef{
			{Key: "database", Type: FieldTypeString, Description: "Database module name", Required: true},
			{Key: "query", Type: FieldTypeSQL, Description: "SQL query (template expressions supported)", Required: true},
			{Key: "params", Type: FieldTypeArray, Description: "Query parameters"},
			{Key: "cache_key", Type: FieldTypeString, Description: "Cache key (template expressions supported)", Required: true},
			{Key: "ttl", Type: FieldTypeDuration, Description: "Cache TTL (e.g. 5m, 1h)", DefaultValue: "5m"},
			{Key: "mode", Type: FieldTypeSelect, Description: "Result mode", Options: []string{"single", "list"}, DefaultValue: "single"},
		},
		Outputs: []StepOutputDef{
			{Key: "row", Type: "map", Description: "First result row (single mode, not nested under 'row')"},
			{Key: "rows", Type: "[]map", Description: "All result rows (list mode)"},
			{Key: "count", Type: "number", Description: "Number of rows (list mode)"},
			{Key: "cache_hit", Type: "boolean", Description: "Whether the result came from cache"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.foreach",
		Plugin:      "pipelinesteps",
		Description: "Iterates over a collection and executes nested steps for each item.",
		ConfigFields: []ConfigFieldDef{
			{Key: "collection", Type: FieldTypeString, Description: "Template expression resolving to the collection to iterate", Required: true},
			{Key: "item_var", Type: FieldTypeString, Description: "Context key for the current item", DefaultValue: "item"},
			{Key: "item_key", Type: FieldTypeString, Description: "Context key for the current item's key/index"},
			{Key: "index_key", Type: FieldTypeString, Description: "Context key for the numeric loop index"},
			{Key: "step", Type: FieldTypeJSON, Description: "Single step definition to execute per item"},
			{Key: "steps", Type: FieldTypeArray, Description: "List of step definitions to execute per item"},
			{Key: "concurrency", Type: FieldTypeNumber, Description: "Worker pool size. 0 = sequential. Time: O(⌈n/c⌉ × per_item). Space: O(c × context_size).", DefaultValue: 0},
			{Key: "error_strategy", Type: FieldTypeSelect, Description: "Error handling for concurrent mode. fail_fast: cancel on first error. collect_errors: continue, mark failed items.", Options: []string{"fail_fast", "collect_errors"}, DefaultValue: "fail_fast"},
		},
		Outputs: []StepOutputDef{
			{Key: "count", Type: "number", Description: "Number of iterations completed"},
			{Key: "results", Type: "[]any", Description: "Output from each iteration"},
			{Key: "error_count", Type: "integer", Description: "Count of failed items (only present with error_strategy: collect_errors)"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.parallel",
		Plugin:      "pipelinesteps",
		Description: "Execute multiple named sub-steps concurrently and collect results. Time: O(max(branch)). Space: O(branches × context_size).",
		ConfigFields: []ConfigFieldDef{
			{Key: "steps", Type: FieldTypeArray, Required: true, Description: "List of sub-steps to run concurrently. Each must have a unique 'name'."},
			{Key: "error_strategy", Type: FieldTypeSelect, Required: false, DefaultValue: "fail_fast", Options: []string{"fail_fast", "collect_errors"}, Description: "fail_fast: cancel on first error. collect_errors: run all, collect partial results."},
		},
		Outputs: []StepOutputDef{
			{Key: "results", Type: "map", Description: "Map of branch_name → branch output (successful branches)"},
			{Key: "errors", Type: "map", Description: "Map of branch_name → error string (failed branches)"},
			{Key: "completed", Type: "integer", Description: "Count of successful branches"},
			{Key: "failed", Type: "integer", Description: "Count of failed branches"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.delegate",
		Plugin:      "pipelinesteps",
		Description: "Delegates execution to another module service.",
		ConfigFields: []ConfigFieldDef{
			{Key: "service", Type: FieldTypeString, Description: "Name of the service module to delegate to", Required: true},
			{Key: "action", Type: FieldTypeString, Description: "Action to invoke on the service"},
		},
		Outputs: []StepOutputDef{
			{Key: "(dynamic)", Type: "any", Description: "Output depends on the delegated service"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.publish",
		Plugin:      "pipelinesteps",
		Description: "Publishes a message to a messaging broker topic or EventBus.",
		ConfigFields: []ConfigFieldDef{
			{Key: "topic", Type: FieldTypeString, Description: "Topic name to publish to", Required: true},
			{Key: "broker", Type: FieldTypeString, Description: "Messaging broker module name (optional, falls back to EventBus)"},
			{Key: "payload", Type: FieldTypeMap, Description: "Message payload (template expressions supported)"},
		},
		Outputs: []StepOutputDef{
			{Key: "published", Type: "boolean", Description: "Whether the message was published successfully"},
			{Key: "topic", Type: "string", Description: "The topic the message was published to"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.event_publish",
		Plugin:      "pipelinesteps",
		Description: "Publishes a structured event in CloudEvents format to a messaging broker or EventPublisher.",
		ConfigFields: []ConfigFieldDef{
			{Key: "topic", Type: FieldTypeString, Description: "Topic name to publish to", Required: true},
			{Key: "stream", Type: FieldTypeString, Description: "Alias for topic (e.g., Kinesis stream name)"},
			{Key: "broker", Type: FieldTypeString, Description: "Messaging broker module name"},
			{Key: "provider", Type: FieldTypeString, Description: "Alias for broker — EventPublisher or MessageBroker service name"},
			{Key: "payload", Type: FieldTypeMap, Description: "Event payload (template expressions supported)"},
			{Key: "data", Type: FieldTypeMap, Description: "Alias for payload"},
			{Key: "headers", Type: FieldTypeMap, Description: "Event headers"},
			{Key: "event_type", Type: FieldTypeString, Description: "CloudEvents type identifier"},
			{Key: "source", Type: FieldTypeString, Description: "CloudEvents source URI (template expressions supported)"},
		},
		Outputs: []StepOutputDef{
			{Key: "published", Type: "boolean", Description: "Whether the event was published successfully"},
			{Key: "event_id", Type: "string", Description: "Generated event ID"},
			{Key: "topic", Type: "string", Description: "The topic the event was published to"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.cache_get",
		Plugin:      "pipelinesteps",
		Description: "Retrieves a value from a cache module by key.",
		ConfigFields: []ConfigFieldDef{
			{Key: "cache", Type: FieldTypeString, Description: "Cache module name", Required: true},
			{Key: "key", Type: FieldTypeString, Description: "Cache key (template expressions supported)", Required: true},
			{Key: "output", Type: FieldTypeString, Description: "Context key to store the result"},
		},
		Outputs: []StepOutputDef{
			{Key: "value", Type: "any", Description: "The cached value (nil if not found)"},
			{Key: "found", Type: "boolean", Description: "Whether the key was found in cache"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.cache_set",
		Plugin:      "pipelinesteps",
		Description: "Stores a value in a cache module by key.",
		ConfigFields: []ConfigFieldDef{
			{Key: "cache", Type: FieldTypeString, Description: "Cache module name", Required: true},
			{Key: "key", Type: FieldTypeString, Description: "Cache key (template expressions supported)", Required: true},
			{Key: "value", Type: FieldTypeString, Description: "Value to cache (template expressions supported)"},
			{Key: "ttl", Type: FieldTypeDuration, Description: "Time-to-live duration (e.g. 5m, 1h)"},
		},
		Outputs: []StepOutputDef{
			{Key: "stored", Type: "boolean", Description: "Whether the value was stored successfully"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.cache_delete",
		Plugin:      "pipelinesteps",
		Description: "Deletes a value from a cache module by key.",
		ConfigFields: []ConfigFieldDef{
			{Key: "cache", Type: FieldTypeString, Description: "Cache module name", Required: true},
			{Key: "key", Type: FieldTypeString, Description: "Cache key to delete (template expressions supported)", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "deleted", Type: "boolean", Description: "Whether the key was deleted"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.retry_with_backoff",
		Plugin:      "pipelinesteps",
		Description: "Retries a nested step with exponential backoff on failure.",
		ConfigFields: []ConfigFieldDef{
			{Key: "max_retries", Type: FieldTypeNumber, Description: "Maximum retry attempts", Required: true},
			{Key: "initial_delay", Type: FieldTypeDuration, Description: "Initial delay before first retry", DefaultValue: "100ms"},
			{Key: "max_delay", Type: FieldTypeDuration, Description: "Maximum delay between retries", DefaultValue: "30s"},
			{Key: "multiplier", Type: FieldTypeNumber, Description: "Backoff multiplier", DefaultValue: 2.0},
			{Key: "step", Type: FieldTypeJSON, Description: "The step definition to retry", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "attempts", Type: "number", Description: "Number of attempts made"},
			{Key: "(nested)", Type: "any", Description: "Output from the nested step on success"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.resilient_circuit_breaker",
		Plugin:      "pipelinesteps",
		Description: "Checks circuit breaker state for a service. Allows requests when closed/half-open, rejects when open.",
		ConfigFields: []ConfigFieldDef{
			{Key: "failure_threshold", Type: FieldTypeNumber, Description: "Number of failures before opening the circuit", DefaultValue: 5},
			{Key: "success_threshold", Type: FieldTypeNumber, Description: "Number of successes in half-open to close the circuit", DefaultValue: 3},
			{Key: "timeout", Type: FieldTypeDuration, Description: "Duration before trying half-open", DefaultValue: "30s"},
			{Key: "service_name", Type: FieldTypeString, Description: "Service identifier for the circuit"},
		},
		Outputs: []StepOutputDef{
			{Key: "circuit_breaker", Type: "map", Description: "Circuit breaker status: {state, service, allowed, transitioned}"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.auth_required",
		Plugin:      "pipelinesteps",
		Description: "Validates JWT or API key authentication. Returns 401 if not authenticated.",
		ConfigFields: []ConfigFieldDef{
			{Key: "roles", Type: FieldTypeArray, Description: "Required roles (any match grants access)"},
			{Key: "scopes", Type: FieldTypeArray, Description: "Required OAuth2 scopes"},
		},
		Outputs: []StepOutputDef{
			{Key: "authenticated", Type: "boolean", Description: "Whether the request is authenticated"},
			{Key: "user_id", Type: "string", Description: "Authenticated user ID from token claims"},
			{Key: "roles", Type: "[]string", Description: "User roles from token claims"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.authz_check",
		Plugin:      "pipelinesteps",
		Description: "Checks authorization using the configured authz module (e.g., Casbin). Returns 403 if denied.",
		ConfigFields: []ConfigFieldDef{
			{Key: "module", Type: FieldTypeString, Description: "Authorization module name", Required: true},
			{Key: "subject", Type: FieldTypeString, Description: "Subject (template expression, e.g. user role)", Required: true},
			{Key: "object", Type: FieldTypeString, Description: "Object (template expression, e.g. request path)", Required: true},
			{Key: "action", Type: FieldTypeString, Description: "Action (template expression, e.g. HTTP method)", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "allowed", Type: "boolean", Description: "Whether the authorization check passed"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.jq",
		Plugin:      "pipelinesteps",
		Description: "Applies a jq expression to transform data in the pipeline context.",
		ConfigFields: []ConfigFieldDef{
			{Key: "expression", Type: FieldTypeString, Description: "jq filter expression", Required: true},
			{Key: "input", Type: FieldTypeString, Description: "Context key of the input value (default: whole context)"},
			{Key: "output", Type: FieldTypeString, Description: "Context key to store the result"},
		},
		Outputs: []StepOutputDef{
			{Key: "result", Type: "any", Description: "Result of the jq expression"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.webhook_verify",
		Plugin:      "pipelinesteps",
		Description: "Verifies webhook signatures from providers like GitHub, GitLab, or Stripe.",
		ConfigFields: []ConfigFieldDef{
			{Key: "provider", Type: FieldTypeSelect, Description: "Webhook provider", Options: []string{"github", "gitlab", "stripe", "generic"}},
			{Key: "scheme", Type: FieldTypeSelect, Description: "Signature scheme", Options: []string{"hmac-sha256", "hmac-sha1"}},
			{Key: "secret", Type: FieldTypeString, Description: "Shared secret for signature verification", Sensitive: true},
			{Key: "secret_from", Type: FieldTypeString, Description: "Context key containing the secret"},
			{Key: "signature_header", Type: FieldTypeString, Description: "HTTP header containing the signature"},
			{Key: "header", Type: FieldTypeString, Description: "Alias for signature_header"},
		},
		Outputs: []StepOutputDef{
			{Key: "verified", Type: "boolean", Description: "Whether the webhook signature is valid"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.workflow_call",
		Plugin:      "pipelinesteps",
		Description: "Calls another workflow and returns its result.",
		ConfigFields: []ConfigFieldDef{
			{Key: "workflow", Type: FieldTypeString, Description: "Workflow name to call", Required: true},
			{Key: "input", Type: FieldTypeMap, Description: "Input data to pass to the workflow"},
		},
		Outputs: []StepOutputDef{
			{Key: "(dynamic)", Type: "any", Description: "Output from the called workflow"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.validate_path_param",
		Plugin:      "pipelinesteps",
		Description: "Validates a URL path parameter exists and matches the expected type.",
		ConfigFields: []ConfigFieldDef{
			{Key: "param", Type: FieldTypeString, Description: "Path parameter name", Required: true},
			{Key: "type", Type: FieldTypeSelect, Description: "Expected type", Options: []string{"string", "integer", "uuid"}},
			{Key: "required", Type: FieldTypeBool, Description: "Whether the parameter is required", DefaultValue: true},
		},
		Outputs: []StepOutputDef{
			{Key: "valid", Type: "boolean", Description: "Whether the parameter is valid"},
			{Key: "value", Type: "string", Description: "The validated parameter value"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.validate_pagination",
		Plugin:      "pipelinesteps",
		Description: "Validates and normalizes pagination query parameters (limit, offset).",
		ConfigFields: []ConfigFieldDef{
			{Key: "maxLimit", Type: FieldTypeNumber, Description: "Maximum allowed limit value", DefaultValue: 100},
			{Key: "defaultLimit", Type: FieldTypeNumber, Description: "Default limit when not provided", DefaultValue: 20},
		},
		Outputs: []StepOutputDef{
			{Key: "limit", Type: "number", Description: "Normalized limit value"},
			{Key: "offset", Type: "number", Description: "Normalized offset value"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.validate_request_body",
		Plugin:      "pipelinesteps",
		Description: "Validates the request body against a JSON Schema.",
		ConfigFields: []ConfigFieldDef{
			{Key: "schema", Type: FieldTypeJSON, Description: "JSON Schema to validate against", Required: true},
			{Key: "required", Type: FieldTypeArray, Description: "List of required body fields"},
		},
		Outputs: []StepOutputDef{
			{Key: "valid", Type: "boolean", Description: "Whether the body passed validation"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.dlq_send",
		Plugin:      "pipelinesteps",
		Description: "Sends a failed message to the dead-letter queue.",
		ConfigFields: []ConfigFieldDef{
			{Key: "topic", Type: FieldTypeString, Description: "DLQ topic name", Required: true},
			{Key: "original_topic", Type: FieldTypeString, Description: "Original topic the message came from"},
			{Key: "error", Type: FieldTypeString, Description: "Error message describing the failure"},
			{Key: "payload", Type: FieldTypeString, Description: "Original message payload"},
			{Key: "broker", Type: FieldTypeString, Description: "Messaging broker module name"},
		},
		Outputs: []StepOutputDef{
			{Key: "sent", Type: "boolean", Description: "Whether the message was sent to DLQ"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.dlq_replay",
		Plugin:      "pipelinesteps",
		Description: "Replays messages from the dead-letter queue to the target topic.",
		ConfigFields: []ConfigFieldDef{
			{Key: "dlq_topic", Type: FieldTypeString, Description: "DLQ topic to read from", Required: true},
			{Key: "target_topic", Type: FieldTypeString, Description: "Target topic to replay messages to", Required: true},
			{Key: "max_messages", Type: FieldTypeNumber, Description: "Maximum messages to replay (default: all)"},
			{Key: "broker", Type: FieldTypeString, Description: "Messaging broker module name"},
		},
		Outputs: []StepOutputDef{
			{Key: "replayed", Type: "number", Description: "Number of messages replayed"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.nosql_get",
		Plugin:      "datastores",
		Description: "Retrieves a document from a NoSQL store by key.",
		ConfigFields: []ConfigFieldDef{
			{Key: "store", Type: FieldTypeString, Description: "NoSQL store module name", Required: true},
			{Key: "key", Type: FieldTypeString, Description: "Document key (template expressions supported)", Required: true},
			{Key: "output", Type: FieldTypeString, Description: "Context key to store the result"},
			{Key: "miss_ok", Type: FieldTypeBool, Description: "Don't fail if key not found", DefaultValue: false},
		},
		Outputs: []StepOutputDef{
			{Key: "value", Type: "any", Description: "The retrieved document"},
			{Key: "found", Type: "boolean", Description: "Whether the key was found"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.nosql_put",
		Plugin:      "datastores",
		Description: "Stores a document in a NoSQL store by key.",
		ConfigFields: []ConfigFieldDef{
			{Key: "store", Type: FieldTypeString, Description: "NoSQL store module name", Required: true},
			{Key: "key", Type: FieldTypeString, Description: "Document key (template expressions supported)", Required: true},
			{Key: "item", Type: FieldTypeJSON, Description: "Document to store"},
		},
		Outputs: []StepOutputDef{
			{Key: "stored", Type: "boolean", Description: "Whether the document was stored successfully"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.nosql_query",
		Plugin:      "datastores",
		Description: "Queries documents from a NoSQL store by key prefix.",
		ConfigFields: []ConfigFieldDef{
			{Key: "store", Type: FieldTypeString, Description: "NoSQL store module name", Required: true},
			{Key: "prefix", Type: FieldTypeString, Description: "Key prefix to filter documents"},
			{Key: "output", Type: FieldTypeString, Description: "Context key to store results"},
		},
		Outputs: []StepOutputDef{
			{Key: "items", Type: "[]any", Description: "Matching documents"},
			{Key: "count", Type: "number", Description: "Number of matching documents"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.base64_decode",
		Plugin:      "pipelinesteps",
		Description: "Decodes a base64-encoded value and validates its type.",
		ConfigFields: []ConfigFieldDef{
			{Key: "input_from", Type: FieldTypeString, Description: "Context key containing the base64-encoded value"},
			{Key: "format", Type: FieldTypeString, Description: "Expected format (e.g. image/png, application/pdf)"},
			{Key: "allowed_types", Type: FieldTypeArray, Description: "List of allowed MIME types"},
			{Key: "max_size_bytes", Type: FieldTypeNumber, Description: "Maximum decoded size in bytes"},
		},
		Outputs: []StepOutputDef{
			{Key: "data", Type: "string", Description: "Decoded data"},
			{Key: "mime_type", Type: "string", Description: "Detected MIME type"},
			{Key: "size", Type: "number", Description: "Decoded size in bytes"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.statemachine_transition",
		Plugin:      "statemachine",
		Description: "Triggers a state machine transition for a given instance.",
		ConfigFields: []ConfigFieldDef{
			{Key: "engine", Type: FieldTypeString, Description: "State machine engine module name", Required: true},
			{Key: "instanceId", Type: FieldTypeString, Description: "State machine instance ID (template expressions supported)", Required: true},
			{Key: "transition", Type: FieldTypeString, Description: "Transition name to trigger", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "previous_state", Type: "string", Description: "State before the transition"},
			{Key: "current_state", Type: "string", Description: "State after the transition"},
			{Key: "transitioned", Type: "boolean", Description: "Whether the transition was applied"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.statemachine_get",
		Plugin:      "statemachine",
		Description: "Retrieves the current state of a state machine instance.",
		ConfigFields: []ConfigFieldDef{
			{Key: "engine", Type: FieldTypeString, Description: "State machine engine module name", Required: true},
			{Key: "instanceId", Type: FieldTypeString, Description: "State machine instance ID (template expressions supported)", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "state", Type: "string", Description: "Current state of the instance"},
			{Key: "instance_id", Type: "string", Description: "Instance ID"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.feature_flag",
		Plugin:      "featureflags",
		Description: "Evaluates a feature flag and stores the result in the pipeline context.",
		ConfigFields: []ConfigFieldDef{
			{Key: "flag", Type: FieldTypeString, Description: "Feature flag name", Required: true},
			{Key: "default", Type: FieldTypeBool, Description: "Default value when flag not found", DefaultValue: false},
			{Key: "output", Type: FieldTypeString, Description: "Context key to store the flag value"},
		},
		Outputs: []StepOutputDef{
			{Key: "enabled", Type: "boolean", Description: "Whether the feature flag is enabled"},
			{Key: "flag", Type: "string", Description: "The flag name that was evaluated"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.shell_exec",
		Plugin:      "cicd",
		Description: "Executes a shell command and captures its output.",
		ConfigFields: []ConfigFieldDef{
			{Key: "command", Type: FieldTypeString, Description: "Command to execute", Required: true},
			{Key: "args", Type: FieldTypeArray, Description: "Command arguments"},
			{Key: "env", Type: FieldTypeMap, Description: "Environment variables"},
			{Key: "workdir", Type: FieldTypeString, Description: "Working directory"},
			{Key: "timeout", Type: FieldTypeDuration, Description: "Execution timeout (e.g. 5m)"},
		},
		Outputs: []StepOutputDef{
			{Key: "stdout", Type: "string", Description: "Standard output from the command"},
			{Key: "stderr", Type: "string", Description: "Standard error from the command"},
			{Key: "exit_code", Type: "number", Description: "Process exit code"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.hash",
		Plugin:      "pipelinesteps",
		Description: "Computes a cryptographic hash of a template-resolved input string.",
		ConfigFields: []ConfigFieldDef{
			{Key: "algorithm", Type: FieldTypeSelect, Description: "Hash algorithm", Options: []string{"md5", "sha256", "sha512"}, DefaultValue: "sha256"},
			{Key: "input", Type: FieldTypeString, Description: "Input string to hash (template expressions supported)", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "hash", Type: "string", Description: "Hex-encoded hash value"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.regex_match",
		Plugin:      "pipelinesteps",
		Description: "Matches a regular expression against a template-resolved input string.",
		ConfigFields: []ConfigFieldDef{
			{Key: "pattern", Type: FieldTypeString, Description: "Regular expression pattern (compiled at config time)", Required: true},
			{Key: "input", Type: FieldTypeString, Description: "Input string to match against (template expressions supported)", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "matched", Type: "boolean", Description: "Whether the pattern matched"},
			{Key: "match", Type: "string", Description: "The matched substring"},
			{Key: "groups", Type: "[]string", Description: "Captured groups from the match"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.static_file",
		Plugin:      "pipelinesteps",
		Description: "Serves a static file from the filesystem as the HTTP response.",
		ConfigFields: []ConfigFieldDef{
			{Key: "path", Type: FieldTypeFilePath, Description: "File path to serve (template expressions supported)", Required: true},
			{Key: "content_type", Type: FieldTypeString, Description: "Content-Type header (auto-detected from extension if not set)"},
		},
		Outputs: []StepOutputDef{
			{Key: "served", Type: "boolean", Description: "Whether the file was served"},
			{Key: "content_type", Type: "string", Description: "The Content-Type that was sent"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.oidc_auth_url",
		Plugin:      "auth",
		Description: "Generates an OIDC/OAuth2 authorization URL and redirects the user.",
		ConfigFields: []ConfigFieldDef{
			{Key: "provider", Type: FieldTypeString, Description: "OIDC provider name (registered in auth module)", Required: true},
			{Key: "redirect_uri", Type: FieldTypeString, Description: "Redirect URI after authentication", Required: true},
			{Key: "scopes", Type: FieldTypeArray, Description: "OAuth2 scopes to request"},
		},
		Outputs: []StepOutputDef{
			{Key: "auth_url", Type: "string", Description: "The generated authorization URL"},
			{Key: "state", Type: "string", Description: "The CSRF state parameter"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.oidc_callback",
		Plugin:      "auth",
		Description: "Handles the OIDC/OAuth2 callback, exchanges the code for tokens, and extracts user info.",
		ConfigFields: []ConfigFieldDef{
			{Key: "provider", Type: FieldTypeString, Description: "OIDC provider name", Required: true},
			{Key: "redirect_uri", Type: FieldTypeString, Description: "Redirect URI (must match auth URL)", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "id_token", Type: "string", Description: "The ID token"},
			{Key: "access_token", Type: "string", Description: "The access token"},
			{Key: "user_info", Type: "map", Description: "User info claims from the ID token"},
			{Key: "email", Type: "string", Description: "User email from claims"},
			{Key: "sub", Type: "string", Description: "Subject identifier from claims"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.m2m_token",
		Plugin:      "auth",
		Description: "Generates or validates a machine-to-machine (M2M) token with custom claims.",
		ConfigFields: []ConfigFieldDef{
			{Key: "action", Type: FieldTypeSelect, Description: "Token action", Options: []string{"generate", "validate", "revoke", "introspect"}, Required: true},
			{Key: "claims", Type: FieldTypeMap, Description: "Custom claims to include in the token"},
			{Key: "ttl", Type: FieldTypeDuration, Description: "Token time-to-live"},
		},
		Outputs: []StepOutputDef{
			{Key: "token", Type: "string", Description: "Generated or validated token"},
			{Key: "claims", Type: "map", Description: "Token claims"},
			{Key: "valid", Type: "boolean", Description: "Whether the token is valid (validate action)"},
		},
	})

	// --- AI plugin steps ---

	r.Register(&StepSchema{
		Type:        "step.ai_complete",
		Plugin:      "ai",
		Description: "Invokes an AI provider for text completion.",
		ConfigFields: []ConfigFieldDef{
			{Key: "provider", Type: FieldTypeString, Description: "AI provider module name"},
			{Key: "model", Type: FieldTypeString, Description: "Model name to use"},
			{Key: "system_prompt", Type: FieldTypeString, Description: "System prompt (template expressions supported)"},
			{Key: "input_from", Type: FieldTypeString, Description: "Dot-path to resolve input text"},
			{Key: "max_tokens", Type: FieldTypeNumber, Description: "Token limit", DefaultValue: 1024},
			{Key: "temperature", Type: FieldTypeNumber, Description: "Temperature parameter"},
		},
		Outputs: []StepOutputDef{
			{Key: "content", Type: "string", Description: "Generated text"},
			{Key: "model", Type: "string", Description: "Model used"},
			{Key: "finish_reason", Type: "string", Description: "Completion finish reason"},
			{Key: "usage", Type: "map", Description: "Token usage (input_tokens, output_tokens)"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.ai_classify",
		Plugin:      "ai",
		Description: "Classifies text input into predefined categories using an AI provider.",
		ConfigFields: []ConfigFieldDef{
			{Key: "provider", Type: FieldTypeString, Description: "AI provider module name"},
			{Key: "model", Type: FieldTypeString, Description: "Model name to use"},
			{Key: "categories", Type: FieldTypeArray, Description: "Valid classification categories", Required: true},
			{Key: "input_from", Type: FieldTypeString, Description: "Dot-path to resolve input text"},
			{Key: "max_tokens", Type: FieldTypeNumber, Description: "Token limit", DefaultValue: 256},
			{Key: "temperature", Type: FieldTypeNumber, Description: "Temperature parameter"},
		},
		Outputs: []StepOutputDef{
			{Key: "category", Type: "string", Description: "Predicted category"},
			{Key: "confidence", Type: "number", Description: "Confidence score (0-1)"},
			{Key: "reasoning", Type: "string", Description: "Explanation of classification"},
			{Key: "usage", Type: "map", Description: "Token usage stats"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.ai_extract",
		Plugin:      "ai",
		Description: "Extracts structured data from text using AI provider with optional tool use.",
		ConfigFields: []ConfigFieldDef{
			{Key: "provider", Type: FieldTypeString, Description: "AI provider module name"},
			{Key: "model", Type: FieldTypeString, Description: "Model name to use"},
			{Key: "schema", Type: FieldTypeJSON, Description: "JSON Schema for extraction structure", Required: true},
			{Key: "input_from", Type: FieldTypeString, Description: "Dot-path to input text"},
			{Key: "max_tokens", Type: FieldTypeNumber, Description: "Token limit", DefaultValue: 1024},
			{Key: "temperature", Type: FieldTypeNumber, Description: "Temperature parameter"},
		},
		Outputs: []StepOutputDef{
			{Key: "extracted", Type: "map", Description: "Extracted structured data"},
			{Key: "method", Type: "string", Description: "Extraction method used"},
			{Key: "usage", Type: "map", Description: "Token usage"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.sub_workflow",
		Plugin:      "ai",
		Description: "Invokes a registered plugin workflow as a sub-workflow.",
		ConfigFields: []ConfigFieldDef{
			{Key: "workflow", Type: FieldTypeString, Description: "Qualified workflow name", Required: true},
			{Key: "input_mapping", Type: FieldTypeMap, Description: "Template expressions mapping parent to child inputs"},
			{Key: "output_mapping", Type: FieldTypeMap, Description: "Dot-paths mapping child outputs to parent keys"},
			{Key: "timeout", Type: FieldTypeDuration, Description: "Execution timeout", DefaultValue: "30s"},
		},
		Outputs: []StepOutputDef{
			{Key: "result", Type: "map", Description: "Child pipeline outputs"},
		},
	})

	// --- CI/CD plugin steps ---

	r.Register(&StepSchema{
		Type:        "step.artifact_pull",
		Plugin:      "cicd",
		Description: "Retrieves an artifact from a configured source (previous execution, URL, or S3).",
		ConfigFields: []ConfigFieldDef{
			{Key: "source", Type: FieldTypeSelect, Description: "Artifact source type", Options: []string{"previous_execution", "url", "s3"}, Required: true},
			{Key: "dest", Type: FieldTypeString, Description: "Destination file path", Required: true},
			{Key: "key", Type: FieldTypeString, Description: "Artifact key (for previous_execution or s3 source)"},
			{Key: "url", Type: FieldTypeString, Description: "Download URL (for url source)"},
			{Key: "execution_id", Type: FieldTypeString, Description: "Execution ID (uses metadata if not set)"},
		},
		Outputs: []StepOutputDef{
			{Key: "source", Type: "string", Description: "Source type used"},
			{Key: "key", Type: "string", Description: "Artifact key"},
			{Key: "dest", Type: "string", Description: "Destination path"},
			{Key: "size", Type: "number", Description: "File size in bytes"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.artifact_push",
		Plugin:      "cicd",
		Description: "Reads a file and stores it as an artifact with SHA256 checksum.",
		ConfigFields: []ConfigFieldDef{
			{Key: "source_path", Type: FieldTypeString, Description: "Path to file to push", Required: true},
			{Key: "key", Type: FieldTypeString, Description: "Storage key", Required: true},
			{Key: "dest", Type: FieldTypeString, Description: "Destination type (default: artifact_store)"},
		},
		Outputs: []StepOutputDef{
			{Key: "key", Type: "string", Description: "Storage key"},
			{Key: "size", Type: "number", Description: "File size in bytes"},
			{Key: "checksum", Type: "string", Description: "SHA256 hex digest"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.build_ui",
		Plugin:      "cicd",
		Description: "Builds a UI project (npm install + npm run build) and copies output to target directory.",
		ConfigFields: []ConfigFieldDef{
			{Key: "source_dir", Type: FieldTypeString, Description: "UI source directory (containing package.json)", Required: true},
			{Key: "output_dir", Type: FieldTypeString, Description: "Target directory for built assets", Required: true},
			{Key: "install_cmd", Type: FieldTypeString, Description: "npm install command", DefaultValue: "npm install --silent"},
			{Key: "build_cmd", Type: FieldTypeString, Description: "Build command", DefaultValue: "npm run build"},
			{Key: "env", Type: FieldTypeMap, Description: "Environment variables"},
			{Key: "timeout", Type: FieldTypeDuration, Description: "Build timeout", DefaultValue: "5m"},
		},
		Outputs: []StepOutputDef{
			{Key: "status", Type: "string", Description: "Build status"},
			{Key: "file_count", Type: "number", Description: "Number of files in output"},
			{Key: "output_dir", Type: "string", Description: "Resolved output path"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.docker_build",
		Plugin:      "cicd",
		Description: "Builds a Docker image from a context directory.",
		ConfigFields: []ConfigFieldDef{
			{Key: "context", Type: FieldTypeString, Description: "Build context directory", Required: true},
			{Key: "dockerfile", Type: FieldTypeString, Description: "Dockerfile path", DefaultValue: "Dockerfile"},
			{Key: "tags", Type: FieldTypeArray, Description: "Image tags"},
			{Key: "build_args", Type: FieldTypeMap, Description: "Build arguments"},
			{Key: "cache_from", Type: FieldTypeArray, Description: "Cache images"},
		},
		Outputs: []StepOutputDef{
			{Key: "images_built", Type: "[]string", Description: "Built image tags"},
			{Key: "digest", Type: "string", Description: "Image digest"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.docker_push",
		Plugin:      "cicd",
		Description: "Pushes a Docker image to a remote registry.",
		ConfigFields: []ConfigFieldDef{
			{Key: "image", Type: FieldTypeString, Description: "Image name/tag to push", Required: true},
			{Key: "registry", Type: FieldTypeString, Description: "Registry prefix"},
			{Key: "auth_provider", Type: FieldTypeString, Description: "Auth provider name"},
		},
		Outputs: []StepOutputDef{
			{Key: "image", Type: "string", Description: "Image name"},
			{Key: "digest", Type: "string", Description: "Image digest"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.docker_run",
		Plugin:      "cicd",
		Description: "Runs a command inside a Docker container.",
		ConfigFields: []ConfigFieldDef{
			{Key: "image", Type: FieldTypeString, Description: "Container image", Required: true},
			{Key: "command", Type: FieldTypeArray, Description: "Command to run"},
			{Key: "env", Type: FieldTypeMap, Description: "Environment variables"},
			{Key: "timeout", Type: FieldTypeDuration, Description: "Execution timeout"},
			{Key: "wait_for_exit", Type: FieldTypeBool, Description: "Wait for container to exit", DefaultValue: true},
		},
		Outputs: []StepOutputDef{
			{Key: "exit_code", Type: "number", Description: "Container exit code"},
			{Key: "stdout", Type: "string", Description: "Standard output"},
			{Key: "stderr", Type: "string", Description: "Standard error"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.deploy",
		Plugin:      "cicd",
		Description: "Executes deployment with strategy support (rolling, blue_green, canary).",
		ConfigFields: []ConfigFieldDef{
			{Key: "environment", Type: FieldTypeString, Description: "Target environment", Required: true},
			{Key: "strategy", Type: FieldTypeSelect, Description: "Deployment strategy", Options: []string{"rolling", "blue_green", "canary"}, Required: true},
			{Key: "image", Type: FieldTypeString, Description: "Container image to deploy", Required: true},
			{Key: "provider", Type: FieldTypeString, Description: "Deployment provider name"},
			{Key: "rollback_on_failure", Type: FieldTypeBool, Description: "Auto-rollback on deployment error"},
			{Key: "health_check", Type: FieldTypeMap, Description: "Health check configuration (path, interval, timeout)"},
		},
		Outputs: []StepOutputDef{
			{Key: "status", Type: "string", Description: "Deployment status"},
			{Key: "image", Type: "string", Description: "Deployed image"},
			{Key: "environment", Type: "string", Description: "Target environment"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.gate",
		Plugin:      "cicd",
		Description: "Approval gate supporting manual, automated, and scheduled approval types.",
		ConfigFields: []ConfigFieldDef{
			{Key: "type", Type: FieldTypeSelect, Description: "Gate type", Options: []string{"manual", "automated", "scheduled"}, Required: true},
			{Key: "timeout", Type: FieldTypeDuration, Description: "Approval timeout", DefaultValue: "24h"},
			{Key: "approvers", Type: FieldTypeArray, Description: "Required approver names"},
			{Key: "auto_approve_conditions", Type: FieldTypeArray, Description: "Conditions for automated approval"},
			{Key: "schedule", Type: FieldTypeMap, Description: "Scheduled window config (weekdays, start_hour, end_hour)"},
		},
		Outputs: []StepOutputDef{
			{Key: "gate_result", Type: "map", Description: "Gate result: {passed, type, reason, approval_required}"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.scan_sast",
		Plugin:      "cicd",
		Description: "Performs static application security testing (SAST).",
		ConfigFields: []ConfigFieldDef{
			{Key: "scanner", Type: FieldTypeString, Description: "Scanner name (e.g., semgrep)", Required: true},
			{Key: "target", Type: FieldTypeString, Description: "Scan target path"},
			{Key: "rules", Type: FieldTypeArray, Description: "Scanner rule sets"},
		},
		Outputs: []StepOutputDef{
			{Key: "findings", Type: "[]any", Description: "Security findings"},
			{Key: "passed", Type: "boolean", Description: "Whether the scan passed"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.scan_container",
		Plugin:      "cicd",
		Description: "Scans a container image for vulnerabilities.",
		ConfigFields: []ConfigFieldDef{
			{Key: "target_image", Type: FieldTypeString, Description: "Image to scan", Required: true},
			{Key: "fail_on_severity", Type: FieldTypeSelect, Description: "Minimum severity to fail", Options: []string{"critical", "high", "medium", "low"}},
		},
		Outputs: []StepOutputDef{
			{Key: "vulnerabilities", Type: "[]any", Description: "Found vulnerabilities"},
			{Key: "severity_counts", Type: "map", Description: "Vulnerability counts by severity"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.scan_deps",
		Plugin:      "cicd",
		Description: "Scans project dependencies for known vulnerabilities.",
		ConfigFields: []ConfigFieldDef{
			{Key: "manifest_path", Type: FieldTypeString, Description: "Package manifest file path"},
			{Key: "fail_on_severity", Type: FieldTypeSelect, Description: "Minimum severity to fail", Options: []string{"critical", "high", "medium", "low"}},
		},
		Outputs: []StepOutputDef{
			{Key: "vulnerabilities", Type: "[]any", Description: "Vulnerable dependencies found"},
			{Key: "passed", Type: "boolean", Description: "Whether the scan passed"},
		},
	})

	// --- Platform plugin steps ---

	r.Register(&StepSchema{
		Type:        "step.platform_template",
		Plugin:      "platform",
		Description: "Resolves a platform resource template with parameters, producing capability declarations.",
		ConfigFields: []ConfigFieldDef{
			{Key: "template_name", Type: FieldTypeString, Description: "Template name to resolve", Required: true},
			{Key: "template_version", Type: FieldTypeString, Description: "Template version"},
			{Key: "parameters", Type: FieldTypeMap, Description: "Template parameters"},
		},
		Outputs: []StepOutputDef{
			{Key: "resolved_resources", Type: "[]any", Description: "Resolved capability declarations"},
			{Key: "template_name", Type: "string", Description: "Resolved template name"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.platform_plan",
		Plugin:      "platform",
		Description: "Plans infrastructure changes for a named platform resource module.",
		ConfigFields: []ConfigFieldDef{
			{Key: "module", Type: FieldTypeString, Description: "Platform resource module name", Required: true},
			{Key: "resources_from", Type: FieldTypeString, Description: "Context key containing resources to plan"},
		},
		Outputs: []StepOutputDef{
			{Key: "plan", Type: "map", Description: "Infrastructure plan"},
			{Key: "actions", Type: "[]any", Description: "Planned actions"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.platform_apply",
		Plugin:      "platform",
		Description: "Applies infrastructure changes for a named platform resource module.",
		ConfigFields: []ConfigFieldDef{
			{Key: "module", Type: FieldTypeString, Description: "Platform resource module name", Required: true},
			{Key: "resources_from", Type: FieldTypeString, Description: "Context key containing resources to apply"},
		},
		Outputs: []StepOutputDef{
			{Key: "result", Type: "map", Description: "Apply result"},
			{Key: "success", Type: "boolean", Description: "Whether the apply succeeded"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.platform_destroy",
		Plugin:      "platform",
		Description: "Destroys infrastructure resources managed by a named platform resource module.",
		ConfigFields: []ConfigFieldDef{
			{Key: "module", Type: FieldTypeString, Description: "Platform resource module name", Required: true},
			{Key: "resources_from", Type: FieldTypeString, Description: "Context key containing resources to destroy"},
		},
		Outputs: []StepOutputDef{
			{Key: "success", Type: "boolean", Description: "Whether the destroy succeeded"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.drift_check",
		Plugin:      "platform",
		Description: "Checks resources for configuration drift by comparing current vs desired state.",
		ConfigFields: []ConfigFieldDef{
			{Key: "provider_service", Type: FieldTypeString, Description: "Provider service name", Required: true},
			{Key: "resources_from", Type: FieldTypeString, Description: "Context key for applied resources", DefaultValue: "applied_resources"},
		},
		Outputs: []StepOutputDef{
			{Key: "reports", Type: "[]any", Description: "Drift report per resource"},
			{Key: "drift_detected", Type: "boolean", Description: "Whether any drift was detected"},
		},
	})

	// --- Pipeline steps plugin (additional) ---

	r.Register(&StepSchema{
		Type:        "step.circuit_breaker",
		Plugin:      "pipelinesteps",
		Description: "Tracks service failures and opens the circuit when a threshold is exceeded to prevent cascading failures.",
		ConfigFields: []ConfigFieldDef{
			{Key: "failure_threshold", Type: FieldTypeNumber, Description: "Failures before opening the circuit", DefaultValue: 5},
			{Key: "success_threshold", Type: FieldTypeNumber, Description: "Successes to close circuit in half-open state", DefaultValue: 3},
			{Key: "timeout", Type: FieldTypeDuration, Description: "Duration before transitioning to half-open", DefaultValue: "30s"},
			{Key: "service_name", Type: FieldTypeString, Description: "Service identifier for the circuit"},
		},
		Outputs: []StepOutputDef{
			{Key: "circuit_breaker", Type: "map", Description: "Status: {state, service, allowed, transitioned}"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.rate_limit",
		Plugin:      "pipelinesteps",
		Description: "Enforces rate limiting using the token bucket algorithm.",
		ConfigFields: []ConfigFieldDef{
			{Key: "requests_per_minute", Type: FieldTypeNumber, Description: "Rate limit in requests per minute", DefaultValue: 60},
			{Key: "burst_size", Type: FieldTypeNumber, Description: "Burst capacity", DefaultValue: 10},
			{Key: "key_from", Type: FieldTypeString, Description: "Template for per-client key (default: global)"},
		},
		Outputs: []StepOutputDef{
			{Key: "rate_limit", Type: "map", Description: "Rate limit result: {allowed, key}"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.constraint_check",
		Plugin:      "pipelinesteps",
		Description: "Validates capability declarations against defined constraints.",
		ConfigFields: []ConfigFieldDef{
			{Key: "resources_from", Type: FieldTypeString, Description: "Context key containing resources to validate", DefaultValue: "resources"},
			{Key: "constraints", Type: FieldTypeArray, Description: "Constraint definitions [{field, operator, value, source}]"},
		},
		Outputs: []StepOutputDef{
			{Key: "constraint_violations", Type: "[]any", Description: "List of constraint violations"},
			{Key: "constraint_summary", Type: "map", Description: "Summary: {passed, total_checked, violation_count}"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.db_create_partition",
		Plugin:      "pipelinesteps",
		Description: "Creates a PostgreSQL LIST partition for a tenant value.",
		ConfigFields: []ConfigFieldDef{
			{Key: "database", Type: FieldTypeString, Description: "Database module name", Required: true},
			{Key: "tenantKey", Type: FieldTypeString, Description: "Dot-path to tenant value", Required: true},
			{Key: "partitionKey", Type: FieldTypeString, Description: "Target partition config key"},
		},
		Outputs: []StepOutputDef{
			{Key: "synced", Type: "boolean", Description: "Whether the partition was created"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.db_sync_partitions",
		Plugin:      "pipelinesteps",
		Description: "Synchronizes partitions from source table for all managed tables.",
		ConfigFields: []ConfigFieldDef{
			{Key: "database", Type: FieldTypeString, Description: "Database module name", Required: true},
			{Key: "partitionKey", Type: FieldTypeString, Description: "Target partition key"},
		},
		Outputs: []StepOutputDef{
			{Key: "synced", Type: "boolean", Description: "Whether synchronization completed"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.s3_upload",
		Plugin:      "pipelinesteps",
		Description: "Uploads base64-encoded binary data to an S3 bucket.",
		ConfigFields: []ConfigFieldDef{
			{Key: "bucket", Type: FieldTypeString, Description: "S3 bucket name", Required: true},
			{Key: "region", Type: FieldTypeString, Description: "AWS region", Required: true},
			{Key: "key", Type: FieldTypeString, Description: "S3 key (template expressions supported)", Required: true},
			{Key: "body_from", Type: FieldTypeString, Description: "Dot-path to base64-encoded body", Required: true},
			{Key: "content_type", Type: FieldTypeString, Description: "Static MIME type"},
			{Key: "content_type_from", Type: FieldTypeString, Description: "Dot-path to MIME type"},
			{Key: "endpoint", Type: FieldTypeString, Description: "Custom S3 endpoint (for MinIO or LocalStack)"},
		},
		Outputs: []StepOutputDef{
			{Key: "url", Type: "string", Description: "Public URL of the uploaded object"},
			{Key: "key", Type: "string", Description: "Resolved S3 key"},
			{Key: "bucket", Type: "string", Description: "Bucket name"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.ui_scaffold",
		Plugin:      "pipelinesteps",
		Description: "Generates a Vite+React+TypeScript UI scaffold from an OpenAPI spec and returns a ZIP file.",
		ConfigFields: []ConfigFieldDef{
			{Key: "title", Type: FieldTypeString, Description: "UI title"},
			{Key: "theme", Type: FieldTypeString, Description: "Theme name"},
			{Key: "auth", Type: FieldTypeBool, Description: "Include authentication module"},
			{Key: "filename", Type: FieldTypeString, Description: "ZIP filename", DefaultValue: "scaffold.zip"},
		},
		Outputs: []StepOutputDef{
			{Key: "status", Type: "number", Description: "HTTP response status (200 on success)"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.ui_scaffold_analyze",
		Plugin:      "pipelinesteps",
		Description: "Analyzes an OpenAPI spec and returns parsed resource/operation structure as JSON.",
		ConfigFields: []ConfigFieldDef{
			{Key: "title", Type: FieldTypeString, Description: "UI title for scaffold analysis"},
			{Key: "theme", Type: FieldTypeString, Description: "Theme for analysis"},
		},
		Outputs: []StepOutputDef{
			{Key: "resources", Type: "[]any", Description: "Parsed API resources"},
			{Key: "operations", Type: "[]any", Description: "Parsed operations"},
		},
	})

	// --- Feature flags plugin steps ---

	r.Register(&StepSchema{
		Type:        "step.ff_gate",
		Plugin:      "featureflags",
		Description: "Evaluates a feature flag and routes pipeline execution based on the result.",
		ConfigFields: []ConfigFieldDef{
			{Key: "flag", Type: FieldTypeString, Description: "Feature flag name", Required: true},
			{Key: "on_enabled", Type: FieldTypeString, Description: "Next step name when flag is enabled", Required: true},
			{Key: "on_disabled", Type: FieldTypeString, Description: "Next step name when flag is disabled", Required: true},
			{Key: "user_from", Type: FieldTypeString, Description: "Template expression to resolve user key"},
			{Key: "group_from", Type: FieldTypeString, Description: "Template expression to resolve group"},
		},
		Outputs: []StepOutputDef{
			{Key: "flag_value", Type: "any", Description: "Evaluated flag value"},
			{Key: "enabled", Type: "boolean", Description: "Whether the flag is enabled"},
		},
	})

	// --- New pipeline steps (pipelinesteps plugin) ---

	r.Register(&StepSchema{
		Type:        "step.raw_response",
		Plugin:      "pipelinesteps",
		Description: "Writes a non-JSON HTTP response (XML, HTML, plain text) with custom status code, content type, and optional headers. Stops pipeline execution.",
		ConfigFields: []ConfigFieldDef{
			{Key: "content_type", Type: FieldTypeString, Description: "Content-Type header (e.g. text/xml)", Required: true},
			{Key: "status", Type: FieldTypeNumber, Description: "HTTP status code", DefaultValue: 200},
			{Key: "headers", Type: FieldTypeMap, Description: "Custom response headers"},
			{Key: "body", Type: FieldTypeString, Description: "Response body (template expressions supported)"},
			{Key: "body_from", Type: FieldTypeString, Description: "Dot-path to body value in pipeline context"},
		},
		Outputs: []StepOutputDef{
			{Key: "status", Type: "number", Description: "HTTP status code sent"},
			{Key: "content_type", Type: "string", Description: "Content type used"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.json_parse",
		Plugin:      "pipelinesteps",
		Description: "Parses a JSON string from pipeline context into a structured value.",
		ConfigFields: []ConfigFieldDef{
			{Key: "source", Type: FieldTypeString, Description: "Dot-path to JSON string value", Required: true},
			{Key: "target", Type: FieldTypeString, Description: "Output key name for parsed result", DefaultValue: "value"},
		},
		Outputs: []StepOutputDef{
			{Key: "(target)", Type: "any", Description: "Parsed JSON value stored under the configured target key"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.auth_validate",
		Plugin:      "pipelinesteps",
		Description: "Validates a Bearer token against a registered AuthProvider module and outputs claims.",
		ConfigFields: []ConfigFieldDef{
			{Key: "auth_module", Type: FieldTypeString, Description: "Service name of AuthProvider module", Required: true},
			{Key: "token_source", Type: FieldTypeString, Description: "Dot-path to Bearer token in pipeline context", Required: true},
			{Key: "subject_field", Type: FieldTypeString, Description: "Output field name for 'sub' claim", DefaultValue: "auth_user_id"},
		},
		Outputs: []StepOutputDef{
			{Key: "(claims)", Type: "any", Description: "All claims from AuthProvider as flat keys"},
			{Key: "(subject_field)", Type: "string", Description: "Value of 'sub' claim mapped to configured subject_field"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.token_revoke",
		Plugin:      "pipelinesteps",
		Description: "Revokes a JWT by extracting its JTI claim and adding it to a token blacklist.",
		ConfigFields: []ConfigFieldDef{
			{Key: "blacklist_module", Type: FieldTypeString, Description: "Service name of TokenBlacklist module", Required: true},
			{Key: "token_source", Type: FieldTypeString, Description: "Dot-path to Bearer token", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "revoked", Type: "boolean", Description: "Whether the JTI was added to the blacklist"},
			{Key: "jti", Type: "string", Description: "The JTI claim value (present if revoked=true)"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.field_reencrypt",
		Plugin:      "pipelinesteps",
		Description: "Re-encrypts pipeline context data with the latest key version using a ProtectedFieldManager.",
		ConfigFields: []ConfigFieldDef{
			{Key: "module", Type: FieldTypeString, Description: "Service name of ProtectedFieldManager", Required: true},
			{Key: "tenant_id", Type: FieldTypeString, Description: "Template expression for tenant ID"},
		},
		Outputs: []StepOutputDef{
			{Key: "reencrypted", Type: "boolean", Description: "Whether re-encryption was performed"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.sandbox_exec",
		Plugin:      "pipelinesteps",
		Description: "Runs a command in a hardened Docker sandbox container with resource limits.",
		ConfigFields: []ConfigFieldDef{
			{Key: "image", Type: FieldTypeString, Description: "Container image URI", DefaultValue: "cgr.dev/chainguard/wolfi-base:latest"},
			{Key: "command", Type: FieldTypeArray, Description: "Command to execute in sandbox"},
			{Key: "security_profile", Type: FieldTypeSelect, Description: "Security profile", Options: []string{"strict", "standard", "permissive"}, DefaultValue: "strict"},
			{Key: "memory_limit", Type: FieldTypeString, Description: "Memory limit (e.g. 128m, 1g)"},
			{Key: "cpu_limit", Type: FieldTypeNumber, Description: "CPU limit (e.g. 0.5)"},
			{Key: "timeout", Type: FieldTypeDuration, Description: "Execution timeout"},
			{Key: "env", Type: FieldTypeMap, Description: "Environment variables"},
			{Key: "fail_on_error", Type: FieldTypeBool, Description: "Stop pipeline if exit_code != 0", DefaultValue: true},
		},
		Outputs: []StepOutputDef{
			{Key: "exit_code", Type: "number", Description: "Container exit code"},
			{Key: "stdout", Type: "string", Description: "Standard output"},
			{Key: "stderr", Type: "string", Description: "Standard error"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.cli_print",
		Plugin:      "pipelinesteps",
		Description: "Writes a template-resolved message to stdout or stderr.",
		ConfigFields: []ConfigFieldDef{
			{Key: "message", Type: FieldTypeString, Description: "Message template (resolved against pipeline context)", Required: true},
			{Key: "newline", Type: FieldTypeBool, Description: "Append trailing newline", DefaultValue: true},
			{Key: "target", Type: FieldTypeSelect, Description: "Output destination", Options: []string{"stdout", "stderr"}, DefaultValue: "stdout"},
		},
		Outputs: []StepOutputDef{
			{Key: "printed", Type: "string", Description: "The resolved message that was written"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.cli_invoke",
		Plugin:      "pipelinesteps",
		Description: "Calls a registered Go CLI command function from the CLICommandRegistry.",
		ConfigFields: []ConfigFieldDef{
			{Key: "command", Type: FieldTypeString, Description: "Registered command name in CLICommandRegistry", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "command", Type: "string", Description: "Name of the executed command"},
			{Key: "success", Type: "boolean", Description: "Whether the command succeeded"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.graphql",
		Plugin:      "pipelinesteps",
		Description: "Executes GraphQL queries or mutations with support for pagination, batch requests, and OAuth2 authentication.",
		ConfigFields: []ConfigFieldDef{
			{Key: "url", Type: FieldTypeString, Description: "GraphQL endpoint URL (template expressions supported)", Required: true},
			{Key: "query", Type: FieldTypeString, Description: "GraphQL query or mutation"},
			{Key: "variables", Type: FieldTypeMap, Description: "Query variables (template expressions supported)"},
			{Key: "data_path", Type: FieldTypeString, Description: "Dot-separated path to extract nested data (e.g. user.profile)"},
			{Key: "headers", Type: FieldTypeMap, Description: "Custom HTTP headers"},
			{Key: "timeout", Type: FieldTypeDuration, Description: "Request timeout", DefaultValue: "30s"},
			{Key: "fail_on_graphql_errors", Type: FieldTypeBool, Description: "Fail if response contains errors", DefaultValue: true},
			{Key: "auth", Type: FieldTypeMap, Description: "Authentication config (type, token, client_id, client_secret)"},
			{Key: "pagination", Type: FieldTypeMap, Description: "Pagination config (strategy, page_info_path, cursor_variable, max_pages)"},
			{Key: "batch", Type: FieldTypeMap, Description: "Batch query config (queries array)"},
		},
		Outputs: []StepOutputDef{
			{Key: "data", Type: "any", Description: "Extracted data (after data_path applied)"},
			{Key: "errors", Type: "[]any", Description: "GraphQL errors"},
			{Key: "has_errors", Type: "boolean", Description: "Whether errors are present"},
			{Key: "status_code", Type: "number", Description: "HTTP status code"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.event_decrypt",
		Plugin:      "pipelinesteps",
		Description: "Decrypts field-level encryption applied by step.event_publish, reading CloudEvents extension attributes.",
		ConfigFields: []ConfigFieldDef{
			{Key: "key_id", Type: FieldTypeString, Description: "Key ID override (supports ${ENV_VAR} expressions)"},
		},
		Outputs: []StepOutputDef{
			{Key: "decrypted", Type: "boolean", Description: "Whether decryption was performed"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.secret_fetch",
		Plugin:      "pipelinesteps",
		Description: "Fetches one or more secrets from a named secrets module (AWS/Vault).",
		ConfigFields: []ConfigFieldDef{
			{Key: "module", Type: FieldTypeString, Description: "Service name of secrets module", Required: true},
			{Key: "secrets", Type: FieldTypeMap, Description: "Map of output key to secret ID/ARN (supports template expressions)", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "(key)", Type: "string", Description: "For each key in secrets map, the resolved secret value"},
			{Key: "fetched", Type: "boolean", Description: "Whether all secrets were fetched"},
		},
	})

	r.Register(&StepSchema{
		Type:        "step.http_proxy",
		Plugin:      "pipelinesteps",
		Description: "Forwards the original HTTP request to a dynamically resolved backend URL.",
		ConfigFields: []ConfigFieldDef{
			{Key: "backend_url_key", Type: FieldTypeString, Description: "Context key containing the backend URL", DefaultValue: "backend_url"},
			{Key: "resource_key", Type: FieldTypeString, Description: "Context key for the resource path suffix", DefaultValue: "path_params.resource"},
			{Key: "forward_headers", Type: FieldTypeArray, Description: "Headers to forward from the original request"},
			{Key: "timeout", Type: FieldTypeDuration, Description: "Request timeout", DefaultValue: "30s"},
		},
		Outputs: []StepOutputDef{
			{Key: "status_code", Type: "number", Description: "Backend response status code"},
			{Key: "proxied_to", Type: "string", Description: "The backend URL that was called"},
			{Key: "body", Type: "string", Description: "Backend response body (when no HTTP writer)"},
			{Key: "headers", Type: "map", Description: "Backend response headers (when no HTTP writer)"},
		},
	})
}
