---
status: implemented
area: ecosystem
owner: workflow
implementation_refs:
  - repo: workflow
    commit: b07721e
  - repo: workflow
    commit: b82891f
  - repo: workflow
    commit: 521d6b9
  - repo: workflow
    commit: a29ea93
  - repo: workflow
    commit: 4a977ca
  - repo: workflow
    commit: c42d6e3
external_refs:
  - "#76"
  - "#118"
  - "wfctl-mcp-hot-reload"
verification:
  last_checked: 2026-04-25
  commands:
    - GOWORK=off go test ./cmd/wfctl ./interfaces ./plugin/...
    - GOWORK=off go test ./...
    - npm test
    - npm run build
    - ./gradlew test
    - GOWORK=off go run ./cmd/wfctl audit plans --dir docs/plans
    - GOWORK=off go run ./cmd/wfctl audit plugins --repo-root /Users/jon/workspace
  result: partial
supersedes: []
superseded_by: []
---

# Workflow Ecosystem Audit Design

Date: 2026-04-25

## Goal

Make the Workflow ecosystem auditable across design intent, implementation state, verification evidence, and release readiness. The immediate output is a small set of focused design documents plus a machine-readable tracking convention that prevents stale designs from looking complete.

## Background

The current workspace has many active Workflow repos: `workflow`, `workflow-registry`, editor and IDE integrations, cloud UI/server repos, scenarios, and dozens of `workflow-plugin-*` repos. Recent plans show strong product direction around `wfctl` as a portable lifecycle CLI, externalized plugin ownership, registry-backed installation, supply-chain verification, and declarative CI/CD.

The issue is not lack of ambition. The issue is traceability. Design docs can be approved without a durable implementation reference; plans can be superseded silently; docs can claim features that have moved or changed; and repo-level tests can be green only when run with local context the design never records.

## Evidence Snapshot

Core `wfctl`, interface, and plugin packages pass with the workspace file disabled:

```sh
GOWORK=off go test ./cmd/wfctl ./interfaces ./plugin/...
```

Critical plugins pass in isolation:

```sh
GOWORK=off go test ./... # workflow-plugin-digitalocean
GOWORK=off go test ./... # workflow-plugin-migrations
GOWORK=off go test ./... # workflow-plugin-supply-chain
```

The broad Go plugin matrix mostly passes with `GOWORK=off`, but found blockers:

- `workflow-plugin-agent`: checksum mismatch for `github.com/GoCodeAlone/genkit/go@v1.6.2-gocodealone.1`.
- `workflow-plugin-dnd`: missing `go.sum` entry for `github.com/GoCodeAlone/workflow@v0.5.1` through `workflow-plugin-gameserver@v0.24.6`.
- `workflow-plugin-gitlab`: tests call an older step `Execute` signature.
- `workflow-plugin-template` and `workflow-plugin-template-private`: missing `go.sum` entries and placeholder identity.

Frontend and IDE checks found:

- `workflow-editor`: build passes, tests fail because the persisted Zustand store sees an invalid `localStorage` shape in jsdom.
- `workflow-ui`: tests and build pass.
- `workflow-cloud-ui`: build passes with a chunk-size warning.
- `workflow-jetbrains`: Gradle test/build is blocked by unauthenticated GitHub Packages access for `@gocodealone/workflow-editor@0.3.68`.

Workspace-level Go tests initially fail because `/Users/jon/workspace/go.work` only includes three scenario modules. That is valid for scenario work, but it is a footgun for cross-repo auditing unless commands explicitly set `GOWORK=off` or use a generated audit workspace.

## Design Tracking Contract

Every new design and plan in `docs/plans/` should include YAML frontmatter:

```yaml
---
status: proposed | approved | planned | in_progress | implemented | superseded | abandoned
area: wfctl | plugins | editor | cloud | core | scenarios | ecosystem
owner: workflow | plugin:<name> | repo:<name>
implementation_refs:
  - repo: workflow
    pr: 486
    commit: ab6edc2
external_refs:
  - "#76"
  - "wfctl-mcp-hot-reload"
verification:
  last_checked: 2026-04-25
  commands:
    - GOWORK=off go test ./cmd/wfctl ./interfaces ./plugin/...
  result: pass | fail | partial | unknown
supersedes: []
superseded_by: []
---
```

Status semantics:

- `proposed`: draft or exploratory, not accepted as direction.
- `approved`: accepted design; no implementation plan yet.
- `planned`: implementation plan exists and aligns with the design.
- `in_progress`: implementation has started.
- `implemented`: implementation refs and verification evidence exist.
- `superseded`: replaced by a newer design or plan.
- `abandoned`: intentionally not pursued.

No doc may claim `implemented` only because related code exists. It needs implementation refs and verification evidence.

`external_refs` records work tracked outside the local design file. Examples include GitHub issue numbers, team-task IDs, production blocker IDs, downstream repo branches, or named backlog items. These refs are not implementation evidence; they are indexable context so planned and in-flight work does not disappear between sessions.

## Known Work Queue

Seed the first audit index with these external refs from the current Workflow and Buymywishlist backlog.

Design-only brainstorms:

- `#76`: all plugins, not only IaC, adopt strict proto-enforced types plus `buf.validate`.
- `#118`: single source of truth for base versions across `workflow-server`, `setup-wfctl`, base images, `app.yaml`, `infra.yaml`, `deploy.yml`, and Dockerfiles.
- `wfctl-mcp-hot-reload`: apply the `workflow-dnd` and `workflow-cardgame` MCP supervisor/reload pattern to `wfctl mcp` so the MCP server can be rebuilt and restarted during iterative development.

In-progress or production-pending:

- `#16`: Buymywishlist/BMW `STRIPE_SECRET_KEY` secret capture. This is user-side secret action and remains a production blocker.
- `#25`: Buymywishlist/BMW staging deploy plus Playwright verification. Gated on trusted-sources fix flowing through and another deploy attempt.
- `#26`: Buymywishlist/BMW production auto-promote plus `/healthz`. Blocked by `#25`.
- `#42`: Buymywishlist/BMW v0.19.0 plugin manifest adoption. Existing local branch is `chore/bmw-plugin-manifest-v0.19.0`; design and plan are committed there, but tasks 1-9 are not executed.

Queued wfctl/workflow core follow-ups:

- `#15`: `wfctl infra apply --only <module>`.
- `#28`: provider-agnostic `deploy.go` env var detection.
- `#29`: plugin-registered `providerCredentialSubKeys` map.
- `#30`: S3/GCS/Azure state store backends.
- `#33`: `wfctl infra apply` dry-run mode showing API calls.
- `#35`: size tier versus provider slug disambiguation.
- `#48`: multi-registry support plus `IaCProvider.EnsureRegistryAuth`.
- `#50`: `wfctl infra apply` prints full planned action list, not only successes.
- `#63`: `wfctl build` provider-agnostic CI summary plus structured outputs for GitHub Actions, GitLab, and others.
- `#78`: `wfctl migrations validate` subcommand with ephemeral-DB migration smoke test.

Queued plugin and Buymywishlist build hygiene:

- `#51`: reproducible Buymywishlist plugin build plus Docker layer reuse.
- `#66`: `workflow-plugin-migrations` v0.3.1 evaluation of consolidating image publish into GoReleaser dockers.

## Plan Index

Add a generated `docs/plans/INDEX.md` in a follow-up implementation. It should list every design and plan with:

- filename
- title
- area
- status
- owner
- implementation refs
- external refs
- last checked date
- verification result
- superseded/supersedes links

The index should be generated from frontmatter so it is not another hand-maintained source of drift.

## Audit Command

Add a future `wfctl audit plans` command with read-only behavior:

```sh
wfctl audit plans --dir docs/plans
wfctl audit plans --dir docs/plans --json
wfctl audit plans --dir docs/plans --stale-after 30d
```

Initial checks:

- missing frontmatter
- invalid status or area
- `implemented` without refs
- `implemented` without verification commands
- stale verification date
- broken `superseded_by` target
- duplicate active designs for the same area/topic
- referenced local commits that cannot be found

The command should report findings without changing files. A later `--fix-index` flag can regenerate `docs/plans/INDEX.md`.

## Focused Designs

This umbrella design delegates detailed product work to four focused designs:

- `2026-04-25-wfctl-lifecycle-product-design.md`
- `2026-04-25-plugin-contract-registry-design.md`
- `2026-04-25-editor-cloud-ide-design.md`
- `2026-04-25-core-runtime-boundaries-design.md`

Each design uses the same tracking frontmatter and contributes rows to the plan index.

## Non-Goals

- Do not rewrite all old design docs in the first pass.
- Do not block feature work on perfect historical metadata.
- Do not make the index a project-management system. It records evidence and status only.

## Acceptance Criteria

- New design and implementation-plan docs carry frontmatter.
- `docs/plans/INDEX.md` can be generated from frontmatter.
- `wfctl audit plans` catches stale or unverifiable implementation claims.
- Release-readiness audits use explicit commands, not ambient local workspace assumptions.
