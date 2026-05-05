# PR W-3b: ComputePlan Diff dispatch + manifest-driven v2 apply

This PR implements W-3b of the IaC conformance + Replace plan
(`docs/plans/2026-05-03-iac-conformance-and-replace.md`). Built on
top of W-3a (PR #527, manifest field + helper package + drift
postcondition + diff cache foundation). Branch:
`feat/iac-replace-dispatch` → `main`.

## Summary

W-3a shipped the v2 IaC contract's foundation; W-3b is the binding
runtime change that wires it together. After this PR:

- `platform.ComputePlan` accepts `(ctx, provider, desired, current)`
  and dispatches `ResourceDriver.Diff` per existing-resource
  candidate under bounded errgroup concurrency (default 8;
  `WFCTL_PLAN_DIFF_CONCURRENCY` 1..32). Replace classification is
  honest: the helper emits `Action="replace"` whenever
  `DiffResult.NeedsReplace` is true OR any `FieldChange.ForceNew` is
  true. (T3.6a → T3.6e)
- Per-resource Diff results are cached via `iac/diffcache` (W-3a's
  T3.5) under the `(PluginVersion, Type, ProviderID, SHAConfig,
  SHAOutputs)` key tuple. Apply correctness does not depend on
  cache hits — fresh CI runners always miss and re-Diff. (T3.6f)
- `wfctl infra plan` now loads the gRPC plugin process so it can
  honestly emit Replace actions before apply. **BREAKING**:
  plugin-load failure exits non-zero with the literal error
  `error: failed to load plugin "<name>": <reason>; wfctl infra plan
  now requires the plugin process to compute Diff (since v0.21.0)`.
  No `--no-provider` escape hatch (rev3 YAGNI fix); operators who
  need pure offline validation use `wfctl validate`. (T3.6b)
- `wfctl infra apply` branches on the loaded plugin's manifest
  `iacProvider.computePlanVersion`: `"v2"` routes through
  `wfctlhelpers.ApplyPlan` (W-3a's helper, now wired); anything
  else takes the legacy `provider.Apply` path. **NO env-var, NO
  operator-flippable gate** — the v1/v2 routing is plugin-author-
  controlled via `plugin.json`. (T3.7)
- `printDriftReportIfAny` (W-3a/T3.1.5, shipped unwired) now fires
  in the v2 dispatch path on success OR partial failure, so
  operators see the stale-input diagnostic exactly when they need
  it most.

## Bugs incidentally fixed by this PR

W-3b's binding nature surfaced three pre-existing bugs that the v1
dispatch path either masked or never reached. All three ship fixed:

1. **Delete-via-Apply state leakage** — Today: `ComputePlan` emits
   delete actions but `DOProvider.Apply` has no `case "delete"`
   (falls through to `default: unknown action`). wfctl prunes
   state regardless. Result: cloud resource leaks. This PR adds
   `case "delete"` to `wfctlhelpers.ApplyPlan` (shipped in W-3a's
   T3.3 doDelete + activated by T3.7's manifest-driven dispatch),
   fixing the leakage for v2 plugins. Operators relying on the
   (broken) skip behavior may see different outcomes — review
   downstream automation that assumed delete actions were no-ops.

2. **ForceNew silently downgraded to Update** (issue C from
   design) — Pre-W-3b: a provider that sets `NeedsUpdate=true` with
   one or more `ForceNew=true` `FieldChange`s (but forgets to set
   `NeedsReplace`) would have `Action="update"` emitted instead of
   `replace`. The Update path then either no-op'd the change (when
   the underlying API rejected the field as immutable) or applied
   it in-place against an API that interpreted the new value as a
   recreate request — both wrong. Fixed in T3.6e: `ComputePlan`
   emits `replace` whenever `NeedsReplace=true` OR
   `hasForceNew(diff.Changes)`. Pin: `platform/differ_replace_test.go`'s
   `TestComputePlan_ForceNewWithoutNeedsReplace_StillEmitsReplace`.

3. **`map[string]bool` silently drops gRPC args at the wire boundary** —
   `cmd/wfctl/deploy_providers.go::remoteResourceDriver.Diff` was
   passing `current.Sensitive` (`map[string]bool`) directly into
   the `args` map. `structpb.NewStruct` rejects `map[string]bool`
   (it accepts `map[string]any` only), and the upstream
   `plugin/external/convert.go::mapToStruct` returns
   `&structpb.Struct{}` on err rather than surfacing the typing
   failure. Result: every Diff dispatch over gRPC for any provider
   whose `ResourceOutput.Sensitive` map was non-nil silently
   observed `args=map[]` on the plugin side — the plugin saw an
   empty arg map and could not honestly compute a diff. v1 plugins
   never tripped this because v1 dispatches `IaCProvider.Plan`
   server-side (no `ResourceDriver.Diff` over gRPC); v2's per-
   resource Diff dispatch surfaces it on the first existing-resource
   call. Fix: `sensitiveToAny()` converter at the call site
   (commit `40e07a1`). **Discovered during T3.9 runtime-launch-
   validation against an out-of-band gRPC stub plugin** (the kind
   of v2-only regression that motivated the validation step).

   *Forward concern (not blocking this PR)*: the upstream
   `plugin/external/convert.go::mapToStruct` still swallows
   `NewStruct` errors. Any future caller in this codebase (or any
   plugin SDK author) that passes an unsupported type to args will
   hit the same silent-drop. A follow-up should make `mapToStruct`
   surface or panic on the typing error so the bug class is closed
   at the root, not just at the one observed call site.

## Architecture decisions recorded

- **`docs/adr/007-t3-9-runtime-validation-via-loader-seam.md`** —
  T3.9 runtime-launch-validation ships as an in-tree Go integration
  test using the `resolveIaCProvider` seam (lifted in T3.6c)
  rather than as an out-of-process gRPC stub plugin. ADR records
  the reasoning + considered alternatives.

## Rollout

This PR ships the v2 dispatch path; it does NOT migrate any
existing plugin to declare `iacProvider.computePlanVersion: "v2"`.
v1 plugins (every plugin shipping today, including `workflow-plugin-
digitalocean`) continue to take the legacy `provider.Apply` path
with **zero runtime change**. P-DO sets `v2` in a follow-up PR
after this one merges, at which point its CI exercises the gRPC
roundtrip against W-3b's tip.

## Test plan

- [x] `GOWORK=off go test -race -count=1 ./interfaces/... ./iac/... ./platform/... ./plugin/sdk/... ./cmd/wfctl/... ./module/...`
  — all green
- [x] T3.9 runtime-launch-validation per
  `docs/adr/007-t3-9-runtime-validation-via-loader-seam.md` —
  end-to-end loader-seam test asserts driver.{diff,delete,create}
  invocations + identity of deleted ProviderID + created Config
  region
- [x] T3.9 drift-report wiring — `captureStderr` test asserts
  `printDriftReportIfAny` output reaches operator on v2 success +
  partial-failure paths
- [x] CHANGELOG entry under `[Unreleased]` calls out the BREAKING
  change to `wfctl infra plan` plus the empty-desired alignment
  with apply
- [ ] (Manual, post-merge) P-DO rebases on W-3b tip and sets
  `iacProvider.computePlanVersion: "v2"` to exercise the gRPC
  dispatch end-to-end against a production provider
