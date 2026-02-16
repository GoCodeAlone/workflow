# Plan: CI/CD Pipeline Platform — Build, Scan, Deploy, Cloud Providers

## Context

The Workflow engine currently handles orchestration of HTTP, messaging, state machine, and scheduler workflows. The goal is to extend it into a CI/CD-capable platform where workflow definitions can include build/compile steps, security scanning, multi-environment deployment, Docker container management, and cloud provider integrations — all driven by YAML config, same as existing workflows.

This builds on the existing plugin architecture (NativePlugin, NativeRegistry) and pipeline step system (PipelineStep interface, StepFactory pattern) already in the codebase.

---

## Existing Infrastructure to Reuse

| Component | Location | Reuse |
|-----------|----------|-------|
| PipelineStep interface | `module/pipeline_step.go` | All new steps implement this |
| StepFactory pattern | `module/pipeline_step.go` | Registration via `RegisterStepFactory()` |
| 16 existing step types | `module/pipeline_step_*.go` | Pattern for new steps |
| NativePlugin interface | `plugin/native.go` | Cloud providers implement this |
| NativeRegistry | `plugin/native_registry.go` | Register cloud provider plugins |
| DeploymentStrategy | `deploy/strategy.go` | Rolling/BlueGreen/Canary — wire to real execution |
| StrategyRegistry | `deploy/registry.go` | Already has 3 strategies registered |
| Connector framework | `connector/interface.go` | EventSource/EventSink for CDC/Redis/SQS |
| Environment UI (mockup) | `ui/src/components/environments/Environments.tsx` | Replace static data with real API |
| Marketplace UI (mockup) | `ui/src/components/marketplace/Marketplace.tsx` | Connect to plugin registry |
| Plugin registry | `plugin/registry.go` | Extend with remote registry |
| Scale utilities | `scale/` | DistributedLock, WorkerPool |
| Admin server wiring | `cmd/server/main.go` | Central registration point |

---

## Phase 1: Foundation — Artifact Store + Docker Sandbox + Core Build Steps

**Goal**: Enable pipeline steps to produce/consume artifacts and execute commands in Docker containers.

### 1a. Artifact Store (`artifact/`)

New package providing scoped storage for build outputs.

**`artifact/store.go`**:
```go
type Store interface {
    Put(ctx context.Context, executionID, key string, reader io.Reader) error
    Get(ctx context.Context, executionID, key string) (io.ReadCloser, error)
    List(ctx context.Context, executionID string) ([]Artifact, error)
    Delete(ctx context.Context, executionID, key string) error
}

type Artifact struct {
    Key       string    `json:"key"`
    Size      int64     `json:"size"`
    CreatedAt time.Time `json:"created_at"`
    Checksum  string    `json:"checksum"`  // SHA256
}
```

**`artifact/local.go`** — Local filesystem backend (stores under `data/artifacts/{executionID}/`).
**`artifact/s3.go`** — S3-compatible backend (AWS S3, MinIO, DigitalOcean Spaces).

### 1b. Docker Sandbox (`sandbox/`)

Wraps Docker Engine Go SDK (`github.com/docker/docker/client`) to execute commands inside containers.

**`sandbox/docker.go`**:
```go
type DockerSandbox struct {
    client *client.Client
    config SandboxConfig
}

type SandboxConfig struct {
    Image        string            `yaml:"image"`
    WorkDir      string            `yaml:"work_dir"`
    Env          map[string]string `yaml:"env"`
    Mounts       []Mount           `yaml:"mounts"`
    MemoryLimit  int64             `yaml:"memory_limit"`
    CPULimit     float64           `yaml:"cpu_limit"`
    Timeout      time.Duration     `yaml:"timeout"`
    NetworkMode  string            `yaml:"network_mode"`
}

func (s *DockerSandbox) Exec(ctx context.Context, cmd []string) (*ExecResult, error)
func (s *DockerSandbox) CopyIn(ctx context.Context, srcPath, destPath string) error
func (s *DockerSandbox) CopyOut(ctx context.Context, srcPath string) (io.ReadCloser, error)
```

### 1c. Core Build Steps

**`module/pipeline_step_shell_exec.go`** — `step.shell_exec`:
```yaml
- name: build-ui
  type: step.shell_exec
  config:
    image: node:20-alpine
    commands:
      - npm ci
      - npm run build
    work_dir: /workspace
    timeout: 300s
    env:
      NODE_ENV: production
    artifacts_out:
      - key: ui-dist
        path: /workspace/dist
```

**`module/pipeline_step_artifact_pull.go`** — `step.artifact_pull`:
```yaml
- name: fetch-assets
  type: step.artifact_pull
  config:
    source: previous_execution
    execution_id: "{{.PreviousExecutionID}}"
    key: ui-dist
    dest: /workspace/assets
```

**`module/pipeline_step_artifact_push.go`** — `step.artifact_push`:
```yaml
- name: store-binary
  type: step.artifact_push
  config:
    source_path: /workspace/output/server
    key: server-binary
    dest: artifact_store
```

### Files to create (Phase 1)
| File | Purpose |
|------|---------|
| `artifact/store.go` | Artifact Store interface + types |
| `artifact/local.go` | Local filesystem backend |
| `artifact/s3.go` | S3-compatible backend |
| `artifact/store_test.go` | Tests |
| `sandbox/docker.go` | Docker sandbox execution |
| `sandbox/docker_test.go` | Tests |
| `module/pipeline_step_shell_exec.go` | step.shell_exec |
| `module/pipeline_step_artifact_pull.go` | step.artifact_pull |
| `module/pipeline_step_artifact_push.go` | step.artifact_push |

### Files to modify (Phase 1)
| File | Change |
|------|--------|
| `engine.go` | Register 3 new step factories |
| `schema/module_schema.go` | Add schemas for 3 new steps |
| `go.mod` | Add `github.com/docker/docker` dependency |

---

## Phase 2: Docker Integration — Build, Push, Run Steps

**Goal**: Full Docker lifecycle management as pipeline steps.

**`module/pipeline_step_docker_build.go`** — `step.docker_build`:
```yaml
- name: build-image
  type: step.docker_build
  config:
    context: /workspace
    dockerfile: Dockerfile
    tags:
      - "myapp:{{.Version}}"
      - "myapp:latest"
    build_args:
      GO_VERSION: "1.25"
    cache_from:
      - "myapp:latest"
```

**`module/pipeline_step_docker_push.go`** — `step.docker_push`:
```yaml
- name: push-image
  type: step.docker_push
  config:
    image: "myapp:{{.Version}}"
    registry: "123456789.dkr.ecr.us-east-1.amazonaws.com"
    auth_provider: aws_ecr
```

**`module/pipeline_step_docker_run.go`** — `step.docker_run`:
```yaml
- name: run-migrations
  type: step.docker_run
  config:
    image: "myapp:{{.Version}}"
    command: ["./migrate", "--up"]
    env:
      DATABASE_URL: "{{.Env.DATABASE_URL}}"
    wait_for_exit: true
    timeout: 120s
```

### Files to create (Phase 2)
| File | Purpose |
|------|---------|
| `module/pipeline_step_docker_build.go` | step.docker_build |
| `module/pipeline_step_docker_push.go` | step.docker_push |
| `module/pipeline_step_docker_run.go` | step.docker_run |

### Files to modify (Phase 2)
| File | Change |
|------|--------|
| `engine.go` | Register 3 new step factories |
| `schema/module_schema.go` | Add schemas for 3 new steps |

---

## Phase 3: Security Scanning Steps

**Goal**: Integrate open-source scanners (Semgrep, Trivy, Grype) as pipeline steps that run inside Docker.

**`module/pipeline_step_scan_sast.go`** — `step.scan_sast`:
```yaml
- name: sast-scan
  type: step.scan_sast
  config:
    scanner: semgrep
    image: "semgrep/semgrep:latest"
    source_path: /workspace
    rules:
      - "p/owasp-top-ten"
      - "p/security-audit"
    fail_on_severity: error
    output_format: sarif
    artifacts_out:
      key: sast-report
```

**`module/pipeline_step_scan_container.go`** — `step.scan_container`:
```yaml
- name: container-scan
  type: step.scan_container
  config:
    scanner: trivy
    image: "myapp:{{.Version}}"
    severity_threshold: HIGH
    ignore_unfixed: true
    output_format: sarif
```

**`module/pipeline_step_scan_deps.go`** — `step.scan_deps`:
```yaml
- name: dep-scan
  type: step.scan_deps
  config:
    scanner: grype
    source_path: /workspace
    fail_on_severity: high
    output_format: sarif
```

**`module/scan_result.go`** — Common scan result types (SARIF-compatible):
```go
type ScanResult struct {
    Scanner     string        `json:"scanner"`
    Findings    []Finding     `json:"findings"`
    Summary     ScanSummary   `json:"summary"`
    PassedGate  bool          `json:"passed_gate"`
}
type Finding struct {
    RuleID      string `json:"rule_id"`
    Severity    string `json:"severity"`
    Message     string `json:"message"`
    Location    string `json:"location"`
    Line        int    `json:"line,omitempty"`
}
type ScanSummary struct {
    Critical int `json:"critical"`
    High     int `json:"high"`
    Medium   int `json:"medium"`
    Low      int `json:"low"`
    Info     int `json:"info"`
}
```

### Files to create (Phase 3)
| File | Purpose |
|------|---------|
| `module/pipeline_step_scan_sast.go` | step.scan_sast |
| `module/pipeline_step_scan_container.go` | step.scan_container |
| `module/pipeline_step_scan_deps.go` | step.scan_deps |
| `module/scan_result.go` | Common scan result types |

### Files to modify (Phase 3)
| File | Change |
|------|--------|
| `engine.go` | Register 3 new step factories |
| `schema/module_schema.go` | Add schemas for 3 new steps |

---

## Phase 4: Environment Backend — Types, Store, API, UI

**Goal**: Replace the static Environments UI mockup with a persistent backend.

### 4a. Backend

**`environment/types.go`**:
```go
type Environment struct {
    ID           string            `json:"id" db:"id"`
    WorkflowID   string            `json:"workflow_id" db:"workflow_id"`
    Name         string            `json:"name" db:"name"`
    Provider     string            `json:"provider" db:"provider"`
    Region       string            `json:"region" db:"region"`
    Config       map[string]any    `json:"config"`
    Secrets      map[string]string `json:"secrets,omitempty"`
    Status       string            `json:"status" db:"status"`
    CreatedAt    time.Time         `json:"created_at" db:"created_at"`
    UpdatedAt    time.Time         `json:"updated_at" db:"updated_at"`
}
```

**`environment/store.go`** — SQLite-backed CRUD:
```go
type Store interface {
    Create(ctx context.Context, env *Environment) error
    Get(ctx context.Context, id string) (*Environment, error)
    List(ctx context.Context, filter Filter) ([]Environment, error)
    Update(ctx context.Context, env *Environment) error
    Delete(ctx context.Context, id string) error
    TestConnection(ctx context.Context, id string) (*ConnectionTestResult, error)
}
```

**`environment/handler.go`** — HTTP API:
- `GET /api/v1/admin/environments` — List
- `GET /api/v1/admin/environments/{id}` — Get
- `POST /api/v1/admin/environments` — Create
- `PUT /api/v1/admin/environments/{id}` — Update
- `DELETE /api/v1/admin/environments/{id}` — Delete
- `POST /api/v1/admin/environments/{id}/test` — Test connectivity

### 4b. Frontend

Modify `ui/src/components/environments/Environments.tsx` to:
- Replace static mockup data with API calls
- Add Zustand store `ui/src/store/environmentStore.ts`
- Add "Test Connection" button per environment
- Add create/edit forms with provider-specific config fields

### Files to create (Phase 4)
| File | Purpose |
|------|---------|
| `environment/types.go` | Environment types |
| `environment/store.go` | Store interface + SQLite implementation |
| `environment/handler.go` | HTTP API handlers |
| `environment/handler_test.go` | Tests |
| `ui/src/store/environmentStore.ts` | Zustand store |

### Files to modify (Phase 4)
| File | Change |
|------|--------|
| `ui/src/components/environments/Environments.tsx` | Wire to real API |
| `cmd/server/main.go` | Register environment routes |

---

## Phase 5: Deployment Steps + Strategy Wiring

**Goal**: Connect the existing DeploymentStrategy framework to actual execution.

**`module/pipeline_step_deploy.go`** — `step.deploy`:
```yaml
- name: deploy-production
  type: step.deploy
  config:
    environment: production
    strategy: rolling
    image: "myapp:{{.Version}}"
    provider: aws_ecs
    health_check:
      path: /health
      interval: 10s
      timeout: 5s
      healthy_threshold: 3
    rollback_on_failure: true
    rolling:
      max_surge: 25%
      max_unavailable: 0
```

**`module/pipeline_step_gate.go`** — `step.gate`:
```yaml
- name: production-approval
  type: step.gate
  config:
    type: manual
    approvers:
      - admin@example.com
    timeout: 24h
    auto_approve_conditions:
      - "scan.passed == true"
      - "tests.passed == true"
```

**`deploy/executor.go`** — Bridges DeploymentStrategy to cloud providers:
```go
type Executor struct {
    strategies *StrategyRegistry
    providers  map[string]CloudProvider
}
func (e *Executor) Deploy(ctx context.Context, req DeployRequest) (*DeployResult, error)
```

### Files to create (Phase 5)
| File | Purpose |
|------|---------|
| `module/pipeline_step_deploy.go` | step.deploy |
| `module/pipeline_step_gate.go` | step.gate |
| `deploy/executor.go` | Strategy-to-provider bridge |

### Files to modify (Phase 5)
| File | Change |
|------|--------|
| `engine.go` | Register 2 new step factories |
| `schema/module_schema.go` | Add schemas for 2 new steps |
| `deploy/strategy.go` | Add Execute method to strategies |

---

## Phase 6: Cloud Provider Plugins

**Goal**: Each cloud provider is a NativePlugin with deployment, registry, and monitoring capabilities.

### CloudProvider Interface

**`provider/interface.go`**:
```go
type CloudProvider interface {
    plugin.NativePlugin

    Deploy(ctx context.Context, req DeployRequest) (*DeployResult, error)
    GetDeploymentStatus(ctx context.Context, deployID string) (*DeployStatus, error)
    Rollback(ctx context.Context, deployID string) error

    PushImage(ctx context.Context, image string, auth RegistryAuth) error
    PullImage(ctx context.Context, image string, auth RegistryAuth) error
    ListImages(ctx context.Context, repo string) ([]ImageTag, error)

    TestConnection(ctx context.Context, config map[string]any) (*ConnectionResult, error)
    GetMetrics(ctx context.Context, deployID string, window time.Duration) (*Metrics, error)
}
```

### 4 Provider Plugins

**`provider/aws/plugin.go`** — AWS: EC2, ECS, EKS, ECR, CloudWatch
**`provider/gcp/plugin.go`** — GCP: GKE, Cloud Run, GCR, Cloud Monitoring
**`provider/azure/plugin.go`** — Azure: AKS, ACI, ACR, Azure Monitor
**`provider/digitalocean/plugin.go`** — DO: DOKS, App Platform, Container Registry

### Files to create (Phase 6)
| File | Purpose |
|------|---------|
| `provider/interface.go` | CloudProvider interface |
| `provider/types.go` | Shared types |
| `provider/aws/plugin.go` | AWS NativePlugin |
| `provider/aws/deploy.go` | AWS deployment |
| `provider/aws/registry.go` | ECR integration |
| `provider/gcp/plugin.go` | GCP NativePlugin |
| `provider/gcp/deploy.go` | GKE, Cloud Run |
| `provider/gcp/registry.go` | GCR/Artifact Registry |
| `provider/azure/plugin.go` | Azure NativePlugin |
| `provider/azure/deploy.go` | AKS, ACI |
| `provider/azure/registry.go` | ACR |
| `provider/digitalocean/plugin.go` | DO NativePlugin |
| `provider/digitalocean/deploy.go` | DOKS, App Platform |
| `provider/digitalocean/registry.go` | DO Container Registry |

### Files to modify (Phase 6)
| File | Change |
|------|--------|
| `cmd/server/main.go` | Register all 4 provider plugins |
| `go.mod` | Add cloud provider SDK dependencies |

---

## Phase 7: Remote Plugin Registry

**Goal**: Extend PluginRegistry to support discovering and installing plugins from a remote HTTP registry.

**`plugin/remote_registry.go`**:
```go
type RemoteRegistry struct {
    baseURL    string
    httpClient *http.Client
    cache      map[string]*PluginManifest
    cacheTTL   time.Duration
}
```

**`plugin/composite_registry.go`** — Combines local + remote.

**Marketplace UI wiring**: Wire `Marketplace.tsx` to real API.

### Files to create (Phase 7)
| File | Purpose |
|------|---------|
| `plugin/remote_registry.go` | Remote HTTP registry client |
| `plugin/composite_registry.go` | Local + remote composite |
| `plugin/registry_handler.go` | HTTP API for search/install/uninstall |

### Files to modify (Phase 7)
| File | Change |
|------|--------|
| `ui/src/components/marketplace/Marketplace.tsx` | Wire to real API |
| `cmd/server/main.go` | Register composite registry |

---

## 11 New Pipeline Step Types Summary

| Step Type | Purpose | Phase |
|-----------|---------|-------|
| `step.shell_exec` | Execute commands in Docker sandbox | 1 |
| `step.artifact_pull` | Pull artifacts into sandbox | 1 |
| `step.artifact_push` | Push artifacts from sandbox | 1 |
| `step.docker_build` | Build Docker images | 2 |
| `step.docker_push` | Push images to registries | 2 |
| `step.docker_run` | Run containers | 2 |
| `step.scan_sast` | SAST scanning (Semgrep) | 3 |
| `step.scan_container` | Container vulnerability scanning (Trivy) | 3 |
| `step.scan_deps` | Dependency scanning (Grype) | 3 |
| `step.deploy` | Deploy using strategy + provider | 5 |
| `step.gate` | Approval gate (manual/automated) | 5 |

---

## Agent Work Distribution (4 Waves)

| Wave | Agents | Phase | Files |
|------|--------|-------|-------|
| **Wave 1** (parallel) | Agent 1 | Phase 1a: Artifact Store | `artifact/*` |
| | Agent 2 | Phase 1b: Docker Sandbox | `sandbox/*` |
| | Agent 3 | Phase 3: Scan Result Types + Step shells | `module/scan_result.go`, scan step files |
| | Agent 4 | Phase 4a: Environment Backend | `environment/*` |
| **Wave 2** (parallel, after Wave 1) | Agent 5 | Phase 1c + 2: Build Steps + Docker Steps | `module/pipeline_step_shell_exec.go`, `module/pipeline_step_artifact_*.go`, `module/pipeline_step_docker_*.go` |
| | Agent 6 | Phase 3: Wire Scan Steps (uses sandbox) | `module/pipeline_step_scan_*.go` |
| | Agent 7 | Phase 4b: Environment UI | `ui/src/components/environments/*`, `ui/src/store/environmentStore.ts` |
| **Wave 3** (parallel, after Wave 2) | Agent 8 | Phase 5: Deploy Steps + Executor | `module/pipeline_step_deploy.go`, `module/pipeline_step_gate.go`, `deploy/executor.go` |
| | Agent 9 | Phase 6: Cloud Providers (AWS + GCP) | `provider/interface.go`, `provider/aws/*`, `provider/gcp/*` |
| | Agent 10 | Phase 6: Cloud Providers (Azure + DO) | `provider/azure/*`, `provider/digitalocean/*` |
| **Wave 4** (after Wave 3) | Agent 11 | Phase 7: Remote Registry + Marketplace UI | `plugin/remote_registry.go`, `plugin/composite_registry.go`, marketplace UI |
| | Agent 12 | Wiring + Registration | `engine.go`, `schema/module_schema.go`, `cmd/server/main.go`, `go.mod` |

---

## Verification Checklist

1. `cd workflow && go build ./...` — all code compiles
2. `go test ./artifact/... ./sandbox/... ./environment/... ./provider/... ./module/... ./deploy/...`
3. All 11 new steps appear in `GET /api/v1/module-schemas`
4. Artifact store: Write/read/list/delete artifacts via API
5. Docker sandbox: `step.shell_exec` runs `echo hello` in alpine
6. Docker build: `step.docker_build` builds a Dockerfile
7. Security scan: `step.scan_sast` runs Semgrep, produces SARIF
8. Environment API: CRUD via API, test connection
9. Environment UI: Loads from API, create/edit/delete works
10. Deploy step: `step.deploy` with rolling strategy
11. Cloud providers: Each registers, appears in `GET /api/v1/admin/plugins`
12. Remote registry: Search remote, install plugin to local
13. Marketplace UI: Shows installed + available from real API
14. Full pipeline: Run example CI/CD config end-to-end
15. UI build: `cd ui && npm run build` — no TS errors
16. Existing tests: `go test ./...` — all pass
