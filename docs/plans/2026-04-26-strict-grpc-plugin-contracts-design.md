---
status: in_progress
area: plugins
owner: workflow
implementation_refs:
  - repo: workflow
    commit: 4150f78
  - repo: workflow
    commit: 8daa224
  - repo: workflow
    commit: 72d2477
  - repo: workflow
    commit: 5c135a0
  - repo: workflow
    commit: dd1b222
  - repo: workflow
    commit: 64c15fa
  - repo: workflow
    commit: 95e80ad
  - repo: workflow
    commit: e91187f
  - repo: workflow
    commit: eb53150
  - repo: workflow-plugin-ci-generator
    commit: 5c158ff154ce43d391473a4ed6cd3d3bf7788931
  - repo: workflow-plugin-approval
    commit: 48898f12c10d800d8b60cc9ee06e81c1580d3f01
  - repo: workflow-plugin-gitlab
    commit: 93eb57223b9401e8a3bc0812e71211cb3a3770fa
  - repo: workflow-plugin-marketplace
    commit: 7ce430e709160e4c77527bb3ce1ee8ff2dd22309
  - repo: workflow-plugin-infra
    commit: 6c1802de752f71b1eac895805e01e2e32f92bb5c
  - repo: workflow-plugin-rooms
    commit: 575a6171de8ae8ea436cd957b0c279f6dc0c3e34
  - repo: workflow-plugin-botdetect
    commit: 40b9a4c11937ce26c01eea292d67bc8c9d098211
  - repo: workflow-plugin-audit
    commit: a720577335c76422e5f732484de1864464e8183d
  - repo: workflow-plugin-sso
    commit: b94f3a661df9f5862b7b736b5824fda2b76d47e8
  - repo: workflow-plugin-ws-auth
    commit: 4a27b88580fc64109a5bd723d3dc0b837342dec3
external_refs:
  - "#76"
verification:
  last_checked: 2026-04-27
  commands:
    - GOWORK=off go test ./plugin/external/... ./cmd/wfctl -count=1
    - GOWORK=off go run ./cmd/wfctl audit plugins --repo-root "$WORKSPACE" --strict-contracts
  notes:
    - Set WORKSPACE to the local checkout root that contains the workflow repo and sibling plugin/application repositories.
  result: partial
supersedes: []
superseded_by: []
---

# Strict gRPC Plugin Contracts Design

Date: 2026-04-26

## Implementation Checkpoint

Core Workflow support is implemented through `workflow eb53150`: contract descriptors, plugin-owned descriptor-set based dynamic codecs, typed SDK adapters, host-side strict dispatch, strict input projection, typed integer output normalization, strict module error surfacing, `wfctl` strict contract audit/validation, and strict scaffolding when run from a Workflow source checkout.

Downstream strict-contract migrations are merged for `workflow-plugin-ci-generator`, `workflow-plugin-approval`, `workflow-plugin-gitlab`, `workflow-plugin-marketplace`, `workflow-plugin-infra`, `workflow-plugin-rooms`, `workflow-plugin-botdetect`, `workflow-plugin-audit`, `workflow-plugin-sso`, `workflow-plugin-ws-auth`, `workflow-plugin-authz`, `workflow-plugin-security`, `workflow-plugin-authz-ui`, and `workflow-plugin-auth`. `workflow-plugin-security-scanner` has an open monitored PR; `workflow-plugin-admin`, `workflow-plugin-agent`, and `workflow-plugin-azure` are verified locally and awaiting PRs; `workflow-plugin-aws` is active.

The design remains `in_progress` because downstream plugin and application repositories still need migration from map-only boundaries to typed descriptors and adapters. The workspace strict-contract audit is expected to fail until that migration completes.

## Goal

Move Workflow external plugins from ad hoc `map[string]any` and `google.protobuf.Struct` contracts to explicit proto-backed contracts that fail during code generation, compilation, or startup conformance checks instead of during live workflow execution.

## Current State

The external plugin proto uses `google.protobuf.Struct` for:

- module creation config
- step creation config
- runtime step config
- trigger data, current data, metadata, and prior step outputs
- service invocation args and results
- host callback payload fields

The public SDK mirrors those fields as `map[string]any`. Plugin repos then parse values with local helpers. This causes repeated failures: wrong key names, lossy numeric coercion, missing required values, shape drift between config schemas and runtime code, and one-sided boundary changes where only the host or plugin is updated.

## Brainstormed Approaches

### Approach 1: Replace Every `Struct` With Concrete RPCs

Each plugin defines a service with concrete methods, requests, and responses, and Workflow calls those services directly.

This gives the strongest compile-time checks, but it breaks the generic plugin host model. The engine would need to know every plugin service at compile time or implement dynamic service discovery and dispatch. That is too much surface area for the first migration.

### Approach 2: Keep Generic RPCs, Add JSON Schema Validation

Plugins keep `Struct`, but every config and runtime payload is validated against JSON Schema before crossing the boundary.

This is useful as a safety net, but it still catches problems at runtime and does not give plugin authors generated request and response types. It should remain a compatibility layer, not the target architecture.

### Approach 3: Typed Contract Descriptors With Generated Adapters

Workflow keeps the generic lifecycle RPC shape, but the wire payload for module config, step config, step input, step output, and service calls becomes a typed contract envelope. Each plugin declares contract descriptors that point to proto message type names. Generated SDK adapters marshal and unmarshal concrete Go proto messages behind stable generic lifecycle methods.

This preserves the host/plugin lifecycle model, supports incremental migration, and moves most mistakes to compile time. It also allows startup conformance checks: the host can require that every advertised module, step, trigger, and service method has a declared input/output contract.

Recommended: Approach 3, with JSON Schema/legacy `Struct` as a temporary compatibility path.

## Contract Model

Add a `ContractRegistry` RPC and manifest section that describe every boundary:

- `module_type`
- `step_type`
- `trigger_type`
- `service_name` and `method`
- config message type
- input message type
- output message type
- compatibility mode: `STRICT_PROTO`, `PROTO_WITH_LEGACY_STRUCT`, or `LEGACY_STRUCT`

Strict contracts use `google.protobuf.Any` containing generated proto messages. Legacy contracts continue to use `Struct`, but only when explicitly declared. A plugin without descriptors is treated as legacy during one transition window, then rejected by `wfctl plugin validate --strict-contracts`.

## SDK Shape

The SDK should expose generic typed helpers instead of requiring plugin authors to hand-write map parsing:

```go
type TypedStep[Config proto.Message, Input proto.Message, Output proto.Message] interface {
	ExecuteTyped(context.Context, *StepRequest[Config, Input]) (*StepResponse[Output], error)
}
```

Generated adapters implement the existing `StepInstance` and `ModuleProvider` interfaces while calling typed methods internally. That lets existing host lifecycle code continue to work while plugin repos migrate one step or module at a time.

## Wire Compatibility

The first core change should be additive:

- add `ContractDescriptor` messages and `GetContractRegistry`
- add typed `Any` fields beside existing `Struct` fields
- add host-side preference for typed fields when descriptors require them
- keep old `Struct` fields for legacy plugins

The second core change should make strict mode enforceable:

- `wfctl plugin validate --strict-contracts`
- `wfctl audit plugins --strict-contracts`
- startup failure for plugins marked strict that omit descriptors or send mismatched messages

The third change should remove default legacy acceptance after all first-party plugins and application-owned plugins are migrated.

## Migration Scope

Migration must include:

- `workflow` external plugin SDK, proto, adapter, validation, and templates
- every `workflow-plugin-*` repo
- application-owned plugin adapters in `workflow-dnd`, `workflow-cardgame`, `core-dump`, and `buymywishlist`

Batch plugin repos by contract shape:

- infrastructure/provider plugins
- SaaS/integration plugins
- auth/security plugins
- game/world plugins
- template/sample plugins
- application repos

Each batch gets a branch, PR, spec review, adversarial code review, CI monitoring, Copilot review handling, and merge only after green checks and no meaningful unresolved review.

### IaC Provider Plugins

These plugins implement `interfaces.ResourceDriver` and consume the IaC
canonical schema. They are tracked separately because their migration
benefits from the cross-provider review discipline at
[`docs/IAC_PLUGIN_REVIEW_CHECKLIST.md`](../IAC_PLUGIN_REVIEW_CHECKLIST.md).

| Plugin | Strict-mode status (target v0.9.0) | Pre-migration audit findings |
|---|---|---|
| `workflow-plugin-aws` | active migration | (Phase C audit pending) |
| `workflow-plugin-azure` | verified locally, awaiting PR | (Phase C audit pending) |
| `workflow-plugin-ci-generator` | merged | n/a (already shipped strict) |
| `workflow-plugin-digitalocean` | pending — v0.8.0 ships legacy compat | F4/F5/F7 cycle: BC-1, BC-2, BC-3, BC-4, BC-5, BC-6, BC-8 closed in v0.8.0; BC-7 not applicable. Issue #37 (Update naming consistency) deferred to v0.8.x |
| `workflow-plugin-gcp` | pending | (Phase C audit pending) |
| `workflow-plugin-tofu` | pending | (Phase C audit pending) |

Each plugin's v0.9.0 strict-migration PR adds a "Pre-migration findings
closed" sub-section to its CHANGELOG referencing the bug classes addressed.

## Testing

Core tests must cover:

- descriptor parsing and validation
- legacy plugin compatibility
- strict plugin happy path
- strict plugin missing descriptor failure
- typed step execution across a real gRPC plugin process
- type mismatch failure with a clear diagnostic naming the plugin, type, field, expected message, and received message

Plugin repo tests must cover at least one representative typed module or step crossing the host/plugin boundary, not just local adapter unit tests.

## Acceptance Criteria

- Workflow exposes additive strict contract descriptors in the external plugin proto.
- SDK users can implement typed steps/modules without `map[string]any` parsing at plugin entrypoints.
- `wfctl` can audit and validate strict contract coverage.
- `workflow-plugin-template` creates a strict-contract plugin by default.
- First-party plugin and application repos have migration plans and batched PRs.
- `docs/plans/INDEX.md` includes this design and its implementation plan.
