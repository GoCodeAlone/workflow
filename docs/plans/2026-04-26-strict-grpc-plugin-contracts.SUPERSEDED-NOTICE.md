---
status: superseded
area: plugins
owner: jon
---

# Supersession Notice — 2026-04-26 Strict-Contracts Plan (IaC Scope)

**Date:** 2026-05-10
**Scope of supersession:** IaCProvider + ResourceDriver migration entries

The IaCProvider + ResourceDriver migration tracker entries in `2026-04-26-strict-grpc-plugin-contracts.md` are SUPERSEDED by the 2026-05-10 force-cutover plan:
- Design: `docs/plans/2026-05-10-strict-contracts-force-cutover-design.md`
- Plan: `docs/plans/2026-05-10-strict-contracts-force-cutover.md`

Per `feedback_force_strict_contracts_no_compat`: the 2026-04-26 additive approach was insufficient; the IaC migration needs hard-cutover.

The Module/Step/Trigger migration tracker entries (workflow-plugin-{audit, sso, ws-auth, authz, security, etc.}) in the 2026-04-26 plan REMAIN LIVE — they're not superseded.

This notice exists as a separate file per `feedback_plan_files_lead_owned`: the 2026-04-26 plan body is scope-locked (immutable until alignment-check + scope-lock-skill unlock); frontmatter is lead-gated and may be edited for status/supersession bookkeeping, but the body and migration-tracker tables remain frozen.
