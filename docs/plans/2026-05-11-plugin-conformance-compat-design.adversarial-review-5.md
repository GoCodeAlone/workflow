### Adversarial Review Report

**Phase:** design
**Artifact:** `docs/plans/2026-05-11-plugin-conformance-compat-design.md`
**Status:** FAIL

**Findings (Critical):**
- None.

**Findings (Important):**
- [unstated assumptions] Command syntax only showed `<plugin-dir>` even though authoritative evidence requires `--artifact`. Recommendation: document `--artifact` as an alternate required input and require exactly one source.
- [YAGNI] The first-scope evidence schema still included `signature: null` while signed third-party trust was deferred. Recommendation: remove the field until signature envelope/key semantics are designed.
- [repo-precedent conflicts] Enforcement controls said `Registry/project config fallback`, conflicting with the design's statement that registry config owns trust only. Recommendation: make enforcement precedence CLI > env > project/global config > default.
- [missing failure modes] Compatibility index unavailability said installs should warn, contradicting required first-party evidence behavior. Recommendation: route unavailable index behavior through evidence policy and compatibility mode.

**Findings (Minor):**
- [missing failure modes] Resolver engine override was mentioned but not assigned to install/update/lock command flags. Recommendation: state ownership for `--engine-version` and `WFCTL_ENGINE_VERSION`.

**Bug-class scan transcript:**
| Class | Result | Note |
|---|---|---|
| Unstated assumptions | Finding | Artifact mode was required for authoritative evidence but not encoded in command syntax. |
| Repo-precedent conflicts | Finding | Registry trust and user enforcement mode were still partly conflated. |
| YAGNI violations | Finding | Signature placeholder leaked into first-scope schema without a signing design. |
| Missing failure modes | Finding | Required evidence and index-unavailable behavior conflicted. |
| Security / privacy at architecture level | Clean | Secret handling remains limited to release/registry jobs and no provider credentials enter conformance. |
| Rollback story | Clean | Warn mode and additive metadata provide rollback. |
| Simpler alternative not considered | Clean | Manifest-only compatibility remains insufficient for strict host/plugin drift. |
| User-intent drift | Clean | The design continues to target scalable plugin compatibility for wfctl. |

**Options the author may not have considered:**
1. Make `--artifact` mandatory always: stronger evidence, but worse local developer ergonomics; local advisory mode is acceptable if clearly marked non-authoritative.
2. Remove install-time enforcement from first scope: lowers risk, but misses the user goal of letting `wfctl` choose compatible plugin versions.

**Verdict reasoning:** FAIL because command input, schema scope, config ownership, and unavailable-index behavior must be unambiguous before planning.
