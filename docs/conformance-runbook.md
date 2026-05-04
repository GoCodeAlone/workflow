# Conformance Runbook

Operational reference for the W-7 IaC conformance suite + per-PR DO
smoke gate. Source of truth for budget approval, threshold changes,
token rotation, and operator interventions when CI fires alerts.

## Overview

The suite ships in `iac/conformance/` and is consumed by every IaC
provider plugin from a `*_test.go` calling
`conformance.Run(t, conformance.Config{...})`. Twelve scenarios pin
the cross-cutting contracts (Replace classification, Delete dispatch,
gRPC roundtrip, outputs refresh, plan-stale diagnostic,
cross-resource validation, JIT cross-module resolution, protected-
replace gate, read consistency, replace-cascade, upsert recovery).

The DO smoke gate runs `Scenario_NeedsReplaceTriggersReplaceAction`
against a real Droplet on every PR that touches the IaC dispatch
surface (`iac/`, `platform/`, `cmd/wfctl/infra*`,
`interfaces/iac_*.go`, `go.mod`/`go.sum`, the workflow files). The
sister workflow in `workflow-plugin-digitalocean` (added in
P-DO/TP5) provisions the Droplet; this repo's
`.github/workflows/conformance-smoke.yml` is the trigger + budget
gate.

## Budget approval

| Field | Value |
|---|---|
| Approver | `jon@langevin.me` |
| Approval date | 2026-05-03 |
| Hard-stop threshold | **$25/mo month-to-date usage** |
| Soft-alert threshold | $15/mo (60 % of cap) |
| Alert channels | GitHub issue (auto-filed via T7.14 dedup helper, label `conformance-budget-incident`); Slack `#wfctl-ops` if configured |
| Per-PR estimated cost | ~$0.0005/PR (one s-1vcpu-512mb-10gb in nyc1 for ~5 minutes) |

### Per-PR cost math

DO Basic Droplet `s-1vcpu-512mb-10gb` priced at **$4/month** /
**$0.00595/hour** per
[https://www.digitalocean.com/pricing/droplets](https://www.digitalocean.com/pricing/droplets)
(verified 2026-05-04).

```
$4/mo ÷ 30 days ÷ 24 hrs ÷ 60 min = $0.0000926/min
$0.0000926/min × 5 min lifetime  = ~$0.000463/PR
                                ≈ $0.0005/PR (sub-tenth-cent)
```

Re-verify pricing alongside any threshold change. Annual review is
appropriate; sooner if DO publishes a price change.

### Changing the cap

The cap is the source of truth. To raise/lower it:

1. Update this file with a new entry under "Budget approval" (do
   NOT overwrite the prior entry — append a row so the audit chain
   is preserved).
2. Update `.github/workflows/conformance-budget-check.yml` to match.
3. Open a PR. Merge requires the approver in the table above (or a
   designated successor named in a follow-up entry here).

## Budget calibration

The $25/mo cap is an estimate; recalibrate after **30 days** of
operational data (i.e., 30 days after T7.13 ships). Review process:

1. Pull DO billing for the conformance account: month-to-date usage
   for the calibration window.
2. Compute observed peak day-of-month spend; multiply by 30 ÷ days
   elapsed for the projected month-end value.
3. Compare to (a) the cap and (b) the soft-alert threshold; raise
   either if the steady-state spend already exceeds 60 % of the cap
   on a typical month.
4. Land the calibration result as a new entry under "Budget approval"
   (date, who reviewed, new threshold).

Recurring 30-day calibration is tracked as a routine task — keep the
review cadence on a calendar reminder rather than a CI cron so the
human-in-the-loop review stays human.

## Token rotation

The DO API token used by the smoke gate is a **long-lived token**
scoped to the `wfctl-conformance@gocodealone.dev` account (no
production resources colocated). Rotation cadence: **quarterly**.

### Rotation procedure

1. In the DO control panel, log in as `wfctl-conformance@…` and
   create a new Personal Access Token with read+write scopes.
   Note the new token value (DO shows it once).
2. Update the GitHub Actions repository secret
   `DO_CONFORMANCE_API_TOKEN` (Settings → Secrets and variables →
   Actions → Repository secrets → Update).
3. Trigger a manual `conformance-smoke.yml` run on a draft PR to
   confirm the new token works (budget pre-step succeeds).
4. After the manual run passes, **revoke** the old token in the DO
   control panel.
5. Update this section's "Last rotated" line below.

| Last rotated | By | Notes |
|---|---|---|
| 2026-05-03 | jon@langevin.me | Initial provisioning of the wfctl-conformance@ account + token |

### Token security invariants

- **NEVER** echo or log `${DO_CONFORMANCE_API_TOKEN}` in a workflow
  step. The budget-check workflow references it via `env:` block
  and `Authorization: Bearer ${DO_CONFORMANCE_API_TOKEN}` only.
- **NEVER** export the token to artifact / step-summary surfaces.
- The token MUST stay scoped to the dedicated account — do NOT
  rotate it onto a token that has access to the production org.

## Helper conventions

T7.14's `scripts/file-or-comment-leak-issue.sh` dedup helper uses
two labels to distinguish operator-filed issues from auto-filed
ones:

- `conformance-leak-incident` — primary label, attached to ALL
  related issues (auto + operator).
- `auto-filed-leak` — secondary label, attached ONLY by the helper.

> **Do not remove the `auto-filed-leak` label from helper-filed
> issues during triage.** It is the dedup key; removing it causes
> the next leak to file a NEW issue rather than appending. To stop
> the helper from dedup'ing onto a particular issue, **close the
> issue** (the helper queries `--state open`).
>
> If you need a cleaner postmortem swimlane, file a new issue with
> only the primary label `conformance-leak-incident` (NOT
> `auto-filed-leak`), link to the auto-filed issue, then close the
> auto-filed one. The next leak will re-open the dedup chain on a
> fresh helper-filed issue, leaving your postmortem issue
> undisturbed.

The same dedup helper is reused by the budget soft-alert (label
`conformance-budget-incident` on auto + operator issues; helper
adds `auto-filed-budget` for dedup).

## Cleanup contract

Layers, in order of execution:

1. **Per-test `t.Cleanup`** in the scenario body — fires on
   success AND failure paths. Force-deletes the resource via the
   provider's driver API.
2. **Outer-job `always()` step** in `conformance-smoke.yml` —
   safety net for panicking tests where `t.Cleanup` did not run.
   Currently a STUB pending `wfctl infra cleanup --tag <name>`
   (tracked as a W-7 follow-up); falls through to layer 3.
3. **Hourly leak-scrubber cron** (`conformance-leak-scrubber.yml`,
   added in T7.14) — lists DO resources tagged
   `conformance-pr-*` older than 1 hour and deletes them.
   Counts get aggregated into the dedup helper's issue body.

If layer 3 fires more than once a day or scrubs > 3 resources in
a single run, the helper escalates by appending to the open issue.
Sustained > 3-events/day for a week is a signal to investigate the
panic source in the test code.

## Operator interventions

### "Conformance budget exceeded" CI failure

Cause: month-to-date spend on the conformance account > $25.

Steps:

1. Check the auto-filed issue (label `conformance-budget-incident`)
   for context.
2. Inspect DO billing on the conformance account; identify the
   spike source. Likely culprits: a panicking test orphaning many
   Droplets, or a price change in the Droplet plan.
3. If the spike was a one-off (now scrubbed), close the issue. The
   next budget-check that observes spend ≤ $25 unblocks PRs.
4. If the spike persists, raise the cap (see "Changing the cap"
   above) — do NOT silently bump it in CI without an audit entry.

### "auto-filed-leak" issue keeps appending

Cause: the leak-scrubber is finding orphaned resources every hour.

Steps:

1. Check the recent commits to `iac/conformance/` and provider
   plugin repos for changes to scenario `t.Cleanup` blocks.
2. Inspect the most recent CI runs of `conformance-smoke.yml` for
   panics in the smoke job's test step.
3. Once the panic source is fixed, close the auto-filed issue —
   the next clean hour resets the dedup chain.

## Workflow inventory

| File | Purpose |
|---|---|
| `.github/workflows/conformance-smoke.yml` | Per-PR smoke gate (paths-filter + budget pre-step + scenario run + always() cleanup safety net) |
| `.github/workflows/conformance-budget-check.yml` | Reusable kill-switch called by every smoke job pre-step; hourly-cached DO billing query + threshold enforcement |
| `.github/workflows/conformance-leak-scrubber.yml` | Hourly cron (T7.14) that deletes orphaned `conformance-pr-*`-tagged Droplets |
| `.github/workflows/scripts/file-or-comment-leak-issue.sh` | Dedup helper (T7.14) — files OR comments on a single open issue keyed on label pair |

## Known follow-ups

- **`wfctl infra cleanup --tag <name>` does not exist yet.** Until
  implemented, smoke jobs rely on T7.14's hourly leak scrubber to
  catch orphaned resources from panicking tests. The smoke
  workflow's outer-job `always()` step currently logs the cleanup
  intent and defers to the scrubber. Tracked as a workflow-repo
  issue (filed at PR-create time) titled "implement wfctl infra
  cleanup --tag for full-wfctl conformance gate cleanup".
- **Sister workflow in `workflow-plugin-digitalocean`** (P-DO/TP5)
  — provisions the actual Droplet for the smoke run. This repo's
  workflow currently exercises the in-tree self-tests until the
  sister workflow lands.
- **`platform.SetDiffCacheForTest` was promoted in W-7** so
  `conformance.Run` can install a deterministic noop cache before
  scenarios execute. See commit `a1fba34` for the rationale.
