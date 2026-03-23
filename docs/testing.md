# Testing Workflow Configs

The `wftest` package provides an in-process integration test harness for workflow
configurations. Tests can be written in Go or in plain YAML.

## Quick Start

### Go-based test

```go
package myapp_test

import (
    "testing"
    "github.com/GoCodeAlone/workflow/wftest"
)

func TestGreetPipeline(t *testing.T) {
    h := wftest.New(t, wftest.WithConfig("config.yaml"))

    result := h.ExecutePipeline("greet", map[string]any{"name": "alice"})
    if result.Error != nil {
        t.Fatal(result.Error)
    }
    if result.Output["message"] != "hello alice" {
        t.Errorf("unexpected message: %v", result.Output["message"])
    }
}
```

### YAML-based test

```yaml
# greet_test.yaml
yaml: |
  pipelines:
    greet:
      steps:
        - name: set_msg
          type: step.set
          config:
            values:
              message: "hello {{ .name }}"

tests:
  greet-alice:
    trigger:
      type: pipeline
      name: greet
      data:
        name: alice
    assertions:
      - output:
          message: "hello alice"
```

Run with Go test:

```sh
go test ./...
```

Or with wfctl:

```sh
wfctl test tests/
wfctl test tests/greet_test.yaml
```

---

## The `wftest` Package

### Creating a Harness

```go
h := wftest.New(t, opts...)
```

**Options:**

| Option | Description |
|--------|-------------|
| `WithConfig("path/to/config.yaml")` | Load engine config from a file |
| `WithYAML("pipelines: ...")` | Load engine config from an inline YAML string |
| `WithServer()` | Start a real HTTP server (enables `BaseURL()`, `GET`, `POST`) |
| `WithPlugin(p)` | Load an additional engine plugin |
| `MockStep(stepType, handler)` | Replace a step type with a mock implementation |
| `WithMockModule(mod)` | Register a mock module in the service registry |

### Executing Pipelines

```go
// Basic execution
result := h.ExecutePipeline("my-pipeline", map[string]any{"key": "value"})

// With execution options
result := h.ExecutePipelineOpts("my-pipeline", nil, wftest.StopAfter("step2"))
```

**Result fields:**

| Field | Type | Description |
|-------|------|-------------|
| `result.Output` | `map[string]any` | Final pipeline output |
| `result.Error` | `error` | Execution error (nil on success) |
| `result.Duration` | `time.Duration` | Elapsed time |
| `result.StepExecuted("name")` | `bool` | Whether a step ran |
| `result.StepOutput("name")` | `map[string]any` | A step's output map |

### HTTP Testing

When using `WithServer()`, the harness starts a real TCP server:

```go
h := wftest.New(t, wftest.WithConfig("config.yaml"), wftest.WithServer())

result := h.GET("/api/users")
result := h.POST("/api/users", `{"name":"alice"}`)
result := h.PUT("/api/users/1", `{"name":"bob"}`)
result := h.DELETE("/api/users/1")

// Check status and body
if result.StatusCode != 200 {
    t.Errorf("expected 200, got %d", result.StatusCode)
}
if !strings.Contains(string(result.RawBody), "alice") {
    t.Error("missing alice in response")
}

// Full URL access
t.Logf("server at %s", h.BaseURL())

// WebSocket
conn, _, err := h.WSDialer("/ws")
```

### Mocking Steps

Replace any registered step type with a mock:

```go
// Fixed output
h := wftest.New(t,
    wftest.WithYAML(`...`),
    wftest.MockStep("step.db_query", wftest.Returns(map[string]any{
        "rows":  []any{},
        "count": 0,
    })),
)

// Record calls and inspect them
rec := wftest.NewRecorder()
rec.WithOutput(map[string]any{"count": 3})

h := wftest.New(t,
    wftest.WithYAML(`...`),
    wftest.MockStep("step.db_query", rec),
)

result := h.ExecutePipeline("fetch-users", nil)
t.Logf("called %d times", rec.CallCount())
t.Logf("first call input: %v", rec.Calls()[0].Input)
```

### StopAfter

Halt a pipeline after a specific step to test partial execution:

```go
result := h.ExecutePipelineOpts("pipeline", nil, wftest.StopAfter("step2"))

if !result.StepExecuted("step1") { t.Error("step1 should have run") }
if !result.StepExecuted("step2") { t.Error("step2 should have run") }
if result.StepExecuted("step3")  { t.Error("step3 should NOT have run") }
```

---

## YAML Test Files

YAML test files (`*_test.yaml`) declare a workflow config and a set of named test
cases in a single file. Use them with `wftest.RunYAMLTests` in Go or `wfctl test`
from the command line.

### File Structure

```yaml
# Workflow config — choose one:
yaml: |
  pipelines: ...          # inline YAML string
# OR
config: path/to/config.yaml

# File-level mocks (apply to all tests unless overridden)
mocks:
  steps:
    step.db_query:
      rows: []
      count: 0

# Test cases
tests:
  my-test-name:
    description: "Optional human-readable label"
    trigger:
      type: pipeline          # pipeline | http.post | http.get
      name: my-pipeline
      data:
        key: value
    stop_after: step2         # optional: halt after this step
    mocks:                    # optional: per-test overrides
      steps:
        step.db_query:
          count: 5
    assertions:
      - output:               # pipeline output check
          message: "hello alice"
      - step: set_msg         # per-step output check
        output:
          message: "hello alice"
      - step: step2           # execution check
        executed: true
      - step: step3
        executed: false
```

### Assertions

| Assertion | Example | Description |
|-----------|---------|-------------|
| Pipeline output | `output: {key: value}` | Check key/value pairs in the final pipeline output |
| Step output | `step: my-step` + `output: {...}` | Check output of a specific step |
| Step executed | `step: my-step` + `executed: true` | Assert whether a step ran |
| HTTP status | `response: {status: 200}` | Check HTTP response code (Go tests with `WithServer()` only) |
| HTTP body | `response: {body: "hello"}` | Check response body substring (Go tests with `WithServer()` only) |

### Mock Precedence

Per-test `mocks` override file-level `mocks` for the same step type. Other step
types from the file-level mock are inherited unchanged.

### Running in Go

```go
// Run a single file
func TestMyPipeline(t *testing.T) {
    wftest.RunYAMLTests(t, "testdata/my_test.yaml")
}

// Run all *_test.yaml files in a directory
func TestAll(t *testing.T) {
    wftest.RunAllYAMLTests(t, "testdata")
}
```

### Running with wfctl

```sh
# Run a single file
wfctl test tests/my_test.yaml

# Run all *_test.yaml files in a directory
wfctl test tests/

# Verbose output
wfctl test -v tests/

# Multiple targets
wfctl test tests/ integration/
```

Exit code is 0 when all tests pass, 1 when any test fails.

> **Note:** `wfctl test` supports pipeline triggers only. HTTP trigger assertions
> (`response:`) require Go-based tests with `WithServer()`.

---

## Plugin Authors

To test a plugin's step types in isolation, load the plugin with `WithPlugin`:

```go
import (
    "testing"
    "github.com/GoCodeAlone/workflow/wftest"
    myplugin "github.com/example/my-plugin"
)

func TestMyStep(t *testing.T) {
    h := wftest.New(t,
        wftest.WithYAML(`
pipelines:
  run-my-step:
    steps:
      - name: s
        type: step.my_custom_step
        config:
          threshold: 10
`),
        wftest.WithPlugin(myplugin.New()),
    )

    result := h.ExecutePipeline("run-my-step", map[string]any{"value": 5})
    if result.Error != nil {
        t.Fatal(result.Error)
    }
}
```

---

## Patterns

### Table-driven tests with a shared harness

```go
func TestGreet(t *testing.T) {
    h := wftest.New(t, wftest.WithConfig("config.yaml"))

    cases := []struct {
        name    string
        input   string
        wantMsg string
    }{
        {"alice", "alice", "hello alice"},
        {"bob",   "bob",   "hello bob"},
    }

    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            result := h.ExecutePipeline("greet", map[string]any{"name": tc.input})
            if result.Output["message"] != tc.wantMsg {
                t.Errorf("want %q, got %q", tc.wantMsg, result.Output["message"])
            }
        })
    }
}
```

### Asserting a step did not execute

```go
result := h.ExecutePipelineOpts("pipeline", nil, wftest.StopAfter("validate"))
if result.StepExecuted("persist") {
    t.Error("persist should not run when validation stops the pipeline")
}
```

### Verifying a mock was called with specific input

```go
rec := wftest.NewRecorder()
h := wftest.New(t,
    wftest.WithConfig("config.yaml"),
    wftest.MockStep("step.send_email", rec),
)

h.ExecutePipeline("notify-user", map[string]any{"user_id": "42"})

if rec.CallCount() != 1 {
    t.Fatalf("expected 1 email, got %d", rec.CallCount())
}
if rec.Calls()[0].Input["user_id"] != "42" {
    t.Errorf("wrong user_id in email step input")
}
```

---

## Stateful Testing

For workflows that manage state across multiple pipeline calls (games, sessions,
multi-step transactions), `wftest` provides a `StateStore` and sequence execution.

### Go API

```go
func TestGameTurnSequence(t *testing.T) {
    h := wftest.New(t,
        wftest.WithConfig("config.yaml"),
        wftest.WithState(),  // enables StateStore
        wftest.MockStep("step.db_query", wftest.Returns(map[string]any{"rows": []any{}})),
    )

    // Seed initial state
    h.State().Seed("sessions", map[string]any{
        "game-1": map[string]any{
            "players": []string{"alice", "bob"},
            "turn":    "alice",
            "hp":      map[string]any{"alice": 30, "bob": 25},
        },
    })

    // Turn 1: alice attacks
    result := h.ExecutePipeline("attack", map[string]any{
        "game_id":  "game-1",
        "attacker": "alice",
        "target":   "bob",
    })
    if result.Error != nil {
        t.Fatal(result.Error)
    }

    // Assert state changed
    if err := h.State().Assert("sessions", map[string]any{
        "game-1": map[string]any{"turn": "bob"},
    }); err != nil {
        t.Errorf("state mismatch after turn 1: %v", err)
    }

    // Turn 2: bob attacks
    result = h.ExecutePipeline("attack", map[string]any{
        "game_id":  "game-1",
        "attacker": "bob",
        "target":   "alice",
    })

    // Assert turn rotated back
    if err := h.State().Assert("sessions", map[string]any{
        "game-1": map[string]any{"turn": "alice"},
    }); err != nil {
        t.Errorf("state mismatch after turn 2: %v", err)
    }
}
```

### Loading State from Fixture Files

```go
// Load complex initial state from a JSON or YAML file
h.State().LoadFixture("testdata/combat_setup.json", "sessions")
h.State().LoadFixture("testdata/inventory.yaml", "cache")
```

### StateStore Methods

| Method | Description |
|--------|-------------|
| `Seed(store, data)` | Load initial state from a map |
| `LoadFixture(path, store)` | Load state from a JSON/YAML file |
| `Get(store, key)` | Retrieve a single value |
| `Set(store, key, value)` | Write a value |
| `GetAll(store)` | Get all entries in a store |
| `Assert(store, expected)` | Check state matches expected (returns error on mismatch) |

### YAML Stateful Tests

Use `state:` for initial data and `sequence:` for multi-step execution with
intermediate state assertions.

```yaml
# game_test.yaml
config: config/app.yaml

tests:
  test_combat_round:
    state:
      fixtures:
        - file: testdata/combat_setup.json
          target: sessions
      seed:
        cache:
          game-1:deck: [card1, card2, card3]

    sequence:
      - name: warrior_attacks
        pipeline: attack
        trigger:
          body:
            game_id: game-1
            attacker: warrior
            target: goblin
        assertions:
          - step: calculate_damage
            output:
              damage: 8
          - state:
              sessions:
                game-1:
                  goblin_hp: 12

      - name: goblin_counterattacks
        pipeline: attack
        trigger:
          body:
            game_id: game-1
            attacker: goblin
            target: warrior
        assertions:
          - step: calculate_damage
            output:
              damage: 3
          - state:
              sessions:
                game-1:
                  warrior_hp: 27

      - name: warrior_draws_card
        pipeline: draw-card
        trigger:
          body:
            game_id: game-1
            player: warrior
        assertions:
          - step: draw
            output:
              card: card1
          - state:
              cache:
                game-1:deck: [card2, card3]
```

### State Block Reference

```yaml
state:
  # Load from files (JSON or YAML)
  fixtures:
    - file: testdata/setup.json    # path relative to test file
      target: sessions             # store name

  # Inline seed data
  seed:
    store_name:
      key1: value1
      key2:
        nested: data
```

### Sequence Steps

When `sequence:` is present (instead of `trigger:`), the harness executes each
step in order. State persists across all steps — the same harness instance is
reused throughout the sequence.

```yaml
sequence:
  - name: step_display_name       # for test output
    pipeline: pipeline-name        # which pipeline to execute
    trigger:                       # trigger data for this step
      type: http                   # or: pipeline, eventbus, scheduler
      method: POST
      path: /api/action
      body: { key: value }
    assertions:
      - step: step_name            # assert step output
        output: { field: value }
      - state:                     # assert state store contents
          store_name:
            key: expected_value
```

### Tips for Stateful Tests

- **Seed only what matters** — don't replicate your entire database schema; seed
  the specific keys your pipeline reads/writes.
- **Assert incrementally** — check state after each sequence step, not just at
  the end. This pinpoints exactly which step broke the state.
- **Use fixtures for complex state** — if your initial state is more than ~10
  lines, put it in a JSON file and use `fixtures:`.
- **State stores are isolated per test** — each `t.Run` subtest gets its own
  StateStore, so tests don't interfere with each other.

---

## BDD / Gherkin Tests

The `wftest/bdd` package provides pre-built [Gherkin](https://cucumber.io/docs/gherkin/)
step definitions backed by the `wftest.Harness`. Each scenario creates a fresh
harness so scenarios are fully isolated.

### Quick Start

Create a Go test file that calls `bdd.RunFeatures`:

```go
// features_test.go
package myapp_test

import (
    "testing"
    "github.com/GoCodeAlone/workflow/wftest/bdd"
)

func TestFeatures(t *testing.T) {
    bdd.RunFeatures(t, "features/",
        bdd.WithConfig("config.yaml"),
    )
}
```

Write feature files in `features/`:

```gherkin
# features/greet.feature
Feature: Greeting pipeline

  @pipeline:greet
  Scenario: Greet a user
    Given the workflow engine is loaded with "config.yaml"
    When I execute pipeline "greet" with:
      | name | alice |
    Then the pipeline should succeed
    And the pipeline output "message" should be "hello alice"
```

Run with: `go test ./... -run TestFeatures`

### Pre-Built Step Definitions

#### Engine Setup (`Given`)

| Step | Description |
|------|-------------|
| `the workflow engine is loaded with "path"` | Load config from a YAML file |
| `the workflow engine is loaded with config:` | Load inline YAML docstring |

#### Mocking (`Given`)

| Step | Description |
|------|-------------|
| `step "type" is mocked to return:` | Mock a step type with a key/value table |
| `step "type" returns JSON:` | Mock a step type with a JSON docstring |
| `module "name" "type" is mocked` | Mock a module in the service registry |

```gherkin
Given step "step.db_query" is mocked to return:
  | user_id | 42    |
  | name    | alice |

Given step "step.ai_call" returns JSON:
  """json
  {"summary": "looks good", "score": 0.95}
  """
```

#### HTTP Triggers (`When`)

| Step | Description |
|------|-------------|
| `I GET "path"` | GET request |
| `I GET "path" with header "name" = "value"` | GET with custom header |
| `I POST "path" with JSON:` | POST with JSON body (docstring) |
| `I POST "path" with:` | POST with table body |
| `I PUT "path" with JSON:` | PUT with JSON body (docstring) |
| `I DELETE "path"` | DELETE request |

HTTP steps require an `http.router` module in the config:

```gherkin
Given the workflow engine is loaded with config:
  """yaml
  modules:
    - name: router
      type: http.router
  pipelines:
    users-list:
      trigger:
        type: http
        config:
          method: GET
          path: /api/users
      steps: [...]
  """
When I GET "/api/users"
Then the response status should be 200
```

#### Pipeline / Event Triggers (`When`)

| Step | Description |
|------|-------------|
| `I execute pipeline "name"` | Execute a pipeline with no input |
| `I execute pipeline "name" with:` | Execute with key/value table as input |
| `I fire event "topic" with:` | Fire an eventbus event |
| `I fire schedule "name"` | Fire a named scheduler trigger |

#### State Setup (`Given`)

| Step | Description |
|------|-------------|
| `state "store" is seeded from "path"` | Load fixture JSON file into a state store |
| `state "store" has key "key" with:` | Inline seed a state store key with a table |

#### Assertions (`Then`)

| Step | Description |
|------|-------------|
| `the pipeline should succeed` | Assert no pipeline error |
| `the pipeline should fail` | Assert a pipeline error occurred |
| `the pipeline output "key" should be "value"` | Assert a pipeline output field |
| `step "name" should have been executed` | Assert step ran |
| `step "name" should not have been executed` | Assert step did not run |
| `step "name" output "key" should be "value"` | Assert step output string |
| `step "name" output "key" should be 42` | Assert step output integer |
| `the response status should be 200` | Assert HTTP status code |
| `the response body should contain "text"` | Assert HTTP body substring |
| `the response JSON "path" should be "value"` | Assert JSON body dot-path value |
| `the response JSON "path" should not be empty` | Assert JSON body dot-path non-empty |
| `the response header "name" should be "value"` | Assert HTTP response header |
| `state "store" key "k" field "f" should be "value"` | Assert state store field (string) |
| `state "store" key "k" field "f" should be 42` | Assert state store field (integer) |

### Options

| Option | Description |
|--------|-------------|
| `bdd.WithConfig(path)` | Default config file applied to every scenario |
| `bdd.WithYAML(yaml)` | Default inline YAML applied to every scenario |
| `bdd.WithMockStep(name, handler)` | Default mock step applied to every scenario |
| `bdd.Strict()` | Fail on undefined or pending steps |

### Strict Mode

In strict mode, undefined or pending steps cause the suite to fail. In the
default (lenient) mode, they are logged as warnings and the scenario is skipped.

```go
func TestFeatures(t *testing.T) {
    bdd.RunFeatures(t, "features/",
        bdd.WithConfig("config.yaml"),
        bdd.Strict(), // fail on undefined/pending steps
    )
}
```

### Pipeline Coverage

`wfctl test --coverage` performs static analysis of your app config and feature
directory to report which pipelines have BDD test coverage.

```
$ wfctl test --coverage config.yaml features/

Pipeline Coverage: 3/5 (60.0%)

COVERED:
  greet                                greet.feature:8 (tag)
  users-list                           api.feature:12 (route)
  users-create                         api.feature:24 (route)

UNCOVERED:
  payment-refund
  admin-report

Scenario Coverage:
  Total:     12
  With pipeline: 10 (83.3%)
  Without:       2
```

Pipelines are linked to features in two ways:

- **Explicit tag**: `@pipeline:name` on a scenario
- **Implicit route match**: `When I POST "/api/path"` steps matched against
  pipeline HTTP trigger configs in the config file

Use `--strict` with `--coverage` to fail if any pipelines are uncovered:

```
$ wfctl test --coverage --strict config.yaml features/
Error: strict: 2 pipeline(s) have no feature coverage: payment-refund, admin-report
```
