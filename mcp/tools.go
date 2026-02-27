package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/schema"
	"github.com/mark3labs/mcp-go/mcp"
	"gopkg.in/yaml.v3"
)

// registerNewTools registers the additional schema and validation tools.
func (s *Server) registerNewTools() {
	// get_module_schema
	s.mcpServer.AddTool(
		mcp.NewTool("get_module_schema",
			mcp.WithDescription("Return the full configuration schema for a given module type. "+
				"Includes description, config fields (key, type, description, required, default, options), "+
				"inputs, outputs, and an example usage snippet."),
			mcp.WithString("module_type",
				mcp.Required(),
				mcp.Description("The module type string (e.g. 'http.server', 'messaging.broker')"),
			),
			mcp.WithReadOnlyHintAnnotation(true),
		),
		s.handleGetModuleSchema,
	)

	// get_step_schema
	s.mcpServer.AddTool(
		mcp.NewTool("get_step_schema",
			mcp.WithDescription("Return the schema for a given pipeline step type. "+
				"Includes description, config keys with types and descriptions, and an example usage snippet."),
			mcp.WithString("step_type",
				mcp.Required(),
				mcp.Description("The step type string (e.g. 'step.set', 'step.http_call')"),
			),
			mcp.WithReadOnlyHintAnnotation(true),
		),
		s.handleGetStepSchema,
	)

	// get_template_functions
	s.mcpServer.AddTool(
		mcp.NewTool("get_template_functions",
			mcp.WithDescription("Return the complete list of available Go template functions for pipeline templates. "+
				"Each function includes its name, signature, description, and an example usage."),
			mcp.WithReadOnlyHintAnnotation(true),
		),
		s.handleGetTemplateFunctions,
	)

	// validate_template_expressions
	s.mcpServer.AddTool(
		mcp.NewTool("validate_template_expressions",
			mcp.WithDescription("Validate template expressions ({{ ... }}) in a YAML pipeline config string. "+
				"Checks for forward references, self-references, undefined step references, "+
				"and hyphenated step name dot-access patterns that require index syntax."),
			mcp.WithString("yaml_content",
				mcp.Required(),
				mcp.Description("The YAML content of the pipeline configuration to validate"),
			),
		),
		s.handleValidateTemplateExpressions,
	)

	// get_config_examples
	s.mcpServer.AddTool(
		mcp.NewTool("get_config_examples",
			mcp.WithDescription("List and optionally return example workflow config files from the example/ directory. "+
				"When called without a name, lists all available examples. "+
				"When called with a name, returns the full content of that example."),
			mcp.WithString("name",
				mcp.Description("Name of a specific example to fetch (e.g. 'api-server-config'). Omit to list all examples."),
			),
			mcp.WithReadOnlyHintAnnotation(true),
		),
		s.handleGetConfigExamples,
	)
}

// --- Tool Handlers for new tools ---

func (s *Server) handleGetModuleSchema(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	moduleType := mcp.ParseString(req, "module_type", "")
	if moduleType == "" {
		return mcp.NewToolResultError("module_type is required"), nil
	}

	reg := schema.GetModuleSchemaRegistry()
	ms := reg.Get(moduleType)
	if ms == nil {
		return mcp.NewToolResultError(fmt.Sprintf("unknown module type %q", moduleType)), nil
	}

	example := generateModuleExample(ms)

	result := map[string]any{
		"type":        ms.Type,
		"label":       ms.Label,
		"category":    ms.Category,
		"description": ms.Description,
		"inputs":      ms.Inputs,
		"outputs":     ms.Outputs,
		"configFields": ms.ConfigFields,
		"defaultConfig": ms.DefaultConfig,
		"example":     example,
	}
	return marshalToolResult(result)
}

func (s *Server) handleGetStepSchema(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	stepType := mcp.ParseString(req, "step_type", "")
	if stepType == "" {
		return mcp.NewToolResultError("step_type is required"), nil
	}
	if !strings.HasPrefix(stepType, "step.") {
		return mcp.NewToolResultError(fmt.Sprintf("step type must begin with 'step.', got %q", stepType)), nil
	}

	info, ok := knownStepTypeDescriptions()[stepType]
	if !ok {
		// Fall back to checking if the type is in the known module types list.
		known := schema.KnownModuleTypes()
		found := false
		for _, t := range known {
			if t == stepType {
				found = true
				break
			}
		}
		if !found {
			return mcp.NewToolResultError(fmt.Sprintf("unknown step type %q", stepType)), nil
		}
		// Return minimal info for step types not in the description table.
		result := map[string]any{
			"type":       stepType,
			"configKeys": []string{},
			"example":    generateStepExample(stepType, []string{}),
		}
		return marshalToolResult(result)
	}

	result := map[string]any{
		"type":        info.Type,
		"description": info.Description,
		"plugin":      info.Plugin,
		"configKeys":  info.ConfigKeys,
		"configDefs":  info.ConfigDefs,
		"example":     generateStepExample(info.Type, info.ConfigKeys),
	}
	return marshalToolResult(result)
}

func (s *Server) handleGetTemplateFunctions(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	funcs := templateFunctionDescriptions()
	return marshalToolResult(map[string]any{
		"functions": funcs,
		"count":     len(funcs),
	})
}

func (s *Server) handleValidateTemplateExpressions(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	yamlContent := mcp.ParseString(req, "yaml_content", "")
	if yamlContent == "" {
		return mcp.NewToolResultError("yaml_content is required"), nil
	}

	// Parse config to get basic structure.
	_, err := config.LoadFromString(yamlContent)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("YAML parse error: %v", err)), nil
	}

	// Re-parse into a generic map to access the pipelines section (which is map[string]any in config).
	var rawDoc map[string]any
	if err := yaml.Unmarshal([]byte(yamlContent), &rawDoc); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("YAML parse error: %v", err)), nil
	}

	// Extract pipelines section.
	pipelinesRaw, _ := rawDoc["pipelines"].(map[string]any)

	// Re-marshal pipelines back to YAML and parse as typed PipelineConfig.
	type minStep struct {
		Name   string         `yaml:"name"`
		Type   string         `yaml:"type"`
		Config map[string]any `yaml:"config"`
	}
	type minPipeline struct {
		Steps []minStep `yaml:"steps"`
	}

	pipelines := make(map[string]minPipeline, len(pipelinesRaw))
	for pName, pRaw := range pipelinesRaw {
		data, err := yaml.Marshal(pRaw)
		if err != nil {
			continue
		}
		var p minPipeline
		if err := yaml.Unmarshal(data, &p); err != nil {
			continue
		}
		pipelines[pName] = p
	}

	var warnings []string

	// Regex patterns for template expression analysis.
	templateRefRe := regexp.MustCompile(`\{\{[^}]*\}\}`)
	stepsRefRe := regexp.MustCompile(`\.steps\.([a-zA-Z0-9_-]+)`)
	hyphenStepRe := regexp.MustCompile(`\.steps\.([a-zA-Z0-9_]*-[a-zA-Z0-9_-]*)`)

	// Walk every pipeline step config looking for template patterns.
	for pName, p := range pipelines {
		// Build ordered step index map.
		stepIndexMap := make(map[string]int, len(p.Steps))
		for i, step := range p.Steps {
			stepIndexMap[step.Name] = i
		}

		for stepIdx, step := range p.Steps {
			// Check all config values for template expressions.
			for configKey, configVal := range step.Config {
				valStr := fmt.Sprintf("%v", configVal)
				if !strings.Contains(valStr, "{{") {
					continue
				}
				exprs := templateRefRe.FindAllString(valStr, -1)
				for _, expr := range exprs {
					// Self-reference check.
					if step.Name != "" {
						selfRefRe := regexp.MustCompile(`\.steps\.` + regexp.QuoteMeta(step.Name) + `\b`)
						if selfRefRe.MatchString(expr) {
							warnings = append(warnings, fmt.Sprintf(
								"[pipeline=%s step=%s config=%s] self-reference: step %q references itself in %s",
								pName, step.Name, configKey, step.Name, expr,
							))
						}
					}

					refs := stepsRefRe.FindAllStringSubmatch(expr, -1)
					for _, ref := range refs {
						refName := ref[1]
						// Forward reference check.
						if refIdx, exists := stepIndexMap[refName]; exists && refIdx > stepIdx {
							warnings = append(warnings, fmt.Sprintf(
								"[pipeline=%s step=%s config=%s] forward reference: step %q (index %d) references step %q (index %d) which has not yet run",
								pName, step.Name, configKey, step.Name, stepIdx, refName, refIdx,
							))
						}
						// Undefined step reference check.
						if _, exists := stepIndexMap[refName]; !exists {
							warnings = append(warnings, fmt.Sprintf(
								"[pipeline=%s step=%s config=%s] undefined step reference: %q not found in pipeline",
								pName, step.Name, configKey, refName,
							))
						}
					}

					// Hyphenated dot-access check.
					hyphenRefs := hyphenStepRe.FindAllStringSubmatch(expr, -1)
					for _, ref := range hyphenRefs {
						stepName := ref[1]
						warnings = append(warnings, fmt.Sprintf(
							"[pipeline=%s step=%s config=%s] hyphenated step name %q uses dot-access; use: {{ index .steps %q \"field\" }} instead",
							pName, step.Name, configKey, stepName, stepName,
						))
					}
				}
			}
		}
	}

	sort.Strings(warnings)

	result := map[string]any{
		"warnings":          warnings,
		"warning_count":     len(warnings),
		"pipelines_checked": len(pipelines),
	}
	return marshalToolResult(result)
}

func (s *Server) handleGetConfigExamples(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name := mcp.ParseString(req, "name", "")

	exampleDir := "example"
	// Support absolute path from the server's working directory.
	if s.pluginDir != "" {
		// Try to derive the root from pluginDir (data/plugins -> .)
		candidate := filepath.Join(filepath.Dir(filepath.Dir(s.pluginDir)), "example")
		if _, err := os.Stat(candidate); err == nil {
			exampleDir = candidate
		}
	}

	if name != "" {
		// Return the content of the named example.
		content, filename, err := readExampleFile(exampleDir, name)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("example %q not found: %v", name, err)), nil
		}
		result := map[string]any{
			"name":     name,
			"filename": filename,
			"content":  content,
		}
		return marshalToolResult(result)
	}

	// List all .yaml files in the example directory.
	examples, err := listExamples(exampleDir)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to list examples: %v", err)), nil
	}

	result := map[string]any{
		"examples": examples,
		"count":    len(examples),
	}
	return marshalToolResult(result)
}

// --- Helper types and data ---

// stepTypeInfoFull extends StepTypeInfo with per-key descriptions.
type stepTypeInfoFull struct {
	Type        string
	Plugin      string
	Description string
	ConfigKeys  []string
	ConfigDefs  []stepConfigKeyDef
}

// stepConfigKeyDef describes a single config key for a step type.
type stepConfigKeyDef struct {
	Key         string `json:"key"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Required    bool   `json:"required,omitempty"`
}

// knownStepTypeDescriptions returns a map of all known step types with descriptions.
func knownStepTypeDescriptions() map[string]stepTypeInfoFull {
	return map[string]stepTypeInfoFull{
		"step.set": {
			Type:        "step.set",
			Plugin:      "pipelinesteps",
			Description: "Sets key/value pairs in the pipeline context. Values can contain template expressions.",
			ConfigKeys:  []string{"values"},
			ConfigDefs: []stepConfigKeyDef{
				{Key: "values", Type: "map", Description: "Map of key/value pairs to merge into the pipeline context", Required: true},
			},
		},
		"step.log": {
			Type:        "step.log",
			Plugin:      "pipelinesteps",
			Description: "Logs a message at the specified log level.",
			ConfigKeys:  []string{"message", "level"},
			ConfigDefs: []stepConfigKeyDef{
				{Key: "message", Type: "string", Description: "Message to log (template expressions supported)", Required: true},
				{Key: "level", Type: "string", Description: "Log level: debug, info, warn, error (default: info)"},
			},
		},
		"step.validate": {
			Type:        "step.validate",
			Plugin:      "pipelinesteps",
			Description: "Validates pipeline context fields against rules.",
			ConfigKeys:  []string{"rules", "required", "schema"},
			ConfigDefs: []stepConfigKeyDef{
				{Key: "rules", Type: "map", Description: "Validation rules per field"},
				{Key: "required", Type: "array", Description: "List of required field names"},
				{Key: "schema", Type: "string", Description: "JSON Schema for request body validation"},
			},
		},
		"step.transform": {
			Type:        "step.transform",
			Plugin:      "pipelinesteps",
			Description: "Transforms pipeline context values using field mapping or template expressions.",
			ConfigKeys:  []string{"mapping", "template"},
			ConfigDefs: []stepConfigKeyDef{
				{Key: "mapping", Type: "map", Description: "Field mapping from source to target keys"},
				{Key: "template", Type: "string", Description: "Go template string for complex transformations"},
			},
		},
		"step.conditional": {
			Type:        "step.conditional",
			Plugin:      "pipelinesteps",
			Description: "Branches pipeline execution based on a condition expression.",
			ConfigKeys:  []string{"condition", "then", "else"},
			ConfigDefs: []stepConfigKeyDef{
				{Key: "condition", Type: "string", Description: "Boolean template expression to evaluate", Required: true},
				{Key: "then", Type: "array", Description: "Steps to execute when condition is true"},
				{Key: "else", Type: "array", Description: "Steps to execute when condition is false"},
			},
		},
		"step.http_call": {
			Type:        "step.http_call",
			Plugin:      "pipelinesteps",
			Description: "Makes an outbound HTTP request and stores the response in the pipeline context.",
			ConfigKeys:  []string{"url", "method", "headers", "body", "timeout", "auth"},
			ConfigDefs: []stepConfigKeyDef{
				{Key: "url", Type: "string", Description: "Request URL (template expressions supported)", Required: true},
				{Key: "method", Type: "string", Description: "HTTP method (GET, POST, PUT, DELETE, PATCH)", Required: true},
				{Key: "headers", Type: "map", Description: "Request headers"},
				{Key: "body", Type: "string", Description: "Request body (template expressions supported)"},
				{Key: "timeout", Type: "string", Description: "Request timeout duration (e.g. 30s)"},
				{Key: "auth", Type: "map", Description: "Authentication config (type, token, etc.)"},
			},
		},
		"step.json_response": {
			Type:        "step.json_response",
			Plugin:      "pipelinesteps",
			Description: "Sends a JSON HTTP response and terminates pipeline execution.",
			ConfigKeys:  []string{"status", "body", "headers"},
			ConfigDefs: []stepConfigKeyDef{
				{Key: "status", Type: "number", Description: "HTTP status code (default: 200)", Required: true},
				{Key: "body", Type: "string|map", Description: "Response body (string template or map for JSON object)"},
				{Key: "headers", Type: "map", Description: "Additional response headers"},
			},
		},
		"step.request_parse": {
			Type:        "step.request_parse",
			Plugin:      "pipelinesteps",
			Description: "Parses incoming HTTP request body, query params, and headers into the pipeline context.",
			ConfigKeys:  []string{"body", "query", "headers"},
			ConfigDefs: []stepConfigKeyDef{
				{Key: "body", Type: "boolean", Description: "Parse the request body (default: true)"},
				{Key: "query", Type: "boolean", Description: "Parse query parameters (default: true)"},
				{Key: "headers", Type: "boolean", Description: "Parse request headers (default: false)"},
			},
		},
		"step.db_query": {
			Type:        "step.db_query",
			Plugin:      "pipelinesteps",
			Description: "Executes a database SELECT query and stores results in the pipeline context.",
			ConfigKeys:  []string{"database", "query", "params"},
			ConfigDefs: []stepConfigKeyDef{
				{Key: "database", Type: "string", Description: "Database module name", Required: true},
				{Key: "query", Type: "string", Description: "SQL query (template expressions supported)", Required: true},
				{Key: "params", Type: "array", Description: "Query parameters"},
			},
		},
		"step.db_exec": {
			Type:        "step.db_exec",
			Plugin:      "pipelinesteps",
			Description: "Executes a database INSERT/UPDATE/DELETE statement.",
			ConfigKeys:  []string{"database", "query", "params"},
			ConfigDefs: []stepConfigKeyDef{
				{Key: "database", Type: "string", Description: "Database module name", Required: true},
				{Key: "query", Type: "string", Description: "SQL statement (template expressions supported)", Required: true},
				{Key: "params", Type: "array", Description: "Statement parameters"},
			},
		},
		"step.foreach": {
			Type:        "step.foreach",
			Plugin:      "pipelinesteps",
			Description: "Iterates over a collection and executes nested steps for each item.",
			ConfigKeys:  []string{"collection", "item_var", "item_key", "step", "steps", "index_key"},
			ConfigDefs: []stepConfigKeyDef{
				{Key: "collection", Type: "string", Description: "Template expression resolving to the collection to iterate", Required: true},
				{Key: "item_var", Type: "string", Description: "Context key for the current item (default: 'item')"},
				{Key: "item_key", Type: "string", Description: "Context key for the current item's key/index"},
				{Key: "index_key", Type: "string", Description: "Context key for the numeric loop index"},
				{Key: "step", Type: "object", Description: "Single step to execute per item"},
				{Key: "steps", Type: "array", Description: "List of steps to execute per item"},
			},
		},
		"step.delegate": {
			Type:        "step.delegate",
			Plugin:      "pipelinesteps",
			Description: "Delegates execution to another module service.",
			ConfigKeys:  []string{"service", "action"},
			ConfigDefs: []stepConfigKeyDef{
				{Key: "service", Type: "string", Description: "Name of the service module to delegate to", Required: true},
				{Key: "action", Type: "string", Description: "Action to invoke on the service"},
			},
		},
		"step.publish": {
			Type:        "step.publish",
			Plugin:      "pipelinesteps",
			Description: "Publishes a message to a messaging broker topic.",
			ConfigKeys:  []string{"topic", "broker", "payload"},
			ConfigDefs: []stepConfigKeyDef{
				{Key: "topic", Type: "string", Description: "Topic name to publish to", Required: true},
				{Key: "broker", Type: "string", Description: "Messaging broker module name"},
				{Key: "payload", Type: "string|map", Description: "Message payload (template expressions supported)"},
			},
		},
		"step.event_publish": {
			Type:        "step.event_publish",
			Plugin:      "pipelinesteps",
			Description: "Publishes a structured event to a messaging broker topic.",
			ConfigKeys:  []string{"topic", "broker", "payload", "headers", "event_type"},
			ConfigDefs: []stepConfigKeyDef{
				{Key: "topic", Type: "string", Description: "Topic name to publish to", Required: true},
				{Key: "broker", Type: "string", Description: "Messaging broker module name"},
				{Key: "payload", Type: "string|map", Description: "Event payload (template expressions supported)"},
				{Key: "headers", Type: "map", Description: "Event headers"},
				{Key: "event_type", Type: "string", Description: "Event type identifier"},
			},
		},
		"step.cache_get": {
			Type:        "step.cache_get",
			Plugin:      "pipelinesteps",
			Description: "Retrieves a value from a cache module by key.",
			ConfigKeys:  []string{"cache", "key", "output"},
			ConfigDefs: []stepConfigKeyDef{
				{Key: "cache", Type: "string", Description: "Cache module name", Required: true},
				{Key: "key", Type: "string", Description: "Cache key (template expressions supported)", Required: true},
				{Key: "output", Type: "string", Description: "Context key to store the result (default: step name)"},
			},
		},
		"step.cache_set": {
			Type:        "step.cache_set",
			Plugin:      "pipelinesteps",
			Description: "Stores a value in a cache module by key.",
			ConfigKeys:  []string{"cache", "key", "value", "ttl"},
			ConfigDefs: []stepConfigKeyDef{
				{Key: "cache", Type: "string", Description: "Cache module name", Required: true},
				{Key: "key", Type: "string", Description: "Cache key (template expressions supported)", Required: true},
				{Key: "value", Type: "string|any", Description: "Value to cache (template expressions supported)"},
				{Key: "ttl", Type: "string", Description: "Time-to-live duration (e.g. 5m, 1h)"},
			},
		},
		"step.cache_delete": {
			Type:        "step.cache_delete",
			Plugin:      "pipelinesteps",
			Description: "Deletes a value from a cache module by key.",
			ConfigKeys:  []string{"cache", "key"},
			ConfigDefs: []stepConfigKeyDef{
				{Key: "cache", Type: "string", Description: "Cache module name", Required: true},
				{Key: "key", Type: "string", Description: "Cache key to delete", Required: true},
			},
		},
		"step.retry_with_backoff": {
			Type:        "step.retry_with_backoff",
			Plugin:      "pipelinesteps",
			Description: "Retries a nested step with exponential backoff on failure.",
			ConfigKeys:  []string{"max_retries", "initial_delay", "max_delay", "multiplier", "step"},
			ConfigDefs: []stepConfigKeyDef{
				{Key: "max_retries", Type: "number", Description: "Maximum retry attempts", Required: true},
				{Key: "initial_delay", Type: "string", Description: "Initial delay before first retry (e.g. 100ms)"},
				{Key: "max_delay", Type: "string", Description: "Maximum delay between retries (e.g. 30s)"},
				{Key: "multiplier", Type: "number", Description: "Backoff multiplier (default: 2.0)"},
				{Key: "step", Type: "object", Description: "The step definition to retry", Required: true},
			},
		},
		"step.resilient_circuit_breaker": {
			Type:        "step.resilient_circuit_breaker",
			Plugin:      "pipelinesteps",
			Description: "Wraps a step with a circuit breaker to prevent cascading failures.",
			ConfigKeys:  []string{"failure_threshold", "reset_timeout", "step", "fallback"},
			ConfigDefs: []stepConfigKeyDef{
				{Key: "failure_threshold", Type: "number", Description: "Number of failures before opening the circuit", Required: true},
				{Key: "reset_timeout", Type: "string", Description: "Duration to wait before trying half-open (e.g. 30s)", Required: true},
				{Key: "step", Type: "object", Description: "The step definition to protect", Required: true},
				{Key: "fallback", Type: "object", Description: "Optional fallback step when circuit is open"},
			},
		},
		"step.auth_required": {
			Type:        "step.auth_required",
			Plugin:      "pipelinesteps",
			Description: "Validates JWT or API key authentication. Returns 401 if not authenticated.",
			ConfigKeys:  []string{"roles", "scopes"},
			ConfigDefs: []stepConfigKeyDef{
				{Key: "roles", Type: "array", Description: "Required roles (any match grants access)"},
				{Key: "scopes", Type: "array", Description: "Required OAuth2 scopes"},
			},
		},
		"step.jq": {
			Type:        "step.jq",
			Plugin:      "pipelinesteps",
			Description: "Applies a jq expression to transform data in the pipeline context.",
			ConfigKeys:  []string{"expression", "input", "output"},
			ConfigDefs: []stepConfigKeyDef{
				{Key: "expression", Type: "string", Description: "jq filter expression", Required: true},
				{Key: "input", Type: "string", Description: "Context key of the input value (default: whole context)"},
				{Key: "output", Type: "string", Description: "Context key to store the result"},
			},
		},
		"step.webhook_verify": {
			Type:        "step.webhook_verify",
			Plugin:      "pipelinesteps",
			Description: "Verifies webhook signatures from providers like GitHub, GitLab, or Stripe.",
			ConfigKeys:  []string{"provider", "scheme", "secret", "secret_from", "header", "signature_header"},
			ConfigDefs: []stepConfigKeyDef{
				{Key: "provider", Type: "string", Description: "Webhook provider (github, gitlab, stripe, generic)"},
				{Key: "scheme", Type: "string", Description: "Signature scheme (hmac-sha256, hmac-sha1)"},
				{Key: "secret", Type: "string", Description: "Shared secret for signature verification"},
				{Key: "secret_from", Type: "string", Description: "Context key containing the secret (alternative to secret)"},
				{Key: "signature_header", Type: "string", Description: "HTTP header containing the signature"},
				{Key: "header", Type: "string", Description: "Alias for signature_header"},
			},
		},
		"step.workflow_call": {
			Type:        "step.workflow_call",
			Plugin:      "pipelinesteps",
			Description: "Calls another workflow and returns its result.",
			ConfigKeys:  []string{"workflow", "input"},
			ConfigDefs: []stepConfigKeyDef{
				{Key: "workflow", Type: "string", Description: "Workflow name to call", Required: true},
				{Key: "input", Type: "map", Description: "Input data to pass to the workflow"},
			},
		},
		"step.validate_path_param": {
			Type:        "step.validate_path_param",
			Plugin:      "pipelinesteps",
			Description: "Validates a URL path parameter exists and matches the expected type.",
			ConfigKeys:  []string{"param", "type", "required"},
			ConfigDefs: []stepConfigKeyDef{
				{Key: "param", Type: "string", Description: "Path parameter name", Required: true},
				{Key: "type", Type: "string", Description: "Expected type (string, integer, uuid)"},
				{Key: "required", Type: "boolean", Description: "Whether the parameter is required (default: true)"},
			},
		},
		"step.validate_pagination": {
			Type:        "step.validate_pagination",
			Plugin:      "pipelinesteps",
			Description: "Validates and normalizes pagination query parameters (limit, offset).",
			ConfigKeys:  []string{"maxLimit", "defaultLimit"},
			ConfigDefs: []stepConfigKeyDef{
				{Key: "maxLimit", Type: "number", Description: "Maximum allowed limit value (default: 100)"},
				{Key: "defaultLimit", Type: "number", Description: "Default limit when not provided (default: 20)"},
			},
		},
		"step.validate_request_body": {
			Type:        "step.validate_request_body",
			Plugin:      "pipelinesteps",
			Description: "Validates the request body against a JSON Schema.",
			ConfigKeys:  []string{"schema", "required"},
			ConfigDefs: []stepConfigKeyDef{
				{Key: "schema", Type: "string|object", Description: "JSON Schema to validate against", Required: true},
				{Key: "required", Type: "array", Description: "List of required body fields"},
			},
		},
		"step.dlq_send": {
			Type:        "step.dlq_send",
			Plugin:      "pipelinesteps",
			Description: "Sends a failed message to the dead-letter queue.",
			ConfigKeys:  []string{"topic", "original_topic", "error", "payload", "broker"},
			ConfigDefs: []stepConfigKeyDef{
				{Key: "topic", Type: "string", Description: "DLQ topic name", Required: true},
				{Key: "original_topic", Type: "string", Description: "Original topic the message came from"},
				{Key: "error", Type: "string", Description: "Error message describing the failure"},
				{Key: "payload", Type: "string|map", Description: "Original message payload"},
				{Key: "broker", Type: "string", Description: "Messaging broker module name"},
			},
		},
		"step.dlq_replay": {
			Type:        "step.dlq_replay",
			Plugin:      "pipelinesteps",
			Description: "Replays messages from the dead-letter queue to the target topic.",
			ConfigKeys:  []string{"dlq_topic", "target_topic", "max_messages", "broker"},
			ConfigDefs: []stepConfigKeyDef{
				{Key: "dlq_topic", Type: "string", Description: "DLQ topic to read from", Required: true},
				{Key: "target_topic", Type: "string", Description: "Target topic to replay messages to", Required: true},
				{Key: "max_messages", Type: "number", Description: "Maximum messages to replay (default: all)"},
				{Key: "broker", Type: "string", Description: "Messaging broker module name"},
			},
		},
		"step.nosql_get": {
			Type:        "step.nosql_get",
			Plugin:      "datastores",
			Description: "Retrieves a document from a NoSQL store by key.",
			ConfigKeys:  []string{"store", "key", "output", "miss_ok"},
			ConfigDefs: []stepConfigKeyDef{
				{Key: "store", Type: "string", Description: "NoSQL store module name", Required: true},
				{Key: "key", Type: "string", Description: "Document key (template expressions supported)", Required: true},
				{Key: "output", Type: "string", Description: "Context key to store the result"},
				{Key: "miss_ok", Type: "boolean", Description: "Don't fail if key not found (default: false)"},
			},
		},
		"step.nosql_put": {
			Type:        "step.nosql_put",
			Plugin:      "datastores",
			Description: "Stores a document in a NoSQL store by key.",
			ConfigKeys:  []string{"store", "key", "item"},
			ConfigDefs: []stepConfigKeyDef{
				{Key: "store", Type: "string", Description: "NoSQL store module name", Required: true},
				{Key: "key", Type: "string", Description: "Document key (template expressions supported)", Required: true},
				{Key: "item", Type: "string|map", Description: "Document to store"},
			},
		},
		"step.nosql_query": {
			Type:        "step.nosql_query",
			Plugin:      "datastores",
			Description: "Queries documents from a NoSQL store by key prefix.",
			ConfigKeys:  []string{"store", "prefix", "output"},
			ConfigDefs: []stepConfigKeyDef{
				{Key: "store", Type: "string", Description: "NoSQL store module name", Required: true},
				{Key: "prefix", Type: "string", Description: "Key prefix to filter documents"},
				{Key: "output", Type: "string", Description: "Context key to store results"},
			},
		},
		"step.base64_decode": {
			Type:        "step.base64_decode",
			Plugin:      "pipelinesteps",
			Description: "Decodes a base64-encoded value and validates its type.",
			ConfigKeys:  []string{"input_from", "format", "allowed_types", "max_size_bytes"},
			ConfigDefs: []stepConfigKeyDef{
				{Key: "input_from", Type: "string", Description: "Context key containing the base64-encoded value"},
				{Key: "format", Type: "string", Description: "Expected format (e.g. image/png, application/pdf)"},
				{Key: "allowed_types", Type: "array", Description: "List of allowed MIME types"},
				{Key: "max_size_bytes", Type: "number", Description: "Maximum decoded size in bytes"},
			},
		},
		"step.statemachine_transition": {
			Type:        "step.statemachine_transition",
			Plugin:      "statemachine",
			Description: "Triggers a state machine transition for a given instance.",
			ConfigKeys:  []string{"engine", "instanceId", "transition"},
			ConfigDefs: []stepConfigKeyDef{
				{Key: "engine", Type: "string", Description: "State machine engine module name", Required: true},
				{Key: "instanceId", Type: "string", Description: "State machine instance ID (template expressions supported)", Required: true},
				{Key: "transition", Type: "string", Description: "Transition name to trigger", Required: true},
			},
		},
		"step.statemachine_get": {
			Type:        "step.statemachine_get",
			Plugin:      "statemachine",
			Description: "Retrieves the current state of a state machine instance.",
			ConfigKeys:  []string{"engine", "instanceId"},
			ConfigDefs: []stepConfigKeyDef{
				{Key: "engine", Type: "string", Description: "State machine engine module name", Required: true},
				{Key: "instanceId", Type: "string", Description: "State machine instance ID (template expressions supported)", Required: true},
			},
		},
		"step.feature_flag": {
			Type:        "step.feature_flag",
			Plugin:      "featureflags",
			Description: "Evaluates a feature flag and stores the result in the pipeline context.",
			ConfigKeys:  []string{"flag", "default", "output"},
			ConfigDefs: []stepConfigKeyDef{
				{Key: "flag", Type: "string", Description: "Feature flag name", Required: true},
				{Key: "default", Type: "boolean", Description: "Default value when flag not found (default: false)"},
				{Key: "output", Type: "string", Description: "Context key to store the flag value"},
			},
		},
		"step.shell_exec": {
			Type:        "step.shell_exec",
			Plugin:      "cicd",
			Description: "Executes a shell command and captures its output.",
			ConfigKeys:  []string{"command", "args", "env", "workdir", "timeout"},
			ConfigDefs: []stepConfigKeyDef{
				{Key: "command", Type: "string", Description: "Command to execute", Required: true},
				{Key: "args", Type: "array", Description: "Command arguments"},
				{Key: "env", Type: "map", Description: "Environment variables"},
				{Key: "workdir", Type: "string", Description: "Working directory"},
				{Key: "timeout", Type: "string", Description: "Execution timeout (e.g. 5m)"},
			},
		},
	}
}

// TemplateFunctionDef describes a template function available in pipeline templates.
type TemplateFunctionDef struct {
	Name        string `json:"name"`
	Signature   string `json:"signature"`
	Description string `json:"description"`
	Example     string `json:"example"`
}

// templateFunctionDescriptions returns descriptions for all built-in template functions.
func templateFunctionDescriptions() []TemplateFunctionDef {
	return []TemplateFunctionDef{
		{
			Name:        "uuid",
			Signature:   "uuid() string",
			Description: "Generates a new random UUID v4 string.",
			Example:     `{{ uuid }}`,
		},
		{
			Name:        "uuidv4",
			Signature:   "uuidv4() string",
			Description: "Generates a new random UUID v4 string. Alias for uuid.",
			Example:     `{{ uuidv4 }}`,
		},
		{
			Name:        "now",
			Signature:   "now(layout ...string) string",
			Description: "Returns the current UTC time formatted with the given Go time layout or named constant (e.g. RFC3339, DateOnly). Defaults to RFC3339 when called with no arguments.",
			Example:     `{{ now "RFC3339" }} or {{ now "2006-01-02" }}`,
		},
		{
			Name:        "lower",
			Signature:   "lower(s string) string",
			Description: "Converts a string to lowercase.",
			Example:     `{{ lower .name }}`,
		},
		{
			Name:        "default",
			Signature:   "default(fallback any, val any) any",
			Description: "Returns fallback when val is nil or an empty string, otherwise returns val.",
			Example:     `{{ default "anonymous" .username }}`,
		},
		{
			Name:        "trimPrefix",
			Signature:   "trimPrefix(prefix string, s string) string",
			Description: "Removes the given prefix from s if present.",
			Example:     `{{ trimPrefix "/api" .path }}`,
		},
		{
			Name:        "trimSuffix",
			Signature:   "trimSuffix(suffix string, s string) string",
			Description: "Removes the given suffix from s if present.",
			Example:     `{{ trimSuffix "/" .path }}`,
		},
		{
			Name:        "json",
			Signature:   "json(v any) string",
			Description: "Marshals a value to a JSON string. Returns '{}' on marshal error.",
			Example:     `{{ json .data }}`,
		},
		{
			Name:        "step",
			Signature:   "step(name string, keys ...string) any",
			Description: "Accesses step outputs by step name and optional nested keys. Returns nil if the step does not exist or a key is missing.",
			Example:     `{{ step "parse-request" "body" "id" }}`,
		},
		{
			Name:        "trigger",
			Signature:   "trigger(keys ...string) any",
			Description: "Accesses trigger data by nested keys. Returns nil if keys do not exist.",
			Example:     `{{ trigger "path_params" "id" }}`,
		},
	}
}

// exampleInfo describes an available config example.
type exampleInfo struct {
	Name        string `json:"name"`
	Filename    string `json:"filename"`
	Description string `json:"description,omitempty"`
}

// listExamples lists all YAML example files in the given directory.
func listExamples(exampleDir string) ([]exampleInfo, error) {
	entries, err := os.ReadDir(exampleDir)
	if os.IsNotExist(err) {
		return []exampleInfo{}, nil
	}
	if err != nil {
		return nil, err
	}

	var examples []exampleInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".yaml")
		// Strip leading numbers (e.g. "01-foo" -> "01-foo" kept as-is).
		examples = append(examples, exampleInfo{
			Name:     name,
			Filename: e.Name(),
		})
	}
	return examples, nil
}

// readExampleFile reads an example YAML file by name.
// name can be the base name with or without the .yaml extension.
func readExampleFile(exampleDir, name string) (string, string, error) {
	// Normalize name.
	if !strings.HasSuffix(name, ".yaml") {
		name += ".yaml"
	}

	candidates := []string{
		filepath.Join(exampleDir, name),
	}

	for _, path := range candidates {
		data, err := os.ReadFile(path) //nolint:gosec // G304: path is within example dir
		if err == nil {
			return string(data), filepath.Base(path), nil
		}
	}
	return "", "", fmt.Errorf("file not found")
}

// generateModuleExample creates an example YAML snippet for a module schema.
func generateModuleExample(ms *schema.ModuleSchema) string {
	var b strings.Builder
	name := strings.ReplaceAll(ms.Type, ".", "-")
	fmt.Fprintf(&b, "modules:\n  - name: my-%s\n    type: %s\n", name, ms.Type)
	if len(ms.ConfigFields) > 0 {
		b.WriteString("    config:\n")
		for i := range ms.ConfigFields {
			f := &ms.ConfigFields[i]
			val := f.DefaultValue
			if val == nil {
				val = exampleValue(*f)
			}
			fmt.Fprintf(&b, "      %s: %v\n", f.Key, val)
		}
	}
	return b.String()
}

// exampleValue returns a placeholder value for a config field.
func exampleValue(f schema.ConfigFieldDef) any {
	switch f.Type {
	case schema.FieldTypeString, schema.FieldTypeFilePath, schema.FieldTypeSQL:
		if f.Placeholder != "" {
			return f.Placeholder
		}
		return ""
	case schema.FieldTypeNumber:
		return 0
	case schema.FieldTypeBool:
		return false
	case schema.FieldTypeDuration:
		return "30s"
	case schema.FieldTypeSelect:
		if len(f.Options) > 0 {
			return f.Options[0]
		}
		return ""
	case schema.FieldTypeArray:
		return "[]"
	case schema.FieldTypeMap, schema.FieldTypeJSON:
		return "{}"
	default:
		return ""
	}
}

// generateStepExample creates an example YAML snippet for a pipeline step type.
func generateStepExample(stepType string, configKeys []string) string {
	var b strings.Builder
	name := strings.TrimPrefix(stepType, "step.")
	name = strings.ReplaceAll(name, "_", "-")
	fmt.Fprintf(&b, "pipelines:\n  my-pipeline:\n    steps:\n")
	fmt.Fprintf(&b, "      - name: %s-step\n        type: %s\n", name, stepType)
	if len(configKeys) > 0 {
		b.WriteString("        config:\n")
		for _, k := range configKeys {
			fmt.Fprintf(&b, "          %s: \"\"\n", k)
		}
	}
	return b.String()
}
