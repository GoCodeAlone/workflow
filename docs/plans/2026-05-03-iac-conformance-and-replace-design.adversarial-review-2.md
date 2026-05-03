### Adversarial Review Report

**Phase:** design
**Cycle:** 2 of 2
**Artifact:** docs/plans/2026-05-03-iac-conformance-and-replace-design.md (commit 77b8962, branch design/iac-conformance-and-replace)
**Status:** FAIL

This is the LAST review cycle. Findings below that the author cannot resolve in revision must be escalated to user via Verdict reasoning rather than silently accepted.

---

**Findings (Critical):**

- **[Repo-precedent conflicts / Missing failure modes] §W-3 + §W-8 — `wfctlhelpers.ApplyPlan` silently drops the `ErrResourceAlreadyExists` upsert recovery path that DO Provider currently implements.** `workflow-plugin-digitalocean/internal/provider.go:194-235` has substantial recovery logic on top of the create switch arm: when `Create` returns `ErrResourceAlreadyExists`, it checks `upsertSupporter` interface (`SupportsUpsert()`), calls `Read` to fetch the existing ProviderID, then dispatches to `Update`. The design's `wfctlhelpers.ApplyPlan` (lines 137-146) shows ONLY a 4-arm switch (`create`/`update`/`replace`/`delete`) with no upsert hook, no `SupportsUpsert` consultation, no recovery branch. The W-8 codemod's `refactor-apply` mode "replaces the create/update switch in `Apply`" — i.e., it deletes the upsert recovery code as a "non-canonical idiom" without a `// wfctl:skip-plan-codemod` marker (because no maintainer reviewing a dry-run will recognize it as canonical-but-undocumented). Result: post-codemod DO plugin loses adoption-on-conflict for VPCs/databases/firewalls where ProviderID isn't passed directly — silent regression of an existing production behavior. **Recommendation:** (a) audit ALL existing provider Apply implementations for non-trivial idioms (DO upsert + ErrResourceAlreadyExists; AWS's `case "update", "replace"` collapse on line 214; GCP's separate `case "replace"` at line 237; Azure's already-correct delete handling) and EITHER include their semantics in `wfctlhelpers.ApplyPlan` as first-class or document them as opt-out via marker; (b) the codemod must AST-detect deviation from a canonical template, abort + flag rather than rewrite when divergence found; (c) add a conformance scenario `Scenario_UpsertOnAlreadyExists` so the existing DO behavior is captured before the codemod runs, not after.

- **[Unstated assumptions / Missing failure modes] §W-3 + §Approach 2 dominance argument — design assumes all 4 provider Apply switches share a canonical shape suitable for one codemod template; spot-checking the actual code disproves this.** Verified shapes:
  - `DOProvider.Apply` (DO): `create`, `update`, no replace, no delete + upsert recovery
  - `AWSProvider.Apply`: `create`, `update`+`replace` collapsed (silent miscompile of replace as Update), `delete`
  - `GCPProvider.Apply`: `create`, `update`, `replace` (separate arm), `delete`
  - `AzureProvider.Apply`: `create`, `update`, `delete`, no replace

  Each is its own bug surface: AWS literally implements `case "update", "replace"` on a single arm (provider/provider.go:214) — the design's "ForceNew silently downgraded to Update" framing applies to AWS doubly because Apply will *appear* to handle replace yet route to Update. This means: (1) the codemod can't apply one template — it must either be 4 templates or hand migration; (2) Approach 2's V1+V2 dual-interface argument gets stronger because per-plugin migration risk is heterogeneous; (3) the author's "Approach 3 strictly dominates Approach 2" table (lines 46-55) understates Approach 3's risk — the "1 PR per plugin (codemod-applied)" cell is wrong if codemod can't safely apply across plugins. **Recommendation:** (a) replace the dominance claim with a per-plugin codemod-feasibility table; (b) reconsider Approach 2 — the V1 wrapper would let GCP-which-already-has-replace and AWS-which-misimplements-replace migrate at independent cadences without each blocking on conformance for the other; (c) at minimum, retitle §W-8 from "codemod" to "codemod for the canonical shape (DO + Azure); manual port for AWS (replace miscompile) + GCP (replace already separate)".

---

**Findings (Important):**

- **[Missing failure modes] §W-3 sequencing constraint "BC-2 audit closed" is not enforceable per the precedent design — the precedent does not define an "audit closed" gate.** `2026-04-28-iac-plugin-review-design.md` Phase C (lines 226-235) describes a 3-step manual process per plugin (run checklist → file issues → open per-finding PRs), with no signal in any tracker that maps to "BC-2 closed for plugin X." The strict-contracts tracker (`2026-04-26-strict-grpc-plugin-contracts.md:151`) shows DO BC-2 closed in v0.8.0, but AWS/GCP/Azure/Tofu all show "(Phase C audit pending)". Per the precedent's own Phase A→B→C sequencing, no BC-2 audit has been *run* for AWS/GCP/Azure as of today. The design's constraint "W-3 gates on each plugin's BC-2 audit being closed" therefore depends on a yet-undone audit cycle for 3 of 4 providers. The cost-benefit table (lines 46-55) implicitly assumes "all 4 providers migrate" but the BC-2 sequencing constraint will block 3 of 4. The design needs to commit: either (a) explicitly defer P-AWS/P-GCP/P-AZ until each plugin's Phase C audit completes (which extends the sequence by 4-12 weeks based on precedent estimates of "4-5 findings per plugin × 6 plugins"), or (b) state that W-3 ships only against DO and the multi-provider generalization waits, OR (c) define a concrete audit-closure gate (e.g., a tracker badge / CI check) that operators can mechanically test. **Recommendation:** add an "Audit closure mechanism" sub-section to W-3 specifying how a reviewer/CI can determine "this plugin's BC-2 is closed" without manual interpretation; and update the sequencing table to make P-AWS/P-GCP/P-AZ depend on BC-2 audit PRs landing first (otherwise the design overpromises its own ordering).

- **[Missing failure modes] §W-3 — the new ComputePlan signature change breaks the second call site (`cmd/wfctl/infra.go:199`, `wfctl infra plan`) without addressing the implication: plan.json is generated WITHOUT JIT-aware substitution but consumed by a future `wfctl infra apply --plan` that DOES expect JIT semantics.** Existing flow:
  1. `wfctl infra plan -o plan.json` → calls `platform.ComputePlan(desired, current)` (line 199), writes plan.json
  2. (operator commits plan.json, time passes)
  3. `wfctl infra apply --plan plan.json` → reads plan.json, calls `provider.Apply(plan)`

  Under W-3 + W-5, ComputePlan needs a provider for Diff dispatch + plan.SchemaVersion bumped. The design discusses the apply-side cost mitigation (cache, parallel concurrency) but is silent on:
  - Does `wfctl infra plan` ALSO call provider.Diff? (If so, plan.json contains action classifications that depend on a provider-version-pinned state — when apply runs against a newer plugin, classifications differ and the SchemaVersion check doesn't catch it because schema hasn't changed, only Diff semantics have.)
  - Does the diff cache survive across `plan` and `apply --plan`? (`~/.cache/wfctl/diff/` is process-local; CI runners are ephemeral; the cache is effectively useless across the two-command workflow.)
  - The `replaceIDMap` only exists during apply — but what about `wfctl infra plan` rendering a plan that depends on a parent's pre-replace ID? The plan output annotation (W-5 lines 235-247) shows `${VPC.id}` placeholder — but if plan.json persists, what value is recorded for the dependent's `vpc_uuid` field? An empty string? The pre-replace value? An unresolved expression?

  **Recommendation:** spell out the plan/apply two-step UX explicitly. Either (a) ban `wfctl infra plan -o file` + `wfctl infra apply --plan file` for plans containing replace-cascade or JIT-resolved fields, with a clear error at plan time; (b) add per-action `RequiresJITResolve []string` to plan.json so apply --plan knows what to defer; (c) document that `wfctl infra apply --plan` is a fast-path that doesn't support cascade-replace and apply-without --plan is the canonical path.

- **[Security / privacy / Unstated assumptions] §W-7 smoke gate cost cap "≤$0.01/PR/provider" is wrong for AWS at the cited resource shape.** Design says `t4g.nano` with "lifetime ≤5 min". AWS pricing realities (verifying intent rather than memorizing): EC2 instances bill per-second with a 60-second minimum. `t4g.nano` on-demand in us-east-1 is roughly $0.0042/hour ≈ $0.00007/minute, so 5 minutes of compute alone is sub-cent. BUT the smoke scenario includes lifecycle: VPC creation (no charge), security group (no charge), EBS root volume (typically minimum 8GB gp3 ≈ $0.08/GB-month prorated to seconds is negligible but SOMETIMES 1-hour minimums apply per service), data transfer out (negligible at this scale), CloudWatch (per-metric, maybe $0.30/month per metric in cents-per-day). The bigger gotcha: AWS NAT Gateway, Elastic IPs, Load Balancers, RDS — if `Scenario_NeedsReplaceTriggersReplaceAction` ends up smoke-testing one of these (because the smoke scenario has to be one with `ForceNew` semantics that's also the cheapest available), the cost-cap claim collapses. Even within EC2 alone, EBS snapshots from an interrupted teardown linger and bill. The claim "≤$0.04/PR all 4 providers" is unsubstantiated. **Recommendation:** (a) name the smoke resource for each provider PRECISELY with current pricing snapshot (link to the provider's pricing page commit-pinned in the design), (b) define a pre-PR cost-budget enforcement check that aborts the smoke if billing API reports >$N spend in last N hours for the conformance account, (c) or downgrade the cost-cap claim from "we will spend ≤$0.01/PR/provider" to "we cap at $1/PR/provider with a hard fail above that" — the larger budget is more honest.

- **[Unstated assumptions / Missing failure modes] §W-9 Plan() deprecation creates a future-Plan-customization gap with no escape hatch.** The deprecation note says `Plan()` "was never called by wfctl; platform.ComputePlan is the canonical planner" and the v0.21 removal is committed. But the `IaCProvider.Plan()` interface method exists for reasons including: a future Tofu plugin that wants to delegate planning to `tofu plan` (an external tool with its own state diffing semantics that platform.ComputePlan can't reproduce); a Pulumi-style provider with strongly-typed input validation; multi-resource-batch planning (e.g., DO databases that share a cluster). Removing `Plan()` from the interface + delegating exclusively to `platform.ComputePlan` collapses these abstractions. **Recommendation:** before v0.21 removal, define a `ProviderPlanner` optional interface (`Plan(ctx, desired, current) (*IaCPlan, error)`) that providers MAY implement to override platform-default planning; `platform.ComputePlan` checks for the optional interface and delegates if present, falls back to driver-Diff-loop otherwise. This preserves the future-extension capability without forcing every provider to ship a Plan stub. The current design forecloses this without weighing the cost.

- **[Unstated assumptions] §W-3 `replaceIDMap` thread safety — author's doubt #2 is acknowledged but not designed.** The doubts section says "if W-3 parallel Diff and W-5 JIT substitution share state, the per-apply mutex needs to be explicit. Worth specifying in implementation but not load-bearing for design correctness." This is incorrect framing: W-3's parallel Diff happens at PLAN time (read-only on current state) and `replaceIDMap` is populated at APPLY time (after a Replace's Create completes), so they don't share a phase. BUT W-5's JIT substitution at apply time IS concurrent with sibling cascades — if the dependency graph allows two replace cascades to apply in parallel (e.g., two unrelated VPCs each replaced with their own dependents), `replaceIDMap` writes from cascade A and reads from cascade B race. The current design implies cascades execute sequentially via topological sort but never asserts that. **Recommendation:** explicitly state "cascade replaces execute sequentially per topological group" OR "replaceIDMap is sync.Map" — and add a conformance scenario `Scenario_ParallelCascadeReplaceIDIsolation` that exercises the race.

- **[Missing failure modes] §W-2 cheap apply-time refresh — partial-failure semantics under bounded concurrency contradict the assumption "state stays at last-known value for failed Reads".** Design says "partial failure logs but doesn't block plan (state stays at last-known value for failed Reads)". But the apply-time refresh runs BEFORE plan compute (W-2.a). If a Read fails for resource X, plan computation for X uses stale Outputs — which means W-3's Diff dispatches against stale Outputs, potentially miscategorizing as `update` what should be `replace` (or vice versa). The Diff cache (W-3) keys on `sha256(current.Outputs)` so a stale-but-cached Diff result compounds. **Recommendation:** either (a) Read failure must abort plan with a clear error ("could not refresh resource X; rerun or use --skip-refresh"), (b) Diff cache must invalidate on failed-Read for that resource, or (c) plan output must annotate which actions used stale outputs ("⚠️ X: outputs not refreshed; classification based on last-known state").

---

**Findings (Minor):**

- **[Unstated assumptions] §W-3 cache key omits `current.ProviderID`.** The cache key is `(plugin-version, resource-type, sha256(spec.Config), sha256(current.Outputs))`. But two resources of the same type with identical Config and identical Outputs but different ProviderIDs (e.g., a stale resource pre-import vs a freshly-adopted one) would share a cache entry, masking import-vs-managed distinction. **Recommendation:** include `current.ProviderID` in the cache key.

- **[YAGNI / Repo-precedent conflicts] §W-7 smoke-gate "DO `s-1vcpu-512mb-10gb` Droplet" choice.** This is DO's smallest *managed-image* droplet. The conformance scenario `Scenario_NeedsReplaceTriggersReplaceAction` needs to exercise a ForceNew field — for Droplets, region is ForceNew. But changing region forces image re-pull (different region's image registry), which triggers DO's per-region image-availability check; the smoke gate needs to pre-stage that. **Recommendation:** name the specific ForceNew field the smoke uses (region? size?) and verify the chosen resource shape supports the change cleanly without provider-side errors that aren't the test's fault.

- **[Missing failure modes] §W-5 plan output annotation (lines 235-247) doesn't handle the case where the operator REVIEWS plan.json offline (no live state access).** The annotation "${STAGING_PG_HOST} ← from coredump-staging-pg.private_ip" assumes the resolution path is computable from plan-time state. But if a downstream pipeline review tool (e.g., a security scanner) reads plan.json without provider access, it sees only the placeholder string. **Recommendation:** include the resolution-graph metadata in plan.json (not just the human-readable summary) so offline review tools can audit the substitution graph.

- **[Rollback] §W-3 + §Rollback section — partial codemod rollback story is missing.** The forward path is "merge W-3 → P-DO codemod → rollout"; rollback is "revert P-DO commit". But what if P-DO ran the codemod, created a per-driver `// wfctl:skip-plan-codemod` opt-out for one resource type, then we want to rollback W-3? The opt-out marker is now meaningless code in DO; reverting P-DO un-marks it but the next forward roll re-introduces. **Recommendation:** add to the Rollback section: "skip markers introduced by codemod runs are durable; rollback of W-3 leaves them as no-ops; next-forward re-runs the codemod respecting the existing markers."

- **[Unstated assumptions] §Tests — "build-time test that `// Deprecated:` marker is present" (W-9 testing line) is a typo / unclear.** Go's `// Deprecated:` is a doc-comment convention, not a build-time check. `staticcheck` SA1019 fires at *call sites* of deprecated symbols, but the W-9 test wants to verify the marker is present on the declaration. **Recommendation:** clarify whether the test is `grep "// Deprecated:" interfaces/iac_provider.go` (a string match) or a vet-style check; latter doesn't exist in stock Go tooling.

---

**Bug-class scan transcript:**

| Class | Result | Note |
|---|---|---|
| Unstated assumptions | Finding | Provider Apply shapes vary widely (DO upsert, AWS update+replace collapse, GCP separate replace, Azure no replace); BC-2 audit "closed" is undefined; Plan() optional-interface escape hatch missing; W-3 thread safety doubt unresolved. |
| Repo-precedent conflicts | Finding | wfctlhelpers.ApplyPlan drops DO upsert recovery; codemod template doesn't fit AWS/GCP shapes; second ComputePlan call site (`infra.go:199`) not addressed in design. |
| YAGNI violations | Clean (recovered from review #1) — ValidatePlan examples now multi-cloud; codemod marker namespace fixed. |
| Missing failure modes | Finding | BC-2 audit gate not enforceable; plan/apply two-step + JIT semantics undefined; partial-Read refresh failure semantics; parallel cascade-replace race; AWS update+replace silent miscompile. |
| Security / privacy | Finding | Smoke-gate AWS cost-cap unsubstantiated; plan.json offline-review threat model untouched. |
| Rollback | Finding | Codemod opt-out marker durability across rollback; SchemaVersion only handles plan-format change, not Diff-semantics change between plugin versions. |
| Simpler alternative not considered | Finding | Per-plugin codemod feasibility was not stress-tested; Approach 2 (V1+V2 dual) gets stronger argument once the heterogeneity of existing Apply shapes is acknowledged. |
| User-intent drift | Clean — Approach 2 explicitly contrasted in revised design table; user's "core-dump is in no rush" cited correctly. |

---

**Cross-checks against the prompt's required scrutiny points:**

1. **W-3 gRPC cost mitigation** — Did it solve or move the cost? **Moved.** The bounded concurrency + cache mitigates per-plan cost, but introduces (a) cache invalidation correctness debt (author doubt #1, unresolved), (b) two-process plan/apply cache miss (cache is process-local; CI runs are ephemeral), (c) partial-Read failure semantics that compound stale Outputs into stale Diff results. The mitigation is necessary but isn't the whole solution.

2. **W-9 deprecate Plan() — new gap?** **Yes.** No optional `ProviderPlanner` interface for future plugins (Tofu/Pulumi-style) that need custom planning. Forecloses an extension point without weighing the cost.

3. **BC-2 sequencing constraint enforceable?** **No.** The precedent design's Phase C is a manual audit cycle with no machine-readable "closed" signal. The constraint is operator-trust today.

4. **ProviderID-via-replaceIDMap thread-safe under W-3 parallel Diff?** **Question is mis-framed in author's doubts.** Parallel Diff is plan-time, replaceIDMap is apply-time — they don't race. But parallel cascade replaces at apply-time DO race on replaceIDMap, and the design doesn't commit to sequential cascade execution.

5. **Smoke gate cost cap $0.01/PR/provider realistic?** **Probably not for AWS.** The number assumes EC2-only with no NAT/EIP/EBS-1hr-minimum tail, no CloudWatch, no NAT Gateway, no Load Balancer. Without a per-provider per-resource pricing audit, the claim is aspirational.

---

**Options the author may not have considered:**

1. **Per-provider conformance opt-in instead of flag-day Plan() removal.** Each plugin declares in `plugin.json` `iac.conformance_version: "v1"`. wfctl branches: v1-conformant plugins use platform.ComputePlan + ApplyPlan helper; pre-v1 plugins use legacy `provider.Plan()` + `provider.Apply()`. Migration is per-plugin with no flag day. Resolves Approach 2 vs Approach 3 by making it a per-plugin choice the conformance suite enforces.

2. **Codemod ships as `wfctl iac migrate-plugin --dry-run` (NOT a separate `cmd/iac-codemod` binary).** The codemod runs from the wfctl binary the operator already has installed, against the plugin source tree. Removes the "is the codemod synced with this wfctl?" version-skew concern. Same Go AST infrastructure, fewer release artifacts.

3. **Replace `IaCPlan.SchemaVersion` int with a content-addressed version set.** Each PlanAction declares the wfctl-feature-set it requires (`features: ["jit-substitution", "cascade-replace"]`). Older wfctl reading newer plan.json checks if its supported feature set is a superset of plan.json's required feature set, fails with a precise message ("this plan requires `jit-substitution`; binary supports `cascade-replace,refresh-outputs`"). Composes better than monotonic schema version.

---

**Verdict reasoning:**

Two Critical findings:

1. **The codemod / `wfctlhelpers.ApplyPlan` design assumed a uniform Apply shape across providers that the actual code disproves.** DO has upsert recovery the helper drops; AWS has a `case "update", "replace"` collapse that's a silent bug today and a different bug under W-3; GCP has a separate `case "replace"` that doesn't fit DO-shaped templates; Azure already implements `case "delete"` correctly without the design crediting it. This isn't a documentation gap — it changes the migration story from "codemod 4 plugins" to "audit each plugin's idioms and decide per-plugin: codemod, manual port, or skip-marker." That delta affects sequencing, risk, AND the Approach 2 vs 3 dominance argument.

2. **The W-3 "BC-2 audit closed" sequencing constraint is not enforceable** per the precedent design which it cites. AWS/GCP/Azure/Tofu Phase C audits are pending; the design overpromises ordering it can't deliver.

Six Important findings cover: BC-2 enforcement gap, plan/apply two-step JIT semantics, smoke-gate AWS cost cap, Plan() deprecation closing future extension paths, replaceIDMap parallel-cascade race, and W-2 partial-Read failure semantics compounding into stale Diff cache hits.

Five Minor findings round out scope.

**Per the prompt's PASS criteria** (zero Criticals + every Important addressed): **FAIL**. Two genuinely-new Criticals, surfaced by reading the actual provider code rather than re-litigating settled findings.

**This is the LAST review cycle (max 2).** Per the skill mandate, I escalate to user:

The design's structural ambition (in-place refactor + conformance suite + codemod) is sound and fits the user's "build benefits all providers" mandate. But the codemod feasibility assumption fails on contact with the actual heterogeneous Apply implementations across DO/AWS/GCP/Azure. **The author should either (a) reframe Approach 3 as "DO + Azure get codemod; AWS + GCP get manual port; document why each", (b) reconsider Approach 2 (V1+V2 dual) given the heterogeneity argument now strengthens it, or (c) commit to a per-provider per-PR migration plan that addresses each plugin's idioms explicitly.** The BC-2 sequencing gate is fixable in revision (define a tracker badge or CI check); the cost-cap claim is fixable (pin per-provider pricing snapshots); but the codemod-uniformity assumption requires a structural rethink before the implementation plan can be written.

Recommend the author either (i) revise to address the two Criticals before alignment-check, OR (ii) explicitly accept the Criticals as known limitations with named follow-ups, in an ADR per the design's own §Decision Record section.
