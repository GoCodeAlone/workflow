---
status: approved
review_cycle: 2
target: docs/plans/2026-05-10-strict-contracts-force-cutover-design.md
target_commit: bb369444
date: 2026-05-10
verdict: FAIL
---

# Adversarial Review Report — Strict-Contracts Force-Cutover Design (Cycle 2)

**Phase:** design (cycle 2)
**Artifact:** `docs/plans/2026-05-10-strict-contracts-force-cutover-design.md` (commit `bb369444`)
**Status:** **FAIL** — 1 Critical, 4 Important, 3 Minor. Cycle 1's Critical/Important findings are resolved cleanly; rev2 introduces one new Critical (consumer pin-bump scope) plus a chain of new Importants around atomic-merge feasibility, the typed `NotSupported` escape hatch, and user-mandate scope-narrowing.

This is cycle 2 of a 2-cycle skill cap. Per protocol the FAIL escalates to user (see "Escalation summary" at the end).

---

## Findings

### CRITICAL

#### C-1 (NEW). Phase 2 pin-bump scope misses hardcoded `version:` pins in consumer GitHub Actions workflow files

Rev2 §Phase 2 states (verbatim):
> `core-dump`: bump `WFCTL_VERSION` GH variable to `v1.0.0` + `.wfctl-lock.yaml` DO plugin pin to `v1.0.0`
> `buymywishlist`: same shape

Verified in workspace at `/Users/jon/workspace/core-dump/.github/workflows/`:

```
teardown.yml:41:          version: v0.14.2
image-launch-ci.yml:98:          version: v0.21.2
tc2-cutover.yml:154:          version: v0.21.2
bootstrap.yml:37:          version: v0.21.2
deploy.yml:36/70/95:          version: v0.14.2
drift-recovery.yml:72:          version: v0.21.2
registry-retention.yml:23:          version: v0.14.2
```

That is **7 different workflow files** with **9 hardcoded `version:` strings** spanning **two different wfctl pins** (`v0.14.2` and `v0.21.2`). None of these are controlled by a single `WFCTL_VERSION` GH variable or `.wfctl-lock.yaml` entry — they're literal YAML pins on the `setup-wfctl` action.

Memory entry `feedback_active_pr_monitoring` and the recent core-dump P0 wfctl bump session (`project_p0_core_dump_wfctl_bump_shipped`) explicitly called out that this drift exists across core-dump workflows AND that fixing them is a multi-file edit, not a single variable bump. Rev2's Phase 2 inherits cycle 1's mistaken framing of this as a one-line bump.

**Why this is Critical, not Minor:** Phase 2 describes Phase 2 as "1 day total, admin-merge same-day after CI passes." With 9 sites in core-dump alone (and unknown counts in BMW + workflow-cloud), Phase 2 is not a same-day pin bump. More importantly, if even ONE workflow file gets missed, that workflow runs old wfctl against new DO plugin v1.0.0 → silent failure mode (legacy wfctl can't load typed-only plugin → falls through to "plugin doesn't expose iac.provider module type" error path → operator confusion).

This bug class is the SAME class the cutover is trying to eliminate (silent runtime breakage from version-pin drift). Phase 2's framing perpetuates it.

**Fix recommendation:**
1. Phase 2 task breakdown must enumerate per-consumer all hardcoded version sites, not just `WFCTL_VERSION` + lockfile.
2. Add a CI gate (or pre-flight script) per consumer that scans `.github/workflows/*.yml` for `setup-wfctl` action invocations and asserts the `version:` matches a single canonical source-of-truth (env var, repo variable, or top-level workflow var).
3. Re-survey core-dump, BMW, workflow-cloud, ratchet, ratchet-cli, workflow-cloud-ui for the same pattern. Cycle 1 I-5's "doesn't import IaCProvider directly" check is necessary but not sufficient — you also need "doesn't pin wfctl version in CI YAML."
4. Acknowledge in §Phase 2 that some consumer repos may have 5-10 workflow files to update, not 1.

---

### IMPORTANT

#### I-1 (NEW). Atomic same-day merge of PR-A + PR-B is structurally impossible across two repos

Rev2 §Phase 1 + §Top doubts §3 describes the merge sequence:
> Two PRs prepared in parallel, held in draft until both are ready, merged same day
> Mitigation: PR-A workflow proto + SDK is shipped as a draft pre-release tag (e.g., `v1.0.0-rc1`) that DO PR-B depends on; iteration happens against the pre-release tag; only when DO PR-B is ready does workflow tag v1.0.0 final + same-day merge both.

This describes a real sequencing problem but doesn't actually solve it. The dependency graph is:

```
workflow PR-A: needs DO v1.0.0 in cross-plugin-build CI gate (per Acceptance Criterion §4)
DO PR-B: needs workflow v1.0.0 (or pre-release rc1) in go.mod
```

That's a hard cycle. Rev2's mitigation breaks it via `v1.0.0-rc1` workflow pre-release, but then:
1. Workflow merges PR-A (using DO PR-B's HEAD SHA in cross-plugin-build, NOT a tagged version) → tags `v1.0.0` final
2. Window opens: workflow `v1.0.0` is published; DO plugin `v1.0.0` is NOT yet tagged (PR-B is unmerged)
3. During this window, ANY operator running `wfctl plugin install workflow-plugin-digitalocean@latest` gets `v0.14.x` (no v1.0.0 exists yet) → that DO version uses legacy `InvokeService` dispatch → workflow `v1.0.0` rejects it at pre-flight gate (per F-1) → operator is locked out
4. Then DO PR-B merges → tags `v1.0.0` → window closes

The window may be minutes or hours but it's NOT zero. Rev2's "same-day coordinated merge" papers over the fact that two git histories can't merge atomically. Per F-1, workflow v1.0.0 will refuse to start a deploy if DO plugin v1.0.0 isn't yet pinned. So there IS a transitional window where workflow v1.0.0 is published but no usable DO plugin pairs with it.

**Why this matters:** the user mandate's "no compat windows" is interpreted by rev2 as "no soak window." But there's still an availability gap during the cutover. Rev2 doesn't acknowledge it.

**Fix recommendation:** Reverse the merge order. DO PR-B merges FIRST (against a workflow `v1.0.0-rc1` pre-release tag) and tags DO `v1.0.0` against `v1.0.0-rc1`. Workflow PR-A then merges and tags `v1.0.0` final, which is wire-compatible with DO `v1.0.0` already published. This collapses the availability window because by the time workflow `v1.0.0` is the latest stable, DO `v1.0.0` already exists in the registry.

Alternatively: explicitly document and accept the transitional window in §F-1; add to §Rollback that if the window exceeds N hours, recommended action is workflow-tag-rollback to v0.27.x.

#### I-2 (NEW). The typed `NotSupported bool` field IS a compile-time-allowed escape hatch — same bug class repackaged

Rev2 §Architecture solves cycle 1 I-1's `codes.Unimplemented` loophole by introducing `NotSupported bool` on every response message. The defense (lines 80-95) is that "every provider MUST implement every method; 'not supported' is an explicit typed-message decision."

Counter: a plugin author can write a fully compile-passing implementation that returns `&pb.EnumerateAllResponse{NotSupported: true}` for every method. The typed `NotSupported` field gives them a compile-time-allowed stub-everything escape hatch. This is structurally indistinguishable from cycle 1 I-1's `codes.Unimplemented` — except `NotSupported` is more dangerous because:
1. It compiles cleanly (vs. `codes.Unimplemented` which at least produces a runtime error visible in logs)
2. It's a conscious deliberate decision that READS as "I considered this and chose not to support it" — when in reality a lazy author can use it to ship a stub
3. wfctl's behavior when `NotSupported: true` is documented in §Data flow §5 as "continue to next provider OR error loud" — but "continue to next provider" IS the silent-failure path that the cycle 1 audit-keys/EnumerateAll bug exhibited

**Why Important not Critical:** the design at least makes the choice typed (capability is in the response, not in transport) and forces the author to write the line. That IS better than `codes.Unimplemented`. But it's not the structural eliminator the design claims.

**Fix recommendation:** Either:
- Define a contract test (or wftest helper) that asserts no provider returns `NotSupported: true` for a method declared as required-by-IaC-conformance-spec. Move the gate from "provider author judgment" to "spec assertion."
- OR split the `service IaCProvider` proto into `service IaCProviderRequired` (every method must return real data) and `service IaCProviderOptional` (every method may return NotSupported). Capability advertisement happens at service-registration time, not per-call. This makes "not implementing a required method" a service-registration-time compile/load failure.

#### I-3 (NEW). User mandate vs. scope reduction — "1 of 5 plugins migrated" is not "force switch with no compat"

User mandate (verbatim, top of rev2):
> "We need to force switch with no backwards compatibility/fallback modes."

Memory entry `feedback_force_strict_contracts_no_compat`:
> "the bug class persisted ... Old workflow tags ... become permanently incompatible with new plugin tags"

Rev2 §Out of scope explicitly defers AWS, GCP, Azure, Tofu plugins, and §Goal narrows from 4 bug classes to 2.

The cycle 1 review flagged the 4-of-5-plugins data point as factual (those four don't currently expose remote IaC dispatch). Rev2's response is "they're out of scope." But the user mandate didn't say "the IaC interfaces that DO is currently using." It said "force switch with no backwards compatibility." The 4 deferred plugins eventually need IaC dispatch (per the IaC conformance project memories `project_iac_conformance_plan` and `project_iac_state_truth_tc2_closeout` — TC2 cascade depends on multi-cloud).

If those 4 plugins ship `pb.IaCProviderServer` net-new at any future point, they will be subject to the SAME cutover discipline rev2 applies to DO. But rev2 has no mechanism to enforce this — there's no spec-level invariant saying "any future IaC plugin MUST implement `pb.IaCProviderServer` before its first ship." A future AWS plugin author could add a `module_instance.go`-style switch dispatcher and re-introduce the bug class because the old `InvokeService` machinery is being deleted but no compile-time gate prevents reinventing it locally.

Wait — that last point is wrong. Rev2 §Removed surface deletes `ServiceInvoker`, `InvokeService` RPC, etc. from workflow. So a future AWS plugin author CAN'T reinvent it on workflow's side. They'd have to fork. So this concern is structurally addressed at the SDK-deletion layer.

But the scope narrowing is still a user-intent gap: rev2 ships 1 plugin migrated. If "force switch" means "the switch is complete," then this design is Phase 1 of N where N is "1 plugin done." User may want clarification: is "force-cutover for 1 plugin then leave the other 4 as net-new whenever" the right interpretation?

**Fix recommendation:** Add to §Top doubts §4 (new): "User mandate is scope-narrowed to DO-only Phase 1 because the other 4 plugins don't currently use the legacy surface. If user wants 'all 5 IaC plugins exposing typed gRPC server' as the mandate, that's a multi-month effort (4 net-new gRPC server implementations) outside this design's scope. Confirm with user before proceeding."

This is a "user clarification needed" escalation, not a fixable-inline issue.

#### I-4 (NEW). F-4 (grpc-go drift gate) doesn't specify the cross-repo enforcement mechanism

Rev2 §F-4:
> workflow declares `tools.go` with explicit `protoc-gen-go` + `protoc-gen-go-grpc` version pins (committed via PR-A)
> Plugin repos must use `replace` directives in `go.mod` or explicit pins matching workflow's choice
> New CI gate in workflow + DO plugin: assert `go.sum` for `google.golang.org/grpc` matches workflow's pinned version. Implemented as `go.sum` grep gate in CI YAML.

Three gaps:

1. **"matches workflow's pinned version" — how does the DO plugin's CI know workflow's pinned version?** Without a cross-repo source of truth, the gate hardcodes a version string in DO's CI YAML that drifts the moment workflow bumps grpc-go. Rev2 doesn't specify the sync mechanism (e.g., DO CI fetches workflow's `tools.go` from a tagged commit and greps the version, OR a shared config file in workflow-registry).

2. **Plugin repos are plural; the design names only DO.** When AWS/GCP/Azure/Tofu eventually implement `pb.IaCProviderServer`, they each need their own CI gate. No template/helper is provided.

3. **"`go.sum` grep gate" misses indirect dependencies.** If a plugin's `go.mod` has `google.golang.org/grpc v1.65.0` but a transitive dep upgrades to `v1.66.0` via `go mod tidy`, `go.sum` will contain BOTH entries. A naive grep passes; the actual loaded version (per `go list -m google.golang.org/grpc`) might be `v1.66.0`. The gate should be `go list -m -json` based, not grep.

**Fix recommendation:** Specify the cross-repo sync mechanism (e.g., `setup-wfctl` action exposes `wfctl env grpc-version` that returns the canonical pin; plugin CIs invoke it). Specify the gate uses `go list -m` not grep. Document that future plugin repos must onboard the gate as part of their `pb.IaCProviderServer` adoption PR.

---

### MINOR

#### M-1 (NEW). DO plugin `internal/dispatcher_coverage_test.go` deletion removes a category of safety the typed model doesn't replicate

Rev2 §PR-B says:
> DELETE `internal/dispatcher_coverage_test.go` (the v0.14.2 reflection-based test added to catch the bug class — now redundant since Go compiler enforces it)

The Go compiler enforces "every method on `pb.IaCProviderServer` interface has a method on `*DOProvider`" via interface satisfaction. But the original test was added (per session memory) to catch a different bug: "did the plugin author add a method to `DOProvider` (e.g., `EnumerateAll`) but forget to register it as a switch case in `module_instance.go`?"

In the typed model, if the plugin author adds a Go method to `*DOProvider` that is NOT in `pb.IaCProviderServer`, it's just dead code. No bug. So the analogy holds — kind of. But the inverse direction is also relevant: if the plugin author adds a method to `pb.IaCProviderServer` (in workflow proto) but `*DOProvider` doesn't implement it, the DO plugin compile fails (per Acceptance Criterion §3). So the typed model DOES catch the safety the test was guarding.

Verdict: rev2 is correct that the test is redundant. But the rationale phrase ("redundant — Go compiler enforces interface satisfaction") is incomplete; the actual reason is that the directionality of the bug class flips with typing (from "method in struct, not in dispatcher" to "method in interface, not in struct" — the latter is compile-checked, the former becomes harmless dead code).

**Fix recommendation:** Update §PR-B to say: "DELETE `internal/dispatcher_coverage_test.go` (the v0.14.2 reflection-based test was guarding 'method exists in struct, missing from switch dispatcher' — that bug class becomes 'harmless dead code' under the typed model, since methods unreferenced by `pb.IaCProviderServer` are never called. The inverse case — 'method in pb.IaCProviderServer, missing from struct' — fails compile via interface satisfaction)."

#### M-2 (NEW). Rev2's pre-flight gate in F-1 reads as a soft-fallback in some readings

§F-1 says "Workflow MUST refuse to start a deploy if any pinned IaC plugin isn't typed-capable" — this is a hard fail (good). But the next sentence: "Pre-flight gate in `wfctl deploy` checks plugin-handle's gRPC server descriptor for `IaCProvider` service registration. If absent → fail loud with actionable error."

Two readings possible:
1. "fail loud" = abort deploy immediately, return non-zero exit code (correct interpretation)
2. "fail loud" = log a warning and continue without IaC steps (silent skip — bug class)

Reading 1 is intended; reading 2 is plausible to a careless implementer. The §Error handling section doesn't disambiguate.

**Fix recommendation:** Add to §F-1: "Pre-flight gate returns exit code 2 and emits the error to stderr; deploy MUST NOT proceed past pre-flight under any flag. There is no `--allow-legacy-plugins` escape." Make the absence of an escape flag explicit.

#### M-3 (NEW). `interfaces.ErrProviderMethodUnimplemented` deletion has no cross-repo impact analysis

Rev2 §Error handling:
> Error sentinel `interfaces.ErrProviderMethodUnimplemented` is deleted alongside the legacy surface; replaced by the typed field.

Verified: this sentinel exists in `interfaces/iac_*.go`. Deletion is a workflow-side change. But:
- Are there any external (non-workflow) consumers that import `interfaces.ErrProviderMethodUnimplemented` and use it in their own error handling?
- §Phase 2 says workflow-cloud / ratchet / etc. don't import `IaCProvider`, but does that survey extend to importing the error sentinel separately?
- Memory entry: this session's audit-keys/cleanup/prune in v0.27.1 used `errors.Is(err, ErrProviderMethodUnimplemented)`. After cutover, that pattern compiles to a removed symbol. wfctl-side code change is in PR-A, but if any downstream consumer of `interfaces` imports it for their own use, they break.

**Fix recommendation:** Add to §I-5 survey: "`grep -rln 'ErrProviderMethodUnimplemented' /Users/jon/workspace/{workflow-cloud,ratchet,ratchet-cli,workflow-cloud-ui,core-dump,buymywishlist}` to confirm no external consumers reference the sentinel directly."

---

## Cycle 1 finding-resolution verification

| Cycle 1 finding | Rev2 claim | Verdict | Notes |
|---|---|---|---|
| **C-1** Phase 2 surface mis-stated | Scope reduced to workflow + DO only; AWS/GCP/Azure/Tofu out of scope | **PASS (resolution accepted)** | Verified: AWS/GCP/Azure/Tofu lack `module_instance.go` (workspace check confirms). Acknowledged as out-of-scope correctly. But triggers new I-3 (user-mandate scope). |
| **C-2** Build-tag = compat shim | Build tag eliminated. Single coordinated PR-A + PR-B merge | **PASS** | Verified: rev2 mentions "build tag" only in §Adversarial review checklist as a thing to forbid (line 262) and in §Review-cycle-1 finding resolution table. No conditional compilation in design. |
| **C-3** Phase 3 missing failure modes | F-1 through F-4 sections added | **PASS-with-caveats** | F-1 through F-4 are substantive (lines 207-241). But F-1 has the "fail loud" ambiguity (M-2). I-4 surfaces gaps in F-4. F-3 (state-file invariance) has only a roundtrip test, no schema-versioning. |
| **I-1** codes.Unimplemented loophole | Replaced by typed `NotSupported bool` field | **PARTIAL (new I-2)** | `codes.Unimplemented` mentioned only as forbidden (lines 95, 264). Typed `NotSupported` is present (lines 90-95, 195). But the typed field is its own escape hatch (new I-2). |
| **I-2** Bug-class coverage overclaimed | Goal narrowed to 2 of 4 bug classes | **PASS** | Lines 22-34 explicitly enumerate "two specific bug classes" + out-of-scope flag-bugs + internal-logic-bugs. Strong. |
| **I-3** Soak window contradicts mandate | Soak dropped entirely | **PASS** | No mention of "soak", "grace period", or "transition window" in rev2. Verified via grep. |
| **I-4** Timeline unrealistic | Timeline 1-2 weeks for DO-only | **PASS-with-caveats** | Rev2 line 131 says "1-2 weeks elapsed" for Phase 1; reasonable for DO-only single-team. But Phase 2 (line 164) says "~1 day total" which the new C-1 contradicts (multi-file YAML pin updates per consumer). |
| **I-5** Application consumer survey | "Verified... none import IaC interfaces directly" | **VERIFIED via workspace grep** | Confirmed empty result for `interfaces.IaCProvider\|interfaces.ResourceDriver` across workflow-cloud/ratchet/ratchet-cli/workflow-cloud-ui. But survey missed CI-YAML version pins (new C-1). |
| **I-6** protoc/grpc-go drift | F-4 section added with tools.go + replace-directive + CI grep gate | **PARTIAL** | F-4 exists (lines 233-241). But mechanism gaps surface as new I-4. |
| **M-1** Acceptance criteria structural | Replaced with structural removal criteria | **PASS** | Lines 305-308 enumerate type-definition removal (`ServiceInvoker`, `ServiceContextInvoker`, etc.), file deletions, RPC method removals. Specific. |
| **M-2** Two-way supersession | Phase 0 added: update 2026-04-26 design + plan frontmatter | **PASS** | Phase 0 (lines 124-129) explicitly updates 2026-04-26 design's frontmatter with `superseded_by` + `status: superseded_partial`. |
| **M-3** ADR-1 wording | Narrowed to "for those interfaces only" | **PASS** | Line 120 + lines 325 use "for those interfaces only" wording. |
| **M-4** Self-challenge §3 not challenging | Escalated as open question | **PASS** | Line 295 escalates as "potential follow-up plan; NOT part of this design's scope." Acknowledges genuine answer. |
| **Alt A** single-PR force-cutover | Adopted | **PASS** | Phase 1 §PR-A + §PR-B is the single-coordinated-merge model. |
| **Alt B** add to existing service Plugin | Rejected | **PASS** | Line 356 explicitly rejects with reasoning. |
| **Alt C** eliminate iacProviderClient wrapper | Adopted | **PASS** | Line 74 + line 327 ADR-3 confirm direct `pb.IaCProviderClient` use. |

**Summary:** All 13 cycle 1 findings have non-trivial resolutions in rev2. 9 are clean PASS, 4 are PASS-with-caveats that surface as new findings (I-1 → new I-2 escape hatch; I-4 → new C-1 + I-4; I-5 → new C-1; I-6 → new I-4).

---

## Bug-class scan transcript

| Class | Found? | Note |
|---|---|---|
| **Unstated assumptions** | YES | C-1 (Phase 2 framing assumes pin-bump is single-variable per repo; verified false for core-dump 9 sites). I-1 (atomic merge across two git histories assumed). |
| **Repo-precedent conflicts** | NONE NEW | Rev2 honors `feedback_force_strict_contracts_no_compat` (no soak), `feedback_per_agent_worktree_per_task_pr` (PR-A + PR-B as separate PRs). Fixed M-2 from cycle 1 (two-way supersession). |
| **YAGNI violations** | PARTIAL | `NotSupported bool` field is borderline — it's a structural improvement over `codes.Unimplemented` but adds a deliberate stub-everything escape hatch (I-2). Whether it's needed at all depends on multi-provider semantics from v0.27.1 which design cites but doesn't quote. |
| **Missing failure modes** | YES | I-1 (transitional window between workflow v1.0.0 tag and DO v1.0.0 tag); M-3 (`ErrProviderMethodUnimplemented` deletion in workflow may break external consumers if any). |
| **Security / privacy** | NONE NEW | Cycle 1's note on plugin auth and gRPC metadata stands; rev2 doesn't expand this surface. Typed proto closes secret-leak risk via structured fields (good). |
| **Rollback story** | PARTIAL | §Rollback (lines 282-291) is honest about the cost ("hard-to-reverse step is workflow PR-A merge"). But doesn't address the I-1 transitional window's rollback path. |
| **Simpler alternative not considered** | YES | See "Options the author may not have considered" below. |
| **User-intent drift** | YES | I-3 — "force switch all of workflow* ecosystem" → rev2 ships 1 plugin migrated. May or may not match user intent; needs clarification. |

---

## Options the author may not have considered

### Option 1 — Reverse the merge order (resolves I-1)

Instead of "workflow PR-A merges, then DO PR-B merges same-day," do:

1. Workflow tags `v1.0.0-rc1` (proto + SDK only, on a release branch, NOT main).
2. DO PR-B merges first against `v1.0.0-rc1`, tags DO `v1.0.0`.
3. Workflow PR-A merges to main, tags workflow `v1.0.0` final (which is wire-compatible with DO `v1.0.0`).

This eliminates the I-1 transitional window because by the time `wfctl install workflow-plugin-digitalocean@latest` returns `v1.0.0`, workflow `v1.0.0` already exists.

Rev2's mitigation in §Top doubts §3 already gestures at this with "pre-release tag" but doesn't reverse the order — workflow still merges first in the description.

### Option 2 — Defer the `NotSupported bool` field by splitting the proto into required + optional services (resolves I-2)

```proto
service IaCProviderRequired {
  rpc Plan(...) returns (...);
  rpc Apply(...) returns (...);
  // every RPC here MUST return real data
}

service IaCProviderOptional {
  rpc EnumerateAll(...) returns (EnumerateAllResponse);
  rpc DetectDrift(...) returns (DetectDriftResponse);
  // each response message has NotSupported bool
}
```

Plugin authors register one or both services. wfctl checks at handle-open time which services are registered. Capability advertisement is service-registration-time (compile-time decision), not per-call-time. Lazy "stub everything" requires explicit non-registration of `IaCProviderOptional`, which is a deliberate documentable choice rather than a buried boolean.

### Option 3 — Add a workflow-side spec invariant requiring `pb.IaCProviderServer` for any IaC plugin (resolves the I-3 future-proofing concern)

Add a §V (invariant) clause to a workflow SPEC: "Any plugin manifest declaring `interfaces.iac` capability MUST ship `pb.IaCProviderServer` registration. Loaded by workflow plugin loader as a hard fail at handle-open time if missing." This is a forward-looking constraint that locks in the cutover for AWS/GCP/Azure/Tofu when they eventually ship IaC.

---

## Verdict reasoning

Rev2 is a substantial improvement over rev1 — every cycle 1 Critical and Important has a non-trivial resolution. The author engaged the review honestly: the `NotSupported` field is a real design choice (not just a rename), Phase 0 + the Phase 2 scope reduction reflect genuine survey work, and §F-1 through §F-4 are substantive failure-mode treatments.

But three new issues block PASS:

1. **C-1** is a fresh Critical: rev2 inherited cycle 1's mis-framing of consumer pin-bumps as single-variable changes; workspace evidence (9 hardcoded version pins in 7 core-dump workflow files) refutes it. This is the same bug class the cutover targets, perpetuated in the cutover plan itself.

2. **I-1** + **I-2** + **I-4** are new Importants that emerge specifically from rev2's design choices (atomic merge feasibility, typed escape hatch, cross-repo CI gate mechanism). They're addressable with the fixes above but need rev3 to land them.

3. **I-3** is a user-intent question that needs explicit confirmation before proceeding. Per skill rules, surface to user.

This is cycle 2 of a 2-cycle skill cap. **Per protocol, escalate to user with unresolved findings.**

---

## Escalation summary (per skill rules: 2-cycle max, FAIL → user)

The design is substantively sound but has 1 Critical and 4 Important issues that rev2 doesn't address. Recommended user actions:

1. **Confirm scope** (I-3): "Force-cutover for DO-only Phase 1, with AWS/GCP/Azure/Tofu deferred indefinitely as out-of-scope" — is that the intended user mandate, or did you mean "force-cutover for all 5 IaC plugins concurrently"?

2. **Accept fix-forward for C-1**: the consumer pin-bump scope must enumerate every `version:` site per consumer repo, not just `WFCTL_VERSION` + lockfile. Add this to Phase 2 task breakdown.

3. **Choose merge order** (I-1): reverse to "DO PR-B first against `v1.0.0-rc1`, then workflow PR-A" to eliminate the transitional window. OR explicitly accept the window with rollback documentation.

4. **Strengthen `NotSupported` semantics** (I-2): adopt Option 2 (split proto into Required + Optional services) OR add a contract test that asserts no provider returns `NotSupported: true` for spec-declared-required methods.

5. **Specify F-4 mechanism** (I-4): how does DO plugin's CI know workflow's pinned grpc-go version? Cross-repo source-of-truth + `go list -m` based gate (not grep).

If user accepts these as fix-forward (i.e., reflected in the implementation plan, not blocking design approval), this cycle 2 review can be re-classified as "PASS with documented follow-ups." Otherwise, rev3 is required.
