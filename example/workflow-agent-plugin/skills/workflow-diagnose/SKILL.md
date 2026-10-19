---
name: workflow-diagnose
description: Use when a GoCodeAlone Workflow configuration fails validation, has unavailable types or plugin prerequisites, or encounters wfctl MCP connection and compatibility errors.
---

# Diagnose a Workflow configuration

First read and follow the [shared contract](../../references/workflow-contract.md).
Use the smallest sanitized reproduction and a safe error summary. Do not request
full logs, credentials or private infrastructure access.

1. Classify the failure: executable/PATH, initialization or unknown version,
   missing tool, malformed YAML, unavailable type/schema, declared prerequisite,
   unresolved import/template, or runtime-only failure. Connection failure means
   dependent checks are unavailable; it does not mean the config is invalid.
2. Discover live tool schemas and the relevant type inventories. Run available
   basic/deep checks on the same authorized sanitized snapshot and compare the
   needed type classes. Inspect parsed topology if useful. Missing observations
   remain unknown; do not infer another release's compatibility.
3. Check imports and declared `requires.plugins`/`plugins.external` separately.
   A basic validation pass can hide both unresolved inputs and missing
   prerequisites. `list_plugins({})` describes manifests, not binary/version
   health. Do not install, auto-fetch, update or search private registries.
4. Explain the cause supported by observations. For optional modernization, use
   only discovered `modernize` dry-run arguments (`apply:false` in v0.86.2).
   Propose a minimal diff with sensitive values removed; avoid unrelated cleanup.
5. Recheck an accepted proposed snapshot through available tools; label proposed
   versus actually checked content and each snapshot's identity. If a runtime
   failure cannot be diagnosed with static evidence, state that limit and the
   next authorized proof needed. Return a safe diagnosis, bounded evidence table
   and remaining blockers. Never promote config validation to runtime acceptance.
