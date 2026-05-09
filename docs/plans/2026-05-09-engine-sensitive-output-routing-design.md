# Engine-side sensitive-output routing through `secrets.Provider`

**Status:** design — pending adversarial-design-review (phase=design)
**Target tag:** workflow `v0.27.0`
**Branch:** `design/engine-sensitive-output-routing` (worktree: `_worktrees/engine-sensitive-output-routing`)
**Author:** brainstorming pipeline (autonomous lead)
**Related:**
- ADR 0015 — Approach B Hybrid split-storage for spaces-key (state ID + secret_key separate)
- workflow PR #581 — `wfctl infra audit-secrets` (post-merge audit surface)
- prior pattern — `cmd/wfctl/infra_output_secrets.go` (`syncInfraOutputSecrets`)

## 1. Problem

The DO plugin's `SpacesKeyDriver.Create` returns a freshly-minted access-key + secret_key pair. The DO API emits `secret_key` exactly once on creation; on subsequent `Read` calls the API does **not** re-emit it. The driver therefore MUST surface `secret_key` in `*ResourceOutput.Outputs` on Create — otherwise the value is lost forever.

Today the engine (call sites in `cmd/wfctl/infra_apply.go:516-557` and `:1000-1040`) takes that `*ResourceOutput.Outputs` map and writes it verbatim into `interfaces.ResourceState.Outputs`, which the configured `IaCStateStore` (filesystem JSON, GCS blob, S3, Postgres, …) persists. **`secret_key` ends up plaintext in the state record.** That violates the user mandate ("plugin shouldn't have an expectation of GH secrets being available; engine should route") and ADR 0015's split-storage contract.

The user mandate further requires that the routing layer live in the **engine**, not in any plugin. A plugin compiled into a wfctl-from-CI run, a wfctl-from-CLI run, a workflow-cloud server run, or a third-party host must all transparently get sensitive-output handling without each host writing its own routing.

## 2. Goals

1. **Engine routes `Sensitive`-flagged outputs to `secrets.Provider`** before the state-write call.
2. **State backend never sees a sensitive value** — `ResourceState.Outputs` carries only non-sensitive fields plus a `secret_ref://<key>` placeholder string for each routed field.
3. **Plugin stays platform-agnostic** — `ResourceDriver` API is unchanged; the plugin only declares "these output keys are sensitive". The engine handles the rest.
4. **Symmetric Delete cleanup** — when a resource is deleted, the engine deletes the routed secrets from `secrets.Provider`.
5. **Backward-compatible** — existing plugins (no `Sensitive` flag, no `SensitiveKeys()`) continue to work as today; existing state records (plaintext sensitive values) are not corrupted.
6. **Plugin-agnostic configuration** — the same routing layer works whether the configured `secrets.Provider` is GitHub Actions, Vault, env, file, AWS Secrets Manager, etc.

## 3. Non-goals

- **No new secret-storage backends.** Reuse `secrets.Provider` implementations as-is.
- **No encryption-at-rest of state.** Out of scope; that's a state-backend concern.
- **No retroactive scrubbing** of existing state records that contain plaintext secrets. Operators run `wfctl infra audit-secrets` (already shipped) and rotate.
- **No new gRPC plugin contract.** `ResourceDriver` interface is unchanged; the new routing is host-side only, downstream of `Apply`/`ApplyPlan`.
- **No multi-secret-provider per state record.** One `secrets.Provider` per apply run, configured via the same `secretsCfg` already used by `bootstrapSecrets` and `syncInfraOutputSecrets`.
- **No per-output secret-name templating.** Naming is mechanical: `<resource_name>_<output_key>` (uppercased + dot-to-underscore via the provider's own normalisation in `EnvProvider.envKey`).

## 4. Approach

### 4.1 New surface

Introduce a single new exported helper in a new package `iac/sensitive`:

```go
// Package sensitive routes sensitive driver outputs through secrets.Provider.
package sensitive

// Router routes sensitive ResourceOutput fields to a secrets.Provider, returning
// a sanitized copy with sensitive values replaced by "secret_ref://<key>"
// placeholders. The original ResourceOutput is not mutated.
//
// Naming: routed secrets are stored under "<resource_name>_<output_key>".
// All keys are passed to provider.Set verbatim; per-provider normalisation
// (e.g., EnvProvider's UPPER_SNAKE_CASE) is the provider's responsibility.
type Router struct {
    provider secrets.Provider
}

func NewRouter(provider secrets.Provider) *Router

// Route routes sensitive fields from out to the provider. Returns a sanitized
// outputs map (sensitive values replaced by "secret_ref://<resource>_<key>")
// suitable for state persistence. mergedKeys is the union of:
//   - out.Sensitive[k] == true (per-call dynamic flag)
//   - driver.SensitiveKeys() (driver-static declaration)
//
// On secrets.Provider.Set failure, returns the error WITHOUT having written
// any state — caller MUST treat as apply failure (operator can rerun).
// If provider is nil and there are sensitive fields, returns an error
// (callers MUST configure a provider when running plugins that emit sensitive
// outputs).
func (r *Router) Route(ctx context.Context, out *interfaces.ResourceOutput, mergedKeys []string) (sanitized map[string]any, err error)

// Restore reads previously-routed secret values from the provider and returns
// a "rehydrated" outputs map suitable for downstream consumers (template
// resolution, infra_output secret generators). Sanitized state outputs
// containing "secret_ref://<key>" placeholders are replaced with the
// provider-fetched value. Non-placeholder values pass through unchanged.
//
// On secrets.Provider.Get failure for any individual key, returns the error;
// caller decides whether to fall back (e.g., operator running on a CI host
// without secrets access).
func (r *Router) Restore(ctx context.Context, sanitized map[string]any) (map[string]any, error)

// Revoke deletes all routed secrets for a resource (called on Delete).
// Best-effort: failures are returned but do NOT block the state-delete
// (operator can run `wfctl infra audit-secrets --prune` to clean up
// orphans).
func (r *Router) Revoke(ctx context.Context, resourceName string, mergedKeys []string) error
```

The placeholder format `secret_ref://<key>` mirrors the existing `secret://<key>` convention in `secrets/secrets.go:13` (`SecretPrefix`). The new prefix `secret_ref://` is distinct so consumers can disambiguate "this state field IS a routed-secret reference" from "this config value is a secret://-style lookup".

### 4.2 Wiring at the state-write boundary

The engine has TWO state-write call sites for `ApplyResult.Resources`:

1. `cmd/wfctl/infra_apply.go:516-557` — `applyWithProviderAndStore`
2. `cmd/wfctl/infra_apply.go:1000-1040` — in-process apply path

Both build a `ResourceState{...Outputs: r.Outputs ...}` literal. We refactor both to call a shared helper:

```go
// persistResourceWithSecretRouting builds a ResourceState from a ResourceOutput,
// routes sensitive fields through the configured Router (if non-nil), and saves
// the sanitized state via store.SaveResource. Returns the same error semantics
// as store.SaveResource for the caller's existing error path.
func persistResourceWithSecretRouting(
    ctx context.Context,
    store interfaces.IaCStateStore,
    router *sensitive.Router, // may be nil — falls through to plaintext-in-state
    driver interfaces.ResourceDriver,
    out interfaces.ResourceOutput,
    rs interfaces.ResourceState,
) error
```

`ApplyPlan` itself stays unchanged. The router is passed to the helper from the call-site, where the existing `resolveSecretsProvider` factory is already wired (see `cmd/wfctl/infra.go:1425` for the pattern).

### 4.3 Router activation policy

The router is constructed at apply entry IFF `secretsCfg != nil`. If `secretsCfg == nil` AND any driver returns sensitive outputs, the helper returns an error like:

```
state write rejected: resource "coredump-deploy-key" has sensitive output keys [secret_key]
but no secrets.Provider is configured. Configure secrets.* in your workflow config or
set --secrets-provider=env to use environment-variable storage.
```

This is a **hard fail** — silently writing plaintext-to-state would defeat the entire premise. The error message names the resource + sensitive keys + remediation, so operators immediately know how to recover.

### 4.4 Read / adoption / refresh behaviour

`adoptExistingResources` (`cmd/wfctl/infra_apply.go:583`) and `infra_refresh_outputs.go:244` call `driver.Read` and persist the live `ResourceOutput`. For sensitive fields:

- **DO Spaces** — `Read` cannot re-emit `secret_key`. Driver returns `Outputs` WITHOUT `secret_key` (just `access_key`, `bucket`, etc.).
- **AWS IAM access keys** — same pattern: secret accessible only at create.
- **Other resources** — Read may legitimately re-emit (e.g., a database connection_string that's stable).

The router's `Route` is called regardless. When a sensitive key is absent from `out.Outputs`, the router silently no-ops for that key (no-op = "we have nothing to route, the existing routed-secret in `secrets.Provider` stays as-is"). A `secret_ref://` placeholder is still inserted into the sanitized state map so the state record has the same shape as a freshly-applied one. This is the **idempotent re-Read invariant**: re-applying never loses or corrupts a previously-routed secret.

### 4.5 Delete cleanup

`doDelete` (`iac/wfctlhelpers/apply.go:540`) does not currently produce a `ResourceOutput`. We add a post-Delete callback at the call site (`cmd/wfctl/infra_apply.go:561` — the `for name := range deleteNames` loop) that invokes `router.Revoke(ctx, name, mergedKeys)`. `mergedKeys` is sourced from the pre-delete state record's `Outputs` (any field with a `secret_ref://` placeholder is a candidate for revocation).

Revoke errors do NOT block state deletion (lest a stale secret-provider creds block all cleanup). They are logged and surface in the run summary; `wfctl infra audit-secrets --prune` is the recovery surface.

### 4.6 Restore at consumer boundaries

`syncInfraOutputSecrets` and the post-apply pipeline_run loop both consume `state.Outputs`. After this PR they may receive `secret_ref://...` placeholders for sensitive fields. We add `router.Restore(ctx, outputs)` at:

- `cmd/wfctl/infra_output_secrets.go:151` — before `buildStateOutputsMap` (so generators see real values).
- Anywhere else `state.Outputs` is read for downstream resolution. We grep these in the plan phase and enumerate.

Where the state is read solely for display / diff (not for value-substitution), no Restore — the placeholder is the desired display value.

### 4.7 Reconciliation with existing surfaces

| Surface | Today | Post-PR |
|---|---|---|
| `ResourceDriver.SensitiveKeys() []string` | Used by `secrets.MaskSensitiveOutputs` for log/plan masking only | ALSO consumed by `Router.Route` as the driver-static sensitive-keys source |
| `ResourceOutput.Sensitive map[string]bool` | Informational; not enforced | Per-call dynamic source for `Router.Route`; OR'd with `SensitiveKeys()` |
| `secrets.DefaultSensitiveKeys()` | Used at `cmd/wfctl/infra.go:674` for resource-summary masking | UNCHANGED — masking is a separate concern from routing |
| `infra_output:` secret generators | Read state.Outputs verbatim and re-Set into provider | Reads via `Router.Restore` so placeholders rehydrate |
| `bootstrapSecrets` | Generates provider-credential keys before apply | UNCHANGED — different source (config-declared, not driver-emitted) |

## 5. Data flow

```
                ┌─────────────────────────────────────────────────┐
                │  Driver.Create returns *ResourceOutput          │
                │  Outputs: {access_key, secret_key, bucket, …}   │
                │  Sensitive: {secret_key: true, access_key: true}│
                └────────────────────┬────────────────────────────┘
                                     │
                                     ▼
                ┌─────────────────────────────────────────────────┐
                │  wfctlhelpers.ApplyPlan — UNCHANGED             │
                │  result.Resources = [output, …]                 │
                └────────────────────┬────────────────────────────┘
                                     │
                                     ▼
                ┌─────────────────────────────────────────────────┐
                │  infra_apply.persistResourceWithSecretRouting   │
                │   1. mergedKeys = SensitiveKeys() ∪ Sensitive   │
                │   2. router.Route(ctx, out, mergedKeys)         │
                │      → for each k in mergedKeys + present:      │
                │           secrets.Provider.Set("<resource>_<k>",│
                │             out.Outputs[k])                     │
                │           sanitized[k] = "secret_ref://<resource>_<k>" │
                │      → other keys pass through                  │
                │   3. rs.Outputs = sanitized                     │
                │   4. store.SaveResource(ctx, rs)                │
                └─────────────────────────────────────────────────┘
```

## 6. Failure modes

| Failure | Behaviour |
|---|---|
| `secrets.Provider.Set` fails | `Route` returns the error; caller treats as apply failure for that resource (recorded in `result.Errors`). State NOT written for that resource. Operator reruns; idempotent Set overwrites if partial. |
| `secrets.Provider` not configured AND driver emits sensitive outputs | Hard fail at the persist boundary with named resource + remediation. |
| `secrets.Provider.Set` succeeds, `store.SaveResource` fails | Orphan secret in provider; state not written. Recovery: `wfctl infra audit-secrets` lists orphans; rerun apply (idempotent Set is fine). Documented as known window. |
| `Router.Restore` Get fails | Error bubbles to consumer (e.g., `syncInfraOutputSecrets`); operator sees actionable error. |
| `Router.Revoke` fails | Logged, run summary records; state-delete proceeds. `audit-secrets --prune` is recovery. |
| Plugin returns `Sensitive: {k: true}` but `Outputs[k]` absent | Router no-ops for that key; placeholder still inserted (idempotent re-Read invariant, §4.4). |
| Plugin returns plaintext sensitive output but does NOT mark `Sensitive` AND `SensitiveKeys()` doesn't list it | Plaintext leaks to state. **Mitigation**: `wfctl infra audit-secrets` already detects via `secrets.DefaultSensitiveKeys()` heuristic match. Plugin reviewer is the long-term fix. Not a regression. |
| Backward-compat: existing state record has plaintext `secret_key` | `Restore` only rewrites `secret_ref://...` placeholders; plaintext values pass through unchanged. Operator-driven rotation cleans up legacy state via the standard rotate flow. |

## 7. Testing

### Unit (`iac/sensitive/router_test.go`)

- `Route` with no sensitive keys → outputs verbatim.
- `Route` with `Sensitive: {k:true}` and `Outputs[k] = "secret"` → `provider.Set("<resource>_<k>", "secret")` called once + sanitized has `secret_ref://<resource>_<k>`.
- `Route` with `SensitiveKeys()` ∪ `Sensitive` → both sources merged; deduplicated.
- `Route` with sensitive key absent from `Outputs` → no `Set` call; placeholder inserted.
- `Route` with `provider.Set` error → returns error; no partial state.
- `Route` with `provider == nil` AND non-empty sensitive keys → returns error naming the resource + keys.
- `Restore` with `secret_ref://k` placeholder → returns `provider.Get("k")` value.
- `Restore` with non-placeholder value → passes through.
- `Restore` with `provider.Get` error → returns error.
- `Revoke` calls `provider.Delete` for each key; aggregates errors.

### Integration (`cmd/wfctl/infra_apply_sensitive_routing_test.go`)

- Apply path with stub driver returning sensitive outputs + env-provider secrets.Provider → state record contains `secret_ref://...` placeholders; env vars contain values.
- Apply path with sensitive driver but no `secretsCfg` → returns hard-fail error.
- Re-apply (idempotent) → second `Set` overwrites; state placeholder unchanged.
- Delete path → `Revoke` calls observed; state record deleted.
- `syncInfraOutputSecrets` reads a state with `secret_ref://...` placeholder → uses `Restore` → secret-generator gets the real value.

### Local CI gates (per `feedback_local_ci_validation_for_ci_touching_tasks`)

Before requesting review, implementer runs:

- `go test ./iac/sensitive/... ./cmd/wfctl/... ./secrets/...`
- `go test -race ./iac/sensitive/... ./cmd/wfctl/...`
- `go vet ./...`
- `golangci-lint run ./iac/sensitive/... ./cmd/wfctl/...`
- `wfctl-strict-contracts` (for any plugin/manifest paths touched — none expected here)

## 8. Migration & rollback

### Migration

- Plugins: no code change required to keep working as-is. Plugins that want engine-side routing add `Sensitive: {k: true}` to their `ResourceOutput` returns (recommended) or rely on the existing `SensitiveKeys()` declaration (already used for masking).
- DO plugin: separate PR; not in this scope. The DO `SpacesKeyDriver` change just adds `Sensitive: {"secret_key": true}` to its Create return.
- Operators: **no action required for greenfield envs**. Operators with pre-existing state records containing plaintext secrets run `wfctl infra audit-secrets` (already shipped in PR #581) to enumerate. Migration tool to retroactively scrub is a follow-up; plaintext-in-legacy-state is not regressed by this PR.

### Rollback

This PR affects runtime apply behaviour (state-write). Rollback path:

1. **Pin to v0.26.x** at `setup-wfctl@vN`. The engine reverts to plaintext-state behaviour.
2. **Existing state records written under v0.27.0** still contain `secret_ref://...` placeholders. v0.26.x consumers do NOT understand this; they will treat the placeholder as the literal value (e.g., `infra_output` generator copies "secret_ref://..." into a downstream secret). Recovery: rotate the affected secrets via `wfctl infra bootstrap --force-rotate <name>` (already shipped); the rotation regenerates plaintext-in-state under v0.26.x.
3. The new package `iac/sensitive` and helper `persistResourceWithSecretRouting` are additive; reverting the call sites to the v0.26.x literal `Outputs: r.Outputs` shape is a one-commit revert of `infra_apply.go`.

## 9. Assumptions

The following assumptions are load-bearing; the adversarial reviewer should attack them.

1. **Single configured `secrets.Provider` per apply run.** `resolveSecretsProvider(secretsCfg)` returns one provider. Multi-provider routing per resource is YAGNI for v0.27.0.
2. **`secrets.Provider.Set` is idempotent overwrite semantics for all production providers** (GitHub, Vault, env, file). Verified by reading existing implementations: `EnvProvider.Set` calls `os.Setenv`; `FileProvider.Set` calls `os.WriteFile`; `GitHubSecretsProvider.Set` calls `gh secret set` which overwrites. Hold for all five `secrets/*_provider.go`.
3. **The DO API's "secret_key emitted only on Create" property generalises to other providers** (AWS IAM access keys, GCP service-account keys, Azure storage account keys). Pattern verified for DO + AWS; design must work for the worst case (Read does NOT re-emit).
4. **`ResourceDriver.SensitiveKeys()` is reasonably populated by existing plugins.** The plan-phase enumerator should grep the IaC plugin repos to confirm. Today most plugins return `nil`; this PR's value is realised when plugins start populating it (or returning `ResourceOutput.Sensitive`).
5. **`secret_ref://` is a free namespace** — no existing config field uses this prefix. Verified: `grep -r "secret_ref://" .` returns zero hits in workflow + workflow-plugin-* repos as of `8de95b4f`.
6. **State records on disk / in cloud-storage are NOT human-edited.** A plaintext value of literally `"secret_ref://x"` (with the prefix) does not occur in practice. If it did, restoration would dereference incorrectly. This is the same trust assumption as `secret://...` config references.
7. **`syncInfraOutputSecrets` and the pipeline-run consumer are the only state-output value-substitution surfaces.** The plan phase grep-enumerates all `state.Outputs` consumers and verifies `Restore` is wired wherever values are used (vs. displayed).

## 10. Top doubts (from self-challenge round)

These are not full FAIL findings; they are the doubts the brainstormer is least confident about and wants the reviewer to attack:

1. **Read/Adoption asymmetry** — for resources where Read DOES re-emit a sensitive value (e.g., a database connection string the cloud will give back on demand), should `Route` overwrite the routed-secret with the freshly-Read value? The design says "yes, idempotently" — but if the cloud re-emits a stale value (caching, eventual consistency), we'd silently corrupt a secret the user already rotated out-of-band. Mitigation: `Set` is overwrite-semantics, so the operator's rotate flow remains authoritative. Probably fine; reviewer should pressure-test.
2. **Failure-window orphan secrets** — `secrets.Set` succeeds, `store.SaveResource` fails → orphan. `audit-secrets --prune` is the recovery, but it requires the operator to remember to run it. Should we instead write state FIRST (with placeholder) then `secrets.Set`? No: that would leave the placeholder in state with no backing value. The current ordering (Set then Save) at least guarantees the secret is always retrievable when state references it. Acceptable trade-off; documented.
3. **Plugin discovery of the Sensitive contract** — today most plugin authors don't know `Sensitive: map[string]bool` exists (it's "informational"). After this PR, missing `Sensitive` for a plaintext-secret output silently leaks to state. Mitigation: workflow-plugin-reviewer skill (already exists) should add a "plugin-sensitive-keys-declared" check. Out of scope for this PR but flagged as follow-up.

## 11. Estimated scope

| Component | Files touched (approx.) | LoC delta |
|---|---|---|
| New `iac/sensitive` package | 2 (`router.go`, `router_test.go`) | +400 |
| State-write helper refactor | 1 (`cmd/wfctl/infra_apply.go`) | +60 / -30 |
| Restore-at-consumer wiring | 2-3 (`infra_output_secrets.go`, `infra_templates.go`) | +30 |
| Delete revoke wiring | 1 (`cmd/wfctl/infra_apply.go`) | +20 |
| Integration tests | 1-2 (`infra_apply_sensitive_routing_test.go`) | +300 |
| Doc update — `DOCUMENTATION.md` Sensitive section | 1 | +40 |
| **Total** | ~8-10 files | ~+800 net |

## 12. Out-of-scope / follow-ups

1. DO plugin update to set `Sensitive: {"secret_key": true}` on `SpacesKeyDriver.Create` — separate PR after v0.27.0 tag.
2. AWS, GCP, Azure plugins — same pattern, separate PRs each (or a single batched PR; coordinator decision).
3. workflow-plugin-reviewer skill update — add "plugin-sensitive-keys-declared" check (lint surface).
4. State-record migration tool to retroactively route legacy plaintext secrets — separate work; not blocking v0.27.0.
5. UI / wfctl display of `secret_ref://...` placeholders — should they render as `(routed: <key>)`? Cosmetic, follow-up.
