package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/module"
	"gopkg.in/yaml.v3"
)

const configWithPipelines = `
modules:
  - name: server
    type: http.server
    config:
      address: ":8080"
  - name: router
    type: http.router
    dependsOn: [server]

pipelines:
  create-user:
    trigger:
      type: http
      config:
        path: /api/v1/users
        method: POST
    steps:
      - type: step.validate
        config:
          rules:
            email: required,email
            password: required,min=8
      - type: step.user_register
      - type: step.json_response
        config:
          statusCode: 201

  login:
    trigger:
      type: http
      config:
        path: /api/v1/login
        method: POST
    steps:
      - type: step.user_login
      - type: step.json_response

  health:
    trigger:
      type: http
      config:
        path: /healthz
        method: GET
    steps:
      - type: step.json_response
        config:
          status: 200
          body:
            status: ok
`

const configWithWorkflowRoutes = `
modules:
  - name: server
    type: http.server
    config:
      address: ":8080"
  - name: router
    type: http.router
    dependsOn: [server]
  - name: jwt
    type: auth.jwt
    config:
      secret: "test-secret"
    dependsOn: [router]
  - name: auth-middleware
    type: http.middleware.auth
    dependsOn: [jwt]

workflows:
  http:
    router: router
    server: server
    routes:
      - method: POST
        path: /api/auth/login
        handler: jwt
      - method: GET
        path: /api/auth/profile
        handler: jwt
        middlewares:
          - auth-middleware
      - method: GET
        path: /api/users/{id}
        handler: jwt
`

const configWithBothSourcesYAML = `
modules:
  - name: server
    type: http.server
    config:
      address: ":8080"
  - name: router
    type: http.router

workflows:
  http:
    router: router
    server: server
    routes:
      - method: GET
        path: /api/items
        handler: items-handler

pipelines:
  create-item:
    trigger:
      type: http
      config:
        path: /api/items
        method: POST
    steps:
      - type: step.validate
        config:
          rules:
            name: required
      - type: step.json_response
        config:
          statusCode: 201
`

const configNoPipelines = `
modules:
  - name: server
    type: http.server
    config:
      address: ":8080"
`

func TestRunAPIExtractMissingArg(t *testing.T) {
	err := runAPIExtract([]string{})
	if err == nil {
		t.Fatal("expected error when no config file given")
	}
	if !strings.Contains(err.Error(), "config file path is required") {
		t.Errorf("expected 'config file path is required', got: %v", err)
	}
}

func TestRunAPIExtractMissingSubcommand(t *testing.T) {
	err := runAPI([]string{})
	if err == nil {
		t.Fatal("expected error when no subcommand given")
	}
}

func TestRunAPIExtractUnknownSubcommand(t *testing.T) {
	err := runAPI([]string{"unknown"})
	if err == nil {
		t.Fatal("expected error for unknown subcommand")
	}
}

func TestRunAPIExtractJSONOutput(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeTestConfig(t, dir, "config.yaml", configWithPipelines)
	outPath := filepath.Join(dir, "openapi.json")

	err := runAPIExtract([]string{"-format", "json", "-output", outPath, cfgPath})
	if err != nil {
		t.Fatalf("api extract failed: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}

	var spec module.OpenAPISpec
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if spec.OpenAPI != "3.0.3" {
		t.Errorf("expected OpenAPI 3.0.3, got %q", spec.OpenAPI)
	}
	if spec.Info.Title == "" {
		t.Error("expected non-empty title")
	}
	if spec.Info.Version == "" {
		t.Error("expected non-empty version")
	}
	if len(spec.Paths) == 0 {
		t.Error("expected at least one path in spec")
	}
}

func TestRunAPIExtractYAMLOutput(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeTestConfig(t, dir, "config.yaml", configWithPipelines)
	outPath := filepath.Join(dir, "openapi.yaml")

	err := runAPIExtract([]string{"-format", "yaml", "-output", outPath, cfgPath})
	if err != nil {
		t.Fatalf("api extract failed: %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}

	var spec module.OpenAPISpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		t.Fatalf("output is not valid YAML: %v", err)
	}

	if spec.OpenAPI != "3.0.3" {
		t.Errorf("expected OpenAPI 3.0.3, got %q", spec.OpenAPI)
	}
}

func TestRunAPIExtractPipelineEndpoints(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeTestConfig(t, dir, "config.yaml", configWithPipelines)
	outPath := filepath.Join(dir, "openapi.json")

	err := runAPIExtract([]string{"-output", outPath, cfgPath})
	if err != nil {
		t.Fatalf("api extract failed: %v", err)
	}

	data, _ := os.ReadFile(outPath)
	var spec module.OpenAPISpec
	json.Unmarshal(data, &spec) //nolint:errcheck

	// Check that pipeline HTTP endpoints are in the spec
	if _, ok := spec.Paths["/api/v1/users"]; !ok {
		t.Error("expected /api/v1/users in spec paths")
	}
	if _, ok := spec.Paths["/api/v1/login"]; !ok {
		t.Error("expected /api/v1/login in spec paths")
	}
	if _, ok := spec.Paths["/healthz"]; !ok {
		t.Error("expected /healthz in spec paths")
	}
}

func TestRunAPIExtractWorkflowRoutes(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeTestConfig(t, dir, "config.yaml", configWithWorkflowRoutes)
	outPath := filepath.Join(dir, "openapi.json")

	err := runAPIExtract([]string{"-output", outPath, cfgPath})
	if err != nil {
		t.Fatalf("api extract failed: %v", err)
	}

	data, _ := os.ReadFile(outPath)
	var spec module.OpenAPISpec
	json.Unmarshal(data, &spec) //nolint:errcheck

	if _, ok := spec.Paths["/api/auth/login"]; !ok {
		t.Error("expected /api/auth/login in spec paths")
	}
	if _, ok := spec.Paths["/api/auth/profile"]; !ok {
		t.Error("expected /api/auth/profile in spec paths")
	}
	if _, ok := spec.Paths["/api/users/{id}"]; !ok {
		t.Error("expected /api/users/{id} in spec paths")
	}
}

func TestRunAPIExtractBothSources(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeTestConfig(t, dir, "config.yaml", configWithBothSourcesYAML)
	outPath := filepath.Join(dir, "openapi.json")

	err := runAPIExtract([]string{"-output", outPath, cfgPath})
	if err != nil {
		t.Fatalf("api extract failed: %v", err)
	}

	data, _ := os.ReadFile(outPath)
	var spec module.OpenAPISpec
	json.Unmarshal(data, &spec) //nolint:errcheck

	// Both workflow route and pipeline should be present
	pathItem, ok := spec.Paths["/api/items"]
	if !ok {
		t.Fatal("expected /api/items in spec paths")
	}

	// GET from workflow routes
	if pathItem.Get == nil {
		t.Error("expected GET /api/items from workflow routes")
	}
	// POST from pipeline
	if pathItem.Post == nil {
		t.Error("expected POST /api/items from pipeline")
	}
}

func TestRunAPIExtractCustomTitle(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeTestConfig(t, dir, "config.yaml", configNoPipelines)
	outPath := filepath.Join(dir, "openapi.json")

	err := runAPIExtract([]string{"-title", "My Custom API", "-version", "2.0.0", "-output", outPath, cfgPath})
	if err != nil {
		t.Fatalf("api extract failed: %v", err)
	}

	data, _ := os.ReadFile(outPath)
	var spec module.OpenAPISpec
	json.Unmarshal(data, &spec) //nolint:errcheck

	if spec.Info.Title != "My Custom API" {
		t.Errorf("expected title 'My Custom API', got %q", spec.Info.Title)
	}
	if spec.Info.Version != "2.0.0" {
		t.Errorf("expected version '2.0.0', got %q", spec.Info.Version)
	}
}

func TestRunAPIExtractWithServers(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeTestConfig(t, dir, "config.yaml", configNoPipelines)
	outPath := filepath.Join(dir, "openapi.json")

	err := runAPIExtract([]string{
		"-server", "https://api.example.com",
		"-server", "https://staging.example.com",
		"-output", outPath,
		cfgPath,
	})
	if err != nil {
		t.Fatalf("api extract failed: %v", err)
	}

	data, _ := os.ReadFile(outPath)
	var spec module.OpenAPISpec
	json.Unmarshal(data, &spec) //nolint:errcheck

	if len(spec.Servers) != 2 {
		t.Errorf("expected 2 servers, got %d", len(spec.Servers))
	}
	if spec.Servers[0].URL != "https://api.example.com" {
		t.Errorf("expected first server URL 'https://api.example.com', got %q", spec.Servers[0].URL)
	}
}

func TestRunAPIExtractStdout(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeTestConfig(t, dir, "config.yaml", configWithPipelines)

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runAPIExtract([]string{cfgPath})

	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("api extract to stdout failed: %v", err)
	}

	var buf strings.Builder
	readBuf := make([]byte, 4096)
	for {
		n, readErr := r.Read(readBuf)
		buf.Write(readBuf[:n])
		if readErr != nil {
			break
		}
	}

	output := buf.String()
	if !strings.Contains(output, `"openapi"`) {
		t.Errorf("expected JSON output with 'openapi' key, got: %s", output[:min(len(output), 200)])
	}
}

func TestRunAPIExtractInvalidFormat(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeTestConfig(t, dir, "config.yaml", configNoPipelines)

	err := runAPIExtract([]string{"-format", "xml", cfgPath})
	if err == nil {
		t.Fatal("expected error for unsupported format")
	}
	if !strings.Contains(err.Error(), "unsupported format") {
		t.Errorf("expected 'unsupported format' error, got: %v", err)
	}
}

func TestRunAPIExtractInvalidConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeTestConfig(t, dir, "config.yaml", "not: valid: yaml: {{{")

	err := runAPIExtract([]string{cfgPath})
	if err == nil {
		t.Fatal("expected error for invalid config")
	}
}

func TestRunAPIExtractMissingConfigFile(t *testing.T) {
	err := runAPIExtract([]string{"/nonexistent/path/config.yaml"})
	if err == nil {
		t.Fatal("expected error for missing config file")
	}
}

func TestRunAPIExtractSchemaInference(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeTestConfig(t, dir, "config.yaml", configWithPipelines)
	outPath := filepath.Join(dir, "openapi.json")

	err := runAPIExtract([]string{"-include-schemas=true", "-output", outPath, cfgPath})
	if err != nil {
		t.Fatalf("api extract failed: %v", err)
	}

	data, _ := os.ReadFile(outPath)
	var spec module.OpenAPISpec
	json.Unmarshal(data, &spec) //nolint:errcheck

	// Check that user register endpoint has request body
	usersPath, ok := spec.Paths["/api/v1/users"]
	if !ok {
		t.Fatal("expected /api/v1/users in spec")
	}
	if usersPath.Post == nil {
		t.Fatal("expected POST /api/v1/users")
	}
	if usersPath.Post.RequestBody == nil {
		t.Error("expected request body for POST /api/v1/users")
	}
}

func TestRunAPIExtractNoSchemaInference(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeTestConfig(t, dir, "config.yaml", configWithPipelines)
	outPath := filepath.Join(dir, "openapi.json")

	err := runAPIExtract([]string{"-include-schemas=false", "-output", outPath, cfgPath})
	if err != nil {
		t.Fatalf("api extract failed: %v", err)
	}

	data, _ := os.ReadFile(outPath)
	if len(data) == 0 {
		t.Fatal("expected non-empty output")
	}
	var spec module.OpenAPISpec
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
}

func TestRunAPIExtractDefaultTitle(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeTestConfig(t, dir, "config.yaml", configNoPipelines)
	outPath := filepath.Join(dir, "openapi.json")

	err := runAPIExtract([]string{"-output", outPath, cfgPath})
	if err != nil {
		t.Fatalf("api extract failed: %v", err)
	}

	data, _ := os.ReadFile(outPath)
	var spec module.OpenAPISpec
	json.Unmarshal(data, &spec) //nolint:errcheck

	// Should fall back to "Workflow API" when no title is provided and none can be inferred
	if spec.Info.Title == "" {
		t.Error("expected non-empty default title")
	}
}

func TestInferValidateSchema(t *testing.T) {
	schema := &module.OpenAPISchema{
		Type:       "object",
		Properties: make(map[string]*module.OpenAPISchema),
	}
	stepCfg := map[string]any{
		"rules": map[string]any{
			"email":    "required,email",
			"password": "required,min=8",
			"age":      "numeric",
		},
	}
	inferValidateSchema(schema, stepCfg)

	if _, ok := schema.Properties["email"]; !ok {
		t.Error("expected email property")
	}
	if schema.Properties["email"].Format != "email" {
		t.Errorf("expected email format, got %q", schema.Properties["email"].Format)
	}
	if _, ok := schema.Properties["password"]; !ok {
		t.Error("expected password property")
	}
	if _, ok := schema.Properties["age"]; !ok {
		t.Error("expected age property")
	}
	if schema.Properties["age"].Type != "number" {
		t.Errorf("expected number type for age, got %q", schema.Properties["age"].Type)
	}

	// Check required fields
	requiredSet := make(map[string]bool)
	for _, r := range schema.Required {
		requiredSet[r] = true
	}
	if !requiredSet["email"] {
		t.Error("expected email in required")
	}
	if !requiredSet["password"] {
		t.Error("expected password in required")
	}
}

func TestParsePipelineEndpointNoHTTPTrigger(t *testing.T) {
	pipelineMap := map[string]any{
		"trigger": map[string]any{
			"type": "schedule",
			"config": map[string]any{
				"cron": "0 * * * *",
			},
		},
	}
	ep := parsePipelineEndpoint("my-pipeline", pipelineMap, false)
	if ep != nil {
		t.Error("expected nil for non-HTTP trigger")
	}
}

func TestParsePipelineEndpointHTTPTrigger(t *testing.T) {
	pipelineMap := map[string]any{
		"trigger": map[string]any{
			"type": "http",
			"config": map[string]any{
				"path":   "/api/test",
				"method": "POST",
			},
		},
		"steps": []any{
			map[string]any{"type": "step.json_response"},
		},
	}
	ep := parsePipelineEndpoint("test-pipeline", pipelineMap, true)
	if ep == nil {
		t.Fatal("expected non-nil endpoint for HTTP trigger")
	}
	if ep.path != "/api/test" {
		t.Errorf("expected path '/api/test', got %q", ep.path)
	}
	if ep.method != "POST" {
		t.Errorf("expected method 'POST', got %q", ep.method)
	}
	if ep.name != "test-pipeline" {
		t.Errorf("expected name 'test-pipeline', got %q", ep.name)
	}
	if len(ep.steps) != 1 {
		t.Errorf("expected 1 step, got %d", len(ep.steps))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
