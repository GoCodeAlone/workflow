# Deploy Pipeline Tutorial: Multi-Environment with Auto-Promotion

This tutorial walks you through building a production-grade deploy pipeline using `wfctl` and workflow's multi-env infrastructure support. By the end you'll have a single `infra.yaml` that describes staging and prod, a GitHub Actions pipeline that auto-promotes from staging to prod after a health check, and zero manual secret setup thanks to `wfctl infra bootstrap`.

**Requires:** `wfctl` v0.11.0+, Go 1.26+. Examples use DigitalOcean — substitute your provider where needed.

**See also:** [CLI Reference](../WFCTL.md) · [Deployment Guide](../DEPLOYMENT_GUIDE.md)

---

## Table of Contents

1. [Minimal single-environment `infra.yaml`](#1-minimal-single-environment-infrayaml)
2. [Add a second environment with `environments:`](#2-add-a-second-environment-with-environments)
3. [Share common config with `imports:`](#3-share-common-config-with-imports)
4. [Declare `ci.deploy.environments` with health checks](#4-declare-cideployenvironments-with-health-checks)
5. [Generate GitHub Actions with `wfctl ci init`](#5-generate-github-actions-with-wfctl-ci-init)
6. [Customize: add a build-and-push job](#6-customize-add-a-build-and-push-job)
7. [Auto-promote: chain staging → prod](#7-auto-promote-chain-staging--prod)
8. [Optional: manual approval gate with `requireApproval`](#8-optional-manual-approval-gate-with-requireapproval)
9. [Zero-secret setup with `wfctl infra bootstrap`](#9-zero-secret-setup-with-wfctl-infra-bootstrap)
10. [Troubleshooting](#10-troubleshooting)

---

## 1. Minimal single-environment `infra.yaml`

Start with a single environment to prove the basics work before adding complexity. A minimal infra config needs a provider account, a state backend, and at least one `infra.*` resource.

```yaml
# infra.yaml
modules:
  - name: do-provider
    type: iac.provider
    config:
      provider: digitalocean
      credentials: env       # reads DIGITALOCEAN_TOKEN from environment
      region: nyc3

  - name: iac-state
    type: iac.state
    config:
      backend: spaces
      region: nyc3
      bucket: my-app-iac-state
      prefix: main/
      accessKey: ${SPACES_ACCESS_KEY}
      secretKey: ${SPACES_SECRET_KEY}

  - name: app-database
    type: infra.database
    config:
      engine: pg
      version: "16"
      size: db-s-1vcpu-1gb
      num_nodes: 1
      provider: do-provider
```

Validate and preview:

```bash
# --allow-no-entry-points suppresses the "no triggers/routes" warning for infra-only configs
# --skip-unknown-types skips provider-specific type checks not loaded locally
wfctl validate --allow-no-entry-points --skip-unknown-types infra.yaml
wfctl infra plan --config infra.yaml
```

Output:

```
  PASS infra.yaml (3 modules, 0 workflows, 0 triggers)
```

```
Infrastructure Plan — infra.yaml

+ create  app-database  (infra.database)
    engine:  pg v16
    size:    db-s-1vcpu-1gb

Plan: 1 to create, 0 to update, 0 to destroy.
```

`infra plan` is primarily focused on `infra.*` resource changes. `iac.provider` and `iac.state` are bootstrap-related modules managed via `wfctl infra bootstrap`; some CLI output may still show them for context, but they are not the main resource diff shown above.

---

## 2. Add a second environment with `environments:`

Extend the config with a top-level `environments:` block for global defaults and per-module `environments:` overrides for resource-specific config.

```yaml
# infra.yaml
environments:
  staging:
    provider: digitalocean
    region: nyc3
  prod:
    provider: digitalocean
    region: nyc1

modules:
  - name: do-provider
    type: iac.provider
    config:
      provider: digitalocean
      credentials: env

  - name: iac-state
    type: iac.state
    config:
      backend: spaces
      bucket: my-app-iac-state
      accessKey: ${SPACES_ACCESS_KEY}
      secretKey: ${SPACES_SECRET_KEY}
    environments:
      staging:
        config:
          prefix: staging/
          region: nyc3
      prod:
        config:
          prefix: prod/
          region: nyc1

  - name: app-database
    type: infra.database
    config:
      engine: pg
      version: "16"
      num_nodes: 1
      provider: do-provider
    environments:
      staging:
        config:
          name: myapp-staging-db
          size: db-s-1vcpu-1gb
          region: nyc3
      prod:
        config:
          name: myapp-prod-db
          size: db-s-2vcpu-4gb
          region: nyc1

  - name: app-dns
    type: infra.dns
    config:
      provider: do-provider
    environments:
      staging: null      # no custom domain for staging
      prod:
        config:
          domain: myapp.com
```

Plan per environment:

```bash
wfctl infra plan --env staging --config infra.yaml
wfctl infra plan --env prod    --config infra.yaml
```

Staging output (note: `app-dns` is absent — skipped by `staging: null`):

```
Infrastructure Plan — infra.yaml

+ create  do-provider  (iac.provider)
    credentials:  env
    provider:     digitalocean

+ create  iac-state  (iac.state)
    backend:    spaces
    bucket:     my-app-iac-state
    accessKey:  ${SPACES_ACCESS_KEY}
    prefix:     staging/

+ create  app-database  (infra.database)
    name:    myapp-staging-db
    engine:  pg v16
    size:    db-s-1vcpu-1gb
    region:  nyc3

Plan: 3 to create, 0 to update, 0 to destroy.
```

Prod output (note: `app-dns` is present and `size` is `db-s-2vcpu-4gb`):

```
Infrastructure Plan — infra.yaml

+ create  do-provider  (iac.provider)
    credentials:  env
    provider:     digitalocean

+ create  iac-state  (iac.state)
    backend:    spaces
    bucket:     my-app-iac-state
    accessKey:  ${SPACES_ACCESS_KEY}
    prefix:     prod/
    region:     nyc1

+ create  app-database  (infra.database)
    name:    myapp-prod-db
    engine:  pg v16
    size:    db-s-2vcpu-4gb
    region:  nyc1

+ create  app-dns  (infra.dns)
    domain:  myapp.com

Plan: 4 to create, 0 to update, 0 to destroy.
```

**Key resolution rules:**

| Scenario | Result |
|----------|--------|
| Module has no `environments:` block | Included in every environment with top-level `config:` |
| `environments: { staging: null }` | Module skipped in staging; other environments use top-level config |
| `environments: { prod: { config: { size: large } } }` | `size` overrides top-level; all other `config` keys are inherited |
| Module omits `region` in its resolved config | `region` defaults from `environments[env].region` |
| Module omits `provider` in its resolved config | `provider` defaults from `environments[env].provider` |

---

## 3. Share common config with `imports:`

Split shared modules (credentials, state backend) into a reusable file and import them. `imports:` paths are relative to the importing file.

```yaml
# shared.yaml
modules:
  - name: do-provider
    type: iac.provider
    config:
      provider: digitalocean
      credentials: env

  - name: iac-state
    type: iac.state
    config:
      backend: spaces
      bucket: my-app-iac-state
      accessKey: ${SPACES_ACCESS_KEY}
      secretKey: ${SPACES_SECRET_KEY}
    environments:
      staging:
        config:
          prefix: staging/
          region: nyc3
      prod:
        config:
          prefix: prod/
          region: nyc1
```

```yaml
# infra.yaml
imports:
  - shared.yaml

environments:
  staging:
    provider: digitalocean
    region: nyc3
  prod:
    provider: digitalocean
    region: nyc1

modules:
  - name: app-database
    type: infra.database
    config:
      engine: pg
      version: "16"
      num_nodes: 1
      provider: do-provider
    environments:
      staging:
        config:
          name: myapp-staging-db
          size: db-s-1vcpu-1gb
      prod:
        config:
          name: myapp-prod-db
          size: db-s-2vcpu-4gb
```

Merge behavior: imported modules are appended after the main file's modules. Map-based fields (workflows, triggers, pipelines) take the main file's values for any shared keys. Imports are resolved recursively — an imported file can itself import others.

Validate the merged config:

```bash
wfctl validate --allow-no-entry-points --skip-unknown-types infra.yaml
```

Output (note the import resolution line):

```
  Resolved 1 import(s): shared.yaml
  PASS infra.yaml (3 modules, 0 workflows, 0 triggers)
```

Run a plan to confirm the shared modules appear:

```bash
wfctl infra plan --env staging --config infra.yaml
```

```
Infrastructure Plan — infra.yaml

+ create  app-database  (infra.database)
    name:    myapp-staging-db
    engine:  pg v16
    size:    db-s-1vcpu-1gb

+ create  do-provider  (iac.provider)
    credentials:  env
    provider:     digitalocean

+ create  iac-state  (iac.state)
    backend:    spaces
    bucket:     my-app-iac-state
    accessKey:  ${SPACES_ACCESS_KEY}
    prefix:     staging/

Plan: 3 to create, 0 to update, 0 to destroy.
```

`do-provider` and `iac-state` come from `shared.yaml` — confirming `imports:` is resolved before computing the plan.

---

## 4. Declare `ci.deploy.environments` with health checks

Add a `ci.deploy.environments` block to describe which environments the pipeline deploys to, how to verify them, and whether they require approval.

```yaml
# infra.yaml  (add to existing config)
ci:
  deploy:
    environments:
      staging:
        provider: digitalocean
        strategy: apply
        healthCheck:
          path: /health
          timeout: 30s
      prod:
        provider: digitalocean
        strategy: apply
        healthCheck:
          path: /health
          timeout: 30s
```

`wfctl ci init` reads this block to generate one deploy job per environment. The health check runs after each `wfctl infra apply` completes and gates the next environment.

---

## 5. Generate GitHub Actions with `wfctl ci init`

Generate a starter workflow from your `infra.yaml`:

```bash
wfctl ci init --platform github-actions \
              --config infra.yaml \
              --output .github/workflows/deploy.yml
```

For the `infra.yaml` from the previous section, this produces:

```yaml
# Generated by wfctl ci init -- customize as needed
name: CI/CD
on:
  push:
    branches: [main]
  pull_request:
jobs:
  build-test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: GoCodeAlone/setup-wfctl@v1
      - run: wfctl ci run --phase build,test
  deploy-staging:
    runs-on: ubuntu-latest
    needs: [build-test]
    steps:
      - uses: actions/checkout@v4
      - uses: GoCodeAlone/setup-wfctl@v1
      - run: wfctl ci run --phase deploy --env staging
  deploy-prod:
    runs-on: ubuntu-latest
    needs: [build-test]
    steps:
      - uses: actions/checkout@v4
      - uses: GoCodeAlone/setup-wfctl@v1
      - run: wfctl ci run --phase deploy --env prod
```

The generated file is a starting point — the next sections show how to customize it with a Docker build step and auto-promotion.

---

## 6. Customize: add a build-and-push job

Insert a `build-image` job between `build-test` and the deploy jobs. This compiles your binary, builds the Docker image, and pushes it to your registry — outputting the image SHA for downstream jobs.

```yaml
  build-image:
    runs-on: ubuntu-latest
    needs: [build-test]
    outputs:
      sha: ${{ steps.meta.outputs.sha }}
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'
      - uses: digitalocean/action-doctl@v2
        with:
          token: ${{ secrets.DIGITALOCEAN_TOKEN }}
      - name: Build binary
        run: CGO_ENABLED=0 GOOS=linux go build -o app-linux ./cmd/app
      - name: Build and push to DOCR
        id: meta
        run: |
          doctl registry login
          docker build -t registry.digitalocean.com/my-registry/myapp:${{ github.sha }} .
          docker push registry.digitalocean.com/my-registry/myapp:${{ github.sha }}
          echo "sha=${{ github.sha }}" >> $GITHUB_OUTPUT
```

Reference the SHA in your deploy jobs via the `IMAGE_SHA` environment variable (your `infra.yaml` container module should use `${IMAGE_SHA}` in its `image:` config):

```yaml
  deploy-staging:
    needs: [build-image]
    steps:
      - uses: actions/checkout@v4
      - uses: GoCodeAlone/setup-wfctl@v1
      - run: wfctl ci run --phase deploy --env staging
        env:
          DIGITALOCEAN_TOKEN: ${{ secrets.DIGITALOCEAN_TOKEN }}
          IMAGE_SHA: ${{ needs.build-image.outputs.sha }}
```

In `infra.yaml`, reference `${IMAGE_SHA}` inside the container module:

```yaml
  - name: app
    type: infra.container_service
    config:
      http_port: 8080
      provider: do-provider
    environments:
      staging:
        config:
          name: myapp-staging
          image: "registry.digitalocean.com/my-registry/myapp:${IMAGE_SHA}"
          instance_count: 1
      prod:
        config:
          name: myapp-prod
          image: "registry.digitalocean.com/my-registry/myapp:${IMAGE_SHA}"
          instance_count: 2
```

---

## 7. Auto-promote: chain staging → prod

Change `deploy-prod.needs` from `[build-test]` to `[deploy-staging]`. This makes prod wait until staging's deploy job (including the health check) passes:

```yaml
  deploy-staging:
    runs-on: ubuntu-latest
    needs: [build-image]           # waits for image push
    steps:
      - uses: actions/checkout@v4
      - uses: GoCodeAlone/setup-wfctl@v1
      - run: wfctl ci run --phase deploy --env staging
        env:
          DIGITALOCEAN_TOKEN: ${{ secrets.DIGITALOCEAN_TOKEN }}
          IMAGE_SHA: ${{ needs.build-image.outputs.sha }}

  deploy-prod:
    runs-on: ubuntu-latest
    needs: [deploy-staging]        # auto-promotes after staging succeeds
    steps:
      - uses: actions/checkout@v4
      - uses: GoCodeAlone/setup-wfctl@v1
      - run: wfctl ci run --phase deploy --env prod
        env:
          DIGITALOCEAN_TOKEN: ${{ secrets.DIGITALOCEAN_TOKEN }}
          IMAGE_SHA: ${{ needs.build-image.outputs.sha }}
```

If staging's health check times out or the deploy errors, GitHub marks `deploy-staging` as failed and `deploy-prod` is automatically skipped.

---

## 8. Optional: manual approval gate with `requireApproval`

Add `requireApproval: true` to a `ci.deploy.environments` entry to pause the pipeline and wait for a human to approve before deploying to that environment.

```yaml
# infra.yaml
ci:
  deploy:
    environments:
      staging:
        provider: digitalocean
        strategy: apply
        healthCheck:
          path: /health
          timeout: 30s
      prod:
        provider: digitalocean
        strategy: apply
        requireApproval: true       # pause before prod deploy
        healthCheck:
          path: /health
          timeout: 30s
```

When `requireApproval: true`, `wfctl ci init` emits `environment: prod` in the generated job:

```yaml
  deploy-prod:
    runs-on: ubuntu-latest
    needs: [deploy-staging]
    environment: prod              # GitHub waits for environment protection approval
    steps:
      - uses: actions/checkout@v4
      - uses: GoCodeAlone/setup-wfctl@v1
      - run: wfctl ci run --phase deploy --env prod
```

GitHub's [Environments](https://docs.github.com/en/actions/deployment/targeting-different-deployment-environments) feature handles the approval gate — no additional engine changes are needed. Configure required reviewers in your repository's Settings → Environments → prod.

---

## 9. Zero-secret setup with `wfctl infra bootstrap`

Add a `secrets:` block to declare secrets that should be auto-generated and stored in your secrets provider:

```yaml
# infra.yaml
infra:
  auto_bootstrap: true   # default; causes `wfctl infra apply` to run bootstrap first

secrets:
  provider: github
  config:
    repo: MyOrg/my-repo
    token_env: GH_MANAGEMENT_TOKEN   # env var holding a repo-scoped GitHub PAT
  generate:
    - key: JWT_SECRET
      type: random_hex
      length: 32
    - key: SPACES
      type: provider_credential
      source: digitalocean.spaces
      name: my-deploy-key            # creates SPACES_ACCESS_KEY + SPACES_SECRET_KEY

modules:
  # ... (do-provider, iac-state, etc.)
```

Run bootstrap once before your first deploy:

```bash
export DIGITALOCEAN_TOKEN=...
export GH_MANAGEMENT_TOKEN=...       # needs repo secrets write permission

wfctl infra bootstrap --config infra.yaml
```

Bootstrap will:
1. Create the IaC state backend bucket (`iac.state backend: spaces`) if it doesn't exist.
2. Generate any secrets listed under `secrets.generate` that don't already exist in the store — existing secrets are skipped.

With `infra.auto_bootstrap: true` (the default), `wfctl infra apply` runs bootstrap automatically before every apply. CI never needs an explicit bootstrap step after the first run — just ensure the token secrets are set in GitHub Actions.

Supported secret providers: `github`, `vault`, `aws`, `env`.

---

## 10. Troubleshooting

### `wfctl infra plan` shows 0 resources

`infra plan` only shows `infra.*` modules. Modules of type `platform.*`, `cloud.account`, `iac.state`, `iac.provider`, and other non-infra module types are categorised separately and won't appear in the plan output — they are still parsed and used internally.

### `imports:` file not found

Paths in `imports:` are relative to the file that declares them. Running `wfctl validate --config infra.yaml` will report which imports fail to resolve with their full paths.

### Per-environment config not applied

Environment names are case-sensitive. `"Staging"` and `"staging"` are different environments. Run `wfctl infra plan --env staging` and verify the plan output shows the expected sizes and regions.

### `null` environment not skipping the module

Verify your YAML uses the bare `null` keyword (not the string `"null"`):

```yaml
environments:
  staging: null    # correct — skips module in staging
  dev: "null"      # wrong — this is the string "null", not nil
```

### `DIGITALOCEAN_TOKEN not set` during bootstrap

`wfctl infra bootstrap` requires `DIGITALOCEAN_TOKEN` in the environment when creating a DO Spaces bucket. Export it before running, or add it as a CI secret and reference it with `env:` in your workflow step.

### Health check timeout after staging deploy

Increase `ci.deploy.environments.staging.healthCheck.timeout` or verify your app's `/health` endpoint responds within the timeout window. The health check polls the app's public URL; if your container takes time to start, consider increasing the timeout to 60–90 seconds.

### `wfctl ci run --phase deploy --env prod` fails in CI with "no config found"

Ensure your `infra.yaml` is committed to the repository root (or pass `--config path/to/infra.yaml` explicitly in the generated workflow step).

---

*This tutorial uses v0.11.0 behavior. For older versions see [WFCTL.md](../WFCTL.md).*
