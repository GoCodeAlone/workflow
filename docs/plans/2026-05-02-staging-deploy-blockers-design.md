# Staging deploy blockers — design

**Status:** Approved 2026-05-02 (user direction: "1. solution A; 2. prefer upstream fixes; 3. yes, iterate"). Cross-repo, multi-PR, autonomous execution.

## Goal

Get core-dump's deploy chain from "passes plan + align" through to "staging app responds to /healthz" by fixing the three known upstream blockers (and any iteration follow-ups), entirely on wfctl + workflow-plugin-{digitalocean,migrations} without doctl/openssl/gh-secret fallbacks. Each upstream fix benefits every other consumer (BMW + future projects) that hits the same gotcha, not just core-dump.

## Background — current deploy state (post 2026-05-02 chain)

Last Deploy run (25247133009) on main `7f8943f9`:
- ✅ build-image: success
- ✅ Plan: clean
- ✅ Align: R-A1..R-A9 all pass (R-A9 silent on canonical schema)
- ❌ Security-check: R4 FAIL on `env_vars["NATS_AUTH_TOKEN"]: potential secret literal`
- ⏸️ Apply: never reached
- ⏸️ Migrate: never reached (would hit core-dump#150 + workflow#513)
- ⏸️ App Platform deploy: never reached

## Three known blockers + iteration

### Blocker 1 — core-dump#154 — R4 NATS_AUTH_TOKEN env-var eager-resolution (Solution A)

**Root cause:** `cmd/wfctl/infra.go` (lines 255, 301, 346, 829) calls `config.ExpandEnvInMap(m.Config)` during `parseInfraResourceSpecs`. That walks the WHOLE config tree (including any `env_vars` submap) and substitutes `${VAR}` → resolved value at parse time. The resolved spec gets serialized into `plan.json`; security-check rule R4 reads plan.json and sees a 64-char hex string in `env_vars["NATS_AUTH_TOKEN"]` (matches `r4SecretKeyName` AND `r4GenericSecret`); R4's `${`-skip never fires because the string was already substituted.

**Fix shape (Solution A — preserve `${VAR}` in plan output for env_vars submaps):**

Modify the env-var expansion path in wfctl so that `env_vars`, `env_vars_secret`, and `secret_env_vars` submaps preserve their `${VAR}` literals through plan serialization. Apply-time injection (which already happens via `injectSecrets` per the existing comment at infra_env_resolve.go:88) resolves them when the plugin actually creates/updates the resource.

Three implementation paths considered:

(i) **Add a "preserve" walker variant.** Replace the four `config.ExpandEnvInMap(m.Config)` call sites in infra.go with a new `config.ExpandEnvInMapPreservingEnvVars(m.Config, []string{"env_vars", "env_vars_secret", "secret_env_vars"})` that recursively walks the map but skips substitution inside specifically-named submaps. Lowest blast radius; explicit list of preserved keys makes the behavior obvious.

(ii) **Eager resolve everywhere except plan path.** Remove env-var expansion from infra.go parse; do it at apply-time only (per-driver). Higher blast radius — every plugin's apply path now needs to expand env vars. Touches every IaC plugin.

(iii) **R4-side narrowing instead.** Make R4 secret-aware: skip if `env_vars[K]` value matches `secrets.generate[].key+suffix` or `secrets.requires[].key`. Doesn't address Solution A's spirit (preserve `${VAR}` in plan output) but solves the practical issue.

**Picked: (i).** Smallest change to current behavior; explicit + reviewable; doesn't perturb apply-side env handling. Other rules that want to inspect `${VAR}` references benefit too. (iii) was the runner-up and stays as a fallback option if (i) hits surprises.

**Files (in workflow repo):**
- `config/expand_env.go` (or wherever ExpandEnvInMap lives; verify location) — add new variant
- `cmd/wfctl/infra.go:255,301,346,829` — switch the four call sites to the new variant
- Tests: new env-var-preservation cases in `cmd/wfctl/infra_*_env_test.go` + the new `config/expand_env_test.go`

**Verification:** integration test that calls `wfctl infra plan -c testdata/.../with-env-vars.yaml` against a fixture with `env_vars: { TOKEN: "${TOKEN}" }`, asserts the resulting plan.json has `env_vars.TOKEN == "${TOKEN}"` (literal preserved). Plus end-to-end: re-run core-dump deploy after wfctl pin bump; security-check R4 should not fire on NATS_AUTH_TOKEN.

### Blocker 2 — core-dump#150 — `--up-if-clean` flag missing on `up` subcommand (upstream fix in workflow-plugin-migrations)

**Root cause:** workflow-plugin-migrations v0.3.6's `pkg/cli/root.go:306` defines `--up-if-clean` ONLY on the `repair-dirty` subcommand (verified by `gh api …root.go?ref=v0.3.6`). core-dump's `Dockerfile.migrate` CMD invokes `workflow-migrate up --driver atlas --source-dir /migrations --up-if-clean`, which fails because cobra rejects the unknown flag for `up`.

**Fix shape (upstream + downstream):**

Add `--up-if-clean` to the `up` subcommand in workflow-plugin-migrations. Semantics: when set, `up` becomes idempotent — if the database is at the latest migration version, exit 0 quietly instead of trying to apply pending migrations (which would be a no-op anyway, but explicit). When migrations ARE pending, behave like normal `up`. This makes Dockerfile CMDs deploy-safe (re-running with no pending migrations doesn't fail).

Cut workflow-plugin-migrations v0.3.7 with the new flag + updated atlas behavior (Blocker 3 below).

**Files (in workflow-plugin-migrations repo):**
- `pkg/cli/root.go` — register `--up-if-clean` flag on the `up` subcommand alongside `repair-dirty`; thread through to the up handler
- `internal/atlas/driver.go` (Up function) — read the `UpIfClean` arg + exit-0-on-no-pending behavior
- `internal/golangmigrate/driver.go` (Up function) — same
- `pkg/cli/root_test.go` — tests for both the flag presence on `up` and the no-pending exit-0 behavior
- `CHANGELOG.md` — entry under Unreleased

**Files (in core-dump):**
- After workflow-plugin-migrations v0.3.7 is released + the workflow-migrate Docker image is rebuilt + pushed: bump `Dockerfile.migrate` from `v0.3.6@sha256:b10ba85a...` → `v0.3.7@sha256:<new>`. Pure version-pin bump.

**Verification:** workflow-plugin-migrations unit tests + a manual run of the new image against a clean database and against a database-with-pending-migrations to verify both paths exit 0.

### Blocker 3 — workflow#513 — atlas Executor panic on apply (upstream fix in workflow-plugin-migrations)

**Root cause:** Panic in upstream `ariga.io/atlas/sql/migrate.(*Executor).Execute` at "index out of range [0] with length 0" when our code calls `ex.ExecuteN(ctx, 0)` (`internal/atlas/driver.go:51`) against the core-dump migrations corpus. Reproduces on both postgres:18-alpine AND apache/age:release_PG18_1.7.0 — not a postgres-side issue. Upstream atlas library bug or a misuse where atlas dereferences an empty slice without bounds-checking.

**Fix shape:**

Two-part:

(a) **Defensive: wrap the panic.** Add a `defer recover()` around `ex.ExecuteN(ctx, 0)` and the matching `ex.Pending(ctx)` call. Convert panic → typed error like `interfaces.MigrationError{Inner: ..., Phase: "atlas-execute"}`. The plugin's caller in core-dump's pre_deploy job sees an error message instead of a process panic; can retry, escalate, or fail the deploy gracefully.

(b) **Investigate root cause.** Bisect the migrations corpus to find which migration file triggers atlas's panic. Likely an empty file, malformed up/down statement, or hash mismatch between filename and content. Fix the corpus OR file upstream atlas issue.

For (b): test with the actual core-dump corpus locally (postgres docker compose + workflow-migrate v0.3.7 image with the recover wrapper from (a)). When the wrapper catches the panic, the error message will identify which migration was being processed; that's the bisect anchor.

**Files (in workflow-plugin-migrations repo):**
- `internal/atlas/driver.go:46-66` (Up), `:151-180` (Status), `:209-...` (Down) — add recover wrappers
- `internal/atlas/driver_test.go` — test that simulates a panic-inducing input + asserts a typed error returned
- Optionally: `internal/atlas/corpus_validator.go` — pre-flight check that validates migration files (non-empty, valid SQL, hash-matched filename) and surfaces issues clearly before atlas ever runs. Defer to a follow-up if the recover wrapper is enough.

**Verification:** unit test that the recover catches the panic + returns a typed error; integration test against the actual core-dump corpus to confirm the bisect target (which migration file is the trigger).

### Iteration — fix what surfaces in apply

After Blockers 1 + 2 + 3 fixes ship + new wfctl pin lands in core-dump:
- Run `wfctl infra apply` → first end-to-end apply against staging
- DO managed Postgres provisioning (~5-10 min on first apply)
- App Platform deploy
- pre_deploy migrate job runs (now actually works)
- App revision rolls
- /healthz endpoint responds

Predictable failure modes:
- DNS / service-discovery for NATS service-to-service (`coredump-nats-staging.internal:4222`)
- Network-policy / trusted_sources on managed-DB
- App Platform health-check timing (the deploy.yml verification step probes /healthz after a soak)

Each new failure is an upstream-or-config decision per user direction. Stay on wfctl. Don't shortcut to doctl.

## PR sequencing

```
PR-A1 (workflow):  preserve ${VAR} in env_vars submaps
                   ↓ ships in workflow v0.21.0
                   ↓
PR-A2 (core-dump): bump wfctl pin to v0.21.0
                   ↓ deploy re-runs; security-check should clear
                   ↓
PR-B1 (workflow-plugin-migrations):
                   --up-if-clean on `up` subcommand
                   atlas Executor recover wrapper
                   ↓ ships as workflow-plugin-migrations v0.3.7
                   ↓ workflow-migrate Docker image pushed to ghcr.io/gocodealone/workflow-migrate:v0.3.7
                   ↓
PR-B2 (core-dump): bump Dockerfile.migrate pin to v0.3.7
                   ↓ deploy re-runs; pre_deploy migrate should reach apply
                   ↓
PR-C1+ (any repo): iterate on whatever surfaces
```

PR-A1 + PR-B1 are independent (different repos, different code). PR-A2 + PR-B2 sequence on their respective upstream releases. PR-C1+ is open-ended iteration.

## Cross-repo coordination

- All upstream PRs use Copilot reviewer + bug-class checklist scan + structpb-boundary scan + adversarial framing per workspace memory.
- Per workspace memory feedback_admin_override_pr_merge: admin-merge once Copilot resolved + CI green.
- Per workspace memory feedback_version_bump_immediate_merge: version-bump PRs (PR-A2, PR-B2) auto-merge.
- workflow#516 lint drift will continue to fail on workflow PRs; admin-merge with override per same memory.
- v0.20.2 ghost tag remains; skip to v0.21.0 (or v0.20.4 if the changes are deemed patch-only — env-var preservation in plan output is arguably a behavior change → minor bump).

## Acceptance criteria

- core-dump #154 closed: re-run Deploy on main; security-check passes for the env_vars-with-secret-references pattern.
- core-dump #150 closed: workflow-migrate `up --up-if-clean` accepted by cobra; pre_deploy migrate exits 0 against the staging DB.
- workflow#513 closed (defensive): the atlas panic is caught + surfaced as a typed error instead of process death. (Root-cause fix is bonus; the recover wrapper is the must-have.)
- Staging app reachable: `curl /healthz` returns 200 from the staging URL.

## Out of scope

- Investigating the root cause of the atlas Executor panic (only the defensive recover wrapper is must-have for #513).
- workflow#516 lint drift (separate cleanup PR).
- v0.20.2 ghost tag deletion (requires repo-admin override of the protect-tags ruleset).
- workflow#514 wfctl build push pipeline (already worked around via buildx stopgap in core-dump).
- App Platform performance tuning, scaling configuration, observability — first deploy is "does it work at all", not "is it production-ready".

## System Impact

- **Auth/secrets:** Solution A changes when env-var values get resolved (parse-time → apply-time injection). Same secret material; different resolution timing. Apply-time path already handles this for non-env_vars fields per the existing infra_env_resolve.go:88 comment. No new auth surface.
- **Plan/state semantics:** plan.json now contains `${VAR}` literals in env_vars submaps. State store unaffected (state is post-apply, env_vars resolved). Existing plans (from before the change) still apply correctly because apply-side already does ExpandEnvInMap.
- **Migration semantics:** `--up-if-clean` on `up` makes the command idempotent against a clean DB. Behavior change for callers passing the new flag; existing callers (no flag) unchanged. Not a backwards-compat break.
- **Atlas recover wrapper:** converts a process-killing panic into a typed error. Behavior change: callers that relied on the process dying (none in our codebase) would now get an error to handle. This is strictly safer.
- **All other categories** (anti-cheat, malware, sandbox, network, filesystem, process/OS, social, NPC, factions, economy, IoT, media, legal, forensics, VERA, achievements, client desktop, terminal, world history, content, telemetry): None — these are wfctl/plugin/migration plumbing changes with no game-runtime surface.
