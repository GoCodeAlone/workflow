package scaffold

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sampleOpenAPISpec is a comprehensive OpenAPI 3.0 spec used across tests.
const sampleOpenAPISpec = `{
  "openapi": "3.0.3",
  "info": {
    "title": "Pet Store API",
    "version": "1.0.0",
    "description": "A sample pet store API"
  },
  "paths": {
    "/api/v1/pets": {
      "get": {
        "operationId": "listPets",
        "summary": "List all pets",
        "tags": ["pets"],
        "responses": {"200": {"description": "success"}}
      },
      "post": {
        "operationId": "createPet",
        "summary": "Create a pet",
        "tags": ["pets"],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {
                  "name": {"type": "string"},
                  "species": {"type": "string", "enum": ["dog", "cat", "bird"]},
                  "age": {"type": "integer"}
                },
                "required": ["name"]
              }
            }
          }
        },
        "responses": {"201": {"description": "created"}}
      }
    },
    "/api/v1/pets/{id}": {
      "get": {
        "operationId": "getPet",
        "summary": "Get a pet",
        "tags": ["pets"],
        "parameters": [{"name": "id", "in": "path", "required": true}],
        "responses": {"200": {"description": "success"}}
      },
      "delete": {
        "operationId": "deletePet",
        "summary": "Delete a pet",
        "tags": ["pets"],
        "parameters": [{"name": "id", "in": "path", "required": true}],
        "responses": {"204": {"description": "deleted"}}
      }
    },
    "/auth/login": {
      "post": {
        "operationId": "login",
        "summary": "Log in",
        "tags": ["auth"],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {
                  "email": {"type": "string", "format": "email"},
                  "password": {"type": "string"}
                },
                "required": ["email", "password"]
              }
            }
          }
        },
        "responses": {"200": {"description": "token"}}
      }
    },
    "/auth/register": {
      "post": {
        "operationId": "register",
        "summary": "Register",
        "tags": ["auth"],
        "requestBody": {
          "required": true,
          "content": {
            "application/json": {
              "schema": {
                "type": "object",
                "properties": {
                  "email": {"type": "string"},
                  "password": {"type": "string"}
                },
                "required": ["email", "password"]
              }
            }
          }
        },
        "responses": {"201": {"description": "registered"}}
      }
    }
  }
}`

// sampleMinimalSpec is a minimal spec with no auth and one resource.
const sampleMinimalSpec = `
openapi: "3.0.3"
info:
  title: "Todo API"
  version: "0.1.0"
paths:
  /todos:
    get:
      operationId: listTodos
      responses:
        "200":
          description: success
    post:
      operationId: createTodo
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              properties:
                title:
                  type: string
                done:
                  type: boolean
              required:
                - title
      responses:
        "201":
          description: created
  /todos/{id}:
    get:
      operationId: getTodo
      parameters:
        - name: id
          in: path
          required: true
      responses:
        "200":
          description: success
    delete:
      operationId: deleteTodo
      parameters:
        - name: id
          in: path
          required: true
      responses:
        "204":
          description: deleted
`

// --- ParseSpec ---

func TestParseSpec_JSON(t *testing.T) {
	spec, err := ParseSpec([]byte(sampleOpenAPISpec))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Info.Title != "Pet Store API" {
		t.Errorf("expected title 'Pet Store API', got %q", spec.Info.Title)
	}
	if len(spec.Paths) != 4 {
		t.Errorf("expected 4 paths, got %d", len(spec.Paths))
	}
}

func TestParseSpec_YAML(t *testing.T) {
	spec, err := ParseSpec([]byte(sampleMinimalSpec))
	if err != nil {
		t.Fatalf("unexpected error parsing YAML: %v", err)
	}
	if spec.Info.Title != "Todo API" {
		t.Errorf("expected 'Todo API', got %q", spec.Info.Title)
	}
	if len(spec.Paths) != 2 {
		t.Errorf("expected 2 paths, got %d", len(spec.Paths))
	}
}

func TestParseSpec_Empty(t *testing.T) {
	_, err := ParseSpec([]byte(""))
	if err == nil {
		t.Fatal("expected error for empty spec")
	}
}

func TestParseSpec_Invalid(t *testing.T) {
	_, err := ParseSpec([]byte("{not valid json}"))
	if err == nil {
		t.Fatal("expected error for invalid spec")
	}
}

// --- AnalyzeSpec ---

func TestAnalyzeSpec_DetectsAuth(t *testing.T) {
	spec, err := ParseSpec([]byte(sampleOpenAPISpec))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	data := AnalyzeSpec(spec, Options{})

	if !data.HasAuth {
		t.Error("expected HasAuth=true for spec with /auth/login and /auth/register")
	}
	if data.LoginPath != "/auth/login" {
		t.Errorf("expected LoginPath='/auth/login', got %q", data.LoginPath)
	}
	if data.RegisterPath != "/auth/register" {
		t.Errorf("expected RegisterPath='/auth/register', got %q", data.RegisterPath)
	}
}

func TestAnalyzeSpec_NoAuthInMinimal(t *testing.T) {
	spec, err := ParseSpec([]byte(sampleMinimalSpec))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	data := AnalyzeSpec(spec, Options{})

	if data.HasAuth {
		t.Error("expected HasAuth=false for spec without auth endpoints")
	}
}

func TestAnalyzeSpec_ForceAuth(t *testing.T) {
	spec, err := ParseSpec([]byte(sampleMinimalSpec))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	data := AnalyzeSpec(spec, Options{Auth: true})

	if !data.HasAuth {
		t.Error("expected HasAuth=true when Options.Auth=true")
	}
	if data.LoginPath == "" {
		t.Error("expected LoginPath to be set when Options.Auth=true")
	}
}

func TestAnalyzeSpec_ResourceGrouping(t *testing.T) {
	spec, err := ParseSpec([]byte(sampleOpenAPISpec))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	data := AnalyzeSpec(spec, Options{})

	if len(data.Resources) != 1 {
		t.Errorf("expected 1 resource (pets), got %d: %v", len(data.Resources), resourceNames(data.Resources))
	}
	rg := data.Resources[0]
	if rg.Name != "Pets" {
		t.Errorf("expected resource name 'Pets', got %q", rg.Name)
	}
	if rg.ListOp == nil {
		t.Error("expected ListOp for GET /api/v1/pets")
	}
	if rg.CreateOp == nil {
		t.Error("expected CreateOp for POST /api/v1/pets")
	}
	if rg.DetailOp == nil {
		t.Error("expected DetailOp for GET /api/v1/pets/{id}")
	}
	if rg.DeleteOp == nil {
		t.Error("expected DeleteOp for DELETE /api/v1/pets/{id}")
	}
}

func TestAnalyzeSpec_TitleOverride(t *testing.T) {
	spec, err := ParseSpec([]byte(sampleOpenAPISpec))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	data := AnalyzeSpec(spec, Options{Title: "My Custom Title"})
	if data.Title != "My Custom Title" {
		t.Errorf("expected title 'My Custom Title', got %q", data.Title)
	}
}

func TestAnalyzeSpec_TitleFromSpec(t *testing.T) {
	spec, err := ParseSpec([]byte(sampleOpenAPISpec))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	data := AnalyzeSpec(spec, Options{})
	if data.Title != "Pet Store API" {
		t.Errorf("expected title from spec 'Pet Store API', got %q", data.Title)
	}
}

func TestAnalyzeSpec_OperationsIncluded(t *testing.T) {
	spec, err := ParseSpec([]byte(sampleOpenAPISpec))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	data := AnalyzeSpec(spec, Options{})

	funcNames := make(map[string]bool)
	for _, op := range data.Operations {
		funcNames[op.FuncName] = true
	}

	// listPets, createPet, getPet, deletePet are in non-auth paths.
	for _, expected := range []string{"listPets", "createPet", "getPet", "deletePet"} {
		if !funcNames[expected] {
			t.Errorf("expected operation %q in Operations list, got: %v", expected, operationFuncNames(data.Operations))
		}
	}
}

func TestAnalyzeSpec_FormFieldsExtracted(t *testing.T) {
	spec, err := ParseSpec([]byte(sampleOpenAPISpec))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	data := AnalyzeSpec(spec, Options{})

	if len(data.Resources) == 0 {
		t.Fatal("expected at least one resource")
	}
	rg := data.Resources[0] // Pets

	if len(rg.FormFields) == 0 {
		t.Fatal("expected form fields from createPet requestBody")
	}

	fieldMap := make(map[string]FormField)
	for _, f := range rg.FormFields {
		fieldMap[f.Name] = f
	}

	age, ok := fieldMap["age"]
	if !ok {
		t.Error("expected 'age' form field")
	} else if age.Type != "number" {
		t.Errorf("expected age.Type='number', got %q", age.Type)
	}

	species, ok := fieldMap["species"]
	if !ok {
		t.Error("expected 'species' form field")
	} else {
		if species.Type != "select" {
			t.Errorf("expected species.Type='select', got %q", species.Type)
		}
		if len(species.Options) != 3 {
			t.Errorf("expected 3 options for species, got %d", len(species.Options))
		}
	}
}

// --- generateFuncName ---

func TestGenerateFuncName(t *testing.T) {
	cases := []struct {
		method string
		path   string
		want   string
	}{
		{"GET", "/api/v1/users", "getApiV1Users"},
		{"POST", "/api/v1/users", "postApiV1Users"},
		{"GET", "/users/{id}", "getUsersById"},
		{"DELETE", "/users/{id}", "deleteUsersById"},
		{"PUT", "/users/{id}/profile", "putUsersByIdProfile"},
	}
	for _, c := range cases {
		got := generateFuncName(c.method, c.path)
		if got != c.want {
			t.Errorf("generateFuncName(%q, %q) = %q, want %q", c.method, c.path, got, c.want)
		}
	}
}

// --- resourceNameFromPath ---

func TestResourceNameFromPath(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"/api/v1/users", "users"},
		{"/api/v1/users/{id}", "users"},
		{"/users", "users"},
		{"/pets/{id}", "pets"},
		{"/api/v2/orders/{id}/items", "items"},
		{"/", ""},
	}
	for _, c := range cases {
		got := resourceNameFromPath(c.path)
		if got != c.want {
			t.Errorf("resourceNameFromPath(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

// --- inferFieldType ---

func TestInferFieldType(t *testing.T) {
	cases := []struct {
		name   string
		schema SpecSchema
		want   string
	}{
		{"email", SpecSchema{Type: "string"}, "email"},
		{"emailAddress", SpecSchema{Type: "string"}, "email"},
		{"password", SpecSchema{Type: "string"}, "password"},
		{"secret_key", SpecSchema{Type: "string"}, "password"},
		{"count", SpecSchema{Type: "integer"}, "number"},
		{"price", SpecSchema{Type: "number"}, "number"},
		{"name", SpecSchema{Type: "string"}, "text"},
		{"status", SpecSchema{Type: "string", Enum: []string{"active", "inactive"}}, "select"},
	}
	for _, c := range cases {
		got := inferFieldType(c.name, &c.schema)
		if got != c.want {
			t.Errorf("inferFieldType(%q, ...) = %q, want %q", c.name, got, c.want)
		}
	}
}

// --- toLabel ---

func TestToLabel(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"name", "Name"},
		{"first_name", "First name"},
		{"emailAddress", "Email Address"},
		{"user_id", "User id"},
	}
	for _, c := range cases {
		got := toLabel(c.input)
		if got != c.want {
			t.Errorf("toLabel(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// --- jsPathExpr ---

func TestJsPathExpr(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"/users", "'/users'"},
		{"/users/{id}", "`/users/${id}`"},
		{"/api/v1/users/{id}/posts/{postId}", "`/api/v1/users/${id}/posts/${postId}`"},
	}
	for _, c := range cases {
		got := jsPathExpr(c.input)
		if got != c.want {
			t.Errorf("jsPathExpr(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// --- tsTupleArgs ---

func TestTsTupleArgs(t *testing.T) {
	cases := []struct {
		method     string
		pathParams []string
		hasBody    bool
		want       string
	}{
		{"GET", nil, false, ""},
		{"GET", []string{"id"}, false, "id: string"},
		{"POST", nil, true, "data: any"},
		{"PUT", []string{"id"}, true, "id: string, data: any"},
		{"DELETE", []string{"id"}, false, "id: string"},
		{"POST", nil, false, "data: any"}, // POST always gets data param
	}
	for _, c := range cases {
		got := tsTupleArgs(c.method, c.pathParams, c.hasBody)
		if got != c.want {
			t.Errorf("tsTupleArgs(%q, %v, %v) = %q, want %q", c.method, c.pathParams, c.hasBody, got, c.want)
		}
	}
}

// --- GenerateScaffold (integration) ---

func TestGenerateScaffold_WithAuth(t *testing.T) {
	spec, err := ParseSpec([]byte(sampleOpenAPISpec))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	data := AnalyzeSpec(spec, Options{})

	outDir := t.TempDir()
	if err := GenerateScaffold(outDir, *data); err != nil {
		t.Fatalf("GenerateScaffold failed: %v", err)
	}

	// Verify all expected files are generated.
	expectedFiles := []string{
		"package.json",
		"tsconfig.json",
		"vite.config.ts",
		"index.html",
		"src/main.tsx",
		"src/App.tsx",
		"src/api.ts",
		"src/auth.tsx",
		"src/components/Layout.tsx",
		"src/components/DataTable.tsx",
		"src/components/FormField.tsx",
		"src/pages/DashboardPage.tsx",
		"src/pages/LoginPage.tsx",
		"src/pages/RegisterPage.tsx",
		"src/pages/PetsPage.tsx",
	}
	for _, f := range expectedFiles {
		path := filepath.Join(outDir, f)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected file not generated: %s", f)
		}
	}
}

func TestGenerateScaffold_NoAuth(t *testing.T) {
	spec, err := ParseSpec([]byte(sampleMinimalSpec))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	data := AnalyzeSpec(spec, Options{})

	outDir := t.TempDir()
	if err := GenerateScaffold(outDir, *data); err != nil {
		t.Fatalf("GenerateScaffold failed: %v", err)
	}

	// Auth files must NOT be generated.
	for _, f := range []string{"src/auth.tsx", "src/pages/LoginPage.tsx", "src/pages/RegisterPage.tsx"} {
		path := filepath.Join(outDir, f)
		if _, err := os.Stat(path); err == nil {
			t.Errorf("auth file should not be generated without auth: %s", f)
		}
	}

	// Resource page must be generated.
	todoPage := filepath.Join(outDir, "src", "pages", "TodosPage.tsx")
	if _, err := os.Stat(todoPage); os.IsNotExist(err) {
		t.Error("expected TodosPage.tsx to be generated")
	}
}

func TestGenerateScaffold_PackageJSON(t *testing.T) {
	spec, err := ParseSpec([]byte(sampleOpenAPISpec))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	data := AnalyzeSpec(spec, Options{})
	outDir := t.TempDir()
	if err := GenerateScaffold(outDir, *data); err != nil {
		t.Fatalf("GenerateScaffold: %v", err)
	}

	pkgData, err := os.ReadFile(filepath.Join(outDir, "package.json"))
	if err != nil {
		t.Fatalf("read package.json: %v", err)
	}

	var pkg map[string]any
	if err := json.Unmarshal(pkgData, &pkg); err != nil {
		t.Fatalf("package.json is not valid JSON: %v\ncontent:\n%s", err, pkgData)
	}

	deps, ok := pkg["dependencies"].(map[string]any)
	if !ok {
		t.Fatal("package.json missing dependencies")
	}
	for _, dep := range []string{"react", "react-dom", "react-router-dom"} {
		if _, ok := deps[dep]; !ok {
			t.Errorf("package.json missing dependency: %s", dep)
		}
	}

	devDeps, ok := pkg["devDependencies"].(map[string]any)
	if !ok {
		t.Fatal("package.json missing devDependencies")
	}
	for _, dep := range []string{"vite", "typescript", "@vitejs/plugin-react"} {
		if _, ok := devDeps[dep]; !ok {
			t.Errorf("package.json missing devDependency: %s", dep)
		}
	}
}

func TestGenerateScaffold_APIClient(t *testing.T) {
	spec, err := ParseSpec([]byte(sampleOpenAPISpec))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	data := AnalyzeSpec(spec, Options{})
	outDir := t.TempDir()
	if err := GenerateScaffold(outDir, *data); err != nil {
		t.Fatalf("GenerateScaffold: %v", err)
	}

	apiTS, err := os.ReadFile(filepath.Join(outDir, "src", "api.ts"))
	if err != nil {
		t.Fatalf("read api.ts: %v", err)
	}

	content := string(apiTS)
	for _, funcName := range []string{"listPets", "createPet", "getPet", "deletePet"} {
		if !strings.Contains(content, funcName) {
			t.Errorf("api.ts missing function %q", funcName)
		}
	}

	// The API base helper must be present.
	if !strings.Contains(content, "apiCall") {
		t.Error("api.ts missing apiCall helper")
	}

	// Bearer token injection must be present.
	if !strings.Contains(content, "Authorization") {
		t.Error("api.ts missing Authorization header")
	}

	// 401 redirect must be present.
	if !strings.Contains(content, "401") {
		t.Error("api.ts missing 401 handling")
	}
}

func TestGenerateScaffold_ViteConfig(t *testing.T) {
	spec, err := ParseSpec([]byte(sampleMinimalSpec))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	data := AnalyzeSpec(spec, Options{})
	outDir := t.TempDir()
	if err := GenerateScaffold(outDir, *data); err != nil {
		t.Fatalf("GenerateScaffold: %v", err)
	}

	viteConfig, err := os.ReadFile(filepath.Join(outDir, "vite.config.ts"))
	if err != nil {
		t.Fatalf("read vite.config.ts: %v", err)
	}

	content := string(viteConfig)
	if !strings.Contains(content, "localhost:8080") {
		t.Error("vite.config.ts should proxy /api to localhost:8080")
	}
	if !strings.Contains(content, "proxy") {
		t.Error("vite.config.ts should have proxy config")
	}
}

func TestGenerateScaffold_AppRoutes(t *testing.T) {
	spec, err := ParseSpec([]byte(sampleOpenAPISpec))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	data := AnalyzeSpec(spec, Options{})
	outDir := t.TempDir()
	if err := GenerateScaffold(outDir, *data); err != nil {
		t.Fatalf("GenerateScaffold: %v", err)
	}

	appTSX, err := os.ReadFile(filepath.Join(outDir, "src", "App.tsx"))
	if err != nil {
		t.Fatalf("read App.tsx: %v", err)
	}

	content := string(appTSX)
	if !strings.Contains(content, "PetsPage") {
		t.Error("App.tsx should import PetsPage")
	}
	if !strings.Contains(content, "LoginPage") {
		t.Error("App.tsx should import LoginPage (auth detected)")
	}
	if !strings.Contains(content, "RegisterPage") {
		t.Error("App.tsx should import RegisterPage (auth detected)")
	}
}

func TestGenerateScaffold_LayoutNav(t *testing.T) {
	spec, err := ParseSpec([]byte(sampleOpenAPISpec))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	data := AnalyzeSpec(spec, Options{})
	outDir := t.TempDir()
	if err := GenerateScaffold(outDir, *data); err != nil {
		t.Fatalf("GenerateScaffold: %v", err)
	}

	layoutTSX, err := os.ReadFile(filepath.Join(outDir, "src", "components", "Layout.tsx"))
	if err != nil {
		t.Fatalf("read Layout.tsx: %v", err)
	}

	content := string(layoutTSX)
	// Should have nav link to pets resource.
	if !strings.Contains(content, "/pets") {
		t.Error("Layout.tsx should have nav link to /pets")
	}
	// Should have logout since auth is present.
	if !strings.Contains(content, "logout") && !strings.Contains(content, "Logout") {
		t.Error("Layout.tsx should have logout for auth-enabled spec")
	}
}

// --- AnalyzeOnly ---

func TestAnalyzeOnly_JSON(t *testing.T) {
	data, err := AnalyzeOnly([]byte(sampleOpenAPISpec), Options{})
	if err != nil {
		t.Fatalf("AnalyzeOnly failed: %v", err)
	}
	if data.Title != "Pet Store API" {
		t.Errorf("expected title 'Pet Store API', got %q", data.Title)
	}
	if !data.HasAuth {
		t.Error("expected HasAuth=true")
	}
	if len(data.Resources) != 1 {
		t.Errorf("expected 1 resource, got %d", len(data.Resources))
	}
}

func TestAnalyzeOnly_YAML(t *testing.T) {
	data, err := AnalyzeOnly([]byte(sampleMinimalSpec), Options{Title: "Override"})
	if err != nil {
		t.Fatalf("AnalyzeOnly failed: %v", err)
	}
	if data.Title != "Override" {
		t.Errorf("expected title 'Override', got %q", data.Title)
	}
}

func TestAnalyzeOnly_InvalidSpec(t *testing.T) {
	_, err := AnalyzeOnly([]byte(""), Options{})
	if err == nil {
		t.Fatal("expected error for empty spec")
	}
}

// --- GenerateToZip ---

func TestGenerateToZip_WithAuth(t *testing.T) {
	spec, err := ParseSpec([]byte(sampleOpenAPISpec))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	data := AnalyzeSpec(spec, Options{})

	zipBytes, err := GenerateToZip(*data)
	if err != nil {
		t.Fatalf("GenerateToZip failed: %v", err)
	}
	if len(zipBytes) == 0 {
		t.Fatal("expected non-empty zip bytes")
	}

	// Verify zip has correct magic bytes.
	if len(zipBytes) < 4 || zipBytes[0] != 'P' || zipBytes[1] != 'K' {
		t.Error("result is not a valid zip file (missing PK magic bytes)")
	}
}

func TestGenerateToZip_ContainsExpectedEntries(t *testing.T) {
	spec, err := ParseSpec([]byte(sampleOpenAPISpec))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	data := AnalyzeSpec(spec, Options{})

	zipBytes, err := GenerateToZip(*data)
	if err != nil {
		t.Fatalf("GenerateToZip failed: %v", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		t.Fatalf("failed to open zip: %v", err)
	}

	entries := make(map[string]bool)
	for _, f := range zr.File {
		entries[f.Name] = true
	}

	expectedPaths := []string{
		"ui/package.json",
		"ui/tsconfig.json",
		"ui/src/App.tsx",
		"ui/src/api.ts",
		"ui/src/auth.tsx",
		"ui/src/pages/PetsPage.tsx",
		"ui/src/pages/LoginPage.tsx",
	}
	for _, p := range expectedPaths {
		if !entries[p] {
			t.Errorf("zip missing expected entry: %s", p)
		}
	}
}

// --- helpers ---

func resourceNames(rgs []ResourceGroup) []string {
	names := make([]string, len(rgs))
	for i, rg := range rgs {
		names[i] = rg.Name
	}
	return names
}

func operationFuncNames(ops []APIOperation) []string {
	names := make([]string, len(ops))
	for i, op := range ops {
		names[i] = op.FuncName
	}
	return names
}
