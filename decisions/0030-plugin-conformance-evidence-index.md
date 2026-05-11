# 0030. Use Generated Plugin Compatibility Evidence

**Status:** Accepted
**Date:** 2026-05-11
**Decision-makers:** GoCodeAlone maintainers
**Related:** docs/plans/2026-05-11-plugin-conformance-compat-design.md

## Context

Workflow plugins currently declare `minEngineVersion`, but that field is only a compatibility claim. Recent DigitalOcean plugin CI showed that executable conformance can catch real host/plugin mismatches, such as a plugin loading on one Workflow release and failing on another. Keeping those checks as plugin-local scripts would duplicate engine-version lookup, private-release auth, output formatting, and install semantics.

## Decision

We will centralize plugin compatibility checks in `wfctl plugin conformance` and store generated compatibility evidence in a versioned index. Manifests may carry a short summary and pointer to that index, but pass/fail claims come from CI-generated output. Rejected alternatives: per-plugin shell scripts, because they drift and cannot guide installs; a hosted compatibility service first, because it is heavier than the local/CI contract needed now.

## Consequences

This makes plugin CI and local development use one contract, and it gives `wfctl plugin install` data for compatibility-aware resolution. It also adds responsibility to `wfctl`: conformance modes must stay precise, and the registry needs additive metadata without breaking older clients. Rollback is straightforward because compatibility fields are optional and plugin-local scripts can remain during the transition.
