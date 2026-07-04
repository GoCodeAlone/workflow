### Adversarial Review Report

**Phase:** design
**Artifact:** `docs/plans/2026-07-04-wfctl-doctor-design.md`
**Status:** PASS

**Findings (Critical):**
- None.

**Findings (Important):**
- None.

**Findings (Minor):**
- `D1` [YAGNI] `docs/plans/2026-07-04-wfctl-doctor-design.md:66-69`: `--include-global`, `--online`, `--format`, and `--strict` make the first command broader than the smallest possible diagnostic. Recommendation: keep because each maps to a concrete lifecycle question, but resist adding auto-repair in this PR.
- `D2` [Repo-precedent conflicts] `docs/plans/2026-07-04-wfctl-doctor-design.md:78-80`: `plugin info` currently uses the raw plugin argument for filesystem lookup while other paths normalize names. Recommendation: implement doctor using `normalizePluginName` and document that it intentionally follows install/list/remove layout.
- `D3` [Multi-component validation] `docs/plans/2026-07-04-wfctl-doctor-design.md:101-104`: validation uses command-function tests rather than a full built-binary release artifact. Recommendation: acceptable for a CLI report command, but final smoke must run `go run ./cmd/wfctl doctor`.

**Bug-class scan transcript:**
| Class | Result | Note |
|---|---|---|
| Project-guidance conflicts | Clean | Matches `docs/AGENT_GUIDE.md:40-48` for new wfctl commands and `docs/AGENT_GUIDE.md:50-57` plugin boundary. |
| Assumptions under attack | Clean | A1-A4 list failure modes and conservative fallbacks. |
| Repo-precedent conflicts | Minor | Plugin path normalization needs explicit care; existing install/remove code uses normalized paths. |
| Artifact-class precedent | Clean | Existing wfctl commands live under `cmd/wfctl`, route through `main.go` + `wfctl.yaml`, and update `docs/WFCTL.md`. |
| YAGNI violations | Minor | Multiple flags are justified; auto-repair is excluded. |
| Missing failure modes | Clean | Missing files, stale lock, legacy provenance, offline default, and direct/local install uncertainty are covered. |
| Security / privacy at architecture level | Clean | No secret expansion, no mutation, no network unless `--online`. |
| Infrastructure impact | Clean | Design creates no infra, IAM, storage, migrations, or deployment resources. |
| Multi-component validation | Minor | Real CLI smoke is planned; no external service boundary is introduced. |
| Declared integration proof | Clean | Existing integration surfaces are config-only checks; no new plugin/runtime integration is declared. |
| Contributed UI rendering proof | Clean | No UI contribution. |
| Rollback story | Clean | Revert-only rollback is sufficient for read-only CLI command. |
| Simpler alternative not considered | Clean | Docs-only and plugin-only alternatives are compared. |
| User-intent drift | Clean | Directly answers request to evaluate, consolidate, extend, and improve full lifecycle use. |
| Existence / runtime-validity | Clean | Consumed artifacts exist in repo: `cmd/wfctl/main.go`, `cmd/wfctl/wfctl.yaml`, `config/wfctl_lockfile.go`, `cmd/wfctl/plugin_install.go`. |

**Options the author may not have considered:**
1. `wfctl plugin status`: lower blast radius, but misses binary/project readiness and forces users to know the issue is plugin-related.
2. `wfctl audit lifecycle`: fits the existing audit namespace, but "audit" implies policy/compliance rather than day-to-day repair guidance.

**Verdict reasoning:** PASS. The design picks a narrow read-only consolidation point, respects plugin ownership, and avoids hidden mutation. Minor risks are implementation discipline issues, not design blockers.
