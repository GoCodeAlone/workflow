### Adversarial Review Report

**Phase:** design
**Artifact:** `docs/plans/2026-05-25-iac-derived-requirements-design.md`
**Status:** FAIL

**Findings (Critical):**
- None.

**Findings (Important):**
- [Repo-precedent conflicts / strict proto] `Requirement Model` lines 124-138:
  the design says `kind`, `runtime`, and `features` are strings, even though the
  user explicitly asked to be strict-proto compatible where possible and the IaC
  proto already uses enums for wire-stable concepts such as drift class. A
  string vocabulary is easy to typo and hard for provider plugins to validate
  compatibly. Recommendation: make category, signal, backend, deployment mode,
  and runtime typed proto enums; reserve repeated string extensions only for
  vendor-specific features with a documented non-strict reason.
- [Missing failure modes] `wfctl infra derive` lines 220-229: config loading
  resolves imports, but YAML mutation edits one file. If a requirement comes
  from an imported module, the design does not say whether generated modules go
  to the root file, the source file, or a new derived file. This can corrupt
  ownership boundaries in multi-file configs. Recommendation: define v1 as
  root-file expansion only, with `source` diagnostics and an explicit future
  `--target-file`; tests must cover imported configs.
- [User-intent drift / plugin interface] `Requirement Sources` lines 191-206:
  dynamic plugin-side requirement providers are deferred to the future, but the
  user's Go-interface analogy asks for plugins/modules/steps to satisfy an
  interface without hard dependency. Static manifest requirements are not enough
  for config-driven observability backends. Recommendation: include a v1
  lightweight Go interface for in-process providers and a strict-proto
  external-plugin service, even if the first implementation supports static
  manifests too.
- [Security/privacy] `Provider Mapping Service` lines 174-180: provider
  mappers return concrete `ResourceSpec` configs, but there is no rule
  preventing secret material from being written into YAML. Observability and
  Datadog configs frequently involve API keys. Recommendation: require mappers
  to emit only secret references or `secrets.generate` requirements; `wfctl`
  rejects generated specs containing known secret-looking plaintext values.
- [Repo-precedent conflicts / editor preservation] `Satisfaction Keys` lines
  146-162: adding top-level `modules[].satisfies` to Go config is not enough.
  `workflow-editor`'s `ModuleConfig` type does not include that field, and its
  `js-yaml` round-trip reconstructs module objects. Editing generated YAML in
  the editor can silently drop satisfaction keys. Recommendation: include
  workflow-editor type/serialization preservation in the plan or explicitly
  block editor round-trip support until it lands.

**Findings (Minor):**
- [Missing failure modes] `wfctl infra derive` lines 212-216: provider and
  runtime are passed as flags, but existing configs already declare
  `iac.provider` modules and per-resource `iac_provider`/`provider` refs.
  Recommendation: define precedence: explicit flag, then env-specific
  provider, then single configured provider, else ambiguity error.
- [YAGNI] `Requirement Model` lines 129-136: modeling every runtime and backend
  in the core design risks expanding before the first implementation proves the
  shape. Recommendation: enumerate only the required observability/runtime
  constants for v1 and keep vendor extension fields for provider plugins.

**Bug-class scan transcript:**

| Class | Result | Note |
|---|---|---|
| Unstated assumptions | Finding | The design assumes root-file YAML writes are acceptable after imported config resolution. |
| Repo-precedent conflicts | Finding | The string feature vocabulary conflicts with `plugin/external/proto/iac.proto`'s strict typed-service precedent. |
| YAGNI violations | Finding | The design risks over-modeling future backends/runtimes before the first provider mapper exists. |
| Missing failure modes | Finding | Multi-file imports, provider-precedence ambiguity, and generated secret handling need explicit behavior. |
| Security / privacy | Finding | Provider-generated ResourceSpecs may accidentally write API keys or tokens into YAML. |
| Rollback story | Clean | The rollback section covers CLI, proto, YAML schema, and provider mapper rollback. |
| Simpler alternative not considered | Clean | Static manifest-only scaffolding and apply-time derivation were considered and rejected. |
| User-intent drift | Finding | Deferring the plugin/module interface weakens the user's stated composability requirement. |

**Options the author may not have considered:**

1. Root-only generated overlay file: instead of mutating the main config, always
   write `workflow.derived.yaml` and add it to `imports:`. This improves review
   isolation but requires import-order guarantees and may be surprising when a
   user expected in-place expansion.
2. Manifest-v2 first, mapper service later: ship a smaller static-only scaffold
   and defer provider RPCs. This is simpler, but it fails the runtime-specific
   sidecar/daemonset/ECS/DO mapping requirement and would likely be reworked.

**Verdict reasoning:** The design direction is sound, but the strict-proto,
multi-file, secret-safety, and plugin-interface gaps are important enough to fix
before writing an implementation plan. No critical flaw requires abandoning the
approach.

