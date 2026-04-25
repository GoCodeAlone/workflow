---
status: approved
area: core
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

# Core Runtime Boundaries Design

Date: 2026-04-25

## Goal

Clarify what belongs in `workflow` core versus external plugins, and define compatibility rules so extraction work does not break `wfctl`, editor schema export, runtime validation, or deployed applications.

## Problem

The March ecosystem restructuring design says core should keep HTTP, pipeline steps, auth, storage, messaging, observability, scheduling, cache, state machine, and OpenAPI, while extracting IaC, deployment, CI/CD, AI, actors, marketplace, GitLab, policy, and scanner concerns.

The current repo still has broad built-in surfaces for IaC, deployment, platform, AI, MCP, CI, registry, and security-related commands. Some of this is likely transition state; some may be intentional because `wfctl` needs core command interfaces even when execution is delegated to plugins. Without explicit boundary rules, every new feature risks drifting back into core.

## Boundary Model

Core owns:

- engine lifecycle
- configuration parsing and validation
- workflow handlers and trigger dispatch
- built-in fundamental modules
- pipeline execution primitives
- plugin SDK and external plugin protocol
- shared interfaces in `interfaces/`
- `wfctl` command router and lifecycle orchestration
- schema export and LSP-compatible metadata

Plugins own:

- cloud/provider implementation details
- integration-specific modules and steps
- IaC resource drivers
- migration drivers
- supply-chain scanners/signers
- SaaS-specific API clients
- game/domain verticals
- dynamic `wfctl` CLI commands that are not part of the universal lifecycle

`wfctl` can own orchestration while plugins own execution. For example, `wfctl infra apply` is core lifecycle orchestration, but AWS, GCP, Azure, DigitalOcean, and OpenTofu resource behavior belongs in plugins.

## Interface Rules

Shared interfaces should live in `workflow/interfaces` only when at least two plugins or one core command and one plugin need the contract.

Interface changes require:

- typed request/response structs where the boundary crosses gRPC or JSON maps
- compatibility tests for old and new plugin behavior where possible
- clear `minEngineVersion` bump guidance
- plugin matrix update

The typed IaC args work is the model to follow: silent `map[string]any` mismatches should become decode errors with method names and missing-field context.

## Extraction Rules

Before extracting a core surface into a plugin:

1. Define the shared interface in `interfaces/`.
2. Add a core adapter or registry.
3. Add schema export support for external declarations.
4. Add `wfctl validate --plugin-dir` coverage.
5. Add editor schema coverage.
6. Add migration notes for existing configs.
7. Add conformance tests for plugin authors.

No extraction is complete until a representative external plugin passes conformance and a representative app config validates without core-only knowledge.

## Runtime Compatibility

Compatibility must cover three planes:

- config plane: YAML and schema validation
- process plane: external plugin discovery, startup, unload, reload
- command plane: `wfctl` lifecycle commands and plugin dynamic commands

Breaking changes should be staged:

- release N: add new path, warn on old path
- release N+1: migrate generated templates and docs
- release N+2: remove old path only if audit shows no active in-repo scenario uses it

## Testing

Baseline:

```sh
GOWORK=off go test ./cmd/wfctl ./interfaces ./plugin/...
```

Follow-up tests:

- interface marshal/unmarshal conformance
- external plugin load and schema export smoke test
- `wfctl validate --plugin-dir` for representative plugin configs
- editor schema export includes external plugin fields
- scenario validation for configs using extracted surfaces

## Acceptance Criteria

- Core/plugin ownership is documented and enforced in review.
- Shared interfaces have typed boundary tests.
- Extraction work includes validation, schema export, editor, and scenario coverage.
- `wfctl` remains the lifecycle orchestrator without absorbing provider-specific implementations.
- Old plans can be marked superseded when this boundary model replaces them.
