### Adversarial Review Report

**Phase:** design
**Artifact:** docs/plans/2026-05-11-plugin-conformance-compat-design.md
**Status:** FAIL

**Findings (Critical):**
- None.

**Findings (Important):**
- [missing failure modes / repo-precedent conflict] Local plugin conformance assumed `ExternalPluginManager` could launch arbitrary plugin dirs, but it only loads installed-layout plugins.
- [missing failure modes / repo-precedent conflict] `--timeout` was promised, but `ExternalPluginManager.LoadPlugin` has no context/deadline path around handshake and dispense.
- [security / unstated assumptions] “Trusted registry source” was load-bearing but undefined.
- [user-intent drift / missing design surface] Install/update/lock resolution was promised, but update and lock algorithms were not designed.
- [rollback gap] Warn-mode rollback was mentioned in the ADR but not wired into config/CLI/env precedence.

**Findings (Minor):**
- [schema consistency] Version grammar mixed `0.51.2` and `v0.51.2`.
- [missing failure modes] Multi-registry/name-normalization behavior for version indexes was underspecified.

**Bug-class scan transcript:**
| Class | Result | Note |
|---|---|---|
| Unstated assumptions | Finding | Trust policy, build/staging behavior, engine discovery, and timeout launch were assumed. |
| Repo-precedent conflicts | Finding | Current external manager and registry/lock code are single-layout/single-manifest. |
| YAGNI violations | Clean | Deferred hosted service and acceptance modes stayed future work. |
| Missing failure modes | Finding | Hung handshake, staging, lock/update drift, index mismatch, and version grammar needed more detail. |
| Security / privacy | Finding | Evidence trust needed concrete identity rules. |
| Rollback story | Finding | Warn-mode rollback needed explicit wiring. |
| Simpler alternative not considered | Finding | A conformance+lock-first rollout was not evaluated. |
| User-intent drift | Finding | Update/lock sorting was promised but not designed. |

**Verdict reasoning:** FAIL. The final revision must define conformance staging/timeout, registry trust, install/update/lock algorithms, warning-mode rollback, version normalization, and same-source registry/index resolution.
