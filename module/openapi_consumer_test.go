package module

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenAPIConsumerName(t *testing.T) {
	c := NewOpenAPIConsumer("test-consumer", OpenAPIConsumerConfig{})
	if c.Name() != "test-consumer" {
		t.Errorf("expected name 'test-consumer', got %q", c.Name())
	}
}

func TestOpenAPIConsumerProvidesServices(t *testing.T) {
	c := NewOpenAPIConsumer("my-consumer", OpenAPIConsumerConfig{})
	providers := c.ProvidesServices()
	if len(providers) != 1 {
		t.Fatalf("expected 1 service provider, got %d", len(providers))
	}
	if providers[0].Name != "my-consumer" {
		t.Errorf("expected provider name 'my-consumer', got %q", providers[0].Name)
	}
	if providers[0].Instance != c {
		t.Error("expected provider instance to be the consumer itself")
	}
}

func TestOpenAPIConsumerRequiresServices(t *testing.T) {
	c := NewOpenAPIConsumer("consumer", OpenAPIConsumerConfig{})
	deps := c.RequiresServices()
	if deps != nil {
		t.Errorf("expected nil dependencies, got %v", deps)
	}
}

func TestOpenAPIConsumerLoadFromFile(t *testing.T) {
	// Create a temp spec file
	spec := OpenAPISpec{
		OpenAPI: "3.0.3",
		Info:    OpenAPIInfo{Title: "File Test", Version: "1.0.0"},
		Paths: map[string]*OpenAPIPath{
			"/items": {
				Get: &OpenAPIOperation{
					Summary:     "List items",
					OperationID: "listItems",
					Responses:   map[string]*OpenAPIResponse{"200": {Description: "OK"}},
				},
			},
		},
	}

	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.json")
	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(specPath, data, 0644); err != nil {
		t.Fatal(err)
	}

	c := NewOpenAPIConsumer("consumer", OpenAPIConsumerConfig{SpecFile: specPath})
	if err := c.loadSpec(); err != nil {
		t.Fatalf("loadSpec failed: %v", err)
	}

	loaded := c.GetSpec()
	if loaded == nil {
		t.Fatal("expected spec to be loaded")
	}
	if loaded.Info.Title != "File Test" {
		t.Errorf("expected title 'File Test', got %q", loaded.Info.Title)
	}
	if len(loaded.Paths) != 1 {
		t.Errorf("expected 1 path, got %d", len(loaded.Paths))
	}
}

func TestOpenAPIConsumerLoadFromURL(t *testing.T) {
	spec := OpenAPISpec{
		OpenAPI: "3.0.3",
		Info:    OpenAPIInfo{Title: "URL Test", Version: "1.0.0"},
		Paths: map[string]*OpenAPIPath{
			"/health": {
				Get: &OpenAPIOperation{
					OperationID: "getHealth",
					Responses:   map[string]*OpenAPIResponse{"200": {Description: "OK"}},
				},
			},
		},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(spec)
	}))
	defer ts.Close()

	c := NewOpenAPIConsumer("consumer", OpenAPIConsumerConfig{SpecURL: ts.URL})
	if err := c.loadSpec(); err != nil {
		t.Fatalf("loadSpec from URL failed: %v", err)
	}

	loaded := c.GetSpec()
	if loaded == nil {
		t.Fatal("expected spec")
	}
	if loaded.Info.Title != "URL Test" {
		t.Errorf("expected title 'URL Test', got %q", loaded.Info.Title)
	}
}

func TestOpenAPIConsumerListOperations(t *testing.T) {
	spec := OpenAPISpec{
		OpenAPI: "3.0.3",
		Info:    OpenAPIInfo{Title: "Ops Test", Version: "1.0.0"},
		Paths: map[string]*OpenAPIPath{
			"/items": {
				Get: &OpenAPIOperation{
					OperationID: "listItems",
					Summary:     "List items",
					Tags:        []string{"items"},
				},
				Post: &OpenAPIOperation{
					OperationID: "createItem",
					Summary:     "Create item",
					Tags:        []string{"items"},
					RequestBody: &OpenAPIRequestBody{Required: true},
				},
			},
			"/items/{id}": {
				Delete: &OpenAPIOperation{
					OperationID: "deleteItem",
					Summary:     "Delete item",
				},
			},
		},
	}

	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.json")
	data, _ := json.MarshalIndent(spec, "", "  ")
	_ = os.WriteFile(specPath, data, 0644)

	c := NewOpenAPIConsumer("consumer", OpenAPIConsumerConfig{SpecFile: specPath})
	_ = c.loadSpec()

	ops := c.ListOperations()
	if len(ops) != 3 {
		t.Fatalf("expected 3 operations, got %d", len(ops))
	}

	opMap := make(map[string]ExternalOperation)
	for _, op := range ops {
		opMap[op.OperationID] = op
	}

	if op, ok := opMap["listItems"]; !ok {
		t.Error("expected listItems operation")
	} else if op.Method != "GET" {
		t.Errorf("expected GET, got %s", op.Method)
	}

	if op, ok := opMap["createItem"]; !ok {
		t.Error("expected createItem operation")
	} else if !op.HasBody {
		t.Error("expected createItem to have body")
	}

	if _, ok := opMap["deleteItem"]; !ok {
		t.Error("expected deleteItem operation")
	}
}

func TestOpenAPIConsumerCallOperation(t *testing.T) {
	// Mock external API
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/items":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []string{"a", "b", "c"},
			})
		case "/items/42":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":   "42",
				"name": "Test Item",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	spec := OpenAPISpec{
		OpenAPI: "3.0.3",
		Info:    OpenAPIInfo{Title: "Call Test", Version: "1.0.0"},
		Servers: []OpenAPIServer{{URL: ts.URL}},
		Paths: map[string]*OpenAPIPath{
			"/items": {
				Get: &OpenAPIOperation{
					OperationID: "listItems",
					Responses:   map[string]*OpenAPIResponse{"200": {Description: "OK"}},
				},
			},
			"/items/{id}": {
				Get: &OpenAPIOperation{
					OperationID: "getItem",
					Parameters: []OpenAPIParameter{
						{Name: "id", In: "path", Required: true, Schema: &OpenAPISchema{Type: "string"}},
					},
					Responses: map[string]*OpenAPIResponse{"200": {Description: "OK"}},
				},
			},
		},
	}

	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.json")
	data, _ := json.MarshalIndent(spec, "", "  ")
	_ = os.WriteFile(specPath, data, 0644)

	c := NewOpenAPIConsumer("consumer", OpenAPIConsumerConfig{SpecFile: specPath})
	_ = c.loadSpec()

	// Call listItems
	result, err := c.CallOperation(context.Background(), "listItems", nil)
	if err != nil {
		t.Fatalf("CallOperation failed: %v", err)
	}
	if result["statusCode"] != 200 {
		t.Errorf("expected status 200, got %v", result["statusCode"])
	}

	// Call getItem with path param
	result, err = c.CallOperation(context.Background(), "getItem", map[string]any{"id": "42"})
	if err != nil {
		t.Fatalf("CallOperation failed: %v", err)
	}
	if result["statusCode"] != 200 {
		t.Errorf("expected status 200, got %v", result["statusCode"])
	}
	body, ok := result["body"].(map[string]any)
	if !ok {
		t.Fatalf("expected body to be map, got %T", result["body"])
	}
	if body["id"] != "42" {
		t.Errorf("expected id '42', got %v", body["id"])
	}
}

func TestOpenAPIConsumerCallOperationNotFound(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.json")
	spec := OpenAPISpec{
		OpenAPI: "3.0.3",
		Info:    OpenAPIInfo{Title: "Test", Version: "1.0.0"},
		Paths:   map[string]*OpenAPIPath{},
	}
	data, _ := json.MarshalIndent(spec, "", "  ")
	_ = os.WriteFile(specPath, data, 0644)

	c := NewOpenAPIConsumer("consumer", OpenAPIConsumerConfig{SpecFile: specPath})
	_ = c.loadSpec()

	_, err := c.CallOperation(context.Background(), "nonexistent", nil)
	if err == nil {
		t.Error("expected error for nonexistent operation")
	}
}

func TestOpenAPIConsumerCallOperationMissingParam(t *testing.T) {
	spec := OpenAPISpec{
		OpenAPI: "3.0.3",
		Info:    OpenAPIInfo{Title: "Test", Version: "1.0.0"},
		Servers: []OpenAPIServer{{URL: "http://localhost"}},
		Paths: map[string]*OpenAPIPath{
			"/items/{id}": {
				Get: &OpenAPIOperation{
					OperationID: "getItem",
					Parameters: []OpenAPIParameter{
						{Name: "id", In: "path", Required: true},
					},
					Responses: map[string]*OpenAPIResponse{"200": {Description: "OK"}},
				},
			},
		},
	}

	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.json")
	data, _ := json.MarshalIndent(spec, "", "  ")
	_ = os.WriteFile(specPath, data, 0644)

	c := NewOpenAPIConsumer("consumer", OpenAPIConsumerConfig{SpecFile: specPath})
	_ = c.loadSpec()

	_, err := c.CallOperation(context.Background(), "getItem", map[string]any{})
	if err == nil {
		t.Error("expected error for missing path parameter")
	}
}

func TestOpenAPIConsumerFieldMapping(t *testing.T) {
	c := NewOpenAPIConsumer("consumer", OpenAPIConsumerConfig{})

	fm := NewFieldMapping()
	fm.Set("userId", "user_id", "uid")
	c.SetFieldMapping(fm)

	got := c.GetFieldMapping()
	if got == nil {
		t.Fatal("expected field mapping")
	}
	if got.Primary("userId") != "user_id" {
		t.Errorf("expected primary 'user_id', got %q", got.Primary("userId"))
	}
}

func TestOpenAPIConsumerAutoFieldMapping(t *testing.T) {
	spec := OpenAPISpec{
		OpenAPI: "3.0.3",
		Info:    OpenAPIInfo{Title: "Test", Version: "1.0.0"},
		Paths:   map[string]*OpenAPIPath{},
		Components: &OpenAPIComponents{
			Schemas: map[string]*OpenAPISchema{
				"User": {
					Type: "object",
					Properties: map[string]*OpenAPISchema{
						"id":    {Type: "string"},
						"email": {Type: "string"},
					},
				},
			},
		},
	}

	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.json")
	data, _ := json.MarshalIndent(spec, "", "  ")
	_ = os.WriteFile(specPath, data, 0644)

	c := NewOpenAPIConsumer("consumer", OpenAPIConsumerConfig{SpecFile: specPath})
	_ = c.loadSpec()

	fm := c.GetFieldMapping()
	if !fm.Has("User.id") {
		t.Error("expected auto-generated mapping for User.id")
	}
	if !fm.Has("User.email") {
		t.Error("expected auto-generated mapping for User.email")
	}
}

func TestOpenAPIConsumerServeOperations(t *testing.T) {
	spec := OpenAPISpec{
		OpenAPI: "3.0.3",
		Info:    OpenAPIInfo{Title: "Test", Version: "1.0.0"},
		Paths: map[string]*OpenAPIPath{
			"/test": {
				Get: &OpenAPIOperation{OperationID: "getTest", Summary: "Get test"},
			},
		},
	}

	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.json")
	data, _ := json.MarshalIndent(spec, "", "  ")
	_ = os.WriteFile(specPath, data, 0644)

	c := NewOpenAPIConsumer("consumer", OpenAPIConsumerConfig{SpecFile: specPath})
	_ = c.loadSpec()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/operations", nil)
	c.ServeOperations(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var ops []ExternalOperation
	if err := json.Unmarshal(w.Body.Bytes(), &ops); err != nil {
		t.Fatalf("failed to parse: %v", err)
	}
	if len(ops) != 1 {
		t.Errorf("expected 1 operation, got %d", len(ops))
	}
}

func TestOpenAPIConsumerNoConfig(t *testing.T) {
	c := NewOpenAPIConsumer("consumer", OpenAPIConsumerConfig{})
	err := c.loadSpec()
	if err == nil {
		t.Error("expected error when no specUrl or specFile provided")
	}
}

func TestOpenAPIConsumerInvalidFile(t *testing.T) {
	c := NewOpenAPIConsumer("consumer", OpenAPIConsumerConfig{SpecFile: "/nonexistent/path.json"})
	err := c.loadSpec()
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestOpenAPIConsumerInvalidSpec(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "bad.json")
	_ = os.WriteFile(specPath, []byte("not json or yaml{{"), 0644)

	c := NewOpenAPIConsumer("consumer", OpenAPIConsumerConfig{SpecFile: specPath})
	err := c.loadSpec()
	if err == nil {
		t.Error("expected error for invalid spec")
	}
}

func TestOpenAPIConsumerMissingVersion(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "noversion.json")
	_ = os.WriteFile(specPath, []byte(`{"info":{"title":"no version"}}`), 0644)

	c := NewOpenAPIConsumer("consumer", OpenAPIConsumerConfig{SpecFile: specPath})
	err := c.loadSpec()
	if err == nil {
		t.Error("expected error for missing openapi version")
	}
}

func TestOpenAPIConsumerNoSpecLoaded(t *testing.T) {
	c := NewOpenAPIConsumer("consumer", OpenAPIConsumerConfig{})

	if ops := c.ListOperations(); ops != nil {
		t.Error("expected nil operations when no spec loaded")
	}

	_, err := c.CallOperation(context.Background(), "op", nil)
	if err == nil {
		t.Error("expected error when no spec loaded")
	}
}

func TestOpenAPIConsumerServeSpec(t *testing.T) {
	spec := OpenAPISpec{
		OpenAPI: "3.0.3",
		Info:    OpenAPIInfo{Title: "Serve Spec Test", Version: "1.0.0"},
		Paths:   map[string]*OpenAPIPath{},
	}

	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.json")
	data, _ := json.MarshalIndent(spec, "", "  ")
	_ = os.WriteFile(specPath, data, 0644)

	c := NewOpenAPIConsumer("consumer", OpenAPIConsumerConfig{SpecFile: specPath})
	_ = c.loadSpec()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/spec", nil)
	c.ServeSpec(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var loaded OpenAPISpec
	if err := json.Unmarshal(w.Body.Bytes(), &loaded); err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if loaded.Info.Title != "Serve Spec Test" {
		t.Errorf("unexpected title: %q", loaded.Info.Title)
	}
}

func TestOpenAPIConsumerServeSpecNoLoad(t *testing.T) {
	c := NewOpenAPIConsumer("consumer", OpenAPIConsumerConfig{})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/spec", nil)
	c.ServeSpec(w, r)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestOpenAPIConsumerLoadYAML(t *testing.T) {
	yamlSpec := `openapi: "3.0.3"
info:
  title: YAML Spec
  version: "1.0.0"
paths:
  /yaml-test:
    get:
      operationId: yamlTest
      responses:
        "200":
          description: OK
`
	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.yaml")
	_ = os.WriteFile(specPath, []byte(yamlSpec), 0644)

	c := NewOpenAPIConsumer("consumer", OpenAPIConsumerConfig{SpecFile: specPath})
	if err := c.loadSpec(); err != nil {
		t.Fatalf("loadSpec from YAML failed: %v", err)
	}

	loaded := c.GetSpec()
	if loaded == nil {
		t.Fatal("expected spec")
	}
	if loaded.Info.Title != "YAML Spec" {
		t.Errorf("expected 'YAML Spec', got %q", loaded.Info.Title)
	}
}
