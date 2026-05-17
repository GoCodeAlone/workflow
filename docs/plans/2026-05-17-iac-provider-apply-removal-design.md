# Design: workflow#699 — IaCProvider.Apply hard-removal (resolves #699)

- **Date:** 2026-05-17
- **Status:** Approved (operator selected Approach D 2026-05-17); revised cycle-1 per adversarial review (2 Critical + 5 Important findings addressed)
- **Issue:** https://github.com/GoCodeAlone/workflow/issues/699
- **Precedent:** ADR 0024 (IaC typed force-cutover), ADR 0025 (IaC optional methods are typed services), Phase 2 (workflow#640) v2 hooks-over-gRPC cascade, Phase 2.5 (workflow#695) IaCProviderFinalizer cascade

## Summary

Hard-delete `IaCProvider.Apply` across workflow + 4 IaC plugins (aws/gcp/azure/DO) + workflow-registry manifests. Eliminates the sentinel-stub runtime-failure surface DO v1.4.0 introduced (the very surface workflow#699 was filed to remove) without introducing a `LegacyApplier` opt-in interface (rejected for YAGNI + ADR 0024 force-cutover precedent).

**Note on title vs. issue body:** Issue #699 is titled "IaCProvider interface segregation" but its body proposes three candidate designs, all of which converge on removing `Apply` from the required surface. This design picks the most-aggressive variant (Approach D, hard-delete). The work resolves #699's body; the title's "segregation" framing is the project_open_followup_queue label, not a literal ISP-style split.

After this change:

- `interfaces.IaCProvider` no longer declares `Apply`.
- `pb.IaCProviderRequired` no longer carries `rpc Apply` (field 6 reserved; method name `Apply` reserved on the service).
- `cmd/wfctl` has a single apply path (the v2 `wfctlhelpers.ApplyPlanWithHooks` dispatch); the v1 `provider.Apply` branch + the `iac/wfctlhelpers/dispatch.go` version-switch are deleted.
- All 4 IaC plugins drop their `Apply` Go method **and** their `iacserver.Apply` gRPC handler.
- The manifest enforcement layer (`cmd/wfctl/deploy_providers.go findIaCPluginDir` switch — the load path that actually drives `wfctl infra apply`, NOT `sdk.ParseManifest`) rejects `iacProvider.computePlanVersion ∈ {"", "v1"}` at parse time. SDK schema tightened in parallel for tooling that uses `ParseManifest`.

## Context

### What this fixes

DO v1.4.0 (Phase 3 of workflow#695 cascade) replaced `DOProvider.Apply` with a sentinel-stub returning `ErrApplyV1Removed`. The Phase 2.5+ cleanup bundle adversarial-design-review cycle-1 (Critical finding C-5) flagged this:

> "Preserves the exact runtime-failure surface ADR 0024 mandates eliminating."

The sentinel-stub is dead code that exists only because `interfaces.IaCProvider.Apply` still requires *some* method body. Issue #699 was filed to do the proper architectural fix.

### What v2 dispatch actually looks like

`wfctl infra apply` (`cmd/wfctl/infra_apply.go`) branches on `wfctlhelpers.DispatchVersionFor(provider)`:

| Dispatch | Apply call | Plugins on this path |
|---|---|---|
| v1 | `provider.Apply(ctx, &plan)` (the legacy in-provider loop) | none, since aws/gcp/azure declared v2 in v1.2.x and DO declared v2 in v1.3.0 |
| v2 | `wfctlhelpers.ApplyPlanWithHooks(ctx, provider, &plan, hooks)` → drives `ResourceDriver` per action + `IaCProviderFinalizer.FinalizeApply` (shipped main commit aac519da, workflow#697 / Phase 2.5) for post-loop deferred-update flush | all 4 GoCodeAlone IaC plugins |

So `provider.Apply` is unreachable from `wfctl infra apply` for every plugin in the ecosystem. It is dead code:

- **DO**: `DOProvider.Apply` returns `ErrApplyV1Removed`; `doIaCServer.Apply` forwards the stub through gRPC; only callable via `wfctl infra apply` if the plugin author forgot to declare v2.
- **aws/gcp/azure**: `<Provider>.Apply` is a hand-rolled per-action loop (literally the v1 fallback implementation `wfctlhelpers.ApplyPlan` replaced). Unreachable since v1.2.x because all 3 plugins declare ComputePlanVersion=v2 in their `Capabilities` RPC response.

### Plugin verification (assumption A1)

Verified by direct grep 2026-05-17 against each plugin repo's main branch (head):

| Plugin | Version (tag) | ComputePlanVersion declaration |
|---|---|---|
| workflow-plugin-aws v1.2.1 | live tag | `internal/iacserver.go:125` → `CapabilitiesResponse{..., ComputePlanVersion: "v2"}` |
| workflow-plugin-gcp v1.2.0 | live tag (head plugin.json shows 1.1.0; sync-plugin-version workflow pending) | `internal/iacserver.go:125` → `ComputePlanVersion: "v2"` |
| workflow-plugin-azure v1.2.1 | live tag | `internal/iacserver.go:128` → `ComputePlanVersion: "v2"` |
| workflow-plugin-digitalocean v1.4.0 | live tag | `plugin.json` `iacProvider.computePlanVersion: v2` + `internal/iacserver.go:182` Capabilities |

Per ADR 0024 cycle 1 I-5: there are no third-party IaC plugins (no `interfaces.IaCProvider` consumers outside `workflow` + the four plugins above).

## Decision

Adopt **Approach D — hard-delete `Apply` from `interfaces.IaCProvider` + `pb.IaCProviderRequired`**.

Rejected alternatives:

- **Approach A** — extract `Apply` to a `LegacyApplier` Go-only interface; keep `rpc Apply` in proto. Rejected: proto layer still carries the sentinel-stub surface; sentinel error class survives at the gRPC boundary. Does not satisfy ADR 0024.
- **Approach B** — split `rpc Apply` into optional `IaCProviderLegacyApplier` service per ADR 0025 pattern. Rejected for YAGNI per ADR 0024 precedent (force-cutover, no compat shim) and because no v1 plugin exists. Adding the optional service to "preserve the opt-in surface" replicates the sentinel-stub anti-pattern in a different guise. **Soft-add-back available** — Approach B is the documented rollback option (see §Rollback) if a third-party plugin ever surfaces; the ADR 0025 optional-service pattern is the channel through which Apply can be re-introduced without re-opening the bug surface.
- **Approach C** — version the Go interface (`IaCProviderV1` vs `IaCProviderV2`). Rejected: leaves two interfaces named "IaC provider" confusing the SDK + adapter type-asserts; does not touch the proto layer at all (so doesn't fix the bug the issue exists to fix).

## Scope (6-PR cascade + registry tail-PR)

PRs sequenced per Phase 2 / Phase 2.5 precedent (rc workflow tag first so plugins can build against new SDK, plugin rc tags second in parallel, plugin majors third, workflow final fourth, registry last).

### PR 1 — workflow `feat/699-iac-apply-removal-rc` → tag `v0.56.0-rc1`

**Files modified (in this safe-edit order to avoid intra-PR compile breakage):**

1. **Adapter + dispatch helper edits first** (these compile against the OLD proto/interface):
   - `cmd/wfctl/iac_typed_adapter.go` — delete `typedIaCAdapter.Apply`, delete `typedIaCAdapter.ComputePlanVersion`, delete the `_ wfctlhelpers.ComputePlanVersionDeclarer = (*typedIaCAdapter)(nil)` interface assertion at `:1348`, delete `ApplyRequest` encoding helpers in this file.
   - `cmd/wfctl/infra_apply.go:465-487` and `:1551-1563` — delete the v1 branch + the `usedV2Dispatch` variable (always true); collapse to single `applyV2ApplyPlanWithHooksFn` call.
2. **Then delete the dispatch helper package**:
   - `iac/wfctlhelpers/dispatch.go` — delete entire file (`ComputePlanVersionDeclarer`, `DispatchVersionFor`, `DispatchVersionV2`); v2 is the only dispatch path now.
3. **Then tighten the loader gate** (the actual production enforcement point — NOT `sdk.ParseManifest`, since `findIaCPluginDir` uses raw `json.Unmarshal` per its own godoc):
   - `cmd/wfctl/deploy_providers.go:162-170` — change the inline `switch m.IaCProvider.ComputePlanVersion` from `case "", "v1", "v2"` to `case "v2"`. Default arm now rejects both empty and `"v1"` with: `plugin %q manifest declares iacProvider.computePlanVersion %q; v0.56.0+ requires "v2" (see workflow#699 — upgrade plugin to v2.0.0 or higher)`.
4. **Then tighten the SDK schema** (defense-in-depth for tooling that uses `ParseManifest`):
   - `plugin/sdk/manifest.go` schema — tighten `iacProvider.computePlanVersion` to `enum: ["v2"]` (was `enum: ["v1","v2"]`).
5. **Then update the typed contract** (last, since this breaks the gRPC service):
   - `plugin/external/proto/iac.proto` — delete `rpc Apply(ApplyRequest) returns (ApplyResponse);` from `service IaCProviderRequired`; add `reserved 6;` for the field number; add `reserved "Apply";` for the method name (gRPC reserves both axes). Delete `message ApplyRequest` (the wrapper around `IaCPlan`, no other consumer). KEEP `message ApplyResponse` — it wraps `ApplyResult`, which `FinalizeApply` telemetry still uses (`iac/wfctlhelpers/apply.go` reads `ApplyResult.Actions` populated by `FinalizeApply`).
   - `plugin/external/proto/iac.pb.go`, `plugin/external/proto/iac_grpc.pb.go` — regenerate via `buf generate`.
6. **Then the Go interface**:
   - `interfaces/iac_provider.go` — delete `Apply(ctx context.Context, plan *IaCPlan) (*ApplyResult, error)` from the `IaCProvider` interface.
7. **Then the SDK auto-register helper**:
   - `plugin/external/sdk/iacserver.go` — `RegisterAllIaCProviderServices` type-assert against the trimmed `pb.IaCProviderRequiredServer`; verify the trimmed required service still compiles after `Apply` is removed.
8. **Then tests + stubs**:
   - `wftest/bdd/strict_iac.go` `iacServiceChecks` — drop the `Apply` row from the IaCProviderRequired check.
   - `cmd/wfctl/iac_loader_gate_test.go`, `cmd/wfctl/plugin_audit_iac_test.go`, `cmd/wfctl/plugin_audit.go`, and any test referencing `provider.Apply(...)` — delete the v1 dispatch coverage; the v2 path is the only call site to cover.
   - Update the `findIaCPluginDir` test (deploy_providers_test.go) to assert the new error message on `""` and `"v1"`.
   - `CHANGELOG.md` — entry noting the breaking change + plugin minimum versions.

**Tests added (PR 1):**

- `findIaCPluginDir` test covering 3 cases: `"v2"` (accept), `""` (reject with actionable error pointing to #699), `"v1"` (reject same shape).
- Manifest-schema test that rejects `iacProvider.computePlanVersion: ""` and `iacProvider.computePlanVersion: "v1"` at `ParseManifest` (defense-in-depth for tooling).
- Integration test that `discoverAndLoadIaCProvider` fails-fast with the actionable error when a plugin declares v1 or omits the field.

**Backwards compat:** none — hard cutover per ADR 0024 precedent. The workflow rc tag is the moment plugins must rebuild against the new SDK; no compat shim.

### PRs 2-5 — plugin rc1 tags (parallel, after PR 1 tags `v0.56.0-rc1`)

Each plugin ships a `v2.0.0-rc1` tag against `workflow v0.56.0-rc1` before its `v2.0.0` final tag. This mirrors the workflow-rc protocol and lets the plugin-conformance gate (see PR 6) build the matrix.

Each plugin PR:

- Bump `github.com/GoCodeAlone/workflow` pin to `v0.56.0-rc1`.
- Delete `<Provider>.Apply` and `<provider>IaCServer.Apply` RPC handler.
- For DO only: delete `ErrApplyV1Removed` constant + `internal/provider_apply_stub_test.go` (sentinel regression-gate obsolete).
- Drop the obsolete v1-Apply coverage in `internal/iacserver_test.go` + provider tests.
- Bump `plugin.json` to `v2.0.0-rc1`, `minEngineVersion: 0.56.0`.
- `CHANGELOG.md` entry: "BREAKING — drop v1 Apply method, require workflow v0.56+, per workflow#699."
- Tag `v2.0.0-rc1`.

| PR | Repo |
|---|---|
| PR 2 | workflow-plugin-digitalocean → tag `v2.0.0-rc1` |
| PR 3 | workflow-plugin-aws → tag `v2.0.0-rc1` |
| PR 4 | workflow-plugin-gcp → tag `v2.0.0-rc1` |
| PR 5 | workflow-plugin-azure → tag `v2.0.0-rc1` |

### PR 6 — workflow plugin-conformance gate + final tag `v0.56.0`

After all 4 plugin rc1 tags exist:

- New CI matrix step in workflow: build each `workflow-plugin-{aws,gcp,azure,digitalocean}@v2.0.0-rc1` against `workflow@v0.56.0-rc1` and run each plugin's iacserver_test smoke.
- On green: tag `workflow v0.56.0` final.
- Bump go.mod minimums in any in-repo consumers that need the new wfctl semantics.

### PRs 7-10 — plugin final tags (parallel, fan-out from PR 6)

Each plugin bumps SDK pin from `v0.56.0-rc1` → `v0.56.0`, bumps plugin.json to `v2.0.0`, tags `v2.0.0`.

### PR 11 — workflow-registry manifest bump (LAST)

- Update `workflow-registry/v1/plugins/{aws,gcp,azure,digitalocean}/manifest.json` to `version: 2.0.0`, `minEngineVersion: 0.56.0`.
- **Sequenced last** so operators on pre-v0.56.0 wfctl who pull from registry don't get a v2.0.0 plugin they can't run.
- This PR is the rollback-sensitive one (see §Rollback) because the registry is a rolling source-of-truth with no version axis.

### Memory + tracker updates

- Update `project_open_followup_queue.md`: mark workflow#699 done; cross-ref the v0.56.0 / plugin-v2.0.0 release notes.
- Update `MEMORY.md` plugin inventory (versions).
- New project memory: `project_workflow_699_apply_removal_shipped.md` (post-merge retro).

## Assumptions

- **A1** — aws/gcp/azure/DO all currently declare ComputePlanVersion=v2 via Capabilities RPC. **Verified 2026-05-17 by direct grep**: aws `internal/iacserver.go:125`, gcp `:125`, azure `:128`, DO `:182`. Pre-PR-1-task-1 re-check predicate: `grep -q '"v2"' internal/iacserver.go` in each plugin repo head.
- **A2** — no third-party IaC plugins exist. Per ADR 0024 cycle 1 I-5 grep. Holds because `interfaces.IaCProvider` is a Go interface (not a registry surface) and the engine + the four GoCodeAlone plugin repos are the only consumers. If this assumption is ever wrong, see §Rollback for the soft-add-back path (Approach B).
- **A3** — `module.PlatformProvider` is a different interface from `interfaces.IaCProvider`. Verified: `module/platform_provider.go:5` (4 methods including `Apply() (*PlatformResult, error)`, no context arg). `module/pipeline_step_iac.go:208` (`provider.Apply()` no args) confirms it calls `PlatformProvider`, not `IaCProvider`. This file is unaffected by this change.
- **A4** — `cmd/wfctl/deploy_providers.go findIaCPluginDir` switch is the actual production enforcement point for `iacProvider.computePlanVersion`. `sdk.ParseManifest` is NOT used by this loader (per the godoc at line 113-129 of deploy_providers.go and at lines 18-24 of `dispatch.go`). The design therefore tightens BOTH the inline switch (primary enforcement) AND the SDK schema (secondary defense for tooling).
- **A5** — `ApplyResult` + `ActionStatus` + `IaCProviderFinalizer.FinalizeApply` shape stays. Verified in main: commits `aac519da` (Phase 2.5) + `7a855934` (Phase 2.3) shipped FinalizeApply + ActionStatus enums. `iac/wfctlhelpers/apply.go` and `plugin/external/proto/iac.proto` both reference these symbols. This design removes only the `Apply` RPC; `FinalizeApply`, `ApplyResult`, `ApplyResponse`, `ActionStatus` all stay.
- **A6** — proto field-number + method-name reservation is breaking-compatible only in the "old client → new server" direction. Verified by buf-breaking-check semantics. New server (v0.56.0) refuses to expose the reserved field/method; old client (pre-v0.56.0) attempting `rpc Apply` against new server gets `codes.Unimplemented`. Operators must upgrade wfctl + plugins atomically — same constraint as Phase 2.

## Rollback

This change cascades through workflow + 4 plugins + 1 registry repo. Rollback path, in reverse order of the cascade:

1. **Revert PR 11 (registry manifest)** FIRST — this is the rolling source-of-truth. Re-publish the previous version pins so `wfctl plugin install` resumes serving the pre-v2.0.0 plugin majors. Registry has no tag axis; rollback is a manifest PR + immediate effect.
2. **Revert PRs 7-10 (plugin v2.0.0 tags)** — re-publish the previous v1.x.x tags as the recommended pins; the registry rollback (step 1) now points operators to those tags.
3. **Revert PR 6 (workflow v0.56.0)** — re-publish v0.55.x as the recommended pin.
4. **Revert PRs 1-5 (rc tags)** — RC tags don't need active revert (operators don't pin to rc), but the workflow rc and plugin rc tags can be left in place or yanked at maintainer discretion.
5. **State-file format invariant** across the cutover — `interfaces.ResourceState` JSON shape unchanged. Operators do not need to migrate state.
6. **Half-rolled-back state window** — between step 1 (registry revert published) and operators actually re-pulling via `wfctl plugin install`, some operators may already have v2.0.0 plugin binaries on disk. These continue to work against v0.56.0 wfctl; the issue is only for operators who downgraded wfctl to v0.55.x in the same window. Document this in the rollback runbook: "If you've already pulled v2.0.0 plugins, either keep v0.56.0 wfctl OR `wfctl plugin install --force` after registry revert."
7. **Soft-add-back option (Approach B)** — if the rollback is driven by a third-party plugin surfacing post-cutover, the architectural re-introduction path is Approach B (optional `IaCProviderLegacyApplier` service per ADR 0025), NOT restoring `rpc Apply` on `IaCProviderRequired`. Approach B preserves the compile-time-safety guarantee while letting the third-party plugin opt in.

The change is runtime-affecting (proto change, plugin gRPC service surface change), so `runtime-launch-validation` applies: each plugin PR must run iacserver_test (the per-plugin runtime smoke) before merge. PR 6 adds the cross-repo conformance gate.

## Adversarial-review-cycle-1 findings addressed

Adversarial review (cycle 1, 2026-05-17) flagged 2 Critical + 5 Important + 3 Minor findings. Resolution:

| Finding | Severity | Resolution |
|---|---|---|
| Schema gate bypass — `findIaCPluginDir` uses raw `json.Unmarshal`, not `sdk.ParseManifest` | Critical | Added explicit file edit to `cmd/wfctl/deploy_providers.go:162-170` switch as the primary enforcement point. SDK schema tightening kept as defense-in-depth. New A4 assumption documents the split-enforcement model. |
| FinalizeApply citation appeared fictional (was reviewer reading pre-rebase tree) | Critical | Re-verified against current main (post-rebase): commits aac519da + 7a855934 shipped Phase 2.5 + Phase 2.3 to main; FinalizeApply lives in `interfaces/`, `plugin/`, `cmd/wfctl/`, `iac/`. A5 now cites specific commits. |
| azure A1 grep not surfaced in original verification | Important | Re-grepped 2026-05-17: azure at `internal/iacserver.go:128` (not :125 like aws/gcp). A1 table updated with per-plugin line numbers + pre-PR-1 re-check predicate. |
| Rollback ignores registry-manifest fan-out | Important | Added PR 11 (registry manifest bump as last cascade step) + §Rollback step 1 (revert registry FIRST) + §Rollback step 6 (half-rolled-back window runbook). |
| Parallel PRs 2-5 race PR 6 | Important | Restructured to 11 PRs: rc tags first (PRs 2-5), conformance gate + workflow final (PR 6), plugin final tags as fan-out (PRs 7-10), registry last (PR 11). |
| ADR-0025 optional-service add-back path not engaged | Important | §Decision Approach B rejection now notes Approach B IS the soft-add-back rollback option per ADR 0025. §Rollback step 7 documents the channel. |
| PR 1 internal edit ordering hazard | Important | PR 1 file list reorganized into 8 numbered safe-edit-order steps. |
| Title vs. issue body mismatch | Minor | Title now reads "(resolves #699)" + §Summary opens with explicit note on title-vs-body. |
| Plugins should ship rc1 tags | Minor | PRs 2-5 now ship `v2.0.0-rc1` before PRs 7-10 ship `v2.0.0`. |
| Reserve method name + field number on service block | Minor | PR 1 file-list step 5 now reserves BOTH field number 6 AND method name `"Apply"` on the service block. |

## References

- Issue: https://github.com/GoCodeAlone/workflow/issues/699
- ADR 0024 (force-cutover precedent): `decisions/0024-iac-typed-force-cutover.md`
- ADR 0025 (optional services as typed services, not flags): `decisions/0025-iac-optional-method-typed-services-not-bool.md`
- Phase 2 (workflow#640): `docs/plans/2026-05-10-strict-contracts-force-cutover.md` + memory `project_v2_lifecycle_phase2_shipped.md`
- Phase 2.5 (workflow#695): merged main commit `aac519da` (IaCProviderFinalizer + OnPlanComplete hook)
- Phase 2.3 (workflow#698): merged main commit `7a855934` (ActionStatus compensation enums)
- Phase 2.5+ Cleanup Bundle adversarial-design-review cycle-1 C-5 finding (originating context for this issue)
- Cycle-1 adversarial-review of this design (2026-05-17, addressed above)
