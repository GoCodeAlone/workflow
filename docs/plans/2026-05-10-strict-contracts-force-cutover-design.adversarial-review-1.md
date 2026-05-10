---
status: approved
review_cycle: 1
target: docs/plans/2026-05-10-strict-contracts-force-cutover-design.md
target_commit: fefbbf46
date: 2026-05-10
verdict: FAIL
---

# Adversarial Review — Strict-Contracts Force-Cutover Design (Cycle 1)

## Status

**FAIL** — 3 Critical findings, 6 Important findings, 4 Minor findings. Design contains a load-bearing factual error about the cutover surface in 4 of 5 in-scope plugin repos that re-shapes Phase 2 from "5 parallel migrations" to "1 migration + 4 net-new server-side gRPC implementations". Several other Critical/Important issues compound (compat-shim creep in Phase 1+4, missing CLI-flag failure-mode coverage, undocumented data-loss risks for in-flight pipelines during Phase 3).

## Findings

### CRITICAL

#### C-1. Phase 2 surface mis-stated for 4 of 5 plugins — only DO has the switch-dispatch surface to delete

The design's §Components Phase 2 says (paraphrased):

> For each of `workflow-plugin-{aws,gcp,azure,digitalocean,tofu}`: … DELETE the entire `internal/module_instance.go` `InvokeMethod` switch (now ~25-30 cases per plugin)

This is materially wrong. Verified in workspace:

- `find /Users/jon/workspace/workflow-plugin-aws -name "module_instance*"` → no matches
- `find /Users/jon/workspace/workflow-plugin-azure -name "module_instance*"` → no matches
- `find /Users/jon/workspace/workflow-plugin-gcp -name "module_instance*"` → no matches
- `find /Users/jon/workspace/workflow-plugin-tofu -name "module_instance*"` → no matches
- `grep -rln "case \"IaCProvider\." /Users/jon/workspace/workflow-plugin-*` → only DO + DO's own worktrees

`/Users/jon/workspace/workflow-plugin-aws/internal/module.go` (the entire module wiring file for AWS) is 30 lines: it returns an `iacProviderModule` that wraps `interfaces.IaCProvider` directly with NO `ServiceInvoker` / `InvokeMethod` / switch dispatch implementation. AWS does not implement `ServiceInvoker` at all. Same for Azure (`internal/provider.go`) and GCP (`provider/`) and Tofu.

Meanwhile `/Users/jon/workspace/workflow/_worktrees/strict-contracts-force-cutover/cmd/wfctl/deploy_providers.go:229` requires:
```go
invoker, ok := mod.(remoteServiceInvoker)
if !ok {
    return nil, nil, fmt.Errorf("plugin %q iac.provider module (%T) does not support service invocation — upgrade with: wfctl plugin update %s", pluginName, mod, pluginName)
}
```

So today, AWS/Azure/GCP/Tofu **CANNOT be loaded as remote IaC providers via wfctl** at all — they fail this type-assert. Either:
1. They are only used in-process / via direct factory calls, never via wfctl's remote IaC code path (then the cutover for them is a NEW capability, not a migration), OR
2. There's a completely different code path the design author hasn't accounted for.

**Implication:** Phase 2 as written collapses for 4 of 5 plugins. The design needs to:
- Verify which plugins actually flow through `discoverAndLoadIaCProvider` today (a workspace survey was claimed but is wrong).
- For plugins that don't currently support remote dispatch, the work is "implement `pb.IaCProviderServer`" net-new, not "delete switch cases". Time-cost is higher and risk profile is different (no working baseline to regression-test against).
- The `~25-30 cases per plugin` count is a projection from DO's pattern that doesn't reflect reality.

**Fix recommendation:** Re-do the Phase 2 survey (`grep -rln "ServiceInvoker\|InvokeMethod" /Users/jon/workspace/workflow-plugin-*`). Per-plugin Phase 2 task breakdown should distinguish "delete switch + add gRPC server" (DO) vs "add gRPC server net-new" (AWS/Azure/GCP/Tofu). Acknowledge that for the latter four, this PR may be the FIRST PR exercising their IaC interfaces end-to-end through wfctl, which inflates QA burden.

#### C-2. Phase 1's "build tag default = legacy, end-state = typed" IS the compat-shim pattern the user mandate explicitly forbids

§Components Phase 1 says:
> All consumers of `remoteIaCProvider` get a feature-flag (build tag `iac_typed`) that switches to typed client. Default: legacy. End-state: typed.

§Rollback Phase 1:
> revert workflow PR; no consumer impact (build-tag default = legacy)

Compare to memory `feedback_force_strict_contracts_no_compat`:
> "Existing in-progress plan ... took the additive approach: keep legacy `Struct` paths beside new typed `Any` payloads ... That preserved compat — and the bug class persisted ... Old workflow tags ... become permanently incompatible with new plugin tags"

A build-tag-gated dual implementation IS the additive approach renamed. It carries every defect of the additive approach the mandate rejected:
- Both code paths must be maintained until Phase 4
- Tests can pass on the typed path while production runs the legacy path (default = legacy)
- Reviewers must mentally hold "is this bug only in path A or both paths" for every PR
- Phase 4's "delete the legacy path" PR is a net new behavior change at the moment a customer would be most exposed (post-tag-publish v1.0.0 → v1.0.1 cleanup)

**The bug class evidence cited in the user mandate (audit-keys at v0.25.0 → wfctl bump → EnumerateAll missing client → workflow v0.27.1 → DO v0.14.2 dispatcher missing) was NOT a coordination failure — it was a "wrong path got exercised in production" failure.** Phase 1 reproduces exactly that failure mode in a build tag.

**Fix recommendation:** Collapse Phase 0 + Phase 1 into a single workflow-side PR that introduces typed proto AND the typed wfctl client AS THE ONLY PATH. Workflow `main` does not ship a build-tag selector. The legacy `remoteIaCProvider` is deleted in this same PR. Yes, this means the workflow PR cannot land until ≥1 plugin is ready in Phase 2. That's the actual force-cutover the mandate requires. Sequencing becomes: (workflow proto + typed-client PR draft) → coordinate-merge with (DO plugin v1.0.0 PR) → merge both same day. Other plugins follow. workflow main is never in a state where build-tag selects between paths.

#### C-3. Phase 3 lacks any treatment of in-flight pipelines or live plugin reload — silent breakage window

§Components Phase 3:
> Default the build tag from `iac_legacy` to `iac_typed` in workflow main
> Bump workflow's plugin loader to require typed-only plugins; reject any plugin that doesn't expose `pb.IaCProviderServer` registration

Missing failure modes:
1. **Pipeline in mid-execution at workflow upgrade time** — operator runs `wfctl deploy` with a long-running plan/apply, upgrades workflow binary mid-flight. Old plugin process is still alive (subprocess), new wfctl talks typed proto, plugin can't decode. What happens? Hang? Crash? Half-applied state? Design doesn't say.
2. **Plugin loaded, mid-call, host calls a typed RPC the plugin doesn't have a handler for** — gRPC returns codes.Unimplemented. wfctl's "treat as 'not implemented'" handler may misinterpret a TRANSITIONAL gap as a "this provider doesn't support this method" semantic and silently fall through to next provider. Same bug class the mandate is trying to kill.
3. **State backend mid-bootstrap** — `BootstrapStateBackend` returns a typed proto in the new flow. If the state backend rolls back partial state due to a typed-decode error, the next deploy sees a corrupt state file. No discussion of state-file format compatibility across the cutover.
4. **`.wfctl-lock.yaml`** — operators pin plugins. If wfctl v1.0.0 rejects a v0.x plugin pin (per Phase 3's "reject any plugin that doesn't expose typed registration"), every existing operator's lock file becomes invalid in one step. No grace period mentioned.

**Fix recommendation:**
- Document plugin process lifecycle expectation: workflow MUST refuse to start a deploy if any pinned plugin isn't typed-capable. State explicitly that mid-call upgrades are unsupported (operators must complete deploys before upgrading).
- For optional-method `codes.Unimplemented`, add a discriminator in the proto so "I don't implement this method by interface design" is distinguishable from "this plugin version is too old". E.g., `IaCProviderInfo.supported_optional_methods` as a typed-known set that wfctl checks at handle-open time.
- Document `.wfctl-lock.yaml` migration step explicitly in Phase 3 acceptance criteria.
- State-file format invariance must be an explicit invariant of the cutover (`§V`-grade if this design ever feeds into a SPEC).

### IMPORTANT

#### I-1. `codes.Unimplemented` for optional sub-interfaces IS a fallback in spirit despite the design's defense

§Top 3 self-challenge doubts §2 acknowledges the tension and offers a defense:
> Defense: it's not a *swallow* — wfctl explicitly checks for Unimplemented and either continues to next provider OR errors loud (per v0.27.1's pattern). Not a soft escape hatch; explicitly typed via the gRPC status code.

This defense is weaker than presented. The whole bug class the mandate targets is "method exists in interface, plugin doesn't implement it, wfctl can't tell at compile time, runtime surfaces it". `codes.Unimplemented` reproduces exactly that semantic: method exists in proto, server doesn't implement handler, wfctl finds out at runtime.

The session-cited DO `EnumerateAll` bug would happen identically post-cutover if DO chose to return `codes.Unimplemented` from the `EnumerateAll` RPC. The compile-time check (server interface satisfaction) only catches "method missing from generated server" — it doesn't catch "method present but stubbed to return codes.Unimplemented".

**Fix recommendation:** Adopt the alternative the design itself names: every provider implements every interface (including optional), with explicit "not supported" semantics encoded in the typed response message rather than via a transport-layer status code. E.g., `EnumerateAllResponse{NotSupported bool, Outputs []*ResourceOutput}`. This forces every provider to make a deliberate decision (compile-time forced) about whether they want to advertise capability or not. `codes.Unimplemented` becomes reserved for "this build is too old to handle this RPC at all" (transport semantic), not for interface design.

#### I-2. CLI-flag bug class is silently NOT addressed — design's stated goal is wider than its mechanism

The design's Goal:
> Eliminate the entire bug class where missing client bridge / missing server dispatcher case / wrong flag name / wrong return shape surfaces at runtime against staging instead of compile time.

Of the 4 listed bug classes, only 2 are actually closed by the typed gRPC mechanism:
- ✅ Missing client bridge → wfctl compile fail
- ✅ Missing server dispatcher → plugin compile fail
- ❌ Wrong flag name (e.g., this session's `--allowlist` vs `--preserve-names`) — CLI flags live in `cobra` definitions, NOT in gRPC. Typed proto doesn't help here.
- ❌ Wrong return shape — depends on what "shape" means. If shape = proto field, typed catches it. If shape = the bug from this session (`expected 1 rotation result, got 0`), that was internal logic in `bootstrapSecrets`, NOT a gRPC shape mismatch. Typed proto doesn't help.

**Fix recommendation:** Either narrow the Goal statement to honestly reflect what typed gRPC catches, OR add explicit complementary work for the other two bug classes (CLI flag schema testing, internal-result-shape contract tests). Otherwise expectations will drift: "we did the strict-contracts cutover, why are we still seeing CLI flag bugs?"

#### I-3. Phase 4 "soak window" definition is dangerously vague AND amounts to a compat window

§Components Phase 4:
> After v1.0.0 is published + soak period (defined as "next plugin re-tag completes against workflow v1.0.0")

§Rollback Phase 4:
> Mitigation: minimum 7-day soak window between Phase 3 and Phase 4

These are inconsistent (one is plugin-event-driven, one is calendar-driven). Worse, the soak IS a compat window — during it, both legacy and typed paths exist, and reverts are possible. The user mandate explicitly says no compat windows.

**Fix recommendation:** Either drop the soak entirely (consistent with the mandate — Phase 3 + Phase 4 are a single PR / single tag) or own it explicitly: "Phases 3-4 are a 7-day soft-launch window during which legacy is reachable behind a build tag for emergency rollback only; this is a deliberate compat window scoped to one workflow release cycle."

#### I-4. Phase 2 timeline assumption ("~2-3 weeks") contradicts session-level data

§Assumptions §2:
> All 5 IaC plugin repos can coordinate Phase 2 within ~2-3 weeks (true: each is independent; per workspace memory `feedback_per_agent_worktree_per_task_pr`, 5 parallel PRs is well within tooling capacity)

The cited memory speaks to **tooling capacity** (per-agent worktrees enable concurrency), not to **PR cycle time**. The recent project memories tell a different story:
- BMW PR 7 (eventbus): 5+ review rounds, multi-day
- BMW PR 9 (adapters): 4 review rounds, mid-PR reviewer replacement
- BMW PR 10 (Stripe Issuing): 7 tasks, multi-day high-parallelism
- DO plugin v0.8.0 ship: 7 review rounds

For a force-cutover migration where compile-time enforcement IS the value-prop, every PR must close the contract gate cleanly OR the workflow PR can't merge. With C-1 above (4 of 5 plugins are net-new gRPC implementations), realistic Phase 2 is more like 4-8 weeks.

**Fix recommendation:** Either accept 4-8 weeks as the realistic timeline OR descope (start with DO + 1 other plugin, ship workflow v1.0.0 with 2-plugin support, follow with per-plugin v1.0.x adds). Don't claim 2-3 weeks then discover at week 4 that Phase 3 is gated.

#### I-5. Application-consumer survey is incomplete

§Components Phase 5 lists `core-dump` + `buymywishlist` as pin-bump consumers. §Assumptions §4:
> No third-party (non-GoCodeAlone-org) plugin uses `IaCProvider` interface (true at survey time; if false, those authors get the breaking-change announcement and migrate or stay on the pre-cutover workflow tag).

Verified `grep -rln "interfaces.IaCProvider\|interfaces.ResourceDriver" /Users/jon/workspace/core-dump /Users/jon/workspace/buymywishlist` → ZERO matches in either repo (only matches in `.claude/worktrees/.../docs/plans/` which are agent scratch). Both consumers use IaC purely via `wfctl` CLI subprocess.

That's good news — but it means the design's framing "pin-bump PRs" undersells what's actually happening: it's "wfctl version pin bump only", not "code change in consumer". Phrasing it as "IaC dependency bump" creates expectations that don't match.

Also: workflow-cloud, ratchet, ratchet-cli, workflow-cloud-ui — none surveyed. workflow-cloud especially uses workflow-engine programmatically (not via wfctl), so if it imports `interfaces.IaCProvider` directly, it gets typed-API breakage.

**Fix recommendation:** Add to §Components Phase 5 an explicit survey checklist:
- [ ] `grep -rln "interfaces.IaCProvider\|interfaces.ResourceDriver"` across: workflow-cloud, ratchet, ratchet-cli, workflow-cloud-ui, workflow-cloud-registry
- [ ] Document each match's migration impact
- [ ] Confirm surveyed = inclusive, not just "the two we already thought of"

#### I-6. Missing failure mode: protoc + grpc-go version drift across 6 repos

§Assumptions §1:
> protoc + grpc-go are already in the workflow build chain (true: per `go.mod` / `Makefile`)

True for workflow. But Phase 2 plugin migrations require each plugin repo to:
- Add protoc to their build pipeline (or vendor generated `.pb.go` from workflow)
- Match `google.golang.org/grpc` minor version with workflow (mismatched grpc-go versions cause silent wire incompatibilities)
- Match `google.golang.org/protobuf` version

Cross-repo dependency upgrade synchronization is one of the most painful parts of the existing Go ecosystem. The design says nothing about how the 5 plugin repos pin grpc-go to a known-compatible version, or what happens if one plugin lags.

**Fix recommendation:** Add explicit cross-repo dependency version requirements as part of Phase 0:
- Workflow declares `tools.go`-style protoc + grpc-go version pins
- Plugin repos must use `replace` directives or explicit `go.mod` versions matching workflow's choice
- CI gate: each plugin's `go.sum` for `google.golang.org/grpc` matches workflow's
- Document the version-drift failure mode (silent wire mismatch) explicitly

### MINOR

#### M-1. Acceptance criterion "grep ZERO matches" is structurally weak

> `grep "InvokeService" cmd/wfctl/ plugin/external/` in workflow main returns ZERO matches in non-test code

This is gameable: a developer renames `InvokeServiceLegacy` → `legacyDispatch` and the grep passes while the substance remains.

**Fix recommendation:** Specify the structural removal: "DELETE `interface ServiceInvoker`, `interface ServiceContextInvoker`, `interface TypedServiceInvoker` type definitions in `plugin/external/sdk/interfaces.go`; DELETE `InvokeService` RPC method on `service Plugin` in `plugin/external/proto/plugin.proto`; DELETE `RemoteModule.InvokeService` method receiver in `plugin/external/remote_module.go`. Verify via `git log -p` that these specific type/method definitions are removed, not just renamed."

#### M-2. SuperSeded-by linkage one-way; risk of plan-state confusion under scope-lock

The new design has `supersedes: docs/plans/2026-04-26-strict-grpc-plugin-contracts-design.md` in frontmatter. The 2026-04-26 design has `supersedes: []` and `superseded_by: []` — i.e., it does NOT yet mark itself superseded.

If `superpowers:scope-lock` evaluates manifest hashes by reading `docs/plans/INDEX.md` or by globbing all `docs/plans/*.md`, it may pick up the old in-progress design's task list AND the new one. Inconsistent state.

**Fix recommendation:** As part of Phase 0 (or as a separate doc-housekeeping commit before Phase 0), update the 2026-04-26 design + plan frontmatter to set `superseded_by: docs/plans/2026-05-10-strict-contracts-force-cutover-design.md` and `status: superseded`. Update `docs/plans/INDEX.md` if one exists.

#### M-3. ADR-1 ("Hard cutover over additive") would invalidate a substantial body of merged work without addressing it

The 2026-04-26 implementation plan tracker lists 14 plugin repos that ALREADY merged additive strict-contracts work, with workflow PR #497 + 14 plugin PRs all merged. The new design says "salvage ContractRegistry / typed-Any infrastructure for Module/Step/Trigger" (§Salvage), but doesn't acknowledge that many plugins (e.g., workflow-plugin-audit, workflow-plugin-sso, workflow-plugin-ws-auth) have NOTHING to do with IaC and so the additive work for them is genuinely complete and unrelated.

This isn't a design flaw, but the ADR phrasing "Hard cutover over additive — supersedes 2026-04-26 design's additive approach" is misleading. The two designs solve different problems; the new one is not a wholesale replacement.

**Fix recommendation:** ADR-1 wording: "Hard cutover for IaC-flavored interfaces (IaCProvider, ResourceDriver) supersedes the additive approach of 2026-04-26. The Module/Step/Trigger additive work (workflow PR #497 + 14 plugin PRs) remains the live design for those interfaces."

#### M-4. Top 3 self-challenge doubts §3 doesn't actually challenge — it defends

§Top 3 doubts §3 (salvage ContractRegistry):
> Defense: ContractRegistry approach is appropriate for the dynamic Module/Step/Trigger registration shape; gRPC services are appropriate for the fixed-interface IaC shape. They're solving different problems.

A self-challenge that ends in "we're right, here's why" isn't a self-challenge. The genuine adversarial framing would be: "If two mental models in one SDK is acceptable, why is two code paths in one wfctl unacceptable (cf. C-2)?" That's the real tension and the design dodges it.

**Fix recommendation:** Either escalate this as an open question OR commit to converging both models eventually (e.g., "Phase 6+ unifies all interfaces under typed gRPC services; ContractRegistry becomes the legacy path"). The current "they're solving different problems" framing is intellectually convenient and may be wrong.

## Bug-class scan transcript

| Class | Found? | Note |
|---|---|---|
| **Unstated assumptions** | YES | C-1 (Phase 2 surface), C-3 (in-flight pipeline behavior), I-6 (grpc-go version sync). Most load-bearing: C-1 — the entire Phase 2 PR breakdown rests on a survey that contradicts the actual code. |
| **Repo-precedent conflicts** | YES | C-2 — Phase 1's build-tag dual-path directly conflicts with the user-mandate memory `feedback_force_strict_contracts_no_compat`; I-3 — soak window same. M-2 — supersession not bidirectional; conflicts with how scope-lock evaluates `docs/plans/`. |
| **YAGNI violations** | NONE FOUND | Optional-sub-interface support could be argued as YAGNI (I-1's alternative removes it), but the design has stated requirements for the multi-provider-try-each semantic from v0.27.1 that justify some mechanism. |
| **Missing failure modes** | YES | C-3 (in-flight pipeline + .wfctl-lock invalidation + state-backend mid-bootstrap); I-1 (codes.Unimplemented as undetectable runtime gap); I-6 (grpc-go version drift); design's §Error handling section is one paragraph and doesn't address any of these. |
| **Security / privacy** | PARTIAL | Design notes sensitive output handling becomes typed proto field (good — closes secret-leak risk). Doesn't address: (a) plugin authentication during Phase 3 transition (does workflow authenticate plugin TLS / handshake change?); (b) gRPC metadata leakage if errors are now structured (could leak more info than legacy string-formatted errors). Not Critical/Important — note as area to revisit during implementation. |
| **Rollback story** | YES | I-3 — soak window definition contradicts mandate AND is too vague to act on. Phase 4's "single PR single revert via git" understates the difficulty (database/state-file format implications, plugin-pin invalidation, etc.). |
| **Simpler alternative not considered** | YES | The simplest force-cutover is "single-PR coordinated merge" (the alternative offered in C-2 fix). Design explicitly proposes 4-phase build-tag flow, never names the single-PR alternative as a baseline to compare against. |
| **User-intent drift** | YES | I-2 — Goal statement promises 4 bug classes addressed; design only addresses 2 of them mechanically. C-2 — Phase 1's build-tag is a partial-additive fallback that the user explicitly forbade. |

## Alternatives the author did not consider

### Alternative A — Single-PR force-cutover (no build tag, no soak)

Instead of 4 phases (proto → typed-client coexists → plugins → cutover), do:

1. Workflow PR draft: typed proto + typed wfctl client + DELETE legacy `remoteIaCProvider` + DELETE `InvokeService` RPC. Held in draft.
2. DO plugin v1.0.0 PR: implement `pb.IaCProviderServer`, DELETE `module_instance.go` switch.
3. Same-day coordinated merge: workflow + DO. Workflow tag = v1.0.0.
4. AWS/Azure/GCP/Tofu net-new IaC-server PRs (or stay on workflow ≤v0.27.x permanently).
5. Application consumers (core-dump, BMW): pin-bump.

Eliminates: build tag, soak window, 4-phase coordination, mixed-state rollback. Costs: workflow PR sits in draft until DO is ready; 4 of 5 plugins might be left behind permanently if their authors don't migrate.

The cost is exactly what the user mandate said is acceptable: "~20 plugin repos updated together; coordinated release; no rolling migration window. The trade is permanent end of this bug class." Why didn't the design consider this? It feels like the additive habit pulled the design back toward phasing.

### Alternative B — Force-merge IaCProvider into pb.PluginServer instead of new pb.IaCProviderServer service

Rather than adding a new gRPC service, add typed RPCs (`Plan`, `Apply`, `Enumerate`, etc.) directly to the existing `service Plugin` proto. Plugins that don't implement them get a clear compile error against the existing service interface. No new service-discovery wiring; reuses existing handle-id model. Lower diff cost. The downside is namespace pollution on the Plugin service — but for a project that's force-cutting-over anyway, that's a fixable Phase-N rename.

### Alternative C — Code-generate the wfctl-side proxy from the proto, eliminating hand-written `remoteIaCProvider` entirely

If the typed gRPC client already exists (protoc-generated), the design's wfctl-side `iacClient` wrapper is a hand-written re-marshalling layer. Remove it. wfctl talks directly to `pb.IaCProviderClient`. This eliminates one of the four bug-class surfaces (the hand-written client bridge) by REMOVING the layer entirely, not by typing it.

The design's §Architecture-target-typed diagram shows wfctl calling `IaCProviderClient.EnumerateAll(...)` directly — this is what Alternative C describes. But §Components Phase 1 then describes a `iacProviderClient` wrapper. The design contradicts itself here. Pick one.

## Verdict

**FAIL.** Three Critical findings (C-1, C-2, C-3) and six Important findings (I-1 through I-6). Design must be revised to:

1. Re-verify the Phase 2 surface against actual plugin code (C-1).
2. Eliminate the build-tag dual-path in Phase 1 (C-2) — collapse Phase 0+1 into a single proto+typed-client+legacy-delete PR.
3. Address in-flight pipeline / plugin reload / state-file / lock-file failure modes in Phase 3 (C-3).
4. Address Important findings I-1 through I-6, especially I-1 (`codes.Unimplemented` design choice) and I-3 (soak window contradiction with mandate).
5. Acknowledge or refute Alternatives A, B, C in the §Brainstormed-approaches-equivalent section.

Cycle 2 should re-review after these revisions land.
