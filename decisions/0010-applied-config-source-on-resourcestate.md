# ADR 0010 — AppliedConfigSource Field on ResourceState

- **Status:** Accepted
- **Date:** 2026-05-06
- **Deciders:** Claude (autonomous design pipeline), Jon Langevin (mandate)
- **Refs:** ADR 0007, docs/plans/2026-05-06-iac-state-truth-and-tc2-closeout-design.md

## Context

`DriftConfigDetector.DetectDriftWithApplied` (ADR 0007) receives the `applied` map from `ResourceState.AppliedConfig`. However, `AppliedConfig` can contain two distinct kinds of data:

1. **True user config** — set by `applyWithProviderAndStore` and `applyPrecomputedPlanWithStore` from `ResourceSpec.Config`. Comparing this against a fresh cloud Read is safe and meaningful.

2. **Adoption-shaped Outputs reflow** — set by `adoptExistingResources` via `liveConfigFromOutputs(live.Outputs)`. When no embedded "config" key exists in Outputs, `liveConfigFromOutputs` falls back to `cloneMap(live.Outputs)`. Comparing such an entry against a fresh Read yields false-positive config-drift (Outputs contain fields like `id`, `created_at`, `endpoint` that are not user-controllable via spec).

Without discrimination, `DetectDriftWithApplied` cannot tell safe from unsafe entries. Two placement options were considered:

1. **Magic key in AppliedConfig** — store the sentinel as `applied_config["_source"]`. Reads directly during `DetectDriftWithApplied` without a separate lookup.
2. **Dedicated field on ResourceState** — `ResourceState.AppliedConfigSource string`. Clean separation of concerns; sentinel is not part of the user-config payload; JSON encoding is `omitempty` so legacy state files remain unaffected.

## Decision

Add `AppliedConfigSource string` as a **dedicated field on `ResourceState`** (Option 2), JSON-tagged `applied_config_source,omitempty`.

Valid values:
- `"apply"` — `AppliedConfig` came from a user-supplied `ResourceSpec.Config`. Config-drift detection is safe and meaningful for this entry.
- `"adoption"` — `AppliedConfig` was reflowed from live cloud Outputs. Config-drift detection MUST be skipped to avoid false-positives.
- `""` (empty / absent from JSON) — legacy state written before this field existed. Consumers MUST treat as `"adoption"` (conservative default).

Write sites:
- `applyWithProviderAndStore` → writes `"apply"`
- `applyPrecomputedPlanWithStore` → writes `"apply"`
- `resourceStateFromLiveOutput` (called by `adoptExistingResources`) → writes `"adoption"`

## Consequences

**Positive:**
- The sentinel is structurally separate from the user config payload — a provider parsing `AppliedConfig` cannot accidentally consume or mutate it.
- `omitempty` tag ensures pre-existing state-store files (no `applied_config_source` key) continue to decode cleanly; empty string is the conservative "adoption" default.
- Backward-compat is bidirectional: old code reading new state JSON silently drops the unknown field (Go `encoding/json` default); new code reading old state JSON sees empty string → adoption default.
- The field is self-documenting in the state store for debugging.

**Negative:**
- State store entries grow by ~30 bytes per resource (JSON key + value). Acceptable at typical resource counts (10–100 per tenant).
- A third write site may be added in the future (e.g. `wfctl infra import` if it adopts the pattern). Each new write site must be audited for correct source labeling.

## Rejected Alternative: Magic Key in AppliedConfig

Adversarial design review #1 cycle 2 (Important finding "sentinel placement") rejected storing the sentinel as `applied_config["_source"]` because:
- Pollutes the user-config payload; providers iterating `AppliedConfig` for their own purposes must filter out the sentinel key.
- A provider that passes `AppliedConfig` unchanged to a downstream system (e.g. a Terraform variable map) would transmit the sentinel as a Terraform variable, causing unexpected behavior.
- Harder to discover and document; JSON state files show the field mixed in with user config rather than at the struct level.

## References

- `interfaces/iac_state.go` — `ResourceState.AppliedConfigSource` field declaration
- `cmd/wfctl/infra_apply.go` — write sites (`applyWithProviderAndStore`, `applyPrecomputedPlanWithStore`, `resourceStateFromLiveOutput`)
- `cmd/wfctl/infra_drift_applied.go` — `buildAppliedSpecMap` (reads the field to filter safe entries)
- ADR 0007 — `DriftConfigDetector` optional interface (consumer of this field)
