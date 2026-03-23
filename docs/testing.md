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
