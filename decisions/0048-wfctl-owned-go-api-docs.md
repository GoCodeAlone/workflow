# 0048. Generate Go API Docs With wfctl

**Status:** Accepted
**Date:** 2026-06-07
**Decision-makers:** workflow maintainers
**Related:** docs/plans/2026-06-07-workflow-docs-ecosystem-design.md

## Context

The public website already syncs Workflow and plugin Markdown, but it does not
publish Go API references for Workflow packages or plugin packages. The API
reference needs released-version awareness, registry-driven plugin discovery,
and Go package parsing. The website is a renderer and release surface; Workflow
already owns `wfctl`, plugin contracts, registry metadata, and Go package
semantics.

## Decision

We will put Go API extraction in `wfctl docs generate` and let
`gocodealone-website` call that command during docs sync. The generator uses Go
toolchain and stdlib package documentation APIs, emits Markdown plus version
metadata, and reads the registry snapshot to discover public plugin repos.

Rejected alternatives: website-only Node generation would scatter Go package
rules into the renderer repo; linking only to pkg.go.dev would not provide a
coherent Workflow ecosystem reference or version navigation.

## Consequences

- The docs pipeline dogfoods `wfctl` and stays Go-native for Go semantics.
- Website sync gains a Go/wfctl dependency but remains mostly renderer logic.
- Versioning and plugin fallbacks can be tested at the CLI layer.
- If generation breaks, website can keep last committed docs while `wfctl`
  reports per-repo warnings.
