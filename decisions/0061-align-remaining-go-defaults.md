# 0061. Align remaining maintained Go defaults

**Status:** Accepted
**Date:** 2026-10-04
**Decision-makers:** Jon Langevin
**Related:** ADR0057; source Task2/S6, PR1022.

## Context

The frozen-source audit found old Go drivers in the MCP legacy CD/release
emitters and the maintained e-commerce Docker builder, plus five active
default/prerequisite claims. The existing source lock is2026-10-04T06:00:42Z,
SHA prefixe037b6274335, three tasks/one PR. The operator's approved Go/default
upgrade covers these boundaries, but their owning files were omitted.

## Decision

Add mcp/wfctl_tools.go and its existing test, the e-commerce Dockerfile,
DOCUMENTATION.md and CONTRIBUTING.md to Task2 ownership. Keep existing
README/build/deploy guide owners. Assert parsed emitted MCP Go versions before
changing the two literals, including the real handler's returned CD YAML.
Verify the DHI builder tag/actual compiler rather than guessing a replacement
or silently changing vendors. Only owning version claims change; preserve
historical receipts, explicit overrides and all protected CI exclusions.

## Consequences

Root-only/CLI-only defaults remain rejected. No new MCP API, plugin detection,
Docker runtime hardening change, authority bypass or fleet migration. Three
tasks/one existing PR remain; rerun the affected tests and final frozen-source
gates. The running33b4 cold check remains a prior checkpoint, not acceptance
of these later changes.
