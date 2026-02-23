package module

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---- spec fixtures ----

const petstoreYAML = `
openapi: "3.0.0"
info:
  title: Petstore
  version: "1.0.0"
paths:
  /pets:
    get:
      operationId: listPets
      summary: List all pets
      parameters:
        - name: limit
          in: query
          required: false
          schema:
            type: integer
            minimum: 1
            maximum: 100
      responses:
        "200":
          description: A list of pets
    post:
      operationId: createPet
      summary: Create a pet
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required:
                - name
              properties:
                name:
                  type: string
                  minLength: 1
                tag:
                  type: string
      responses:
        "201":
          description: Created
  /pets/{petId}:
    get:
      operationId: showPetById
      summary: Get a pet by ID
      parameters:
        - name: petId
          in: path
          required: true
          schema:
            type: integer
      responses:
        "200":
          description: Expected response to a valid request
`

const petstoreJSON = `{
  "openapi": "3.0.0",
  "info": {"title": "Petstore JSON", "version": "1.0.0"},
  "paths": {
    "/items": {
      "get": {
        "operationId": "listItems",
        "summary": "List items",
        "responses": {"200": {"description": "ok"}}
      }
    }
  }
}`

// ---- helpers ----

// writeTempSpec writes content to a temp file and returns the path.
func writeTempSpec(t *testing.T, ext, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "spec"+ext)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write temp spec: %v", err)
	}
	return path
}

// newTestRouter is a minimal HTTPRouter that records registered routes.
type testRouter struct {
	routes []struct {
		method, path string
		handler      HTTPHandler
	}
}

func (r *testRouter) AddRoute(method, path string, handler HTTPHandler) {
	r.routes = append(r.routes, struct {
		method, path string
		handler      HTTPHandler
	}{method, path, handler})
}

func (r *testRouter) findHandler(method, path string) HTTPHandler {
	for _, rt := range r.routes {
		if rt.method == method && rt.path == path {
			return rt.handler
		}
	}
	return nil
}

// ---- tests ----

func TestOpenAPIModule_ParseYAML(t *testing.T) {
	specPath := writeTempSpec(t, ".yaml", petstoreYAML)

	mod := NewOpenAPIModule("petstore", OpenAPIConfig{
		SpecFile: specPath,
		BasePath: "/api/v1",
	})

	if err := mod.Init(nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if mod.spec == nil {
		t.Fatal("spec was not parsed")
	}
	if mod.spec.Info.Title != "Petstore" {
		t.Errorf("expected title 'Petstore', got %q", mod.spec.Info.Title)
	}
	if len(mod.spec.Paths) != 2 {
		t.Errorf("expected 2 paths, got %d", len(mod.spec.Paths))
	}
}

func TestOpenAPIModule_ParseJSON(t *testing.T) {
	specPath := writeTempSpec(t, ".json", petstoreJSON)

	mod := NewOpenAPIModule("json-api", OpenAPIConfig{
		SpecFile: specPath,
		BasePath: "/api",
	})

	if err := mod.Init(nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if mod.spec.Info.Title != "Petstore JSON" {
		t.Errorf("expected title 'Petstore JSON', got %q", mod.spec.Info.Title)
	}
}

func TestOpenAPIModule_MissingSpecFile(t *testing.T) {
	mod := NewOpenAPIModule("bad", OpenAPIConfig{})
	if err := mod.Init(nil); err == nil {
		t.Fatal("expected error for missing spec_file")
	}
}

func TestOpenAPIModule_NonExistentFile(t *testing.T) {
	mod := NewOpenAPIModule("bad", OpenAPIConfig{SpecFile: "/does/not/exist.yaml"})
	if err := mod.Init(nil); err == nil {
		t.Fatal("expected error for non-existent spec file")
	}
}

func TestOpenAPIModule_RegisterRoutes(t *testing.T) {
	specPath := writeTempSpec(t, ".yaml", petstoreYAML)

	mod := NewOpenAPIModule("petstore", OpenAPIConfig{
		SpecFile: specPath,
		BasePath: "/api/v1",
	})
	if err := mod.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	router := &testRouter{}
	mod.RegisterRoutes(router)

	// Expect: GET /api/v1/pets, POST /api/v1/pets, GET /api/v1/pets/{petId}
	// plus /api/v1/openapi.json, /api/v1/openapi.yaml
	paths := make(map[string]bool)
	for _, rt := range router.routes {
		paths[rt.method+":"+rt.path] = true
	}

	expected := []string{
		"GET:/api/v1/pets",
		"POST:/api/v1/pets",
		"GET:/api/v1/pets/{petId}",
		"GET:/api/v1/openapi.json",
		"GET:/api/v1/openapi.yaml",
	}
	for _, e := range expected {
		if !paths[e] {
			t.Errorf("expected route %q to be registered, got routes: %v", e, routeKeys(router))
		}
	}
}

func TestOpenAPIModule_SwaggerUIRoute(t *testing.T) {
	specPath := writeTempSpec(t, ".yaml", petstoreYAML)

	mod := NewOpenAPIModule("petstore", OpenAPIConfig{
		SpecFile: specPath,
		BasePath: "/api/v1",
		SwaggerUI: OpenAPISwaggerUIConfig{
			Enabled: true,
			Path:    "/docs",
		},
	})
	if err := mod.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	router := &testRouter{}
	mod.RegisterRoutes(router)

	found := false
	for _, rt := range router.routes {
		if rt.method == "GET" && rt.path == "/api/v1/docs" {
			found = true
		}
	}
	if !found {
		t.Error("Swagger UI route /api/v1/docs not registered")
	}
}

func TestOpenAPIModule_SwaggerUIResponse(t *testing.T) {
	specPath := writeTempSpec(t, ".yaml", petstoreYAML)

	mod := NewOpenAPIModule("petstore", OpenAPIConfig{
		SpecFile: specPath,
		BasePath: "/api/v1",
		SwaggerUI: OpenAPISwaggerUIConfig{
			Enabled: true,
			Path:    "/docs",
		},
	})
	if err := mod.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	router := &testRouter{}
	mod.RegisterRoutes(router)

	h := router.findHandler("GET", "/api/v1/docs")
	if h == nil {
		t.Fatal("swagger UI handler not found")
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/docs", nil)
	h.Handle(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "swagger-ui") {
		t.Error("swagger UI HTML not in response body")
	}
}

func TestOpenAPIModule_SpecEndpoint(t *testing.T) {
	specPath := writeTempSpec(t, ".yaml", petstoreYAML)

	mod := NewOpenAPIModule("petstore", OpenAPIConfig{
		SpecFile: specPath,
		BasePath: "/api/v1",
	})
	if err := mod.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	router := &testRouter{}
	mod.RegisterRoutes(router)

	h := router.findHandler("GET", "/api/v1/openapi.json")
	if h == nil {
		t.Fatal("spec handler not found")
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/openapi.json", nil)
	h.Handle(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Errorf("spec endpoint did not return valid JSON: %v", err)
	}
}

func TestOpenAPIModule_RequestValidation_ValidQuery(t *testing.T) {
	specPath := writeTempSpec(t, ".yaml", petstoreYAML)

	mod := NewOpenAPIModule("petstore", OpenAPIConfig{
		SpecFile:   specPath,
		BasePath:   "/api/v1",
		Validation: OpenAPIValidationConfig{Request: true},
	})
	if err := mod.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	router := &testRouter{}
	mod.RegisterRoutes(router)

	h := router.findHandler("GET", "/api/v1/pets")
	if h == nil {
		t.Fatal("GET /api/v1/pets handler not found")
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/pets?limit=10", nil)
	h.Handle(w, r)

	// 501 is the stub response — validation passed
	if w.Code != http.StatusNotImplemented {
		t.Errorf("expected 501 stub response (validation OK), got %d: %s", w.Code, w.Body.String())
	}
}

func TestOpenAPIModule_RequestValidation_InvalidQuery(t *testing.T) {
	specPath := writeTempSpec(t, ".yaml", petstoreYAML)

	mod := NewOpenAPIModule("petstore", OpenAPIConfig{
		SpecFile:   specPath,
		BasePath:   "/api/v1",
		Validation: OpenAPIValidationConfig{Request: true},
	})
	if err := mod.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	router := &testRouter{}
	mod.RegisterRoutes(router)

	h := router.findHandler("GET", "/api/v1/pets")
	if h == nil {
		t.Fatal("GET /api/v1/pets handler not found")
	}

	// "limit" must be integer — send a non-integer
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/pets?limit=notanumber", nil)
	h.Handle(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 validation error, got %d: %s", w.Code, w.Body.String())
	}
	var errBody map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("could not decode error body: %v", err)
	}
	if errBody["error"] != "request validation failed" {
		t.Errorf("unexpected error body: %v", errBody)
	}
}

func TestOpenAPIModule_RequestValidation_Body(t *testing.T) {
	specPath := writeTempSpec(t, ".yaml", petstoreYAML)

	mod := NewOpenAPIModule("petstore", OpenAPIConfig{
		SpecFile:   specPath,
		BasePath:   "/api/v1",
		Validation: OpenAPIValidationConfig{Request: true},
	})
	if err := mod.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	router := &testRouter{}
	mod.RegisterRoutes(router)

	h := router.findHandler("POST", "/api/v1/pets")
	if h == nil {
		t.Fatal("POST /api/v1/pets handler not found")
	}

	t.Run("valid body", func(t *testing.T) {
		body := `{"name": "Fluffy", "tag": "cat"}`
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/v1/pets", bytes.NewBufferString(body))
		r.Header.Set("Content-Type", "application/json")
		h.Handle(w, r)
		if w.Code != http.StatusNotImplemented {
			t.Errorf("expected 501 stub (validation OK), got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("missing required field", func(t *testing.T) {
		body := `{"tag": "cat"}` // missing 'name'
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/v1/pets", bytes.NewBufferString(body))
		r.Header.Set("Content-Type", "application/json")
		h.Handle(w, r)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400 validation error, got %d: %s", w.Code, w.Body.String())
		}
	})
}

func TestOpenAPIModule_NoValidation(t *testing.T) {
	specPath := writeTempSpec(t, ".yaml", petstoreYAML)

	// Validation disabled — bad input still returns 501 (stub)
	mod := NewOpenAPIModule("petstore", OpenAPIConfig{
		SpecFile:   specPath,
		BasePath:   "/api/v1",
		Validation: OpenAPIValidationConfig{Request: false},
	})
	if err := mod.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	router := &testRouter{}
	mod.RegisterRoutes(router)

	h := router.findHandler("GET", "/api/v1/pets")
	if h == nil {
		t.Fatal("handler not found")
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/pets?limit=notanumber", nil)
	h.Handle(w, r)

	if w.Code != http.StatusNotImplemented {
		t.Errorf("expected 501 (validation disabled), got %d", w.Code)
	}
}

func TestOpenAPIModule_ModuleInterface(t *testing.T) {
	specPath := writeTempSpec(t, ".yaml", petstoreYAML)
	mod := NewOpenAPIModule("petstore", OpenAPIConfig{SpecFile: specPath})

	if mod.Name() != "petstore" {
		t.Errorf("Name() = %q, want %q", mod.Name(), "petstore")
	}
	if deps := mod.Dependencies(); deps != nil {
		t.Errorf("Dependencies() should be nil")
	}
	providers := mod.ProvidesServices()
	if len(providers) != 1 {
		t.Errorf("ProvidesServices() count = %d, want 1", len(providers))
	}
	if providers[0].Name != "petstore" {
		t.Errorf("ProvidesServices()[0].Name = %q, want %q", providers[0].Name, "petstore")
	}
	if reqs := mod.RequiresServices(); reqs != nil {
		t.Errorf("RequiresServices() should be nil")
	}
}

func TestOpenAPIModule_StartStop(t *testing.T) {
	specPath := writeTempSpec(t, ".yaml", petstoreYAML)
	mod := NewOpenAPIModule("petstore", OpenAPIConfig{SpecFile: specPath})
	if err := mod.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := mod.Start(context.TODO()); err != nil {
		t.Errorf("Start: %v", err)
	}
	if err := mod.Stop(context.TODO()); err != nil {
		t.Errorf("Stop: %v", err)
	}
}

func TestParseOpenAPISpec_InvalidYAML(t *testing.T) {
	_, err := parseOpenAPISpec([]byte(":\tinvalid: yaml: ["))
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestValidateScalarValue(t *testing.T) {
	minVal := 1.0
	maxVal := 100.0
	minLen := 2
	maxLen := 5

	tests := []struct {
		name    string
		val     string
		schema  *openAPISchema
		wantErr bool
	}{
		{"valid integer", "42", &openAPISchema{Type: "integer", Minimum: &minVal, Maximum: &maxVal}, false},
		{"too small integer", "0", &openAPISchema{Type: "integer", Minimum: &minVal}, true},
		{"invalid integer", "abc", &openAPISchema{Type: "integer"}, true},
		{"valid number", "3.14", &openAPISchema{Type: "number"}, false},
		{"invalid number", "pi", &openAPISchema{Type: "number"}, true},
		{"valid boolean true", "true", &openAPISchema{Type: "boolean"}, false},
		{"valid boolean false", "false", &openAPISchema{Type: "boolean"}, false},
		{"invalid boolean", "yes", &openAPISchema{Type: "boolean"}, true},
		{"valid string", "hello", &openAPISchema{Type: "string", MinLength: &minLen, MaxLength: &maxLen}, false},
		{"string too short", "a", &openAPISchema{Type: "string", MinLength: &minLen}, true},
		{"string too long", "toolongstring", &openAPISchema{Type: "string", MaxLength: &maxLen}, true},
		{"enum match", "cat", &openAPISchema{Type: "string", Enum: []any{"cat", "dog"}}, false},
		{"enum mismatch", "fish", &openAPISchema{Type: "string", Enum: []any{"cat", "dog"}}, true},
		// Query/path parameters are always strings; integer enum values from YAML
		// (e.g. enum: [1, 2, 3]) are compared as their string representation.
		// This is intentional: the parameter "1" should match the YAML integer 1.
		{"enum integer yaml matches string param", "1", &openAPISchema{Type: "integer", Enum: []any{1, 2, 3}}, false},
		{"enum integer yaml no match", "4", &openAPISchema{Type: "integer", Enum: []any{1, 2, 3}}, true},
		{"enum nil values skipped", "a", &openAPISchema{Enum: []any{nil, "a", nil}}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := validateScalarValue(tt.val, "param", tt.schema)
			if tt.wantErr && len(errs) == 0 {
				t.Error("expected validation error, got none")
			}
			if !tt.wantErr && len(errs) > 0 {
				t.Errorf("unexpected validation errors: %v", errs)
			}
		})
	}
}

func TestSwaggerUIHTML(t *testing.T) {
	html := swaggerUIHTML("My API", "/api/v1/openapi.json")
	if !strings.Contains(html, "swagger-ui") {
		t.Error("HTML missing swagger-ui reference")
	}
	if !strings.Contains(html, "/api/v1/openapi.json") {
		t.Error("HTML missing spec URL")
	}
	if !strings.Contains(html, "My API") {
		t.Error("HTML missing title")
	}
}

func TestHTMLEscape(t *testing.T) {
	got := htmlEscape(`<script>alert("xss")</script>`)
	if strings.Contains(got, "<script>") {
		t.Errorf("HTML not escaped: %s", got)
	}
}

func TestValidateScalarValue_Pattern(t *testing.T) {
	t.Run("valid pattern match", func(t *testing.T) {
		schema := &openAPISchema{Type: "string", Pattern: "^foo[0-9]+$"}
		errs := validateScalarValue("foo123", "param", schema)
		if len(errs) > 0 {
			t.Errorf("expected no errors, got %v", errs)
		}
	})

	t.Run("pattern mismatch", func(t *testing.T) {
		schema := &openAPISchema{Type: "string", Pattern: "^foo[0-9]+$"}
		errs := validateScalarValue("bar", "param", schema)
		if len(errs) == 0 {
			t.Error("expected validation error for non-matching pattern, got none")
		}
	})

	t.Run("invalid regex pattern returns error", func(t *testing.T) {
		schema := &openAPISchema{Type: "string", Pattern: "["}
		errs := validateScalarValue("anything", "param", schema)
		if len(errs) == 0 {
			t.Error("expected validation error for invalid regex pattern, got none")
		}
	})
}

func TestValidateJSONValue_Pattern(t *testing.T) {
	t.Run("valid pattern match", func(t *testing.T) {
		schema := &openAPISchema{Type: "string", Pattern: "^foo[0-9]+$"}
		errs := validateJSONValue("foo123", "body", schema)
		if len(errs) > 0 {
			t.Errorf("expected no errors, got %v", errs)
		}
	})

	t.Run("pattern mismatch", func(t *testing.T) {
		schema := &openAPISchema{Type: "string", Pattern: "^foo[0-9]+$"}
		errs := validateJSONValue("bar", "body", schema)
		if len(errs) == 0 {
			t.Error("expected validation error for non-matching pattern, got none")
		}
	})

	t.Run("invalid regex pattern returns error", func(t *testing.T) {
		schema := &openAPISchema{Type: "string", Pattern: "["}
		errs := validateJSONValue("anything", "body", schema)
		if len(errs) == 0 {
			t.Error("expected validation error for invalid regex pattern, got none")
		}
	})

	t.Run("integer with fractional part rejected", func(t *testing.T) {
		schema := &openAPISchema{Type: "integer"}
		errs := validateJSONValue(float64(3.14), "count", schema)
		if len(errs) == 0 {
			t.Error("expected validation error for fractional integer, got none")
		}
	})

	t.Run("integer without fractional part accepted", func(t *testing.T) {
		schema := &openAPISchema{Type: "integer"}
		errs := validateJSONValue(float64(3), "count", schema)
		if len(errs) > 0 {
			t.Errorf("expected no errors for whole integer, got %v", errs)
		}
	})

	t.Run("type mismatch error includes actual type", func(t *testing.T) {
		schema := &openAPISchema{Type: "string"}
		errs := validateJSONValue(float64(42), "field", schema)
		if len(errs) == 0 {
			t.Error("expected validation error for wrong type, got none")
		}
		if len(errs) > 0 && !strings.Contains(errs[0], "float64") {
			t.Errorf("expected error to mention actual type, got: %s", errs[0])
		}
	})
}

func TestOpenAPIModule_PathParameterValidation(t *testing.T) {
	specPath := writeTempSpec(t, ".yaml", petstoreYAML)

	mod := NewOpenAPIModule("petstore", OpenAPIConfig{
		SpecFile:   specPath,
		BasePath:   "/api/v1",
		Validation: OpenAPIValidationConfig{Request: true},
	})
	if err := mod.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	router := &testRouter{}
	mod.RegisterRoutes(router)

	h := router.findHandler("GET", "/api/v1/pets/{petId}")
	if h == nil {
		t.Fatal("GET /api/v1/pets/{petId} handler not found")
	}

	t.Run("valid integer path param", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/v1/pets/42", nil)
		// Simulate Go 1.22 path value extraction
		r.SetPathValue("petId", "42")
		h.Handle(w, r)
		// 501 = stub response, meaning validation passed
		if w.Code != http.StatusNotImplemented {
			t.Errorf("expected 501 stub (validation OK), got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("invalid non-integer path param", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/v1/pets/not-an-id", nil)
		r.SetPathValue("petId", "not-an-id")
		h.Handle(w, r)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400 validation error for non-integer petId, got %d: %s", w.Code, w.Body.String())
		}
	})
}

// routeKeys returns a list of "METHOD:path" strings from a testRouter.
func routeKeys(r *testRouter) []string {
	keys := make([]string, len(r.routes))
	for i, rt := range r.routes {
		keys[i] = rt.method + ":" + rt.path
	}
	return keys
}
