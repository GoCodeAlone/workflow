package module

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAPIGeneratorName(t *testing.T) {
	g := NewOpenAPIGenerator("test-openapi", OpenAPIGeneratorConfig{})
	if g.Name() != "test-openapi" {
		t.Errorf("expected name 'test-openapi', got %q", g.Name())
	}
}

func TestOpenAPIGeneratorProvidesServices(t *testing.T) {
	g := NewOpenAPIGenerator("my-openapi", OpenAPIGeneratorConfig{})
	providers := g.ProvidesServices()
	if len(providers) != 1 {
		t.Fatalf("expected 1 service provider, got %d", len(providers))
	}
	if providers[0].Name != "my-openapi" {
		t.Errorf("expected provider name 'my-openapi', got %q", providers[0].Name)
	}
	if providers[0].Instance != g {
		t.Error("expected provider instance to be the generator itself")
	}
}

func TestOpenAPIGeneratorRequiresServices(t *testing.T) {
	g := NewOpenAPIGenerator("gen", OpenAPIGeneratorConfig{})
	deps := g.RequiresServices()
	if deps != nil {
		t.Errorf("expected nil dependencies, got %v", deps)
	}
}

func TestOpenAPIHTTPHandler(t *testing.T) {
	called := false
	handler := &OpenAPIHTTPHandler{Handler: func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}}

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/openapi.json", nil)
	handler.Handle(w, r)

	if !called {
		t.Error("expected handler to be called")
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestOpenAPIGeneratorDefaults(t *testing.T) {
	g := NewOpenAPIGenerator("gen", OpenAPIGeneratorConfig{})
	if g.config.Title != "Workflow API" {
		t.Errorf("expected default title 'Workflow API', got %q", g.config.Title)
	}
	if g.config.Version != "1.0.0" {
		t.Errorf("expected default version '1.0.0', got %q", g.config.Version)
	}
}

func TestOpenAPIGeneratorBuildSpec(t *testing.T) {
	g := NewOpenAPIGenerator("gen", OpenAPIGeneratorConfig{
		Title:       "Test API",
		Version:     "2.0.0",
		Description: "A test API",
		Servers:     []string{"http://localhost:8080"},
	})

	workflows := map[string]any{
		"http-admin": map[string]any{
			"router": "admin-router",
			"routes": []any{
				map[string]any{
					"method":  "GET",
					"path":    "/api/v1/users",
					"handler": "user-handler",
				},
				map[string]any{
					"method":      "POST",
					"path":        "/api/v1/users",
					"handler":     "user-handler",
					"middlewares": []any{"cors", "auth-middleware"},
				},
				map[string]any{
					"method":  "GET",
					"path":    "/api/v1/users/{id}",
					"handler": "user-handler",
				},
				map[string]any{
					"method":  "DELETE",
					"path":    "/api/v1/users/{id}",
					"handler": "user-handler",
				},
			},
		},
	}

	g.BuildSpec(workflows)

	spec := g.GetSpec()
	if spec == nil {
		t.Fatal("expected spec to be non-nil")
	}

	if spec.OpenAPI != "3.0.3" {
		t.Errorf("expected OpenAPI 3.0.3, got %q", spec.OpenAPI)
	}
	if spec.Info.Title != "Test API" {
		t.Errorf("expected title 'Test API', got %q", spec.Info.Title)
	}
	if spec.Info.Version != "2.0.0" {
		t.Errorf("expected version '2.0.0', got %q", spec.Info.Version)
	}
	if len(spec.Servers) != 1 || spec.Servers[0].URL != "http://localhost:8080" {
		t.Errorf("unexpected servers: %+v", spec.Servers)
	}

	// Check paths
	if len(spec.Paths) != 2 {
		t.Errorf("expected 2 paths, got %d", len(spec.Paths))
	}

	usersPath := spec.Paths["/api/v1/users"]
	if usersPath == nil {
		t.Fatal("expected /api/v1/users path")
	}
	if usersPath.Get == nil {
		t.Error("expected GET operation on /api/v1/users")
	}
	if usersPath.Post == nil {
		t.Error("expected POST operation on /api/v1/users")
	}

	// POST should have auth-related responses (because of auth-middleware)
	if usersPath.Post != nil {
		if _, ok := usersPath.Post.Responses["401"]; !ok {
			t.Error("expected 401 response for authenticated endpoint")
		}
	}

	// Check path params
	userByIDPath := spec.Paths["/api/v1/users/{id}"]
	if userByIDPath == nil {
		t.Fatal("expected /api/v1/users/{id} path")
	}
	if userByIDPath.Get == nil {
		t.Error("expected GET operation on /api/v1/users/{id}")
	}
	if userByIDPath.Get != nil && len(userByIDPath.Get.Parameters) != 1 {
		t.Errorf("expected 1 path parameter, got %d", len(userByIDPath.Get.Parameters))
	}
	if userByIDPath.Get != nil && len(userByIDPath.Get.Parameters) == 1 {
		param := userByIDPath.Get.Parameters[0]
		if param.Name != "id" {
			t.Errorf("expected parameter name 'id', got %q", param.Name)
		}
		if param.In != "path" {
			t.Errorf("expected parameter in 'path', got %q", param.In)
		}
	}
}

func TestOpenAPIGeneratorServeJSON(t *testing.T) {
	g := NewOpenAPIGenerator("gen", OpenAPIGeneratorConfig{Title: "JSON Test"})
	g.BuildSpec(map[string]any{
		"http": map[string]any{
			"routes": []any{
				map[string]any{
					"method":  "GET",
					"path":    "/health",
					"handler": "health",
				},
			},
		},
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/openapi.json", nil)
	g.ServeJSON(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected application/json, got %q", ct)
	}

	var spec OpenAPISpec
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatalf("failed to parse JSON response: %v", err)
	}
	if spec.Info.Title != "JSON Test" {
		t.Errorf("unexpected title: %q", spec.Info.Title)
	}
}

func TestOpenAPIGeneratorServeYAML(t *testing.T) {
	g := NewOpenAPIGenerator("gen", OpenAPIGeneratorConfig{Title: "YAML Test"})
	g.BuildSpec(map[string]any{})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/openapi.yaml", nil)
	g.ServeYAML(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/x-yaml" {
		t.Errorf("expected application/x-yaml, got %q", ct)
	}
}

func TestOpenAPIGeneratorServeHTTP(t *testing.T) {
	g := NewOpenAPIGenerator("gen", OpenAPIGeneratorConfig{Title: "HTTP Test"})
	g.BuildSpec(map[string]any{})

	// JSON path
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/openapi.json", nil)
	g.ServeHTTP(w, r)
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected JSON content type, got %q", ct)
	}

	// YAML path
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodGet, "/api/openapi.yaml", nil)
	g.ServeHTTP(w, r)
	if ct := w.Header().Get("Content-Type"); ct != "application/x-yaml" {
		t.Errorf("expected YAML content type, got %q", ct)
	}
}

func TestOpenAPIGeneratorNoSpec(t *testing.T) {
	g := NewOpenAPIGenerator("gen", OpenAPIGeneratorConfig{})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/openapi.json", nil)
	g.ServeJSON(w, r)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestGenerateOperationID(t *testing.T) {
	tests := []struct {
		method   string
		path     string
		expected string
	}{
		{"get", "/api/v1/users", "getApiV1Users"},
		{"post", "/api/v1/users", "postApiV1Users"},
		{"get", "/api/v1/users/{id}", "getApiV1UsersById"},
		{"delete", "/api/v1/users/{id}/role", "deleteApiV1UsersByIdRole"},
	}

	for _, tt := range tests {
		got := generateOperationID(tt.method, tt.path)
		if got != tt.expected {
			t.Errorf("generateOperationID(%q, %q) = %q, want %q", tt.method, tt.path, got, tt.expected)
		}
	}
}

func TestOpenAPIGeneratorBuildSpecFromRoutes(t *testing.T) {
	g := NewOpenAPIGenerator("gen", OpenAPIGeneratorConfig{
		Title:   "Routes Test",
		Version: "1.0.0",
	})

	routes := []RouteDefinition{
		{
			Method:  "GET",
			Path:    "/items",
			Handler: "items-handler",
			Summary: "List items",
			Tags:    []string{"items"},
		},
		{
			Method:      "POST",
			Path:        "/items",
			Handler:     "items-handler",
			Summary:     "Create item",
			Tags:        []string{"items"},
			Middlewares: []string{"auth"},
		},
	}

	g.BuildSpecFromRoutes(routes)
	spec := g.GetSpec()

	if spec == nil {
		t.Fatal("expected spec")
	}
	if len(spec.Paths) != 1 {
		t.Errorf("expected 1 path, got %d", len(spec.Paths))
	}

	itemsPath := spec.Paths["/items"]
	if itemsPath == nil {
		t.Fatal("expected /items path")
	}
	if itemsPath.Get == nil || itemsPath.Get.Summary != "List items" {
		t.Error("expected GET with summary 'List items'")
	}
	if itemsPath.Post == nil || itemsPath.Post.Summary != "Create item" {
		t.Error("expected POST with summary 'Create item'")
	}
}

func TestOpenAPIGeneratorSortedPaths(t *testing.T) {
	g := NewOpenAPIGenerator("gen", OpenAPIGeneratorConfig{})
	g.BuildSpec(map[string]any{
		"http": map[string]any{
			"routes": []any{
				map[string]any{"method": "GET", "path": "/z/last"},
				map[string]any{"method": "GET", "path": "/a/first"},
				map[string]any{"method": "GET", "path": "/m/middle"},
			},
		},
	})

	paths := g.SortedPaths()
	if len(paths) != 3 {
		t.Fatalf("expected 3 paths, got %d", len(paths))
	}
	if paths[0] != "/a/first" || paths[1] != "/m/middle" || paths[2] != "/z/last" {
		t.Errorf("paths not sorted: %v", paths)
	}
}

func TestOpenAPIGeneratorAnnotations(t *testing.T) {
	g := NewOpenAPIGenerator("gen", OpenAPIGeneratorConfig{})
	g.BuildSpec(map[string]any{
		"http": map[string]any{
			"routes": []any{
				map[string]any{
					"method":      "GET",
					"path":        "/api/custom",
					"handler":     "handler",
					"summary":     "Custom summary",
					"operationId": "customOp",
					"tags":        []any{"custom-tag"},
				},
			},
		},
	})

	spec := g.GetSpec()
	pathItem := spec.Paths["/api/custom"]
	if pathItem == nil || pathItem.Get == nil {
		t.Fatal("expected path and GET op")
	}

	if pathItem.Get.Summary != "Custom summary" {
		t.Errorf("expected custom summary, got %q", pathItem.Get.Summary)
	}
	if pathItem.Get.OperationID != "customOp" {
		t.Errorf("expected customOp, got %q", pathItem.Get.OperationID)
	}
	if len(pathItem.Get.Tags) != 1 || pathItem.Get.Tags[0] != "custom-tag" {
		t.Errorf("expected [custom-tag], got %v", pathItem.Get.Tags)
	}
}

func TestOpenAPIGeneratorRequestBody(t *testing.T) {
	g := NewOpenAPIGenerator("gen", OpenAPIGeneratorConfig{})
	g.BuildSpec(map[string]any{
		"http": map[string]any{
			"routes": []any{
				map[string]any{"method": "POST", "path": "/create"},
				map[string]any{"method": "GET", "path": "/list"},
			},
		},
	})

	spec := g.GetSpec()

	// POST should have request body
	postOp := spec.Paths["/create"]
	if postOp == nil || postOp.Post == nil {
		t.Fatal("expected POST op")
	}
	if postOp.Post.RequestBody == nil {
		t.Error("expected request body for POST")
	}

	// GET should not have request body
	getOp := spec.Paths["/list"]
	if getOp == nil || getOp.Get == nil {
		t.Fatal("expected GET op")
	}
	if getOp.Get.RequestBody != nil {
		t.Error("GET should not have request body")
	}
}

func TestOpenAPIGeneratorCatchAllParam(t *testing.T) {
	g := NewOpenAPIGenerator("gen", OpenAPIGeneratorConfig{})
	g.BuildSpec(map[string]any{
		"http": map[string]any{
			"routes": []any{
				map[string]any{"method": "GET", "path": "/static/{path...}"},
			},
		},
	})

	spec := g.GetSpec()
	pathItem := spec.Paths["/static/{path...}"]
	if pathItem == nil || pathItem.Get == nil {
		t.Fatal("expected path")
	}
	// Catch-all {path...} should NOT be treated as a regular path param
	if len(pathItem.Get.Parameters) != 0 {
		t.Errorf("expected no path params for catch-all, got %d", len(pathItem.Get.Parameters))
	}
}

func TestOpenAPIGeneratorEmptyWorkflows(t *testing.T) {
	g := NewOpenAPIGenerator("gen", OpenAPIGeneratorConfig{Title: "Empty"})
	g.BuildSpec(map[string]any{})

	spec := g.GetSpec()
	if spec == nil {
		t.Fatal("expected spec")
	}
	if len(spec.Paths) != 0 {
		t.Errorf("expected 0 paths, got %d", len(spec.Paths))
	}
}

func TestOpenAPIGeneratorInvalidWorkflowConfig(t *testing.T) {
	g := NewOpenAPIGenerator("gen", OpenAPIGeneratorConfig{})

	// Workflow config that isn't a map
	g.BuildSpec(map[string]any{
		"broken": "not-a-map",
	})

	spec := g.GetSpec()
	if spec == nil {
		t.Fatal("expected spec even with bad config")
	}
	if len(spec.Paths) != 0 {
		t.Errorf("expected 0 paths for broken config, got %d", len(spec.Paths))
	}

	// Workflow with non-array routes
	g.BuildSpec(map[string]any{
		"broken": map[string]any{
			"routes": "not-an-array",
		},
	})

	spec = g.GetSpec()
	if len(spec.Paths) != 0 {
		t.Errorf("expected 0 paths for non-array routes, got %d", len(spec.Paths))
	}

	// Routes with missing method/path
	g.BuildSpec(map[string]any{
		"ok": map[string]any{
			"routes": []any{
				map[string]any{"method": "GET"}, // no path
				map[string]any{"path": "/x"},    // no method
				"not-a-map",
			},
		},
	})

	spec = g.GetSpec()
	if len(spec.Paths) != 0 {
		t.Errorf("expected 0 paths for incomplete routes, got %d", len(spec.Paths))
	}
}

func TestSchemaRefAndSchemaArray(t *testing.T) {
	ref := SchemaRef("UserProfile")
	if ref.Ref != "#/components/schemas/UserProfile" {
		t.Errorf("expected $ref '#/components/schemas/UserProfile', got %q", ref.Ref)
	}
	if ref.Type != "" {
		t.Errorf("$ref schema should have no type, got %q", ref.Type)
	}

	arr := SchemaArray(SchemaRef("Item"))
	if arr.Type != "array" {
		t.Errorf("expected type 'array', got %q", arr.Type)
	}
	if arr.Items == nil || arr.Items.Ref != "#/components/schemas/Item" {
		t.Error("expected items to be $ref to Item")
	}
}

func TestRegisterComponentSchema(t *testing.T) {
	g := NewOpenAPIGenerator("gen", OpenAPIGeneratorConfig{})

	g.RegisterComponentSchema("Foo", &OpenAPISchema{
		Type: "object",
		Properties: map[string]*OpenAPISchema{
			"name": {Type: "string"},
		},
	})
	g.RegisterComponentSchema("Bar", &OpenAPISchema{
		Type:     "object",
		Required: []string{"id"},
		Properties: map[string]*OpenAPISchema{
			"id": {Type: "integer"},
		},
	})

	if len(g.compSchema) != 2 {
		t.Fatalf("expected 2 component schemas, got %d", len(g.compSchema))
	}
	if g.compSchema["Foo"] == nil || g.compSchema["Bar"] == nil {
		t.Error("expected Foo and Bar schemas to be registered")
	}
}

func TestSetOperationSchema(t *testing.T) {
	g := NewOpenAPIGenerator("gen", OpenAPIGeneratorConfig{})

	req := SchemaRef("CreateFooRequest")
	resp := SchemaRef("Foo")
	g.SetOperationSchema("POST", "/api/foo", req, resp)

	if len(g.opSchemas) != 1 {
		t.Fatalf("expected 1 operation override, got %d", len(g.opSchemas))
	}
	override := g.opSchemas["POST /api/foo"]
	if override == nil {
		t.Fatal("expected override for POST /api/foo")
	}
	if override.RequestSchema != req {
		t.Error("request schema mismatch")
	}
	if override.ResponseSchema != resp {
		t.Error("response schema mismatch")
	}
}

func TestApplySchemas_ComponentsAddedToSpec(t *testing.T) {
	g := NewOpenAPIGenerator("gen", OpenAPIGeneratorConfig{})
	g.BuildSpec(map[string]any{})

	g.RegisterComponentSchema("Widget", &OpenAPISchema{
		Type: "object",
		Properties: map[string]*OpenAPISchema{
			"color": {Type: "string"},
			"size":  {Type: "integer"},
		},
	})

	g.ApplySchemas()

	spec := g.GetSpec()
	if spec.Components == nil {
		t.Fatal("expected components to be set")
	}
	if spec.Components.Schemas == nil {
		t.Fatal("expected schemas to be set")
	}
	widget := spec.Components.Schemas["Widget"]
	if widget == nil {
		t.Fatal("expected Widget schema")
	}
	if widget.Type != "object" {
		t.Errorf("expected type 'object', got %q", widget.Type)
	}
	if len(widget.Properties) != 2 {
		t.Errorf("expected 2 properties, got %d", len(widget.Properties))
	}
}

func TestApplySchemas_OperationOverrides(t *testing.T) {
	g := NewOpenAPIGenerator("gen", OpenAPIGeneratorConfig{})
	g.BuildSpec(map[string]any{
		"http": map[string]any{
			"routes": []any{
				map[string]any{"method": "GET", "path": "/api/items"},
				map[string]any{"method": "POST", "path": "/api/items"},
			},
		},
	})

	// Register schemas
	g.RegisterComponentSchema("Item", &OpenAPISchema{
		Type: "object",
		Properties: map[string]*OpenAPISchema{
			"id":   {Type: "string"},
			"name": {Type: "string"},
		},
	})
	g.RegisterComponentSchema("CreateItemRequest", &OpenAPISchema{
		Type:     "object",
		Required: []string{"name"},
		Properties: map[string]*OpenAPISchema{
			"name": {Type: "string"},
		},
	})

	// Set operation schemas
	g.SetOperationSchema("GET", "/api/items", nil, SchemaArray(SchemaRef("Item")))
	g.SetOperationSchema("POST", "/api/items", SchemaRef("CreateItemRequest"), SchemaRef("Item"))

	g.ApplySchemas()

	spec := g.GetSpec()

	// Check GET response was overridden
	getOp := spec.Paths["/api/items"].Get
	if getOp == nil {
		t.Fatal("expected GET operation")
	}
	resp200 := getOp.Responses["200"]
	if resp200 == nil {
		t.Fatal("expected 200 response")
	}
	jsonContent := resp200.Content["application/json"]
	if jsonContent == nil {
		t.Fatal("expected application/json content")
	}
	if jsonContent.Schema.Type != "array" {
		t.Errorf("expected array type, got %q", jsonContent.Schema.Type)
	}
	if jsonContent.Schema.Items == nil || jsonContent.Schema.Items.Ref != "#/components/schemas/Item" {
		t.Error("expected items $ref to Item")
	}

	// Check POST request body was overridden
	postOp := spec.Paths["/api/items"].Post
	if postOp == nil {
		t.Fatal("expected POST operation")
	}
	reqBody := postOp.RequestBody
	if reqBody == nil {
		t.Fatal("expected request body")
	}
	reqSchema := reqBody.Content["application/json"].Schema
	if reqSchema.Ref != "#/components/schemas/CreateItemRequest" {
		t.Errorf("expected $ref to CreateItemRequest, got %q", reqSchema.Ref)
	}

	// Check POST response was overridden
	postResp := postOp.Responses["200"]
	if postResp == nil {
		t.Fatal("expected 200 response for POST")
	}
	postRespSchema := postResp.Content["application/json"].Schema
	if postRespSchema.Ref != "#/components/schemas/Item" {
		t.Errorf("expected $ref to Item, got %q", postRespSchema.Ref)
	}
}

func TestApplySchemas_NonMatchingPathSkipped(t *testing.T) {
	g := NewOpenAPIGenerator("gen", OpenAPIGeneratorConfig{})
	g.BuildSpec(map[string]any{
		"http": map[string]any{
			"routes": []any{
				map[string]any{"method": "GET", "path": "/api/real"},
			},
		},
	})

	// Set schema for a path that doesn't exist in the spec
	g.SetOperationSchema("GET", "/api/nonexistent", nil, SchemaRef("Foo"))
	g.ApplySchemas()

	spec := g.GetSpec()
	// Should not crash and /api/real should be unmodified
	if spec.Paths["/api/real"] == nil {
		t.Fatal("expected /api/real to remain")
	}
	if spec.Paths["/api/nonexistent"] != nil {
		t.Error("should not create new paths from overrides")
	}
}

func TestApplySchemas_NilSpec(t *testing.T) {
	g := NewOpenAPIGenerator("gen", OpenAPIGeneratorConfig{})
	// Don't call BuildSpec - spec is nil
	g.RegisterComponentSchema("Foo", &OpenAPISchema{Type: "object"})
	// Should not panic
	g.ApplySchemas()
	if g.GetSpec() != nil {
		t.Error("spec should remain nil")
	}
}

func TestRegisterAdminSchemas_Integration(t *testing.T) {
	g := NewOpenAPIGenerator("gen", OpenAPIGeneratorConfig{Title: "Admin API"})

	// Build a spec with a couple of admin routes to test override application
	g.BuildSpecFromRoutes([]RouteDefinition{
		{Method: "GET", Path: "/api/v1/auth/setup-status", Handler: "auth"},
		{Method: "POST", Path: "/api/v1/auth/login", Handler: "auth"},
		{Method: "GET", Path: "/api/v1/admin/engine/config", Handler: "engine"},
		{Method: "GET", Path: "/api/v1/admin/companies", Handler: "v1"},
		{Method: "POST", Path: "/api/v1/admin/companies", Handler: "v1"},
		{Method: "GET", Path: "/api/v1/admin/workflows", Handler: "v1"},
		{Method: "POST", Path: "/api/v1/admin/ai/generate", Handler: "ai"},
		{Method: "GET", Path: "/api/v1/admin/components", Handler: "dynamic"},
	})

	RegisterAdminSchemas(g)
	g.ApplySchemas()

	spec := g.GetSpec()
	if spec == nil {
		t.Fatal("expected spec")
	}

	// Verify component schemas were registered
	if spec.Components == nil || spec.Components.Schemas == nil {
		t.Fatal("expected component schemas")
	}

	// Spot-check a few key schemas
	expectedSchemas := []string{
		"LoginRequest", "AuthResponse", "UserProfile", "SetupStatusResponse",
		"WorkflowConfig", "EngineStatus", "ModuleSchema",
		"AIGenerateRequest", "AIGenerateResponse",
		"Company", "Organization", "Project", "Workflow", "Execution",
		"ComponentInfo", "IAMProvider", "DashboardData", "AuditEntry",
		"ErrorResponse", "SuccessResponse",
	}
	for _, name := range expectedSchemas {
		if spec.Components.Schemas[name] == nil {
			t.Errorf("expected component schema %q", name)
		}
	}

	// Verify operation overrides were applied
	// GET /api/v1/auth/setup-status should return SetupStatusResponse
	setupStatus := spec.Paths["/api/v1/auth/setup-status"]
	if setupStatus == nil || setupStatus.Get == nil {
		t.Fatal("expected setup-status path")
	}
	respSchema := setupStatus.Get.Responses["200"].Content["application/json"].Schema
	if respSchema.Ref != "#/components/schemas/SetupStatusResponse" {
		t.Errorf("expected $ref to SetupStatusResponse, got %q", respSchema.Ref)
	}

	// POST /api/v1/auth/login should have LoginRequest body and AuthResponse
	login := spec.Paths["/api/v1/auth/login"]
	if login == nil || login.Post == nil {
		t.Fatal("expected login path")
	}
	loginReq := login.Post.RequestBody.Content["application/json"].Schema
	if loginReq.Ref != "#/components/schemas/LoginRequest" {
		t.Errorf("expected $ref to LoginRequest, got %q", loginReq.Ref)
	}
	loginResp := login.Post.Responses["200"].Content["application/json"].Schema
	if loginResp.Ref != "#/components/schemas/AuthResponse" {
		t.Errorf("expected $ref to AuthResponse, got %q", loginResp.Ref)
	}

	// GET /api/v1/admin/companies should return array of Company
	companies := spec.Paths["/api/v1/admin/companies"]
	if companies == nil || companies.Get == nil {
		t.Fatal("expected companies path")
	}
	compResp := companies.Get.Responses["200"].Content["application/json"].Schema
	if compResp.Type != "array" {
		t.Errorf("expected array, got %q", compResp.Type)
	}
	if compResp.Items == nil || compResp.Items.Ref != "#/components/schemas/Company" {
		t.Error("expected items $ref to Company")
	}

	// POST /api/v1/admin/companies should have CreateCompanyRequest
	compPost := companies.Post
	if compPost == nil {
		t.Fatal("expected POST on companies")
	}
	compPostReq := compPost.RequestBody.Content["application/json"].Schema
	if compPostReq.Ref != "#/components/schemas/CreateCompanyRequest" {
		t.Errorf("expected $ref to CreateCompanyRequest, got %q", compPostReq.Ref)
	}

	// Verify JSON serialization works (no cycles, no marshalling errors)
	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("failed to marshal spec to JSON: %v", err)
	}
	if !strings.Contains(string(data), `"$ref":"#/components/schemas/`) {
		t.Error("expected $ref in JSON output")
	}
}

func TestOpenAPIServeJSONParseable(t *testing.T) {
	g := NewOpenAPIGenerator("gen", OpenAPIGeneratorConfig{
		Title:       "Parse Test",
		Description: "Testing JSON roundtrip",
	})
	g.BuildSpec(map[string]any{
		"http": map[string]any{
			"routes": []any{
				map[string]any{"method": "GET", "path": "/api/test"},
			},
		},
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/openapi.json", nil)
	g.ServeJSON(w, r)

	body := w.Body.String()
	if !strings.Contains(body, "Parse Test") {
		t.Error("expected title in JSON output")
	}
	if !strings.Contains(body, "3.0.3") {
		t.Error("expected OpenAPI version in JSON output")
	}
}
