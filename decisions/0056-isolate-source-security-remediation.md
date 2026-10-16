# 0056. Isolate source security remediation

**Status:** Accepted
**Date:** 2026-10-03
**Decision-makers:** Jon Langevin (standing autonomous follow-up approval)
**Related:** supersedes 0055; `docs/plans/2026-10-03-release-security-source-design.md`

## Context

The attempted six-PR scanner schedule missed independently pinned mutation
harness bytes in active and unadopted staged CI. Updating the authority bundle
does not replace those executable pins. Rewriting that staged context would
cross another rollout's ownership; operator reconciliation was requested.

## Decision

Proceed with independently executable Go/UI security and test-host build-reuse
changes in one reviewed source-only PR. Run actual host and supported upstream
analysis locally; preserve CI authority and disclose the existing scanner gap.
Do not claim scanner CI fixed or Signal/release scope complete.

Retain the unfinished scanner design for owner reconciliation, not execution.
Rejected: holding production source fixes unnecessarily, weakening the policy,
fake old-image references, or silently expanding the approved Signal manifest.

## Consequences

This patch changes no CI permission, workflow or staged bytes and creates no
release tag. Runtime/analysis failures remain blocking gates. A later verified
release and scanner rollout are separate remaining work, not source-only proof.
