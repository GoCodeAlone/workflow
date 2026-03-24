package module

import (
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

func TestTemplateEngine_ResolveSimpleField(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{"name": "Alice"}, nil)

	result, err := te.Resolve("{{ .name }}", pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Alice" {
		t.Errorf("expected 'Alice', got %q", result)
	}
}

func TestTemplateEngine_ResolveNestedStepReference(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("validate", map[string]any{"result": "passed"})

	result, err := te.Resolve("{{ .steps.validate.result }}", pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "passed" {
		t.Errorf("expected 'passed', got %q", result)
	}
}

func TestTemplateEngine_ResolveMetadata(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(nil, map[string]any{"pipeline": "order-pipeline"})

	result, err := te.Resolve("{{ .meta.pipeline }}", pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "order-pipeline" {
		t.Errorf("expected 'order-pipeline', got %q", result)
	}
}

func TestTemplateEngine_ResolveTriggerData(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{"source": "webhook"}, nil)

	result, err := te.Resolve("{{ .trigger.source }}", pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "webhook" {
		t.Errorf("expected 'webhook', got %q", result)
	}
}

func TestTemplateEngine_NonTemplatePassthrough(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(nil, nil)

	plain := "just a plain string"
	result, err := te.Resolve(plain, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != plain {
		t.Errorf("expected %q, got %q", plain, result)
	}
}

func TestTemplateEngine_EmptyStringPassthrough(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(nil, nil)

	result, err := te.Resolve("", pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestTemplateEngine_MissingKeyLogsWarning(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(nil, map[string]any{"pipeline": "test-pipeline"})

	// Capture log output to verify the warning is emitted.
	var logBuf strings.Builder
	handler := slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn})
	pc.Logger = slog.New(handler)

	// Non-strict mode: missing key resolves to zero value but logs a warning.
	result, err := te.Resolve("{{ .nonexistent }}", pc)
	if err != nil {
		t.Fatalf("unexpected error in non-strict mode: %v", err)
	}
	// The zero value for a missing key in a map renders as "<no value>".
	_ = result

	// A warning should have been logged with the pipeline name, not the full template.
	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "template resolved missing key to zero value") {
		t.Errorf("expected missing-key warning in log, got: %q", logOutput)
	}
	if !strings.Contains(logOutput, "test-pipeline") {
		t.Errorf("expected pipeline name in log, got: %q", logOutput)
	}
	// The raw template attribute must NOT appear in logs (security: may contain secrets/PII).
	// The key name may appear in the error message from text/template, which is acceptable.
	if strings.Contains(logOutput, "template={{ .nonexistent }}") {
		t.Errorf("log should not contain raw template= attribute (security), got: %q", logOutput)
	}
}

func TestTemplateEngine_StrictModeReturnsError(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(nil, nil)
	pc.StrictTemplates = true

	_, err := te.Resolve("{{ .nonexistent }}", pc)
	if err == nil {
		t.Fatal("expected error in strict mode for missing key")
	}
	if !strings.Contains(err.Error(), "template exec error") {
		t.Errorf("expected 'template exec error' in message, got: %v", err)
	}
}

func TestTemplateEngine_StrictModePassesForPresentKey(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{"name": "Alice"}, nil)
	pc.StrictTemplates = true

	result, err := te.Resolve("{{ .name }}", pc)
	if err != nil {
		t.Fatalf("unexpected error in strict mode for present key: %v", err)
	}
	if result != "Alice" {
		t.Errorf("expected 'Alice', got %q", result)
	}
}

func TestTemplateEngine_StrictModeStepFieldTypoReturnsError(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("auth", map[string]any{"affiliate_id": "tenant123"})
	pc.StrictTemplates = true

	// Correct access should succeed.
	result, err := te.Resolve("{{ .steps.auth.affiliate_id }}", pc)
	if err != nil {
		t.Fatalf("unexpected error for correct field in strict mode: %v", err)
	}
	if result != "tenant123" {
		t.Errorf("expected 'tenant123', got %q", result)
	}

	// Typo in field name should fail in strict mode.
	_, err = te.Resolve("{{ .steps.auth.affilate_id }}", pc)
	if err == nil {
		t.Fatal("expected error for typo in field name in strict mode")
	}
}

func TestTemplateEngine_NonStrictModeStepFieldTypoLogsWarning(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("auth", map[string]any{"affiliate_id": "tenant123"})

	var logBuf strings.Builder
	handler := slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn})
	pc.Logger = slog.New(handler)

	// Typo in field name: should resolve to zero value with a warning.
	result, err := te.Resolve("{{ .steps.auth.affilate_id }}", pc)
	if err != nil {
		t.Fatalf("unexpected error in non-strict mode for field typo: %v", err)
	}
	_ = result // zero/empty value

	if !strings.Contains(logBuf.String(), "template resolved missing key to zero value") {
		t.Errorf("expected missing-key warning in log, got: %q", logBuf.String())
	}
}

func TestTemplateEngine_StrictModeStepHelperMissingStepReturnsError(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(nil, nil)
	pc.StrictTemplates = true

	// step helper for nonexistent step should fail in strict mode.
	_, err := te.Resolve(`{{ step "nonexistent" "field" }}`, pc)
	if err == nil {
		t.Fatal("expected error for missing step in strict mode via step helper")
	}
}

func TestTemplateEngine_StrictModeStepHelperMissingFieldReturnsError(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("auth", map[string]any{"affiliate_id": "tenant123"})
	pc.StrictTemplates = true

	// step helper for existing step but missing field should fail in strict mode.
	_, err := te.Resolve(`{{ step "auth" "affilate_id" }}`, pc)
	if err == nil {
		t.Fatal("expected error for missing field in strict mode via step helper")
	}
}

func TestTemplateEngine_StrictModeStepHelperSucceeds(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("auth", map[string]any{"affiliate_id": "tenant123"})
	pc.StrictTemplates = true

	result, err := te.Resolve(`{{ step "auth" "affiliate_id" }}`, pc)
	if err != nil {
		t.Fatalf("unexpected error in strict mode for correct step helper access: %v", err)
	}
	if result != "tenant123" {
		t.Errorf("expected 'tenant123', got %q", result)
	}
}

func TestTemplateEngine_StrictModeTriggerHelperMissingKeyReturnsError(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{"source": "webhook"}, nil)
	pc.StrictTemplates = true

	// trigger helper for missing key should fail in strict mode.
	_, err := te.Resolve(`{{ trigger "nonexistent_key" }}`, pc)
	if err == nil {
		t.Fatal("expected error for missing trigger key in strict mode via trigger helper")
	}
}

func TestTemplateEngine_InvalidTemplateReturnsError(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(nil, nil)

	_, err := te.Resolve("{{ .unclosed", pc)
	if err == nil {
		t.Fatal("expected error for invalid template syntax")
	}
	if !strings.Contains(err.Error(), "template parse error") {
		t.Errorf("expected 'template parse error' in message, got: %v", err)
	}
}

func TestTemplateEngine_TemplateWithMixedContent(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{"user": "Bob"}, nil)

	result, err := te.Resolve("Hello {{ .user }}!", pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Hello Bob!" {
		t.Errorf("expected 'Hello Bob!', got %q", result)
	}
}

func TestTemplateEngine_ResolveMap_SimpleValues(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{"name": "Alice", "age": 30}, nil)

	data := map[string]any{
		"greeting": "Hello {{ .name }}",
		"count":    42,
		"flag":     true,
	}

	result, err := te.ResolveMap(data, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["greeting"] != "Hello Alice" {
		t.Errorf("expected 'Hello Alice', got %v", result["greeting"])
	}
	// Non-string values should pass through unchanged
	if result["count"] != 42 {
		t.Errorf("expected count=42, got %v", result["count"])
	}
	if result["flag"] != true {
		t.Errorf("expected flag=true, got %v", result["flag"])
	}
}

func TestTemplateEngine_ResolveMap_NestedMaps(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{"env": "prod"}, nil)

	data := map[string]any{
		"outer": map[string]any{
			"inner": "running in {{ .env }}",
		},
	}

	result, err := te.ResolveMap(data, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	outer, ok := result["outer"].(map[string]any)
	if !ok {
		t.Fatalf("expected outer to be map[string]any, got %T", result["outer"])
	}
	if outer["inner"] != "running in prod" {
		t.Errorf("expected 'running in prod', got %v", outer["inner"])
	}
}

func TestTemplateEngine_ResolveMap_Slices(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{"item": "widget"}, nil)

	data := map[string]any{
		"items": []any{"{{ .item }}", "static", 123},
	}

	result, err := te.ResolveMap(data, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	items, ok := result["items"].([]any)
	if !ok {
		t.Fatalf("expected items to be []any, got %T", result["items"])
	}
	if items[0] != "widget" {
		t.Errorf("expected items[0]='widget', got %v", items[0])
	}
	if items[1] != "static" {
		t.Errorf("expected items[1]='static', got %v", items[1])
	}
	if items[2] != 123 {
		t.Errorf("expected items[2]=123, got %v", items[2])
	}
}

func TestTemplateEngine_ResolveMap_ErrorPropagation(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(nil, nil)

	data := map[string]any{
		"broken": "{{ .unclosed",
	}

	_, err := te.ResolveMap(data, pc)
	if err == nil {
		t.Fatal("expected error from invalid template in map")
	}
}

func TestTemplateEngine_FuncUUID(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(nil, nil)

	result, err := te.Resolve("{{ uuid }}", pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// UUID v4 has the form xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx
	if len(result) != 36 {
		t.Errorf("expected UUID of length 36, got %q (len=%d)", result, len(result))
	}
	if result[14] != '4' {
		t.Errorf("expected UUID version 4, got %q", result)
	}
}

func TestTemplateEngine_FuncNowNoArgs(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(nil, nil)

	result, err := te.Resolve(`{{ now }}`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "T") {
		t.Errorf("expected RFC3339 timestamp from no-arg now, got %q", result)
	}
}

func TestTemplateEngine_FuncNowRFC3339(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(nil, nil)

	result, err := te.Resolve(`{{ now "RFC3339" }}`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) == 0 {
		t.Error("expected non-empty timestamp")
	}
	// RFC3339 strings contain 'T' and 'Z' or offset
	if !strings.Contains(result, "T") {
		t.Errorf("expected RFC3339 timestamp, got %q", result)
	}
}

func TestTemplateEngine_FuncNowRawLayout(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(nil, nil)

	result, err := te.Resolve(`{{ now "2006-01-02" }}`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should be a date of the form YYYY-MM-DD
	if len(result) != 10 {
		t.Errorf("expected date of length 10, got %q", result)
	}
}

func TestTemplateEngine_FuncTrimPrefix(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{"phone": "+15551234567"}, nil)

	result, err := te.Resolve(`{{ .phone | trimPrefix "+" }}`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "15551234567" {
		t.Errorf("expected '15551234567', got %q", result)
	}
}

func TestTemplateEngine_FuncTrimPrefixNotPresent(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{"val": "hello"}, nil)

	result, err := te.Resolve(`{{ .val | trimPrefix "world" }}`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello" {
		t.Errorf("expected 'hello', got %q", result)
	}
}

func TestTemplateEngine_FuncTrimSuffix(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{"val": "hello.txt"}, nil)

	result, err := te.Resolve(`{{ .val | trimSuffix ".txt" }}`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello" {
		t.Errorf("expected 'hello', got %q", result)
	}
}

func TestTemplateEngine_FuncTrimSuffixNotPresent(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{"val": "hello"}, nil)

	result, err := te.Resolve(`{{ .val | trimSuffix ".txt" }}`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello" {
		t.Errorf("expected 'hello', got %q", result)
	}
}

func TestTemplateEngine_FuncDefault(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("parse", map[string]any{"query_params": map[string]any{}})

	result, err := te.Resolve(`{{ .steps.parse.query_params.page_size | default "25" }}`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "25" {
		t.Errorf("expected '25', got %q", result)
	}
}

func TestTemplateEngine_ResolveMap_DoesNotMutateInput(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{"x": "resolved"}, nil)

	data := map[string]any{
		"key": "{{ .x }}",
	}

	result, err := te.ResolveMap(data, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Original should be unchanged
	if data["key"] != "{{ .x }}" {
		t.Errorf("original map was mutated")
	}
	if result["key"] != "resolved" {
		t.Errorf("expected 'resolved', got %v", result["key"])
	}
}

// --- preprocessTemplate tests ---

func TestPreprocessTemplate_HyphenatedStepName(t *testing.T) {
	input := "{{ .steps.my-step.field }}"
	result := preprocessTemplate(input)
	if !strings.Contains(result, `index .steps "my-step" "field"`) {
		t.Errorf("expected index rewrite, got %q", result)
	}
}

func TestPreprocessTemplate_NoHyphens(t *testing.T) {
	input := "{{ .steps.validate.result }}"
	result := preprocessTemplate(input)
	if result != input {
		t.Errorf("expected unchanged %q, got %q", input, result)
	}
}

func TestPreprocessTemplate_SingleHyphenatedSegment(t *testing.T) {
	input := "{{ .my-var }}"
	result := preprocessTemplate(input)
	if !strings.Contains(result, `index . "my-var"`) {
		t.Errorf("expected index rewrite for single hyphenated segment, got %q", result)
	}
}

func TestPreprocessTemplate_MultipleHyphenatedSegments(t *testing.T) {
	input := "{{ .steps.my-step.sub-field.more }}"
	result := preprocessTemplate(input)
	if !strings.Contains(result, `index .steps "my-step" "sub-field" "more"`) {
		t.Errorf("expected index rewrite for multiple hyphenated segments, got %q", result)
	}
}

func TestPreprocessTemplate_WithPipe(t *testing.T) {
	input := "{{ .steps.my-step.field | lower }}"
	result := preprocessTemplate(input)
	if !strings.Contains(result, `index .steps "my-step" "field"`) {
		t.Errorf("expected index rewrite before pipe, got %q", result)
	}
	if !strings.Contains(result, "| lower") {
		t.Errorf("expected pipe preserved, got %q", result)
	}
}

func TestPreprocessTemplate_WithFunction(t *testing.T) {
	input := `{{ default "x" .steps.my-step.field }}`
	result := preprocessTemplate(input)
	if !strings.Contains(result, `index .steps "my-step" "field"`) {
		t.Errorf("expected index rewrite with function, got %q", result)
	}
	if !strings.Contains(result, `default "x"`) {
		t.Errorf("expected function preserved, got %q", result)
	}
}

func TestPreprocessTemplate_StringLiteralsSkipped(t *testing.T) {
	input := `{{ index .steps "my-step" "field" }}`
	result := preprocessTemplate(input)
	if result != input {
		t.Errorf("expected unchanged when hyphens are in string literals, got %q", result)
	}
}

func TestPreprocessTemplate_MixedContent(t *testing.T) {
	input := "Hello {{ .steps.my-step.name }}!"
	result := preprocessTemplate(input)
	if !strings.HasPrefix(result, "Hello ") {
		t.Errorf("expected text prefix preserved, got %q", result)
	}
	if !strings.HasSuffix(result, "!") {
		t.Errorf("expected text suffix preserved, got %q", result)
	}
	if !strings.Contains(result, `index .steps "my-step" "name"`) {
		t.Errorf("expected index rewrite in action, got %q", result)
	}
}

func TestPreprocessTemplate_NoTemplate(t *testing.T) {
	input := "plain text"
	result := preprocessTemplate(input)
	if result != input {
		t.Errorf("expected unchanged %q, got %q", input, result)
	}
}

func TestPreprocessTemplate_AlreadyUsingIndex(t *testing.T) {
	input := `{{ index .steps "my-step" "field" }}`
	result := preprocessTemplate(input)
	if result != input {
		t.Errorf("expected unchanged %q, got %q", input, result)
	}
}

// --- step and trigger helper function tests ---

func TestTemplateEngine_StepFunction(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("validate", map[string]any{"result": "passed"})

	result, err := te.Resolve(`{{ step "validate" "result" }}`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "passed" {
		t.Errorf("expected 'passed', got %q", result)
	}
}

func TestTemplateEngine_StepFunctionHyphenated(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("parse-request", map[string]any{
		"path_params": map[string]any{"id": "42"},
	})

	result, err := te.Resolve(`{{ step "parse-request" "path_params" "id" }}`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "42" {
		t.Errorf("expected '42', got %q", result)
	}
}

func TestTemplateEngine_StepFunctionDeepNesting(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("parse-request", map[string]any{
		"body": map[string]any{
			"nested": "deep-value",
		},
	})

	result, err := te.Resolve(`{{ step "parse-request" "body" "nested" }}`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "deep-value" {
		t.Errorf("expected 'deep-value', got %q", result)
	}
}

func TestTemplateEngine_StepFunctionMissing(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(nil, nil)

	result, err := te.Resolve(`{{ step "nonexistent" "field" }}`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// nil renders as "<no value>" with missingkey=zero; just verify no error
	_ = result
}

func TestTemplateEngine_TriggerFunction(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{
		"path_params": map[string]any{"id": "99"},
	}, nil)

	result, err := te.Resolve(`{{ trigger "path_params" "id" }}`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "99" {
		t.Errorf("expected '99', got %q", result)
	}
}

func TestPreprocessTemplate_UnclosedAction(t *testing.T) {
	input := "{{ .steps.my-step.field"
	result := preprocessTemplate(input)
	if result != input {
		t.Errorf("unclosed action should be returned unchanged, got %q", result)
	}
}

func TestPreprocessTemplate_EscapedQuotesPreserved(t *testing.T) {
	input := `{{ index .steps "my\"step" "field" }}`
	result := preprocessTemplate(input)
	if !strings.Contains(result, `"my\"step"`) {
		t.Errorf("escaped quotes should be preserved, got %q", result)
	}
}

func TestTemplateEngine_HyphenatedResolveEndToEnd(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("parse-request", map[string]any{
		"path_params": map[string]any{"id": "123"},
	})

	result, err := te.Resolve("{{ .steps.parse-request.path_params.id }}", pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "123" {
		t.Errorf("expected '123', got %q", result)
	}
}

// --- New template function tests ---

func TestTemplateEngine_FuncUpper(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(nil, nil)

	result, err := te.Resolve(`{{ "hello" | upper }}`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "HELLO" {
		t.Errorf("expected 'HELLO', got %q", result)
	}
}

func TestTemplateEngine_FuncTitle(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(nil, nil)

	result, err := te.Resolve(`{{ "hello world" | title }}`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Hello World" {
		t.Errorf("expected 'Hello World', got %q", result)
	}
}

func TestTemplateEngine_FuncReplace(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(nil, nil)

	result, err := te.Resolve(`{{ replace "o" "0" "hello" }}`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hell0" {
		t.Errorf("expected 'hell0', got %q", result)
	}
}

func TestTemplateEngine_FuncContains(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(nil, nil)

	result, err := te.Resolve(`{{ contains "ell" "hello" }}`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "true" {
		t.Errorf("expected 'true', got %q", result)
	}

	result, err = te.Resolve(`{{ contains "xyz" "hello" }}`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "false" {
		t.Errorf("expected 'false', got %q", result)
	}
}

func TestTemplateEngine_FuncHasPrefix(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(nil, nil)

	result, err := te.Resolve(`{{ hasPrefix "hel" "hello" }}`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "true" {
		t.Errorf("expected 'true', got %q", result)
	}

	result, err = te.Resolve(`{{ hasPrefix "xyz" "hello" }}`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "false" {
		t.Errorf("expected 'false', got %q", result)
	}
}

func TestTemplateEngine_FuncHasSuffix(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(nil, nil)

	result, err := te.Resolve(`{{ hasSuffix "llo" "hello" }}`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "true" {
		t.Errorf("expected 'true', got %q", result)
	}

	result, err = te.Resolve(`{{ hasSuffix "xyz" "hello" }}`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "false" {
		t.Errorf("expected 'false', got %q", result)
	}
}

func TestTemplateEngine_FuncSplit(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{"csv": "a,b,c"}, nil)

	// split returns a slice; we can use join to verify
	result, err := te.Resolve(`{{ $parts := split "," .csv }}{{ index $parts 1 }}`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "b" {
		t.Errorf("expected 'b', got %q", result)
	}
}

func TestTemplateEngine_FuncJoin(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{
		"items": []any{"a", "b", "c"},
	}, nil)

	result, err := te.Resolve(`{{ join "," .items }}`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "a,b,c" {
		t.Errorf("expected 'a,b,c', got %q", result)
	}
}

func TestTemplateEngine_FuncJoinStringSlice(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{"csv": "x,y,z"}, nil)

	result, err := te.Resolve(`{{ join "-" (split "," .csv) }}`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "x-y-z" {
		t.Errorf("expected 'x-y-z', got %q", result)
	}
}

func TestTemplateEngine_FuncTrimSpace(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(nil, nil)

	result, err := te.Resolve(`{{ "  hello  " | trimSpace }}`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello" {
		t.Errorf("expected 'hello', got %q", result)
	}
}

func TestTemplateEngine_FuncUrlEncode(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(nil, nil)

	result, err := te.Resolve(`{{ "hello world&foo=bar" | urlEncode }}`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello+world%26foo%3Dbar" {
		t.Errorf("expected 'hello+world%%26foo%%3Dbar', got %q", result)
	}
}

func TestTemplateEngine_FuncAdd(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{"a": 3, "b": 4}, nil)

	result, err := te.Resolve(`{{ add .a .b }}`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "7" {
		t.Errorf("expected '7', got %q", result)
	}

	// float + int = float
	pc2 := NewPipelineContext(map[string]any{"a": 1.5, "b": 2}, nil)
	result, err = te.Resolve(`{{ add .a .b }}`, pc2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "3.5" {
		t.Errorf("expected '3.5', got %q", result)
	}
}

func TestTemplateEngine_FuncSub(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{"a": 10, "b": 3}, nil)

	result, err := te.Resolve(`{{ sub .a .b }}`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "7" {
		t.Errorf("expected '7', got %q", result)
	}
}

func TestTemplateEngine_FuncMul(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{"a": 3, "b": 4}, nil)

	result, err := te.Resolve(`{{ mul .a .b }}`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "12" {
		t.Errorf("expected '12', got %q", result)
	}
}

func TestTemplateEngine_FuncDiv(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{"a": 10, "b": 4}, nil)

	result, err := te.Resolve(`{{ div .a .b }}`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "2.5" {
		t.Errorf("expected '2.5', got %q", result)
	}

	// Divide by zero returns 0
	pc2 := NewPipelineContext(map[string]any{"a": 10, "b": 0}, nil)
	result, err = te.Resolve(`{{ div .a .b }}`, pc2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "0" {
		t.Errorf("expected '0', got %q", result)
	}
}

func TestTemplateEngine_FuncToInt(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{"val": 3.7}, nil)

	result, err := te.Resolve(`{{ toInt .val }}`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "3" {
		t.Errorf("expected '3', got %q", result)
	}
}

func TestTemplateEngine_FuncToFloat(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{"val": 42}, nil)

	result, err := te.Resolve(`{{ toFloat .val }}`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "42" {
		t.Errorf("expected '42', got %q", result)
	}
}

func TestTemplateEngine_FuncToString(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{"val": 42}, nil)

	result, err := te.Resolve(`{{ toString .val }}`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "42" {
		t.Errorf("expected '42', got %q", result)
	}
}

func TestTemplateEngine_FuncLength(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{
		"str":   "hello",
		"items": []any{1, 2, 3},
		"m":     map[string]any{"a": 1, "b": 2},
	}, nil)

	result, err := te.Resolve(`{{ length .str }}`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "5" {
		t.Errorf("expected '5', got %q", result)
	}

	result, err = te.Resolve(`{{ length .items }}`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "3" {
		t.Errorf("expected '3', got %q", result)
	}

	result, err = te.Resolve(`{{ length .m }}`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "2" {
		t.Errorf("expected '2', got %q", result)
	}
}

func TestTemplateEngine_FuncCoalesce(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{"val": "hello"}, nil)

	result, err := te.Resolve(`{{ coalesce "" "" .val }}`, pc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello" {
		t.Errorf("expected 'hello', got %q", result)
	}

	// When all nil/empty, renders as <no value> (missingkey=zero)
	pc2 := NewPipelineContext(nil, nil)
	result, err = te.Resolve(`{{ coalesce .a .b "fallback" }}`, pc2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "fallback" {
		t.Errorf("expected 'fallback', got %q", result)
	}
}

func TestTemplateEngine_FuncSum(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{
		"nums": []any{10, 20, 30},
		"items": []any{
			map[string]any{"amount": 10.5},
			map[string]any{"amount": 20.0},
			map[string]any{"amount": 5.5},
		},
	}, nil)

	// Sum scalars
	got, err := te.Resolve(`{{ sum .nums }}`, pc)
	if err != nil {
		t.Fatal(err)
	}
	if got != "60" {
		t.Fatalf("expected 60, got %q", got)
	}

	// Sum with key
	got, err = te.Resolve(`{{ sum .items "amount" }}`, pc)
	if err != nil {
		t.Fatal(err)
	}
	if got != "36" {
		t.Fatalf("expected 36, got %q", got)
	}
}

func TestTemplateEngine_FuncPluck(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{
		"users": []any{
			map[string]any{"name": "Alice", "age": 30},
			map[string]any{"name": "Bob", "age": 25},
		},
	}, nil)
	got, err := te.Resolve(`{{ json (pluck .users "name") }}`, pc)
	if err != nil {
		t.Fatal(err)
	}
	if got != `["Alice","Bob"]` {
		t.Fatalf("expected [\"Alice\",\"Bob\"], got %q", got)
	}
}

func TestTemplateEngine_FuncFlatten(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{
		"nested": []any{
			[]any{1, 2},
			[]any{3, 4},
		},
	}, nil)
	got, err := te.Resolve(`{{ json (flatten .nested) }}`, pc)
	if err != nil {
		t.Fatal(err)
	}
	if got != `[1,2,3,4]` {
		t.Fatalf("expected [1,2,3,4], got %q", got)
	}
}

func TestTemplateEngine_FuncUnique(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{
		"tags": []any{"go", "rust", "go", "python", "rust"},
	}, nil)
	got, err := te.Resolve(`{{ json (unique .tags) }}`, pc)
	if err != nil {
		t.Fatal(err)
	}
	if got != `["go","rust","python"]` {
		t.Fatalf("expected deduplicated, got %q", got)
	}
}

func TestTemplateEngine_FuncGroupBy(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{
		"items": []any{
			map[string]any{"cat": "books", "title": "Go"},
			map[string]any{"cat": "toys", "title": "Ball"},
			map[string]any{"cat": "books", "title": "Rust"},
		},
	}, nil)
	got, err := te.Resolve(`{{ json (groupBy .items "cat") }}`, pc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `"books"`) || !strings.Contains(got, `"toys"`) {
		t.Fatalf("expected grouped output, got %q", got)
	}
}

func TestTemplateEngine_FuncSortBy(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{
		"items": []any{
			map[string]any{"name": "Charlie", "score": 3},
			map[string]any{"name": "Alice", "score": 1},
			map[string]any{"name": "Bob", "score": 2},
		},
	}, nil)
	got, err := te.Resolve(`{{ json (pluck (sortBy .items "score") "name") }}`, pc)
	if err != nil {
		t.Fatal(err)
	}
	if got != `["Alice","Bob","Charlie"]` {
		t.Fatalf("expected sorted, got %q", got)
	}
}

func TestTemplateEngine_FuncFirstLast(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{
		"items": []any{"a", "b", "c"},
		"empty": []any{},
	}, nil)

	got, err := te.Resolve(`{{ first .items }}`, pc)
	if err != nil {
		t.Fatal(err)
	}
	if got != "a" {
		t.Fatalf("expected a, got %q", got)
	}

	got, err = te.Resolve(`{{ last .items }}`, pc)
	if err != nil {
		t.Fatal(err)
	}
	if got != "c" {
		t.Fatalf("expected c, got %q", got)
	}

	got, err = te.Resolve(`{{ first .empty }}`, pc)
	if err != nil {
		t.Fatal(err)
	}
	if got != "<no value>" {
		t.Fatalf("expected <no value>, got %q", got)
	}
}

func TestTemplateEngine_FuncMinMax(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{
		"nums": []any{5, 2, 8, 1, 9},
		"items": []any{
			map[string]any{"price": 10.5},
			map[string]any{"price": 3.0},
			map[string]any{"price": 7.5},
		},
	}, nil)

	got, err := te.Resolve(`{{ min .nums }}`, pc)
	if err != nil {
		t.Fatal(err)
	}
	if got != "1" {
		t.Fatalf("expected 1, got %q", got)
	}

	got, err = te.Resolve(`{{ max .nums }}`, pc)
	if err != nil {
		t.Fatal(err)
	}
	if got != "9" {
		t.Fatalf("expected 9, got %q", got)
	}

	got, err = te.Resolve(`{{ min .items "price" }}`, pc)
	if err != nil {
		t.Fatal(err)
	}
	if got != "3" {
		t.Fatalf("expected 3, got %q", got)
	}

	got, err = te.Resolve(`{{ max .items "price" }}`, pc)
	if err != nil {
		t.Fatal(err)
	}
	if got != "10.5" {
		t.Fatalf("expected 10.5, got %q", got)
	}
}

func TestTemplateEngine_FuncSortByStrings(t *testing.T) {
	te := NewTemplateEngine()
	pc := NewPipelineContext(map[string]any{
		"items": []any{
			map[string]any{"name": "Charlie"},
			map[string]any{"name": "Alice"},
			map[string]any{"name": "Bob"},
		},
	}, nil)
	got, err := te.Resolve(`{{ json (pluck (sortBy .items "name") "name") }}`, pc)
	if err != nil {
		t.Fatal(err)
	}
	if got != `["Alice","Bob","Charlie"]` {
		t.Fatalf("expected lexicographic sort, got %q", got)
	}
}

func TestTemplateEngine_FuncSumInt64Precision(t *testing.T) {
	// Verify that summing large int64 values does not lose precision through float64.
	// float64 can only represent integers exactly up to 2^53 (9007199254740992).
	te := NewTemplateEngine()
	large := int64(9007199254740993) // 2^53 + 1 — not representable as float64
	pc := NewPipelineContext(map[string]any{
		"nums": []any{large, int64(1)},
	}, nil)
	got, err := te.Resolve(`{{ sum .nums }}`, pc)
	if err != nil {
		t.Fatal(err)
	}
	expected := fmt.Sprintf("%d", large+1)
	if got != expected {
		t.Fatalf("expected %s, got %q (float64 precision loss?)", expected, got)
	}
}

func TestTemplateEngine_FuncMinMaxInt64Precision(t *testing.T) {
	// Verify that min/max over large int64 values do not lose precision via float64.
	te := NewTemplateEngine()
	large := int64(9007199254740993) // 2^53 + 1
	small := int64(9007199254740991) // 2^53 - 1
	pc := NewPipelineContext(map[string]any{
		"nums": []any{large, small},
	}, nil)

	got, err := te.Resolve(`{{ min .nums }}`, pc)
	if err != nil {
		t.Fatal(err)
	}
	if got != fmt.Sprintf("%d", small) {
		t.Fatalf("min: expected %d, got %q", small, got)
	}

	got, err = te.Resolve(`{{ max .nums }}`, pc)
	if err != nil {
		t.Fatal(err)
	}
	if got != fmt.Sprintf("%d", large) {
		t.Fatalf("max: expected %d, got %q", large, got)
	}
}
