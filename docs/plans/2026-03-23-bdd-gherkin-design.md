# wftest/bdd — Gherkin BDD Support for Workflow Testing

**Date:** 2026-03-23
**Status:** Approved

## Overview

Add BDD/Gherkin support to the wftest integration test harness. Pre-built godog step definitions map Gherkin scenarios to wftest harness methods. Pipeline + scenario coverage reporting. Strict mode for CI enforcement of unimplemented features.

## Architecture

```
wftest/bdd/
├── steps.go          # Pre-built godog step definitions
├── context.go        # BDD test context (wraps wftest.Harness per scenario)
├── coverage.go       # Pipeline + scenario coverage calculation
├── strict.go         # Undefined/pending step detection
├── runner.go         # RunFeatures(t, dir, opts...) integration point
└── runner_test.go
```

**Dependency:** `github.com/cucumber/godog v0.15.1` (same version as modular)

## Pre-Built Step Definitions

### Engine Setup

```gherkin
Given the workflow engine is loaded with "config/app.yaml"
Given the workflow engine is loaded with config:
  """yaml
  pipelines:
    greet:
      steps:
        - name: say_hello
          type: step.set
          config:
            values:
              message: "hello"
  """
```

### Mocking

```gherkin
Given step "step.db_query" is mocked to return:
  | key   | value |
  | rows  | []    |
  | count | 0     |
Given step "step.db_query" returns JSON:
  """json
  {"rows": [{"id": 1, "email": "test@example.com"}], "count": 1}
  """
Given module "database" "db" is mocked
```

### HTTP Triggers

```gherkin
When I POST "/api/v1/auth/register" with JSON:
  """json
  {"email": "test@example.com", "password": "secret123"}
  """
When I GET "/api/v1/users/me" with header "Authorization" = "Bearer token123"
When I PUT "/api/v1/users/123" with:
  | name | Updated Name |
When I DELETE "/api/v1/items/456"
```

### Pipeline Triggers

```gherkin
When I execute pipeline "process-order" with:
  | order_id | 123     |
  | items    | [a, b]  |
When I fire event "user.created" with:
  | user_id | 123 |
When I fire schedule "daily-cleanup"
```

### Response Assertions

```gherkin
Then the response status should be 201
Then the response body should contain "success"
Then the response JSON "user.id" should not be empty
Then the response JSON "error" should be "email required"
Then the response header "Content-Type" should be "application/json"
```

### Step Assertions

```gherkin
Then step "insert_user" should have been executed
Then step "send_email" should not have been executed
Then step "calculate_damage" output "damage" should be 8
Then step "insert_user" output "rows_affected" should be 1
```

### State Assertions

```gherkin
Given state "sessions" is seeded from "testdata/combat_setup.json"
Given state "sessions" has key "game-1" with:
  | players | ["alice", "bob"] |
  | turn    | alice            |
Then state "sessions" key "game-1" field "goblin_hp" should be 12
Then state "sessions" key "game-1" field "turn" should be "bob"
```

### Sequence (Multi-Step Stateful)

```gherkin
Scenario: Full combat round
  Given the workflow engine is loaded with "config/app.yaml"
  And state "sessions" is seeded from "testdata/combat.json"

  When I execute pipeline "attack" with:
    | game_id  | game-1  |
    | attacker | warrior |
    | target   | goblin  |
  Then step "calculate_damage" output "damage" should be 8
  And state "sessions" key "game-1" field "goblin_hp" should be 12

  When I execute pipeline "attack" with:
    | game_id  | game-1  |
    | attacker | goblin  |
    | target   | warrior |
  Then state "sessions" key "game-1" field "warrior_hp" should be 27
```

## Go Integration

```go
func TestFeatures(t *testing.T) {
    bdd.RunFeatures(t, "features/",
        bdd.WithConfig("config/app.yaml"),
        bdd.WithMockStep("step.db_query", wftest.Returns(defaultDBResponse)),
    )
}

func TestAuthFeatures(t *testing.T) {
    bdd.RunFeatures(t, "features/auth.feature",
        bdd.WithConfig("config/app.yaml"),
    )
}
```

## Coverage

### Pipeline Coverage

Scans app.yaml for all pipeline names, scans .feature files for pipeline references (explicit `@pipeline:name` tags + implicit HTTP route matching), reports gaps.

```
$ wfctl test --coverage config/

Pipeline Coverage: 45/78 (57.7%)

COVERED:
  auth-register .............. auth.feature:12
  auth-login ................ auth.feature:28
  wishlist-create ........... wishlists.feature:5

UNCOVERED:
  payment-refund
  admin-dispute-update
  cron-expire-claims
```

### Scenario Coverage

Counts total scenarios across .feature files, runs them, reports implementation status.

```
Scenario Coverage:
  Total:       85 scenarios
  Implemented: 72 (84.7%)
  Passing:     70 (82.4%)
  Pending:      2 (2.4%)
  Undefined:   13 (15.3%)
```

## Feature ↔ Pipeline Linking

Two methods:

**Explicit:** `@pipeline:name` tag on scenarios
```gherkin
@pipeline:auth-register
Scenario: Successful registration
  When I POST "/api/v1/auth/register" with JSON:
  ...
```

**Implicit:** HTTP route matching — `POST /api/v1/auth/register` matched against pipeline trigger configs in app.yaml.

## Strict Mode

`wfctl test --strict` (or `bdd.RunFeatures(t, ..., bdd.Strict())`) fails on undefined/pending steps:

```
$ wfctl test --strict features/

FAIL: 3 scenarios have undefined steps:
  auth.feature:23 - Given the user has MFA enabled
  payment.feature:45 - When the webhook signature is invalid
  admin.feature:12 - Then the audit log should contain an entry

Exit code: 1
```

Default is lenient — undefined steps warn but don't fail. Allows incremental development.

## Implementation Phases

### Phase 1: Core BDD Runner + Step Definitions
- wftest/bdd package with godog dependency
- context.go — BDD context wrapping wftest.Harness
- steps.go — all pre-built step definitions (engine setup, mocking, HTTP, pipeline, response, step, state assertions)
- runner.go — RunFeatures(t, path, opts...)
- Test with sample .feature file

### Phase 2: Coverage
- coverage.go — pipeline coverage (scan app.yaml + feature files)
- coverage.go — scenario coverage (count defined vs implemented vs passing)
- wfctl test --coverage integration

### Phase 3: Strict Mode + CLI
- strict.go — undefined/pending step detection
- wfctl test --strict integration
- Update docs/testing.md with BDD section
