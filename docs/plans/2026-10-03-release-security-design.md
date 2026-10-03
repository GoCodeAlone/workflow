# Workflow Release Security Maintenance

**Status:** Approved under standing autonomous follow-up authority; design review pending.
**Baseline:** main `49e1803424f303f6f98cef51cc9ea2dee96c7d38`.
**Boundary:** independent Workflow maintenance, not a Signal feature or fleet migration.

## Goal

Remove fixable dependency findings and restore trustworthy release verification
before publishing the next stable Workflow patch. Keep the current Go minor,
existing runtime/API contracts, scanner fail-on-vulnerability behavior, and
credential-free public CI.

## Evidence

- PR #1020 passed all 25 checks; merged source `6d3cf77` adds no dependency or
  CI changes. Main OSV run `37138999915` and previous-main `37101607116` have
  byte-equal normalized advisory/package/version/source rows: 159 findings,
  157 fixable. Unchanged findings are not a security exception.
- Every Go analysis attempt in run `37138999915` failed: the digest-pinned
  scanner's Go 1.26.2 and `GOTOOLCHAIN=local` cannot load Go 1.26.5 modules.
- Main CI `37138999955` timed out at the default 10m package ceiling near the
  end of wfctl; runtime matrices passed. One identical-source rerun is in
  progress. Coverage plus cold fixture builds needs an explicit bounded budget.
- [Official Go downloads](https://go.dev/dl/?mode=json) lists supported stable
  Go 1.26.8 and 1.27.1. Stay on 1.26.8; a 1.27/fleet migration is unrelated.
- [OSV Go analysis](https://github.com/google/osv-scanner/blob/main/docs/scan-source.md)
  uses govulncheck and requires a working compiler. Restore analysis rather
  than adding vulnerability ignores. `GO-2026-5932` is OpenPGP-only and has no
  fixed version; prove package absence and native analysis classification.

## Approach

| Option | Trade-off | Decision |
|---|---|---|
| Publish unchanged baseline | Fast; knowingly retains fixable runtime/UI findings and incomplete analysis | Reject |
| Workflow-only supported patch maintenance | Targeted pins/locks, real runtime proof, independent review | Choose |
| Workspace-wide Go 1.27 migration | Much larger consumer/runner blast radius | Defer |

Requirements:
- R1: Pin active Workflow Go modules, CI/release/compiler examples, and Docker
  build versions to 1.26.8. Update affected OpenTelemetry packages to at least
  fixed 1.45.0; repair vulnerable gRPC/x-text fixture pins using the already
  established root versions. Preserve module boundaries and negative fixtures.
- R2: Refresh only affected npm dependency resolutions, preserving compatible
  declared ranges where possible. Cover UI runtime `js-yaml` and affected
  development packages. No blanket `npm update`, major migration, or new app.
- R3: Keep the existing digest-pinned OSV action and reporter. Explicitly use
  `GOTOOLCHAIN=go1.26.8`/`GOWORK=off` for Go analysis; verify real analysis
  succeeds, retains raw package findings, classifies uncalled packages, and
  still rejects called vulnerabilities. No ignore/config suppression or
  `--fail-on-vuln` relaxation. If the pinned analyzer cannot support this
  toolchain, stop and backport the demonstrated limitation before changing it.
- R4: All full Go race suites in CI, snapshot, and stable release receive a
  documented 30m per-package ceiling. Keep all tests, assertions, race checks,
  and each fault-case deadline unchanged. Update exact policy hashes through
  existing policy tooling, not exemptions. No runner or token policy changes.
- R5: Source and CI checks, real Workflow HTTP/YAML startup, SDK plugin record
  matrices, live strict sandbox, registry CLI fixtures, UI tests/build, nested
  module tidy checks, and local/CI scanner proof must pass before merge.
  This boundary does not create a stable release tag; the existing release
  task subsequently verifies downloaded artifacts at fetched green main.

## Global Design Guidance

Use repository `AGENTS.md`, `CLAUDE.md`, `docs/AGENT_GUIDE.md`,
`docs/REPO_LAYOUT.md`; existing dependency-update automation and public workflow
policy remain authoritative. Reuse OSV/govulncheck, module parsers, npm's lock
resolver, and policytool. No new plugin, CLI, provider, or scenario capability.

## Security Review

- Fix upstream vulnerabilities, including development/build dependencies;
  do not dismiss a finding solely because it is marked dev.
- OpenPGP is not Signal encryption. Do not remove all `x/crypto`, blanket-ignore
  the advisory, or call an unsuccessful reachability scan clean.
- Compiler downloads use Go's authenticated toolchain mechanism; no floating
  `auto` pin, unverified executable, or host trust modification.
- Existing automatic repository token only; no cloud mutation, new secret,
  broader permission, self-hosted runner, or public/private boundary change.

## Infrastructure Impact

Public GitHub-hosted Linux CI unchanged. Cold compiler/dependency analysis can
increase duration; test jobs remain bounded. No deploy, external service API,
database migration, runtime infrastructure, or production resource mutation.

## Multi-Component Validation

| Integration | Class | Proof |
|---|---|---|
| Go compiler/runtime + server | runtime-integrated | built server starts real YAML HTTP app; representative request returns expected JSON |
| wfctl + SDK/native plugin + Docker | runtime-integrated | existing real-host record/composite/custody matrices and live strict sandbox |
| wfctl + registry + Git | runtime-integrated | real CLI forward/no-op/rollback and negative snapshot matrix |
| UI + YAML parser | runtime-integrated | UI suite/build; real parser rejects hostile merge input with fixed release |
| OSV + Go package loader + reporter | runtime-integrated | native analysis succeeds; clean graph accepted and intentionally called vulnerable fixture rejected |
| policy allowlists + workflows | config-only | regenerated exact hashes; existing positive/mutation policy suite |
| fleet/toolchain minor migration, Signal releases | deferred | existing owners/plans; not this PR |

## Assumptions

| ID | Claim | Failure handling |
|---|---|---|
| A1 | Supported Go patch preserves plugin/API behavior | Real host matrix; hold release on regression |
| A2 | Existing pinned analyzer can load patch-compatible compiler | Execute actual container analysis; no success claim on error |
| A3 | Affected npm fixes fit current major ranges | Inspect resolver diff; isolate required range changes and explain |
| A4 | OpenPGP absent from shipped imports | Check actual package graph; called result remains blocking |
| A5 | Timeout is aggregate budget, not a hung test | Inspect full stack, retain case deadlines, complete full suite |

## Self-Challenge

- A passing differential scan does not prove a clean baseline: require full scan.
- Native analysis may silently fail: require successful analysis evidence plus
  a called-vulnerability positive control, not only reporter exit zero.
- Raising the package ceiling can mask hangs: 30m is finite; existing subprocess,
  fault-case and command deadlines remain intact; no flaky assertions removed.

## Rollback

Revert the one maintenance PR and rerun source, policy, UI, scanner, and runtime
checks; the dependency vulnerabilities return and publication stays held.
No tag moves, registry rollback, deployment, or data migration is involved.
