# 0050. Split plugin install modes

**Status:** Accepted
**Date:** 2026-06-16
**Decision-makers:** maintainer, Codex
**Related:** docs/plans/2026-06-16-plugin-lock-and-domain-migration-design.md

## Context

`wfctl.yaml` is the human-authored plugin manifest. `.wfctl-lock.yaml` is the
generated resolver artifact. Current `wfctl plugin install` prefers an existing
lockfile even when `wfctl.yaml` changed, so a manifest bump can silently install
old plugin versions in CI and local runs.

Package managers separate mutable local install from frozen CI install: npm has
`npm install` versus `npm ci`, Yarn and pnpm use immutable/frozen install flags,
and Cargo exposes targeted lock updates plus locked execution.

## Decision

`wfctl plugin install` will be manifest-aware by default: if `wfctl.yaml` is
newer in meaning than `.wfctl-lock.yaml`, it refreshes the lock and then
installs. CI will use `wfctl plugin ci` or `wfctl plugin install --locked`,
which validates lock/manifest consistency and installs without writing.

Rejected: keep `plugin install` lock-only and require operators to remember
`plugin lock` first. That preserves the observed footgun.

## Consequences

- Local behavior matches common package-manager ergonomics.
- CI gets an explicit no-write command and a clear stale-lock failure.
- Lockfiles need machine-checkable manifest provenance so drift is detected
  without relying on timestamps or human review.
- Repos using `wfctl plugin install` in CI should migrate to `wfctl plugin ci`.
