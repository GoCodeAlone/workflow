# Design: workflow#699 — IaCProvider.Apply hard-removal

- **Date:** 2026-05-17
- **Status:** Approved (operator selected Approach D 2026-05-17)
- **Issue:** https://github.com/GoCodeAlone/workflow/issues/699
- **Precedent:** ADR 0024 (IaC typed force-cutover), ADR 0025 (IaC optional methods are typed services), Phase 2 (workflow#640) v2 hooks-over-gRPC cascade, Phase 2.5 (workflow#695) IaCProviderFinalizer cascade

## Summary

Hard-delete `IaCProvider.Apply` across workflow + 4 IaC plugins (aws/gcp/azure/DO). Eliminates the sentinel-stub runtime-failure surface DO v1.4.0 introduced (the very surface workflow#699 was filed to remove) without introducing a `LegacyApplier` opt-in interface (rejected for YAGNI + ADR 0024 force-cutover precedent).

After this change:

- `interfaces.IaCProvider` no longer declares `Apply`.
- `pb.IaCProviderRequired` no longer carries `rpc Apply` (field 6 reserved).
- `cmd/wfctl` has a single apply path (the v2 `wfctlhelpers.ApplyPlanWithHooks` dispatch); the v1 `provider.Apply` branch + the `iac/wfctlhelpers/dispatch.go` version-switch are deleted.
- All 4 IaC plugins drop their `Apply` Go method **and** their `iacserver.Apply` gRPC handler.
- The manifest schema rejects `iacProvider.computePlanVersion ∈ {"", "v1"}` at parse time (was `enum: ["v1","v2"]`).

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
| v2 | `wfctlhelpers.ApplyPlanWithHooks(ctx, provider, &plan, hooks)` → drives `ResourceDriver` per action + `IaCProviderFinalizer.FinalizeApply` for post-loop hooks | all 4 GoCodeAlone IaC plugins |

So `provider.Apply` is unreachable from `wfctl infra apply` for every plugin in the ecosystem. It is dead code:

- **DO**: `DOProvider.Apply` returns `ErrApplyV1Removed`; `doIaCServer.Apply` forwards the stub through gRPC; only callable via `wfctl infra apply` if the plugin author forgot to declare v2.
- **aws/gcp/azure**: `<Provider>.Apply` is a hand-rolled per-action loop (literally the v1 fallback implementation `wfctlhelpers.ApplyPlan` replaced). Unreachable since v1.2.x because all 3 plugins declare ComputePlanVersion=v2 in their `Capabilities` RPC response (`internal/iacserver.go:125`).

### Plugin verification (assumption A1)

| Plugin | Version | ComputePlanVersion declaration |
|---|---|---|
| workflow-plugin-aws v1.2.1 | live tag | `internal/iacserver.go:125` → `CapabilitiesResponse{..., ComputePlanVersion: "v2"}` |
| workflow-plugin-gcp v1.2.x | live tag | same shape (Phase 2 sweep) |
| workflow-plugin-azure v1.2.x | live tag | same shape (Phase 2 sweep) |
| workflow-plugin-digitalocean v1.4.0 | live tag | `plugin.json` `iacProvider.computePlanVersion: v2` + Capabilities |

Per ADR 0024 cycle 1 I-5: there are no third-party IaC plugins (no `interfaces.IaCProvider` consumers outside `workflow` + the four plugins above).

## Decision

Adopt **Approach D — hard-delete `Apply` from `interfaces.IaCProvider` + `pb.IaCProviderRequired`**.

Rejected alternatives:

- **Approach A** — extract `Apply` to a `LegacyApplier` Go-only interface; keep `rpc Apply` in proto. Rejected: proto layer still carries the sentinel-stub surface; sentinel error class survives at the gRPC boundary. Does not satisfy ADR 0024.
- **Approach B** — split `rpc Apply` into optional `IaCProviderLegacyApplier` service. Rejected for YAGNI per ADR 0024 precedent (force-cutover, no compat shim) and because no v1 plugin exists. Adding the optional service to "preserve the opt-in surface" replicates the sentinel-stub anti-pattern in a different guise.
- **Approach C** — version the Go interface (`IaCProviderV1` vs `IaCProviderV2`). Rejected: leaves two interfaces named "IaC provider" confusing the SDK + adapter type-asserts; does not touch the proto layer at all (so doesn't fix the bug the issue exists to fix).

## Scope (5-PR cascade)

PRs sequenced per Phase 2 / Phase 2.5 precedent (rc workflow tag first so plugins can build against new SDK, plugin majors second in parallel, workflow final third).

### PR 1 — workflow `feat/699-iac-apply-removal-rc` → tag `v0.56.0-rc1`

**Files modified:**

- `plugin/external/proto/iac.proto` — delete `rpc Apply(ApplyRequest) returns (ApplyResponse);` from `service IaCProviderRequired`; reserve field number 6; keep `message ApplyResponse` (still used by `FinalizeApply`-related telemetry transport via `ApplyResult.Actions`).
- `plugin/external/proto/iac.pb.go` — regenerate via `buf generate`.
- `interfaces/iac_provider.go` — delete `Apply(ctx context.Context, plan *IaCPlan) (*ApplyResult, error)` from the `IaCProvider` interface.
- `iac/wfctlhelpers/dispatch.go` — delete entire file (`ComputePlanVersionDeclarer`, `DispatchVersionFor`, `DispatchVersionV2`); v2 is the only dispatch path now.
- `cmd/wfctl/infra_apply.go:465-487` and `:1551-1563` — delete the v1 branch + the `usedV2Dispatch` variable (always true); collapse to single `applyV2ApplyPlanWithHooksFn` call.
- `cmd/wfctl/iac_typed_adapter.go` — delete `typedIaCAdapter.Apply`, `typedIaCAdapter.ComputePlanVersion`, the `_ wfctlhelpers.ComputePlanVersionDeclarer = (*typedIaCAdapter)(nil)` interface assertion at `:1348`, and the `ApplyRequest` encoding paths.
- `plugin/sdk/manifest.go` schema — tighten `iacProvider.computePlanVersion` to `enum: ["v2"]`; reject missing OR `"v1"` at `ParseManifest`. Error message must point operators at this issue + the plugin name.
- `plugin/external/sdk/iacserver.go` — `RegisterAllIaCProviderServices` type-assert against the trimmed `pb.IaCProviderRequiredServer`; verify the trimmed required service still compiles after `Apply` is removed from the interface.
- `wftest/bdd/strict_iac.go` `iacServiceChecks` — drop the `Apply` row from the IaCProviderRequired check.
- `cmd/wfctl/iac_loader_gate_test.go`, `cmd/wfctl/plugin_audit_iac_test.go`, `cmd/wfctl/plugin_audit.go`, and any test using `provider.Apply(...)` — delete the v1 dispatch coverage; the v2 path is the only call site to cover.
- `CHANGELOG.md` — entry noting the breaking change + plugin minimum versions.

**Tests added:**

- Manifest-schema test that rejects `iacProvider.computePlanVersion: ""` and `iacProvider.computePlanVersion: "v1"` with a clear "plugin foo needs to be upgraded to v2 (see workflow#699)" error.
- Integration test that an IaC plugin loaded via `discoverAndLoadIaCProvider` fails-fast at manifest parse if it advertises v1 (or omits the field).

**Backwards compat:** none — hard cutover per ADR 0024 precedent. The workflow rc tag is the moment plugins must rebuild against the new SDK; no compat shim.

### PR 2 — workflow-plugin-digitalocean (DO) — tag `v2.0.0`

- Bump `github.com/GoCodeAlone/workflow` pin to `v0.56.0-rc1` (then `v0.56.0` on PR 6).
- Delete `DOProvider.Apply` (the sentinel stub).
- Delete `ErrApplyV1Removed` constant.
- Delete `doIaCServer.Apply` RPC handler.
- Delete `internal/provider_apply_stub_test.go` (the sentinel regression-gate is obsolete).
- Bump `plugin.json` `minEngineVersion: 0.56.0` and `version: 2.0.0`.
- `CHANGELOG.md` entry: "BREAKING — drop v1 Apply method, require workflow v0.56+, per workflow#699."

### PR 3 — workflow-plugin-aws — tag `v2.0.0` (parallel with 4/5)

- Bump SDK pin to `v0.56.0-rc1` → `v0.56.0`.
- Delete `AWSProvider.Apply` (hand-rolled per-action loop, dead since v1.2.0).
- Delete `awsIaCServer.Apply` RPC handler.
- Drop the obsolete v1-Apply coverage in `internal/iacserver_test.go` + provider tests.
- Bump `plugin.json` to `v2.0.0`, `minEngineVersion: 0.56.0`.

### PR 4 — workflow-plugin-gcp — tag `v2.0.0` (parallel)

Same shape as PR 3. Delete `GCPProvider.Apply` + `gcpIaCServer.Apply` + dead tests; bump pin + manifest.

### PR 5 — workflow-plugin-azure — tag `v2.0.0` (parallel)

Same shape as PR 3. Delete `AzureProvider.Apply` + `azureIaCServer.Apply` + dead tests; bump pin + manifest.

### PR 6 — workflow final — tag `v0.56.0` (after PRs 2-5 are tagged)

- Bump go.mod minimums in registry consumers (e.g. workflow-registry plugin manifests, `core-dump` / `BMW` deploy paths) that need to surface new plugin majors.
- Update workflow-registry plugin manifests for aws/gcp/azure/DO to `v2.0.0`.
- Final tag.

### Memory + tracker updates

- Update `project_open_followup_queue.md`: mark workflow#699 done; cross-ref the v0.56.0 / plugin-v2.0.0 release notes.
- Update `MEMORY.md` plugin inventory (versions).
- New project memory: `project_workflow_699_apply_removal_shipped.md` (post-merge retro).

## Assumptions

- **A1** — aws/gcp/azure all currently declare ComputePlanVersion=v2 via Capabilities RPC. Verified: `internal/iacserver.go:125` (`CapabilitiesResponse{..., ComputePlanVersion: "v2"}`). Holds at the time of writing (2026-05-17); will re-verify at PR 1 task 1.
- **A2** — no third-party IaC plugins exist. Per ADR 0024 cycle 1 I-5 grep. Holds because `interfaces.IaCProvider` is a Go interface (not a registry surface) and the engine + the four GoCodeAlone plugin repos are the only consumers.
- **A3** — `module.PlatformProvider` is a different interface from `interfaces.IaCProvider`. Verified: `module/platform_provider.go:5` (4 methods including `Apply() (*PlatformResult, error)`, no context arg). `module/pipeline_step_iac.go:208` (`provider.Apply()` no args) confirms it calls `PlatformProvider`, not `IaCProvider`. This file is unaffected by this change.
- **A4** — workflow#693 manifest gate is the right enforcement point. Verified: `plugin/sdk/manifest.go:128` (`s.Validate(doc)` runs at ParseManifest; the schema accepts only the values we put in). Tightening the schema enum is the simplest enforcement.
- **A5** — `ApplyResult` + `ActionStatus` + `IaCProviderFinalizer.FinalizeApply` shape stays. Verified: Phase 2.3 (workflow#698) shipped `ApplyResult.Actions` + ActionStatus enums; FinalizeApply still uses `ApplyResult` shape for compensation telemetry. This change removes only the `Apply` RPC; it does not touch `ApplyResult` or `FinalizeApply`.
- **A6** — proto field-number reservation is breaking-compatible only in the "old client → new server" direction. Verified by buf-breaking-check semantics. New server (v0.56.0) refuses to encode/decode a reserved field; old client (pre-v0.56.0) attempting `rpc Apply` against new server gets `codes.Unimplemented` (the service descriptor no longer has the method). Operators must upgrade wfctl + plugins atomically — same constraint as Phase 2.

## Rollback

This change cascades through workflow + 4 plugins simultaneously. Rollback path:

1. **Revert workflow tag v0.56.0** → re-publish v0.55.x as the recommended pin. The pre-#699 state (sentinel-stub in DO, dead Apply loops in aws/gcp/azure) is the rollback shape.
2. **Revert plugin v2.0.0 tags** → re-publish the previous v1.x tags as the recommended pins.
3. **State-file format invariant** across the cutover — `interfaces.ResourceState` JSON shape unchanged. Operators do not need to migrate state.
4. **Consumer pin bumps** (registry, BMW, core-dump) — revert the go.mod bumps.

The change is runtime-affecting (proto change, plugin gRPC service surface change), so `runtime-launch-validation` applies: each plugin PR must run `docker compose up + curl healthz`-equivalent verification (the iacserver_test for each plugin) before merge.

## Adversarial-review targets (for the next phase)

The adversarial-design-review pass should attack:

1. **A1/A2 brittleness** — what if a third-party plugin actually exists somewhere we forgot? What's the worst failure mode? (Expected: plugin author rebuilds against v0.56.0 SDK, gets a clear compile-time error pointing them to this design doc.)
2. **Manifest gate sufficiency** — does the schema rejection actually fire on every load path? (Expected: yes, all loader paths go through `ParseManifest`.)
3. **Proto field reservation** — is reserving field 6 enough? Should we also reserve the `ApplyRequest` message type name? (Expected: reserving the field number is sufficient; the message type can stay since `ApplyResult` shape is unchanged.)
4. **Single-PR vs cascade trade-off** — could we land this as one mega-PR across all 5 repos? (Expected: no, because workflow rc tag must exist before plugins can pin against it.)
5. **Cascade ordering** — what if a consumer (BMW, core-dump) pins a new plugin major BEFORE workflow v0.56.0 is tagged? (Expected: this can't happen because the plugin v2.0.0 tag depends on the workflow rc tag; consumers should pin both atomically.)

## References

- Issue: https://github.com/GoCodeAlone/workflow/issues/699
- ADR 0024 (force-cutover precedent): `decisions/0024-iac-typed-force-cutover.md`
- ADR 0025 (optional services as typed services, not flags): `decisions/0025-iac-optional-method-typed-services-not-bool.md`
- Phase 2 (workflow#640): `docs/plans/2026-05-10-strict-contracts-force-cutover.md` + memory `project_v2_lifecycle_phase2_shipped.md`
- Phase 2.5 (workflow#695): memory `project_v2_lifecycle_phase2_shipped.md`
- Phase 2.5+ Cleanup Bundle adversarial-design-review cycle-1 C-5 finding (originating context for this issue)
