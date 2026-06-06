# `ci.deploy.environments` Schema Reference

Complete field reference for deployment environment configuration.

---

## Structure

```yaml
ci:
  deploy:
    environments:
      staging:
        provider: do-app-platform
        requireApproval: false
        preDeploy:
          - wfctl ci run --phase migrate --env staging
        healthCheck:
          path: /healthz
          timeout: 30s
      prod:
        provider: do-app-platform
        requireApproval: true
```

---

## `ci.deploy.environments.<name>`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `provider` | string | ✓ | Deploy provider (e.g. `do-app-platform`) |
| `cluster` | string | — | Cluster name (Kubernetes providers) |
| `namespace` | string | — | Kubernetes namespace |
| `region` | string | — | Cloud region |
| `strategy` | string | — | Rollout strategy: `rolling`, `blue-green`, `canary` |
| `requireApproval` | bool | false | Require manual approval before deploy (GitHub environment protection) |
| `preDeploy` | string[] | — | Commands run before the deploy step |
| `healthCheck` | HealthCheck | — | Endpoint to poll after deploy |

---

## `healthCheck`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `path` | string | ✓ | HTTP path to poll (e.g. `/healthz`) |
| `timeout` | duration | — | Maximum wait time (e.g. `60s`) |

---

## Environment ordering in `wfctl ci init`

`wfctl ci init` generates chained deploy jobs. Ordering rules:
1. Environments without `requireApproval` deploy first (alphabetical within group).
2. Environments with `requireApproval: true` deploy second (alphabetical within group).
3. Each job `needs` the previous — forming a sequential chain.

```
build-image → deploy-staging → deploy-prod
```

To decouple environments (parallel deploys), set identical `needs` manually in the generated YAML.

---

## Generated deploy job shape

For each environment, `wfctl ci init` emits:

```yaml
deploy-staging:
  needs: [build-image]
  runs-on: ubuntu-latest
  steps:
    - uses: actions/checkout@9f698171ed81b15d1823a05fc7211befd50c8ae0 # v6.0.3
      with:
        ref: ${{ github.event.workflow_run.head_sha || github.sha }}
    - uses: GoCodeAlone/setup-wfctl@bcd880980f5bbe8d192d0c20ff6279d25331f956 # v1
    - run: wfctl ci run --phase deploy --env staging
```

For environments with `requireApproval: true`, an `environment: <name>` key is added to trigger GitHub's environment protection rules.

---

## Conditional Human Gate for Destructive Operations

Some operational commands are only destructive under specific conditions. For
example, `wfctl migrate repair-dirty` changes migration metadata only when a
known dirty version is present. Use GitHub environment protection on the repair
job, not on every deploy job, when you only want human review for that repair.

```yaml
repair-staging-migrations:
  environment: staging
  runs-on: ubuntu-latest
  steps:
    - uses: actions/checkout@9f698171ed81b15d1823a05fc7211befd50c8ae0 # v6.0.3
    - uses: GoCodeAlone/setup-wfctl@bcd880980f5bbe8d192d0c20ff6279d25331f956 # v1
    - run: |
        wfctl migrate repair-dirty --config infra.yaml --env staging \
          --database app-db \
          --app app-service \
          --job-image "registry.example.com/app-migrate:${IMAGE_SHA}" \
          --expected-dirty-version 20260426000005 \
          --force-version 20260422000001 \
          --then-up \
          --confirm-force FORCE_MIGRATION_METADATA \
          --approve-destructive \
          --job-env-from-env DATABASE_URL
      env:
        DATABASE_URL: ${{ secrets.STAGING_DATABASE_URL }}
```

Without `--approve-destructive`, wfctl writes a JSON approval artifact and exits
with status `approval_required` before calling the provider. On GitHub Actions,
the default artifact path is `$RUNNER_TEMP/wfctl-destructive-approval.json`.
When `GITHUB_STEP_SUMMARY` is set, wfctl also writes the operation, environment,
provider job status, diagnostics, and log tail to the run summary.

---

## `environments.local` (dev overrides)

The special `local` environment is used by `wfctl dev up` and applies build overrides for fast iteration:

```yaml
environments:
  local:
    build:
      targets:
        - name: server
          type: go
          path: ./cmd/server
          config:
            race: true
      security:
        hardened: false
        sbom: false
```

See [08 — Local Dev](./08-local-dev.md) for details.

---

*See also:* [Tutorial §3 — Multi-env](../../tutorials/build-deploy-pipeline.md#3-multi-environment-staging--prod) · [05 — CLI Reference](./05-cli-reference.md)
