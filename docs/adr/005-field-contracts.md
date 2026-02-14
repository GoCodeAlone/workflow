# ADR 005: Component Field Contracts

## Status
Accepted

## Context
Dynamic components accept map[string]interface{} parameters. Without formal contracts: missing fields cause panics, type mismatches produce confusing errors, interfaces are undiscoverable, and no pre-execution validation exists.

## Decision
Introduced field contracts: FieldContract struct with RequiredInputs, OptionalInputs, Outputs. FieldSpec with Type, Description, Default. Components declare contracts via Contract() function. Pre-execution validation checks fields and types. ApplyDefaults fills optional fields. Backward compatible (components without contracts work as before).

## Consequences
**Positive**: Runtime errors caught before execution; self-documenting interfaces; plugin SDK auto-generates docs from contracts; default values reduce boilerplate; registry enables tooling.

**Negative**: Additional boilerplate; map-based declaration convention; small validation overhead; contracts are optional. Mitigated by SDK scaffolding, community validator checks, simple convention, and negligible overhead.
