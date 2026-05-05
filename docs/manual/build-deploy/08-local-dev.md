# Local Dev

How to use `wfctl dev up` for fast local iteration with per-environment build overrides.

---

## Overview

`wfctl dev up` wires into the same `ci.build` pipeline as CI, but applies `environments.local.build` overrides before running the build. This means:

- Security hardening is relaxed by default (no distroless enforcement, no SBOM generation).
- Container targets get Docker local layer cache injected.
- Per-target `config` overrides apply (e.g. `race: true` for Go).

---

## Quick start

```sh
wfctl dev up [--config infra.yaml]
```

The command:
1. Loads config and merges `environments.local.build` overrides.
2. Runs `wfctl build --env local` (dry-run if `WFCTL_BUILD_DRY_RUN=1`).
3. Starts services via Docker Compose (default), local processes, or minikube.

---

## `environments.local.build` overrides

Target-level config keys are merged by target name. Environment keys win over base config keys.

```yaml
environments:
  local:
    build:
      targets:
        - name: server
          type: go
          path: ./cmd/server
          config:
            race: true       # add -race
            ldflags: ""      # clear CI ldflags for readable stack traces
      security:
        hardened: false
        sbom: false
```

### Merge semantics

- Targets are matched by `name`.
- The env override's `config` map is merged on top of the base `config` (env keys win).
- `security` from the env override replaces the base security entirely (not merged).

---

## Automatic local defaults

When `env == "local"` and no `environments.local.build.security` block is set, wfctl automatically applies:

```go
Security: &CIBuildSecurity{
    Hardened:   false,
    SBOM:       false,
    Provenance: "",
    NonRoot:    false,
}
```

This keeps `docker build` fast — no distroless base image enforcement, no SBOM generation.

If you set `environments.local.build.security` explicitly, that value is preserved as-is.

---

## Local Docker cache

Container targets running under `env == "local"` automatically get Docker's local layer cache injected:

```yaml
cache:
  from:
    - type: local
```

This speeds up repeated `docker build` calls by reusing cached layers.

---

## Hot reload

For local Go processes, use `--local` to run services as native processes with hot-reload:

```sh
wfctl dev up --local
```

This calls `runDevProcess` which watches for file changes and restarts binaries.

---

## Kubernetes (minikube)

```sh
wfctl dev up --k8s
```

Deploys to a local minikube cluster via `kubectl apply`.

---

## Exposure

Expose local services to the internet via tunnel:

```yaml
environments:
  local:
    exposure:
      method: tailscale
      tailscale:
        funnel: true
        hostname: myapp-dev
```

```sh
wfctl dev up --expose tailscale
```

---

*See also:* [Tutorial §10 — Local Dev](../../tutorials/build-deploy-pipeline.md#10-local-dev-with-wfctl-dev-up) · [07 — Security Hardening](./07-security-hardening.md)
