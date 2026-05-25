# IaC Derived Requirements Design

**Date:** 2026-05-25
**Status:** Draft — revised after adversarial-review cycle 1
**Owner:** autonomous pipeline
**Related:** `decisions/0043-iac-derived-requirements.md`

## Problem

Workflow configs increasingly describe application intent at a higher level than
the concrete infrastructure needed to run it. A user can declare an API app, a
NATS broker, or observability for OTel, Prometheus, Loki, Grafana, or Datadog,
but today the user or an agent still has to hand-write the matching `infra.*`
modules. That is error-prone and provider-specific.

The target behavior is explicit and reviewable:

1. Application and plugin declarations emit provider-neutral requirements.
2. IaC provider plugins map those requirements to provider/runtime-specific
   `infra.*` modules.
3. `wfctl` expands the YAML before apply, so the resulting config is visible in
   review and CI.
4. Users can satisfy a requirement manually by declaring a stable key in YAML;
   `wfctl` must not guess by resource similarity.

## Current Inventory

- `config.PluginManifestFile.ModuleInfraRequirements` and
  `cmd/wfctl/plugin_infra.go` already provide a static manifest-only dependency
  detector. It is too weak for provider/runtime mapping, explicit satisfaction,
  or observability variants.
- `cmd/wfctl/infra.go` plans and applies `infra.*` and `platform.*` modules
  from the top-level `modules:` list. The old `infrastructure:` block is
  preserved by config round-trips but is not the active provisioning path.
- IaC plugins already expose strict typed gRPC services through
  `plugin/external/proto/iac.proto`. That proto explicitly avoids
  `google.protobuf.Struct` and `Any`; provider-specific free-form payloads use
  JSON bytes fields.
- `workflow-editor` uses `js-yaml` and preserves top-level ordering plus unknown
  top-level keys, but it is not an AST-preserving comment-safe YAML mutator.
  `wfctl` should use Go's existing `gopkg.in/yaml.v3` node API for targeted
  edits instead of importing editor code.
- DigitalOcean, AWS, GCP, and Azure plugins all expose typed IaC capabilities
  for common canonical resource types such as `infra.container_service`,
  `infra.k8s_cluster`, `infra.database`, `infra.cache`, and `infra.storage`.

## Goals

1. Add a provider-neutral requirement model that can represent observability,
   web/API apps, message brokers, databases, caches, storage, and future
   support resources without provider names leaking into application config.
2. Add `wfctl infra derive` to calculate missing requirements and write the
   expanded YAML before `infra plan` / `infra apply`.
3. Add deterministic explicit overrides through `modules[].satisfies`.
4. Add a strict-proto provider mapping surface so provider plugins can describe
   how requirements map to their supported resource types.
5. Support observability choices across OTel first, plus Prometheus, Loki,
   Grafana, and Datadog without baking those products into Workflow core
   provider logic.
6. Keep the first implementation small: generate concrete modules and leave
   provisioning to the existing IaC pipeline.

## Non-Goals

- Do not derive at apply time. `infra plan` and `infra apply` should consume the
  already-expanded YAML.
- Do not infer that an arbitrary existing resource satisfies a requirement by
  comparing config. Only explicit `satisfies` keys count.
- Do not build a full policy engine or Terraform-style expression language.
- Do not make Workflow core own AWS, GCP, Azure, DigitalOcean, Datadog, or
  Grafana resource mapping rules.
- Do not commit application-specific names such as `cms_*` or `multisite_*` to
  generic observability or IaC plugins.
- Do not require the visual editor to preserve every comment before this can
  ship. The CLI owns YAML expansion; editor preservation improvements can follow.

## Approaches Considered

### Approach A: Core derivation engine, provider plugin mappers

`wfctl` gathers requirements from workflow config, installed plugin metadata,
and optional plugin/provider typed services. It asks the selected IaC provider
plugin to map the unsatisfied requirements to concrete `infra.*` module
stubs, then writes those modules into YAML with `satisfies` keys.

Pros: clear ownership boundary; works across DO/AWS/GCP/Azure; keeps provider
specifics out of core; produces reviewable YAML. Cons: adds a new provider
optional service and a YAML mutator.

This is the recommended approach.

### Approach B: Observability plugin directly writes provider YAML

The observability plugin could expose a CLI command that edits config for OTel,
Datadog, Prometheus, Loki, and Grafana.

Pros: fast for observability. Cons: repeats the same problem for NATS, web/API,
databases, caches, and provider runtimes; plugin CLI commands do not naturally
own cross-provider IaC mapping or core YAML ordering rules.

Rejected.

### Approach C: Derive inside `infra apply`

`infra apply` could synthesize missing resources in memory just before planning.

Pros: smallest command surface. Cons: hides generated infrastructure from review,
CI diffs, and agents; makes user overrides fuzzy; conflicts with the user's
explicit request for a YAML-expanding command.

Rejected.

## Design

### Requirement Model

Add a small Workflow-owned requirement model in `interfaces` or a new `iac/derive`
package, mirrored in `plugin/external/proto/iac.proto`.

Core fields:

- `key`: stable requirement identifier, for example
  `observability.telemetry.default` or `messaging.nats.events`.
- `kind`: typed proto enum for the broad category, for example
  `REQUIREMENT_KIND_OBSERVABILITY`, `REQUIREMENT_KIND_WEB_API`,
  `REQUIREMENT_KIND_MESSAGE_BROKER`, `REQUIREMENT_KIND_DATABASE`,
  `REQUIREMENT_KIND_CACHE`, and `REQUIREMENT_KIND_STORAGE`.
- `source`: module/service/plugin path that produced the requirement.
- `resource_type_hint`: optional canonical target such as
  `infra.container_service`, `infra.k8s_cluster`, or `infra.database`.
- `environment`: optional target environment.
- `runtime`: optional typed proto enum such as `RUNTIME_KUBERNETES`,
  `RUNTIME_ECS`, `RUNTIME_CLOUD_RUN`, `RUNTIME_AZURE_CONTAINER_APPS`, or
  `RUNTIME_DO_APP_PLATFORM`.
- `signals`: repeated typed proto enum for telemetry signals:
  `TELEMETRY_SIGNAL_TRACES`, `TELEMETRY_SIGNAL_METRICS`, and
  `TELEMETRY_SIGNAL_LOGS`.
- `backends`: repeated typed proto enum for requested observability backends:
  `OBSERVABILITY_BACKEND_OTEL`, `OBSERVABILITY_BACKEND_DATADOG`,
  `OBSERVABILITY_BACKEND_PROMETHEUS`, `OBSERVABILITY_BACKEND_LOKI`, and
  `OBSERVABILITY_BACKEND_GRAFANA`.
- `deployment_modes`: repeated typed proto enum for portable deployment shape:
  `DEPLOYMENT_MODE_SIDECAR`, `DEPLOYMENT_MODE_DAEMONSET`,
  `DEPLOYMENT_MODE_SIBLING_SERVICE`, and `DEPLOYMENT_MODE_MANAGED`.
- `vendor_features`: repeated strings for provider/product-specific extension
  flags that do not justify a Workflow-owned enum. These are non-portable and
  must be namespaced, for example `datadog.apm` or `grafana.datasource`.
- `parameters_json`: JSON bytes only for data that cannot be modeled
  generically without provider or product leakage.

`parameters_json` is a deliberate exception to pure scalar proto fields. It
follows the existing strict IaC proto pattern for provider-specific
`config_json` and avoids `Struct` / `Any`. The default is typed proto enums;
strings are allowed only for explicitly namespaced vendor extensions.

### Satisfaction Keys

Add `satisfies` to module config as a top-level field on `config.ModuleConfig`:

```yaml
modules:
  - name: app-telemetry
    type: infra.container_service
    satisfies:
      - observability.telemetry.default
    config:
      image: otel/opentelemetry-collector-contrib:latest
```

The derivation engine considers a requirement satisfied when any module lists
that key. It does not inspect resource type, name, labels, sidecars, or provider
config to infer equivalence.

Generated modules must include `satisfies` so repeated runs are idempotent.

### Provider Mapping Service

Add an optional strict-proto service to `plugin/external/proto/iac.proto`:

```proto
service IaCProviderRequirementMapper {
  rpc MapRequirements(MapRequirementsRequest) returns (MapRequirementsResponse);
}
```

The request contains typed requirement messages plus provider/runtime/environment
context. The response returns:

- accepted requirement keys,
- rejected requirement diagnostics,
- concrete `ResourceSpec` messages to write as `modules:` entries,
- optional ordered notes for interactive display.

Provider plugins register this service only when they support mapping. Absence
means the provider can still plan/apply explicit `infra.*` modules, but `wfctl
infra derive` cannot ask it to synthesize provider-specific modules.

This matches the existing typed-IaC optional-service pattern: registration is
the capability signal, not a boolean flag or an unimplemented response field.

### Requirement Sources

Initial requirement sources:

- Built-in config shape: web/API modules, `services:`, and common broker/cache
  module types can produce neutral requirements.
- Plugin static declarations: evolve `moduleInfraRequirements` into a v2 shape
  that can carry `key`, `kind`, telemetry signals, observability backends,
  deployment modes, vendor features, and `resource_type_hint` while preserving
  the existing v1 fields.
- Observability plugin declarations: `observability.telemetry` and
  `observability.collector` declare observability requirements and supported
  backends without application-specific names.

Runtime/config-aware source:

- Add a lightweight Go interface for in-process providers, for example
  `IaCRequirementProvider`, that modules, steps, or plugin adapters can satisfy
  without Workflow core depending on a concrete observability plugin package.
- Add an optional strict-proto external-plugin service for config-aware
  requirement discovery. It returns the same typed requirement messages consumed
  by the provider mapper. Absence of service registration means the plugin only
  participates through static manifest declarations.
- The first observability implementation should use static declarations when
  the module config is simple and the dynamic service when selected backends or
  signal sets depend on module config.

### `wfctl infra derive`

Add:

```sh
wfctl infra derive --config workflow.yaml --provider aws --env staging --runtime ecs --write
wfctl infra derive --config workflow.yaml --provider digitalocean --env prod --runtime do_app_platform --dry-run --format yaml
wfctl infra derive --config workflow.yaml --provider gcp --env prod --non-interactive --write
```

Behavior:

1. Load the workflow YAML as `yaml.Node`.
2. Load the semantic config through `config.LoadFromFile`.
3. Gather requirements.
4. Remove requirements already listed by `modules[].satisfies`.
5. Resolve provider plugin and optional mapper service.
6. In interactive TTY mode, prompt only for ambiguous provider/runtime choices.
7. In non-interactive mode, fail on ambiguity with a deterministic diagnostic.
8. Insert generated modules into `modules:` while preserving existing order,
   comments, unknown top-level keys, and unknown module keys where `yaml.v3`
   can retain them.
9. Print a summary of added and already-satisfied requirement keys.

`--dry-run` prints the expanded YAML or a JSON summary without writing. `--write`
updates the file atomically. A later `--output` can write to a separate path.

Provider/runtime precedence:

1. Explicit `--provider` and `--runtime` flags.
2. Environment-specific provider hints already present in config.
3. A single unambiguous `iac.provider` module.
4. Otherwise an ambiguity diagnostic in non-interactive mode or a prompt in TTY
   mode.

Multi-file behavior for v1:

- `config.LoadFromFile` still resolves imports to gather requirements.
- `wfctl infra derive --write` mutates only the root `--config` file.
- Generated modules are written to the root file even when the source
  requirement came from an imported file. The summary must include the source
  path so the user can move the generated module manually if desired.
- A future `--target-file` can direct generated modules into a specific imported
  file, but v1 does not guess ownership.

### Observability Mapping

The observability plugin remains generic. Applications choose names and backends
in their own YAML. The requirement features describe intent:

- OTel collector: `backend.otel`, selected signals, deployment mode.
- Prometheus: prefer OTel metrics exporter or scrape config when possible.
- Loki: prefer OTel logs exporter when possible.
- Grafana: dashboards/datasource requirements only when a provider plugin
  supports them; otherwise diagnostics explain the manual external setup.
- Datadog: prefer OTel exporter to Datadog. Direct Datadog agent sidecar or
  service is supported when the requirement asks for `backend.datadog` and the
  provider mapper has a runtime-specific mapping.

Examples:

- Kubernetes provider mapper may produce an OTel Collector deployment/daemonset
  and Prometheus/Loki exporter config.
- ECS provider mapper may produce Datadog agent sidecar definitions or an OTel
  collector sidecar depending on selected backend.
- DigitalOcean App Platform mapper may produce sibling service components
  because its driver already models "sidecars" as sibling services.

Secret handling:

- Requirement mappers must not return plaintext API keys, tokens, connection
  strings, or private keys in generated `ResourceSpec.Config`.
- Generated specs may reference secrets through `${SECRET_NAME}` placeholders
  or add separate secret-generation/secret-requirement declarations.
- `wfctl infra derive` rejects generated YAML when provider output includes
  suspicious plaintext secret-looking keys such as `api_key`, `token`,
  `password`, `private_key`, or `secret` unless the value is a placeholder.
- Observability examples that need Datadog, Grafana, Prometheus remote-write, or
  Loki credentials must generate secret references, not values.

### YAML Mutation

Use `gopkg.in/yaml.v3` node editing in a new small package, for example
`config/yamledit` or `iac/yamledit`.

Rules:

- Keep the original document root and mapping order.
- Create `modules:` if missing.
- Append generated modules after the last existing `infra.*` module; if no
  `infra.*` module exists, append at the end of `modules:`.
- Preserve comments and unknown keys by editing nodes rather than unmarshalling
  and re-marshalling the full config struct.
- Do not try to preserve comments inside newly generated nodes beyond a concise
  generated block comment.
- Add golden tests with comments, anchors, unknown top-level keys, unknown module
  keys, empty `modules:`, and repeated derive runs.

The editor's `js-yaml` ordering behavior is precedent only. It is not sufficient
for CLI mutation because it does not retain comments.

Editor preservation:

- Add `satisfies?: string[]` to `workflow-editor`'s `ModuleConfig` type and
  serialization path in the same implementation plan or a prerequisite PR.
- Until that lands, `wfctl infra derive` should warn when it detects a project
  workflow-editor version below the first version that preserves `satisfies`.
- Editor support is preservation-only for v1; the visual UI does not need to
  author derived requirements before the CLI command ships.

### CLI Plugin Evaluation

This should not be implemented as a CLI plugin. A provider or observability
plugin can add convenience commands later, but the derivation command needs
first-class access to config loading, plugin discovery, typed IaC provider
handles, and atomic YAML writes. Those are core `wfctl` responsibilities.

CLI plugin functionality is still useful for provider-specific diagnostics
after derivation, for example `datadog verify` or `grafana datasource test`, but
not for the generic derivation engine.

### IaC Plugin Evaluation

IaC plugin functionality is the right extension point for mapping. Core can
define the neutral requirement model and command behavior; provider plugins own
the translation from requirements to `ResourceSpec` for Kubernetes, ECS, Cloud
Run, Azure Container Apps, DigitalOcean App Platform, and future runtimes.

This also creates a reusable pattern for non-observability cases:

- web/API app declaration -> `infra.container_service`, ingress, registry,
  certificate, DNS as supported by the provider.
- NATS declaration -> managed broker, Kubernetes workload, ECS sidecar, or
  explicit unsupported diagnostic.
- database/cache/storage declaration -> provider-managed resource or local
  container for development.

## Backwards Compatibility

- Existing configs continue to plan/apply. `satisfies` is optional.
- Existing provider plugins continue to work; only plugins implementing the new
  optional mapper service participate in derivation.
- Existing `moduleInfraRequirements` entries remain valid. The v2 fields are
  additive.
- `infra plan` and `infra apply` do not auto-derive.

## Assumptions

1. `modules:` remains the canonical place for provisioned `infra.*` resources.
2. Provider plugins can map at least common observability and app requirements
   without needing live cloud API calls.
3. The strict-proto IaC optional-service pattern is acceptable for another
   optional provider capability.
4. `yaml.v3` node mutation can preserve enough source fidelity for safe
   machine edits. Tests will define the exact guarantee.
5. Interactive ambiguity is limited to provider/runtime/backend choices; if this
   becomes wider, the design should stop and add a richer planning report rather
   than more prompts.
6. Root-file expansion is acceptable for v1 multi-file configs as long as the
   command reports the source path for each generated requirement and remains
   idempotent.

## Rollback

This affects CLI behavior, plugin contracts, YAML config schema, and provider
plugin loading paths.

- Core command rollback: revert the `wfctl infra derive` PR. Existing expanded
  YAML remains valid because generated resources are normal `infra.*` modules.
- Proto/service rollback: because the provider mapper is optional, reverting
  the host support leaves existing provider plugins usable for plan/apply.
  Plugins that already implemented the mapper must pin the workflow version that
  includes it.
- YAML field rollback: `satisfies` is additive. Older engines ignore the field
  once generated YAML contains concrete modules.
- Provider mapper rollback: release a provider patch that stops advertising the
  optional service; `wfctl infra derive` will emit an unsupported-provider
  diagnostic instead of generating modules.

## Self-Challenge

1. Laziest plausible solution: extend `moduleInfraRequirements` and add a
   scaffold command with no provider plugin RPC. This fails the cross-provider
   runtime mapping requirement and would bake provider choices into core.
2. Fragile assumption: provider mapping does not need live cloud calls. If false,
   the mapper RPC must accept credentials/config and may become slow. The first
   implementation should fail if a mapper tries to perform apply-like work.
3. YAGNI risk: modeling every observability backend upfront could get heavy.
   Mitigation: v1 defines enums only for the required OTel, Datadog,
   Prometheus, Loki, and Grafana path, and provider plugins can reject
   unsupported combinations.
4. Partial failure: YAML write corruption would be the highest-impact bug.
   Mitigation: parse as `yaml.Node`, write atomically, and add golden/idempotence
   tests before implementation.
5. Repo precedent conflict: current IaC proto avoids `Struct`/`Any`. This design
   follows that precedent with typed proto enums for portable concepts; only
   provider-specific free-form fields use JSON bytes, matching existing
   `ResourceSpec.config_json`.

## Adversarial Review Resolution

Cycle 1 report:
`docs/plans/2026-05-25-iac-derived-requirements-design.adversarial-review-1.md`.

Resolved changes:

- Replaced stringly typed `kind`, `runtime`, and observability `features` with
  proto enum fields; kept namespaced strings only for vendor extensions.
- Defined v1 multi-file behavior as root-file expansion with source diagnostics.
- Promoted plugin/module requirement providers into v1 via a Go interface plus
  optional strict-proto external-plugin service.
- Added secret-output rules and `wfctl` rejection behavior for generated
  plaintext secrets.
- Added workflow-editor preservation work for `modules[].satisfies`.
- Added provider/runtime precedence rules.
