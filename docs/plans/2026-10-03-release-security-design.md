# Workflow Release Security Maintenance

**Status:** Unfinished; scanner rollout blocked by D6. Source-only slice has its own design/lock; no six-PR execution is authorized by this draft.
**Baseline:** main `49e1803424f303f6f98cef51cc9ea2dee96c7d38`.
**Boundary:** independent Workflow maintenance, not a Signal feature or fleet migration.

## Goal

Remediate fixable shipped/library and npm findings and restore trustworthy verification
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
  progress at design drafting; it subsequently reproduced the timeout at a
  later capability fixture. Newer main `49e18034` passed on its second attempt.
  Repeated real-host compilation consumes aggregate budget, not a stuck case.
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
- R1: Raise root, example, and plugin-fixture Go floors to 1.26.8; update
  active source-fixture compiler overrides and runnable documentation. Existing
  CI setup-go drivers remain unchanged: Go's authenticated default toolchain
  selection must choose 1.26.8 for these modules, verified in CI and actual
  artifact build metadata. Explicit workflow driver/fleet migration is deferred.
  Do not change the independently hash-bound policytool module/wrapper in this
  boundary. Its old stdlib-floor findings require an exact Go 1.26.5
  govulncheck/package-import proof of no affected calls, not an ignore.
  Update affected OpenTelemetry packages to at least
  fixed 1.45.0; repair vulnerable gRPC/x-text fixture pins using the already
  established root versions. Preserve module boundaries and negative fixtures.
- R2: Refresh only affected npm dependency resolutions, preserving compatible
  declared ranges where possible. Cover UI runtime `js-yaml` and affected
  development packages. No blanket `npm update`, major migration, or new app.
- R3: Use official OSV action v2.6.0, index digest
  `sha256:71ad04ab2f8798be47870f9b18817ad317c2f8f2f97aa6726ba10d5578bc174a`
  (Linux/amd64 `sha256:13cef841c7b8de79248e572de0c278d64eaf0a4006e2973fa690b777be30eee4`).
  Its verified config supplies Go 1.27.1 with `GOTOOLCHAIN=local`, sufficient
  to load the 1.26.8 modules. This is scanner tooling, not a runtime/fleet minor
  migration. Keep reporter and fail-on-vulnerability behavior; verify real analysis
  succeeds, retains raw package findings, classifies uncalled packages, and
  still rejects called vulnerabilities. No ignore/config suppression or
  `--fail-on-vuln` relaxation. Retain analyzed JSON as well as SARIF.
- R4: Reuse only one immutable source-built record host (`target=".", race=true`)
  per test process, lazily, with exact compiler/build-environment and cwd
  matching; changed inputs use the uncached builder. Explicit prebuilt
  selection/denial happens first. Copy to distinct mode-0700 test paths and
  clean the process-owned build directory before exit. Keep SDK/fake-Docker/
  registry/mutant/capability builds independent. Preserve every case, race
  check, build/child/fault deadline, GORACE option, and the existing default
  package ceiling. Prove the whole cold race/coverage suite completes within
  600s; otherwise stop and backport that failed assumption before changing scope.
- R6: Follow `docs/public-workflow-policy.md`: source-only remediation PR,
  OSV context staging PR, policy authority staging PR for the mutation harness
  hash only, combined already-authorized adoption PR, OSV promotion PR, and
  authority promotion PR. Six independent PRs; no same-PR trust/adoption,
  wrapper/module change, old staged-CI replacement, runner or token change.
- R5: Source and CI checks, real Workflow HTTP/YAML startup, SDK plugin record
  matrices, live strict sandbox, registry CLI fixtures, UI tests/build, nested
  module tidy checks, and local/CI scanner proof must pass before merge.
  This boundary does not create a stable release tag; the existing release
  task subsequently verifies downloaded artifacts at fetched green main.
  Concurrent v0.86.1 at `49e18034` is not retagged; maintenance completion does
  not claim it is patched. A later verified patch supersedes it for promotion.

## Global Design Guidance

Use repository `AGENTS.md`, `CLAUDE.md`, `docs/AGENT_GUIDE.md`,
`docs/REPO_LAYOUT.md`; existing dependency-update automation and public workflow
policy in `docs/public-workflow-policy.md` remain authoritative. Reuse OSV/govulncheck, module parsers, npm's lock
resolver, and policytool. No new plugin, CLI, provider, or scenario capability.

## Security Review

- Fix upstream vulnerabilities, including development/build dependencies;
  do not dismiss a finding solely because it is marked dev.
- OpenPGP is not Signal encryption. Do not remove all `x/crypto`, blanket-ignore
  the advisory, or call an unsuccessful reachability scan clean.
- Runtime compiler selection uses Go's authenticated module-floor mechanism
  with the unchanged older CI driver; exact build metadata must show 1.26.8.
  Scanner/compiler assets remain digest/authentication bound; no unverified
  executable or host trust modification. No new GOTOOLCHAIN policy exception.
- Existing automatic repository token only; no cloud mutation, new secret,
  broader permission, self-hosted runner, or public/private boundary change.

## Infrastructure Impact

Public GitHub-hosted Linux runners unchanged. Compiler/dependency analysis can
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
| policy allowlists + workflows | config-only | trusted-base stage/adopt/promote validation and existing mutation suite; wrapper bytes unchanged |
| fleet/toolchain minor migration, Signal releases | deferred | existing owners/plans; not this PR |

## Assumptions

| ID | Claim | Failure handling |
|---|---|---|
| A1 | Supported Go patch preserves plugin/API behavior | Real host matrix; hold release on regression |
| A2 | Updated pinned analyzer can load the declared module floor | Execute actual container analysis; no success claim on error |
| A3 | Affected npm fixes fit current major ranges | Inspect resolver diff; isolate required range changes and explain |
| A4 | OpenPGP absent from shipped imports | Check Linux/Darwin amd64/arm64 and Windows amd64 release package graphs/tags; native OSV classification separately scoped |
| A5 | Removing four duplicate host builds leaves cold suite below 600s | Retain case deadlines and complete full race/coverage suite; otherwise hold, not silently extend timeout |
| A6 | Policytool's affected stdlib calls are absent | Run govulncheck with its exact 1.26.5 compiler and verify imports; any called finding blocks publication |

## Self-Challenge

- A passing differential scan does not prove a clean baseline: require full scan.
- Native analysis may silently fail: require successful analysis evidence plus
  a called-vulnerability positive control, not only reporter exit zero.
- Reusing a host can hide changed build inputs: require exact environment/cwd
  matching, independent mutant builds, no prebuilt fallback, and real matrices.
- Vulnerable control fixtures must be materialized outside the recursively
  scanned checkout and cleaned afterward, with no scanner exclusions/ignores.

## Rollback

Revert source dependency changes through reviewed commits and hold promotion;
scanner/workflow authority rollback uses the same stage/adopt/promote lifecycle,
never a direct hash rewrite. Rerun source, policy, UI, scanner, and runtime checks.
No tag moves, registry rollback, deployment, or data migration is involved.

## Review Backport 2026-10-03

D1-D3 (Minor): enumerate release platforms; retain analyzed JSON; keep vulnerable
positive controls outside the scan root. Implemented in R3/A4/self-challenge.

D4-D5 (Important, discovered after the first pass): the initial one-PR hash
refresh violates existing staged authority; explicit GOTOOLCHAIN is categorically
forbidden. Use six already-authorized transitions and a current upstream image,
without changing the policy implementation or wrapper. The policytool module is
not silently patched because that would change executable pins in unrelated
current/staged CI contexts. All Signal manifest/task boundaries remain unchanged.

D6 (Important, unresolved): the mutation harness itself is executable-pinned by
active and staged CI. Its digest constant cannot change through authority-bundle
adoption alone. Existing active CI `532f...` and unadopted staged CI `37b...`
must be reconciled by the owner before a replacement context can authorize new
harness bytes. No hash rewrite, fake old-image comment, harness skip, or trust
exception is permissible. Operator clarification requested; do not execute the
six-PR schedule above. Independently executable source remediation is designed
in `2026-10-03-release-security-source-design.md`.
