# 0055. Bound release security maintenance

**Status:** Superseded by 0056; six-PR scanner schedule disproved before implementation
**Date:** 2026-10-03
**Decision-makers:** Jon Langevin (standing autonomous follow-up approval)
**Related:** `docs/plans/2026-10-03-release-security-design.md`; `docs/public-workflow-policy.md`

## Context

Existing main contains fixable runtime/UI vulnerabilities and an OSV image
whose compiler cannot load current modules. Full CLI coverage/race runs can
exhaust 600s after repeatedly compiling the same host. Public workflow authority
requires staged transitions; Go environment overrides are categorically denied.

## Decision

Patch shipped/library dependencies within Go 1.26 and npm-compatible ranges.
Reuse only identical test-host builds, preserving all runtime proofs and limits.
Use supported, digest-pinned upstream OSV tooling with working native analysis;
retain both raw classification and SARIF. Stage/adopt/promote the scanner workflow
and mutation-harness authority, six PRs in an independent maintenance scope.

Do not weaken environment policy, hide findings, change the policytool wrapper,
replace unrelated staged CI contexts, or undertake a fleet upgrade. Existing
drivers select the module's supported compiler floor; verify actual artifact
metadata. Policytool's old stdlib-floor finding needs exact-compiler no-call proof.

## Consequences

Publication waits for successful runtime and analysis gates, not unchanged
scanner counts. The existing Signal manifest is unchanged. Already-created
v0.86.1 remains immutable; a verified subsequent patch supplies patched consumers.
Any failure of the 600s or native-analysis assumptions requires a design backport,
not another blind retry or an unrecorded scope expansion.
