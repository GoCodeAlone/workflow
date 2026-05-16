# Post-cloud-SDK Plugin Ecosystem Sweep — Design

**Status:** Draft
**Date:** 2026-05-16
**Operator:** Jon (autonomous-mode mandate 2026-05-16: "continue with follow-ups, you'll probably need a new brainstorm/design pass before implementation to ensure the accuracy of your plans. continue autonomously")
**Related:** GoCodeAlone/workflow#656 (umbrella tracking issue), [[project_cloud_sdk_extraction_complete]] (just-shipped predecessor work), ADR 0034 (cross-repo autonomous plugin work), ADR 0039 (strict-contracts c1 ruling — plan-2 plugin shipping pattern)

## Goal

Bump 8 lagging plugin repos from workflow `v0.51.6/v0.51.7/pseudo-version` pins → `v0.53.1` so the entire plugin ecosystem is current with the post-cloud-SDK-extraction workflow tag. Mechanical sweep — no API redesign, no SDK extension. Closes #656's `engine pin sweep` half. Defers the `host conformance` half (gcp #6 + azure #4) and the `v2 action lifecycle migration` (#640) to separate design passes.

## Architecture

Per-plugin parallel PRs across 8 repos. Each PR is single-task: bump `go.mod` workflow pin → `v0.53.1` + `go mod tidy` + verify build + verify tests + bump `plugin.json minEngineVersion` to `"0.53.0"` (or add if missing) + cut new patch/minor tag + release.

8-PR cluster, one PR per plugin repo. No cross-repo dependency ordering — each repo is independently shippable. Implementer-1/-2/-3 from existing `cloud-sdk-bcd` team can claim across repos in parallel; verification gate per-plugin keeps blast radius bounded.

```
parallel: PR1 (payments)  PR2 (audit-chain)  PR3 (tofu — first release)
          PR4 (ci-generator)  PR5 (agent)  PR6 (github)
          PR7 (gitlab)  PR8 (azure — pseudo-version → clean tag)
```

## Components

### Per-PR scope (identical 5-step pattern, 8 PRs)

1. `go.mod` pin bump: `github.com/GoCodeAlone/workflow vOLD → v0.53.1`
2. `GOWORK=off go mod tidy` — refresh transitive deps
3. Build + test verification: `go build ./... && go test ./... -race`
4. `plugin.json` `minEngineVersion: "0.53.0"` (add if missing — only `agent` per #656; verify against current state)
5. Tag + release: GoReleaser-driven via existing `.github/workflows/release.yml` per repo

### Per-repo specifics

| # | Plugin | Old pin | Old tag | New pin | New tag | minEng action |
|---|--------|---------|---------|---------|---------|---------------|
| 1 | workflow-plugin-payments | v0.51.6 | v0.4.5 | v0.53.1 | v0.4.6 | confirm `0.51.2` → `0.53.0` |
| 2 | workflow-plugin-audit-chain | v0.51.6 | v0.2.3 | v0.53.1 | v0.2.4 | confirm `0.51.5` → `0.53.0` |
| 3 | workflow-plugin-tofu | v0.51.7 | (none) | v0.53.1 | v0.1.0 (first release) | confirm `0.51.7` → `0.53.0` |
| 4 | workflow-plugin-ci-generator | v0.51.7 | v0.1.3 | v0.53.1 | v0.1.4 | confirm `0.51.7` → `0.53.0` |
| 5 | workflow-plugin-agent | v0.51.7 | v0.9.2 | v0.53.1 | v0.9.3 | confirm `0.51.7` → `0.53.0` |
| 6 | workflow-plugin-github | v0.51.7 | v1.0.3 | v0.53.1 | v1.0.4 | confirm `0.51.7` → `0.53.0` |
| 7 | workflow-plugin-gitlab | v0.51.7 | v1.0.2 | v0.53.1 | v1.0.3 | confirm `0.51.7` → `0.53.0` |
| 8 | workflow-plugin-azure | v0.51.11-pseudo | v1.1.1 | v0.53.1 | v1.1.2 | confirm `0.52.0` → `0.53.0` |

(`workflow-plugin-aws v1.1.0`, `workflow-plugin-gcp v1.1.0`, `workflow-plugin-digitalocean v1.1.0` already on v0.52.0+/v0.53.0 pins — out of scope.)

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
- Reports back to team-lead; that plugin's PR pauses; the other 7 PRs continue.

**Per-plugin test failure on bump** — same handling: capture, file upstream, pause that plugin.

**GoReleaser failure** (azure pattern from prior session — release published as draft) — handled in-line via `gh release edit vX.Y.Z --draft=false --latest`.

**No release infrastructure** (workflow-plugin-tofu — no prior release tag) — verify `.github/workflows/release.yml` exists; if missing, file as scope-extension to add release workflow before tag.

## Testing

- **Per-plugin build verification** — `GOWORK=off go build ./...` clean (workflow-side test target uses GOWORK=off; plugins should NOT need it but defensive).
- **Per-plugin test run** — `go test ./... -race` PASS.
- **Per-plugin GoReleaser dry-run** — `goreleaser release --snapshot --skip=publish --clean` (per-plugin) for tofu's first-release case.
- **Cross-plugin smoke** — after all 8 ship, `wfctl plugin list` against a representative consumer (e.g. BMW or core-dump) confirms all show `latest tag` matching the new releases.

## Out of scope (intentional non-goals — separate future design passes)

- **gcp #6 + azure #4 host conformance** — requires conformance test infrastructure (Plug + ExternalPluginManager subprocess invocation + RPC verification); not a pin-bump concern.
- **#640 v2 action lifecycle migration** — substantive scope (5 invariants in issue body), needs its own brainstorm.
- **Catalog manifest-derivation** — schema/manifest/wfctl/UI/MCP refactor; high blast radius.
- **TypedProvider migration for the 5 plan-2 types** — SDK scaffolding ready (PR #686), waits for first consumer.
- **MessagePublisher/MessageSubscriber for IaC-bridge modules** — decisions/0038 Non-Goal; requires SDK extension.
- **aws-sdk-go-v2 extraction from `provider/aws/`/`plugin/rbac/aws.go`/`iam/aws.go`/`artifact/s3.go`** — too large for this cycle.
- **godo extraction** — already verified absent from workflow core go.mod; no work needed.
- **Phase B RLV doc** — non-blocking nicety, separate.

## Assumptions

1. **`sdk.Serve` + `sdk.ServePluginFull` surfaces still present in workflow v0.53.1.** Verified by inspection of `plugin/external/sdk/serve.go` + `serve_full.go` on `origin/main`. If false, bumps break catastrophically.
2. **No silent strict-contracts requirement for non-IaC plugins.** Strict-contracts cutover (force) targeted IaC plugin contracts; non-IaC ServePluginFull surface untouched. Verified by ADR 0024 + observation that azure/aws/gcp/DO already shipped via the IaC path. If false, every non-IaC plugin needs a typed-Provider migration before this sweep ships.
3. **Per-plugin GitHub Actions release workflow exists** for 7 of 8 plugins (tofu unverified — flag for confirmation in Task 3).
4. **`minEngineVersion: "0.53.0"` is the right floor** — workflow v0.53.0 was tagged 2026-05-15 carrying the SDK extension; v0.53.1 is a patch on top. Plugins that don't use the SDK extension can stay on `"0.53.0"`. (We are NOT bumping minEng to v0.53.1 since these plugins don't need the v0.53.1 patch behavior; semver minimum-floor convention.)
5. **GoReleaser configurations match prior pattern** — all 8 plugins ship via `goreleaser release --clean` triggered by tag push (see ADR 0034); azure uses `runs-on: ubuntu-latest` post fix; if any plugin still uses `[self-hosted, Linux, X64]` on a public repo, that's surfaced + fixed in-line.
6. **`workflow-plugin-tofu` has no release tag yet** — first-release semantics use `v0.1.0` per repo convention; if the repo has uncommitted work-in-progress preventing release, that surfaces in Task 3 verification.
7. **Pseudo-version pin replacement is mechanical** for azure — `replace` directive replaced + `go mod tidy` resolves to clean v0.53.1 tag. If azure has divergent commits beyond the pseudo-version's base, additional work surfaces.

## Self-challenge round (top 3 doubts surfaced)

1. **Hidden API drift in non-IaC plugins.** 35 commits / 210 files changed between v0.51.6 + v0.53.1. Even if `sdk.Serve*` signatures are stable, peripheral surface (e.g., handler types, plugin registration helpers) may have shifted. Per-plugin verification CATCHES this; risk is per-plugin pause + upstream-issue overhead, not silent breakage.
2. **`workflow-plugin-tofu` first-release scope creep.** Tofu has no prior release, so cutting `v0.1.0` requires verifying the repo has a buildable + testable + release-workflow-ready state. May surface as multi-task scope extension. Mitigation: Task 3 has explicit "verify release.yml present + buildable" pre-step; if fails, scope-pause + file as separate followup.
3. **Operator availability during 8-PR-parallel-execution.** Cloud-SDK-bcd team has 3 implementers; 8 PRs in parallel = each implementer owns 2-3. Compaction across 8 PRs in one team session is heavy. Mitigation: per-PR is small (single commit + tag), low review surface, code-reviewer can sweep approvals fast.

## Rollback

Per-plugin rollback: each plugin's tag bump is independently revertable.

If a plugin's release ships then a downstream consumer breaks:
- Operator OR autonomous follow-up reverts the affected plugin's pin commit + cuts a `vX.Y.Z+1` tag re-pinning to the previous workflow tag (v0.51.6 / v0.51.7 / pseudo).
- Old plugin tag (vX.Y.Z) is permanent in the Go proxy + can't be deleted, but `wfctl plugin install` resolves to `latest` so consumers pick up the rollback tag automatically.
- This is the same per-plugin matched-pair rollback pattern as plan-2 PR 4/5 (workflow core deletion + plugin v1.1.0 release as matched pair).

If `workflow v0.53.1` ITSELF needs revert (extremely unlikely — already shipped + adversarial-reviewed): the entire 8-plugin sweep reverts as a CASCADE, each plugin re-pins to v0.51.x, ships a new patch tag.

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
