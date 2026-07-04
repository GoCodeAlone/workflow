# Repository Layout

This repository is a multi-surface Go project. Keep the top level reserved for
durable product packages and documented roots. Do not commit throwaway generated
application directories at the root.

## Main Roots

| Path | Purpose |
|------|---------|
| `cmd/server/` | Workflow server binary. |
| `cmd/wfctl/` | CLI and MCP-facing lifecycle tooling. |
| `cmd/workflow-lsp-server/` | Language server. |
| `config/` | YAML config structs and import/merge logic. |
| `module/` | Built-in module and pipeline step implementations. |
| `handlers/` | Workflow handler types. |
| `plugins/` | Built-in engine plugins. |
| `plugin/` | Plugin SDK, manifests, external plugin adapters, and contracts. |
| `cigen/` | Config-derived CI plan analyzer and built-in renderers. |
| `lint/` | Reusable go/analysis-style static checkers, parameterized for use by any Workflow host app (e.g. `lint/lockio`). |
| `iac/`, `infra/`, `platform/`, `provider/` | Infrastructure planning, provider, and platform abstractions. |
| `ui/` | React visual builder. |
| `mcp/` | MCP server and tools. |
| `docs/` | User, operator, design, and reference documentation. |
| `decisions/` | ADRs and durable technical decisions. |
| `example/` | Runnable examples, sample apps, and example-only Go module. |
| `deploy/` | Deployment manifests and operational examples. |
| `test/`, `tests/`, `wftest/` | Shared test fixtures, integration suites, and workflow test harness code. |

## Examples

Use the singular `example/` directory for committed examples.

- Top-level `example/*.yaml` files are runnable config examples and should stay
  readable as documentation.
- Full sample apps belong under `example/<app-name>/`.
- External plugin samples belong under `example/<plugin-or-feature-name>/`.
- Do not add a top-level `examples/` directory in this repo.

If a generated scaffold is only scratch output, keep it outside the repo or in an
ignored temporary path. If it becomes a durable example, move it under
`example/<purpose>/` with a README and validation path.

## Tests And Fixtures

- Unit tests live next to source as `*_test.go`.
- Package-local fixtures use `testdata/`.
- Cross-package integration/load/chaos suites use `tests/`.
- Workflow test framework code lives in `wftest/`.
- Do not add root-level `test-*` or ad hoc directories such as `mytest2`.

## CI Generation Boundary

`wfctl ci plan` and `wfctl ci generate` use the core `cigen` package for the
platform-neutral plan and first-class built-in renderers. The
`workflow-plugin-ci-generator` plugin consumes that same core package for
workflow steps. Provider/API plugins such as `workflow-plugin-github`,
`workflow-plugin-gitlab`, and `workflow-plugin-infra` are not renderer owners.
See `decisions/0045-ci-generation-boundary.md`.
