package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

// --- Test YAML fixtures ---

const testConfigYAML = `
modules:
  - name: server
    type: http.server
    config:
      address: ":8080"
  - name: router
    type: http.router
    dependsOn: [server]
  - name: db
    type: database.workflow
    config:
      driver: postgres
      dsn: "postgres://localhost/test"
  - name: auth
    type: auth.jwt
    config:
      secret: test-secret

pipelines:
  get-users:
    trigger:
      type: http
      config:
        method: GET
        path: /api/users
    steps:
      - name: parse
        type: step.request_parse
        config: {}
      - name: query
        type: step.db_query
        config:
          database: db
          query: "SELECT * FROM users"
      - name: respond
        type: step.json_response
        config:
          status: 200
          body: "{{ .steps.query.rows }}"

  create-user:
    trigger:
      type: http
      config:
        method: POST
        path: /api/users
    steps:
      - name: auth-check
        type: step.auth_required
        config: {}
      - name: validate
        type: step.validate
        config:
          rules:
            email: "required,email"
            name: "required"
      - name: insert
        type: step.db_exec
        config:
          database: db
          query: "INSERT INTO users (email, name) VALUES ($1, $2)"
      - name: respond
        type: step.json_response
        config:
          status: 201

  on-user-created:
    trigger:
      type: event
      config:
        topic: user.created
    steps:
      - name: notify
        type: step.publish
        config:
          topic: notifications
          payload: "User created"
`

const testConfigSimpleYAML = `
modules:
  - name: server
    type: http.server
    config:
      address: ":8080"
`

// --- API Extract Tests ---

func TestAPIExtract(t *testing.T) {
	srv := NewServer("")
	req := makeCallToolRequest(map[string]any{
		"yaml_content": testConfigYAML,
	})

	result, err := srv.handleAPIExtract(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse result JSON: %v", err)
	}

	if data["openapi"] == nil {
		t.Error("expected openapi field in spec")
	}
	if data["paths"] == nil {
		t.Error("expected paths field in spec")
	}

	paths, _ := data["paths"].(map[string]any)
	if paths["/api/users"] == nil {
		t.Error("expected /api/users path in spec")
	}
}

func TestAPIExtract_WithTitle(t *testing.T) {
	srv := NewServer("")
	req := makeCallToolRequest(map[string]any{
		"yaml_content": testConfigSimpleYAML,
		"title":        "My Custom API",
		"version":      "2.0.0",
	})

	result, err := srv.handleAPIExtract(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	if !contains(text, "My Custom API") {
		t.Error("expected custom title in spec")
	}
	if !contains(text, "2.0.0") {
		t.Error("expected custom version in spec")
	}
}

func TestAPIExtract_MissingContent(t *testing.T) {
	srv := NewServer("")
	req := makeCallToolRequest(map[string]any{})
	result, err := srv.handleAPIExtract(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := extractText(t, result)
	if !contains(text, "yaml_content is required") {
		t.Errorf("expected error message, got %q", text)
	}
}

func TestAPIExtract_MalformedYAML(t *testing.T) {
	srv := NewServer("")
	req := makeCallToolRequest(map[string]any{
		"yaml_content": "{{invalid yaml",
	})
	result, err := srv.handleAPIExtract(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := extractText(t, result)
	if !contains(text, "YAML parse error") {
		t.Errorf("expected YAML parse error, got %q", text)
	}
}

// --- Diff Tests ---

func TestDiffConfigs_AddedModule(t *testing.T) {
	srv := NewServer("")

	oldYAML := `
modules:
  - name: server
    type: http.server
`
	newYAML := `
modules:
  - name: server
    type: http.server
  - name: db
    type: database.workflow
    config:
      driver: postgres
`

	req := makeCallToolRequest(map[string]any{
		"old_yaml": oldYAML,
		"new_yaml": newYAML,
	})

	result, err := srv.handleDiffConfigs(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var data mcpDiffResult
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if len(data.Modules) != 2 {
		t.Fatalf("expected 2 module diffs, got %d", len(data.Modules))
	}

	// Find the db module
	var dbDiff mcpModuleDiff
	for _, m := range data.Modules {
		if m.Name == "db" {
			dbDiff = m
			break
		}
	}
	if dbDiff.Status != "added" {
		t.Errorf("expected db status=added, got %q", dbDiff.Status)
	}
	if !dbDiff.Stateful {
		t.Error("database.workflow should be marked as stateful")
	}
}

func TestDiffConfigs_RemovedStateful(t *testing.T) {
	srv := NewServer("")

	oldYAML := `
modules:
  - name: db
    type: database.workflow
    config:
      driver: postgres
`
	newYAML := `
modules: []
`

	req := makeCallToolRequest(map[string]any{
		"old_yaml": oldYAML,
		"new_yaml": newYAML,
	})

	result, err := srv.handleDiffConfigs(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var data mcpDiffResult
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if len(data.Modules) != 1 {
		t.Fatalf("expected 1 module diff, got %d", len(data.Modules))
	}
	if data.Modules[0].Status != "removed" {
		t.Errorf("expected removed, got %q", data.Modules[0].Status)
	}
	if !contains(data.Modules[0].Detail, "stateful") {
		t.Error("expected stateful warning in detail")
	}
}

func TestDiffConfigs_BreakingChange(t *testing.T) {
	srv := NewServer("")

	oldYAML := `
modules:
  - name: db
    type: database.workflow
    config:
      driver: postgres
      dsn: "postgres://localhost/old"
`
	newYAML := `
modules:
  - name: db
    type: database.workflow
    config:
      driver: postgres
      dsn: "postgres://localhost/new"
`

	req := makeCallToolRequest(map[string]any{
		"old_yaml": oldYAML,
		"new_yaml": newYAML,
	})

	result, err := srv.handleDiffConfigs(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var data mcpDiffResult
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if len(data.BreakingChanges) == 0 {
		t.Error("expected breaking changes for DSN change")
	}
	if data.Modules[0].Status != "changed" {
		t.Errorf("expected changed status, got %q", data.Modules[0].Status)
	}
}

func TestDiffConfigs_PipelineChanged(t *testing.T) {
	srv := NewServer("")

	oldYAML := `
modules: []
pipelines:
  get-users:
    trigger:
      type: http
      config:
        method: GET
        path: /api/users
    steps:
      - name: respond
        type: step.json_response
`
	newYAML := `
modules: []
pipelines:
  get-users:
    trigger:
      type: http
      config:
        method: POST
        path: /api/users
    steps:
      - name: respond
        type: step.json_response
`

	req := makeCallToolRequest(map[string]any{
		"old_yaml": oldYAML,
		"new_yaml": newYAML,
	})

	result, err := srv.handleDiffConfigs(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var data mcpDiffResult
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if len(data.Pipelines) != 1 {
		t.Fatalf("expected 1 pipeline diff, got %d", len(data.Pipelines))
	}
	if data.Pipelines[0].Status != "changed" {
		t.Errorf("expected changed, got %q", data.Pipelines[0].Status)
	}
	if !contains(data.Pipelines[0].Detail, "TRIGGER CHANGED") {
		t.Error("expected trigger changed detail")
	}
}

func TestDiffConfigs_MissingOld(t *testing.T) {
	srv := NewServer("")
	req := makeCallToolRequest(map[string]any{
		"new_yaml": testConfigSimpleYAML,
	})
	result, err := srv.handleDiffConfigs(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := extractText(t, result)
	if !contains(text, "old_yaml is required") {
		t.Errorf("expected error, got %q", text)
	}
}

// --- Manifest Tests ---

func TestManifestAnalyze(t *testing.T) {
	srv := NewServer("")
	req := makeCallToolRequest(map[string]any{
		"yaml_content": testConfigYAML,
		"name":         "test-app",
	})

	result, err := srv.handleManifestAnalyze(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if data["name"] != "test-app" {
		t.Errorf("expected name=test-app, got %v", data["name"])
	}
	if data["databases"] == nil {
		t.Error("expected databases in manifest")
	}
}

func TestManifestAnalyze_MissingContent(t *testing.T) {
	srv := NewServer("")
	req := makeCallToolRequest(map[string]any{})
	result, err := srv.handleManifestAnalyze(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := extractText(t, result)
	if !contains(text, "yaml_content is required") {
		t.Errorf("expected error, got %q", text)
	}
}

// --- Contract Tests ---

func TestContractGenerate(t *testing.T) {
	srv := NewServer("")
	req := makeCallToolRequest(map[string]any{
		"yaml_content": testConfigYAML,
	})

	result, err := srv.handleContractGenerate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var data mcpContract
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if data.Version != "1.0" {
		t.Errorf("expected version=1.0, got %q", data.Version)
	}
	if len(data.Endpoints) < 2 {
		t.Errorf("expected at least 2 endpoints, got %d", len(data.Endpoints))
	}
	if len(data.Modules) < 3 {
		t.Errorf("expected at least 3 modules, got %d", len(data.Modules))
	}
	if len(data.Steps) == 0 {
		t.Error("expected steps in contract")
	}

	// Check auth detection
	for _, ep := range data.Endpoints {
		if ep.Path == "/api/users" && ep.Method == "POST" {
			if !ep.AuthRequired {
				t.Error("POST /api/users should require auth")
			}
		}
	}
}

func TestContractGenerate_WithBaseline(t *testing.T) {
	srv := NewServer("")

	// Generate baseline
	req := makeCallToolRequest(map[string]any{
		"yaml_content": testConfigYAML,
	})
	result, err := srv.handleContractGenerate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	baselineJSON := extractText(t, result)

	// Generate comparison with modified config (removed create-user pipeline)
	modifiedYAML := `
modules:
  - name: server
    type: http.server
  - name: db
    type: database.workflow

pipelines:
  get-users:
    trigger:
      type: http
      config:
        method: GET
        path: /api/users
    steps:
      - name: respond
        type: step.json_response
        config:
          status: 200
`

	req2 := makeCallToolRequest(map[string]any{
		"yaml_content":  modifiedYAML,
		"baseline_json": baselineJSON,
	})

	result2, err := srv.handleContractGenerate(context.Background(), req2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result2)
	var comp mcpContractComparison
	if err := json.Unmarshal([]byte(text), &comp); err != nil {
		t.Fatalf("failed to parse comparison: %v", err)
	}

	if comp.BreakingCount == 0 {
		t.Error("expected breaking changes when removing POST /api/users endpoint")
	}
}

func TestContractGenerate_Events(t *testing.T) {
	srv := NewServer("")
	req := makeCallToolRequest(map[string]any{
		"yaml_content": testConfigYAML,
	})

	result, err := srv.handleContractGenerate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var data mcpContract
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if len(data.Events) == 0 {
		t.Error("expected events in contract")
	}

	// Check for subscription and publish events
	hasSubscribe := false
	hasPublish := false
	for _, ev := range data.Events {
		if ev.Direction == "subscribe" {
			hasSubscribe = true
		}
		if ev.Direction == "publish" {
			hasPublish = true
		}
	}
	if !hasSubscribe {
		t.Error("expected subscribe event")
	}
	if !hasPublish {
		t.Error("expected publish event")
	}
}

// --- Compat Check Tests ---

func TestCompatCheck(t *testing.T) {
	srv := NewServer("")
	req := makeCallToolRequest(map[string]any{
		"yaml_content": testConfigYAML,
	})

	result, err := srv.handleCompatCheck(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var data mcpCompatResult
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if len(data.RequiredModules) == 0 {
		t.Error("expected required modules")
	}
	if len(data.RequiredSteps) == 0 {
		t.Error("expected required steps")
	}
}

func TestCompatCheck_UnknownType(t *testing.T) {
	srv := NewServer("")
	req := makeCallToolRequest(map[string]any{
		"yaml_content": `
modules:
  - name: mystery
    type: mystery.unknown.type

pipelines:
  test:
    trigger:
      type: http
      config:
        method: GET
        path: /test
    steps:
      - name: s1
        type: step.nonexistent_step
`,
	})

	result, err := srv.handleCompatCheck(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var data mcpCompatResult
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if data.Compatible {
		t.Error("expected incompatible with unknown types")
	}
	if len(data.Issues) < 2 {
		t.Errorf("expected at least 2 issues (module + step), got %d", len(data.Issues))
	}
}

func TestCompatCheck_MissingContent(t *testing.T) {
	srv := NewServer("")
	req := makeCallToolRequest(map[string]any{})
	result, err := srv.handleCompatCheck(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := extractText(t, result)
	if !contains(text, "yaml_content is required") {
		t.Errorf("expected error, got %q", text)
	}
}

// --- Template Validate Config Tests ---

func TestTemplateValidateConfig(t *testing.T) {
	srv := NewServer("")
	req := makeCallToolRequest(map[string]any{
		"yaml_content": testConfigYAML,
	})

	result, err := srv.handleTemplateValidateConfig(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if data["valid"] != true {
		t.Errorf("expected valid=true, got %v (errors: %v)", data["valid"], data["errors"])
	}

	moduleCount := data["module_count"].(float64)
	if moduleCount < 3 {
		t.Errorf("expected at least 3 modules, got %.0f", moduleCount)
	}
	stepCount := data["step_count"].(float64)
	if stepCount < 5 {
		t.Errorf("expected at least 5 steps, got %.0f", stepCount)
	}
}

func TestTemplateValidateConfig_UnknownTypes(t *testing.T) {
	srv := NewServer("")
	req := makeCallToolRequest(map[string]any{
		"yaml_content": `
modules:
  - name: m1
    type: totally.fake.module
pipelines:
  p1:
    trigger:
      type: http
      config:
        method: GET
        path: /test
    steps:
      - name: s1
        type: step.totally_fake
`,
	})

	result, err := srv.handleTemplateValidateConfig(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if data["valid"] != false {
		t.Error("expected valid=false for unknown types")
	}
	errors := data["errors"].([]any)
	if len(errors) < 2 {
		t.Errorf("expected at least 2 errors (module + step), got %d", len(errors))
	}
}

func TestTemplateValidateConfig_BrokenDependency(t *testing.T) {
	srv := NewServer("")
	req := makeCallToolRequest(map[string]any{
		"yaml_content": `
modules:
  - name: router
    type: http.router
    dependsOn: [nonexistent-server]
`,
	})

	result, err := srv.handleTemplateValidateConfig(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if data["valid"] != false {
		t.Error("expected valid=false for broken dependency")
	}
	if data["dep_valid"].(float64) != 0 {
		t.Error("expected 0 valid dependencies")
	}
}

func TestTemplateValidateConfig_Strict(t *testing.T) {
	srv := NewServer("")
	// Config with a module that has an unknown config field — should produce warning
	req := makeCallToolRequest(map[string]any{
		"yaml_content": `
modules:
  - name: server
    type: http.server
    config:
      address: ":8080"
`,
		"strict": true,
	})

	result, err := srv.handleTemplateValidateConfig(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	// Should be valid (no errors, may have warnings but address is a known field)
	// Just verify the structure is correct
	if data["module_count"] == nil {
		t.Error("expected module_count in result")
	}
}

// --- Generate GitHub Actions Tests ---

func TestGenerateGithubActions(t *testing.T) {
	srv := NewServer("")
	req := makeCallToolRequest(map[string]any{
		"yaml_content": testConfigYAML,
	})

	result, err := srv.handleGenerateGithubActions(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	ciYAML, _ := data["ci_yaml"].(string)
	if ciYAML == "" {
		t.Error("expected ci_yaml in result")
	}
	if !contains(ciYAML, "name: CI") {
		t.Error("CI workflow should have name")
	}

	cdYAML, _ := data["cd_yaml"].(string)
	if cdYAML == "" {
		t.Error("expected cd_yaml in result")
	}
	if !contains(cdYAML, "name: CD") {
		t.Error("CD workflow should have name")
	}

	features, _ := data["features"].(map[string]any)
	if features == nil {
		t.Fatal("expected features in result")
	}
	if features["hasHTTP"] != true {
		t.Error("expected hasHTTP=true")
	}
	if features["hasDatabase"] != true {
		t.Error("expected hasDatabase=true")
	}
	if features["hasAuth"] != true {
		t.Error("expected hasAuth=true")
	}
}

func TestGenerateGithubActions_WithAuth(t *testing.T) {
	srv := NewServer("")
	req := makeCallToolRequest(map[string]any{
		"yaml_content": testConfigYAML,
	})

	result, err := srv.handleGenerateGithubActions(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	ciYAML, _ := data["ci_yaml"].(string)
	if !contains(ciYAML, "JWT_SECRET") {
		t.Error("CI workflow should include JWT_SECRET for auth projects")
	}
}

func TestGenerateGithubActions_WithDatabase(t *testing.T) {
	srv := NewServer("")
	req := makeCallToolRequest(map[string]any{
		"yaml_content": testConfigYAML,
	})

	result, err := srv.handleGenerateGithubActions(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	ciYAML, _ := data["ci_yaml"].(string)
	if !contains(ciYAML, "migrations") {
		t.Error("CI workflow should include migration step for database projects")
	}
}

func TestGenerateGithubActions_CustomRegistry(t *testing.T) {
	srv := NewServer("")
	req := makeCallToolRequest(map[string]any{
		"yaml_content": testConfigSimpleYAML,
		"registry":     "docker.io",
		"platforms":    "linux/arm64",
	})

	result, err := srv.handleGenerateGithubActions(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	cdYAML, _ := data["cd_yaml"].(string)
	if !contains(cdYAML, "docker.io") {
		t.Error("CD workflow should use custom registry")
	}
	if !contains(cdYAML, "linux/arm64") {
		t.Error("CD workflow should use custom platforms")
	}
}

func TestGenerateGithubActions_MissingContent(t *testing.T) {
	srv := NewServer("")
	req := makeCallToolRequest(map[string]any{})
	result, err := srv.handleGenerateGithubActions(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := extractText(t, result)
	if !contains(text, "yaml_content is required") {
		t.Errorf("expected error, got %q", text)
	}
}

// --- Detect Project Features Tests ---

func TestDetectProjectFeatures(t *testing.T) {
	srv := NewServer("")
	req := makeCallToolRequest(map[string]any{
		"yaml_content": testConfigYAML,
	})

	result, err := srv.handleDetectProjectFeatures(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var features mcpProjectFeatures
	if err := json.Unmarshal([]byte(text), &features); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if !features.HasHTTP {
		t.Error("expected hasHTTP=true")
	}
	if !features.HasDatabase {
		t.Error("expected hasDatabase=true")
	}
	if !features.HasAuth {
		t.Error("expected hasAuth=true")
	}
	if features.HasUI {
		t.Error("expected hasUI=false (no static.fileserver)")
	}
	if len(features.ModuleTypes) == 0 {
		t.Error("expected moduleTypes to be populated")
	}
}

func TestDetectProjectFeatures_UI(t *testing.T) {
	srv := NewServer("")
	req := makeCallToolRequest(map[string]any{
		"yaml_content": `
modules:
  - name: server
    type: http.server
  - name: ui
    type: static.fileserver
    config:
      root: ./dist
`,
	})

	result, err := srv.handleDetectProjectFeatures(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	text := extractText(t, result)
	var features mcpProjectFeatures
	if err := json.Unmarshal([]byte(text), &features); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if !features.HasUI {
		t.Error("expected hasUI=true for static.fileserver")
	}
	if !features.HasHTTP {
		t.Error("expected hasHTTP=true for http.server")
	}
}

func TestDetectProjectFeatures_MissingContent(t *testing.T) {
	srv := NewServer("")
	req := makeCallToolRequest(map[string]any{})
	result, err := srv.handleDetectProjectFeatures(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := extractText(t, result)
	if !contains(text, "yaml_content is required") {
		t.Errorf("expected error, got %q", text)
	}
}
