# Workflow Source Security Remediation

**Status:** Approved; design review PASS with two proof refinements incorporated.
**Baseline:** main `49e1803424f303f6f98cef51cc9ea2dee96c7d38`.
**Authority:** independent source-only maintenance. Existing Signal scope unchanged.
**Decision:** `decisions/0056-isolate-source-security-remediation.md`.

## Goal And Requirements

Ship reviewed, patched Workflow source and reliable real-host tests without
modifying protected CI authority, unrelated staged work, or release tags.

- S1: Root, example, and external-plugin fixture Go floors become 1.26.8;
  affected OTel modules become 1.45.0; vulnerable gRPC/x-text fixture pins use
  established root 1.83.2/0.42.0. Tidy each owning module. Source fixture
  compiler overrides and runnable minimum-version docs match 1.26.8.
- S2: Patch the ten affected npm package families within existing compatible
  ranges, preserving unrelated lock resolutions and direct dependencies.
  Add a real js-yaml merge-budget regression and exercise the actual imported
  `@gocodealone/workflow-editor/utils` `parseYaml`; run the UI suite/build.
- S3: Lazily reuse only the immutable source-built record host (`.`, race),
  matching cwd, compiler and complete build environment. Changed inputs use
  the original uncached path. Explicit downloaded-binary selection/denial runs
  first. Copy to distinct mode-0700 paths; remove the owned cache before exit.
  SDK, Docker, registry, mutant, and capability builders remain independent.
  Preserve GORACE, all deadlines/cases and the default 600s package limit.
- S4: Prove original-source failures, new cache semantics, full cold
  race/coverage completion, Go package/build metadata, actual Workflow HTTP
  startup, SDK/composite/custody matrices, strict Docker and registry matrices,
  UI build/test and version-skew audit. New source must be lint-clean.
- S5: Preserve CI/workflow/policytool bytes and staged contexts. Exact Go
  1.26.5 policytool reachability proof is required. Independently run supported
  OSV v2.6.0 against source plus an isolated called-vulnerability control,
  using verified image index digest
  `sha256:71ad04ab2f8798be47870f9b18817ad317c2f8f2f97aa6726ba10d5578bc174a`;
  retain tool identity and analyzed JSON. No scan suppression, ignores or clean-CI claim for the
  old pinned main scanner. Source PR merge needs all required PR checks green.

## Alternatives

| Option | Decision |
|---|---|
| Hold all source fixes until unrelated trust rollout settles | Reject: independent production remediation can proceed |
| Source-only patch with real runtime/native analysis proof | Choose |
| Rewrite staged CI/harness authority or weaken environment policy | Reject: owner reconciliation required |

## Global Design Guidance

Repository `AGENTS.md`, `CLAUDE.md`, `docs/AGENT_GUIDE.md`, `docs/REPO_LAYOUT.md`
and `docs/public-workflow-policy.md` apply. Reuse Go/npm resolvers, existing
runtime fixtures and upstream scanner. No new plugin, CLI, app, framework,
scenario, provider, Go minor migration or CI-policy exception.

## Security Review

- Main/base full scans have identical 159 findings, 157 fixable. Code-based
  triage found plausible shipped TLS/relative-URL DoS and UI YAML CPU DoS;
  unchanged baseline is not risk acceptance. Patch owning dependencies.
- Go drivers in existing CI stay pinned 1.26.5; their authenticated default
  selection must compile the new module floor with 1.26.8. Check actual binary
  build metadata, not the job label. No production tag is created here.
- Policytool Go 1.26.5 and scanner CI rollout stay outside this source boundary;
  prove no affected policytool calls using that exact compiler. Called findings
  or analysis errors block progress. Old main OSV remains a disclosed issue.
- Source cache contains only a test executable in an owned private directory;
  no credentials/input/result data, persistent custody store, or runtime cache.

## Infrastructure Impact

No workflow, runner, IAM/token, deployment, resource or data change. Temporary
local compiler/scanner containers and test services only. Preserve automatic
repository-token policy and concurrent v0.86.1 tag/run.

## Multi-Component Validation

| Integration | Class | Proof |
|---|---|---|
| compiler + Workflow server | runtime-integrated | actual built server/YAML/HTTP request plus compiler metadata |
| wfctl + real SDK/plugin/Docker | runtime-integrated | complete existing host, late-output, composite and crash matrices; live strict sandbox |
| wfctl + registry + Git | runtime-integrated | default-API Linux CLI accepted/rejected snapshot matrix |
| UI + js-yaml | runtime-integrated | actual installed parser regression, UI tests/build |
| upstream OSV + Go loader + reporter | runtime-integrated | successful native analysis, preserved classification, outside-tree called-vulnerability control rejected |
| existing CI policy | config-only | no byte changes; existing positive/mutation suite |
| scanner CI rollout/fleet upgrade/Signal promotion | separate | user approved CI reconciliation; this source-only PR does not perform it; existing Signal tasks remain open |

## Assumptions And Self-Challenge

| ID | Claim | Verification/Failure |
|---|---|---|
| A1 | Four avoided duplicate host builds provide sufficient margin | Whole cold Linux race/coverage suite must finish under 600s; otherwise backport, do not silently extend limits |
| A2 | Cache cannot hide a compiler/environment override | Changed-input and error regressions; explicit prebuilt selection still first |
| A3 | Dependency patches preserve typed SDK/runtime contracts | Real host/HTTP/registry/Docker matrices and all required CI |
| A4 | Module floor drives actual compiler 1.26.8 | Older-driver probe and `go version -m` on built artifacts |
| A5 | Remaining module-only findings are uncalled | Successful analyzed JSON, per-release-platform OpenPGP absence, exact policytool compiler proof; no suppression |

## Rollback

Reviewed revert of the single source-remediation PR, rerun build/runtime tests,
and hold promotion because known vulnerabilities return. No workflow trust,
tag, registry, deployment or data rollback occurs.

### Backport 2026-10-03: Bootstrap Pins And Actual Toolchains

Cause: full skew audit found existing Go 1.26.5 scaffold/generator, Docker
bootstrap and deployment-documentation pins, not just protected CI/policytool.
Changing those product defaults is the separately deferred fleet/generator
boundary; the source-only patch does not silently perform it.

Evidence: root/example/eleven owning fixture floors and relevant fixture
overrides are 1.26.8; old `go1.26.5+auto` driver builds patched source with
actual 1.26.8 metadata. Exact policytool 1.26.5 analysis has eight module-only
stdlib rows and zero called findings; its wrapper passes unchanged. Forcing
`GOTOOLCHAIN=go1.26.5` across the root mutation harness prevents root-level
helper compilation. CI-equivalent authenticated auto selection runs all
mutation assertions, but concurrent worker edits invalidate its production
immutability snapshot; repeat once in a frozen, noncompeting plain checkout.

Correction: audit classifies every residual pin as protected driver, standalone
legacy scaffold/bootstrap, historical example or pending fleet follow-up.
Never call this a completed fleet upgrade. Nested Git worktree artifact VCS
metadata reports the outer checkout: frozen plain-checkout verification is
required for exact-head evidence. Local path/actual CLI and compiler checks
establish source behavior, not a release. Scope: no manifest/task/PR change.

### Backport 2026-10-03: Bundled Editor Parser Prerequisite

Cause: installed editor 0.2.0 embeds its own js-yaml implementation. Updating
Workflow's external lock cannot patch it. Real consumer regression remains
RED (140 tests PASS, one editor hostile-merge test FAIL); direct updated
js-yaml tests and UI build pass.

Correction: independently design/review/patch/release compatible editor 0.2.x,
then resolve it through existing Workflow `^0.2.0` range. Preserve current
Workflow manifest/PR grouping; no parser mock, UI shim, externalization shortcut
or weakened assertion. The adjacent owning-library fix has its own design and
PR, not an unrecorded edit in this source-only manifest.
