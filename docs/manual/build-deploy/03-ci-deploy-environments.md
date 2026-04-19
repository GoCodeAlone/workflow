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
    - uses: actions/checkout@v4
      with:
        ref: ${{ github.event.workflow_run.head_sha || github.sha }}
    - uses: GoCodeAlone/setup-wfctl@v1
    - run: wfctl ci run --phase deploy --env staging
```

For environments with `requireApproval: true`, an `environment: <name>` key is added to trigger GitHub's environment protection rules.

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
