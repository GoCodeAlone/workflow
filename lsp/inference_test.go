package lsp

import (
	"os"
	"path/filepath"
	"testing"
)

const inferenceTestYAML = `
pipelines:
  api-pipeline:
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
`

func TestBuildPipelineContext_Basic(t *testing.T) {
	ctx := BuildPipelineContext(inferenceTestYAML, "api-pipeline", "", "", "", "")
	if ctx == nil {
		t.Fatal("expected non-nil context")
	}
	if ctx.PipelineName != "api-pipeline" {
		t.Errorf("expected pipelineName api-pipeline, got %q", ctx.PipelineName)
	}
	if len(ctx.StepOutputs) != 3 {
		t.Fatalf("expected 3 step outputs, got %d", len(ctx.StepOutputs))
	}

	// First step: request_parse → path_params, query, body, headers
	parse := ctx.StepOutputs[0]
	if parse.StepName != "parse" {
		t.Errorf("expected step name 'parse', got %q", parse.StepName)
	}
	if parse.StepType != "step.request_parse" {
		t.Errorf("expected step type 'step.request_parse', got %q", parse.StepType)
	}
	for _, key := range []string{"path_params", "query", "body", "headers"} {
		if _, ok := parse.Outputs[key]; !ok {
			t.Errorf("expected output key %q in parse step", key)
		}
	}

	// Second step: db_query list mode → rows, count
	query := ctx.StepOutputs[1]
	if query.StepName != "query" {
		t.Errorf("expected step name 'query', got %q", query.StepName)
	}
	for _, key := range []string{"rows", "count"} {
		if _, ok := query.Outputs[key]; !ok {
			t.Errorf("expected output key %q in query step", key)
		}
	}
}

func TestBuildPipelineContext_UpToStep(t *testing.T) {
	ctx := BuildPipelineContext(inferenceTestYAML, "api-pipeline", "query", "", "", "")
	if ctx == nil {
		t.Fatal("expected non-nil context")
	}
	// Should include only 'parse' step (stops before 'query')
	if len(ctx.StepOutputs) != 1 {
		t.Fatalf("expected 1 step output, got %d", len(ctx.StepOutputs))
	}
	if ctx.StepOutputs[0].StepName != "parse" {
		t.Errorf("expected parse step, got %q", ctx.StepOutputs[0].StepName)
	}
}

func TestBuildPipelineContext_UnknownPipeline(t *testing.T) {
	ctx := BuildPipelineContext(inferenceTestYAML, "nonexistent", "", "", "", "")
	if ctx == nil {
		t.Fatal("expected non-nil context even for unknown pipeline")
	}
	if len(ctx.StepOutputs) != 0 {
		t.Errorf("expected 0 step outputs for unknown pipeline, got %d", len(ctx.StepOutputs))
	}
}

func TestBuildPipelineContext_InvalidYAML(t *testing.T) {
	ctx := BuildPipelineContext("{invalid yaml: [", "pipeline", "", "", "", "")
	if ctx == nil {
		t.Fatal("expected non-nil context even for invalid YAML")
	}
	if len(ctx.StepOutputs) != 0 {
		t.Errorf("expected 0 steps for invalid YAML")
	}
}

func TestBuildPipelineContext_SetStep(t *testing.T) {
	yml := `
pipelines:
  p:
    steps:
      - name: init
        type: step.set
        config:
          values:
            user_id: "123"
            role: admin
`
	ctx := BuildPipelineContext(yml, "p", "", "", "", "")
	if len(ctx.StepOutputs) != 1 {
		t.Fatalf("expected 1 step output, got %d", len(ctx.StepOutputs))
	}
	outputs := ctx.StepOutputs[0].Outputs
	if _, ok := outputs["user_id"]; !ok {
		t.Error("expected user_id output from step.set")
	}
	if _, ok := outputs["role"]; !ok {
		t.Error("expected role output from step.set")
	}
}

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
	// Write spec to a temp file.
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "spec.yaml")
	if err := os.WriteFile(specPath, []byte(specYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := BuildPipelineContext(inferenceTestYAML, "api-pipeline", "query", specPath, "POST", "/items/{id}")
	if ctx == nil {
		t.Fatal("expected non-nil context")
	}
	if ctx.Trigger == nil {
		t.Fatal("expected trigger schema to be populated")
	}
	if ctx.Trigger.Type != "http" {
		t.Errorf("expected trigger type 'http', got %q", ctx.Trigger.Type)
	}
	if _, ok := ctx.Trigger.PathParams["id"]; !ok {
		t.Error("expected path param 'id'")
	}
	if _, ok := ctx.Trigger.QueryParams["filter"]; !ok {
		t.Error("expected query param 'filter'")
	}
	if _, ok := ctx.Trigger.BodyFields["name"]; !ok {
		t.Error("expected body field 'name'")
	}
	if _, ok := ctx.Trigger.BodyFields["quantity"]; !ok {
		t.Error("expected body field 'quantity'")
	}
}

func TestBuildPipelineContext_OpenAPITrigger_MissingFile(t *testing.T) {
	ctx := BuildPipelineContext(inferenceTestYAML, "api-pipeline", "", "/nonexistent/spec.yaml", "GET", "/items")
	if ctx == nil {
		t.Fatal("expected non-nil context")
	}
	if ctx.Trigger != nil {
		t.Error("expected nil trigger for missing spec file")
	}
}

func TestBuildPipelineContext_OpenAPITrigger_MissingPath(t *testing.T) {
	specYAML := `
openapi: "3.0.0"
paths:
  /other:
    get: {}
`
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "spec.yaml")
	if err := os.WriteFile(specPath, []byte(specYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := BuildPipelineContext(inferenceTestYAML, "api-pipeline", "", specPath, "GET", "/items")
	if ctx.Trigger != nil {
		t.Error("expected nil trigger for missing path in spec")
	}
}
