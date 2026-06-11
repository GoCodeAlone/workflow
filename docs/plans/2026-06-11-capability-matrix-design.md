---
status: approved
area: ecosystem
owner: workflow
implementation_refs: []
external_refs: []
verification:
  last_checked: 2026-06-11
  commands:
    - GOWORK=off go test ./cmd/wfctl ./capability ./manifest ./plugin -count=1
  result: pass
supersedes: []
superseded_by: []
---

# Capability Matrix Design

Date: 2026-06-11

## Goal

Create a generated capability matrix for the Workflow ecosystem and a generated
capability profile for individual Workflow applications. The matrix must help
humans and agents discover already-built functionality, avoid reimplementing
features such as authn/authz/tenancy, and understand what could be impacted by a
change.

## Original User Need

Workflow has grown to roughly 80 plugins plus core packages, provider patterns,
and application repos. It is difficult to answer:

- What functionality already exists?
- Which plugin or provider implements it?
- Is it released, local-only, tested, or only partially declared?
- Which Workflow capabilities are already used by a given application?
- When an app already uses cross-cutting behavior such as multi-tenancy, authz,
  secrets, or provider-specific deployment, what should new functionality also
  respect?
- How can this inventory feed `gocodealone-website` documentation without a
  separate hand-maintained source?

## Global Design Guidance

Source: repo guidance and prior durable decisions:

- `docs/AGENT_GUIDE.md`: use `GOWORK=off`, clean worktrees for broad work, and
  update docs/tests with CLI or config behavior.
- `docs/REPO_LAYOUT.md`: `cmd/wfctl`, `capability`, `manifest`, `plugin`,
  `data/registry`, `docs`, and `decisions` are the relevant ownership roots.
- `docs/plans/2026-04-25-workflow-ecosystem-audit-design.md`: ecosystem state
  should be generated from frontmatter/manifests/evidence, not manually claimed.
- `docs/plans/2026-04-25-plugin-contract-registry-design.md`: plugin manifests,
  registry metadata, and compatibility evidence are the canonical plugin
  contract/discovery surfaces.
- `decisions/0048-wfctl-owned-go-api-docs.md`: Workflow owns extraction logic;
  `gocodealone-website` should consume generated Workflow artifacts.

| guidance | design response |
|---|---|
| Keep Workflow semantics in Workflow-owned code | Add `wfctl capability ...` commands and a Go inventory package in this repo. |
| Prefer generated evidence over hand-maintained claims | Every row records source path, source kind, confidence/status, and finding codes. |
| Respect plugin boundaries | Provider-specific runtime checks stay in plugins; core reads manifests, contracts, lockfiles, and config declarations. |
| Website is renderer/consumer | Emit JSON and Markdown that website can ingest without duplicating analysis logic. |

## Current Sources

The first implementation should reuse existing sources before adding new schema:

- `plugin.PluginManifest`: `moduleTypes`, `stepTypes`, `triggerTypes`,
  `workflowTypes`, `wiringHooks`, `iacServices`, `iacStateBackends`,
  `stepSchemas`, `modernizeRules`, `dependencies`, and compatibility versions.
- `data/registry/plugins/*/manifest.json`: registry-visible release metadata,
  tier, downloads, keywords, and public docs information.
- `wfctl audit plugins`: manifest shape and strict-contract coverage.
- `manifest.Analyze`: application-level infrastructure and service
  requirements from Workflow config.
- `wfctl.yaml` and `.wfctl-lock.yaml`: declared and locked app plugin usage.
- Installed plugin manifests in `WFCTL_PLUGIN_DIR`: local provider capabilities
  actually available to the app.

## Capability Model

Add a reusable Go package under `capability/inventory` or
`capability/matrix`. The package should define a stable JSON schema around these
concepts:

```go
type Capability struct {
    ID          string
    Category    string
    Name        string
    Description string
    Lifecycle   string // released, local, inferred, deprecated, unknown
    Providers   []Provider
    Evidence    []Evidence
    Findings    []Finding
}

type Provider struct {
    Name          string
    Kind          string // core-plugin, external-plugin, package, provider
    Version       string
    ReleaseStatus string // released, local-only, missing-registry, unknown
    Source        string
    Capabilities  []string
}

type Usage struct {
    CapabilityID string
    Mode         string // declared, inferred
    Confidence   string // high, medium, low
    Evidence     []Evidence
    Findings     []Finding
}

type Evidence struct {
    SourceKind string // plugin.json, registry, wfctl.yaml, lockfile, workflow-config, audit
    SourcePath string
    JSONPath   string
    Detail     string
}
```

Names are illustrative; implementation can refine the exact struct shape. The
required behavior is stable IDs, provider rows, source evidence, release status,
and confidence for inference.

## Ecosystem Capability Matrix

Add a `wfctl capability ecosystem` command that scans registry and local plugin
repos and emits JSON or Markdown:

```sh
wfctl capability ecosystem \
  --repo-root .. \
  --registry data/registry \
  --format json \
  --output docs/generated/capabilities/ecosystem.json
```

The command should include both released/registry-visible and local plugin repo
capabilities. Rows must show release state rather than hiding local-only entries.

Required row groups:

- Core/built-in plugin capabilities from `data/registry` and built-in manifests.
- External/local plugin capabilities from `workflow-plugin-*` repos.
- Provider capabilities: GitHub, GitLab, AWS, Azure, GCP, DigitalOcean, Vault,
  secrets/env management, CI generation, IaC services, state backends.
- Cross-cutting product capabilities: authn, authz, SSO, multi-tenancy,
  secrets, observability, migrations, feature flags, payments, messaging,
  storage, audit, moderation, security scanning, deployment/CI, docs/API
  generation.
- Contract/test health from plugin audit output when available.

The matrix should not pretend every low-level module or step type is a product
capability. The generator should map known type names into a curated taxonomy
and include an `uncategorized` section for newly discovered types that need
taxonomy review.

## Application Capability Profile

Add a `wfctl capability app` command that reads an application and emits a
profile:

```sh
wfctl capability app \
  --manifest wfctl.yaml \
  --workflow workflow.yaml \
  --plugin-dir .wfctl/plugins \
  --format json \
  --output docs/generated/capabilities/app.json
```

It should classify usage as:

- `declared`: explicit in `wfctl.yaml`, `.wfctl-lock.yaml`, plugin manifests,
  Workflow module/step/trigger config, provider config, or environment config.
- `inferred`: detected from config patterns such as auth middleware, tenant
  fields, tenant-scoped modules, secret references, route protection, provider
  secrets, deploy config, or CI config.
- `missing-provider`: app uses or references a capability but no locked or
  installed provider declares it.
- `policy-risk`: app already uses a cross-cutting capability and a new or
  adjacent surface appears not to respect it.

Inferred rows must show evidence and confidence. Low-confidence findings should
not fail by default.

## Application Consistency Check

Add a `wfctl capability check` command after the read-only profile command. It
should run policy-style checks suitable for agents and CI:

```sh
wfctl capability check --manifest wfctl.yaml --workflow workflow.yaml
```

Initial checks:

- If tenancy is declared or inferred, HTTP routes, storage modules, database
  steps, and provider resources should either show tenant scoping evidence or a
  documented exemption.
- If authz is declared or inferred, new routes/actions should have authz
  evidence, not only authn.
- If secrets are declared, provider secret stores and generated env references
  should resolve to a known provider/scope.
- If a plugin type is used, a provider should be declared in `wfctl.yaml`,
  lockfile, installed plugin manifests, or registry metadata.

The first implementation can report warnings and JSON findings only. Strict
failure mode can be added once the evidence model is stable.

## Website Documentation

Workflow owns generated outputs. `gocodealone-website` consumes those outputs.
Initial generated artifacts:

- `docs/generated/capabilities/schema.json`
- `docs/generated/capabilities/ecosystem.json`
- `docs/generated/capabilities/ecosystem.md`
- Optional sample `docs/generated/capabilities/app-profile-example.json`

The website should render the ecosystem JSON into searchable docs/pages and link
back to provider/plugin docs. Workflow should not add website-only extraction
logic.

## CLI User Experience

Commands should support `--format json|md`, `--output`, and read-only default
behavior. Human output should be concise and table-oriented; JSON should carry
the full evidence graph. Errors should distinguish malformed source files from
soft inventory gaps.

Example human summary:

```text
Capability ecosystem: 94 providers, 31 released, 63 local, 12 warnings

CATEGORY       CAPABILITY       PROVIDERS                  STATUS
auth           authn            workflow-plugin-auth       released
auth           authz            workflow-plugin-authz      released
platform       github-secrets   workflow-plugin-github     local
tenancy        tenant-scope     workflow-plugin-platform   inferred
```

## Security Review

The ecosystem command reads local manifests and registry JSON only. It should
not execute plugin binaries. It may read Git metadata and local files, but it
must not upload data.

The app command reads application config and may expose capability names,
provider names, env var names, route paths, and module names. It must not print
secret values. Evidence should point to keys/paths, not resolved secret content.

The website-facing JSON should be appropriate for public docs. If app profiles
are generated for private applications, they should remain local unless the app
repo explicitly commits them.

## Infrastructure Impact

No cloud resources, secrets, databases, queues, or production services are
created or modified. This is local read-only analysis and generated docs output.
CI impact is limited to new tests and optional future documentation generation.

## Multi-Component Validation

Validation must prove both outputs:

- Ecosystem path: fixture registry + fixture local plugin repo →
  `wfctl capability ecosystem --format json` → expected released/local rows and
  evidence.
- App path: fixture app with `wfctl.yaml`, lockfile, Workflow config, and plugin
  manifests → `wfctl capability app --format json` → expected declared/inferred
  rows and policy findings.
- CLI/docs path: generated Markdown renders deterministic tables and JSON
  validates against the emitted schema.

Website validation can be a follow-up PR in `gocodealone-website` that consumes
the Workflow artifact and builds the docs site.

## Rollback

The first implementation is additive. Rollback is to revert the new package,
CLI subcommands, generated docs, and tests. Existing `wfctl audit`,
`wfctl docs generate`, plugin install, and runtime loading paths are unchanged.

If website consumption is added later and fails, revert the website sync change
while keeping the Workflow JSON artifact available for CLI and agents.

## Assumptions

- Plugin manifests and registry manifests are sufficient to seed most provider
  rows, even if a curated taxonomy is needed above raw type names.
- `manifest.Analyze` and existing config readers can provide enough app-level
  facts for useful declared/inferred profiles.
- Inference will produce some false positives, so it must be marked with
  evidence and confidence.
- Website docs can consume generated JSON/Markdown from Workflow release
  artifacts or a checked-in generated snapshot.
- Agents can be instructed to consult the matrix/profile during design review
  once the command exists.

## Deferred

- Runtime plugin execution for capability verification. Use existing
  `verify-capabilities` and conformance commands for runtime checks.
- Strict CI failure mode for app policy findings. Start with warnings until the
  signal is trustworthy.
- Full website implementation. This design defines the Workflow-owned artifact;
  website rendering should be a downstream PR.
- Automatic code scanning of arbitrary application source files. First pass
  reads Workflow-owned config and manifests.

## Acceptance Criteria

- `wfctl capability ecosystem` emits deterministic JSON and Markdown from a
  registry plus local plugin repos.
- `wfctl capability app` emits declared and inferred application capability
  usage with evidence and confidence.
- `wfctl capability check` reports initial policy findings without reading or
  printing secret values.
- Generated ecosystem JSON includes both released and local-only capabilities.
- Generated Markdown can be consumed by website docs without custom extraction.
- Tests cover registry-only, local-only, mixed release state, inferred app
  capability, missing provider, and policy-risk cases.

## Related Decision

See `decisions/0049-capability-inventory-source.md`.
