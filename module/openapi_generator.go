package module

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/CrisisTextLine/modular"
	"gopkg.in/yaml.v3"
)

// --- OpenAPI 3.0 spec structs (minimal inline definitions) ---

// OpenAPISpec represents a minimal OpenAPI 3.0 specification document.
type OpenAPISpec struct {
	OpenAPI    string                  `json:"openapi" yaml:"openapi"`
	Info       OpenAPIInfo             `json:"info" yaml:"info"`
	Servers    []OpenAPIServer         `json:"servers,omitempty" yaml:"servers,omitempty"`
	Paths      map[string]*OpenAPIPath `json:"paths" yaml:"paths"`
	Components *OpenAPIComponents      `json:"components,omitempty" yaml:"components,omitempty"`
}

// OpenAPIInfo holds API metadata.
type OpenAPIInfo struct {
	Title       string `json:"title" yaml:"title"`
	Version     string `json:"version" yaml:"version"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// OpenAPIServer describes a server URL.
type OpenAPIServer struct {
	URL         string `json:"url" yaml:"url"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// OpenAPIPath holds operations for a single path.
type OpenAPIPath struct {
	Get     *OpenAPIOperation `json:"get,omitempty" yaml:"get,omitempty"`
	Post    *OpenAPIOperation `json:"post,omitempty" yaml:"post,omitempty"`
	Put     *OpenAPIOperation `json:"put,omitempty" yaml:"put,omitempty"`
	Delete  *OpenAPIOperation `json:"delete,omitempty" yaml:"delete,omitempty"`
	Patch   *OpenAPIOperation `json:"patch,omitempty" yaml:"patch,omitempty"`
	Options *OpenAPIOperation `json:"options,omitempty" yaml:"options,omitempty"`
}

// OpenAPIOperation describes an API operation.
type OpenAPIOperation struct {
	Summary     string                      `json:"summary,omitempty" yaml:"summary,omitempty"`
	OperationID string                      `json:"operationId,omitempty" yaml:"operationId,omitempty"`
	Tags        []string                    `json:"tags,omitempty" yaml:"tags,omitempty"`
	Parameters  []OpenAPIParameter          `json:"parameters,omitempty" yaml:"parameters,omitempty"`
	RequestBody *OpenAPIRequestBody         `json:"requestBody,omitempty" yaml:"requestBody,omitempty"`
	Responses   map[string]*OpenAPIResponse `json:"responses" yaml:"responses"`
}

// OpenAPIParameter describes a path/query/header parameter.
type OpenAPIParameter struct {
	Name        string         `json:"name" yaml:"name"`
	In          string         `json:"in" yaml:"in"` // path, query, header
	Required    bool           `json:"required,omitempty" yaml:"required,omitempty"`
	Description string         `json:"description,omitempty" yaml:"description,omitempty"`
	Schema      *OpenAPISchema `json:"schema,omitempty" yaml:"schema,omitempty"`
}

// OpenAPIRequestBody describes a request body.
type OpenAPIRequestBody struct {
	Required    bool                         `json:"required,omitempty" yaml:"required,omitempty"`
	Description string                       `json:"description,omitempty" yaml:"description,omitempty"`
	Content     map[string]*OpenAPIMediaType `json:"content,omitempty" yaml:"content,omitempty"`
}

// OpenAPIResponse describes a response.
type OpenAPIResponse struct {
	Description string                       `json:"description" yaml:"description"`
	Content     map[string]*OpenAPIMediaType `json:"content,omitempty" yaml:"content,omitempty"`
}

// OpenAPIMediaType describes a media type with schema.
type OpenAPIMediaType struct {
	Schema *OpenAPISchema `json:"schema,omitempty" yaml:"schema,omitempty"`
}

// OpenAPISchema is a minimal JSON Schema subset for OpenAPI.
type OpenAPISchema struct {
	Type        string                    `json:"type,omitempty" yaml:"type,omitempty"`
	Format      string                    `json:"format,omitempty" yaml:"format,omitempty"`
	Description string                    `json:"description,omitempty" yaml:"description,omitempty"`
	Properties  map[string]*OpenAPISchema `json:"properties,omitempty" yaml:"properties,omitempty"`
	Items       *OpenAPISchema            `json:"items,omitempty" yaml:"items,omitempty"`
	Required    []string                  `json:"required,omitempty" yaml:"required,omitempty"`
}

// OpenAPIComponents holds reusable schema components.
type OpenAPIComponents struct {
	Schemas map[string]*OpenAPISchema `json:"schemas,omitempty" yaml:"schemas,omitempty"`
}

// --- OpenAPI Generator Module ---

// OpenAPIGeneratorConfig holds configuration for the OpenAPI generator module.
type OpenAPIGeneratorConfig struct {
	Title       string   `json:"title" yaml:"title"`
	Version     string   `json:"version" yaml:"version"`
	Description string   `json:"description" yaml:"description"`
	Servers     []string `json:"servers" yaml:"servers"`
}

// OpenAPIGenerator is a module that scans workflow route definitions and
// generates an OpenAPI 3.0 specification, serving it at configurable endpoints.
type OpenAPIGenerator struct {
	name   string
	config OpenAPIGeneratorConfig
	app    modular.Application
	spec   *OpenAPISpec
	mu     sync.RWMutex
}

// NewOpenAPIGenerator creates a new OpenAPI generator module.
func NewOpenAPIGenerator(name string, config OpenAPIGeneratorConfig) *OpenAPIGenerator {
	if config.Title == "" {
		config.Title = "Workflow API"
	}
	if config.Version == "" {
		config.Version = "1.0.0"
	}
	return &OpenAPIGenerator{
		name:   name,
		config: config,
	}
}

// Name returns the module name.
func (g *OpenAPIGenerator) Name() string { return g.name }

// Init registers the generator as a service and builds the initial spec.
func (g *OpenAPIGenerator) Init(app modular.Application) error {
	g.app = app
	return app.RegisterService(g.name, g)
}

// BuildSpec scans the workflow config and builds the OpenAPI spec.
// This should be called after all modules and workflows are registered.
func (g *OpenAPIGenerator) BuildSpec(workflows map[string]any) {
	g.mu.Lock()
	defer g.mu.Unlock()

	spec := &OpenAPISpec{
		OpenAPI: "3.0.3",
		Info: OpenAPIInfo{
			Title:       g.config.Title,
			Version:     g.config.Version,
			Description: g.config.Description,
		},
		Paths: make(map[string]*OpenAPIPath),
	}

	// Add servers
	for _, s := range g.config.Servers {
		spec.Servers = append(spec.Servers, OpenAPIServer{URL: s})
	}

	// Extract routes from workflow configurations
	for workflowType, wfConfig := range workflows {
		g.extractRoutes(spec, workflowType, wfConfig)
	}

	g.spec = spec
}

// pathParamRegex matches {paramName} in route paths.
var pathParamRegex = regexp.MustCompile(`\{([^}]+)\}`)

// extractRoutes extracts HTTP routes from a workflow config section.
func (g *OpenAPIGenerator) extractRoutes(spec *OpenAPISpec, workflowType string, wfConfig any) {
	wfMap, ok := wfConfig.(map[string]any)
	if !ok {
		return
	}

	routesRaw, ok := wfMap["routes"]
	if !ok {
		return
	}

	routes, ok := routesRaw.([]any)
	if !ok {
		return
	}

	for _, routeRaw := range routes {
		route, ok := routeRaw.(map[string]any)
		if !ok {
			continue
		}

		method := ""
		if m, ok := route["method"].(string); ok {
			method = strings.ToLower(m)
		}
		path := ""
		if p, ok := route["path"].(string); ok {
			path = p
		}
		if method == "" || path == "" {
			continue
		}

		handler := ""
		if h, ok := route["handler"].(string); ok {
			handler = h
		}

		// Build operation
		op := g.buildOperation(method, path, handler, workflowType, route)

		// Get or create path item
		pathItem, exists := spec.Paths[path]
		if !exists {
			pathItem = &OpenAPIPath{}
			spec.Paths[path] = pathItem
		}

		// Assign to correct method
		switch method {
		case "get":
			pathItem.Get = op
		case "post":
			pathItem.Post = op
		case "put":
			pathItem.Put = op
		case "delete":
			pathItem.Delete = op
		case "patch":
			pathItem.Patch = op
		case "options":
			pathItem.Options = op
		}
	}
}

// buildOperation creates an OpenAPI operation from a route config.
func (g *OpenAPIGenerator) buildOperation(method, path, handler, workflowType string, route map[string]any) *OpenAPIOperation {
	// Generate operation ID from method + path
	opID := generateOperationID(method, path)

	// Determine tag from handler or workflow type
	tag := handler
	if tag == "" {
		tag = workflowType
	}

	op := &OpenAPIOperation{
		Summary:     fmt.Sprintf("%s %s", strings.ToUpper(method), path),
		OperationID: opID,
		Tags:        []string{tag},
		Responses:   make(map[string]*OpenAPIResponse),
	}

	// Extract path parameters from {param} patterns
	matches := pathParamRegex.FindAllStringSubmatch(path, -1)
	for _, match := range matches {
		paramName := match[1]
		// Skip catch-all patterns like {path...}
		if strings.HasSuffix(paramName, "...") {
			continue
		}
		op.Parameters = append(op.Parameters, OpenAPIParameter{
			Name:     paramName,
			In:       "path",
			Required: true,
			Schema:   &OpenAPISchema{Type: "string"},
		})
	}

	// Add request body for methods that typically have one
	if method == "post" || method == "put" || method == "patch" {
		op.RequestBody = &OpenAPIRequestBody{
			Required: true,
			Content: map[string]*OpenAPIMediaType{
				"application/json": {
					Schema: &OpenAPISchema{Type: "object"},
				},
			},
		}
	}

	// Check for explicit OpenAPI annotations in route config
	if summary, ok := route["summary"].(string); ok {
		op.Summary = summary
	}
	if desc, ok := route["description"].(string); ok {
		op.Summary = desc
	}
	if opIDOverride, ok := route["operationId"].(string); ok {
		op.OperationID = opIDOverride
	}
	if tags, ok := route["tags"].([]any); ok {
		op.Tags = nil
		for _, t := range tags {
			if s, ok := t.(string); ok {
				op.Tags = append(op.Tags, s)
			}
		}
	}

	// Default responses
	op.Responses["200"] = &OpenAPIResponse{
		Description: "Successful response",
		Content: map[string]*OpenAPIMediaType{
			"application/json": {
				Schema: &OpenAPISchema{Type: "object"},
			},
		},
	}
	op.Responses["400"] = &OpenAPIResponse{Description: "Bad request"}
	op.Responses["500"] = &OpenAPIResponse{Description: "Internal server error"}

	// Check middlewares for auth → add 401
	if middlewares, ok := route["middlewares"].([]any); ok {
		for _, mw := range middlewares {
			if mwStr, ok := mw.(string); ok && strings.Contains(mwStr, "auth") {
				op.Responses["401"] = &OpenAPIResponse{Description: "Unauthorized"}
				op.Responses["403"] = &OpenAPIResponse{Description: "Forbidden"}
				break
			}
		}
	}

	return op
}

// generateOperationID creates a camelCase operation ID from method + path.
func generateOperationID(method, path string) string {
	// Remove leading slash and replace special chars
	clean := strings.TrimPrefix(path, "/")
	clean = strings.ReplaceAll(clean, "/", "_")
	clean = strings.ReplaceAll(clean, "{", "by_")
	clean = strings.ReplaceAll(clean, "}", "")
	clean = strings.ReplaceAll(clean, "-", "_")
	clean = strings.ReplaceAll(clean, ".", "_")

	// Build camelCase
	parts := strings.Split(clean, "_")
	var result strings.Builder
	result.WriteString(method)
	for _, p := range parts {
		if p == "" {
			continue
		}
		result.WriteString(strings.ToUpper(p[:1]) + p[1:])
	}
	return result.String()
}

// GetSpec returns the current OpenAPI spec.
func (g *OpenAPIGenerator) GetSpec() *OpenAPISpec {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.spec
}

// ServeJSON serves the OpenAPI spec as JSON.
func (g *OpenAPIGenerator) ServeJSON(w http.ResponseWriter, _ *http.Request) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.spec == nil {
		http.Error(w, "OpenAPI spec not yet generated", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(g.spec); err != nil {
		http.Error(w, "failed to encode spec: "+err.Error(), http.StatusInternalServerError)
	}
}

// ServeYAML serves the OpenAPI spec as YAML.
func (g *OpenAPIGenerator) ServeYAML(w http.ResponseWriter, _ *http.Request) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.spec == nil {
		http.Error(w, "OpenAPI spec not yet generated", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/x-yaml")
	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	if err := enc.Encode(g.spec); err != nil {
		http.Error(w, "failed to encode spec: "+err.Error(), http.StatusInternalServerError)
	}
}

// Handle dispatches to JSON or YAML handler based on path suffix.
func (g *OpenAPIGenerator) Handle(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, ".yaml") || strings.HasSuffix(r.URL.Path, ".yml") {
		g.ServeYAML(w, r)
		return
	}
	g.ServeJSON(w, r)
}

// ServeHTTP implements http.Handler.
func (g *OpenAPIGenerator) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.Handle(w, r)
}

// --- OpenAPI Spec from Route Definitions ---

// RouteDefinition is a simplified route for external spec building.
type RouteDefinition struct {
	Method      string
	Path        string
	Handler     string
	Middlewares []string
	Summary     string
	Tags        []string
}

// BuildSpecFromRoutes builds an OpenAPI spec from explicit route definitions.
func (g *OpenAPIGenerator) BuildSpecFromRoutes(routes []RouteDefinition) {
	g.mu.Lock()
	defer g.mu.Unlock()

	spec := &OpenAPISpec{
		OpenAPI: "3.0.3",
		Info: OpenAPIInfo{
			Title:       g.config.Title,
			Version:     g.config.Version,
			Description: g.config.Description,
		},
		Paths: make(map[string]*OpenAPIPath),
	}

	for _, s := range g.config.Servers {
		spec.Servers = append(spec.Servers, OpenAPIServer{URL: s})
	}

	for _, route := range routes {
		method := strings.ToLower(route.Method)
		routeMap := map[string]any{
			"method":  route.Method,
			"path":    route.Path,
			"handler": route.Handler,
		}
		if route.Summary != "" {
			routeMap["summary"] = route.Summary
		}
		if len(route.Tags) > 0 {
			tags := make([]any, len(route.Tags))
			for i, t := range route.Tags {
				tags[i] = t
			}
			routeMap["tags"] = tags
		}
		if len(route.Middlewares) > 0 {
			mws := make([]any, len(route.Middlewares))
			for i, m := range route.Middlewares {
				mws[i] = m
			}
			routeMap["middlewares"] = mws
		}

		op := g.buildOperation(method, route.Path, route.Handler, "", routeMap)

		pathItem, exists := spec.Paths[route.Path]
		if !exists {
			pathItem = &OpenAPIPath{}
			spec.Paths[route.Path] = pathItem
		}

		switch method {
		case "get":
			pathItem.Get = op
		case "post":
			pathItem.Post = op
		case "put":
			pathItem.Put = op
		case "delete":
			pathItem.Delete = op
		case "patch":
			pathItem.Patch = op
		}
	}

	g.spec = spec
}

// SortedPaths returns the spec paths sorted alphabetically (useful for stable output).
func (g *OpenAPIGenerator) SortedPaths() []string {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.spec == nil {
		return nil
	}

	paths := make([]string, 0, len(g.spec.Paths))
	for p := range g.spec.Paths {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}
