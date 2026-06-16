### Adversarial Review Report

**Phase:** plan
**Artifact:** docs/plans/2026-06-16-plugin-lock-and-domain-migration.md
**Status:** PASS

**Findings (Critical):**
- None.

**Findings (Important):**
- `P1` [Infrastructure verification mismatch] [Task 5]: New private GitHub repo and secrets were required but verification did not explicitly prove privacy or secret presence. Recommendation: add `gh repo view` and `gh secret list` checks. _Resolution: plan updated Task 5 steps/verify._
- `P2` [Missing integration proof] [Task 6]: Tenant registration could be blocked by admin/bootstrap auth, tempting ad hoc scripts. Recommendation: make missing admin auth a stop-before-DNS gate and route to onboarding automation follow-up. _Resolution: plan updated Task 6 and F4._

**Findings (Minor):**
- `P3` [Hidden serial dependencies] [Scope Manifest]: Tasks 4 and 7 depend on released artifacts/route proof from earlier tasks; parallel execution would be unsafe. Recommendation: keep PR grouping but execute only after release/proof gates. _Resolution: task steps already encode gates._

**Bug-class scan transcript:**

| Class | Result | Note |
|---|---|---|
| Project-guidance conflicts | Clean | Plan respects core/plugin boundaries and infrastructure plan-before-apply. |
| Assumptions under attack | Finding | P2 tightened admin/onboarding assumption. |
| Repo-precedent conflicts | Clean | Uses existing `cmd/wfctl`, provider plugin, DNS repo, and multisite content-repo paths. |
| Artifact-class precedent | Clean | CLI tests/docs, plugin tests, DNS repo runbooks, content-repo template match existing shapes. |
| YAGNI violations | Clean | `plugin ci` is justified by CI no-write requirement; no broad package manager rewrite. |
| Missing failure modes | Finding | P1/P2 fixed repo secret/admin proof paths. |
| Security / privacy | Clean | No secret values printed; private content repo and rights guard included. |
| Infrastructure impact | Finding | P1 added explicit GitHub repo/secret verification. |
| Multi-component validation | Finding | P2 added no-DNS-until-route/admin proof gate. |
| Rollback story | Clean | Each task has rollback note. |
| Simpler alternative not considered | Clean | Design rejected manual `plugin lock` discipline. |
| User-intent drift | Clean | Covers CI install, lock drift alerts, DNS migration, Blackorchid route-first migration. |
| Existence / runtime-validity | Clean | Plan requires real commands, release artifacts, GH Actions, and route probes. |
| Over-decomposition / under-decomposition | Clean | Seven PRs map to real rollback boundaries. |
| Verification-class mismatch | Clean | CLI/plugin/infrastructure/content tasks have matching verification classes. |
| Auth/authz chain composition | Clean | Admin operation evidence required; no client-asserted auth gate introduced. |
| Hidden serial dependencies | Finding | P3 recorded release/proof ordering risk; plan gates already handle it. |
| Missing rollback wiring | Clean | Rollback lines are task-local. |
| Missing integration proof | Finding | P2 fixed before PASS. |
| Infrastructure verification mismatch | Finding | P1 fixed before PASS. |
| Plugin-loader runtime layout | Clean | Plugin install tasks use normal release/install layout, not ad hoc binaries. |
| Config-validation schema rules | Clean | Plan includes `wfctl config validate`, plugin tests, and content manifest validation. |
| Identifier / naming-convention match | Clean | Commands/fields match existing `wfctl`, `.wfctl-lock.yaml`, and multisite naming. |

**Options the author may not have considered:**
1. Execute Blackorchid as manual runbook only: lower automation risk, but violates user's "proceed autonomously" and repeats bespoke-operation failure mode.
2. Merge Tasks 2 and 3 into one provider PR: fewer releases, but rollback of Cloudflare TXT behavior and infra policy injection should stay independent.
3. Add `wfctl plugin verify-lock` instead of `plugin ci`: useful as a later alias, but CI users need one command that validates and installs.

**Verdict reasoning:** PASS. Important findings were resolved in the plan before this report was committed. Remaining risks are gated by release/proof sequencing and should be enforced during scope lock and execution.
