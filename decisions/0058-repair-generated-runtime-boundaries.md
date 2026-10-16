# 0058. Repair generated runtime boundaries

**Status:** Accepted
**Date:** 2026-10-04
**Decision-makers:** Jon Langevin
**Related:** ADR0057; source PR1022; source security design/plan.

## Context

Approved Go/defaults maintenance requires actual generated-app/plugin launch.
The source lock was 2026-10-04T04:47:52Z, SHA prefix cb44d785a8dd, three tasks
and one PR. Real emitted binary compilation succeeds but HTTP configuration
fails: WithAllDefaults supplies handlers/triggers, not module plugins. All
three application templates have the same bootstrap omission. Real SDK plugin
loading succeeds after correcting the probe's internal.Version ldflag, but
configuration fails: StringValue uses scalar protobuf JSON while the host
always marshals/unmarshals an object map.

## Decision

Under the standing explicit autonomous-change approval, extend Task2 owning
files to the three application main templates and external convert.go/tests.
Reuse plugins/all.DefaultPlugins in generated bootstraps. Preserve the SDK's
existing strict StringValue wire contract: adapt only its value map to/from
canonical scalar protobuf JSON, with empty-map defaults and strict rejection
of unknown fields/invalid types. Ordinary messages and legacy boundaries are
unchanged. Add RED/GREEN regressions and rerun both real generated boundaries.
Recheck alignment and restamp the same three-task/one-PR manifest before edits.

## Alternatives And Consequences

Accepting compilation as runtime proof is rejected. New bespoke module
registration and a replacement scaffold ABI are unnecessary; existing registry
and published wrapper schema suffice. Broad well-known-type conversion is out
of scope. Changes repair defects observed through required real boundaries,
not speculative features. Strict negatives, independent review, complete new
compiler verification and all actual checks remain mandatory. No protected CI
write, release, deployment, or Signal completion is authorized by this repair.
