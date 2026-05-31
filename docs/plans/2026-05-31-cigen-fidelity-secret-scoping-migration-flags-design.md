# cigen fidelity: per-phase secret scoping + migration operational flags — design

**Date:** 2026-05-31
**Author:** autonomous pipeline (codingsloth@pm.me approved)
**Status:** Design — adversarial review PASS (2 cycles, 2026-05-31)
**Repo:** `workflow` (the `cigen` package + GHA renderer). Follow-on to v0.67.0; closes 2 gaps measured in the multisite regen evidence (retro 2026-05-30).

## 1. Problem

Two fidelity gaps in `cigen`'s GitHub Actions output vs the hand-tuned `gocodealone-multisite/infra.yml` (both documented in `cigen/testdata/multisite/GAP.md`):

- **#3 over-broad secret scoping.** `cigen.Analyze` builds ONE global `Secrets` union (from the primary config only) and `render_gha` writes that union into **every** apply job's `env:`. In a two-phase deploy, `apply-prereq` thus receives secrets it never uses (e.g. `MULTISITE_DB_URL`), widening blast radius. The hand-written workflow scopes each job to its own secrets.
- **#4 bare migration step.** `cigen` emits `wfctl migrations up --config <cfg>`; the real workflow uses `wfctl migrations up --config deploy.yaml --env prod --format json`. `--format json` (machine-readable output) and `--env <env>` are missing. (`MigrationsSpec.Env` field already exists from PR4 but is never populated, and `--format json` is never emitted.)

## 2. Goal

`cigen` GHA output: (a) each apply job's `env:` carries only the secrets that phase's config references (+ the migration DB secret only on the migrating phase); (b) the migrations step emits `--format json` always and `--env <env>` when an environment is unambiguously derivable. `wfctl ci generate` benefits immediately; the ci-generator plugin picks it up on its next workflow-dep bump (noted, not required).

## 3. Non-goals

- Per-phase scoping for **single-phase** deploys (one apply job → the union IS its scope; no change).
- Deriving `--env` when ambiguous (multiple `ci.migrations[0].environments` keys) — omit + warn rather than guess.
- Scoping when the phase-config is given only as a **logical alias** (MCP `--phase-config-alias`, no real file to load) — fall back to the union + a warning.
- A plugin release/bump (separate optional follow-on; `wfctl ci generate` gets the fix directly).
- Touching GitLab/Jenkins/CircleCI renderers (GHA only — that's where phases + the measured gap live; GitLab uses project-level CI vars so per-job scoping is N/A).

## 4. Architecture

### #3 — per-phase secret scoping

- **Model:** add to `DeployPhase`: `Secrets []SecretRef` (the secrets that phase's apply job needs) **and `Scoped bool`** (true iff per-phase derivation actually ran on a real loaded config). Keep `CIPlan.Secrets` (union) for single-phase, JSON consumers, and the unscoped fallback. **The renderer branches on `Scoped`, NOT on `len(Secrets)>0`** — so a genuinely zero-secret phase (Scoped=true, empty Secrets) emits an empty `env:`, while an unloadable phase (Scoped=false) falls back to the union. (Resolves adversarial C2: empty-slice must not mean "fall back".)
- **Phase config source (resolves I1):** the prereq config is loaded from **`opts.PhaseConfig`** (a real filesystem path), NOT from `configs[1:]` — `Analyze`'s `configs []string` is single-element in all current call paths (only `configs[0]` is read). Load it via `config.LoadFromFile(opts.PhaseConfig)`.
- **Analyze:** when `opts.PhaseConfig` is a real, loadable file (not alias-only, file exists+parses): load it, run existing `deriveSecrets` on it → prereq phase `{Secrets, Scoped:true}`; the primary config's secrets → the deploy (last) phase `{Secrets, Scoped:true}`. `derivePhases` order unchanged (prereq-first, deploy/primary-last).
- **Migrating phase = the LAST phase (resolves I2).** This matches `render_gha`'s existing `isLastPhase` step placement. `deriveMigrations` is called ONLY on the primary config (unchanged); the migration DB secret (`Migrations.DBEnv`) is added to the **last** phase's `Secrets`, never the prereq's. (If a future design needs migrations in a non-last phase, that's a separate change — documented as a constraint, not silently assumed.)
- **Fallback (Scoped=false):** single-phase, OR `opts.PhaseConfig` alias-only/missing/unparseable → phases keep `Scoped:false`; renderer uses `CIPlan.Secrets` union (current behavior). Emit a `Warnings[]` note only for the alias-only/unloadable *multi-phase* case ("per-phase secret scoping unavailable: phase config not loadable; using union"). Single-phase needs no warning (union IS its scope).
- **render_gha (`writeApplyJob`):** `env:` = `phase.Secrets` when `phase.Scoped`, else `CIPlan.Secrets`.

### #4 — migration operational flags

- **`--format json`:** appended to the generated `wfctl migrations up` step. **Operator explicitly requested this** (match the deployed multisite workflow). Tradeoff acknowledged (resolves I3): the generated step does not pipe the JSON downstream, so a failed migration logs a JSON blob rather than text — but it matches the real workflow, exit-code on failure is unchanged (`runMigrationsUp` still errors non-zero), and an operator can edit it out. Kept as requested; noted in GAP.md as a deliberate match-the-deployed-pattern choice.
- **`--env <env>`:** populate `MigrationsSpec.Env` in `deriveMigrations` from `ci.migrations[0].environments` — **only when exactly one** key exists (unambiguous). Zero keys → `Env` empty, no `--env` (this is the multisite case — see C1). ≥2 keys → `Env` empty + a `Warnings[]` note ("migrations environment ambiguous (N declared); --env omitted — set it in the workflow"). Render already emits `--env <Env>` iff `Env != ""`.

### Evidence refresh (HONEST — resolves adversarial C1)

The multisite `deploy.yaml` `ci.migrations` block declares **NO `environments:`** key → `--env` is NOT derivable for multisite; only `--format json` lands. So in `GAP.md`:
- **Move to "now derivable / matched":** per-phase secret scoping (apply-prereq no longer carries the deploy-only DB secret) + `--format json` on the migrations step.
- **KEEP in "not derivable" (corrected, not removed):** the migrations `--env <env>` — requires an `environments:` declaration in `ci.migrations` that multisite does not have (also the hash-suffixed DB secret name, GHCR image-wait, GHCR creds, GA4 step, smoke matrix, concurrency, SHA-pins).
Regenerate `cigen/testdata/multisite/{plan.json,generated-infra.yml}` with the real binary; the measured diff must show apply-prereq's `env:` shrunk (no DB secret) + the migrations step with `--format json` (and NO `--env`, honestly, since multisite declares no environments). The claim is exactly the measured diff — never "matches the hand-tuned `--env prod`".

## 5. Data flow

`Analyze(primary, opts{PhaseConfig})` → loads primary (+ phase-config if real) → `deriveSecrets` per config → `DeployPhase.Secrets` per phase + `CIPlan.Secrets` union + `Migrations{DBEnv,Env}` → `RenderGitHubActions` emits per-apply-job `env:` (phase scope, union fallback) + migrations step `wfctl migrations up --config <cfg> [--env <env>] --format json`.

## 6. Error handling / partial failure

- Phase-config path set but file missing/unparseable → do NOT fail; warn + fall back to union scoping (the path may be a future-checkout-relative or alias path). Log to `Warnings[]`.
- `ci.migrations[0].environments` ambiguous (≥2) → omit `--env`, warn.
- Single phase → union (unchanged); no warning (it's correct).
- A secret in the migrating phase that's ALSO in prereq (shared cred, e.g. SPACES_*) → appears in both jobs' env legitimately (each phase needs it). Per-phase scoping is per-config-reference, so shared creds correctly appear in both.

## 7. Testing

- Unit (golden): a two-phase fixture (primary + prereq config) → assert prereq phase `Secrets` excludes the deploy-only secret (e.g. DB url) and the deploy phase includes it; assert `render_gha` apply-prereq `env:` lacks it and apply-deploy `env:` has it. Single-phase fixture → union unchanged.
- Migrations: fixture with `ci.migrations.environments: {prod: …}` → `--env prod --format json`; fixture with 2 envs → `--format json` only + warning; fixture with 0 → `--format json` only.
- Alias-only phase-config → union fallback + warning (no load).
- Renderer output parses as YAML (existing parse-back assertion).
- **Runtime / demonstration-fidelity:** real `wfctl ci generate` on the committed multisite testdata → regenerate evidence; the real diff must show apply-prereq no longer carrying the DB secret and the migrations step carrying `--format json` (+ `--env` iff multisite declares a single env). Commit the literal output.

## Global Design Guidance

Source: `docs/design-guidance.md`.

| guidance | response |
|---|---|
| Dogfood; wfctl is primary surface | Improves `wfctl ci generate` directly |
| Reuse over rebuild | Reuses `deriveSecrets`/`MigrationsSpec.Env`/render scaffolding; additive `DeployPhase.Secrets` |
| Secrets never logged | Scoping reduces secret exposure in generated CI (names only, as before; tighter blast radius) |
| Multi-component validation; real-consumer proof | Multisite real-config regen + measured diff (demonstration-fidelity) |

## Security Review

Net **reduction** in secret exposure: per-phase scoping removes secrets from jobs that don't use them (smaller blast radius if a job/log is compromised). Still names-only in manifests; no values. `--format json` changes only output format (no secret in output). No new trust boundary.

## Infrastructure Impact

None — generates YAML. No cloud resources, no release (the cigen change ships in `wfctl ci generate`; plugin bump is a separate optional follow-on). The committed multisite evidence is a testdata artifact; the live `infra.yml` is untouched (ADR 0004 stands).

## Multi-Component Validation

Real `wfctl ci generate` against the committed multisite configs (primary + prereq) — exercises Analyze (two-config load) → render → measured diff. Golden tests assert the per-phase env split + migration flags.

## Assumptions

1. `config.LoadFromFile` can load the phase-config (deploy.prereq.yaml) the same way as the primary. *(It's the same config shape; verify in impl.)*
2. The migrating phase = the **LAST** phase (matches `render_gha`'s `isLastPhase` step placement; `deriveMigrations` runs only on the primary/deploy config — prereq provisions DB, deploy/last runs migrations). *(See §4 #3.)*
3. `ci.migrations[0].environments` keys are the deploy env names; a single key is the unambiguous `--env`. *(Matches the config schema. NOTE: multisite declares NO `environments:` → `--env` NOT derivable for multisite — see C1/§4 Evidence refresh.)*
4. Per-phase scoping only matters for multi-phase; single-phase union is already correct. *(By construction.)*

## Rollback

Runtime-affecting class: generator output (CI workflow content). Rollback = revert the cigen PR; `wfctl ci generate` reverts to the union-scoping + bare-migrations output. Additive `DeployPhase.Secrets` field (JSON omitempty) — old plan.json consumers unaffected. No release, no migration, no state.

## Self-challenge — top doubts

1. **Is per-phase scoping worth the Analyze two-config-load complexity?** It's the operator-requested fidelity fix + a real (if modest) blast-radius reduction; bounded (load + existing deriveSecrets per config, union fallback). Worth it.
2. **`--env` derivation from `ci.migrations.environments` may not match the operator's intent** (they might run migrations against a different env than declared). Mitigated: only derive when exactly one env declared; warn + omit otherwise — never guess.
3. **Alias-only phase-config (MCP path) can't be scoped.** Falls back to the union (today's behavior) + a warning — no regression, just no improvement in that path.
