# Engine-side sensitive-output routing through `secrets.Provider`

**Status:** design — revised after adversarial-design-review cycle 1
**Target tag:** workflow `v0.27.0`
**Branch:** `design/engine-sensitive-output-routing` (worktree: `_worktrees/engine-sensitive-output-routing`)
**Author:** brainstorming pipeline (autonomous lead)
**Related:**
- ADR 0015 — Approach B Hybrid split-storage for spaces-key (state ID + secret_key separate)
- workflow PR #581 — `wfctl infra audit-secrets` (config anti-pattern audit; **distinct** from the new state-vs-provider audit added by this PR)
- prior pattern — `cmd/wfctl/infra_output_secrets.go` (`syncInfraOutputSecrets`)

## Revision history

- **rev1 (2026-05-09):** addressed adversarial-design-review findings: dropped `Router.Restore` (replaced with hold-and-hand-off in-memory map); restricted routing trigger to per-call `ResourceOutput.Sensitive` only (NOT `SensitiveKeys()`); added new `wfctl infra audit-state-secrets` task to scope; enumerated all five state-write call sites; clarified resource-name source as explicit parameter; added drift-detection masking task; addressed orphan-cloud-resource-on-Save-failure with compensating-Delete; restricted `Route` invocation to Create/Update only.

## 1. Problem

The DO plugin's `SpacesKeyDriver.Create` returns a freshly-minted access-key + secret_key pair. The DO API emits `secret_key` exactly once on creation; on subsequent `Read` calls the API does **not** re-emit it. The driver therefore MUST surface `secret_key` in `*ResourceOutput.Outputs` on Create — otherwise the value is lost forever.

Today the engine (call sites in `cmd/wfctl/infra_apply.go:516-557` and `:1000-1040`) takes that `*ResourceOutput.Outputs` map and writes it verbatim into `interfaces.ResourceState.Outputs`, which the configured `IaCStateStore` (filesystem JSON, GCS blob, S3, Postgres, …) persists. **`secret_key` ends up plaintext in the state record.** That violates the user mandate ("plugin shouldn't have an expectation of GH secrets being available; engine should route") and ADR 0015's split-storage contract.

The user mandate further requires that the routing layer live in the **engine**, not in any plugin. A plugin compiled into a wfctl-from-CI run, a wfctl-from-CLI run, a workflow-cloud server run, or a third-party host must all transparently get sensitive-output handling without each host writing its own routing.

## 2. Goals

1. **Engine routes per-call `Sensitive`-flagged outputs to `secrets.Provider`** before the state-write call (Create + Update only; not Read/Adoption/Refresh — see §4.4).
2. **State backend never sees a sensitive value** — `ResourceState.Outputs` carries only non-sensitive fields plus a `secret_ref://<resource>_<key>` placeholder string for each routed field.
3. **Plugin stays platform-agnostic** — `ResourceDriver` API is unchanged; the plugin only declares per-call `Sensitive: {k: true}` for outputs that need routing.
4. **Symmetric Delete cleanup** — when a resource is deleted, the engine deletes the routed secrets from `secrets.Provider`.
5. **Backward-compatible** — existing plugins (no `Sensitive` flag set) continue to work as today; existing state records (plaintext sensitive values) are not corrupted.
6. **Plugin-agnostic configuration** — the same routing layer works whether the configured `secrets.Provider` is GitHub Actions, Vault, env, file, AWS Secrets Manager, etc., subject to the **write-only-provider constraint** (§4.6).
7. **In-process hand-off, not state-cold-rehydrate** — post-apply consumers receive routed values via an engine-held in-memory map. State-cold rehydrate is **not supported** in v0.27.0; consumers needing cold rehydrate use the existing `secret://<key>` resolver against `secrets.Provider` directly with documented secret names.

## 3. Non-goals

- **No new secret-storage backends.** Reuse `secrets.Provider` implementations as-is.
- **No encryption-at-rest of state.** Out of scope; that's a state-backend concern.
- **No retroactive scrubbing** of existing state records that contain plaintext secrets. The new `wfctl infra audit-state-secrets` (§4.7) reports them; operators rotate via the standard rotate flow.
- **No new gRPC plugin contract.** `ResourceDriver` interface is unchanged.
- **No multi-secret-provider per state record.** One `secrets.Provider` per apply run, configured via the same `secretsCfg` already used by `bootstrapSecrets` and `syncInfraOutputSecrets`.
- **No per-output secret-name templating.** Naming is mechanical: `<resource_name>_<output_key>`.
- **No state-cold rehydrate API** (was `Router.Restore`; removed per adversarial-review). Consumers needing routed secrets at cold-start use `secret://<key>` resolver directly.
- **No reuse of `SensitiveKeys()` for routing trigger** (was `OR`'d with `Sensitive`; dropped per adversarial-review). `SensitiveKeys()` continues to drive **display masking only**.

## 4. Approach

### 4.1 New surface — free functions in `iac/sensitive` package

```go
// Package sensitive routes sensitive driver outputs through secrets.Provider.
package sensitive

// Route routes sensitive fields from out through provider, keyed under
// "<resourceName>_<outputKey>". Returns:
//   - sanitized: a copy of out.Outputs with sensitive values replaced by
//     "secret_ref://<resourceName>_<key>" placeholders. Suitable for state.
//   - hydrated: a flat map[secret_name]value of routed secrets, suitable for
//     in-process hand-off to post-apply consumers (syncInfraOutputSecrets,
//     pipeline-run, etc). Includes ONLY routed fields — non-sensitive fields
//     stay in sanitized.
//
// Routing trigger is exclusively out.Sensitive[k]==true (per-call dynamic).
// SensitiveKeys() is NOT consulted (that's display-masking-only).
//
// Sensitive keys whose value is absent from out.Outputs are SKIPPED — no
// provider.Set call, no placeholder inserted (the engine has no value to
// route; existing routed-secret in provider stays as-is). This is the
// idempotent re-Apply contract.
//
// On any provider.Set failure, returns the error WITHOUT having written
// state. Caller treats as apply failure for that resource. The caller's
// recovery flow (§4.5) is responsible for compensating the partial-Set.
//
// resourceName MUST be the canonical state key (rs.Name at the call site),
// NOT out.Name — adoption/refresh paths may have empty out.Name.
func Route(
    ctx context.Context,
    provider secrets.Provider,
    resourceName string,
    out *interfaces.ResourceOutput,
) (sanitized map[string]any, hydrated map[string]string, err error)

// Revoke deletes routed secrets for resourceName. mergedKeys is the union of:
//   - keys with "secret_ref://" placeholders in pre-delete state.Outputs
//   - secrets.DefaultSensitiveKeys() (legacy-state best-effort: pre-rev1 state
//     records have plaintext values, no placeholders, so we use the heuristic
//     name match)
// Errors are aggregated and returned but DO NOT block the caller's
// state-delete (the caller logs and proceeds; orphan secrets are reported
// by `wfctl infra audit-state-secrets`).
func Revoke(
    ctx context.Context,
    provider secrets.Provider,
    resourceName string,
    mergedKeys []string,
) error

// IsPlaceholder reports whether v is a "secret_ref://..." placeholder string.
// Used by drift comparison and display paths.
func IsPlaceholder(v any) bool
```

The placeholder format `secret_ref://<resource>_<key>` is **distinct** from the existing `secret://<key>` convention in `secrets/secrets.go:13`. Justification: `secret://` is **user-supplied** in config (resolved by `Resolver.Resolve`); `secret_ref://` is **engine-generated** in state (never user-typed). The two-namespace split lets `audit-state-secrets` (§4.7) detect a state record where someone hand-typed `secret://X` (probably a bug — they should be storing the resolved value, not a config reference). Verified `grep -rn "secret_ref://"` returns zero hits in workflow + workflow-plugin-* repos as of `8de95b4f`.

### 4.2 Wiring at the state-write boundary

There are **five** state-write call sites (enumerated by `grep -n "store.SaveResource\|Outputs:.*r.Outputs\|Outputs:.*live.Outputs" cmd/wfctl/*.go`):

| # | File:Line | Context | Routing |
|---|---|---|---|
| 1 | `infra_apply.go:550-557` | `applyWithProviderAndStore` post-Apply | **Route** (Create/Update outputs) |
| 2 | `infra_apply.go:1032-1040` | In-process apply path post-Apply | **Route** (Create/Update outputs) |
| 3 | `infra_apply.go:637` | `adoptExistingResources` (post-Read) | **Sanitize-only** (§4.4) |
| 4 | `infra_apply.go:705` | `resourceStateFromLiveOutput` builder | **Sanitize-only** (§4.4) |
| 5 | `infra_refresh_outputs.go:244` | `runInfraRefreshOutputs` post-Read | **Sanitize-only** (§4.4) |

"**Route**" = `sensitive.Route(ctx, provider, rs.Name, out)`; sanitized goes to state, hydrated returned to caller for in-process hand-off.

"**Sanitize-only**" = if an output value matches a routed-secret name pattern (i.e., a sensitive key is present), DO NOT call `provider.Set` (Read paths must not pollute the secret store with potentially-stale cloud values, per cache-pollution adversarial finding); instead, replace with placeholder if a routed secret already exists in provider OR drop the field if no routed secret is registered. The latter case is the "Read can't re-emit" path (DO `secret_key`).

A new shared helper centralises the call-site logic:

```go
// persistResourceWithSecretRouting builds rs from out, routes (or sanitizes,
// per mode) sensitive fields, calls store.SaveResource, and returns the
// hydrated routed-secret map for in-memory hand-off to post-apply consumers
// (mode=apply only; mode=read returns nil hydrated map).
//
// On store.SaveResource failure AFTER provider.Set succeeded (mode=apply),
// the helper invokes driver.Delete to compensate the partial cloud-resource
// creation, then returns a wrapped error naming both the original
// SaveResource failure and the compensating-Delete outcome. Operators reading
// the error can distinguish "save failed, cloud cleaned up" from "save failed,
// cloud orphan persists" — the latter requires manual intervention.
func persistResourceWithSecretRouting(
    ctx context.Context,
    store interfaces.IaCStateStore,
    provider secrets.Provider, // may be nil if no driver emits sensitive
    driver interfaces.ResourceDriver,
    rs interfaces.ResourceState,
    out interfaces.ResourceOutput,
    mode persistMode, // apply | read
) (hydrated map[string]string, err error)
```

`ApplyPlan` itself stays unchanged.

### 4.3 Router activation policy

The `provider` is constructed at apply entry IFF `secretsCfg != nil` via the existing `resolveSecretsProvider`. If `secretsCfg == nil` AND any driver returns a `*ResourceOutput` with non-empty `Sensitive` map, the helper returns:

```
state write rejected: resource "coredump-deploy-key" has sensitive output keys [secret_key]
but no secrets.Provider is configured. Configure secrets.* in your workflow config or
add `secrets: { provider: env }` for ad-hoc local runs.
```

**Default for ad-hoc/local runs**: the helper documentation recommends `secrets: { provider: env }` (uses `EnvProvider` from `secrets/secrets.go:51`) as the no-config-friendly fallback so the bar to entry is low. This is documented in `DOCUMENTATION.md` as part of the §11 doc-update task.

### 4.4 Read / adoption / refresh behaviour (Sanitize-only)

`adoptExistingResources` and `runInfraRefreshOutputs` call `driver.Read` and persist live `ResourceOutput`. For these paths:

- **`Route` is NOT called** — Read may return cached/stale values; writing them to `secrets.Provider` would risk overwriting a still-valid routed secret with a stale clone (cache-pollution adversarial finding).
- **Sanitize-only**: when an output's key matches a known routed-secret pattern (i.e., the same resource has had a previous Apply that routed this key), insert the `secret_ref://<resource>_<key>` placeholder. Otherwise (no prior routed secret), pass through. For DO-style "Read can't re-emit secret_key", this means: state record on subsequent refreshes still has the placeholder from the original Apply; live Read has no `secret_key`; sanitized state preserves the placeholder.
- **Discovery of "known routed-secret pattern"**: the helper consults the **pre-existing** state record (`store.GetResource(ctx, rs.Name)` if available) and inherits any `secret_ref://` placeholders present. New keys that the driver newly marks `Sensitive` during Read are **dropped** (not routed) — explicit conservative bias.

This is the **idempotent re-Read invariant**: refresh/adoption never loses or corrupts a previously-routed secret, and never writes anything new to `secrets.Provider`.

### 4.5 Compensating-Delete on partial failure

Per adversarial finding #6: if `provider.Set` succeeds and `store.SaveResource` fails, naive rerun of Apply will mint a NEW cloud resource (e.g., new DO Spaces access key), leaking the old one. The helper avoids this with a **compensating Delete**:

1. `driver.Create` (or Update) succeeds → `out` produced.
2. `provider.Set(<resource>_<key>, value)` for each sensitive field — if any fails, return error WITHOUT calling `SaveResource` and WITHOUT rerunning Set for already-set fields. Operator reruns; idempotent overwrite is fine for fields not yet attempted; partially-Set fields are overwritten with the same value on rerun.
3. `store.SaveResource(ctx, rs)` — if this fails AND we already called Set:
   1. Call `driver.Delete(ctx, refFromRS(rs))` to clean up the cloud resource.
   2. Call `provider.Delete(ctx, <resource>_<key>)` for each Set we made, to clean up the orphan secret.
   3. Return a wrapped error: `state save failed (compensating delete: <ok|err>): <orig save err>`.

If the compensating Delete also fails, the error names what's still leaked — operators see exactly what to clean up manually. This is the "engine never silently leaks" contract.

### 4.6 Write-only-provider handling (`secrets.Provider.Get == ErrUnsupported`)

`GitHubSecretsProvider.Get` returns `ErrUnsupported` (`secrets/github_provider.go:52`). This is by GitHub Actions API design — secrets are write-only after Set. Implications:

- **Route** does not call `Get`; only `Set` and `Delete`. Compatible with write-only providers.
- **Hydrated in-memory hand-off** (the replacement for the rejected `Restore` API): the engine holds the just-Set value in `hydrated map[string]string` at apply time. Post-apply consumers in the **same process** receive the map and use it directly. State-cold-rehydrate is **not supported** — operators on GitHub-only hosts who want post-apply consumers to access routed secrets must run those consumers within the same `wfctl infra apply` invocation (already the case for `syncInfraOutputSecrets`).
- **Cold-start consumers** (e.g., a workflow-cloud server reading state weeks later) consume routed secrets via the existing `secret://<key>` resolver directly. Documented secret names are stable: `<resource_name>_<output_key>`. On a write-only provider, cold-start consumers cannot read routed secrets; they must re-apply or accept the constraint. This is a **fundamental property of the write-only provider** (GitHub Actions design), not a workflow limitation.

### 4.7 New audit surface — `wfctl infra audit-state-secrets`

Adversarial-review Critical finding: the original design cited `wfctl infra audit-secrets` as recovery, but that command audits CONFIG (`secrets.generate` block), not state-vs-provider. Adding a new command:

```
wfctl infra audit-state-secrets [--config infra.yaml] [--prune]
  Walks every entry in IaCStateStore. For each Outputs[k] that is:
    - a "secret_ref://<name>" placeholder → confirm secrets.Provider has <name> via List() or Get()
    - a plaintext value matching secrets.DefaultSensitiveKeys() → flag legacy plaintext (manual rotation needed)
    - a "secret://<key>" string (user-typed bug) → flag mistaken config reference in state
  Then walks secrets.Provider.List() (where supported) for any "<resource>_<key>" name whose <resource> is NOT in IaCStateStore → orphan, candidate for prune.

  --prune: deletes confirmed orphan secrets from secrets.Provider (gated on --yes for non-interactive).

Exit codes:
  0  no findings
  1  findings (legacy plaintext, missing routed values, orphan secrets)
  2  audit error (cannot read state, provider unsupported, etc.)
```

This is the recovery surface for: (a) failure-window orphans (§4.5 on success-after-Set-but-before-Save), (b) Revoke failures (§4.5 of original design — moved to compensating Delete in rev1), (c) legacy plaintext-state migration triage. Distinct from `audit-secrets` (config audit); operators run both.

### 4.8 Drift-detection compatibility

After this PR, `state.Outputs` has `secret_ref://...` placeholders; live `ResourceOutput.Outputs` from `Read` has either no value (DO `secret_key`) or plaintext. Naive `driver.Diff(desired, current)` would report drift on every refresh.

**Plan task**: in every Diff/drift call site, mask sensitive keys before comparison. Specifically:
- `cmd/wfctl/infra_apply_refresh.go` — refresh loop's Diff comparison
- `iac/wfctlhelpers` — none currently call Diff; future-proof
- Any provider's `DetectDrift` — out of scope for this PR (per-provider; documented as follow-up; existing default behaviour already returns nil drift on most types since drift detection is opt-in via `DriftConfigDetector` per `interfaces/iac_provider.go:119`)

The masking helper:

```go
// MaskSensitiveForDiff returns copies of desired and current with sensitive keys
// (any key in current matching "secret_ref://*" prefix, OR named in
// driver.SensitiveKeys()) elided from both, so drift comparison sees a
// consistent view. Idempotent for non-routed outputs.
func MaskSensitiveForDiff(driverKeys []string, desired, current map[string]any) (map[string]any, map[string]any)
```

### 4.9 In-memory hand-off to post-apply consumers

`syncInfraOutputSecrets` (currently at `cmd/wfctl/infra.go:1450`) and the post-apply pipeline-run consumer both consume `state.Outputs`. After this PR they receive sanitized state with placeholders. The new `hydrated` map flows through:

```go
// runInfraApply (cmd/wfctl/infra.go) flow:
//   1. apply with persistResourceWithSecretRouting → returns hydrated map per resource
//   2. accumulate per-resource hydrated maps into runHydrated map[resourceName]map[secretName]value
//   3. pass runHydrated into post-apply consumers
hydratedAll := map[string]map[string]string{} // resource -> secret_name -> value
// ... applyWithProviderAndStore returns hydratedAll
syncInfraOutputSecrets(ctx, secretsCfg, secretsProvider, states, wfCfg, envName, hydratedAll)
```

`syncInfraOutputSecrets` checks `hydratedAll[moduleName][outputKey]` BEFORE `state.Outputs[outputKey]`. If the placeholder is in state and hydrated map has the value, use the hydrated value. If the placeholder is in state and hydrated map is empty (not this-process's apply), the consumer either fetches from `secrets.Provider.Get` (if supported) or skips with a documented warning.

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
                │   1. sanitized, hydrated, err =                 │
                │        sensitive.Route(ctx, provider, rs.Name, out) │
                │      → for each k in out.Sensitive where        │
                │           out.Outputs[k] is present:            │
                │           secrets.Provider.Set("<resource>_<k>",│
                │             out.Outputs[k])                     │
                │           sanitized[k] = "secret_ref://<resource>_<k>" │
                │           hydrated["<resource>_<k>"] = value    │
                │      → other keys pass through to sanitized     │
                │   2. rs.Outputs = sanitized                     │
                │   3. err := store.SaveResource(ctx, rs)         │
                │      on err: compensating Delete (§4.5)         │
                │   4. return hydrated to caller for hand-off     │
                └────────────────────┬────────────────────────────┘
                                     │ hydrated map
                                     ▼
                ┌─────────────────────────────────────────────────┐
                │  syncInfraOutputSecrets / pipeline-run          │
                │  reads (resource, key) from hydrated FIRST,     │
                │  falls back to provider.Get if absent           │
                └─────────────────────────────────────────────────┘
```

## 6. Failure modes

| Failure | Behaviour |
|---|---|
| `secrets.Provider.Set` fails for the first routed key | Return error from `Route`; no state written; no further Set calls. Operator reruns; idempotent overwrite is fine. |
| `secrets.Provider.Set` fails for a later key (after some succeeded) | Return error from `Route`; no state written. Earlier-Set values are NOT cleaned up automatically (operator reruns; idempotent overwrite is fine). |
| `store.SaveResource` fails after Set succeeded | Compensating `driver.Delete` + `provider.Delete` for each routed key; if compensation succeeds, error names "save failed, cloud cleaned up"; if compensation fails, error names exact orphans. Operator runs `wfctl infra audit-state-secrets --prune`. |
| `secrets.Provider` not configured AND driver emits sensitive outputs | Hard fail at the persist boundary with named resource + remediation pointing to `secrets: { provider: env }` for local runs. |
| `provider.Get` fails / unsupported (write-only host) for in-memory hand-off path | Engine uses hydrated in-memory map (populated by the same-process Apply). Cold-start cross-process consumers fall back to provider.Get directly via `secret://<key>` resolver (with a clear "write-only provider, cannot rehydrate" message if Get unsupported). |
| `Revoke` (compensating-delete branch) fails | Aggregated error returned but state-delete proceeds. `audit-state-secrets --prune` is recovery. |
| Plugin returns `Sensitive: {k: true}` but `Outputs[k]` absent | Route silently skips that key; no Set call; **no placeholder inserted in sanitized either** (idempotent re-Read; pre-existing placeholder from prior Apply is preserved by Sanitize-only Read paths in §4.4). |
| Plugin emits plaintext sensitive output without setting `Sensitive` flag | Plaintext leaks to state. **Mitigation**: `wfctl infra audit-state-secrets` detects via `DefaultSensitiveKeys()` heuristic match. Plugin reviewer is the long-term fix. Not a regression. |
| Backward-compat: existing state record has plaintext `secret_key` | Sanitize-only Read paths leave plaintext alone; subsequent Apply (Route) replaces with placeholder. Operator-driven rotation cleans up legacy state via the standard rotate flow. |
| Drift detection: state has placeholder, live has no value (DO secret_key) | `MaskSensitiveForDiff` elides sensitive keys from both sides before Diff. No false-positive drift. |
| Drift detection: state has placeholder, live has plaintext (e.g., re-emitting connection_string) | Same masking applies; drift on these fields is suppressed. Drift in **non-sensitive** fields is still detected normally. |
| Cache-pollution: Read returns stale-cached secret_key | Sanitize-only Read paths NEVER write to provider. The routed value in provider is whatever the most recent successful Apply Set; never overwritten by Read. |

## 7. Testing

### Unit (`iac/sensitive/route_test.go`)

- `Route` with no Sensitive map → outputs verbatim; hydrated empty.
- `Route` with `Sensitive: {k:true}` and `Outputs[k] = "secret"` → `provider.Set("<res>_<k>", "secret")` called once + sanitized has `secret_ref://<res>_<k>` + hydrated has `<res>_<k>: secret`.
- `Route` with sensitive key absent from `Outputs` → no `Set` call; no placeholder; hydrated does not include that key.
- `Route` with `provider.Set` error → returns error; no further Set calls.
- `Route` with `provider == nil` AND non-empty Sensitive map → returns error naming the resource + keys.
- `Route` with empty resourceName → returns error (defensive — must be canonical name).
- `Revoke` calls `provider.Delete` for each key; aggregates errors (does not return on first error).
- `IsPlaceholder("secret_ref://x")` → true; non-prefix string → false; non-string → false.

### Integration (`cmd/wfctl/infra_apply_sensitive_routing_test.go`)

- Apply with stub driver returning sensitive outputs + env-provider → state record has placeholders; env vars contain values; hydrated map returned.
- Apply with sensitive driver but no `secretsCfg` → returns hard-fail error naming resource + sensitive keys.
- Re-apply (idempotent) → second Set overwrites; placeholder unchanged; hydrated map identical.
- `store.SaveResource` failure injected → compensating Delete observed (driver Delete + provider Delete called); error names compensation outcome.
- Adoption path with stub driver returning Sensitive map → Sanitize-only behaviour: no Set call; pre-existing state placeholder preserved; new sensitive key dropped.
- Refresh path with same → identical Sanitize-only behaviour.
- Delete path → Revoke calls observed for both placeholder-derived keys AND legacy `DefaultSensitiveKeys` heuristics.
- `syncInfraOutputSecrets` with hydrated map for current-apply secrets → uses hydrated value; without hydrated → falls back to provider.Get.
- Drift detection on placeholder-state vs. live-without-secret-key → no drift reported (`MaskSensitiveForDiff` working).

### Local CI gates (per `feedback_local_ci_validation_for_ci_touching_tasks`)

Before requesting review, implementer runs:

- `go test ./iac/sensitive/... ./cmd/wfctl/... ./secrets/...`
- `go test -race ./iac/sensitive/... ./cmd/wfctl/...`
- `go vet ./...`
- `golangci-lint run ./iac/sensitive/... ./cmd/wfctl/...`

## 8. Migration & rollback

### Migration

- Plugins: no code change required to keep working as-is. Plugins that want engine-side routing add `Sensitive: {k: true}` to their `ResourceOutput` returns on Create/Update.
- DO plugin: separate PR; not in this scope. Adds `Sensitive: {"secret_key": true}` to `SpacesKeyDriver.Create`.
- Operators: **no action required for greenfield envs**. Operators with pre-existing state records run `wfctl infra audit-state-secrets` (new in this PR) to enumerate plaintext-sensitive keys, then rotate via the standard `wfctl infra bootstrap --force-rotate <name>` flow (which under v0.27.0 will route to provider via the new path).

### Rollback

This PR affects runtime apply behaviour (state-write). Rollback path:

1. **Pin to v0.26.x** at `setup-wfctl@vN`. The engine reverts to plaintext-state behaviour.
2. **State records written under v0.27.0** still contain `secret_ref://...` placeholders. v0.26.x consumers do NOT understand this; they will treat the placeholder as the literal value (e.g., `infra_output` generator copies "secret_ref://..." into a downstream secret). Recovery: rotate the affected secrets via `wfctl infra bootstrap --force-rotate <name>` running v0.27.0 first to generate plaintext state, OR manually edit the state record (filesystem JSON) to inline the value from `secrets.Provider`. Documented runbook in §11 doc-update task.
3. The new package `iac/sensitive`, helper `persistResourceWithSecretRouting`, and `audit-state-secrets` command are additive; reverting the call sites to v0.26.x literal `Outputs: r.Outputs` shape is a one-commit revert of `infra_apply.go` + `infra_refresh_outputs.go`.

## 9. Assumptions

The following assumptions are load-bearing.

1. **Single configured `secrets.Provider` per apply run.** `resolveSecretsProvider(secretsCfg)` returns one provider.
2. **`secrets.Provider.Set` is idempotent overwrite** for production providers. Verified: `EnvProvider.Set` → `os.Setenv`; `FileProvider.Set` → `os.WriteFile`; `GitHubSecretsProvider.Set` → `PUT /secrets/<key>` (RFC: PUT is idempotent); Vault Transit / AWS Secrets Manager — overwrites on `Set` per provider docs.
3. **Plugin authors will populate `ResourceOutput.Sensitive` on Create/Update for sensitive outputs.** Without it, plaintext leaks to state. `audit-state-secrets` is the detection surface; workflow-plugin-reviewer skill update is the long-term fix.
4. **`secret_ref://` is a free namespace.** Verified: zero hits in workflow + workflow-plugin-* repos as of `8de95b4f`.
5. **State records on disk / in cloud-storage are NOT human-edited.** A plaintext value of literally `"secret_ref://x"` does not occur. Same trust assumption as `secret://...` config references.
6. **Same-process hand-off is sufficient for `syncInfraOutputSecrets` and pipeline-run.** Verified by reading `cmd/wfctl/infra.go:1414-1450`: both run within the same `runInfraApply` invocation.
7. **Cold-start consumers know secret names.** `<resource_name>_<output_key>` is stable and documented. Operators write `secret://coredump-deploy-key_secret_key` in dependent configs to pull from `secrets.Provider`.
8. **Read paths can detect known routed-secret keys via pre-existing state placeholders.** §4.4's Sanitize-only logic uses `store.GetResource` to read the prior state; `IaCStateStore.GetResource` is a stable interface method (`interfaces/iac_state.go:16`).

## 10. Top doubts (after rev1)

1. **Plugin discovery of the Sensitive contract** — plugins that don't set `Sensitive` silently leak plaintext to state. `audit-state-secrets` heuristic detection is best-effort. Long-term fix is workflow-plugin-reviewer skill check; out of scope for v0.27.0 but flagged as follow-up.
2. **Compensating Delete edge cases** — if `driver.Delete` requires a `ProviderID` that's only available in `out` (not yet persisted to state), the helper threads it correctly; but if Delete itself partially fails (e.g., DO API rate-limit during cleanup), the orphan persists. `audit-state-secrets` detects. Acceptable.
3. **Drift masking false-negatives** — `MaskSensitiveForDiff` masks sensitive keys from both sides. If an operator rotates a routed secret out-of-band (manually deletes from `secrets.Provider`), the state placeholder still exists but the value is gone. Drift detection won't catch this; `audit-state-secrets` will (as "missing routed value"). Acceptable; documented.

## 11. Estimated scope (rev1)

| Component | Files touched | LoC delta |
|---|---|---|
| New `iac/sensitive` package (Route, Revoke, IsPlaceholder, MaskSensitiveForDiff) | 2 (`route.go`, `route_test.go`) | +500 |
| State-write helper + 5 call-site refactors | 2 (`cmd/wfctl/infra_apply.go`, `infra_refresh_outputs.go`) | +120 / -40 |
| Hydrated-map plumbing through `runInfraApply` | 2 (`cmd/wfctl/infra.go`, `cmd/wfctl/infra_output_secrets.go`) | +60 |
| Compensating-Delete logic in helper | (within `infra_apply.go`) | +50 |
| New `wfctl infra audit-state-secrets` command | 2 (`cmd/wfctl/infra_audit_state_secrets.go`, `_test.go`) | +400 |
| Drift masking helper + wiring | 1 (`cmd/wfctl/infra_apply_refresh.go`) | +60 |
| Integration tests | 1 (`cmd/wfctl/infra_apply_sensitive_routing_test.go`) | +400 |
| Doc update — `DOCUMENTATION.md` + `docs/WFCTL.md` | 2 | +80 |
| **Total** | ~12 files | ~+1.6k net |

## 12. Out-of-scope / follow-ups

1. DO plugin update to set `Sensitive: {"secret_key": true}` on `SpacesKeyDriver.Create` — separate PR after v0.27.0 tag.
2. AWS, GCP, Azure plugins — same pattern, separate PRs each.
3. workflow-plugin-reviewer skill update — add "plugin-sensitive-keys-declared" check (lint surface).
4. State-record migration tool to retroactively route legacy plaintext secrets — separate work.
5. UI / wfctl display of `secret_ref://...` placeholders — should they render as `(routed: <key>)`? Cosmetic, follow-up.
6. Drift detection for **provider-side** `DetectDrift` paths (per-provider plugins) — out of scope; per-provider follow-up; existing default behaviour returns nil.
