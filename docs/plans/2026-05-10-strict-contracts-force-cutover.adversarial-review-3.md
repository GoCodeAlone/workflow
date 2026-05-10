---
status: approved
review_cycle: 3
target: docs/plans/2026-05-10-strict-contracts-force-cutover.md
target_commit: 0679a83f
phase: plan
date: 2026-05-10
verdict: FAIL
---

# Adversarial Review — Strict-Contracts Force-Cutover Plan (Cycle 3, plan-phase)

**Phase:** plan (cycle 3)
**Artifact:** `docs/plans/2026-05-10-strict-contracts-force-cutover.md` (commit `0679a83f`)
**Cycle 2 baseline:** `docs/plans/2026-05-10-strict-contracts-force-cutover.adversarial-review-2.md` (commit `582e5d59`) — verdict FAIL with 2 Critical + 3 Important.
**Cycle 1 baseline:** `docs/plans/2026-05-10-strict-contracts-force-cutover.adversarial-review-1.md` — verdict FAIL with 3 Critical + 4 Important.
**Design baseline:** `docs/plans/2026-05-10-strict-contracts-force-cutover-design.md` (rev5, 4 design-cycle PASSes).

**Verdict: FAIL.** One Critical and one Important finding remain. Per "don't nitpick" — both block the plan from working as authored.

---

## Cycle 2 finding-resolution verification

| Cycle 2 finding | Rev3 claim | Verified? |
|---|---|---|
| **C-1-NEW** Task 20-bis docker-run + plan --output for v0.14.2 fixture | Replaced with curl-binary-download + apply `--state-file` `--dry-run`; Task 22 also referenced as cleaned up | **NO — see C-1 below.** v0.14.2 binary asset DOES exist (verified via HTTP HEAD on releases asset URL), but the `--state-file` and `--dry-run` flags DO NOT EXIST in v0.14.2 `infra apply`. Task 22 STILL contains the original `docker run ghcr.io/gocodealone/wfctl:v0.14.2` invocation that cycle 2 flagged — it was never updated. The flag `wfctl infra state list --state-file` is also fictional (v0.14.2 + current main both lack it). |
| **C-2-NEW** Task 4-bis 3-line stub for ServeIaCPlugin | Concrete `IaCServeOptions` struct + signature spec added | **PARTIAL — see I-1 below.** API shape is now concrete, BUT (a) the implementation pseudo-code is structurally inconsistent with the goplugin API (verified against `go-plugin@v1.7.0/server.go:87` — `GRPCServer` field is `func([]grpc.ServerOption) *grpc.Server` factory; service registration must happen INSIDE the factory callback, not on a pre-built server passed to a "serveWithHandshake" loop); (b) PR 3 Task 9 line 989 still references `sdk.ServeOptions{...}` symbol but Task 4-bis defines the type as `sdk.IaCServeOptions` — symbol-name mismatch. |
| **I-1-NEW** cross-plugin-build matrix only does go build | Subprocess gRPC wire test added (Task 6 step lines 717-725) | **YES.** Task 6 now specifies `go build -o /tmp/do-plugin ./cmd/...` followed by `go test -tags=integration -run TestIaC_CrossPluginWireTest ./plugin/external/sdk/...` — actual subprocess + typed RPC wire exchange. Resolution sound. |
| **I-2-NEW** SUPERSEDED-NOTICE.md sibling-file insufficient | Per "don't nitpick" — accepted as documentation-only limitation | **YES (under nitpick filter).** Lead conversation drives PR cadence; downstream automation does not auto-discover work from `docs/plans/*.md`. Soft supersession is acceptable. Drop. |
| **I-3-NEW** Two-variable model hardcoded regardless of Task 20-bis result | Task 21 conditional decision-tree added at intro | **PARTIAL — see Minor note below.** The decision tree is documented at the top of Task 21 (lines 1399-1406). However, the rest of Task 21 (Files list, gh-variable-set commands, Step 2 instructions) is written for the two-variable branch unconditionally. A competent implementer would follow the conditional branch the decision tree directs them to, so this is a Minor inconsistency rather than a blocker. Drop. |

---

## Critical findings

### C-1 — Task 22 retains the SAME `docker run ghcr.io/gocodealone/wfctl:v0.14.2` invocation cycle 2 C-1-NEW flagged; AND Task 20-bis's replacement `apply --state-file --dry-run` invocation uses flags that DO NOT EXIST in v0.14.2 wfctl

**Evidence — Task 22 unchanged:**

Plan Task 22 Step 1 (lines 1488-1492):

```bash
# Run v0.14.2 wfctl against a known-good infra config; capture the generated state file
docker run --rm -v $(pwd):/work ghcr.io/gocodealone/wfctl:v0.14.2 \
  infra plan -c /work/infra.yaml --env staging --output /work/test/fixtures/state-v0.14.2.json
```

This is BYTE-FOR-BYTE identical to the cycle 2 C-1-NEW finding's evidence. The `ghcr.io/gocodealone/wfctl:v0.14.2` OCI image does not exist (verified in cycle 2: the workflow `release.yml:152` does `ko build ./cmd/server` only — no `ko build ./cmd/wfctl`). The cycle 2 finding was acknowledged in Task 20-bis ("per cycle 2 plan-phase C-1-NEW: ghcr.io/gocodealone/wfctl image doesn't exist") but the same broken invocation was left in Task 22.

Task 22 Step 2 then has:

```bash
wfctl infra state list --state-file test/fixtures/state-v0.14.2.json
```

`grep -nE '\-\-state-file|"state-file"' /Users/jon/workspace/workflow/_worktrees/strict-contracts-force-cutover/cmd/wfctl/` returns ZERO matches. The `--state-file` flag does NOT exist in current main (PR 4's v1.0.0 base) — so even if a fixture file existed, `wfctl infra state list --state-file <file>` would fail with `flag provided but not defined: -state-file`. v0.14.2 source (`/tmp/workflow-v0.14.2/cmd/wfctl/infra_state.go:50-67`) confirms the only flags accepted by `infra state list` are `--config / -c` (state is read from the configured backend, NOT from a file path).

**Evidence — Task 20-bis flag invocation broken:**

Plan Task 20-bis Step 1 (lines 1358-1364):

```bash
"$WFCTL_LEGACY_BIN" infra apply \
  -c example/iac-do/infra.yaml \
  --env staging \
  --auto-approve \
  --state-file test/fixtures/state-v0.14.2.json \
  --dry-run  # dry-run produces the would-be state without actually applying
```

Verified against `/tmp/workflow-v0.14.2/cmd/wfctl/infra.go:760-776` — the v0.14.2 `infra apply` flag set is:
- `--config / -c` (config file)
- `--auto-approve / -y` (skip confirmation)
- `--show-sensitive / -S`
- `--env`

There is NO `--state-file` flag. There is NO `--dry-run` flag. The plan invocation will fail with `flag provided but not defined: -state-file`.

The plan acknowledges this risk (line 1366): "If `--dry-run` doesn't write state, alternative: capture an existing state file from a prior real v0.14.2 deploy (operator has access to one in their staging Spaces backend)." But the alternative is offered only as a fallback for dry-run-not-writing-state, not for the larger missing-flag problem. The plan does not commit to actually using the alternative — Step 1 still authors the broken command as primary.

**Why Critical:**

The state-file-compat verification gate is THE mechanism the plan uses to prevent PR 5 cascade-block (cycle 1 I-4) and to gate Task 21's two-variable decision tree (cycle 2 I-3-NEW conditional). Both Task 20-bis (workflow PR 4 pre-flight) AND Task 22 (core-dump PR 5 CI gate) reference commands that cannot execute. The pre-flight gate is unrunnable, so:
- Cycle 1 I-4 (PR 5 cascade-block) remains unresolved — operator will discover state-format incompat at PR 5 merge time, not at PR 4 prep time.
- Cycle 2 I-3-NEW conditional (Task 21 single-vs-two-variable model) cannot select a branch because Task 20-bis cannot return a result.
- The CHANGELOG claim in PR 4 ("v1.0.0 reads v0.14.2 state cleanly, verified") cannot be substantiated.

**Resolution required:**

Pick ONE of three concrete approaches and update BOTH Task 20-bis and Task 22 in lockstep:

1. **Pre-baked fixture** — capture a real v0.14.2-produced state file from existing core-dump production state (the operator has access per the plan's own line 1366 escape hatch); check it in as `test/fixtures/state-v0.14.2.json`; remove the fixture-generation step entirely. Both Task 20-bis Step 1 and Task 22 Step 1 become "fixture is checked in; no generation step." Verification step becomes a Go unit test that reads the JSON directly + asserts schema fields, with no `wfctl` CLI invocation involved.

2. **In-process v0.14.2 build-and-call** — `go install github.com/GoCodeAlone/workflow/cmd/wfctl@v0.14.2` (via Go module proxy, which DOES work — workflow v0.14.2 is published), then run `wfctl infra apply` against a sandbox infra config that uses a fake/local state backend. But this still hits the missing `--state-file` flag problem; the actual v0.14.2 path WRITES state to whatever backend the YAML config declares, so the test would have to point the YAML's state backend at a local file location.

3. **Drop the runtime verification, use schema-pin** — instead of round-tripping a real wfctl binary, document the v0.14.2 state-file schema as a checked-in JSON Schema file (or a Go struct definition tagged "v0.14.2 schema"), and have Task 20-bis assert that v1.0.0's state-loading code accepts that schema. No fixture-generation step needed.

Approach 1 is the cheapest + most honest. The plan must commit to one in writing.

---

## Important findings

### I-1 — Task 4-bis pseudo-code creates `*grpc.Server` outside the goplugin lifecycle, but `goplugin.Serve`'s API requires service registration INSIDE the `GRPCServer func([]grpc.ServerOption) *grpc.Server` factory callback; AND PR 3 Task 9 references `sdk.ServeOptions{...}` while Task 4-bis defines the type as `sdk.IaCServeOptions`

**Evidence — goplugin API shape:**

`/Users/jon/go/pkg/mod/github.com/!go!code!alone/go-plugin@v1.7.0/server.go:87`:
```go
GRPCServer func([]grpc.ServerOption) *grpc.Server
```

This field is a FACTORY function. `goplugin.Serve(...)` invokes it with goplugin's own option list to construct the gRPC server inside its setup pipeline. The plugin author cannot pass a pre-built `*grpc.Server`.

The existing `sdk.Serve` (verified at `plugin/external/sdk/serve.go:32-39`) handles this correctly:

```go
goplugin.Serve(&goplugin.ServeConfig{
    HandshakeConfig: ext.Handshake,
    GRPCServer:      goplugin.DefaultGRPCServer,
    Plugins:         goplugin.PluginSet{"plugin": &servePlugin{server: server}},
})
```

`servePlugin.GRPCServer(broker, s *grpc.Server)` is the SECOND callback (the per-plugin one) where `pb.RegisterPluginServiceServer(s, p.server)` happens. THAT callback receives the actual `*grpc.Server` goplugin built.

Plan Task 4-bis pseudo-code (lines 578-595):

```go
func ServeIaCPlugin(provider any, opts IaCServeOptions) error {
    grpcSrv := grpc.NewServer(opts.GRPCServerOptions...)
    if err := RegisterAllIaCProviderServices(grpcSrv, provider); err != nil {
        return fmt.Errorf("ServeIaCPlugin: %w", err)
    }
    if opts.AdvertiseInContractRegistry {
        // Hook the registered services into BuildContractRegistry's response
    }
    return serveWithHandshake(grpcSrv, opts.PluginInfo)
}

func serveWithHandshake(grpcSrv *grpc.Server, info *PluginInfo) error {
    // ... uses existing sdk.Serve internals; refactored to be reusable
}
```

This sequence is structurally inconsistent with `goplugin.Serve`. The `*grpc.Server` is built outside goplugin's lifecycle, then handed to a hypothetical `serveWithHandshake` that the plan claims is "extracted from the existing `sdk.Serve` flow." But there IS no extractable "create grpc server then serve it" function in `sdk.Serve` — the existing flow is `goplugin.Serve(...)` with a `GRPCServer` factory + a `servePlugin.GRPCServer(broker, s)` per-plugin registration callback.

The correct shape is roughly:

```go
func ServeIaCPlugin(provider any, opts IaCServeOptions) error {
    iacRegister := func(s *grpc.Server) error {
        return RegisterAllIaCProviderServices(s, provider)
    }
    goplugin.Serve(&goplugin.ServeConfig{
        HandshakeConfig: ext.Handshake,
        GRPCServer:      goplugin.DefaultGRPCServer,
        Plugins: goplugin.PluginSet{
            "plugin": &iacServePlugin{
                pluginServer:  newGRPCServer(provider),  // existing PluginService registration
                iacRegistrar:  iacRegister,              // additional typed-IaC service registration
            },
        },
    })
    return nil
}

type iacServePlugin struct {
    pluginServer *grpcServer
    iacRegistrar func(*grpc.Server) error
}

func (p *iacServePlugin) GRPCServer(broker *goplugin.GRPCBroker, s *grpc.Server) error {
    pb.RegisterPluginServiceServer(s, p.pluginServer)  // legacy
    p.pluginServer.setBroker(broker)
    return p.iacRegistrar(s)  // typed IaC services
}
```

The plan's structure (build server outside, pass to "serveWithHandshake") cannot work — an implementer following it literally will hit a wall when they try to "extract" a function from `sdk.Serve` that doesn't exist.

**Evidence — symbol-name mismatch:**

Task 4-bis defines `IaCServeOptions` (line 555):
```go
type IaCServeOptions struct { ... }
```

PR 3 Task 9 (line 989) calls:
```go
sdk.ServeIaCPlugin(iacServer, sdk.ServeOptions{...})
```

`sdk.ServeOptions` is undefined; the actual type is `sdk.IaCServeOptions`. Compile error.

**Why Important:**

Task 4-bis is the load-bearing API spec for PR 3 Task 9. If the API spec is structurally wrong (cannot be implemented as written) AND the symbol name in PR 3 Task 9 doesn't match the symbol name Task 4-bis defines, then:
- Two parallel implementers (one on workflow PR 2, one on DO plugin PR 3) will invent different shapes — exactly the failure cycle 2 C-2-NEW raised.
- The implementer following PR 3 Task 9's diff will write `sdk.ServeOptions{...}` and hit "undefined: sdk.ServeOptions" + then have to invent the actual type name.
- The PR 2 implementer who tries to write `serveWithHandshake` will discover it can't be extracted from `sdk.Serve` because the existing flow doesn't have that shape, and will invent their own integration point.

This is repeatable cycle 2 C-2-NEW under a slightly different name.

**Why Important and not Critical:**

Both defects are mechanical fixes:
1. Replace pseudo-code body with the goplugin.Serve + per-plugin GRPCServer callback pattern (5-10 lines of correctly-shaped Go).
2. Update PR 3 Task 9 line 989 to write `sdk.IaCServeOptions{...}` instead of `sdk.ServeOptions{...}`.

Once those two edits land, the plan compiles and the API contract is consistent across PR 2 and PR 3.

**Resolution required:**

1. Rewrite the Task 4-bis pseudo-code body (lines 578-595) so service registration happens inside a `goplugin.Plugin.GRPCServer(broker, s)` callback, not on a pre-built server. Reference the existing `servePlugin` pattern at `plugin/external/sdk/serve.go:42-56`. Drop `serveWithHandshake` — there's nothing to extract; goplugin.Serve already IS the loop.
2. Update PR 3 Task 9 line 989 from `sdk.ServeOptions{...}` to `sdk.IaCServeOptions{...}` so the symbol name matches Task 4-bis's struct definition.

---

## Verdict reasoning

One Critical and one Important finding remain. Both trace to incomplete cycle 2 finding-resolution work — the prose of the rev3 plan claims the cycle 2 issues were addressed but verification against actual artifacts (v0.14.2 source on disk, goplugin.Serve API) shows the resolutions were partial:

- **C-1** (Task 22 docker-run unchanged + Task 20-bis flag invocation broken) — the cycle 2 C-1-NEW finding was acknowledged in Task 20-bis's preface ("per cycle 2 plan-phase C-1-NEW: ghcr.io/gocodealone/wfctl image doesn't exist") but the fix only addressed the IMAGE problem, not the FLAG problem (`--state-file`, `--dry-run` don't exist in v0.14.2 `infra apply`); AND Task 22 was never updated, still uses the rejected docker invocation. The state-file-compat gate is unrunnable as authored.

- **I-1** (Task 4-bis pseudo-code structurally inconsistent with goplugin + symbol-name mismatch with PR 3 Task 9) — the cycle 2 C-2-NEW finding was addressed by adding a concrete `IaCServeOptions` struct, BUT the implementation body cannot work against goplugin.Serve's actual API (verified against `go-plugin@v1.7.0/server.go:87`), and PR 3 Task 9 still calls `sdk.ServeOptions{...}` which doesn't match the defined type name `sdk.IaCServeOptions`.

Cycle 2 I-1-NEW (cross-plugin-build wire test) is RESOLVED — the rev3 plan adds an actual subprocess + typed RPC step. Cycle 2 I-2-NEW (SUPERSEDED-NOTICE) and I-3-NEW (two-variable conditional) drop under the "don't nitpick" filter — both are documentation/operator-judgment concerns rather than runtime blockers.

**Per skill rules** (PASS only with ZERO Critical + every Important resolved/escalated): plan has 1 Critical and 1 Important. **FAIL.** Plan needs revision and re-review.

Both findings are concretely actionable. C-1 requires picking one of three viable fixture-capture approaches (pre-baked is cheapest) and updating BOTH Task 20-bis AND Task 22 in lockstep. I-1 requires rewriting Task 4-bis pseudo-code to use goplugin's per-plugin GRPCServer callback pattern + fixing one symbol name in PR 3 Task 9.

---

## Bug-class scan transcript

| Class | Found? | Note |
|---|---|---|
| **Unstated assumptions** | C-1 | v0.14.2 wfctl flag surface assumed without source verification |
| **Repo-precedent conflicts** | I-1 | Task 4-bis pseudo-code doesn't match goplugin.Serve's actual API |
| **YAGNI violations** | NONE | (Task 21 conditional now in place, addresses cycle 2 YAGNI) |
| **Missing failure modes** | C-1 | If state-file-compat gate cannot run, what happens? Plan's escape hatch (line 1366) says "use existing v0.14.2 state file" but never commits to that as primary path |
| **Security / privacy** | NONE | No new attack surface |
| **Rollback story** | INTACT | Each runtime-affecting task has a Rollback note |
| **Simpler alternative not considered** | C-1 | Pre-baked fixture (operator has v0.14.2 state file in staging Spaces) is simpler than runtime fixture-capture |
| **User-intent drift** | NONE | "No compat shim" intent honored |
| **Verification-class mismatch** | C-1 | State-file-compat verification claimed but the test commands cannot execute |
| **Hidden serial dependencies** | NONE NEW | Cycle 2 I-3-NEW conditional addressed Task 20-bis → Task 21 dependency at the prose level |
| **Missing rollback wiring** | NONE | Each task documents rollback |
| **Over/under-decomposition** | NONE blocking | PR 4 size is intentional (atomic cutover) |
| **Symbol-name consistency across tasks** | I-1 | sdk.ServeOptions vs sdk.IaCServeOptions |

---

## Plan-vs-design alignment

| Design claim | Plan implementation | Status |
|---|---|---|
| State-file format invariant (F-3) verified at PR 4 pre-flight | Task 20-bis cannot execute as authored (broken flags) + Task 22 cannot execute (broken docker image + broken flag) | **DRIFT (C-1)** |
| `sdk.ServeIaCPlugin` is a single API call replacing sdk.Serve+manual-registration | API name + struct defined; pseudo-code body cannot compose with goplugin; Task 9 references wrong type name | **PARTIAL (I-1)** |
| cross-plugin-build catches DO wire incompat at workflow-CI time | Task 6 step 717-725 specifies subprocess build + typed RPC test | **HONORED** |
| Adapter NOT a re-marshalling wrapper (ADR-0026) | Task 15-bis adapter wraps typed pb client + dispatches typed calls | **HONORED** |
| Engine consumers see no API change | Task 16 returns `interfaces.IaCProvider` (the adapter); engine consumers unchanged | **HONORED** |
| 2026-04-26 plan superseded for IaC scope | SUPERSEDED-NOTICE.md sibling file; humans see it; automation may not (acceptable per "don't nitpick") | **HONORED** (under nitpick filter) |
| Two-variable consumer model conditional on Task 20-bis | Decision tree at Task 21 intro; rest of task body unconditionally written for two-variable branch | **MINOR INCONSISTENCY** (acceptable per "don't nitpick") |

---

## Escalation summary

Two changes required before re-review:

1. **C-1 disposition (state-file-compat verification):** Pick one of three approaches (pre-baked fixture, in-process v0.14.2-via-go-install, schema-pin). Update Task 20-bis Step 1 AND Task 22 Step 1+Step 2 in lockstep. The cycle 2 finding's resolution was applied to Task 20-bis only; Task 22 still has the rejected docker invocation. Either approach must remove BOTH broken flag invocations (`--state-file`, `--dry-run`) since neither exists in v0.14.2 OR current main.

2. **I-1 disposition (Task 4-bis API shape + Task 9 symbol):** Rewrite Task 4-bis lines 578-595 pseudo-code body to use the goplugin per-plugin `GRPCServer(broker, s)` callback pattern (reference `plugin/external/sdk/serve.go:42-56` for the existing `servePlugin` shape). Drop `serveWithHandshake` — there is nothing to extract. Then update PR 3 Task 9 line 989 from `sdk.ServeOptions{...}` to `sdk.IaCServeOptions{...}` so the symbol matches Task 4-bis's actual struct name.

Once these two are resolved, no further Critical/Important findings expected. Recommend: revise plan to v4 + cycle 4 plan-phase adversarial review (likely PASS).
