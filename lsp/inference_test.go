package lsp

import (
	"os"
	"path/filepath"
	"testing"
)

// inferenceYAML has two pipelines so we can test cursor-based selection.
const inferenceYAML = `modules:
  - name: server
    type: http.server
    config:
      address: :8080

pipelines:
  api-pipeline:
    trigger:
      type: http
      method: GET
      path: /items
    steps:
      - name: parse
        type: step.request_parse
        config:
          path_params: [id]
          query_params: [filter]
      - name: query
        type: step.db_query
        config:
          database: mydb
          query: "SELECT * FROM items WHERE id = $1"
          mode: list
      - name: respond
        type: step.json_response
        config:
          status: 200
          body_from: "{{step \"query\" \"rows\"}}"

  other-pipeline:
    steps:
      - name: init
        type: step.set
        config:
          values:
            foo: bar
`

func makeDoc(t *testing.T, content string) (*Registry, *Document) {
	t.Helper()
	reg := NewRegistry()
	store := NewDocumentStore()
	doc := store.Set("file:///test.yaml", content)
	return reg, doc
}

// TestBuildPipelineContext_Basic verifies step output collection for the pipeline
// containing the cursor. Cursor is inside "respond" step so parse+query are included.
func TestBuildPipelineContext_Basic(t *testing.T) {
	reg, doc := makeDoc(t, inferenceYAML)

	// In inferenceYAML (cat -n shows 1-based):
	//   yaml line 8 = "  api-pipeline:" (ctx.Line=7)
	//   yaml line 14 = "      - name: parse" (ctx.Line=13)
	//   yaml line 19 = "      - name: query" (ctx.Line=18)
	//   yaml line 25 = "      - name: respond" (ctx.Line=24)
	//   yaml line 31 = "  other-pipeline:" (ctx.Line=30)
	// Cursor at ctx.Line=28 (yaml line 29) is inside "respond" but before "other-pipeline".
	ctx := BuildPipelineContext(reg, doc, 28)
	if ctx == nil {
		t.Fatal("expected non-nil context")
	}
	if ctx.PipelineName != "api-pipeline" {
		t.Errorf("expected pipelineName api-pipeline, got %q", ctx.PipelineName)
	}
	if len(ctx.StepOrder) != 2 {
		t.Fatalf("expected 2 preceding steps (parse, query), got %d: %v", len(ctx.StepOrder), ctx.StepOrder)
	}
	if ctx.StepOrder[0] != "parse" || ctx.StepOrder[1] != "query" {
		t.Errorf("unexpected step order: %v", ctx.StepOrder)
	}
	if ctx.Steps["parse"] == nil || ctx.Steps["query"] == nil {
		t.Error("expected parse and query in Steps map")
	}
}

// TestBuildPipelineContext_CursorBased verifies that cursor line selects the correct pipeline.
func TestBuildPipelineContext_CursorBased(t *testing.T) {
	reg, doc := makeDoc(t, inferenceYAML)

	// "other-pipeline:" is at yaml line 31 (ctx.Line=30).
	// Cursor at ctx.Line=34 is inside other-pipeline's steps.
	ctx := BuildPipelineContext(reg, doc, 34)
	if ctx == nil {
		t.Fatal("expected non-nil context")
	}
	if ctx.PipelineName != "other-pipeline" {
		t.Errorf("expected other-pipeline, got %q", ctx.PipelineName)
	}
}

// TestBuildPipelineContext_StepFields verifies that step fields use []schema.InferredOutput.
func TestBuildPipelineContext_StepFields(t *testing.T) {
	reg, doc := makeDoc(t, inferenceYAML)

	// Cursor inside respond step (ctx.Line=28); parse+query should be in Steps.
	ctx := BuildPipelineContext(reg, doc, 28)
	query := ctx.Steps["query"]
	if query == nil {
		t.Fatal("expected query step in context")
	}
	fieldKeys := map[string]bool{}
	for _, f := range query.Fields {
		fieldKeys[f.Key] = true
	}
	if !fieldKeys["rows"] || !fieldKeys["count"] {
		t.Errorf("expected rows and count in query step fields, got %v", query.Fields)
	}
}

// TestBuildPipelineContext_SetStep verifies step.set output inference.
func TestBuildPipelineContext_SetStep(t *testing.T) {
	yml := `pipelines:
  p:
    steps:
      - name: init
        type: step.set
        config:
          values:
            user_id: "123"
            role: admin
      - name: respond
        type: step.json_response
        config:
          status: 200
`
	reg, doc := makeDoc(t, yml)
	// Cursor inside respond (line 11+).
	ctx := BuildPipelineContext(reg, doc, 12)
	if len(ctx.StepOrder) != 1 || ctx.StepOrder[0] != "init" {
		t.Fatalf("expected [init], got %v", ctx.StepOrder)
	}
	init := ctx.Steps["init"]
	if init == nil {
		t.Fatal("expected init step")
	}
	fieldKeys := map[string]bool{}
	for _, f := range init.Fields {
		fieldKeys[f.Key] = true
	}
	if !fieldKeys["user_id"] || !fieldKeys["role"] {
		t.Errorf("expected user_id and role, got %v", init.Fields)
	}
}

// TestBuildPipelineContext_NoOpenAPI verifies that a generic http trigger is returned
// when no openapi module is present.
func TestBuildPipelineContext_NoOpenAPI(t *testing.T) {
	yml := `pipelines:
  p:
    steps:
      - name: a
        type: step.set
        config:
          values:
            x: "1"
      - name: b
        type: step.json_response
        config:
          status: 200
`
	reg, doc := makeDoc(t, yml)
	ctx := BuildPipelineContext(reg, doc, 11)
	if ctx.Trigger == nil {
		t.Fatal("expected trigger to be set")
	}
	if ctx.Trigger.Type != "http" {
		t.Errorf("expected generic http trigger, got %q", ctx.Trigger.Type)
	}
}

// TestBuildPipelineContext_OpenAPITrigger tests OpenAPI auto-discovery via modules section.
func TestBuildPipelineContext_OpenAPITrigger(t *testing.T) {
	specYAML := `
openapi: "3.0.0"
info:
  title: Test API
  version: "1.0"
paths:
  /items/{id}:
    post:
      parameters:
        - name: id
          in: path
          schema:
            type: integer
        - name: filter
          in: query
          schema:
            type: string
        - name: Authorization
          in: header
          schema:
            type: string
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              properties:
                name:
                  type: string
                  description: Item name
                quantity:
                  type: integer
`
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "spec.yaml")
	if err := os.WriteFile(specPath, []byte(specYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	yml := `modules:
  - name: api
    type: openapi
    config:
      spec_file: ` + specPath + `

pipelines:
  create-item:
    trigger:
      type: http
      method: POST
      path: /items/{id}
    steps:
      - name: parse
        type: step.request_parse
        config: {}
      - name: respond
        type: step.json_response
        config:
          status: 200
`
	reg, doc := makeDoc(t, yml)
	// Cursor inside respond step.
	ctx := BuildPipelineContext(reg, doc, 20)
	if ctx.Trigger == nil {
		t.Fatal("expected trigger schema")
	}
	if ctx.Trigger.Type != "http" {
		t.Errorf("expected http trigger, got %q", ctx.Trigger.Type)
	}

	// Check path params.
	ppNames := map[string]bool{}
	for _, p := range ctx.Trigger.PathParams {
		ppNames[p.Name] = true
	}
	if !ppNames["id"] {
		t.Errorf("expected path param 'id', got %v", ctx.Trigger.PathParams)
	}

	// Check query params.
	qpNames := map[string]bool{}
	for _, p := range ctx.Trigger.QueryParams {
		qpNames[p.Name] = true
	}
	if !qpNames["filter"] {
		t.Errorf("expected query param 'filter', got %v", ctx.Trigger.QueryParams)
	}

	// Check headers.
	hNames := map[string]bool{}
	for _, h := range ctx.Trigger.Headers {
		hNames[h.Name] = true
	}
	if !hNames["Authorization"] {
		t.Errorf("expected header 'Authorization', got %v", ctx.Trigger.Headers)
	}

	// Check body fields.
	bfNames := map[string]bool{}
	for _, b := range ctx.Trigger.BodyFields {
		bfNames[b.Name] = true
	}
	if !bfNames["name"] || !bfNames["quantity"] {
		t.Errorf("expected body fields name and quantity, got %v", ctx.Trigger.BodyFields)
	}
}

// TestBuildPipelineContext_UnknownPipeline verifies empty context for cursor outside any pipeline.
func TestBuildPipelineContext_UnknownPipeline(t *testing.T) {
	yml := `modules:
  - name: server
    type: http.server
`
	reg, doc := makeDoc(t, yml)
	ctx := BuildPipelineContext(reg, doc, 2)
	if ctx == nil {
		t.Fatal("expected non-nil context")
	}
	if ctx.PipelineName != "" {
		t.Errorf("expected empty pipeline name, got %q", ctx.PipelineName)
	}
	if len(ctx.StepOrder) != 0 {
		t.Errorf("expected 0 steps, got %d", len(ctx.StepOrder))
	}
}

// TestBuildPipelineContext_InvalidYAML verifies graceful handling of bad YAML.
func TestBuildPipelineContext_InvalidYAML(t *testing.T) {
	reg := NewRegistry()
	store := NewDocumentStore()
	// Store with invalid YAML still creates a doc (Node may be nil).
	doc := store.Set("file:///bad.yaml", "{invalid yaml: [")
	ctx := BuildPipelineContext(reg, doc, 0)
	if ctx == nil {
		t.Fatal("expected non-nil context for invalid YAML")
	}
	if len(ctx.StepOrder) != 0 {
		t.Errorf("expected 0 steps for invalid YAML")
	}
}

// TestBuildPipelineContext_NilDoc verifies nil doc returns empty context.
func TestBuildPipelineContext_NilDoc(t *testing.T) {
	reg := NewRegistry()
	ctx := BuildPipelineContext(reg, nil, 0)
	if ctx == nil {
		t.Fatal("expected non-nil context for nil doc")
	}
}
