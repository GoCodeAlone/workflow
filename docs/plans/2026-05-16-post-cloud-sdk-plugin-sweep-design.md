# Post-cloud-SDK Plugin Ecosystem Sweep — Design

**Status:** Draft
**Date:** 2026-05-16
**Operator:** Jon (autonomous-mode mandate 2026-05-16: "continue with follow-ups, you'll probably need a new brainstorm/design pass before implementation to ensure the accuracy of your plans. continue autonomously")
**Related:** GoCodeAlone/workflow#656 (umbrella tracking issue), [[project_cloud_sdk_extraction_complete]] (just-shipped predecessor work), ADR 0034 (cross-repo autonomous plugin work), ADR 0039 (strict-contracts c1 ruling — plan-2 plugin shipping pattern)

## Goal

Bump 15 lagging plugin repos from workflow `v0.51.6/v0.51.7/pseudo-version` pins → `v0.53.1` so the entire plugin ecosystem is current with the post-cloud-SDK-extraction workflow tag. Mechanical sweep — no API redesign, no SDK extension. Closes the `engine pin sweep` half of `workflow#656` (which itself was anchored to v0.52.x and has stale inventory; this sweep updates + supersedes #656's table — first PR description leaves a comment on #656 noting supersession). Defers the `host conformance` half (gcp #6 + azure #4) and the `v2 action lifecycle migration` (#640) to separate design passes.

## Architecture

Per-plugin parallel PRs across 15 repos. Each PR is single-task: bump `go.mod` workflow pin → `v0.53.1` + `GOWORK=off go mod tidy` + verify build + verify tests + bump `plugin.json minEngineVersion` to `"0.53.0"` (tested-floor semantic — see Assumptions #4) + cut new patch/minor tag + release.

15-PR cluster, one PR per plugin repo. **One ordering constraint**: `workflow-plugin-authz` MUST tag + release BEFORE `workflow-plugin-agent` PR runs because `agent`'s `go.mod` directly imports `workflow-plugin-authz v0.2.2`; bumping agent's workflow pin to v0.53.1 forces MVS resolution of authz against v0.53.1's API surface, which fails unless authz also has a v0.53.1-compatible release. **Mitigation**: authz PR runs in the first wave; agent's PR (wave 2) gets a DUAL-BUMP commit (`workflow → v0.53.1` AND `workflow-plugin-authz v0.2.2 → v0.5.4` in the SAME `go.mod` change — `go mod tidy` will NOT auto-upgrade authz since workflow's own go.mod doesn't import authz, so no MVS forcing function exists. Implementer must add the authz line explicitly.).

```
wave 1 (parallel — no deps):  PR1 payments  PR2 audit-chain  PR3 tofu
                              PR4 ci-generator  PR5 github  PR6 gitlab
                              PR7 azure  PR8 admin  PR9 bento  PR10 authz-ui
                              PR11 authz  PR12 eventbus  PR13 security  PR14 supply-chain

wave 2 (after authz tag in wave 1):  PR15 agent (DUAL-BUMP: workflow + authz)
```

Cloud-sdk-bcd team has 3 implementers; 15 PRs ÷ 3 = 5 PRs per implementer. Each PR is single-commit + tag (small per-PR scope), so total team session load is bounded by review throughput rather than per-PR complexity.

### Self-hosted runner dependency (4 of 15 plugins)

`workflow-plugin-tofu`, `workflow-plugin-authz-ui`, `workflow-plugin-security`, `workflow-plugin-supply-chain` ALL use `runs-on: [self-hosted, Linux, X64]` in their release workflows. authz-ui specifically requires self-hosted for `GOPRIVATE: github.com/GoCodeAlone/*` fetch via `RELEASES_TOKEN`. The GoCodeAlone org runners (AM5GamingRig, AM5GamingRig-2, Jonathans-MBP) are currently online (verified via `gh api /orgs/GoCodeAlone/actions/runners`). **This is intentional infrastructure**, not an oversight; we KEEP the self-hosted shape (NOT migrate to `ubuntu-latest`). If a runner goes offline mid-sweep, those 4 PRs pause until runners return; the other 11 PRs continue independently.

## Components

### Per-PR scope (identical 5-step pattern, 14 of 15 PRs; PR #15 has 6 steps — see "Agent extended pattern" below)

1. `go.mod` pin bump: `github.com/GoCodeAlone/workflow vOLD → v0.53.1`
2. `GOWORK=off go mod tidy` — refresh transitive deps
3. Build + test verification: `go build ./... && go test ./... -race`
4. `plugin.json` `minEngineVersion: "0.53.0"` (add if missing — verify against current state)
5. Tag + release: GoReleaser-driven via existing `.github/workflows/release.yml` per repo

### Agent extended pattern (PR #15 ONLY — 6 steps)

PR #15 (`workflow-plugin-agent`) extends step 1 with a SECOND go.mod line bump:

1. `go.mod` DUAL pin bump: `github.com/GoCodeAlone/workflow vOLD → v0.53.1` AND `github.com/GoCodeAlone/workflow-plugin-authz v0.2.2 → v0.5.4`. Both lines change in the SAME commit. The authz bump is MANDATORY (not optional) — `go mod tidy` (step 2) will NOT auto-upgrade authz because workflow's go.mod doesn't import authz, so MVS has no forcing function.
2. `GOWORK=off go mod tidy`
3. Build + test verification — confirms both bumps compile + run together.
4. `plugin.json` `minEngineVersion: "0.53.0"`
5. (additional) Smoke-test that authz tag v0.5.4 actually exists on the remote BEFORE step 1 — if authz is still in CI from PR #11, agent PAUSES until authz tag publishes.
6. Tag + release: agent v0.9.3.

### Per-repo specifics — UNIFIED TABLE (PR# = wave-diagram order; no dual numbering)

| PR# | Wave | Plugin | Old pin | Old tag | New pin | New tag | minEng action | Notes |
|-----|------|--------|---------|---------|---------|---------|---------------|-------|
| 1 | 1 | workflow-plugin-payments | v0.51.6 | v0.4.5 | v0.53.1 | v0.4.6 | `0.51.2` → `0.53.0` | |
| 2 | 1 | workflow-plugin-audit-chain | v0.51.6 | v0.2.3 | v0.53.1 | v0.2.4 | `0.51.5` → `0.53.0` | |
| 3 | 1 | workflow-plugin-tofu | v0.51.7 | v0.1.2 | v0.53.1 | v0.1.3 | `0.51.7` → `0.53.0` | git tags exist (v0.1.0/v0.1.1/v0.1.2) but no GitHub releases; this PR is the first release-with-binaries — **MANDATORY pre-step**: inspect `.goreleaser.yaml` for `release: draft: true` and patch to `false` BEFORE tag push (same failure mode as the prior azure session — see Error Handling) |
| 4 | 1 | workflow-plugin-ci-generator | v0.51.7 | v0.1.3 | v0.53.1 | v0.1.4 | `0.51.7` → `0.53.0` | |
| 5 | 1 | workflow-plugin-github | v0.51.7 | v1.0.3 | v0.53.1 | v1.0.4 | `0.51.7` → `0.53.0` | |
| 6 | 1 | workflow-plugin-gitlab | v0.51.7 | v1.0.2 | v0.53.1 | v1.0.3 | `0.51.7` → `0.53.0` | |
| 7 | 1 | workflow-plugin-azure | v0.51.11-pseudo | v1.1.1 | v0.53.1 | v1.1.2 | confirm `0.52.0` → `0.53.0` | the workflow pin is a raw pseudo-version in `require` (no `replace` directive); update the require line + `go mod tidy` resolves to clean v0.53.1 tag |
| 8 | 1 | workflow-plugin-admin | v0.51.7 | v1.0.0 | v0.53.1 | v1.0.1 | `0.51.7` → `0.53.0` | |
| 9 | 1 | workflow-plugin-bento | v0.51.7 | v1.1.2 | v0.53.1 | v1.1.3 | `0.51.7` → `0.53.0` | |
| 10 | 1 | workflow-plugin-authz-ui | v0.51.7 | v1.0.0 | v0.53.1 | v1.0.1 | `0.51.7` → `0.53.0` | self-hosted runner (intentional — GOPRIVATE fetch via RELEASES_TOKEN) |
| 11 | 1 | workflow-plugin-authz | v0.51.7 | v0.5.3 | v0.53.1 | v0.5.4 | `0.51.7` → `0.53.0` | **First wave** — PR15 (agent) blocks on this tag |
| 12 | 1 | workflow-plugin-eventbus | v0.51.6 | v0.3.4 | v0.53.1 | v0.3.5 | confirm current → `0.53.0` | |
| 13 | 1 | workflow-plugin-security | v0.51.7 | v2.0.0 | v0.53.1 | v2.0.1 | confirm current → `0.53.0` | self-hosted runner |
| 14 | 1 | workflow-plugin-supply-chain | v0.51.7 | v0.4.0 | v0.53.1 | v0.4.1 | confirm current → `0.53.0` | self-hosted runner |
| 15 | 2 | workflow-plugin-agent | v0.51.7 | v0.9.2 | v0.53.1 | v0.9.3 | `0.51.7` → `0.53.0` | **DEPENDS ON PR11** — directly imports workflow-plugin-authz v0.2.2; DUAL-BUMP commit required (workflow + authz lines) — see "Agent extended pattern" |

(`workflow-plugin-aws v1.1.0`, `workflow-plugin-gcp v1.1.0`, `workflow-plugin-digitalocean v1.1.0` already on v0.52.0+/v0.53.0 pins — out of scope.)

### Out of scope (verified separate cadence — DEFER to dedicated future sweep)

These 4 plugins pin workflow `v0.3.56` or have no releases at all — they're so far behind the current ecosystem (50+ minor versions) that bumping mechanically is unsafe:

| Plugin | Current pin | Latest tag | Reason |
|--------|-------------|------------|--------|
| workflow-plugin-waf | v0.3.56 | v0.2.1 | 50+ minor versions behind; security-cadence cluster; needs dedicated assessment |
| workflow-plugin-sandbox | v0.3.56 | v0.2.1 | Same; security-cadence cluster |
| workflow-plugin-data-protection | v0.3.56 | v0.2.1 | Same; security-cadence cluster |
| workflow-plugin-cloud-ui | (no go.mod) | (no release) | Likely React-only / not a Go plugin; needs structural verification |

These get a separate dedicated design pass — see Out-of-Scope section.

### Mid-tier security plugins INCLUDED (verified 2026-05-16)

`workflow-plugin-security` (v2.0.0, pin v0.51.7) and `workflow-plugin-supply-chain` (v0.4.0, pin v0.51.7) have continued shipping past the original v0.3.56 security-cadence cluster baseline. Verified: both have `release.yml` configs (using `[self-hosted, Linux, X64]` runners, same as authz-ui/tofu — see "Self-hosted runner dependency" section), pin the same workflow baseline as the other 13 in scope, and ship regularly. ADDED to scope as PR13 + PR14 in the unified table above. Original "Task 0 cadence-classification" step COLLAPSED — verification done at design time, not runtime.

## Data flow

No runtime data flow change. Build-time pin propagation only:

```
upstream workflow v0.53.1 (already tagged)
  → pin bump in plugin go.mod
    → GOWORK=off go mod tidy
      → re-resolved transitive deps
        → CI builds + tests pass
          → tag + GoReleaser release
            → wfctl plugin install + image-launch picks up new tag
```

## Error handling

**Per-plugin compile breakage on bump** — if a plugin's source uses a workflow API that drifted between `v0.51.x` and `v0.53.1`, `go build` fails. Implementer:
- Captures the breakage signature (function name + signature delta).
- Files an upstream issue against `GoCodeAlone/workflow` documenting the API drift.
- DOES NOT silently work around the breakage (would mask the upstream regression).
- Reports back to team-lead; that plugin's PR pauses; the other 14 PRs continue.

**Transitive dep compile breakage** — `workflow-plugin-agent` directly imports `workflow-plugin-authz v0.2.2`. When agent bumps workflow → v0.53.1, Go's MVS resolves the entire module graph against v0.53.1, INCLUDING authz v0.2.2 source compiled against v0.53.1's API. If authz v0.2.2 references any workflow API that drifted, agent build fails in a transitive — not in agent's own code.

Two-part mitigation:
- **Sequencing**: PR #11 (authz v0.5.4 release) lands BEFORE PR #5 (agent) starts. Agent's go.mod gets BOTH `workflow v0.53.1` AND `workflow-plugin-authz v0.5.4` bumps in the same commit (so MVS resolves to fresh authz code, not stale v0.2.2).
- **Defensive**: if any other plugin also has cross-plugin deps (probe via Task 0), apply the same wave-2 sequencing.

**Per-plugin test failure on bump** — same handling: capture, file upstream, pause that plugin.

**GoReleaser failure** (azure pattern from prior session — release published as draft) — handled in-line via `gh release edit vX.Y.Z --draft=false --latest`.

**No release-with-binary infrastructure** (workflow-plugin-tofu — git tags v0.1.0/v0.1.1/v0.1.2 exist but no GoReleaser-published releases) — verify `.github/workflows/release.yml` + `.goreleaser.yaml` configs exist; if either is missing, scope-extend the tofu PR to add them before tag push. Tag conflict at v0.1.0/v0.1.1/v0.1.2 already exists, so tofu's new tag is **v0.1.3** (next sequential).

**Tofu draft-release pre-check (MANDATORY)** — verified 2026-05-16: tofu's `.goreleaser.yaml` has `release: draft: true`. This is the SAME failure mode as the prior session's azure regression (release published as draft → `wfctl plugin install` cannot resolve). Implementer MUST inspect `.goreleaser.yaml` for `draft: true` and patch to `false` (or remove the line) BEFORE tag push. The `goreleaser --snapshot --skip=publish` dry-run does NOT catch this — it never publishes anything. This pre-check is in tofu's PR3 row in the unified table; do NOT rely on the dry-run gate alone. (Same defensive check should run for all 4 self-hosted-runner plugins as a precaution: tofu, authz-ui, security, supply-chain.)

**Wave-2 cascading rollback** — if PR #15 (agent) ships then a downstream consumer breaks, reverting agent's tag is straightforward (next patch tag re-pinning to v0.9.2). However, if PR #11 (authz v0.5.4) needs revert, agent v0.9.3 ALSO requires a follow-up rollback because agent's go.mod imports authz v0.5.4 directly. The rollback ORDER is: agent v0.9.4 (re-pin authz to v0.5.3 + workflow to v0.51.7) FIRST, then authz v0.5.5 revert. Don't revert authz alone while agent v0.9.3 ships.

## Testing

- **Per-plugin build verification** — `GOWORK=off go build ./...` clean (workflow-side test target uses GOWORK=off; plugins should NOT need it but defensive).
- **Per-plugin test run** — `go test ./... -race` PASS.
- **Per-plugin GoReleaser dry-run** — `goreleaser release --snapshot --skip=publish --clean` (per-plugin) for tofu's first-release-with-binaries case + any plugin where `release.yml` is being touched.
- **Operator-run post-deploy verification (NOT a CI gate)** — after all 13 ship + a representative consumer (BMW, core-dump, ratchet) bumps, the operator manually runs `wfctl plugin list` against that consumer to confirm all updated plugins resolve to the new tag. This is intentionally NOT a CI gate because it requires live infrastructure that's neither reproducible nor reliable across the per-PR CI.

## Out of scope (intentional non-goals — separate future design passes)

- **gcp #6 + azure #4 host conformance** — requires conformance test infrastructure (subprocess invocation via ExternalPluginManager + RPC verification); not a pin-bump concern.
- **#640 v2 action lifecycle migration** — substantive scope (5 invariants in issue body), needs its own brainstorm. User direction 2026-05-16: "worth tracking as well" → memory-track in `MEMORY.md` + `project_cloud_sdk_extraction_complete.md`'s Deferred section.
- **Catalog manifest-derivation** — schema/manifest/wfctl/UI/MCP refactor (172+ hardcoded type strings in `schema/schema.go`); high blast radius.
- **TypedProvider migration for the 5 plan-2 types** — SDK scaffolding ready (workflow PR #686), waits for first consumer.
- **MessagePublisher/MessageSubscriber for IaC-bridge modules** — `decisions/0038-plugin-modules-on-iac-serve-bridge.md` Non-Goal; requires SDK extension.
- **aws-sdk-go-v2 extraction from `provider/aws/`/`plugin/rbac/aws.go`/`iam/aws.go`/`artifact/s3.go`** — too large for this cycle.
- **godo extraction** — already verified absent from workflow core go.mod; no work needed.
- **Phase B RLV doc** — non-blocking nicety, separate.
- **Security-cadence cluster (waf v0.2.1 / sandbox v0.2.1 / data-protection v0.2.1, all pinned v0.3.56)** — 50+ minor versions behind; bumping mechanically is unsafe. Needs dedicated cadence-governance assessment.
- **workflow-plugin-cloud-ui** — no Go go.mod; React-only structural shape; doesn't fit the "Go plugin pin sweep" pattern. Separate.

## Assumptions

1. **`sdk.Serve` + `sdk.ServePluginFull` surfaces still present in workflow v0.53.1.** Verified by inspection of `plugin/external/sdk/serve.go` + `serve_full.go` on `origin/main`. If false, bumps break catastrophically.
2. **No silent strict-contracts requirement for non-IaC plugins.** Strict-contracts cutover (force) targeted IaC plugin contracts (per `decisions/0024-iac-typed-force-cutover.md`); non-IaC ServePluginFull surface untouched. Verified by inspection of payments + agent source (both use `sdk.ServePluginFull` / `sdk.Serve` patterns + typed_contracts that are still supported in v0.53.1). If false, every non-IaC plugin needs a typed-Provider migration before this sweep ships.
3. **Per-plugin GitHub Actions release workflow exists** for 12 of 13 plugins. Tofu has the directory but never published a release; Task 3 verifies `release.yml` + `.goreleaser.yml` configs are present + valid before tag push.
4. **`minEngineVersion: "0.53.0"` is the tested-floor semantic, not a feature-floor semantic.** This is honest disclosure: "this plugin tag has been tested + verified against workflow v0.53.x". Operators running older workflow tags (v0.51.x, v0.52.x) are not blocked from installing — wfctl warns but allows — but support is on a best-effort basis. The reviewer's YAGNI flag is acknowledged: a feature-floor analysis (e.g., payments uses no v0.53.x APIs, true minEng = v0.51.7) would be more precise but adds per-plugin overhead. We pick tested-floor as the universal rule for sweep efficiency. (NOT bumping to v0.53.1 since no plugin uses v0.53.1-specific features; semver minimum convention says we declare the FLOOR not the ceiling.)
5. **GoReleaser configurations match prior pattern** — all 13 plugins ship via `goreleaser release --clean` triggered by tag push (see `decisions/0034-cross-repo-agent-operation-for-plugin-prs.md`); azure uses `runs-on: ubuntu-latest` post the prior session fix; if any plugin still uses `[self-hosted, Linux, X64]` on a public repo, that's surfaced + fixed in-line.
6. **Tag conflict for tofu — v0.1.3 is correct.** Verified: tofu has git tags v0.1.0/v0.1.1/v0.1.2 but NO GitHub releases (no GoReleaser binaries). The next semantic tag is v0.1.3. Pushing v0.1.0/v0.1.1/v0.1.2 would conflict with the existing tag in the Go proxy.
7. **Pseudo-version pin replacement is mechanical** for azure — the workflow pin is a raw pseudo-version in `require` (no `replace` directive in azure's go.mod, verified 2026-05-16); update the require line + `go mod tidy` resolves to clean v0.53.1 tag. If azure has divergent commits beyond the pseudo-version's base, additional work surfaces.
8. **Cross-plugin transitive deps are limited to agent → authz v0.2.2.** Probed via inspection of go.mod files. If Task 0 surfaces additional cross-plugin direct imports, those PRs also gain wave-2 sequencing.
9. **Targeting v0.53.1 (not v0.53.0)** — v0.53.1 is the released head; targeting it avoids a follow-up bump when the next consumer needs a v0.53.1-specific patch. v0.53.0 would be equally valid for these 13 plugins (none use v0.53.1's TypedModules SDK additions or try-activate rollback). Picked v0.53.1 for ecosystem-recency hygiene.
10. **Security plugins (waf/sandbox/data-protection) on v0.3.56 are intentionally excluded** — 50+ minor versions behind suggests a genuinely separate cadence (likely paused / unmaintained). Sweeping them in this design would mask the separate governance question. They get a dedicated future design pass.
11. **`workflow-plugin-cloud-ui` has no Go go.mod** — likely React-only or different structural shape; verified by API probe returning 404 on go.mod content. Out of scope by category, not by deferral.
12. **`workflow-plugin-security` (v2.0.0) + `workflow-plugin-supply-chain` (v0.4.0)** were on the original v0.3.56 security cluster but have shipped newer versions individually; their pin v0.51.7 matches the in-scope plugins' baseline, suggesting they may belong in this sweep. Task 0 verifies cadence governance before final scope decision.

## Self-challenge round (top doubts surfaced + adversarial-review feedback incorporated)

1. **Hidden API drift in non-IaC plugins.** 35 commits / 210 files changed between v0.51.6 + v0.53.1. Even if `sdk.Serve*` signatures are stable, peripheral surface (e.g., handler types, plugin registration helpers) may have shifted. Per-plugin verification CATCHES this; risk is per-plugin pause + upstream-issue overhead, not silent breakage.
2. **Operator availability during 13-PR-parallel-execution.** Cloud-SDK-bcd team has 3 implementers; 13 PRs in parallel = each implementer owns 4-5. Compaction across 13 PRs in one team session is heavy. Mitigation: per-PR is single-commit + tag (small per-PR scope), low review surface, code-reviewer can sweep approvals fast. If team session compacts mid-sweep, restart points are per-PR (which plugin still needs work).
3. **Transitive dep surprise (caught by adversarial review).** Agent → authz creates ordering dependency. If MORE cross-plugin direct deps exist (Task 0 probes), more wave-2 sequencing required.
4. **Cadence-classification accuracy (caught by adversarial review).** Initial scope missed admin/bento/authz/authz-ui/eventbus + security/supply-chain. Revised scope now includes them. Risk: security-cadence governance may say "not in this sweep" — Task 0 verifies before the security/supply-chain PRs dispatch.

## Adversarial-design-review findings (cycle 3 — post-2-revision-cycle polish, NOT re-reviewed)

Cycle 3 review surfaced 2 fresh Criticals + 2 Importants + 2 Minors after cycle-2 fixes were addressed. Per skill spec, only 2 revision cycles allowed; this third pass applies SURGICAL line-edits (NOT a re-design) and proceeds to writing-plans without a 4th adversarial pass. User-override logged.

- **Critical 1 cycle 3 (dual numbering — table `#` ≠ wave PR#)** — FIXED. Per-repo table now has explicit `PR#` column matching wave-diagram numbering. Eventbus row 13 collision (with security row 13 in old secondary table) FIXED — secondary table merged into unified table; eventbus is PR12, security is PR13, supply-chain is PR14, agent is PR15.
- **Critical 2 cycle 3 (tofu draft=true unsurfaced)** — FIXED. New "Tofu draft-release pre-check (MANDATORY)" section in Error Handling explicitly tells implementer to patch `release: draft: true` → `false` BEFORE tag push. Defensive check extended to all 4 self-hosted-runner plugins.
- **Important 1 cycle 3 (tofu first-release runner-availability risk)** — ACCEPTED inline; mitigation note in tofu's PR3 row; no separate text addition (already covered by the prioritized-manual-verification implication of "MANDATORY pre-step").
- **Important 2 cycle 3 (dry-run cannot catch draft flag)** — FIXED in same edit as C-2 above; "MANDATORY pre-check" is BEFORE the dry-run, not relying on it.
- **Minor 1 cycle 3 (table integrity / row 13 collision)** — FIXED (table merge per C-1).
- **Minor 2 cycle 3 ("replace directive" language wrong for azure)** — FIXED. Updated row PR7 + Assumption #7 to clarify it's a raw pseudo-version in `require`, no `replace` directive.

## Adversarial-design-review findings (cycle 2) — addressed in cycle 2 revision

- **Critical 1 cycle 2 (authz-ui self-hosted runner unacknowledged)** — FIXED. New "Self-hosted runner dependency" section in Architecture documents 4 plugins (tofu, authz-ui, security, supply-chain) using `[self-hosted, Linux, X64]` runners; runners verified online; intentional infrastructure (NOT migrating to ubuntu-latest); contingency for runner offline scenario.
- **Critical 2 cycle 2 (stale "8 PRs" count in two places)** — FIXED. All references updated to 15.
- **Important 1 cycle 2 (agent dual-bump underspecified)** — FIXED. New "Agent extended pattern" section in Per-PR scope explicitly lays out 6-step pattern for PR #15 with the dual-bump in step 1.
- **Important 2 cycle 2 (#656 anchored to v0.52.x)** — FIXED. Goal section explicitly notes #656's stale inventory + design supersedes it; first PR description leaves a comment on #656 noting supersession.
- **Important 3 cycle 2 (Task 0 never defined)** — FIXED. Task 0 COLLAPSED — security + supply-chain verified at design time + added as PRs #13/#14. No runtime gate.
- **Minor 3 cycle 2 (wave-2 cascading rollback)** — FIXED. Error Handling section + Rollback section both document the agent-before-authz revert order.

## Adversarial-design-review findings (cycle 1) — addressed in cycle 1 revision

- **Critical 1 (tofu first-release factual error)** — FIXED. Tofu has tags v0.1.0/v0.1.1/v0.1.2 but no GoReleaser releases. Next tag = v0.1.3.
- **Critical 2 (admin/bento/authz silent exclusion)** — FIXED. Scope expanded to 13 plugins (added admin, bento, authz, authz-ui, eventbus). security/supply-chain flagged for Task 0 verification.
- **Important 1 (transitive dep risk for agent→authz v0.2.2)** — FIXED. PR sequencing — authz v0.5.4 in wave 1, agent in wave 2 with dual-bump (workflow + authz).
- **Important 2 (uniform minEng = YAGNI for non-IaC)** — ACKNOWLEDGED. Assumption #4 reframes as "tested-floor semantic" rather than "feature-floor". Universal rule for sweep efficiency; per-plugin feature-floor analysis would be more precise but adds overhead.
- **Important 3 (incomplete inventory audit)** — FIXED. Verified 16 plugins total: 13 in-scope + 4 verified-out-of-scope (security cluster v0.3.56-era + cloud-ui Go-less).
- **Minor 1 (ADR 0024 reference unverifiable)** — FIXED. Citation now specifies path `decisions/0024-iac-typed-force-cutover.md`.
- **Minor 2 (cross-plugin smoke test not automatable)** — FIXED. Reclassified as operator-run post-deploy verification, not CI gate.
- **Minor 3 (v0.53.1 vs v0.53.0 unjustified)** — FIXED. Assumption #9 explains.

## Rollback

Per-plugin rollback: each plugin's tag bump is independently revertable.

If a plugin's release ships then a downstream consumer breaks:
- Operator OR autonomous follow-up reverts the affected plugin's pin commit + cuts a `vX.Y.Z+1` tag re-pinning to the previous workflow tag (v0.51.6 / v0.51.7 / pseudo).
- Old plugin tag (vX.Y.Z) is permanent in the Go proxy + can't be deleted, but `wfctl plugin install` resolves to `latest` so consumers pick up the rollback tag automatically.
- This is the same per-plugin matched-pair rollback pattern as plan-2 PR 4/5 (workflow core deletion + plugin v1.1.0 release as matched pair).

If `workflow v0.53.1` ITSELF needs revert (extremely unlikely — already shipped + adversarial-reviewed): the entire 15-plugin sweep reverts as a CASCADE, each plugin re-pins to v0.51.x, ships a new patch tag. Agent (PR #15) reverts BEFORE authz (PR #11) per the wave-2 cascading rollback rule above.

## Decisions to record

This sweep does NOT trigger ADR creation per `recording-decisions` skill conditions:
- No precedent divergence — matches the per-plugin-PR + per-plugin-tag pattern from plan-2.
- No non-trivial trade-off — sweep is mechanical.
- No adversarial override (will surface during adversarial review).
- No cross-skill structural change.

If adversarial review surfaces a need (e.g., a per-plugin pause becomes a permanent SDK gap requiring documented response), an ADR captures it then.

## Next pipeline step

After this design lands + adversarial-design-review --phase=design PASSES → invoke `superpowers:writing-plans` for the per-plugin task breakdown.

## Memory updates (post-execution)

Append to `project_cloud_sdk_extraction_complete.md`'s "Deferred / out-of-scope" section: mark "Plugin ecosystem v0.53.1 sweep" COMPLETE; flag #640 + gcp#6 + azure#4 + catalog-manifest-derivation as the remaining followups.

Track #640 explicitly per user direction (2026-05-16 inline) — record in MEMORY.md as standalone next-pass candidate alongside catalog manifest-derivation.
