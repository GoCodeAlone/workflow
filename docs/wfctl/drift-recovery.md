# wfctl infra drift recovery

Operator procedure for detecting and recovering from IaC state drift.

## What is drift?

Drift is a divergence between the IaC state store (what wfctl believes exists)
and the actual cloud state (what the provider API reports). Three classes:

| Class | Description | Recovery |
|-------|-------------|----------|
| `ghost` | State says resource exists; cloud returns 404 | Prune state entry via `--refresh` |
| `config` | Both exist but configs differ (e.g. someone edited cloud-side) | Reconcile via normal `wfctl infra apply` |
| `in-sync` | State and cloud agree | No action needed |

## Detect drift

```sh
wfctl infra drift -c infra.yaml --env staging
```

Example output:

```
Detecting drift for infra.yaml...
  GHOST    coredump-staging-vpc          infra.vpc             — cloud reports not found
  GHOST    coredump-staging-db           infra.database        — cloud reports not found
  IN-SYNC  coredump-staging              infra.container_service
  IN-SYNC  coredump-nats-staging         infra.container_service

drift detected — run 'wfctl infra apply --refresh' to prune ghosts and reconcile
```

Exit code is non-zero when any drift is found. Use in CI to gate deploys.

## Recover ghost-in-state entries

A ghost means the cloud resource was deleted outside of wfctl (manual delete,
billing expiry, failed apply that partially cleaned up, etc.). The state store
still has a record, so wfctl's next plan sees no action — the resource stays
deleted with no creates.

**Step 1 — dry-run (always do this first):**

```sh
wfctl infra apply --refresh -c infra.yaml --env staging
```

This runs drift detection and prints what *would* be pruned — no state
mutations occur. Confirm the listed ghosts are genuine 404s.

**Step 2 — approve and prune:**

```sh
wfctl infra apply --refresh -c infra.yaml --env staging --auto-approve
```

For each ghost, wfctl:
1. Emits an audit log line to stderr (timestamp + resource name + type)
2. Calls `store.DeleteResource` to remove the state entry
3. Prints `Refresh: pruned ghost <name> (<type>)`

After pruning, the normal plan+apply phase runs. The plan will now generate
`create` actions for the pruned resources (since they are absent from both
state and cloud).

## Protected resources

Resources with `protected: true` in their applied state outputs are guarded
against accidental pruning. Without an explicit override, the command errors
and prints:

```
wfctl: BLOCKED: coredump-staging-db is protected; cannot prune without --allow-protected-prune
```

To prune a protected resource, pass both flags:

```sh
wfctl infra apply --refresh --allow-protected-prune -c infra.yaml --env staging --auto-approve
```

This is a two-key contract: `--refresh` + `--allow-protected-prune` must both
be present to permit the prune. Use this only after manually confirming the
cloud resource is genuinely absent (e.g. `doctl databases list`, AWS console).

## Audit log

Every state mutation (prune) emits a line to stderr:

```
wfctl: state mutation prune <name> (type=<type> protected=<bool>) reason=ghost-in-state at <timestamp>
```

In CI, capture stderr to your run logs. These lines are machine-parseable for
audit trail requirements.

## Production safety checklist

Before running `--auto-approve` in a production environment:

- [ ] Run `wfctl infra drift` and review all ghost entries
- [ ] Verify each ghost is a genuine 404 via cloud console or provider CLI
- [ ] Confirm the state store backup is current (if applicable)
- [ ] Confirm no in-flight apply is running against the same state backend
- [ ] For protected resources: double-check the resource is not in a soft-delete
      / pending state (some providers briefly return 404 during deletion)

## Config drift (class=config)

Config drift does not require `--refresh`. The normal `wfctl infra apply`
reconciles config drift by re-applying the declared spec to the cloud resource.
The `wfctl infra drift` output shows field-level differences:

```
  CONFIG   my-cluster   infra.k8s_cluster
    node_count: expected=3  actual=2
```

Run `wfctl infra apply -c infra.yaml --env staging --auto-approve` to converge.

## CI integration

Add drift detection as a blocking check before deploy:

```yaml
- name: Detect drift
  run: wfctl infra drift -c infra.yaml --env staging
  # exits non-zero if any drift; fails the step
```

Add ghost-recovery as a separate manual workflow step to run before re-deploy
after an interrupted apply:

```yaml
- name: Recover state drift (manual)
  run: wfctl infra apply --refresh -c infra.yaml --env staging --auto-approve
  if: github.event_name == 'workflow_dispatch'
```

This document is the canonical operator reference for drift recovery.
For detailed design rationale see the inline comments in
`cmd/wfctl/infra_apply_refresh.go`.
