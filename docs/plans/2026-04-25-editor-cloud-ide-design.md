---
status: approved
area: editor
owner: workflow
implementation_refs: []
external_refs:
  - "#25"
  - "#26"
verification:
  last_checked: 2026-04-25
  commands:
    - npm test
    - npm run build
    - ./gradlew test
  result: partial
supersedes: []
superseded_by: []
---

# Editor, Cloud, And IDE Design

Date: 2026-04-25

## Goal

Make Workflow authoring feel consistent across the standalone editor, shared UI packages, cloud UI, VS Code, JetBrains, and runtime schema endpoints.

## Problem

The authoring surface is split across several repos:

- `workflow-editor`
- `workflow-ui`
- `workflow-vscode`
- `workflow-jetbrains`
- `workflow-cloud`
- `workflow-cloud-ui`
- schema exports from `workflow`

The pieces mostly build, but verification and distribution are uneven. `workflow-editor` builds but tests fail due test storage setup. `workflow-ui` tests/build pass. `workflow-cloud-ui` builds with a large bundle warning. `workflow-vscode` compiles. `workflow-jetbrains` is blocked by unauthenticated GitHub Packages access to `@gocodealone/workflow-editor@0.3.68`.

## Authoring Contract

All authoring clients should depend on the same schema contract:

- core module schemas from `workflow`
- external plugin schemas from installed plugin manifests and schema providers
- editor field metadata with stable labels, descriptions, options, placeholders, and validation hints
- source-map support for YAML round trips
- LSP diagnostics matching `wfctl validate`

The editor should not have to hardcode long-lived knowledge that `wfctl editor-schemas` or runtime schema endpoints can provide.

## Package Distribution

Shared packages should have a clear public/private policy:

- public npm packages should be installable without GitHub Packages auth
- private packages should fail with a clear setup message and documented token env var
- JetBrains and VS Code builds should use the same package source as CI

The current JetBrains failure is an environment/auth issue, but it is still a product issue because it blocks repeatable extension builds.

## Test Environment

`workflow-editor` tests should own their jsdom storage setup. The current failure points at `localStorage.getItem` and Zustand storage `setItem` not being functions. Add a durable test setup that provides a standards-shaped storage object before stores import.

The test setup should cover:

- `localStorage.getItem`
- `localStorage.setItem`
- `localStorage.removeItem`
- `localStorage.clear`
- fetch fallback for relative `/api/...` URLs
- `ResizeObserver`
- `DOMMatrix`

The goal is not to hide real persistence bugs. It is to make tests fail for component behavior instead of environment shape.

## Runtime UX

Authoring clients should expose the same flows:

- validate current YAML
- inspect modules and dependencies
- fetch module/plugin schemas
- run template validation
- show LSP diagnostics
- show plugin install/enable state

The cloud UI can add tenancy and organization context, but the core authoring interactions should remain portable.

Buymywishlist/BMW staging and production gates (`#25`, `#26`) should be treated as downstream runtime UX evidence. A staging deploy followed by Playwright verification and `/healthz` promotion checks is concrete proof that the authoring/deploy surfaces work outside the core repo.

## Bundle Health

`workflow-cloud-ui` currently builds but warns that a chunk exceeds 500 kB. Treat this as a warning, not a blocker, but add a bundle budget so growth is intentional.

## Testing

Baseline commands:

```sh
cd workflow-editor && npm test
cd workflow-editor && npm run build
cd workflow-ui && npm test
cd workflow-ui && npm run build
cd workflow-vscode && npm run compile
cd workflow-cloud-ui/ui && npm run build
cd workflow-jetbrains && ./gradlew test
```

Expected after remediation:

- editor tests pass
- shared UI tests pass
- VS Code compile passes
- cloud UI build passes without unexpected warnings
- JetBrains build either passes or fails with an explicit documented auth prerequisite

## Acceptance Criteria

- Editor, IDE, and cloud clients consume one schema contract.
- `workflow-editor` tests pass in a clean jsdom setup.
- IDE builds have reproducible package-auth instructions.
- Cloud UI bundle warnings are tracked with an explicit budget.
- Authoring UX can validate and inspect Workflow configs consistently across clients.
