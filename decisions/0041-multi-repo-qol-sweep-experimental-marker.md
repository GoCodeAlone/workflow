# ADR 0041: Multi-Repo QoL Sweep — Experimental-Status Marker for Unverified Plugins

**Date:** 2026-05-19
**Status:** Accepted
**Author:** team-lead (autonomous-mode)

## Context

A new external user adopted the workflow project. The codebase was built with OSS adoption in mind, but a fresh audit reveals 50 public plugin repos, of which only 7 are exercised by merged main-branch usage in any active GoCodeAlone-internal project (`buymywishlist`, `core-dump`, `workflow-compute`, `ratchet`). The other 43 — including frequently-discussed cloud providers (`aws`, `gcp`, `azure`) and `analytics` (currently pinned in unmerged BMW worktrees only) — compile and pass unit tests but have never been validated end-to-end in production usage.

We had to choose how to surface this state to external users. Three options:

1. **Hide it.** Treat all public plugins as equally supported and let users discover the gap themselves.
2. **Deprecate the unverified plugins** until they get production validation. This is too aggressive — many of these plugins are well-tested in isolation.
3. **Add an `experimental` status marker** to the registry manifest + a README banner. Surface the verification gap honestly without taking working code offline.

## Decision

Option 3. Each unverified plugin gets:

- `"status": "experimental"` in `workflow-registry/plugins/<name>/manifest.json`.
- A README banner explaining the verification gap in plain language.
- The wfctl marketplace and the static API JSON expose `status` so users see it before install.

Verified plugins get `"status": "verified"`. The verified-vs-experimental distinction is **about GoCodeAlone-internal production usage**, not code quality. A plugin can be `experimental` while passing 100% of its own unit tests.

## Rationale

- **Trust signal**: an external user choosing between two plugins should know which has production miles.
- **No regression**: existing wfctl clients ignore unknown manifest fields, so the rollout is additive.
- **Low-cost upgrade path**: when a plugin gets adopted in a GoCodeAlone project, a one-line manifest change promotes it to verified.
- **Honest defaults**: future plugins ship as `experimental` until used in anger.

## Alternatives Rejected

- **Deprecate-or-promote**: rejected as too aggressive; we lose value by hiding working code.
- **Per-plugin maturity matrix in the registry README only**: too far from the install point. Users scan the marketplace, not the README.
- **A separate "blessed plugins" list**: easy to drift out of sync with the registry; the manifest is already the canonical schema, so the status belongs there.

## Consequences

- 43 public plugins gain an `experimental` banner this sweep — a visible downgrade. Phrasing emphasizes verification gap not code quality.
- Future plugins ship `experimental` by default unless an active GoCodeAlone project pins them.
- Maintenance: when verification state changes, both manifest and README banner must be kept in sync. Mitigation: a `wfctl plugin verify <name>` subcommand could automate this in a follow-up, but is out of scope for this sweep.

## Autonomous-Mode Bypass of User-Approval Gate

This sweep was authored under the brainstorming skill, which mandates user approval before implementation. The user explicitly granted autonomy for this session ("Proceed autonomously, I won't be around to approve things"). The bypass is recorded here so future contributors understand the decision trail. All locked-manifest invariants (`scope-lock`, alignment-check) still apply; only the human-approval gate is bypassed.

## References

- `docs/plans/2026-05-19-multi-repo-qol-sweep-design.md` — design doc this ADR backs
- `workflow-registry/schema/registry-schema.json` — schema being extended
- `feedback_continuous_autonomous_phases` — autonomous-mandate precedent
