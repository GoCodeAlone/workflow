# ADR 0009 — wfctl --include: positive-list scope flag, distinct from --target

**Status:** Accepted 2026-05-07

## Context

The TC2 cutover workflow for core-dump applies infrastructure changes against a large multi-resource config. The original approach used YAML rewriting to stub out resources not targeted in a given cutover phase. This was fragile: a YAML rewrite bug could silently apply to the wrong resource set, and the rewrite logic was duplicated across CI steps.

A scope-filter flag was requested to allow operators to say "apply only these named resources" without modifying the config file. The desired flag is a positive-list ("include these resources") rather than a negative-list ("exclude these resources") or an action-list ("only create/update/delete").

### Candidate designs

1. **`--target=<csv>`**: precedent from Terraform. Single flag controlling which resources are planned and applied.
2. **`--include=<csv>`**: positive-list scope filter. Names must exist in config or state; unknown names are rejected at parse time.

The user explicitly de-scoped `--target` as an action-list flag (one that controls which actions from a plan are executed). The team record notes: "action-list scoping is out of scope" (plan locked 2026-05-06).

## Decision

Add `--include=<csv>` to both `wfctl infra apply` and `wfctl infra plan`. The flag:

- Accepts a comma-separated list of resource names.
- Filters both the desired `ResourceSpec` list AND the current `ResourceState` list before they are passed to the provider.
- State-only resources (in state but not in config) are eligible for delete when included; spec-only resources (in config but not in state) are eligible for create when included.
- An empty `--include` (or no flag) means all-resources (back-compat behavior).
- Unknown names (not in config or state) are rejected at apply/plan entry with a descriptive error listing all unknown names.
- `--include` + `--plan` is rejected at flag-parse time: a plan already carries the scope from the plan-time invocation; applying a scoped plan with a different `--include` would produce confusing partial-apply behavior.

Design 1 (`--target`) was not selected because the adversarial design review (Critical finding "App stub fragility") specifically called out that `--target` terminology implies action-list semantics to Terraform users, which conflicts with the intended positive-list scope behavior. `--include` is unambiguous: it names what is in scope, nothing more.

## Consequences

- TC2 cutover can invoke `wfctl infra plan --include=<csv>` and `wfctl infra apply --plan plan.json` without YAML rewriting.
- Operators running ad-hoc partial applies can scope to a subset of resources without editing config files.
- Resources NOT in the include set are left entirely untouched by the scoped apply (state is not loaded for them; they do not appear in the plan).
- Out-of-scope resources that drift in the interim will not be detected or corrected by a scoped apply; operators must run an unscoped apply to reconcile the full resource set.
- Sibling commands (`wfctl infra status`, `wfctl infra refresh-outputs`) do not yet support `--include`; adding it there is a follow-up.

## References

- Design doc: `docs/plans/2026-05-06-iac-state-truth-and-tc2-closeout-design.md`
- Plan: `docs/plans/2026-05-06-iac-state-truth-and-tc2-closeout.md` Task 10 (T2.4)
- Adversarial design review Critical finding: "App stub fragility" (cycle 1)
- Scope lock: `docs/plans/2026-05-06-iac-state-truth-and-tc2-closeout.md.scope-lock`
- Out-of-scope: `--target` as action-list (user de-scoped, plan locked)
