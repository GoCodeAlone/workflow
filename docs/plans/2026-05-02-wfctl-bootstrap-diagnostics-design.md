# wfctl bootstrap diagnostics + schema validation — design

**Status:** Approved 2026-05-02. Sequential 3-PR rollout.

## Goal

Make the canonical `wfctl infra bootstrap` flow diagnose schema misconfigurations clearly and prevent the most common ones at align time. Validated end-to-end by re-running the core-dump bootstrap+deploy chain entirely on wfctl (no `doctl` / `gh secret set` fallbacks).

## Background

On 2026-05-02 the first post-merge core-dump deploy attempt (P3) failed during the `wfctl infra bootstrap --env staging` step with:

```
secret "SPACES_access_key_access_key": created
secret "SPACES_access_key_secret_key": created
secret "SPACES_secret_key_access_key": created
secret "SPACES_secret_key_secret_key": created
secret "NATS_AUTH_TOKEN": created
...
error: plugin "digitalocean" iac.provider factory returned nil
```

Two compounding misdiagnoses emerged from a side-by-side comparison with the working BMW deploy:

1. **core-dump infra.yaml schema mistake.** P1 wrote two `secrets.generate[]` entries for the Spaces key pair (`key: SPACES_access_key` + `key: SPACES_secret_key`), expecting them to bind 1:1 to the two sub-fields of one Spaces credential. wfctl's documented behavior is to auto-suffix `_access_key` / `_secret_key` onto each `key:` (per `providerCredentialSubKeys` map in `cmd/wfctl/infra_bootstrap.go:251`). One entry with `key: SPACES` produces the desired pair; BMW's working config uses exactly that. Two entries produce four wrongly-named secrets. **wfctl behavior is correct as designed.**

2. **core-dump infra.yaml iac.provider config mistake.** P1 wrote `credentials: env`, which is not a real wfctl shorthand. The plugin's `Initialize` requires `token: "${DIGITALOCEAN_TOKEN}"` explicitly (per `internal/provider.go:51`). BMW's working config sets it. The plugin returned an error, which would have been clear — but wfctl swallowed the message.

The reason these two mistakes were hard to diagnose: **the external-plugin adapter swallows plugin errors**. When `CreateModule` returns `(nil, error)`, the adapter at `plugin/external/adapter.go:328-330` checks `createResp.Error != ""` and returns bare nil. wfctl then reports the generic "iac.provider factory returned nil" with no detail of why. The actual `digitalocean: missing required config key 'token'` message never reaches the operator.

## Scope — three sequential PRs

Each PR is independently mergeable; no cross-PR dependencies. PR 1 is on the critical path for the core-dump deploy chain. PR 2 + PR 3 are quality-of-life improvements that prevent the same diagnostic gap from recurring.

### PR 1 — `core-dump` infra.yaml correction

Repo: `GoCodeAlone/core-dump`. Critical path; ships first; unblocks the deploy chain.

**Files:**
- `infra.yaml` (modify)

**Changes:**

Replace the `secrets.generate[]` Spaces entries from:

```yaml
- key: SPACES_access_key
  type: provider_credential
  source: digitalocean.spaces
  name: coredump-deploy-key
- key: SPACES_secret_key
  type: provider_credential
  source: digitalocean.spaces
  name: coredump-deploy-key
```

to:

```yaml
- key: SPACES
  type: provider_credential
  source: digitalocean.spaces
  name: coredump-deploy-key
```

Replace the `iac.provider` module config from:

```yaml
- name: do-provider
  type: iac.provider
  config:
    provider: digitalocean
    credentials: env
```

to:

```yaml
- name: do-provider
  type: iac.provider
  config:
    provider: digitalocean
    token: ${DIGITALOCEAN_TOKEN}
    spaces_access_key: ${SPACES_access_key}
    spaces_secret_key: ${SPACES_secret_key}
```

Comment block above the iac.provider block referencing this design doc + the BMW pattern.

**Cleanup of stale secrets:** the prior bootstrap created four wrongly-named GH repo secrets (`SPACES_ACCESS_KEY_ACCESS_KEY`, `SPACES_ACCESS_KEY_SECRET_KEY`, `SPACES_SECRET_KEY_ACCESS_KEY`, `SPACES_SECRET_KEY_SECRET_KEY`) plus a legitimate `NATS_AUTH_TOKEN`. Delete the four wrong secrets via `gh secret delete` before re-running bootstrap so the new run starts clean. NATS_AUTH_TOKEN stays.

**Verification:**
1. PR CI green (no behavioral changes outside infra.yaml; existing CI suite covers it).
2. Re-trigger Bootstrap workflow for staging via `gh workflow run bootstrap.yml -f environment=staging --ref main` after merge.
3. Bootstrap should produce exactly two secrets: `SPACES_access_key`, `SPACES_secret_key` (plus NATS_AUTH_TOKEN if missing).
4. The next CI completion auto-fires Deploy via `workflow_run`. Deploy-staging should now reach `wfctl infra plan/align/security-check/apply` with valid creds in env.
5. End-to-end: app reachable on /healthz at the staging URL.

### PR 2 — `workflow` adapter error propagation

Repo: `GoCodeAlone/workflow`. Quality improvement; ships in next workflow patch release.

**Files:**
- `plugin/external/adapter.go` (modify)
- `plugin/external/adapter_test.go` (modify or add)

**Changes:**

In `ExternalPluginAdapter.ModuleFactories()` (`plugin/external/adapter.go:309-343`), replace the silent-nil-on-error pattern:

```go
createResp, createErr := a.client.client.CreateModule(ctx, &pb.CreateModuleRequest{...})
if createErr != nil || createResp.Error != "" {
    return nil
}
```

with an `errorModule` (the same pattern already used a few lines earlier for `configErr`):

```go
createResp, createErr := a.client.client.CreateModule(ctx, &pb.CreateModuleRequest{...})
if createErr != nil {
    return &errorModule{name: name, err: fmt.Errorf("create remote module %s: %w", tn, createErr)}
}
if createResp.Error != "" {
    return &errorModule{name: name, err: fmt.Errorf("create remote module %s: plugin reported: %s", tn, createResp.Error)}
}
```

Apply the same fix to `StepFactories()` (`plugin/external/adapter.go:354+`) for symmetry.

The caller in `cmd/wfctl/deploy_providers.go:160-164` currently checks `if mod == nil` and reports a generic "factory returned nil" message. With the change above, `mod` is non-nil but is an `errorModule`. Update the caller to detect `errorModule` and surface its embedded error message:

```go
mod := factory("iac-provider", cfg)
if errMod, ok := mod.(*errorModule); ok {
    mgr.Shutdown()
    return nil, nil, fmt.Errorf("plugin %q iac.provider factory failed: %w", pluginName, errMod.err)
}
if mod == nil {
    mgr.Shutdown()
    return nil, nil, fmt.Errorf("plugin %q iac.provider factory returned nil (unexpected — file an issue)", pluginName)
}
```

(The bare-nil branch becomes a defensive backstop for unforeseen failure modes; in practice, the `errorModule` branch handles every plugin-side error path.)

**Tests:** add a unit test for `ExternalPluginAdapter.ModuleFactories()` that simulates `CreateModule` returning a non-empty `Error` field and asserts the returned module is an `errorModule` whose error contains the plugin's reported message. Add an integration-style test for `discoverAndLoadIaCProvider` that injects a fake plugin returning an error from CreateModule and asserts the wrapped error message reaches the caller.

**Backwards compatibility:** existing behavior on success path is unchanged (CreateModule succeeds → RemoteModule returned). The only behavior change is on failure path: callers that previously saw bare nil now get an `errorModule`. The two known callers (`discoverAndLoadIaCProvider` for `iac.provider`, plus engine module-loading for general modules) need to handle the `errorModule` case. Engine module-loading already does (per the existing `errorModule` usage on the `configErr` branch above the changed block).

**No CHANGELOG-breaking change** — this strictly improves diagnostics; consumers that did the right thing always still work.

### PR 3 — `workflow` `infra align` validation rule for suspicious `provider_credential` schema

Repo: `GoCodeAlone/workflow`. Quality improvement; ships in next workflow minor release.

**Files:**
- `cmd/wfctl/infra_align_rules.go` (modify — add rule)
- `cmd/wfctl/infra_align_rules_test.go` (modify or add)
- `docs/wfctl/infra-align-rules.md` if it exists, else `CHANGELOG.md` entry only

**Changes:**

Add a new align rule (next sequential R-A* identifier) that validates `secrets.generate[]` entries:

For each entry where `type: provider_credential` and `source` is in `providerCredentialSubKeys`:
- Compute the auto-suffixes that would be appended (e.g. `_access_key`, `_secret_key` for `digitalocean.spaces`).
- For each suffix, check whether the entry's `key:` ALREADY ends with that suffix.
- If yes, emit a WARN under default mode and FAIL under `--strict`:

> `secrets.generate[<index>].key: "SPACES_access_key"` ends with `_access_key`. For `provider_credential` source `digitalocean.spaces`, wfctl auto-appends sub-key suffixes (`_access_key`, `_secret_key`) onto the entry's key, producing `SPACES_access_key_access_key` / `SPACES_access_key_secret_key`. To bind the credential pair to env vars `SPACES_access_key` + `SPACES_secret_key`, declare ONE entry with `key: SPACES`. See docs/wfctl/secrets-generate.md.

Rule kind: `RuleKindSchemaShape` (a new kind, parallel to existing kinds).

**Tests:** unit tests covering:
- `key: SPACES` (clean — passes).
- `key: SPACES_access_key` (suspicious — warns under default; fails under `--strict`).
- `key: MY_THING_secret_key` (suspicious — warns).
- `key: NATS_AUTH_TOKEN` for `type: random_hex` (not provider_credential — no warning regardless of suffix).
- `key: SPACES_access_key` for `type: provider_credential` `source: aws.s3` (no warning until `aws.s3` is added to `providerCredentialSubKeys`; the rule is per-source-aware).

**Backwards compatibility:** the rule is WARN-only by default. Existing consumers without the suspicious pattern are unaffected. BMW's `key: SPACES` passes cleanly. Consumers with the pattern (e.g. core-dump pre-PR-1) get a WARN message but their bootstrap still proceeds. `--strict` mode fails, as is the documented intent of `--strict` for align rules.

## Out of scope

- **Refactoring `providerCredentialSubKeys` to a plugin-registered hook.** Tracked as TODO at `cmd/wfctl/infra_bootstrap.go:247-250`. Not blocking these PRs.
- **`credentials: env` shortform support in iac.provider.** Not a real shortform; not implemented anywhere in wfctl. PR 1 simply replaces the misuse with the real `token:` / `spaces_access_key:` / `spaces_secret_key:` form. Adding a shortform is a separate design conversation.
- **Renaming `key:` to `key_prefix:` for clarity.** Schema breaking change. Not justified by current evidence (one consumer error).
- **Workaround in core-dump bootstrap.yml using `doctl` / `gh secret set` directly.** Explicitly rejected by user direction (2026-05-02): "we are not falling back to doctl or similar. The entire point is to dogfood our own tooling."

## Cross-repo coordination

- PR 1 (core-dump) merges first. Stale secrets cleaned up. Bootstrap re-runs. Deploy validates end-to-end.
- After PR 2 (workflow) merges and ships in a workflow release, the wfctl version pin in core-dump (`.github/workflows/{deploy,bootstrap,teardown,registry-retention}.yml`) bumps to that release in a follow-up PR. From that point, future similar misconfigs surface with the actual plugin error message.
- After PR 3 (workflow) merges and ships, the wfctl pin bumps again. From that point, the suspicious schema pattern fails at `wfctl infra align --strict` (which deploy.yml already runs), preventing the same misconfig at PR-time rather than at deploy-time.

No cross-repo timing dependencies; each ships when its review cycle completes.

## Acceptance criteria

- PR 1: post-merge bootstrap run produces correctly-named secrets; deploy reaches plan/align/security-check/apply chain; staging app reachable.
- PR 2: a unit test simulating plugin error in CreateModule asserts the wrapped error message is propagated to the caller.
- PR 3: a unit test for `key: SPACES_access_key` + `provider_credential digitalocean.spaces` asserts the rule fires.

## System Impact

- **auth/authorization:** PR 1 changes which env vars the plugin sees for token/spaces creds — same secret material, different env var names. No new auth surface.
- **secrets:** PR 1 deletes 4 wrongly-named GH repo secrets; recreates 2 correctly. No change in secret material content, just naming. PR 2 + PR 3 do not touch secrets.
- **deploy pipeline:** PR 1 unblocks the deploy chain that's been broken since P3 merged. PR 2 + PR 3 only affect diagnostics + validation; no runtime change.
- **plugin contract:** PR 2 changes the failure-mode response from "factory returns bare nil" to "factory returns errorModule". Engine and wfctl both already handle errorModule on the configErr branch, so this is a uniform convention rather than a new contract.
- **align rules:** PR 3 adds one new rule. Existing rules unaffected. The new rule is WARN-only by default; only `--strict` mode promotes to fail. core-dump's deploy.yml runs `--strict`, so once core-dump bumps wfctl past this release, the rule is load-bearing for that consumer.
- All other System Impact Matrix categories (anti-cheat, malware, sandbox, network, filesystem, process/OS, social, NPC, factions, economy, IoT, media, legal, forensics, VERA, achievements, client desktop, terminal, world history, content, telemetry): None — these are wfctl/CI/IaC plumbing changes with no game-runtime surface.
