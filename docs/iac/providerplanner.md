# ProviderPlanner adapter author guide

`ProviderPlanner` is an optional v2 interface declared in [`interfaces/iac_provider.go`](../../interfaces/iac_provider.go). It lets a plugin replace core wfctl's default plan logic (`platform.ComputePlan` + `driver.Diff` dispatch) with its own plan computation — typically because the plugin wraps a foreign engine (Terraform/OpenTofu, Pulumi) that already produces a plan from external state and needs to surface that plan back to wfctl rather than rebuilding it from scratch.

This guide explains what the interface is, when to implement it, how core wfctl handles plugins that do not implement it, and the type-assertion pattern reserved for future adapter PRs at the dispatch site.

## What `ProviderPlanner` is

```go
type ProviderPlanner interface {
    PlanV2(ctx context.Context, desired []ResourceSpec, current []ResourceState) (IaCPlan, error)
}
```

`PlanV2` mirrors the signature of `platform.ComputePlan` so that an adapter implementation can be invoked at the dispatch site as a drop-in replacement. The signature returns an `IaCPlan` value (not a pointer) — distinct from the existing `IaCProvider.Plan` method which returns `*IaCPlan`. The value-return is intentional: it underlines that `PlanV2` is a pure computation, not a method that retains plan state on the provider.

The interface is **purely additive**. A plugin that does not implement `ProviderPlanner` is still a valid `IaCProvider` and continues to work without changes.

## When to implement

Implement `ProviderPlanner` when your plugin needs custom plan logic that core wfctl's default Diff dispatch cannot express. Typical cases:

- **Tofu/Terraform-style adapters** that derive desired-vs-current state from a `.tfstate` file the plugin manages itself, rather than from wfctl's `[]ResourceState` snapshot.
- **Pulumi-style adapters** that delegate plan computation to the Pulumi engine and need to translate its preview output into an `IaCPlan` directly.
- **Composite providers** where a single resource type fans out to multiple sub-resources whose plan ordering must be coordinated across drivers.

If your plugin's resources can be planned independently per driver and the per-driver Diff output is sufficient, do **not** implement `ProviderPlanner` — the default dispatch already does the right thing.

## How core wfctl handles plugins that do not implement it

In v0.21.0, core wfctl's `platform.ComputePlan` (in [`platform/differ.go`](../../platform/differ.go)) dispatches `driver.Diff` directly per resource — it resolves a `ResourceDriver` for each spec via `p.ResourceDriver(spec.Type)`, calls `driver.Diff(ctx, spec, currentOut)` for every modification candidate, and assembles the per-driver output into an `IaCPlan`. (Note: `IaCProvider.Plan`, when implemented, typically delegates back to `platform.ComputePlan` — but `ComputePlan` itself does not call `provider.Plan`.) There is no type-assertion against `ProviderPlanner` at the dispatch site — meaning even a plugin that does implement `ProviderPlanner` will not have its `PlanV2` method invoked by core code in v0.21.0.

The `ProviderPlanner` interface is reserved as a forward-compatible extension hook. Adapter PRs that wish to use it will add the type-assertion at the dispatch site as part of their own design discussion.

## Type-assertion pattern at the dispatch site

The future adapter PR is expected to wire `ProviderPlanner` into `platform.ComputePlan` with a pattern of the following shape:

```go
// Illustrative pattern for the dispatch site in platform/differ.go's
// ComputePlan (not yet wired in v0.21.0).
if planner, ok := provider.(interfaces.ProviderPlanner); ok {
    return planner.PlanV2(ctx, desired, current)
}
// Fall through to the existing per-driver Diff dispatch in ComputePlan.
```

The pattern is the standard Go optional-interface idiom: type-assert against the optional interface, fall through to the default if the provider does not implement it. The same optional-interface pattern is already used by `ProviderIDValidator` in [`interfaces/iac_resource_driver.go`](../../interfaces/iac_resource_driver.go) — type-assert against the optional interface, fall through to a default if the implementer does not satisfy it.

Adapter authors wiring `PlanV2` into the dispatch site should also note the pointer/value asymmetry: `IaCProvider.Plan` returns `*IaCPlan` while `PlanV2` returns `IaCPlan` by value, so the wrapper at the dispatch site converts as needed (e.g. `p := planner.PlanV2(...); return &p, nil`).

## Provenance

This interface ships per [ADR 009](../adr/009-providerplanner-included-per-user-override.md), which records the user's Option-C ratification on 2026-05-03 of the decision to ship the extension hook in W-9 alongside the cross-plugin build CI gate, rather than deferring it to the first concrete adapter PR.
