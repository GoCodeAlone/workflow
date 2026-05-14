# Cloud-SDK Extraction Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Extract the Azure SDK (and establish the reusable `IaCStateBackend` gRPC contract + host-resolution pattern) out of workflow core's `module/` package into the `workflow-plugin-azure` sidecar, so `Azure/azure-sdk-for-go` drops from core's `go.mod` entirely.

**Architecture:** A new strict `IaCStateBackend` gRPC service is added to `plugin/external/proto/iac.proto`, mapping 1:1 onto the existing 6-method `module.IaCStateStore` interface. Core's `iac.state` module stays, but its hardcoded backend `switch` gains a path that resolves an `IaCStateBackend` gRPC client from a loaded plugin. Phase 0 is a mechanical precursor that splits the one remaining mixed cloud-backend file. Phase A implements the contract end-to-end for the `azure_blob` backend — the pattern every later phase reuses.

**Tech Stack:** Go 1.26+, `buf` for proto generation, `hashicorp/go-plugin` gRPC sidecars, the `modular` framework, `superpowers:executing-plans` TDD loop.

**Base branch:** main (worktree branch `feat/cloud-sdk-extraction` already carries the committed design doc + `scripts/audit-cloud-symbols.sh`)

**Design:** `docs/plans/2026-05-14-cloud-sdk-extraction-design.md` (adversarial review PASS, cycle 11)

---

## Scope Manifest

**PR Count:** 5
**Tasks:** 14
**Estimated Lines of Change:** ~1800 (informational; not enforced)

**Out of scope:**
- **Phases B (AWS), C (GCP), D (DigitalOcean)** — deferred to a follow-on plan authored *after* Phase A merges. Their concrete tasks genuinely depend on Phase A's outputs: the benchmark-validated `IaCStateBackend` proto shape, the host-side gRPC-client resolution pattern, and the plugin-side state-backend serve path. Planning them now would be fiction. The design (`docs/plans/2026-05-14-cloud-sdk-extraction-design.md`) is the authoritative spec for B/C/D; this plan delivers Phase 0 + Phase A, which the design explicitly designates as the "validates the contract end-to-end" increment.
- The out-of-`module/` AWS SDK surface (`provider/aws/`, `plugin/rbac/aws.go`, `iam/aws.go`, `artifact/s3.go`) — per design Non-Goals (the #653-retained "RBAC/secrets/artifact stay" surface).
- `github.com/digitalocean/godo` extraction — per design Non-Goals.
- `aws-sdk-go-v2/service/kinesis` — transitive via `modular`, per design Non-Goals.
- Touching the comment-only stubs `nosql_dynamodb.go` / `storage_artifact_s3.go` — they carry no SDK.
- Changing `wfctl plugin install` discovery flow.

**PR Grouping:**

| PR # | Title | Tasks | Branch |
|------|-------|-------|--------|
| 1 | Phase 0: split platform_kubernetes_kind.go + wire audit script into CI | Task 1, Task 2, Task 3 | feat/cloud-sdk-extraction-p0 |
| 2 | Phase A: IaCStateBackend proto + benchmark harness + proto lock | Task 4, Task 5, Task 6 | feat/cloud-sdk-extraction-pa-proto |
| 3 | Phase A: host-side IaCStateBackend resolution + secret-redaction + gRPC-logging guard | Task 7, Task 8, Task 9, Task 10 | feat/cloud-sdk-extraction-pa-host |
| 4 | Phase A: workflow-plugin-azure implements azure_blob IaCStateBackend | Task 11, Task 12 | (cross-repo: workflow-plugin-azure `feat/azure-blob-state-backend`) |
| 5 | Phase A: core deletes iac_state_azure.go + strips azure_blob case → drops azure-sdk from go.mod | Task 13, Task 14 | feat/cloud-sdk-extraction-pa-core |

**Status:** Draft

---

## Cross-repo note

PR 4 lands in a **different repository** (`/Users/jon/workspace/workflow-plugin-azure`), not the `workflow` worktree. The executing pipeline must create a branch + PR there separately. PR 4's plugin release (a tagged version implementing the published proto) **must merge and tag before PR 5** — PR 5's core deletion makes `backend: azure_blob` fail to build unless the plugin version implementing `IaCStateBackend` is loadable. PRs 2 and 3 can land in either order relative to each other but both precede PR 4 (the plugin needs the published proto) and PR 5.

---

## PR 1 — Phase 0: split `platform_kubernetes_kind.go` + wire audit script into CI

Mechanical, behavior-equivalent precursor. After this PR, no `init()` registers both a core-staying and a plugin-bound Kubernetes backend, and the single SDK-bearing platform file (`platform_kubernetes_gke.go`) is isolated for a later clean deletion.

### Task 1: Split `platform_kubernetes_kind.go` into `_core.go` + `_gke.go`

**Files:**
- Create: `module/platform_kubernetes_core.go`
- Create: `module/platform_kubernetes_gke.go`
- Modify: `module/platform_kubernetes_kind.go` (becomes empty → delete) — `git rm` it at the end
- Test: `module/platform_kubernetes_test.go` (existing — must stay green; no new test file, this is pure code movement verified by the existing suite + build)

**Step 1: Establish the baseline — run the existing suite green before touching anything**

Run: `go test ./module/ -run 'Kubernetes|Platform' -v`
Expected: PASS (all existing kubernetes/platform tests green — this is the behavior-equivalence baseline)

**Step 2: Create `module/platform_kubernetes_core.go`**

Move into this new file, verbatim, from `platform_kubernetes_kind.go`:
- the `kindBackend` type + all its methods (`plan`/`apply`/`status`/`destroy` and helpers)
- the `eksErrorBackend` type + all its methods
- the `aksBackend` type + all its methods (incl. `azureToken`, `aksResourceGroup`, `aksLocation`, `aksSubscriptionID`, `buildAgentPools`) — `aksBackend` is SDK-free (`net/http` OAuth2), it stays in core
- a new `func init()` registering **only** the four core-staying names:

```go
func init() {
	RegisterKubernetesBackend("kind", func(_ map[string]any) (kubernetesBackend, error) {
		return &kindBackend{}, nil
	})
	RegisterKubernetesBackend("k3s", func(_ map[string]any) (kubernetesBackend, error) {
		return &kindBackend{}, nil
	})
	RegisterKubernetesBackend("eks", func(_ map[string]any) (kubernetesBackend, error) {
		return &eksErrorBackend{}, nil
	})
	RegisterKubernetesBackend("aks", func(_ map[string]any) (kubernetesBackend, error) {
		return &aksBackend{}, nil
	})
}
```

The import block for `_core.go` is exactly the imports those three backends use: `bytes`, `context`, `encoding/json`, `fmt`, `io`, `net/http`, `net/url`, `strings`, `time`, and `github.com/GoCodeAlone/workflow/internal/legacyaws` (the `eksErrorBackend` stub dependency). **No `google.golang.org/api` import** — that belongs only in `_gke.go`.

**Step 3: Create `module/platform_kubernetes_gke.go`**

Move into this new file, verbatim, from `platform_kubernetes_kind.go`:
- the `gkeBackend` type + all its methods (`gkeLocation`, `gkeProject`, `plan`, `apply`, `status`, `destroy`, `containerService`)
- a new `func init()` registering **only** `gke`:

```go
func init() {
	RegisterKubernetesBackend("gke", func(_ map[string]any) (kubernetesBackend, error) {
		return &gkeBackend{}, nil
	})
}
```

The import block for `_gke.go` is exactly what `gkeBackend` uses, including `container "google.golang.org/api/container/v1"` and `"google.golang.org/api/option"`.

**Step 4: Delete the now-empty original file**

Run: `git rm module/platform_kubernetes_kind.go`
(All four backend types + the old `init()` have been moved out; the file is empty.)

**Step 5: Build + vet**

Run: `go build ./... && go vet ./module/...`
Expected: exit 0, no errors (pure code movement — every symbol still resolves, the SDK imports just live in different files)

**Step 6: Run the kubernetes/platform suite — behavior equivalence**

Run: `go test ./module/ -run 'Kubernetes|Platform' -v`
Expected: PASS — identical result to Step 1. The same five backend names (`kind`/`k3s`/`eks`/`gke`/`aks`) are registered after the split as before.

**Step 7: Confirm the audit script sees the split correctly**

Run: `bash scripts/audit-cloud-symbols.sh`
Expected: under "azure-sdk-for-go" the only `module/` entries are `iac_module.go` + `iac_state_azure.go` (both REAL) and `cloud_account_azure.go` (comment-only); `platform_kubernetes_kind.go` no longer appears (file deleted); under "google.golang.org/api" the gke real-import file is now `module/platform_kubernetes_gke.go`.

**Step 8: Commit**

```bash
git add module/platform_kubernetes_core.go module/platform_kubernetes_gke.go
git rm module/platform_kubernetes_kind.go
git commit -m "refactor(module): split platform_kubernetes_kind.go into _core + _gke

Phase 0 precursor for cloud-SDK extraction. kindBackend/eksErrorBackend/
aksBackend (all SDK-free) move to platform_kubernetes_core.go with a core
init(); gkeBackend (the only SDK-bearing k8s backend) moves to
platform_kubernetes_gke.go with its own init(). Behavior-equivalent: same
five backend names registered. Isolates the lone SDK-bearing platform
file for a later clean deletion."
```

Rollback: `git revert` — pure code movement, no behavior diff, no contract, no go.mod change.

---

### Task 2: Fix the stale Azure-SDK doc comment

**Files:**
- Modify: `module/platform_kubernetes_core.go` (the comment moved here with `aksBackend` in Task 1)

**Step 1: Locate the stale comment**

Run: `grep -n 'Requires the Azure SDK' module/platform_kubernetes_core.go`
Expected: one match — a doc comment above `aksBackend` reading approximately `// Requires the Azure SDK (github.com/Azure/azure-sdk-for-go) to be available.`

**Step 2: Correct the comment**

`aksBackend.azureToken` is a plain `net/http` OAuth2 client-credentials POST against `login.microsoftonline.com` — it does **not** import the Azure SDK. Replace the stale line with an accurate one, e.g.:

```go
// aksBackend provisions AKS clusters via the Azure Resource Manager REST API.
// It authenticates with a net/http OAuth2 client-credentials flow against
// login.microsoftonline.com — it does NOT import github.com/Azure/azure-sdk-for-go.
```

**Step 3: Verify the audit script no longer flags the file as a comment-only Azure match**

Run: `bash scripts/audit-cloud-symbols.sh | grep -A6 'azure-sdk-for-go'`
Expected: `module/platform_kubernetes_core.go` does **not** appear in the azure-sdk section at all (the SDK name is no longer even mentioned in the file).

**Step 4: Build**

Run: `go build ./module/...`
Expected: exit 0 (comment-only change).

**Step 5: Commit**

```bash
git add module/platform_kubernetes_core.go
git commit -m "docs(module): fix stale 'Requires the Azure SDK' comment on aksBackend

aksBackend.azureToken is a net/http OAuth2 client, not an azure-sdk
consumer. The stale comment is what fooled an earlier inventory pass into
mis-counting platform_kubernetes_kind.go as an azure-sdk importer."
```

Rollback: `git revert` — comment-only.

---

### Task 3: Extend `audit-cloud-symbols.sh` with the `init()`-partition assertion + wire it into CI

**Files:**
- Modify: `scripts/audit-cloud-symbols.sh`
- Modify: `.github/workflows/ci.yml` (or the repo's primary CI workflow — confirm the exact filename with `ls .github/workflows/`)
- Test: `scripts/audit-cloud-symbols.sh --check` (the script self-verifies)

**Step 1: Identify the CI workflow file**

Run: `ls .github/workflows/`
Expected: a primary build/test workflow (e.g. `ci.yml`, `test.yml`, `go.yml`). Note its name for Step 4.

**Step 2: Add the `init()`-partition assertion to the script**

In `scripts/audit-cloud-symbols.sh`, extend the `--check` path so it fails if any post-Phase-0 file registers both a core-staying and a plugin-bound Kubernetes backend in one `init()`. Add, after the existing `platform_kubernetes_kind.go` advisory block (which becomes moot once the file is gone — guard it with a file-existence check):

```bash
echo
echo "== Invariant: no init() mixes core-staying + plugin-bound k8s backends =="
# Post-Phase-0, platform_kubernetes_core.go must register ONLY kind/k3s/eks/aks
# and platform_kubernetes_gke.go must register ONLY gke. A file registering a
# name from the other set is a partition violation.
CORE_K8S=module/platform_kubernetes_core.go
GKE_K8S=module/platform_kubernetes_gke.go
if [[ -f "$CORE_K8S" && -f "$GKE_K8S" ]]; then
  if grep -qE 'RegisterKubernetesBackend\("gke"' "$CORE_K8S"; then
    echo "  VIOLATION: $CORE_K8S registers the plugin-bound 'gke' backend"; FAIL=1
  fi
  for n in kind k3s eks aks; do
    if grep -qE "RegisterKubernetesBackend\\(\"$n\"" "$GKE_K8S"; then
      echo "  VIOLATION: $GKE_K8S registers the core-staying '$n' backend"; FAIL=1
    fi
  done
  [[ $FAIL -eq 0 ]] && echo "  OK — init() partition clean"
fi
```

Also guard the existing `platform_kubernetes_kind.go` advisory block with `[[ -f module/platform_kubernetes_kind.go ]]` so it silently skips post-Phase-0 (the file is gone).

**Step 3: Run the script's check mode locally**

Run: `bash scripts/audit-cloud-symbols.sh --check`
Expected: prints the real-import map, the new "init() partition clean" line shows `OK`, final line `audit-cloud-symbols: OK`, exit 0.

**Step 4: Wire it into CI**

Add a step to the CI workflow identified in Step 1, in the existing build/test job, after checkout + Go setup:

```yaml
      - name: Cloud-SDK inventory + partition audit
        run: bash scripts/audit-cloud-symbols.sh --check
```

**Step 5: Verify the workflow YAML is valid**

Run: `bash -n scripts/audit-cloud-symbols.sh` (script syntax) and visually confirm the YAML indentation matches the surrounding steps in the workflow file.
Expected: script syntax OK; YAML step nested at the same level as sibling steps.

**Step 6: Commit**

```bash
git add scripts/audit-cloud-symbols.sh .github/workflows/
git commit -m "ci(audit): enforce k8s-backend init() partition + run audit on every PR

Extends audit-cloud-symbols.sh --check with an init()-partition assertion
(platform_kubernetes_core.go registers only kind/k3s/eks/aks; _gke.go only
gke) and wires the script into CI so the cloud-SDK inventory becomes a
build-enforced artifact rather than a prose claim."
```

Rollback: `git revert` — CI-config + script change; reverting restores the prior (report-only) script and drops the CI step. Re-run `bash scripts/audit-cloud-symbols.sh` to confirm report-only mode after revert.

---

## PR 2 — Phase A: `IaCStateBackend` proto + benchmark harness + proto lock

Defines the new strict contract and validates the unary-transport decision with a real 1 MB-state benchmark *before* the proto is locked (design self-challenge doubt #3).

### Task 4: Add the `IaCStateBackend` service + messages to `iac.proto`

**Files:**
- Modify: `plugin/external/proto/iac.proto`
- Create (generated): `plugin/external/proto/iac.pb.go`, `plugin/external/proto/iac_grpc.pb.go` (regenerated by `buf`)
- Test: `plugin/external/proto/iac_statebackend_test.go` (a compile-level test that the generated Go types exist with the expected shape)

**Step 1: Write the failing test**

Create `plugin/external/proto/iac_statebackend_test.go`:

```go
package proto

import "testing"

// Compile-level guard: the IaCStateBackend service + its messages must exist
// in the generated package with the IaCStateStore-mirroring shape.
func TestIaCStateBackendGeneratedTypesExist(t *testing.T) {
	var _ IaCStateBackendServer // service interface generated
	var _ IaCStateBackendClient // client interface generated
	_ = &GetStateRequest{ResourceId: "r"}
	_ = &GetStateResponse{Exists: true, State: &IaCState{}}
	_ = &SaveStateRequest{State: &IaCState{}}
	_ = &ListStatesRequest{Filter: map[string]string{"k": "v"}}
	_ = &LockRequest{ResourceId: "r"}
	_ = &UnlockRequest{ResourceId: "r"}
	// IaCState mirrors module.IaCState's JSON-serialisable fields.
	s := &IaCState{ResourceId: "r", ResourceType: "kubernetes", Provider: "azure", Status: "active"}
	if s.GetResourceId() != "r" {
		t.Fatalf("IaCState.ResourceId accessor missing")
	}
}
```

**Step 2: Run the test to verify it fails**

Run: `go test ./plugin/external/proto/ -run TestIaCStateBackendGeneratedTypesExist -v`
Expected: FAIL — build error, `IaCStateBackendServer` / `GetStateRequest` etc. undefined.

**Step 3: Add the service + messages to `iac.proto`**

Append to `plugin/external/proto/iac.proto` (after the `ResourceDriver` service, before EOF). Mirror `module.IaCState` field-for-field (see `module/iac_state.go:4-18`):

```proto
// IaCStateBackend — strict contract for IaC state storage backends served by a
// plugin sidecar. Maps 1:1 onto module.IaCStateStore (6 methods). Unary RPCs:
// the PR 2 benchmark validated unary transport for 1 MB state blobs against the
// in-process baseline. No lock-lease/TTL field — added additively only once a
// plugin backend implements honored expiry with a conformance test.
service IaCStateBackend {
  rpc GetState   (GetStateRequest)    returns (GetStateResponse);
  rpc SaveState  (SaveStateRequest)   returns (SaveStateResponse);
  rpc ListStates (ListStatesRequest)  returns (ListStatesResponse);
  rpc DeleteState(DeleteStateRequest) returns (DeleteStateResponse);
  rpc Lock       (LockRequest)        returns (LockResponse);
  rpc Unlock     (UnlockRequest)      returns (UnlockResponse);
}

// IaCState mirrors module.IaCState (module/iac_state.go). Maps used there
// (Outputs, Config) carry arbitrary JSON — represented here as google.protobuf.Struct.
message IaCState {
  string resource_id   = 1;
  string resource_type = 2;
  string provider      = 3;
  string provider_ref  = 4;
  string provider_id   = 5;
  string config_hash   = 6;
  string status        = 7;
  google.protobuf.Struct outputs = 8;
  google.protobuf.Struct config  = 9;
  repeated string dependencies   = 10;
  string created_at = 11;
  string updated_at = 12;
  string error      = 13;
}

message GetStateRequest    { string resource_id = 1; }
message GetStateResponse   { IaCState state = 1; bool exists = 2; }
message SaveStateRequest   { IaCState state = 1; }   // idempotent: full-state replace, last-writer-wins
message SaveStateResponse  {}
message ListStatesRequest  { map<string, string> filter = 1; }
message ListStatesResponse { repeated IaCState states = 1; }
message DeleteStateRequest  { string resource_id = 1; }
message DeleteStateResponse {}
message LockRequest    { string resource_id = 1; }
message LockResponse   {}
message UnlockRequest  { string resource_id = 1; }
message UnlockResponse {}
```

Confirm `iac.proto` already imports `google/protobuf/struct.proto` (it uses `Struct` elsewhere); if not, add `import "google/protobuf/struct.proto";` near the top with the other imports.

**Step 4: Regenerate the Go bindings**

Run: `cd plugin/external/proto && buf generate` (per `plugin/external/proto/README.md`)
Expected: `iac.pb.go` + `iac_grpc.pb.go` regenerated, now containing `IaCStateBackendServer`, `IaCStateBackendClient`, and the message types. `git diff --stat` shows only the two `*.pb.go` files changed plus `iac.proto`.

**Step 5: Run the test to verify it passes**

Run: `go test ./plugin/external/proto/ -run TestIaCStateBackendGeneratedTypesExist -v`
Expected: PASS.

**Step 6: Full build**

Run: `go build ./...`
Expected: exit 0.

**Step 7: Commit**

```bash
git add plugin/external/proto/iac.proto plugin/external/proto/iac.pb.go plugin/external/proto/iac_grpc.pb.go plugin/external/proto/iac_statebackend_test.go
git commit -m "feat(proto): add IaCStateBackend service to iac.proto

Strict 6-method contract mirroring module.IaCStateStore 1:1, with an
IaCState message mirroring module.IaCState. Unary RPCs. No TTL field
(additive follow-up, gated on a backend honoring expiry). Regenerated
bindings via buf."
```

Rollback: `git revert` — proto + generated code only, no runtime wiring yet; reverting leaves core building exactly as before.

---

### Task 5: Build the `IaCStateBackend` round-trip benchmark harness

**Files:**
- Create: `module/benchmark_iac_state_backend_test.go`
- Test: itself (a `Benchmark*` function)

**Step 1: Write the benchmark**

Create `module/benchmark_iac_state_backend_test.go`. It drives a synthetic ~1 MB `IaCState` through a full `Lock → GetState → SaveState → Unlock` cycle two ways: (a) directly against an in-process `IaCStateStore` (the `memory` backend — the baseline this design replaces), (b) against the same store wrapped behind a real in-memory gRPC `IaCStateBackend` server+client pair (the post-extraction path). It reports `b.ReportMetric` for added latency.

```go
package module

import (
	"context"
	"net"
	"strings"
	"testing"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// oneMBState builds an IaCState whose JSON payload is ~1 MB (Outputs map padded).
func oneMBState() *IaCState {
	big := strings.Repeat("x", 1024)
	outputs := make(map[string]any, 1024)
	for i := 0; i < 1024; i++ {
		outputs["k"+strings.Repeat("0", 3)+itoa(i)] = big
	}
	return &IaCState{
		ResourceID: "bench-resource", ResourceType: "kubernetes", Provider: "azure",
		Status: "active", Outputs: outputs, Config: map[string]any{"size": "large"},
		CreatedAt: "2026-05-14T00:00:00Z", UpdatedAt: "2026-05-14T00:00:00Z",
	}
}

func itoa(i int) string { // tiny local helper; avoid strconv import noise in bench file
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

// BenchmarkIaCStateBackend_InProcess is the baseline: direct IaCStateStore calls.
func BenchmarkIaCStateBackend_InProcess(b *testing.B) {
	store := NewMemoryIaCStateStore()
	st := oneMBState()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := store.Lock(st.ResourceID); err != nil {
			b.Fatal(err)
		}
		if _, err := store.GetState(st.ResourceID); err != nil {
			b.Fatal(err)
		}
		if err := store.SaveState(st); err != nil {
			b.Fatal(err)
		}
		if err := store.Unlock(st.ResourceID); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkIaCStateBackend_GRPC is the post-extraction path: same store, same
// cycle, but every call crosses a real (in-memory bufconn) gRPC boundary.
func BenchmarkIaCStateBackend_GRPC(b *testing.B) {
	lis := bufconn.Listen(4 << 20) // 4 MiB — gRPC default message cap
	srv := grpc.NewServer()
	pb.RegisterIaCStateBackendServer(srv, newBenchStateBackendServer(NewMemoryIaCStateStore()))
	go func() { _ = srv.Serve(lis) }()
	defer srv.Stop()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		b.Fatal(err)
	}
	defer conn.Close()
	client := pb.NewIaCStateBackendClient(conn)
	st := oneMBState()
	pbState := iacStateToProto(st) // helper introduced in Task 7; for the bench, inline a minimal conversion
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := client.Lock(ctx, &pb.LockRequest{ResourceId: st.ResourceID}); err != nil {
			b.Fatal(err)
		}
		if _, err := client.GetState(ctx, &pb.GetStateRequest{ResourceId: st.ResourceID}); err != nil {
			b.Fatal(err)
		}
		if _, err := client.SaveState(ctx, &pb.SaveStateRequest{State: pbState}); err != nil {
			b.Fatal(err)
		}
		if _, err := client.Unlock(ctx, &pb.UnlockRequest{ResourceId: st.ResourceID}); err != nil {
			b.Fatal(err)
		}
	}
}
```

> **Note for the executor:** `newBenchStateBackendServer` and `iacStateToProto` do not exist yet — Task 5 introduces a minimal local `newBenchStateBackendServer` in this same `_test.go` file (a thin adapter wrapping an `IaCStateStore` behind the generated `IaCStateBackendServer` interface, plus inline proto⇄struct conversion). Task 7 promotes the *production* converters into non-test code; the benchmark file may then be simplified to reuse them. Keep the bench file self-contained for this task.

Add to the same file a minimal `newBenchStateBackendServer` implementing `pb.IaCStateBackendServer` by delegating to an `IaCStateStore`, with inline `IaCState`⇄`pb.IaCState` conversion (use `structpb.NewStruct` for the `Outputs`/`Config` maps).

**Step 2: Run the benchmark to verify it builds + runs**

Run: `go test ./module/ -bench BenchmarkIaCStateBackend -benchmem -run '^$' -count=6 | tee /tmp/iac-state-bench.txt`
Expected: both `BenchmarkIaCStateBackend_InProcess` and `_GRPC` run and report ns/op + B/op. (No assertion yet — Task 6 evaluates the numbers.)

**Step 3: Commit**

```bash
git add module/benchmark_iac_state_backend_test.go
git commit -m "test(module): add IaCStateBackend gRPC-vs-in-process benchmark harness

Drives a ~1 MB synthetic IaCState through Lock/GetState/SaveState/Unlock
both in-process (baseline) and over a real bufconn gRPC boundary
(post-extraction path). Feeds the proto-transport decision in the next
task."
```

Rollback: `git revert` — test-only file.

---

### Task 6: Run the benchmark, record the result, lock the proto-transport decision

**Files:**
- Create: `docs/plans/2026-05-14-iac-state-backend-benchmark.md` (the recorded result + decision)
- Modify: `plugin/external/proto/iac.proto` (only if the benchmark forces a streaming redesign — expected: no change)

**Step 1: Run the benchmark with statistical rigor**

Run: `go test ./module/ -bench BenchmarkIaCStateBackend -benchmem -run '^$' -count=10 | tee /tmp/iac-state-bench.txt`
Expected: 10 samples each for `_InProcess` and `_GRPC`.

**Step 2: Compute the added latency**

Run: `go run golang.org/x/perf/cmd/benchstat /tmp/iac-state-bench.txt` (or `benchstat` if already on PATH per the Makefile's `bench-compare` target)
Expected: a side-by-side of `_InProcess` vs `_GRPC` ns/op with variance.

**Step 3: Evaluate against the acceptance bar**

Acceptance bar (set here, per design open-item "concrete acceptance threshold"): **unary transport is accepted if the gRPC path's p50 added latency for the full 4-call cycle is < 5 ms over the in-process baseline.** Rationale: an IaC plan/apply does one Lock/Get/Save/Unlock cycle per resource batch; sub-5 ms per cycle is negligible against real cloud-provider API latency (hundreds of ms).
- **If the bar is met** (expected — bufconn gRPC round-trips are tens of µs): the unary proto from Task 4 is **locked as-is**. No proto change.
- **If the bar is NOT met:** do NOT proceed. The proto needs a streaming redesign for `GetState`/`SaveState` — revise Task 4's proto, regenerate, re-run this task. This is the design's self-challenge doubt #3 gate.

**Step 4: Record the result + decision**

Create `docs/plans/2026-05-14-iac-state-backend-benchmark.md` with: the raw benchstat output, the computed p50 added latency, the 5 ms bar, and the verdict (`unary LOCKED` or `streaming required — proto revised`). This file is the durable evidence the design's "benchmark before proto lock" gate was honored.

**Step 5: Commit**

```bash
git add docs/plans/2026-05-14-iac-state-backend-benchmark.md
git commit -m "docs(plans): record IaCStateBackend transport benchmark — unary locked

Benchmark result: gRPC bufconn round-trip adds <Nms> p50 over the
in-process baseline for the full 1 MB-state Lock/Get/Save/Unlock cycle,
under the 5 ms acceptance bar. Unary IaCStateBackend proto locked; no
streaming redesign needed."
```

(If streaming was required, the commit also includes the revised `iac.proto` + regenerated bindings and the message reflects that.)

Rollback: `git revert` — documentation; if a proto revision was included, reverting also reverts that (back to the Task 4 unary shape).

---

## PR 3 — Phase A: host-side `IaCStateBackend` resolution + secret-redaction + gRPC-logging guard

Wires the engine so `iac.state` can dispatch to a plugin-served backend, and lands the two blocking security tasks from the design's Security section.

### Task 7: Production `IaCState` ⇄ `pb.IaCState` converters + an `IaCStateStore` gRPC client adapter

**Files:**
- Create: `module/iac_state_grpc_client.go`
- Test: `module/iac_state_grpc_client_test.go`

**Step 1: Write the failing test**

Create `module/iac_state_grpc_client_test.go`. It stands up an in-memory `IaCStateBackend` server (delegating to `NewMemoryIaCStateStore()`), wraps the client end in the new `grpcIaCStateStore` adapter, and asserts the adapter satisfies `IaCStateStore` and round-trips a state correctly:

```go
package module

import (
	"context"
	"net"
	"testing"

	pb "github.com/GoCodeAlone/workflow/plugin/external/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func TestGRPCIaCStateStoreRoundTrip(t *testing.T) {
	lis := bufconn.Listen(4 << 20)
	srv := grpc.NewServer()
	pb.RegisterIaCStateBackendServer(srv, newBenchStateBackendServer(NewMemoryIaCStateStore()))
	go func() { _ = srv.Serve(lis) }()
	defer srv.Stop()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	var store IaCStateStore = newGRPCIaCStateStore(pb.NewIaCStateBackendClient(conn))

	want := &IaCState{ResourceID: "r1", ResourceType: "kubernetes", Provider: "azure", Status: "active",
		Outputs: map[string]any{"endpoint": "https://x"}, Config: map[string]any{"size": "L"}}
	if err := store.SaveState(want); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	got, err := store.GetState("r1")
	if err != nil || got == nil {
		t.Fatalf("GetState: %v (got=%v)", err, got)
	}
	if got.ResourceID != "r1" || got.Status != "active" || got.Outputs["endpoint"] != "https://x" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if err := store.Lock("r1"); err != nil {
		t.Fatalf("Lock: %v", err)
	}
	missing, err := store.GetState("nope")
	if err != nil || missing != nil {
		t.Fatalf("GetState(missing) should be nil,nil — got %v,%v", missing, err)
	}
	if err := store.Unlock("r1"); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
}
```

**Step 2: Run the test to verify it fails**

Run: `go test ./module/ -run TestGRPCIaCStateStoreRoundTrip -v`
Expected: FAIL — `newGRPCIaCStateStore` undefined.

**Step 3: Implement the converters + adapter**

Create `module/iac_state_grpc_client.go` with: `iacStateToProto(*IaCState) (*pb.IaCState, error)` and `iacStateFromProto(*pb.IaCState) (*IaCState, error)` (using `structpb.NewStruct` / `.AsMap()` for the `Outputs`/`Config` maps), and `grpcIaCStateStore` — a struct holding a `pb.IaCStateBackendClient` that implements all six `IaCStateStore` methods by delegating over gRPC. `GetState` maps a `GetStateResponse{Exists:false}` to `(nil, nil)` per the interface contract ("Returns nil, nil when not found"). Constructor: `newGRPCIaCStateStore(c pb.IaCStateBackendClient) *grpcIaCStateStore`. Use `context.Background()` for now (a context-plumbing follow-up can thread a real ctx later — out of scope here).

Also: move the `newBenchStateBackendServer` helper out of the Task 5 `_test.go` file into this file as `iacStateBackendServer` (a production type — it is the *server* half core needs nowhere yet, but the Azure plugin's Task 11 needs the exact same delegation shape; keeping one canonical copy avoids drift). Update `module/benchmark_iac_state_backend_test.go` to use the promoted `iacStateToProto` + `iacStateBackendServer` and delete its inline copies.

**Step 4: Run the test to verify it passes**

Run: `go test ./module/ -run TestGRPCIaCStateStoreRoundTrip -v`
Expected: PASS.

**Step 5: Re-run the benchmark to confirm the refactor didn't break it**

Run: `go test ./module/ -bench BenchmarkIaCStateBackend -benchmem -run '^$' -count=1`
Expected: both benchmarks still run cleanly.

**Step 6: Commit**

```bash
git add module/iac_state_grpc_client.go module/iac_state_grpc_client_test.go module/benchmark_iac_state_backend_test.go
git commit -m "feat(module): IaCState proto converters + grpcIaCStateStore client adapter

grpcIaCStateStore implements module.IaCStateStore over an
IaCStateBackendClient — the host-side half of the new contract. Promotes
the proto<->struct converters and the delegating server shape out of the
benchmark test file so the plugin side (Phase A PR4) reuses one canonical
copy."
```

Rollback: `git revert` — new file + test; no engine wiring yet, core builds unchanged.

---

### Task 8: Engine-side plugin backend registry — resolve `iac.state` backends from loaded plugins

**Files:**
- Modify: `module/iac_module.go`
- Create: `module/iac_state_plugin_registry.go`
- Test: `module/iac_state_plugin_registry_test.go`

**Step 1: Spike — confirm the engine's external-plugin-manager handle (≤15 min, no code)**

Read `engine.go` (`BuildFromConfig`) and `plugin/external/` to confirm how the engine loads external plugins at startup and where a module's `Init(app modular.Application)` can reach the set of loaded `ExternalPluginAdapter`s. The deploy path (`cmd/wfctl/deploy_providers.go`) scans `./data/plugins` directly; the engine likely already has an `ExternalPluginManager` in `BuildFromConfig`. Record the handle path in a one-paragraph comment at the top of `iac_state_plugin_registry.go`. **If the engine has no such handle reachable from module Init**, the fallback (design Architecture §1) is a package-level registry that the engine populates at plugin-load time — implement that instead. Pick whichever the spike confirms; both satisfy the design.

**Step 2: Write the failing test**

Create `module/iac_state_plugin_registry_test.go`. Test the registry: registering a backend name → `pb.IaCStateBackendClient` factory, and looking it up. Use a fake client. Assert: an unknown backend name returns `(nil, false)`; a registered name returns the client; registering a **reserved** name (`memory`/`filesystem`/`postgres`) returns an error (design Failure-modes "reserved-name collision").

```go
package module

import "testing"

func TestIaCStateBackendRegistry(t *testing.T) {
	reg := newIaCStateBackendRegistry()
	if _, ok := reg.resolve("azure_blob"); ok {
		t.Fatal("empty registry should not resolve azure_blob")
	}
	fake := &fakeStateBackendClient{}
	if err := reg.register("azure_blob", fake); err != nil {
		t.Fatalf("register: %v", err)
	}
	got, ok := reg.resolve("azure_blob")
	if !ok || got != fake {
		t.Fatalf("resolve azure_blob: ok=%v got=%v", ok, got)
	}
	for _, reserved := range []string{"memory", "filesystem", "postgres"} {
		if err := reg.register(reserved, fake); err == nil {
			t.Fatalf("register(%q) must fail — reserved core backend name", reserved)
		}
	}
}
```

(Define a minimal `fakeStateBackendClient` satisfying `pb.IaCStateBackendClient` in the test file.)

**Step 3: Run the test to verify it fails**

Run: `go test ./module/ -run TestIaCStateBackendRegistry -v`
Expected: FAIL — `newIaCStateBackendRegistry` undefined.

**Step 4: Implement the registry**

Create `module/iac_state_plugin_registry.go`: an `iacStateBackendRegistry` struct wrapping a `map[string]pb.IaCStateBackendClient` + a mutex. `register(name, client)` rejects the reserved names `memory`/`filesystem`/`postgres` with a clear error (`"plugin registered reserved iac.state backend name %q"`). `resolve(name)` returns `(client, ok)`. Provide a package-level default registry instance the engine populates at plugin-load time, plus `newIaCStateBackendRegistry()` for tests.

**Step 5: Run the test to verify it passes**

Run: `go test ./module/ -run TestIaCStateBackendRegistry -v`
Expected: PASS.

**Step 6: Wire `IaCModule.Init()` to consult the registry**

Modify `module/iac_module.go` `Init()`: in the backend `switch`, for any backend name **not** in the core set (`memory`/`filesystem`/`postgres` — and, until later phases, still `spaces`/`gcs`/`azure_blob` keep their in-process cases for now), add a `default:` arm that consults the plugin registry: if `reg.resolve(m.backend)` succeeds, `m.store = newGRPCIaCStateStore(client)`; if not, return the existing `"unsupported backend"` error **extended** with `" (or load the plugin that provides it)"`. Crucially: the `default` arm must run *before* the final error return. The in-process `azure_blob` case stays untouched in this PR — PR 5 deletes it. The point of this task is the *plumbing* exists and is tested; PR 5 flips `azure_blob` to use it.

Add a focused test in `iac_state_plugin_registry_test.go` constructing an `IaCModule` with `backend: "azure_blob_test_only"`, a registry pre-populated with a fake client for that name, and asserting `Init()` sets `m.store` to a `*grpcIaCStateStore`.

**Step 7: Build + test**

Run: `go build ./... && go test ./module/ -run 'IaCStateBackend|IaCModule' -v`
Expected: exit 0, PASS.

**Step 8: Commit**

```bash
git add module/iac_module.go module/iac_state_plugin_registry.go module/iac_state_plugin_registry_test.go
git commit -m "feat(module): engine-side iac.state plugin-backend registry + dispatch

IaCModule.Init() now resolves non-core backend names from a registry the
engine populates at plugin-load time, constructing a grpcIaCStateStore
client. Reserved core names (memory/filesystem/postgres) are rejected at
registration. The in-process azure_blob case is untouched here — the
plumbing exists and is tested; Phase A PR5 flips azure_blob onto it."
```

Rollback: `git revert` — the registry is additive and the `azure_blob` in-process path is unchanged, so reverting leaves `iac.state` working exactly as before. Rollback note: revert commit + `go test ./module/...` to confirm in-process backends still construct.

---

### Task 9: Extend secret redaction to recognise `credentials:` / `credentials_ref:` keys

**Files:**
- Modify: `module/step_output_redactor.go`
- Test: `module/step_output_redactor_test.go` (existing — add cases)

**Step 1: Write the failing test**

Add to `module/step_output_redactor_test.go`:

```go
func TestRedactCredentialsBlock(t *testing.T) {
	in := map[string]any{
		"credentials": map[string]any{
			"accessKey": "AKIAEXAMPLE",
			"secretKey": "supersecret",
		},
		"credentials_ref": "aws-creds-module",
		"bucket":          "public-bucket-name",
	}
	out := RedactStepOutput(in)
	creds := out["credentials"].(map[string]any)
	if creds["accessKey"] != RedactionPlaceholder || creds["secretKey"] != RedactionPlaceholder {
		t.Fatalf("credentials block not redacted: %+v", creds)
	}
	// credentials_ref is a module NAME, not a secret — must NOT be redacted.
	if out["credentials_ref"] != "aws-creds-module" {
		t.Fatalf("credentials_ref should not be redacted (it is a module reference)")
	}
	if out["bucket"] != "public-bucket-name" {
		t.Fatalf("non-sensitive field wrongly redacted")
	}
}
```

**Step 2: Run the test to verify it fails**

Run: `go test ./module/ -run TestRedactCredentialsBlock -v`
Expected: FAIL — `accessKey` already matches `access_key`? Check: `SensitiveFieldPatterns` has `access_key` (underscore) but the key here is `accessKey` (camelCase). The existing substring match is case-insensitive but `accessKey` does not contain `access_key`. So `accessKey`/`secretKey` are **not** currently redacted → test fails.

**Step 3: Implement**

In `module/step_output_redactor.go`, add to `SensitiveFieldPatterns` the camelCase / bare forms that a `credentials:` block uses: `"accesskey"`, `"secretkey"`, `"sessiontoken"`, `"account_key"`, `"accountkey"`, `"clientsecret"`, `"client_secret"`. Because matching is case-insensitive substring, `"accesskey"` matches `accessKey`. Also ensure a key literally named `credentials` whose value is a map gets its children recursively redacted — the existing `redactMap` recursion already covers nested maps, so adding the leaf patterns is sufficient. Do **not** add `credentials_ref` to the patterns — it is a module reference, not a secret (the test guards this).

**Step 4: Run the test to verify it passes**

Run: `go test ./module/ -run 'Redact' -v`
Expected: PASS (the new test + all existing redaction tests still green).

**Step 5: Commit**

```bash
git add module/step_output_redactor.go module/step_output_redactor_test.go
git commit -m "feat(module): redact inline credentials: block keys (accessKey/secretKey/etc.)

Option-1 credentials move raw cloud secrets inline into plugin-native
module config. Extends SensitiveFieldPatterns with the camelCase forms a
credentials: block uses so the config-version store + execution tracing
redact them. credentials_ref: (a module reference, not a secret) is
deliberately left un-redacted."
```

Rollback: `git revert` — redaction is additive; reverting only narrows what's redacted (no functional break, but re-widening is the forward fix).

---

### Task 10: gRPC-interceptor guard test — assert no interceptor logs `CreateModule` bodies

**Files:**
- Create: `plugin/external/grpc_logging_guard_test.go`
- Test: itself

**Step 1: Write the guard test**

The design's Security section verified at design time that `plugin/external/` adds no body-logging gRPC interceptor (only `callback_server.go:85,118` logs, and neither touches module config). This test makes that a permanent CI guard: if a future change adds an interceptor that could log `CreateModule` request bodies (which carry `credentials:` blocks), CI fails.

Create `plugin/external/grpc_logging_guard_test.go`:

```go
package external

import (
	"os"
	"regexp"
	"testing"
)

// The plugin SDK must NOT install a gRPC interceptor that logs request bodies —
// CreateModule requests carry inline credentials: blocks. This test fails if
// grpc.NewServer / grpc.NewClient anywhere in plugin/external/ is constructed
// with a *UnaryInterceptor option, forcing a reviewer to look. See the
// cloud-sdk-extraction design, Security section.
func TestNoBodyLoggingInterceptor(t *testing.T) {
	interceptorOpt := regexp.MustCompile(`(Chain)?Unary(Server|Client)?Interceptor`)
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !match(name, ".go") || match(name, "_test.go") {
			continue
		}
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if interceptorOpt.Match(b) {
			t.Fatalf("%s references a gRPC interceptor option — if it logs request "+
				"bodies it can leak inline credentials: blocks. Audit it and, if safe, "+
				"add an explicit allowlist entry to this test.", name)
		}
	}
}

func match(s, suffix string) bool { return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix }
```

**Step 2: Run the test**

Run: `go test ./plugin/external/ -run TestNoBodyLoggingInterceptor -v`
Expected: PASS (no interceptor exists today — design verified this).

**Step 3: Commit**

```bash
git add plugin/external/grpc_logging_guard_test.go
git commit -m "test(plugin/external): guard against gRPC body-logging interceptors

CreateModule requests carry inline credentials: blocks. This guard fails
CI if any plugin/external/ file gains a gRPC interceptor option, forcing
a reviewer to confirm it cannot log request bodies. Implements the
cloud-sdk-extraction design's Security guard-test requirement."
```

Rollback: `git revert` — test-only.

---

## PR 4 — Phase A: `workflow-plugin-azure` implements `azure_blob` `IaCStateBackend` (cross-repo)

**Repository:** `/Users/jon/workspace/workflow-plugin-azure` (NOT the workflow worktree). Branch: `feat/azure-blob-state-backend`. This PR depends on PRs 2 (published proto) and is a prerequisite for PR 5.

### Task 11: Port `AzureBlobIaCStateStore` into workflow-plugin-azure + serve it as `IaCStateBackend`

**Files (in `/Users/jon/workspace/workflow-plugin-azure`):**
- Create: `internal/statebackend/azure_blob.go` (the ported store — copy from workflow's `module/iac_state_azure.go`)
- Create: `internal/statebackend/server.go` (the `IaCStateBackendServer` gRPC impl delegating to the store)
- Modify: the plugin's main entrypoint + `plugin.json` to advertise the `azure_blob` `IaCStateBackend`
- Test: `internal/statebackend/azure_blob_test.go` (port the existing tests from workflow's `module/iac_state_azure_test.go` if present; otherwise test against the `AzureBlobClient` interface with a fake)

**Step 1: Inspect the current plugin structure**

Run: `ls -R /Users/jon/workspace/workflow-plugin-azure/{cmd,internal,provider,drivers} 2>/dev/null; cat /Users/jon/workspace/workflow-plugin-azure/plugin.json`
Expected: understand where `sdk.ServeIaCPlugin` is called and how `plugin.json` declares capabilities.

**Step 2: Port the store**

Copy `module/iac_state_azure.go` from the workflow worktree into `internal/statebackend/azure_blob.go` in the plugin repo. It already carries its own `AzureBlobClient` interface + `azureRealClient` (azblob-backed) impl — it is self-contained. Adjust the package name. The plugin repo *gains* the `Azure/azure-sdk-for-go/sdk/storage/azblob` dependency (it likely already has it for its IaC resource-provider role — confirm with `grep azblob go.mod`).

**Step 3: Port the tests, run them**

Copy `module/iac_state_azure_test.go` (if it exists in the worktree) into `internal/statebackend/azure_blob_test.go`. Run: `go test ./internal/statebackend/ -v`
Expected: PASS — the store's logic is unchanged, only its home moved.

**Step 4: Write the `IaCStateBackendServer` impl**

Create `internal/statebackend/server.go` implementing `proto.IaCStateBackendServer` (from `github.com/GoCodeAlone/workflow/plugin/external/proto`) by delegating each RPC to an `AzureBlobIaCStateStore`, with the same `IaCState`⇄`pb.IaCState` conversion shape promoted in workflow Task 7. Mirror that converter exactly (the plugin imports the same `proto` package, so the wire types are identical).

**Step 5: Wire it into the plugin's serve path + manifest**

Register the `IaCStateBackend` service on the plugin's gRPC server alongside its existing `IaCProviderRequired` service, and add `azure_blob` to the plugin's advertised state-backend capabilities in `plugin.json` (mirror how the existing `iacProvider` capability is declared — the host's registry-population step in workflow Task 8 reads this).

**Step 6: Build + load-test the plugin**

Run: `go build ./... && go test ./...` in the plugin repo.
Expected: exit 0, PASS.
Then load-test: build the plugin binary, point a minimal workflow config with `iac.state` `backend: azure_blob` at it (using the workflow worktree's `server` binary built from PR 3's branch), and confirm the engine resolves the plugin-served backend. **Verification (plugin change class — load into host + exercise):** the engine logs the `iac.state` module constructing a `grpcIaCStateStore` for `azure_blob`, and a `SaveState`/`GetState` round-trips. Capture the transcript.

**Step 7: Commit (in the plugin repo)**

```bash
git add internal/statebackend/ plugin.json cmd/
git commit -m "feat: serve azure_blob IaCStateBackend

Ports AzureBlobIaCStateStore from workflow core and serves it behind the
new proto.IaCStateBackend gRPC contract. Advertises azure_blob in
plugin.json so the workflow engine resolves it at plugin-load time. This
plugin version is the prerequisite for workflow dropping its in-core
azure_blob backend."
```

Rollback: `git revert` in the plugin repo — additive (new service + capability); the plugin's existing IaC-provider role is untouched, so reverting leaves the plugin fully functional minus the new state backend.

---

### Task 12: Tag a `workflow-plugin-azure` release implementing `IaCStateBackend`

**Files (in `/Users/jon/workspace/workflow-plugin-azure`):**
- Modify: `CHANGELOG.md`

**Step 1: Add the CHANGELOG entry**

Document the new `azure_blob` `IaCStateBackend` capability and the migration note: workflow configs using `iac.state` `backend: azure_blob` on a workflow-core version that has dropped the in-core backend (the PR 5 core version) **must** load this plugin version or newer.

**Step 2: Commit + tag**

```bash
git add CHANGELOG.md
git commit -m "docs: changelog for azure_blob IaCStateBackend support"
# Minor version bump — new capability, additive. Confirm current latest tag first:
git tag -a vX.Y.0 -m "azure_blob IaCStateBackend support"
```

**Step 3: Push branch + open the plugin PR; after merge, push the tag**

Run: `git push -u origin feat/azure-blob-state-backend` then `gh pr create ...`. After the plugin PR merges, push the tag so workflow Task 14 can pin to it. **Verification (version pin — the tag must be resolvable):** `git ls-remote --tags origin | grep vX.Y.0` returns the tag.

Rollback: delete the tag (`git push origin :refs/tags/vX.Y.0`) + revert the CHANGELOG commit. The tag is the externally-visible artifact; deleting it before any consumer pins is clean.

---

## PR 5 — Phase A: core deletes `iac_state_azure.go` + strips `azure_blob` case → drops azure-sdk from go.mod

The payoff PR. **Prerequisite: PR 4's plugin version is merged + tagged** — after this PR, `backend: azure_blob` has no in-core implementation.

### Task 13: Delete `iac_state_azure.go` + strip the `azure_blob` case from `iac_module.go`

**Files:**
- Delete: `module/iac_state_azure.go`
- Delete: `module/iac_state_azure_test.go` (if it exists — its logic now lives + is tested in the plugin repo, Task 11)
- Modify: `module/iac_module.go`
- Modify: `go.mod`, `go.sum`
- Test: `module/iac_module_test.go` (existing + a new case)

**Step 1: Write the failing test**

Add to `module/iac_module_test.go` a test asserting that `backend: azure_blob` with **no plugin registered** now returns the plugin-guidance error (not a successful in-process construction), and that with a fake plugin client registered it constructs a `grpcIaCStateStore`:

```go
func TestIaCModuleAzureBlobRequiresPlugin(t *testing.T) {
	m := NewIaCModule("st", map[string]any{"backend": "azure_blob", "container": "c",
		"account_url": "https://x", "account_name": "n", "account_key": "k"})
	err := m.Init(newTestApp(t))
	if err == nil {
		t.Fatal("azure_blob with no plugin loaded must error — in-core backend is gone")
	}
	if !strings.Contains(err.Error(), "azure_blob") || !strings.Contains(err.Error(), "plugin") {
		t.Fatalf("error should point at the missing plugin: %v", err)
	}
}
```

(Reuse whatever test-app constructor `iac_module_test.go` already uses; `newTestApp` is a placeholder for the existing helper.)

**Step 2: Run the test to verify it fails**

Run: `go test ./module/ -run TestIaCModuleAzureBlobRequiresPlugin -v`
Expected: FAIL — the in-process `azure_blob` case still constructs an `AzureBlobIaCStateStore` successfully, so `Init()` returns nil.

**Step 3: Strip the `azure_blob` case + `newAzureSharedKeyCredential`**

In `module/iac_module.go`: remove the entire `case "azure_blob":` block (lines ~86-106) and the `newAzureSharedKeyCredential` helper + the `azblob` import. The `default:` arm added in Task 8 now handles `azure_blob` — it consults the plugin registry and returns the plugin-guidance error if unregistered. Also: while in this file, **fix the stale line-18 doc comment** (`"Supported backends: 'memory' … 'filesystem' … 'spaces'"`) to list all currently-supported backends accurately (`memory`, `filesystem`, `gcs`, `spaces`, `postgres`, plus "and any backend provided by a loaded plugin").

**Step 4: Delete `iac_state_azure.go`**

Run: `git rm module/iac_state_azure.go` (and `git rm module/iac_state_azure_test.go` if present).

**Step 5: Tidy go.mod**

Run: `go mod tidy`
Expected: `go.mod` + `go.sum` lose `github.com/Azure/azure-sdk-for-go/sdk/azcore` and `.../sdk/storage/azblob` (and any now-unused transitive azure deps). Confirm with `git diff go.mod`.

**Step 6: Run the audit script — Azure is gone**

Run: `bash scripts/audit-cloud-symbols.sh | grep -A8 'azure-sdk-for-go'`
Expected: the `azure-sdk-for-go` section is **empty** (no REAL, no comment-only) — zero azure-sdk references anywhere in the repo.

**Step 7: Build + test**

Run: `go build ./... && go test ./module/ -run 'IaCModule|IaCStateBackend' -v`
Expected: exit 0; PASS including the new `TestIaCModuleAzureBlobRequiresPlugin`.

**Step 8: Runtime-launch validation**

This task changes plugin loading paths + `go.mod` — a `runtime-launch-validation` trigger. Build the server, launch it with a config that uses `iac.state` `backend: azure_blob` **with the Task 11 plugin available**, and confirm it reaches healthy startup + the backend resolves over gRPC. Then launch with the plugin **absent** and confirm a clean, actionable error (not a panic). Capture both transcripts.

Run: `go build -o /tmp/server ./cmd/server && /tmp/server -config <azure_blob-test-config> ...`
Expected: with plugin → engine ready, `iac.state` backend resolved; without plugin → clean `"iac.state backend \"azure_blob\": ... load the plugin"` error, exit non-zero, no panic.

**Step 9: Commit**

```bash
git add module/iac_module.go go.mod go.sum
git rm module/iac_state_azure.go
# git rm module/iac_state_azure_test.go  # if it existed
git commit -m "feat(module)!: drop in-core azure_blob IaC state backend

Deletes iac_state_azure.go and strips the azure_blob case +
newAzureSharedKeyCredential from iac_module.go. backend: azure_blob now
resolves an IaCStateBackend gRPC client from workflow-plugin-azure
(>= vX.Y.0). go mod tidy removes Azure/azure-sdk-for-go entirely — the
audit script confirms zero azure-sdk references repo-wide.

BREAKING: iac.state with backend: azure_blob now requires
workflow-plugin-azure to be loaded. See docs/migrations.

Rollback: revert this commit + go mod tidy restores the in-core backend
and re-adds azure-sdk to go.mod; smoke-check with an azure_blob config."
```

Rollback: revert the commit + `go mod tidy` (restores `iac_state_azure.go`, the in-core case, and the azure-sdk deps) + relaunch the server with an `azure_blob` config to confirm the in-core path works again.

---

### Task 14: Migration doc + pin the engine's plugin-registry population to advertise `azure_blob`

**Files:**
- Create: `docs/migrations/2026-05-14-cloud-sdk-extraction.md`
- Modify: the engine plugin-load wiring confirmed in Task 8's spike (where plugins are loaded → populate `iacStateBackendRegistry`)
- Test: `module/iac_state_plugin_registry_test.go` (extend) + a launch check

**Step 1: Write the migration doc**

Create `docs/migrations/2026-05-14-cloud-sdk-extraction.md` covering (per the design's Migration section, Phase A scope only): `iac.state` with `backend: azure_blob` now requires `wfctl plugin install workflow-plugin-azure` (≥ the Task 12 tag); the yaml `backend: azure_blob` value is unchanged; `memory`/`filesystem`/`postgres` are unaffected. Note that Phases B/C/D (AWS/GCP/DO) will follow the same pattern in subsequent releases.

**Step 2: Wire plugin-load → registry population**

In the engine plugin-load path (from Task 8's spike), after each external plugin is loaded, read its advertised `IaCStateBackend` capabilities from `plugin.json` / its `ContractRegistry` and call `iacStateBackendRegistry.register(name, client)` for each — building the `pb.IaCStateBackendClient` from the loaded plugin's gRPC connection (mirror the `typedIaCAdapter` construction in `cmd/wfctl/iac_typed_adapter.go`, but for the engine context and the `IaCStateBackend` service name `workflow.plugin.external.iac.IaCStateBackend`).

**Step 3: Write/extend the test**

Add a test that loads a fake plugin advertising `azure_blob` and asserts the engine's registry has it resolvable after load. If a full plugin-load test is too heavy for a unit test, assert the *population function* in isolation: given a fake `ExternalPluginAdapter` advertising `azure_blob`, the population step calls `registry.register("azure_blob", <client>)`.

**Step 4: Build + test + launch validation**

Run: `go build ./... && go test ./module/ -run 'IaCStateBackend|IaCModule' -v`
Expected: exit 0, PASS.
Then the end-to-end launch check from Task 13 Step 8 should now work *without manual registry seeding* — the engine auto-populates from the loaded plugin. Re-run that launch with the Task 11 plugin in `./data/plugins/` and confirm `azure_blob` resolves with zero manual wiring. Capture the transcript.

**Step 5: Commit**

```bash
git add docs/migrations/2026-05-14-cloud-sdk-extraction.md module/ engine.go
git commit -m "feat(engine): auto-populate iac.state backend registry from loaded plugins

At plugin-load time the engine reads each plugin's advertised
IaCStateBackend capabilities and registers a gRPC client into the
iac.state backend registry, so iac.state backend: azure_blob resolves
with zero manual wiring. Adds the user-facing migration doc.

Rollback: revert this commit — iac.state plugin backends then require
manual registry seeding (the registry + dispatch from Task 8 remain);
core in-process backends (memory/filesystem/postgres) are unaffected."
```

Rollback: revert the commit; the registry + dispatch plumbing (Task 8) survive, only the auto-population is removed. Core backends unaffected. Relaunch with a `memory` backend config to confirm.

---

## Notes for the executor

- **TDD discipline:** every task above follows write-test → see-it-fail → implement → see-it-pass → commit. Do not skip the "see it fail" step — it proves the test exercises the new behavior.
- **Cross-repo PR 4:** create the branch + PR in `/Users/jon/workspace/workflow-plugin-azure` separately. It must merge + tag before PR 5. If the pipeline cannot operate cross-repo autonomously, surface PR 4 as a blocker for human action rather than skipping it.
- **PR ordering:** PR 1 → (PR 2, PR 3 in either order) → PR 4 → PR 5. PR 5 is the only breaking change and the only one that touches `go.mod`.
- **Benchmark gate (Task 6):** if the benchmark fails the 5 ms bar, STOP and redesign the proto for streaming before continuing — this is a design-mandated gate, not a formality.
- **Follow-on plan:** once PR 5 merges, author the Phase B/C/D plan. Phase B (AWS) reuses Task 7's converters + Task 8's registry + Task 11's plugin pattern; Phase C (GCP) additionally runs the `kubernetesBackend` interface-audit spike for the `gke` contract decision (design Architecture §2); Phase D (DigitalOcean `spaces`) rides Phase B's `iac_state_spaces.go` deletion.
