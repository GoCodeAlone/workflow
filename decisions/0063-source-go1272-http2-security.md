# 0063. Patch source and generated HTTP/2 toolchains

**Status:** Accepted
**Date:** 2026-10-10
**Decision-makers:** Jon Langevin; coordinating parent assigns Compute
**Related:** ADR0056-0062; source PR1022; separate Workflow CI rollout.

## Context

The parent verified that the managed Signal successor is paused and explicitly
assigned this source security repair to Compute: Go1.27.2/x/net0.60, retaining
OTel1.45, independent review, exact-head tests and normal merge/release gates.
This does not resume Signal features or transfer Signal acceptance.

Source PR1022 at3e62f5fa has Go1.27.1/x/net0.58.0/OTel1.45.0. The consumed
wfctl0.86.1 binary has Go1.26.5/x/net0.58.0. GO-2026-6611 fixes the HTTP/2
SETTINGS CPU issue in Go1.27.2 and x/net0.60.0. Official Go downloads confirm
stable1.27.2; the Go proxy/checksum database confirms x/net0.60.0 and unchanged
required companion versions. The original source manifest was
Locked2026-10-04T06:44:30Z, SHA256
`a77fbe3cc3696c048e0b4ff53aa6840c0b484d04e4bc6ce0318dce6738b2dab0`.

## Decision

Amend existing Task2, keeping three tasks/one existing PR1022: source/example/
eleven fixture Go floors, existing generated/default/compiler-host pins and
literal version tests move1.27.1→1.27.2; owning x/net requirements move0.58→0.60.
Retain OTel1.45 and explicit user version overrides. Authenticate sums and run
native targeted tidy; no unrelated module refresh or checksum text replacement.
Official Docker, CircleCI and DHI catalogs define the corresponding builder
tags; catalog existence is not actual image/compiler/runtime acceptance.
Integrate accepted main without a force-push; preserve its source-map-js1.2.2
fix. Source-only protected-byte comparison uses the recorded integration base;
historical49e18034 proof is retained rather than misapplied to newer main.

Protected CI/scripts/policytool remain outside this source PR. Their separate
governed rollout must supply compatible accepted CI/release wiring. Existing
source/runtime/SDK/UI/scanner gates remain mandatory on the new exact head.
Normal merge/release authorization does not permit failed checks, new settings
exceptions, unchecked tags or a clean-all-dependencies claim.

## Consequences

Root-only patch rejected: generated consumers and real fixture hosts would
retain an older compiler than the source floor. Blanket upgrades rejected:
x/net0.60 uses the already selected companions. A second source PR rejected:
the existing open source3/1 lane owns this bounded repair. Source-only review
does not close Compute staging identities, Signal, Docker or release gates.
Rollback uses a reviewed revert/rebuild and reinstates the security release
hold; older vulnerable binaries cannot become an accepted fallback.
