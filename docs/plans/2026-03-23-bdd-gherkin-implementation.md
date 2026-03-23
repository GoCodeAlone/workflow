# wftest/bdd Gherkin Support — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add BDD/Gherkin support to wftest with pre-built godog step definitions, pipeline + scenario coverage, and strict mode for CI.

**Architecture:** New `wftest/bdd` sub-package wrapping `cucumber/godog`. Each Gherkin scenario creates a fresh `wftest.Harness`. Pre-built step definitions map Gherkin phrases to harness methods. Coverage scans app.yaml + feature files. Strict mode detects undefined steps.

**Tech Stack:** Go 1.26, cucumber/godog v0.15.1, wftest harness

**Design Doc:** `docs/plans/2026-03-23-bdd-gherkin-design.md`

---

### Task 1: BDD Context + Runner Foundation

**Files:**
- Create: `wftest/bdd/context.go`
- Create: `wftest/bdd/runner.go`
- Create: `wftest/bdd/options.go`
- Create: `wftest/bdd/runner_test.go`
- Create: `wftest/bdd/testdata/minimal.feature`

**Step 1: Add godog dependency**

```bash
cd /Users/jon/workspace/workflow
go get github.com/cucumber/godog@v0.15.1
go mod tidy
```

**Step 2: Create BDD context**

`wftest/bdd/context.go` — wraps a `wftest.Harness` per scenario:

```go
package bdd

import (
    "testing"
    "github.com/GoCodeAlone/workflow/wftest"
)

// ScenarioContext holds state for a single Gherkin scenario.
type ScenarioContext struct {
    t       *testing.T
    harness *wftest.Harness
    result  *wftest.Result
    opts    []wftest.Option
}
```

**Step 3: Create runner**

`wftest/bdd/runner.go` — `RunFeatures(t, path, opts...)`:
- Creates a godog test suite
- Registers all pre-built step definitions
- Runs against the specified feature file or directory
- Each scenario gets a fresh ScenarioContext with a new Harness

**Step 4: Create options**

`wftest/bdd/options.go`:
```go
type Option func(*Config)
func WithConfig(path string) Option
func WithYAML(yaml string) Option
func WithMockStep(name string, handler wftest.StepHandler) Option
func Strict() Option
```

**Step 5: Create minimal test feature**

`wftest/bdd/testdata/minimal.feature`:
```gherkin
Feature: Minimal BDD test
  Scenario: Execute a simple pipeline
    Given the workflow engine is loaded with config:
      """yaml
      pipelines:
        greet:
          steps:
            - name: hello
              type: step.set
              config:
                values:
                  message: "world"
      """
    When I execute pipeline "greet"
    Then the pipeline should succeed
    And the pipeline output "message" should be "world"
```

**Step 6: Write runner test**

`wftest/bdd/runner_test.go`:
```go
func TestRunFeatures_Minimal(t *testing.T) {
    RunFeatures(t, "testdata/minimal.feature")
}
```

**Step 7: Run test, verify it fails (no step definitions yet)**

Run: `go test ./wftest/bdd/ -v -count=1`
Expected: FAIL — step definitions not implemented

**Step 8: Commit**

```bash
git commit -m "feat(wftest/bdd): add BDD runner foundation with godog"
```

---

### Task 2: Engine Setup Step Definitions

**Files:**
- Create: `wftest/bdd/steps_engine.go`
- Modify: `wftest/bdd/runner.go` — register engine steps

**Step 1: Implement engine setup steps**

```go
// "Given the workflow engine is loaded with {string}"
func (sc *ScenarioContext) theEngineIsLoadedWithFile(path string) error

// "Given the workflow engine is loaded with config:" (docstring)
func (sc *ScenarioContext) theEngineIsLoadedWithConfig(config *godog.DocString) error
```

**Step 2: Register in runner's InitializeScenario**

**Step 3: Verify minimal.feature passes**

Run: `go test ./wftest/bdd/ -run TestRunFeatures_Minimal -v -count=1`

**Step 4: Commit**

```bash
git commit -m "feat(wftest/bdd): add engine setup step definitions"
```

---

### Task 3: Mock Step Definitions

**Files:**
- Create: `wftest/bdd/steps_mock.go`
- Create: `wftest/bdd/testdata/mock.feature`

**Step 1: Implement mock steps**

```go
// "Given step {string} is mocked to return:" (table)
func (sc *ScenarioContext) stepIsMockedToReturnTable(stepType string, table *godog.Table) error

// "Given step {string} returns JSON:" (docstring)
func (sc *ScenarioContext) stepReturnsJSON(stepType string, doc *godog.DocString) error

// "Given module {string} {string} is mocked"
func (sc *ScenarioContext) moduleIsMocked(moduleType, name string) error
```

**Step 2: Write mock.feature test**

```gherkin
Feature: Mock support
  Scenario: Mock step with table
    Given the workflow engine is loaded with config:
      """yaml
      pipelines:
        query:
          steps:
            - name: fetch
              type: step.db_query
              config:
                database: db
                query: "SELECT 1"
                mode: single
      """
    And step "step.db_query" returns JSON:
      """json
      {"row": {"id": 1, "name": "test"}, "found": true}
      """
    When I execute pipeline "query"
    Then the pipeline should succeed
```

**Step 3: Run tests, commit**

```bash
git commit -m "feat(wftest/bdd): add mock step definitions"
```

---

### Task 4: HTTP Trigger Step Definitions

**Files:**
- Create: `wftest/bdd/steps_http.go`
- Create: `wftest/bdd/testdata/http.feature`

**Step 1: Implement HTTP steps**

```go
// "When I POST {string} with JSON:" (docstring)
func (sc *ScenarioContext) iPOSTWithJSON(path string, doc *godog.DocString) error

// "When I GET {string}"
func (sc *ScenarioContext) iGET(path string) error

// "When I GET {string} with header {string} = {string}"
func (sc *ScenarioContext) iGETWithHeader(path, header, value string) error

// "When I PUT {string} with JSON:" (docstring)
func (sc *ScenarioContext) iPUTWithJSON(path string, doc *godog.DocString) error

// "When I DELETE {string}"
func (sc *ScenarioContext) iDELETE(path string) error

// "When I POST {string} with:" (table — key/value pairs)
func (sc *ScenarioContext) iPOSTWithTable(path string, table *godog.Table) error
```

**Step 2: Write http.feature test**

**Step 3: Run tests, commit**

```bash
git commit -m "feat(wftest/bdd): add HTTP trigger step definitions"
```

---

### Task 5: Pipeline + Event + Schedule Trigger Step Definitions

**Files:**
- Create: `wftest/bdd/steps_trigger.go`
- Create: `wftest/bdd/testdata/triggers.feature`

**Step 1: Implement trigger steps**

```go
// "When I execute pipeline {string}"
func (sc *ScenarioContext) iExecutePipeline(name string) error

// "When I execute pipeline {string} with:" (table)
func (sc *ScenarioContext) iExecutePipelineWith(name string, table *godog.Table) error

// "When I fire event {string} with:" (table)
func (sc *ScenarioContext) iFireEventWith(topic string, table *godog.Table) error

// "When I fire schedule {string}"
func (sc *ScenarioContext) iFireSchedule(name string) error
```

**Step 2: Write triggers.feature**

**Step 3: Commit**

```bash
git commit -m "feat(wftest/bdd): add pipeline, event, schedule trigger steps"
```

---

### Task 6: Response + Step Output Assertion Step Definitions

**Files:**
- Create: `wftest/bdd/steps_assert.go`
- Create: `wftest/bdd/testdata/assertions.feature`

**Step 1: Implement assertion steps**

```go
// "Then the response status should be {int}"
func (sc *ScenarioContext) theResponseStatusShouldBe(code int) error

// "Then the response body should contain {string}"
func (sc *ScenarioContext) theResponseBodyShouldContain(text string) error

// "Then the response JSON {string} should be {string}"
func (sc *ScenarioContext) theResponseJSONShouldBe(path, expected string) error

// "Then the response JSON {string} should not be empty"
func (sc *ScenarioContext) theResponseJSONShouldNotBeEmpty(path string) error

// "Then the response header {string} should be {string}"
func (sc *ScenarioContext) theResponseHeaderShouldBe(header, expected string) error

// "Then the pipeline should succeed"
func (sc *ScenarioContext) thePipelineShouldSucceed() error

// "Then the pipeline should fail"
func (sc *ScenarioContext) thePipelineShouldFail() error

// "Then the pipeline output {string} should be {string}"
func (sc *ScenarioContext) thePipelineOutputShouldBe(key, expected string) error

// "Then step {string} should have been executed"
func (sc *ScenarioContext) stepShouldHaveBeenExecuted(name string) error

// "Then step {string} should not have been executed"
func (sc *ScenarioContext) stepShouldNotHaveBeenExecuted(name string) error

// "Then step {string} output {string} should be {int}"
func (sc *ScenarioContext) stepOutputShouldBeInt(step, key string, expected int) error

// "Then step {string} output {string} should be {string}"
func (sc *ScenarioContext) stepOutputShouldBeString(step, key, expected string) error
```

**Step 2: Write assertions.feature with multiple scenarios**

**Step 3: Verify minimal.feature now fully passes**

**Step 4: Commit**

```bash
git commit -m "feat(wftest/bdd): add response and step output assertion steps"
```

---

### Task 7: State Step Definitions

**Files:**
- Create: `wftest/bdd/steps_state.go`
- Create: `wftest/bdd/testdata/state.feature`
- Create: `wftest/bdd/testdata/fixture.json`

**Step 1: Implement state steps**

```go
// "Given state {string} is seeded from {string}"
func (sc *ScenarioContext) stateIsSeededFrom(store, path string) error

// "Given state {string} has key {string} with:" (table)
func (sc *ScenarioContext) stateHasKeyWith(store, key string, table *godog.Table) error

// "Then state {string} key {string} field {string} should be {string}"
func (sc *ScenarioContext) stateFieldShouldBe(store, key, field, expected string) error

// "Then state {string} key {string} field {string} should be {int}"
func (sc *ScenarioContext) stateFieldShouldBeInt(store, key, field string, expected int) error
```

**Step 2: Write state.feature with stateful multi-step scenario**

**Step 3: Commit**

```bash
git commit -m "feat(wftest/bdd): add state seed and assertion step definitions"
```

---

### Task 8: Pipeline Coverage

**Files:**
- Create: `wftest/bdd/coverage.go`
- Create: `wftest/bdd/coverage_test.go`

**Step 1: Implement pipeline coverage scanning**

```go
// PipelineCoverage scans an app.yaml for pipeline names and .feature files
// for pipeline references. Returns covered and uncovered pipeline lists.
type CoverageReport struct {
    TotalPipelines    int
    CoveredPipelines  []PipelineCoverageEntry
    UncoveredPipelines []string
    TotalScenarios    int
    ImplementedScenarios int
    PassingScenarios  int
    PendingScenarios  int
    UndefinedScenarios int
}

func CalculateCoverage(configPath string, featureDir string) (*CoverageReport, error)
```

Pipeline detection:
- Parse app.yaml, extract all pipeline names from `pipelines:` section
- Parse .feature files for `@pipeline:name` tags
- Parse .feature files for HTTP route patterns (`POST "/api/v1/..."`) and match against pipeline trigger configs

**Step 2: Write tests with sample config + features**

**Step 3: Commit**

```bash
git commit -m "feat(wftest/bdd): add pipeline + scenario coverage calculation"
```

---

### Task 9: Strict Mode

**Files:**
- Create: `wftest/bdd/strict.go`
- Modify: `wftest/bdd/runner.go` — add strict option
- Create: `wftest/bdd/testdata/undefined.feature`

**Step 1: Implement strict mode**

When `Strict()` option is set:
- After running all features, check for scenarios with undefined or pending steps
- Return error listing each undefined step with file:line
- In lenient mode (default), log warnings but don't fail

**Step 2: Write test with an intentionally undefined step**

`testdata/undefined.feature`:
```gherkin
Feature: Undefined steps
  Scenario: This has an undefined step
    Given the workflow engine is loaded with config:
      """yaml
      pipelines:
        test:
          steps:
            - name: s
              type: step.set
              config:
                values: { x: 1 }
      """
    When I do something that has no step definition
    Then it should fail in strict mode
```

Test that lenient mode passes, strict mode fails.

**Step 3: Commit**

```bash
git commit -m "feat(wftest/bdd): add strict mode for undefined step detection"
```

---

### Task 10: wfctl test Integration + Docs

**Files:**
- Modify: `cmd/wfctl/test.go` — add `--coverage` and `--strict` flags, add `.feature` file support
- Modify: `docs/testing.md` — add BDD section

**Step 1: Update wfctl test to support .feature files**

When `wfctl test` encounters `.feature` files (or a directory containing them), use `bdd.RunFeatures` instead of the YAML runner. Detect by file extension.

Add flags:
- `--coverage` — print pipeline + scenario coverage report
- `--strict` — fail on undefined/pending steps

**Step 2: Update docs/testing.md**

Add BDD section covering:
- Writing .feature files
- Available step definitions (full reference)
- Running with `wfctl test features/`
- Coverage: `wfctl test --coverage config/`
- Strict mode: `wfctl test --strict features/`
- Go integration: `bdd.RunFeatures(t, ...)`
- Linking features to pipelines via `@pipeline:name` tags

**Step 3: Commit**

```bash
git commit -m "feat: add BDD support to wfctl test + update testing docs"
```
