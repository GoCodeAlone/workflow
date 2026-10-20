# 0065. Use Go for assistant package verification

**Status:** Accepted
**Date:** 2026-10-19
**Related:** `0064-package-workflow-agent-example.md`, `docs/plans/2026-10-19-workflow-agent-plugin.md`

## Context

The initial package used a Python stdlib MCP client and Python jsonschema for two
vendored portable manifests. Neither language nor validator is required by the
MCP or Agent Plugins standards. The repository already uses Go and
`github.com/santhosh-tekuri/jsonschema/v6 v6.0.3`.
Its existing dependency checksums also pin `github.com/dlclark/regexp2 v1.11.0`.

## Decision

At the user's request, replace the new Python tests/helper with Go. Keep a small
standalone tests module using that existing schema dependency/version; the smoke
consumer itself uses the Go standard library. Preserve all assertions, real
release probes, failure privacy and lifecycle regressions. A related root Go
integration test invokes the nested suite through the existing native CI command.
The workflow trust policy requires three staged PRs for workflow changes, so
the workflow and its authority manifests remain unchanged.

Python's short standalone implementation did not outweigh a second test language
and package manager. Reimplementing JSON Schema validation would weaken the
official-schema proof, so reuse the existing Go validator instead.
The unchanged official schema contains ECMA negative lookahead unsupported by
Go's standard regexp package. Use the validator's documented regexp2 adapter at
that existing pinned version rather than weakening the schema.

## Consequences

No Python/pip dependency remains in the distributed package. The standalone
tests need Go and existing public schema dependencies, with no Workflow engine
or private module import. The three skills/direct MCP runtime are unchanged.
This replaces implementation within the same three tasks and draft PR; it does
not expand the shared MCP SDK or platform scope.
