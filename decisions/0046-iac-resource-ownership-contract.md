# 0046. Add optional IaC resource ownership contract

**Status:** Accepted
**Date:** 2026-06-01
**Decision-makers:** autonomous pipeline
**Related:** workflow#779, `docs/plans/2026-06-01-iac-resource-ownership.md`

## Context

Workflow needs a cross-driver way to detect whether a cloud resource is owned by
the caller before `wfctl infra apply` mutates it. DNS already has a separate
TXT-policy gate, but natively taggable cloud resources need a provider-neutral
read/write/list contract. Existing IaC extensions use optional typed gRPC
services discovered through ContractRegistry; absence of a service is the
negative signal.

## Decision

Add an optional `IaCProviderOwnership` service and matching Go interface for
generic cloud-resource ownership. `wfctl infra apply` will call it only when an
operator supplies an owner identity, and will set missing ownership before the
first mutation. `wfctl infra owners` will list resources reported by providers.

Rejected alternatives: overload `Enumerator.EnumerateByTag` because it cannot
read or set ownership for a specific resource; reuse DNS TXT policy because DNS
ownership is zone/record-shaped and already solved separately.

## Consequences

The core contract lands before provider implementations, so older providers keep
working when no owner is supplied. Operators get a uniform safety gate once
providers implement the optional service. Provider cascades must translate this
contract to tags, labels, or naming conventions without changing core again.
