---
status: approved
area: wfctl
owner: workflow
implementation_refs: []
verification:
  last_checked: 2026-04-25
  commands:
    - GOWORK=off go test ./cmd/wfctl ./interfaces ./plugin/...
  result: pass
supersedes: []
superseded_by: []
---

# wfctl Lifecycle Product Design

Date: 2026-04-25

## Goal

Align `wfctl` around one product promise: portable application lifecycle management for Workflow projects, with CI/CD kept declarative and provider-specific plumbing owned by Workflow and plugins.

## Problem

`wfctl` has grown beyond validation and scaffolding. The command map includes setup, validation, build, deploy, infra, CI, registry, plugin management, MCP, security, dev clusters, tenants, tests, secrets, and wizard flows. The README still frames `wfctl` mostly as inspect/validate/generate, so user expectations lag behind the actual product surface.

Recent designs correctly push toward a stronger lifecycle product: plugin lockfiles, registry auth, typed IaC args, state-independent teardown, output reading, deploy verification, and secret sinks. The remaining gap is product coherence. Users should not need provider shell in CI when `wfctl` can own it. Users should not need to know which subcommand is old static code, dynamic plugin CLI, or a workflow pipeline.

## Design

### Product Surface

Define `wfctl` around these lifecycle stages:

1. Project setup: `init`, `wizard`, `scaffold`, `plugin add`, `plugin install`.
2. Authoring: `validate`, `schema`, `editor-schemas`, `dsl-reference`, `template validate`, `mcp`.
3. Local development: `dev up`, `dev status`, `run`, `test`, `ports`.
4. Build: `build`, `build-ui`, build hooks, SBOM/signing hooks.
5. Infrastructure: `infra plan/apply/destroy/teardown/status/drift/outputs/bootstrap/state`.
6. Deployment: `deploy`, `deploy verify`, rollback hooks.
7. CI/CD: `ci init/run/validate`, `generate`, provider-neutral command wrappers.
8. Operations: `secrets`, `security`, `tenant`, `docs`, `audit`.

Documentation, help text, and examples should use this lifecycle ordering consistently.

### Command Ownership

Each command should declare an owner:

- core static command
- workflow-backed command from `cmd/wfctl/wfctl.yaml`
- plugin dynamic command from installed `plugin.json`

The user-facing behavior should be identical regardless of owner. The command should provide `--help`, structured error output, and a representative dry-run or validation mode where appropriate.

### CI/CD Portability

The rule is: CI systems call `wfctl`; `wfctl` handles platform and provider differences.

Provider-specific shell in generated GitHub Actions, GitLab CI, Jenkins, or local scripts is a bug unless there is no supported provider abstraction yet. The correct response is to add a provider or plugin extension point, not to document copy-pasted shell.

`wfctl ci init` and `wfctl generate` should prefer the smallest portable command sequence:

```sh
wfctl plugin install
wfctl validate --dir config
wfctl build --push --env "$ENV"
wfctl infra apply --env "$ENV"
wfctl deploy --env "$ENV"
wfctl deploy verify --env "$ENV"
```

### Runtime Evidence

Every lifecycle command that touches runtime behavior needs representative verification:

- `cmd --help` exits 0 and includes the lifecycle category.
- representative dry-run exits 0.
- command writes step summary in CI mode.
- command failure prints a concise root cause.
- plugin-backed commands are included in command discovery tests.

## Error Handling

Errors should be grouped by lifecycle stage:

- configuration error: invalid YAML, missing plugin, bad schema
- environment error: missing credential, Docker unavailable, unauthenticated package registry
- provider error: cloud API failure, unsupported resource type
- safety error: destructive action missing approval
- integrity error: checksum/signature/SBOM failure

The CLI should unwrap internal workflow pipeline errors and print the actionable error, as it already does in `cmd/wfctl/main.go`.

## Testing

The immediate test baseline is:

```sh
GOWORK=off go test ./cmd/wfctl ./interfaces ./plugin/...
```

Follow-up tests should add:

- command tree snapshot test
- lifecycle category coverage test
- generated CI contains only `wfctl` lifecycle commands for supported providers
- dynamic plugin command conflict test
- build hook and install hook smoke tests

## Acceptance Criteria

- `docs/WFCTL.md`, README, and `wfctl --help` describe the same lifecycle model.
- Every top-level command has a lifecycle category and owner.
- Generated CI avoids provider shell when Workflow has a provider abstraction.
- Core `wfctl` tests pass with `GOWORK=off`.
- `wfctl audit plans` exists as the governance hook for design implementation evidence.
