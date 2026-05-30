# Multisite CI Generation Gap Analysis

This document records an honest comparison between `wfctl ci generate` output
and the hand-written `.github/workflows/infra.yml` for `GoCodeAlone/gocodealone-multisite`.

**The live `infra.yml` is NOT modified.** `generated-infra.yml` is a committed
validation artifact per ADR 0004 (demonstration-fidelity mandate).

## How this was produced

The `--config-path-alias` / `--phase-config-alias` flags are used so the
committed artifact shows clean repo-relative `deploy.yaml` / `deploy.prereq.yaml`
paths instead of the testdata-relative paths the binary would otherwise emit
(it uses the `--config` argument verbatim as the CI trigger `paths:` filter).

```sh
# 1. Real ci plan
wfctl-pr4 ci plan \
  -c cigen/testdata/multisite/deploy.yaml \
  --phase-config cigen/testdata/multisite/deploy.prereq.yaml \
  --config-path-alias deploy.yaml \
  --phase-config-alias deploy.prereq.yaml \
  --out cigen/testdata/multisite/plan.json

# 2. Real ci generate
wfctl-pr4 ci generate \
  -c cigen/testdata/multisite/deploy.yaml \
  --phase-config cigen/testdata/multisite/deploy.prereq.yaml \
  --config-path-alias deploy.yaml \
  --phase-config-alias deploy.prereq.yaml \
  --platform github_actions \
  --out cigen/testdata/multisite \
  --write
# output: cigen/testdata/multisite/.github/workflows/multisite.yml
# renamed to: cigen/testdata/multisite/generated-infra.yml

# 3. Real diff
diff -u .../infra.yml cigen/testdata/multisite/generated-infra.yml > /tmp/multisite.diff
```

## Real plan.json warnings[] array

```json
[
  "secret \"MULTISITE_DB_URL\" is populated by IaC output — the real GitHub secret name may differ (e.g. include a resource hash suffix); verify the secret name matches what wfctl infra bootstrap writes",
  "secret \"SPACES_access_key\" does not match ^[A-Z0-9_]+$ — the config casing is preserved as-is; you may need a GitHub-side alias if the platform normalises secret names to upper-case",
  "secret \"SPACES_secret_key\" does not match ^[A-Z0-9_]+$ — the config casing is preserved as-is; you may need a GitHub-side alias if the platform normalises secret names to upper-case"
]
```

## Real diff summary

- Hand-written `infra.yml`: 283 lines
- Generated `generated-infra.yml`: 134 lines
- Diff: 248 lines removed, 99 lines added (384-line diff total)

Nearly all lines differ. The generated file is correct in structure and
logically sound, but is much simpler than the production hand-written file.

---

## Matched (correctly derived from config)

The following features ARE present in the generated output and match the
intent of the hand-written workflow, as confirmed by the diff:

- **Two-phase plan/apply structure**: `plan`, `apply-prereq`, `apply-deploy`
  jobs in the correct sequence with `needs: apply-prereq` chaining.
- **PR trigger + push-to-main trigger + workflow_dispatch**: all three trigger
  types present in `on:`.
- **paths: filters on both PR and push**: clean repo-relative `deploy.yaml`
  and `deploy.prereq.yaml` paths (via `--config-path-alias` / `--phase-config-alias`).
- **wfctl plugin install step**: present in plan and both apply jobs (derived
  from `iac.*` and `analytics.*` module types → `plugin_install=true`).
- **Plan-guard in both apply jobs**: the `wfctl infra plan | tee plan-guard.txt`
  + grep pattern that refuses destructive changes is present in both
  `apply-prereq` and `apply-deploy`. Derived from `protected: true` on both
  `multisite-pg` and `gocodealone-multisite` modules.
- **apply-job `env:` block with 1:1 secret name mapping**: all 16 secrets
  from the union of both configs are present in the `env:` block on each
  apply job using `${{ secrets.NAME }}` syntax.
- **Two-phase plugin install in plan job**: `Install plugins (prereq)` and
  `Install plugins (deploy)` steps both present.
- **Migrations step in apply-deploy** (FUNCTIONAL — fixed): `Run migrations`
  step runs `wfctl migrations up --config 'deploy.yaml'`, the REAL migration
  runner, derived from `ci.migrations`. This is the same subcommand the
  hand-written workflow uses (`wfctl migrations up --config deploy.yaml --env
  prod --format json`); the generated form omits the operational `--env prod
  --format json` flags (see "NOT derivable" below). A previous version of the
  generator emitted `wfctl ci run --config ... --phase migrate`, which is NOT a
  valid `ci run` phase (only build|test|deploy) and would have failed at
  runtime; that defect is fixed.
- **Smoke job**: derived from `infra.container_service` with
  `health_check.http_path=/healthz` and `domain: gocodealone.tech`. URL
  correctly computed as `https://gocodealone.tech/healthz`.
- **Post plan comment job**: GitHub script step that posts plan.md as PR comment.
- **permissions: contents: read + pull-requests: write**: at workflow level.

---

## NOT derivable (stays hand-authored), with real warnings

The following features are present in the hand-written `infra.yml` but NOT
produced by the generator. Where relevant, the real warnings[] from plan.json
explain the gap.

### 1. Hash-suffixed DB secret name

The hand-written workflow uses:
```yaml
MULTISITE_DB_URL: ${{ secrets.MULTISITE_PG__URI_6D4758EBCF22872E6C0D93190FB952E4 }}
```

The generator uses the config name verbatim:
```yaml
MULTISITE_DB_URL: ${{ secrets.MULTISITE_DB_URL }}
```

The plan.json warning explicitly flags this:
> "secret "MULTISITE_DB_URL" is populated by IaC output — the real GitHub
> secret name may differ (e.g. include a resource hash suffix); verify the
> secret name matches what wfctl infra bootstrap writes"

The hash (`6D4758EBCF22872E6C0D93190FB952E4`) is a wfctl-infra-bootstrap output;
it is not derivable from the deploy config alone.

### 2. SPACES_access_key → SPACES_ACCESS_KEY case normalisation

The hand-written workflow passes secrets as:
```yaml
SPACES_access_key: ${{ secrets.SPACES_ACCESS_KEY }}
SPACES_secret_key: ${{ secrets.SPACES_SECRET_KEY }}
```

The generator uses the config casing verbatim:
```yaml
SPACES_access_key: ${{ secrets.SPACES_access_key }}
SPACES_secret_key: ${{ secrets.SPACES_secret_key }}
```

The plan.json warnings flag both:
> "secret "SPACES_access_key" does not match ^[A-Z0-9_]+$ — the config casing
> is preserved as-is; you may need a GitHub-side alias if the platform
> normalises secret names to upper-case"

GitHub Actions silently normalises stored secret names to upper-case, so
`SPACES_access_key` on the left side of the env mapping must reference
`secrets.SPACES_ACCESS_KEY` on the right side when the secret was stored
under the upper-case name. The generator warns but cannot resolve this
automatically without inspecting the live GitHub secret store.

### 3. Image wait loop (GHCR polling)

The hand-written `apply-deploy` includes a step that polls the GHCR package
registry until the container image built by `build.yml` is available:
```yaml
- name: Wait for image
  env:
    GH_TOKEN: ${{ secrets.RELEASES_TOKEN || github.token }}
    IMAGE_SHA: ${{ inputs.image_sha || github.sha }}
  run: |
    short_sha="${IMAGE_SHA:0:12}"
    for i in $(seq 1 30); do
      if gh api -X GET /orgs/.../versions --jq "..." | grep -qx "${short_sha}"; then ...
```

The generator has no knowledge of a separate `build.yml` workflow or that
this image must be waited for. Not derivable.

### 4. GHCR_CREDENTIALS derivation step

The hand-written workflow derives `GHCR_CREDENTIALS` dynamically from
`RELEASES_TOKEN`:
```yaml
- name: Derive GHCR_CREDENTIALS
  run: printf 'GHCR_CREDENTIALS=%s:%s\n' "$actor" "$GH_TOKEN" >> "$GITHUB_ENV"
```

The generator simply passes `${{ secrets.GHCR_CREDENTIALS }}` as a static
secret. The config `deploy.yaml` declares `GHCR_CREDENTIALS` as a named
secret, so the generator is technically correct for the declared config — but
the runtime implementation is more sophisticated. Not derivable from config.

### 5. workflow_dispatch phase selector inputs

The hand-written workflow has typed `workflow_dispatch.inputs`:
```yaml
workflow_dispatch:
  inputs:
    phase: {type: choice, options: [auto, prereq, full]}
    image_sha: {required: false, ...}
```

This drives conditional apply logic (`inputs.phase == 'prereq'`). The
generator emits `workflow_dispatch:` with no inputs.

### 6. apply-deploy conditional includes apply-prereq skip logic

The hand-written `apply-deploy` condition:
```yaml
always() &&
(needs.apply-prereq.result == 'success' || needs.apply-prereq.result == 'skipped') &&
((github.event_name == 'push' && ...) ||
 (github.event_name == 'workflow_dispatch' && (inputs.phase == 'full' || inputs.phase == 'auto')))
```

The generator emits a simpler condition:
```yaml
if: "(github.event_name == 'push' && github.ref == 'refs/heads/main') || github.event_name == 'workflow_dispatch'"
```

The `always()` + `skipped` guard is not derivable. Also missing: the
phase-input-gated dispatch (`inputs.phase == 'full'`).

### 7. continue-on-error: true on plan steps

The hand-written plan job uses `continue-on-error: true` on both plan steps
so a fresh deploy (where `SPACES_*` secrets don't yet exist) can still pass.
The generator emits plan steps without `continue-on-error`.

### 8. Per-step env blocks in plan + apply-prereq

The hand-written workflow passes `DIGITALOCEAN_TOKEN`, `RELEASES_TOKEN`,
`SPACES_access_key`, `SPACES_secret_key` as per-step env on the plan steps
and apply-prereq step (not as job-level env). The generator places the full
secret set in the job-level `env:` block instead, which technically works
but differs in structure. This is a correctness delta, not just style.

### 9. setup-go and SHA-pinned action references

The hand-written workflow pins actions by SHA:
```yaml
- uses: GoCodeAlone/setup-wfctl@362fe9aaf4792e5adffa2b406ee39dcad31f54a9
```
and adds `actions/setup-go@v6` with `go-version-file: go.mod`.

The generator uses tag references (`@v4`, `@v1`) without SHA pinning and
does not emit a Go setup step. Not derivable from config.

### 10. analytics.google_ga4_ensure apply pipeline step

The `deploy.yaml` has:
```yaml
pipelines:
  apply:
    steps:
      - name: ensure_gocodealone_ga4
        type: step.analytics_google_ga4_ensure
```

The generator has no hook for `pipelines.apply` steps and emits no GA4
provisioning step in CI. Not derivable without pipeline-step introspection.

### 11. Concurrency group

The hand-written workflow has:
```yaml
concurrency:
  group: gocodealone-multisite-infra-${{ github.ref_name }}-...
  cancel-in-progress: true
```

The generator emits no `concurrency:` block. Not derivable.

### 12. Custom multi-route smoke matrix

The hand-written smoke job tests 6 routes across 3 domains with retry loops.
The generator emits a single `curl --fail` against `https://gocodealone.tech/healthz`.
The smoke URL is correctly derived from the `infra.container_service` health
check path and primary domain, but the multi-domain retry matrix is not.

### 13. Workflow name and global env vars

The hand-written workflow is named `Infrastructure` with global env vars
(`MULTISITE_PUBLIC_URL`, `MULTISITE_WWW_URL`, `MULTISITE_ADMIN_URL`).
The generator derives the name from the config basename (`multisite`) and
emits no global env block.

### 14. Migration `--env prod --format json` flags

The generated migrations step is now functional and uses the correct
subcommand:
```yaml
run: wfctl migrations up --config 'deploy.yaml'
```

The hand-written form adds operational flags:
```yaml
run: wfctl migrations up --config deploy.yaml --env prod --format json
```

The `--env prod` flag selects a deploy-environment-scoped migration config;
the `deploy.yaml` config has a single top-level `ci.migrations` block with no
named environments, so the generator has no environment name to emit. (The
generator WILL emit `--env <env>` when `MigrationsSpec.Env` is set.) `--format
json` is a presentation choice. Neither is derivable from the base config.

---

## What the generator got WRONG (not just incomplete)

None remaining at command level. Both previously-documented defects are FIXED:

- **Migration step command (FIXED)**: the generator previously emitted
  `wfctl ci run --config ... --phase migrate`, but `wfctl ci run` only accepts
  the phases `build|test|deploy` and errors on anything else (`unknown phase:
  "migrate"`) — the generated step would have failed at runtime. It now emits
  `wfctl migrations up --config '<cfg>'`, the real migration runner. The
  DB-url secret is already wired into the apply job's `env:` block via the
  secrets union, so the command can read it.
- **paths: filter (FIXED in this artifact)**: the binary uses the `--config`
  argument verbatim as the CI trigger `paths:` filter, so running it from the
  workflow repo root with `cigen/testdata/multisite/deploy.yaml` would emit a
  testdata-relative path. This artifact now uses `--config-path-alias deploy.yaml`
  / `--phase-config-alias deploy.prereq.yaml` to produce clean repo-relative
  paths. Operators generating CI for a real project should either run from the
  project root (so `-c deploy.yaml` is already clean) or pass the alias flags.

---

## Verdict

`wfctl ci generate` correctly derives the two-phase plan/apply structure,
plan-guard, secret inventory (by config name), smoke URL, a FUNCTIONAL
migrations step (`wfctl migrations up`), plugin install, and trigger shape.
It correctly warns about the DB hash-suffix and SPACES casing gaps. It does
NOT derive: hash-suffixed secret references, image wait loops, GHCR credential
derivation, phase-selector dispatch inputs, action SHA pinning, apply pipeline
steps, concurrency guards, per-step env scoping, multi-route smoke matrix, the
`always()+skipped` dependency condition, or the migration `--env prod --format
json` operational flags. The generator is a useful starting scaffold; the
14+ hand-authored additions are each justified by runtime or operational
concerns not encodable in the workflow config format alone.
