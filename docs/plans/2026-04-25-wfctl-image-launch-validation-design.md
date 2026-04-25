# wfctl Ephemeral Image-Launch Validation + Dev Loop — Design

**Status:** Design-only. Execution deferred until explicitly ordered.
**Branch:** `design/wfctl-image-launch-validation`
**Date:** 2026-04-25
**Authors:** team-lead (codingsloth@pm.me)

## Why this exists

Three production-class incidents in 48 hours (workflow-plugin-migrations v0.3.0 timestamp panic; wfctl v0.18.10 release with three Copilot-caught bugs; BMW deploy v0.14.2-vs-v0.18.11 engine/tooling skew with `plugins.workflow.dev` DNS death) all shipped because nobody actually launched the built image and watched it start before merging. Every failure is detectable in <60 seconds on a laptop. Today, every workflow consumer (BMW, ratchet, workflow-cloud, future ones) writes their own bespoke image-launch CI workflow file or skips the gate entirely. wfctl already owns the build path, the plugin install path, the app.yaml schema, and the infra mapping — it should own the launch-and-validate path too.

## Goals

1. **One-command CI gate**: `wfctl validate launch` builds the image, spins up an ephemeral environment with the dependencies app.yaml requires, runs migrations, polls `/healthz`, scrapes startup logs for known failure signatures, exits 0/1. Replaces 100+ line bespoke yaml in every consumer.
2. **One-command dev loop**: `wfctl dev up --docker` extends the existing `wfctl dev` command tree (today: `--local`, `--k8s`) with a Docker-driven mode that mirrors what runs in CI but stays up, watches files, rebuilds + restarts the right component when source changes.
3. **Same machinery for both**: CI's one-shot is a special case of the dev loop's lifecycle (start → wait healthy → tear down). One synthesis layer, two front-ends.
4. **Cross-cut into IaC**: derive ephemeral environment from `infra.yaml` (managed pg → postgres container; managed redis → redis container; spaces → minio) so local-dev parity tracks production topology automatically.

## Non-goals

- NOT a multi-cloud emulator (no localstack-style AWS/DO/GCP API mocks).
- NOT a bundled UI dev server (defer to npm/vite/etc. — wfctl runs them, doesn't replace them).
- NOT a plugin marketplace browser.
- NOT a replacement for `docker compose` for arbitrary applications (only for workflow-engine-based apps).
- NOT a full Kubernetes-equivalent local environment (the existing `wfctl dev --k8s` minikube path stays as-is for that need).
- NOT a replacement for unit tests; this is the integration/launch gate above them.

## Existing scaffolding to extend

Found by code exploration (cmd/wfctl/*.go):

| Capability | Location | Reuse plan |
|---|---|---|
| `wfctl validate` (single cmd, not dispatcher) | `cmd/wfctl/validate.go:16` | Add sibling `wfctl validate launch` (top-level alias) and convert `validate` to a dispatcher with `config` + `launch` subcommands. Keep the bare `wfctl validate` invocation working as `validate config` for back-compat. |
| `wfctl build` (shells to `docker build`) | `cmd/wfctl/build.go:105`, `cmd/wfctl/build_image.go:8` | Reuse build orchestration; add a `--load` mode that ensures the image is in the local daemon for testcontainers to pick up by tag. |
| `wfctl dev up --local --k8s` | `cmd/wfctl/dev.go:13–26` | Add `--docker` mode alongside `--local` and `--k8s`. Same lifecycle (up/down/logs/status/restart). |
| `DetectPluginInfraNeeds(cfg, manifests)` | `cmd/wfctl/plugin_infra.go:54` | This is the **synthesis hook**. Reads `manifest.ModuleInfraRequirements[moduleType]` and returns `[]config.InfraRequirement`. Drives ephemeral container choice. |
| `resolveInfraOutput` + `syncInfraOutputSecrets` | `cmd/wfctl/infra_output_secrets.go:37,94` | Reverse-applied: instead of pulling DO state outputs into env vars, synthesize ephemeral output values (e.g. ephemeral pg DSN) and inject as env vars to the launched container. |
| `ExternalPluginManager.ReloadPlugin(name)` | `plugin/external/manager.go:190` | Hot-swap path for dev loop: rebuild plugin binary → call ReloadPlugin → engine re-attaches. Already supported. |
| `WriteStepSummary` | `cmd/wfctl/ci_output_summary.go:32` | Reuse for CI gate failure reports. Currently GHA-only; task #63 covers provider-agnostic extension and is a non-blocking dependency for this design. |
| `wfctl plugin install` | `cmd/wfctl/plugin_install.go:27` | Already populates `data/plugins/<name>/<name>`; both modes call this before container start. |
| Default health paths `/healthz` `/readyz` `/livez` | `module/health.go:26–31,49` | Default healthcheck target. User-overridable via `--healthcheck` flag. **Caveat**: `health:` module is opt-in in app.yaml, so the synthesis layer must verify presence and warn if absent. |

## Docker access: plugin-first, system-fallback

(Added per user feedback during brainstorm: "we're shelling to system docker? That seems a mistake, what if the system docker is missing? Can we make this a plugin setting so lib-docker can be embedded via plugin, and if missing, we use system docker?")

`wfctl build` today shells out to system `docker build` via `os/exec` (cmd/wfctl/build_image.go:8). That's a hidden runtime dependency: if Docker isn't installed, isn't in `$PATH`, is the wrong version, or is configured for the wrong context, every wfctl Docker operation fails with cryptic errors. Adding `wfctl validate launch` and `wfctl dev --docker` would compound the problem — every consumer is forced to reason about whose Docker they're using.

**Resolution**: introduce a Docker access abstraction backed by the existing plugin system.

### `DockerProvider` — strict proto-defined gRPC service from day one

(Hard requirement per user feedback during brainstorm: "Make sure that the new design re: docker is forcing strict grpc proto definitions so we avoid the issues we've been experiencing with loose mappings". Same principle driving v0.20.0 IaC proto enforcement (#41) and the all-plugins proto enforcement design (#76). The DockerProvider plugin is brand-new — it must NOT inherit the loose `InvokeMethod(name, map[string]any)` pattern that gave us the v0.18.11 Troubleshoot bug class. Strict proto from v1, no escape hatches.)

The DockerProvider is defined as a typed gRPC service under `proto/workflow/docker/v1/`. Every method has its own request/response message with `buf.validate` annotations on every field. Both client and server install `protovalidate-go` interceptors that reject malformed messages at the wire — same pattern as the IaC v0.20.0 design.

#### Schema sketch

```protobuf
// proto/workflow/docker/v1/docker_provider.proto
syntax = "proto3";
package workflow.docker.v1;
import "buf/validate/validate.proto";

service DockerProvider {
  rpc Build(BuildRequest) returns (BuildResponse);
  rpc Push(PushRequest) returns (PushResponse);
  rpc Pull(PullRequest) returns (PullResponse);
  rpc Run(RunRequest) returns (RunResponse);
  rpc Inspect(InspectRequest) returns (InspectResponse);
  rpc Logs(LogsRequest) returns (stream LogChunk);
  rpc Stop(StopRequest) returns (StopResponse);
  rpc Remove(RemoveRequest) returns (RemoveResponse);
  rpc CreateNetwork(CreateNetworkRequest) returns (Network);
  rpc RemoveNetwork(RemoveNetworkRequest) returns (RemoveNetworkResponse);
  rpc CreateVolume(CreateVolumeRequest) returns (Volume);
  rpc RemoveVolume(RemoveVolumeRequest) returns (RemoveVolumeResponse);
  rpc DaemonInfo(DaemonInfoRequest) returns (DaemonInfo);
}

message BuildRequest {
  string context_dir = 1 [(buf.validate.field).string.min_len = 1];
  string dockerfile = 2;  // optional, default "Dockerfile" relative to context_dir
  repeated string tags = 3 [(buf.validate.field).repeated = {
    min_items: 1,
    items: { string: { pattern: "^[a-z0-9./_:@-]+$" } }
  }];
  map<string, string> build_args = 4;
  string platform = 5 [(buf.validate.field).string.pattern = "^(|linux|windows)/(amd64|arm64|arm|386)$"];
  bool no_cache = 6;
  bool pull = 7;
}

message RunRequest {
  string image = 1 [(buf.validate.field).string.min_len = 1];
  string name = 2 [(buf.validate.field).string.pattern = "^[a-zA-Z0-9][a-zA-Z0-9_.-]*$"];
  repeated string command = 3;
  map<string, string> env = 4;
  repeated PortMapping ports = 5;
  repeated VolumeMount volumes = 6;
  string network = 7;
  Healthcheck healthcheck = 8;
  bool detach = 9;
  bool auto_remove = 10;
}

message PortMapping {
  uint32 container_port = 1 [(buf.validate.field).uint32 = { gt: 0, lte: 65535 }];
  uint32 host_port = 2 [(buf.validate.field).uint32 = { lte: 65535 }];  // 0 = ephemeral
  string protocol = 3 [(buf.validate.field).string = { in: ["tcp", "udp", "sctp", ""] }];
}

message Healthcheck {
  repeated string test = 1 [(buf.validate.field).repeated.min_items = 1];
  google.protobuf.Duration interval = 2 [(buf.validate.field).duration = { gt: { seconds: 0 } }];
  google.protobuf.Duration timeout = 3;
  int32 retries = 4 [(buf.validate.field).int32.gte = 0];
}

// ... full schema in proto/workflow/docker/v1/
```

(Exact surface grows from need; the above is the v1 sketch — Build/Run/Logs/Stop are minimum for `validate launch`. Schema lives under `proto/workflow/docker/v1/` with `buf.gen.yaml` codegen and `buf lint`/`buf breaking` CI gates — same toolchain as v0.20.0 IaC.)

#### Plugin SDK pattern

`plugin/sdk/docker/`:
- `interceptor.go` — `protovalidate-go` server + client unary/streaming interceptors. Validate request INPUT, validate response OUTPUT. Reject with gRPC `InvalidArgument` (request-side) or `Internal` (response-side) on validation failure.
- `server.go` — `NewDockerProviderServer` helper wires interceptor into HashiCorp go-plugin's gRPC server.
- `client.go` — typed client constructor with interceptor.
- `helpers.go` — small constructors for common types (e.g. `BuildOptsForDockerfile(dir)`).

Same SDK pattern as `plugin/sdk/iac/` from the v0.20.0 design. Where appropriate, the interceptor module is shared between SDKs (it's generic; the validate.proto annotations on each service's messages are what differ).

#### Validation guarantees on the wire

Every DockerProvider RPC enforces at the boundary:
- `image` is non-empty; references match an OCI tag/digest pattern
- Port numbers in 1–65535 (container) / 0–65535 (host, where 0 = ephemeral)
- Container names match Docker's actual `^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`
- Healthcheck interval `> 0`
- Tags array is non-empty for Build; each tag is a valid OCI reference
- Platform string matches `os/arch` shape
- Build context_dir is non-empty and resolves to an existing path

If a request arrives with `image: ""` or `ports: [{container_port: 70000}]`, the server rejects with `InvalidArgument: image: must not be empty` *before* hitting the Docker daemon. No silent dropping, no `map[string]any` wrapping, no escape hatch.

#### `systemDockerFallback` parity

The system fallback (in-tree `os/exec`-based) implements the SAME Go interface synthesized from the proto. It does NOT go through gRPC (it's in-process), but the input/output types it consumes/returns match the proto-generated Go types — so call sites switch between providers with no code change. Validation runs locally before exec'ing `docker` (the same `protovalidate-go` library, just invoked client-side instead of via interceptor). Effectively: the proto types ARE the Go interface, regardless of transport.

This means even users who never install `workflow-plugin-docker` still get the strict-proto guarantees — empty image, bad ports, malformed tags all caught before `docker` ever runs. No regression to the `map[string]any` lossiness via the fallback.

#### Why this matters

- The v0.18.11 Troubleshoot bug ("missing resource_type arg silently dropped") cannot occur on a strict-proto RPC. There is no map-of-any to omit a key from; either the field is in the typed message or the request doesn't compile.
- New plugin types (Podman, nerdctl, BuildKit-only) implement the same proto, get the same wire validation for free.
- Cross-language tooling (a Rust wfctl, a TS dev-tools layer, etc.) gets the same contract via standard codegen.
- The pattern proven on IaC (v0.20.0) and being generalized in #76 (all-plugins) extends naturally — DockerProvider becomes the second category after IaC.

#### Sequencing implication for proto tooling

The `buf` toolchain (buf.yaml, buf.gen.yaml, lint+breaking CI) being introduced in v0.20.0 IaC proto enforcement (#41) is reused here. DockerProvider proto lives alongside IaC proto under `proto/workflow/<category>/v1/`, sharing the same generated code layout and the same `plugin/sdk/<category>/` SDK pattern. Whichever workstream lands the buf scaffolding first, the other inherits. This design **does not** introduce duplicate proto tooling.

### Two providers, one interface

1. **`workflow-plugin-docker`** (new external gRPC plugin, embedded library impl)
   - Statically links `github.com/docker/docker/client` (the Moby Go SDK).
   - Talks directly to the Docker daemon socket (`/var/run/docker.sock` on Linux/macOS, `npipe://./pipe/docker_engine` on Windows).
   - No system `docker` binary required — only the daemon.
   - Plugin is registered in workflow-registry as a builtin/auto-install candidate.
   - Same plugin SDK pattern as DO/AWS/GCP/Azure providers.
   - Bonus: same plugin can also wrap testcontainers-go and compose-go (via embedded calls), giving wfctl a single consistent surface.

2. **`systemDockerFallback`** (in-tree, no plugin needed)
   - Shells to `docker` binary on `$PATH` via `os/exec`.
   - Wraps current `wfctl build` behavior under the same `DockerProvider` interface.
   - The existing code in `cmd/wfctl/build_image.go:8` becomes the implementation.
   - Activates when no `workflow-plugin-docker` is installed.

### Selection logic

```
On wfctl Docker operation:
  if workflow-plugin-docker is installed AND --docker-mode != "system":
      use plugin
  else if --docker-mode == "plugin" (explicit):
      try plugin install (auto), retry
      on failure: error "plugin not available, run `wfctl plugin install workflow-plugin-docker`"
  else:
      check `docker` binary on $PATH
      if absent: error "no Docker provider available (system docker not on $PATH, plugin not installed)"
      else: use systemDockerFallback
```

CLI: `wfctl ... --docker-mode={plugin,system,auto}` (default `auto` — prefer plugin if installed, else system).

### Why this is the right shape

- **Optional dependency at the binary level**: a fresh `wfctl` install with no Docker doesn't fail loudly until a Docker-needing command runs. `wfctl validate config` (no Docker), `wfctl plugin install`, `wfctl infra plan` (no Docker) all keep working without Docker.
- **Pluggable backend**: future providers (Podman, nerdctl, BuildKit-only without daemon) become drop-in plugins implementing `DockerProvider`. No core wfctl change needed.
- **Aligned with the rest of wfctl**: every other workflow capability (IaC, agents, payments, security, supply-chain) is plugin-shaped. Docker access shouldn't be the special-case shellout.
- **Backwards compatible**: existing CI that already has system `docker` works unchanged via the fallback. Adopting the plugin is opt-in.
- **Cleans up #51** (Follow-up: reproducible bmw-plugin build + Docker layer reuse) — once Docker access is plugin-mediated, the plugin can implement BuildKit caching, layer reuse, etc. uniformly.

### Side effects of this addition

- **Refactor `wfctl build`** (`cmd/wfctl/build.go`, `build_image.go`) to use `DockerProvider` rather than `os/exec`. Existing behavior preserved via `systemDockerFallback`.
- **`wfctl validate launch` and `wfctl dev --docker`** consume `DockerProvider` from day one — never `os/exec`.
- **testcontainers-go integration**: testcontainers-go itself uses `docker/docker/client`. The plugin can use it directly without additional dependency hops; the system fallback can't run testcontainers-go (compose features depending on the SDK), so the system fallback is **less capable** — it can do `Build/Run/Logs/Stop` via shellout but cannot do testcontainers' wait-strategies natively. Acceptable degradation: with system fallback, `wfctl validate launch` falls back to crude polling instead of testcontainers' richer wait primitives.
- **Plugin install bootstrapping**: `wfctl validate launch` first run prompts to install workflow-plugin-docker if it's missing and the system has Docker available; user can `--docker-mode=system` to skip.

### Sequencing implication

- v0.18.12 MVP ships **the abstraction + system fallback** (so existing behavior is preserved and the surface is in place).
- v0.18.13 / v0.19.0 ships **`workflow-plugin-docker`** with the embedded library impl. Until then, `--docker-mode=plugin` is a no-op + warning (system used).
- Refactoring `wfctl build` to use the abstraction can happen in v0.18.12 or be deferred to v0.19.0 — defer is safer (fewer code paths in flight at once).

## Three approaches considered

### Approach A — testcontainers-go only (greenfield-only synthesis)

Use `github.com/testcontainers/testcontainers-go` exclusively. Drop user-supplied compose.yaml entirely; everything is synthesized from app.yaml + infra.yaml.

**Pros:**
- Single library, single code path, smallest cognitive load.
- Lightweight dep; ~12 MB transitive vs. compose-go's heavier tree.
- Native Go API for healthcheck wait strategies (HTTP, log line, port, custom).
- Doesn't conflict with `wfctl build`'s existing `os/exec docker build` — testcontainers talks to the same Docker daemon.

**Cons:**
- Users with sophisticated existing compose.yaml setups (e.g., custom test fixtures, Postgres replicas, mock services) get no path. Either they fight wfctl's synthesis or they bypass it.
- Adopting testcontainers-go means owning every dependency (e.g., Stripe webhook mock, Redis cluster topology) as code in workflow rather than declaratively in yaml.
- Breaking change later if compose support is added — users would re-author.

### Approach B — compose-go + Docker SDK only (compose-first)

Always require a `compose.test.yaml`. wfctl reads it via `compose-spec/compose-go`, runs containers via the Docker SDK or shells to `docker compose up`. Synthesizes a starting compose.yaml on first run via `wfctl scaffold compose`.

**Pros:**
- Familiar to anyone who's used `docker compose` — zero ramp-up.
- User has full escape hatch (edit the yaml).
- One canonical source of truth: the compose.yaml file in the repo.

**Cons:**
- Forces every consumer to author and maintain a yaml file even for the simple "synthesize from app.yaml" case. BMW would commit `compose.test.yaml` + keep it in sync with app.yaml plugins. Doubled maintenance.
- compose-go itself is fine; running compose well in Go means either reimplementing `docker compose up`'s logic (depends_on, profiles, healthchecks, networks, volume lifecycle) or shelling to `docker compose` (process management hell, version-pin sensitivity).
- `docker/compose/v2` as a library is heavyweight (~80 MB transitive deps) and not designed for embedding; it expects to be the binary entry point.

### Approach C — Hybrid: testcontainers-go for synthesis path, compose-go for user-supplied path (RECOMMENDED)

Two front-ends, one shared lifecycle interface.

- **Synthesis mode (default, no compose.yaml present)**: testcontainers-go drives. wfctl reads app.yaml + infra.yaml, calls `DetectPluginInfraNeeds`, picks containers from a built-in mapping table (`infra.database` → `postgres:16`, `infra.cache` → `redis:7`, `infra.spaces` → `minio/minio`), wires a Docker network, runs `wfctl plugin install` to populate `data/plugins/`, mounts that into the app container, starts everything with healthchecks, polls.
- **Compose mode (compose.test.yaml present, or `--compose <path>`)**: parse via `compose-spec/compose-go`, run via testcontainers-go's `compose` module (which wraps compose execution without dragging in `docker/compose/v2` directly). Or, if user prefers, shell to `docker compose up -d` and use testcontainers-go for the healthcheck poll layer only.
- **Hybrid mode (compose.test.yaml present + `infra:` directive)**: compose runs user's services; wfctl synthesizes anything declared in app.yaml that isn't in the compose. E.g., user's compose runs Stripe webhook mock; wfctl auto-adds postgres because compose doesn't include it. Resolved via name conflict: compose wins ties.
- **Migration path**: `wfctl scaffold compose` writes the synthesized environment as a starting compose.test.yaml so users who outgrow synthesis have a no-friction graduation.

**Why C is the recommendation:**
- Most consumers (BMW today, future single-app-yaml shops) get zero-yaml synthesis — `wfctl validate launch` Just Works.
- Advanced consumers (workflow-cloud's multi-tenant tests, ratchet's mock SaaS fixtures) keep their custom compose.
- Both paths share the lifecycle interface (start → wait healthy → tear down) implemented by testcontainers-go's container/compose primitives, so dev loop hot-reload, log streaming, etc. work identically.
- New dependency surface is bounded: testcontainers-go (~12 MB) + compose-go (~3 MB), no docker/compose/v2.

**Tradeoffs accepted:**
- Two code paths to maintain. Mitigated by sharing the lifecycle interface and keeping compose mode thin (parse + delegate to testcontainers' compose module).
- Edge cases at the synthesis/compose boundary (hybrid mode). Mitigated by clear precedence rule (compose wins) and explicit conflict warnings.

**Library pin choice:**
- `github.com/testcontainers/testcontainers-go` (latest stable; Apache-2.0; well-maintained, 8k+ stars, used by ent, Kubernetes integration tests).
- `github.com/compose-spec/compose-go` (latest stable; Apache-2.0; the official compose spec parser).

---

## Architecture

### Layer 1: Synthesis (app.yaml → infra plan)

```
app.yaml + infra.yaml + manifests
        │
        ▼
DetectPluginInfraNeeds() ───► []InfraRequirement
        │                       (db, cache, blob, queue, …)
        │
        ▼
synthesizeEphemeralEnv()
        │
        ▼
EphemeralEnv {
  Containers []ContainerSpec  // pg, redis, minio, …
  AppContainer ContainerSpec   // built from `wfctl build`
  PreDeploySteps []Step        // migrations
  HealthChecks []HealthSpec    // /healthz with timeout
  EnvVars map[string]string    // synthesized DSNs
  Network string               // bridge name
}
```

Mapping table (initial):

| infra requirement | image | port | healthcheck | DSN env-var name |
|---|---|---|---|---|
| `infra.database` (pg) | `postgres:16-alpine` | 5432 | `pg_isready` | `DATABASE_URL` (synthesized) |
| `infra.cache` (redis) | `redis:7-alpine` | 6379 | `redis-cli ping` | `REDIS_URL` |
| `infra.spaces` (s3) | `minio/minio:latest` | 9000 | HTTP `/minio/health/live` | `S3_*` (access/secret/endpoint) |
| `infra.queue` (sqs/etc) | TBD per provider | — | — | — |

Mapping is data-driven (YAML/JSON in wfctl) so contributors can add types without code changes for net-new resources.

### Layer 2: Lifecycle (testcontainers-go)

```
EphemeralEnv ───► Lifecycle interface
                    │
                    ├── Up(ctx) error          // create network, start containers, wait healthy
                    ├── Down(ctx) error        // stop, remove, prune volumes
                    ├── Logs(ctx, name) io.Reader
                    ├── Restart(ctx, name) error
                    ├── Reload(ctx, name) error  // for plugin hot-swap via go-plugin
                    └── Healthz(ctx, target string, timeout time.Duration) error
```

Implemented by:
- `synthesisLifecycle` (testcontainers-go containers + network)
- `composeLifecycle` (testcontainers-go compose module)
- `hybridLifecycle` (compose + supplemental synthesized containers)

### Layer 3: Front-ends

#### `wfctl validate launch` (CI gate)

```
wfctl validate launch \
  [--config app.yaml] \
  [--compose compose.test.yaml]   # optional, switches mode \
  [--healthcheck /healthz] \
  [--healthcheck-host http://localhost:8080] \
  [--timeout 60s] \
  [--keep-on-failure]              # don't tear down on fail (debugging)
```

Behavior:
1. `wfctl build --load` to ensure the image is in the local daemon by tag `wfctl-validate:<sha>`.
2. Synthesize or load compose.
3. Run pre-deploy steps (migrations) if app.yaml declares them.
4. Up the lifecycle.
5. Poll healthcheck until 200 or timeout.
6. Scrape app container logs for known failure signatures: `Setup error`, `fetch manifest from remote`, `plugin not loaded`, `panic:`, `failed to build engine`, `dial tcp.*no such host`. Each signature → structured failure block in summary.
7. On failure: emit `WriteStepSummary` with logs, exit code, healthz timeline, container exit reasons. Exit 1.
8. On success: emit terse summary, exit 0.
9. Always `Down()` (defer) unless `--keep-on-failure`.

#### `wfctl dev up --docker` (dev loop)

Extends the existing `wfctl dev` (cmd/wfctl/dev.go:13–26) with a third mode alongside `--local` and `--k8s`. Same `up`/`down`/`logs`/`status`/`restart` subcommands.

```
wfctl dev up --docker [--watch] [--no-rebuild]
```

Behavior delta vs. `validate launch`:
- Stays up. Streams logs to terminal with per-service prefixes + colors.
- File watcher (`fsnotify`-based, no external `air`/`gow` dep — minimal, ~50 LOC) watches:
  - `cmd/server/`, `cmd/<binary>/` Go source → rebuild server binary, container restart
  - `<plugin-dir>/` Go source → rebuild plugin binary, call `ReloadPlugin` (no container restart)
  - `migrations/` SQL → run `wfctl migrate up` against ephemeral pg, no restart
  - `app.yaml` → restart container (config hot-reload deferred — engine doesn't support today)
  - `ui/src/` → no-op; user runs `npm run dev` in a separate terminal, accessed via vite proxy or `wfctl dev` exposes the engine on a port the vite proxy targets
- `wfctl dev down` tears down + clears volumes (with confirmation if `--no-purge` not set).
- `wfctl dev status` shows container state + last restart + watcher state.
- `wfctl dev logs <name>` streams logs for a service.

### Layer 4: IaC cross-cut

#### IaC informs local/CI

`infra.yaml` declares `infra.database` with provider `digitalocean` and tier `db-s-1vcpu-1gb`. Local synthesis ignores the provider/tier and picks `postgres:16-alpine` from the mapping table. The same `app.yaml` config consumes `DATABASE_URL` either way — synthesized in dev/CI, injected from DO state outputs in production.

This means `wfctl validate launch` validates the same engine config that `wfctl infra apply` will deploy. Drift between local/staging/prod becomes drift between the synthesis mapping and the actual infra plugin behavior — testable.

#### Local/CI informs IaC

When `wfctl validate launch` succeeds, wfctl can mark the SHA as "launch-validated" in a small `.wfctl-validated.json` file. `wfctl infra plan` and `wfctl ci run --phase deploy` can warn if the current SHA isn't validated. This is opt-in advisory, not a block — but surfaces the gap.

#### Equivalence boundary (explicit non-goals)

| Aspect | In scope | Out of scope |
|---|---|---|
| Engine boots | YES | — |
| Plugins load | YES | — |
| Config schema valid | YES | — |
| Migrations run | YES | — |
| HTTP routes wire | YES | — |
| `/healthz` returns 200 | YES | — |
| Cloud autoscale behavior | — | Out (DO App Platform autoscale, no local equivalent) |
| Region-specific networking | — | Out (DO Spaces region peculiarities) |
| Read-replica routing | — | Out (RDS replica failover) |
| Real Stripe/external API integrations | — | Out (use mocks via compose mode) |
| Production-grade load testing | — | Out (use k6/artillery against real staging) |

### Layer 5: CI summary integration

Reuse `WriteStepSummary` (cmd/wfctl/ci_output_summary.go:32). On failure, emit:

```markdown
## ❌ wfctl validate launch — image launch failed

**Build SHA:** abc1234
**Mode:** synthesis (no compose.test.yaml found)
**Containers:** postgres:16-alpine ✅ (healthy 4s), wfctl-validate:abc1234 ❌

### Healthz timeline
- 00:00 container start
- 00:02 first poll: connection refused
- 00:08 first poll: connection refused
- 00:14 container exited with code 1
- 00:14 abort: container not running

### Failure signatures
- `Setup error: failed to build engine: ... fetch manifest from remote: dial tcp ... no such host` (line 12)

### Container logs (last 50 lines)
```
<grep'd interesting lines>
```

### Suggested actions
- Verify all `requires:` plugins in app.yaml are pre-installed in the image
- Check workflow-server version alignment with wfctl version (skew?)
```

Provider-agnostic emission depends on task #63 (multi-CI provider summary). For v0.18.12 MVP, GHA-only is acceptable; GitLab/etc. fall back to stderr.

## Phasing

This is too large for one release. Three phases:

### v0.18.12 — `wfctl validate launch` MVP (CI gate)

- Synthesis mode only (testcontainers-go).
- `--config app.yaml` derives ephemeral env via `DetectPluginInfraNeeds`.
- Healthz poll + log signature scrape.
- WriteStepSummary on failure (GHA only initially).
- Mapping table: pg, redis (cover BMW + ratchet + workflow-cloud).
- Replaces task #81 (BMW docker-compose CI integration test) — BMW becomes a one-line CI step.

**Out of v0.18.12:** compose mode, `wfctl dev --docker`, IaC cross-cut, hot-reload, file watcher.

### v0.19.0 — `wfctl dev up --docker` + compose mode

- Dev loop with file watcher + rebuild + restart.
- Plugin hot-swap via `ReloadPlugin`.
- `compose-spec/compose-go` parsing.
- Hybrid mode (compose + supplemental synthesis).
- `wfctl scaffold compose` for migration path.

### v0.20.0+ — IaC cross-cut + advisory gates

- `.wfctl-validated.json` marker.
- `wfctl infra plan` reads marker, advises on unvalidated SHA.
- Per-provider local mappings as plugin-extensible (DO plugin contributes `infra.database` → `postgres:16` mapping; AWS plugin same; Azure same).

## Sequencing relative to other in-flight design work

| Workstream | Status | Relationship |
|---|---|---|
| v0.18.11.1 Troubleshoot RPC fix (#80) | PR #481, awaiting merge | Independent; lands first. |
| v0.18.12 placeholder for WriteStepSummary (#71) | Pending | This design **subsumes** #71. WriteStepSummary work happens as part of the launch-failure summary emission. |
| v0.19.0 plugin manifest split (#42) | Pending | Compatible. Manifest `ModuleInfraRequirements` is already the synthesis input; if v0.19.0 changes the manifest format, the synthesis layer adapts. |
| v0.20.0 IaC proto enforcement (#41) | Design committed | Compatible. Phase 3 IaC cross-cut benefits from typed IaCProvider — every provider can declare its local mapping via proto. |
| Task #76 all-plugins proto enforcement | Design committed | Compatible. Synthesis layer is engine-side, doesn't touch plugin RPC. |
| Task #79 BMW migrations CI | Pending | This design **subsumes** #79. Migrations run in `wfctl validate launch`. BMW gets it for free. |
| Task #81 BMW image-launch CI | Pending | This design **subsumes** #81. BMW's CI step becomes `wfctl validate launch`. |
| Task #87 superpowers improvements | Pending | This design feeds back: a "wfctl validate launch" gate becomes part of the finishing-a-development-branch skill's mandatory steps for runtime-affecting changes. |

## Error handling

- **Docker daemon not running**: detect early, fail fast with `wfctl validate launch: Docker daemon not reachable. Start Docker Desktop or `dockerd` and retry.`
- **Image build failure**: surface `wfctl build` error verbatim; do not start anything.
- **Migration failure**: capture migrate logs, emit in summary; tear down (or `--keep-on-failure`).
- **Healthz timeout**: dump container logs, emit signature scrape, exit 1.
- **Container OOM / panic during boot**: detect via container exit code; emit container logs.
- **Network hangs (e.g. plugin trying to fetch from `plugins.workflow.dev`)**: signature scrape catches; explicit error message in summary.
- **Cleanup failure on Down**: warn but don't fail the test (resource leak detection is best-effort).

## Testing strategy

- **Unit tests**: synthesis layer (`DetectPluginInfraNeeds` → `EphemeralEnv` mapping); log signature scraper; healthz timeline emitter. Pure Go, no Docker.
- **Integration tests**: `wfctl validate launch` against a tiny test app.yaml in `cmd/wfctl/testdata/`. Requires Docker daemon. Gated behind `-tags docker_integration` build tag so unit `go test ./...` doesn't require Docker.
- **Acceptance tests**: end-to-end with BMW's actual app.yaml (committed test fixtures), exercises the BMW deploy failure modes from this session as red-light cases.
- **Test-catches-regression invariant** (per session feedback): for each known failure signature, a test stages a container that emits exactly that signature and asserts the scraper catches it.

## Open questions for the implementation plan

These are explicitly punted to writing-plans / implementation, not blockers for this design:

1. testcontainers-go vs ory/dockertest comparison — both serve, testcontainers is industry standard but dockertest is lighter; team-lead committed to testcontainers-go in the recommendation, but the implementation plan should validate the perf/dep cost on first use.
2. Default healthz path discovery — engine's `health:` module is opt-in; should `wfctl validate launch` warn if absent and offer to inject? Or fail loudly?
3. Plugin SDK contract for `local_image` — when AWS/GCP/Azure/DO plugins start declaring local equivalents (v0.20.0+), what's the proto field and registry?
4. `wfctl scaffold compose` output: include healthchecks, networks, volumes? Profile sections? Match testcontainers' synthesis exactly?
5. Performance targets: launch time, teardown time, file-watcher debounce, plugin reload latency.

## Adjacent task surfaces

This design touches or absorbs these existing tasks:

- **#71** (wfctl v0.18.12 WriteStepSummary into apply/deploy failure paths) — absorbed; this design uses the same emitter.
- **#63** (wfctl provider-agnostic CI summary — GHA + GitLab + others) — strict prereq for full v0.18.12 ship; MVP can ship GHA-only with #63 as a follow-up.
- **#79** (BMW: ephemeral pg migrations CI per PR) — absorbed; BMW migrates as a side-effect of `wfctl validate launch`.
- **#81** (BMW: docker-compose CI integration test) — absorbed; BMW's CI becomes one-line `wfctl validate launch`.
- **#78** (`wfctl migrations validate` subcommand) — partially absorbed; the migrate-against-ephemeral-pg subset is in `wfctl validate launch`. Standalone command still useful for migration-only validation flows.

## Risk and rollback

- **Adopting new deps (testcontainers-go, compose-go) increases supply chain surface.** Mitigation: pin versions in go.mod; track via supply-chain plugin; revisit annually.
- **Rollback**: design is additive — `wfctl validate launch` is a new command. Removing it is a non-breaking change. The few tasks (#71, #79, #81) it absorbs revert to standalone tasks if cancelled.
- **Risk that synthesis mapping diverges from real cloud behavior**: explicit non-goal (see equivalence boundary). Mitigation: document the boundary, surface drift in summary output (e.g. "this validation does not test DO Spaces region replication; staging is your check").

## Approval

This design is approved by the user (codingsloth@pm.me) via the autonomous-mode brainstorm dispatch. The implementation plan derived from this design will be written by `superpowers:writing-plans` and verified by `superpowers:alignment-check`. **Execution will not begin** until the user explicitly orders it; the alignment-check PASS is the terminal state for this run.
