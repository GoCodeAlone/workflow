# 2026-05-15 — Plugin-modules-on-IaC: Phase B clean break

This migration covers **Phase B** of the
[plugin-modules-on-IaC plan](../plans/2026-05-15-plugin-modules-on-iac.md):
workflow-core sheds the remaining in-core AWS/DO storage + state surfaces and
the SDK-bearing AWS credential resolvers. Each surface is now plugin-native.

The companion **Phase C** migration (GCP) follows in a separate PR; this doc is
amended in-place when that ships.

## Engine floor

Phase B requires **workflow `>= v0.53.0`** in any deployment that uses the
affected backends. The `>= v0.53.0` engine has the typed `IaCStateBackend`
gRPC contract (Phase A, decisions/0036), the `Configure` RPC that delivers the
`iac.state` module YAML to the plugin, and the plugin-backend registry that
`IaCModule.Init` consults in its `default:`-arm.

## What changed

| Surface | Was | Now |
|---|---|---|
| `iac.state` `backend: spaces` | in-core `module.SpacesIaCStateStore` | plugin-served by [`workflow-plugin-digitalocean`](https://github.com/GoCodeAlone/workflow-plugin-digitalocean) `>= v1.1.0` |
| `iac.state` `backend: s3` | (already moved in v0.53.0; no in-core impl since then) | plugin-served by [`workflow-plugin-aws`](https://github.com/GoCodeAlone/workflow-plugin-aws) `>= v1.1.0` |
| `storage.s3` module | in-core `module.S3Storage` (registered by `plugins/storage`) | plugin-native in `workflow-plugin-aws >= v1.1.0` |
| `step.s3_upload` pipeline step | in-core `module.S3UploadStep` (registered by `plugins/pipelinesteps`) | plugin-native in `workflow-plugin-aws >= v1.1.0` |
| `cloud.account` `provider: aws` + `credentialType: profile` or `role_arn` | SDK-bearing resolver loaded the profile / called `sts:AssumeRole` in-core | core records a `credential_source` marker only; the aws plugin performs SDK resolution via `awscreds.BuildAWSConfig` (decisions/0036 + 0038) |

The YAML field names and `backend:` values are **unchanged**. The break is
strictly about *which binary* serves them.

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
(in-core backends: 'memory', 'filesystem', 'gcs', 'postgres').
If "spaces" is a plugin-provided backend (e.g. 'azure_blob' via
workflow-plugin-azure, 'spaces' via workflow-plugin-digitalocean,
's3' via workflow-plugin-aws), install and load that plugin
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

### `cloud.account provider: aws` with `credentialType: profile` or `role_arn`

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

`credentialType: static` and `credentialType: env` are unaffected — those
have always been SDK-free.

## Rollback

Phase B's clean-breaks roll back only as a **matched pair** with the plugin
releases that serve them — reverting PR `feat/phase-b-core-deletion`
restores the in-core paths, but the plugin v1.1.0 tags are immutable. A
patch-level defect in either plugin port is resolved with a `v1.1.1`
release, not by re-introducing the in-core implementation.

The `cloud_account_aws.go` deletion (164 lines of dead code that #653 had
already orphaned) is not part of the matched-pair rollback — it had zero
non-test consumers.

## Verification

Once Phase B is merged:

- `go mod tidy` against the merged tree should make no net change to AWS SDK
  service modules — `aws-sdk-go-v2` stays in `go.mod` because `provider/aws/`,
  `plugin/rbac/aws.go`, `iam/aws.go`, and `artifact/s3.go` still import it.
- The `.phase-b-complete` marker arms
  `scripts/audit-cloud-symbols.sh --check`'s zero-`aws-sdk-go-v2` invariant on
  `module/cloud_account_aws_creds.go`. Running the audit script post-merge
  must report `audit-cloud-symbols: OK`.

## Related design + plans

- Plan: [docs/plans/2026-05-15-plugin-modules-on-iac.md](../plans/2026-05-15-plugin-modules-on-iac.md)
- Decisions: 0034 (autonomous plugin releases), 0035 (assumed-seam grep), 0036 (Configure RPC), 0038 (credential_source marker)
- Predecessors: [v0.52.0 godo removal](v0.52.0-godo-removal.md), [v0.53.0 AWS IaC removal](v0.53.0-aws-iac-removal.md), [2026-05-14 azure plugin extraction](2026-05-14-cloud-sdk-extraction.md)
