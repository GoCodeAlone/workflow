# wftest Integration Test Harness — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a `wftest` package that enables integration testing of workflow pipelines without a full server build, supporting Go API + YAML test files.

**Architecture:** Exported `wftest` package in the engine repo provides `Harness` with config loading, HTTP/pipeline/event injection, step/module mocking, and a YAML test runner. External trigger plugins register adapters via `TriggerAdapter` interface.

**Tech Stack:** Go 1.26, net/http/httptest, encoding/json, gopkg.in/yaml.v3

**Design Doc:** `docs/plans/2026-03-23-test-harness-design.md`

---

### Task 1: Core Harness — New, WithConfig, WithYAML, ExecutePipeline

**Files:**
- Create: `wftest/harness.go`
- Create: `wftest/options.go`
- Create: `wftest/result.go`
- Create: `wftest/harness_test.go`

**Step 1: Write failing test**

```go
// wftest/harness_test.go
package wftest_test

import (
    "testing"
    "github.com/GoCodeAlone/workflow/wftest"
)

func TestHarness_ExecutePipeline_SetStep(t *testing.T) {
    h := wftest.New(t, wftest.WithYAML(`
pipelines:
  greet:
    steps:
      - name: set_greeting
        type: step.set
        config:
          values:
            message: "hello world"
`))

    result := h.ExecutePipeline("greet", nil)
    if result.Error != nil {
        t.Fatalf("unexpected error: %v", result.Error)
    }
    if result.Output["message"] != "hello world" {
        t.Errorf("expected 'hello world', got %v", result.Output["message"])
    }
}

func TestHarness_ExecutePipeline_WithInput(t *testing.T) {
    h := wftest.New(t, wftest.WithYAML(`
pipelines:
  echo:
    steps:
      - name: copy
        type: step.set
        config:
          values:
            echoed: "{{ .input_val }}"
`))

    result := h.ExecutePipeline("echo", map[string]any{"input_val": "test123"})
    if result.Output["echoed"] != "test123" {
        t.Errorf("expected 'test123', got %v", result.Output["echoed"])
    }
}

func TestHarness_WithConfig_LoadsFile(t *testing.T) {
    // Write a temp YAML file and load it
    h := wftest.New(t, wftest.WithYAML(`
pipelines:
  simple:
    steps:
      - name: done
        type: step.set
        config:
          values:
            status: "ok"
`))
    result := h.ExecutePipeline("simple", nil)
    if result.Output["status"] != "ok" {
        t.Errorf("expected 'ok', got %v", result.Output["status"])
    }
}

func TestHarness_ExecutePipeline_NotFound(t *testing.T) {
    h := wftest.New(t, wftest.WithYAML(`
pipelines:
  exists:
    steps:
      - name: s
        type: step.set
        config:
          values: { x: 1 }
`))
    result := h.ExecutePipeline("does-not-exist", nil)
    if result.Error == nil {
        t.Error("expected error for missing pipeline")
    }
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/jon/workspace/workflow && go test ./wftest/ -v -count=1`
Expected: FAIL — package doesn't exist yet

**Step 3: Implement harness core**

`wftest/options.go`:
```go
package wftest

// Option configures a Harness.
type Option func(*Harness)

// WithYAML configures the harness with inline YAML config.
func WithYAML(yaml string) Option {
    return func(h *Harness) { h.yamlConfig = yaml }
}

// WithConfig loads config from a YAML file path.
func WithConfig(path string) Option {
    return func(h *Harness) { h.configPath = path }
}
```

`wftest/result.go`:
```go
package wftest

import "time"

// Result holds the outcome of a pipeline execution or HTTP request.
type Result struct {
    Output      map[string]any            // Final pipeline output (Current or _pipeline_output)
    StepResults map[string]map[string]any // Per-step outputs
    Error       error
    Duration    time.Duration

    // HTTP-specific (populated for HTTP triggers)
    StatusCode int
    Headers    map[string]string
    RawBody    []byte
}

// StepOutput returns the output map for a specific step.
func (r *Result) StepOutput(name string) map[string]any {
    if r.StepResults == nil { return nil }
    return r.StepResults[name]
}

// StepExecuted returns whether a step was executed.
func (r *Result) StepExecuted(name string) bool {
    _, ok := r.StepResults[name]
    return ok
}

// JSON parses the HTTP response body as JSON.
func (r *Result) JSON() map[string]any {
    // json.Unmarshal r.RawBody
}
```

`wftest/harness.go`:
```go
package wftest

import (
    "context"
    "testing"
    "github.com/GoCodeAlone/workflow"
    "github.com/GoCodeAlone/workflow/config"
    "github.com/GoCodeAlone/workflow/handlers"
    "github.com/GoCodeAlone/workflow/plugins/pipelinesteps"
    "github.com/GoCodeAlone/modular"
)

// Harness is an in-process workflow engine for integration testing.
type Harness struct {
    t          *testing.T
    yamlConfig string
    configPath string
    engine     *workflow.StdEngine
    app        modular.Application
    // ... mock registries, trigger adapters
}

// New creates a test harness with the given options.
func New(t *testing.T, opts ...Option) *Harness {
    t.Helper()
    h := &Harness{t: t}
    for _, opt := range opts {
        opt(h)
    }
    h.init()
    return h
}

func (h *Harness) init() {
    h.t.Helper()
    // Create minimal app + engine
    logger := /* discard logger */
    h.app = modular.NewStdApplication(nil, logger)
    h.engine = workflow.NewStdEngine(h.app, logger)

    // Load built-in plugins
    h.engine.LoadPlugin(pipelinesteps.New())
    h.engine.RegisterWorkflowHandler(handlers.NewPipelineWorkflowHandler())

    // Load config
    var cfg *config.WorkflowConfig
    if h.yamlConfig != "" {
        cfg, _ = config.LoadFromString(h.yamlConfig)
    } else if h.configPath != "" {
        cfg, _ = config.LoadFromFile(h.configPath)
    }
    if cfg != nil {
        h.engine.BuildFromConfig(cfg)
    }
}

// ExecutePipeline runs a pipeline by name with the given trigger data.
func (h *Harness) ExecutePipeline(name string, data map[string]any) *Result {
    h.t.Helper()
    ctx := h.t.Context()
    start := time.Now()
    output, err := h.engine.ExecutePipeline(ctx, name, data)
    return &Result{
        Output:   output,
        Error:    err,
        Duration: time.Since(start),
    }
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /Users/jon/workspace/workflow && go test ./wftest/ -v -count=1`
Expected: PASS

**Step 5: Commit**

```bash
git add wftest/
git commit -m "feat: add wftest package — core harness with ExecutePipeline"
```

---

### Task 2: HTTP Injection (POST/GET/PUT/DELETE)

**Files:**
- Modify: `wftest/harness.go` — add HTTP methods
- Create: `wftest/http.go` — HTTP request helpers
- Modify: `wftest/harness_test.go` — add HTTP tests

**Step 1: Write failing tests**

```go
func TestHarness_POST_SimpleRoute(t *testing.T) {
    h := wftest.New(t, wftest.WithYAML(`
modules:
    - name: http-server
      type: http.server
      config:
        port: 0
    - name: router
      type: http.router
workflows:
    http:
        router: router
        server: http-server
        routes: []
pipelines:
    create-user:
        trigger:
            type: http
            config:
                path: /api/users
                method: POST
        steps:
            - name: respond
              type: step.json_response
              config:
                status: 201
                body:
                    created: true
`))

    result := h.POST("/api/users", `{"email":"test@example.com"}`)
    if result.StatusCode != 201 {
        t.Errorf("expected 201, got %d", result.StatusCode)
    }
    body := result.JSON()
    if body["created"] != true {
        t.Errorf("expected created=true, got %v", body["created"])
    }
}
```

**Step 2: Implement HTTP injection**

Extract the engine's HTTP router handler and call `ServeHTTP` with `httptest.NewRecorder` + `httptest.NewRequest`. No TCP listener needed.

Read how the engine's HTTP workflow handler registers routes — find the `http.Handler` and expose it for test injection.

**Step 3: Run tests, commit**

```bash
git commit -m "feat(wftest): add HTTP request injection (POST/GET/PUT/DELETE)"
```

---

### Task 3: Step and Module Mocking

**Files:**
- Create: `wftest/mock.go`
- Modify: `wftest/options.go` — add MockStep, MockModule options
- Modify: `wftest/harness.go` — integrate mocks into engine
- Create: `wftest/mock_test.go`

**Step 1: Write failing tests**

```go
func TestHarness_MockStep(t *testing.T) {
    h := wftest.New(t,
        wftest.WithYAML(`
pipelines:
  query-users:
    steps:
      - name: fetch
        type: step.db_query
        config:
          database: db
          query: "SELECT * FROM users"
          mode: list
      - name: respond
        type: step.set
        config:
          values:
            count: "{{ index .steps \"fetch\" \"count\" }}"
`),
        wftest.MockStep("step.db_query", wftest.Returns(map[string]any{
            "rows":  []any{map[string]any{"id": 1, "email": "test@example.com"}},
            "count": 1,
        })),
    )

    result := h.ExecutePipeline("query-users", nil)
    if result.Error != nil {
        t.Fatal(result.Error)
    }
}

func TestHarness_RecordStep(t *testing.T) {
    recorder := wftest.NewRecorder()
    h := wftest.New(t,
        wftest.WithYAML(`...`),
        wftest.MockStep("step.db_exec", recorder),
    )
    h.ExecutePipeline("insert-user", map[string]any{"email": "test@example.com"})
    if len(recorder.Calls()) != 1 {
        t.Errorf("expected 1 call, got %d", len(recorder.Calls()))
    }
}
```

**Step 2: Implement mock step factory**

A mock step factory returns a `PipelineStep` that captures config and input, then returns canned output. Register mock factories BEFORE calling `BuildFromConfig` so they override real step types.

**Step 3: Implement mock module**

Mock modules register in the service registry so steps that look up modules by name find the mock instead.

**Step 4: Run tests, commit**

```bash
git commit -m "feat(wftest): add MockStep, MockModule, and call recording"
```

---

### Task 4: EventBus and Scheduler Injection

**Files:**
- Modify: `wftest/harness.go` — add FireEvent, FireSchedule
- Modify: `wftest/harness_test.go` — add event/scheduler tests

**Step 1: Write failing tests for EventBus injection**

```go
func TestHarness_FireEvent(t *testing.T) {
    h := wftest.New(t, wftest.WithYAML(`
pipelines:
  on-user-created:
    trigger:
      type: eventbus
      config:
        topic: user.created
    steps:
      - name: log_event
        type: step.set
        config:
          values:
            handled: true
            user_id: "{{ .user_id }}"
`))

    result := h.FireEvent("user.created", map[string]any{"user_id": "123"})
    if result.Output["handled"] != true {
        t.Error("expected event to be handled")
    }
}
```

**Step 2: Implement by finding the EventBus trigger and publishing directly**

**Step 3: Implement FireSchedule similarly**

**Step 4: Run tests, commit**

```bash
git commit -m "feat(wftest): add FireEvent and FireSchedule injection"
```

---

### Task 5: External Trigger Adapter Interface

**Files:**
- Create: `wftest/trigger_adapter.go`
- Modify: `wftest/harness.go` — add InjectTrigger, adapter registry
- Create: `wftest/trigger_adapter_test.go`

**Step 1: Define and test the TriggerAdapter interface**

```go
// TriggerAdapter allows external plugins to register test injection support.
type TriggerAdapter interface {
    Name() string
    Inject(h *Harness, event string, data map[string]any) (*Result, error)
}

func RegisterTriggerAdapter(adapter TriggerAdapter)
```

**Step 2: Implement adapter registry + InjectTrigger method**

**Step 3: Write a mock adapter test that verifies the full flow**

**Step 4: Commit**

```bash
git commit -m "feat(wftest): add TriggerAdapter interface for external plugin triggers"
```

---

### Task 6: YAML Test File Parser and Runner

**Files:**
- Create: `wftest/yaml_runner.go` — parses *_test.yaml, executes tests
- Create: `wftest/yaml_types.go` — YAML struct types
- Create: `wftest/yaml_runner_test.go`

**Step 1: Define YAML types**

```go
type TestFile struct {
    Config string              `yaml:"config"`
    Mocks  MockConfig          `yaml:"mocks"`
    Tests  map[string]TestCase `yaml:"tests"`
}

type TestCase struct {
    Description string            `yaml:"description"`
    Trigger     TriggerDef        `yaml:"trigger"`
    StopAfter   string            `yaml:"stop_after"`
    Mocks       *MockConfig       `yaml:"mocks"` // per-test override
    Assertions  []Assertion       `yaml:"assertions"`
}

type TriggerDef struct {
    Type    string         `yaml:"type"`
    Method  string         `yaml:"method"`
    Path    string         `yaml:"path"`
    Headers map[string]string `yaml:"headers"`
    Body    map[string]any `yaml:"body"`
    Topic   string         `yaml:"topic"` // for eventbus
    Event   string         `yaml:"event"` // for external triggers
    Name    string         `yaml:"name"`  // for scheduler
    Data    map[string]any `yaml:"data"`
}

type Assertion struct {
    Step     string         `yaml:"step"`
    Output   map[string]any `yaml:"output"`
    Executed *bool          `yaml:"executed"`
    Response *ResponseAssert `yaml:"response"`
}
```

**Step 2: Implement RunYAMLTests(t, path)**

Parses the YAML file, creates a harness per test case (with merged mocks), fires the trigger, then checks assertions. Each test case becomes a `t.Run` subtest.

**Step 3: Write test with an actual *_test.yaml fixture**

Create `wftest/testdata/simple_test.yaml` with 3-4 test cases and verify they run.

**Step 4: Commit**

```bash
git commit -m "feat(wftest): add YAML test file parser and runner"
```

---

### Task 7: stop_after Support

**Files:**
- Modify: `wftest/harness.go` — add stop_after to pipeline execution
- Modify: `wftest/harness_test.go`

**Step 1: Write failing test**

```go
func TestHarness_StopAfter(t *testing.T) {
    h := wftest.New(t, wftest.WithYAML(`
pipelines:
  multi-step:
    steps:
      - name: step1
        type: step.set
        config:
          values: { a: 1 }
      - name: step2
        type: step.set
        config:
          values: { b: 2 }
      - name: step3
        type: step.set
        config:
          values: { c: 3 }
`))

    result := h.ExecutePipelineOpts("multi-step", nil, wftest.StopAfter("step2"))
    if result.StepExecuted("step1") != true { t.Error("step1 should have run") }
    if result.StepExecuted("step2") != true { t.Error("step2 should have run") }
    if result.StepExecuted("step3") != false { t.Error("step3 should NOT have run") }
}
```

**Step 2: Implement by wrapping the pipeline context with a cancellation after the named step**

**Step 3: Commit**

```bash
git commit -m "feat(wftest): add stop_after for partial pipeline execution"
```

---

### Task 8: Server Mode (WithServer)

**Files:**
- Modify: `wftest/harness.go` — add WithServer, start real listeners
- Create: `wftest/server.go` — server lifecycle management
- Modify: `wftest/harness_test.go` — server mode tests

**Step 1: Implement WithServer option**

When enabled:
- Start real HTTP listener on a free port
- Expose `h.BaseURL()` for real HTTP clients
- Expose `h.WSDialer(path)` for real WS connections (if WS plugin loaded)
- Start engine via `engine.Start(ctx)` instead of just `BuildFromConfig`
- Cleanup on `t.Cleanup`

**Step 2: Write tests using real HTTP client against the server**

**Step 3: Commit**

```bash
git commit -m "feat(wftest): add WithServer mode for real protocol testing"
```

---

### Task 9: WithPlugin for Real Plugin Loading

**Files:**
- Modify: `wftest/options.go` — add WithPlugin option
- Modify: `wftest/harness.go` — load plugins during init

**Step 1: Implement WithPlugin**

```go
func WithPlugin(p plugin.EnginePlugin) Option {
    return func(h *Harness) {
        h.plugins = append(h.plugins, p)
    }
}
```

Load plugins after built-in plugins, before BuildFromConfig.

**Step 2: Write test loading a real plugin (use pipelinesteps as example)**

**Step 3: Commit**

```bash
git commit -m "feat(wftest): add WithPlugin for real plugin loading"
```

---

### Task 10: wfctl test CLI Command

**Files:**
- Create: `cmd/wfctl/test.go`
- Modify: `cmd/wfctl/main.go` — register "test" command
- Create: `cmd/wfctl/test_test.go`

**Step 1: Implement `wfctl test <file_or_dir>`**

- Parse args: file path or directory
- If directory: find all *_test.yaml files
- For each: parse YAML, create harness, run tests
- Print PASS/FAIL per test case with timing
- Exit with non-zero on any failure

**Step 2: Write test with a fixture YAML**

**Step 3: Commit**

```bash
git commit -m "feat: add wfctl test CLI command for YAML test runner"
```

---

### Task 11: Documentation — docs/testing.md

**Files:**
- Create: `docs/testing.md`

Write the testing guide covering:
1. Quick Start (10-line pipeline test)
2. Go-Based Testing (harness creation, HTTP, pipeline, mocking, server mode)
3. YAML-Based Testing (file format, mocks, assertions, running)
4. Patterns (auth, DB, events, partial execution)
5. Plugin Authors: Making Triggers Testable (TriggerAdapter)

**Commit:**
```bash
git commit -m "docs: add comprehensive workflow testing guide"
```
