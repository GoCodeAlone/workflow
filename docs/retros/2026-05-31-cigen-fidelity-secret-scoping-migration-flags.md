# Retro — cigen fidelity: per-phase secret scoping + migration flags (#805)

**Date:** 2026-05-31
**PR:** GoCodeAlone/workflow#805 (merged 1afc67b8d)
**Design:** docs/plans/2026-05-31-cigen-fidelity-secret-scoping-migration-flags-design.md
**Plan:** docs/plans/2026-05-31-cigen-fidelity-secret-scoping-migration-flags.md

## What shipped

Two measured-gap fixes in `cigen`'s GHA output (follow-on to v0.67.0 smart-CI; gaps recorded in `cigen/testdata/multisite/GAP.md`):

- **#3 per-phase secret scoping.** `Analyze` loads the prereq config from `opts.PhaseConfig`, runs the existing `deriveSecrets` per config; `DeployPhase` gains `Secrets []SecretRef` + `Scoped bool`. Renderer branches each apply job's `env:` SOURCE on `phase.Scoped` (NOT `len`), union fallback when unscoped. Multisite effect: `apply-prereq` env dropped 6 deploy-only secrets incl `MULTISITE_DB_URL`; `apply-deploy` unchanged.
- **#4 migration flags.** `migrationsUpCommand` always appends `--format json`; `deriveMigrations` derives `--env` only when exactly one `ci.migrations[0].environments` key (≥2 → omit + warn). Multisite has no `environments:` → gets `--format json`, no `--env` (honest).

Single PR, no release. `wfctl ci generate` gets the fix directly; ci-generator plugin inherits on next dep bump.

## What went well

- **Existence/runtime check caught at design time** (the [[feedback_verify_artifact_exists_not_just_shape]] lesson, applied): before locking the plan, verified `runMigrationsUp` actually has `--env` + `--format text|json` flags (`cmd/wfctl/migrations.go:112-132`). Had they been fictional, the generated step would have failed at runtime — exactly the bug class the prior retro flagged. This is the first cycle where the existence-check ran proactively, not as a post-mortem.
- **Plan-phase adversarial review earned its keep.** Cycle-1 FAIL caught 3 Criticals the design review couldn't: (C1) new tests appended to the external `package cigen_test` calling unexported funcs → won't compile; (C2) missing `config` import; (C3) Task 5 regen used **fictional CLI flags** (`--stdout`, `ci plan --format`, `ci generate --output-dir`) when the real recipe was already documented in GAP.md. C3 is the same fake-artifact trap as #4's existence-check, but on the *evidence* side — a plan that "looked right" would have produced a non-runnable regen.
- **Honest evidence held.** Code review confirmed GAP.md makes no `--env prod` overclaim for multisite; the on-disk golden test (`multisite_evidence_test.go`) locks the committed artifact's behavior, not just its shape.
- **gopls false-positive correctly ignored.** Post-impl diagnostics claimed `DeployPhase has no Secrets/Scoped` — cross-checkout resolution via the parent `go.work` pointing at the *main* checkout (no fields). Authoritative `GOWORK=off go test ./cigen/...` was green. Known trap, handled.

## What was friction

- **Background monitor agent didn't loop.** The dispatched `pr-monitoring`-style agent exited while CI was still pending instead of sleeping between polls, then emitted ~5 spurious "completed" replay notifications. Worked around with a background bash poll-loop (`sleep 60` × N until 0-pending) which is deterministic and cheap. **Lesson:** for "wait until CI settles," a background bash poll-loop beats a subagent — the agent has no reliable sleep primitive and burns tokens re-checking; bash `sleep` in a background command re-invokes the lead exactly once on exit.
- **Cross-repo plan location.** The design was first committed to the workspace repo (`/Users/jon/workspace/docs/plans`), then copied into the workflow worktree so design+plan+lock+code+evidence ship in one PR. Minor duplication. For repo-specific code work, author the design in the target repo's worktree from the start.

## Follow-ups

- None blocking. The ci-generator plugin picks up the cigen fix on its next workflow-dep bump (optional; noted in plan out-of-scope).
- Latent: `--env` derivation only activates for configs that declare `ci.migrations[].environments`; none of our current deployed configs do. If a multi-env migration config appears, the single-key derivation + ambiguity warning are ready.

## Gate tally

design adversarial 2 cycles (C1 `--env` overstatement, C2 empty-slice sentinel → `Scoped bool`) + plan adversarial 2 cycles (C1/C2 test-package compile, C3 fictional CLI flags, Important overclaimed golden test) + alignment PASS (zero drift) + scope-lock + code review PASS (3 optional Minors). 20/20 CI checks green → admin-merge (Copilot down, [[feedback_copilot_down_admin_merge_green]]).
