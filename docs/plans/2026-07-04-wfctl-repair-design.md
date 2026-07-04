# wfctl Repair Design

## Global Design Guidance

Source: `AGENTS.md`, `docs/AGENT_GUIDE.md`, `docs/REPO_LAYOUT.md`, `decisions/0050-plugin-lock-install-modes.md`, `decisions/0052-top-level-wfctl-doctor.md`, workspace `docs/PORTFOLIO.md`.

| guidance | design response |
|---|---|
| `wfctl` owns lifecycle orchestration; plugins own provider-specific behavior | `repair` only runs `plugin lock` and `plugin install`; no provider-specific fixes. |
| Update docs/tests with CLI behavior | Add `cmd/wfctl/*_test.go`, README, `docs/WFCTL.md`, embedded `wfctl.yaml`. |
| Use `GOWORK=off` | Verification commands use `GOWORK=off`. |
| Reuse existing tooling inventory | Existing `wfctl doctor`, `plugin lock`, `plugin install`, `plugin ci`, and `update` cover needed capabilities; no new resolver. |

## Goal

Add `wfctl repair`: a guarded, dry-run-by-default lifecycle repair command that turns actionable `doctor` plugin diagnostics into ordered existing commands.

## Approaches

| option | summary | trade-off |
|---|---|---|
| A recommended | Top-level `repair`, dry-run default, `--apply` delegates to `plugin lock`/`plugin install` | Clear UX, low mutation surface, reuses tested paths. |
| B | Add `doctor --repair` | Overloads read-only command promised by ADR 0052. |
| C | Add full repair framework with typed fix IDs | More extensible but premature; larger public contract. |

## Architecture

- New `cmd/wfctl/repair.go`.
- Flags mirror doctor project/plugin paths: `--workflow`, `--manifest`, `--lock-file`, `--plugin-dir`, `--include-global`, `--online`, `--format`.
- Mutation requires `--apply`; dry-run prints planned commands.
- Planner reads the `doctorReport` plus manifest/lock/install state and emits ordered actions:
  1. `plugin lock` when manifest exists and lockfile missing/stale/incomplete.
  2. `plugin install` when a project plugin is missing/stale or after relock.
  3. Suggest-only items for binary update, workflow creation/parse failures, missing manifest, global plugin state.
- Executor calls injected function hooks in tests; production hooks call existing `runPluginLock` and `runPluginInstall`.

## Assumptions

| id | assumption | challenge | fallback |
|---|---|---|---|
| A1 | Existing `plugin lock`/`plugin install` are correct mutation authority | If they are wrong, repair repeats the bug | Keep repair as orchestration only; fix authoritative commands separately. |
| A2 | Reinstalling after relock is safe/idempotent | Registry/network failure may leave lock updated but install incomplete | Ordered output shows failure; rerun `wfctl repair --apply`. |
| A3 | Default dry-run avoids surprising file/network mutation | Users may expect repair to mutate by default | Docs and usage show `--apply` explicitly. |

## Self-Challenge

1. Laziest solution: docs-only "run doctor then fixes." Rejected: user asked to address repair follow-up and repeated lifecycle drift now justifies a command.
2. Fragile assumption: relock+install idempotency. Mitigation: delegate to package-manager-like install semantics from ADR 0050 and do not delete existing plugins first.
3. YAGNI sweep: no typed repair registry, no plugin/provider repair API, no binary self-update, no global mutation.

## Security Review

- No new secrets/auth.
- `--apply` may download plugins through existing checksum/compatibility enforcement; repair does not add `--skip-checksum`.
- Dry-run default limits accidental supply-chain/network mutation.
- Online release check remains opt-in via `--online`.

## Infrastructure Impact

- No cloud resources, migrations, services, or deployment config.
- Local filesystem writes only when `--apply`: `.wfctl-lock.yaml` and `data/plugins`.
- Rollback: revert command changes; user can restore prior lockfile/plugins from VCS/cache or rerun prior pinned install.

## Multi-Component Validation

- Unit tests cover planner and apply ordering with injected runners.
- CLI smoke proves `wfctl repair --help` loads through embedded command wiring.
- Dry-run smoke against a temp project verifies no file mutation.
- Package test verifies doctor/repair/plugin lifecycle command compatibility.

## Out Of Scope

- Provider-specific repair.
- Binary self-update.
- Creating new workflow projects or plugin manifests.
- Global plugin mutation.
- Changing plugin resolution/install semantics.
