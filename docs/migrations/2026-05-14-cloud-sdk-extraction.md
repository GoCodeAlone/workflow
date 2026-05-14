# 2026-05-14 — Cloud-SDK extraction: `azure_blob` IaC state backend moves to a plugin

## What changed

The in-core `azure_blob` IaC state backend was removed from workflow core.
`module/iac_state_azure.go` (the `AzureBlobIaCStateStore`) and the `azure_blob`
case in `IaCModule.Init` are gone. As a result, `github.com/Azure/azure-sdk-for-go`
is no longer a dependency of the workflow module — `go mod tidy` drops `azcore`,
`storage/azblob`, and their transitive `sdk/internal` entirely.

`backend: azure_blob` is still a valid `iac.state` config value — but it now
resolves to an `IaCStateBackend` gRPC client served by
[`workflow-plugin-azure`](https://github.com/GoCodeAlone/workflow-plugin-azure)
v1.1.0+. The plugin advertises the backend via the `iacStateBackends` field in
its `plugin.json`; the engine populates the in-core backend registry at
plugin-load time, and `IaCModule.Init` constructs a `grpcIaCStateStore` for it.

## Why

Workflow core should own IaC interfaces and orchestration, not provider SDKs.
Dependabot bumps to `azure-sdk-for-go` now target the Azure plugin repo, not
core. This mirrors the pattern established by the godo removal (issue #617) and
the AWS IaC removal (v0.53.0). See the design plan at
`docs/plans/2026-05-14-cloud-sdk-extraction-design.md`.

## Breaking change

An `iac.state` module with `backend: azure_blob` now **requires
`workflow-plugin-azure` (>= v1.1.0) to be loaded**. With no plugin loaded, the
module fails to initialize with an actionable error:

```
iac.state "<name>": unsupported backend "azure_blob"
(use 'memory', 'filesystem', 'spaces', 'gcs', 'azure_blob', or 'postgres',
 or load the plugin that provides it)
```

The yaml `backend: azure_blob` value itself is **unchanged** — no config
rewrite is needed beyond installing the plugin. The `account_url`,
`account_name`, `account_key`, `container`, and `prefix` config keys are
unchanged and continue to be honored by the plugin's backend.

## Unaffected backends

The `memory`, `filesystem`, `spaces`, `gcs`, and `postgres` IaC state backends
remain in workflow core and are **not** affected by this change. Only
`azure_blob` moved to a plugin.

## Migration recipe

1. Install the Azure plugin (v1.1.0+):
   ```sh
   wfctl plugin install workflow-plugin-azure@1.1.0
   ```
   Or declare it in your workflow config under `plugins.external`:
   ```yaml
   plugins:
     external:
       - name: workflow-plugin-azure
         version: ">=1.1.0"
         autoFetch: true
   ```

   To declare the dependency without auto-fetch:
   ```yaml
   requires:
     plugins:
       - workflow-plugin-azure
   ```

2. No config rewrite is required. The `iac.state` module keeps its
   `backend: azure_blob` value and all its existing config keys:
   ```yaml
   modules:
     - name: iac-state
       type: iac.state
       config:
         backend: azure_blob
         container: my-state-container
         account_url: https://myaccount.blob.core.windows.net
         account_name: myaccount
         account_key: <key>
   ```

## Phases B/C/D

This is Phase A of the cloud-SDK extraction. Phases B, C, and D apply the same
pattern to the AWS, GCP, and DigitalOcean IaC state backends in subsequent
releases — each backend moves to its provider plugin, and the corresponding
cloud SDK drops from workflow core's `go.mod`.

## Rollback

If you need to roll back, revert the commit
`feat(module)!: drop in-core azure_blob IaC state backend` and run
`go mod tidy` — this restores `module/iac_state_azure.go`, the in-core
`azure_blob` case, and re-adds `azure-sdk-for-go` to `go.mod`. Smoke-check with
an `azure_blob` config and no plugin loaded.
