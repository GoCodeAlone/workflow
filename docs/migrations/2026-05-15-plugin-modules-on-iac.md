# 2026-05-15 — Plugin-modules-on-IaC: Phase B + Phase C clean break

This migration covers **Phase B** (AWS / DigitalOcean) and **Phase C** (GCP)
of the
[plugin-modules-on-IaC plan](../plans/2026-05-15-plugin-modules-on-iac.md).
Workflow-core sheds the remaining in-core cloud-SDK-bearing surfaces:
S3/Spaces/GCS IaC state stores, `storage.s3` + `storage.gcs` modules,
`step.s3_upload`, the in-core `gkeBackend`, and the SDK-bearing AWS
profile/role_arn credential resolvers. Each surface is now plugin-native.

Phase B shipped in PR `feat/phase-b-core-deletion`; Phase C in
`feat/phase-c-core-deletion`. Engine + plugin versioning is covered below.

## Engine floor

Both phases require **workflow `>= v0.53.0`** in any deployment that uses the
affected backends. The `>= v0.53.0` engine has the typed `IaCStateBackend`
gRPC contract (Phase A, decisions/0036), the `Configure` RPC that delivers the
`iac.state` module YAML to the plugin, and the plugin-backend registry that
`IaCModule.Init` consults in its `default:`-arm. Phase C additionally relies on
the `grpcKubernetesBackend` adapter + plugin-backend registry shipped in PR
`#681` (ADR 0037) for the `platform.kubernetes type: gke` resolution path.

## What changed

| Surface | Was | Now |
|---|---|---|
| `iac.state` `backend: spaces` | in-core `module.SpacesIaCStateStore` | plugin-served by [`workflow-plugin-digitalocean`](https://github.com/GoCodeAlone/workflow-plugin-digitalocean) `>= v1.1.0` |
| `iac.state` `backend: s3` | (already moved in v0.53.0; no in-core impl since then) | plugin-served by [`workflow-plugin-aws`](https://github.com/GoCodeAlone/workflow-plugin-aws) `>= v1.1.0` |
| `storage.s3` module | in-core `module.S3Storage` (registered by `plugins/storage`) | plugin-native in `workflow-plugin-aws >= v1.1.0` |
| `step.s3_upload` pipeline step | in-core `module.S3UploadStep` (registered by `plugins/pipelinesteps`) | plugin-native in `workflow-plugin-aws >= v1.1.0` |
| `cloud.account` `provider: aws` + `credentials.type: profile` or `role_arn` | SDK-bearing resolver loaded the profile / called `sts:AssumeRole` in-core | core records a `credential_source` marker only; the aws plugin performs SDK resolution via `awscreds.BuildAWSConfig` (decisions/0036 + 0038) |
| `iac.state` `backend: gcs` | in-core `module.GCSIaCStateStore` (via `cloud.google.com/go/storage`) | plugin-served by [`workflow-plugin-gcp`](https://github.com/GoCodeAlone/workflow-plugin-gcp) `>= v1.1.0` |
| `storage.gcs` module | in-core `module.GCSStorage` (registered by `plugins/storage`) | plugin-native in `workflow-plugin-gcp >= v1.1.0` |
| `platform.kubernetes` `type: gke` | in-core `gkeBackend` (via `google.golang.org/api/container/v1`) | plugin-served by `workflow-plugin-gcp >= v1.1.0`; routed through the `grpcKubernetesBackend` adapter (ADR 0037) |

The YAML field names and `backend:` / `type:` / `provider:` values are
**unchanged**. The break is strictly about *which binary* serves them.

`platform.kubernetes type: kind`, `k3s`, `eks`, and `aks` stay in core
(kind/k3s are in-memory test backends; eks is an actionable-error stub
pointing at `workflow-plugin-aws`; aks uses Azure REST + OAuth2 with no
Azure-SDK import — see Phase A's `cloud_account_azure.go` rewrite).

## Why

Workflow core owns IaC orchestration interfaces, not provider SDKs. Provider
SDKs ride with the provider plugin, where Dependabot bumps and SDK CVE patches
belong. This continues the pattern set by the `godo` removal
([v0.52.0](v0.52.0-godo-removal.md)) and the AWS IaC core removal
([v0.53.0](v0.53.0-aws-iac-removal.md)).

## Breaking change — action required

### `iac.state backend: spaces`

Load `workflow-plugin-digitalocean >= v1.1.0`. The YAML `backend: spaces`
value is unchanged; all existing config keys (`region`, `bucket`, `prefix`,
`accessKey`, `secretKey`, `endpoint`) keep their semantics. The
`DO_SPACES_ACCESS_KEY` / `DO_SPACES_SECRET_KEY` environment fallbacks are
preserved by the plugin port.

Without the plugin, `IaCModule.Init` fails fast:

```
iac.state "<name>": backend "spaces" is not built into workflow core
(in-core backends: 'memory', 'filesystem', 'postgres').
If "spaces" is a plugin-provided backend (e.g. 'azure_blob' via
workflow-plugin-azure, 'spaces' via workflow-plugin-digitalocean,
's3' via workflow-plugin-aws, 'gcs' via workflow-plugin-gcp),
install and load that plugin
```

### `iac.state backend: s3`

Load `workflow-plugin-aws >= v1.1.0`. Same shape as the `spaces` migration —
YAML unchanged, error message above identifies the missing plugin.

### `storage.s3` module + `step.s3_upload` pipeline step

Both move into `workflow-plugin-aws >= v1.1.0`. Credentials can be inline in
the module/step config, or referenced via `credentials_ref:` pointing at an
`aws.credentials` module loaded by the plugin. With no plugin loaded the
module type / step type is unknown at engine boot — load the plugin in the
deployment's plugin manifest.

### `cloud.account provider: aws` with `credentials.type: profile` or `role_arn`

The credential config sits under the nested `credentials:` map on the
`cloud.account` module (the key is `credentials.type`, not a flat
`credentialType:`). The affected shape:

```yaml
modules:
  - name: aws-account
    type: cloud.account
    config:
      provider: aws
      region: us-east-1
      credentials:
        type: profile        # or role_arn
        profile: team-prod   # for type=profile
        # roleArn / externalId / sessionName for type=role_arn
```

Core no longer resolves the profile or calls `sts:AssumeRole`. Instead the
resolver records `Extra["credential_source"] = "profile"` or `"role_arn"`
(plus `Extra["profile"]` / `m.creds.RoleARN` + `Extra["external_id"]`) and
logs a `workflow: aws credential_source=…` warning.

The aws plugin's `awscreds.BuildAWSConfig` consumes the marker at the point of
need and performs the SDK-bearing resolution in-plugin. This is a
**co-deploy** requirement: core `>= v0.53.0` AND `workflow-plugin-aws
>= v1.1.0` must be deployed together. Mixing an old plugin against new core
results in a `credential_source` marker the plugin can't interpret — the core
warning is what tells operators which side to upgrade.

`credentials.type: static` and `credentials.type: env` are unaffected — those
paths have always been SDK-free and resolve in-core.

### `iac.state backend: gcs`

Load `workflow-plugin-gcp >= v1.1.0`. The YAML `backend: gcs` value and all
config keys (`bucket`, `prefix`, plus any GCP credential config) keep their
semantics. Application Default Credentials and service-account JSON resolution
still work — they just happen in the plugin process now.

Without the plugin, `IaCModule.Init` returns the same actionable error as the
spaces/s3 cases (in-core backends list now `'memory', 'filesystem',
'postgres'`; plugin examples list includes `'gcs' via workflow-plugin-gcp`).
The wfctl direct-path commands (`wfctl infra ...`) return the same shape:

```
iac.state backend "gcs" is now plugin-served by workflow-plugin-gcp v1.1.0;
install and load the plugin to use the GCS backend (wfctl direct-path
commands no longer support in-tree gcs)
```

### `storage.gcs` module

Moves into `workflow-plugin-gcp >= v1.1.0`. Same shape as `storage.s3`:
credentials inline or referenced via `credentials_ref:` pointing at a
`gcp.credentials` module loaded by the plugin. With no plugin loaded the
module type is unknown at engine boot — load the plugin in the deployment's
plugin manifest.

### `platform.kubernetes type: gke`

The in-core `gkeBackend` (which spoke directly to
`google.golang.org/api/container/v1`) is removed. The `type: gke` dispatch
now flows through the `kubernetesBackendClientRegistry` populated at
plugin-load time by `workflow-plugin-gcp >= v1.1.0`, routed via the
`grpcKubernetesBackend` adapter shipped in PR `#681` per
[ADR 0037](../../decisions/0037-gke-cross-process-contract.md).

The YAML `type: gke` value is unchanged. All cluster-level config keys
(`project`, `location`/`zone`, `version`, `nodeGroups`, …) keep their
semantics; the plugin's `GKEDriver.Read` conforms its output to the same
status/endpoint keys the in-core `gkeBackend` produced.

Without the plugin, `PlatformKubernetes.Init` fails fast and the error
message identifies `workflow-plugin-gcp` as the missing plugin (same shape
as the iac.state error above).

## OAuth2 ADC allowlist disclosure

Workflow core's `provider/gcp/` package retains
`golang.org/x/oauth2/google` for its service-account credential resolution
(`google.Credentials`, `FindDefaultCredentials`,
`CredentialsFromJSONWithTypeAndParams`). That import transitively pulls
**`cloud.google.com/go/compute/metadata`** — the OAuth2 Application Default
Credentials helper used to fetch tokens from the GCE/GKE metadata server.

The Phase C asymmetric audit gate (in
[`scripts/audit-cloud-symbols.sh`](../../scripts/audit-cloud-symbols.sh)) and
the mirroring `.github/workflows/ci.yml` `cloud-sdk-audit` job **allowlist
this single transitive path** and **fail CI on any other** `cloud.google.com/go/*`
dep. Any new GCP SDK package (e.g. `cloud.google.com/go/storage`,
`google.golang.org/api/*`) belongs in `workflow-plugin-gcp`, not core.

This is the GCP-side mirror of Phase B's `aws-sdk-go-v2`-retention paragraph:
`provider/aws/` legitimately uses the AWS SDK for its deploy pipeline,
`provider/gcp/` legitimately uses OAuth2 ADC for service-account auth, and
both arrangements are intentional — the audit gate just guards against
scope creep beyond those known seams.

## Rollback

Both Phase B and Phase C clean-breaks roll back only as a **matched pair**
with the plugin releases that serve them — reverting PR
`feat/phase-b-core-deletion` or `feat/phase-c-core-deletion` restores the
in-core paths, but the corresponding plugin v1.1.0 tags are immutable. A
patch-level defect in any plugin port is resolved with a `v1.1.1` release,
not by re-introducing the in-core implementation.

A running deployment that has already cut over to plugin-served `gcs` /
`gke` must coordinate engine + plugin versions on rollback — pinning both
sides to a pre-Phase-C state in the deploy manifest.

The `cloud_account_aws.go` deletion (164 lines of dead code that #653 had
already orphaned) is not part of the matched-pair rollback — it had zero
non-test consumers.

## Verification

### Phase B (post-merge)

- `go mod tidy` against the merged tree makes no net change to AWS SDK
  service modules — `aws-sdk-go-v2` stays in `go.mod` because `provider/aws/`,
  `plugin/rbac/aws.go`, `iam/aws.go`, and `artifact/s3.go` still import it.
- The `.phase-b-complete` marker arms
  `scripts/audit-cloud-symbols.sh --check`'s zero-`aws-sdk-go-v2` invariant on
  `module/cloud_account_aws_creds.go`.

### Phase C (post-merge)

- `go mod tidy` drops `cloud.google.com/go/storage`, `google.golang.org/api`,
  `cloud.google.com/go/auth*`, `cloud.google.com/go/monitoring`,
  `cloud.google.com/go/iam`, and `GoogleCloudPlatform/opentelemetry-operations-go/*`
  (~24 lines). `cloud.google.com/go/compute/metadata` remains as the only
  `cloud.google.com/go/*` entry (allowlisted, see disclosure above).
- The `.phase-c-complete` marker arms two additional `--check` invariants:
  - `module/` has **zero real imports** of `cloud.google.com/go`,
    `google.golang.org/api`, or `github.com/Azure/azure-sdk-for-go`.
  - The whole-repo build graph (`go list -deps ./...`) has zero
    `Azure/azure-sdk-for-go`, zero `google.golang.org/api`, and zero
    `cloud.google.com/go/*` **except** `compute/metadata`.

### Cross-phase invariant re-check

Run from a checkout of the merged tree:

```bash
bash scripts/audit-cloud-symbols.sh --check       # → audit-cloud-symbols: OK
GOWORK=off go list -deps ./... \
  | grep '^cloud\.google\.com/go' \
  | grep -v '^cloud\.google\.com/go/compute/metadata$'   # → empty
GOWORK=off go list -deps ./... \
  | grep -E '^(google\.golang\.org/api|github\.com/Azure/azure-sdk-for-go)' # → empty
GOWORK=off go build ./... && GOWORK=off go test ./...     # → all green
```

Phase A's invariants (typed `IaCStateBackend` contract, `Configure` RPC) are
re-validated by the same audit run since `module/` is the scope they protect.

## Phase recap

| Phase | What | PRs | ADRs |
|---|---|---|---|
| **A** | Typed `IaCStateBackend` gRPC contract; `Configure` RPC; plugin-backend registry; `azure_blob` → workflow-plugin-azure v1.1.0 | plan-1 PRs 1–3; locked B/C/D plan PRs 1–2 | 0035, 0036 |
| **B** | In-core `iac_state_spaces`, `s3_storage`, `pipeline_step_s3_upload` deletion; SDK-free AWS profile/role_arn resolvers with `credential_source` markers; `cloud_account_aws.go` (dead) deletion; `aws-sdk-go-v2` *retained* in `go.mod` for `provider/aws/` et al. | `feat/phase-b-core-deletion` (PR `#687`) | 0034, 0038 |
| **C** | In-core `iac_state_gcs`, `storage_gcs`, `platform_kubernetes_gke` deletion; GCP SDKs dropped from `go.mod` (one allowlisted OAuth2 ADC transitive); permanent asymmetric audit + CI gate; wfctl gcs/s3/spaces actionable errors | `feat/phase-c-core-deletion` | 0037, 0039 (TBD — captures the gate-allowlist trade-off; follow-up) |

Plan-1 and plan-2 manifests + per-task spec records live under
`docs/plans/` (`2026-05-14-cloud-sdk-extraction-bcd.md`,
`2026-05-15-plugin-modules-on-iac.md`).

**Final invariant statement:** workflow-core now imports zero cloud-provider
SDK clients in `module/`; provider-specific surfaces (`provider/aws/`,
`provider/gcp/`'s OAuth2-only path) retain only what's needed for the
out-of-scope deploy-pipeline / credential-resolution work that #653 +
decisions/0034 explicitly carve out. Every other cloud-provider integration
crosses the engine ↔ plugin gRPC boundary.

## Related design + plans

- Plans: [2026-05-14 cloud-SDK extraction (B/C/D)](../plans/2026-05-14-cloud-sdk-extraction-bcd.md), [2026-05-15 plugin-modules-on-iac](../plans/2026-05-15-plugin-modules-on-iac.md)
- Decisions: 0034 (autonomous plugin releases), 0035 (assumed-seam grep / real-import audit), 0036 (Configure RPC), 0037 (GKE cross-process contract — ResourceDriver fold), 0038 (credential_source marker), 0039 (TBD — asymmetric audit gate + compute/metadata allowlist trade-off; follow-up filing)
- Predecessors: [v0.52.0 godo removal](v0.52.0-godo-removal.md), [v0.53.0 AWS IaC removal](v0.53.0-aws-iac-removal.md), [2026-05-14 azure plugin extraction](2026-05-14-cloud-sdk-extraction.md)
