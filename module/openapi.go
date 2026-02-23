package module

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/CrisisTextLine/modular"
	"gopkg.in/yaml.v3"
)

// OpenAPIValidationConfig controls which request/response parts are validated.
type OpenAPIValidationConfig struct {
	Request  bool `yaml:"request"  json:"request"`
	Response bool `yaml:"response" json:"response"`
}

// OpenAPISwaggerUIConfig controls Swagger UI hosting.
type OpenAPISwaggerUIConfig struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Path    string `yaml:"path"    json:"path"`
}

// OpenAPIConfig holds the full configuration for an OpenAPI module.
type OpenAPIConfig struct {
	SpecFile   string                  `yaml:"spec_file"  json:"spec_file"`
	BasePath   string                  `yaml:"base_path"  json:"base_path"`
	Validation OpenAPIValidationConfig `yaml:"validation" json:"validation"`
	SwaggerUI  OpenAPISwaggerUIConfig  `yaml:"swagger_ui" json:"swagger_ui"`
	RouterName string                  `yaml:"router"     json:"router"` // optional: explicit router to attach to
}

// ---- Minimal OpenAPI v3 structs (parsed from YAML/JSON) ----

// openAPISpec is a minimal representation of an OpenAPI 3.x specification.
type openAPISpec struct {
	OpenAPI string                     `yaml:"openapi" json:"openapi"`
	Info    openAPIInfo                `yaml:"info"    json:"info"`
	Paths   map[string]openAPIPathItem `yaml:"paths"   json:"paths"`
}

type openAPIInfo struct {
	Title   string `yaml:"title"   json:"title"`
	Version string `yaml:"version" json:"version"`
}

// openAPIPathItem maps HTTP methods to operation objects.
type openAPIPathItem map[string]*openAPIOperation

// openAPIOperation holds the metadata for a single operation.
type openAPIOperation struct {
	OperationID string                     `yaml:"operationId" json:"operationId"`
	Summary     string                     `yaml:"summary"     json:"summary"`
	Parameters  []openAPIParameter         `yaml:"parameters"  json:"parameters"`
	RequestBody *openAPIRequestBody        `yaml:"requestBody" json:"requestBody"`
	Responses   map[string]openAPIResponse `yaml:"responses"   json:"responses"`
}

// openAPIParameter describes a path, query, header, or cookie parameter.
type openAPIParameter struct {
	Name     string         `yaml:"name"     json:"name"`
	In       string         `yaml:"in"       json:"in"` // path | query | header | cookie
	Required bool           `yaml:"required" json:"required"`
	Schema   *openAPISchema `yaml:"schema"   json:"schema"`
}

// openAPIRequestBody describes the request body for an operation.
type openAPIRequestBody struct {
	Required bool                        `yaml:"required" json:"required"`
	Content  map[string]openAPIMediaType `yaml:"content"  json:"content"`
}

// openAPIMediaType holds a schema for a content type entry.
type openAPIMediaType struct {
	Schema *openAPISchema `yaml:"schema" json:"schema"`
}

// openAPIResponse describes a single response entry.
type openAPIResponse struct {
	Description string `yaml:"description" json:"description"`
}

// openAPISchema is a minimal JSON Schema subset used for parameter/body validation.
type openAPISchema struct {
	Type       string                    `yaml:"type"       json:"type"`
	Required   []string                  `yaml:"required"   json:"required"`
	Properties map[string]*openAPISchema `yaml:"properties" json:"properties"`
	Format     string                    `yaml:"format"     json:"format"`
	Minimum    *float64                  `yaml:"minimum"    json:"minimum"`
	Maximum    *float64                  `yaml:"maximum"    json:"maximum"`
	MinLength  *int                      `yaml:"minLength"  json:"minLength"`
	MaxLength  *int                      `yaml:"maxLength"  json:"maxLength"`
	Pattern    string                    `yaml:"pattern"    json:"pattern"`
	Enum       []any                     `yaml:"enum"       json:"enum"`
}

// ---- OpenAPIModule ----

// OpenAPIModule parses an OpenAPI v3 spec and registers HTTP routes that
// validate incoming requests against the spec schemas.
type OpenAPIModule struct {
	name       string
	cfg        OpenAPIConfig
	spec       *openAPISpec
	specBytes  []byte // raw spec bytes for serving
	routerName string
	logger     *slog.Logger
}

// NewOpenAPIModule creates a new OpenAPIModule with the given name and config.
func NewOpenAPIModule(name string, cfg OpenAPIConfig) *OpenAPIModule {
	return &OpenAPIModule{
		name:       name,
		cfg:        cfg,
		routerName: cfg.RouterName,
		logger:     slog.Default(),
	}
}

// Name returns the module name.
func (m *OpenAPIModule) Name() string { return m.name }

// Init loads and parses the spec file.
func (m *OpenAPIModule) Init(app modular.Application) error {
	if app != nil {
		if logger := app.Logger(); logger != nil {
			if sl, ok := logger.(*slog.Logger); ok {
				m.logger = sl
			}
		}
	}

	if m.cfg.SpecFile == "" {
		return fmt.Errorf("openapi module %q: spec_file is required", m.name)
	}

	data, err := os.ReadFile(m.cfg.SpecFile) //nolint:gosec // path comes from operator config
	if err != nil {
		return fmt.Errorf("openapi module %q: reading spec file %q: %w", m.name, m.cfg.SpecFile, err)
	}
	m.specBytes = data

	spec, err := parseOpenAPISpec(data)
	if err != nil {
		return fmt.Errorf("openapi module %q: parsing spec: %w", m.name, err)
	}
	m.spec = spec
	m.logger.Info("OpenAPI spec loaded",
		"module", m.name,
		"title", spec.Info.Title,
		"version", spec.Info.Version,
		"paths", len(spec.Paths),
	)
	return nil
}

// Dependencies returns nil; routing is wired via ProvidesServices / Init wiring hooks.
func (m *OpenAPIModule) Dependencies() []string { return nil }

// Start is a no-op; routes are registered during wiring.
func (m *OpenAPIModule) Start(_ context.Context) error { return nil }

// Stop is a no-op.
func (m *OpenAPIModule) Stop(_ context.Context) error { return nil }

// ProvidesServices exposes this module as an OpenAPIModule service so wiring
// hooks can find it and register its routes on an HTTP router.
func (m *OpenAPIModule) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        m.name,
			Description: "OpenAPI spec router module",
			Instance:    m,
		},
	}
}

// RequiresServices returns nil; router dependency is resolved via wiring hooks.
func (m *OpenAPIModule) RequiresServices() []modular.ServiceDependency { return nil }

// RouterName returns the optional explicit router module name to attach routes to.
func (m *OpenAPIModule) RouterName() string { return m.routerName }

// RegisterRoutes attaches all spec paths (and optional Swagger UI / spec endpoints)
// to the given HTTPRouter.
func (m *OpenAPIModule) RegisterRoutes(router HTTPRouter) {
	if m.spec == nil {
		m.logger.Warn("OpenAPI spec not loaded; skipping route registration", "module", m.name)
		return
	}

	basePath := strings.TrimRight(m.cfg.BasePath, "/")

	// Register a route for each path+method in the spec
	for specPath, pathItem := range m.spec.Paths {
		for method, op := range pathItem {
			httpMethod := strings.ToUpper(method)
			if !isValidHTTPMethod(httpMethod) {
				continue
			}
			routePath := basePath + openAPIPathToHTTPPath(specPath)
			handler := m.buildRouteHandler(specPath, httpMethod, op)
			router.AddRoute(httpMethod, routePath, handler)
			m.logger.Debug("OpenAPI route registered",
				"module", m.name,
				"method", httpMethod,
				"path", routePath,
				"operationId", op.OperationID,
			)
		}
	}

	// Serve raw spec at /openapi.json and /openapi.yaml
	if len(m.specBytes) > 0 {
		specPathJSON := basePath + "/openapi.json"
		specPathYAML := basePath + "/openapi.yaml"
		specHandler := m.buildSpecHandler()
		router.AddRoute(http.MethodGet, specPathJSON, specHandler)
		router.AddRoute(http.MethodGet, specPathYAML, specHandler)
		m.logger.Debug("OpenAPI spec endpoint registered", "module", m.name, "paths", []string{specPathJSON, specPathYAML})
	}

	// Serve Swagger UI
	if m.cfg.SwaggerUI.Enabled {
		uiPath := m.cfg.SwaggerUI.Path
		if uiPath == "" {
			uiPath = "/docs"
		}
		uiRoutePath := basePath + uiPath
		specURL := basePath + "/openapi.json"
		uiHandler := m.buildSwaggerUIHandler(specURL)
		router.AddRoute(http.MethodGet, uiRoutePath, uiHandler)
		m.logger.Info("Swagger UI registered", "module", m.name, "path", uiRoutePath, "spec", specURL)
	}
}

// ---- Handler builders ----

// buildRouteHandler creates an HTTPHandler that validates the request (if enabled)
// and returns a 501 Not Implemented stub response. In a full integration the
// caller would wrap this handler or replace the stub with real business logic.
func (m *OpenAPIModule) buildRouteHandler(specPath, method string, op *openAPIOperation) HTTPHandler {
	validateReq := m.cfg.Validation.Request
	return &openAPIRouteHandler{
		module:      m,
		specPath:    specPath,
		method:      method,
		op:          op,
		validateReq: validateReq,
	}
}

// buildSpecHandler serves the raw spec bytes as JSON (re-serialised from the
// parsed spec) so consumers always get valid JSON regardless of whether the
// original file was YAML.
func (m *OpenAPIModule) buildSpecHandler() HTTPHandler {
	specJSON, err := json.Marshal(m.spec)
	if err != nil {
		specJSON = m.specBytes // fallback to raw bytes
	}
	return &openAPISpecHandler{specJSON: specJSON}
}

// buildSwaggerUIHandler returns an inline Swagger UI page that loads the spec
// from specURL. This avoids any asset bundling — a CDN-hosted swagger-ui is used.
func (m *OpenAPIModule) buildSwaggerUIHandler(specURL string) HTTPHandler {
	html := swaggerUIHTML(m.spec.Info.Title, specURL)
	return &openAPISwaggerUIHandler{html: html}
}

// ---- openAPIRouteHandler ----

type openAPIRouteHandler struct {
	module      *OpenAPIModule
	specPath    string
	method      string
	op          *openAPIOperation
	validateReq bool
}

func (h *openAPIRouteHandler) Handle(w http.ResponseWriter, r *http.Request) {
	if h.validateReq {
		if errs := h.validate(r); len(errs) > 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":  "request validation failed",
				"errors": errs,
			})
			return
		}
	}

	// Default stub: 501 Not Implemented
	// In a full integration callers wire their own handler on top of this module.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotImplemented)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"error":       "not implemented",
		"operationId": h.op.OperationID,
		"path":        h.specPath,
		"method":      h.method,
	})
}

// validate checks required parameters and request body against the spec.
func (h *openAPIRouteHandler) validate(r *http.Request) []string {
	var errs []string

	// Validate parameters
	for _, p := range h.op.Parameters {
		val := extractParam(r, p)
		if p.Required && val == "" {
			errs = append(errs, fmt.Sprintf("required parameter %q (in %s) is missing", p.Name, p.In))
			continue
		}
		if val != "" && p.Schema != nil {
			if schemaErrs := validateScalarValue(val, p.Name, p.Schema); len(schemaErrs) > 0 {
				errs = append(errs, schemaErrs...)
			}
		}
	}

	// Validate request body
	if h.op.RequestBody != nil {
		ct := r.Header.Get("Content-Type")
		// Normalise content-type (strip params like "; charset=utf-8")
		if idx := strings.Index(ct, ";"); idx >= 0 {
			ct = strings.TrimSpace(ct[:idx])
		}

		var mediaType *openAPIMediaType
		if mt, ok := h.op.RequestBody.Content[ct]; ok {
			mediaType = &mt
		} else if mt, ok := h.op.RequestBody.Content["application/json"]; ok && ct == "" {
			// Default to application/json when no Content-Type is sent
			mediaType = &mt
		}

		if h.op.RequestBody.Required && r.ContentLength == 0 && r.Body == http.NoBody {
			errs = append(errs, "request body is required but missing")
		} else if mediaType != nil && mediaType.Schema != nil {
			bodyBytes, err := io.ReadAll(r.Body)
			if err == nil && len(bodyBytes) > 0 {
				var bodyData any
				if jsonErr := json.Unmarshal(bodyBytes, &bodyData); jsonErr == nil {
					if bodyErrs := validateJSONBody(bodyData, mediaType.Schema); len(bodyErrs) > 0 {
						errs = append(errs, bodyErrs...)
					}
				}
				// Restore body for downstream handlers
				r.Body = io.NopCloser(strings.NewReader(string(bodyBytes)))
			}
		}
	}

	return errs
}

// ---- openAPISpecHandler ----

type openAPISpecHandler struct {
	specJSON []byte
}

func (h *openAPISpecHandler) Handle(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(h.specJSON) //nolint:gosec // G705: spec JSON is loaded from a trusted config file, not user input
}

// ---- openAPISwaggerUIHandler ----

type openAPISwaggerUIHandler struct {
	html string
}

func (h *openAPISwaggerUIHandler) Handle(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(h.html)) //nolint:gosec // G705: HTML is generated from a trusted template, not user input
}

// ---- Helpers ----

// parseOpenAPISpec parses a YAML or JSON byte slice into an openAPISpec.
func parseOpenAPISpec(data []byte) (*openAPISpec, error) {
	var spec openAPISpec
	// Try YAML first (which also handles JSON since JSON is valid YAML)
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("yaml parse: %w", err)
	}
	if spec.OpenAPI == "" {
		// May be JSON that yaml couldn't decode properly; try JSON directly
		if err := json.Unmarshal(data, &spec); err != nil {
			return nil, fmt.Errorf("neither yaml nor json parse succeeded: %w", err)
		}
	}
	return &spec, nil
}

// openAPIPathToHTTPPath converts OpenAPI path templates ({param}) to Go 1.22+
// ServeMux patterns ({param}). For older mux implementations the braces are
// kept since most custom routers accept the same syntax.
func openAPIPathToHTTPPath(specPath string) string {
	// OpenAPI uses {param}; Go 1.22 net/http.ServeMux uses {param} too.
	// No transformation needed — return as-is.
	return specPath
}

// isValidHTTPMethod returns true for standard HTTP verbs (OpenAPI supports a
// defined subset: get, put, post, delete, options, head, patch, trace).
func isValidHTTPMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodPut, http.MethodPost,
		http.MethodDelete, http.MethodOptions, http.MethodHead,
		http.MethodPatch, "TRACE":
		return true
	}
	return false
}

// extractParam extracts a parameter value from the request based on its location.
func extractParam(r *http.Request, p openAPIParameter) string {
	switch p.In {
	case "query":
		return r.URL.Query().Get(p.Name)
	case "header":
		return r.Header.Get(p.Name)
	case "path":
		// Go 1.22 net/http.ServeMux populates path values via r.PathValue
		return r.PathValue(p.Name)
	case "cookie":
		if c, err := r.Cookie(p.Name); err == nil {
			return c.Value
		}
	}
	return ""
}

// validateScalarValue validates a string value against a schema (type/format/enum checks).
func validateScalarValue(val, name string, schema *openAPISchema) []string {
	var errs []string
	switch schema.Type {
	case "integer":
		n, err := strconv.ParseInt(val, 10, 64)
		if err != nil {
			errs = append(errs, fmt.Sprintf("parameter %q must be an integer, got %q", name, val))
			return errs
		}
		if schema.Minimum != nil && float64(n) < *schema.Minimum {
			errs = append(errs, fmt.Sprintf("parameter %q must be >= %v", name, *schema.Minimum))
		}
		if schema.Maximum != nil && float64(n) > *schema.Maximum {
			errs = append(errs, fmt.Sprintf("parameter %q must be <= %v", name, *schema.Maximum))
		}
	case "number":
		f, err := strconv.ParseFloat(val, 64)
		if err != nil {
			errs = append(errs, fmt.Sprintf("parameter %q must be a number, got %q", name, val))
			return errs
		}
		if schema.Minimum != nil && f < *schema.Minimum {
			errs = append(errs, fmt.Sprintf("parameter %q must be >= %v", name, *schema.Minimum))
		}
		if schema.Maximum != nil && f > *schema.Maximum {
			errs = append(errs, fmt.Sprintf("parameter %q must be <= %v", name, *schema.Maximum))
		}
	case "boolean":
		if val != "true" && val != "false" {
			errs = append(errs, fmt.Sprintf("parameter %q must be 'true' or 'false', got %q", name, val))
		}
	case "string":
		if schema.MinLength != nil && len(val) < *schema.MinLength {
			errs = append(errs, fmt.Sprintf("parameter %q must have minLength %d", name, *schema.MinLength))
		}
		if schema.MaxLength != nil && len(val) > *schema.MaxLength {
			errs = append(errs, fmt.Sprintf("parameter %q must have maxLength %d", name, *schema.MaxLength))
		}
		if schema.Pattern != "" {
			if ok, _ := regexp.MatchString(schema.Pattern, val); !ok {
				errs = append(errs, fmt.Sprintf("parameter %q does not match pattern %q", name, schema.Pattern))
			}
		}
	}
	// Enum validation
	if len(schema.Enum) > 0 {
		found := false
		for _, e := range schema.Enum {
			if fmt.Sprintf("%v", e) == val {
				found = true
				break
			}
		}
		if !found {
			errs = append(errs, fmt.Sprintf("parameter %q must be one of %v", name, schema.Enum))
		}
	}
	return errs
}

// validateJSONBody validates a decoded JSON body against an object schema.
func validateJSONBody(body any, schema *openAPISchema) []string {
	var errs []string
	obj, ok := body.(map[string]any)
	if !ok {
		if schema.Type == "object" {
			return []string{"request body must be a JSON object"}
		}
		return nil
	}
	// Check required fields
	for _, req := range schema.Required {
		if _, present := obj[req]; !present {
			errs = append(errs, fmt.Sprintf("request body: required field %q is missing", req))
		}
	}
	// Validate individual properties
	for field, propSchema := range schema.Properties {
		val, present := obj[field]
		if !present {
			continue
		}
		if fieldErrs := validateJSONValue(val, field, propSchema); len(fieldErrs) > 0 {
			errs = append(errs, fieldErrs...)
		}
	}
	return errs
}

// validateJSONValue validates a decoded JSON value against a schema.
func validateJSONValue(val any, name string, schema *openAPISchema) []string {
	var errs []string
	switch schema.Type {
	case "string":
		s, ok := val.(string)
		if !ok {
			return []string{fmt.Sprintf("field %q must be a string", name)}
		}
		if schema.MinLength != nil && len(s) < *schema.MinLength {
			errs = append(errs, fmt.Sprintf("field %q must have minLength %d", name, *schema.MinLength))
		}
		if schema.MaxLength != nil && len(s) > *schema.MaxLength {
			errs = append(errs, fmt.Sprintf("field %q must have maxLength %d", name, *schema.MaxLength))
		}
		if schema.Pattern != "" {
			if ok2, _ := regexp.MatchString(schema.Pattern, s); !ok2 {
				errs = append(errs, fmt.Sprintf("field %q does not match pattern %q", name, schema.Pattern))
			}
		}
	case "integer":
		f, ok := val.(float64)
		if !ok {
			return []string{fmt.Sprintf("field %q must be an integer", name)}
		}
		if schema.Minimum != nil && f < *schema.Minimum {
			errs = append(errs, fmt.Sprintf("field %q must be >= %v", name, *schema.Minimum))
		}
		if schema.Maximum != nil && f > *schema.Maximum {
			errs = append(errs, fmt.Sprintf("field %q must be <= %v", name, *schema.Maximum))
		}
	case "number":
		f, ok := val.(float64)
		if !ok {
			return []string{fmt.Sprintf("field %q must be a number", name)}
		}
		if schema.Minimum != nil && f < *schema.Minimum {
			errs = append(errs, fmt.Sprintf("field %q must be >= %v", name, *schema.Minimum))
		}
		if schema.Maximum != nil && f > *schema.Maximum {
			errs = append(errs, fmt.Sprintf("field %q must be <= %v", name, *schema.Maximum))
		}
	case "boolean":
		if _, ok := val.(bool); !ok {
			errs = append(errs, fmt.Sprintf("field %q must be a boolean", name))
		}
	case "object":
		if subErrs := validateJSONBody(val, schema); len(subErrs) > 0 {
			errs = append(errs, subErrs...)
		}
	}
	// Enum validation
	if len(schema.Enum) > 0 {
		found := false
		for _, e := range schema.Enum {
			if fmt.Sprintf("%v", e) == fmt.Sprintf("%v", val) {
				found = true
				break
			}
		}
		if !found {
			errs = append(errs, fmt.Sprintf("field %q must be one of %v", name, schema.Enum))
		}
	}
	return errs
}

// swaggerUIHTML returns a minimal, self-contained Swagger UI HTML page that
// loads the spec from specURL using the official Swagger UI CDN bundle.
func swaggerUIHTML(title, specURL string) string {
	if title == "" {
		title = "API Documentation"
	}
	return `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>` + htmlEscape(title) + `</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    SwaggerUIBundle({
      url: "` + htmlEscape(specURL) + `",
      dom_id: '#swagger-ui',
      presets: [SwaggerUIBundle.presets.apis, SwaggerUIBundle.SwaggerUIStandalonePreset],
      layout: "StandaloneLayout"
    });
  </script>
</body>
</html>`
}

// htmlEscape escapes a string for safe embedding in HTML attributes/text.
func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&#34;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}
