# Security Hardening

How wfctl enforces supply-chain security by default and how to customize or opt out.

---

## Default security posture

Every config loaded by `wfctl` has `ci.build.security` defaults applied **automatically at load time**:

| Setting | Default | Description |
|---------|---------|-------------|
| `hardened` | `true` | Enable all hardening |
| `sbom` | `true` | Generate + attach CycloneDX SBOM |
| `provenance` | `slsa-3` | SLSA level-3 BuildKit provenance |
| `non_root` | `true` | Enforce non-root user in images |

These defaults apply when `ci.build.security` is absent. Explicit values are preserved.

---

## SBOM generation

When `security.sbom == true` (default), after each image build:

1. `syft <image-ref> -o cyclonedx-json` writes `<image-ref>-sbom.json` to the working directory.
2. The SBOM is attached to the image as an OCI artifact:
   - If `oras` is on PATH: `oras attach <image> --artifact-type application/vnd.cyclonedx+json <sbom>`.
   - Otherwise: `cosign attach sbom --sbom <sbom> --type cyclonedx <image>`.
   - If neither is available: SBOM is generated but not attached (warning printed).

### Requirements

- `syft` binary on PATH (or `github.com/anchore/syft` as a Go module dep).
- `oras` or `cosign` on PATH for OCI attachment.

---

## Provenance attestation

When `security.provenance == "slsa-3"` (default), `docker buildx build` is passed `--attest=type=provenance,mode=max`. For ko builds, `--provenance` is passed natively.

---

## Base image policy

Restrict which base images are permitted:

```yaml
ci:
  build:
    security:
      base_image_policy:
        allow_prefixes:
          - gcr.io/distroless/
          - cgr.dev/chainguard/
        deny_prefixes:
          - ubuntu:latest
          - debian:latest
```

`wfctl build --security-audit` checks each `FROM` line against this policy.

---

## `wfctl build --security-audit`

Runs security linting across all targets and containers. Exit code 1 if any critical finding.

```sh
wfctl build --security-audit --config infra.yaml
```

### What it checks

**Dockerfile linting:**

| Finding | Severity |
|---------|---------|
| `USER root` or missing `USER` directive | critical |
| `FROM <base>:latest` without digest pinning | warn |
| `ADD` from an untrusted URL | warn |
| Commands that embed secrets | critical |
| Base image not in `allow_prefixes` | critical (if policy set) |

**Builder SecurityLint:**

Each builder's `SecurityLint()` method is called. See [04 — Builder Plugins](./04-builder-plugins.md) for per-builder findings.

---

## Opting out (not recommended)

```yaml
ci:
  build:
    security:
      hardened: false
      sbom: false
```

Running `wfctl ci validate` with `hardened: false` emits:

```
warning: hardened defaults disabled — images may not meet supply-chain baseline
```

### Local dev exemption

`wfctl dev up` automatically sets `hardened: false, sbom: false` for the `local` environment to avoid slowing down local iteration. Production deployments are unaffected.

---

*See also:* [Tutorial §15 — Signing + Attestation](../../tutorials/build-deploy-pipeline.md#15-signing--attestation) · [08 — Local Dev](./08-local-dev.md)
