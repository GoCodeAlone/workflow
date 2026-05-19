# Design: workflow#699 — IaCProvider.Apply hard-removal (resolves #699)

- **Date:** 2026-05-17
- **Status:** Approved (operator selected Approach D 2026-05-17); revised cycle-2 per adversarial review (3 Critical + 5 Important findings addressed); architectural pivot to typed Capabilities-RPC gate
- **Issue:** https://github.com/GoCodeAlone/workflow/issues/699
- **Precedent:** ADR 0024 (IaC typed force-cutover), ADR 0025 (IaC optional methods are typed services), Phase 2 (workflow#640) v2 hooks-over-gRPC cascade, Phase 2.5 (workflow#695) IaCProviderFinalizer cascade

## Summary

Hard-delete `IaCProvider.Apply` across workflow + 4 IaC plugins (aws/gcp/azure/DO). Eliminates the sentinel-stub runtime-failure surface DO v1.4.0 introduced (the very surface workflow#699 was filed to remove) without introducing a `LegacyApplier` opt-in interface (rejected for YAGNI + ADR 0024 force-cutover precedent).

**Note on title vs. issue body:** Issue #699 is titled "IaCProvider interface segregation" but its body proposes three candidate designs, all of which converge on removing `Apply` from the required surface. This design picks the most-aggressive variant (Approach D, hard-delete). The work resolves #699's body; the title's "segregation" framing is the `project_open_followup_queue` label, not a literal ISP-style split.

After this change:

- `interfaces.IaCProvider` no longer declares `Apply`.
- `pb.IaCProviderRequired` no longer carries `rpc Apply`. `message ApplyRequest`, `message ApplyResponse`, AND `message ApplyResult` are deleted (they were only used by the deleted RPC + the deleted wfctl-side `applyResultFromPB` adapter — `interfaces.ApplyResult` is a Go type populated wfctl-side by `iac/wfctlhelpers/apply.go:318` and is unaffected). Field-tag and method-name reservation on the proto service is NOT used (proto3 `reserved` keyword applies only to messages/enums); a CI lint guards against re-introduction.
- `cmd/wfctl` has a single apply path (the v2 `wfctlhelpers.ApplyPlanWithHooks` dispatch); the v1 `provider.Apply` branch + the `iac/wfctlhelpers/dispatch.go` version-switch are deleted.
- All 4 IaC plugins drop their `Apply` Go method **and** their `iacserver.Apply` gRPC handler.
- The enforcement gate moves from `plugin.json` parsing (`findIaCPluginDir`'s inline switch) to `discoverAndLoadIaCProvider` at LOAD time, where it calls the plugin's typed `Capabilities` RPC and rejects providers whose `CapabilitiesResponse.compute_plan_version != "v2"`. This eliminates the need to backfill `iacProvider.computePlanVersion` in 4 plugin.jsons + 4 registry manifests (all 4 plugins already populate the typed RPC field, verified A1).

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
| v2 | `wfctlhelpers.ApplyPlanWithHooks(ctx, provider, &plan, hooks)` → drives `ResourceDriver` per action; `iac/wfctlhelpers/apply.go:318` populates `result.Actions` wfctl-side; `IaCProviderFinalizer.FinalizeApply` (shipped main commit aac519da, workflow#697 / Phase 2.5) runs post-loop deferred-update flush; FinalizeApply returns `FinalizeApplyResponse{Errors}` (proto lines 531-557), NOT `ApplyResult` (proto line 334) | all 4 GoCodeAlone IaC plugins |

So `provider.Apply` is unreachable from `wfctl infra apply` for every plugin in the ecosystem. `pb.ApplyResult` + `pb.ApplyResponse` + `pb.ApplyRequest` are referenced only by:

- `cmd/wfctl/iac_typed_adapter.go:366` (`typedIaCAdapter.Apply`) — deleted by this design.
- `cmd/wfctl/iac_typed_adapter.go:1193` (`applyResultFromPB`) — deleted by this design.
- `cmd/wfctl/iac_typed_adapter_test.go:510,542,568,586` — deleted by this design.
- `plugin/external/proto/iac_proto_test.go:26,129,137` — deleted by this design.

After this design lands, those messages are dead and CAN be deleted from the proto.

### Plugin verification (assumption A1)

Verified by direct grep 2026-05-17 against each plugin repo's main branch (head):

| Plugin | Version (tag) | ComputePlanVersion declaration |
|---|---|---|
| workflow-plugin-aws v1.2.1 | live tag | `internal/iacserver.go:125` → `CapabilitiesResponse{..., ComputePlanVersion: "v2"}` |
| workflow-plugin-gcp v1.2.0 | live tag | `internal/iacserver.go:125` → `ComputePlanVersion: "v2"` (head plugin.json shows 1.1.0; tracked under m-NEW-1 followup #TBD for sync-plugin-version workflow gap) |
| workflow-plugin-azure v1.2.1 | live tag | `internal/iacserver.go:128` → `ComputePlanVersion: "v2"` |
| workflow-plugin-digitalocean v1.4.0 | live tag | `plugin.json` `iacProvider.computePlanVersion: v2` + `internal/iacserver.go:182` Capabilities |

Important: `plugin.json` declarations are inconsistent (only DO has it). The typed RPC declaration is what matters because the typed `CapabilitiesResponse.compute_plan_version` field is populated by all 4 plugins. This design pivots the enforcement gate to the typed field — see A4.

Per ADR 0024 cycle 1 I-5: there are no third-party IaC plugins (no `interfaces.IaCProvider` consumers outside `workflow` + the four plugins above).

## Decision

Adopt **Approach D — hard-delete `Apply` from `interfaces.IaCProvider` + `pb.IaCProviderRequired`**.

Rejected alternatives:

- **Approach A** — extract `Apply` to a `LegacyApplier` Go-only interface; keep `rpc Apply` in proto. Rejected: proto layer still carries the sentinel-stub surface; sentinel error class survives at the gRPC boundary. Does not satisfy ADR 0024.
- **Approach B** — split `rpc Apply` into optional `IaCProviderLegacyApplier` service per ADR 0025 pattern. Rejected for YAGNI per ADR 0024 precedent (force-cutover, no compat shim) and because no v1 plugin exists. Adding the optional service to "preserve the opt-in surface" replicates the sentinel-stub anti-pattern in a different guise. **Soft-add-back available** — Approach B is the documented rollback option (see §Rollback) if a third-party plugin ever surfaces; the ADR 0025 optional-service pattern is the channel through which Apply can be re-introduced without re-opening the bug surface.
- **Approach C** — version the Go interface (`IaCProviderV1` vs `IaCProviderV2`). Rejected: leaves two interfaces named "IaC provider" confusing the SDK + adapter type-asserts; does not touch the proto layer at all.

## Scope (10-PR cascade — registry tail rolled into each plugin's final PR)

PRs sequenced per Phase 2 / Phase 2.5 precedent (rc workflow tag first so plugins can build against new SDK, plugin rc tags second in parallel, plugin majors third with their own registry-manifest bumps inline).

### PR 1 — workflow `feat/699-iac-apply-removal-rc` → tag `v0.56.0-rc1`

**Files modified (safe-edit order to avoid intra-PR compile breakage):**

1. **Adapter + dispatch helper edits first** (these compile against the OLD proto/interface):
   - `cmd/wfctl/iac_typed_adapter.go:366` — delete `typedIaCAdapter.Apply`; delete `typedIaCAdapter.ComputePlanVersion`; delete `applyResultFromPB` at `:1193`; delete the `_ wfctlhelpers.ComputePlanVersionDeclarer = (*typedIaCAdapter)(nil)` interface assertion at `:1348`.
   - `cmd/wfctl/infra_apply.go:465-487` and `:1660-1722` (verified via `grep -n usedV2Dispatch`) — delete the v1 branch + the `usedV2Dispatch` variable (always true); collapse to single `applyV2ApplyPlanWithHooksFn` call. Both sites have identical v1/v2 branch shape; one collapse pattern applies to both.
   - `cmd/wfctl/iac_typed_adapter_test.go:510-590` — delete `pb.ApplyResult`-using tests.
2. **Then delete the dispatch helper package**:
   - `iac/wfctlhelpers/dispatch.go` — delete entire file (`ComputePlanVersionDeclarer`, `DispatchVersionFor`, `DispatchVersionV2`); v2 is the only dispatch path now.
3. **Then move the loader gate from parse-time to load-time** (architectural pivot — see A4):
   - `cmd/wfctl/deploy_providers.go` — modify `discoverAndLoadIaCProvider`:
     - After typedIaCAdapter is constructed and the plugin handshake completes, immediately call `Capabilities` with a bounded timeout context (`context.WithTimeout(ctx, 10*time.Second)`) — do NOT use `context.Background()` and do NOT share the load-gate caps fetch with the long-lived `fetchCapabilities` cache (transient failures must not poison the adapter for the entire wfctl invocation; cycle-3 I-NEW-6).
     - Gate on `CapabilitiesResponse.compute_plan_version`. Reject with: `plugin %q declares CapabilitiesResponse.compute_plan_version = %q; v0.56.0+ requires "v2" (see workflow#699 — upgrade plugin to v2.0.0 or higher)`.
     - `findIaCPluginDir`'s inline switch (`:162-170`) ALREADY accepts `case "", "v1", "v2"`; no change there. Add a deprecation log line emission when the matched manifest declares `"v1"` or empty to nudge plugin authors toward declaring `"v2"` explicitly in plugin.json (defense-in-depth; the gRPC gate is the enforcement).
   - Reason: aws/gcp/azure plugin.json files do not carry `iacProvider.computePlanVersion`; only their typed gRPC response does. Gating at parse time would reject every aws/gcp/azure plugin. The typed RPC is the source-of-truth.
4. **Then loosen the SDK schema** (the inverse of the original plan — schema stays permissive since the load-time gate now does the enforcement):
   - `plugin/sdk/manifest.go` schema — leave `iacProvider.computePlanVersion` as `enum: ["v1","v2"]` (no change). Add a docstring note that the SDK schema is the manifest-validation surface only; runtime enforcement is at `discoverAndLoadIaCProvider`.
5. **Then update the typed contract** (last, since this breaks the gRPC service):
   - `plugin/external/proto/iac.proto` — delete `rpc Apply(ApplyRequest) returns (ApplyResponse);` from `service IaCProviderRequired`; delete `message ApplyRequest`, `message ApplyResponse`, `message ApplyResult`, `message ActionResult` (all dead after step 1). Add a comment to `service IaCProviderRequired`: `// Method "Apply" was removed per workflow#699; do not re-introduce. CI lint guards against re-appearance.` NO `reserved` keyword usage — proto3 `reserved` applies only to messages/enums, not services; field-tag reservation on a service is invalid proto3.
   - `plugin/external/proto/iac.pb.go`, `plugin/external/proto/iac_grpc.pb.go` — regenerate via `buf generate`.
   - `Makefile` (or existing `ci.yaml`) — add lint step `grep -L 'rpc Apply' plugin/external/proto/iac.proto || (echo "workflow#699: rpc Apply re-introduced; see decisions/0024" && exit 1)`.
6. **Then the Go interface**:
   - `interfaces/iac_provider.go` — delete `Apply(ctx context.Context, plan *IaCPlan) (*ApplyResult, error)` from the `IaCProvider` interface.
7. **Then the SDK auto-register helper**:
   - `plugin/external/sdk/iacserver.go` — `RegisterAllIaCProviderServices` type-assert against the trimmed `pb.IaCProviderRequiredServer`; verify the trimmed required service still compiles after `Apply` is removed.
8. **Then tests + stubs**:
   - `wftest/bdd/strict_iac.go` `iacServiceChecks` — drop the `Apply` row from the IaCProviderRequired check.
   - `cmd/wfctl/iac_loader_gate_test.go`, `cmd/wfctl/plugin_audit_iac_test.go`, `cmd/wfctl/plugin_audit.go`, `plugin/external/proto/iac_proto_test.go` — delete the v1 dispatch coverage; add a new load-gate test asserting the new error message on `compute_plan_version = "v1"` and `""`.
   - `CHANGELOG.md` — entry noting the breaking change + plugin minimum versions.
9. **Then delete the migration codemod** (its reason-to-exist evaporates the moment Apply is removed):
   - `cmd/iac-codemod/` — delete entire directory (`add_validate_plan.go`, `lint.go`, `main.go`, `refactor_apply.go`, `refactor_plan.go` + tests). The `AssertApplyDelegatesToHelper` analyzer + `refactor-apply` rewriter exist solely to migrate v1 `Apply` impls to v2 `wfctlhelpers.ApplyPlan` delegation; with Apply removed, both are dead tools.

**Edit-list correction (per cycle-3 C-NEW-5):** the two `usedV2Dispatch` collapse sites in `cmd/wfctl/infra_apply.go` live in TWO different functions: the primary `runInfraApply` (~`:465-540`) and `applyPrecomputedPlanWithStore` (~`:1600-1730`, function declared at `:1604`). Both must be edited with the same collapse pattern; do NOT assume a single edit suffices. Verify via `grep -n usedV2Dispatch cmd/wfctl/infra_apply.go` BEFORE and AFTER the edit — the count must go from 5 (467, 472, 536, 1662, 1664, 1711) to 0.

**Pre-PR-1 verification step:**

- `grep -rln 'wfctlhelpers.DispatchVersionV2\|wfctlhelpers.ComputePlanVersionDeclarer\|pb.ApplyResult\|pb.ApplyRequest\|pb.ApplyResponse\|applyResultFromPB' --include='*.go' .` MUST return only files this PR modifies. Particularly: clean up any stale `_worktrees/*` worktrees that still reference deleted symbols (cycle-3 reviewer found `_worktrees/wf663-topo`, `_worktrees/phase-b-core-deletion`, `_worktrees/phase2.5-cleanup` had old Apply references; rebase or delete each before PR 1 lands — otherwise the worktree's compile breaks).
- `go test ./module/...` to verify `module.PlatformProvider.Apply()` (different interface, A3) is not accidentally affected by proto regen.
- `go build ./... && go vet ./...` pre-merge gate (covers `interfaces.IaCProvider` interface change + every consumer; the targeted `./module/...` test alone is insufficient because the interface change ripples across `cmd/wfctl/`, `iac/wfctlhelpers/`, `plugin/external/sdk/`, `wftest/bdd/`).
- `grep -L 'rpc Apply' plugin/external/proto/iac.proto || exit 1` lint check added to CI (workflow#699 re-introduction guard).
- For each `workflow-plugin-{aws,gcp,azure,digitalocean}`: `grep -l 'applyResultToPB\|applyResultFromPB' internal/iacserver.go` — confirm helpers are dead and delete them in PRs 2-5 along with `iacserver.Apply` handler.

**Tests added (PR 1):**

- Load-gate test covering 3 cases: plugin returns `Capabilities.compute_plan_version = "v2"` (accept), `""` (reject with actionable error pointing to #699), `"v1"` (reject same shape).
- Test that `discoverAndLoadIaCProvider` fails-fast with the actionable error when Capabilities RPC fails or returns wrong value.

**Backwards compat:** none — hard cutover per ADR 0024 precedent. The workflow rc tag is the moment plugins must rebuild against the new SDK; no compat shim.

### PRs 2-5 — plugin rc1 tags (parallel, after PR 1 tags `v0.56.0-rc1`)

Each plugin ships a `v2.0.0-rc1` tag against `workflow v0.56.0-rc1` before its `v2.0.0` final tag. This mirrors the workflow-rc protocol and lets the plugin-conformance gate (see PR 6) build the matrix.

Each plugin PR:

- Bump `github.com/GoCodeAlone/workflow` pin to `v0.56.0-rc1`. Use SHA-pin (`v0.56.0-rc1` resolves to a commit) so proxy.golang.org indexing isn't blocking; OR use `go.mod replace` to local path for in-CI matrix testing; pick whichever the GoReleaser pipeline already supports. Plugin CI already has `setup-wfctl` action — extend to also pin workflow SDK.
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
| PR 4 | workflow-plugin-gcp → tag `v2.0.0-rc1`. **Also file followup issue** for the sync-plugin-version workflow gap (m-NEW-1: head plugin.json shows 1.1.0 despite v1.2.0 tag). |
| PR 5 | workflow-plugin-azure → tag `v2.0.0-rc1` |

### PR 6 — workflow plugin-conformance gate + final tag `v0.56.0`

After all 4 plugin rc1 tags exist:

- New CI matrix step in workflow: build each `workflow-plugin-{aws,gcp,azure,digitalocean}@v2.0.0-rc1` against `workflow@v0.56.0-rc1` and run each plugin's iacserver_test smoke.
- Matrix mechanics: each cell uses `GOFLAGS=-mod=mod` + `go.mod replace github.com/GoCodeAlone/workflow => github.com/GoCodeAlone/workflow vX.Y.Z-rc1` to bypass proxy indexing lag; OR `gh release download v0.56.0-rc1` of the workflow rc tarball. Either is fine; pick what the existing GoReleaser conformance gate uses.
- On green: tag `workflow v0.56.0` final.
- Bump go.mod minimums in any in-repo consumers that need the new wfctl semantics.

### PRs 7-10 — plugin final tags + registry manifest bumps (parallel, fan-out from PR 6)

Each plugin's final-tag PR includes the registry manifest update in the same PR (collapses PR 11 into per-plugin scope; cleaner rollback unit since each plugin's registry pin can be reverted independently):

- Bump SDK pin from `v0.56.0-rc1` → `v0.56.0`, bump plugin.json to `v2.0.0`, tag `v2.0.0`.
- Update `workflow-registry/v1/plugins/<provider>/manifest.json` to `version: 2.0.0`, `minEngineVersion: 0.56.0`. (DO registry pin currently at `1.0.12`; this PR catches DO up from the manifest-derivation lag in the same hop — see I-NEW-3.)

| PR | Repo |
|---|---|
| PR 7 | workflow-plugin-digitalocean → tag `v2.0.0` + registry manifest bump (`1.0.12` → `2.0.0`) |
| PR 8 | workflow-plugin-aws → tag `v2.0.0` + registry manifest bump |
| PR 9 | workflow-plugin-gcp → tag `v2.0.0` + registry manifest bump |
| PR 10 | workflow-plugin-azure → tag `v2.0.0` + registry manifest bump |

### Memory + tracker updates

- Update `project_open_followup_queue.md`: mark workflow#699 done; cross-ref the v0.56.0 / plugin-v2.0.0 release notes.
- Update `MEMORY.md` plugin inventory (versions).
- New project memory: `project_workflow_699_apply_removal_shipped.md` (post-merge retro).
- File followup issue for gcp sync-plugin-version gap (PR 4 incidental).

## Assumptions

- **A1** — aws/gcp/azure/DO all currently declare ComputePlanVersion=v2 via Capabilities RPC. **Verified 2026-05-17 by direct grep**: aws `internal/iacserver.go:125`, gcp `:125`, azure `:128`, DO `:182`. Pre-PR-1-task-1 re-check predicate: `grep -q '"v2"' internal/iacserver.go` in each plugin repo head.
- **A2** — no third-party IaC plugins exist. Per ADR 0024 cycle 1 I-5 grep. Holds because `interfaces.IaCProvider` is a Go interface (not a registry surface) and the engine + the four GoCodeAlone plugin repos are the only consumers. If this assumption is ever wrong, see §Rollback for the soft-add-back path (Approach B).
- **A3** — `module.PlatformProvider` is a different interface from `interfaces.IaCProvider`. Verified: `module/platform_provider.go:5` (4 methods including `Apply() (*PlatformResult, error)`, no context arg). `module/pipeline_step_iac.go:208` (`provider.Apply()` no args) confirms it calls `PlatformProvider`, not `IaCProvider`. This file is unaffected by this change.
- **A4** — load-time enforcement via the typed `CapabilitiesResponse.compute_plan_version` is the correct gate (NOT parse-time `findIaCPluginDir` plugin.json switch). Verified: all 4 plugins populate the typed field, only DO populates the plugin.json field. The typed RPC is callable at load time (`typedIaCAdapter.fetchCapabilities` at `iac_typed_adapter.go:315` already does this for `SupportedCanonicalKeys`).
- **A5** — `ApplyResult` + `ActionStatus` + `IaCProviderFinalizer.FinalizeApply` GO-side shapes stay (the proto-side `pb.ApplyResult` is dead and deleted; the Go `interfaces.ApplyResult` is populated wfctl-side by `iac/wfctlhelpers/apply.go:318` and unaffected). Verified in main: commits `aac519da` (Phase 2.5) + `7a855934` (Phase 2.3) shipped FinalizeApply + ActionStatus enums; `FinalizeApply` returns `FinalizeApplyResponse{Errors}` (proto lines 531-557), NOT `ApplyResult`.
- **A6** — wire-format breaking change is accepted per ADR 0024 force-cutover. NO `buf-breaking-check` compat-preserving claim. Operators must upgrade wfctl + plugins atomically — same constraint as Phase 2. The minimum-engine-version field in plugin manifests (`minEngineVersion: 0.56.0`) is the operator-facing gate that surfaces the upgrade requirement.

## Rollback

This change cascades through workflow + 4 plugins + 4 registry manifest bumps (collapsed into per-plugin PRs 7-10). Rollback path, per-plugin granular:

1. **Per-plugin rollback** — each plugin's PR (7-10) is independently revertable. Reverting `workflow-plugin-X` PR (7-10) restores the registry manifest pin AND the plugin's v1.x tag as the recommended version. Cleaner unit than the previous "PR 11 mega-rollback" — operators on plugin-X get rolled back without affecting plugins Y/Z.
2. **Workflow rollback** — revert PR 6 (`workflow v0.56.0` tag). Re-publish v0.55.x as the recommended pin. ALL plugins must roll back too (because their `minEngineVersion: 0.56.0` blocks them on the rolled-back workflow). This is the "nuclear" rollback path.
3. **RC tag handling** — RC tags (PRs 1-5 rc1) don't need active revert; operators don't pin to rc. RC tags can be left in place or yanked at maintainer discretion.
4. **State-file format invariant** across the cutover — `interfaces.ResourceState` JSON shape unchanged. Operators do not need to migrate state.
5. **Half-rolled-back state window** — between registry-manifest revert (step 1) and operators actually re-pulling via `wfctl plugin install`, some operators may already have v2.0.0 plugin binaries on disk. These continue to work against v0.56.0 wfctl; the issue is only for operators who downgraded wfctl to v0.55.x in the same window. Document in rollback runbook: "If you've already pulled v2.0.0 plugins, either keep v0.56.0 wfctl OR `wfctl plugin install --force` after registry revert."
6. **Rollback floor (all 4 plugins)** — registry manifests are stale relative to live tags across the board (per cycle-3 I-NEW-7 inspection 2026-05-17): aws `0.1.2`, gcp `0.1.3`, azure `0.1.2`, DO `1.0.12`. PRs 7-10 each bump from these stale pins straight to `2.0.0` — an aggressive jump for aws/gcp/azure (`0.1.x` → `2.0.0`). Rollback restores LIVE tags, not registry pins: aws → `v1.2.1`, gcp → `v1.2.0`, azure → `v1.2.1`, DO → `v1.4.0`. Each PR (7-10) description must document its specific rollback floor. The pre-existing manifest-derivation lag is tracked under the "catalog manifest-derivation refactor" followup queue item but cannot be cleanly separated from this cascade because the same registry PRs need to land regardless.
7. **Soft-add-back option (Approach B)** — if the rollback is driven by a third-party plugin surfacing post-cutover, the architectural re-introduction path is Approach B (optional `IaCProviderLegacyApplier` service per ADR 0025), NOT restoring `rpc Apply` on `IaCProviderRequired`. Approach B preserves the compile-time-safety guarantee while letting the third-party plugin opt in.

The change is runtime-affecting (proto change, plugin gRPC service surface change), so `runtime-launch-validation` applies: each plugin PR must run iacserver_test (the per-plugin runtime smoke) before merge. PR 6 adds the cross-repo conformance gate.

## Adversarial-review cycle 1 + cycle 2 findings addressed

| Finding | Cycle | Severity | Resolution |
|---|---|---|---|
| Schema gate bypass — `findIaCPluginDir` raw `json.Unmarshal` | 1 C | Critical | Cycle 2 pivoted: enforcement moved from parse-time (findIaCPluginDir) to load-time (discoverAndLoadIaCProvider via typed Capabilities RPC). See A4. |
| FinalizeApply citation | 1 C | Critical | Cycle 1: re-verified after rebase. Cycle 2 reviewer further found that FinalizeApply does NOT return ApplyResult; corrected A5 + Context. |
| azure A1 grep line ref | 1 I | Important | Per-plugin line numbers in A1 table. |
| Rollback ignores registry fan-out | 1 I | Important | Cycle 2: registry-manifest scope collapsed into per-plugin PRs 7-10. Rollback granular per plugin (step 1). |
| Parallel PRs race PR 6 | 1 I | Important | rc1 protocol for plugins (PRs 2-5) → conformance gate (PR 6) → fan-out (PRs 7-10). |
| Approach B as add-back | 1 I | Important | Decision §Approach B + Rollback step 7. |
| PR 1 edit ordering | 1 I | Important | 8 numbered safe-edit-order steps. |
| Title vs body | 1 M | Minor | Summary note. |
| Plugin rc1 tags | 1 M | Minor | PRs 2-5 ship rc1. |
| Reserve method+field on service | 1 M | Minor | Cycle 2 reviewer flagged this as invalid proto3; replaced with CI lint (PR 1 step 5). |
| C-NEW-1: aws/gcp/azure plugin.json missing field | 2 C | Critical | Pivoted gate to typed Capabilities RPC (A4). plugin.json no longer required to declare field. |
| C-NEW-2: ApplyResult/Response not actually used by FinalizeApply | 2 C | Critical | Deleted `message ApplyRequest/Response/Result` AND `pb.ApplyResult`-using tests + `applyResultFromPB` (PR 1 step 5). Verified by `grep pb.ApplyResult` returning only files this PR modifies. |
| C-NEW-3: `reserved` invalid on proto3 service | 2 C | Critical | Dropped `reserved` syntax. CI lint instead (PR 1 step 5). |
| I-NEW-1: stale line `:1551-1563` | 2 I | Important | Re-verified via grep: actual lines `:467-487` + `:1660-1722`. PR 1 step 1 updated. |
| I-NEW-2: worktree shadow | 2 I | Important | Pre-PR-1 verification step grep added. Stale worktree to be cleaned up before PR 1. |
| I-NEW-3: DO registry pin 1.0.12 vs live 1.4.0 | 2 I | Important | Rollback step 6 documents floor (recommend `1.4.0`, not `1.0.12`). |
| I-NEW-4: buf-breaking-check overstatement | 2 I | Important | A6 rewritten: explicit "no wire-compat preserved, force-cutover per ADR 0024." |
| I-NEW-5: rc-tag matrix mechanics | 2 I | Important | PRs 2-5 mechanics now specify SHA-pin OR `go.mod replace` OR `gh release download`. PR 6 same. |
| m-NEW-1: gcp sync-plugin-version gap | 2 M | Minor | PR 4 files followup issue inline. |
| m-NEW-2: A3 PlatformProvider regression risk | 2 M | Minor | PR 1 verification step adds `go test ./module/...` |

## Cycle-3 adversarial-review findings (surgically fixed, max-cycles reached)

Per `adversarial-design-review` skill, 2 revision cycles is the cap. Cycle 3 surfaced 3 narrowly-scoped Critical findings + 5 Important; all 3 Critical fixes are typo-class edits applied directly in this revision (cycle-3 reviewer's own recommendation: "These are surgical: add `cmd/iac-codemod` deletion to PR 1 step 9, name `applyPrecomputedPlanWithStore` explicitly in PR 1 step 1, add `go build ./... && go vet ./...` to pre-PR-1 gate. These are typo-class edits to the design doc; they do not require another full cycle."). Operator escalation summary:

| Finding | Severity | Resolution |
|---|---|---|
| C-NEW-4: `cmd/iac-codemod` graveyard | Critical | PR 1 step 9 added: delete entire directory. |
| C-NEW-5: `applyPrecomputedPlanWithStore` not explicit in PR 1 step 1 | Critical | PR 1 step 1 edit-list correction names both call sites + grep predicate (5→0 `usedV2Dispatch` count). |
| C-NEW-6: missing `go build` + `go vet` pre-merge gate | Critical | Pre-PR-1 verification step now requires `go build ./... && go vet ./...`. |
| I-NEW-6: Capabilities-RPC timeout / cache poisoning | Important | PR 1 step 3 mandates `context.WithTimeout(ctx, 10s)` + separate cache for load-gate path. |
| I-NEW-7: aws/gcp/azure registry floors (not just DO) | Important | Rollback step 6 covers all 4 plugins with per-plugin live-tag floors. |
| I-NEW-8: misleading "RELAXED back" language | Important | Rewritten: `findIaCPluginDir` ALREADY accepts `"", "v1", "v2"`; design adds deprecation log only. |
| I-NEW-9: `applyResultToPB` in plugin iacservers | Important | Pre-PR-1 grep predicate added; PRs 2-5 must delete the helpers. |
| I-NEW-10: bare `#TBD` for gcp followup | Important | Will file the gcp sync-plugin-version followup issue BEFORE PR 1 lands, then patch the number. |

Operator acceptance request (per `adversarial-design-review` skill): all 3 Critical findings have surgical fixes incorporated above (typo-class). 5 Important findings have concrete fixes incorporated. The design is committed; no further adversarial cycles are budgeted.

## References

- Issue: https://github.com/GoCodeAlone/workflow/issues/699
- ADR 0024 (force-cutover precedent): `decisions/0024-iac-typed-force-cutover.md`
- ADR 0025 (optional services as typed services, not flags): `decisions/0025-iac-optional-method-typed-services-not-bool.md`
- ADR 0029 (capability extension — canonical_keys + compute_plan_version): `decisions/0029-capability-extension-canonical-keys-and-compute-plan-version.md`
- Phase 2 (workflow#640): `docs/plans/2026-05-10-strict-contracts-force-cutover.md` + memory `project_v2_lifecycle_phase2_shipped.md`
- Phase 2.5 (workflow#695): merged main commit `aac519da` (IaCProviderFinalizer + OnPlanComplete hook)
- Phase 2.3 (workflow#698): merged main commit `7a855934` (ActionStatus compensation enums)
- Phase 2.5+ Cleanup Bundle adversarial-design-review cycle-1 C-5 finding (originating context for this issue)
- Cycle-1 + Cycle-2 + Cycle-3 adversarial review of this design (2026-05-17, all addressed above; Cycle-3 was the final allowed cycle per skill bound)
