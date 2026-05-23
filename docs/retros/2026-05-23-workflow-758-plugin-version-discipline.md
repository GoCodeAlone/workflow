# workflow#758 — plugin version discipline (Layer 1+2+3 pilot)

Date: 2026-05-23
Status: Pilot complete; Layer 3b sweep (56 repos) tracked in workflow#760

## Summary

Eliminated the `sync-plugin-version.yml` PR pileup by deleting the sync mechanism, surfacing the build-time-injected runtime version through `GetManifest`, and gating non-semver tags at both `wfctl plugin validate-contract --for-publish` and registry sync.

## Shipped

| Layer | Repo | PR | Merge SHA | Notes |
|-------|------|----|-----------|-------|
| 1 | workflow | #759 | 43153f6518 | sdk.ResolveBuildVersion + IaCServeOptions.BuildVersion + sdk.WithBuildVersion + wfctl plugin validate-contract; tagged v0.61.0 |
| 2 | workflow-registry | #110 | 940ecc405c | tag-string semver gate in sync-versions.sh |
| 3 pilot | workflow-plugin-digitalocean | #165 | 0568b5b01e | canonical reference |
| 3 pilot | workflow-plugin-aws | #26 | 52f2af5fa7 | per-repo Version-var variance |
| 3 pilot | workflow-plugin-gcp | #19 | 641555b664 | renamed `provider.ProviderVersion` → `provider.Version` to satisfy validate-contract ldflag regex |
| 3 pilot | workflow-plugin-azure | #22 | 7b02379e46 | IaC |
| 3 pilot | workflow-plugin-github | #19 | 925e7bfafc | non-IaC; needed nested `capabilities` block in plugin.json |

## Pipeline

- **Brainstorming**: 4 design cycles to PASS
  - Cycle 1 (drop plugin.json.version): 3 Critical (engine load-order, downloads URL rewrite, --for-publish version source)
  - Cycle 2 (pivot to keep field): 2 new Critical (NC1 user-intent drift on ldflag contract, NC2 branch-test version surface)
  - Cycle 3 (restored contract): 5 Important
  - Cycle 4-revC: PASS
- **Writing-plans**: cycle 4-P1 PASS with 6 Important fixes applied inline
- **Alignment-check**: PASS
- **Scope-lock** at 2026-05-23T20:08:47Z

## Per-repo variance observed

Layer 3 fan-out surfaced predictable variance the design accommodated:

1. **Version var name**: `internal.Version` (DO, github), `provider.Version` (GCP, renamed), `provider.ProviderVersion` (AWS originally). validate-contract greps the goreleaser ldflag for `-X .*\.Version=` — symbol path doesn't matter as long as the suffix matches.
2. **main.go path**: `cmd/plugin/main.go` (DO), `cmd/workflow-plugin-<name>/main.go` (AWS/GCP/Azure/github). validate-contract scans all `cmd/**/main.go`.
3. **Non-IaC plugins** (github): `sdk.Serve` + `WithBuildVersion`, not `IaCServeOptions.BuildVersion`. Single contract regex accepts both.
4. **plugin.json capabilities shape**: github had flat top-level fields (`moduleTypes`, `stepTypes` at root); validate-contract Check 2 required a nested `capabilities:` block. Future Layer 3b PRs may need the same restructure.
5. **Goreleaser before-hook variance**: ~50 plugins use in-place `sed`, ~4 use `.release/plugin.json`. Layer 3 release.yml step-7 falls back: `.release/` if present, else `.` (in-place).
6. **Test files**: any test asserting committed-version-vs-download-URL invariants breaks under sentinel `"0.0.0"` and must be retired or rewritten to assert structural archive invariants only.

## What worked

- **4 design adversarial cycles**: cycle 1 surfaced 3 Critical that would have made the original plan unbuildable. Subsequent cycles caught real defects (regex narrowness, struct-shape mismatch, tarball-postcheck missing). PASS required mechanically verified ground truth (e.g. running `ParseSemver("v0.0.0-dev")` empirically; reading actual `*grpcServer` shape vs imagined `serveConfig`).
- **Parallel agent fan-out**: 4 sub-agents migrated AWS, GCP, Azure, github in parallel after DO canonical landed. Total wall-time from agent dispatch to all-5-merged: ~14 minutes.
- **Tag-arrival heartbeat preserved**: prior G1 chain (plugin tag → notify dispatch → registry sync, shipped 2026-05-21) means sync-plugin-version.yml deletion didn't lose any operator-visible signal. Verified by reading workflow-registry/scripts/sync-versions.sh.
- **wfctl validate-contract as the central gate**: collapses what an earlier cycle had as curl|bash supply-chain (C4 in plan adversarial); single binary distributed via setup-wfctl action; same regex enforced operator-side AND registry-side.

## What surprised

- **`runtime/debug.ReadBuildInfo().Main.Version` doesn't return release tag for goreleaser-built binaries** (cycle 4-A2 N-C1). Goreleaser invokes `go build` not `go install`; `Main.Version` is pseudo-version. Only ldflag (`-X .Version=`) delivers the tag. The cycle-3 design briefly considered a no-arg `sdk.BuildVersion()` helper based on this surface — caught by empirical adversarial-review verification before any code shipped.
- **`PluginManifest.ParseSemver` strict M.m.p only** — rejects `v0.0.0-dev`, `v1.2.3-rc.1`, anything with prerelease segments. Sentinel chose flat `"0.0.0"` to side-step. Prerelease tag publishing deferred to a separate design that updates ParseSemver + sync-versions in concert.
- **github plugin's flat capabilities shape**: validate-contract Check 2 (`capabilities populated, non-empty`) failed initially because github's plugin.json had top-level `moduleTypes` + `stepTypes` instead of nested under `capabilities`. Pattern restructure was a 1-PR step but pre-flight audit didn't catch it. Layer 3b agents should verify nested `capabilities` shape and restructure if needed.

## Deferred

- **Layer 3b sweep (56 repos)** — tracked in workflow#760. User authorization required before dispatching parallel agents.
- **Full SemVer 2.0.0 prerelease support** — requires concerted ParseSemver + sync-versions + wfctl install update; deferred to separate design.
- **Binary-vs-file capability freshness gate at contract-check time** — `wfctl plugin validate-contract --for-publish` could spawn the just-built binary, call its `GetContractRegistry` RPC, diff vs committed plugin.json. Catches stale `capabilities` at tag time. Deferred (cycle 4-A1 I3).
- **Gap-repos** (no release pipeline today) — separate per-repo "establish release pipeline" issues. Not included in the 56-count.

## Operator-visible changes

- `wfctl plugin info <name>` now reports the binary's runtime version (ldflag-injected) instead of the disk plugin.json `.version`. For local dev installs from a non-release tag, operators see `(devel) [@ shortsha[.dirty]]` instead of a stale committed version.
- `plugin.json.version` on committed `main` is now a sentinel `"0.0.0"` for all migrated plugins. Goreleaser before-hook continues to write `{{ .Version }}` into the tarball.
- `wfctl plugin validate-contract` is the new operator-facing gate. `--for-publish` enforces strict-semver tag format; `--release-dir` verifies the shipped tarball carries the tag.

## Links

- Issue: GoCodeAlone/workflow#758
- Layer 3b follow-up: GoCodeAlone/workflow#760
- Design: `docs/plans/2026-05-23-plugin-version-discipline-design.md`
- Plan: `docs/plans/2026-05-23-plugin-version-discipline.md`
- Operator docs: `docs/PLUGIN_RELEASE_GATES.md`
