---
status: approved
area: ecosystem-docs
owner: workflow
implementation_refs: []
external_refs:
  - GoCodeAlone/workflow#907
  - GoCodeAlone/workflow-plugin-cloudflare#14
  - GoCodeAlone/workflow-plugin-namecheap#18
verification:
  last_checked: 2026-06-11
  commands:
    - GOWORK=off go test ./cmd/wfctl/internal/prompt ./capability/...
  result: pass
supersedes:
  - docs/plans/2026-06-11-capability-matrix-design.md
superseded_by: []
---

# Docs And Capability Ecosystem Design

Date: 2026-06-11

## Goal

Turn the new Workflow capability inventory into a docs-facing ecosystem catalog,
make application capability reports useful even when no warnings are present,
generate Go-level API documentation for Workflow and plugins, render Mermaid
diagrams in the website docs, and make `wfctl` interactive flows consistently
cancellable.

This design is phased because the work crosses Workflow core, the website, and
many external plugin repositories. Workflow owns the semantic extraction and
machine-readable artifacts; the website renders those artifacts; plugin repos
own their Go package comments and exported API documentation.

## Original User Need

The current ecosystem is too large to reason about by memory. Users and agents
need to know:

- what capabilities already exist across core and plugins;
- which plugin/provider implements a capability;
- which capabilities are in use by an application;
- which cross-cutting capabilities must be respected when adding features;
- how to integrate each plugin at the Go/API level;
- how capability and Go API docs can power `gocodealone-website`;
- how docs can automatically cross-reference related providers/plugins;
- how Mermaid docs render as diagrams rather than code blocks;
- how every `wfctl` prompt can be escaped/cancelled without trapping users.

## Guidance And Existing Boundaries

| Source | Relevant Constraint | Design Response |
|---|---|---|
| `docs/AGENT_GUIDE.md` | Use `GOWORK=off`; use clean worktrees for broad changes; update docs/tests with CLI/config behavior. | All Go verification uses `GOWORK=off`; broad work is in isolated worktrees; CLI/docs behavior gets tests. |
| `docs/REPO_LAYOUT.md` | Core keeps bootstrap-critical CLI/shared contracts; external plugins own provider runtime integrations. | Workflow emits shared docs/capability artifacts. Provider-specific docs and behavior updates land in each plugin repo. |
| `docs/plans/2026-06-11-capability-matrix-design.md` | Capability inventory should be generated from manifests/evidence, not manual claims. | Extend the existing inventory package instead of creating website-only parsing. |
| `decisions/0048-wfctl-owned-go-api-docs.md` | Workflow owns Go API extraction; website consumes generated artifacts. | Add a `wfctl docs go` style generator in Workflow and make website ingestion a separate consumer phase. |

## Current State

- `wfctl capability ecosystem|app|check` exists.
- Ecosystem JSON/Markdown is generated under `docs/generated/capabilities/`.
- The generated matrix is raw-inventory oriented and includes many
  uncategorized rows.
- `wfctl capability check` emits only findings, so healthy apps appear empty.
- The website sync script copies Workflow docs and plugin READMEs, but does not
  ingest capability catalog data or generated Go docs.
- Mermaid fences exist in docs, but the Starlight site currently renders them as
  code.
- `cmd/wfctl/internal/prompt` centralizes prompt widgets, but `esc`/`ctrl+c`
  semantics are inconsistent enough to block `wfctl secrets setup`.
- Some plugin repos contain local `_worktrees` directories; generators must
  ignore those to avoid stale code contaminating docs.

## Design

### 1. Workflow-Owned Docs Artifacts

Workflow should emit stable artifacts that downstream tooling can consume:

- `docs/generated/capabilities/ecosystem.json`: raw/evidence-rich inventory.
- `docs/generated/capabilities/catalog.json`: curated docs-facing catalog.
- `docs/generated/capabilities/catalog.md`: human-readable catalog snapshot.
- `docs/generated/capabilities/crossrefs.json`: plugin/provider dependency and
  implementation graph.
- `docs/generated/go-docs/index.json`: Go package docs index for core and
  selected plugin modules.
- `docs/generated/go-docs/<module-slug>.md`: Markdown generated from real
  `go doc`/`go list` output.

The catalog is derived from the taxonomy plus inventory. It should show product
capabilities and provider relationships by default, while preserving raw
uncategorized rows in maintainer reports.

### 2. Capability-Driven App Reports

`wfctl capability app` should remain the complete JSON profile. `wfctl
capability check` should default to a concise summary that includes:

- detected capabilities;
- evidence counts/source kinds;
- missing-provider and policy findings;
- a no-finding success line that still shows what was detected.

A `--findings-only` flag should preserve the current warning-only output for CI
or scripts that want the old behavior.

### 3. Cross References

Cross references should be generated, not hand curated. Sources:

- plugin manifests: `dependencies`, module/step/trigger/provider declarations,
  `iacProvider`, `iacServices`, state backends, CLI commands;
- registry manifests: release status, repository, source module;
- taxonomy aliases/tags: product-level capability grouping;
- Go modules: module path and exported package docs.

The output should support:

- "plugins implementing GitHub provider";
- "plugins implementing secrets management";
- "plugins that depend on authz";
- "capabilities supplied by this plugin";
- "other providers in the same capability family".

### 4. Go API Docs Generation

Generation must use built-in Go tooling where possible:

- `go list -json ./...` for packages;
- `go doc -all <pkg>` for package-level API text;
- `go env GOMOD` and `go list -m -json` for module metadata.

Published website docs should be generated from released versions. Local working
tree docs are allowed only for preview, CI verification, and maintainer review.
The artifact index must carry module path, repository, version/ref, generation
source (`release` or `local`), and enough metadata for the website to expose at
least major-version navigation for Workflow core and plugin API docs.

The generator must skip `.git`, `.worktrees`, `_worktrees`, `vendor`,
`node_modules`, generated docs output, and scratch directories. It should not
require externally installed documentation tools.

Plugin repos need package comments/doc.go files that explain:

- what the plugin provides in Workflow terms;
- which module, step, trigger, provider, and service contracts it implements;
- how a Go consumer/host integrates it;
- how the config and secret/provider contracts are intended to be used;
- which related plugins/capabilities to consider.

### 5. Website Rendering

The website should consume Workflow-generated artifacts rather than duplicating
capability logic. It should add:

- a capability catalog/docs route;
- plugin detail sections for capabilities, dependencies, provider family, and
  Go API docs;
- docs pages generated from `docs/generated/go-docs`;
- version navigation driven by the generated Go-doc index, with released
  versions as the default public source;
- Mermaid rendering during Astro/Starlight build.

Package/action versions must be checked at implementation time before adding or
pinning any website dependency.

### 6. Prompt Cancellation

Interactive prompts should have a single contract:

- `ctrl+c`, `esc`, and user-abort keys return a distinct cancellation error;
- non-TTY remains `ErrNotInteractive`;
- callers map cancellation to a concise user-facing "cancelled" error/status;
- cancellation works in select, multiselect, input, confirm, secrets setup, and
  the project wizard.

This keeps non-interactive detection separate from intentional user aborts.

### 7. Provider Followups

Outstanding provider issues remain part of the ecosystem-docs path:

- `workflow-plugin-cloudflare#14`: audit whether Cloudflare config needs
  account ID or other scope metadata in addition to API token.
- `workflow-plugin-namecheap#18`: audit whether `namecheap_client_ip` has value
  given Namecheap's external allowlist requirement.
- GitLab secrets/environment management should be implemented in the GitLab
  provider with GitLab SDK semantics, not copied from GitHub assumptions.

These should land after Workflow emits the shared cross-reference and Go-docs
artifacts so provider PRs can update their docs consistently.

## Phasing

### Phase 1: Workflow Core Generator And CLI Quality

Add docs-facing catalog/crossrefs, improve capability check output, add
Go-docs generator, and fix prompt cancellation in Workflow. This is the
foundation PR.

### Phase 2: Website Consumer

Update `gocodealone-website` to render Mermaid diagrams and consume generated
capability/Go-doc artifacts.

### Phase 3: Provider Plugin Documentation And Followups

Batch provider plugin PRs first: GitHub, GitLab, AWS, Azure, GCP,
DigitalOcean, Cloudflare, Namecheap, Hover, Vault, Infra, CI generator.

### Phase 4: Cross-Cutting Plugin Documentation

Batch auth/security/tenant/secrets/observability/migrations/messaging/payment
plugins.

### Phase 5: Remaining Plugin Documentation And Releases

Finish long-tail plugin API docs, regenerate all docs, merge green PRs, and run
releases for repos that changed.

## Acceptance Criteria

- Workflow emits catalog, cross-reference, and Go-doc artifacts from real source
  data.
- `wfctl capability check` is useful for healthy apps and keeps a
  findings-only mode.
- Every centralized prompt widget supports abort semantics consistently.
- Website docs render Mermaid as diagrams.
- Plugin Go-doc pages are navigable from website plugin docs.
- Provider/capability cross references are generated automatically.
- Provider followups are tracked and implemented in the relevant plugin repos.
- All changed repos have tests passing, merged green PRs, releases as needed,
  and regenerated docs.

## Non-Goals

- Replacing `pkg.go.dev`; the website should provide Workflow-specific context
  and can still link to pkg.go.dev.
- Executing arbitrary plugin binaries during docs generation.
- Hand-curating every cross reference in Markdown.
- Moving provider-specific runtime behavior back into Workflow core.
