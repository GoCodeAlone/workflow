---
status: approved
area: plugins
owner: workflow
implementation_refs: []
external_refs:
  - "#76"
verification:
  last_checked: 2026-04-26
  commands:
    - GOWORK=off go test ./cmd/wfctl ./plugin/external/...
  result: pass
supersedes: []
superseded_by: []
---

# Strict gRPC Plugin Contracts Design

Date: 2026-04-26

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
