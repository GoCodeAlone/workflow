# Agent Guide

Use this guide for agent-specific operating rules. Keep root agent files short
and link here instead of duplicating long instructions.

## Commands

When this repo is checked out under a parent `go.work`, prefix Go commands with
`GOWORK=off` so tests resolve against this repository's `go.mod`.

```sh
GOWORK=off go test ./...
GOWORK=off go test ./cmd/wfctl -run TestName -count=1
GOWORK=off golangci-lint run
make build-wfctl
make build-examples
make test-configs
```

For UI work:

```sh
cd ui
npm ci
npm test -- --run
npm run dev
```

## Working Rules

- Inspect local `AGENTS.md`, `CLAUDE.md`, README, and relevant docs before
  changing a repo.
- Use clean worktrees for broad or release-bound changes when the primary
  checkout is dirty.
- Do not revert unrelated user changes.
- Prefer focused tests first, then broaden based on blast radius.
- Keep examples in `example/`; see `docs/REPO_LAYOUT.md`.
- Update docs when behavior, commands, config shape, or repo layout changes.

## Common Change Points

| Change | Usual updates |
|--------|---------------|
| New module type | `module/` or plugin implementation, schema metadata, example YAML, docs. |
| New pipeline step | Step implementation, plugin registration, tests, docs. |
| New wfctl command | `cmd/wfctl/`, `docs/WFCTL.md`, usage text, tests. |
| Config format change | `config/`, schema/validation tests, docs. |
| CI generation change | `cigen/`, `cmd/wfctl/ci*.go`, `docs/WFCTL.md`, and possibly `workflow-plugin-ci-generator`. |

## Plugin Boundary

Workflow is plugin-first, but not every extension point should be moved out of
core immediately. Core keeps bootstrap-critical CLI behavior and shared
contracts; external plugins own provider-specific runtime integrations. For CI
generation, see `decisions/0045-ci-generation-boundary.md`. For native
provider job execution, keep the shared `IaCProviderRunner` contract in core and
implement cloud-specific runners in provider plugins.
