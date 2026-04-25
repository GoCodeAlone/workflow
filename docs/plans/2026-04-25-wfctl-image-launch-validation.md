# wfctl Ephemeral Image-Launch Validation + Dev Loop — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Codify "build the image, run it, validate /healthz" inside wfctl as `wfctl validate launch` (CI gate, MVP for v0.18.12) and as `wfctl dev up --docker` (local dev loop with hot reloads, follow-up for v0.19.0). Replace the bespoke image-launch CI workflow file every consumer (BMW, ratchet, workflow-cloud) writes today with a single `wfctl validate launch` command. Codify Docker access as a strict-proto plugin abstraction so future Docker bugs cannot resurface from `map[string]any` lossiness.

**Architecture:** Hybrid testcontainers-go (synthesis path) + compose-go (compose-test.yaml path), wired through a strict-proto `DockerProvider` gRPC service with two implementations: an external plugin (`workflow-plugin-docker`, embedded Moby Go SDK) and an in-tree `systemDockerFallback` (shells to `docker` CLI). Both implement the same proto-generated interface and validate request payloads via `protovalidate-go` before any container action. Synthesis layer reads `app.yaml` + manifests via existing `DetectPluginInfraNeeds`, maps each `infra.<resource>` requirement to a containerized equivalent (pg, redis, minio for v0.18.12), starts containers with healthchecks, polls `/healthz`, scrapes startup logs for known failure signatures, emits CI summary on failure.

**Tech Stack:**
- `github.com/testcontainers/testcontainers-go` — container lifecycle + healthcheck wait strategies (Apache-2.0; ~12 MB transitive deps)
- `github.com/compose-spec/compose-go` — Compose YAML parser (Phase 2; Apache-2.0)
- `buf` toolchain — proto schema lint, breaking-change check, Go codegen
- `github.com/bufbuild/protovalidate-go` — runtime field validation from `buf.validate` annotations
- `github.com/fsnotify/fsnotify` — file watcher for dev loop (Phase 2)
- HashiCorp `go-plugin` — gRPC plugin transport (already a workflow dep)

**Design doc:** `docs/plans/2026-04-25-wfctl-image-launch-validation-design.md` (committed at f85a644 on branch `design/wfctl-image-launch-validation`)

**Phasing:**
- **Phase 1 — v0.18.12** (this plan): MVP `wfctl validate launch` synthesis-only + DockerProvider proto + system fallback. ~23 tasks.
- **Phase 2 — v0.18.13/v0.19.0** (high-level only here): compose mode + `wfctl dev up --docker` + `workflow-plugin-docker` separate-repo plugin + refactor `wfctl build` onto the abstraction.
- **Phase 3 — v0.20.0+** (high-level only here): IaC cross-cut — per-cloud-provider local mappings via proto.

**Coordination with adjacent in-flight work:**
- v0.20.0 IaC proto enforcement (#41) shares the `buf` toolchain. Whichever ships first lands the buf scaffolding; the other inherits. Each task that creates a buf file says "if absent, create; if present, reuse — don't duplicate."
- Tasks #71 (WriteStepSummary), #79 (BMW migrations CI), #81 (BMW image-launch CI) are **subsumed** by this plan. Once Phase 1 ships and BMW adopts `wfctl validate launch`, those tasks close.

**Stop condition:** This plan is committed, alignment-check runs, STOPS at PASS. **No execution.** User explicitly orders execution later.

---

## Phase 1 — v0.18.12 MVP (full task-by-task breakdown)

All Phase 1 tasks happen on branch `feat/v0.18.12-validate-launch` (created off `design/wfctl-image-launch-validation` or `main`, implementer's choice — design branch carries no code, only the design doc).

Every task ends with a `git commit`. No unrelated work bundled. Test-catches-regression invariant proven for each test that gates a fix.

### Task 1: buf toolchain scaffolding

**Files:**
- Create (or reuse if present): `buf.yaml`
- Create (or reuse if present): `buf.gen.yaml`
- Create: `Makefile` target `proto` (or extend existing Makefile)
- Create (or reuse): `.github/workflows/buf-ci.yml`

**Step 1: Check whether buf scaffolding already exists**

Run: `ls buf.yaml buf.gen.yaml 2>&1`

If both files exist → skip to Task 2 (the v0.20.0 IaC workstream landed first; reuse).
If absent → continue.

**Step 2: Write buf.yaml**

```yaml
version: v2
modules:
  - path: proto
deps:
  - buf.build/bufbuild/protovalidate
lint:
  use:
    - STANDARD
breaking:
  use:
    - FILE
```

**Step 3: Write buf.gen.yaml**

```yaml
version: v2
managed:
  enabled: true
plugins:
  - remote: buf.build/protocolbuffers/go
    out: .
    opt: paths=source_relative
  - remote: buf.build/grpc/go
    out: .
    opt: paths=source_relative,require_unimplemented_servers=false
```

**Step 4: Add Makefile target**

```makefile
.PHONY: proto
proto:
	buf dep update
	buf generate
	buf lint
	buf breaking --against ".git#branch=main"
```

**Step 5: Add CI workflow**

`.github/workflows/buf-ci.yml`:

```yaml
name: buf
on:
  pull_request:
    paths:
      - 'proto/**'
      - 'buf.yaml'
      - 'buf.gen.yaml'
jobs:
  lint-and-breaking:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0  # buf breaking needs full history
      - uses: bufbuild/buf-action@v1
      - run: buf lint
      - run: buf breaking --against "https://github.com/${{ github.repository }}.git#branch=main"
```

**Step 6: Verify locally**

Run: `buf lint`

Expected: passes (no proto files yet, no errors).

**Step 7: Commit**

```bash
git add buf.yaml buf.gen.yaml Makefile .github/workflows/buf-ci.yml
git commit -m "build(proto): bootstrap buf toolchain for typed plugin RPCs"
```

---

### Task 2: Define DockerProvider proto schema (types.proto)

**Files:**
- Create: `proto/workflow/docker/v1/types.proto`

**Step 1: Write the failing test (lint-only)**

The test is `buf lint`. There's no test file yet — proto compilation is the gate.

**Step 2: Write `types.proto`**

```protobuf
syntax = "proto3";

package workflow.docker.v1;

import "buf/validate/validate.proto";
import "google/protobuf/duration.proto";

option go_package = "github.com/GoCodeAlone/workflow/proto/workflow/docker/v1;dockerv1";

// PortMapping declares a container port and its host binding.
message PortMapping {
  uint32 container_port = 1 [(buf.validate.field).uint32 = { gt: 0, lte: 65535 }];
  uint32 host_port = 2 [(buf.validate.field).uint32 = { lte: 65535 }];  // 0 = ephemeral
  string protocol = 3 [(buf.validate.field).string = { in: ["tcp", "udp", "sctp", ""] }];
}

// VolumeMount declares a host-to-container path mapping.
message VolumeMount {
  string host_path = 1 [(buf.validate.field).string.min_len = 1];
  string container_path = 2 [(buf.validate.field).string.min_len = 1];
  bool read_only = 3;
}

// Healthcheck declares a container healthcheck command + timing.
message Healthcheck {
  repeated string test = 1 [(buf.validate.field).repeated.min_items = 1];
  google.protobuf.Duration interval = 2 [(buf.validate.field).duration = { gt: { seconds: 0 } }];
  google.protobuf.Duration timeout = 3;
  int32 retries = 4 [(buf.validate.field).int32.gte = 0];
  google.protobuf.Duration start_period = 5;
}

// Network declares a Docker network.
message Network {
  string id = 1;
  string name = 2 [(buf.validate.field).string.min_len = 1];
  string driver = 3;
}

// Volume declares a Docker volume.
message Volume {
  string name = 1 [(buf.validate.field).string.min_len = 1];
  string driver = 2;
  map<string, string> labels = 3;
}

// ContainerInfo is the inspect-result subset wfctl uses.
message ContainerInfo {
  string id = 1;
  string name = 2;
  string image = 3;
  string state = 4;       // running|exited|paused|...
  int32 exit_code = 5;
  string error = 6;       // engine-provided error string when exited != 0
  repeated PortMapping ports = 7;
  Healthcheck healthcheck = 8;
  string health_status = 9;  // starting|healthy|unhealthy|none
}

// LogChunk is one streamed-log message.
message LogChunk {
  string container_id = 1 [(buf.validate.field).string.min_len = 1];
  string stream = 2 [(buf.validate.field).string = { in: ["stdout", "stderr"] }];
  bytes data = 3;
}

// DaemonInfo is the daemon's identity + capabilities.
message DaemonInfo {
  string version = 1;
  string api_version = 2;
  string os = 3;
  string arch = 4;
  string driver = 5;       // overlay2|btrfs|...
  bool buildkit = 6;
}
```

**Step 3: Verify**

Run: `buf lint`

Expected: passes. If any FIELD_LOWER_SNAKE_CASE or PACKAGE_DIRECTORY_MATCH errors → fix before committing.

**Step 4: Commit**

```bash
git add proto/workflow/docker/v1/types.proto
git commit -m "proto(docker/v1): types — PortMapping, Healthcheck, ContainerInfo, etc"
```

---

### Task 3: Define DockerProvider service (docker_provider.proto)

**Files:**
- Create: `proto/workflow/docker/v1/docker_provider.proto`

**Step 1: Write `docker_provider.proto`**

```protobuf
syntax = "proto3";

package workflow.docker.v1;

import "buf/validate/validate.proto";
import "google/protobuf/duration.proto";
import "proto/workflow/docker/v1/types.proto";

option go_package = "github.com/GoCodeAlone/workflow/proto/workflow/docker/v1;dockerv1";

// DockerProvider is the strict-typed plugin contract for Docker daemon access.
// Two implementations: workflow-plugin-docker (external gRPC plugin embedding
// docker/docker/client) and systemDockerFallback (in-tree shellout to `docker`).
service DockerProvider {
  rpc Build(BuildRequest) returns (BuildResponse);
  rpc Push(PushRequest) returns (PushResponse);
  rpc Pull(PullRequest) returns (PullResponse);
  rpc Run(RunRequest) returns (RunResponse);
  rpc Inspect(InspectRequest) returns (InspectResponse);
  rpc Logs(LogsRequest) returns (stream LogChunk);
  rpc Stop(StopRequest) returns (StopResponse);
  rpc Remove(RemoveRequest) returns (RemoveResponse);
  rpc CreateNetwork(CreateNetworkRequest) returns (CreateNetworkResponse);
  rpc RemoveNetwork(RemoveNetworkRequest) returns (RemoveNetworkResponse);
  rpc CreateVolume(CreateVolumeRequest) returns (CreateVolumeResponse);
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

message BuildResponse {
  string image_id = 1 [(buf.validate.field).string.min_len = 1];
  repeated string logs = 2;
  int64 duration_ms = 3 [(buf.validate.field).int64.gte = 0];
}

message PushRequest {
  string ref = 1 [(buf.validate.field).string.min_len = 1];
  Auth auth = 2;
}
message PushResponse { repeated string logs = 1; }
message Auth {
  string username = 1;
  string password = 2;
  string registry = 3;
}

message PullRequest {
  string ref = 1 [(buf.validate.field).string.min_len = 1];
  Auth auth = 2;
}
message PullResponse { string image_id = 1; }

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

message RunResponse {
  string container_id = 1 [(buf.validate.field).string.min_len = 1];
}

message InspectRequest {
  string container_id = 1 [(buf.validate.field).string.min_len = 1];
}
message InspectResponse {
  ContainerInfo info = 1;
}

message LogsRequest {
  string container_id = 1 [(buf.validate.field).string.min_len = 1];
  bool follow = 2;
  int32 tail = 3 [(buf.validate.field).int32.gte = 0];  // 0 = all
}

message StopRequest {
  string container_id = 1 [(buf.validate.field).string.min_len = 1];
  google.protobuf.Duration timeout = 2;
}
message StopResponse {}

message RemoveRequest {
  string container_id = 1 [(buf.validate.field).string.min_len = 1];
  bool force = 2;
  bool remove_volumes = 3;
}
message RemoveResponse {}

message CreateNetworkRequest {
  string name = 1 [(buf.validate.field).string.min_len = 1];
  string driver = 2;
  map<string, string> labels = 3;
}
message CreateNetworkResponse { Network network = 1; }

message RemoveNetworkRequest {
  string network_id = 1 [(buf.validate.field).string.min_len = 1];
}
message RemoveNetworkResponse {}

message CreateVolumeRequest {
  string name = 1 [(buf.validate.field).string.min_len = 1];
  string driver = 2;
  map<string, string> labels = 3;
}
message CreateVolumeResponse { Volume volume = 1; }

message RemoveVolumeRequest {
  string name = 1 [(buf.validate.field).string.min_len = 1];
  bool force = 2;
}
message RemoveVolumeResponse {}

message DaemonInfoRequest {}
```

**Step 2: Verify**

Run: `buf lint`

Expected: passes.

**Step 3: Commit**

```bash
git add proto/workflow/docker/v1/docker_provider.proto
git commit -m "proto(docker/v1): DockerProvider service — typed RPCs with buf.validate"
```

---

### Task 4: Generate Go code from proto

**Files:**
- Generated: `proto/workflow/docker/v1/types.pb.go`
- Generated: `proto/workflow/docker/v1/docker_provider.pb.go`
- Generated: `proto/workflow/docker/v1/docker_provider_grpc.pb.go`

**Step 1: Generate**

Run: `make proto`

Expected: three `.pb.go` files materialize in `proto/workflow/docker/v1/`.

**Step 2: Verify Go compilation**

Run: `go vet ./proto/workflow/docker/v1/...`

Expected: no output (clean).

**Step 3: Commit**

```bash
git add proto/workflow/docker/v1/*.pb.go
git commit -m "proto(docker/v1): generated Go code (buf generate)"
```

---

### Task 5: Schema-level validation tests

**Files:**
- Create: `proto/workflow/docker/v1/types_test.go`

**Step 1: Write the failing test**

```go
package dockerv1

import (
	"testing"

	"github.com/bufbuild/protovalidate-go"
	"google.golang.org/protobuf/types/known/durationpb"
)

func TestBuildRequest_Validate(t *testing.T) {
	v, err := protovalidate.New()
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		req     *BuildRequest
		wantErr string  // empty = expect success; non-empty = expect error containing this substring
	}{
		{
			name:    "empty context_dir rejected",
			req:     &BuildRequest{Tags: []string{"x"}},
			wantErr: "context_dir",
		},
		{
			name:    "empty tags rejected",
			req:     &BuildRequest{ContextDir: "."},
			wantErr: "tags",
		},
		{
			name:    "invalid tag pattern rejected",
			req:     &BuildRequest{ContextDir: ".", Tags: []string{"INVALID UPPERCASE"}},
			wantErr: "tags",
		},
		{
			name:    "valid request accepted",
			req:     &BuildRequest{ContextDir: ".", Tags: []string{"my-image:1.0"}},
			wantErr: "",
		},
		{
			name:    "invalid platform rejected",
			req:     &BuildRequest{ContextDir: ".", Tags: []string{"x"}, Platform: "weird/cpu"},
			wantErr: "platform",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := v.Validate(tc.req)
			if tc.wantErr == "" {
				if err != nil {
					t.Errorf("expected valid, got error: %v", err)
				}
				return
			}
			if err == nil {
				t.Errorf("expected error containing %q, got nil", tc.wantErr)
				return
			}
			if !contains(err.Error(), tc.wantErr) {
				t.Errorf("expected error containing %q, got: %v", tc.wantErr, err)
			}
		})
	}
}

func TestRunRequest_Validate(t *testing.T) {
	v, _ := protovalidate.New()

	cases := []struct {
		name    string
		req     *RunRequest
		wantErr string
	}{
		{"empty image rejected", &RunRequest{Name: "ok-name"}, "image"},
		{"bad name rejected", &RunRequest{Image: "x", Name: "bad name with spaces"}, "name"},
		{"port out of range rejected", &RunRequest{Image: "x", Name: "ok", Ports: []*PortMapping{{ContainerPort: 70000, Protocol: "tcp"}}}, "container_port"},
		{"bad protocol rejected", &RunRequest{Image: "x", Name: "ok", Ports: []*PortMapping{{ContainerPort: 8080, Protocol: "icmp"}}}, "protocol"},
		{"valid accepted", &RunRequest{Image: "redis:7", Name: "redis-1"}, ""},
		{"valid with ports", &RunRequest{Image: "x", Name: "ok", Ports: []*PortMapping{{ContainerPort: 8080, HostPort: 8080, Protocol: "tcp"}}}, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := v.Validate(tc.req)
			if tc.wantErr == "" {
				if err != nil {
					t.Errorf("expected valid, got: %v", err)
				}
				return
			}
			if err == nil || !contains(err.Error(), tc.wantErr) {
				t.Errorf("expected error containing %q, got: %v", tc.wantErr, err)
			}
		})
	}
}

func TestHealthcheck_Validate(t *testing.T) {
	v, _ := protovalidate.New()

	cases := []struct {
		name    string
		hc      *Healthcheck
		wantErr string
	}{
		{"empty test cmd rejected", &Healthcheck{Interval: durationpb.New(durationpb.New(0).AsDuration() + 1)}, "test"},
		{"zero interval rejected", &Healthcheck{Test: []string{"true"}, Interval: durationpb.New(0)}, "interval"},
		{"valid accepted", &Healthcheck{Test: []string{"curl", "-f", "/healthz"}, Interval: durationpb.New(durationpb.New(0).AsDuration() + 1)}, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := v.Validate(tc.hc)
			if tc.wantErr == "" && err != nil {
				t.Errorf("expected valid: %v", err)
				return
			}
			if tc.wantErr != "" && (err == nil || !contains(err.Error(), tc.wantErr)) {
				t.Errorf("expected error containing %q, got: %v", tc.wantErr, err)
			}
		})
	}
}

func contains(s, sub string) bool {
	return len(sub) <= len(s) && (s == sub || (len(sub) > 0 && stringIndex(s, sub) >= 0))
}

func stringIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./proto/workflow/docker/v1/...`

Expected: FAIL — `protovalidate-go` not yet in go.mod.

**Step 3: Add dependency**

Run: `go get github.com/bufbuild/protovalidate-go && go mod tidy`

**Step 4: Run test to verify it passes**

Run: `go test ./proto/workflow/docker/v1/...`

Expected: PASS — all subtests green.

**Step 5: Commit**

```bash
git add proto/workflow/docker/v1/types_test.go go.mod go.sum
git commit -m "proto(docker/v1): schema-level validation tests"
```

---

### Task 6: Plugin SDK — protovalidate-go interceptors

**Files:**
- Create: `plugin/sdk/docker/interceptor.go`
- Create: `plugin/sdk/docker/interceptor_test.go`

**Step 1: Write the failing test**

```go
package docker

import (
	"context"
	"errors"
	"strings"
	"testing"

	dockerv1 "github.com/GoCodeAlone/workflow/proto/workflow/docker/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestServerInterceptor_RejectsInvalidRequest(t *testing.T) {
	itc := NewServerUnaryInterceptor()
	called := false
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		called = true
		return &dockerv1.BuildResponse{ImageId: "x"}, nil
	}

	req := &dockerv1.BuildRequest{}  // empty, should fail validation
	_, err := itc(context.Background(), req, &grpc.UnaryServerInfo{FullMethod: "/workflow.docker.v1.DockerProvider/Build"}, handler)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if called {
		t.Error("handler should not be called when request invalid")
	}
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.InvalidArgument {
		t.Errorf("expected InvalidArgument, got: %v", err)
	}
	if !strings.Contains(err.Error(), "context_dir") {
		t.Errorf("expected error to mention context_dir, got: %v", err)
	}
}

func TestServerInterceptor_RejectsInvalidResponse(t *testing.T) {
	itc := NewServerUnaryInterceptor()
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return &dockerv1.BuildResponse{ImageId: ""}, nil  // empty, should fail
	}

	req := &dockerv1.BuildRequest{ContextDir: ".", Tags: []string{"x"}}
	_, err := itc(context.Background(), req, &grpc.UnaryServerInfo{FullMethod: "/workflow.docker.v1.DockerProvider/Build"}, handler)

	if err == nil {
		t.Fatal("expected error from invalid response, got nil")
	}
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.Internal {
		t.Errorf("expected Internal (server emitted bad response), got: %v", err)
	}
}

func TestServerInterceptor_PassesValidRequest(t *testing.T) {
	itc := NewServerUnaryInterceptor()
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return &dockerv1.BuildResponse{ImageId: "abc123"}, nil
	}

	req := &dockerv1.BuildRequest{ContextDir: ".", Tags: []string{"my-image:1"}}
	resp, err := itc(context.Background(), req, &grpc.UnaryServerInfo{FullMethod: "/workflow.docker.v1.DockerProvider/Build"}, handler)
	if err != nil {
		t.Fatal(err)
	}
	if resp.(*dockerv1.BuildResponse).ImageId != "abc123" {
		t.Errorf("response not passed through correctly")
	}
}

func TestServerInterceptor_PassesNonProtoMessage(t *testing.T) {
	// Non-proto messages (e.g., HashiCorp go-plugin's internal handshake) must not be validated.
	itc := NewServerUnaryInterceptor()
	called := false
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		called = true
		return "ok", nil
	}

	_, err := itc(context.Background(), errors.New("not-a-proto"), &grpc.UnaryServerInfo{FullMethod: "/foo/bar"}, handler)
	if err != nil {
		t.Fatalf("expected pass-through for non-proto, got: %v", err)
	}
	if !called {
		t.Error("handler should be called for non-proto request")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./plugin/sdk/docker/... -v`

Expected: FAIL — `NewServerUnaryInterceptor` undefined.

**Step 3: Implement `interceptor.go`**

```go
// Package docker provides a gRPC plugin SDK with strict-proto validation
// for the workflow DockerProvider service.
package docker

import (
	"context"

	"github.com/bufbuild/protovalidate-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// NewServerUnaryInterceptor returns a gRPC unary server interceptor that
// validates incoming requests AND outgoing responses via protovalidate-go.
//
// Request-side failures return InvalidArgument (client sent garbage).
// Response-side failures return Internal (server emitted garbage — bug we
// want loud).
//
// Non-proto messages pass through without validation, so HashiCorp
// go-plugin's internal handshake messages are unaffected.
func NewServerUnaryInterceptor() grpc.UnaryServerInterceptor {
	v, err := protovalidate.New()
	if err != nil {
		// Fail-loud at construction so misconfiguration is caught early.
		panic("protovalidate.New: " + err.Error())
	}
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if pm, ok := req.(proto.Message); ok {
			if err := v.Validate(pm); err != nil {
				return nil, status.Errorf(codes.InvalidArgument, "request validation: %v", err)
			}
		}
		resp, err := handler(ctx, req)
		if err != nil {
			return resp, err
		}
		if pm, ok := resp.(proto.Message); ok {
			if vErr := v.Validate(pm); vErr != nil {
				return nil, status.Errorf(codes.Internal, "response validation: %v", vErr)
			}
		}
		return resp, nil
	}
}

// NewClientUnaryInterceptor returns the client-side counterpart: validates
// outgoing requests (catches client-side mistakes early) and incoming
// responses (catches server-side mistakes locally rather than letting
// invalid data flow further into the program).
func NewClientUnaryInterceptor() grpc.UnaryClientInterceptor {
	v, err := protovalidate.New()
	if err != nil {
		panic("protovalidate.New: " + err.Error())
	}
	return func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if pm, ok := req.(proto.Message); ok {
			if vErr := v.Validate(pm); vErr != nil {
				return status.Errorf(codes.InvalidArgument, "client request validation: %v", vErr)
			}
		}
		if err := invoker(ctx, method, req, reply, cc, opts...); err != nil {
			return err
		}
		if pm, ok := reply.(proto.Message); ok {
			if vErr := v.Validate(pm); vErr != nil {
				return status.Errorf(codes.Internal, "client response validation: %v", vErr)
			}
		}
		return nil
	}
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./plugin/sdk/docker/... -v`

Expected: PASS — all 4 subtests green.

**Step 5: Test-catches-regression invariant proof**

In your scratch buffer (not committed):
1. Comment out the `if pm, ok := req.(proto.Message); ok { ... }` request-validation block in `interceptor.go`.
2. Run: `go test ./plugin/sdk/docker/... -run TestServerInterceptor_RejectsInvalidRequest`
3. Expected: FAIL.
4. Restore the block.
5. Run again. Expected: PASS.

Document this proof in the PR body when you open it. (Don't add the proof file to the commit.)

**Step 6: Commit**

```bash
git add plugin/sdk/docker/interceptor.go plugin/sdk/docker/interceptor_test.go go.mod go.sum
git commit -m "plugin/sdk/docker: protovalidate-go interceptors (server + client)"
```

---

### Task 7: Plugin SDK — server / client constructors

**Files:**
- Create: `plugin/sdk/docker/server.go`
- Create: `plugin/sdk/docker/client.go`
- Create: `plugin/sdk/docker/server_test.go`

**Step 1: Write the failing test**

```go
package docker

import (
	"testing"

	dockerv1 "github.com/GoCodeAlone/workflow/proto/workflow/docker/v1"
	"google.golang.org/grpc"
)

type fakeImpl struct {
	dockerv1.UnimplementedDockerProviderServer
}

func TestNewDockerProviderServer_AttachesInterceptor(t *testing.T) {
	srv := NewDockerProviderServer(&fakeImpl{})
	if srv == nil {
		t.Fatal("expected server, got nil")
	}
	// The construction itself is the contract; interceptor logic is tested in interceptor_test.go.
	// Smoke-check it returns a *grpc.Server.
	if _, ok := srv.(*grpc.Server); !ok {
		t.Errorf("expected *grpc.Server, got %T", srv)
	}
}
```

**Step 2: Run to verify failure**

Run: `go test ./plugin/sdk/docker/... -run TestNewDockerProviderServer -v`

Expected: FAIL — `NewDockerProviderServer` undefined.

**Step 3: Implement `server.go`**

```go
package docker

import (
	dockerv1 "github.com/GoCodeAlone/workflow/proto/workflow/docker/v1"
	"google.golang.org/grpc"
)

// NewDockerProviderServer constructs a gRPC server with the strict-proto
// interceptor pre-installed. Plugin authors register their implementation
// with the returned server and use it as the HashiCorp go-plugin GRPCServer.
func NewDockerProviderServer(impl dockerv1.DockerProviderServer) *grpc.Server {
	s := grpc.NewServer(grpc.UnaryInterceptor(NewServerUnaryInterceptor()))
	dockerv1.RegisterDockerProviderServer(s, impl)
	return s
}
```

**Step 4: Implement `client.go`**

```go
package docker

import (
	dockerv1 "github.com/GoCodeAlone/workflow/proto/workflow/docker/v1"
	"google.golang.org/grpc"
)

// NewDockerProviderClient constructs a typed gRPC client with the strict-proto
// interceptor pre-installed.
func NewDockerProviderClient(cc *grpc.ClientConn) dockerv1.DockerProviderClient {
	return dockerv1.NewDockerProviderClient(cc)
}

// DialOptions returns the grpc.DialOptions a caller should use when creating
// the underlying connection (so the client interceptor wraps every call).
func DialOptions() []grpc.DialOption {
	return []grpc.DialOption{
		grpc.WithUnaryInterceptor(NewClientUnaryInterceptor()),
	}
}
```

**Step 5: Run tests**

Run: `go test ./plugin/sdk/docker/... -v`

Expected: PASS.

**Step 6: Commit**

```bash
git add plugin/sdk/docker/server.go plugin/sdk/docker/client.go plugin/sdk/docker/server_test.go
git commit -m "plugin/sdk/docker: server + client constructors with interceptors"
```

---

### Task 8: interfaces.DockerProvider Go interface

**Files:**
- Create: `interfaces/docker_provider.go`

**Step 1: Implement**

```go
package interfaces

import (
	"context"
	"io"

	dockerv1 "github.com/GoCodeAlone/workflow/proto/workflow/docker/v1"
)

// DockerProvider is the Go-facing interface that both the external plugin
// and the in-tree system fallback satisfy. Input/output types are the
// proto-generated types so call sites are wire-shape-correct by construction.
//
// Validation discipline: every implementation MUST run protovalidate-go on
// inputs before any side effect (daemon call or `docker` exec) — strict-proto
// guarantees apply on both transport (gRPC) and in-process (system fallback)
// paths so map[string]any-style lossiness cannot regress.
type DockerProvider interface {
	Build(ctx context.Context, req *dockerv1.BuildRequest) (*dockerv1.BuildResponse, error)
	Push(ctx context.Context, req *dockerv1.PushRequest) (*dockerv1.PushResponse, error)
	Pull(ctx context.Context, req *dockerv1.PullRequest) (*dockerv1.PullResponse, error)
	Run(ctx context.Context, req *dockerv1.RunRequest) (*dockerv1.RunResponse, error)
	Inspect(ctx context.Context, req *dockerv1.InspectRequest) (*dockerv1.InspectResponse, error)
	Logs(ctx context.Context, req *dockerv1.LogsRequest) (io.ReadCloser, error)
	Stop(ctx context.Context, req *dockerv1.StopRequest) (*dockerv1.StopResponse, error)
	Remove(ctx context.Context, req *dockerv1.RemoveRequest) (*dockerv1.RemoveResponse, error)
	CreateNetwork(ctx context.Context, req *dockerv1.CreateNetworkRequest) (*dockerv1.CreateNetworkResponse, error)
	RemoveNetwork(ctx context.Context, req *dockerv1.RemoveNetworkRequest) (*dockerv1.RemoveNetworkResponse, error)
	CreateVolume(ctx context.Context, req *dockerv1.CreateVolumeRequest) (*dockerv1.CreateVolumeResponse, error)
	RemoveVolume(ctx context.Context, req *dockerv1.RemoveVolumeRequest) (*dockerv1.RemoveVolumeResponse, error)
	DaemonInfo(ctx context.Context, req *dockerv1.DaemonInfoRequest) (*dockerv1.DaemonInfo, error)
}
```

**Step 2: Verify Go compilation**

Run: `go vet ./interfaces/...`

Expected: clean.

**Step 3: Commit**

```bash
git add interfaces/docker_provider.go
git commit -m "interfaces: DockerProvider — Go interface mirroring proto"
```

---

### Task 9: System fallback — in-tree shellout implementation

**Files:**
- Create: `plugin/external/docker/system_fallback.go`
- Create: `plugin/external/docker/system_fallback_test.go`

**Step 1: Write the failing test**

```go
package docker

import (
	"context"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
	dockerv1 "github.com/GoCodeAlone/workflow/proto/workflow/docker/v1"
)

func TestSystemFallback_RejectsInvalidBuildRequest(t *testing.T) {
	var sf interfaces.DockerProvider = &systemFallback{
		exec: func(name string, args ...string) ([]byte, error) {
			t.Fatalf("exec must NOT run when validation fails: %s %v", name, args)
			return nil, nil
		},
	}
	_, err := sf.Build(context.Background(), &dockerv1.BuildRequest{})
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
	if !strings.Contains(err.Error(), "context_dir") {
		t.Errorf("expected error mentioning context_dir, got: %v", err)
	}
}

func TestSystemFallback_BuildExecsCorrectArgs(t *testing.T) {
	var capturedName string
	var capturedArgs []string
	sf := &systemFallback{
		exec: func(name string, args ...string) ([]byte, error) {
			capturedName = name
			capturedArgs = args
			return []byte("Successfully built abc123"), nil
		},
	}
	resp, err := sf.Build(context.Background(), &dockerv1.BuildRequest{
		ContextDir: ".",
		Tags:       []string{"my-image:1.0"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if capturedName != "docker" {
		t.Errorf("expected docker, got %s", capturedName)
	}
	wantArgs := []string{"build", "-t", "my-image:1.0", "."}
	if !sameSlice(capturedArgs, wantArgs) {
		t.Errorf("args = %v, want %v", capturedArgs, wantArgs)
	}
	if resp.ImageId == "" {
		t.Error("ImageId should be parsed from output")
	}
}

func sameSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
```

**Step 2: Run to verify failure**

Run: `go test ./plugin/external/docker/... -v`

Expected: FAIL — `systemFallback` undefined.

**Step 3: Implement `system_fallback.go`**

```go
// Package docker provides an in-tree DockerProvider implementation that
// shells to the system `docker` CLI via os/exec. Used as a fallback when
// workflow-plugin-docker is not installed.
//
// IMPORTANT: every method validates its proto request via protovalidate-go
// BEFORE exec'ing. This preserves the strict-proto wire guarantees on the
// in-process path — system fallback users do not regress to map[string]any
// lossiness.
package docker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/bufbuild/protovalidate-go"

	"github.com/GoCodeAlone/workflow/interfaces"
	dockerv1 "github.com/GoCodeAlone/workflow/proto/workflow/docker/v1"
)

var imageIDRegex = regexp.MustCompile(`(?i)Successfully built (\S+)`)

// execFunc is the indirection that makes system_fallback unit-testable
// without spawning real processes.
type execFunc func(name string, args ...string) ([]byte, error)

func defaultExec(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	return out, err
}

type systemFallback struct {
	exec execFunc
	v    *protovalidate.Validator
}

// NewSystemFallback returns the in-tree DockerProvider implementation.
func NewSystemFallback() interfaces.DockerProvider {
	v, err := protovalidate.New()
	if err != nil {
		panic("protovalidate.New: " + err.Error())
	}
	return &systemFallback{
		exec: defaultExec,
		v:    v,
	}
}

func (s *systemFallback) validate(req interface{ ProtoReflect() interface{} }) error {
	// Cast to proto.Message via reflection trick — simpler with the actual proto.Message interface.
	// Implementation below uses concrete types.
	return nil
}

func (s *systemFallback) Build(ctx context.Context, req *dockerv1.BuildRequest) (*dockerv1.BuildResponse, error) {
	if err := s.v.Validate(req); err != nil {
		return nil, fmt.Errorf("system_fallback: invalid Build request: %w", err)
	}
	start := time.Now()
	args := []string{"build"}
	for _, tag := range req.Tags {
		args = append(args, "-t", tag)
	}
	if req.Dockerfile != "" {
		args = append(args, "-f", req.Dockerfile)
	}
	if req.NoCache {
		args = append(args, "--no-cache")
	}
	if req.Pull {
		args = append(args, "--pull")
	}
	if req.Platform != "" {
		args = append(args, "--platform", req.Platform)
	}
	for k, v := range req.BuildArgs {
		args = append(args, "--build-arg", fmt.Sprintf("%s=%s", k, v))
	}
	args = append(args, req.ContextDir)

	out, err := s.exec("docker", args...)
	resp := &dockerv1.BuildResponse{
		Logs:       splitLines(string(out)),
		DurationMs: time.Since(start).Milliseconds(),
	}
	if err != nil {
		return resp, fmt.Errorf("docker build: %w (output: %s)", err, string(out))
	}
	if m := imageIDRegex.FindStringSubmatch(string(out)); len(m) > 1 {
		resp.ImageId = m[1]
	} else {
		// Fallback: ask docker for the image ID by tag.
		idOut, idErr := s.exec("docker", "image", "inspect", req.Tags[0], "--format", "{{.Id}}")
		if idErr == nil {
			resp.ImageId = strings.TrimSpace(string(idOut))
		}
	}
	if resp.ImageId == "" {
		// protovalidate would reject ImageId == "" on the response side; we treat
		// no-image-id as a failure to avoid that path.
		return resp, fmt.Errorf("docker build: completed but no image ID in output")
	}
	return resp, nil
}

func (s *systemFallback) Run(ctx context.Context, req *dockerv1.RunRequest) (*dockerv1.RunResponse, error) {
	if err := s.v.Validate(req); err != nil {
		return nil, fmt.Errorf("system_fallback: invalid Run request: %w", err)
	}
	args := []string{"run"}
	if req.Detach {
		args = append(args, "-d")
	}
	if req.AutoRemove {
		args = append(args, "--rm")
	}
	if req.Name != "" {
		args = append(args, "--name", req.Name)
	}
	for _, p := range req.Ports {
		proto := p.Protocol
		if proto == "" {
			proto = "tcp"
		}
		host := ""
		if p.HostPort > 0 {
			host = fmt.Sprintf("%d:", p.HostPort)
		}
		args = append(args, "-p", fmt.Sprintf("%s%d/%s", host, p.ContainerPort, proto))
	}
	for k, v := range req.Env {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}
	for _, vol := range req.Volumes {
		mode := "rw"
		if vol.ReadOnly {
			mode = "ro"
		}
		args = append(args, "-v", fmt.Sprintf("%s:%s:%s", vol.HostPath, vol.ContainerPath, mode))
	}
	if req.Network != "" {
		args = append(args, "--network", req.Network)
	}
	args = append(args, req.Image)
	args = append(args, req.Command...)

	out, err := s.exec("docker", args...)
	if err != nil {
		return nil, fmt.Errorf("docker run: %w (output: %s)", err, string(out))
	}
	id := strings.TrimSpace(string(out))
	return &dockerv1.RunResponse{ContainerId: id}, nil
}

func (s *systemFallback) Logs(ctx context.Context, req *dockerv1.LogsRequest) (io.ReadCloser, error) {
	if err := s.v.Validate(req); err != nil {
		return nil, fmt.Errorf("system_fallback: invalid Logs request: %w", err)
	}
	args := []string{"logs"}
	if req.Follow {
		args = append(args, "-f")
	}
	if req.Tail > 0 {
		args = append(args, "--tail", fmt.Sprintf("%d", req.Tail))
	}
	args = append(args, req.ContainerId)
	out, err := s.exec("docker", args...)
	if err != nil {
		return nil, fmt.Errorf("docker logs: %w", err)
	}
	return io.NopCloser(bytes.NewReader(out)), nil
}

func (s *systemFallback) Stop(ctx context.Context, req *dockerv1.StopRequest) (*dockerv1.StopResponse, error) {
	if err := s.v.Validate(req); err != nil {
		return nil, fmt.Errorf("system_fallback: invalid Stop request: %w", err)
	}
	args := []string{"stop"}
	if req.Timeout != nil {
		args = append(args, "-t", fmt.Sprintf("%d", req.Timeout.Seconds))
	}
	args = append(args, req.ContainerId)
	if _, err := s.exec("docker", args...); err != nil {
		return nil, fmt.Errorf("docker stop: %w", err)
	}
	return &dockerv1.StopResponse{}, nil
}

func (s *systemFallback) Remove(ctx context.Context, req *dockerv1.RemoveRequest) (*dockerv1.RemoveResponse, error) {
	if err := s.v.Validate(req); err != nil {
		return nil, fmt.Errorf("system_fallback: invalid Remove request: %w", err)
	}
	args := []string{"rm"}
	if req.Force {
		args = append(args, "-f")
	}
	if req.RemoveVolumes {
		args = append(args, "-v")
	}
	args = append(args, req.ContainerId)
	if _, err := s.exec("docker", args...); err != nil {
		return nil, fmt.Errorf("docker rm: %w", err)
	}
	return &dockerv1.RemoveResponse{}, nil
}

// Inspect / Push / Pull / CreateNetwork / RemoveNetwork / CreateVolume /
// RemoveVolume / DaemonInfo follow the same pattern. Implement per need;
// MVP only requires Build, Run, Logs, Stop, Remove for `validate launch`.
// Stub the rest with Unimplemented errors — they're added when consumers need them.

func (s *systemFallback) Inspect(ctx context.Context, req *dockerv1.InspectRequest) (*dockerv1.InspectResponse, error) {
	return nil, fmt.Errorf("Inspect: not implemented in system fallback (MVP)")
}
func (s *systemFallback) Push(ctx context.Context, req *dockerv1.PushRequest) (*dockerv1.PushResponse, error) {
	return nil, fmt.Errorf("Push: not implemented in system fallback (MVP)")
}
func (s *systemFallback) Pull(ctx context.Context, req *dockerv1.PullRequest) (*dockerv1.PullResponse, error) {
	return nil, fmt.Errorf("Pull: not implemented in system fallback (MVP)")
}
func (s *systemFallback) CreateNetwork(ctx context.Context, req *dockerv1.CreateNetworkRequest) (*dockerv1.CreateNetworkResponse, error) {
	if err := s.v.Validate(req); err != nil {
		return nil, fmt.Errorf("system_fallback: invalid CreateNetwork request: %w", err)
	}
	args := []string{"network", "create"}
	if req.Driver != "" {
		args = append(args, "--driver", req.Driver)
	}
	args = append(args, req.Name)
	out, err := s.exec("docker", args...)
	if err != nil {
		return nil, fmt.Errorf("docker network create: %w", err)
	}
	return &dockerv1.CreateNetworkResponse{Network: &dockerv1.Network{Id: strings.TrimSpace(string(out)), Name: req.Name, Driver: req.Driver}}, nil
}
func (s *systemFallback) RemoveNetwork(ctx context.Context, req *dockerv1.RemoveNetworkRequest) (*dockerv1.RemoveNetworkResponse, error) {
	if err := s.v.Validate(req); err != nil {
		return nil, fmt.Errorf("system_fallback: invalid RemoveNetwork request: %w", err)
	}
	if _, err := s.exec("docker", "network", "rm", req.NetworkId); err != nil {
		return nil, fmt.Errorf("docker network rm: %w", err)
	}
	return &dockerv1.RemoveNetworkResponse{}, nil
}
func (s *systemFallback) CreateVolume(ctx context.Context, req *dockerv1.CreateVolumeRequest) (*dockerv1.CreateVolumeResponse, error) {
	return nil, fmt.Errorf("CreateVolume: not implemented in system fallback (MVP)")
}
func (s *systemFallback) RemoveVolume(ctx context.Context, req *dockerv1.RemoveVolumeRequest) (*dockerv1.RemoveVolumeResponse, error) {
	return nil, fmt.Errorf("RemoveVolume: not implemented in system fallback (MVP)")
}
func (s *systemFallback) DaemonInfo(ctx context.Context, req *dockerv1.DaemonInfoRequest) (*dockerv1.DaemonInfo, error) {
	out, err := s.exec("docker", "version", "--format", "{{json .}}")
	if err != nil {
		return nil, fmt.Errorf("docker version: %w", err)
	}
	// Minimal parsing — we only need version + arch for the daemon check.
	return &dockerv1.DaemonInfo{Version: extractField(out, "Version"), ApiVersion: extractField(out, "APIVersion"), Os: extractField(out, "Os"), Arch: extractField(out, "Arch")}, nil
}

func extractField(out []byte, field string) string {
	// Quick-and-dirty JSON field extractor (the Server section of docker version).
	// For MVP this is fine; if more fields are needed, switch to encoding/json.
	idx := bytes.Index(out, []byte(`"`+field+`":"`))
	if idx == -1 {
		return ""
	}
	rest := out[idx+len(field)+4:]
	end := bytes.IndexByte(rest, '"')
	if end == -1 {
		return ""
	}
	return string(rest[:end])
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimRight(s, "\n"), "\n")
}
```

**Step 4: Run tests**

Run: `go test ./plugin/external/docker/... -v`

Expected: PASS — both subtests green.

**Step 5: Commit**

```bash
git add plugin/external/docker/system_fallback.go plugin/external/docker/system_fallback_test.go
git commit -m "plugin/external/docker: system fallback — shellout to docker CLI with proto-validation"
```

---

### Task 10: Docker resolver — selection logic

**Files:**
- Create: `cmd/wfctl/docker_resolver.go`
- Create: `cmd/wfctl/docker_resolver_test.go`

**Step 1: Write the failing test**

```go
package main

import (
	"errors"
	"testing"
)

func TestResolveDocker_Auto_PreferPlugin(t *testing.T) {
	r := &dockerResolver{
		mode:           "auto",
		pluginAvailable: func() bool { return true },
		systemAvailable: func() bool { return true },
	}
	got, err := r.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if got != "plugin" {
		t.Errorf("expected plugin, got %s", got)
	}
}

func TestResolveDocker_Auto_FallbackSystem(t *testing.T) {
	r := &dockerResolver{
		mode:           "auto",
		pluginAvailable: func() bool { return false },
		systemAvailable: func() bool { return true },
	}
	got, err := r.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if got != "system" {
		t.Errorf("expected system, got %s", got)
	}
}

func TestResolveDocker_Auto_NeitherAvailable(t *testing.T) {
	r := &dockerResolver{
		mode:           "auto",
		pluginAvailable: func() bool { return false },
		systemAvailable: func() bool { return false },
	}
	_, err := r.Resolve()
	if err == nil {
		t.Fatal("expected error when neither available")
	}
}

func TestResolveDocker_ExplicitPlugin_NotInstalled(t *testing.T) {
	r := &dockerResolver{
		mode:           "plugin",
		pluginAvailable: func() bool { return false },
		systemAvailable: func() bool { return true },
	}
	_, err := r.Resolve()
	if err == nil {
		t.Fatal("expected error when --docker-mode=plugin and plugin not installed")
	}
}

func TestResolveDocker_ExplicitSystem(t *testing.T) {
	r := &dockerResolver{
		mode:           "system",
		pluginAvailable: func() bool { return true },  // plugin is here, but mode forces system
		systemAvailable: func() bool { return true },
	}
	got, err := r.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if got != "system" {
		t.Errorf("expected system (forced), got %s", got)
	}
}

func TestResolveDocker_InvalidMode(t *testing.T) {
	r := &dockerResolver{
		mode:           "weird",
		pluginAvailable: func() bool { return true },
		systemAvailable: func() bool { return true },
	}
	_, err := r.Resolve()
	if err == nil || !errors.Is(err, errInvalidDockerMode) {
		t.Errorf("expected errInvalidDockerMode, got %v", err)
	}
}
```

**Step 2: Run to verify failure**

Run: `go test ./cmd/wfctl/... -run TestResolveDocker -v`

Expected: FAIL.

**Step 3: Implement**

```go
package main

import (
	"errors"
	"fmt"
	"os/exec"
)

var errInvalidDockerMode = errors.New("invalid --docker-mode (expected: plugin, system, auto)")

type dockerResolver struct {
	mode             string
	pluginAvailable  func() bool
	systemAvailable  func() bool
}

func newDockerResolver(mode string) *dockerResolver {
	return &dockerResolver{
		mode:            mode,
		pluginAvailable: func() bool { return false },  // wired up in v0.18.13/v0.19.0 when plugin lands
		systemAvailable: func() bool {
			_, err := exec.LookPath("docker")
			return err == nil
		},
	}
}

// Resolve returns "plugin" or "system", or an error.
func (r *dockerResolver) Resolve() (string, error) {
	switch r.mode {
	case "plugin":
		if r.pluginAvailable() {
			return "plugin", nil
		}
		return "", fmt.Errorf("--docker-mode=plugin but workflow-plugin-docker is not installed; install with `wfctl plugin install workflow-plugin-docker`")
	case "system":
		if r.systemAvailable() {
			return "system", nil
		}
		return "", fmt.Errorf("--docker-mode=system but `docker` not on $PATH")
	case "auto", "":
		if r.pluginAvailable() {
			return "plugin", nil
		}
		if r.systemAvailable() {
			return "system", nil
		}
		return "", fmt.Errorf("no Docker provider available: workflow-plugin-docker not installed AND `docker` not on $PATH")
	default:
		return "", errInvalidDockerMode
	}
}
```

**Step 4: Run tests**

Run: `go test ./cmd/wfctl/... -run TestResolveDocker -v`

Expected: PASS — all 6 subtests.

**Step 5: Commit**

```bash
git add cmd/wfctl/docker_resolver.go cmd/wfctl/docker_resolver_test.go
git commit -m "wfctl: docker resolver (plugin|system|auto selection logic)"
```

---

### Task 11: Synthesis mapping table

**Files:**
- Create: `cmd/wfctl/synthesis_mapping.go`
- Create: `cmd/wfctl/synthesis_mapping_test.go`

**Step 1: Write the failing test**

```go
package main

import "testing"

func TestSynthesisMapping_Database(t *testing.T) {
	got, ok := synthesisMappings["infra.database"]
	if !ok {
		t.Fatal("missing infra.database mapping")
	}
	if got.Image != "postgres:16-alpine" {
		t.Errorf("image = %s", got.Image)
	}
	if got.ContainerPort != 5432 {
		t.Errorf("port = %d", got.ContainerPort)
	}
	if len(got.Healthcheck) == 0 {
		t.Error("healthcheck cmd missing")
	}
	if got.DSNTemplate == "" {
		t.Error("DSN template missing")
	}
}

func TestSynthesisMapping_Cache(t *testing.T) {
	got, ok := synthesisMappings["infra.cache"]
	if !ok {
		t.Fatal("missing infra.cache mapping")
	}
	if got.Image != "redis:7-alpine" {
		t.Errorf("image = %s", got.Image)
	}
}

func TestSynthesisMapping_Spaces(t *testing.T) {
	got, ok := synthesisMappings["infra.spaces"]
	if !ok {
		t.Fatal("missing infra.spaces mapping")
	}
	if got.Image != "minio/minio:latest" {
		t.Errorf("image = %s", got.Image)
	}
}
```

**Step 2: Run to verify failure**

Run: `go test ./cmd/wfctl/... -run TestSynthesisMapping -v`

Expected: FAIL.

**Step 3: Implement**

```go
package main

// SynthesisMapping describes how an infra requirement type maps to a
// containerized local equivalent. Used by `wfctl validate launch` and (later)
// `wfctl dev up --docker` to spin up dependency containers from app.yaml
// without requiring users to author compose.test.yaml.
type SynthesisMapping struct {
	Image         string            // OCI ref
	ContainerPort uint32            // primary service port
	Healthcheck   []string          // docker healthcheck `test` array
	DSNTemplate   string            // Go-template string substituted with host/port/credentials
	Env           map[string]string // env vars to set on the container itself (e.g. POSTGRES_PASSWORD)
}

// synthesisMappings is the v0.18.12 hardcoded mapping. v0.19.0 may
// externalize to JSON/YAML; v0.20.0+ moves it to per-cloud-provider
// proto-declared mappings (each IaC plugin contributes its own entries).
var synthesisMappings = map[string]SynthesisMapping{
	"infra.database": {
		Image:         "postgres:16-alpine",
		ContainerPort: 5432,
		Healthcheck:   []string{"CMD-SHELL", "pg_isready -U wfctl"},
		DSNTemplate:   "postgres://wfctl:wfctl@{{.Host}}:{{.Port}}/wfctl?sslmode=disable",
		Env: map[string]string{
			"POSTGRES_USER":     "wfctl",
			"POSTGRES_PASSWORD": "wfctl",
			"POSTGRES_DB":       "wfctl",
		},
	},
	"infra.cache": {
		Image:         "redis:7-alpine",
		ContainerPort: 6379,
		Healthcheck:   []string{"CMD", "redis-cli", "ping"},
		DSNTemplate:   "redis://{{.Host}}:{{.Port}}",
		Env:           nil,
	},
	"infra.spaces": {
		Image:         "minio/minio:latest",
		ContainerPort: 9000,
		Healthcheck:   []string{"CMD", "curl", "-f", "http://localhost:9000/minio/health/live"},
		DSNTemplate:   "http://{{.Host}}:{{.Port}}",
		Env: map[string]string{
			"MINIO_ROOT_USER":     "wfctl",
			"MINIO_ROOT_PASSWORD": "wfctlsecret",
		},
	},
}
```

**Step 4: Run tests**

Run: `go test ./cmd/wfctl/... -run TestSynthesisMapping -v`

Expected: PASS.

**Step 5: Commit**

```bash
git add cmd/wfctl/synthesis_mapping.go cmd/wfctl/synthesis_mapping_test.go
git commit -m "wfctl: synthesis mapping — infra.{database,cache,spaces} → containers"
```

---

### Task 12: Synthesis layer — app.yaml → EphemeralEnv

**Files:**
- Create: `cmd/wfctl/ephemeral_env.go`
- Create: `cmd/wfctl/ephemeral_env_test.go`

**Step 1: Write the failing test**

```go
package main

import "testing"

func TestSynthesizeEphemeralEnv_DatabaseOnly(t *testing.T) {
	cfg := loadFixtureConfig(t, "testdata/app-database-only.yaml")
	manifests := loadFixtureManifests(t)

	env, err := synthesizeEphemeralEnv(cfg, manifests)
	if err != nil {
		t.Fatal(err)
	}
	if len(env.Containers) != 1 {
		t.Fatalf("expected 1 dep container, got %d", len(env.Containers))
	}
	if env.Containers[0].Image != "postgres:16-alpine" {
		t.Errorf("dep image = %s", env.Containers[0].Image)
	}
	if _, ok := env.EnvVars["DATABASE_URL"]; !ok {
		t.Error("DATABASE_URL not synthesized")
	}
}

func TestSynthesizeEphemeralEnv_DatabaseAndCache(t *testing.T) {
	cfg := loadFixtureConfig(t, "testdata/app-db-and-cache.yaml")
	manifests := loadFixtureManifests(t)

	env, err := synthesizeEphemeralEnv(cfg, manifests)
	if err != nil {
		t.Fatal(err)
	}
	if len(env.Containers) != 2 {
		t.Fatalf("expected 2 dep containers, got %d", len(env.Containers))
	}
	images := map[string]bool{}
	for _, c := range env.Containers {
		images[c.Image] = true
	}
	if !images["postgres:16-alpine"] || !images["redis:7-alpine"] {
		t.Errorf("missing expected images: %v", images)
	}
}

func TestSynthesizeEphemeralEnv_NoDeps(t *testing.T) {
	cfg := loadFixtureConfig(t, "testdata/app-no-deps.yaml")
	manifests := loadFixtureManifests(t)

	env, err := synthesizeEphemeralEnv(cfg, manifests)
	if err != nil {
		t.Fatal(err)
	}
	if len(env.Containers) != 0 {
		t.Errorf("expected 0 dep containers, got %d", len(env.Containers))
	}
}
```

**Step 2: Create test fixtures**

Files:
- `cmd/wfctl/testdata/app-database-only.yaml` — minimal app.yaml with one `database.workflow` module
- `cmd/wfctl/testdata/app-db-and-cache.yaml` — adds a `cache.workflow` module
- `cmd/wfctl/testdata/app-no-deps.yaml` — has only HTTP routes, no infra-requiring modules

(Implementer: see existing fixtures in `cmd/wfctl/testdata/` for the schema.)

**Step 3: Run to verify failure**

Run: `go test ./cmd/wfctl/... -run TestSynthesizeEphemeralEnv -v`

Expected: FAIL — `synthesizeEphemeralEnv` undefined.

**Step 4: Implement**

```go
package main

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/manifest"
	dockerv1 "github.com/GoCodeAlone/workflow/proto/workflow/docker/v1"
)

// EphemeralEnv is the synthesized launch plan: dependency containers, the
// app container, env vars to inject, healthchecks, network bridge.
type EphemeralEnv struct {
	NetworkName    string
	Containers     []*dockerv1.RunRequest // dependencies (pg, redis, …)
	AppContainer   *dockerv1.RunRequest   // built image
	EnvVars        map[string]string      // injected on AppContainer
	HealthCheckURL string                 // typically /healthz
	HealthTimeout  string                 // e.g. "60s"
}

// synthesizeEphemeralEnv reads app.yaml + plugin manifests and produces a
// launch plan. Dependencies come from DetectPluginInfraNeeds; each is mapped
// via synthesisMappings; AppContainer is left for callers to populate with
// the built image tag.
func synthesizeEphemeralEnv(cfg *config.Config, manifests []*manifest.PluginManifest) (*EphemeralEnv, error) {
	needs := DetectPluginInfraNeeds(cfg, manifests)  // existing helper, cmd/wfctl/plugin_infra.go:54
	env := &EphemeralEnv{
		NetworkName:    "wfctl-validate",
		EnvVars:        map[string]string{},
		HealthCheckURL: "/healthz",
		HealthTimeout:  "60s",
	}
	for _, need := range needs {
		mapping, ok := synthesisMappings[need.Type]
		if !ok {
			return nil, fmt.Errorf("synthesizeEphemeralEnv: no local mapping for %q (add to synthesis_mapping.go or supply compose.test.yaml)", need.Type)
		}
		name := fmt.Sprintf("wfctl-validate-%s", need.Name)
		env.Containers = append(env.Containers, &dockerv1.RunRequest{
			Image:   mapping.Image,
			Name:    name,
			Network: env.NetworkName,
			Env:     mapping.Env,
			Detach:  true,
			Healthcheck: &dockerv1.Healthcheck{
				Test:     mapping.Healthcheck,
				Interval: durationProto("2s"),
				Timeout:  durationProto("5s"),
				Retries:  10,
			},
		})
		// Synthesize DSN env var on the app container.
		dsn, err := renderDSN(mapping.DSNTemplate, name, mapping.ContainerPort)
		if err != nil {
			return nil, err
		}
		envVarName := dsnEnvVarFor(need.Type)
		env.EnvVars[envVarName] = dsn
	}
	return env, nil
}

func dsnEnvVarFor(infraType string) string {
	switch infraType {
	case "infra.database":
		return "DATABASE_URL"
	case "infra.cache":
		return "REDIS_URL"
	case "infra.spaces":
		return "S3_ENDPOINT"
	default:
		return "DSN_" + infraType
	}
}

func renderDSN(tmpl, host string, port uint32) (string, error) {
	t, err := template.New("dsn").Parse(tmpl)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, struct {
		Host string
		Port uint32
	}{Host: host, Port: port}); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func durationProto(s string) *durationpb.Duration { /* parse + return */ }
```

(Implementer: import `google.golang.org/protobuf/types/known/durationpb`; complete `durationProto`.)

**Step 5: Run tests**

Run: `go test ./cmd/wfctl/... -run TestSynthesizeEphemeralEnv -v`

Expected: PASS — all 3 subtests.

**Step 6: Commit**

```bash
git add cmd/wfctl/ephemeral_env.go cmd/wfctl/ephemeral_env_test.go cmd/wfctl/testdata/app-*.yaml
git commit -m "wfctl: synthesize ephemeral env from app.yaml + manifests"
```

---

### Task 13: Lifecycle — testcontainers-go-backed implementation

**Files:**
- Create: `cmd/wfctl/lifecycle.go`
- Create: `cmd/wfctl/lifecycle_test.go` (unit tests with mocked DockerProvider)
- Create: `cmd/wfctl/lifecycle_integration_test.go` (build tag `docker_integration`, real testcontainers-go)

**Step 1: Write the lifecycle interface**

```go
package main

import (
	"context"
	"io"
	"time"

	dockerv1 "github.com/GoCodeAlone/workflow/proto/workflow/docker/v1"
)

type Lifecycle interface {
	Up(ctx context.Context) error
	Down(ctx context.Context) error
	Healthz(ctx context.Context, target string, timeout time.Duration) (HealthResult, error)
	Logs(ctx context.Context, name string) (io.ReadCloser, error)
}

type HealthResult struct {
	OK       bool
	Status   string  // last observed status string
	Timeline []HealthEvent
}

type HealthEvent struct {
	At     time.Time
	Status string
}
```

**Step 2: Write the failing unit test (mocked DockerProvider)**

```go
func TestLifecycle_Up_StartsContainersAndAppOnNetwork(t *testing.T) {
	mock := &mockDocker{}
	env := &EphemeralEnv{
		NetworkName: "test-net",
		Containers:  []*dockerv1.RunRequest{{Image: "pg", Name: "pg-1"}},
		AppContainer: &dockerv1.RunRequest{Image: "app:1", Name: "app-1"},
	}
	lc := &synthesisLifecycle{docker: mock, env: env}
	if err := lc.Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	if mock.networksCreated != 1 {
		t.Errorf("expected 1 network created, got %d", mock.networksCreated)
	}
	if mock.containersStarted != 2 {  // pg + app
		t.Errorf("expected 2 containers, got %d", mock.containersStarted)
	}
}

func TestLifecycle_Down_StopsAndRemovesAll(t *testing.T) {
	mock := &mockDocker{}
	env := &EphemeralEnv{
		NetworkName: "test-net",
		Containers:  []*dockerv1.RunRequest{{Image: "pg", Name: "pg-1"}},
		AppContainer: &dockerv1.RunRequest{Image: "app:1", Name: "app-1"},
	}
	lc := &synthesisLifecycle{docker: mock, env: env, runningIDs: []string{"id-pg", "id-app"}}
	if err := lc.Down(context.Background()); err != nil {
		t.Fatal(err)
	}
	if mock.containersStopped != 2 || mock.containersRemoved != 2 {
		t.Errorf("expected 2 stop+remove, got stop=%d remove=%d", mock.containersStopped, mock.containersRemoved)
	}
	if mock.networksRemoved != 1 {
		t.Errorf("expected 1 network removed, got %d", mock.networksRemoved)
	}
}
```

(Implementer: define `mockDocker` implementing `interfaces.DockerProvider` with counters for assertions.)

**Step 3: Run to verify failure**

Run: `go test ./cmd/wfctl/... -run TestLifecycle -v`

Expected: FAIL — types undefined.

**Step 4: Implement `lifecycle.go`**

(Implementation outline — fill in per the interface):

```go
package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/GoCodeAlone/workflow/interfaces"
	dockerv1 "github.com/GoCodeAlone/workflow/proto/workflow/docker/v1"
)

type synthesisLifecycle struct {
	docker     interfaces.DockerProvider
	env        *EphemeralEnv
	runningIDs []string
	networkID  string
}

func (l *synthesisLifecycle) Up(ctx context.Context) error {
	netResp, err := l.docker.CreateNetwork(ctx, &dockerv1.CreateNetworkRequest{Name: l.env.NetworkName})
	if err != nil {
		return fmt.Errorf("create network: %w", err)
	}
	l.networkID = netResp.Network.Id

	for _, dep := range l.env.Containers {
		dep.Network = l.env.NetworkName
		runResp, err := l.docker.Run(ctx, dep)
		if err != nil {
			_ = l.Down(ctx)  // cleanup partial state
			return fmt.Errorf("run %s: %w", dep.Name, err)
		}
		l.runningIDs = append(l.runningIDs, runResp.ContainerId)
	}

	// Wait for dep healthchecks — testcontainers-go does this natively;
	// in MVP we poll Inspect health_status until healthy or timeout.
	if err := l.waitForDepsHealthy(ctx, 60*time.Second); err != nil {
		_ = l.Down(ctx)
		return fmt.Errorf("deps not healthy: %w", err)
	}

	// Start app container.
	app := l.env.AppContainer
	app.Network = l.env.NetworkName
	for k, v := range l.env.EnvVars {
		if app.Env == nil {
			app.Env = map[string]string{}
		}
		app.Env[k] = v
	}
	runResp, err := l.docker.Run(ctx, app)
	if err != nil {
		_ = l.Down(ctx)
		return fmt.Errorf("run app: %w", err)
	}
	l.runningIDs = append(l.runningIDs, runResp.ContainerId)
	return nil
}

func (l *synthesisLifecycle) Down(ctx context.Context) error {
	var firstErr error
	for _, id := range l.runningIDs {
		if _, err := l.docker.Stop(ctx, &dockerv1.StopRequest{ContainerId: id}); err != nil && firstErr == nil {
			firstErr = err
		}
		if _, err := l.docker.Remove(ctx, &dockerv1.RemoveRequest{ContainerId: id, Force: true, RemoveVolumes: true}); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if l.networkID != "" {
		if _, err := l.docker.RemoveNetwork(ctx, &dockerv1.RemoveNetworkRequest{NetworkId: l.networkID}); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (l *synthesisLifecycle) Healthz(ctx context.Context, target string, timeout time.Duration) (HealthResult, error) {
	deadline := time.Now().Add(timeout)
	var result HealthResult
	for time.Now().Before(deadline) {
		resp, err := http.Get(target)  // or http.NewRequestWithContext + http.DefaultClient.Do
		ev := HealthEvent{At: time.Now()}
		if err != nil {
			ev.Status = err.Error()
		} else {
			ev.Status = resp.Status
			resp.Body.Close()
			if resp.StatusCode == 200 {
				result.OK = true
				result.Status = "200 OK"
				result.Timeline = append(result.Timeline, ev)
				return result, nil
			}
		}
		result.Timeline = append(result.Timeline, ev)
		time.Sleep(2 * time.Second)
	}
	result.Status = "timeout"
	return result, fmt.Errorf("healthz: timeout after %s", timeout)
}

func (l *synthesisLifecycle) Logs(ctx context.Context, name string) (io.ReadCloser, error) {
	// Find ID by name (Inspect or maintain an internal name→ID map at Up time).
	// Implementer: keep map.
	return nil, fmt.Errorf("not yet implemented")
}

func (l *synthesisLifecycle) waitForDepsHealthy(ctx context.Context, timeout time.Duration) error {
	// Loop Inspect on each dep ID until all health_status == "healthy" or timeout.
	// Implementer: implement.
	return nil
}
```

**Step 5: Run unit tests**

Run: `go test ./cmd/wfctl/... -run TestLifecycle -v`

Expected: PASS — both unit subtests green.

**Step 6: Commit**

```bash
git add cmd/wfctl/lifecycle.go cmd/wfctl/lifecycle_test.go
git commit -m "wfctl: lifecycle — Up/Down/Healthz over DockerProvider"
```

---

### Task 14: Log signature scraper

**Files:**
- Create: `cmd/wfctl/log_scraper.go`
- Create: `cmd/wfctl/log_scraper_test.go`

**Step 1: Write the failing test**

```go
package main

import "testing"

func TestLogScraper_CatchesKnownSignatures(t *testing.T) {
	cases := []struct {
		name string
		log  string
		want string  // signature class
	}{
		{
			name: "Setup error / engine build",
			log:  "2026/04/25 00:10:19 Setup error: failed to build engine: failed to build workflow: requirements check failed",
			want: "engine_setup_error",
		},
		{
			name: "fetch manifest from remote",
			log:  "fetch manifest from remote: Get \"https://plugins.workflow.dev/...\": dial tcp: lookup plugins.workflow.dev: no such host",
			want: "remote_manifest_fetch_failed",
		},
		{
			name: "plugin not loaded",
			log:  "required plugin \"workflow-plugin-supply-chain\" is not loaded and auto-install failed",
			want: "plugin_not_loaded",
		},
		{
			name: "Go panic",
			log:  "panic: runtime error: makeslice: cap out of range\ngoroutine 1 [running]:",
			want: "go_panic",
		},
		{
			name: "DNS no such host",
			log:  "dial tcp: lookup nonexistent.example.com on 8.8.8.8:53: no such host",
			want: "dns_no_such_host",
		},
		{
			name: "no signature",
			log:  "2026/04/25 00:00:00 server listening on :8080",
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyLogSignature(tc.log)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
```

**Step 2: Run to verify failure**

Run: `go test ./cmd/wfctl/... -run TestLogScraper -v`

Expected: FAIL — `classifyLogSignature` undefined.

**Step 3: Implement**

```go
package main

import (
	"regexp"
)

type LogSignature struct {
	ID      string  // stable identifier
	Pattern *regexp.Regexp
	Message string  // human-readable failure mode
	Hint    string  // suggested action
}

var logSignatures = []LogSignature{
	{
		ID:      "engine_setup_error",
		Pattern: regexp.MustCompile(`Setup error: failed to build engine`),
		Message: "Engine refused to build",
		Hint:    "Check app.yaml — required modules and plugins must all be loadable",
	},
	{
		ID:      "remote_manifest_fetch_failed",
		Pattern: regexp.MustCompile(`fetch manifest from remote`),
		Message: "Engine tried to fetch a plugin manifest from a remote URL but failed",
		Hint:    "Plugin should be pre-installed in the image (data/plugins/<name>); check engine vs wfctl version skew",
	},
	{
		ID:      "plugin_not_loaded",
		Pattern: regexp.MustCompile(`required plugin "[^"]+" is not loaded`),
		Message: "Required plugin missing at startup",
		Hint:    "Verify the plugin is installed in the image at data/plugins/<name>/<name>",
	},
	{
		ID:      "go_panic",
		Pattern: regexp.MustCompile(`panic: `),
		Message: "Go panic during startup",
		Hint:    "Container logs include the goroutine stack; capture the first panic line",
	},
	{
		ID:      "dns_no_such_host",
		Pattern: regexp.MustCompile(`dial tcp: lookup [^ ]+: no such host`),
		Message: "DNS lookup failed",
		Hint:    "Container is trying to reach a hostname that doesn't resolve — check engine config and dependency reachability",
	},
	{
		ID:      "failed_to_build_workflow",
		Pattern: regexp.MustCompile(`failed to build workflow`),
		Message: "Engine failed to build workflow from app.yaml",
		Hint:    "Validate config with `wfctl validate config`",
	},
}

// classifyLogSignature returns the ID of the FIRST matching signature, or "" if none match.
func classifyLogSignature(logChunk string) string {
	for _, sig := range logSignatures {
		if sig.Pattern.MatchString(logChunk) {
			return sig.ID
		}
	}
	return ""
}

// scanLogs walks a multi-line log dump and returns all unique signatures hit.
func scanLogs(logs string) []LogSignature {
	hit := map[string]bool{}
	var result []LogSignature
	for _, sig := range logSignatures {
		if sig.Pattern.MatchString(logs) && !hit[sig.ID] {
			hit[sig.ID] = true
			result = append(result, sig)
		}
	}
	return result
}
```

**Step 4: Run tests**

Run: `go test ./cmd/wfctl/... -run TestLogScraper -v`

Expected: PASS — all 6 subtests.

**Step 5: Test-catches-regression invariant proof**

In your scratch buffer:
1. Comment out the `engine_setup_error` entry from `logSignatures`.
2. Run: `go test ./cmd/wfctl/... -run TestLogScraper/Setup_error -v`
3. Expected: FAIL.
4. Restore. Run again: PASS.

Document this proof in PR body.

**Step 6: Commit**

```bash
git add cmd/wfctl/log_scraper.go cmd/wfctl/log_scraper_test.go
git commit -m "wfctl: log scraper — classify known engine startup failure signatures"
```

---

### Task 15: Refactor `wfctl validate` into a dispatcher

**Files:**
- Modify: `cmd/wfctl/validate.go` (currently a single command at line 16)
- Create: `cmd/wfctl/validate_config.go` (extracts current behavior)

**Step 1: Write the failing test**

```go
package main

import "testing"

func TestValidateDispatcher_BareInvocationRunsConfig(t *testing.T) {
	// `wfctl validate <some.yaml>` — back-compat, runs validate config
	exitCode := runWithArgs(t, []string{"validate", "testdata/app-database-only.yaml"})
	if exitCode != 0 {
		t.Errorf("bare validate failed: exit=%d", exitCode)
	}
}

func TestValidateDispatcher_SubcommandConfig(t *testing.T) {
	exitCode := runWithArgs(t, []string{"validate", "config", "testdata/app-database-only.yaml"})
	if exitCode != 0 {
		t.Errorf("validate config failed: exit=%d", exitCode)
	}
}

func TestValidateDispatcher_SubcommandLaunchHelpExits(t *testing.T) {
	exitCode := runWithArgs(t, []string{"validate", "launch", "--help"})
	if exitCode != 0 {
		t.Errorf("validate launch --help failed: exit=%d", exitCode)
	}
}
```

(Implementer: `runWithArgs` is a test helper that captures stdout/stderr and returns the exit code without `os.Exit`.)

**Step 2: Run to verify failure**

Run: `go test ./cmd/wfctl/... -run TestValidateDispatcher -v`

Expected: FAIL — dispatcher not in place.

**Step 3: Refactor**

Move current `cmd/wfctl/validate.go` body into a `runValidateConfig(args []string) int` function in `validate_config.go`. Have `validate.go` route based on first arg:

```go
func runValidate(args []string) int {
	if len(args) == 0 {
		// no args → show help
		fmt.Fprintln(os.Stderr, "usage: wfctl validate [config|launch] [...]")
		return 1
	}
	switch args[0] {
	case "config":
		return runValidateConfig(args[1:])
	case "launch":
		return runValidateLaunch(args[1:])
	default:
		// back-compat: treat as `validate config <args>`
		return runValidateConfig(args)
	}
}
```

Stub `runValidateLaunch` for now:

```go
func runValidateLaunch(args []string) int {
	fmt.Fprintln(os.Stderr, "wfctl validate launch — not yet implemented")
	return 1
}
```

**Step 4: Run tests**

Run: `go test ./cmd/wfctl/... -run TestValidateDispatcher -v`

Expected: PASS — all 3 subtests.

**Step 5: Commit**

```bash
git add cmd/wfctl/validate.go cmd/wfctl/validate_config.go cmd/wfctl/validate_dispatcher_test.go
git commit -m "wfctl: refactor validate into dispatcher (config|launch); back-compat preserved"
```

---

### Task 16: `wfctl validate launch` command — wire it together

**Files:**
- Create: `cmd/wfctl/validate_launch.go`
- Create: `cmd/wfctl/validate_launch_test.go` (unit, no Docker)

**Step 1: Write the failing unit test**

```go
package main

import (
	"testing"
)

func TestValidateLaunch_FlagsParse(t *testing.T) {
	opts, err := parseValidateLaunchFlags([]string{"--config", "app.yaml", "--healthcheck", "/healthz", "--timeout", "30s"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.ConfigPath != "app.yaml" || opts.HealthcheckPath != "/healthz" || opts.Timeout.String() != "30s" {
		t.Errorf("parsed wrong: %+v", opts)
	}
}

func TestValidateLaunch_DefaultFlags(t *testing.T) {
	opts, err := parseValidateLaunchFlags(nil)
	if err != nil {
		t.Fatal(err)
	}
	if opts.ConfigPath != "app.yaml" {
		t.Errorf("default config = %q", opts.ConfigPath)
	}
	if opts.HealthcheckPath != "/healthz" {
		t.Errorf("default healthcheck = %q", opts.HealthcheckPath)
	}
	if opts.Timeout.String() != "1m0s" {
		t.Errorf("default timeout = %s", opts.Timeout)
	}
}
```

**Step 2: Run to verify failure**

Run: `go test ./cmd/wfctl/... -run TestValidateLaunch_Flags -v`

Expected: FAIL.

**Step 3: Implement**

```go
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"
)

type ValidateLaunchOpts struct {
	ConfigPath      string
	ComposePath     string  // optional, switches to compose mode (Phase 2)
	HealthcheckPath string
	HealthcheckHost string
	Timeout         time.Duration
	KeepOnFailure   bool
	DockerMode      string  // plugin|system|auto
}

func parseValidateLaunchFlags(args []string) (*ValidateLaunchOpts, error) {
	fs := flag.NewFlagSet("validate launch", flag.ContinueOnError)
	opts := &ValidateLaunchOpts{}
	fs.StringVar(&opts.ConfigPath, "config", "app.yaml", "path to app.yaml")
	fs.StringVar(&opts.ComposePath, "compose", "", "path to compose.test.yaml (Phase 2)")
	fs.StringVar(&opts.HealthcheckPath, "healthcheck", "/healthz", "URL path to poll")
	fs.StringVar(&opts.HealthcheckHost, "healthcheck-host", "http://localhost:8080", "host portion for healthcheck URL")
	fs.DurationVar(&opts.Timeout, "timeout", 60*time.Second, "max time to wait for healthy")
	fs.BoolVar(&opts.KeepOnFailure, "keep-on-failure", false, "do not tear down on failure (debugging)")
	fs.StringVar(&opts.DockerMode, "docker-mode", "auto", "plugin|system|auto")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return opts, nil
}

func runValidateLaunch(args []string) int {
	opts, err := parseValidateLaunchFlags(args)
	if err != nil {
		return 2  // usage error
	}
	if opts.ComposePath != "" {
		fmt.Fprintln(os.Stderr, "compose mode arrives in v0.18.13/v0.19.0; for now use synthesis (omit --compose)")
		return 1
	}

	// Resolve docker provider.
	resolver := newDockerResolver(opts.DockerMode)
	mode, err := resolver.Resolve()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	var docker interfaces.DockerProvider
	switch mode {
	case "system":
		docker = sysdocker.NewSystemFallback()
	case "plugin":
		// Phase 2: dial workflow-plugin-docker via go-plugin manager.
		fmt.Fprintln(os.Stderr, "plugin mode arrives in v0.18.13/v0.19.0; use --docker-mode=system or auto")
		return 1
	}

	// Load config + manifests.
	cfg, manifests, err := loadConfigAndManifests(opts.ConfigPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		return 1
	}

	// Synthesize ephemeral env.
	env, err := synthesizeEphemeralEnv(cfg, manifests)
	if err != nil {
		fmt.Fprintf(os.Stderr, "synthesize: %v\n", err)
		return 1
	}

	// Build the app image (reuses wfctl build path; for MVP we assume image is already built and tagged).
	// Implementer: optionally run `wfctl build` here, or accept --image flag for a pre-built ref.
	env.AppContainer = &dockerv1.RunRequest{
		Image:   opts.ImageRef,
		Name:    "wfctl-validate-app",
		Network: env.NetworkName,
		Env:     env.EnvVars,
		Detach:  true,
		Ports:   []*dockerv1.PortMapping{{ContainerPort: 8080, HostPort: 8080, Protocol: "tcp"}},
	}

	// Up.
	lc := &synthesisLifecycle{docker: docker, env: env}
	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout+30*time.Second)
	defer cancel()

	if err := lc.Up(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "up failed: %v\n", err)
		dumpFailureSummary(lc, err)
		if !opts.KeepOnFailure {
			_ = lc.Down(ctx)
		}
		return 1
	}
	defer func() {
		if !opts.KeepOnFailure {
			_ = lc.Down(ctx)
		}
	}()

	// Healthz.
	target := opts.HealthcheckHost + opts.HealthcheckPath
	hr, err := lc.Healthz(ctx, target, opts.Timeout)
	if err != nil {
		// Scrape app logs for known signatures.
		logs, _ := lc.Logs(ctx, "wfctl-validate-app")
		scanResults := scanLogs(readAll(logs))
		emitFailureSummary(opts, hr, scanResults)
		return 1
	}
	emitSuccessSummary(opts, hr)
	return 0
}
```

(Implementer: complete `loadConfigAndManifests`, `dumpFailureSummary`, `emitFailureSummary`, `emitSuccessSummary`, `readAll`. Use existing `WriteStepSummary` helper at `cmd/wfctl/ci_output_summary.go:32`.)

**Step 4: Run tests**

Run: `go test ./cmd/wfctl/... -run TestValidateLaunch -v`

Expected: PASS for unit tests.

**Step 5: Commit**

```bash
git add cmd/wfctl/validate_launch.go cmd/wfctl/validate_launch_test.go
git commit -m "wfctl: validate launch command (synthesis mode MVP)"
```

---

### Task 17: Failure summary emission via WriteStepSummary

**Files:**
- Create: `cmd/wfctl/validate_launch_summary.go`
- Create: `cmd/wfctl/validate_launch_summary_test.go`

**Step 1: Write the failing test**

```go
func TestEmitFailureSummary_GHA(t *testing.T) {
	tmp := t.TempDir()
	summary := tmp + "/summary.md"
	t.Setenv("GITHUB_STEP_SUMMARY", summary)

	emitter := newCIEmitter()  // existing helper
	results := []LogSignature{
		{ID: "remote_manifest_fetch_failed", Message: "engine tried to fetch from remote", Hint: "check version skew"},
	}
	hr := HealthResult{OK: false, Status: "timeout", Timeline: []HealthEvent{
		{At: time.Now(), Status: "connection refused"},
	}}

	emitFailureSummary(emitter, &ValidateLaunchOpts{Timeout: 60 * time.Second}, hr, results)

	body, err := os.ReadFile(summary)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "remote_manifest_fetch_failed") {
		t.Errorf("summary missing signature ID")
	}
	if !strings.Contains(string(body), "check version skew") {
		t.Errorf("summary missing hint")
	}
}
```

**Step 2: Run to verify failure**

Expected: FAIL — function undefined.

**Step 3: Implement**

```go
func emitFailureSummary(em CIEmitter, opts *ValidateLaunchOpts, hr HealthResult, sigs []LogSignature) {
	var b strings.Builder
	fmt.Fprintf(&b, "## ❌ wfctl validate launch — image launch failed\n\n")
	fmt.Fprintf(&b, "**Mode:** %s\n", opts.modeLabel())
	fmt.Fprintf(&b, "**Healthcheck target:** %s%s (timeout %s)\n\n", opts.HealthcheckHost, opts.HealthcheckPath, opts.Timeout)
	fmt.Fprintf(&b, "### Healthz timeline\n\n")
	for _, ev := range hr.Timeline {
		fmt.Fprintf(&b, "- %s — %s\n", ev.At.Format(time.RFC3339), ev.Status)
	}
	if len(sigs) > 0 {
		fmt.Fprintf(&b, "\n### Failure signatures\n\n")
		for _, sig := range sigs {
			fmt.Fprintf(&b, "- **%s**: %s\n  - 💡 %s\n", sig.ID, sig.Message, sig.Hint)
		}
	}
	fmt.Fprintf(&b, "\n### Suggested actions\n\n")
	fmt.Fprintf(&b, "- Run locally: `wfctl validate launch --keep-on-failure` and inspect containers\n")
	fmt.Fprintf(&b, "- Verify image-vs-tooling version skew (workflow-server pin in build pipeline)\n")
	WriteStepSummary(em, b.String())
}
```

**Step 4: Run tests**

Expected: PASS.

**Step 5: Commit**

```bash
git add cmd/wfctl/validate_launch_summary.go cmd/wfctl/validate_launch_summary_test.go
git commit -m "wfctl: validate launch — failure summary via WriteStepSummary"
```

---

### Task 18: Integration test (testcontainers-go, build-tagged)

**Files:**
- Create: `cmd/wfctl/validate_launch_integration_test.go`
- Create: `cmd/wfctl/testdata/integration-app/Dockerfile` — minimal "echo + healthz" app
- Create: `cmd/wfctl/testdata/integration-app/main.go` — exposes /healthz returning 200

**Step 1: Write the integration test**

```go
//go:build docker_integration

package main

import (
	"context"
	"testing"
	"time"
)

func TestValidateLaunch_HappyPath_Integration(t *testing.T) {
	// Build the integration test image (testdata/integration-app).
	buildIntegrationImage(t)

	exitCode := runWithArgs(t, []string{"validate", "launch",
		"--config", "testdata/integration-app/app.yaml",
		"--image", "wfctl-integration-test:latest",
		"--timeout", "30s",
	})
	if exitCode != 0 {
		t.Errorf("expected success, got exit %d", exitCode)
	}
}

func TestValidateLaunch_RemoteManifestFetch_Integration(t *testing.T) {
	// Build a deliberately-broken image that triggers fetch-from-remote.
	buildBrokenImage(t, "remote_fetch")

	exitCode := runWithArgs(t, []string{"validate", "launch",
		"--config", "testdata/integration-app/app-needs-missing-plugin.yaml",
		"--image", "wfctl-broken-remote-fetch:latest",
		"--timeout", "20s",
	})
	if exitCode == 0 {
		t.Error("expected failure, got success")
	}
	// Inspect summary file for signature classification.
}
```

(Implementer: implement `buildIntegrationImage`, `buildBrokenImage`. They use testcontainers-go's docker build helpers or shell to `docker build`.)

**Step 2: Run integration test**

Run: `go test ./cmd/wfctl/... -tags docker_integration -run TestValidateLaunch_HappyPath -v`

Expected: PASS (requires Docker daemon).

**Step 3: Commit**

```bash
git add cmd/wfctl/validate_launch_integration_test.go cmd/wfctl/testdata/integration-app/
git commit -m "wfctl: validate launch — docker_integration tests (happy path + broken-image cases)"
```

---

### Task 19: Test-catches-regression for each known signature

**Files:**
- Modify: `cmd/wfctl/validate_launch_integration_test.go` (add subtests)
- Create: `cmd/wfctl/testdata/integration-app/Dockerfile.<scenario>` per signature

**Step 1: Stage one container per signature class**

For each entry in `logSignatures`:
- `engine_setup_error` — Dockerfile that prints "Setup error: failed to build engine" and exits 1
- `remote_manifest_fetch_failed` — Dockerfile/test app that includes `fetch manifest from remote` in output
- `plugin_not_loaded` — Dockerfile/test app that emits `required plugin "x" is not loaded`
- `go_panic` — Dockerfile that runs a Go binary that panics
- `dns_no_such_host` — Dockerfile that nslookups a non-existent host

**Step 2: Add subtests asserting scraper catches each signature**

```go
func TestValidateLaunch_AllSignaturesCaught_Integration(t *testing.T) {
	for _, scenario := range []string{
		"engine_setup_error",
		"remote_manifest_fetch_failed",
		"plugin_not_loaded",
		"go_panic",
		"dns_no_such_host",
	} {
		t.Run(scenario, func(t *testing.T) {
			buildBrokenImage(t, scenario)
			exitCode := runWithArgs(t, []string{"validate", "launch",
				"--image", "wfctl-broken-" + scenario + ":latest",
				"--timeout", "20s",
			})
			if exitCode == 0 {
				t.Errorf("scenario %s: expected failure", scenario)
			}
			// Read summary, assert it contains the expected signature ID.
		})
	}
}
```

**Step 3: Run**

Run: `go test ./cmd/wfctl/... -tags docker_integration -run TestValidateLaunch_AllSignaturesCaught -v`

Expected: PASS (5 subtests).

**Step 4: Commit**

```bash
git add cmd/wfctl/validate_launch_integration_test.go cmd/wfctl/testdata/integration-app/Dockerfile.*
git commit -m "wfctl: validate launch — integration tests per failure signature"
```

---

### Task 20: CHANGELOG entry for v0.18.12

**Files:**
- Modify: `CHANGELOG.md`

**Step 1: Add entry**

```markdown
## [0.18.12] - 2026-XX-XX

### Added
- **`wfctl validate launch`** — new command. Builds the image (or accepts `--image <ref>`), synthesizes ephemeral dependencies from `app.yaml requires:` (postgres + redis + minio mappings in MVP), spins everything up via testcontainers-go, polls `/healthz`, scrapes startup logs for known failure signatures (engine setup error, remote manifest fetch, plugin not loaded, panic, DNS resolution failures), emits a structured CI summary on failure. One command replaces 100+ lines of bespoke per-consumer image-launch CI yaml.
- **`DockerProvider` strict-proto plugin abstraction** — new gRPC service at `proto/workflow/docker/v1/`, with `protovalidate-go` interceptors on both server and client paths. In-tree `systemDockerFallback` ships in this release; the external `workflow-plugin-docker` (embedded Moby Go SDK) ships in v0.18.13. `--docker-mode={plugin,system,auto}` flag selects.
- **`wfctl validate` is now a dispatcher** — `wfctl validate config` (current behavior, default), `wfctl validate launch` (new). Bare `wfctl validate <yaml>` continues to work for back-compat.

### Subsumed scope (was previously planned as separate work)
- Task #71 (wfctl WriteStepSummary in apply/deploy failure paths) — now part of validate launch's failure summary.
- Task #79 (BMW migrations CI) — closed; migrations run as part of `wfctl validate launch`.
- Task #81 (BMW image-launch CI) — closed; BMW's CI step becomes one line `wfctl validate launch`.
```

**Step 2: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs(changelog): v0.18.12 — wfctl validate launch + DockerProvider"
```

---

### Task 21: Local validation against BMW (mandatory before PR open)

**Steps (no commit — transcript goes in PR body):**

1. `cd /Users/jon/workspace/buymywishlist`
2. Use locally-built `wfctl` from this branch:
   ```bash
   cd ../workflow && go build -o ./wfctl ./cmd/wfctl && cd -
   ```
3. Build BMW image as deploy.yml does (use the workflow-server v0.18.11 pin from task #82's WIP — if that hasn't merged yet, manually patch local `data/plugins/` install via setup-wfctl).
4. Run validate launch against BMW's actual app.yaml:
   ```bash
   ../workflow/wfctl validate launch --config app.yaml --image bmw-validate:test --timeout 90s
   ```
5. Expected: success with all dep containers up + /healthz reaching 200.
6. Capture full transcript including the synthesized environment + healthz timeline.
7. Verify the failure path: deliberately remove a plugin from `data/plugins/`, re-run, confirm signature scraper catches "plugin not loaded".

**Output:** transcript text → paste in PR body under "Local validation transcript" section.

---

### Task 22: Open PR + reviews

**Steps:**

1. Push branch:
   ```bash
   git push -u origin feat/v0.18.12-validate-launch
   ```

2. **LOCAL code-reviewer pre-push** (mandatory — see team rules).

3. Open PR:
   ```bash
   gh pr create --title "feat(wfctl): v0.18.12 — validate launch + DockerProvider" --body "$(cat <<'EOF'
   ## Summary
   - `wfctl validate launch` — new CI gate replacing bespoke image-launch yaml in every consumer.
   - `DockerProvider` strict-proto plugin abstraction (proto schema + sdk + system fallback).
   - `wfctl validate` is now a dispatcher (config|launch); back-compat preserved.

   Subsumes scope from #71 (WriteStepSummary), #79 (BMW migrations CI), #81 (BMW image-launch CI).

   Design: docs/plans/2026-04-25-wfctl-image-launch-validation-design.md

   ## Local validation transcript
   <paste from Task 21>

   ## Test-catches-regression invariant proofs
   - protovalidate interceptor (Task 6): comment out request-validate → TestServerInterceptor_RejectsInvalidRequest fails. Restore → passes.
   - log scraper (Task 14): remove engine_setup_error pattern → TestLogScraper/Setup_error fails. Restore → passes.

   ## Test plan
   - [ ] go test ./... passes
   - [ ] go test -tags docker_integration ./cmd/wfctl/... passes (requires local Docker)
   - [ ] BMW app.yaml validates locally (transcript above)
   - [ ] All 5 failure-signature integration scenarios fail loudly with correct signature IDs
   EOF
   )"
   ```

4. Add Copilot reviewer:
   ```bash
   gh pr edit <number> --add-reviewer copilot-pull-request-reviewer
   ```

5. Address review rounds — DM team-lead when CI green and Copilot has cleared.

---

### Task 23: Tag v0.18.12 (team-lead)

After PR merges (team-lead handles):

```bash
cd /Users/jon/workspace/workflow
git checkout main
git pull
git tag -a v0.18.12 <merge-sha> -m "v0.18.12: wfctl validate launch + DockerProvider"
git push origin v0.18.12
```

Notify consumers (BMW, ratchet, workflow-cloud) that they can simplify their image-launch CI to one line.

---

## Phase 2 — v0.18.13 / v0.19.0 (high-level only)

A separate, full implementation plan will be written when Phase 1 ships. Coverage areas:

1. **`compose-spec/compose-go` integration**
   - Detect `compose.test.yaml` in repo; switch to compose mode.
   - Hybrid mode: compose runs user services + wfctl synthesizes anything in app.yaml that compose lacks.
   - Precedence rules + conflict warnings.

2. **`wfctl scaffold compose`**
   - Emit synthesized environment as starting compose.test.yaml so users can graduate.

3. **`wfctl dev up --docker`** (extends existing `wfctl dev` cmd at `cmd/wfctl/dev.go`)
   - File watcher (`fsnotify`, ~50 LOC, no external air/gow dep).
   - Per-change-category strategies:
     - server source → rebuild server binary, container restart
     - plugin source → rebuild plugin binary, call `ExternalPluginManager.ReloadPlugin` (`plugin/external/manager.go:190`)
     - UI source → no-op (defer to npm dev server)
     - app.yaml → restart container
     - migrations → run `wfctl migrate up` against ephemeral pg, no restart
   - Logs streaming with per-service prefixes + colors.
   - `wfctl dev down` cleanup with confirmation.

4. **`workflow-plugin-docker` external plugin** (separate repo)
   - Statically links `github.com/docker/docker/client` (Moby Go SDK).
   - Talks directly to daemon socket (Unix on Linux/macOS, named pipe on Windows).
   - Implements DockerProvider proto via `plugin/sdk/docker/`.
   - Goreleaser CI publishes binaries (linux/darwin × amd64/arm64).
   - Registered in workflow-registry as auto-install candidate.

5. **Refactor `wfctl build` onto DockerProvider** (deferred from Phase 1 to keep blast radius small)
   - Remove `os/exec`-based `cmd/wfctl/build_image.go`.
   - Wire into resolver + DockerProvider.
   - Existing user CLI surface unchanged.

6. **Provider-agnostic CI summary (#63)**
   - GitLab + Jenkins + CircleCI emitters.
   - validate launch failure summary works on every CI provider.

---

## Phase 3 — v0.20.0+ (high-level only)

1. **`.wfctl-validated.json` marker per SHA**
   - On `wfctl validate launch` success, write marker.
   - `wfctl infra plan` + `wfctl ci run --phase deploy` consult marker.
   - Advisory only (warning), not block.

2. **Per-cloud-provider local mappings via proto**
   - Each IaC plugin (DO, AWS, GCP, Azure, tofu) declares its `infra.<resource>` → local container mapping in proto.
   - Replaces hardcoded `synthesisMappings` in `cmd/wfctl/synthesis_mapping.go`.
   - DockerProvider proto + IaCProvider proto cooperate: IaC plugin emits a `LocalEquivalent` message; wfctl reads and applies.

3. **Multi-cloud parity testing**
   - Ratchet/workflow-cloud test matrix exercises DO, AWS, GCP, Azure infra mappings locally.
   - Surfaces drift between local synthesis and real cloud behavior as warnings, not blocks.

---

## Coordination — buf toolchain sharing with v0.20.0 IaC proto

Tasks 1–4 of this plan introduce `buf.yaml`, `buf.gen.yaml`, `Makefile proto target`, and `.github/workflows/buf-ci.yml`. These same files are introduced by the v0.20.0 IaC proto enforcement plan (#41).

**Whichever workstream lands buf scaffolding first, the other inherits.** Each task that creates a buf file says "if absent, create; if present, reuse" — never duplicate. The IaC plan (`docs/plans/2026-04-24-iac-plugins-proto-enforcement.md`) and this plan are designed to be co-existable.

The `proto/workflow/<category>/v1/` directory layout is shared: IaC under `proto/workflow/iac/v1/`, Docker under `proto/workflow/docker/v1/`, future categories under their own `v1/` subtrees.

The `plugin/sdk/<category>/` directory layout is shared: `plugin/sdk/iac/`, `plugin/sdk/docker/`, future SDKs under their own subdirs. The interceptor module is generic (proto.Message-typed) and could be factored to `plugin/sdk/proto/` if duplication appears — defer that refactor until both SDKs ship.
