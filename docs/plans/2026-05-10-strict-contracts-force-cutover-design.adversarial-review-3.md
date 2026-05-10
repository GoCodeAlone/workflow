---
status: approved
review_cycle: 3
target: docs/plans/2026-05-10-strict-contracts-force-cutover-design.md
target_commit: 4e541659
date: 2026-05-10
verdict: FAIL
---

# Adversarial Review — Strict-Contracts Force-Cutover Design (Cycle 3)

**Phase:** design (cycle 3)
**Artifact:** `docs/plans/2026-05-10-strict-contracts-force-cutover-design.md` (commit `4e541659`, rev3)
**Status:** **FAIL** — 1 Critical, 2 Important. Per cycle 3 directive ("don't nitpick"), Minor findings omitted.

Cycle 2 findings (C-1, I-1, I-2, I-3, I-4) all have substantive resolutions in rev3 — verification table at bottom shows 5 of 5 PASS or PASS-with-caveats. The new Critical (C-1 below) is unrelated to the cycle 2 findings and was not surfaced in cycles 1 or 2: it is structural collateral damage from §Removed surface that was overlooked because cycle 1+2 framed `InvokeService` as IaC-specific machinery when it is in fact the SDK's only generic service-dispatch surface, used by SecurityScanner-class plugins and at least 5 other workflow-plugin-* repos.

---

## Findings

### CRITICAL

#### C-1 (NEW). §Removed surface deletes the SDK's generic service-dispatch surface used by 6+ non-IaC plugins; cutover breaks SecurityScanner adapter and every plugin using `ServiceInvoker`

Rev3 §Architecture →§Removed surface (lines 156-164) enumerates deletions:

> - `plugin/external/sdk/interfaces.go`: DELETE `ServiceInvoker`, `ServiceContextInvoker`, `TypedServiceInvoker` type definitions
> - `plugin/external/sdk/grpc_server.go`: DELETE `InvokeService` RPC handler implementation
> - `plugin/external/proto/plugin.proto`: DELETE `InvokeServiceRequest`, `InvokeServiceResponse` messages + the `InvokeService` RPC method
> - `plugin/external/remote_module.go`: DELETE `RemoteModule.InvokeService` and `RemoteModule.InvokeServiceContext` method receivers

Verified in workspace: these surfaces are NOT IaC-specific. They are workflow's only generic plugin service-dispatch mechanism, consumed by:

**Workflow itself (in-tree consumer of `RemoteModule.InvokeService`):**
- `/Users/jon/workspace/workflow/_worktrees/strict-contracts-force-cutover/plugin/external/security_scanner_adapter.go:49,64,78` — `p.module.InvokeService("ScanSAST", args)` / `"ScanContainer"` / `"ScanDeps"`. Wraps `interfaces.SecurityScanner`.

**Other workflow-plugin-* repos using `ServiceInvoker` / `InvokeMethod` / `InvokeService` for non-IaC dispatch (verified via `grep -rln` across `/Users/jon/workspace/workflow-plugin-*`):**
- `workflow-plugin-approval/internal/module_engine.go` + `typed.go` + `steps.go`
- `workflow-plugin-audit/internal/module_collector.go` + `typed.go` + `sink_db.go` + `step_query.go` + `module_sink_db.go` + `module_sink_s3.go`
- `workflow-plugin-gitlab/internal/typed.go`
- `workflow-plugin-migrations/internal/module_driver.go` + `module_migrations.go` + `atlasplugin/modules.go`
- `workflow-plugin-security-scanner/internal/module_scanner.go` + `typed.go`
- `workflow-plugin-digitalocean/internal/module_instance.go` (the in-scope deletion target)

That's **6 workflow-plugin-* repos plus 1 in-tree workflow adapter** that the cutover would break by deleting `ServiceInvoker` / `InvokeService` / `RemoteModule.InvokeService*`.

Rev3 §Scope (line 49) lists "All other plugin interfaces (Module, Step, Trigger, Service)" as untouched. But §Removed surface deletes the underlying RPC + interfaces these plugins are wired to. The two statements are mutually inconsistent. §Scope says "Service is untouched"; §Removed surface deletes the only mechanism by which Service plugins can be remotely dispatched.

The acceptance criterion (line 165) — `git diff --stat` ≥1500 lines deleted — would CI-pass against the workflow-only diff but break every plugin's CI immediately on next build because `pluginexternal.RemoteModule.InvokeService` would no longer exist.

**Why this is Critical, not Important:**
1. Multiple production plugin repos (audit, approval, migrations, security-scanner, gitlab) are on the workflow main branch's main consumer surface. The cutover deletes their RPC.
2. workflow-cloud (per cycle 2 verification) imports `pluginexternal` directly via `cloudplugin/sample_registry.go:46` and `cmd/cloudserver/main.go:116`. Its plugin manager calls `LoadPlugin` which returns `*ExternalPluginAdapter` whose `Module()` returns a `*RemoteModule` whose `InvokeService` would be gone. workflow-cloud breaks on workflow v1.0.0.
3. The session-evidence bug class this design targets (audit-keys / EnumerateAll IaC dispatch) is a strict subset of the surface being deleted. The deletion overshoots the scope by a factor of 6+ plugin repos.
4. Phase 2 §pin-bumps does not enumerate any of these 6 repos as needing migration; they're treated as out-of-scope, but they are in fact direct consumers of the deleted surface.

**Fix recommendation (one of three):**
- **Option A — Narrow the deletion scope.** Delete ONLY the IaC-specific paths in `cmd/wfctl/deploy_providers.go` (the `remoteIaCProvider` proxy struct + the type-assert sites). KEEP `ServiceInvoker`, `TypedServiceInvoker`, `InvokeService` RPC, `RemoteModule.InvokeService*` for non-IaC plugin use. The cutover is then "delete the IaC-specific proxy + add typed `pb.IaCProviderClient`"; non-IaC plugins continue using the generic surface. This is the minimum-blast-radius option and matches what the cycle 1 + 2 framing implied was being deleted.
- **Option B — Expand scope honestly.** Acknowledge the cutover is across all `ServiceInvoker`-using plugins (~7 repos). Each of those needs a typed `pb.<Capability>Server` interface (security-scanner needs `pb.SecurityScannerServer`, audit needs `pb.AuditCollectorServer`, etc.). Phase 1 expands from 2 PRs to ~10 PRs. Timeline is no longer "1-2 weeks" — it is "8-16 weeks parallel cross-org effort." The "DO-only Phase 1" framing per I-3 is structurally impossible because the SDK surface deletion is not IaC-scoped.
- **Option C — Defer the SDK-surface deletion.** Keep the `InvokeService` RPC + `ServiceInvoker` surface live; add typed `pb.IaCProviderClient` alongside; delete only the `remoteIaCProvider` wrapper struct in wfctl. The "no compat window" mandate is preserved at the IaC interface level (typed-only for IaC) but the generic SDK surface remains for other plugin classes pending their own per-class typed cutovers. ADR records that this is a partial cutover with named follow-up plans for non-IaC plugin classes.

The design currently reads as Option A (per §Scope's claim that other interfaces are untouched) but is written as Option B (per §Removed surface's deletions). One of the two has to give before the design is implementable.

---

### IMPORTANT

#### I-1 (NEW). Six optional services with per-service registration helpers creates plugin-side registration boilerplate that itself becomes a forgotten-step bug class

Rev3 §Architecture (lines 105-145) introduces a 7-service split:
- 1 required service (`IaCProviderRequired`)
- 6 optional services (`IaCProviderEnumerator`, `IaCProviderDriftDetector`, `IaCProviderCredentialRevoker`, `IaCProviderMigrationRepairer`, `IaCProviderValidator`, `IaCProviderDriftConfigDetector`)

Plugin authors register each capability via separate helpers:
```go
sdk.RegisterIaCProviderRequiredServer(grpcServer, provider)  // required
sdk.RegisterIaCProviderEnumeratorServer(grpcServer, provider)  // optional
sdk.RegisterIaCProviderDriftDetectorServer(grpcServer, provider)  // optional
// ... etc
```

This is the cycle 2 I-2 fix taken to its logical conclusion: capability advertisement happens at service registration, not via per-call boolean. Defense reads sound on its own merits.

But the user-mandate goal is "make the missing-handler bug class a compile-time error." The 7-service split MOVES the bug class from "stub method returns NotSupported" to "plugin author forgot to call `RegisterIaCProviderEnumeratorServer`." That is structurally the SAME bug class as cycle 2 I-2's `NotSupported bool` escape hatch — except now it manifests as "the service silently isn't there" rather than "the boolean is silently true."

Specifically:
- A DO plugin author intends to support EnumerateAll. They write `func (p *DOProvider) EnumerateAll(ctx, *pb.EnumerateAllRequest) (*pb.EnumerateAllResponse, error) {...}` correctly. They register `IaCProviderRequired` (because compiler forces them to). They forget to call `sdk.RegisterIaCProviderEnumeratorServer(server, impl)`. Plugin compiles; ships; runtime: wfctl checks "is `IaCProviderEnumerator` advertised on this plugin handle?" — answer is no; wfctl falls through silently to next provider OR errors. This is the EnumerateAll-missing bug class re-created.
- The compile-time gate the design touts ("Go compiler enforces full interface satisfaction") only applies to IMPLEMENTING the methods, not to REGISTERING the service. There is no language-level mechanism that says "if you implement `pb.IaCProviderEnumeratorServer`, you must register it."

Two structural alternatives the design didn't consider:
- **Single service, all methods required, server returns typed sentinel for unsupported optional methods.** Reverses cycle 2's I-2 split: every method is in one service; optional methods may return a typed `Unsupported` *response message* (not a transport status, not a `NotSupported bool` field). The compile-time gate enforces "every method has a handler"; the runtime gate enforces "explicit unsupported decision per method." Capability is per-method, not per-service-registration.
- **Reflection-based registration check at wfctl handle-open time.** Add a workflow-side helper `sdk.MustRegisterIaCProvider(server, impl)` that uses Go reflection or build-time codegen to inspect `impl` for satisfied interfaces and register all matching services automatically. Plugin author writes `sdk.MustRegisterIaCProvider(server, doProvider)` once. If `doProvider` has an `EnumerateAll` method matching the `pb.IaCProviderEnumeratorServer` interface, it is auto-registered. No per-capability registration call to forget.

**Why this is Important not Critical:** The 7-service split IS better than the cycle 1 `codes.Unimplemented` design — it converts a per-call runtime decision to a per-plugin compile-and-register decision, narrowing the failure surface. But it is not the structural bug-class eliminator the design's framing promises. The bug class moves from "method missing" to "service registration missing" — both of which present the same way to the user (silent fall-through or runtime error).

**Fix recommendation:** Either:
1. Adopt the auto-registration pattern (option 2 above) so plugin authors cannot forget to register, OR
2. Add a wftest contract test `wftest.AssertAllImplementedServicesRegistered(t, plugin)` that uses reflection to compare implemented interfaces against registered services and fails if any implemented capability is unregistered. Mandate the test in the §Acceptance criteria.
3. Acknowledge in §Top doubts (renumbered) that the 7-service split TRADES one bug-class surface (NotSupported boolean) for another (service-registration omission), and the trade is justified by frequency-of-occurrence not elimination.

The current text reads as "this is the structural fix"; honest framing is "this is a better trade-off."

#### I-2 (NEW). Hardcoded YAML pin replacement with a single `${{ vars.WFCTL_VERSION }}` collapses two distinct version semantics into one — `teardown.yml` and `registry-retention.yml` need to remain pinned to OLD wfctl versions for state-file backward compat

Rev3 §Phase 2 (lines 228-238) prescribes:

> Hunt and replace EVERY hardcoded `version: vX.Y.Z` in `.github/workflows/*.yml` — cycle 2 C-1 enumerated 9 hardcoded values across 7 files: `teardown.yml`, `deploy.yml`, `bootstrap.yml`, `image-launch-ci.yml`, `tc2-cutover.yml`, `drift-recovery.yml`, `registry-retention.yml`. Replace each with `version: ${{ vars.WFCTL_VERSION }}` (defer to the GH variable so future bumps are single-source-of-truth).

Verified in workspace (`grep -nE "version: v[0-9]" /Users/jon/workspace/core-dump/.github/workflows/*.yml`):

```
bootstrap.yml:37:          version: v0.21.2     <- "current" wfctl, used for fresh deploys
deploy.yml:36/70/95:       version: v0.14.2     <- OLD wfctl, used for STAGED deploy
drift-recovery.yml:72:     version: v0.21.2     <- "current" wfctl
image-launch-ci.yml:98:    version: v0.21.2     <- "current" wfctl
registry-retention.yml:23: version: v0.14.2     <- OLD wfctl, used for retention-management
tc2-cutover.yml:154:       version: v0.21.2     <- "current" wfctl
teardown.yml:41:           version: v0.14.2     <- OLD wfctl, used for state-file teardown
```

Two distinct version semantics are entangled in the YAML pin set:
- **"Current" wfctl (v0.21.2)** — used for new deploys, bootstrap, drift-recovery. These should bump to v1.0.0.
- **"Old" wfctl (v0.14.2)** — used for `teardown.yml`, `deploy.yml` rollback paths, `registry-retention.yml`. These were pinned old INTENTIONALLY (per project memories `project_p0_core_dump_wfctl_bump_shipped` from 2026-04-13: bumping wfctl on teardown can render existing state files unreadable; bumping wfctl on registry-retention may incompatibly read old `wfctl-lock.yaml` shapes).

Rev3 prescribes a single `${{ vars.WFCTL_VERSION }}` replacement for ALL 9 sites. After the cutover, ALL 7 workflows use whatever `WFCTL_VERSION` resolves to. If `WFCTL_VERSION = v1.0.0`, then `teardown.yml` runs `wfctl v1.0.0` against state files written by `wfctl v0.14.2` deploys. Per cycle 1 C-3 (state-file format invariance) — which rev3 §F-3 promises is upheld for THIS cutover — this works for v0.14.2 → v1.0.0 IF the state-file schema is stable. But:

1. Has anyone verified that v0.14.2-written state files are readable by v1.0.0? Rev3 §F-3 only commits to the schema being unchanged across THE CUTOVER (v0.27.x → v1.0.0). It does NOT commit to v0.14.2 → v1.0.0 schema compatibility, and v0.14.2 was the wfctl version BEFORE the IaC interfaces were even consolidated (per memory: v0.14.2 is from the pre-IaC era).
2. If the answer is "v0.14.2 state files require v0.14.2 wfctl to read," then collapsing teardown.yml to use the new variable BREAKS teardowns of any infrastructure deployed pre-v0.21.x.
3. Even if v0.14.2 state files happen to work, the framing "single source of truth via WFCTL_VERSION" hides the version-semantics distinction. A future operator bumping `WFCTL_VERSION` to v2.0.0 has no signal that some workflows were intentionally pinned old.

**Fix recommendation:** Per-workflow audit before applying the single-variable rewrite. Two GH variables, not one:
- `WFCTL_DEPLOY_VERSION` — for fresh-deploy workflows (bootstrap, deploy, drift-recovery, image-launch-ci, tc2-cutover). Bump to v1.0.0 in Phase 2.
- `WFCTL_LEGACY_STATE_VERSION` — for teardown + retention workflows that read state-files from old deploys. Bump only when verified state-file-format-compat across the version span.

Alternative: explicitly verify and document that ALL state files written by ANY supported old wfctl version (≥v0.14.2) are readable by v1.0.0 wfctl, and add a CI gate that exercises this for each historical version. If verified, the single-variable approach is fine; if not, two variables are required.

The CI regression-prevention gate (rev3 line 233) prevents future hardcoded pin drift but does NOT prevent the wrong version semantics being applied to teardown/retention workflows.

---

## Cycle 2 finding-resolution verification

| Cycle 2 finding | Rev3 resolution claim | Verdict | Notes |
|---|---|---|---|
| **C-1** Phase 2 missed hardcoded YAML pins | Phase 2 enumerates 9 sites across 7 core-dump files + adds CI regression gate | **PASS-with-caveats** | Files enumerated (line 232) match workspace grep. CI gate spec is concrete (line 233). New caveat: see I-2 above (single-variable replacement collapses two version semantics). |
| **I-1** Atomic merge impossible | Operator-side upgrade order: workflow rc1 → DO v1.0.0 → workflow v1.0.0 cutover (line 383-389) | **PASS** | Three-step order is sound: (1) workflow `v1.0.0-rc1` ships typed proto + KEEPS legacy; (2) DO `v1.0.0` ships typed against rc1; (3) workflow `v1.0.0` final ships cutover (deletes legacy). At step (3), DO v1.0.0 already exists in the registry; no availability gap. Operator order documented per F-1 pre-flight gate. Note: rev3 explicitly says rc1 is "Backwards-compatible. wfctl can load both legacy plugins (v0.14.x) and typed plugins (v1.0.0+rc)" — this is a transitional dual-mode in the rc1 release, but it is NOT in workflow main, and main never has both paths. Acceptable per "no soak in main" mandate. |
| **I-2** NotSupported stub escape hatch | Split into `IaCProviderRequired` (no NotSupported) + 6 optional services (registration-based capability advertisement) | **PASS-with-new-finding** | Split is implemented at lines 88-145. Required service has no NotSupported field. Optional services are registered-only-if-implemented. Cycle 2 I-2 closed. NEW concern surfaces — see I-1 above (registration-omission is a new bug-class surface). |
| **I-3** User-intent drift on scope | §Scope (line 37) explicitly notes user authorization for the reduction; recorded as ADR-1 override | **PASS** | Lines 37: "User authorized this reduced scope via cycle 3 directive ('don't nitpick; cycle as many times as necessary'). Recorded as ADR-1 override." Honest framing, defers expansion to future plans. |
| **I-4** F-4 grep wrong tool | Replaced with `go list -m -json` + grpc-versions.txt artifact | **PASS** | Lines 305-323 specify: workflow publishes `grpc-versions.txt` per release tag; plugin CIs run `go list -m -json google.golang.org/grpc | jq -r .Version`; gate asserts match. Mechanism is sound; cross-repo source-of-truth is concrete. |

**Summary:** All 5 cycle 2 findings have substantive resolutions. C-1 and I-2 close cleanly. I-1 + I-2 + I-3 + I-4 close as PASS. The new findings (C-1 + I-1 + I-2 in this report) are unrelated to the cycle 2 set; they are surfaced fresh by cycle 3's deeper inspection of the SDK-surface-removal scope and YAML-pin-version semantics.

---

## Bug-class scan transcript

| Class | Found? | Note |
|---|---|---|
| **Unstated assumptions** | YES | C-1 (assumed `InvokeService` is IaC-specific; it is the SDK's generic dispatch surface); I-2 (assumed all 9 YAML pins encode the same version semantic; two distinct semantics exist). |
| **Repo-precedent conflicts** | YES | C-1 — §Scope says "Module/Step/Trigger/Service untouched" but §Removed surface deletes the RPC underneath those interfaces. Internal contradiction. |
| **YAGNI violations** | NONE NEW | The 7-service split (per cycle 2 I-2 fix) is borderline — could collapse to 2 services or 1 service with typed sentinel response — but the design rationale (capability advertisement at registration time) is defensible. Flagged in I-1 as a trade-off, not YAGNI. |
| **Missing failure modes** | YES | C-1 — no failure-mode treatment for "non-IaC plugin breaks at workflow v1.0.0 build because RemoteModule.InvokeService is gone." I-2 — no failure-mode treatment for "teardown of pre-v0.21.x infrastructure with v1.0.0 wfctl." |
| **Security / privacy** | NONE NEW | Cycle 1 + 2 notes stand. Typed proto closes secret-leak risk via structured fields. |
| **Rollback story** | PARTIAL | §Rollback (lines 364-375) is honest about cost. Doesn't address the C-1 cascade — rolling back workflow v1.0.0 also requires reverting any in-flight non-IaC plugin migrations triggered by the deletion. |
| **Simpler alternative not considered** | YES | C-1 fix-recommendation Option A (narrow deletion scope to IaC-specific paths only) is simpler than the design's current scope and matches the IaC-only goal stated in §Scope. |
| **User-intent drift** | RESOLVED-CYCLE-2 | I-3 explicitly closed via user authorization. No new drift. |

---

## Verdict reasoning

Rev3 substantively resolves all cycle 2 findings. The C-1 + I-1 + I-2 findings here are NOT regressions of cycle 2 work — they are deeper structural issues that surface only when reading §Removed surface against the actual workflow + workflow-plugin-* code. Specifically:

- **C-1** is load-bearing: if the §Removed surface deletions land as written, every non-IaC plugin using `ServiceInvoker` breaks on the workflow v1.0.0 release. That includes the in-tree `security_scanner_adapter.go` AND 5+ external plugin repos. The cutover is structurally larger than the design admits.
- **I-1** is a real architectural trade-off the design papers over: the 7-service split moves the bug class from "stub returns NotSupported" to "service-registration omitted." Both surfaces present identically to the user. The fix is auto-registration via reflection OR a wftest contract test, not the current per-helper registration model.
- **I-2** is a missed semantic: rewriting all `version: vX.Y.Z` to a single GH variable assumes all 9 pins encode "the wfctl version this workflow needs," but 4 of 9 pins encode "the wfctl version the OLD state files require." Two GH variables, not one, OR explicit cross-version state-file-compat verification.

**Per skill rules** (cycle 3 explicit user directive: "cycle as many times as necessary"): FAIL escalates to user with these three concrete fixes. Recommend rev4 addressing C-1 (definitive answer on deletion scope: Option A, B, or C), I-1 (registration-omission bug class), and I-2 (per-workflow version semantic audit).

---

## Escalation summary

The design is structurally close to shippable but has one Critical that determines the fundamental shape of the cutover (C-1: how broad is the SDK-surface deletion?) and two Importants (I-1: registration-omission bug class; I-2: state-file-compat across version spans).

Recommended user actions:

1. **Resolve C-1 — pick deletion scope**:
   - **Option A (recommended for "DO-only Phase 1" framing):** narrow deletion to IaC-specific paths in `cmd/wfctl/deploy_providers.go` only. Keep `ServiceInvoker` / `InvokeService` RPC alive for non-IaC plugin classes. Acknowledge in §Scope that the SDK-surface deletion is scoped to IaC, not all-plugins.
   - **Option B (matches "force switch all of workflow* ecosystem"):** expand Phase 1 to include parallel typed-server work for all 6+ non-IaC plugin repos. Timeline becomes 8-16 weeks.
   - **Option C (compromise):** keep `InvokeService` RPC; delete only the `remoteIaCProvider` wrapper struct in wfctl. ADR records this is a partial cutover with named follow-up plans.

2. **Resolve I-1 — registration-omission bug class**: pick auto-registration via reflection OR mandatory wftest contract test. Update §Acceptance criteria to enforce the chosen mechanism.

3. **Resolve I-2 — YAML pin semantics**: per-workflow audit of state-file-compat assumptions before single-variable rewrite. Either two GH variables (deploy vs legacy-state) OR documented + CI-tested state-file-compat across the version span.

If rev4 lands these three resolutions, the design is ready for execution. The cycle 2 findings are well-resolved; cycle 3 finds new issues that didn't surface earlier because cycles 1 + 2 framed the scope narrower than the actual §Removed surface implies.
