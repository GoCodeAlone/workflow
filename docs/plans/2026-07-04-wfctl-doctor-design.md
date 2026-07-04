---
status: approved
area: wfctl
owner: workflow
implementation_refs: []
external_refs:
  - "user: evaluate wfctl lifecycle consolidation after install docs"
verification:
  last_checked: 2026-07-04
  commands:
    - GOWORK=off go test ./cmd/wfctl
  result: pass
supersedes: []
superseded_by: []
---

# wfctl Doctor Design

## Goal

Add a read-only lifecycle diagnostic command that tells operators whether their
`wfctl` binary, project config, plugin manifest, lockfile, and installed plugin
state are healthy, and prints the next command needed to repair each issue.

## Global Design Guidance

Source: `AGENTS.md`, `docs/AGENT_GUIDE.md`, `docs/REPO_LAYOUT.md`,
`docs/plans/2026-04-25-workflow-ecosystem-audit-design.md`,
workspace `docs/PORTFOLIO.md`.

| guidance | design response |
|---|---|
| `wfctl` is portable lifecycle tooling | Add one lifecycle entry point that explains existing commands. |
| Plugin-first boundary | Diagnose plugin state; do not implement provider/plugin behavior in core. |
| Use clean worktrees + `GOWORK=off` | Work occurs in `.worktrees/wfctl-lifecycle-evaluation`; plan verification uses `GOWORK=off`. |
| Update docs/tests for CLI changes | Add command tests and `docs/WFCTL.md` section. |
| Portfolio has registry/plugins/setup-wfctl already | Reuse existing plugin registry/install/update commands; no duplicate installer or plugin manager. |

## Current State

- `wfctl` has many mature subcommands: `validate`, `config validate`,
  `plugin lock/install/ci/update/list/info/deps`, `update`, `capability check`,
  `audit repo`, `infra plan/apply/status`, `secrets`, `env`, `ci`.
- Docs now explain install/update/plugin lifecycle, but the CLI does not answer
  "what is wrong in this checkout?" in one place.
- Existing lock provenance (`config.ValidateWfctlLockfileProvenance`) and
  installed-plugin verification helpers make a read-only diagnostic cheap.

## Approaches

| option | summary | trade-off |
|---|---|---|
| A | Top-level `wfctl doctor` | Recommended. Covers binary + project + plugin state; read-only; discoverable. |
| B | `wfctl plugin status` only | Smaller but misses binary/version/project readiness. |
| C | Docs/templates only | No behavior risk, but leaves operators manually composing checks. |

## Design

Add `wfctl doctor [options]`.

Flags:
- `--workflow workflow.yaml`: project workflow config to parse when present.
- `--manifest wfctl.yaml`: project plugin manifest.
- `--lock-file .wfctl-lock.yaml`: generated plugin lockfile.
- `--plugin-dir data/plugins`: project plugin install directory.
- `--include-global`: include global plugin directory summary.
- `--online`: check latest GitHub release; default is offline.
- `--format text|json`: human output by default, structured output for CI.
- `--strict`: exit non-zero when warnings or errors exist.

Checks:
- Binary: current version, executable path, dev-build warning, optional latest
  release check.
- Project config: workflow file missing/parse status; suggest `wfctl init`,
  `wfctl wizard`, or `wfctl validate`.
- Plugin manifest/lock: load `wfctl.yaml`; if plugins are declared, require
  `.wfctl-lock.yaml`; validate provenance; warn on stale/mismatched lock.
- Installed plugins: for each locked plugin, verify conventional install path
  exists and installed version matches lock.
- Global plugins: optional summary only; project lock remains authoritative.

Output is a sectioned report with status `OK|WARN|ERROR`, a short message, and
optional `Fix:` command. JSON emits the same structure for tooling.

## Security Review

- No secrets are read; auth env var names from manifests are not expanded.
- No network access unless `--online` is provided.
- The command does not install, update, delete, or rewrite files.
- Paths are user-supplied local paths; diagnostics should report paths but not
  file contents.
- JSON output must not include plugin binary stdout/stderr; it only reports
  manifest and filesystem status.

## Infrastructure Impact

None. No cloud resources, migrations, queues, IAM, secrets, or deployments.

## Multi-Component Validation

- CLI command proof: `GOWORK=off go run ./cmd/wfctl doctor --help`.
- Project/plugin boundary proof: tests create `wfctl.yaml`, `.wfctl-lock.yaml`,
  and installed plugin layout, then run the real doctor command function.
- Optional online update check reuses existing release-fetcher tests/patterns.

## Assumptions

| id | assumption | challenge | fallback |
|---|---|---|---|
| A1 | Lockfile provenance is authoritative for manifest freshness | legacy lockfiles may lack provenance | report warning with `wfctl plugin lock` fix, not error. |
| A2 | Conventional plugin install layout is enough for status | some direct/local installs may lack metadata | verify manifest/version where possible; otherwise warn. |
| A3 | Top-level command is more discoverable than plugin-only status | command list is already large | keep help terse and make command read-only. |
| A4 | Offline default avoids surprising users | users may expect update freshness | add explicit `--online` and fix text pointing to `wfctl update --check`. |

## Self-Challenge

| doubt | answer |
|---|---|
| Could this be just docs? | Docs were fixed; the remaining gap is checkout-specific diagnosis. |
| Is `doctor` too broad? | First slice is read-only and limited to existing lifecycle surfaces. |
| Does this duplicate `validate`? | It does not validate full workflow behavior; it points to `validate` when needed. |

## Rollback

Revert the `doctor` command wiring, tests, and docs. No data migration or
runtime state change exists. Existing lifecycle commands continue unchanged.

## Out of Scope

- Auto-repair mode.
- Provider-specific diagnostics.
- Plugin registry mutation.
- Generated website docs and release automation for this first implementation
  PR unless the final Workflow release process triggers them later.
