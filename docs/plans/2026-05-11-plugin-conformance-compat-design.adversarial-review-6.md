### Adversarial Review Report

**Phase:** design
**Artifact:** `docs/plans/2026-05-11-plugin-conformance-compat-design.md`
**Status:** PASS

**Findings (Critical):**
- None.

**Findings (Important):**
- None.

**Findings (Minor):**
- [missing failure modes] `--engine-version local` is named in failure handling but not fully specified as a literal accepted value. Recommendation: plan should either implement `local` as a documented advisory sentinel or remove that wording during implementation.
- [YAGNI] Cross-source compatibility index pointers are allowed when trusted, but first-scope implementation may not need them. Recommendation: plan should keep cross-source pointers out of first task unless existing manifest fields already require them.
- [repo-precedent conflicts] The design adds a global config file path while some current plugin-lock tests intentionally avoid home/default registry lookup. Recommendation: plan should isolate user-global config reads to install/update flows or add explicit tests for lock behavior.

**Bug-class scan transcript:**
| Class | Result | Note |
|---|---|---|
| Unstated assumptions | Clean | Artifact-first evidence, pseudo-version override, trust source, and policy ownership are now explicit. |
| Repo-precedent conflicts | Finding | Global config behavior needs careful tests because lock code currently avoids some home/default registry lookups. |
| YAGNI violations | Finding | Cross-source index pointers are future-facing and should not expand first implementation unless already present. |
| Missing failure modes | Finding | Engine sentinel wording needs exact implementation semantics. |
| Security / privacy at architecture level | Clean | Conformance uses local metadata calls only, avoids provider credentials, and treats untrusted evidence as advisory. |
| Rollback story | Clean | Warn mode, additive metadata, and plugin-local script retention provide rollback. |
| Simpler alternative not considered | Clean | Manifest-only and per-plugin scripts are explicitly considered and rejected. |
| User-intent drift | Clean | Design targets wfctl/plugin compatibility, version selection, and reusable plugin CI conformance as requested. |

**Options the author may not have considered:**
1. Implement conformance evidence and registry update first, leaving install enforcement warn-only for one release. This reduces breakage risk but delays the install-time protection the user asked for.
2. Make compatibility index fetch best-effort until registries publish indexes for all first-party plugins. This smooths rollout but weakens the "required from engine" semantics.

**Verdict reasoning:** PASS. Remaining findings are implementation-plan guardrails, not design blockers. The artifact-first evidence model, trust boundaries, config precedence, resolver behavior, rollback path, and test surface are specific enough for planning.
