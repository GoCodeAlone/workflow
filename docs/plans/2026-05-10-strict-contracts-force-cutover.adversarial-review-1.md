---
status: approved
review_cycle: 1
target: docs/plans/2026-05-10-strict-contracts-force-cutover.md
target_commit: a1f4a2b1
phase: plan
date: 2026-05-10
verdict: FAIL
---

# Adversarial Review — Strict-Contracts Force-Cutover Plan (Cycle 1, plan-phase)

**Phase:** plan (cycle 1)
**Artifact:** `docs/plans/2026-05-10-strict-contracts-force-cutover.md` (commit `a1f4a2b1`)
**Design baseline:** `docs/plans/2026-05-10-strict-contracts-force-cutover-design.md` (rev5, commit `6073c3ce`) — design-phase cycle 4 verdict PASS.

**Verdict: FAIL.** Three Critical and three Important findings. Per "don't nitpick", Minor and style observations are omitted; only findings that block the plan from working or from achieving design goals are recorded.

---

## Critical findings

### C-1 (NEW) — PR 4 Task 16 changes the wfctl-side return type away from `interfaces.IaCProvider` but does not address the workflow-internal consumers of `interfaces.IaCProvider` outside `cmd/wfctl/`

**Evidence:**

`grep -rln "interfaces.IaCProvider\|interfaces.ResourceDriver" --include="*.go"` (excluding tests) finds these non-cmd/wfctl consumers in `/Users/jon/workspace/workflow/_worktrees/strict-contracts-force-cutover/`:

- `module/infra_module.go:28` — `provider interfaces.IaCProvider` field on `InfraModule`; engine-side bridge from YAML `infra.*` modules to provider drivers.
- `module/infra_module.go:83` — `provider, ok := providerSvc.(interfaces.IaCProvider)` — runtime type-assert in module bridge.
- `module/infra_module_deploy_bridge.go` — same.
- `platform/types.go`, `platform/differ.go` — platform-layer types reference `interfaces.IaCProvider`.
- `iac/wfctlhelpers/apply.go:78` — `func ApplyPlan(ctx context.Context, p interfaces.IaCProvider, plan *interfaces.IaCPlan)` — central apply path; called from `cmd/wfctl/infra_apply.go:61` (`var applyV2ApplyPlanFn = wfctlhelpers.ApplyPlan`).
- `iac/wfctlhelpers/dispatch.go` — same.
- `iac/wfctlhelpers/apply.go:91` — second call site signature taking `interfaces.IaCProvider`.
- `iac/refreshoutputs/refresh.go` — refresh path.
- `iac/conformance/scenarios.go`, `iac/conformance/scenario_grpc_roundtrip.go` — conformance harness.
- `iac/iactest/fakeprovider.go` — test double.
- `plugin/sdk/iaclint/iaclint.go` — lint helper.
- `interfaces/iac_resource_driver.go` — interface itself.
- `plugins/infra/plugin.go` — engine plugin registration.

**Why this is Critical:**

PR 4 Task 16 says it'll "REWRITE `discoverAndLoadIaCProvider` to return typed clients" and "every wfctl caller (audit-keys, prune, rotate-and-prune, cleanup, drift, status, plan, apply, destroy)" gets switched. But the wfctl callers themselves currently call `wfctlhelpers.ApplyPlan(ctx, provider, plan)` and `platform.ComputePlan(ctx, provider, specs, current)` — both of which take `interfaces.IaCProvider`. If `discoverAndLoadIaCProvider` returns `pb.IaCProviderRequiredClient` instead, those helper signatures break — and `wfctlhelpers/apply.go` is in a DIFFERENT package from `cmd/wfctl/`, so changing it cascades to:

- `module/infra_module.go` (engine-side, runs in workflow `server` binary, NOT wfctl)
- `platform/differ.go` (platform layer)
- `iac/refreshoutputs/refresh.go`
- `iac/conformance/*` (conformance harness)
- `plugin/sdk/iaclint/iaclint.go` (lint helper)
- `iac/iactest/fakeprovider.go` (test double)

The plan addresses ZERO of these. Three architectural choices exist:

**Option A:** Keep `interfaces.IaCProvider` as the engine-internal Go contract; build a `pb.IaCProviderRequiredClient → interfaces.IaCProvider` adapter inside wfctl. **This is exactly the wrapper layer cycle 1 Alternative C SAID TO ELIMINATE** — the design rejected this approach.

**Option B:** Refactor every internal consumer (~10 files outside cmd/wfctl) to take `pb.IaCProviderRequiredClient` (or a typed-client union) instead of `interfaces.IaCProvider`. The plan does not list any of these files in any task's "Files:" section. PR 4 task scope explodes from 7 tasks to ~20+.

**Option C:** Keep `interfaces.IaCProvider` AND keep the existing in-process dispatch (`module/infra_module.go`) using the Go interface, while ONLY replacing the wfctl-to-plugin gRPC wire path. Then `interfaces.IaCProvider` is satisfied wfctl-side by an adapter wrapping `pb.IaCProviderRequiredClient` — same wrapper layer Alternative C rejected.

The plan must pick one and own its task cost. Currently Task 16 implies Option B (no wrapper) but enumerates only the cmd/wfctl callers — leaving `wfctlhelpers/apply.go`, `platform/differ.go`, `module/infra_module.go`, etc. broken at compile time.

**Resolution required:** PR 4 must add explicit tasks for each of these consumer files, OR the plan must adopt Option C and own that the wrapper is being reintroduced (and update ADR 0026 accordingly). The current state — Option B implied, scope omitted — is a P0 plan defect: PR 4 will not compile as authored.

---

### C-2 (NEW) — PR 2 (Task 4) does NOT add the SDK API that PR 3 (Task 9) calls

**Evidence:**

PR 3 Task 9 (lines 879-891) shows the DO plugin entrypoint diff:

```diff
-    sdk.Serve(&plugin.ServePlugins{...})
+    grpcServer := grpc.NewServer()
+    iacServer := internal.NewIaCServer(provider)
+    if err := sdk.RegisterAllIaCProviderServices(grpcServer, iacServer); err != nil {
+        log.Fatalf("register iac services: %v", err)
+    }
+    sdk.ServeWithServer(grpcServer)
```

Two SDK functions are called: `sdk.RegisterAllIaCProviderServices(grpcServer, iacServer)` and `sdk.ServeWithServer(grpcServer)`.

`grep -n "func ServeWithServer" /Users/jon/workspace/workflow/_worktrees/strict-contracts-force-cutover/plugin/external/sdk/*.go` returns ZERO matches. The function does not exist today.

PR 2 Task 4 adds `RegisterAllIaCProviderServices(s *grpc.Server, provider any)` (lines 470-499). It does NOT modify `plugin/external/sdk/serve.go` to expose `ServeWithServer`. The current `sdk.Serve(provider PluginProvider)` (`serve.go:26`) calls `newGRPCServer(provider)` internally and hands its server to `goplugin.Serve` via `servePlugin.GRPCServer` — the plugin author has NO handle to the `*grpc.Server` to register additional typed services on.

**Why this is Critical:**

PR 3 Task 9 cannot be executed against PR 2's SDK as planned — the symbols don't exist and PR 2 doesn't add them. Two follow-on bugs:

1. `sdk.RegisterAllIaCProviderServices(s *grpc.Server, ...)` requires the plugin author to construct their OWN `grpc.NewServer()`, but `sdk.Serve` already does that internally and uses it inside `goplugin.Serve`. The plugin author cannot share the server unless `sdk.Serve` is refactored to expose it OR a new `Serve*` variant accepts pre-registered services.

2. PR 3 Task 9 also lists `internal/main.go` as the file to modify, but the actual DO plugin entrypoint is at `cmd/plugin/main.go` (verified: 13 lines, calls `sdk.Serve(internal.NewDOPlugin())`). Task 9's diff against "internal/main.go" wouldn't match the real file location. (This is a secondary path-correctness defect; the SDK-shape defect above is the load-bearing one.)

**Resolution required:** PR 2 must include a task that refactors `plugin/external/sdk/serve.go` (or adds a new entrypoint, e.g., `sdk.ServeWithServices(provider, registerExtras func(*grpc.Server) error)`) so that PR 3 can register typed services on the same gRPC server `sdk.Serve` already uses. Without this, PR 3 cannot ship.

---

### C-3 (NEW) — PR 4 Task 16 deletes the legacy wfctl-side proxy but does not address the engine-side `module/infra_module.go` bridge that ALSO consumes `IaCProvider` via the legacy in-process Go-interface dispatch path

**Evidence:**

`module/infra_module.go:11-12` documents itself as "bridges a YAML `infra.*` module declaration to an [interfaces.IaCProvider]'s [interfaces.ResourceDriver] for a single resource type." Line 83: `provider, ok := providerSvc.(interfaces.IaCProvider)`. This bridge runs INSIDE the workflow engine `server` binary (not wfctl), and it consumes `interfaces.IaCProvider` directly as a Go interface — there is no gRPC client involved when the plugin is loaded as an in-process module.

The plan claims (Phase 0 and Scope sections) that the migration is "wfctl-side wire format only" and the engine's in-process module loading isn't affected. But `module/infra_module.go` is NOT a wfctl-only path — it's the YAML `infra.*` module wiring used by the workflow engine `server` binary when an operator runs a YAML config that uses `infra.spaces_key` (or any `infra.*` module).

If the plan deletes `interfaces.IaCProvider` (or even the `ServiceInvoker`-based remote loader path that `infra_module.go` depends on through `providerSvc`), engine-side YAML configs that use `infra.*` modules break. The plan does not enumerate this.

If the plan does NOT delete `interfaces.IaCProvider` (because engine-side consumers keep it), then C-1 above stands: wfctl-side has a wrapper-or-refactor problem.

**Why this is Critical:**

The design's stated scope (line 50) claims "Application consumers ... NONE of these import `interfaces.IaCProvider` directly. All use IaC via `wfctl` CLI subprocess. Their migration is a wfctl version pin bump only." This is FALSE for the workflow engine itself — `module/infra_module.go` imports `interfaces.IaCProvider` directly, and the workflow engine is the host runtime that operators ship as `server`. Plugin authors who use `infra.*` modules from a YAML config rely on the engine-internal interface dispatch path, NOT wfctl.

The plan owes either:
- A task to migrate `module/infra_module.go` to use `pb.IaCProviderRequiredClient` (which then drags in `platform/differ.go` and the rest of the C-1 list), or
- Explicit acknowledgement that engine-side YAML `infra.*` module loading remains on the legacy `ServiceInvoker` path (in which case the legacy code is NOT deleted, and the design's "atomic delete" claim is false).

The plan does not pick. Both interpretations leave a code path the plan doesn't address.

**Resolution required:** Add a task that explicitly handles `module/infra_module.go` and the `infra.*` module loading path. Either (a) keep the legacy Go-interface in place for in-process module loading and document the asymmetry (wfctl uses typed gRPC, engine uses Go interface) — this is a sustained two-mental-model state, not an "atomic cutover", or (b) include the engine-side migration in PR 4's scope.

---

## Important findings

### I-1 — PR 4 Task 16 estimates "rewrite ~600 lines deleted" without enumerating the Go file delete + retain-Go-source change set, and the cross-PR `infra_apply_v2_loader_test.go`-style tests aren't addressed

**Evidence:**

`grep -ln "remoteIaCProvider\|InvokeService" cmd/wfctl/*.go` finds these *_test.go files that exercise the legacy proxy:

- `cmd/wfctl/deploy_providers_remote_iac_test.go` (multiple test functions, e.g. lines 17-22, 829, 905, 990, 1046, 1063-1068)
- `cmd/wfctl/deploy_providers_remote_iac_compat_test.go`
- `cmd/wfctl/deploy_remote_provider_test.go`
- `cmd/wfctl/deploy_providers_dispatch_matrix_test.go`
- `cmd/wfctl/deploy_providers_remote_driver_test.go`
- `cmd/wfctl/deploy_providers_strict_bridge_coverage_test.go`
- `cmd/wfctl/infra_audit_keys.go` (uses `remoteIaCProvider`-style indirection)
- `cmd/wfctl/infra_strict_mode_test.go`

PR 4 Task 16 says "DELETE: legacy paths" and references `cmd/wfctl/deploy_providers_remote.go` (which doesn't exist as a separate file; the proxy lives in `deploy_providers.go`). It does NOT enumerate the 6+ test files above, nor does it commit to deleting them vs. converting them. The plan says "All existing tests continue to pass; legacy-targeting tests deleted alongside the legacy code they tested" — a one-line policy that doesn't survive the test-file count.

**Why Important:**

Mass deletion of test files conflates two scope decisions: (a) which tests are obsolete because the proxy is gone, and (b) which tests cover behavior the typed path also needs. Without enumerating, an executing implementer either deletes too much (loses regression coverage) or too little (compile errors). PR 4 is already large; this multiplies its review-cycle cost.

**Resolution required:** PR 4 Task 16 (or a new Task 16b) must list each `cmd/wfctl/deploy_providers_*_test.go` file and tag it as DELETE / CONVERT. The plan should also estimate the count of test functions inside `deploy_providers_remote_iac_test.go` (currently >1000 lines) that need to either be ported to typed-client tests or be discarded with rationale.

---

### I-2 — Task 7 pre-release tag `v1.0.0-rc1` strategy is sound for the release pipeline but the cross-plugin-build CI matrix does NOT include workflow-plugin-digitalocean

**Evidence:**

`/Users/jon/workspace/workflow/_worktrees/strict-contracts-force-cutover/.github/workflows/cross-plugin-build-test.yml` matrix (line 39): `plugin: [workflow-plugin-aws, workflow-plugin-gcp, workflow-plugin-azure]`. **DigitalOcean is NOT in the matrix.**

The plan's PR 2 Task 6 says it'll "Add CI matrix row" for the typed-IaC E2E test, and PR 4 Task 20 says "add DO plugin v1.0.0 to the matrix". Neither task specifies whether DO is added to `cross-plugin-build-test.yml` (compile-compat gate) or to a separate workflow. The design's §Cross-repo integration test (line 384) claims "Workflow's CI matrix `cross-plugin-build` ... gets a new entry: builds workflow PR-A against DO plugin PR-B head SHA. Catches wire incompatibility at workflow-CI time." That entry doesn't exist today.

The release pipeline (`release.yml:271`) DOES support pre-release tags via `prerelease: ${{ contains(env.TAG_NAME, '-') }}` — so the rc1 strategy works for publishing. Good.

But `goreleaser` itself isn't directly invoked in `release.yml`; the release uses `softprops/action-gh-release@v2`. `find -maxdepth 2 -name "*goreleaser*"` returns ZERO matches in workflow root. Plan Task 2 references `.goreleaser.d/grpc-versions.tpl` and `release.extra_files` (a goreleaser-specific concept). Workflow doesn't appear to use goreleaser. Task 2's "add goreleaser config" implies infrastructure that doesn't exist.

**Why Important:**

(a) PR 2 Task 6 says it adds a matrix row but doesn't specify which workflow file. (b) Task 2's "GoReleaser v2 publishes grpc-versions.txt" is partially false — workflow uses `action-gh-release`, not goreleaser. The grpc-versions.txt artifact mechanism needs to be wired into the existing `release.yml`'s asset upload step (`softprops/action-gh-release` accepts `files:` for assets), not into a non-existent goreleaser config. (c) Without DO in `cross-plugin-build-test.yml`, the plan's "cross-plugin-build catches incompat at workflow-CI time" promise is empty — the gate isn't watching DO.

**Resolution required:** Task 2 needs to update its diff to wire `grpc-versions.txt` into the existing `release.yml`'s asset-upload step (under `softprops/action-gh-release@v2 with: files:`), NOT into a goreleaser config. Tasks 6 and 20 need to explicitly add DO to `cross-plugin-build-test.yml`'s matrix list, and ideally also to `conformance-smoke.yml` since that already references workflow-plugin-digitalocean as a downstream dispatch target.

---

### I-3 — Task 1 (PR 1, "supersede 2026-04-26 design frontmatter") edits a plan file under `docs/plans/` that may be scope-locked

**Evidence:**

Workspace memory (`feedback_plan_files_lead_owned`): "Plan files in `docs/plans/` are lead-owned — scope-lock blocks subagent Write to plan paths; orchestrating conversation must author plans inline."

PR 1 Task 1 (lines 65-106) modifies `docs/plans/2026-04-26-strict-grpc-plugin-contracts-design.md` and `docs/plans/2026-04-26-strict-grpc-plugin-contracts.md`. If superpowers' scope-lock guard is active for the previous (still-live for Module/Step/Trigger work) plan, an implementer subagent invoked from the superpowers:executing-plans skill cannot write to those paths.

**Why Important:**

PR 1 is the prerequisite for PR 2, and the plan claims "PR 1 independent; can land any time before PR 2." If the scope-lock guard blocks the implementer's Write, PR 1 stalls. Worse, the plan author hasn't flagged this as a coordination concern — the lead conversation may need to do the frontmatter edit inline, not delegate to the implementer.

**Resolution required:** PR 1 Task 1 should add a note: "Lead conversation must author the frontmatter edit directly (NOT delegate to implementer subagent) per `feedback_plan_files_lead_owned`. Or: add an explicit scope-lock-override note to the implementer's prompt acknowledging the cross-plan edit." The plan should pick one approach, not leave the resolution implicit.

---

## Hidden-serial-dependency findings

### I-4 — PRs 5-9 are NOT actually parallel because Task 22 (state-file-compat gate) can fail and force `WFCTL_LEGACY_STATE_VERSION` consumers to stay pinned

**Evidence:**

Task 22 (line 1313): "If gate FAILS: `WFCTL_LEGACY_STATE_VERSION` files MUST stay pinned to v0.14.2 until state-file-compat shim lands as a separate workflow PR."

The plan groups Task 21 (pin bump) and Task 22 (compat gate verification) into PR 5. If Task 22 fails AT MERGE TIME of PR 5, PR 5 partially merges (the `WFCTL_VERSION` bump) and partially defers (the `WFCTL_LEGACY_STATE_VERSION` files). The follow-up "separate workflow PR" is not in the plan's PR list. PR 6 (BMW pin bump, Task 23) and PR 7-9 (other consumers) all might have analogous legacy-state-version semantics that the plan doesn't survey — Task 23's "Same pattern as core-dump Task 21" implies BMW also needs the two-variable model, but the plan hasn't done the BMW-side per-file audit.

**Why Important:**

The plan's "PRs 5-9 parallel after PR 4 merges" claim assumes PR 5 merges cleanly. If Task 22's gate fails (a known possibility per the plan's own escape hatch), PR 5's merge is conditional, follow-up workflow PR is undefined, and PRs 6-9 may inherit the same condition. Cascade-block risk that the plan doesn't sequence.

The plan should either (a) make Task 22 a PRECONDITION of PR 5 (run the gate locally + verify GREEN before opening PR 5; if RED, file the workflow shim PR FIRST), or (b) split PR 5 into two PRs (pin bump + compat verification), so a failed gate doesn't half-merge the bump.

**Resolution required:** Reorder Task 22 to run BEFORE PR 5's pin-bump commit, OR split PR 5 into PR 5a (compat verification, gate-only) + PR 5b (pin bump conditional on PR 5a green). Also: BMW per-file YAML survey (Task 23) is currently a one-line "Same pattern as core-dump"; the plan should at least call out that BMW MUST be surveyed for legacy-state-version semantics before Task 23 ships.

---

## Verification-class mismatch

(None at the Critical/Important threshold. All tasks that change runtime behavior have a TDD-pattern test step. Task 13 — DO v1.0.0 tag — has a runtime smoke step. Phase 2 pin-bump tasks have CI gates plus the state-file-compat gate. Task 9's `git rm internal/module_instance.go` is verified by build-passes, which is appropriate for a delete.)

---

## Over/under-decomposition

(Surfaced in C-1 above — PR 4 is correctly identified as large but the plan's task list is INCOMPLETE rather than wrong-sized. Tasks 8-11 in PR 3 are split TDD-fashion; that's intentional per superpowers:test-driven-development and not over-decomposition.)

---

## Plan-vs-design alignment

| Design claim | Plan implementation | Status |
|---|---|---|
| ADR 0026: "no hand-written wrapper" | PR 4 Task 16 implies no wrapper but doesn't address engine-side `interfaces.IaCProvider` consumers — implicit wrapper-or-broader-refactor missing | **DRIFT (C-1)** |
| `sdk.RegisterAllIaCProviderServices(grpcServer, provider)` is one line for plugin author | PR 3 Task 9 also requires `grpcServer := grpc.NewServer()` + `sdk.ServeWithServer` (latter doesn't exist) | **DRIFT (C-2)** |
| Single coordinated cutover, no mixed state in workflow main | If engine-side `module/infra_module.go` retains legacy path while wfctl uses typed gRPC, there's a sustained two-path state inside workflow main | **DRIFT (C-3)** |
| `grpc-versions.txt` published per release | Task 2 wires it into "goreleaser config" that doesn't exist; release uses `softprops/action-gh-release` | **DRIFT (I-2)** |
| cross-plugin-build matrix catches DO-PR-B regression | DO is NOT in the matrix today; Task 6/20 don't add it explicitly | **DRIFT (I-2)** |
| 2026-04-26 plan frontmatter updated | Task 1 may be blocked by scope-lock | **OPEN (I-3)** |

---

## Bug-class scan transcript

| Class | Found? | Note |
|---|---|---|
| **Unstated assumptions** | C-1, C-3 | Engine-side IaCProvider consumers + scope of `interfaces.IaCProvider` deletion are unstated. |
| **Repo-precedent conflicts** | I-2, I-3 | Workflow doesn't use goreleaser; scope-lock on plan files. |
| **YAGNI violations** | NONE blocking | Task 12 marked "skip" inside PR 3, moved to PR 4 — clean. |
| **Missing failure modes** | I-4 | PR 5 cascade-block risk if state-file-compat gate fails. |
| **Security / privacy** | NONE NEW | Typed proto fields preserved; no new attack surface. |
| **Rollback story** | INTACT (mostly) | Each runtime-affecting task has a Rollback note. PR 4 Task 16 rollback ("revert + emergency v0.99.x tag") is honest about cost. |
| **Simpler alternative not considered** | C-1 | Adapter-wrapper IS the simpler alternative the design rejected — but the plan implicitly needs one if Option B is taken honestly. |
| **User-intent drift** | NONE | Plan's "no compat shim" intent matches mandate within scope. |
| **Verification-class mismatch** | NONE blocking | TDD pattern uniform across runtime-affecting tasks. |
| **Hidden serial dependencies** | I-4 | PR 5 cascade. |
| **Missing rollback wiring** | NONE | Each runtime-affecting task lists a Rollback action. |
| **Over/under-decomposition** | C-1 surfaces under-decomposition (PR 4 missing tasks for engine-side consumers); not pure size issue. |

---

## Verdict reasoning

Three Critical findings (C-1 engine-side IaCProvider consumers; C-2 missing SDK API; C-3 module bridge path unaddressed) each block the plan from compiling-and-running as authored. Three Important findings (I-1 test-file delete enumeration; I-2 CI matrix + goreleaser-vs-action mismatch; I-3 scope-lock; I-4 PR 5 cascade) block the plan from achieving design goals.

C-1 and C-3 are the load-bearing findings — they either (a) reintroduce the wrapper layer cycle 1 Alternative C explicitly rejected, (b) explode PR 4's task count by ~2x, or (c) leave a sustained two-mental-model state (wfctl uses typed gRPC; engine uses Go interface). The plan currently picks none of these explicitly.

C-2 is straightforward to fix in PR 2 — add a Task that exposes the gRPC server handle from `sdk.Serve` (or a new `sdk.ServeWithServices` variant). Without this, PR 3 Task 9 cannot compile.

**Per skill rules** (PASS only with ZERO Critical + every Important resolved/escalated): plan has 3 Critical and 4 Important. **FAIL.** Plan needs revision and re-review.

---

## Escalation summary

Three architectural decisions the plan must make explicitly before re-review:

1. **C-1 / C-3 disposition:** Pick Option A (wrapper, contradicts ADR 0026), Option B (engine-wide refactor, expand PR 4), or Option C (sustained two-path state, document the asymmetry). Update ADR 0026 and the plan's PR 4 task list accordingly.

2. **C-2 disposition:** Add a PR 2 task that exposes the gRPC server for additional service registration. Pick a concrete API shape (`sdk.ServeWithServices(provider, register func(*grpc.Server) error)` is the obvious candidate). Update PR 3 Task 9's diff to use it.

3. **I-2 disposition:** Confirm whether workflow uses goreleaser or `softprops/action-gh-release`. Rewrite Task 2 to match. Add DO explicitly to `cross-plugin-build-test.yml`'s matrix in Task 6 (or Task 20).

Once these three are resolved, the remaining Important findings (I-1 test enumeration, I-3 scope-lock note, I-4 PR 5 cascade) are sentence-to-paragraph-level edits.

Recommend: revise plan to v3 + cycle 2 plan-phase adversarial review.
