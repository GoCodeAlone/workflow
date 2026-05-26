# ADR-0032: Tombstone `platform/providers/aws/` dead code

**Date:** 2026-05-13
**Status:** Accepted
**Related:** Issue #653, Phase 3; ADR-0024 (IaC typed force-cutover); Phase 1 (PR #657); Phase 2 (PR #659)

## Context

`platform/providers/aws/` implemented `platform.Provider` for Amazon Web Services,
gating all 24 files behind `//go:build aws`. Investigation for issue #653 Phase 3
revealed:

1. Zero external callers: no code outside the package itself (or its own tests)
   ever imported or instantiated `platform/providers/aws.NewProvider()`.
2. No CI coverage: no CI job runs `go test -tags aws ./...` or `go build -tags aws ./...`.
3. No user documentation: no example YAML config, no guide, no mention in DOCUMENTATION.md.
4. Not superseded by `workflow-plugin-aws`: that plugin implements `interfaces.IaCProvider`
   (the gRPC plugin boundary), which is a separate, orthogonal abstraction from
   `platform.Provider` (the capability-based in-core abstraction).
5. AWS SDK maintenance burden: `service/ec2`, `service/dynamodb`,
   `service/elasticloadbalancingv2`, `service/rds`, and `service/sqs` were listed in
   `go.mod` solely as transitive requirements of this dead tree.

## Decision

Delete `platform/providers/aws/` and its `drivers/` subtree (24 files, ~2,000 LOC).
Preserve the `platform.Provider` interface and its live implementations
(`DockerComposeProvider`, `MockProvider`).

Promote the Phase 2 CI gate placeholder for `service/eks` to strict enforcement
(removing the `--exclude-dir=platform` exemption) and add the 5 exclusive packages
to the banned list in `go.mod` and the grep gate.

## Consequences

- **Positive:** 5 AWS SDK packages removed from `go.mod`; CI gate tightened; ~2,000 LOC
  of dead code eliminated; no future AWS SDK upgrade compatibility burden for unused code.
- **Neutral:** The `platform.Provider` interface and its two live implementations
  (`DockerComposeProvider`, `MockProvider`) are completely unaffected.
- **Breaking (theoretical):** Any downstream project building workflow core with
  `-tags aws` would lose the `platform/providers/aws` package. Evidence that any
  such project exists: none (build tag undocumented, no CI exercises it, no example
  YAML uses it).
- **Canonical AWS IaC path:** `workflow-plugin-aws` (implements `interfaces.IaCProvider`)
  is the only supported AWS IaC integration since workflow v0.53.0.
