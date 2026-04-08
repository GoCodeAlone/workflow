# wfctl Infra Bootstrap, Output Wiring, and Secrets Provider — Design

**Date:** 2026-04-08
**Status:** Approved

## Problem Statement

Infrastructure bootstrapping logic (creating state backends, generating credentials, discovering database URLs, wiring outputs between resources) currently lives in CI shell scripts (GitHub Actions). This locks the deployment process to a specific CI provider. All infrastructure intelligence should live in `wfctl` so the same commands work on any CI system or locally.

## Solution

Three new capabilities in the workflow engine:

1. **`wfctl infra bootstrap`** — creates state backend, generates credentials and secrets
2. **Output wiring** — `{{ outputs.resource.key }}` template syntax for cross-resource references
3. **Secrets provider integration** — pluggable secrets storage (GitHub, Vault, AWS, env) with sensitive value protection

## 1. `wfctl infra bootstrap`

New command that prepares the infrastructure state backend and generates required credentials.

```bash
wfctl infra bootstrap --config infra/staging.yaml
```

**Behavior:**
1. Reads `iac.state` module config
2. Creates state backend (e.g., DO Spaces bucket) if missing — uses provider API
3. Generates state backend credentials (e.g., Spaces access keys) if not available
4. Generates application secrets from `secrets.generate` config
5. Stores credentials/secrets via configured `secrets.provider`
6. Idempotent — skips creation/generation if already exists

**Auto-bootstrap in apply:**

```yaml
infra:
  auto_bootstrap: true  # default: true
```

When `true`, `wfctl infra apply` runs bootstrap logic before applying. When `false`, errors if state backend missing. Users can explicitly set `false` and run `wfctl infra bootstrap` manually.

## 2. Output Wiring — `{{ outputs.resource.key }}`

Cross-resource output references in infra YAML:

```yaml
- name: staging-app
  type: infra.container_service
  config:
    env_vars:
      DATABASE_URL: "{{ outputs.staging-db.uri }}"
      JWT_SECRET: "{{ secrets.jwt_secret }}"
```

**Resolution flow:**
1. Parse YAML, detect `{{ outputs.* }}` and `{{ secrets.* }}` references
2. Auto-infer dependencies (staging-app depends on staging-db)
3. Topological sort: apply resources in dependency order
4. After each resource applies, store outputs in state
5. Before applying next resource, resolve template expressions from accumulated outputs
6. Template resolution happens in-memory only — never written to YAML files

**Implementation:** In `parseInfraResourceSpecs()` (`cmd/wfctl/infra.go`), scan config values for template expressions, build dependency graph, resolve after each apply step.

## 3. Secrets Provider Integration

Uses existing `secrets.Provider` interface:

```go
type Provider interface {
    Name() string
    Get(ctx context.Context, key string) (string, error)
    Set(ctx context.Context, key, value string) error
    Delete(ctx context.Context, key string) error
    List(ctx context.Context) ([]string, error)
}
```

**Existing providers:** EnvProvider, AWSSecretsManagerProvider, VaultProvider

**New provider:** GitHubSecretsProvider — stores/retrieves secrets via GitHub repository secrets API.

**Config:**

```yaml
secrets:
  provider: github
  config:
    repo: GoCodeAlone/workflow-dnd
    token_env: GH_MANAGEMENT_TOKEN
  generate:
    - key: jwt_secret
      type: random_hex
      length: 32
    - key: spaces_access_key
      type: provider_credential
      source: digitalocean.spaces
```

**Secret generators:**
- `random_hex` — `crypto/rand` hex string of specified length
- `random_base64` — base64-encoded random bytes
- `provider_credential` — calls provider API to generate credentials (e.g., DO Spaces keys)

## 4. Sensitive Value Protection

Following Terraform/Pulumi best practices:

### State Files
- Sensitive outputs marked with `sensitive: true` in `ResourceOutput`
- Stored in state as encrypted references, not plaintext
- State file format: `{"uri": "secret://state/staging-db/uri"}` — the actual value is stored separately in the secrets provider or encrypted in-place

### Logs and Plan Output
- All sensitive values automatically masked with `(sensitive)` in:
  - `wfctl infra plan` output (table and markdown formats)
  - `wfctl infra apply` progress logs
  - `wfctl infra status` output
- Masking applied by checking `ResourceOutput.Sensitive` flag and `secrets.*` config values

### Template Resolution
- `{{ outputs.staging-db.uri }}` resolves in-memory at apply time
- The resolved value is passed to the provider's Create/Update API but never logged
- Pipeline step outputs containing sensitive values are marked and masked in step output logs

### Implementation
- Add `Sensitive bool` field to `interfaces.ResourceOutput` (and `platform.ResourceOutput`)
- Add `SensitiveKeys []string` to `ResourceDriver` interface (drivers declare which output keys are sensitive — e.g., database driver declares `uri`, `password`)
- Add masking middleware in `cmd/wfctl/infra.go` plan/apply formatters
- Add `--show-sensitive` flag for debugging (off by default, requires explicit opt-in)

## 5. Updated Infra YAML Example

```yaml
infra:
  auto_bootstrap: true

secrets:
  provider: github
  config:
    repo: GoCodeAlone/workflow-dnd
    token_env: GH_MANAGEMENT_TOKEN
  generate:
    - key: jwt_secret
      type: random_hex
      length: 32

modules:
  - name: do-provider
    type: iac.provider
    config:
      provider: digitalocean
      credentials: env
      region: nyc3

  - name: iac-state
    type: iac.state
    config:
      backend: spaces
      region: nyc3
      bucket: dnd-iac-state
      prefix: staging/

  - name: staging-vpc
    type: infra.vpc
    config:
      name: dnd-staging-vpc
      cidr: "10.10.0.0/16"
      provider: do-provider

  - name: staging-firewall
    type: infra.firewall
    config:
      name: dnd-staging-firewall
      provider: do-provider
      inbound_rules:
        - protocol: tcp
          ports: "8180"
          sources: ["75.61.150.38"]

  - name: staging-registry
    type: infra.registry
    config:
      name: dnd-registry
      tier: basic
      provider: do-provider

  - name: staging-db
    type: infra.database
    config:
      name: dnd-staging-db
      engine: pg
      version: "16"
      size: db-s-1vcpu-1gb
      provider: do-provider

  - name: staging-app
    type: infra.container_service
    config:
      name: dnd-staging
      image: registry.digitalocean.com/dnd-registry/dnd-server:latest
      http_port: 8180
      instance_count: 1
      provider: do-provider
      env_vars:
        DATABASE_URL: "{{ outputs.staging-db.uri }}"
        SESSION_STORE: "pg"
        JWT_SECRET: "{{ secrets.jwt_secret }}"
        SINGLE_PORT: "true"
        GRPC_PORT: "8180"

pipelines:
  apply:
    steps:
      - name: apply_vpc
        type: step.iac_apply
        config:
          platform: staging-vpc
          state_store: iac-state
      - name: apply_firewall
        type: step.iac_apply
        config:
          platform: staging-firewall
          state_store: iac-state
      - name: apply_registry
        type: step.iac_apply
        config:
          platform: staging-registry
          state_store: iac-state
      - name: apply_db
        type: step.iac_apply
        config:
          platform: staging-db
          state_store: iac-state
      - name: apply_app
        type: step.iac_apply
        config:
          platform: staging-app
          state_store: iac-state
```

## 6. Simplified CI

After implementation, any CI system needs only:

```yaml
# GitHub Actions
- uses: GoCodeAlone/setup-wfctl@v1
- run: wfctl infra bootstrap -c infra/staging.yaml
  env:
    DIGITALOCEAN_TOKEN: ${{ secrets.DIGITALOCEAN_TOKEN }}
    GH_MANAGEMENT_TOKEN: ${{ secrets.GH_MANAGEMENT_TOKEN }}
- run: wfctl infra apply -c infra/staging.yaml -y
  env:
    DIGITALOCEAN_TOKEN: ${{ secrets.DIGITALOCEAN_TOKEN }}

# GitLab CI (identical commands)
script:
  - wfctl infra bootstrap -c infra/staging.yaml
  - wfctl infra apply -c infra/staging.yaml -y
```

Required secrets: **2** (DIGITALOCEAN_TOKEN + GH_MANAGEMENT_TOKEN). Everything else auto-generated.

## 7. Implementation Scope

| Component | Repo | New/Modify |
|---|---|---|
| `wfctl infra bootstrap` command | workflow | New in `cmd/wfctl/infra.go` |
| Auto-bootstrap in apply | workflow | Modify `cmd/wfctl/infra.go` |
| Output template resolution | workflow | New in `cmd/wfctl/infra.go` |
| Auto-dependency inference | workflow | Modify `cmd/wfctl/infra.go` |
| Secrets config parsing | workflow | New in `cmd/wfctl/infra.go` |
| `{{ secrets.* }}` resolution | workflow | New in `cmd/wfctl/infra.go` |
| Secret generators | workflow | New `secrets/generators.go` |
| GitHubSecretsProvider | workflow | New `secrets/github_provider.go` |
| Sensitive output masking | workflow | Modify `cmd/wfctl/infra.go`, `interfaces/iac_provider.go` |
| `Sensitive` field on ResourceOutput | workflow | Modify `interfaces/iac_provider.go` |
| `SensitiveKeys` on ResourceDriver | workflow | Modify `interfaces/iac_resource_driver.go` |
| DO database driver sensitive keys | workflow-plugin-digitalocean | Modify `drivers/database.go` |
| Update deploy.yml to use wfctl | workflow-dnd | Modify `.github/workflows/deploy.yml` |
| Update infra/staging.yaml | workflow-dnd | Modify `infra/staging.yaml` |
| Update docs/DEPLOYMENT.md | workflow-dnd | Modify `docs/DEPLOYMENT.md` |
