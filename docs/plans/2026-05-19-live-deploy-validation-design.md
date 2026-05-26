# Live-Deployment Example Validation — Design

**Date:** 2026-05-19
**Trigger:** The 2026-05-19 multi-repo QoL sweep validated plugin examples at SCHEMA level (`wfctl validate --skip-unknown-types`) but never ran them end-to-end against real cloud accounts. Promotion from `experimental` to `verified` remains a manual decision tied to GoCodeAlone-internal usage.
**Mode:** Design only (operator must provision CI secrets before execution).

## Problem

`wfctl validate --skip-unknown-types` confirms a YAML config parses and references known module types, but does not exercise the providers. A plugin can ship with valid YAML that fails at runtime — wrong field types, deprecated APIs, broken auth flow, infra-side rate limits. Today nothing catches this until an operator pins the plugin in a real project.

Symptoms today:
- `aws#23`, `gcp#16`, `azure#20`, `tofu#11`, `ci-generator#9` shipped READMEs + examples that pass schema validation but have never been live-tested.
- `digitalocean` is the only IaC plugin with merged production usage (BMW + core-dump + workflow-compute).
- Promotion from `experimental` → `verified` requires a human to pin the plugin in a real wfctl.yaml. Slow + manual.

## Goal

Add a CI matrix that runs each P0/P1 plugin's `examples/minimal/config.yaml` against staging cloud accounts via OIDC. On green, the plugin auto-promotes to `verified` via a registry-manifest PR. On red, surfaces a failure annotation on the plugin's repo + opens a tracking issue.

## Non-Goals

- Replace the existing `wfctl validate --skip-unknown-types` schema check (still useful as a fast gate).
- Run examples for non-IaC, non-cloud plugins (eventbus, payments, twilio etc. need different validation surfaces — payments needs a Stripe test API; twilio needs a sandbox account; those are out of scope here).
- Validate against PRODUCTION accounts. Staging only.

## Approach

### Phase 1: per-provider staging accounts + OIDC

For each IaC provider (AWS, GCP, Azure, DigitalOcean, OpenTofu-via-any-provider):

1. Operator creates a dedicated staging account/project/subscription.
2. Configure GitHub OIDC trust:
   - AWS: IAM role + `aws-actions/configure-aws-credentials@v4`.
   - GCP: Workload Identity Federation + `google-github-actions/auth@v2`.
   - Azure: federated credential + `azure/login@v2`.
   - DigitalOcean: short-lived API token rotated via OIDC + Vault (or accept long-lived staging token).
3. Repo secret matrix populated:
   ```
   STAGING_AWS_ROLE_ARN
   STAGING_GCP_WORKLOAD_IDENTITY_PROVIDER + STAGING_GCP_SERVICE_ACCOUNT
   STAGING_AZURE_TENANT_ID + AZURE_CLIENT_ID + AZURE_SUBSCRIPTION_ID
   STAGING_DIGITALOCEAN_TOKEN
   ```

### Phase 2: workflow main repo — `live-deploy.yml` workflow

New workflow file `.github/workflows/live-deploy.yml`:

```yaml
name: live-deploy
on:
  workflow_dispatch:
  schedule:
    - cron: '0 6 * * 1'   # weekly Monday 06:00 UTC
permissions:
  id-token: write
  contents: read
  pull-requests: write
jobs:
  live-deploy:
    strategy:
      fail-fast: false
      matrix:
        plugin: [aws, gcp, azure, digitalocean, tofu]
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          repository: GoCodeAlone/workflow-plugin-${{ matrix.plugin }}
          ref: main
      - uses: GoCodeAlone/setup-wfctl@v1
      - name: Configure cloud auth (${{ matrix.plugin }})
        run: ./.github/scripts/cloud-auth.sh ${{ matrix.plugin }}
      - name: wfctl deploy --dry-run
        run: wfctl deploy --dry-run examples/minimal/config.yaml
        timeout-minutes: 10
      - name: Report status
        if: always()
        run: ./.github/scripts/report-validation.sh ${{ matrix.plugin }} ${{ job.status }}
```

### Phase 3: registry promotion / demotion

`report-validation.sh` consumes the job status and:

- **GREEN:** if the plugin's registry manifest is currently `experimental`, opens a PR against `workflow-registry` flipping it to `verified` with a citation to the workflow run.
- **RED:** if the plugin's manifest is currently `verified`, opens a PR demoting it to `experimental` + opens a tracking issue on the plugin repo.
- **NO CHANGE:** no PR; record the run in a structured artifact for audit.

A `--explain` flag on `wfctl plugin marketplace-verify` (already shipped in `workflow#725`) can read the latest validation-run artifact to display the live-deploy history alongside the org-usage signal.

## Assumptions

- Operator can provision dedicated staging accounts. **Load-bearing.** Without this, the workflow is inert.
- `wfctl deploy --dry-run` exists for all 5 IaC providers. **Verify before execution** — `digitalocean` has it (used in BMW); `aws`/`gcp`/`azure`/`tofu` need verification.
- OIDC trust for all 4 cloud providers is achievable from GitHub Actions. True today — all 4 publish official auth actions.
- The cost of running the matrix weekly is bounded (no idle infra; each example deploys + tears down). Estimated <$5/week if examples are correctly written.
- Promotion/demotion PRs are admin-mergeable autonomously (per `feedback_admin_override_pr_merge`).

## Top doubts

1. **Cost runaway.** Examples that fail to tear down can leave cloud infra running. Mitigation: each example must include a teardown step + the workflow runs `wfctl destroy` after `deploy --dry-run` even on failure. Verify in alignment-check.
2. **Flaky staging.** Cloud-provider transient errors will cause false demotions. Mitigation: require 2 consecutive RED runs before opening a demotion PR. The first RED opens an investigation issue.
3. **wfctl deploy --dry-run semantics differ across providers.** If `--dry-run` is too permissive, the signal is meaningless. Verify each provider's dry-run actually validates IAM/API access, not just YAML.

## Rollback

The workflow is fire-and-forget reporting; rollback = disable the workflow file. No data is persisted in workflow main beyond the PR/issue creations, which are independently revertible.

## Dependencies

- `workflow#725` (`marketplace-verify` subcommand) is the human-readable counterpart to this automated promotion path.
- ADR-0041 (experimental-status marker) defines the manifest schema this PR exercises.

## Success criteria

- 5 IaC plugins (aws, gcp, azure, digitalocean, tofu) have weekly green live-deploy runs.
- Promotion PR opened automatically when a plugin earns its first GREEN run.
- Demotion PR + issue opened automatically when a `verified` plugin earns 2 consecutive REDs.
- Operator inspects monthly cost report and signs off on continued scheduled runs.

## Out of scope

- Live-deployment validation for non-IaC plugins (eventbus / payments / twilio / etc.) — file separate designs per provider category.
- Replacing the existing `wfctl validate --skip-unknown-types` schema check.
- Integration with workflow-cloud SaaS — this is workflow-engine OSS scope.
