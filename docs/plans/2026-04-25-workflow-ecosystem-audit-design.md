---
status: approved
area: ecosystem
owner: workflow
implementation_refs: []
verification:
  last_checked: 2026-04-25
  commands:
    - GOWORK=off go test ./cmd/wfctl ./interfaces ./plugin/...
    - GOWORK=off go test ./...
    - npm test
    - npm run build
    - ./gradlew test
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

## Plan Index

Add a generated `docs/plans/INDEX.md` in a follow-up implementation. It should list every design and plan with:

- filename
- title
- area
- status
- owner
- implementation refs
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
