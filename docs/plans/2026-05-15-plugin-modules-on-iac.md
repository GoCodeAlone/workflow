# Plugin Modules on IaC — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Extend `sdk.ServeIaCPlugin` to also serve module + step factories, then ship the resulting plugin-native `storage.s3` / `storage.gcs` / `step.s3_upload` / `aws.credentials` / `gcp.credentials` modules + the workflow-core deletions absorbed from the locked B/C/D plan's blocked scope.

**Architecture:** Per `decisions/0038`: extend `IaCServeOptions` with `Modules map[string]sdk.ModuleProvider` + `Steps map[string]sdk.StepProvider`; introduce a thin `mapBackedProvider` adapter (~30 LOC) that implements `sdk.PluginProvider` + `sdk.ModuleProvider` + `sdk.StepProvider` by delegating to the supplied maps; `iacPluginServiceBridge` constructs `newGRPCServer(mapBackedProvider)` when those fields are non-nil so the existing `grpc_server.go` Module/Step lifecycle (handle state, error wrapping, mutex-guarded dispatch) is reused without refactor. Backwards compatible (zero-value options = current behavior); single registered `pb.PluginServiceServer`; no proto change. Plugins consuming the extension serve `storage.s3` / `storage.gcs` / `step.s3_upload` / `aws.credentials` / `gcp.credentials` via this path; workflow core then deletes those in-core types + drops `cloud.google.com/go/*` + `google.golang.org/api/*` from `go.mod` entirely (Phase B + Phase C deletions absorbed from the locked B/C/D plan's PRs 6 + 10).

**Tech Stack:** Go 1.x, gRPC (`go-plugin`), protobuf, `aws-sdk-go-v2`, `cloud.google.com/go`, `google.golang.org/api`, GoReleaser v2, the superpowers autonomous pipeline.

**Base branch:** main

---

## Scope Manifest

**PR Count:** 5
**Tasks:** 19
**Estimated Lines of Change:** ~2400 (informational; not enforced)

**Out of scope:**
- `MessagePublisher` / `MessageSubscriber` capability for IaC-bridge-served modules — `iacGRPCPlugin.GRPCServer` discards the `*goplugin.GRPCBroker`; modules registered via this plan's bridge path do not get pub/sub. The 5 modules in this plan need none. Lifting the limit is a follow-up SDK change.
- `sdk.TypedModuleProvider` (STRICT_PROTO contracts) for IaC-bridge-served modules — v1 uses only the legacy `sdk.ModuleProvider` (config-Struct) interface. The 5 modules in this plan need plain config-Struct round-trip.
- Trigger factories — none of the in-scope modules need triggers.
- `GetModuleSchemas` (UI metadata) for the new modules — `storage.s3` / `storage.gcs` / `step.s3_upload` work without UI schemas (matching their in-core ancestors).
- `InvokeService` (string-dispatch) — strict-contracts cutover removed it deliberately; this plan does not revive it.
- The out-of-`module/` AWS surface (`provider/aws/`, `plugin/rbac/aws.go`, `iam/aws.go`, `artifact/s3.go`) — inherited Non-Goal from the locked B/C/D plan.
- `github.com/digitalocean/godo` extraction — inherited Non-Goal.
- IaC state at-rest format (JSON → binary/pb) — inherited Non-Goal; needs separate brainstorming.
- The locked B/C/D plan's PR 5 (#118 DO release) + PR 9 (#681 gke wiring) — independently in flight on `origin/main`; this plan does NOT re-implement them. PR 4 + PR 5 of THIS plan depend on those merging on their own.

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | workflow core: SDK extension — IaCServeOptions.Modules/Steps + mapBackedProvider adapter | Task 1, Task 2 | `workflow`: `feat/plugin-modules-on-iac-sdk` |
| 2 | workflow-plugin-aws: in-plugin credentials + storage.s3 + step.s3_upload + release (cross-repo) | Task 3, Task 4, Task 5, Task 6, Task 7 | `workflow-plugin-aws`: `feat/aws-plugin-modules` |
| 3 | workflow-plugin-gcp: in-plugin credentials + storage.gcs + release (cross-repo, REUSES existing branch with B/C/D Tasks 20/21/22 on it) | Task 8, Task 9, Task 10, Task 11 | `workflow-plugin-gcp`: `feat/gcs-gke-storage` |
| 4 | workflow core: Phase B deletion — AWS files + `spaces` case + resolver rewrite + go.mod | Task 12, Task 13, Task 14, Task 15, Task 16 | `workflow`: `feat/phase-b-core-deletion` |
| 5 | workflow core: Phase C deletion — GCP files + `gcs` case + permanent CI gate | Task 17, Task 18, Task 19 | `workflow`: `feat/phase-c-core-deletion` |

**Execution order / dependencies:**
- **PR 1** — first; the SDK extension. After PR 1 merges, the team-lead cuts workflow `v0.53.0` from `origin/main` HEAD (this is the canonical floor every plugin in this plan pins to via `minEngineVersion`).
- **PR 2 + PR 3** — both depend on PR 1 merged + workflow `v0.53.0` tagged. Run in parallel. Each cuts a release tag at the end (aws plugin minor bump; gcp plugin minor bump).
- **PR 4** (Phase B core deletion, supersedes locked B/C/D PR 6) — depends on **PR 2 release tag** (aws) AND **locked-plan PR 5 (#118) release tag** (DO `v1.1.0`).
- **PR 5** (Phase C core deletion, supersedes locked B/C/D PR 10) — depends on **PR 3 release tag** (gcp) AND **locked-plan PR 9 (#681) merged** (gke wiring on `origin/main`).
- No PR stacking — every `workflow` PR branches off `origin/main` directly. PR 3 is the one exception (reuses the existing `feat/gcs-gke-storage` branch, which already has the locked B/C/D plan's Tasks 20/21/22 commits — that branch was off `workflow-plugin-gcp` main; the new tasks add on top).

**Cross-plan boundary:** The locked B/C/D plan stays as-is. That plan's PR 6 (workflow #TBD — Phase B core deletion) and PR 10 (workflow #TBD — Phase C core deletion) **are not opened** — their work ships under THIS plan's PR 4 + PR 5. The locked plan's PR 8 (workflow-plugin-gcp Tasks 20-24 + release) is **partially absorbed**: Tasks 20/21/22 commits already exist on `feat/gcs-gke-storage`; Tasks 23/24 + release ship under THIS plan's PR 3 (which reuses that same branch). The locked plan's manifest does not need to be unlocked — the work gets done; it just ships under different PR numbers, recorded in `decisions/0038`.

**Status:** Locked 2026-05-15T05:19:00Z

---

## Cross-repo note

PRs 2 and 3 land in **different git repositories** than the planning worktree. Per `decisions/0034-cross-repo-agent-operation-for-plugin-prs.md` this is **fully autonomous** — implement, push, open PR, AND cut/push the release tag, all following normal review discipline (feature branch → PR → admin-merge → tag; never direct-to-default-branch).

**Every cross-repo task dispatch MUST state, explicitly in the implementer prompt, the absolute path of the repo it works in:**
- `workflow-plugin-aws` → `/Users/jon/workspace/workflow-plugin-aws`
- `workflow-plugin-gcp` → `/Users/jon/workspace/workflow-plugin-gcp`
- planning worktree (PRs 1/4/5) → `/Users/jon/workspace/workflow/_worktrees/plugin-modules-on-iac`

## Environment note (ALL workflow-core tasks)

The planning worktree sits under a parent `go.work` that does not list it. **Every Go command in `/Users/jon/workspace/workflow/_worktrees/plugin-modules-on-iac` must be prefixed `GOWORK=off`** (`GOWORK=off go build ./...`, `GOWORK=off go test ./...`, `GOWORK=off go mod tidy`). IDE "not in workspace" / "undefined" diagnostics there are that artifact, not real — always verify via `GOWORK=off go build`. The plugin repos are normal checkouts — no `GOWORK=off`.

## Workflow-core pin (ALL plugin tasks — PRs 2 + 3)

After PR 1 of THIS plan merges, the team-lead tags `workflow v0.53.0` from `origin/main`. PRs 2 + 3 then pin both:
- `go.mod`: `go get github.com/GoCodeAlone/workflow@v0.53.0 && go mod tidy`
- `plugin.json`: `"minEngineVersion": "0.53.0"`

**If a different workflow tag is cut before PRs 2/3's release tasks run** (e.g. someone tags `v0.53.1` first), use whatever tag is the latest workflow release that contains PR 1 of this plan — `git -C /Users/jon/workspace/workflow tag --sort=-v:refname | head -1` and verify it includes PR 1's merge commit before pinning.

---

### Task 1: workflow core — `mapBackedProvider` adapter + `IaCServeOptions.Modules`/`.Steps` fields + bridge delegation

**Repo:** planning worktree (PR 1, branch `feat/plugin-modules-on-iac-sdk` off `origin/main`) — `GOWORK=off` on all Go commands.

**Files:**
- Modify: `plugin/external/sdk/iacserver.go` — extend `IaCServeOptions`; add `mapBackedProvider` struct (~30 LOC); modify `iacPluginServiceBridge` to embed/hold a `*grpcServer` delegate when modules/steps non-nil; modify `registerAllIaCProviderServicesWithOpts` to wire it
- Test: `plugin/external/sdk/iacserver_internal_test.go` (or new `plugin/external/sdk/iacserver_modules_test.go`)
- Reference (do NOT modify): `plugin/external/sdk/interfaces.go:39-61` (`sdk.PluginProvider`/`ModuleProvider`/`ModuleInstance`/`StepProvider`/`StepInstance` interfaces), `plugin/external/sdk/grpc_server.go:43-49` (`newGRPCServer(provider PluginProvider) *grpcServer` constructor + struct), `plugin/external/sdk/iacserver.go:206-218` (existing `IaCServeOptions` struct).

**Context:** Per `decisions/0038` — Approach A (Bridge extension + mapBackedProvider adapter). The bridge currently implements only `GetContractRegistry` + `GetManifest`; everything else returns `Unimplemented` via `pb.UnimplementedPluginServiceServer`. We add a thin adapter that lets the bridge reuse `grpc_server.go`'s existing handle-state + lifecycle code without refactoring it.

**Step 1: Read `grpc_server.go` end-to-end (load-bearing assumption verification)**

Run: `cat plugin/external/sdk/grpc_server.go | wc -l` (expect ~600+ lines). Read the file. Confirm:
- `newGRPCServer(provider PluginProvider) *grpcServer` (line 43) takes ONE `PluginProvider`; nothing else in its constructor.
- `CreateModule` (line 269 area), `InitModule`, `StartModule`, `StopModule`, `DestroyModule`, `CreateStep`, `ExecuteStep`, `DestroyStep` all dispatch via `s.provider.(ModuleProvider)` / `s.provider.(StepProvider)` type-assertion. Confirm the assertion failure path returns a clean gRPC error (not a panic).
- `registerModuleInstance` (call site of `mam.SetMessagePublisher`) guards with `if callbackClient != nil`. Confirmed-nil path is safe.

If anything diverges from this description, STOP and DM team-lead — the design's load-bearing assumption (decisions/0038 Consequences) is wrong and needs revisit before code.

**Step 2: Write the failing test**

In `plugin/external/sdk/iacserver_modules_test.go` (new file, package `sdk`):

```go
func TestIaCBridge_ModulesAndSteps_Delegate(t *testing.T) {
    // Construct a mapBackedProvider via NewIaCServeOptions
    fakeMod := &fakeModuleProvider{types: []string{"storage.test"}}
    fakeStep := &fakeStepProvider{types: []string{"step.test"}}
    opts := IaCServeOptions{
        Modules: map[string]ModuleProvider{"storage.test": fakeMod},
        Steps:   map[string]StepProvider{"step.test": fakeStep},
    }
    s := grpc.NewServer()
    err := registerAllIaCProviderServicesWithOpts(s, &fakeIaCRequiredProvider{}, opts)
    require.NoError(t, err)

    // Resolve the registered PluginServiceServer and call GetModuleTypes
    // (use grpc.ClientConn over a bufconn or direct in-process call via the
    //  registered handler — match the iacserver_serve_test.go pattern)
    types, err := callGetModuleTypes(s)
    require.NoError(t, err)
    assert.ElementsMatch(t, []string{"storage.test"}, types.Types)

    stypes, err := callGetStepTypes(s)
    require.NoError(t, err)
    assert.ElementsMatch(t, []string{"step.test"}, stypes.Types)
}

func TestIaCBridge_ZeroValueOptions_ModulesUnimplemented(t *testing.T) {
    // Backwards-compat: zero-value options → GetModuleTypes returns Unimplemented
    s := grpc.NewServer()
    err := registerAllIaCProviderServicesWithOpts(s, &fakeIaCRequiredProvider{}, IaCServeOptions{})
    require.NoError(t, err)
    _, err = callGetModuleTypes(s)
    require.Error(t, err)
    assert.Equal(t, codes.Unimplemented, status.Code(err))
}

func TestIaCBridge_NilBroker_NoMessagePublisherCall(t *testing.T) {
    // Regression guard: a MessageAwareModule registered via the bridge MUST
    // never have SetMessagePublisher/SetMessageSubscriber called (no broker
    // plumbed through iacGRPCPlugin per the design Non-Goal). If a future
    // change adds broker plumbing, this test fails loudly so the implementer
    // remembers to also add a positive pub/sub test.
    mam := &fakeMessageAwareModule{}
    fakeMod := &fakeModuleProvider{
        types:    []string{"storage.test"},
        instance: mam,  // CreateModule returns this
    }
    opts := IaCServeOptions{Modules: map[string]ModuleProvider{"storage.test": fakeMod}}
    s := grpc.NewServer()
    require.NoError(t, registerAllIaCProviderServicesWithOpts(s, &fakeIaCRequiredProvider{}, opts))
    _, err := callCreateModule(s, "storage.test", "test-instance", nil)
    require.NoError(t, err)
    assert.False(t, mam.SetMessagePublisherCalled, "SetMessagePublisher MUST NOT be called via the IaC bridge path (no broker plumbed)")
    assert.False(t, mam.SetMessageSubscriberCalled, "SetMessageSubscriber MUST NOT be called via the IaC bridge path")
}
```

Define the `fake*` test helpers inline in the test file.

**Step 3: Verify the tests fail**

Run: `GOWORK=off go test ./plugin/external/sdk/ -run 'IaCBridge_(ModulesAndSteps|ZeroValueOptions|NilBroker)' -v`
Expected: FAIL — `IaCServeOptions` has no `Modules`/`Steps` fields; `mapBackedProvider` undefined; the bridge has no module/step delegation.

**Step 4: Implement `mapBackedProvider`**

In `plugin/external/sdk/iacserver.go`, add:

```go
// mapBackedProvider adapts user-supplied module/step provider maps to the
// sdk.PluginProvider + sdk.ModuleProvider + sdk.StepProvider interfaces that
// grpc_server.go's existing PluginService implementation expects. Per ADR
// 0038, this is the smallest viable extraction path that lets the IaC
// bridge reuse newGRPCServer's handle-state + lifecycle code without
// refactoring grpc_server.go.
//
// The adapter is intentionally thin: ModuleTypes/StepTypes return map
// keys; CreateModule/CreateStep look up the named provider in the map and
// delegate. Manifest returns a zero-valued PluginManifest — the
// iacPluginServiceBridge implements GetManifest directly (using
// IaCServeOptions.ManifestProvider) and never calls back through this
// adapter, so Manifest's return value is never observed; it exists solely
// to satisfy the PluginProvider interface contract that newGRPCServer
// requires. ContractRegistry is intentionally NOT implemented — the
// iacPluginServiceBridge implements GetContractRegistry directly (walks
// the gRPC server's registered services) and never calls back through
// the delegate.
type mapBackedProvider struct {
    modules map[string]ModuleProvider
    steps   map[string]StepProvider
}

// Manifest satisfies sdk.PluginProvider. Return value is unobserved (the
// bridge handles GetManifest directly via IaCServeOptions.ManifestProvider)
// — the method exists only to satisfy the interface so newGRPCServer's
// PluginProvider parameter type-checks at compile time.
func (p *mapBackedProvider) Manifest() PluginManifest { return PluginManifest{} }

func (p *mapBackedProvider) ModuleTypes() []string {
    out := make([]string, 0, len(p.modules))
    for name := range p.modules {
        out = append(out, name)
    }
    return out
}

func (p *mapBackedProvider) CreateModule(typeName, name string, config map[string]any) (ModuleInstance, error) {
    mp, ok := p.modules[typeName]
    if !ok {
        return nil, fmt.Errorf("mapBackedProvider: unknown module type %q", typeName)
    }
    return mp.CreateModule(typeName, name, config)
}

func (p *mapBackedProvider) StepTypes() []string {
    out := make([]string, 0, len(p.steps))
    for name := range p.steps {
        out = append(out, name)
    }
    return out
}

func (p *mapBackedProvider) CreateStep(typeName, name string, config map[string]any) (StepInstance, error) {
    sp, ok := p.steps[typeName]
    if !ok {
        return nil, fmt.Errorf("mapBackedProvider: unknown step type %q", typeName)
    }
    return sp.CreateStep(typeName, name, config)
}
```

**Step 5: Extend `IaCServeOptions`**

In `plugin/external/sdk/iacserver.go`, modify the struct (line 206-218 area):

```go
type IaCServeOptions struct {
    PluginInfo       *PluginInfo
    ManifestProvider *pluginpkg.PluginManifest

    // Modules supplies plugin-native module providers. When non-nil, the
    // bridge wires GetModuleTypes / CreateModule / InitModule / StartModule /
    // StopModule / DestroyModule to delegate to grpc_server.go's existing
    // PluginService implementation via a thin mapBackedProvider adapter.
    // Zero-value = current behavior (Unimplemented for those RPCs).
    // See decisions/0038.
    Modules map[string]ModuleProvider

    // Steps supplies plugin-native step providers. Same wiring rationale as
    // Modules; values are sdk.StepProvider — the same interface non-IaC
    // plugins consume via sdk.Serve.
    Steps map[string]StepProvider
}
```

**Step 6: Wire bridge delegation**

Modify `iacPluginServiceBridge` struct + `registerAllIaCProviderServicesWithOpts` so the bridge holds an optional `*grpcServer` delegate constructed from a `mapBackedProvider`:

```go
type iacPluginServiceBridge struct {
    pb.UnimplementedPluginServiceServer
    grpcSrv      *grpc.Server
    diskManifest *pluginpkg.PluginManifest

    // delegate, when non-nil, handles GetModuleTypes / CreateModule /
    // InitModule / StartModule / StopModule / DestroyModule / GetStepTypes /
    // CreateStep / ExecuteStep / DestroyStep by forwarding to grpc_server.go's
    // existing implementation. Constructed by registerAllIaCProviderServicesWithOpts
    // when IaCServeOptions.Modules or .Steps is non-nil. Zero-value ⇒ those
    // RPCs continue returning Unimplemented via UnimplementedPluginServiceServer.
    delegate *grpcServer
}

// Override each delegated method on the bridge:
func (b *iacPluginServiceBridge) GetModuleTypes(ctx context.Context, req *emptypb.Empty) (*pb.TypeList, error) {
    if b.delegate == nil {
        return b.UnimplementedPluginServiceServer.GetModuleTypes(ctx, req)
    }
    return b.delegate.GetModuleTypes(ctx, req)
}
// ... same pattern for: CreateModule, InitModule, StartModule, StopModule,
//                       DestroyModule, GetStepTypes, CreateStep, ExecuteStep, DestroyStep
```

In `registerAllIaCProviderServicesWithOpts`:
```go
bridge := &iacPluginServiceBridge{
    grpcSrv:      s,
    diskManifest: opts.ManifestProvider,
}
if opts.Modules != nil || opts.Steps != nil {
    bridge.delegate = newGRPCServer(&mapBackedProvider{
        modules: opts.Modules,
        steps:   opts.Steps,
    })
}
if _, alreadyRegistered := s.GetServiceInfo()[pb.PluginService_ServiceDesc.ServiceName]; !alreadyRegistered {
    pb.RegisterPluginServiceServer(s, bridge)
}
```

**Step 7: Verify tests pass**

Run: `GOWORK=off go build ./... && GOWORK=off go test ./plugin/external/sdk/ -v`
Expected: PASS — including the existing `iacserver_*_test.go` (no regression) + the 3 new tests.

**Step 8: Verify the rest of the codebase still builds + tests still green**

Run: `GOWORK=off go test ./...`
Expected: PASS.

**Step 9: Commit**

```bash
git status   # confirm: iacserver.go + iacserver_modules_test.go + maybe go.sum (no go.mod change needed — no new deps)
git add plugin/external/sdk/iacserver.go plugin/external/sdk/iacserver_modules_test.go
git commit -m "feat(sdk): IaCServeOptions.Modules + .Steps via mapBackedProvider adapter (decisions/0038)"
```

**Verification class:** Plugin / extension — exercised via direct in-process gRPC calls in tests. Backwards compat is the critical invariant; `iacserver_*_test.go` regression coverage proves it.

---

### Task 2: workflow core — runtime-launch validation + integration test of an IaC plugin serving modules

**Repo:** planning worktree (PR 1, SAME branch `feat/plugin-modules-on-iac-sdk` as Task 1) — `GOWORK=off`.

**Files:**
- Test: `plugin/external/sdk/iac_e2e_test.go` (extend) — add an end-to-end test that constructs an IaCServeOptions with Modules + Steps and verifies the engine-side adapter sees them via the standard `pb.PluginServiceClient`.
- Or new: `plugin/external/sdk/iac_modules_e2e_test.go` if the existing file is ill-suited.

**Context:** Task 1's tests prove the bridge wires correctly in-process. This task proves the engine-side `ExternalPluginAdapter.ModuleFactories()` / `.StepFactories()` actually surface those modules when called against an IaC-bridge plugin — the design's "engine-side: zero change" claim.

**Change class:** Plugin loading path → runtime-launch-validation required.
**Rollback:** revert PR 1 — `IaCServeOptions.Modules`/`.Steps` removed; bridge falls back to `Unimplemented` for Module/Step RPCs; no other code path affected. Plugins that haven't started using the new fields are unaffected.

**Step 1: Write the failing test**

In `plugin/external/sdk/iac_modules_e2e_test.go`:

```go
func TestEndToEnd_IaCBridge_EngineAdapterSeesModules(t *testing.T) {
    // Spin up a real grpc.Server with the IaC bridge; construct a
    // pb.PluginServiceClient against it; call GetModuleTypes + CreateModule.
    // The adapter pattern (ExternalPluginAdapter.ModuleFactories) calls these
    // RPCs — proving the integration end-to-end without the full go-plugin
    // subprocess machinery.
    s, listener := newBufconnServer(t)
    defer s.Stop()

    fakeMod := &fakeModuleProvider{
        types: []string{"storage.test"},
        instance: &fakeModuleInstance{},
    }
    opts := IaCServeOptions{
        Modules: map[string]ModuleProvider{"storage.test": fakeMod},
    }
    require.NoError(t, registerAllIaCProviderServicesWithOpts(s, &fakeIaCRequiredProvider{}, opts))
    go s.Serve(listener)

    conn, err := bufconnDial(listener)
    require.NoError(t, err)
    client := pb.NewPluginServiceClient(conn)

    // GetModuleTypes — the adapter's first call
    types, err := client.GetModuleTypes(context.Background(), &emptypb.Empty{})
    require.NoError(t, err)
    assert.ElementsMatch(t, []string{"storage.test"}, types.Types)

    // CreateModule — the adapter's second call
    cfg, _ := structpb.NewStruct(map[string]any{"k": "v"})
    resp, err := client.CreateModule(context.Background(), &pb.CreateModuleRequest{
        Type: "storage.test", Name: "n", Config: cfg,
    })
    require.NoError(t, err)
    assert.NotEmpty(t, resp.HandleId)
    assert.Empty(t, resp.Error)
}
```

**Step 2: Verify it fails**

Run: `GOWORK=off go test ./plugin/external/sdk/ -run EndToEnd_IaCBridge_EngineAdapterSeesModules -v`
Expected: FAIL until Task 1's wiring is in place. (Task 1 should already make it pass — this task is the integration-level proof.)

**Step 3: Verify it passes (after Task 1's wiring is in)**

Run: `GOWORK=off go test ./plugin/external/sdk/ -run EndToEnd -v`
Expected: PASS.

**Step 4: Runtime-launch validation — bufconn end-to-end (the canonical evidence for the workflow-side path)**

The Step 1 bufconn test already exercises the IaC bridge through a real `pb.PluginServiceClient` — same gRPC dispatch the production engine uses. **Bufconn is the canonical runtime-launch evidence for the workflow-side change** because: (a) the IaC bridge code is identical regardless of the transport (bufconn vs unix-domain socket vs go-plugin subprocess); (b) the engine adapter's `ModuleFactories()`/`StepFactories()` calls are the same gRPC interface methods invoked from a bufconn client; (c) no HTTP/2 escape hatches or test-only shortcuts exist in the bridge dispatch path. A subprocess-binary load adds plumbing-level coverage (go-plugin handshake) but tests no additional bridge logic — and that subprocess coverage IS exercised by Tasks 7 and 11 in the plugin repos against real plugin binaries.

So for THIS task: the Step 1 test IS the runtime-launch validation. Capture the test transcript as `runtime-launch-validation` evidence. Tasks 7/11 cover the subprocess-handshake side in the plugin repos.

Run: `GOWORK=off go test ./... -run 'IaC|Engine' -count=1`
Expected: green across all suites.

**Step 5: Commit**

```bash
git add plugin/external/sdk/iac_modules_e2e_test.go
git commit -m "test(sdk): end-to-end IaC bridge ↔ pb.PluginServiceClient integration"
```

After PR 1 merges, the team-lead cuts workflow `v0.53.0` from `origin/main` HEAD (`git tag -a v0.53.0 origin/main -m '...' && git push origin v0.53.0`). PRs 2 + 3 then unblock.

---

### Task 3: workflow-plugin-aws — `awscreds.BuildAWSConfig` + `credential_source` marker handling

**Repo:** `/Users/jon/workspace/workflow-plugin-aws` (PR 2, branch `feat/aws-plugin-modules` off the aws plugin's default branch).

**Files:**
- Create: `internal/awscreds/awscreds.go`, `internal/awscreds/awscreds_test.go`
- Modify: the aws plugin's existing IaC-provider credential path (locate via `grep -rn 'CloudCredentials\|AccessKey\|LoadDefaultConfig' internal/ provider/ | head`) to route through `BuildAWSConfig`
- Reference (do NOT modify): workflow `module/cloud_account_aws_creds.go` (the SDK-bearing `awsProfileResolver`/`awsRoleARNResolver` bodies being re-homed here), workflow `module/cloud_account.go` (`CloudCredentials` struct shape)

**Context:** PR 4 of THIS plan (Phase B core deletion, Task 13 below) rewrites core's `awsProfileResolver`/`awsRoleARNResolver` to *declare, don't resolve* — they record `Extra["credential_source"] = "profile"|"role_arn"` markers. The SDK-bearing resolution (`config.LoadDefaultConfig(WithSharedConfigProfile)`, `sts.AssumeRole`) is re-homed **here**. `BuildAWSConfig` is the single in-plugin entry point: given a `CloudCredentials` it returns a resolved `aws.Config`, handling static keys, env/default chain, and the `profile`/`role_arn` markers.

**Step 1: Write failing test** — `awscreds_test.go`: `BuildAWSConfig` with static keys (config carries them); with `credential_source: "role_arn"` + a fake STS injection point (AssumeRole exercised); with `credential_source: "profile"` (`AWS_CONFIG_FILE` temp); with empty input (default chain, no error).

**Step 2: Verify it fails** — `cd /Users/jon/workspace/workflow-plugin-aws && go test ./internal/awscreds/ -v` → FAIL `undefined`.

**Step 3: Implement** — `internal/awscreds/awscreds.go`. **IMPORTANT path-routing note:** `CredInput.Source` is populated DIFFERENTLY depending on which call-site is constructing the input:
- **Standalone-module path (`storage.s3`/`step.s3_upload`/`aws.credentials` Providers in Tasks 4-6 of this plan):** the Provider's `CreateModule`/`CreateStep` reads `config["credentials"]["type"]` (the YAML field — `"static"`/`"env"`/`"profile"`/`"role_arn"`) directly from the supplied config map and assigns it to `CredInput.Source`. The plugin SUBPROCESS receives that raw config; `CloudCredentials.Extra` is never serialized into the standalone-module config path.
- **IaC-provider path (the existing aws plugin's `IaCProviderRequired.Initialize`):** the IaC provider also receives the raw YAML config (not `CloudCredentials.Extra`), so populate `CredInput.Source` from `config["credentials"]["type"]` THERE TOO. Task 13's `credential_source` marker on `CloudAccount.Extra` is consumed only by in-core code paths that THIS plan's PR 4 deletes; it never crosses the gRPC boundary into the plugin subprocess.

```go
type CredInput struct {
    AccessKey    string
    SecretKey    string
    SessionToken string
    Region       string
    RoleARN      string
    ExternalID   string
    Profile      string
    Source       string  // "static"|"env"|"profile"|"role_arn"|"" — populated by the call-site from config["credentials"]["type"] (the YAML field). NOT from CloudAccount.Extra (which never crosses the plugin boundary).
}

func BuildAWSConfig(ctx context.Context, c CredInput) (aws.Config, error) {
    switch c.Source {
    case "profile":
        // Port from workflow's deleted module/cloud_account_aws_creds.go awsProfileResolver SDK block
        return config.LoadDefaultConfig(ctx, config.WithSharedConfigProfile(c.Profile))
    case "role_arn":
        // Port awsRoleARNResolver SDK block: base config with optional region+static, sts.NewFromConfig, AssumeRole, return assumed
        // ...
    }
    if c.AccessKey != "" && c.SecretKey != "" {
        return config.LoadDefaultConfig(ctx, config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(c.AccessKey, c.SecretKey, c.SessionToken)))
    }
    return config.LoadDefaultConfig(ctx) // env/default chain
}
```

Wire the aws plugin's existing IaC-provider credential path to call `BuildAWSConfig` when receiving a `CloudCredentials` from the host (so a `credential_source` marker is honored).

**Step 4: Verify** — `cd /Users/jon/workspace/workflow-plugin-aws && go build ./... && go test ./internal/awscreds/ ./internal/... -v` → PASS.

**Step 5: Commit**
```bash
git add internal/awscreds/ internal/<modified-provider-path>
git commit -m "feat: in-plugin AWS credential resolution with credential_source marker handling"
```

---

### Task 4: workflow-plugin-aws — `aws.credentials` Provider + `credref` registry

**Repo:** `/Users/jon/workspace/workflow-plugin-aws` (PR 2, SAME branch `feat/aws-plugin-modules` as Task 3).

**Files:**
- Create: `internal/credref/registry.go`, `internal/credref/registry_test.go`
- Create: `internal/modules/aws_credentials.go`, `internal/modules/aws_credentials_test.go`

**Context:** `aws.credentials` is the optional in-plugin DRY module that lets a config declare credentials once and have many `storage.s3`/`step.s3_upload` modules `credentials_ref:` it. The locked B/C/D plan's design §3 spec; re-implemented here per `decisions/0038`'s scope absorption.

**Step 1: Write failing tests**
`credref/registry_test.go`: `Register("name", credInput)` succeeds first time; second `Register("name", ...)` returns error (duplicate); `Resolve("name")` returns the input + true; `Resolve("missing")` returns zero + false; concurrent Register/Resolve safe under `-race`. **Each test MUST `t.Cleanup(credref.Reset)` to clear the package-level global** so tests don't pollute each other (the registry is process-global on purpose to support `credentials_ref:` resolution from sibling modules; isolation is a test-only concern).
`aws_credentials_test.go`: `aws.credentials` Provider's `CreateModule` parses a config with a `credentials:` sub-block, builds a `CredInput`, registers it under the module name; the module's `Init` is a no-op; `Start`/`Stop` no-op. Same `t.Cleanup(credref.Reset)` pattern.

**Step 2: Verify they fail** — `cd /Users/jon/workspace/workflow-plugin-aws && go test ./internal/credref/ ./internal/modules/ -v` → FAIL.

**Step 3: Implement** — `internal/credref/registry.go`:
```go
package credref

import (
    "fmt"
    "sync"
    "github.com/GoCodeAlone/workflow-plugin-aws/internal/awscreds"
)

var (
    mu       sync.RWMutex
    registry = map[string]awscreds.CredInput{}
)

func Register(name string, c awscreds.CredInput) error {
    mu.Lock()
    defer mu.Unlock()
    if _, exists := registry[name]; exists {
        return fmt.Errorf("credref: name %q already registered (credentials_ref names must be unique within a config)", name)
    }
    registry[name] = c
    return nil
}

func Resolve(name string) (awscreds.CredInput, bool) {
    mu.RLock()
    defer mu.RUnlock()
    c, ok := registry[name]
    return c, ok
}

// Reset clears the registry. Test-only — production code never calls this.
// Tests that call Register MUST `t.Cleanup(credref.Reset)` to avoid
// polluting other tests in the same package.
func Reset() {
    mu.Lock()
    defer mu.Unlock()
    registry = map[string]awscreds.CredInput{}
}
```

`internal/modules/aws_credentials.go` — implements `sdk.ModuleProvider`:
```go
type AWSCredentialsProvider struct{}

func (p *AWSCredentialsProvider) ModuleTypes() []string { return []string{"aws.credentials"} }

func (p *AWSCredentialsProvider) CreateModule(typeName, name string, config map[string]any) (sdk.ModuleInstance, error) {
    credsMap, _ := config["credentials"].(map[string]any)
    c := awscreds.CredInput{
        AccessKey:    stringField(credsMap, "accessKey"),
        SecretKey:    stringField(credsMap, "secretKey"),
        SessionToken: stringField(credsMap, "sessionToken"),
        Region:       stringField(config, "region"),
        RoleARN:      stringField(credsMap, "roleArn"),
        ExternalID:   stringField(credsMap, "externalId"),
        Profile:      stringField(credsMap, "profile"),
        Source:       stringField(credsMap, "credential_source"),
    }
    if err := credref.Register(name, c); err != nil {
        return nil, err
    }
    return &awsCredentialsInstance{name: name}, nil
}

type awsCredentialsInstance struct{ name string }
func (m *awsCredentialsInstance) Init() error                  { return nil }
func (m *awsCredentialsInstance) Start(ctx context.Context) error { return nil }
func (m *awsCredentialsInstance) Stop(ctx context.Context) error  { return nil }
```

**Step 4: Verify they pass** — `cd /Users/jon/workspace/workflow-plugin-aws && go build ./... && go test ./internal/... -race -v` → PASS.

**Step 5: Commit**
```bash
git add internal/credref/ internal/modules/aws_credentials.go internal/modules/aws_credentials_test.go
git commit -m "feat: aws.credentials Provider + credref registry (process-local, unique-name)"
```

---

### Task 5: workflow-plugin-aws — plugin-native `storage.s3` Provider

**Repo:** `/Users/jon/workspace/workflow-plugin-aws` (PR 2, SAME branch `feat/aws-plugin-modules` as Task 4).

**Files:**
- Create: `internal/modules/storage_s3.go`, `internal/modules/storage_s3_test.go`
- Reference (do NOT modify): workflow `module/s3_storage.go` (`S3Storage` + `NewS3Storage`); the in-plugin S3 store at `workflow-plugin-aws/internal/statebackend/s3.go` (already merged via PR 3 of the locked B/C/D plan — same SDK invocation pattern).

**Context:** `storage.s3` becomes a plugin-native module via `IaCServeOptions.Modules`. Credentials inline (a `credentials:` sub-block in the module config) OR `credentials_ref:` an `aws.credentials` module by name (resolved from the `credref` registry, Task 4).

**Step 1: Write failing test** — `storage_s3_test.go`: `CreateModule` with inline `credentials:` block builds the module, `Init()` resolves an `aws.Config` via `awscreds.BuildAWSConfig`; `CreateModule` with `credentials_ref:` looks up `credref` and uses that `CredInput`; `credentials_ref:` to an unregistered name returns a clean "credentials_ref %q not found; declare an aws.credentials module first" error.

**Step 2: Verify it fails** — `cd /Users/jon/workspace/workflow-plugin-aws && go test ./internal/modules/ -run StorageS3 -v` → FAIL.

**Step 3: Implement** — port `workflow module/s3_storage.go`'s `S3Storage` into `internal/modules/storage_s3.go` (`package modules`, S3 client construction via `awscreds.BuildAWSConfig` from inline block or `credref.Resolve`). Implements `sdk.ModuleProvider` + `sdk.ModuleInstance`. Do NOT import `workflow/module`.

**Step 4: Verify it passes** — `cd /Users/jon/workspace/workflow-plugin-aws && go build ./... && go test ./internal/modules/ -v` → PASS.

**Step 5: Commit**
```bash
git add internal/modules/storage_s3.go internal/modules/storage_s3_test.go
git commit -m "feat: plugin-native storage.s3 module"
```

---

### Task 6: workflow-plugin-aws — plugin-native `step.s3_upload` Provider

**Repo:** `/Users/jon/workspace/workflow-plugin-aws` (PR 2, SAME branch `feat/aws-plugin-modules` as Task 5).

**Files:**
- Create: `internal/steps/s3_upload.go`, `internal/steps/s3_upload_test.go`
- Reference (do NOT modify): workflow `module/pipeline_step_s3_upload.go` (`S3UploadStep` + `NewS3UploadStepFactory`, required config: `bucket`/`region`/`key`/`body_from`).

**Context:** `step.s3_upload` becomes plugin-native via `IaCServeOptions.Steps`. Credentials via inline `credentials:` block or `credentials_ref:` (Task 4's registry).

**Step 1: Write failing test** — `s3_upload_test.go`: `CreateStep` with config (`bucket`/`region`/`key`/`body_from` required); `Execute` round-trips a small payload through a fake S3 client; `credentials_ref:` resolution.

**Step 2: Verify it fails** — `cd /Users/jon/workspace/workflow-plugin-aws && go test ./internal/steps/ -run S3Upload -v` → FAIL.

**Step 3: Implement** — port workflow `module/pipeline_step_s3_upload.go` → `internal/steps/s3_upload.go` (`package steps`). Implements `sdk.StepProvider` + `sdk.StepInstance`. Creds via `awscreds.BuildAWSConfig`.

**Step 4: Verify it passes** — `cd /Users/jon/workspace/workflow-plugin-aws && go build ./... && go test ./...` → PASS.

**Step 5: Commit**
```bash
git add internal/steps/s3_upload.go internal/steps/s3_upload_test.go
git commit -m "feat: plugin-native step.s3_upload"
```

---

### Task 7: workflow-plugin-aws — wire IaCServeOptions + plugin.json + capability parity test + release

**Repo:** `/Users/jon/workspace/workflow-plugin-aws` (PR 2, SAME branch `feat/aws-plugin-modules` as Task 6).

**Files:**
- Modify: `cmd/workflow-plugin-aws/main.go` — populate `IaCServeOptions.Modules` + `.Steps` with the 3 providers (Tasks 4-6)
- Modify: `plugin.json` — add `"aws.credentials"` + `"storage.s3"` to `capabilities.moduleTypes`; `"step.s3_upload"` to `capabilities.stepTypes`; bump `version` (minor); pin `minEngineVersion: "0.53.0"` (or whatever workflow tag PR 1 of THIS plan landed in — confirm via `git -C /Users/jon/workspace/workflow tag --sort=-v:refname | head -1`)
- Modify: `go.mod` / `go.sum` — `go get github.com/GoCodeAlone/workflow@v0.53.0 && go mod tidy`
- Modify/extend: `internal/host_conformance_test.go` — assert `plugin.json capabilities.moduleTypes ↔ NewIaCServer().GetModuleTypes` and `capabilities.stepTypes ↔ GetStepTypes` parity (in-process bridge call, no subprocess)
- Modify: `CHANGELOG.md` — entry for the new modules + step type + the `minEngineVersion` bump

**Context:** Final task of PR 2 — wire the providers into the plugin's main entrypoint, declare them in `plugin.json`, prove parity, release the tag.

**Change class:** Plugin-loading path + version pin → runtime-launch-validation required.
**Rollback:** plugin release is additive; on a defect cut a patch; PR 4 (Phase B core deletion) is blocked on this release tag, so a defect surfaces at PR 4's CI before any in-core path is removed.

**Step 1: Update `main.go`** — populate `IaCServeOptions.Modules` (`"storage.s3"` + `"aws.credentials"`) and `.Steps` (`"step.s3_upload"`). Stage the file.

**Step 2: Update `plugin.json`** — add the capability entries; bump `version` `vX.Y.Z` → `vX.(Y+1).0` (look at current tag; minor bump for new capabilities); set `minEngineVersion: "0.53.0"` (confirm exact workflow tag).

**Step 3: Update `go.mod`** — `go get github.com/GoCodeAlone/workflow@v0.53.0 && go mod tidy`.

**Step 4: Write/extend the parity test** — in `internal/host_conformance_test.go`:
```go
func TestPluginJSONCapabilities_ModuleStep_Parity(t *testing.T) {
    // Read plugin.json, extract capabilities.moduleTypes + .stepTypes.
    // Construct the bridge in-process via the same path main.go uses.
    // Assert GetModuleTypes returns exactly the moduleTypes set; GetStepTypes returns exactly the stepTypes set.
}
```

**Step 5: Verify build + tests green** — `cd /Users/jon/workspace/workflow-plugin-aws && go build ./... && go test ./...` → PASS.

**Step 6: Runtime-launch validation (subprocess plugin load)** — build the plugin binary; load it via `wfctl` against a minimal workflow config that lists the new module + step types; confirm `wfctl plugin install ./dist/<binary>` succeeds + `wfctl plugin list` shows `aws.credentials`/`storage.s3` in `moduleTypes` and `step.s3_upload` in `stepTypes`. Capture the full transcript. If the plugin binary's standard go-plugin handshake fails or the host can't dispatch `GetModuleTypes` to the subprocess, the in-process bufconn tests of Task 1/2 (workflow side) won't catch it — this subprocess load is the canonical evidence per the runtime-launch-validation class. **If `wfctl` can't be exercised against this plugin in CI** (e.g. the plugin repo's CI lacks `wfctl`), document why a SHELL-level go-plugin handshake test is sufficient and capture THAT transcript instead — no silent skip.

**Step 7: Commit (release prep)** — stage every modified file (`cmd/`, `plugin.json`, `go.mod`, `go.sum`, `internal/host_conformance_test.go`, `CHANGELOG.md`, plus the runtime-launch validation transcript path/file):
```bash
git add -A   # then verify with git status; never silently leave files unstaged
git commit -m "chore: release workflow-plugin-aws v<minor> — storage.s3 + step.s3_upload + aws.credentials via IaC bridge"
```

**Step 8: Open PR 2 + tag (after merge)**
- Open PR with the standard body (summary + test plan + the runtime-launch transcript reference).
- After PR 2 is admin-merged to the aws plugin default branch: `git checkout main && git pull && git tag v<version> && git push origin v<version>`. Verify `gh release view v<version> --repo GoCodeAlone/workflow-plugin-aws` shows assets; GoReleaser run `success`.

---

### Task 8 PRE-STEP (MANDATORY before dispatching Task 8): coordinate the in-progress locked-plan Task 23 (#22)

The locked B/C/D plan's TaskList shows task #22 ("Implement Task 23: workflow-plugin-gcp storage.gcs + gcp.credentials + release") as **`in_progress`** — that scope OVERLAPS exactly with this plan's Tasks 9-11. Same branch (`feat/gcs-gke-storage`), same files. Dispatching this plan's Tasks 8-11 while another agent is mid-flight on #22 will produce commit-collision, ownership conflicts, and merge-loss (per `feedback_per_agent_worktree_per_task_pr` and `feedback_worktree_agents_must_ff_before_commit`).

**Team-lead MUST execute this pre-step (NOT an implementer task) before dispatching Task 8:**
1. Probe locked-plan #22 owner liveness (per `feedback_check_tmux_when_agent_silent`): `TaskGet 22` for the owner; `SendMessage` a status-request to that owner; check tmux pane via shell if no response.
2. If alive: send `shutdown_request` to the owner; wait for `shutdown_response`.
3. TaskUpdate #22 → status `completed` with a comment "abandoned — superseded by 2026-05-15-plugin-modules-on-iac plan Tasks 8-11; the WORK is the same, the new plan's Task spec accommodates the SDK extension this plan adds." (Per scope-lock semantics, locked-plan Task 23's WORK is delivered by this new plan; the TaskList entry can close as completed-via-supersession.)
4. Verify `feat/gcs-gke-storage` branch state: `git -C /Users/jon/workspace/workflow-plugin-gcp fetch origin && git -C /Users/jon/workspace/workflow-plugin-gcp log --oneline origin/feat/gcs-gke-storage | head -10`. Identify which commits are on the branch (locked-plan Tasks 20/21/22 SHAs already there). If any partial Task-23-equivalent commits exist (from the in-progress agent's work), surface to user before proceeding — do NOT silently overwrite.

Only AFTER this pre-step is complete may the team-lead dispatch Task 8 to implementer-3 (or whichever fresh implementer takes the gcp stream).

---

### Task 8: workflow-plugin-gcp — `gcpcreds.BuildGCPOptions`

**Repo:** `/Users/jon/workspace/workflow-plugin-gcp` (PR 3, REUSES branch `feat/gcs-gke-storage` — already has the locked B/C/D plan's Tasks 20+21+22 commits on it; pre-step above coordinates with the in-progress locked-plan agent).

**Files:**
- Create: `internal/gcpcreds/gcpcreds.go`, `internal/gcpcreds/gcpcreds_test.go`

**Context:** `BuildGCPOptions` is the in-plugin gcp credential helper. The gcp credential resolvers (`module/cloud_account_gcp.go`) are already SDK-free in workflow core, so this helper is simpler than its aws counterpart — it converts a `CredInput`-like struct into `[]option.ClientOption` (`option.WithCredentialsJSON` for inline `ServiceAccountJSON`; ADC fallback for empty input).

**Step 1: Write failing test** — `gcpcreds_test.go`: `BuildGCPOptions` with inline service-account JSON returns the right options; with empty input returns empty (ADC default).

**Step 2: Verify it fails** — `cd /Users/jon/workspace/workflow-plugin-gcp && go test ./internal/gcpcreds/ -v` → FAIL.

**Step 3: Implement** — `internal/gcpcreds/gcpcreds.go`:
```go
type CredInput struct {
    ServiceAccountJSON []byte
    ProjectID          string
}

func BuildGCPOptions(c CredInput) []option.ClientOption {
    var opts []option.ClientOption
    if len(c.ServiceAccountJSON) > 0 {
        opts = append(opts, option.WithCredentialsJSON(c.ServiceAccountJSON))
    }
    return opts
}
```

**Step 4: Verify it passes** — `cd /Users/jon/workspace/workflow-plugin-gcp && go build ./... && go test ./internal/gcpcreds/ -v` → PASS.

**Step 5: Commit**
```bash
git add internal/gcpcreds/
git commit -m "feat: in-plugin GCP credential helper (BuildGCPOptions)"
```

---

### Task 9: workflow-plugin-gcp — `gcp.credentials` Provider + `credref` registry

**Repo:** `/Users/jon/workspace/workflow-plugin-gcp` (PR 3, SAME branch `feat/gcs-gke-storage`).

**Files:**
- Create: `internal/credref/registry.go`, `internal/credref/registry_test.go`
- Create: `internal/modules/gcp_credentials.go`, `internal/modules/gcp_credentials_test.go`

**Context:** Mirror Task 4 structurally for gcp.

**Step 1-5:** Mirror Task 4's pattern exactly. Provider type returns `[]string{"gcp.credentials"}` from `ModuleTypes()`; `CreateModule` parses the config + calls `credref.Register(name, gcpCredInput)`; instance has no-op lifecycle. Include the `Reset()` test-only helper in `internal/credref/registry.go`; every test that calls `Register` MUST `t.Cleanup(credref.Reset)`. Tests assert duplicate-register error, Resolve round-trip, Init/Start/Stop no-op, race-clean concurrent access.

Commit:
```bash
git add internal/credref/ internal/modules/gcp_credentials.go internal/modules/gcp_credentials_test.go
git commit -m "feat: gcp.credentials Provider + credref registry"
```

---

### Task 10: workflow-plugin-gcp — plugin-native `storage.gcs` Provider

**Repo:** `/Users/jon/workspace/workflow-plugin-gcp` (PR 3, SAME branch `feat/gcs-gke-storage`).

**Files:**
- Create: `internal/modules/storage_gcs.go`, `internal/modules/storage_gcs_test.go`
- Reference (do NOT modify): workflow `module/storage_gcs.go` (`GCSStorage` + `NewGCSStorage`); the in-plugin GCS state store at `workflow-plugin-gcp/internal/statebackend/gcs.go` (already on this branch via the locked B/C/D plan's Task 20).

**Context:** Mirror Task 5 structurally for gcp.

**Step 1-5:** Mirror Task 5's pattern. Port workflow `module/storage_gcs.go`'s `GCSStorage` into `internal/modules/storage_gcs.go`; creds via `gcpcreds.BuildGCPOptions` from inline `credentials:` or `credentials_ref:`. Tests for inline-creds, ref-creds, missing-ref-error.

Commit:
```bash
git add internal/modules/storage_gcs.go internal/modules/storage_gcs_test.go
git commit -m "feat: plugin-native storage.gcs module"
```

---

### Task 11: workflow-plugin-gcp — wire IaCServeOptions + plugin.json + parity test + release

**Repo:** `/Users/jon/workspace/workflow-plugin-gcp` (PR 3, SAME branch `feat/gcs-gke-storage`).

**Files:** mirror Task 7 for gcp.

**Change class:** Plugin-loading path + version pin → runtime-launch-validation required.
**Rollback:** plugin release additive; on defect cut patch; PR 5 (Phase C core deletion) is blocked on this release tag.

**Steps:** Mirror Task 7 — including the explicit runtime-launch-validation Step 6 (subprocess plugin binary load via `wfctl plugin install` against a minimal workflow config; verify `wfctl plugin list` shows the new types; capture transcript). `cmd/workflow-plugin-gcp/main.go` populates `IaCServeOptions.Modules` (`"storage.gcs"` + `"gcp.credentials"`); no `.Steps` (gcp has none in scope). `plugin.json` adds the capability entries; bumps `version` (minor); sets `minEngineVersion: "0.53.0"`. `go.mod` pinned to `v0.53.0`. Parity test extended for `moduleTypes ↔ GetModuleTypes`. Open PR; admin-merge; tag.

The branch already has the locked B/C/D plan's `gcs` IaCStateBackend + `gke` ResourceDriver work — those + the new `storage.gcs` + `gcp.credentials` ship together as PR 3 of THIS plan.

```bash
git add cmd/ plugin.json go.mod go.sum internal/host_conformance_test.go CHANGELOG.md
git commit -m "chore: release workflow-plugin-gcp v<minor> — gcs + storage.gcs + gcp.credentials + gke (absorbs B/C/D PR 8)"
```

After PR 3 admin-merges: `git tag v<version> && git push origin v<version>`. Verify GoReleaser.

---

### Task 12 PRE-STEP (MANDATORY before any commit on PR 4's branch): verify cross-plan release tags exist

PR 4 (Phase B core deletion) is **hard-blocked** on:
1. **PR 2 of THIS plan released** (workflow-plugin-aws minor bump tag — Task 7)
2. **Locked-plan PR 5 (#118) released as `v1.1.0`** (workflow-plugin-digitalocean — Task 13 of the locked B/C/D plan)

Both must be installable BEFORE PR 4 starts deleting in-core paths. There is no CI gate enforcing this; the executor must check explicitly.

**Team-lead MUST execute this pre-step before dispatching Task 12 (or any later Phase B task):**
```bash
gh release view v<aws-version> --repo GoCodeAlone/workflow-plugin-aws --json assets --jq '.assets|length'   # expect ≥4 (linux/darwin × amd64/arm64)
gh release view v1.1.0 --repo GoCodeAlone/workflow-plugin-digitalocean --json assets --jq '.assets|length'   # expect ≥4
```
Both must succeed and report assets. If EITHER is missing, do NOT start PR 4 — surface to user (the missing release is the blocker; chase that first). Re-running this check at PR-4-merge time is also required (covered in Task 16 Step 4).

---

### Task 12: workflow core — delete dead `cloud_account_aws.go`

**Repo:** planning worktree (PR 4, branch `feat/phase-b-core-deletion` off `origin/main`) — `GOWORK=off`.

**Files:** Delete `module/cloud_account_aws.go`.

**Context:** `cloud_account_aws.go` holds `AWSConfigProvider` + `CloudAccount.AWSConfig()` + `CloudAccount.ValidateCredentials()` — verified dead code (#653 removed `awsProviderFrom` and every consumer; locked B/C/D plan's Task 14 already verified zero non-test consumers). Pre-step above must be complete before this task starts.

**Step 1: Verify zero non-test consumers** — `grep -rn 'AWSConfigProvider\|\.AWSConfig(\|\.ValidateCredentials(' --include='*.go' . | grep -v '_test.go' | grep -v 'cloud_account_aws.go'` → expected no output.

**Step 2: Delete + build** — `git rm module/cloud_account_aws.go; GOWORK=off go build ./...` → succeeds. Delete any orphaned `_test.go` references too.

**Step 3: Test** — `GOWORK=off go test ./module/...` → PASS.

**Step 4: Commit**
```bash
git add -A
git commit -m "refactor: delete dead cloud_account_aws.go (zero consumers, removed by #653)"
```

---

### Task 13: workflow core — rewrite `awsProfileResolver` + `awsRoleARNResolver` SDK-free with `credential_source` markers

**Repo:** planning worktree (PR 4, SAME branch `feat/phase-b-core-deletion`) — `GOWORK=off`.

**Files:** Modify `module/cloud_account_aws_creds.go`, `module/cloud_account_aws_creds_test.go`.

**Context:** `awsStaticResolver`/`awsEnvResolver` are already SDK-free; `awsProfileResolver`/`awsRoleARNResolver` carry the only `aws-sdk-go-v2` imports in this file. Per the locked B/C/D plan's Task 15: rewrite both to *declare, don't resolve* — record `Extra["credential_source"] = "profile"|"role_arn"` markers; the aws plugin's `awscreds.BuildAWSConfig` (Task 3 of THIS plan) performs the SDK-bearing resolution. Mitigation for the gap window where an old aws plugin sees the marker: core logs a warning when emitting one.

**Step 1: Update tests** — `cloud_account_aws_creds_test.go`: change `awsProfileResolver`/`awsRoleARNResolver` assertions to expect `Extra["credential_source"]` markers (NOT resolved keys). Keep the `awsRoleARNResolver` `roleARN == ""` → `fmt.Errorf` required-check assertion. Assert a warning is logged when a marker is emitted (capture via `log.SetOutput`).

**Step 2: Verify they fail** — `GOWORK=off go test ./module/ -run 'AwsProfile|AwsRoleARN|CredentialResolver' -v` → FAIL.

**Step 3: Rewrite the two resolver bodies** —
`awsProfileResolver.Resolve`: keep through `m.creds.Extra["profile"] = profile`, then:
```go
    m.creds.Extra["credential_source"] = "profile"
    logCredentialSourceMarker("aws", "profile")
    return nil
}
```
`awsRoleARNResolver.Resolve`: keep `credsMap` nil-check + `roleARN`/`externalID` extraction + `m.creds.RoleARN`/`Extra["external_id"]` records + `roleARN == ""` required-check; then:
```go
    m.creds.Extra["credential_source"] = "role_arn"
    logCredentialSourceMarker("aws", "role_arn")
    return nil
}
```
Add `func logCredentialSourceMarker(provider, source string) { log.Printf("workflow: %s credential_source=%q recorded; resolution deferred to plugin (decisions/0036+0038)", provider, source) }`. Update the import block to `"fmt"` + `"log"` + `"os"` (drop `context`, `aws`, `config`, `credentials`, `sts`).

**Step 4: Verify they pass** — `GOWORK=off go build ./... && GOWORK=off go test ./module/ -run 'AwsProfile|AwsRoleARN|CredentialResolver' -v` → PASS.

**Step 5: Commit**
```bash
git add module/cloud_account_aws_creds.go module/cloud_account_aws_creds_test.go
git commit -m "refactor: AWS profile/role_arn resolvers declare credential_source marker + warn, no SDK"
```

---

### Task 14: workflow core — delete `iac_state_spaces.go` + strip `spaces` case

**Repo:** planning worktree (PR 4, SAME branch `feat/phase-b-core-deletion`) — `GOWORK=off`.

**Files:** Delete `module/iac_state_spaces.go`, `module/iac_state_spaces_test.go`. Modify `module/iac_module.go` (remove `case "spaces":`).

**Context:** `iac_state_spaces.go` is now plugin-served — aws plugin `s3` (locked plan's PR 3, MERGED), DO plugin `spaces` (locked plan's PR 5, in-flight as #118). Per the locked B/C/D plan's Task 16: there is **NO `case "s3":`** in `iac_module.go`; only `case "spaces":` is removed.

**Step 1: Delete + remove case** — `git rm module/iac_state_spaces.go module/iac_state_spaces_test.go`. In `module/iac_module.go`: delete the entire `case "spaces":` block; update the `default:`-arm error-message in-core-backends list (drop `'spaces'`; leave `'gcs'` for PR 5 of THIS plan).

**Step 2: Build** — `GOWORK=off go build ./...`. Verify: `grep -rn 'NewSpacesIaCStateStore\|SpacesIaCStateStore' --include='*.go' .` → expected no output.

**Step 3: Test** — `GOWORK=off go test ./module/ -run 'IaCModule|IaCState' -v` → PASS.

**Step 4: Commit**
```bash
git add -A
git commit -m "refactor: delete in-core spaces IaC state store — now plugin-served (clean break)"
```

---

### Task 15: workflow core — delete `s3_storage.go` + `pipeline_step_s3_upload.go` + drop built-in registrations

**Repo:** planning worktree (PR 4, SAME branch `feat/phase-b-core-deletion`) — `GOWORK=off`.

**Files:** Delete `module/s3_storage.go`, `module/pipeline_step_s3_upload.go` (+ `_test.go` if present). Modify `plugins/storage/plugin.go` (drop `"storage.s3"` factory at `:89`, capability `:37`, schema `:326`). Modify `plugins/pipelinesteps/plugin.go` (drop `"step.s3_upload"` factory at `:183`, capability `:93`). Modify `DOCUMENTATION.md`.

**Context:** `storage.s3` + `step.s3_upload` are now plugin-native in `workflow-plugin-aws` (Tasks 5 + 6 of THIS plan, PR 2). Mirrors locked B/C/D plan's Task 17.

**Steps:** Mirror locked plan Task 17's procedure — `git rm` the impls; edit the two `plugins/*/plugin.go` files per the line refs; update DOCUMENTATION.md. `grep -rn 'NewS3Storage\|NewS3UploadStepFactory\|S3Storage\|S3UploadStep' --include='*.go' .` → expected no output. `GOWORK=off go test ./plugins/storage/... ./plugins/pipelinesteps/... ./module/...` → PASS.

```bash
git add -A
git commit -m "refactor: delete in-core storage.s3 + step.s3_upload — now plugin-native"
```

---

### Task 16: workflow core — `go mod tidy` + `.phase-b-complete` marker + Phase B migration doc + image-launch validation

**Repo:** planning worktree (PR 4, SAME branch `feat/phase-b-core-deletion`) — `GOWORK=off`.

**Files:** Modify `go.mod`/`go.sum`. Create `.phase-b-complete` (tracked marker for `scripts/audit-cloud-symbols.sh --check`). Create `docs/migrations/2026-05-15-plugin-modules-on-iac.md` (Phase B section).

**Context:** After Tasks 12-15, `module/` no longer imports `aws-sdk-go-v2` for the IaC-state / standalone-S3 surface. `aws-sdk-go-v2` **stays** in `go.mod` (`provider/aws/`, `plugin/rbac/aws.go`, `iam/aws.go`, `artifact/s3.go` still import it — out of scope). `go mod tidy` drops only the now-unused service modules. The `.phase-b-complete` marker arms the audit script's `cloud_account_aws_creds.go` zero-`aws-sdk-go-v2` invariant.

**Change class:** Build pipeline + go.mod dependency change + plugin loading path → runtime-launch-validation required.
**Rollback:** revert PR 4; deleted files recoverable from git, the in-core `spaces`/`storage.s3`/`step.s3_upload` paths + SDK-bearing resolvers restore, `go.mod` re-tidies. The `spaces` clean-break rolls back only as a matched pair with the DO plugin `v1.1.0` release (locked plan's PR 5).

**Step 1: Tidy + marker** — `GOWORK=off go mod tidy && touch .phase-b-complete`.

**Step 2: Audit script enforcing mode** — `bash scripts/audit-cloud-symbols.sh --check` → `audit-cloud-symbols: OK` (with `.phase-b-complete` present, asserts `cloud_account_aws_creds.go` has 0 `aws-sdk-go-v2` imports). FAIL → Task 13 incomplete.

**Step 3: Build + full test + image-launch validation** — `GOWORK=off go build ./... && GOWORK=off go test ./...`; runtime-launch-validation: build + launch the server against a representative `iac.state` config; confirm clean startup; capture transcript.

**Step 4: Re-verify cross-plan releases STILL exist** (defensive check at PR-4 merge time, in case anything was rolled back since Task 12's pre-step) —
```bash
gh release view v<aws-version> --repo GoCodeAlone/workflow-plugin-aws --json assets --jq '.assets|length'   # expect ≥4
gh release view v1.1.0 --repo GoCodeAlone/workflow-plugin-digitalocean --json assets --jq '.assets|length'   # expect ≥4
```
Both must still report assets. Abort the merge otherwise.

**Step 5: Migration doc** — `docs/migrations/2026-05-15-plugin-modules-on-iac.md` Phase B section: `iac.state backend: spaces` → load `workflow-plugin-digitalocean >= v1.1.0`; `iac.state backend: s3` → load `workflow-plugin-aws >= v<release>`; `storage.s3` / `step.s3_upload` → load aws plugin (creds inline or `credentials_ref:` an `aws.credentials` module); `provider: aws` with `credentialType: profile|role_arn` co-deploy requirement (core+aws-plugin together); workflow `>= v0.53.0` engine floor.

**Step 6: Commit**
```bash
git add -A   # then verify git status; never silently leave files unstaged (covers go.mod, go.sum, .phase-b-complete, docs/migrations/, runtime-launch transcript file if applicable)
git commit -m "build: drop unused aws-sdk-go-v2 IaC modules + arm Phase B audit invariant"
```

---

### Task 17: workflow core — delete GCP files + strip `gcs` case

**Repo:** planning worktree (PR 5, branch `feat/phase-c-core-deletion` off `origin/main`) — `GOWORK=off`.

**Files:** Delete `module/iac_state_gcs.go`, `module/storage_gcs.go`, `module/platform_kubernetes_gke.go` (+ `_test.go` if present). Modify `module/iac_module.go` (remove `case "gcs":`). Modify `plugins/storage/plugin.go` (drop `"storage.gcs"` factory `:109`, capability `:39`, schema `:352`). Modify `DOCUMENTATION.md` (remove `storage.gcs`).

**Context:** Depends on **PR 3 release tag** (gcp plugin) + **locked-plan PR 9 (#681)** merged (gke wiring). `iac_state_gcs.go` (`gcs` backend) → gcp plugin (locked plan Task 21 — already on `feat/gcs-gke-storage` branch shipping in THIS plan's PR 3); `storage_gcs.go` → plugin-native (THIS plan Task 10); `platform_kubernetes_gke.go` (`gkeBackend` + its `gke` `init()` registration) → its `gke` dispatch flows through the locked plan's PR 9's `kubernetesBackendClientRegistry` + `grpcKubernetesBackend`. Mirrors locked B/C/D plan's Task 27.

**Step 1: Delete + strip** — `git rm` the 3 files (+ test files if present). In `module/iac_module.go`: delete `case "gcs":`; the `default:`-arm in-core list becomes `'memory', 'filesystem', 'postgres'`. In `plugins/storage/plugin.go`: remove `storage.gcs` factory + capability + schema. Update `DOCUMENTATION.md`.

**Step 2: Build** — `GOWORK=off go build ./...`. Verify: `grep -rn 'NewGCSIaCStateStore\|NewGCSStorage\|gkeBackend\|GCSIaCStateStore\|GCSStorage' --include='*.go' .` → expected no output.

**Step 3: Test** — `GOWORK=off go test ./module/... ./plugins/storage/...` → PASS.

**Step 4: Commit**
```bash
git add -A
git commit -m "refactor: delete in-core gcs store + storage.gcs + gkeBackend — now plugin-served"
```

---

### Task 18: workflow core — drop GCP SDKs from go.mod + permanent asymmetric CI gate

**Repo:** planning worktree (PR 5, SAME branch `feat/phase-c-core-deletion`) — `GOWORK=off`.

**Files:** Modify `go.mod`/`go.sum`, `scripts/audit-cloud-symbols.sh`, `.github/workflows/ci.yml`. Create `.phase-c-complete`.

**Context:** After Task 17, `cloud.google.com/go/storage` + `google.golang.org/api/*` have **zero** importers in core's build graph — `go mod tidy` drops them entirely. The permanent CI gate is **asymmetric** (locked B/C/D plan Task 28 — same spec): (a) `go list -deps ./...` asserts **zero** `Azure/azure-sdk-for-go` AND **zero** `cloud.google.com/go` / `google.golang.org/api` packages anywhere in core's build graph; (b) `audit-cloud-symbols.sh --check` asserts **zero** `aws-sdk-go-v2` imports under `module/` — AWS gone from `module/`, but `aws-sdk-go-v2` *remains* a `go.mod` entry for the out-of-scope `provider/aws/` etc. surface. `godo` remains — not asserted.

**Change class:** Build pipeline + go.mod dependency change + plugin loading path → runtime-launch-validation required.
**Rollback:** revert PR 5; deleted files recoverable from git; `go.mod` re-adds the GCP SDKs on `go mod tidy`. A running deployment that already cut over to plugin-served `gcs` must coordinate engine + plugin versions on rollback.

**Step 1: Tidy + marker** — `GOWORK=off go mod tidy && touch .phase-c-complete`. Confirm `go.mod` no longer lists `cloud.google.com/go/storage` or `google.golang.org/api`.

**Step 2: Permanent invariants** — in `scripts/audit-cloud-symbols.sh`, add the `--check` block: `GOWORK=off go list -deps ./... 2>/dev/null | grep -E 'Azure/azure-sdk-for-go|cloud\.google\.com/go|google\.golang\.org/api'` must be **empty** (FAIL if any line). Add a `module/`-scoped `aws-sdk-go-v2` zero-import assertion (the existing whole-repo map already separates `module/` from elsewhere — assert the `module/` count is 0). In `.github/workflows/ci.yml` `cloud-sdk-audit` job, confirm `audit-cloud-symbols.sh --check` runs and the new graph check executes there.

**Step 3: Build + audit + image-launch** —
```
GOWORK=off go build ./... && GOWORK=off go test ./...
bash scripts/audit-cloud-symbols.sh --check          # expect: audit-cloud-symbols: OK
GOWORK=off go list -deps ./... | grep -E 'Azure/azure-sdk-for-go|cloud\.google\.com/go|google\.golang\.org/api'  # expect: no output
```
Runtime-launch validation: build + launch server against representative `iac.state` / `platform.kubernetes` config; clean startup; transcript captured.

**Step 4: Commit**
```bash
git add go.mod go.sum scripts/audit-cloud-symbols.sh .github/workflows/ci.yml .phase-c-complete
git commit -m "build: drop GCP SDKs from go.mod + permanent asymmetric cloud-SDK CI gate"
```

---

### Task 19: workflow core — Phase C migration doc + final cross-phase verification

**Repo:** planning worktree (PR 5, SAME branch `feat/phase-c-core-deletion`) — `GOWORK=off`.

**Files:** Modify `docs/migrations/2026-05-15-plugin-modules-on-iac.md` (append Phase C). Modify `DOCUMENTATION.md` (final pass).

**Context:** Final docs + cross-phase coherence check.

**Step 1:** Phase C migration section: `iac.state backend: gcs` → load `workflow-plugin-gcp >= v<release>`; `platform.kubernetes provider: gke` → load gcp plugin (`provider: kind|k3s|eks|aks` unchanged, still core); `storage.gcs` → load gcp plugin (creds inline or `credentials_ref:` a `gcp.credentials` module); workflow `>= v0.53.0` engine floor. yaml `backend:`/`provider:`/module-type names unchanged.

**Step 2: Final verification** — render-preview the migration doc (no broken anchors); `bash scripts/audit-cloud-symbols.sh --check` → `OK` (both `.phase-b-complete` + `.phase-c-complete` present); `GOWORK=off go build ./... && GOWORK=off go test ./...` → green.

**Step 3: Commit**
```bash
git add docs/migrations/2026-05-15-plugin-modules-on-iac.md DOCUMENTATION.md
git commit -m "docs: Phase C migration guide + final plugin-modules-on-iac doc pass"
```

---

## Rollback (whole-plan)

This plan changes a **plugin SDK API surface**, **plugin loading paths**, and **`go.mod` dependency trees** — runtime-affecting per the `runtime-launch-validation` trigger list. Per-PR rollback:

- **PR 1 (SDK extension)** — additive; revert removes `Modules`/`Steps` fields + `mapBackedProvider` + the bridge delegate. Plugins that haven't started using the new fields are unaffected; plugins that have will fail to build against the reverted SDK and can pin the prior workflow version.
- **PR 2 + PR 3 (plugin PRs)** — additive plugin features. Revert is harmless to a workflow core that still has the in-core modules. On a defect, prefer a forward patch release over deleting a tag.
- **PR 4 (Phase B core deletion)** — reverting restores the in-core `spaces`/`storage.s3`/`step.s3_upload` paths + SDK-bearing resolvers; `go.mod` re-tidies. The `spaces` clean-break is the one external-user-visible compat break — PR 4 + the DO plugin `v1.1.0` release (locked plan's PR 5) roll back **as a matched pair**.
- **PR 5 (Phase C core deletion)** — reverting restores in-core `gcs`/`storage.gcs`/`gkeBackend` + re-adds the GCP SDKs on `go mod tidy`. A deployment already cut over to plugin-served `gcs` must coordinate engine + plugin versions on rollback (either also roll back the gcp plugin or keep the gcs-serving plugin installed since the reverted engine routes `backend: gcs` to the in-core case).
- **Forward-fix preferred:** each core deletion PR removes the in-core path only AFTER the plugin replacement is released and the dispatch is wired — a broken phase fails at PR CI (image-launch + audit-script gates), not in production.

## Notes for the executor

- **Team sizing:** 19 tasks → 3 implementers (per `subagent-driven-development` sizing). The `cloud-sdk-bcd` team is still active with implementer-1/-2/-3 + spec/code reviewers; reuse it (the team knows the patterns from the locked-plan execution) rather than spawn a fresh one.
- **Cross-repo discipline:** every PR-2/3 dispatch MUST name the absolute plugin-repo path and state it is a *different* repo than the worktree.
- **`GOWORK=off`** on every Go command in the planning worktree; never in the plugin repos.
- **The `git add` lines in task specs are illustrative, not exhaustive.** Always `git status` and stage every created/modified file (test fakes, generated `.pb.go`, `go.sum`).
- **PR 1 is the gate for all plugin work** — PRs 2/3 cannot ship until PR 1 is merged AND workflow `v0.53.0` (or the next workflow tag containing PR 1) is cut. The team-lead cuts that tag.
- **Dependency gates are real:** PR 4 needs PR 2 release tag + locked-plan PR 5 (#118) release tag; PR 5 needs PR 3 release tag + locked-plan PR 9 (#681) merged. The scope-lock per-task checkpoint applies.
- **PR 3 reuses an existing branch** (`feat/gcs-gke-storage`) that has the locked plan's Tasks 20+21+22 commits on it. Tasks 8-11 of THIS plan add on top. Ensure `git pull --ff-only origin feat/gcs-gke-storage` before each commit.
- **The Phase A precedent + locked B/C/D plan are the templates** — `workflow-plugin-azure` for the IaC-bridge plugin pattern; the locked plan's already-merged commits for the `s3`/`gcs`/`spaces` IaCStateBackend serve pattern. Cite + reuse; don't reinvent.
