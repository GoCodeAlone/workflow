package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"sort"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/module"
	"gopkg.in/yaml.v3"
)

// serverFlag is a flag.Value that accumulates multiple -server flags.
type serverFlag []string

func (s *serverFlag) String() string {
	return strings.Join(*s, ", ")
}

func (s *serverFlag) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func runAPI(args []string) error {
	if len(args) < 1 {
		return apiUsage()
	}
	switch args[0] {
	case "extract":
		return runAPIExtract(args[1:])
	default:
		return apiUsage()
	}
}

func apiUsage() error {
	fmt.Fprintf(flag.CommandLine.Output(), `Usage: wfctl api <subcommand> [options]

Subcommands:
  extract   Extract OpenAPI 3.0 spec from a workflow config file (offline)
`)
	return fmt.Errorf("api subcommand is required")
}

// runAPIExtract parses a workflow YAML config file offline and outputs an
// OpenAPI 3.0 specification of all HTTP endpoints defined in the config.
func runAPIExtract(args []string) error {
	fs := flag.NewFlagSet("api extract", flag.ContinueOnError)
	format := fs.String("format", "json", "Output format: json or yaml")
	title := fs.String("title", "", "API title (default: extracted from config or \"Workflow API\")")
	version := fs.String("version", "1.0.0", "API version")
	var servers serverFlag
	fs.Var(&servers, "server", "Server URL to include (repeatable)")
	output := fs.String("output", "", "Write to file instead of stdout")
	includeSchemas := fs.Bool("include-schemas", true, "Attempt to infer request/response schemas from step types")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl api extract [options] <config.yaml>

Parse a workflow config file offline and output an OpenAPI 3.0 specification
of all HTTP endpoints defined in the config.

Examples:
  wfctl api extract config.yaml
  wfctl api extract -format yaml -output openapi.yaml config.yaml
  wfctl api extract -title "My API" -version "2.0.0" config.yaml
  wfctl api extract -server https://api.example.com config.yaml

Options:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		fs.Usage()
		return fmt.Errorf("config file path is required")
	}

	configPath := fs.Arg(0)
	cfg, err := config.LoadFromFile(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Determine title: flag > config-derived > default
	apiTitle := *title
	if apiTitle == "" {
		apiTitle = extractTitleFromConfig(cfg)
	}
	if apiTitle == "" {
		apiTitle = "Workflow API"
	}

	// Build generator with settings
	genCfg := module.OpenAPIGeneratorConfig{
		Title:   apiTitle,
		Version: *version,
		Servers: []string(servers),
	}
	gen := module.NewOpenAPIGenerator("api-extract", genCfg)

	// Build spec from workflow routes
	gen.BuildSpec(cfg.Workflows)

	// Extract pipeline HTTP endpoints and add them to the spec
	if len(cfg.Pipelines) > 0 {
		pipelineRoutes := extractPipelineRoutes(cfg.Pipelines, *includeSchemas, gen)
		if len(pipelineRoutes) > 0 {
			gen.BuildSpecFromRoutes(appendToExistingSpec(gen, pipelineRoutes))
		}
	}

	if *includeSchemas {
		gen.ApplySchemas()
	}

	spec := gen.GetSpec()
	if spec == nil {
		spec = &module.OpenAPISpec{
			OpenAPI: "3.0.3",
			Info: module.OpenAPIInfo{
				Title:   apiTitle,
				Version: *version,
			},
			Paths: make(map[string]*module.OpenAPIPath),
		}
	}

	// Determine output writer
	var w *os.File
	if *output != "" {
		f, err := os.Create(*output)
		if err != nil {
			return fmt.Errorf("failed to create output file: %w", err)
		}
		defer f.Close()
		w = f
	} else {
		w = os.Stdout
	}

	// Encode output
	switch strings.ToLower(*format) {
	case "yaml", "yml":
		enc := yaml.NewEncoder(w)
		enc.SetIndent(2)
		if err := enc.Encode(spec); err != nil {
			return fmt.Errorf("failed to encode spec as YAML: %w", err)
		}
	case "json", "":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(spec); err != nil {
			return fmt.Errorf("failed to encode spec as JSON: %w", err)
		}
	default:
		return fmt.Errorf("unsupported format %q: use json or yaml", *format)
	}

	if *output != "" {
		fmt.Fprintf(os.Stderr, "OpenAPI spec written to %s\n", *output)
	}
	return nil
}

// extractTitleFromConfig attempts to derive a meaningful API title from the config.
// It looks for module names that suggest an application name.
func extractTitleFromConfig(cfg *config.WorkflowConfig) string {
	// Look for a server module with a descriptive name
	for _, mod := range cfg.Modules {
		if mod.Type == "http.server" && mod.Name != "server" && mod.Name != "" {
			return strings.Title(strings.ReplaceAll(mod.Name, "-", " ")) //nolint:staticcheck
		}
	}
	return ""
}

// pipelineEndpoint describes an HTTP endpoint extracted from a pipeline definition.
type pipelineEndpoint struct {
	name           string
	method         string
	path           string
	steps          []map[string]any
	includeSchemas bool
}

// extractPipelineRoutes scans the pipelines map for HTTP-triggered pipelines
// and returns route definitions for each one.
func extractPipelineRoutes(pipelines map[string]any, includeSchemas bool, gen *module.OpenAPIGenerator) []module.RouteDefinition {
	// Collect endpoints sorted by name for stable output
	names := make([]string, 0, len(pipelines))
	for name := range pipelines {
		names = append(names, name)
	}
	sort.Strings(names)

	var routes []module.RouteDefinition
	for _, name := range names {
		raw := pipelines[name]
		pipelineMap, ok := raw.(map[string]any)
		if !ok {
			continue
		}

		ep := parsePipelineEndpoint(name, pipelineMap, includeSchemas)
		if ep == nil {
			continue
		}

		route := module.RouteDefinition{
			Method:  strings.ToUpper(ep.method),
			Path:    ep.path,
			Handler: ep.name,
			Tags:    []string{"pipelines"},
			Summary: fmt.Sprintf("%s %s (pipeline: %s)", strings.ToUpper(ep.method), ep.path, ep.name),
		}

		if includeSchemas && len(ep.steps) > 0 {
			applyPipelineSchemas(gen, ep)
		}

		routes = append(routes, route)
	}
	return routes
}

// parsePipelineEndpoint extracts HTTP trigger details from a pipeline config map.
// Returns nil if the pipeline has no HTTP trigger.
func parsePipelineEndpoint(name string, pipelineMap map[string]any, includeSchemas bool) *pipelineEndpoint {
	triggerRaw, ok := pipelineMap["trigger"]
	if !ok {
		return nil
	}
	triggerMap, ok := triggerRaw.(map[string]any)
	if !ok {
		return nil
	}

	triggerType, _ := triggerMap["type"].(string)
	if triggerType != "http" {
		return nil
	}

	triggerConfig, ok := triggerMap["config"].(map[string]any)
	if !ok {
		return nil
	}

	path, _ := triggerConfig["path"].(string)
	method, _ := triggerConfig["method"].(string)
	if path == "" || method == "" {
		return nil
	}

	ep := &pipelineEndpoint{
		name:           name,
		method:         method,
		path:           path,
		includeSchemas: includeSchemas,
	}

	// Extract steps
	if stepsRaw, ok := pipelineMap["steps"].([]any); ok {
		for _, stepRaw := range stepsRaw {
			if stepMap, ok := stepRaw.(map[string]any); ok {
				ep.steps = append(ep.steps, stepMap)
			}
		}
	}

	return ep
}

// applyPipelineSchemas infers request/response schemas from pipeline step types
// and registers them with the OpenAPI generator.
func applyPipelineSchemas(gen *module.OpenAPIGenerator, ep *pipelineEndpoint) {
	var reqSchema *module.OpenAPISchema
	var respSchema *module.OpenAPISchema
	hasAuthRequired := false
	var statusCode string

	for _, step := range ep.steps {
		stepType, _ := step["type"].(string)
		stepCfg, _ := step["config"].(map[string]any)

		switch stepType {
		case "step.validate":
			// Infer request body schema from validation rules
			if stepCfg != nil {
				if reqSchema == nil {
					reqSchema = &module.OpenAPISchema{
						Type:       "object",
						Properties: make(map[string]*module.OpenAPISchema),
					}
				}
				inferValidateSchema(reqSchema, stepCfg)
			}

		case "step.user_register":
			// Request: email + password; Response: user object
			if reqSchema == nil {
				reqSchema = userCredentialsSchema()
			}
			if respSchema == nil {
				respSchema = userObjectSchema()
			}

		case "step.user_login":
			// Expects email and password in request, returns token in response
			if reqSchema == nil {
				reqSchema = userCredentialsSchema()
			}
			if respSchema == nil {
				respSchema = loginResponseSchema()
			}

		case "step.auth_required":
			hasAuthRequired = true

		case "step.json_response":
			// Determine status code from config
			if stepCfg != nil {
				if sc, ok := stepCfg["statusCode"]; ok {
					statusCode = fmt.Sprintf("%v", sc)
				} else if sc, ok := stepCfg["status"]; ok {
					statusCode = fmt.Sprintf("%v", sc)
				}
				// Infer response schema from body if present
				if body, ok := stepCfg["body"]; ok {
					if respSchema == nil {
						respSchema = inferBodySchema(body)
					}
				}
			}
			if respSchema == nil {
				respSchema = &module.OpenAPISchema{Type: "object"}
			}
		}
	}

	// Set the inferred schemas on the operation
	gen.SetOperationSchema(ep.method, ep.path, reqSchema, respSchema)

	// If auth is required or we have a custom status code, register a component schema
	// and set additional responses.
	if hasAuthRequired || statusCode != "" {
		// We handle the status code override by registering a component schema
		// The ApplySchemas call will wire up the request/response schemas.
		// For auth, register a 401 schema by adding it as a component.
		if hasAuthRequired {
			gen.RegisterComponentSchema("UnauthorizedError", &module.OpenAPISchema{
				Type: "object",
				Properties: map[string]*module.OpenAPISchema{
					"error": {Type: "string", Example: "Unauthorized"},
				},
			})
		}
	}
}

// inferValidateSchema parses step.validate config rules and populates an OpenAPI schema.
// Rule format: "required,email" or "required,min=8".
func inferValidateSchema(schema *module.OpenAPISchema, stepCfg map[string]any) {
	rules, ok := stepCfg["rules"].(map[string]any)
	if !ok {
		return
	}

	for field, ruleRaw := range rules {
		ruleStr, _ := ruleRaw.(string)
		parts := strings.Split(ruleStr, ",")

		fieldSchema := &module.OpenAPISchema{Type: "string"}
		isRequired := false

		for _, part := range parts {
			part = strings.TrimSpace(part)
			switch {
			case part == "required":
				isRequired = true
			case part == "email":
				fieldSchema.Format = "email"
			case strings.HasPrefix(part, "min="):
				// min length hint — keep as string
			case part == "numeric" || part == "number":
				fieldSchema.Type = "number"
			case part == "boolean" || part == "bool":
				fieldSchema.Type = "boolean"
			}
		}

		schema.Properties[field] = fieldSchema
		if isRequired {
			schema.Required = append(schema.Required, field)
		}
	}

	// Sort required for stable output
	sort.Strings(schema.Required)
}

// inferBodySchema creates a schema from a body config value.
func inferBodySchema(body any) *module.OpenAPISchema {
	bodyMap, ok := body.(map[string]any)
	if !ok {
		return &module.OpenAPISchema{Type: "object"}
	}

	schema := &module.OpenAPISchema{
		Type:       "object",
		Properties: make(map[string]*module.OpenAPISchema),
	}
	for k, v := range bodyMap {
		switch v.(type) {
		case int, int64, float64:
			schema.Properties[k] = &module.OpenAPISchema{Type: "integer"}
		case bool:
			schema.Properties[k] = &module.OpenAPISchema{Type: "boolean"}
		default:
			schema.Properties[k] = &module.OpenAPISchema{Type: "string"}
		}
	}
	return schema
}

// userCredentialsSchema returns a schema for email+password request bodies.
func userCredentialsSchema() *module.OpenAPISchema {
	return &module.OpenAPISchema{
		Type: "object",
		Properties: map[string]*module.OpenAPISchema{
			"email":    {Type: "string", Format: "email"},
			"password": {Type: "string", Format: "password"},
		},
		Required: []string{"email", "password"},
	}
}

// userObjectSchema returns a schema for a user response object.
func userObjectSchema() *module.OpenAPISchema {
	return &module.OpenAPISchema{
		Type: "object",
		Properties: map[string]*module.OpenAPISchema{
			"id":    {Type: "string", Format: "uuid"},
			"email": {Type: "string", Format: "email"},
		},
	}
}

// loginResponseSchema returns a schema for a login response with a token.
func loginResponseSchema() *module.OpenAPISchema {
	return &module.OpenAPISchema{
		Type: "object",
		Properties: map[string]*module.OpenAPISchema{
			"token": {Type: "string", Description: "JWT access token"},
		},
		Required: []string{"token"},
	}
}

// appendToExistingSpec builds a combined route list from the existing spec paths
// plus new pipeline routes, used when rebuilding the spec to include both.
func appendToExistingSpec(gen *module.OpenAPIGenerator, pipelineRoutes []module.RouteDefinition) []module.RouteDefinition {
	spec := gen.GetSpec()
	if spec == nil {
		return pipelineRoutes
	}

	// Collect existing routes from the spec
	var existing []module.RouteDefinition
	paths := gen.SortedPaths()
	for _, path := range paths {
		pathItem := spec.Paths[path]
		if pathItem == nil {
			continue
		}
		ops := []struct {
			method string
			op     *module.OpenAPIOperation
		}{
			{"GET", pathItem.Get},
			{"POST", pathItem.Post},
			{"PUT", pathItem.Put},
			{"DELETE", pathItem.Delete},
			{"PATCH", pathItem.Patch},
			{"OPTIONS", pathItem.Options},
		}
		for _, entry := range ops {
			if entry.op == nil {
				continue
			}
			route := module.RouteDefinition{
				Method:  entry.method,
				Path:    path,
				Summary: entry.op.Summary,
				Tags:    entry.op.Tags,
			}
			if len(entry.op.Parameters) > 0 {
				// Extract handler from tags if available
				if len(entry.op.Tags) > 0 {
					route.Handler = entry.op.Tags[0]
				}
			}
			// Extract middlewares hints from responses
			if _, hasAuth := entry.op.Responses["401"]; hasAuth {
				route.Middlewares = []string{"auth"}
			}
			existing = append(existing, route)
		}
	}

	return append(existing, pipelineRoutes...)
}
