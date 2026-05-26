### Adversarial Review Report

**Phase:** plan
**Artifact:** `docs/plans/2026-05-11-plugin-conformance-compat.md`
**Status:** FAIL

**Findings (Critical):**
- None.

**Findings (Important):**
- [repo-precedent conflicts] Task 4 placed compatibility index editing under `wfctl registry`, but current `cmd/wfctl/registry_container.go` makes that command container-registry-only and sends plugin catalog users to `wfctl plugin-registry`. Recommendation: implement compatibility updates under `wfctl plugin-registry compatibility update` and modify `cmd/wfctl/registry_cmd.go`, not `registry_container.go`.

**Findings (Minor):**
- [verification-class mismatch] Runtime validation used the stale `wfctl registry compatibility update` command. Recommendation: update validation to invoke `wfctl plugin-registry compatibility update`.
- [missing rollback wiring] Task 4 rollback referenced generated indexes but not the command-surface conflict. Recommendation: keep rollback as commit revert; no registry-container command should be introduced.

**Bug-class scan transcript:**
| Class | Result | Note |
|---|---|---|
| Unstated assumptions | Clean | Plan states artifact-first evidence and policy modes. |
| Repo-precedent conflicts | Finding | Plugin registry command surface was misidentified. |
| YAGNI violations | Clean | Out-of-scope list excludes signatures, live provider acceptance, and hosted services. |
| Missing failure modes | Finding | Runtime validation used the wrong command surface. |
| Security / privacy at architecture level | Clean | Plan keeps conformance metadata-only and excludes secrets. |
| Rollback story | Finding | Rollback needed to preserve container-registry command ownership. |
| Simpler alternative not considered | Clean | Plan rejects per-plugin scripts and manifest-only checks via the design. |
| User-intent drift | Clean | Plan targets wfctl-native plugin conformance as requested. |
| Over-decomposition / under-decomposition | Clean | Seven tasks map to coherent implementation slices. |
| Verification-class mismatch | Finding | Task 4 validation invoked the wrong command. |
| Hidden serial dependencies | Clean | Tasks are intentionally serial within one PR because later tasks depend on earlier models and resolver APIs. |
| Missing rollback wiring | Finding | Task 4 rollback needed command ownership guardrail. |

**Options the author may not have considered:**
1. Add a deprecated alias under `wfctl registry`: this would preserve the design's original spelling, but fights the recent container-registry split and risks more user confusion.
2. Put index updates under `wfctl plugin compatibility update`: this keeps plugin-related commands together, but the command mutates registry files rather than a local plugin install.

**Verdict reasoning:** FAIL because implementing under the wrong CLI surface would conflict with existing wfctl command ownership and user-facing help.
