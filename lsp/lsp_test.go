package lsp

import (
	"fmt"
	"testing"

	protocol "github.com/tliron/glsp/protocol_3_16"
)

const testYAML = `modules:
  - name: server
    type: http.server
    config:
      address: :8080
  - name: router
    type: http.router
    dependsOn:
      - server
  - name: mymod
    type: nonexistent.module

triggers:
  http:
    port: 8080
  badtrigger:
    foo: bar
`

// TestRegistry_ModuleTypes checks that the registry loads module types.
func TestRegistry_ModuleTypes(t *testing.T) {
	reg := NewRegistry()
	if len(reg.ModuleTypes) == 0 {
		t.Fatal("registry has no module types")
	}
	// http.server must be registered.
	info, ok := reg.ModuleTypes["http.server"]
	if !ok {
		t.Fatal("http.server not in registry")
	}
	if info.Type != "http.server" {
		t.Errorf("unexpected type: %q", info.Type)
	}
	// Should have config keys.
	if len(info.ConfigKeys) == 0 {
		t.Error("http.server should have config keys")
	}
}

// TestRegistry_StepTypes checks step type registry.
func TestRegistry_StepTypes(t *testing.T) {
	reg := NewRegistry()
	if len(reg.StepTypes) == 0 {
		t.Fatal("registry has no step types")
	}
	if _, ok := reg.StepTypes["step.set"]; !ok {
		t.Error("step.set not in step type registry")
	}
}

// TestRegistry_TriggerTypes checks trigger type registry.
func TestRegistry_TriggerTypes(t *testing.T) {
	reg := NewRegistry()
	if _, ok := reg.TriggerTypes["http"]; !ok {
		t.Error("http trigger not in registry")
	}
	if _, ok := reg.TriggerTypes["schedule"]; !ok {
		t.Error("schedule trigger not in registry")
	}
}

// TestDocumentStore_SetGet checks basic document store operations.
func TestDocumentStore_SetGet(t *testing.T) {
	store := NewDocumentStore()
	doc := store.Set("file:///test.yaml", testYAML)
	if doc == nil {
		t.Fatal("Set returned nil")
	}
	got := store.Get("file:///test.yaml")
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.Content != testYAML {
		t.Error("content mismatch")
	}
}

// TestDocumentStore_ParseYAML checks that YAML is parsed on Set.
func TestDocumentStore_ParseYAML(t *testing.T) {
	store := NewDocumentStore()
	doc := store.Set("file:///test.yaml", testYAML)
	if doc.Node == nil {
		t.Fatal("document should have parsed YAML node")
	}
}

// TestDiagnostics_UnknownModuleType checks that unknown module types produce errors.
func TestDiagnostics_UnknownModuleType(t *testing.T) {
	reg := NewRegistry()
	store := NewDocumentStore()
	doc := store.Set("file:///test.yaml", testYAML)

	diags := Diagnostics(reg, doc)

	found := false
	for _, d := range diags {
		if containsStr(d.Message, "nonexistent.module") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected diagnostic for nonexistent.module, got %d diags: %v", len(diags), diagMessages(diags))
	}
}

// TestDiagnostics_UnknownTriggerType checks that unknown trigger types produce errors.
func TestDiagnostics_UnknownTriggerType(t *testing.T) {
	reg := NewRegistry()
	store := NewDocumentStore()
	doc := store.Set("file:///test.yaml", testYAML)

	diags := Diagnostics(reg, doc)

	found := false
	for _, d := range diags {
		if containsStr(d.Message, "badtrigger") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected diagnostic for badtrigger, got: %v", diagMessages(diags))
	}
}

// TestDiagnostics_ValidConfig checks no spurious errors on valid config.
func TestDiagnostics_ValidConfig(t *testing.T) {
	validYAML := `modules:
  - name: server
    type: http.server
    config:
      address: :8080

triggers:
  http:
    port: 8080
`
	reg := NewRegistry()
	store := NewDocumentStore()
	doc := store.Set("file:///valid.yaml", validYAML)
	diags := Diagnostics(reg, doc)

	// Should have no errors (warnings for unknown config keys are ok but
	// there should be no unknown type errors).
	for _, d := range diags {
		if d.Severity != nil && *d.Severity == 1 { // DiagnosticSeverityError
			t.Errorf("unexpected error: %s", d.Message)
		}
	}
}

// TestCompletions_ModuleType checks that module type completions are returned.
func TestCompletions_ModuleType(t *testing.T) {
	reg := NewRegistry()
	store := NewDocumentStore()
	doc := store.Set("file:///test.yaml", testYAML)

	ctx := PositionContext{
		Section:   SectionModules,
		FieldName: "type",
	}
	items := Completions(reg, doc, ctx)
	if len(items) == 0 {
		t.Fatal("no completions for module type")
	}
	found := false
	for _, item := range items {
		if item.Label == "http.server" {
			found = true
			break
		}
	}
	if !found {
		t.Error("http.server not in module type completions")
	}
}

// TestCompletions_TopLevel checks top-level key completions.
func TestCompletions_TopLevel(t *testing.T) {
	reg := NewRegistry()
	store := NewDocumentStore()
	doc := store.Set("file:///test.yaml", "")

	ctx := PositionContext{Section: SectionTopLevel}
	items := Completions(reg, doc, ctx)
	if len(items) == 0 {
		t.Fatal("no top-level completions")
	}

	labels := make(map[string]bool, len(items))
	for _, item := range items {
		labels[item.Label] = true
	}
	for _, expected := range []string{"modules", "workflows", "triggers"} {
		if !labels[expected] {
			t.Errorf("missing top-level key completion: %q", expected)
		}
	}
}

// TestHover_ModuleType checks hover for module types.
func TestHover_ModuleType(t *testing.T) {
	reg := NewRegistry()
	store := NewDocumentStore()
	doc := store.Set("file:///test.yaml", testYAML)

	ctx := PositionContext{
		Section:    SectionModules,
		ModuleType: "http.server",
		FieldName:  "type",
	}
	hover := Hover(reg, doc, ctx)
	if hover == nil {
		t.Fatal("expected hover for http.server")
	}
}

// TestContextAt checks basic context detection.
func TestContextAt(t *testing.T) {
	yaml := `modules:
  - name: server
    type: http.server
`
	ctx := ContextAt(yaml, 2, 10)
	// Line 2 is "    type: http.server" — should detect modules section.
	if ctx.Section != SectionModules {
		t.Errorf("expected SectionModules, got %q", ctx.Section)
	}
}

// TestTemplateFunctions checks the template functions list.
func TestTemplateFunctions(t *testing.T) {
	fns := templateFunctions()
	if len(fns) == 0 {
		t.Fatal("no template functions")
	}
	foundUUID := false
	for _, f := range fns {
		if f == "uuidv4" {
			foundUUID = true
			break
		}
	}
	if !foundUUID {
		t.Error("uuidv4 not in template functions")
	}
}

// TestRegistry_StepTypes_RichMetadata checks that step types have config and output metadata.
func TestRegistry_StepTypes_RichMetadata(t *testing.T) {
	reg := NewRegistry()
	info, ok := reg.StepTypes["step.db_query"]
	if !ok {
		t.Fatal("step.db_query not in step type registry")
	}
	if len(info.ConfigDefs) == 0 {
		t.Error("step.db_query should have config field definitions")
	}
	if len(info.Outputs) == 0 {
		t.Error("step.db_query should have output definitions")
	}
	if len(info.ConfigKeys) == 0 {
		t.Error("step.db_query should have config keys")
	}
}

// TestHover_StepType checks hover for step types.
func TestHover_StepType(t *testing.T) {
	reg := NewRegistry()
	store := NewDocumentStore()
	doc := store.Set("file:///test.yaml", testYAML)

	ctx := PositionContext{
		Section:  SectionPipeline,
		StepType: "step.db_query",
	}
	hover := Hover(reg, doc, ctx)
	if hover == nil {
		t.Fatal("expected hover for step.db_query")
	}
	content := hover.Contents.(protocol.MarkupContent).Value
	if !containsStr(content, "step.db_query") {
		t.Error("hover should contain step type name")
	}
	if !containsStr(content, "database") {
		t.Error("hover should contain config key 'database'")
	}
	if !containsStr(content, "Outputs") {
		t.Error("hover should contain outputs section")
	}
}

// TestHover_StepConfigField checks hover for step config fields.
func TestHover_StepConfigField(t *testing.T) {
	reg := NewRegistry()
	store := NewDocumentStore()
	doc := store.Set("file:///test.yaml", testYAML)

	ctx := PositionContext{
		Section:   SectionPipeline,
		StepType:  "step.http_call",
		FieldName: "url",
	}
	hover := Hover(reg, doc, ctx)
	if hover == nil {
		t.Fatal("expected hover for step.http_call url field")
	}
	content := hover.Contents.(protocol.MarkupContent).Value
	if !containsStr(content, "url") {
		t.Error("hover should mention the field name")
	}
	if !containsStr(content, "step.http_call") {
		t.Error("hover should mention the step type")
	}
}

// TestCompletions_StepConfigKeys checks step config key completions.
func TestCompletions_StepConfigKeys(t *testing.T) {
	reg := NewRegistry()
	store := NewDocumentStore()
	doc := store.Set("file:///test.yaml", testYAML)

	ctx := PositionContext{
		Section:  SectionPipeline,
		StepType: "step.db_query",
	}
	items := Completions(reg, doc, ctx)
	if len(items) == 0 {
		t.Fatal("expected config key completions for step.db_query")
	}
	labels := make(map[string]bool)
	for _, item := range items {
		labels[item.Label] = true
	}
	for _, expected := range []string{"database", "query"} {
		if !labels[expected] {
			t.Errorf("expected config key %q in completions", expected)
		}
	}
}

// TestContextAt_PipelineStep checks context detection for pipeline step config.
func TestContextAt_PipelineStep(t *testing.T) {
	yaml := `pipelines:
  my-pipeline:
    steps:
      - name: lookup
        type: step.db_query
        config:
          database: mydb
`
	// Cursor on "database: mydb" (line 6, indent 10)
	ctx := ContextAt(yaml, 6, 12)
	if ctx.Section != SectionPipeline {
		t.Errorf("expected SectionPipeline, got %q", ctx.Section)
	}
	if ctx.StepType != "step.db_query" {
		t.Errorf("expected StepType step.db_query, got %q", ctx.StepType)
	}
	if ctx.FieldName != "database" {
		t.Errorf("expected FieldName 'database', got %q", ctx.FieldName)
	}
}

// TestContextAt_PipelineStepTypeLine checks context detection on a step type line.
func TestContextAt_PipelineStepTypeLine(t *testing.T) {
	yaml := `pipelines:
  my-pipeline:
    steps:
      - name: lookup
        type: step.db_query
`
	// Cursor on "type: step.db_query" (line 4)
	ctx := ContextAt(yaml, 4, 14)
	if ctx.Section != SectionPipeline {
		t.Errorf("expected SectionPipeline, got %q", ctx.Section)
	}
	if ctx.StepType != "step.db_query" {
		t.Errorf("expected StepType step.db_query, got %q", ctx.StepType)
	}
	if ctx.FieldName != "type" {
		t.Errorf("expected FieldName 'type', got %q", ctx.FieldName)
	}
}

const pipelineYAML = `
pipelines:
  my-pipeline:
    steps:
      - name: parse
        type: step.request_parse
        config:
          path_params: [id]
      - name: query
        type: step.db_query
        config:
          database: mydb
          query: "SELECT * FROM items"
          mode: list
      - name: respond
        type: step.json_response
        config:
          status: 200
          body_from: "{{ .steps.query.rows }}"
`

// TestCompletions_TemplateTopLevel checks that top-level dot gives namespace completions.
func TestCompletions_TemplateTopLevel(t *testing.T) {
	reg := NewRegistry()
	store := NewDocumentStore()
	doc := store.Set("file:///pipeline.yaml", pipelineYAML)

	ctx := PositionContext{
		Section:      SectionPipeline,
		InTemplate:   true,
		PipelineName: "my-pipeline",
		TemplatePath: &TemplateExprPath{Namespace: "", Raw: "."},
	}
	items := Completions(reg, doc, ctx)
	labels := make(map[string]bool)
	for _, item := range items {
		labels[item.Label] = true
	}
	if !labels[".steps"] {
		t.Error("expected .steps in top-level template completions")
	}
	if !labels[".trigger"] {
		t.Error("expected .trigger in top-level template completions")
	}
}

// TestCompletions_TemplateStepNames checks that .steps namespace gives step name completions.
func TestCompletions_TemplateStepNames(t *testing.T) {
	reg := NewRegistry()
	store := NewDocumentStore()
	doc := store.Set("file:///pipeline.yaml", pipelineYAML)

	ctx := PositionContext{
		Section:         SectionPipeline,
		InTemplate:      true,
		PipelineName:    "my-pipeline",
		CurrentStepName: "respond",
		TemplatePath:    &TemplateExprPath{Namespace: "steps", Raw: ".steps."},
	}
	items := Completions(reg, doc, ctx)
	if len(items) == 0 {
		t.Fatal("expected step name completions")
	}
	labels := make(map[string]bool)
	for _, item := range items {
		labels[item.Label] = true
	}
	if !labels["parse"] {
		t.Error("expected 'parse' step in completions")
	}
	if !labels["query"] {
		t.Error("expected 'query' step in completions")
	}
}

// TestCompletions_TemplateStepOutputKeys checks that .steps.stepName gives output key completions.
func TestCompletions_TemplateStepOutputKeys(t *testing.T) {
	reg := NewRegistry()
	store := NewDocumentStore()
	doc := store.Set("file:///pipeline.yaml", pipelineYAML)

	ctx := PositionContext{
		Section:         SectionPipeline,
		InTemplate:      true,
		PipelineName:    "my-pipeline",
		CurrentStepName: "respond",
		TemplatePath:    &TemplateExprPath{Namespace: "steps", StepName: "query", Raw: ".steps.query."},
	}
	items := Completions(reg, doc, ctx)
	if len(items) == 0 {
		t.Fatal("expected output key completions for query step")
	}
	labels := make(map[string]bool)
	for _, item := range items {
		labels[item.Label] = true
	}
	if !labels["rows"] {
		t.Error("expected 'rows' in query step output completions")
	}
	if !labels["count"] {
		t.Error("expected 'count' in query step output completions")
	}
}

// TestCompletions_TemplateTrigger checks that .trigger gives trigger field completions.
func TestCompletions_TemplateTrigger(t *testing.T) {
	reg := NewRegistry()
	store := NewDocumentStore()
	doc := store.Set("file:///pipeline.yaml", pipelineYAML)

	ctx := PositionContext{
		Section:      SectionPipeline,
		InTemplate:   true,
		PipelineName: "my-pipeline",
		TemplatePath: &TemplateExprPath{Namespace: "trigger", Raw: ".trigger."},
	}
	items := Completions(reg, doc, ctx)
	if len(items) == 0 {
		t.Fatal("expected trigger field completions")
	}
	labels := make(map[string]bool)
	for _, item := range items {
		labels[item.Label] = true
	}
	if !labels["path_params"] {
		t.Error("expected 'path_params' in trigger completions")
	}
	if !labels["query"] {
		t.Error("expected 'query' in trigger completions")
	}
}

// TestContextAt_TemplatePath checks that ContextAt populates TemplatePath.
func TestContextAt_TemplatePath(t *testing.T) {
	yml := `pipelines:
  my-pipeline:
    steps:
      - name: respond
        type: step.json_response
        config:
          body_from: "{{ .steps.query.rows }}"
`
	// Line 6 is the body_from line. Cursor inside {{ .steps.query.rows }}
	// "          body_from: \"{{ .steps.query.rows }}\""
	// Let's count: 10 spaces + body_from: "{{ .steps.query.rows }}"
	// cursor at position 25 (inside .steps.query.rows)
	ctx := ContextAt(yml, 6, 25)
	if !ctx.InTemplate {
		t.Error("expected InTemplate=true")
	}
	if ctx.TemplatePath == nil {
		t.Error("expected TemplatePath to be populated")
	}
}

// TestContextAt_PipelineName checks that ContextAt extracts the pipeline name.
func TestContextAt_PipelineName(t *testing.T) {
	yml := `pipelines:
  my-pipeline:
    steps:
      - name: query
        type: step.db_query
        config:
          database: mydb
`
	ctx := ContextAt(yml, 6, 12) // "          database: mydb"
	if ctx.Section != SectionPipeline {
		t.Errorf("expected SectionPipeline, got %q", ctx.Section)
	}
	if ctx.PipelineName != "my-pipeline" {
		t.Errorf("expected PipelineName 'my-pipeline', got %q", ctx.PipelineName)
	}
}

// --- Template hover tests ---

const pipelineHoverYAML = `pipelines:
  user-lookup:
    trigger:
      type: http
    steps:
      - name: parse
        type: step.request_parse
        config: {}
      - name: lookup
        type: step.db_query
        config:
          mode: single
      - name: setResult
        type: step.set
        config:
          values:
            email: "{{ .steps.lookup.row }}"
`

func TestHover_TemplateFunction(t *testing.T) {
	reg := NewRegistry()
	store := NewDocumentStore()
	doc := store.Set("file:///test.yaml", pipelineHoverYAML)

	ctx := PositionContext{
		InTemplate: true,
		TemplatePath: &TemplateExprPath{
			Raw: "lower",
		},
	}
	hover := Hover(reg, doc, ctx)
	if hover == nil {
		t.Fatal("expected hover for 'lower' template function")
	}
	content := hover.Contents.(protocol.MarkupContent).Value
	if !containsStr(content, "lower") {
		t.Error("hover should mention 'lower'")
	}
}

func TestHover_TemplateNamespaces(t *testing.T) {
	reg := NewRegistry()
	store := NewDocumentStore()
	doc := store.Set("file:///test.yaml", pipelineHoverYAML)

	ctx := PositionContext{
		InTemplate: true,
		TemplatePath: &TemplateExprPath{
			Raw:       ".",
			Namespace: "",
		},
	}
	hover := Hover(reg, doc, ctx)
	if hover == nil {
		t.Fatal("expected hover for template namespace list")
	}
	content := hover.Contents.(protocol.MarkupContent).Value
	if !containsStr(content, "steps") {
		t.Error("hover should mention 'steps' namespace")
	}
	if !containsStr(content, "trigger") {
		t.Error("hover should mention 'trigger' namespace")
	}
}

func TestHover_TemplateStepOutputs(t *testing.T) {
	reg := NewRegistry()
	store := NewDocumentStore()
	doc := store.Set("file:///test.yaml", pipelineHoverYAML)

	ctx := PositionContext{
		InTemplate:      true,
		Section:         SectionPipeline,
		PipelineName:    "user-lookup",
		CurrentStepName: "setResult",
		TemplatePath: &TemplateExprPath{
			Namespace: "steps",
			StepName:  "lookup",
			Raw:       ".steps.lookup",
		},
	}
	hover := Hover(reg, doc, ctx)
	if hover == nil {
		t.Fatal("expected hover for step outputs")
	}
	content := hover.Contents.(protocol.MarkupContent).Value
	if !containsStr(content, "lookup") {
		t.Error("hover should mention step name 'lookup'")
	}
}

func TestHover_TemplateStepField(t *testing.T) {
	reg := NewRegistry()
	store := NewDocumentStore()
	doc := store.Set("file:///test.yaml", pipelineHoverYAML)

	ctx := PositionContext{
		InTemplate:      true,
		Section:         SectionPipeline,
		PipelineName:    "user-lookup",
		CurrentStepName: "setResult",
		TemplatePath: &TemplateExprPath{
			Namespace:   "steps",
			StepName:    "lookup",
			FieldPrefix: "row",
			Raw:         ".steps.lookup.row",
		},
	}
	hover := Hover(reg, doc, ctx)
	if hover == nil {
		t.Fatal("expected hover for step field")
	}
	content := hover.Contents.(protocol.MarkupContent).Value
	if !containsStr(content, "row") {
		t.Error("hover should mention field 'row'")
	}
	if !containsStr(content, "lookup") {
		t.Error("hover should mention step name 'lookup'")
	}
}

func TestHover_TemplateTrigger(t *testing.T) {
	reg := NewRegistry()
	store := NewDocumentStore()
	doc := store.Set("file:///test.yaml", pipelineHoverYAML)

	ctx := PositionContext{
		InTemplate: true,
		TemplatePath: &TemplateExprPath{
			Namespace: "trigger",
			Raw:       ".trigger",
		},
	}
	hover := Hover(reg, doc, ctx)
	if hover == nil {
		t.Fatal("expected hover for trigger namespace")
	}
	content := hover.Contents.(protocol.MarkupContent).Value
	if !containsStr(content, "trigger") {
		t.Error("hover should mention 'trigger'")
	}
	if !containsStr(content, "path_params") {
		t.Error("hover should list path_params sub-namespace")
	}
}

func TestHover_TemplateTriggerSubfield(t *testing.T) {
	reg := NewRegistry()
	store := NewDocumentStore()
	doc := store.Set("file:///test.yaml", pipelineHoverYAML)

	ctx := PositionContext{
		InTemplate: true,
		TemplatePath: &TemplateExprPath{
			Namespace: "trigger",
			SubField:  "path_params",
			Raw:       ".trigger.path_params",
		},
	}
	hover := Hover(reg, doc, ctx)
	if hover == nil {
		t.Fatal("expected hover for trigger subfield")
	}
	content := hover.Contents.(protocol.MarkupContent).Value
	if !containsStr(content, "path_params") {
		t.Error("hover should mention 'path_params'")
	}
}

func TestHover_TemplateMeta(t *testing.T) {
	reg := NewRegistry()
	store := NewDocumentStore()
	doc := store.Set("file:///test.yaml", pipelineHoverYAML)

	ctx := PositionContext{
		InTemplate: true,
		TemplatePath: &TemplateExprPath{
			Namespace:   "meta",
			FieldPrefix: "pipeline_name",
			Raw:         ".meta.pipeline_name",
		},
	}
	hover := Hover(reg, doc, ctx)
	if hover == nil {
		t.Fatal("expected hover for meta field")
	}
	content := hover.Contents.(protocol.MarkupContent).Value
	if !containsStr(content, "pipeline_name") {
		t.Error("hover should mention 'pipeline_name'")
	}
}

func TestHover_TemplateBody(t *testing.T) {
	reg := NewRegistry()
	store := NewDocumentStore()
	doc := store.Set("file:///test.yaml", pipelineHoverYAML)

	ctx := PositionContext{
		InTemplate: true,
		TemplatePath: &TemplateExprPath{
			Namespace:   "body",
			FieldPrefix: "email",
			Raw:         ".body.email",
		},
	}
	hover := Hover(reg, doc, ctx)
	if hover == nil {
		t.Fatal("expected hover for body field")
	}
	content := hover.Contents.(protocol.MarkupContent).Value
	if !containsStr(content, "body") {
		t.Error("hover should mention 'body'")
	}
}

// helpers

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || findSubstr(s, sub))
}

func findSubstr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func diagMessages(diags []protocol.Diagnostic) []string {
	msgs := make([]string, len(diags))
	for i, d := range diags {
		msgs[i] = fmt.Sprintf("[%d] %s", i, d.Message)
	}
	return msgs
}
