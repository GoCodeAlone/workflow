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
		Type:        "step.branch",
		Plugin:      "pipelinesteps",
		Description: "Switch/case routing: evaluates a field, executes only the matched branch's sub-steps inline, then jumps to merge_step. Unlike step.conditional, skipped branches never execute.",
		ConfigFields: []ConfigFieldDef{
			{Key: "field", Type: FieldTypeString, Description: "Dot-path field to evaluate for branch selection", Required: true},
			{Key: "branches", Type: FieldTypeMap, Description: "Map of field values to lists of inline step configs", Required: true},
			{Key: "default", Type: FieldTypeArray, Description: "Sub-steps to run when no branch matches", Required: false},
			{Key: "merge_step", Type: FieldTypeString, Description: "Step name to jump to after the branch completes (empty = continue sequentially)", Required: false},
		},
		Outputs: []StepOutputDef{
			{Key: "matched_value", Type: "string", Description: "The field value that was matched"},
			{Key: "branch", Type: "string", Description: "Name of the branch that was executed"},
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

	// ---- Actor Send ----

	r.Register(&StepSchema{
		Type:        "step.actor_send",
		Plugin:      "actors",
		Description: "Send a message to an actor without waiting for a response (fire-and-forget).",
		ConfigFields: []ConfigFieldDef{
			{Key: "pool", Type: FieldTypeString, Description: "Name of the actor.pool module to send to", Required: true},
			{Key: "identity", Type: FieldTypeString, Description: "Unique key for auto-managed actors"},
			{Key: "message", Type: FieldTypeJSON, Description: "Message to send (must include 'type' field)", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "delivered", Type: "boolean", Description: "Whether the message was delivered"},
		},
	})

	// ---- Actor Ask ----

	r.Register(&StepSchema{
		Type:        "step.actor_ask",
		Plugin:      "actors",
		Description: "Send a message to an actor and wait for a response.",
		ConfigFields: []ConfigFieldDef{
			{Key: "pool", Type: FieldTypeString, Description: "Name of the actor.pool module to send to", Required: true},
			{Key: "identity", Type: FieldTypeString, Description: "Unique key for auto-managed actors"},
			{Key: "timeout", Type: FieldTypeDuration, Description: "How long to wait for the actor's reply before failing", DefaultValue: "10s"},
			{Key: "message", Type: FieldTypeJSON, Description: "Message to send (must include 'type' field)", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "*", Type: "any", Description: "The actor's reply — varies by message handler"},
		},
	})

	// ---- API Gateway Apply ----

	r.Register(&StepSchema{
		Type:        "step.apigw_apply",
		Plugin:      "platform",
		Description: "Applies (provisions or updates) an API gateway configuration.",
		ConfigFields: []ConfigFieldDef{
			{Key: "gateway", Type: FieldTypeString, Description: "Name of the platform.apigateway module", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "id", Type: "string", Description: "API gateway ID"},
			{Key: "endpoint", Type: "string", Description: "Gateway endpoint URL"},
		},
	})

	// ---- API Gateway Destroy ----

	r.Register(&StepSchema{
		Type:        "step.apigw_destroy",
		Plugin:      "platform",
		Description: "Destroys a provisioned API gateway.",
		ConfigFields: []ConfigFieldDef{
			{Key: "gateway", Type: FieldTypeString, Description: "Name of the platform.apigateway module", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "destroyed", Type: "boolean", Description: "Whether the gateway was destroyed"},
		},
	})

	// ---- API Gateway Plan ----

	r.Register(&StepSchema{
		Type:        "step.apigw_plan",
		Plugin:      "platform",
		Description: "Plans API gateway changes without applying them.",
		ConfigFields: []ConfigFieldDef{
			{Key: "gateway", Type: FieldTypeString, Description: "Name of the platform.apigateway module", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "plan", Type: "string", Description: "Human-readable plan output"},
			{Key: "changes", Type: "number", Description: "Number of changes planned"},
		},
	})

	// ---- API Gateway Status ----

	r.Register(&StepSchema{
		Type:        "step.apigw_status",
		Plugin:      "platform",
		Description: "Gets the current status of an API gateway.",
		ConfigFields: []ConfigFieldDef{
			{Key: "gateway", Type: FieldTypeString, Description: "Name of the platform.apigateway module", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "status", Type: "string", Description: "Current gateway status"},
			{Key: "endpoint", Type: "string", Description: "Gateway endpoint URL"},
		},
	})

	// ---- App Deploy ----

	r.Register(&StepSchema{
		Type:        "step.app_deploy",
		Plugin:      "platform",
		Description: "Deploys an application container to the target platform.",
		ConfigFields: []ConfigFieldDef{
			{Key: "app", Type: FieldTypeString, Description: "Name of the app.container module", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "deployed", Type: "boolean", Description: "Whether deployment succeeded"},
			{Key: "endpoint", Type: "string", Description: "Service endpoint after deployment"},
		},
	})

	// ---- App Rollback ----

	r.Register(&StepSchema{
		Type:        "step.app_rollback",
		Plugin:      "platform",
		Description: "Rolls back an application container to a previous version.",
		ConfigFields: []ConfigFieldDef{
			{Key: "app", Type: FieldTypeString, Description: "Name of the app.container module", Required: true},
			{Key: "revision", Type: FieldTypeString, Description: "Target revision to roll back to (empty = previous)"},
		},
		Outputs: []StepOutputDef{
			{Key: "rolled_back", Type: "boolean", Description: "Whether rollback succeeded"},
		},
	})

	// ---- App Status ----

	r.Register(&StepSchema{
		Type:        "step.app_status",
		Plugin:      "platform",
		Description: "Gets the current deployment status of an application container.",
		ConfigFields: []ConfigFieldDef{
			{Key: "app", Type: FieldTypeString, Description: "Name of the app.container module", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "status", Type: "string", Description: "Deployment status"},
			{Key: "replicas", Type: "number", Description: "Current replica count"},
		},
	})

	// ---- Argo Delete ----

	r.Register(&StepSchema{
		Type:        "step.argo_delete",
		Plugin:      "platform",
		Description: "Deletes an Argo Workflow.",
		ConfigFields: []ConfigFieldDef{
			{Key: "service", Type: FieldTypeString, Description: "Name of the argo.workflows module", Required: true},
			{Key: "workflow_run", Type: FieldTypeString, Description: "Workflow run name to delete"},
		},
		Outputs: []StepOutputDef{
			{Key: "deleted", Type: "boolean", Description: "Whether the workflow was deleted"},
		},
	})

	// ---- Argo List ----

	r.Register(&StepSchema{
		Type:        "step.argo_list",
		Plugin:      "platform",
		Description: "Lists Argo Workflows in a namespace.",
		ConfigFields: []ConfigFieldDef{
			{Key: "service", Type: FieldTypeString, Description: "Name of the argo.workflows module", Required: true},
			{Key: "label_selector", Type: FieldTypeString, Description: "Label selector filter"},
		},
		Outputs: []StepOutputDef{
			{Key: "workflows", Type: "[]any", Description: "List of workflows"},
			{Key: "count", Type: "number", Description: "Number of workflows"},
		},
	})

	// ---- Argo Logs ----

	r.Register(&StepSchema{
		Type:        "step.argo_logs",
		Plugin:      "platform",
		Description: "Retrieves logs from an Argo Workflow.",
		ConfigFields: []ConfigFieldDef{
			{Key: "service", Type: FieldTypeString, Description: "Name of the argo.workflows module", Required: true},
			{Key: "workflow_run", Type: FieldTypeString, Description: "Workflow run name to get logs for"},
		},
		Outputs: []StepOutputDef{
			{Key: "logs", Type: "string", Description: "Workflow logs"},
		},
	})

	// ---- Argo Status ----

	r.Register(&StepSchema{
		Type:        "step.argo_status",
		Plugin:      "platform",
		Description: "Gets the status of an Argo Workflow.",
		ConfigFields: []ConfigFieldDef{
			{Key: "service", Type: FieldTypeString, Description: "Name of the argo.workflows module", Required: true},
			{Key: "workflow_run", Type: FieldTypeString, Description: "Workflow run name to check status"},
		},
		Outputs: []StepOutputDef{
			{Key: "phase", Type: "string", Description: "Workflow phase (Pending, Running, Succeeded, Failed)"},
			{Key: "message", Type: "string", Description: "Status message"},
		},
	})

	// ---- Argo Submit ----

	r.Register(&StepSchema{
		Type:        "step.argo_submit",
		Plugin:      "platform",
		Description: "Submits an Argo Workflow from a template or manifest.",
		ConfigFields: []ConfigFieldDef{
			{Key: "service", Type: FieldTypeString, Description: "Name of the argo.workflows module", Required: true},
			{Key: "workflow_name", Type: FieldTypeString, Description: "Workflow template name (defaults to step name)"},
			{Key: "steps", Type: FieldTypeArray, Description: "Workflow step definitions"},
		},
		Outputs: []StepOutputDef{
			{Key: "name", Type: "string", Description: "Created workflow name"},
			{Key: "namespace", Type: "string", Description: "Workflow namespace"},
		},
	})

	// ---- Artifact Delete ----

	r.Register(&StepSchema{
		Type:        "step.artifact_delete",
		Plugin:      "storage",
		Description: "Deletes an artifact from the artifact store.",
		ConfigFields: []ConfigFieldDef{
			{Key: "store", Type: FieldTypeString, Description: "Name of the storage.artifact module", Required: true},
			{Key: "key", Type: FieldTypeString, Description: "Artifact key to delete", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "deleted", Type: "boolean", Description: "Whether the artifact was deleted"},
		},
	})

	// ---- Artifact Download ----

	r.Register(&StepSchema{
		Type:        "step.artifact_download",
		Plugin:      "storage",
		Description: "Downloads an artifact from the artifact store.",
		ConfigFields: []ConfigFieldDef{
			{Key: "store", Type: FieldTypeString, Description: "Name of the storage.artifact module", Required: true},
			{Key: "key", Type: FieldTypeString, Description: "Artifact key to download", Required: true},
			{Key: "dest", Type: FieldTypeString, Description: "Local path to write the artifact"},
		},
		Outputs: []StepOutputDef{
			{Key: "path", Type: "string", Description: "Local path where artifact was written"},
			{Key: "size", Type: "number", Description: "Artifact size in bytes"},
		},
	})

	// ---- Artifact List ----

	r.Register(&StepSchema{
		Type:        "step.artifact_list",
		Plugin:      "storage",
		Description: "Lists artifacts in the artifact store.",
		ConfigFields: []ConfigFieldDef{
			{Key: "store", Type: FieldTypeString, Description: "Name of the storage.artifact module", Required: true},
			{Key: "prefix", Type: FieldTypeString, Description: "Optional prefix filter"},
		},
		Outputs: []StepOutputDef{
			{Key: "artifacts", Type: "[]any", Description: "List of artifact metadata"},
			{Key: "count", Type: "number", Description: "Number of artifacts"},
		},
	})

	// ---- Artifact Upload ----

	r.Register(&StepSchema{
		Type:        "step.artifact_upload",
		Plugin:      "storage",
		Description: "Uploads a file as an artifact to the artifact store.",
		ConfigFields: []ConfigFieldDef{
			{Key: "store", Type: FieldTypeString, Description: "Name of the storage.artifact module", Required: true},
			{Key: "key", Type: FieldTypeString, Description: "Artifact key (storage path)", Required: true},
			{Key: "source", Type: FieldTypeString, Description: "Local file path to upload", Required: true},
			{Key: "metadata", Type: FieldTypeMap, Description: "Additional metadata to attach"},
		},
		Outputs: []StepOutputDef{
			{Key: "key", Type: "string", Description: "Stored artifact key"},
			{Key: "store", Type: "string", Description: "Name of the store used"},
		},
	})

	// ---- Build Binary ----

	r.Register(&StepSchema{
		Type:        "step.build_binary",
		Plugin:      "cicd",
		Description: "Builds a Go binary from source.",
		ConfigFields: []ConfigFieldDef{
			{Key: "source", Type: FieldTypeString, Description: "Source directory or package path", Required: true},
			{Key: "output", Type: FieldTypeString, Description: "Output binary path"},
			{Key: "os", Type: FieldTypeString, Description: "Target OS (GOOS)"},
			{Key: "arch", Type: FieldTypeString, Description: "Target architecture (GOARCH)"},
			{Key: "ldflags", Type: FieldTypeString, Description: "Linker flags"},
		},
		Outputs: []StepOutputDef{
			{Key: "binary_path", Type: "string", Description: "Path to the built binary"},
		},
	})

	// ---- Build From Config ----

	r.Register(&StepSchema{
		Type:        "step.build_from_config",
		Plugin:      "cicd",
		Description: "Builds an application using the workflow engine config as build specification.",
		ConfigFields: []ConfigFieldDef{
			{Key: "config", Type: FieldTypeString, Description: "Path to build config file", Required: true},
			{Key: "target", Type: FieldTypeString, Description: "Build target name"},
		},
		Outputs: []StepOutputDef{
			{Key: "artifact", Type: "string", Description: "Path to the built artifact"},
			{Key: "success", Type: "boolean", Description: "Whether the build succeeded"},
		},
	})

	// ---- Cloud Validate ----

	r.Register(&StepSchema{
		Type:        "step.cloud_validate",
		Plugin:      "cloud",
		Description: "Validates cloud provider credentials and connectivity.",
		ConfigFields: []ConfigFieldDef{
			{Key: "account", Type: FieldTypeString, Description: "Name of the cloud.account module", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "valid", Type: "boolean", Description: "Whether credentials are valid"},
			{Key: "provider", Type: "string", Description: "Cloud provider name"},
			{Key: "region", Type: "string", Description: "Configured region"},
		},
	})

	// ---- CodeBuild Create Project ----

	r.Register(&StepSchema{
		Type:        "step.codebuild_create_project",
		Plugin:      "cicd",
		Description: "Creates an AWS CodeBuild project.",
		ConfigFields: []ConfigFieldDef{
			{Key: "project", Type: FieldTypeString, Description: "Name of the aws.codebuild module", Required: true},
			{Key: "project_name", Type: FieldTypeString, Description: "CodeBuild project name", Required: true},
			{Key: "source", Type: FieldTypeMap, Description: "Source configuration (type, location)"},
			{Key: "environment", Type: FieldTypeMap, Description: "Build environment configuration"},
		},
		Outputs: []StepOutputDef{
			{Key: "project_name", Type: "string", Description: "Created project name"},
			{Key: "arn", Type: "string", Description: "Project ARN"},
		},
	})

	// ---- CodeBuild Delete Project ----

	r.Register(&StepSchema{
		Type:        "step.codebuild_delete_project",
		Plugin:      "cicd",
		Description: "Deletes an AWS CodeBuild project.",
		ConfigFields: []ConfigFieldDef{
			{Key: "project", Type: FieldTypeString, Description: "Name of the aws.codebuild module", Required: true},
			{Key: "project_name", Type: FieldTypeString, Description: "CodeBuild project name to delete", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "deleted", Type: "boolean", Description: "Whether the project was deleted"},
		},
	})

	// ---- CodeBuild List Builds ----

	r.Register(&StepSchema{
		Type:        "step.codebuild_list_builds",
		Plugin:      "cicd",
		Description: "Lists builds for an AWS CodeBuild project.",
		ConfigFields: []ConfigFieldDef{
			{Key: "project", Type: FieldTypeString, Description: "Name of the aws.codebuild module", Required: true},
			{Key: "project_name", Type: FieldTypeString, Description: "CodeBuild project name", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "builds", Type: "[]any", Description: "List of build IDs"},
			{Key: "count", Type: "number", Description: "Number of builds"},
		},
	})

	// ---- CodeBuild Logs ----

	r.Register(&StepSchema{
		Type:        "step.codebuild_logs",
		Plugin:      "cicd",
		Description: "Retrieves logs for an AWS CodeBuild build.",
		ConfigFields: []ConfigFieldDef{
			{Key: "project", Type: FieldTypeString, Description: "Name of the aws.codebuild module", Required: true},
			{Key: "build_id", Type: FieldTypeString, Description: "CodeBuild build ID", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "logs", Type: "string", Description: "Build log output"},
		},
	})

	// ---- CodeBuild Start ----

	r.Register(&StepSchema{
		Type:        "step.codebuild_start",
		Plugin:      "cicd",
		Description: "Starts an AWS CodeBuild build.",
		ConfigFields: []ConfigFieldDef{
			{Key: "project", Type: FieldTypeString, Description: "Name of the aws.codebuild module", Required: true},
			{Key: "project_name", Type: FieldTypeString, Description: "CodeBuild project name", Required: true},
			{Key: "env_override", Type: FieldTypeMap, Description: "Environment variable overrides"},
		},
		Outputs: []StepOutputDef{
			{Key: "build_id", Type: "string", Description: "Started build ID"},
			{Key: "status", Type: "string", Description: "Initial build status"},
		},
	})

	// ---- CodeBuild Status ----

	r.Register(&StepSchema{
		Type:        "step.codebuild_status",
		Plugin:      "cicd",
		Description: "Gets the status of an AWS CodeBuild build.",
		ConfigFields: []ConfigFieldDef{
			{Key: "project", Type: FieldTypeString, Description: "Name of the aws.codebuild module", Required: true},
			{Key: "build_id", Type: FieldTypeString, Description: "CodeBuild build ID", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "status", Type: "string", Description: "Build status (IN_PROGRESS, SUCCEEDED, FAILED, etc.)"},
			{Key: "phase", Type: "string", Description: "Current build phase"},
		},
	})

	// ---- DNS Apply ----

	r.Register(&StepSchema{
		Type:        "step.dns_apply",
		Plugin:      "platform",
		Description: "Applies DNS zone and record changes.",
		ConfigFields: []ConfigFieldDef{
			{Key: "zone", Type: FieldTypeString, Description: "Name of the platform.dns module", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "zone_id", Type: "string", Description: "DNS zone ID"},
			{Key: "nameservers", Type: "[]string", Description: "Zone nameservers"},
		},
	})

	// ---- DNS Plan ----

	r.Register(&StepSchema{
		Type:        "step.dns_plan",
		Plugin:      "platform",
		Description: "Plans DNS changes without applying them.",
		ConfigFields: []ConfigFieldDef{
			{Key: "zone", Type: FieldTypeString, Description: "Name of the platform.dns module", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "plan", Type: "string", Description: "Human-readable plan output"},
			{Key: "changes", Type: "number", Description: "Number of changes planned"},
		},
	})

	// ---- DNS Status ----

	r.Register(&StepSchema{
		Type:        "step.dns_status",
		Plugin:      "platform",
		Description: "Gets the current status of a DNS zone.",
		ConfigFields: []ConfigFieldDef{
			{Key: "zone", Type: FieldTypeString, Description: "Name of the platform.dns module", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "status", Type: "string", Description: "Zone status"},
			{Key: "zone_id", Type: "string", Description: "DNS zone ID"},
		},
	})

	// ---- DigitalOcean Deploy ----

	r.Register(&StepSchema{
		Type:        "step.do_deploy",
		Plugin:      "platform",
		Description: "Deploys an application to DigitalOcean App Platform.",
		ConfigFields: []ConfigFieldDef{
			{Key: "app", Type: FieldTypeString, Description: "Name of the platform.do_app module", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "app_id", Type: "string", Description: "DigitalOcean app ID"},
			{Key: "live_url", Type: "string", Description: "App live URL"},
		},
	})

	// ---- DigitalOcean Destroy ----

	r.Register(&StepSchema{
		Type:        "step.do_destroy",
		Plugin:      "platform",
		Description: "Destroys a DigitalOcean App Platform application.",
		ConfigFields: []ConfigFieldDef{
			{Key: "app", Type: FieldTypeString, Description: "Name of the platform.do_app module", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "destroyed", Type: "boolean", Description: "Whether the app was destroyed"},
		},
	})

	// ---- DigitalOcean Logs ----

	r.Register(&StepSchema{
		Type:        "step.do_logs",
		Plugin:      "platform",
		Description: "Retrieves logs from a DigitalOcean App Platform application.",
		ConfigFields: []ConfigFieldDef{
			{Key: "app", Type: FieldTypeString, Description: "Name of the platform.do_app module", Required: true},
			{Key: "component", Type: FieldTypeString, Description: "App component name"},
		},
		Outputs: []StepOutputDef{
			{Key: "logs", Type: "string", Description: "Application log output"},
		},
	})

	// ---- DigitalOcean Scale ----

	r.Register(&StepSchema{
		Type:        "step.do_scale",
		Plugin:      "platform",
		Description: "Scales a DigitalOcean App Platform application.",
		ConfigFields: []ConfigFieldDef{
			{Key: "app", Type: FieldTypeString, Description: "Name of the platform.do_app module", Required: true},
			{Key: "instances", Type: FieldTypeNumber, Description: "Desired instance count", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "instances", Type: "number", Description: "New instance count"},
		},
	})

	// ---- DigitalOcean Status ----

	r.Register(&StepSchema{
		Type:        "step.do_status",
		Plugin:      "platform",
		Description: "Gets the status of a DigitalOcean App Platform application.",
		ConfigFields: []ConfigFieldDef{
			{Key: "app", Type: FieldTypeString, Description: "Name of the platform.do_app module", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "phase", Type: "string", Description: "App deployment phase"},
			{Key: "live_url", Type: "string", Description: "App live URL"},
		},
	})

	// ---- ECS Apply ----

	r.Register(&StepSchema{
		Type:        "step.ecs_apply",
		Plugin:      "platform",
		Description: "Applies (deploys) an ECS Fargate service.",
		ConfigFields: []ConfigFieldDef{
			{Key: "service", Type: FieldTypeString, Description: "Name of the platform.ecs module", Required: true},
			{Key: "image", Type: FieldTypeString, Description: "Container image to deploy"},
		},
		Outputs: []StepOutputDef{
			{Key: "service_arn", Type: "string", Description: "ECS service ARN"},
			{Key: "status", Type: "string", Description: "Service status"},
		},
	})

	// ---- ECS Destroy ----

	r.Register(&StepSchema{
		Type:        "step.ecs_destroy",
		Plugin:      "platform",
		Description: "Destroys an ECS Fargate service.",
		ConfigFields: []ConfigFieldDef{
			{Key: "service", Type: FieldTypeString, Description: "Name of the platform.ecs module", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "destroyed", Type: "boolean", Description: "Whether the service was destroyed"},
		},
	})

	// ---- ECS Plan ----

	r.Register(&StepSchema{
		Type:        "step.ecs_plan",
		Plugin:      "platform",
		Description: "Plans ECS service deployment changes without applying them.",
		ConfigFields: []ConfigFieldDef{
			{Key: "service", Type: FieldTypeString, Description: "Name of the platform.ecs module", Required: true},
			{Key: "image", Type: FieldTypeString, Description: "Container image to plan for"},
		},
		Outputs: []StepOutputDef{
			{Key: "plan", Type: "string", Description: "Human-readable plan output"},
			{Key: "changes", Type: "number", Description: "Number of changes planned"},
		},
	})

	// ---- ECS Status ----

	r.Register(&StepSchema{
		Type:        "step.ecs_status",
		Plugin:      "platform",
		Description: "Gets the status of an ECS Fargate service.",
		ConfigFields: []ConfigFieldDef{
			{Key: "service", Type: FieldTypeString, Description: "Name of the platform.ecs module", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "status", Type: "string", Description: "Service status"},
			{Key: "running_count", Type: "number", Description: "Number of running tasks"},
		},
	})

	// ---- Git Checkout ----

	r.Register(&StepSchema{
		Type:        "step.git_checkout",
		Plugin:      "cicd",
		Description: "Checks out a Git branch, tag, or commit.",
		ConfigFields: []ConfigFieldDef{
			{Key: "path", Type: FieldTypeString, Description: "Local repository path", Required: true},
			{Key: "ref", Type: FieldTypeString, Description: "Branch, tag, or commit SHA to checkout", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "commit", Type: "string", Description: "HEAD commit SHA after checkout"},
			{Key: "branch", Type: "string", Description: "Current branch name"},
		},
	})

	// ---- Git Clone ----

	r.Register(&StepSchema{
		Type:        "step.git_clone",
		Plugin:      "cicd",
		Description: "Clones a Git repository to a local path.",
		ConfigFields: []ConfigFieldDef{
			{Key: "url", Type: FieldTypeString, Description: "Repository URL to clone", Required: true},
			{Key: "path", Type: FieldTypeString, Description: "Local path to clone into", Required: true},
			{Key: "branch", Type: FieldTypeString, Description: "Branch to clone (default: main)"},
			{Key: "depth", Type: FieldTypeNumber, Description: "Clone depth (0 = full history)"},
			{Key: "token", Type: FieldTypeString, Description: "Auth token for private repos", Sensitive: true},
		},
		Outputs: []StepOutputDef{
			{Key: "commit", Type: "string", Description: "HEAD commit SHA"},
			{Key: "path", Type: "string", Description: "Local path where repo was cloned"},
		},
	})

	// ---- Git Commit ----

	r.Register(&StepSchema{
		Type:        "step.git_commit",
		Plugin:      "cicd",
		Description: "Creates a Git commit with staged changes.",
		ConfigFields: []ConfigFieldDef{
			{Key: "path", Type: FieldTypeString, Description: "Local repository path", Required: true},
			{Key: "message", Type: FieldTypeString, Description: "Commit message", Required: true},
			{Key: "author_name", Type: FieldTypeString, Description: "Commit author name"},
			{Key: "author_email", Type: FieldTypeString, Description: "Commit author email"},
			{Key: "add_all", Type: FieldTypeBool, Description: "Stage all changes before committing"},
		},
		Outputs: []StepOutputDef{
			{Key: "sha", Type: "string", Description: "New commit SHA"},
		},
	})

	// ---- Git Push ----

	r.Register(&StepSchema{
		Type:        "step.git_push",
		Plugin:      "cicd",
		Description: "Pushes local commits to a remote Git repository.",
		ConfigFields: []ConfigFieldDef{
			{Key: "path", Type: FieldTypeString, Description: "Local repository path", Required: true},
			{Key: "remote", Type: FieldTypeString, Description: "Remote name (default: origin)", DefaultValue: "origin"},
			{Key: "branch", Type: FieldTypeString, Description: "Branch to push"},
			{Key: "token", Type: FieldTypeString, Description: "Auth token for private repos", Sensitive: true},
			{Key: "force", Type: FieldTypeBool, Description: "Force push"},
		},
		Outputs: []StepOutputDef{
			{Key: "pushed", Type: "boolean", Description: "Whether push succeeded"},
		},
	})

	// ---- Git Tag ----

	r.Register(&StepSchema{
		Type:        "step.git_tag",
		Plugin:      "cicd",
		Description: "Creates a Git tag on the current commit.",
		ConfigFields: []ConfigFieldDef{
			{Key: "path", Type: FieldTypeString, Description: "Local repository path", Required: true},
			{Key: "tag", Type: FieldTypeString, Description: "Tag name to create", Required: true},
			{Key: "message", Type: FieldTypeString, Description: "Annotated tag message (empty = lightweight tag)"},
			{Key: "push", Type: FieldTypeBool, Description: "Push tag to remote after creating"},
		},
		Outputs: []StepOutputDef{
			{Key: "tag", Type: "string", Description: "Created tag name"},
			{Key: "sha", Type: "string", Description: "Tagged commit SHA"},
		},
	})

	// ---- GitLab Create MR ----

	r.Register(&StepSchema{
		Type:        "step.gitlab_create_mr",
		Plugin:      "gitlab",
		Description: "Creates a GitLab merge request.",
		ConfigFields: []ConfigFieldDef{
			{Key: "client", Type: FieldTypeString, Description: "Name of the gitlab.client module", Required: true},
			{Key: "project_id", Type: FieldTypeString, Description: "GitLab project ID or path", Required: true},
			{Key: "source_branch", Type: FieldTypeString, Description: "Source branch for the MR", Required: true},
			{Key: "target_branch", Type: FieldTypeString, Description: "Target branch for the MR", Required: true},
			{Key: "title", Type: FieldTypeString, Description: "MR title", Required: true},
			{Key: "description", Type: FieldTypeString, Description: "MR description"},
		},
		Outputs: []StepOutputDef{
			{Key: "iid", Type: "number", Description: "Merge request internal ID"},
			{Key: "url", Type: "string", Description: "Merge request URL"},
		},
	})

	// ---- GitLab MR Comment ----

	r.Register(&StepSchema{
		Type:        "step.gitlab_mr_comment",
		Plugin:      "gitlab",
		Description: "Adds a comment to a GitLab merge request.",
		ConfigFields: []ConfigFieldDef{
			{Key: "client", Type: FieldTypeString, Description: "Name of the gitlab.client module", Required: true},
			{Key: "project_id", Type: FieldTypeString, Description: "GitLab project ID or path", Required: true},
			{Key: "mr_iid", Type: FieldTypeNumber, Description: "Merge request internal ID", Required: true},
			{Key: "body", Type: FieldTypeString, Description: "Comment body text", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "note_id", Type: "number", Description: "Created note ID"},
		},
	})

	// ---- GitLab Parse Webhook ----

	r.Register(&StepSchema{
		Type:        "step.gitlab_parse_webhook",
		Plugin:      "gitlab",
		Description: "Parses and validates a GitLab webhook payload.",
		ConfigFields: []ConfigFieldDef{
			{Key: "webhook", Type: FieldTypeString, Description: "Name of the gitlab.webhook module", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "event_type", Type: "string", Description: "GitLab event type (push, tag, merge_request, etc.)"},
			{Key: "project_id", Type: "number", Description: "GitLab project ID"},
			{Key: "payload", Type: "any", Description: "Parsed webhook payload"},
		},
	})

	// ---- GitLab Pipeline Status ----

	r.Register(&StepSchema{
		Type:        "step.gitlab_pipeline_status",
		Plugin:      "gitlab",
		Description: "Gets the status of a GitLab pipeline.",
		ConfigFields: []ConfigFieldDef{
			{Key: "client", Type: FieldTypeString, Description: "Name of the gitlab.client module", Required: true},
			{Key: "project_id", Type: FieldTypeString, Description: "GitLab project ID or path", Required: true},
			{Key: "pipeline_id", Type: FieldTypeNumber, Description: "Pipeline ID", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "status", Type: "string", Description: "Pipeline status (running, success, failed, etc.)"},
			{Key: "url", Type: "string", Description: "Pipeline web URL"},
		},
	})

	// ---- GitLab Trigger Pipeline ----

	r.Register(&StepSchema{
		Type:        "step.gitlab_trigger_pipeline",
		Plugin:      "gitlab",
		Description: "Triggers a GitLab CI/CD pipeline.",
		ConfigFields: []ConfigFieldDef{
			{Key: "client", Type: FieldTypeString, Description: "Name of the gitlab.client module", Required: true},
			{Key: "project_id", Type: FieldTypeString, Description: "GitLab project ID or path", Required: true},
			{Key: "ref", Type: FieldTypeString, Description: "Branch or tag to run pipeline on", Required: true},
			{Key: "variables", Type: FieldTypeMap, Description: "Pipeline variables to pass"},
		},
		Outputs: []StepOutputDef{
			{Key: "pipeline_id", Type: "number", Description: "Triggered pipeline ID"},
			{Key: "status", Type: "string", Description: "Initial pipeline status"},
			{Key: "url", Type: "string", Description: "Pipeline web URL"},
		},
	})

	// ---- IaC Apply ----

	r.Register(&StepSchema{
		Type:        "step.iac_apply",
		Plugin:      "platform",
		Description: "Applies infrastructure changes from an IaC plan.",
		ConfigFields: []ConfigFieldDef{
			{Key: "platform", Type: FieldTypeString, Description: "Name of the platform module", Required: true},
			{Key: "resource_id", Type: FieldTypeString, Description: "Resource ID to manage"},
			{Key: "state_store", Type: FieldTypeString, Description: "Name of the iac.state module for state backend"},
		},
		Outputs: []StepOutputDef{
			{Key: "applied", Type: "boolean", Description: "Whether changes were applied"},
			{Key: "resources", Type: "[]any", Description: "Applied resource details"},
		},
	})

	// ---- IaC Destroy ----

	r.Register(&StepSchema{
		Type:        "step.iac_destroy",
		Plugin:      "platform",
		Description: "Destroys infrastructure managed by an IaC module.",
		ConfigFields: []ConfigFieldDef{
			{Key: "platform", Type: FieldTypeString, Description: "Name of the platform module", Required: true},
			{Key: "resource_id", Type: FieldTypeString, Description: "Resource ID to destroy"},
			{Key: "state_store", Type: FieldTypeString, Description: "Name of the iac.state module for state backend"},
		},
		Outputs: []StepOutputDef{
			{Key: "destroyed", Type: "boolean", Description: "Whether resources were destroyed"},
		},
	})

	// ---- IaC Drift Detect ----

	r.Register(&StepSchema{
		Type:        "step.iac_drift_detect",
		Plugin:      "platform",
		Description: "Detects configuration drift between IaC state and actual infrastructure.",
		ConfigFields: []ConfigFieldDef{
			{Key: "platform", Type: FieldTypeString, Description: "Name of the platform module", Required: true},
			{Key: "resource_id", Type: FieldTypeString, Description: "Resource ID to check"},
			{Key: "state_store", Type: FieldTypeString, Description: "Name of the iac.state module for state backend"},
			{Key: "config", Type: FieldTypeMap, Description: "Current config to compare against state"},
		},
		Outputs: []StepOutputDef{
			{Key: "has_drift", Type: "boolean", Description: "Whether drift was detected"},
			{Key: "drifted_resources", Type: "[]any", Description: "List of drifted resources"},
		},
	})

	// ---- IaC Plan ----

	r.Register(&StepSchema{
		Type:        "step.iac_plan",
		Plugin:      "platform",
		Description: "Plans infrastructure changes without applying them.",
		ConfigFields: []ConfigFieldDef{
			{Key: "platform", Type: FieldTypeString, Description: "Name of the platform module", Required: true},
			{Key: "resource_id", Type: FieldTypeString, Description: "Resource ID to plan"},
			{Key: "state_store", Type: FieldTypeString, Description: "Name of the iac.state module for state backend"},
		},
		Outputs: []StepOutputDef{
			{Key: "plan", Type: "string", Description: "Human-readable plan output"},
			{Key: "changes", Type: "number", Description: "Number of changes planned"},
		},
	})

	// ---- IaC Status ----

	r.Register(&StepSchema{
		Type:        "step.iac_status",
		Plugin:      "platform",
		Description: "Gets the current provisioning status of IaC-managed infrastructure.",
		ConfigFields: []ConfigFieldDef{
			{Key: "platform", Type: FieldTypeString, Description: "Name of the platform module", Required: true},
			{Key: "resource_id", Type: FieldTypeString, Description: "Resource ID to query"},
			{Key: "state_store", Type: FieldTypeString, Description: "Name of the iac.state module for state backend"},
		},
		Outputs: []StepOutputDef{
			{Key: "status", Type: "string", Description: "Provisioning status"},
			{Key: "resources", Type: "[]any", Description: "Managed resource states"},
		},
	})

	// ---- Kubernetes Apply ----

	r.Register(&StepSchema{
		Type:        "step.k8s_apply",
		Plugin:      "platform",
		Description: "Applies Kubernetes manifests to a cluster.",
		ConfigFields: []ConfigFieldDef{
			{Key: "cluster", Type: FieldTypeString, Description: "Name of the platform.kubernetes module", Required: true},
			{Key: "manifest", Type: FieldTypeString, Description: "YAML manifest or path to manifest file"},
			{Key: "namespace", Type: FieldTypeString, Description: "Kubernetes namespace"},
		},
		Outputs: []StepOutputDef{
			{Key: "applied", Type: "boolean", Description: "Whether manifests were applied"},
			{Key: "resources", Type: "[]any", Description: "Applied resource summaries"},
		},
	})

	// ---- Kubernetes Destroy ----

	r.Register(&StepSchema{
		Type:        "step.k8s_destroy",
		Plugin:      "platform",
		Description: "Deletes Kubernetes resources.",
		ConfigFields: []ConfigFieldDef{
			{Key: "cluster", Type: FieldTypeString, Description: "Name of the platform.kubernetes module", Required: true},
			{Key: "manifest", Type: FieldTypeString, Description: "YAML manifest or path to manifest file"},
			{Key: "namespace", Type: FieldTypeString, Description: "Kubernetes namespace"},
		},
		Outputs: []StepOutputDef{
			{Key: "destroyed", Type: "boolean", Description: "Whether resources were deleted"},
		},
	})

	// ---- Kubernetes Plan ----

	r.Register(&StepSchema{
		Type:        "step.k8s_plan",
		Plugin:      "platform",
		Description: "Diffs Kubernetes manifests against the current cluster state.",
		ConfigFields: []ConfigFieldDef{
			{Key: "cluster", Type: FieldTypeString, Description: "Name of the platform.kubernetes module", Required: true},
			{Key: "manifest", Type: FieldTypeString, Description: "YAML manifest or path to manifest file"},
			{Key: "namespace", Type: FieldTypeString, Description: "Kubernetes namespace"},
		},
		Outputs: []StepOutputDef{
			{Key: "diff", Type: "string", Description: "Diff output"},
			{Key: "changes", Type: "number", Description: "Number of changes detected"},
		},
	})

	// ---- Kubernetes Status ----

	r.Register(&StepSchema{
		Type:        "step.k8s_status",
		Plugin:      "platform",
		Description: "Gets the status of Kubernetes resources.",
		ConfigFields: []ConfigFieldDef{
			{Key: "cluster", Type: FieldTypeString, Description: "Name of the platform.kubernetes module", Required: true},
			{Key: "kind", Type: FieldTypeString, Description: "Resource kind (Deployment, Service, etc.)"},
			{Key: "name", Type: FieldTypeString, Description: "Resource name"},
			{Key: "namespace", Type: FieldTypeString, Description: "Kubernetes namespace"},
		},
		Outputs: []StepOutputDef{
			{Key: "status", Type: "string", Description: "Resource status"},
			{Key: "ready", Type: "boolean", Description: "Whether resource is ready"},
		},
	})

	// ---- Marketplace Detail ----

	r.Register(&StepSchema{
		Type:        "step.marketplace_detail",
		Plugin:      "marketplace",
		Description: "Gets detailed information about a marketplace plugin.",
		ConfigFields: []ConfigFieldDef{
			{Key: "plugin", Type: FieldTypeString, Description: "Plugin name to get details for", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "plugin", Type: "any", Description: "Plugin metadata and details"},
		},
	})

	// ---- Marketplace Install ----

	r.Register(&StepSchema{
		Type:        "step.marketplace_install",
		Plugin:      "marketplace",
		Description: "Installs a plugin from the marketplace.",
		ConfigFields: []ConfigFieldDef{
			{Key: "plugin", Type: FieldTypeString, Description: "Plugin name to install", Required: true},
			{Key: "version", Type: FieldTypeString, Description: "Plugin version (default: latest)"},
		},
		Outputs: []StepOutputDef{
			{Key: "installed", Type: "boolean", Description: "Whether installation succeeded"},
			{Key: "version", Type: "string", Description: "Installed plugin version"},
		},
	})

	// ---- Marketplace Installed ----

	r.Register(&StepSchema{
		Type:        "step.marketplace_installed",
		Plugin:      "marketplace",
		Description: "Lists installed marketplace plugins.",
		ConfigFields: []ConfigFieldDef{},
		Outputs: []StepOutputDef{
			{Key: "plugins", Type: "[]any", Description: "List of installed plugin metadata"},
			{Key: "count", Type: "number", Description: "Number of installed plugins"},
		},
	})

	// ---- Marketplace Search ----

	r.Register(&StepSchema{
		Type:        "step.marketplace_search",
		Plugin:      "marketplace",
		Description: "Searches the plugin marketplace.",
		ConfigFields: []ConfigFieldDef{
			{Key: "query", Type: FieldTypeString, Description: "Search query string"},
			{Key: "category", Type: FieldTypeString, Description: "Filter by category"},
		},
		Outputs: []StepOutputDef{
			{Key: "results", Type: "[]any", Description: "Search result plugins"},
			{Key: "count", Type: "number", Description: "Number of results"},
		},
	})

	// ---- Marketplace Uninstall ----

	r.Register(&StepSchema{
		Type:        "step.marketplace_uninstall",
		Plugin:      "marketplace",
		Description: "Uninstalls a marketplace plugin.",
		ConfigFields: []ConfigFieldDef{
			{Key: "plugin", Type: FieldTypeString, Description: "Plugin name to uninstall", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "uninstalled", Type: "boolean", Description: "Whether uninstall succeeded"},
		},
	})

	// ---- Marketplace Update ----

	r.Register(&StepSchema{
		Type:        "step.marketplace_update",
		Plugin:      "marketplace",
		Description: "Updates an installed marketplace plugin to a newer version.",
		ConfigFields: []ConfigFieldDef{
			{Key: "plugin", Type: FieldTypeString, Description: "Plugin name to update", Required: true},
			{Key: "version", Type: FieldTypeString, Description: "Target version (default: latest)"},
		},
		Outputs: []StepOutputDef{
			{Key: "updated", Type: "boolean", Description: "Whether update succeeded"},
			{Key: "version", Type: "string", Description: "Updated plugin version"},
		},
	})

	// ---- Network Apply ----

	r.Register(&StepSchema{
		Type:        "step.network_apply",
		Plugin:      "platform",
		Description: "Applies VPC networking changes.",
		ConfigFields: []ConfigFieldDef{
			{Key: "network", Type: FieldTypeString, Description: "Name of the platform.networking module", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "vpc_id", Type: "string", Description: "VPC ID"},
			{Key: "subnet_ids", Type: "[]string", Description: "Created subnet IDs"},
		},
	})

	// ---- Network Plan ----

	r.Register(&StepSchema{
		Type:        "step.network_plan",
		Plugin:      "platform",
		Description: "Plans VPC networking changes without applying them.",
		ConfigFields: []ConfigFieldDef{
			{Key: "network", Type: FieldTypeString, Description: "Name of the platform.networking module", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "plan", Type: "string", Description: "Human-readable plan output"},
			{Key: "changes", Type: "number", Description: "Number of changes planned"},
		},
	})

	// ---- Network Status ----

	r.Register(&StepSchema{
		Type:        "step.network_status",
		Plugin:      "platform",
		Description: "Gets the status of VPC networking resources.",
		ConfigFields: []ConfigFieldDef{
			{Key: "network", Type: FieldTypeString, Description: "Name of the platform.networking module", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "status", Type: "string", Description: "Network status"},
			{Key: "vpc_id", Type: "string", Description: "VPC ID"},
		},
	})

	// ---- NoSQL Delete ----

	r.Register(&StepSchema{
		Type:        "step.nosql_delete",
		Plugin:      "datastores",
		Description: "Deletes an item from a NoSQL store by key.",
		ConfigFields: []ConfigFieldDef{
			{Key: "store", Type: FieldTypeString, Description: "Name of the nosql.* module", Required: true},
			{Key: "key", Type: FieldTypeString, Description: "Item key to delete", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "deleted", Type: "boolean", Description: "Whether the item was deleted"},
		},
	})

	// ---- Policy Evaluate ----

	r.Register(&StepSchema{
		Type:        "step.policy_evaluate",
		Plugin:      "policy",
		Description: "Evaluates input data against a loaded policy and returns an allow/deny decision.",
		ConfigFields: []ConfigFieldDef{
			{Key: "engine", Type: FieldTypeString, Description: "Name of the policy.* module", Required: true},
			{Key: "policy", Type: FieldTypeString, Description: "Policy name to evaluate", Required: true},
			{Key: "input", Type: FieldTypeJSON, Description: "Input data for policy evaluation"},
		},
		Outputs: []StepOutputDef{
			{Key: "allowed", Type: "boolean", Description: "Whether the policy allows the action"},
			{Key: "reason", Type: "string", Description: "Decision reason or denial message"},
		},
	})

	// ---- Policy List ----

	r.Register(&StepSchema{
		Type:        "step.policy_list",
		Plugin:      "policy",
		Description: "Lists all loaded policies in a policy engine.",
		ConfigFields: []ConfigFieldDef{
			{Key: "engine", Type: FieldTypeString, Description: "Name of the policy.* module", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "policies", Type: "[]string", Description: "List of loaded policy names"},
			{Key: "count", Type: "number", Description: "Number of loaded policies"},
		},
	})

	// ---- Policy Load ----

	r.Register(&StepSchema{
		Type:        "step.policy_load",
		Plugin:      "policy",
		Description: "Loads a policy into the policy engine at runtime.",
		ConfigFields: []ConfigFieldDef{
			{Key: "engine", Type: FieldTypeString, Description: "Name of the policy.* module", Required: true},
			{Key: "name", Type: FieldTypeString, Description: "Policy name", Required: true},
			{Key: "content", Type: FieldTypeString, Description: "Policy content (Rego, CEL, etc.)", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "loaded", Type: "boolean", Description: "Whether the policy was loaded successfully"},
		},
	})

	// ---- Policy Test ----

	r.Register(&StepSchema{
		Type:        "step.policy_test",
		Plugin:      "policy",
		Description: "Runs tests against a policy to verify its behavior.",
		ConfigFields: []ConfigFieldDef{
			{Key: "engine", Type: FieldTypeString, Description: "Name of the policy.* module", Required: true},
			{Key: "policy", Type: FieldTypeString, Description: "Policy name to test", Required: true},
			{Key: "cases", Type: FieldTypeJSON, Description: "Test cases (array of {input, expected_allow})"},
		},
		Outputs: []StepOutputDef{
			{Key: "passed", Type: "boolean", Description: "Whether all test cases passed"},
			{Key: "results", Type: "[]any", Description: "Per-case test results"},
		},
	})

	// ---- Region Deploy ----

	r.Register(&StepSchema{
		Type:        "step.region_deploy",
		Plugin:      "platform",
		Description: "Deploys to a specific region in a multi-region configuration.",
		ConfigFields: []ConfigFieldDef{
			{Key: "module", Type: FieldTypeString, Description: "Name of the platform.region module", Required: true},
			{Key: "region", Type: FieldTypeString, Description: "Region name to deploy to"},
		},
		Outputs: []StepOutputDef{
			{Key: "deployed", Type: "boolean", Description: "Whether deployment succeeded"},
			{Key: "endpoint", Type: "string", Description: "Region endpoint after deployment"},
		},
	})

	// ---- Region Failover ----

	r.Register(&StepSchema{
		Type:        "step.region_failover",
		Plugin:      "platform",
		Description: "Triggers a failover to a secondary region.",
		ConfigFields: []ConfigFieldDef{
			{Key: "module", Type: FieldTypeString, Description: "Name of the platform.region module", Required: true},
			{Key: "from_region", Type: FieldTypeString, Description: "Region to fail over from"},
			{Key: "to_region", Type: FieldTypeString, Description: "Region to fail over to"},
		},
		Outputs: []StepOutputDef{
			{Key: "active_region", Type: "string", Description: "Active region after failover"},
		},
	})

	// ---- Region Promote ----

	r.Register(&StepSchema{
		Type:        "step.region_promote",
		Plugin:      "platform",
		Description: "Promotes a region to primary (increases traffic weight or priority).",
		ConfigFields: []ConfigFieldDef{
			{Key: "module", Type: FieldTypeString, Description: "Name of the platform.region module", Required: true},
			{Key: "region", Type: FieldTypeString, Description: "Region to promote", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "promoted", Type: "boolean", Description: "Whether promotion succeeded"},
		},
	})

	// ---- Region Status ----

	r.Register(&StepSchema{
		Type:        "step.region_status",
		Plugin:      "platform",
		Description: "Gets the health and status of all regions.",
		ConfigFields: []ConfigFieldDef{
			{Key: "module", Type: FieldTypeString, Description: "Name of the platform.region module", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "regions", Type: "[]any", Description: "Region status objects"},
			{Key: "active_count", Type: "number", Description: "Number of healthy regions"},
		},
	})

	// ---- Region Sync ----

	r.Register(&StepSchema{
		Type:        "step.region_sync",
		Plugin:      "platform",
		Description: "Syncs state or configuration across all regions.",
		ConfigFields: []ConfigFieldDef{
			{Key: "module", Type: FieldTypeString, Description: "Name of the platform.region module", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "synced", Type: "boolean", Description: "Whether sync succeeded"},
			{Key: "regions_synced", Type: "number", Description: "Number of regions synced"},
		},
	})

	// ---- Region Weight ----

	r.Register(&StepSchema{
		Type:        "step.region_weight",
		Plugin:      "platform",
		Description: "Sets the traffic weight for a region.",
		ConfigFields: []ConfigFieldDef{
			{Key: "module", Type: FieldTypeString, Description: "Name of the platform.region module", Required: true},
			{Key: "region", Type: FieldTypeString, Description: "Region name", Required: true},
			{Key: "weight", Type: FieldTypeNumber, Description: "Traffic weight (0-100)", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "weight", Type: "number", Description: "Applied weight"},
		},
	})

	// ---- Scaling Apply ----

	r.Register(&StepSchema{
		Type:        "step.scaling_apply",
		Plugin:      "platform",
		Description: "Applies autoscaling policy changes.",
		ConfigFields: []ConfigFieldDef{
			{Key: "scaling", Type: FieldTypeString, Description: "Name of the platform.autoscaling module", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "applied", Type: "boolean", Description: "Whether scaling policies were applied"},
		},
	})

	// ---- Scaling Destroy ----

	r.Register(&StepSchema{
		Type:        "step.scaling_destroy",
		Plugin:      "platform",
		Description: "Removes autoscaling policies.",
		ConfigFields: []ConfigFieldDef{
			{Key: "scaling", Type: FieldTypeString, Description: "Name of the platform.autoscaling module", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "destroyed", Type: "boolean", Description: "Whether policies were removed"},
		},
	})

	// ---- Scaling Plan ----

	r.Register(&StepSchema{
		Type:        "step.scaling_plan",
		Plugin:      "platform",
		Description: "Plans autoscaling policy changes without applying them.",
		ConfigFields: []ConfigFieldDef{
			{Key: "scaling", Type: FieldTypeString, Description: "Name of the platform.autoscaling module", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "plan", Type: "string", Description: "Human-readable plan output"},
			{Key: "changes", Type: "number", Description: "Number of changes planned"},
		},
	})

	// ---- Scaling Status ----

	r.Register(&StepSchema{
		Type:        "step.scaling_status",
		Plugin:      "platform",
		Description: "Gets the status of autoscaling policies.",
		ConfigFields: []ConfigFieldDef{
			{Key: "scaling", Type: FieldTypeString, Description: "Name of the platform.autoscaling module", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "status", Type: "string", Description: "Scaling status"},
			{Key: "policies", Type: "[]any", Description: "Active scaling policy details"},
		},
	})

	// ---- Secret Rotate ----

	r.Register(&StepSchema{
		Type:        "step.secret_rotate",
		Plugin:      "secrets",
		Description: "Rotates a secret by generating a new value and updating the secrets backend.",
		ConfigFields: []ConfigFieldDef{
			{Key: "provider", Type: FieldTypeString, Description: "Name of the secrets module", Required: true},
			{Key: "secret_id", Type: FieldTypeString, Description: "Secret ID or ARN to rotate", Required: true},
		},
		Outputs: []StepOutputDef{
			{Key: "rotated", Type: "boolean", Description: "Whether the secret was rotated"},
			{Key: "version_id", Type: "string", Description: "New secret version ID"},
		},
	})

	// ---- Trace Annotate ----

	r.Register(&StepSchema{
		Type:        "step.trace_annotate",
		Plugin:      "observability",
		Description: "Adds attributes or events to the current trace span.",
		ConfigFields: []ConfigFieldDef{
			{Key: "attributes", Type: FieldTypeMap, Description: "Key/value attributes to add to the span"},
			{Key: "event", Type: FieldTypeString, Description: "Event name to record on the span"},
		},
		Outputs: []StepOutputDef{
			{Key: "annotated", Type: "boolean", Description: "Whether annotation was applied"},
		},
	})

	// ---- Trace Extract ----

	r.Register(&StepSchema{
		Type:        "step.trace_extract",
		Plugin:      "observability",
		Description: "Extracts trace context from incoming request headers or message attributes.",
		ConfigFields: []ConfigFieldDef{
			{Key: "source", Type: FieldTypeString, Description: "Source to extract from: headers, message, context", DefaultValue: "headers"},
		},
		Outputs: []StepOutputDef{
			{Key: "trace_id", Type: "string", Description: "Extracted trace ID"},
			{Key: "span_id", Type: "string", Description: "Extracted span ID"},
		},
	})

	// ---- Trace Inject ----

	r.Register(&StepSchema{
		Type:        "step.trace_inject",
		Plugin:      "observability",
		Description: "Injects trace context into outgoing request headers or message attributes.",
		ConfigFields: []ConfigFieldDef{
			{Key: "target", Type: FieldTypeString, Description: "Target to inject into: headers, message", DefaultValue: "headers"},
		},
		Outputs: []StepOutputDef{
			{Key: "injected", Type: "boolean", Description: "Whether context was injected"},
		},
	})

	// ---- Trace Link ----

	r.Register(&StepSchema{
		Type:        "step.trace_link",
		Plugin:      "observability",
		Description: "Creates a causal link between the current span and another span.",
		ConfigFields: []ConfigFieldDef{
			{Key: "trace_id", Type: FieldTypeString, Description: "Trace ID to link to", Required: true},
			{Key: "span_id", Type: FieldTypeString, Description: "Span ID to link to"},
		},
		Outputs: []StepOutputDef{
			{Key: "linked", Type: "boolean", Description: "Whether the link was created"},
		},
	})

	// ---- Trace Start ----

	r.Register(&StepSchema{
		Type:        "step.trace_start",
		Plugin:      "observability",
		Description: "Starts a new trace span for the current pipeline step or operation.",
		ConfigFields: []ConfigFieldDef{
			{Key: "name", Type: FieldTypeString, Description: "Span name", Required: true},
			{Key: "attributes", Type: FieldTypeMap, Description: "Initial span attributes"},
		},
		Outputs: []StepOutputDef{
			{Key: "trace_id", Type: "string", Description: "New trace ID"},
			{Key: "span_id", Type: "string", Description: "New span ID"},
		},
	})
}
