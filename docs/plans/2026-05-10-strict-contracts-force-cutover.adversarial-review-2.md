---
status: approved
review_cycle: 2
target: docs/plans/2026-05-10-strict-contracts-force-cutover.md
target_commit: 472bda61
phase: plan
date: 2026-05-10
verdict: FAIL
---

# Adversarial Review — Strict-Contracts Force-Cutover Plan (Cycle 2, plan-phase)

**Phase:** plan (cycle 2)
**Artifact:** `docs/plans/2026-05-10-strict-contracts-force-cutover.md` (commit `472bda61`)
**Cycle 1 baseline:** `docs/plans/2026-05-10-strict-contracts-force-cutover.adversarial-review-1.md` (commit `6834ac1b`) — verdict FAIL with 3 Critical + 4 Important.
**Design baseline:** `docs/plans/2026-05-10-strict-contracts-force-cutover-design.md` (rev5).

**Verdict: FAIL.** Two Critical and three Important findings. Per "don't nitpick" — every finding below blocks the plan from working or from achieving design goals.

---

## Cycle 1 finding-resolution verification

| Cycle 1 finding | Rev2 disposition | Verified? |
|---|---|---|
| **C-1** Engine-side `interfaces.IaCProvider` consumers ignored | Task 15-bis adds typed-client → `interfaces.IaCProvider` adapter; engine consumers unchanged. Plan's Goal/Scope sections explicitly narrow to "gRPC bridge cutover only" and out-of-scope-list adds `module/infra_module.go`, `iac/wfctlhelpers/apply.go`, `platform/differ.go`, `iac/refreshoutputs/refresh.go`, `plugin/sdk/iaclint/iaclint.go`. | YES (re-introduces wrapper but plan owns it; see C-1-NEW for the consequences) |
| **C-2** `sdk.RegisterAllIaCProviderServices` + `ServeWithServer` did not exist in PR 2 | Task 4-bis adds `sdk.ServeIaCPlugin(provider, opts)` high-level API; PR 3 Task 9 diff updated to use it. | PARTIAL — see C-2-NEW: Task 4-bis is a stub, not a concrete spec. |
| **C-3** Engine-side type-assert in `module/infra_module.go` ignored | Subsumed by C-1 disposition (same file list, same adapter resolution). | YES (consistent with C-1 resolution) |
| **I-1** Test-file DELETE-vs-CONVERT enumeration missing | Task 16 lines 1138-1147 enumerate 6 specific test files with per-file DELETE/CONVERT/KEEP tags. | YES |
| **I-2** `cross-plugin-build` matrix + goreleaser-vs-action mismatch | Task 2 references `softprops/action-gh-release@v2` (correct); Task 6 says "add new matrix row"; Task 20 explicitly adds DO. | PARTIAL — see I-2-NEW: matrix row content still under-specified. |
| **I-3** Scope-lock blocks edit of 2026-04-26 plan | Task 1 creates `SUPERSEDED-NOTICE.md` instead of editing the plan. Edits only the design frontmatter. | YES (the workaround is structurally appropriate) |
| **I-4** PR 5 cascade-block on state-file-compat | Task 20-bis pre-flight in PR 4 adds the gate BEFORE PR 5 merges. | NO — see C-1-NEW (Task 20-bis assumes a non-existent docker image) |

---

## Critical findings

### C-1-NEW — Task 20-bis state-file-compat fixture step uses `ghcr.io/gocodealone/wfctl:v0.14.2` which does not exist as a published container image

**Evidence:**

`/Users/jon/workspace/workflow/_worktrees/strict-contracts-force-cutover/.github/workflows/release.yml:152` shows `ko build ./cmd/server` — the release pipeline publishes ONLY `cmd/server` as a container image to ghcr.io. The `cmd/wfctl` build (lines 175-178) goes through `actions/upload-artifact` and `softprops/action-gh-release@v2 with: files: dist/*` only. There is NO `ko build ./cmd/wfctl` step. wfctl ships as a binary asset attached to GitHub Releases, not as an OCI image.

Task 20-bis (line 1289) and Task 22 (line 1406-1407) both encode:

```bash
docker run --rm -v $(pwd):/work ghcr.io/gocodealone/wfctl:v0.14.2 \
  infra plan -c /work/example/iac-do/infra.yaml --env staging --output /work/test/fixtures/state-v0.14.2.json
```

This will fail with `manifest unknown` at `docker run`. The whole PR 5 cascade-block prevention strategy depends on Task 20-bis producing a v0.14.2-shaped fixture; if it can't, the gate is unrunnable as authored.

**Why Critical:**

Task 20-bis is THE mechanism the rev2 plan uses to address cycle 1 I-4. If it doesn't run, the cycle 1 I-4 finding ("PR 5 cascade-block risk") remains unresolved — operator may merge PR 4 + PR 5 without ever validating state-file-compat across the v0.14.2 → v1.0.0 boundary.

Three viable fixes:

1. **Use the binary release asset** — `curl -L https://github.com/GoCodeAlone/workflow/releases/download/v0.14.2/wfctl-v0.14.2-linux-amd64.tar.gz | tar -xz && ./wfctl infra plan ...`. Concrete, works, matches actual release artifact shape.
2. **Build v0.14.2 from source** — `git clone -b v0.14.2 ... && go build -o /tmp/wfctl-v0.14.2 ./cmd/wfctl && /tmp/wfctl-v0.14.2 infra plan ...`.
3. **Pre-bake the fixture once** — capture a real v0.14.2-produced state file from existing core-dump production state and check it in as a permanent test asset; remove the fixture-generation step entirely.

The plan must pick one. Currently it picks "docker run a non-existent image", which can't execute.

**Secondary defect in same step:** the `infra plan ... --output` flag and `infra state list --state-file ...` flag both need actual existence verification against the v0.14.2 wfctl CLI surface. `infra plan` writes plan output, not state; a plan output file is NOT semantically equivalent to a state file. State files are written by `apply`, not `plan`. The command shape `infra plan ... --output state.json` is unlikely to produce what Task 22 then reads as a state file. Even if Fix #1/#2/#3 above resolves the image, the COMMAND shape still won't capture a state file.

**Resolution required:**
- Replace docker-image step with one of the three viable approaches.
- Verify the actual command for capturing a v0.14.2 state file (likely `apply --dry-run` is wrong; needs to be a real apply against a fake/sandbox provider, OR an existing-state extraction).
- If a real state file can't be captured cheaply, drop Task 20-bis as written and instead check in a hand-curated `state-v0.14.2.json` fixture once + assert against its known schema.

---

### C-2-NEW — Task 4-bis is a 3-line stub that doesn't actually specify the `sdk.ServeIaCPlugin` API shape; PR 3 Task 9 references a `sdk.ServeOptions{}` parameter that has no definition

**Evidence:**

Plan Task 4-bis (lines 545-557):
```
**Step 1: Failing test** — write a stub plugin process; assert ServeIaCPlugin registers all services + responds to handshake.
**Step 2: Implement** — wraps existing sdk.Serve machinery + injects auto-registration before serve loop.
**Step 3: Tests + commit.**
```

That's it — three sentences. No diff, no API signature, no `ServeOptions` struct definition.

Plan Task 9 (line 929):
```diff
+    iacServer := internal.NewIaCServer(provider)
+    sdk.ServeIaCPlugin(iacServer, sdk.ServeOptions{...})
```

`sdk.ServeOptions{...}` is referenced but never defined. The cycle 1 C-2 finding said the plan needs to "Pick a concrete API shape (`sdk.ServeWithServices(provider, register func(*grpc.Server) error)` is the obvious candidate)." Rev2 invented a new name (`ServeIaCPlugin`) and a new parameter type (`ServeOptions`) but didn't specify either.

The existing `sdk.Serve` (verified in `plugin/external/sdk/serve.go:26`) takes a `PluginProvider` and internally calls `newGRPCServer(provider)` then hands the server to `goplugin.Serve` via the `servePlugin` adapter (line 50: `pb.RegisterPluginServiceServer(s, p.server)` is the ONLY service registration). To add typed IaC services, `ServeIaCPlugin` must:

- Either fork the `servePlugin.GRPCServer` callback to ALSO call `RegisterAllIaCProviderServices(s, iacServer)` after `RegisterPluginServiceServer`, OR
- Replace `sdk.Serve` entirely with a parallel path that constructs its own `goplugin.ServeConfig`.

Neither is "wraps existing sdk.Serve machinery + injects auto-registration before serve loop" — the existing sdk.Serve machinery doesn't have an "after-construct, before-serve" injection point. The `*grpc.Server` is created INSIDE goplugin.Serve via `goplugin.DefaultGRPCServer`, not in `sdk.Serve` itself.

**Why Critical:**

PR 3 Task 9 cannot compile because `sdk.ServeIaCPlugin` and `sdk.ServeOptions` aren't actually specified. An implementer following the plan literally would write `sdk.ServeIaCPlugin(iacServer, sdk.ServeOptions{...})`, then face an undefined-symbol compile error and have to invent the API shape themselves (the plan provides no guidance). Two parallel implementers (one on PR 2, one on PR 3) would invent different shapes, causing rework.

Additionally, the existing `PluginProvider` interface (verified `plugin/external/sdk/interfaces.go:13`) is the load-bearing contract for `sdk.Serve`. An IaC plugin entrypoint switching to `ServeIaCPlugin` either (a) drops `PluginProvider` and loses module/step/trigger/cli/hook capabilities the DO plugin already has, OR (b) `ServeIaCPlugin` must accept BOTH the IaC server AND the PluginProvider so the plugin can register both surfaces. The plan doesn't disambiguate.

**Resolution required:**

Task 4-bis must include:
1. The full Go signature: `func ServeIaCPlugin(provider PluginProvider, iacServer any, opts ServeOptions)` (or whatever the agreed shape is — but it must be CONCRETE).
2. The `ServeOptions` struct definition (what fields? `goplugin.HandshakeConfig` override? logger? broker config?).
3. The actual integration point with `goplugin.Serve` — either a new `servePlugin` variant that registers BOTH `pb.PluginServiceServer` AND the IaC services, or an explicit fork.
4. A note on whether `ServeIaCPlugin` deprecates `Serve` for IaC plugins or coexists.

PR 3 Task 9's diff must use the actual shape, not `ServeOptions{...}`.

---

## Important findings

### I-1-NEW — Task 6 / Task 20 add DO to `cross-plugin-build-test.yml` matrix but don't specify what build target the matrix runs against DO; the existing matrix only runs `go build ./...` against AWS/GCP/Azure with NO IaC-specific verification

**Evidence:**

`/Users/jon/workspace/workflow/_worktrees/strict-contracts-force-cutover/.github/workflows/cross-plugin-build-test.yml` lines 43-44 + 50-59:
```yaml
      matrix:
        plugin: [workflow-plugin-aws, workflow-plugin-gcp, workflow-plugin-azure]
      ...
          repository: GoCodeAlone/${{ matrix.plugin }}
          path: ${{ matrix.plugin }}
      ...
          cd ${{ matrix.plugin }}
```

Task 6 step 2 (line 697-703):
```yaml
      - name: Typed-IaC E2E test
        run: GOWORK=off go test -tags=integration ./plugin/external/sdk/... -run TestIaC_EndToEnd
```

This is a workflow-side test, NOT a workflow-against-DO-plugin compile/run check. The cycle 1 I-2 finding ("workflow's CI matrix `cross-plugin-build` ... gets a new entry: builds workflow PR-A against DO plugin PR-B head SHA") is NOT addressed by Task 6 — Task 6 just adds a workflow-only in-process test.

Task 20 step 2 (line 1235-1241) says "add DO plugin v1.0.0 to the matrix" but doesn't specify the matrix's verification step. Adding a `workflow-plugin-digitalocean` row to the existing matrix would just run the existing `cd workflow-plugin-digitalocean && go build ./...` — verifying DO compiles against workflow's main, NOT verifying the typed gRPC bridge actually exchanges typed messages.

The design rev5 `§Cross-repo integration test` (line 384) claims `cross-plugin-build` "Catches wire incompatibility at workflow-CI time" — but a `go build` doesn't catch wire incompatibility. Wire incompatibility surfaces at runtime when actual gRPC bytes flow between the two binaries.

**Why Important:**

The CI gate exists (matrix has rows) but isn't doing the work the design promises. PR 4 merges + tags v1.0.0 thinking the gate caught wire issues, but the gate only proved compile-time satisfaction. Wire-format drift (e.g., proto field renaming with same number, transitive grpc-go version skew) lands silently in v1.0.0.

**Resolution required:**

Task 6 (or a new sub-task) must specify:
- A new dedicated CI workflow OR a matrix step that runs DO plugin's binary as a subprocess of workflow's wfctl, calls a real typed RPC end-to-end, asserts the response.
- OR: the `cross-plugin-build-test.yml` matrix gets an `iac_e2e: true` flag for the DO row that triggers `wfctl infra audit-keys --plugin ./workflow-plugin-digitalocean/dist/wfctl-plugin-digitalocean` (or similar) so the typed bridge actually exchanges bytes.

Just `go build ./...` is necessary but not sufficient.

---

### I-2-NEW — Task 1's SUPERSEDED-NOTICE.md is structurally fine but downstream agents that walk `docs/plans/*.md` for canonical task lists will pick up BOTH the 2026-04-26 plan AND the SUPERSEDED-NOTICE.md; the notice doesn't modify the canonical plan's task list

**Evidence:**

Cycle 1 I-3 was about the scope-lock guard preventing edits to the locked plan. Rev2's Task 1 elegantly works around this: NEW notice file at `docs/plans/2026-04-26-strict-grpc-plugin-contracts.SUPERSEDED-NOTICE.md`. Good.

But the `superpowers:scope-lock` mechanism (memory `feedback_plan_files_lead_owned`) tracks plan files by exact path. A subagent or downstream automation that walks `docs/plans/*.md` to enumerate "active plans" or "live task lists" would still find the 2026-04-26 plan with its old Migration Tracker entries pointing at AWS/GCP/Azure/Tofu IaC migrations as live work. The SUPERSEDED-NOTICE is a sibling file, not a redirect, not a frontmatter override.

The plan's actual content is unchanged. Only the design frontmatter is updated (line 80-90 of Task 1). An LLM agent or a build script reading `docs/plans/2026-04-26-strict-grpc-plugin-contracts.md` directly will see no supersession marker.

**Why Important:**

Phase 2 in the 2026-04-26 plan still says "migrate AWS/GCP/Azure/Tofu IaC" as live work. Rev2 explicitly says these are out-of-scope for THIS cutover but doesn't pull them out of the OLDER tracker. A future agent given "execute the next task on docs/plans/2026-04-26-strict-grpc-plugin-contracts.md" will start working on AWS/GCP/Azure IaC strict-contracts migration that this design has explicitly deferred.

**Resolution required:**

Either:
1. Add a top-of-file comment block to `docs/plans/2026-04-26-strict-grpc-plugin-contracts.md` itself (a 5-line markdown comment is minimal scope; if scope-lock truly blocks ANY edit, do it via the lead conversation as Task 1 already correctly notes for the design file).
2. Or commit a `.supersession` sidecar file that downstream tools can grep for (`git ls-files | grep '\.supersession$'`) — but such tooling doesn't exist today.

The current SUPERSEDED-NOTICE.md is a soft supersession that humans may notice but automation will not.

---

### I-3-NEW — Task 21 two-variable model (`WFCTL_VERSION` + `WFCTL_LEGACY_STATE_VERSION`) is hard-coded as required, but the entire reason for the legacy variable IS state-file-compat, which Task 20-bis is supposed to verify clean. If Task 20-bis passes, the legacy variable becomes unnecessary. The plan doesn't make Task 21's two-variable split CONDITIONAL on Task 20-bis result.

**Evidence:**

Task 20-bis (line 1284-1306) is "state-file-compat verification PRE-flight". If it passes (v1.0.0 reads v0.14.2 state cleanly), then by definition the v0.14.2 wfctl binary is no longer needed for state-file-compat — v1.0.0 reads the same state files. The justification for keeping `teardown.yml`, `deploy.yml` rollback, `registry-retention.yml` on v0.14.2 (per `project_p0_core_dump_wfctl_bump_shipped`) was state-file-compat. Once verified, those files COULD also bump to v1.0.0.

But Task 21 (line 1322-1394) hardcodes the two-variable split: `WFCTL_LEGACY_STATE_VERSION = v0.14.2` for those 3 files, regardless of whether Task 20-bis passes.

**Why Important:**

If Task 20-bis passes, the second variable is dead weight — adds complexity to operators and to CI gates without justification. If Task 20-bis fails, only THEN is the second variable load-bearing. The plan's structure should be:

1. Task 20-bis runs FIRST.
2. If PASS: Task 21 uses the SINGLE variable model (`WFCTL_VERSION = v1.0.0` everywhere).
3. If FAIL: Task 21 uses the TWO-variable model (`WFCTL_LEGACY_STATE_VERSION = v0.14.2` for the 3 files; everything else `WFCTL_VERSION = v1.0.0`); track follow-up to ship the compat shim.

Currently Task 21 always does the two-variable split. The legacy variable becomes a permanent operator-facing complexity that may not be needed.

**Resolution required:**

Task 21 needs branching: "If Task 20-bis result is GREEN, use the single-variable rewrite (all 9 files → `WFCTL_VERSION`). If RED, use the two-variable rewrite as currently specified." The plan should be explicit that the variable count is OUTPUT of Task 20-bis, not INPUT.

Alternative: keep the two-variable model regardless and document that as a stable consumer-side abstraction (operators might want to roll back to legacy wfctl independently of state-file-compat reasons). This is a valid design choice but should be stated explicitly, not implicit.

---

## Verdict reasoning

Two Critical findings remain — both around cycle 1 finding-resolution work that LOOKS resolved on a skim but doesn't survive code-grade verification:

- **C-1-NEW** (Task 20-bis ghcr.io image doesn't exist) — invalidates the entire mechanism the rev2 plan added to address cycle 1 I-4. The state-file-compat pre-flight gate is unrunnable as authored.
- **C-2-NEW** (Task 4-bis is a 3-line stub) — re-introduces the same cycle 1 C-2 defect: PR 3 references SDK functions that PR 2 doesn't actually specify (it now references `ServeIaCPlugin` + `ServeOptions{}` instead of `RegisterAllIaCProviderServices` + `ServeWithServer`, but the substance is the same — a placeholder API).

Three Important findings — each blocks the plan from achieving its design goals:

- **I-1-NEW** (cross-plugin-build matrix doesn't actually verify wire compat) — the gate the design promises catches wire incompat doesn't catch wire incompat.
- **I-2-NEW** (SUPERSEDED-NOTICE.md doesn't actually mark the canonical plan superseded for automation) — risks downstream agents picking up the deferred AWS/GCP/Azure/Tofu work.
- **I-3-NEW** (two-variable consumer model hardcoded regardless of Task 20-bis result) — adds operator complexity that may not be needed.

All five findings have concrete resolution paths described above. None require architectural rework — they are spec-tightening of existing plan structure.

**Per skill rules** (PASS only with ZERO Critical + every Important resolved/escalated): plan has 2 Critical and 3 Important. **FAIL.** Plan needs revision and re-review.

---

## Bug-class scan transcript

| Class | Found? | Note |
|---|---|---|
| **Unstated assumptions** | C-1-NEW, C-2-NEW | wfctl ghcr image existence; ServeIaCPlugin API shape |
| **Repo-precedent conflicts** | C-1-NEW | release.yml only ko-builds cmd/server, not cmd/wfctl |
| **YAGNI violations** | I-3-NEW | Two-variable consumer model may be unnecessary |
| **Missing failure modes** | C-1-NEW | If Task 20-bis can't run, what happens? Plan has no fallback |
| **Security / privacy** | NONE | No new attack surface |
| **Rollback story** | INTACT | Each runtime-affecting task has a Rollback note |
| **Simpler alternative not considered** | I-2-NEW | Simple top-of-file edit on the locked plan would clear automation risk; SUPERSEDED-NOTICE adds complexity without solving it for automation |
| **User-intent drift** | NONE | "No compat shim" intent honored within scope |
| **Verification-class mismatch** | I-1-NEW | cross-plugin-build claims wire-incompat catching but only verifies compile |
| **Hidden serial dependencies** | I-3-NEW | Task 21 hardcodes Task 20-bis output assumption |
| **Missing rollback wiring** | NONE | Each task documents rollback |
| **Over/under-decomposition** | NONE blocking | PR 4 is large but per cycle 1 reasoning that's intentional (atomic cutover) |
| **Adapter ~300 LOC realistic?** | YES (verified) | `remoteIaCProvider` proxy is ~600 lines / 31 methods; typed-call adapter is roughly half that since each method becomes a 5-10 line typed RPC dispatch with no map[string]any marshalling. Plan estimate plausible. |

---

## Plan-vs-design alignment

| Design claim | Plan implementation | Status |
|---|---|---|
| State-file format invariant (F-3) | Task 20-bis verifies via docker run of v0.14.2 wfctl image | **DRIFT (C-1-NEW)** — image doesn't exist |
| `sdk.RegisterAllIaCProviderServices` is one-line for plugin author | Task 4 + Task 4-bis claim it; Task 4-bis is unspecified stub | **DRIFT (C-2-NEW)** |
| cross-plugin-build catches DO wire incompat at workflow-CI time | Task 6 + Task 20 add DO to matrix; matrix runs `go build`, not wire test | **DRIFT (I-1-NEW)** |
| Adapter NOT a re-marshalling wrapper (ADR-0026) | Task 15-bis adapter wraps typed pb client + dispatches typed calls; no map[string]any | **HONORED** (verified — adapter design is structurally distinct from the rejected proxy) |
| Engine consumers see no API change | Task 16 returns `interfaces.IaCProvider` (the adapter); engine consumers unchanged | **HONORED** |
| 2026-04-26 plan superseded for IaC scope | SUPERSEDED-NOTICE.md sibling file; plan content unchanged | **PARTIAL (I-2-NEW)** — humans see it, automation may not |

---

## Escalation summary

Two architectural decisions to make explicitly before re-review:

1. **C-1-NEW disposition:** Pick one of three viable v0.14.2-fixture-capture mechanisms (binary-asset download, build-from-source, pre-baked fixture). Verify the actual command shape produces a state file (not a plan file). Update Task 20-bis + Task 22 accordingly.

2. **C-2-NEW disposition:** Specify the full `ServeIaCPlugin` Go signature + `ServeOptions` struct + `goplugin.Serve` integration point in Task 4-bis. Update Task 9's diff to match. Confirm whether IaC plugins keep the `PluginProvider` capability surface or trade it for IaC-only.

Three Important findings have sentence-to-paragraph-level resolutions above (I-1-NEW: specify the actual wire-test step; I-2-NEW: add a top-of-file comment OR explicitly own the soft-supersession scope; I-3-NEW: branch Task 21 on Task 20-bis output OR justify the permanent two-variable model).

Recommend: revise plan to v3 + cycle 3 plan-phase adversarial review.
