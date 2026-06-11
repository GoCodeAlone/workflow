# 0049. Generate Capability Inventory In Workflow

**Status:** Accepted
**Date:** 2026-06-11
**Decision-makers:** workflow maintainers
**Related:** docs/plans/2026-06-11-capability-matrix-design.md

## Context

Workflow now spans core packages, built-in plugins, external provider plugins,
application repos, and website documentation. Maintainers and agents need a
cross-reference for what exists and what an app already uses. Existing sources
are fragmented: plugin manifests list low-level types, registry manifests track
release discovery, `wfctl audit plugins` reports contract health, and workflow
config analysis reports app requirements.

## Decision

Workflow will own a generated capability inventory model and `wfctl` commands
that emit ecosystem and application capability views. The website will consume
Workflow-owned JSON/Markdown instead of reimplementing capability extraction.

Rejected alternatives: a hand-maintained documentation matrix would drift; a
website-only generator would duplicate Workflow semantics in the renderer; an
application-only checker would not solve ecosystem discovery.

## Consequences

- Capability docs and agent-readable inventory share one source of truth.
- Plugin and application metadata need evidence fields so generated rows are
  auditable instead of merely descriptive.
- The first taxonomy must be curated enough to be useful, but conservative
  enough that unknown capabilities are reported rather than guessed.
- Website docs gain a dependency on generated Workflow artifacts but avoid
  owning Workflow-specific analysis logic.
