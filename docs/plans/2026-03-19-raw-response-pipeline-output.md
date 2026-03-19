# step.pipeline_output + engine.ExecutePipeline() Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `step.pipeline_output` step type and `engine.ExecutePipeline()` Go API so Go callers can invoke pipelines directly without HTTP serialization round-trips.

**Architecture:** `step.pipeline_output` stores structured data in `pc.Metadata["_pipeline_output"]` and stops the pipeline. `ExecutePipeline()` on `StdEngine` looks up a named pipeline, runs it, and extracts `_pipeline_output` (falling back to `Current`). The HTTP trigger fallback is updated to check `_pipeline_output` before the generic 202 response. `step.raw_response` already exists and is registered — no changes needed there.

**Tech Stack:** Go 1.26, standard library only, existing workflow engine patterns

**Note:** `step.raw_response` already exists at `module/pipeline_step_raw_response.go` and is registered in `plugins/pipelinesteps/plugin.go:148`. It does NOT have tests — Task 1 adds them.

---

### Task 1: Add tests for existing step.raw_response

**Files:**
- Create: `module/pipeline_step_raw_response_test.go`

**Step 1: Write the tests**

```go
package module

import (
	"context"
	"net/http/httptest"
	"testing"
)

func TestRawResponseStep_BasicResponse(t *testing.T) {
	factory := NewRawResponseStepFactory()
	step, err := factory("respond", map[string]any{
		"content_type": "application/json",
		"status":       200,
		"body":         `{"gameId":"abc-123"}`,
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	recorder := httptest.NewRecorder()
	pc := NewPipelineContext(nil, map[string]any{
		"_http_response_writer": recorder,
	})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if !result.Stop {
		t.Error("expected Stop=true")
	}

	resp := recorder.Result()
	if resp.StatusCode != 200 {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected Content-Type=application/json, got %s", ct)
	}

	body := recorder.Body.String()
	if body != `{"gameId":"abc-123"}` {
		t.Errorf("expected raw JSON body, got %q", body)
	}
	if pc.Metadata["_response_handled"] != true {
		t.Error("expected _response_handled=true")
	}
}

func TestRawResponseStep_NoDoubleEncoding(t *testing.T) {
	// This is the critical test: verify that JSON template output
	// is written as-is, not double-encoded like step.json_response does.
	factory := NewRawResponseStepFactory()
	step, err := factory("respond", map[string]any{
		"content_type": "application/json",
		"body":         `{{ index .steps "fetch" "stateJson" }}`,
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	recorder := httptest.NewRecorder()
	pc := NewPipelineContext(nil, map[string]any{
		"_http_response_writer": recorder,
	})
	pc.StepOutputs["fetch"] = map[string]any{
		"stateJson": `{"status":"active","turn":1}`,
	}
	pc.Current["steps"] = pc.StepOutputs

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if !result.Stop {
		t.Error("expected Stop=true")
	}

	body := recorder.Body.String()
	// Must be raw JSON, NOT wrapped in quotes like: "{\"status\":\"active\"}"
	expected := `{"status":"active","turn":1}`
	if body != expected {
		t.Errorf("double-encoding detected!\nexpected: %s\ngot:      %s", expected, body)
	}
}

func TestRawResponseStep_CustomStatusAndHeaders(t *testing.T) {
	factory := NewRawResponseStepFactory()
	step, err := factory("respond", map[string]any{
		"content_type": "text/html",
		"status":       201,
		"headers": map[string]any{
			"X-Custom": "value",
		},
		"body": "<h1>Created</h1>",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	recorder := httptest.NewRecorder()
	pc := NewPipelineContext(nil, map[string]any{
		"_http_response_writer": recorder,
	})

	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	resp := recorder.Result()
	if resp.StatusCode != 201 {
		t.Errorf("expected status 201, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/html" {
		t.Errorf("expected Content-Type=text/html, got %s", ct)
	}
	if xc := resp.Header.Get("X-Custom"); xc != "value" {
		t.Errorf("expected X-Custom=value, got %s", xc)
	}
	if recorder.Body.String() != "<h1>Created</h1>" {
		t.Errorf("unexpected body: %s", recorder.Body.String())
	}
}

func TestRawResponseStep_NoResponseWriter(t *testing.T) {
	factory := NewRawResponseStepFactory()
	step, err := factory("respond", map[string]any{
		"content_type": "application/json",
		"body":         `{"ok":true}`,
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	// No _http_response_writer in metadata
	pc := NewPipelineContext(nil, nil)

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if !result.Stop {
		t.Error("expected Stop=true")
	}
	if result.Output["body"] != `{"ok":true}` {
		t.Errorf("expected body in output, got %v", result.Output)
	}
	if result.Output["content_type"] != "application/json" {
		t.Errorf("expected content_type in output, got %v", result.Output)
	}
}

func TestRawResponseStep_BodyFrom(t *testing.T) {
	factory := NewRawResponseStepFactory()
	step, err := factory("respond", map[string]any{
		"content_type": "application/json",
		"body_from":    "steps.fetch.data",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	recorder := httptest.NewRecorder()
	pc := NewPipelineContext(nil, map[string]any{
		"_http_response_writer": recorder,
	})
	pc.StepOutputs["fetch"] = map[string]any{
		"data": `{"items":[1,2,3]}`,
	}

	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if recorder.Body.String() != `{"items":[1,2,3]}` {
		t.Errorf("unexpected body: %s", recorder.Body.String())
	}
}

func TestRawResponseStep_ContentTypeRequired(t *testing.T) {
	factory := NewRawResponseStepFactory()
	_, err := factory("respond", map[string]any{
		"body": "hello",
	}, nil)
	if err == nil {
		t.Error("expected error when content_type is missing")
	}
}
```

**Step 2: Run tests to verify they pass**

Run: `cd /Users/jon/workspace/workflow && go test ./module/ -run TestRawResponseStep -v`
Expected: All 6 tests PASS

**Step 3: Commit**

```bash
git add module/pipeline_step_raw_response_test.go
git commit -m "test: add tests for step.raw_response"
```

---

### Task 2: Implement step.pipeline_output

**Files:**
- Create: `module/pipeline_step_pipeline_output.go`
- Create: `module/pipeline_step_pipeline_output_test.go`

**Step 1: Write the failing test**

```go
package module

import (
	"context"
	"testing"
)

func TestPipelineOutputStep_Source(t *testing.T) {
	factory := NewPipelineOutputStepFactory()
	step, err := factory("result", map[string]any{
		"source": "steps.fetch",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	pc.StepOutputs["fetch"] = map[string]any{
		"gameId": "abc-123",
		"status": "active",
	}
	pc.Current["steps"] = pc.StepOutputs

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if !result.Stop {
		t.Error("expected Stop=true")
	}
	if result.Output["gameId"] != "abc-123" {
		t.Errorf("expected gameId=abc-123, got %v", result.Output["gameId"])
	}
	if result.Output["status"] != "active" {
		t.Errorf("expected status=active, got %v", result.Output["status"])
	}

	// Verify _pipeline_output is set in metadata
	pipeOut, ok := pc.Metadata["_pipeline_output"].(map[string]any)
	if !ok {
		t.Fatal("expected _pipeline_output in metadata")
	}
	if pipeOut["gameId"] != "abc-123" {
		t.Errorf("expected _pipeline_output gameId=abc-123, got %v", pipeOut["gameId"])
	}
}

func TestPipelineOutputStep_SourceNestedField(t *testing.T) {
	factory := NewPipelineOutputStepFactory()
	step, err := factory("result", map[string]any{
		"source": "steps.fetch.row",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	pc.StepOutputs["fetch"] = map[string]any{
		"row": map[string]any{
			"id":   "123",
			"name": "test",
		},
		"found": true,
	}

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if !result.Stop {
		t.Error("expected Stop=true")
	}

	pipeOut, ok := pc.Metadata["_pipeline_output"].(map[string]any)
	if !ok {
		t.Fatal("expected _pipeline_output in metadata")
	}
	if pipeOut["id"] != "123" {
		t.Errorf("expected id=123, got %v", pipeOut["id"])
	}
}

func TestPipelineOutputStep_Values(t *testing.T) {
	factory := NewPipelineOutputStepFactory()
	step, err := factory("result", map[string]any{
		"values": map[string]any{
			"gameId": "{{ .gameId }}",
			"turn":   "{{ index .steps \"state\" \"turnNumber\" }}",
		},
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"gameId": "game-42",
	}, nil)
	pc.StepOutputs["state"] = map[string]any{
		"turnNumber": "5",
	}
	pc.Current["steps"] = pc.StepOutputs

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if !result.Stop {
		t.Error("expected Stop=true")
	}
	if result.Output["gameId"] != "game-42" {
		t.Errorf("expected gameId=game-42, got %v", result.Output["gameId"])
	}
	if result.Output["turn"] != "5" {
		t.Errorf("expected turn=5, got %v", result.Output["turn"])
	}

	pipeOut := pc.Metadata["_pipeline_output"].(map[string]any)
	if pipeOut["gameId"] != "game-42" {
		t.Errorf("expected _pipeline_output gameId=game-42, got %v", pipeOut["gameId"])
	}
}

func TestPipelineOutputStep_RequiresSourceOrValues(t *testing.T) {
	factory := NewPipelineOutputStepFactory()
	_, err := factory("result", map[string]any{}, nil)
	if err == nil {
		t.Error("expected error when neither source nor values is provided")
	}
}

func TestPipelineOutputStep_SourceNotFound(t *testing.T) {
	factory := NewPipelineOutputStepFactory()
	step, err := factory("result", map[string]any{
		"source": "steps.nonexistent",
	}, nil)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	// Should return empty output, not error
	if !result.Stop {
		t.Error("expected Stop=true")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/jon/workspace/workflow && go test ./module/ -run TestPipelineOutputStep -v`
Expected: FAIL — `NewPipelineOutputStepFactory` not defined

**Step 3: Write the implementation**

```go
package module

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/modular"
)

// PipelineOutputStep marks structured data as the pipeline's return value.
// The data is stored in pc.Metadata["_pipeline_output"] for extraction by
// engine.ExecutePipeline() or the HTTP trigger fallback handler.
type PipelineOutputStep struct {
	name   string
	source string            // dot-path to step output (e.g. "steps.fetch")
	values map[string]string // template map (e.g. {"gameId": "{{ .gameId }}"})
	tmpl   *TemplateEngine
}

// NewPipelineOutputStepFactory returns a StepFactory for step.pipeline_output.
func NewPipelineOutputStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		source, _ := config["source"].(string)
		var values map[string]string
		if v, ok := config["values"].(map[string]any); ok {
			values = make(map[string]string, len(v))
			for k, val := range v {
				if s, ok := val.(string); ok {
					values[k] = s
				}
			}
		}

		if source == "" && len(values) == 0 {
			return nil, fmt.Errorf("pipeline_output step %q: 'source' or 'values' is required", name)
		}

		return &PipelineOutputStep{
			name:   name,
			source: source,
			values: values,
			tmpl:   NewTemplateEngine(),
		}, nil
	}
}

func (s *PipelineOutputStep) Name() string { return s.name }

func (s *PipelineOutputStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	var output map[string]any

	if s.source != "" {
		// Resolve from step outputs using the existing resolveBodyFrom helper
		resolved := resolveBodyFrom(s.source, pc)
		if m, ok := resolved.(map[string]any); ok {
			output = m
		} else {
			// Source didn't resolve to a map — return empty
			output = make(map[string]any)
		}
	} else {
		// Resolve template values
		output = make(map[string]any, len(s.values))
		for k, tmplExpr := range s.values {
			resolved, err := s.tmpl.Resolve(tmplExpr, pc)
			if err != nil {
				output[k] = tmplExpr // fallback to unresolved
			} else {
				output[k] = resolved
			}
		}
	}

	// Store in metadata for extraction by ExecutePipeline() / HTTP trigger fallback
	pc.Metadata["_pipeline_output"] = output

	return &StepResult{Output: output, Stop: true}, nil
}

var _ PipelineStep = (*PipelineOutputStep)(nil)
```

**Step 4: Run tests to verify they pass**

Run: `cd /Users/jon/workspace/workflow && go test ./module/ -run TestPipelineOutputStep -v`
Expected: All 5 tests PASS

**Step 5: Commit**

```bash
git add module/pipeline_step_pipeline_output.go module/pipeline_step_pipeline_output_test.go
git commit -m "feat: add step.pipeline_output for structured pipeline return values"
```

---

### Task 3: Register step.pipeline_output in plugin

**Files:**
- Modify: `plugins/pipelinesteps/plugin.go:148` (after `step.raw_response` line)

**Step 1: Add the registration**

Add this line after line 148 (`"step.raw_response"` registration):

```go
		"step.pipeline_output":   wrapStepFactory(module.NewPipelineOutputStepFactory()),
```

**Step 2: Verify build**

Run: `cd /Users/jon/workspace/workflow && go build ./...`
Expected: Clean build, no errors

**Step 3: Commit**

```bash
git add plugins/pipelinesteps/plugin.go
git commit -m "feat: register step.pipeline_output in pipeline steps plugin"
```

---

### Task 4: Update HTTP trigger fallback to check _pipeline_output

**Files:**
- Modify: `module/http_trigger.go:449-455`

**Step 1: Read the current fallback block (lines 449-463)**

The current code at lines 449-463 is:

```go
		// If the pipeline set response_status in its output (without writing
		// directly to the response writer), use those values to build the response.
		if result := resultHolder.Get(); result != nil {
			if writePipelineContextResponse(w, result) {
				return
			}
		}

		// Fallback: return a generic accepted response when the pipeline doesn't
		// write its own HTTP response.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
```

**Step 2: Add _pipeline_output check between the response_status check and the generic fallback**

Replace lines 449-463 with:

```go
		// If the pipeline set response_status in its output (without writing
		// directly to the response writer), use those values to build the response.
		if result := resultHolder.Get(); result != nil {
			if writePipelineContextResponse(w, result) {
				return
			}

			// If a step.pipeline_output set _pipeline_output, write it as JSON.
			if pipeOut, ok := result["_pipeline_output"]; ok && pipeOut != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				if err := json.NewEncoder(w).Encode(pipeOut); err != nil {
					log.Printf("http trigger: failed to write pipeline output: %v", err)
				}
				return
			}
		}

		// Fallback: return a generic accepted response when the pipeline doesn't
		// write its own HTTP response.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
```

Note: `json` and `log` are already imported in `http_trigger.go`. Verify after editing.

**Step 3: Verify build**

Run: `cd /Users/jon/workspace/workflow && go build ./...`
Expected: Clean build

**Step 4: Commit**

```bash
git add module/http_trigger.go
git commit -m "feat: HTTP trigger fallback checks _pipeline_output before generic 202"
```

---

### Task 5: Add HTTP trigger _pipeline_output test

**Files:**
- Modify: `module/http_trigger_test.go` (add test at end of file)

**Step 1: Read the existing test helper at the top of http_trigger_test.go**

The file uses `httptest.NewRecorder()` and a mock engine that captures workflow triggers. Read lines 300-340 to understand the `mockTriggerEngine` pattern.

**Step 2: Write the test**

Add this test at the end of `module/http_trigger_test.go`. Use the existing mock patterns from the file — read them first. The test should:

1. Create an HTTP trigger with a route
2. Set up a mock engine that returns `{"_pipeline_output": {"gameId": "test-123"}}` in the result holder
3. Send an HTTP request
4. Verify the response is `{"gameId":"test-123"}` with status 200 and Content-Type application/json
5. Verify it's NOT the generic `{"status":"workflow triggered"}` fallback

The exact test code depends on the mock patterns already in the file — read them before writing. The key assertion:

```go
if resp.Code != 200 {
    t.Errorf("expected 200, got %d", resp.Code)
}
var body map[string]any
json.NewDecoder(resp.Body).Decode(&body)
if body["gameId"] != "test-123" {
    t.Errorf("expected gameId=test-123, got %v", body["gameId"])
}
```

**Step 3: Run the test**

Run: `cd /Users/jon/workspace/workflow && go test ./module/ -run TestHTTPTrigger_PipelineOutput -v`
Expected: PASS

**Step 4: Commit**

```bash
git add module/http_trigger_test.go
git commit -m "test: verify HTTP trigger _pipeline_output fallback"
```

---

### Task 6: Implement engine.ExecutePipeline()

**Files:**
- Modify: `engine.go` (add method after `TriggerWorkflow`, add to `Engine` interface)

**Step 1: Write the failing test**

Create or append to `engine_test.go`. The test needs a fully wired engine with a pipeline. Read the existing test patterns in `engine_test.go` first to understand how to set up an engine with pipelines.

The test should:
1. Create a `StdEngine`
2. Register a simple pipeline with a `step.set` that outputs `{"gameId": "test-42"}` and a `step.pipeline_output` that sources from it
3. Call `engine.ExecutePipeline(ctx, "test_pipeline", data)`
4. Verify the result contains `{"gameId": "test-42"}`
5. Test error case: call with unknown pipeline name, verify error

**Step 2: Run test to verify it fails**

Expected: FAIL — `ExecutePipeline` method not defined

**Step 3: Implement ExecutePipeline**

Add this method to `engine.go` after the `TriggerWorkflow` method (after line 678):

```go
// ExecutePipeline runs a named pipeline synchronously and returns its
// structured output. For use by Go callers (gRPC servers, tests) that
// don't need HTTP request/response threading.
//
// If the pipeline uses step.pipeline_output, the explicitly marked output
// is returned. Otherwise, the pipeline's merged Current state is returned.
func (e *StdEngine) ExecutePipeline(ctx context.Context, name string, data map[string]any) (map[string]any, error) {
	pipeline, ok := e.pipelineRegistry[name]
	if !ok {
		return nil, fmt.Errorf("pipeline %q not found", name)
	}

	pc, err := pipeline.Execute(ctx, data)
	if err != nil {
		return nil, fmt.Errorf("pipeline %q: %w", name, err)
	}

	// Prefer explicit pipeline output if step.pipeline_output was used
	if pipeOut, ok := pc.Metadata["_pipeline_output"].(map[string]any); ok {
		return pipeOut, nil
	}

	return pc.Current, nil
}
```

Also add `ExecutePipeline` to the `Engine` interface at line 1084:

```go
	ExecutePipeline(ctx context.Context, name string, data map[string]any) (map[string]any, error)
```

**Step 4: Run test to verify it passes**

Run: `cd /Users/jon/workspace/workflow && go test -run TestExecutePipeline -v`
Expected: PASS

**Step 5: Verify full test suite**

Run: `cd /Users/jon/workspace/workflow && go test ./... 2>&1 | tail -20`
Expected: All tests pass (some packages may have no tests)

**Step 6: Commit**

```bash
git add engine.go engine_test.go
git commit -m "feat: add engine.ExecutePipeline() Go API for direct pipeline invocation"
```

---

### Task 7: Add PipelineExecutor interface

**Files:**
- Create: `interfaces/pipeline_executor.go`

**Step 1: Write the interface**

```go
package interfaces

import "context"

// PipelineExecutor provides direct pipeline invocation for Go callers
// (gRPC servers, tests, etc.) without HTTP serialization overhead.
// *workflow.StdEngine satisfies this interface.
type PipelineExecutor interface {
	// ExecutePipeline runs a named pipeline synchronously and returns its
	// structured output. Returns _pipeline_output if set by
	// step.pipeline_output, otherwise the pipeline's merged Current state.
	ExecutePipeline(ctx context.Context, name string, data map[string]any) (map[string]any, error)
}
```

**Step 2: Add compile-time interface check to engine.go**

Add this line near the other interface checks (search for `var _ ` in engine.go):

```go
var _ interfaces.PipelineExecutor = (*StdEngine)(nil)
```

**Step 3: Verify build**

Run: `cd /Users/jon/workspace/workflow && go build ./...`
Expected: Clean build

**Step 4: Commit**

```bash
git add interfaces/pipeline_executor.go engine.go
git commit -m "feat: add PipelineExecutor interface for consumer decoupling"
```

---

### Task 8: Run full test suite and verify

**Step 1: Run all tests**

Run: `cd /Users/jon/workspace/workflow && go test ./... 2>&1 | tail -30`
Expected: All packages pass

**Step 2: Run race detector**

Run: `cd /Users/jon/workspace/workflow && go test -race ./module/ -run "TestRawResponse\|TestPipelineOutput" -v`
Expected: No race conditions detected

**Step 3: Verify step registration in MCP schema**

Run: `cd /Users/jon/workspace/workflow && go build -o /tmp/wfctl ./cmd/wfctl && /tmp/wfctl list-step-types 2>&1 | grep -E "raw_response|pipeline_output"`
Expected: Both `step.raw_response` and `step.pipeline_output` appear in the list
