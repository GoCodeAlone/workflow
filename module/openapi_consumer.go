package module

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/CrisisTextLine/modular"
	"gopkg.in/yaml.v3"
)

// OpenAPIConsumerConfig holds configuration for the OpenAPI consumer module.
type OpenAPIConsumerConfig struct {
	SpecURL  string `json:"specUrl" yaml:"specUrl"`
	SpecFile string `json:"specFile" yaml:"specFile"`
}

// OpenAPIConsumer parses an external OpenAPI spec and generates typed HTTP
// client methods matching the spec operations. It provides an ExternalAPIClient
// service that other modules can use to call the external API.
type OpenAPIConsumer struct {
	name         string
	config       OpenAPIConsumerConfig
	spec         *OpenAPISpec
	client       *http.Client
	fieldMapping *FieldMapping
	mu           sync.RWMutex
}

// NewOpenAPIConsumer creates a new OpenAPI consumer module.
func NewOpenAPIConsumer(name string, config OpenAPIConsumerConfig) *OpenAPIConsumer {
	return &OpenAPIConsumer{
		name:         name,
		config:       config,
		client:       &http.Client{},
		fieldMapping: NewFieldMapping(),
	}
}

// Name returns the module name.
func (c *OpenAPIConsumer) Name() string { return c.name }

// Init registers the consumer as a service and loads the spec.
func (c *OpenAPIConsumer) Init(app modular.Application) error {
	if err := c.loadSpec(); err != nil {
		return fmt.Errorf("openapi consumer %q: failed to load spec: %w", c.name, err)
	}

	return app.RegisterService(c.name, c)
}

// SetClient sets a custom HTTP client (useful for testing).
func (c *OpenAPIConsumer) SetClient(client *http.Client) {
	c.client = client
}

// SetFieldMapping sets the field mapping for transforming data between local
// workflow data and external API schemas.
func (c *OpenAPIConsumer) SetFieldMapping(fm *FieldMapping) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.fieldMapping = fm
}

// GetFieldMapping returns the current field mapping.
func (c *OpenAPIConsumer) GetFieldMapping() *FieldMapping {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.fieldMapping
}

// loadSpec loads the OpenAPI spec from either a URL or a file.
func (c *OpenAPIConsumer) loadSpec() error {
	if c.config.SpecURL != "" {
		return c.loadFromURL(c.config.SpecURL)
	}
	if c.config.SpecFile != "" {
		return c.loadFromFile(c.config.SpecFile)
	}
	return fmt.Errorf("either specUrl or specFile must be provided")
}

// loadFromURL fetches and parses an OpenAPI spec from a URL.
func (c *OpenAPIConsumer) loadFromURL(url string) error {
	resp, err := c.client.Get(url)
	if err != nil {
		return fmt.Errorf("failed to fetch spec from %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("spec URL returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read spec body: %w", err)
	}

	return c.parseSpec(body)
}

// loadFromFile reads and parses an OpenAPI spec from a local file.
func (c *OpenAPIConsumer) loadFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read spec file %s: %w", path, err)
	}

	return c.parseSpec(data)
}

// parseSpec parses JSON or YAML OpenAPI spec data.
func (c *OpenAPIConsumer) parseSpec(data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var spec OpenAPISpec

	// Try JSON first, then YAML
	if err := json.Unmarshal(data, &spec); err != nil {
		if err2 := yaml.Unmarshal(data, &spec); err2 != nil {
			return fmt.Errorf("failed to parse spec as JSON or YAML: json=%v, yaml=%v", err, err2)
		}
	}

	if spec.OpenAPI == "" {
		return fmt.Errorf("invalid OpenAPI spec: missing openapi version field")
	}

	c.spec = &spec

	// Auto-generate field mappings from the spec
	c.generateFieldMappings()

	return nil
}

// generateFieldMappings builds field mapping definitions from the loaded spec.
// It creates mappings between the external API's schema properties and
// local workflow data field names.
func (c *OpenAPIConsumer) generateFieldMappings() {
	if c.spec == nil || c.spec.Components == nil {
		return
	}

	for schemaName, schema := range c.spec.Components.Schemas {
		if schema.Properties == nil {
			continue
		}
		for propName := range schema.Properties {
			// Map external field names using schemaName.propName as the logical name
			logicalName := schemaName + "." + propName
			c.fieldMapping.Set(logicalName, propName)
		}
	}
}

// GetSpec returns the loaded OpenAPI spec.
func (c *OpenAPIConsumer) GetSpec() *OpenAPISpec {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.spec
}

// ListOperations returns all operations defined in the loaded spec.
func (c *OpenAPIConsumer) ListOperations() []ExternalOperation {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.spec == nil {
		return nil
	}

	var ops []ExternalOperation
	for path, pathItem := range c.spec.Paths {
		if pathItem.Get != nil {
			ops = append(ops, operationFromSpec("GET", path, pathItem.Get))
		}
		if pathItem.Post != nil {
			ops = append(ops, operationFromSpec("POST", path, pathItem.Post))
		}
		if pathItem.Put != nil {
			ops = append(ops, operationFromSpec("PUT", path, pathItem.Put))
		}
		if pathItem.Delete != nil {
			ops = append(ops, operationFromSpec("DELETE", path, pathItem.Delete))
		}
		if pathItem.Patch != nil {
			ops = append(ops, operationFromSpec("PATCH", path, pathItem.Patch))
		}
	}

	return ops
}

// ExternalOperation describes a callable operation from an external API spec.
type ExternalOperation struct {
	Method      string   `json:"method"`
	Path        string   `json:"path"`
	OperationID string   `json:"operationId"`
	Summary     string   `json:"summary"`
	Tags        []string `json:"tags"`
	HasBody     bool     `json:"hasBody"`
}

func operationFromSpec(method, path string, op *OpenAPIOperation) ExternalOperation {
	return ExternalOperation{
		Method:      method,
		Path:        path,
		OperationID: op.OperationID,
		Summary:     op.Summary,
		Tags:        op.Tags,
		HasBody:     op.RequestBody != nil,
	}
}

// CallOperation invokes an external API operation by its operation ID.
// It resolves path parameters from the provided data map, applies field mappings,
// and returns the response.
func (c *OpenAPIConsumer) CallOperation(ctx context.Context, operationID string, data map[string]any) (map[string]any, error) {
	c.mu.RLock()
	spec := c.spec
	fm := c.fieldMapping
	c.mu.RUnlock()

	if spec == nil {
		return nil, fmt.Errorf("no spec loaded")
	}

	// Find the operation
	var method, path string
	var op *OpenAPIOperation
	for p, pathItem := range spec.Paths {
		if pathItem.Get != nil && pathItem.Get.OperationID == operationID {
			method, path, op = "GET", p, pathItem.Get
			break
		}
		if pathItem.Post != nil && pathItem.Post.OperationID == operationID {
			method, path, op = "POST", p, pathItem.Post
			break
		}
		if pathItem.Put != nil && pathItem.Put.OperationID == operationID {
			method, path, op = "PUT", p, pathItem.Put
			break
		}
		if pathItem.Delete != nil && pathItem.Delete.OperationID == operationID {
			method, path, op = "DELETE", p, pathItem.Delete
			break
		}
		if pathItem.Patch != nil && pathItem.Patch.OperationID == operationID {
			method, path, op = "PATCH", p, pathItem.Patch
			break
		}
	}

	if op == nil {
		return nil, fmt.Errorf("operation %q not found", operationID)
	}

	// Resolve path parameters
	resolvedPath := path
	for _, param := range op.Parameters {
		if param.In == "path" {
			val, ok := fm.Resolve(data, param.Name)
			if !ok {
				// Fall back to direct data lookup
				val, ok = data[param.Name]
			}
			if !ok {
				return nil, fmt.Errorf("missing path parameter %q", param.Name)
			}
			resolvedPath = strings.ReplaceAll(resolvedPath, "{"+param.Name+"}", fmt.Sprintf("%v", val))
		}
	}

	// Build base URL
	baseURL := ""
	if len(spec.Servers) > 0 {
		baseURL = strings.TrimRight(spec.Servers[0].URL, "/")
	}
	fullURL := baseURL + resolvedPath

	// Build request body for methods that have one
	var bodyReader io.Reader
	if op.RequestBody != nil && data != nil {
		bodyBytes, err := json.Marshal(data)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if bodyReader != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	result := map[string]any{
		"statusCode": resp.StatusCode,
		"status":     resp.Status,
	}

	// Try to parse JSON response
	var jsonResp any
	if err := json.Unmarshal(respBody, &jsonResp); err == nil {
		result["body"] = jsonResp
	} else {
		result["body"] = string(respBody)
	}

	return result, nil
}

// ServeOperations serves the list of available operations as JSON.
func (c *OpenAPIConsumer) ServeOperations(w http.ResponseWriter, _ *http.Request) {
	ops := c.ListOperations()
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(ops); err != nil {
		http.Error(w, "failed to encode operations: "+err.Error(), http.StatusInternalServerError)
	}
}

// ServeSpec serves the loaded spec directly.
func (c *OpenAPIConsumer) ServeSpec(w http.ResponseWriter, _ *http.Request) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.spec == nil {
		http.Error(w, "no spec loaded", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(c.spec); err != nil {
		http.Error(w, "failed to encode spec: "+err.Error(), http.StatusInternalServerError)
	}
}
