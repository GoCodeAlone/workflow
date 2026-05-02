# wfctl infra drift detection + recovery — design

**Status:** Approved 2026-05-02 (user direction: "Detect drift sounds like a good idea. And you can do a refresh apply to recover, but we need to get to the point that we can safely recover and align... Definitely prefer solutions built into wfctl that are reusable").

## Goal

Production-safe state-vs-cloud drift detection and recovery primitives, built into wfctl as reusable commands. Validated against core-dump's current state-drift scenario (state thinks coredump-staging-db exists, DO returns 404), but designed to handle the same class of issue across all consumers + IaC plugins.

## Background — what exists vs what's missing

The CLI scaffolding is complete; the plugin-side implementation is the gap.

**Already exists:**
- `wfctl infra drift` CLI command — wired through `runInfraDrift` → `driftInfraModules` → `provider.DetectDrift(ctx, refs)`.
- `wfctl infra status` — calls `provider.Status(ctx, refs)`.
- `wfctl infra import --name <local> --id <cloud-id>` — adopts orphan cloud resource into state.
- `wfctl infra state {list, export, import}` — direct state CRUD.
- `interfaces.IaCProvider.DetectDrift` method signature.
- `interfaces.DriftResult{Name, Type, Drifted, Expected, Actual, Fields}` struct.
- `ResourceDriver.Read` already implemented per resource — the Read-vs-spec comparison is doable per-driver.

**Missing (the gap to close):**
- `workflow-plugin-digitalocean.DOProvider.DetectDrift` is a stub returning `Drifted: false` for everything (verified at `internal/provider.go:295-302`).
- `interfaces.DriftResult` has no field encoding the **class** of drift — can't distinguish "ghost in state (cloud says 404)" from "config drift (both exist, configs differ)".
- `wfctl infra apply` has no `--refresh` flag for safe state↔cloud reconciliation.
- No documented procedure / safety model for production recovery.

## Drift class taxonomy

Three classes of state-vs-cloud divergence the design must address:

- **(a) Ghost in state.** State says the resource exists; cloud Read returns "not found." Recovery: prune state entry. Next `wfctl infra plan` will then generate a `create` action that re-provisions the resource. **In scope for v1.**

- **(b) Orphan in cloud.** Cloud has the resource; state has no record. Detection requires listing all cloud resources of every managed type and reporting un-tracked ones — a fundamentally different traversal than DetectDrift's "iterate state". Recovery already exists via `wfctl infra import --name --id`. **Detection out of scope for v1; explicit import remains the supported recovery path.**

- **(c) Config drift.** Both state and cloud have the resource but their configs differ (someone edited the cloud-side directly, or apply was interrupted, or the driver computed Apply incorrectly). Recovery: reconcile (default direction = state → cloud, applying local config; rare reverse direction `--adopt-cloud` = cloud → state). **In scope for v1, default state→cloud.**

## Approach selection

Three approaches considered. Recommended: **(i) — minimal additive changes**.

### (i) Extend existing primitives (recommended)
- Implement DOProvider.DetectDrift properly (Read-per-resource, compare, report drift).
- Add `Class` enum field to `interfaces.DriftResult` (additive, backwards-compatible).
- Add `--refresh` flag to `wfctl infra apply` that runs DetectDrift first, prunes ghosts from state, then proceeds with normal plan/apply for class (c) reconciliation.
- Documented production recovery procedure in workflow docs.

**Trade-off:** smallest change; uses existing CLI surfaces; backwards-compatible with all existing consumers; no new interfaces.

### (ii) New `wfctl infra reconcile` command
A dedicated command with `--dry-run`/`--apply`/`--interactive` modes wrapping the same logic.

**Trade-off:** discoverable; clearer mental model. But duplicates plan/apply orchestration for marginal UX benefit; existing `wfctl infra apply --refresh` is more aligned with the established CLI shape.

### (iii) Two new commands: `wfctl infra prune` + `wfctl infra reconcile`
Separate primitive for class-(a) ghost prune from class-(c) config reconcile.

**Trade-off:** more granular but more surface to maintain; the unification under apply --refresh is what most operators reach for in practice.

## Design (sections scaled to complexity)

### Section 1 — Plugin-side DetectDrift implementation

In `workflow-plugin-digitalocean/internal/provider.go`, replace the stub `DetectDrift`:

```go
func (p *DOProvider) DetectDrift(ctx context.Context, resources []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
    var results []interfaces.DriftResult
    for _, ref := range resources {
        d, err := p.ResourceDriver(ref.Type)
        if err != nil {
            results = append(results, interfaces.DriftResult{
                Name:  ref.Name, Type: ref.Type,
                Class: interfaces.DriftClassUnknown,
                Fields: []string{"provider:" + err.Error()},
            })
            continue
        }
        out, err := d.Read(ctx, ref)
        if err != nil {
            if errors.Is(err, interfaces.ErrResourceNotFound) {
                // Ghost in state — cloud says 404
                results = append(results, interfaces.DriftResult{
                    Name:  ref.Name, Type: ref.Type,
                    Drifted: true,
                    Class:   interfaces.DriftClassGhost,
                })
                continue
            }
            // Transient API error — propagate; do NOT classify as drift
            return results, fmt.Errorf("read %s/%s: %w", ref.Type, ref.Name, err)
        }
        // Compare cloud-side actual to local state's applied-config (driver-specific)
        diff := d.Diff(ctx, /* desired from spec */, out)
        if diff != nil && diff.Drifted {
            results = append(results, interfaces.DriftResult{
                Name:  ref.Name, Type: ref.Type,
                Drifted: true, Class: interfaces.DriftClassConfig,
                Expected: diff.Expected, Actual: diff.Actual, Fields: diff.Fields,
            })
        } else {
            results = append(results, interfaces.DriftResult{
                Name: ref.Name, Type: ref.Type,
                Drifted: false, Class: interfaces.DriftClassInSync,
            })
        }
    }
    return results, nil
}
```

Key behaviors:
- **Ghost detection** uses `errors.Is(err, interfaces.ErrResourceNotFound)`. Other errors (rate limit, auth, network) propagate up — they are NOT eligible for state-prune.
- **Config drift** uses each driver's existing `Diff` method. The desired-spec source is either the local config (if available in DetectDrift's caller context) or the state's applied-config snapshot.
- **In-sync** results are recorded explicitly so callers (and the CLI) can show full coverage.

A practical note on transient-vs-genuine 404: use `interfaces.ErrResourceNotFound` as the wrapped sentinel error. Drivers must wrap real 404s with this sentinel when returning from `Read`. Generic API errors stay as-is — caller treats them as "unknown, retry later."

### Section 2 — DriftResult struct extension

Add a `Class` field to `interfaces.DriftResult` (backwards-compatible additive change):

```go
type DriftClass string

const (
    DriftClassUnknown DriftClass = ""        // legacy; absent = unknown
    DriftClassInSync  DriftClass = "in-sync"
    DriftClassGhost   DriftClass = "ghost"   // state has it, cloud says 404
    DriftClassConfig  DriftClass = "config"  // both exist, configs differ
)

type DriftResult struct {
    Name     string         `json:"name"`
    Type     string         `json:"type"`
    Drifted  bool           `json:"drifted"`
    Class    DriftClass     `json:"class,omitempty"` // NEW
    Expected map[string]any `json:"expected,omitempty"`
    Actual   map[string]any `json:"actual,omitempty"`
    Fields   []string       `json:"fields,omitempty"`
}
```

`omitempty` on Class ensures consumers that ignore the field still see legacy-shaped results.

### Section 3 — `wfctl infra apply --refresh` flag

Add `--refresh` to `runInfraApply`'s flag set. Behavior:

1. Run `DetectDrift` against current state.
2. For each `Class: DriftClassGhost` result:
   - In dry-run mode (default if `--refresh` is set): print "would prune <name> from state (cloud reports not found)".
   - In `--apply` mode (or `--auto-approve`): call `state.Delete(ctx, ref)`. Audit-log the prune.
   - Hard-block on `protected: true` resources unless `--allow-protected-prune` is also set. Print "BLOCKED: <name> is protected; explicit --allow-protected-prune required to prune."
3. After ghost-prune, run normal plan + apply. Class-(c) config drifts surface as update actions in the resulting plan.

Flags added:
- `--refresh` — enables drift-first refresh-then-apply
- `--allow-protected-prune` — explicit confirmation for protected resources
- (existing `--auto-approve` continues to apply)

Default operator workflow:
```
wfctl infra apply --refresh -c infra.yaml --env staging   # dry-run; prints what would be pruned
wfctl infra apply --refresh -c infra.yaml --env staging --auto-approve   # actually prunes + applies
```

### Section 4 — CLI surface for `wfctl infra drift`

Already exists; just extend the output to print the Class:

```
Detecting drift for infra.yaml...
  GHOST  coredump-staging-vpc (infra.vpc)        — cloud reports not found
  GHOST  coredump-staging-db (infra.database)    — cloud reports not found
  IN-SYNC  coredump-staging (infra.container_service)
  IN-SYNC  coredump-nats-staging (infra.container_service)

Drift detected — run 'wfctl infra apply --refresh' to prune ghosts and reconcile.
```

The exit code is non-zero if any drift is found (existing behavior). The new Class taxonomy makes the message actionable.

### Section 5 — Production safety model

The recovery primitive must be safe to run in production. Specifically:

- `wfctl infra apply --refresh` is **dry-run by default** when invoked without `--auto-approve`.
- Ghost-prune of `protected: true` resources requires `--allow-protected-prune` AND the operator typing the resource name interactively (or `--auto-approve`+`--allow-protected-prune` in CI). Two-key contract.
- All state mutations (prunes, updates) emit audit-log lines: `wfctl: state mutation <op> <resource> by <user> at <timestamp> reason=<message>`. Audit lines go to stderr so stdout remains a structured plan-summary.
- The `interfaces.ErrResourceNotFound` sentinel must be returned ONLY for genuine 404s — drivers must NOT wrap rate-limit, auth, or network errors with it. This is a load-bearing invariant for safe ghost-prune.

### Section 6 — Cross-plugin scope

PR-D1 implements DetectDrift only for workflow-plugin-digitalocean (validates the design + unblocks core-dump). Once landed, the same Read-loop pattern can be ported to AWS / GCP / Azure / Tofu plugins as a separate workstream (each plugin already has Read implementations; they just need to wrap-with-sentinel the not-found case).

Tracked as follow-up issues (not blocking this design): one issue per plugin asking for DetectDrift implementation.

### Section 7 — Validation against core-dump

After PR-D1 + PR-D2 + workflow release + workflow-plugin-digitalocean release + lockfile bumps land:

```
cd /Users/jon/workspace/core-dump
wfctl infra drift -c infra.yaml --env staging
# Expected output:
#   GHOST  coredump-staging-vpc (infra.vpc)        — cloud reports not found
#   GHOST  coredump-staging-db (infra.database)    — cloud reports not found
#   IN-SYNC  coredump-staging (infra.container_service)
#   IN-SYNC  coredump-nats-staging (infra.container_service)

wfctl infra apply --refresh -c infra.yaml --env staging --auto-approve
# Expected:
#   Pruning ghost: coredump-staging-vpc
#   Pruning ghost: coredump-staging-db
#   Plan: 2 actions (create coredump-staging-vpc, create coredump-staging-db)
#   Applying...
```

Deploy chain then completes through to staging app reachable on /healthz.

## Out of scope

- **Class-(b) orphan-in-cloud detection.** Requires cloud-wide resource listing across every managed type. Bigger workstream. Existing `wfctl infra import --name --id` remains the recovery path.
- **Cross-plugin rollout.** This design is plugin-1 (DigitalOcean). AWS/GCP/Azure/Tofu DetectDrift implementations are separate workstreams.
- **Cloud-as-source-of-truth `--adopt-cloud` mode.** Reverses the IaC contract; rare use case; left as a future option.
- **Drift webhook / continuous monitoring.** Out of scope; this design provides the detection primitive that a webhook would consume.
- **State-store transactional locking.** Existing locks remain; this design doesn't extend them.

## Cross-repo coordination

- PR-D1 lands in workflow-plugin-digitalocean (DetectDrift impl).
- PR-D2 lands in workflow (DriftClass enum + apply --refresh + docs).
- After both ship in releases (workflow-plugin-digitalocean v0.8.2 + workflow v0.20.5):
- PR-D3 bumps both lockfile + wfctl pin in core-dump + BMW.
- Validate against core-dump staging recovery as the empirical proof.

## Acceptance criteria

- `wfctl infra drift` on core-dump staging surfaces 2 ghosts (VPC + DB) with `Class: ghost`.
- `wfctl infra apply --refresh` (dry-run) prints "would prune VPC + DB" without mutating.
- `wfctl infra apply --refresh --auto-approve` prunes the 2 ghost entries from state, then applies the resulting 2-create plan, reaching App Platform deploy + /healthz green.
- Existing consumers without `--refresh` see no behavior change.
- Protected resources require explicit `--allow-protected-prune` to prune (verified by unit test).
- DetectDrift errors (transient API failures) propagate; do NOT trigger spurious prunes.

## System Impact

- **State store**: PR-D2 introduces state-mutation paths (prune) gated behind explicit flags. State-store contract unchanged; only callers added.
- **Plugin contract**: PR-D1 changes DOProvider.DetectDrift behavior. Backwards-compatible (only stub→real, return shape additive). Other plugins unaffected.
- **CLI**: `wfctl infra apply --refresh` is a new flag. Existing `wfctl infra apply` behavior unchanged for users who don't pass `--refresh`.
- **Production safety**: dry-run by default; `--allow-protected-prune` two-key contract; audit logging.
- **All other System Impact Matrix categories** (auth, anti-cheat, malware, sandbox, network, filesystem, process/OS, social, NPC, factions, economy, IoT, media, legal, forensics, VERA, achievements, client desktop, terminal, world history, content, telemetry): None — wfctl/plugin/state-mgmt plumbing.
