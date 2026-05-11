### Adversarial Review Report

**Phase:** design
**Artifact:** docs/plans/2026-05-11-plugin-conformance-compat-design.md
**Status:** FAIL

**Findings (Critical):**
- None.

**Findings (Important):**
- [user-intent drift / missing design surface] The design specifies version index shape and install consumption, but not the producer/publisher path that makes registry indexes exist. First scope needs CI/index generation, `wfctl plugin conformance --format json` output schema, merge/update command or registry push path, atomic index update rules, and required plugin CI matrix wiring.
- [unstated assumptions / user-intent drift] Current engine version and released engine versions are load-bearing but undefined. The design needs engine version source of truth, release discovery, plugin CI matrix policy, stale-evidence behavior, and whether compatibility is tested per latest engine only or across a supported engine window.
- [repo-precedent conflict / schema mismatch] Proposed lockfile compatibility is plugin-level, but evidence is platform/artifact-specific. Current lockfile stores platform-specific URLs and SHA-256 under `platforms`, so compatibility should live under each platform entry or be a repeated compatibility list with `os`, `arch`, and `artifactSHA256`.
- [missing failure modes / strict-default ambiguity] Default `enforce` blocks known trusted fails, but missing evidence still installs via `minEngineVersion` with only a warning. The adoption policy must explicitly say whether first-party plugins require evidence or whether this is a named transitional mode with a sunset condition.

**Findings (Minor):**
- [missing failure modes] `--force` and warn mode both need lockfile recording. Define exact `--force` reason behavior for installs and updates.
- [schema consistency] `evidenceDigest` needs canonical bytes and digest algorithm, e.g. SHA-256 over canonical JSON for the exact evidence record.
- [security / privacy] CI trust boundaries are missing. Fork PRs should not execute arbitrary released plugin binaries while organization tokens are available.

**Bug-class scan transcript:**
| Class | Result | Note |
|---|---|---|
| Unstated assumptions | Finding | Engine version discovery, tested engine matrix breadth, stale evidence refresh, and evidence publishing are assumed. |
| Repo-precedent conflicts | Finding | Current lockfile archive metadata is platform-scoped; the design used plugin-level compatibility. |
| YAGNI violations | Clean | Hosted service, acceptance mode, and non-IaC modes remain deferred. |
| Missing failure modes | Finding | Missing/stale evidence, mixed platform evidence, force recording, evidence digest mismatch semantics, and publishing failures need sharper behavior. |
| Security / privacy | Finding | Untrusted binary execution is acknowledged, but fork/tag/secret-bearing CI boundaries are undefined. |
| Rollback story | Clean | Warn mode, env/config precedence, additive registry fields, retained scripts, and lock metadata removal are covered. |
| Simpler alternative not considered | Finding | A lockfile-first rollout with generated local evidence before registry enforcement was not evaluated. |
| User-intent drift | Finding | Consumption is designed, but automatic compatibility determination and index publishing remain underdesigned. |

**Options the author may not have considered:**
1. Evidence producer first, resolver second: ship `wfctl plugin conformance --format json` plus `wfctl registry compatibility update` before install enforcement.
2. Platform-scoped compatibility in lockfiles: store compatibility under `plugins.<name>.platforms.<os-arch>.compatibility`.
3. First-party evidence-required mode: require exact evidence for GoCodeAlone registries after an adoption marker; keep advisory fallback for community/user registries.

**Verdict reasoning:** FAIL. The design fixed earlier blockers but still lacks central producer/publisher semantics, engine matrix policy, and platform-scoped lockfile evidence. These are central to the user's compatibility automation goal.
