# wfctl Deploy-Log Observability — Design

**Status:** Approved (autonomous pipeline, 2026-04-24)

**Target release:** workflow v0.18.10 + workflow-plugin-digitalocean v0.7.8

**Goal:** When a `wfctl infra apply` or `wfctl ci run --phase deploy` fails or its health check times out, wfctl automatically surfaces provider-side context (deployment phase, build/pre-deploy/run logs, root cause) in CI output instead of leaving the operator to hunt in the provider's web console. Structured per-phase output uses CI-provider-agnostic grouping (GitHub Actions `::group::`, GitLab `section_start`, Jenkins/CircleCI plain separators) and writes a summary to `$GITHUB_STEP_SUMMARY` (and equivalents) on completion or failure.

**Why:** BMW staging deploy on 2026-04-24 hung for ten minutes with the single error line `plugin health check "bmw-staging": timed out waiting for healthy; last status: no deployment found`, while DO's App Platform pre-deploy job was silently failing with a migration-format error. wfctl had no visibility past its own health-check poll. User mandate: "we shouldn't have to hunt for this info."

---

## Approach

Introduce an optional **`Troubleshooter`** interface on `ResourceDriver`. Drivers that can explain their own failures (DO App Platform, in this release; AWS ECS, GCP Cloud Run, K8s in future releases) implement it. wfctl calls it automatically after any terminal failure — health check timeout, driver error — and renders the returned `[]Diagnostic` in CI output before the original error.

This is **narrower** than a generic `StreamLogs` API: Troubleshoot returns a *synchronous batch* of recent provider-side events (deployments, job runs, errors), each with enough context — phase, root cause, timestamp, and log tail — to explain the failure. No streaming, no bidirectional protocols, no cross-plugin coordination. The interface is optional; drivers that don't implement it silently pass through (wfctl keeps current behavior).

### Why not streaming?

Streaming logs over go-plugin gRPC requires a new bidirectional method, context cancellation plumbing, and careful test harnesses. The value is real-time display during long builds, which is nice-to-have. The value we actually need — "when something fails, show me why" — is served by a synchronous batch fetched *after* the failure, at roughly the same cost as the existing HealthCheck poll. Defer streaming to v0.19.0 when `IaCProvider.StreamLogs` can be designed alongside the plugin-manifest split (task #42) and provider-agnostic CI summary (task #63).

### Why not a new top-level "log fetch" command?

A separate `wfctl infra logs <resource>` command would duplicate the driver-dispatch plumbing for a use case that's almost always post-failure. Making wfctl call Troubleshoot *automatically* on failure means the common case is zero-config. If ad-hoc log fetching is later useful, a CLI wrapper is a thin shell over the same interface.

---

## Architecture

### Interface (workflow/interfaces)

```go
// Diagnostic is a single troubleshooting finding returned by a Troubleshooter.
type Diagnostic struct {
    ID     string    `json:"id"`              // provider-side id (deployment id, task arn)
    Phase  string    `json:"phase"`           // terminal or current phase
    Cause  string    `json:"cause"`           // human-readable root cause
    At     time.Time `json:"at"`              // when the event occurred
    Detail string    `json:"detail,omitempty"` // optional verbose tail (log excerpt, stack)
}

// Troubleshooter is an optional interface on ResourceDriver.
type Troubleshooter interface {
    Troubleshoot(ctx context.Context, ref ResourceRef, failureMsg string) ([]Diagnostic, error)
}
```

Kept deliberately terse: `ID` is a click-through hint for operators who still want the console; `Phase` is the DO/AWS terminology mapped to driver-agnostic strings (`"pre_deploy"`, `"build"`, `"run"`); `Cause` is the one-liner summary; `At` is the event timestamp; `Detail` is an optional multi-line log tail for the case where the cause alone isn't enough (e.g., a stack trace).

### gRPC plumbing (workflow plugin/grpc)

`Troubleshooter` is optional: dispatcher calls a generic `InvokeMethod("Troubleshoot", ...)` with typed args, checks for an `UNIMPLEMENTED` response, treats it as "driver doesn't support troubleshooting" and silently falls back. This matches the existing pattern for `BootstrapStateBackend` (v0.7.4) and the `Scale` method — no breaking change for plugins that don't implement it.

### DO plugin (workflow-plugin-digitalocean)

`AppPlatformDriver.Troubleshoot(ctx, ref, failureMsg)`:

1. Fetch the app via `client.Get(ctx, ref.ProviderID)`.
2. Iterate `app.ActiveDeployment`, `app.InProgressDeployment`, `app.PendingDeployment`. Pick whichever is non-nil (prefer InProgress, then Pending, then Active — most recent wins).
3. Call `client.ListDeployments(ctx, appID)` to get recent deployments.
4. For each relevant deployment (latest 3), fetch logs:
   - `GET /v2/apps/{id}/deployments/{dep_id}/logs?type=pre_deploy&follow=false`
   - `GET /v2/apps/{id}/deployments/{dep_id}/logs?type=build&follow=false`
   - `GET /v2/apps/{id}/deployments/{dep_id}/logs?type=run&follow=false`
   - `GET /v2/apps/{id}/deployments/{dep_id}/logs?type=deploy&follow=false`
5. Synthesize one `Diagnostic` per phase that had activity, with `Detail` = tail of that phase's log (last 100 lines, 4KB cap).
6. Extract `Cause` from the first non-ACTIVE phase: look for patterns like `exit code N`, `Error:`, `failed to parse`, `connection refused`.

Output ordering: most recent deployment first; within a deployment, phases in causal order (pre_deploy → build → deploy → run).

Resource types other than `infra.app_platform` are out of scope for v0.7.8 — their `Troubleshoot` returns `(nil, nil)` (no findings).

### wfctl call sites (workflow/cmd/wfctl)

Two hook points:

**Hook 1 — `wfctl ci run --phase deploy` (cmd/wfctl/deploy_providers.go)**
After `pluginDeployProvider.Deploy()` returns error OR `HealthCheck` polling times out, check whether the driver implements Troubleshooter (via gRPC probe). If yes, call `Troubleshoot(ctx, ref, originalErr.Error())` with a 30-second timeout, render diagnostics, then propagate the original error.

**Hook 2 — `wfctl infra apply` (cmd/wfctl/infra_apply.go)**
When `driver.Apply()` or `HealthCheck()` fails, same logic.

Render helper lives in `cmd/wfctl/ci_output.go` (new file) and wraps the per-CI-provider group emission:

```go
func EmitDiagnosticGroup(w io.Writer, resource string, diags []Diagnostic) {
    g := detectCIProvider() // GITHUB_ACTIONS | GITLAB_CI | JENKINS | CIRCLECI | "plain"
    g.GroupStart(w, fmt.Sprintf("Troubleshoot: %s", resource))
    for _, d := range diags {
        fmt.Fprintf(w, "  [%s] %s — %s (at %s)\n", d.Phase, d.ID, d.Cause, d.At.Format(time.RFC3339))
        if d.Detail != "" {
            fmt.Fprintln(w, "  " + strings.ReplaceAll(d.Detail, "\n", "\n  "))
        }
    }
    g.GroupEnd(w)
}
```

### CI-provider summary (GitHub Step Summary and equivalents)

In addition to streaming diagnostics to stdout, wfctl writes a compact Markdown summary to `$GITHUB_STEP_SUMMARY` at process exit (success OR failure):

```markdown
## wfctl: deploy staging — FAILED

**Resource:** bmw-staging (digitalocean/app_platform, id=f8b6200c-…)
**Phase:** pre_deploy — ERROR
**Root cause:** `workflow-migrate up: first .: file does not exist`
**Deployment:** https://cloud.digitalocean.com/apps/f8b6200c-…/deployments/abc-123

### Phase timings
| Phase | Status | Duration |
|---|---|---|
| build | SUCCESS | 2m 14s |
| pre_deploy | ERROR | 0m 03s |
| deploy | (not reached) | — |
| run | (not reached) | — |

<details><summary>pre_deploy log tail (12 lines)</summary>

```
workflow-migrate up: first .: file does not exist
Error: exit status 1
```

</details>
```

The summary writer is a thin wrapper:
- GHA: append to `$GITHUB_STEP_SUMMARY` file (if set)
- GitLab: emit via `$CI_JOB_ANNOTATIONS` (if set) or `section_start` + plain output
- Jenkins / CircleCI: plain text to stdout (no native summary mechanism)
- Local / unknown: skip

### Detection helper

```go
// detectCIProvider returns the provider-specific group emitter. Defaults to
// plain-text separators when running outside a known CI.
func detectCIProvider() CIGroupEmitter {
    switch {
    case os.Getenv("GITHUB_ACTIONS") == "true":
        return githubEmitter{summaryPath: os.Getenv("GITHUB_STEP_SUMMARY")}
    case os.Getenv("GITLAB_CI") == "true":
        return gitlabEmitter{}
    case os.Getenv("JENKINS_HOME") != "":
        return jenkinsEmitter{}
    case os.Getenv("CIRCLECI") == "true":
        return circleCIEmitter{}
    default:
        return plainEmitter{}
    }
}
```

---

## Data flow (BMW deploy failure, post-v0.18.10)

```
wfctl ci run --phase deploy --env staging
  → pluginDeployProvider.Deploy(…)
      → driver.Update(ref) → DO CreateDeployment(forceBuild=true) → deploymentID
      → driver.HealthCheck(ref) → poll Active/InProgress/Pending deployment phase
      → 10 min → phase="ERROR" (pre_deploy failed)
      → returns error: "plugin health check: …"
  → deploy_providers.go catches the error
  → probe driver for Troubleshooter → true
  → driver.Troubleshoot(ctx, ref, errMsg)
      → DO plugin fetches app, picks latest deployment (InProgress or most recent failed)
      → fetches pre_deploy + build + run logs
      → synthesizes 3 Diagnostics (pre_deploy ERROR, build SUCCESS, run NOT_STARTED)
  → wfctl emits ::group:: blocks with each Diagnostic's ID, phase, cause, detail
  → wfctl writes Markdown summary to $GITHUB_STEP_SUMMARY
  → wfctl exits with original error + exit code 1
```

Operator opens the GHA run, sees the collapsed "Troubleshoot: bmw-staging" group, expands it, and immediately reads:

```
[pre_deploy] dep-abc-123 — workflow-migrate up: first .: file does not exist (at 2026-04-24T17:42:45Z)
  time="…" level=error msg="golang-migrate up: first .: file does not exist"
  Error: exit status 1
  command exited with status 1
```

Diagnosis time: seconds, not a console trip.

---

## Testing

**DO plugin (workflow-plugin-digitalocean):**
- Unit: mock `godo.Client` returning canned deployment + logs; assert Diagnostic fields, phase ordering, Cause extraction from common error patterns.
- Integration: tabletest matrix over pre_deploy failure, build failure, run failure (container exit), timeout during build, multi-deployment history.

**wfctl (workflow):**
- `TestDeployFailure_EmitsDiagnostics` — fake ResourceDriver implementing Troubleshooter returns known Diagnostics; assert `::group::` blocks and summary file contain expected text.
- `TestDeployFailure_WithoutTroubleshooter` — fake ResourceDriver with no Troubleshooter; assert wfctl behaves identically to v0.18.9 (no crash, original error preserved).
- `TestCIProvider_Detection` — env-var matrix for GHA/GitLab/Jenkins/CircleCI/plain.
- `TestStepSummary_Markdown` — golden-file comparison of rendered summary.

**End-to-end (deferred):** BMW staging deploy post-v0.18.10 bump should surface the pre-deploy migration error in its GHA output within the same run where the deploy fails. Validate manually after release.

---

## Rollout

**Phase 1 — workflow v0.18.10 interface + wfctl wiring:**
- Commit `Troubleshooter`/`Diagnostic` in `interfaces/iac_resource_driver.go` (already drafted).
- Add `cmd/wfctl/ci_output.go` (CIGroupEmitter, provider detection, summary writer).
- Wire Troubleshoot call at deploy-provider and infra-apply failure paths.
- Tests (unit-only; no DO integration yet).
- Open PR, review, merge, tag v0.18.10.

**Phase 2 — DO plugin v0.7.8 implementation:**
- Implement `AppPlatformDriver.Troubleshoot` via godo deployments + logs API.
- Add gRPC dispatch case (InvokeMethod `"Troubleshoot"`).
- Unit tests with mocked godo.Client.
- Open PR, review, merge, tag v0.7.8.

**Phase 3 — BMW consumer bump:**
- BMW bumps `setup-wfctl` pin to v0.18.10.
- BMW bumps workflow-plugin-digitalocean pin to v0.7.8.
- Single PR on BMW repo; no infra.yaml or deploy.yml changes needed — observability is purely additive.

---

## Success criteria

- Running `wfctl ci run --phase deploy` against a DO App Platform target that fails pre_deploy surfaces a clearly-labeled diagnostic block in CI output and a Markdown summary in `$GITHUB_STEP_SUMMARY` with the root-cause error line from the pre_deploy log, without the operator visiting the DO console.
- Plugins that don't implement Troubleshooter (AWS/GCP/Azure/tofu/ci-generator/supply-chain) continue to work with v0.18.10 without modification.
- wfctl exit codes and error text for callers relying on grep-based log parsing remain unchanged (observability is additive, not substitutive).
- BMW retry on v0.18.10 + DO plugin v0.7.8 + still-broken-migrations configuration shows the migration-format error in its GHA output within 30 seconds of the 10-minute health-check timeout.

---

## Non-goals

- Generic `IaCProvider.StreamLogs` interface — deferred to v0.19.0 (bundles with plugin-manifest split, task #42, and CI summary scope, task #63).
- AWS / GCP / Azure / tofu Troubleshoot implementations — DO only for v0.7.8; other drivers silently no-op.
- Real-time log display during long builds — synchronous post-failure batch is enough for v0.18.10.
- State-heal behavior (task #67) — orthogonal; stays in the backlog.
- Polling deployment logs during the happy path (success case) — only called on failure.
- Log redaction beyond the existing `SensitiveKeys()` mechanism — Diagnostics surface what the driver knows; if a plugin leaks secrets in `Detail`, that's a plugin-side bug to fix separately.
