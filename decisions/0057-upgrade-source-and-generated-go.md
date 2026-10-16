# 0057. Upgrade source and generated Go together

**Status:** Superseded by 0063 (patch floor; generated/default scope retained)
**Date:** 2026-10-04
**Decision-makers:** Jon Langevin
**Related:** ADR0056; source PR1022.

## Context

Operator: "Increase to latest go (1.27.1 I believe), update the ci process to
align, admin merge as necessary once all checks passing". Official stable
downloads confirm1.27.1. The source plan was Locked2026-10-03T18:25:28Z,
manifest SHA prefix90173f5ef1fc, three tasks/one PR. It excluded Go-minor,
Docker and generator defaults. Leaving those defaults old generates apps
that cannot build against the upgraded Workflow floor.

## Decision

Amend Task2 to upgrade source/example/eleven fixtures and maintained generated
module, CI, container and SDK/build defaults to1.27.1. Include admin/legacy
Docker builders and verified CircleCI cimg/go:1.27.1 output. Preserve explicit
user build-version overrides. Keep three tasks and existing sole PR1022,
cache/UI fixes and dependency versions. CI authority remains a separate
stage/adopt/promote rollout; no protected files enter this source PR.

Retest native1.27.1 with compatible golangci-lint2.14.0, real generated output,
actual runtime and full cold Linux suite before source merge. Older compiler
auto-selection is a compatibility probe, not hosted local-toolchain readiness.
Use the approved ordinary-review admin exception only after independent agent
review and all actual checks pass. No failed-check/policy bypass or settings edit.

## Alternatives And Consequences

Root-only bump rejected: scaffold/SDK/deploy/build defaults would still drift.
Fleet migration rejected: the request is addressed in Workflow, not every repo.
A second source PR rejected: the existing open remediation owns the compiler
and receives fresh exact-head evidence. Source and generated defaults align;
isolated policytool keeps its language minimum outside this boundary. Rollback
restores known older limitations, so hold release rather than silently downgrade.
No tag, release, deployment, completed Signal or release-governance claim.
