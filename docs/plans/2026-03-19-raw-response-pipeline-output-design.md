# Design: step.raw_response + step.pipeline_output + engine.ExecutePipeline()

**Date:** 2026-03-19
**Status:** Approved

## Problem

`step.json_response` double-encodes when the body template produces JSON: the template resolves to `{"gameId":"abc"}` but `json.NewEncoder(w).Encode()` wraps it as `"{\"gameId\":\"abc\"}"`. This breaks internal consumers (gRPC proxy servers) that parse the HTTP response body.

More broadly, Go callers (gRPC servers, tests) currently invoke pipelines via HTTP round-trip when they could call the engine directly with zero serialization.

## Solution

Three complementary additions to the workflow engine:

### 1. step.raw_response

Writes resolved template body as raw bytes to `http.ResponseWriter` — no `json.Marshal`.

```yaml
- name: respond
  type: step.raw_response
  config:
    statusCode: 200
    contentType: application/json
    headers:
      X-Custom: "value"
    body: '{{ index .steps "state" "stateJson" }}'
```

- Gets `_http_response_writer` from `pc.Metadata`
- Sets Content-Type (default `application/json`), custom headers, status code
- Writes `[]byte(resolvedBody)` directly
- Sets `_response_handled = true`, returns `StepResult{Stop: true}`
- Falls back to output map when no ResponseWriter (non-HTTP context)

### 2. step.pipeline_output

Marks specific data as the pipeline's structured return value.

```yaml
- name: result
  type: step.pipeline_output
  config:
    source: steps.state        # dot-path to step output map
    # OR
    values:                    # explicit key-value map
      gameId: "{{ .gameId }}"
      status: "{{ index .steps \"state\" \"status\" }}"
```

- Resolves source path or values map
- Stores in `pc.Metadata["_pipeline_output"]`
- Returns `StepResult{Output: data, Stop: true}`
- HTTP trigger fallback checks `_pipeline_output` and writes as JSON (single-encode)

### 3. engine.ExecutePipeline() Go API

```go
func (e *StdEngine) ExecutePipeline(ctx context.Context, name string, data map[string]any) (map[string]any, error)
```

- Looks up pipeline in registry, calls `Run(ctx, data)`
- Returns `_pipeline_output` from metadata if set, otherwise `Current`
- Exposed via `PipelineExecutor` interface for consumer decoupling

## File Changes

| File | Change |
|------|--------|
| `module/pipeline_step_raw_response.go` | New (~40 lines) |
| `module/pipeline_step_raw_response_test.go` | New (tests) |
| `module/pipeline_step_pipeline_output.go` | New (~50 lines) |
| `module/pipeline_step_pipeline_output_test.go` | New (tests) |
| `engine.go` | Add `ExecutePipeline()` (~25 lines) |
| `engine_test.go` | Tests for ExecutePipeline |
| `interfaces/engine.go` | Add `PipelineExecutor` interface |
| `plugins/pipelinesteps/plugin.go` | Register both factories |
| `module/http_trigger.go` | Check `_pipeline_output` in fallback |

## Testing

- `step.raw_response`: verify raw bytes written (no double-encoding), status code, content-type, custom headers, non-HTTP fallback
- `step.pipeline_output`: verify source path resolution, values template resolution, `_pipeline_output` in metadata, HTTP fallback writes single-encoded JSON
- `engine.ExecutePipeline()`: verify pipeline lookup, data passing, `_pipeline_output` extraction, `Current` fallback, error on unknown pipeline
