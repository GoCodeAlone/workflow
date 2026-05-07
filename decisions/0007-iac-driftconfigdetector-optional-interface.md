# ADR 0007 — IaC DriftConfigDetector Optional Interface

- **Status:** Accepted
- **Date:** 2026-05-06
- **Deciders:** Claude (autonomous design pipeline), Jon Langevin (mandate)
- **Refs:** workflow-plugin-digitalocean#47, [IaC state-truth design (GoCodeAlone/workflow-plugin-digitalocean)](https://github.com/GoCodeAlone/workflow-plugin-digitalocean/blob/main/docs/plans/2026-05-06-iac-state-truth-and-tc2-closeout-design.md)

## Context

`IaCProvider.DetectDrift(ctx, refs)` is existence-only: it classifies resources as `Ghost`, `InSync`, or `Unknown`, but never `DriftClassConfig`. Drivers like `VPCDriver` and `AppPlatformDriver` cannot compute meaningful config-drift without the spec that was applied — they receive only cloud-live state, not the operator's intended config. Issue workflow-plugin-digitalocean#47 tracked two possible remediation paths:

1. **Required-signature change** — extend `IaCProvider.DetectDrift` to accept a third argument: `applied map[string]map[string]any`. Every plugin implementing the interface would need a signature update, including aws/gcp/azure and any out-of-tree plugin.

2. **Optional capability interface** — add a new interface `DriftConfigDetector` that plugins MAY implement. Callers type-assert; plugins that don't implement it fall back to the existing existence-only `DetectDrift`. This is the pattern already established in the repo by `ComputePlanVersionDeclarer`, `ProviderValidator`, `Enumerator`, and `UpsertSupporter`.

## Decision

Add `DriftConfigDetector` as an **OPTIONAL interface** (Path 2). `IaCProvider.DetectDrift` is unchanged.

```go
type DriftConfigDetector interface {
    DetectDriftWithApplied(ctx context.Context, resources []ResourceRef, applied map[string]map[string]any) ([]DriftResult, error)
}
```

Callers type-assert and fall back:

```go
if d, ok := provider.(DriftConfigDetector); ok {
    results, err = d.DetectDriftWithApplied(ctx, refs, appliedMap)
} else {
    results, err = provider.DetectDrift(ctx, refs)
}
```

The `applied` map is keyed by `ref.Name` and sourced from `ResourceState.AppliedConfig`. The sentinel field `ResourceState.AppliedConfigSource` (see ADR 0010) discriminates "apply" (true user-supplied config) from "adoption" (Outputs reflow). Providers MUST refuse to compute config-drift on adoption-shaped entries to avoid false-positives.

## Consequences

**Positive:**
- Sibling plugins (aws/gcp/azure) require **zero code changes** — they continue to satisfy `IaCProvider` and fall through to existence-only detection.
- Out-of-tree plugins remain binary-compatible and compile-compatible.
- The optional-declarer pattern is already established in this repo; a new developer encountering `DriftConfigDetector` has existing precedents to follow.
- Detection capability can be added incrementally, one plugin at a time, without a coordinated multi-repo release.

**Negative:**
- Callers accumulate type-assertion branches. Mitigated by the established pattern and the small number of call sites (two: `runInfraApplyRefreshPhase`, `runInfraStatusDrift`).
- Plugin authors must discover the optional interface via docs or code search. Tracked as a follow-up: consolidate the opt-in capability list in `docs/IAC_PLUGIN_AUTHORING.md`.

## Rejected Alternative: Required-Signature Change

Adversarial design review #1 (Critical finding) rejected changing `DetectDrift(ctx, refs)` to `DetectDrift(ctx, refs, applied)` because:
- Every plugin implementing `IaCProvider` must update its signature simultaneously — a coordinated multi-repo change.
- Breaks binary compatibility for any out-of-tree plugin (missing method → plugin fails to load).
- Conflicts with the repo's established optional-declarer precedent.

## References

- `interfaces/iac_provider.go` — `DriftConfigDetector` declaration
- `interfaces/iac_state.go` — `ResourceState.AppliedConfigSource` (ADR 0010)
- `cmd/wfctl/infra_apply_refresh.go` — caller type-assertion
- `cmd/wfctl/infra_status_drift.go` — caller type-assertion
- `cmd/wfctl/infra_drift_applied.go` — `buildAppliedSpecMap` helper

---

## 2026-05-07 update: rename DetectDriftWithApplied → DetectDriftWithSpecs (v0.22.3)

**Context:** While v0.22.2 was being finalized, workflow-plugin-digitalocean shipped v0.10.5 with its own implementation of the DriftConfigDetector interface. The DO plugin chose the signature:

```go
DetectDriftWithSpecs(ctx context.Context, resources []ResourceRef, specs map[string]ResourceSpec) ([]DriftResult, error)
```

This uses `ResourceSpec` (the typed wrapper with `Name`, `Type`, and `Config` fields) instead of `map[string]map[string]any`. The typed form is cleaner — callers pass all of Name/Type/Config in one value, providers can read Name and Type directly without key lookup.

**Decision:** Adopt the DO plugin's signature as canonical. The autonomous mandate includes "build the right way; refactor where needed." Aligning the interface with the shipped implementation avoids a duplicate method on DOProvider and is the correct long-term design.

**Wire protocol change:** The original plan called `IaCProvider.DetectDriftWithApplied` as a separate RPC method. The DO plugin v0.10.5 routes spec-injection through `IaCProvider.DetectDrift` by checking for a `"specs"` key in the args map — no new RPC case needed. The wfctl `remoteIaCProvider.DetectDriftWithSpecs` now calls `IaCProvider.DetectDrift` with `refs` + `specs` args. This is more robust: plugins that predate DriftConfigDetector support still handle `IaCProvider.DetectDrift` (they ignore the unknown `specs` key and return existence-only results).

**Changes in this release (v0.22.3):**
- `interfaces.DriftConfigDetector.DetectDriftWithApplied` renamed to `DetectDriftWithSpecs`; parameter changed from `map[string]map[string]any` to `map[string]ResourceSpec`
- `buildAppliedSpecMap` return type changed from `map[string]map[string]any` to `map[string]interfaces.ResourceSpec`; each entry is now wrapped as `ResourceSpec{Name, Type, Config}`
- `remoteIaCProvider.DetectDriftWithSpecs` sends `IaCProvider.DetectDrift` with `specs` arg (not a separate `DetectDriftWithApplied` RPC name)
- All callers and tests updated accordingly
