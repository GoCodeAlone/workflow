package module

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// ---- spec fixtures ----

const xPipelineYAML = `
openapi: "3.0.0"
info:
  title: Pipeline API
  version: "1.0.0"
paths:
  /greet:
    get:
      operationId: greetUser
      summary: Greet the user
      x-pipeline: greet-pipeline
      parameters:
        - name: name
          in: query
          required: false
          schema:
            type: string
      responses:
        "200":
          description: Greeting
  /stub:
    get:
      operationId: stubOp
      summary: No pipeline
      responses:
        "200":
          description: OK
`

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

func TestOpenAPIModule_JSONSourceNoYAMLEndpoint(t *testing.T) {
	specPath := writeTempSpec(t, ".json", petstoreJSON)

	mod := NewOpenAPIModule("json-api", OpenAPIConfig{
		SpecFile: specPath,
		BasePath: "/api",
	})
	if err := mod.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	router := &testRouter{}
	mod.RegisterRoutes(router)

	paths := make(map[string]bool)
	for _, rt := range router.routes {
		paths[rt.method+":"+rt.path] = true
	}

	// JSON source spec: /openapi.json should be registered
	if !paths["GET:/api/openapi.json"] {
		t.Error("expected GET:/api/openapi.json to be registered for JSON source")
	}
	// /openapi.yaml should NOT be registered for a JSON source spec
	if paths["GET:/api/openapi.yaml"] {
		t.Error("expected GET:/api/openapi.yaml NOT to be registered for JSON source")
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

func TestOpenAPIModule_RegisterRoutesFalse(t *testing.T) {
	specPath := writeTempSpec(t, ".yaml", petstoreYAML)

	falseVal := false
	mod := NewOpenAPIModule("petstore", OpenAPIConfig{
		SpecFile:       specPath,
		BasePath:       "/api/v1",
		RegisterRoutes: &falseVal,
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

	paths := make(map[string]bool)
	for _, rt := range router.routes {
		paths[rt.method+":"+rt.path] = true
	}

	// Spec endpoints and Swagger UI should still be registered
	if !paths["GET:/api/v1/openapi.json"] {
		t.Error("expected GET:/api/v1/openapi.json to be registered even when register_routes=false")
	}
	if !paths["GET:/api/v1/openapi.yaml"] {
		t.Error("expected GET:/api/v1/openapi.yaml to be registered even when register_routes=false")
	}
	if !paths["GET:/api/v1/docs"] {
		t.Error("expected GET:/api/v1/docs (Swagger UI) to be registered even when register_routes=false")
	}

	// Spec-path routes must NOT be registered
	specRoutes := []string{
		"GET:/api/v1/pets",
		"POST:/api/v1/pets",
		"GET:/api/v1/pets/{petId}",
	}
	for _, route := range specRoutes {
		if paths[route] {
			t.Errorf("expected spec route %q NOT to be registered when register_routes=false", route)
		}
	}
}

func TestOpenAPIModule_RegisterRoutesNilDefaultsTrue(t *testing.T) {
	specPath := writeTempSpec(t, ".yaml", petstoreYAML)

	// When RegisterRoutes is nil (not set), spec-path routes should be registered (default true).
	mod := NewOpenAPIModule("petstore", OpenAPIConfig{
		SpecFile: specPath,
		BasePath: "/api/v1",
	})
	if err := mod.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	router := &testRouter{}
	mod.RegisterRoutes(router)

	paths := make(map[string]bool)
	for _, rt := range router.routes {
		paths[rt.method+":"+rt.path] = true
	}

	for _, route := range []string{"GET:/api/v1/pets", "POST:/api/v1/pets", "GET:/api/v1/pets/{petId}"} {
		if !paths[route] {
			t.Errorf("expected spec route %q to be registered when register_routes is not set (default true)", route)
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

	t.Run("invalid JSON", func(t *testing.T) {
		body := `{"name": "Fluffy",` // malformed JSON
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/v1/pets", bytes.NewBufferString(body))
		r.Header.Set("Content-Type", "application/json")
		h.Handle(w, r)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400 validation error for malformed JSON, got %d: %s", w.Code, w.Body.String())
		}
	})
}

const webhookFormYAML = `
openapi: "3.0.0"
info:
  title: Webhook API
  version: "1.0.0"
paths:
  /webhook:
    post:
      operationId: receiveWebhook
      requestBody:
        required: true
        content:
          application/x-www-form-urlencoded:
            schema:
              type: object
              required:
                - Body
              properties:
                Body:
                  type: string
                  minLength: 1
                From:
                  type: string
      responses:
        "200":
          description: OK
`

func TestOpenAPIModule_RequestValidation_FormEncoded(t *testing.T) {
	specPath := writeTempSpec(t, ".yaml", webhookFormYAML)

	mod := NewOpenAPIModule("webhook", OpenAPIConfig{
		SpecFile:   specPath,
		Validation: OpenAPIValidationConfig{Request: true},
	})
	if err := mod.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	router := &testRouter{}
	mod.RegisterRoutes(router)

	h := router.findHandler("POST", "/webhook")
	if h == nil {
		t.Fatal("POST /webhook handler not found")
	}

	t.Run("valid form body", func(t *testing.T) {
		body := "Body=Hello+World&From=%2B15551234567"
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		h.Handle(w, r)
		if w.Code != http.StatusNotImplemented {
			t.Errorf("expected 501 stub (validation OK), got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("missing required field", func(t *testing.T) {
		body := "From=%2B15551234567" // missing required 'Body'
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		h.Handle(w, r)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400 validation error for missing required field, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "Body") {
			t.Errorf("expected error mentioning 'Body', got: %s", w.Body.String())
		}
	})

	t.Run("empty body when required", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(""))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		h.Handle(w, r)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for empty required body, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("present-but-empty field violates minLength", func(t *testing.T) {
		body := "Body=" // Body key present but empty value, violates minLength:1
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		h.Handle(w, r)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for empty field with minLength, got %d: %s", w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), "minLength") {
			t.Errorf("expected minLength error, got: %s", w.Body.String())
		}
	})
}

func TestOpenAPIModule_MaxBodySize(t *testing.T) {
	specPath := writeTempSpec(t, ".yaml", petstoreYAML)

	mod := NewOpenAPIModule("petstore", OpenAPIConfig{
		SpecFile:     specPath,
		BasePath:     "/api/v1",
		Validation:   OpenAPIValidationConfig{Request: true},
		MaxBodyBytes: 10, // very small limit to trigger the check
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

	body := `{"name": "Fluffy", "tag": "cat"}` // 33 bytes, exceeds limit of 10
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/pets", bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/json")
	h.Handle(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for oversized body, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "exceeds maximum") {
		t.Errorf("expected error message about size limit, got: %s", w.Body.String())
	}
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
			errs := validateScalarValue(tt.val, "param", "parameter", tt.schema)
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
		errs := validateScalarValue("foo123", "param", "parameter", schema)
		if len(errs) > 0 {
			t.Errorf("expected no errors, got %v", errs)
		}
	})

	t.Run("pattern mismatch", func(t *testing.T) {
		schema := &openAPISchema{Type: "string", Pattern: "^foo[0-9]+$"}
		errs := validateScalarValue("bar", "param", "parameter", schema)
		if len(errs) == 0 {
			t.Error("expected validation error for non-matching pattern, got none")
		}
	})

	t.Run("invalid regex pattern returns error", func(t *testing.T) {
		schema := &openAPISchema{Type: "string", Pattern: "["}
		errs := validateScalarValue("anything", "param", "parameter", schema)
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

// ---- x-pipeline tests ----

func TestOpenAPIModule_XPipelineParsed(t *testing.T) {
	specPath := writeTempSpec(t, ".yaml", xPipelineYAML)

	mod := NewOpenAPIModule("pipe-api", OpenAPIConfig{
		SpecFile: specPath,
		BasePath: "/api",
	})
	if err := mod.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Verify x-pipeline was parsed for the greet operation.
	pathItem := mod.spec.Paths["/greet"]
	if pathItem == nil {
		t.Fatal("expected /greet path in spec")
	}
	op := pathItem["get"]
	if op == nil {
		t.Fatal("expected GET operation on /greet")
	}
	if op.XPipeline != "greet-pipeline" {
		t.Errorf("expected x-pipeline 'greet-pipeline', got %q", op.XPipeline)
	}

	// Verify the stub operation has no x-pipeline.
	stubItem := mod.spec.Paths["/stub"]
	if stubItem == nil {
		t.Fatal("expected /stub path in spec")
	}
	stubOp := stubItem["get"]
	if stubOp == nil {
		t.Fatal("expected GET operation on /stub")
	}
	if stubOp.XPipeline != "" {
		t.Errorf("expected empty x-pipeline on stub, got %q", stubOp.XPipeline)
	}
}

func TestOpenAPIModule_XPipeline_ExecutesPipeline(t *testing.T) {
	specPath := writeTempSpec(t, ".yaml", xPipelineYAML)

	mod := NewOpenAPIModule("pipe-api", OpenAPIConfig{
		SpecFile: specPath,
		BasePath: "/api",
	})
	if err := mod.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Create a simple pipeline that sets a greeting.
	greetStep := &stubPipelineStep{
		name: "set-greeting",
		exec: func(_ context.Context, pc *PipelineContext) (*StepResult, error) {
			name := "world"
			if n, ok := pc.TriggerData["name"].(string); ok && n != "" {
				name = n
			}
			return &StepResult{Output: map[string]any{"greeting": "hello " + name}}, nil
		},
	}
	greetPipeline := &Pipeline{
		Name:  "greet-pipeline",
		Steps: []PipelineStep{greetStep},
	}

	mod.SetPipelineLookup(func(name string) (*Pipeline, bool) {
		if name == "greet-pipeline" {
			return greetPipeline, true
		}
		return nil, false
	})

	router := &testRouter{}
	mod.RegisterRoutes(router)

	h := router.findHandler("GET", "/api/greet")
	if h == nil {
		t.Fatal("GET /api/greet handler not found")
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/greet?name=Alice", nil)
	h.Handle(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	if resp["greeting"] != "hello Alice" {
		t.Errorf("expected greeting 'hello Alice', got %v", resp["greeting"])
	}
}

func TestOpenAPIModule_XPipeline_NotFound(t *testing.T) {
	specPath := writeTempSpec(t, ".yaml", xPipelineYAML)

	mod := NewOpenAPIModule("pipe-api", OpenAPIConfig{
		SpecFile: specPath,
		BasePath: "/api",
	})
	if err := mod.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Set a lookup that never finds the pipeline.
	mod.SetPipelineLookup(func(name string) (*Pipeline, bool) {
		return nil, false
	})

	router := &testRouter{}
	mod.RegisterRoutes(router)

	h := router.findHandler("GET", "/api/greet")
	if h == nil {
		t.Fatal("GET /api/greet handler not found")
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/greet", nil)
	h.Handle(w, r)

	if w.Code != http.StatusBadGateway {
		t.Errorf("expected 502 for missing pipeline, got %d: %s", w.Code, w.Body.String())
	}
}

func TestOpenAPIModule_XPipeline_StubWithoutPipeline(t *testing.T) {
	specPath := writeTempSpec(t, ".yaml", xPipelineYAML)

	mod := NewOpenAPIModule("pipe-api", OpenAPIConfig{
		SpecFile: specPath,
		BasePath: "/api",
	})
	if err := mod.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// No pipeline lookup set — routes without x-pipeline should still return 501.
	router := &testRouter{}
	mod.RegisterRoutes(router)

	h := router.findHandler("GET", "/api/stub")
	if h == nil {
		t.Fatal("GET /api/stub handler not found")
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/stub", nil)
	h.Handle(w, r)

	if w.Code != http.StatusNotImplemented {
		t.Errorf("expected 501 for stub, got %d: %s", w.Code, w.Body.String())
	}
}

func TestOpenAPIModule_XPipeline_NilLookup(t *testing.T) {
	specPath := writeTempSpec(t, ".yaml", xPipelineYAML)

	mod := NewOpenAPIModule("pipe-api", OpenAPIConfig{
		SpecFile: specPath,
		BasePath: "/api",
	})
	if err := mod.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// No lookup set — x-pipeline route should return 500.
	router := &testRouter{}
	mod.RegisterRoutes(router)

	h := router.findHandler("GET", "/api/greet")
	if h == nil {
		t.Fatal("GET /api/greet handler not found")
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/greet", nil)
	h.Handle(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for nil pipeline lookup, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "pipeline lookup not configured") {
		t.Errorf("expected descriptive error, got: %s", w.Body.String())
	}
}

func TestOpenAPIExtractRequestData_JSONBody(t *testing.T) {
	body := `{"foo": "bar", "count": 42}`
	r := httptest.NewRequest(http.MethodPost, "/test?q=1", bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/json")

	data := openAPIExtractRequestData(r)

	if data["q"] != "1" {
		t.Errorf("expected query param q=1, got %v", data["q"])
	}
	if data["foo"] != "bar" {
		t.Errorf("expected body field foo=bar, got %v", data["foo"])
	}
	if data["count"] != float64(42) {
		t.Errorf("expected body field count=42, got %v", data["count"])
	}

	// Body must be restored.
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("failed to read restored body: %v", err)
	}
	if string(bodyBytes) != body {
		t.Errorf("body not restored: got %q, want %q", string(bodyBytes), body)
	}
}

func TestOpenAPIExtractRequestData_QueryParamNotOverwrittenByBody(t *testing.T) {
	body := `{"name": "from-body"}`
	r := httptest.NewRequest(http.MethodPost, "/test?name=from-query", bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/json")

	data := openAPIExtractRequestData(r)

	// Query param should win over body field.
	if data["name"] != "from-query" {
		t.Errorf("expected query param to win, got %v", data["name"])
	}
}

func TestOpenAPIExtractRequestData_NonJSONBodySkipped(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString("plain text"))
	r.Header.Set("Content-Type", "text/plain")

	data := openAPIExtractRequestData(r)

	// No body fields should appear.
	if len(data) != 0 {
		t.Errorf("expected empty data for non-JSON body, got %v", data)
	}
}

// stubPipelineStep is a minimal PipelineStep implementation for testing.
type stubPipelineStep struct {
	name string
	exec func(ctx context.Context, pc *PipelineContext) (*StepResult, error)
}

func (s *stubPipelineStep) Name() string { return s.name }
func (s *stubPipelineStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	return s.exec(ctx, pc)
}

// TestOpenAPIModule_XPipeline_ResponseStatusFromContext verifies that when a
// pipeline step sets response_status/response_body/response_headers in its
// output and no step writes directly to the HTTP response writer, the openapi
// handler uses those values instead of falling through to 200 with all state.
func TestOpenAPIModule_XPipeline_ResponseStatusFromContext(t *testing.T) {
	specPath := writeTempSpec(t, ".yaml", xPipelineYAML)

	mod := NewOpenAPIModule("pipe-api", OpenAPIConfig{
		SpecFile: specPath,
		BasePath: "/api",
	})
	if err := mod.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Pipeline step that returns a 403 with a custom body via result.Current,
	// without writing to the HTTP response writer directly.
	authStep := &stubPipelineStep{
		name: "auth-check",
		exec: func(_ context.Context, _ *PipelineContext) (*StepResult, error) {
			return &StepResult{
				Output: map[string]any{
					"response_status": 403,
					"response_body":   `{"error":"forbidden"}`,
					"response_headers": map[string]any{
						"Content-Type": "application/json",
					},
				},
				Stop: true,
			}, nil
		},
	}
	authPipeline := &Pipeline{
		Name:  "greet-pipeline",
		Steps: []PipelineStep{authStep},
	}

	mod.SetPipelineLookup(func(name string) (*Pipeline, bool) {
		if name == "greet-pipeline" {
			return authPipeline, true
		}
		return nil, false
	})

	router := &testRouter{}
	mod.RegisterRoutes(router)

	h := router.findHandler("GET", "/api/greet")
	if h == nil {
		t.Fatal("GET /api/greet handler not found")
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/greet", nil)
	h.Handle(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 from pipeline context, got %d: %s", w.Code, w.Body.String())
	}
	if w.Body.String() != `{"error":"forbidden"}` {
		t.Errorf("expected pipeline body, got %q", w.Body.String())
	}
	if w.Header().Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type header, got %q", w.Header().Get("Content-Type"))
	}
}

// TestOpenAPIModule_XPipeline_NoResponseStatusFallsThrough verifies that when
// response_status is absent from result.Current, the handler still falls through
// to the default 200 JSON encoding of result.Current.
func TestOpenAPIModule_XPipeline_NoResponseStatusFallsThrough(t *testing.T) {
	specPath := writeTempSpec(t, ".yaml", xPipelineYAML)

	mod := NewOpenAPIModule("pipe-api", OpenAPIConfig{
		SpecFile: specPath,
		BasePath: "/api",
	})
	if err := mod.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	dataStep := &stubPipelineStep{
		name: "produce-data",
		exec: func(_ context.Context, _ *PipelineContext) (*StepResult, error) {
			return &StepResult{Output: map[string]any{"key": "value"}}, nil
		},
	}
	dataPipeline := &Pipeline{
		Name:  "greet-pipeline",
		Steps: []PipelineStep{dataStep},
	}

	mod.SetPipelineLookup(func(name string) (*Pipeline, bool) {
		if name == "greet-pipeline" {
			return dataPipeline, true
		}
		return nil, false
	})

	router := &testRouter{}
	mod.RegisterRoutes(router)

	h := router.findHandler("GET", "/api/greet")
	if h == nil {
		t.Fatal("GET /api/greet handler not found")
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/greet", nil)
	h.Handle(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 fallback, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("expected JSON fallback body, got error: %v", err)
	}
	if resp["key"] != "value" {
		t.Errorf("expected key=value in fallback body, got %v", resp)
	}
}

// TestOpenAPIModule_XPipeline_ResponseStatus_Float64 verifies that response_status
// emitted as float64 (common after JSON round-trip) is correctly coerced.
func TestOpenAPIModule_XPipeline_ResponseStatus_Float64(t *testing.T) {
	specPath := writeTempSpec(t, ".yaml", xPipelineYAML)

	mod := NewOpenAPIModule("pipe-api", OpenAPIConfig{
		SpecFile: specPath,
		BasePath: "/api",
	})
	if err := mod.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	step := &stubPipelineStep{
		name: "float-status",
		exec: func(_ context.Context, _ *PipelineContext) (*StepResult, error) {
			return &StepResult{
				Output: map[string]any{
					"response_status": float64(422),
					"response_body":   `{"error":"unprocessable"}`,
					"response_headers": map[string]string{
						"Content-Type": "application/json",
					},
				},
				Stop: true,
			}, nil
		},
	}
	pipe := &Pipeline{Name: "greet-pipeline", Steps: []PipelineStep{step}}
	mod.SetPipelineLookup(func(name string) (*Pipeline, bool) {
		if name == "greet-pipeline" {
			return pipe, true
		}
		return nil, false
	})

	router := &testRouter{}
	mod.RegisterRoutes(router)

	h := router.findHandler("GET", "/api/greet")
	if h == nil {
		t.Fatal("handler not found")
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/greet", nil)
	h.Handle(w, r)

	if w.Code != 422 {
		t.Errorf("expected 422 from float64 status, got %d: %s", w.Code, w.Body.String())
	}
	if w.Body.String() != `{"error":"unprocessable"}` {
		t.Errorf("unexpected body: %q", w.Body.String())
	}
}

// ---- Response validation spec fixtures ----

const responseValidationYAML = `
openapi: "3.0.0"
info:
  title: Response Validation API
  version: "1.0.0"
paths:
  /pets:
    get:
      operationId: listPets
      summary: List all pets
      x-pipeline: list-pets-pipeline
      responses:
        "200":
          description: A list of pets
          content:
            application/json:
              schema:
                type: array
                items:
                  type: object
                  required:
                    - id
                    - name
                  properties:
                    id:
                      type: integer
                    name:
                      type: string
                      minLength: 1
                    tag:
                      type: string
    post:
      operationId: createPet
      summary: Create a pet
      x-pipeline: create-pet-pipeline
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
      responses:
        "201":
          description: Created pet
          content:
            application/json:
              schema:
                type: object
                required:
                  - id
                  - name
                properties:
                  id:
                    type: integer
                  name:
                    type: string
  /pets/{petId}:
    get:
      operationId: getPet
      summary: Get a pet by ID
      x-pipeline: get-pet-pipeline
      parameters:
        - name: petId
          in: path
          required: true
          schema:
            type: integer
      responses:
        "200":
          description: A single pet
          content:
            application/json:
              schema:
                type: object
                required:
                  - id
                  - name
                properties:
                  id:
                    type: integer
                  name:
                    type: string
                  tag:
                    type: string
        "404":
          description: Pet not found
          content:
            application/json:
              schema:
                type: object
                required:
                  - error
                properties:
                  error:
                    type: string
  /no-response-schema:
    get:
      operationId: noSchema
      summary: Endpoint with no response schema
      x-pipeline: no-schema-pipeline
      responses:
        "200":
          description: No content schema defined
`

// JSON:API style response spec for complex validation scenarios
const jsonAPIResponseYAML = `
openapi: "3.0.0"
info:
  title: JSON:API Response Validation
  version: "1.0.0"
paths:
  /articles:
    get:
      operationId: listArticles
      summary: List articles (JSON:API format)
      x-pipeline: list-articles-pipeline
      responses:
        "200":
          description: JSON:API compliant response
          content:
            application/vnd.api+json:
              schema:
                type: object
                required:
                  - data
                properties:
                  data:
                    type: array
                    items:
                      type: object
                      required:
                        - type
                        - id
                        - attributes
                      properties:
                        type:
                          type: string
                        id:
                          type: string
                        attributes:
                          type: object
                          required:
                            - title
                          properties:
                            title:
                              type: string
                            body:
                              type: string
                        relationships:
                          type: object
                          properties:
                            author:
                              type: object
                              properties:
                                data:
                                  type: object
                                  required:
                                    - type
                                    - id
                                  properties:
                                    type:
                                      type: string
                                    id:
                                      type: string
                  meta:
                    type: object
                    properties:
                      total:
                        type: integer
                  links:
                    type: object
                    properties:
                      self:
                        type: string
                      next:
                        type: string
`

// ---- Response validation tests ----

func TestOpenAPIModule_ResponseValidation_ValidResponse(t *testing.T) {
	specPath := writeTempSpec(t, ".yaml", responseValidationYAML)

	mod := NewOpenAPIModule("resp-api", OpenAPIConfig{
		SpecFile:   specPath,
		BasePath:   "/api",
		Validation: OpenAPIValidationConfig{Response: true, ResponseAction: "error"},
	})
	if err := mod.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Pipeline returns a valid array of pets
	step := &stubPipelineStep{
		name: "list-pets",
		exec: func(_ context.Context, _ *PipelineContext) (*StepResult, error) {
			return &StepResult{
				Output: map[string]any{
					"response_status": 200,
					"response_body":   `[{"id":1,"name":"Fido","tag":"dog"},{"id":2,"name":"Whiskers"}]`,
					"response_headers": map[string]any{
						"Content-Type": "application/json",
					},
				},
				Stop: true,
			}, nil
		},
	}
	pipe := &Pipeline{Name: "list-pets-pipeline", Steps: []PipelineStep{step}}
	mod.SetPipelineLookup(func(name string) (*Pipeline, bool) {
		if name == "list-pets-pipeline" {
			return pipe, true
		}
		return nil, false
	})

	router := &testRouter{}
	mod.RegisterRoutes(router)

	h := router.findHandler("GET", "/api/pets")
	if h == nil {
		t.Fatal("GET /api/pets handler not found")
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/pets", nil)
	h.Handle(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestOpenAPIModule_ResponseValidation_InvalidResponse_ErrorAction(t *testing.T) {
	specPath := writeTempSpec(t, ".yaml", responseValidationYAML)

	mod := NewOpenAPIModule("resp-api", OpenAPIConfig{
		SpecFile:   specPath,
		BasePath:   "/api",
		Validation: OpenAPIValidationConfig{Response: true, ResponseAction: "error"},
	})
	if err := mod.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Pipeline returns a pet missing the required "name" field
	step := &stubPipelineStep{
		name: "list-pets",
		exec: func(_ context.Context, _ *PipelineContext) (*StepResult, error) {
			return &StepResult{
				Output: map[string]any{
					"response_status": 200,
					"response_body":   `[{"id":1}]`,
					"response_headers": map[string]any{
						"Content-Type": "application/json",
					},
				},
				Stop: true,
			}, nil
		},
	}
	pipe := &Pipeline{Name: "list-pets-pipeline", Steps: []PipelineStep{step}}
	mod.SetPipelineLookup(func(name string) (*Pipeline, bool) {
		if name == "list-pets-pipeline" {
			return pipe, true
		}
		return nil, false
	})

	router := &testRouter{}
	mod.RegisterRoutes(router)

	h := router.findHandler("GET", "/api/pets")
	if h == nil {
		t.Fatal("GET /api/pets handler not found")
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/pets", nil)
	h.Handle(w, r)

	// With action=error, we expect a 500 response with validation errors
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("expected JSON error body: %v", err)
	}
	if resp["error"] != "response validation failed" {
		t.Errorf("expected 'response validation failed' error, got %v", resp["error"])
	}
	errs, ok := resp["errors"].([]any)
	if !ok || len(errs) == 0 {
		t.Errorf("expected validation errors, got %v", resp["errors"])
	}
}

func TestOpenAPIModule_ResponseValidation_InvalidResponse_WarnAction(t *testing.T) {
	specPath := writeTempSpec(t, ".yaml", responseValidationYAML)

	mod := NewOpenAPIModule("resp-api", OpenAPIConfig{
		SpecFile:   specPath,
		BasePath:   "/api",
		Validation: OpenAPIValidationConfig{Response: true, ResponseAction: "warn"},
	})
	if err := mod.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Pipeline returns a pet missing the required "name" field
	step := &stubPipelineStep{
		name: "list-pets",
		exec: func(_ context.Context, _ *PipelineContext) (*StepResult, error) {
			return &StepResult{
				Output: map[string]any{
					"response_status": 200,
					"response_body":   `[{"id":1}]`,
					"response_headers": map[string]any{
						"Content-Type": "application/json",
					},
				},
				Stop: true,
			}, nil
		},
	}
	pipe := &Pipeline{Name: "list-pets-pipeline", Steps: []PipelineStep{step}}
	mod.SetPipelineLookup(func(name string) (*Pipeline, bool) {
		if name == "list-pets-pipeline" {
			return pipe, true
		}
		return nil, false
	})

	router := &testRouter{}
	mod.RegisterRoutes(router)

	h := router.findHandler("GET", "/api/pets")
	if h == nil {
		t.Fatal("GET /api/pets handler not found")
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/pets", nil)
	h.Handle(w, r)

	// With action=warn, the response should still be sent (200)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 (warning only), got %d: %s", w.Code, w.Body.String())
	}
	// Body should be the original pipeline body
	if w.Body.String() != `[{"id":1}]` {
		t.Errorf("expected original pipeline body, got %q", w.Body.String())
	}
}

func TestOpenAPIModule_ResponseValidation_DefaultFallback_InvalidFallback(t *testing.T) {
	specPath := writeTempSpec(t, ".yaml", responseValidationYAML)

	mod := NewOpenAPIModule("resp-api", OpenAPIConfig{
		SpecFile:   specPath,
		BasePath:   "/api",
		Validation: OpenAPIValidationConfig{Response: true, ResponseAction: "error"},
	})
	if err := mod.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Pipeline returns output without response_status — falls through to 200 default
	step := &stubPipelineStep{
		name: "list-pets",
		exec: func(_ context.Context, _ *PipelineContext) (*StepResult, error) {
			return &StepResult{
				Output: map[string]any{
					"result": []any{
						map[string]any{"id": float64(1), "name": "Fido"},
					},
				},
			}, nil
		},
	}
	pipe := &Pipeline{Name: "list-pets-pipeline", Steps: []PipelineStep{step}}
	mod.SetPipelineLookup(func(name string) (*Pipeline, bool) {
		if name == "list-pets-pipeline" {
			return pipe, true
		}
		return nil, false
	})

	router := &testRouter{}
	mod.RegisterRoutes(router)

	h := router.findHandler("GET", "/api/pets")
	if h == nil {
		t.Fatal("GET /api/pets handler not found")
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/pets", nil)
	h.Handle(w, r)

	// The spec expects an array at the top level, but we're sending an object
	// (the full pipeline state). This should fail validation in error mode.
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 (response is object, spec expects array), got %d: %s", w.Code, w.Body.String())
	}
}

func TestOpenAPIModule_ResponseValidation_NoSchema_Passes(t *testing.T) {
	specPath := writeTempSpec(t, ".yaml", responseValidationYAML)

	mod := NewOpenAPIModule("resp-api", OpenAPIConfig{
		SpecFile:   specPath,
		BasePath:   "/api",
		Validation: OpenAPIValidationConfig{Response: true, ResponseAction: "error"},
	})
	if err := mod.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	step := &stubPipelineStep{
		name: "no-schema",
		exec: func(_ context.Context, _ *PipelineContext) (*StepResult, error) {
			return &StepResult{
				Output: map[string]any{
					"response_status": 200,
					"response_body":   `{"anything":"goes"}`,
					"response_headers": map[string]any{
						"Content-Type": "application/json",
					},
				},
				Stop: true,
			}, nil
		},
	}
	pipe := &Pipeline{Name: "no-schema-pipeline", Steps: []PipelineStep{step}}
	mod.SetPipelineLookup(func(name string) (*Pipeline, bool) {
		if name == "no-schema-pipeline" {
			return pipe, true
		}
		return nil, false
	})

	router := &testRouter{}
	mod.RegisterRoutes(router)

	h := router.findHandler("GET", "/api/no-response-schema")
	if h == nil {
		t.Fatal("GET /api/no-response-schema handler not found")
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/no-response-schema", nil)
	h.Handle(w, r)

	// No schema defined — response should pass through
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 (no schema to validate against), got %d: %s", w.Code, w.Body.String())
	}
}

func TestOpenAPIModule_ResponseValidation_JSONAPI_ValidResponse(t *testing.T) {
	specPath := writeTempSpec(t, ".yaml", jsonAPIResponseYAML)

	mod := NewOpenAPIModule("jsonapi", OpenAPIConfig{
		SpecFile:   specPath,
		BasePath:   "/api",
		Validation: OpenAPIValidationConfig{Response: true, ResponseAction: "error"},
	})
	if err := mod.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// A valid JSON:API response
	validBody := `{
		"data": [
			{
				"type": "articles",
				"id": "1",
				"attributes": {
					"title": "Hello World",
					"body": "This is my first article."
				},
				"relationships": {
					"author": {
						"data": {"type": "people", "id": "42"}
					}
				}
			}
		],
		"meta": {"total": 1},
		"links": {"self": "/articles"}
	}`

	step := &stubPipelineStep{
		name: "list-articles",
		exec: func(_ context.Context, _ *PipelineContext) (*StepResult, error) {
			return &StepResult{
				Output: map[string]any{
					"response_status": 200,
					"response_body":   validBody,
					"response_headers": map[string]any{
						"Content-Type": "application/vnd.api+json",
					},
				},
				Stop: true,
			}, nil
		},
	}
	pipe := &Pipeline{Name: "list-articles-pipeline", Steps: []PipelineStep{step}}
	mod.SetPipelineLookup(func(name string) (*Pipeline, bool) {
		if name == "list-articles-pipeline" {
			return pipe, true
		}
		return nil, false
	})

	router := &testRouter{}
	mod.RegisterRoutes(router)

	h := router.findHandler("GET", "/api/articles")
	if h == nil {
		t.Fatal("GET /api/articles handler not found")
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/articles", nil)
	h.Handle(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for valid JSON:API response, got %d: %s", w.Code, w.Body.String())
	}
}

func TestOpenAPIModule_ResponseValidation_JSONAPI_InvalidResponse(t *testing.T) {
	specPath := writeTempSpec(t, ".yaml", jsonAPIResponseYAML)

	mod := NewOpenAPIModule("jsonapi", OpenAPIConfig{
		SpecFile:   specPath,
		BasePath:   "/api",
		Validation: OpenAPIValidationConfig{Response: true, ResponseAction: "error"},
	})
	if err := mod.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Invalid JSON:API response — missing required "type" and "attributes" in data items
	invalidBody := `{
		"data": [
			{
				"id": "1"
			}
		]
	}`

	step := &stubPipelineStep{
		name: "list-articles",
		exec: func(_ context.Context, _ *PipelineContext) (*StepResult, error) {
			return &StepResult{
				Output: map[string]any{
					"response_status": 200,
					"response_body":   invalidBody,
					"response_headers": map[string]any{
						"Content-Type": "application/vnd.api+json",
					},
				},
				Stop: true,
			}, nil
		},
	}
	pipe := &Pipeline{Name: "list-articles-pipeline", Steps: []PipelineStep{step}}
	mod.SetPipelineLookup(func(name string) (*Pipeline, bool) {
		if name == "list-articles-pipeline" {
			return pipe, true
		}
		return nil, false
	})

	router := &testRouter{}
	mod.RegisterRoutes(router)

	h := router.findHandler("GET", "/api/articles")
	if h == nil {
		t.Fatal("GET /api/articles handler not found")
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/articles", nil)
	h.Handle(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for invalid JSON:API response, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("expected JSON error body: %v", err)
	}
	errs, ok := resp["errors"].([]any)
	if !ok || len(errs) == 0 {
		t.Fatalf("expected validation errors, got %v", resp["errors"])
	}

	// Check that it caught missing required fields
	errStr := strings.Join(func() []string {
		ss := make([]string, len(errs))
		for i, e := range errs {
			ss[i] = e.(string)
		}
		return ss
	}(), " ")
	if !strings.Contains(errStr, "type") {
		t.Errorf("expected error about missing 'type' field, got: %s", errStr)
	}
	if !strings.Contains(errStr, "attributes") {
		t.Errorf("expected error about missing 'attributes' field, got: %s", errStr)
	}
}

func TestOpenAPIModule_ResponseValidation_JSONAPI_WrongContentType(t *testing.T) {
	specPath := writeTempSpec(t, ".yaml", jsonAPIResponseYAML)

	mod := NewOpenAPIModule("jsonapi", OpenAPIConfig{
		SpecFile:   specPath,
		BasePath:   "/api",
		Validation: OpenAPIValidationConfig{Response: true, ResponseAction: "error"},
	})
	if err := mod.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Response with wrong content type (application/json instead of application/vnd.api+json)
	step := &stubPipelineStep{
		name: "list-articles",
		exec: func(_ context.Context, _ *PipelineContext) (*StepResult, error) {
			return &StepResult{
				Output: map[string]any{
					"response_status": 200,
					"response_body":   `{"data":[]}`,
					"response_headers": map[string]any{
						"Content-Type": "application/json",
					},
				},
				Stop: true,
			}, nil
		},
	}
	pipe := &Pipeline{Name: "list-articles-pipeline", Steps: []PipelineStep{step}}
	mod.SetPipelineLookup(func(name string) (*Pipeline, bool) {
		if name == "list-articles-pipeline" {
			return pipe, true
		}
		return nil, false
	})

	router := &testRouter{}
	mod.RegisterRoutes(router)

	h := router.findHandler("GET", "/api/articles")
	if h == nil {
		t.Fatal("GET /api/articles handler not found")
	}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/articles", nil)
	h.Handle(w, r)

	// Should fail because the Content-Type doesn't match the spec
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for wrong content type, got %d: %s", w.Code, w.Body.String())
	}
}

func TestOpenAPIModule_ResponseValidation_DirectWrite(t *testing.T) {
	specPath := writeTempSpec(t, ".yaml", responseValidationYAML)

	mod := NewOpenAPIModule("resp-api", OpenAPIConfig{
		SpecFile:   specPath,
		BasePath:   "/api",
		Validation: OpenAPIValidationConfig{Response: true, ResponseAction: "error"},
	})
	if err := mod.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Pipeline step writes directly to the response writer with an invalid response
	step := &stubPipelineStep{
		name: "create-pet",
		exec: func(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
			rw := ctx.Value(HTTPResponseWriterContextKey).(http.ResponseWriter)
			rw.Header().Set("Content-Type", "application/json")
			rw.WriteHeader(http.StatusCreated)
			_, _ = rw.Write([]byte(`{"wrong":"fields"}`))
			return &StepResult{Output: map[string]any{}}, nil
		},
	}
	pipe := &Pipeline{Name: "create-pet-pipeline", Steps: []PipelineStep{step}}
	mod.SetPipelineLookup(func(name string) (*Pipeline, bool) {
		if name == "create-pet-pipeline" {
			return pipe, true
		}
		return nil, false
	})

	router := &testRouter{}
	mod.RegisterRoutes(router)

	h := router.findHandler("POST", "/api/pets")
	if h == nil {
		t.Fatal("POST /api/pets handler not found")
	}

	w := httptest.NewRecorder()
	body := strings.NewReader(`{"name":"Fido"}`)
	r := httptest.NewRequest(http.MethodPost, "/api/pets", body)
	r.Header.Set("Content-Type", "application/json")
	h.Handle(w, r)

	// With error action, the invalid response should be blocked
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for invalid direct-write response, got %d: %s", w.Code, w.Body.String())
	}
}

func TestOpenAPIModule_ResponseValidation_DirectWrite_Valid(t *testing.T) {
	specPath := writeTempSpec(t, ".yaml", responseValidationYAML)

	mod := NewOpenAPIModule("resp-api", OpenAPIConfig{
		SpecFile:   specPath,
		BasePath:   "/api",
		Validation: OpenAPIValidationConfig{Response: true, ResponseAction: "error"},
	})
	if err := mod.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Pipeline step writes a valid response directly
	step := &stubPipelineStep{
		name: "create-pet",
		exec: func(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
			rw := ctx.Value(HTTPResponseWriterContextKey).(http.ResponseWriter)
			rw.Header().Set("Content-Type", "application/json")
			rw.WriteHeader(http.StatusCreated)
			_, _ = rw.Write([]byte(`{"id":1,"name":"Fido"}`))
			return &StepResult{Output: map[string]any{}}, nil
		},
	}
	pipe := &Pipeline{Name: "create-pet-pipeline", Steps: []PipelineStep{step}}
	mod.SetPipelineLookup(func(name string) (*Pipeline, bool) {
		if name == "create-pet-pipeline" {
			return pipe, true
		}
		return nil, false
	})

	router := &testRouter{}
	mod.RegisterRoutes(router)

	h := router.findHandler("POST", "/api/pets")
	if h == nil {
		t.Fatal("POST /api/pets handler not found")
	}

	w := httptest.NewRecorder()
	body := strings.NewReader(`{"name":"Fido"}`)
	r := httptest.NewRequest(http.MethodPost, "/api/pets", body)
	r.Header.Set("Content-Type", "application/json")
	h.Handle(w, r)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201 for valid response, got %d: %s", w.Code, w.Body.String())
	}
}

func TestOpenAPIModule_ResponseValidation_ArrayConstraints(t *testing.T) {
	const arrayConstraintYAML = `
openapi: "3.0.0"
info:
  title: Array Constraint API
  version: "1.0.0"
paths:
  /items:
    get:
      operationId: listItems
      x-pipeline: list-items
      responses:
        "200":
          description: Items list
          content:
            application/json:
              schema:
                type: array
                minItems: 1
                maxItems: 3
                items:
                  type: object
                  required:
                    - name
                  properties:
                    name:
                      type: string
`
	specPath := writeTempSpec(t, ".yaml", arrayConstraintYAML)

	t.Run("too_few_items", func(t *testing.T) {
		mod := NewOpenAPIModule("arr-api", OpenAPIConfig{
			SpecFile:   specPath,
			BasePath:   "/api",
			Validation: OpenAPIValidationConfig{Response: true, ResponseAction: "error"},
		})
		if err := mod.Init(nil); err != nil {
			t.Fatalf("Init: %v", err)
		}
		step := &stubPipelineStep{
			name: "list-items",
			exec: func(_ context.Context, _ *PipelineContext) (*StepResult, error) {
				return &StepResult{
					Output: map[string]any{
						"response_status":  200,
						"response_body":    `[]`,
						"response_headers": map[string]any{"Content-Type": "application/json"},
					},
					Stop: true,
				}, nil
			},
		}
		pipe := &Pipeline{Name: "list-items", Steps: []PipelineStep{step}}
		mod.SetPipelineLookup(func(name string) (*Pipeline, bool) {
			if name == "list-items" {
				return pipe, true
			}
			return nil, false
		})
		router := &testRouter{}
		mod.RegisterRoutes(router)
		h := router.findHandler("GET", "/api/items")
		if h == nil {
			t.Fatal("route GET /api/items not registered")
		}
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/items", nil)
		h.Handle(w, r)
		if w.Code != http.StatusInternalServerError {
			t.Errorf("expected 500 for too few items, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("too_many_items", func(t *testing.T) {
		mod := NewOpenAPIModule("arr-api2", OpenAPIConfig{
			SpecFile:   specPath,
			BasePath:   "/api",
			Validation: OpenAPIValidationConfig{Response: true, ResponseAction: "error"},
		})
		if err := mod.Init(nil); err != nil {
			t.Fatalf("Init: %v", err)
		}
		step := &stubPipelineStep{
			name: "list-items",
			exec: func(_ context.Context, _ *PipelineContext) (*StepResult, error) {
				return &StepResult{
					Output: map[string]any{
						"response_status":  200,
						"response_body":    `[{"name":"a"},{"name":"b"},{"name":"c"},{"name":"d"}]`,
						"response_headers": map[string]any{"Content-Type": "application/json"},
					},
					Stop: true,
				}, nil
			},
		}
		pipe := &Pipeline{Name: "list-items", Steps: []PipelineStep{step}}
		mod.SetPipelineLookup(func(name string) (*Pipeline, bool) {
			if name == "list-items" {
				return pipe, true
			}
			return nil, false
		})
		router := &testRouter{}
		mod.RegisterRoutes(router)
		h := router.findHandler("GET", "/api/items")
		if h == nil {
			t.Fatal("route GET /api/items not registered")
		}
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/items", nil)
		h.Handle(w, r)
		if w.Code != http.StatusInternalServerError {
			t.Errorf("expected 500 for too many items, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("valid_array", func(t *testing.T) {
		mod := NewOpenAPIModule("arr-api3", OpenAPIConfig{
			SpecFile:   specPath,
			BasePath:   "/api",
			Validation: OpenAPIValidationConfig{Response: true, ResponseAction: "error"},
		})
		if err := mod.Init(nil); err != nil {
			t.Fatalf("Init: %v", err)
		}
		step := &stubPipelineStep{
			name: "list-items",
			exec: func(_ context.Context, _ *PipelineContext) (*StepResult, error) {
				return &StepResult{
					Output: map[string]any{
						"response_status":  200,
						"response_body":    `[{"name":"a"},{"name":"b"}]`,
						"response_headers": map[string]any{"Content-Type": "application/json"},
					},
					Stop: true,
				}, nil
			},
		}
		pipe := &Pipeline{Name: "list-items", Steps: []PipelineStep{step}}
		mod.SetPipelineLookup(func(name string) (*Pipeline, bool) {
			if name == "list-items" {
				return pipe, true
			}
			return nil, false
		})
		router := &testRouter{}
		mod.RegisterRoutes(router)
		h := router.findHandler("GET", "/api/items")
		if h == nil {
			t.Fatal("route GET /api/items not registered")
		}
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/api/items", nil)
		h.Handle(w, r)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200 for valid array, got %d: %s", w.Code, w.Body.String())
		}
	})
}

func TestOpenAPIModule_ResponseValidation_DefaultAction_IsWarn(t *testing.T) {
	specPath := writeTempSpec(t, ".yaml", responseValidationYAML)

	// No ResponseAction specified — should default to "warn"
	mod := NewOpenAPIModule("resp-api", OpenAPIConfig{
		SpecFile:   specPath,
		BasePath:   "/api",
		Validation: OpenAPIValidationConfig{Response: true},
	})
	if err := mod.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}

	step := &stubPipelineStep{
		name: "list-pets",
		exec: func(_ context.Context, _ *PipelineContext) (*StepResult, error) {
			return &StepResult{
				Output: map[string]any{
					"response_status":  200,
					"response_body":    `[{"id":1}]`, // missing required "name"
					"response_headers": map[string]any{"Content-Type": "application/json"},
				},
				Stop: true,
			}, nil
		},
	}
	pipe := &Pipeline{Name: "list-pets-pipeline", Steps: []PipelineStep{step}}
	mod.SetPipelineLookup(func(name string) (*Pipeline, bool) {
		if name == "list-pets-pipeline" {
			return pipe, true
		}
		return nil, false
	})

	router := &testRouter{}
	mod.RegisterRoutes(router)

	h := router.findHandler("GET", "/api/pets")
	if h == nil {
		t.Fatal("handler for GET /api/pets not found")
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/pets", nil)
	h.Handle(w, r)

	// Default action is warn, so response should pass through
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 with default warn action, got %d: %s", w.Code, w.Body.String())
	}
}

// ---- additionalProperties tests ----

// additionalPropertiesTrueYAML exercises additionalProperties: true (bool shorthand).
const additionalPropertiesTrueYAML = `
openapi: "3.0.0"
info:
  title: AdditionalProperties True
  version: "1.0.0"
paths:
  /metadata:
    post:
      operationId: postMetadata
      summary: Post metadata with any extra keys
      x-pipeline: metadata-pipeline
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              properties:
                name:
                  type: string
              additionalProperties: true
      responses:
        "200":
          description: OK
`

// additionalPropertiesFalseYAML exercises additionalProperties: false (reject extras).
const additionalPropertiesFalseYAML = `
openapi: "3.0.0"
info:
  title: AdditionalProperties False
  version: "1.0.0"
paths:
  /strict:
    post:
      operationId: postStrict
      summary: Post object with no extra keys allowed
      x-pipeline: strict-pipeline
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              properties:
                name:
                  type: string
              additionalProperties: false
      responses:
        "200":
          description: OK
`

// additionalPropertiesSchemaYAML exercises additionalProperties: <schema>.
const additionalPropertiesSchemaYAML = `
openapi: "3.0.0"
info:
  title: AdditionalProperties Schema
  version: "1.0.0"
paths:
  /typed-extra:
    post:
      operationId: postTypedExtra
      summary: Post object where extra keys must be strings
      x-pipeline: typed-extra-pipeline
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              properties:
                name:
                  type: string
              additionalProperties:
                type: string
      responses:
        "200":
          description: OK
`

// TestOpenAPIModule_AdditionalProperties_True verifies that a spec with
// "additionalProperties: true" is parsed without error and that arbitrary
// extra keys in the request body are accepted.
func TestOpenAPIModule_AdditionalProperties_True(t *testing.T) {
	specPath := writeTempSpec(t, ".yaml", additionalPropertiesTrueYAML)

	mod := NewOpenAPIModule("ap-true", OpenAPIConfig{
		SpecFile:   specPath,
		Validation: OpenAPIValidationConfig{Request: true},
	})
	if err := mod.Init(nil); err != nil {
		t.Fatalf("Init with additionalProperties:true should succeed, got: %v", err)
	}

	step := &stubPipelineStep{
		name: "metadata",
		exec: func(_ context.Context, _ *PipelineContext) (*StepResult, error) {
			return &StepResult{Output: map[string]any{"response_status": 200}, Stop: true}, nil
		},
	}
	pipe := &Pipeline{Name: "metadata-pipeline", Steps: []PipelineStep{step}}
	mod.SetPipelineLookup(func(name string) (*Pipeline, bool) {
		if name == "metadata-pipeline" {
			return pipe, true
		}
		return nil, false
	})

	router := &testRouter{}
	mod.RegisterRoutes(router)

	h := router.findHandler("POST", "/metadata")
	if h == nil {
		t.Fatal("POST /metadata handler not found")
	}

	// Body contains declared key "name" plus extra keys — all should pass
	body := `{"name":"test","extra_field":"value","another_key":42}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/metadata", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	h.Handle(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected extra keys to pass with additionalProperties:true, got status %d: %s", w.Code, w.Body.String())
	}
}

// TestOpenAPIModule_AdditionalProperties_False verifies that extra keys are
// rejected when "additionalProperties: false" is set.
func TestOpenAPIModule_AdditionalProperties_False(t *testing.T) {
	specPath := writeTempSpec(t, ".yaml", additionalPropertiesFalseYAML)

	mod := NewOpenAPIModule("ap-false", OpenAPIConfig{
		SpecFile:   specPath,
		Validation: OpenAPIValidationConfig{Request: true},
	})
	if err := mod.Init(nil); err != nil {
		t.Fatalf("Init with additionalProperties:false should succeed, got: %v", err)
	}

	step := &stubPipelineStep{
		name: "strict",
		exec: func(_ context.Context, _ *PipelineContext) (*StepResult, error) {
			return &StepResult{Output: map[string]any{"response_status": 200}, Stop: true}, nil
		},
	}
	pipe := &Pipeline{Name: "strict-pipeline", Steps: []PipelineStep{step}}
	mod.SetPipelineLookup(func(name string) (*Pipeline, bool) {
		if name == "strict-pipeline" {
			return pipe, true
		}
		return nil, false
	})

	router := &testRouter{}
	mod.RegisterRoutes(router)

	h := router.findHandler("POST", "/strict")
	if h == nil {
		t.Fatal("POST /strict handler not found")
	}

	// Body contains extra key "unknown" which should be rejected
	body := `{"name":"test","unknown":"value"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/strict", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	h.Handle(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for extra key with additionalProperties:false, got %d: %s", w.Code, w.Body.String())
	}
}

// TestOpenAPIModule_AdditionalProperties_Schema verifies that extra keys are
// validated against the schema when "additionalProperties: {type: string}" is set.
func TestOpenAPIModule_AdditionalProperties_Schema(t *testing.T) {
	specPath := writeTempSpec(t, ".yaml", additionalPropertiesSchemaYAML)

	mod := NewOpenAPIModule("ap-schema", OpenAPIConfig{
		SpecFile:   specPath,
		Validation: OpenAPIValidationConfig{Request: true},
	})
	if err := mod.Init(nil); err != nil {
		t.Fatalf("Init with additionalProperties schema should succeed, got: %v", err)
	}

	step := &stubPipelineStep{
		name: "typed-extra",
		exec: func(_ context.Context, _ *PipelineContext) (*StepResult, error) {
			return &StepResult{Output: map[string]any{"response_status": 200}, Stop: true}, nil
		},
	}
	pipe := &Pipeline{Name: "typed-extra-pipeline", Steps: []PipelineStep{step}}
	mod.SetPipelineLookup(func(name string) (*Pipeline, bool) {
		if name == "typed-extra-pipeline" {
			return pipe, true
		}
		return nil, false
	})

	router := &testRouter{}
	mod.RegisterRoutes(router)

	h := router.findHandler("POST", "/typed-extra")
	if h == nil {
		t.Fatal("POST /typed-extra handler not found")
	}

	// Valid: extra key is a string
	body := `{"name":"test","extra":"string-value"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/typed-extra", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	h.Handle(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected string extra key to pass additionalProperties:{type:string}, got status %d: %s", w.Code, w.Body.String())
	}

	// Invalid: extra key value is an integer, not a string — should be rejected
	bodyInvalid := `{"name":"test","extra":42}`
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest(http.MethodPost, "/typed-extra", strings.NewReader(bodyInvalid))
	r2.Header.Set("Content-Type", "application/json")
	h.Handle(w2, r2)

	if w2.Code != http.StatusBadRequest {
		t.Errorf("expected integer extra key to fail additionalProperties:{type:string}, got status %d: %s", w2.Code, w2.Body.String())
	}
}

// TestOpenAPIAdditionalProperties_UnmarshalYAML checks that the custom YAML
// unmarshaller handles bool and object forms correctly.
func TestOpenAPIAdditionalProperties_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		input      string
		wantBool   bool
		wantSchema bool   // whether Schema should be non-nil
		wantType   string // expected Schema.Type when wantSchema is true
	}{
		{"true", true, false, ""},
		{"false", false, false, ""},
		{"type: string", false, true, "string"},
		{"type: integer", false, true, "integer"},
	}

	for _, tc := range tests {
		var ap openAPIAdditionalProperties
		if err := yaml.Unmarshal([]byte(tc.input), &ap); err != nil {
			t.Errorf("UnmarshalYAML(%q): unexpected error: %v", tc.input, err)
			continue
		}
		if ap.Bool != tc.wantBool {
			t.Errorf("UnmarshalYAML(%q): Bool = %v, want %v", tc.input, ap.Bool, tc.wantBool)
		}
		if (ap.Schema != nil) != tc.wantSchema {
			t.Errorf("UnmarshalYAML(%q): Schema non-nil = %v, want %v", tc.input, ap.Schema != nil, tc.wantSchema)
		}
		if tc.wantSchema && ap.Schema != nil && ap.Schema.Type != tc.wantType {
			t.Errorf("UnmarshalYAML(%q): Schema.Type = %q, want %q", tc.input, ap.Schema.Type, tc.wantType)
		}
	}
}

// TestOpenAPIAdditionalProperties_UnmarshalJSON checks the JSON unmarshaller.
func TestOpenAPIAdditionalProperties_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		input      string
		wantBool   bool
		wantSchema bool
		wantType   string
	}{
		{`true`, true, false, ""},
		{`false`, false, false, ""},
		{`{"type":"string"}`, false, true, "string"},
	}

	for _, tc := range tests {
		var ap openAPIAdditionalProperties
		if err := json.Unmarshal([]byte(tc.input), &ap); err != nil {
			t.Errorf("UnmarshalJSON(%q): unexpected error: %v", tc.input, err)
			continue
		}
		if ap.Bool != tc.wantBool {
			t.Errorf("UnmarshalJSON(%q): Bool = %v, want %v", tc.input, ap.Bool, tc.wantBool)
		}
		if (ap.Schema != nil) != tc.wantSchema {
			t.Errorf("UnmarshalJSON(%q): Schema non-nil = %v, want %v", tc.input, ap.Schema != nil, tc.wantSchema)
		}
		if tc.wantSchema && ap.Schema != nil && ap.Schema.Type != tc.wantType {
			t.Errorf("UnmarshalJSON(%q): Schema.Type = %q, want %q", tc.input, ap.Schema.Type, tc.wantType)
		}
	}
}
