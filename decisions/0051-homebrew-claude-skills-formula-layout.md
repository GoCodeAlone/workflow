# 0051. Restore claude-skills formula layout

**Status:** Accepted
**Date:** 2026-07-03
**Decision-makers:** Codex
**Related:** docs/plans/2026-07-03-wfctl-install-lifecycle-design.md, docs/plans/2026-07-03-wfctl-install-lifecycle.md

## Context

The tap README claimed `claude-skills` was installable, but `brew info gocodealone/tap/claude-skills` failed after `brew update`. The formula exists only as `claude-skills.rb` at the tap root, while current Homebrew resolves formulas from `Formula/`.

## Decision

Move `claude-skills.rb` to `Formula/claude-skills.rb` and document it as an available formula only after that layout is fixed. Rejected alternatives: removing it from README would hide a broken intended recipe; leaving the root file would keep the install path nonfunctional.

## Consequences

`brew install gocodealone/tap/claude-skills` should resolve after the tap PR merges and users run `brew update`. The change is a formula layout fix, not a new release of `claude-skills`. Rollback is moving the file back or reverting the tap PR, but that would make the README table false again.
