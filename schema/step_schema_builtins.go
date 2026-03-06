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
			{Key: "routes", Type: FieldTypeMap, Description: "Map of field values to step names for branching"},
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
			{Key: "mode", Type: FieldTypeSelect, Description: "Result mode", Options: []string{"single", "list"}, DefaultValue: "single"},
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
