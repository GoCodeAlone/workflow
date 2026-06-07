---
status: approved
area: ecosystem
owner: workflow
implementation_refs: []
external_refs:
  - "docs go-api generation"
  - "Workflow 0.8 quirks cleanup"
verification:
  last_checked: 2026-06-07
  commands:
    - GOWORK=off go test ./cmd/wfctl ./config ./module ./plugin/...
    - npm run sync-docs
    - npm run build
  result: planned
supersedes: []
superseded_by: []
---

# Workflow Docs Ecosystem Design

Date: 2026-06-07

## Goal

Make the public docs present Workflow as a coherent ecosystem: a YAML-first
application platform, a `wfctl`-assisted lifecycle tool, an extensible Go SDK,
and a plugin marketplace with released-version API reference.

## Global Design Guidance

Source: `docs/AGENT_GUIDE.md`, `docs/REPO_LAYOUT.md`,
`docs/plans/2026-04-25-workflow-ecosystem-audit-design.md`,
`decisions/0045-ci-generation-boundary.md`, and
`decisions/0048-wfctl-owned-go-api-docs.md`.

| Guidance | Design response |
|---|---|
| Use `GOWORK=off` in this multi-repo workspace. | All Go verification commands specify `GOWORK=off`. |
| Keep durable examples under `example/`. | No new root examples; docs point at existing examples or generated reference pages. |
| `wfctl` is the lifecycle CLI and ecosystem audit surface. | Go API generation lives in `wfctl docs generate`, not website-only scripts. |
| Plugin/provider specifics live in plugins while core owns shared contracts. | Workflow docs cover SDK/contracts; plugin docs cover per-plugin packages and capabilities. |
| Design artifacts must remain traceable to implementation and verification. | Plan phases create PRs/releases and remove defect-ledger docs only after code fixes land. |

## Reader Model

The docs must serve two equally valid readers:

1. **Application assemblers** who use YAML, plugin manifests, wfctl helpers,
   and deployment docs without writing Go code.
2. **Application composers/extenders** who assemble a Workflow app and then add
   app-local Go code, custom dynamic components, plugins, or provider contracts
   as seen in production applications such as Buymywishlist and
   workflow-compute.

The public site should make both paths visible without forcing YAML-only users
through Go API pages or hiding SDK details from extension authors.

## Information Architecture

Reorganize generated docs into a stable narrative:

- **Start Here**: what Workflow is, install, first app, core concepts.
- **Build Applications**: YAML-first path, wfctl-assisted path, manual path,
  local test/debug, deploy, manage.
- **Extend With Code**: dynamic components, app-local Go code, plugin
  authoring, contract testing, and production-style composition examples.
- **Ecosystem**: plugin catalog, provider capabilities, plugin status, release
  and compatibility notes.
- **Reference**: config schema, wfctl CLI, Go API reference, plugin API
  reference, migrations, and release notes.

The website's Starlight sidebar should expose these lanes directly rather than
autogenerating a flat `workflow/` bucket that mixes tutorials, reference,
runbooks, and defect documents.

## Go API Reference

`wfctl docs generate` will produce Markdown API pages plus metadata JSON. The
generator owns Go-specific concerns:

- discover package docs using Go stdlib parsing (`go/parser`, `go/doc`,
  `go/ast`) and `go list` where needed;
- render package overview, exported constants, variables, functions, types,
  methods, examples, import path, source links, module version, and warnings;
- read the website/registry plugin snapshot to enumerate public plugins;
- clone or reuse released tags for Workflow and plugin repositories;
- emit warnings per repo/package without failing the whole ecosystem build when
  one plugin has no matching tag or no importable package;
- support a curated package allowlist first.

Workflow curated packages start with:

- `github.com/GoCodeAlone/workflow/plugin`
- `github.com/GoCodeAlone/workflow/plugin/sdk`
- `github.com/GoCodeAlone/workflow/plugin/external/sdk`
- `github.com/GoCodeAlone/workflow/config`
- `github.com/GoCodeAlone/workflow/cigen`
- `github.com/GoCodeAlone/workflow/module`
- `github.com/GoCodeAlone/workflow/pipeline`
- `github.com/GoCodeAlone/workflow/handlers`

Plugin package coverage is curated per repo: root package when importable, SDK
or contract packages when present, and exported provider/step packages that are
not `internal`, `cmd`, `testdata`, generated-only, or private application
entrypoints.

## Versioning

Docs generate from released versions, not `main`.

For Workflow pre-1.0, the public selector groups by minor line (`v0.8`,
`v0.9`) because every pre-1.0 minor may change compatibility. After v1.0, the
default selector groups by major line (`v1`, `v2`) with a metadata shape that
can expose minor selectors later.

Generated output should be routeable as:

- `/docs/reference/go/workflow/latest/`
- `/docs/reference/go/workflow/v0.8/`
- `/docs/reference/go/plugins/<slug>/latest/`
- `/docs/reference/go/plugins/<slug>/<version-line>/`

The first website implementation may render version links rather than a custom
dropdown. The metadata must make a dropdown possible without regenerating page
content.

## Defect-Ledger Docs And 0.8 Cleanup

`docs/config-field-quirks.md` is accurate enough to be useful but harmful as
public doctrine. Known framework rough edges should become 0.8 fixes, then the
doc should be removed from public sync and replaced by migration/release notes.

Fixes for the 0.8 path:

- `http.server` accepts `port` as an alias for `address`, normalizing integer
  ports to `":<port>"`.
- `step.db_exec` and `step.db_query` accept `module` as an alias for
  `database`.
- DB steps accept `args` as an alias for `params`.
- DB query mode accepts `many`/`one` as aliases for `list`/`single`.
- `step.request_parse` accepts `format: json` as equivalent to
  `parse_body: true`.
- Register `step.response` as an alias for `step.json_response`.
- `step.json_response` avoids double-serializing template results that resolve
  to JSON arrays/objects.
- `step.conditional` supports `if`/`then`/`else` alongside the existing
  `field`/`routes` switch mode.
- `wfctl modernize` and schema/LSP suggestions should guide users away from
  old or intuitive-but-noncanonical spellings.

The inline trigger model is a real architecture choice. It should be explained
prominently in guide docs, not listed as a quirk. A future route-to-pipeline
compatibility feature can be planned separately if product direction requires
it.

## Security Review

Go API generation reads public repos and produces static Markdown. It must not
execute plugin code, run arbitrary package tests, or publish secrets. Clone
URLs come from trusted registry metadata but are still treated as untrusted
content: generation must cap clone depth, avoid shell interpolation, and redact
tokens from warnings. Website workflows use the least token scope needed to
fetch public repositories and open docs PRs.

The 0.8 config alias work preserves existing behavior while accepting additional
input forms. Aliases must be normalized before execution so downstream modules
do not have multiple runtime interpretations.

## Infrastructure Impact

No cloud resources are created by the docs generator. Website CI/release
workflows gain Go/wfctl setup and may need caching for cloned repos or generated
docs. Public site size increases because API reference pages are committed and
packaged into `site.tar.gz`.

Workflow 0.8 cleanup requires a minor release and downstream docs regeneration.
It may trigger plugin/website/registry sync workflows but should not require
production infrastructure approval.

## Multi-Component Validation

The minimum proof spans real boundaries:

- `wfctl docs generate` runs against the released Workflow tag and at least
  two plugin repos, writing Markdown and metadata.
- `gocodealone-website` `npm run sync-docs` invokes the real generator and
  commits/render-previews generated API pages.
- Starlight build indexes the generated pages and version metadata.
- 0.8 alias fixes are verified through real config validation and representative
  pipeline execution for HTTP, DB, request parsing, response, and conditional
  steps.

## Rollback

- Docs generation rollback: revert website docs generator invocation and keep
  the last committed Markdown snapshot. `sync-docs` must degrade to previous
  behavior if generated API docs are absent.
- Workflow generator rollback: revert the `wfctl docs generate` PR and release
  a patch if needed; website remains on committed reference docs until the next
  successful sync.
- 0.8 behavior rollback: aliases are additive and can be disabled by reverting
  the minor release before public docs remove the quirks page. The quirks page
  is removed only after the release and regenerated docs confirm fixes.

## Assumptions

1. Public Workflow and plugin repos have release tags that match registry
   versions often enough for released-version docs to be useful.
2. A curated package list is acceptable for the first API reference release.
3. Website release workflows can install/use Go and `wfctl` without expanding
   permissions beyond repository contents and PR creation.
4. Pre-1.0 minor-line versioning is the right compatibility grouping for
   Workflow until v1.0.
5. The quirks doc reflects active behavior and can be retired only after tests
   prove the corresponding aliases/normalizations exist.

## Non-Goals

- Reimplementing pkg.go.dev completely.
- Executing plugin code during docs generation.
- Creating a custom search engine beyond Starlight/Pagefind indexing.
- Fixing every historical planning/audit document in `docs/plans/`.
- Adding route-to-pipeline syntax in the same PR as the alias cleanup unless
  the implementation plan explicitly schedules it as a later phase.

## Phases

1. **Design and plan lock**: commit this design, ADR, adversarial reviews, and
   implementation plan.
2. **Workflow 0.8 config cleanup**: fix the active quirks with tests and release
   a minor version.
3. **Go API generator**: add `wfctl docs generate` with curated Workflow and
   plugin package output plus version metadata.
4. **Website docs reorganization**: update Starlight sidebar/content layout,
   invoke `wfctl docs generate`, render versioned API pages, and remove the
   retired quirks page from public docs.
5. **Release and sync**: release Workflow, regenerate website docs, release the
   website, and verify downstream multisite dispatch.

Deferrals become later phases in this sequence rather than unscheduled backlog.
