# Workflow IaC Strategy — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build cloud-agnostic IaC into the workflow engine with 4 provider plugins (AWS, GCP, Azure, DO), OpenTofu adapter, CI generator, and deployment steps.

**Architecture:** The engine already has a newer `platform.*` system (`platform.Provider`, `ResourceDriver`, `platform.StateStore`) and an older `module.PlatformProvider` system. We consolidate on the newer system, extract interfaces to `workflow/interfaces/`, extract providers to standalone gRPC plugins, and add an abstract composition layer on top. The existing AWS provider (behind `//go:build aws` tag) and credential resolvers for all 4 clouds provide a strong starting foundation.

**Tech Stack:** Go 1.26, aws-sdk-go-v2 v1.41+, cloud.google.com/go, azure-sdk-for-go Track 2 (azcore v1.21+), godo v1.178+, hclwrite (hcl/v2 v2.24+), opentofu v1.11+

**Design Doc:** `docs/plans/2026-03-21-iac-strategy-design.md`

---

## Phase 1: Core Interfaces + Engine Consolidation

### Task 1: Extract IaC Interfaces to `workflow/interfaces/`

**Files:**
- Create: `interfaces/iac_provider.go`
- Create: `interfaces/iac_resource_driver.go`
- Create: `interfaces/iac_state.go`
- Create: `interfaces/iac_sizing.go`
- Modify: `platform/provider.go` — add backward-compat type aliases
- Modify: `platform/resource_driver.go` — add backward-compat type aliases
- Modify: `platform/state_store.go` — add backward-compat type aliases
- Test: `interfaces/iac_test.go`

**Step 1: Write interface compliance tests**

```go
// interfaces/iac_test.go
package interfaces_test

import (
    "testing"
    "github.com/GoCodeAlone/workflow/interfaces"
)

// Verify interface shapes compile — these are compile-time checks
var _ interfaces.IaCProvider = (*mockProvider)(nil)
var _ interfaces.ResourceDriver = (*mockDriver)(nil)
var _ interfaces.IaCStateStore = (*mockState)(nil)

type mockProvider struct{}
// ... implement all methods returning zero values

func TestSizeConstants(t *testing.T) {
    sizes := []interfaces.Size{
        interfaces.SizeXS, interfaces.SizeS, interfaces.SizeM,
        interfaces.SizeL, interfaces.SizeXL,
    }
    if len(sizes) != 5 {
        t.Fatal("expected 5 size tiers")
    }
}

func TestResourceSpecDependsOn(t *testing.T) {
    spec := interfaces.ResourceSpec{
        Name: "db", Type: "infra.database",
        DependsOn: []string{"network"},
    }
    if len(spec.DependsOn) != 1 {
        t.Fatal("expected 1 dependency")
    }
}
```

**Step 2: Run test to verify it fails**

Run: `cd /Users/jon/workspace/workflow && go test ./interfaces/ -run TestSize -v`
Expected: FAIL — types don't exist yet

**Step 3: Create the interface files**

`interfaces/iac_provider.go`:
```go
package interfaces

import "context"

// IaCProvider is the main interface that cloud provider plugins implement.
// Each provider (AWS, GCP, Azure, DO) implements this as a gRPC plugin.
type IaCProvider interface {
    Name() string
    Version() string
    Initialize(ctx context.Context, config map[string]any) error

    // Capabilities returns what resource types this provider supports
    Capabilities() []CapabilityDeclaration

    // Lifecycle
    Plan(ctx context.Context, desired []ResourceSpec, current []ResourceState) (*Plan, error)
    Apply(ctx context.Context, plan *Plan) (*ApplyResult, error)
    Destroy(ctx context.Context, resources []ResourceRef) (*DestroyResult, error)

    // Observability
    Status(ctx context.Context, resources []ResourceRef) ([]ResourceStatus, error)
    DetectDrift(ctx context.Context, resources []ResourceRef) ([]DriftResult, error)

    // Migration
    Import(ctx context.Context, cloudID string, resourceType string) (*ResourceState, error)

    // Sizing
    ResolveSizing(resourceType string, size Size, hints *ResourceHints) (*ProviderSizing, error)

    // Resource drivers for fine-grained CRUD
    ResourceDriver(resourceType string) (ResourceDriver, error)

    Close() error
}

type Size string

const (
    SizeXS Size = "xs"
    SizeS  Size = "s"
    SizeM  Size = "m"
    SizeL  Size = "l"
    SizeXL Size = "xl"
)

type ResourceHints struct {
    CPU     string `json:"cpu,omitempty" yaml:"cpu,omitempty"`
    Memory  string `json:"memory,omitempty" yaml:"memory,omitempty"`
    Storage string `json:"storage,omitempty" yaml:"storage,omitempty"`
}

type ProviderSizing struct {
    InstanceType string         `json:"instance_type"`
    Specs        map[string]any `json:"specs"`
}

type CapabilityDeclaration struct {
    ResourceType string   `json:"resource_type"` // infra.database, infra.vpc, etc.
    Tier         int      `json:"tier"`          // 1=infra, 2=shared, 3=app
    Operations   []string `json:"operations"`    // create, read, update, delete, scale
}

type ResourceSpec struct {
    Name      string         `json:"name"`
    Type      string         `json:"type"`
    Config    map[string]any `json:"config"`
    Size      Size           `json:"size,omitempty"`
    Hints     *ResourceHints `json:"hints,omitempty"`
    DependsOn []string       `json:"depends_on,omitempty"`
}

type ResourceRef struct {
    Name       string `json:"name"`
    Type       string `json:"type"`
    ProviderID string `json:"provider_id,omitempty"`
}

type ResourceStatus struct {
    Name       string         `json:"name"`
    Type       string         `json:"type"`
    ProviderID string         `json:"provider_id"`
    Status     string         `json:"status"` // running, stopped, degraded, unknown
    Outputs    map[string]any `json:"outputs"`
}

type DriftResult struct {
    Name     string         `json:"name"`
    Type     string         `json:"type"`
    Drifted  bool           `json:"drifted"`
    Expected map[string]any `json:"expected"`
    Actual   map[string]any `json:"actual"`
    Fields   []string       `json:"fields"` // which fields drifted
}
```

`interfaces/iac_resource_driver.go`:
```go
package interfaces

import "context"

// ResourceDriver handles CRUD for a single resource type within a provider.
type ResourceDriver interface {
    Create(ctx context.Context, spec ResourceSpec) (*ResourceOutput, error)
    Read(ctx context.Context, ref ResourceRef) (*ResourceOutput, error)
    Update(ctx context.Context, ref ResourceRef, spec ResourceSpec) (*ResourceOutput, error)
    Delete(ctx context.Context, ref ResourceRef) error
    Diff(ctx context.Context, desired ResourceSpec, current *ResourceOutput) (*DiffResult, error)
    HealthCheck(ctx context.Context, ref ResourceRef) (*HealthResult, error)
    Scale(ctx context.Context, ref ResourceRef, replicas int) (*ResourceOutput, error)
}

type ResourceOutput struct {
    Name       string         `json:"name"`
    Type       string         `json:"type"`
    ProviderID string         `json:"provider_id"`
    Outputs    map[string]any `json:"outputs"` // IPs, endpoints, connection strings
    Status     string         `json:"status"`
}

type DiffResult struct {
    NeedsUpdate  bool           `json:"needs_update"`
    NeedsReplace bool           `json:"needs_replace"`
    Changes      []FieldChange  `json:"changes"`
}

type FieldChange struct {
    Path     string `json:"path"`
    Old      any    `json:"old"`
    New      any    `json:"new"`
    ForceNew bool   `json:"force_new"` // change requires resource replacement
}

type HealthResult struct {
    Healthy bool   `json:"healthy"`
    Message string `json:"message,omitempty"`
}
```

`interfaces/iac_state.go`:
```go
package interfaces

import "time"

// IaCStateStore provides persistent state tracking for managed resources.
type IaCStateStore interface {
    SaveResource(state ResourceState) error
    GetResource(name string) (*ResourceState, error)
    ListResources() ([]ResourceState, error)
    DeleteResource(name string) error

    SavePlan(plan Plan) error
    GetPlan(id string) (*Plan, error)

    Lock(resource string) error
    Unlock(resource string) error

    Close() error
}

type ResourceState struct {
    ID             string         `json:"id"`
    Name           string         `json:"name"`
    Type           string         `json:"type"`
    Provider       string         `json:"provider"`
    ProviderID     string         `json:"provider_id"`
    ConfigHash     string         `json:"config_hash"`
    AppliedConfig  map[string]any `json:"applied_config"`
    Outputs        map[string]any `json:"outputs"`
    Dependencies   []string       `json:"dependencies"`
    CreatedAt      time.Time      `json:"created_at"`
    UpdatedAt      time.Time      `json:"updated_at"`
    LastDriftCheck time.Time      `json:"last_drift_check,omitempty"`
}

type Plan struct {
    ID        string       `json:"id"`
    Actions   []PlanAction `json:"actions"`
    CreatedAt time.Time    `json:"created_at"`
}

type PlanAction struct {
    Action   string       `json:"action"` // create, update, replace, delete
    Resource ResourceSpec `json:"resource"`
    Current  *ResourceState `json:"current,omitempty"`
    Changes  []FieldChange  `json:"changes,omitempty"`
}

type ApplyResult struct {
    PlanID    string           `json:"plan_id"`
    Resources []ResourceOutput `json:"resources"`
    Errors    []ActionError    `json:"errors,omitempty"`
}

type DestroyResult struct {
    Destroyed []string      `json:"destroyed"`
    Errors    []ActionError `json:"errors,omitempty"`
}

type ActionError struct {
    Resource string `json:"resource"`
    Action   string `json:"action"`
    Error    string `json:"error"`
}
```

`interfaces/iac_sizing.go`:
```go
package interfaces

// SizingMap defines the default resource allocations per size tier.
var SizingMap = map[Size]SizingDefaults{
    SizeXS: {CPU: "0.25", Memory: "512Mi", DBStorage: "10Gi", CacheMemory: "256Mi"},
    SizeS:  {CPU: "1", Memory: "2Gi", DBStorage: "50Gi", CacheMemory: "1Gi"},
    SizeM:  {CPU: "2", Memory: "4Gi", DBStorage: "100Gi", CacheMemory: "4Gi"},
    SizeL:  {CPU: "4", Memory: "16Gi", DBStorage: "500Gi", CacheMemory: "16Gi"},
    SizeXL: {CPU: "8", Memory: "32Gi", DBStorage: "1Ti", CacheMemory: "64Gi"},
}

type SizingDefaults struct {
    CPU         string `json:"cpu"`
    Memory      string `json:"memory"`
    DBStorage   string `json:"db_storage"`
    CacheMemory string `json:"cache_memory"`
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /Users/jon/workspace/workflow && go test ./interfaces/ -v`
Expected: PASS

**Step 5: Add backward-compat aliases in platform/ package**

Modify `platform/types.go` to re-export from interfaces where types overlap.

**Step 6: Commit**

```bash
git add interfaces/iac_*.go
git commit -m "feat: extract IaC interfaces to interfaces/ package"
```

---

### Task 2: Extend State Backends (S3 conditional writes, GCS, Azure Blob, PostgreSQL)

**Files:**
- Modify: `module/iac_state_spaces.go` — upgrade to use S3 conditional writes for atomic locking
- Create: `module/iac_state_gcs.go` — GCS backend with generation-match locking
- Create: `module/iac_state_azure.go` — Azure Blob Storage backend with lease locking
- Create: `module/iac_state_postgres.go` — PostgreSQL backend with advisory locks
- Modify: `module/iac_module.go` — register new backends
- Test: `module/iac_state_gcs_test.go`, `module/iac_state_azure_test.go`, `module/iac_state_postgres_test.go`

**Step 1: Write failing tests for each new backend**

Each test should use a mock client interface (same pattern as existing Spaces backend) to avoid real cloud calls.

**Step 2: Implement GCS backend**

Uses `cloud.google.com/go/storage` (latest). Locking via generation-match preconditions on GCS objects — truly atomic, no race conditions.

**Step 3: Implement Azure Blob backend**

Uses `github.com/Azure/azure-sdk-for-go/sdk/storage/azblob` (Track 2). Locking via blob leases (60s renewable).

**Step 4: Implement PostgreSQL backend**

Uses `github.com/jackc/pgx/v5` (already in go.mod). Locking via `pg_advisory_lock()`. Schema: `iac_resources` table (name PK, type, provider, provider_id, config_hash, applied_config JSONB, outputs JSONB, dependencies TEXT[], timestamps).

**Step 5: Fix Spaces backend locking**

Replace HeadObject+PutObject with S3 conditional writes (`If-None-Match: *` on PutObject) for atomic lock acquisition.

**Step 6: Register all backends in iac_module.go**

**Step 7: Run all tests, commit**

```bash
git commit -m "feat: add GCS, Azure Blob, PostgreSQL state backends with atomic locking"
```

---

### Task 3: Resource Differ (Plan Computation)

**Files:**
- Create: `platform/differ.go` — computes plan from desired specs vs current state
- Create: `platform/differ_test.go`

**Step 1: Write failing tests**

```go
func TestDiffer_NewResource(t *testing.T) {
    desired := []interfaces.ResourceSpec{{Name: "db", Type: "infra.database"}}
    current := []interfaces.ResourceState{}
    plan := differ.ComputePlan(desired, current)
    if len(plan.Actions) != 1 || plan.Actions[0].Action != "create" {
        t.Fatal("expected 1 create action")
    }
}

func TestDiffer_DeletedResource(t *testing.T) { ... }
func TestDiffer_UpdatedResource(t *testing.T) { ... }
func TestDiffer_DependencyOrdering(t *testing.T) { ... }
func TestDiffer_NoChanges(t *testing.T) { ... }
```

**Step 2: Implement differ**

- Compare desired vs current by name
- New names → create actions
- Missing names → delete actions (reverse dependency order)
- Changed config hash → update actions (call driver.Diff if available)
- Topological sort by DependsOn for apply ordering

**Step 3: Run tests, commit**

---

### Task 4: Sizing Engine

**Files:**
- Create: `platform/sizing.go` — resolves Size + Hints to concrete resource requirements
- Create: `platform/sizing_test.go`

**Step 1: Write failing tests**

Test that size "m" for infra.database returns cpu=2, memory=4Gi, storage=100Gi. Test that hints override defaults (size "m" + cpu "3" → cpu=3, memory=4Gi). Test that unknown size returns error.

**Step 2: Implement sizing resolution**

Use `interfaces.SizingMap` as defaults. Merge with ResourceHints (hints override defaults). Return merged spec that provider plugins use to find closest SKU.

**Step 3: Run tests, commit**

---

### Task 5: Enhance `wfctl infra` CLI

**Files:**
- Modify: `cmd/wfctl/infra.go` — add real plan computation, markdown output, state commands
- Create: `cmd/wfctl/infra_state.go` — `wfctl infra state list/export/import`
- Test: `cmd/wfctl/infra_test.go`

**Step 1: Write tests for CLI argument parsing**

**Step 2: Implement enhanced plan subcommand**

- Parse infra.yaml → extract ResourceSpec list
- Load current state from iac.state module
- Run differ to compute plan
- Print plan as table (default) or `--format markdown` for PR comments
- `--output plan.json` to save plan for later apply

**Step 3: Implement apply/destroy/status/drift subcommands**

- `wfctl infra apply` — load plan (from file or compute), confirm (unless `--auto-approve`), call provider.Apply(), update state store
- `wfctl infra destroy` — confirm (unless `--auto-approve`), call provider.Destroy() in reverse dependency order, remove from state
- `wfctl infra status` — load state, call provider.Status() for each resource, print table
- `wfctl infra drift` — load state, call provider.DetectDrift(), print drift report
- `wfctl infra import` — call provider.Import() with cloud ID + type, save to state

**Step 4: Implement state subcommands**

- `wfctl infra state list` — table of tracked resources
- `wfctl infra state export --format tfstate` — write to .tfstate
- `wfctl infra state import --from tfstate <file>` — read from .tfstate
- `wfctl infra state import --from pulumi <file>` — read Pulumi checkpoint JSON

**Step 5: Run tests, commit**

---

## Phase 2: Four Provider Plugins (parallel implementation)

All four plugins follow the same pattern: standalone Go module, gRPC plugin via `plugin/external/sdk`, implements `IaCProvider` + `ResourceDriver` per resource type.

### Task 6: workflow-plugin-digitalocean (extraction from engine)

**Repo:** `/Users/jon/workspace/workflow-plugin-digitalocean` (new)

**SDK:** `github.com/digitalocean/godo v1.178.0`

**Step 1: Create repo with standard plugin structure**

```
workflow-plugin-digitalocean/
├── cmd/plugin/main.go           # gRPC plugin entrypoint
├── internal/
│   ├── plugin.go                # PluginProvider, ModuleProvider, StepProvider
│   ├── provider.go              # IaCProvider implementation
│   ├── sizing.go                # DO-specific SKU mapping
│   ├── drivers/
│   │   ├── droplet.go           # infra.container_service (Droplet) + infra.k8s_cluster (DOKS)
│   │   ├── app_platform.go      # infra.container_service (App Platform variant)
│   │   ├── database.go          # infra.database
│   │   ├── load_balancer.go     # infra.load_balancer
│   │   ├── vpc.go               # infra.vpc
│   │   ├── firewall.go          # infra.firewall
│   │   ├── dns.go               # infra.dns
│   │   ├── spaces.go            # infra.storage
│   │   ├── registry.go          # infra.registry (DOCR)
│   │   ├── certificate.go       # infra.certificate
│   │   └── kubernetes.go        # infra.k8s_cluster (DOKS)
│   └── drivers/*_test.go        # one test file per driver
├── plugin.json                  # capabilities manifest
├── go.mod
├── .goreleaser.yaml
└── .github/workflows/
    ├── ci.yml
    └── release.yml
```

**Step 2: Extract and enhance from feat/iac-digitalocean branch**

Port `platform_do_app.go`, `platform_do_database.go`, `platform_do_dns.go`, `platform_do_networking.go` — but rewrite using `godo v1.178.0` comprehensively:

- **Droplets**: `godo.Droplets` — full CRUD, resize, power actions, snapshots
- **App Platform**: `godo.Apps` — multi-service AppSpec, DOCR source, workers, jobs, static sites
- **Databases**: `godo.Databases` — all engines, users, firewall rules, replicas, connection pools, resize
- **Load Balancers**: `godo.LoadBalancers` — forwarding rules, health checks, sticky sessions
- **VPCs**: `godo.VPCs` + `godo.VPCPeerings` — full lifecycle
- **Firewalls**: `godo.Firewalls` — inbound/outbound rules, droplet/tag targets
- **DNS**: `godo.Domains` — idempotent record management (read existing → diff → create/update/delete)
- **Spaces**: `godo.Spaces` via S3-compatible API — bucket CRUD, CORS, lifecycle rules
- **Registry (DOCR)**: `godo.Registry` — repo management, garbage collection
- **Kubernetes (DOKS)**: `godo.Kubernetes` — clusters, node pools, upgrades
- **Certificates**: `godo.Certificates` — Let's Encrypt + custom

**Step 3: Write tests with mock godo client for each driver**

**Step 4: Add sizing map**

```go
var doSizing = map[string]map[interfaces.Size]string{
    "infra.container_service": {SizeXS: "s-1vcpu-512mb-10gb", SizeS: "s-1vcpu-2gb", SizeM: "s-2vcpu-4gb", SizeL: "s-4vcpu-8gb", SizeXL: "s-8vcpu-16gb"},
    "infra.database":         {SizeXS: "db-s-1vcpu-1gb", SizeS: "db-s-1vcpu-2gb", SizeM: "db-s-2vcpu-4gb", SizeL: "db-s-4vcpu-8gb", SizeXL: "db-s-8vcpu-16gb"},
}
```

**Step 5: GoReleaser config, CI, release workflow, push, tag v0.1.0**

---

### Task 7: workflow-plugin-aws (extraction + enhancement)

**Repo:** `/Users/jon/workspace/workflow-plugin-aws` (new)

**SDK:** `github.com/aws/aws-sdk-go-v2 v1.41.4+` — import only needed service modules

**Step 1: Create repo, same structure as DO plugin**

**Step 2: Port existing `platform/providers/aws/` drivers (already behind build tag on main)**

The engine already has these drivers in `platform/providers/aws/`. Extract and adapt to the plugin SDK interface:
- **EKS**: `service/eks` — clusters, node groups, add-ons, OIDC providers
- **VPC**: `service/ec2` — VPCs, subnets, route tables, internet gateways, NAT gateways
- **RDS**: `service/rds` — instances, Aurora clusters, parameter groups, subnet groups
- **IAM**: `service/iam` — roles, policies, instance profiles
- **ALB/NLB**: `service/elasticloadbalancingv2` — load balancers, target groups, listeners

**Step 3: Add missing drivers**

- **ECS Fargate**: `service/ecs` — task definitions, services, clusters
- **ElastiCache**: `service/elasticache` — Redis/Memcached/Valkey clusters
- **ECR**: `service/ecr` — repositories, lifecycle policies
- **Route 53**: `service/route53` — hosted zones, record sets, health checks
- **API Gateway v2**: `service/apigatewayv2` — HTTP APIs, routes, integrations
- **S3**: `service/s3` — buckets, versioning, lifecycle
- **ACM**: `service/acm` — certificate requests, DNS validation

**Step 4: AWS sizing map (instance types per resource type)**

**Step 5: Tests, GoReleaser, CI, tag v0.1.0**

---

### Task 8: workflow-plugin-gcp

**Repo:** `/Users/jon/workspace/workflow-plugin-gcp` (new)

**SDK:** `cloud.google.com/go` + `google.golang.org/api` (latest versions)

**Step 1: Create repo**

**Step 2: Implement drivers**

- **Cloud Run**: `cloud.google.com/go/run` v1.15.0 — services, revisions, traffic splitting
- **GKE**: `cloud.google.com/go/container` v1.46.0 — clusters, node pools
- **Cloud SQL**: `google.golang.org/api/sqladmin/v1` — instances, databases, users, replicas
- **Memorystore**: `cloud.google.com/go/redis` v1.18.3 — Redis instances
- **VPC Network**: `cloud.google.com/go/compute` v1.57.0 — networks (global), subnetworks (regional), firewall rules
- **Cloud Load Balancing**: `cloud.google.com/go/compute` — forwarding rules, backend services, health checks, URL maps
- **Cloud DNS**: `google.golang.org/api/dns/v1` — managed zones, record sets
- **Artifact Registry**: `cloud.google.com/go/artifactregistry` v1.20.0 — repos, cleanup policies
- **API Gateway**: `cloud.google.com/go/apigateway` v1.7.7 — gateways, APIs, configs
- **IAM**: `google.golang.org/api/iam/v1` — service accounts, role bindings
- **GCS**: `cloud.google.com/go/storage` v1.60.0 — buckets, lifecycle
- **Managed SSL**: `cloud.google.com/go/compute` — SSL certificates

**Step 3: GCP sizing map (machine types, Cloud SQL tiers)**

**Step 4: Tests, GoReleaser, CI, tag v0.1.0**

---

### Task 9: workflow-plugin-azure

**Repo:** `/Users/jon/workspace/workflow-plugin-azure` (new)

**SDK:** `github.com/Azure/azure-sdk-for-go/sdk` Track 2 (azcore v1.21.0+, azidentity v1.13.1+)

**Step 1: Create repo**

**Step 2: Implement drivers**

- **ACI**: `armcontainerinstance` v2.4.0 — container groups
- **AKS**: `armcontainerservice` — managed clusters, agent pools
- **Azure SQL**: `armsql` v1.2.0 — servers, databases, elastic pools
- **Azure Cache for Redis**: `armredis` — instances
- **VNet**: `armnetwork` v6.2.0 — virtual networks, subnets, route tables
- **Azure LB / App Gateway**: `armnetwork` — load balancers, application gateways
- **Azure DNS**: `armdns` v1.2.0 — zones, record sets
- **ACR**: `armcontainerregistry` v1.2.0 — registries, replications
- **APIM**: `armapimanagement` — services, APIs, policies
- **NSGs**: `armnetwork` — network security groups, rules
- **Managed Identity**: `armmsi` — user-assigned identities, role assignments
- **Blob Storage**: `azblob` — storage accounts, containers
- **App Service Certificates**: `armappservice` — certificates

**Step 3: Azure sizing map (VM sizes, database DTUs/vCores)**

**Step 4: Tests, GoReleaser, CI, tag v0.1.0**

---

## Phase 3: OpenTofu/Terraform Adapter

### Task 10: workflow-plugin-tofu

**Repo:** `/Users/jon/workspace/workflow-plugin-tofu` (new)

**SDKs:**
- `github.com/hashicorp/hcl/v2` v2.24.0 (hclwrite for HCL generation)
- `github.com/zclconf/go-cty` v1.18.0 (type system for HCL values)
- `github.com/opentofu/opentofu` v1.11.5 (embedded execution)

**Step 1: Create repo**

```
workflow-plugin-tofu/
├── cmd/plugin/main.go
├── internal/
│   ├── plugin.go
│   ├── generator/
│   │   ├── hcl.go               # infra.yaml → .tf files via hclwrite
│   │   ├── hcl_test.go
│   │   ├── provider_aws.go      # AWS resource → HCL mapping
│   │   ├── provider_gcp.go
│   │   ├── provider_azure.go
│   │   ├── provider_do.go
│   │   └── provider_test.go
│   ├── executor/
│   │   ├── tofu.go              # Embedded OpenTofu execution
│   │   ├── tofu_test.go
│   │   ├── terraform.go         # External TF binary execution
│   │   └── terraform_test.go
│   └── state/
│       ├── tfstate_import.go    # .tfstate → workflow ResourceState
│       ├── tfstate_export.go    # workflow ResourceState → .tfstate
│       └── tfstate_test.go
├── plugin.json
├── go.mod
└── .goreleaser.yaml
```

**Step 2: Implement HCL generator**

Map each abstract resource type to Terraform resource blocks using hclwrite:
- `infra.database` + provider=aws → `resource "aws_db_instance" "name" { ... }`
- `infra.vpc` + provider=gcp → `resource "google_compute_network" "name" { ... }` + subnetworks

**Step 3: Implement OpenTofu executor**

Embed `opentofu` as a Go library. Steps: `step.tofu_init`, `step.tofu_plan`, `step.tofu_apply`.

**Step 4: Implement Terraform executor**

Shell out to external `terraform` binary. User provides path via config: `terraform_binary: /usr/local/bin/terraform`.

**Step 5: Implement state adapters**

Parse `.tfstate` JSON → extract resource entries → map to `interfaces.ResourceState`. Reverse for export.

**Step 6: Tests, GoReleaser, CI, tag v0.1.0**

---

## Phase 4: CI Generator + Deployment Steps

### Task 11: workflow-plugin-ci-generator

**Repo:** `/Users/jon/workspace/workflow-plugin-ci-generator` (new)

**Step 1: Create repo**

```
workflow-plugin-ci-generator/
├── cmd/plugin/main.go
├── internal/
│   ├── plugin.go
│   ├── generator.go             # Common generation logic
│   ├── platforms/
│   │   ├── github_actions.go    # GHA workflow YAML generation
│   │   ├── github_actions_test.go
│   │   ├── gitlab_ci.go         # .gitlab-ci.yml generation
│   │   ├── gitlab_ci_test.go
│   │   ├── jenkins.go           # Declarative Jenkinsfile generation
│   │   ├── jenkins_test.go
│   │   ├── circleci.go          # CircleCI config generation
│   │   └── circleci_test.go
│   └── templates/               # Go template fragments per platform
├── plugin.json
└── go.mod
```

**Step 2: Implement GHA generator**

Generate modern GHA workflows (actions/checkout@v4, setup-go@v5, permissions block, `github-script@v7`). Output includes:
- `infra.yml` — plan on PR, apply on merge
- `build.yml` — build + test + container push
- `deploy.yml` — deploy after infra apply

**Step 3: Implement GitLab CI generator**

Generate `.gitlab-ci.yml` using GitLab CI v17+ syntax: `rules:` (not `only:`), `needs:` for DAG, `environment:` for deployment tracking.

**Step 4: Implement Jenkins generator**

Generate declarative `Jenkinsfile`: `pipeline { agent { } stages { } }` syntax.

**Step 5: Implement CircleCI generator**

Generate `.circleci/config.yml` using v2.1 syntax: orbs, executors, workflows with approval jobs.

**Step 6: Add `wfctl ci generate` command to engine**

**Step 7: Tests, GoReleaser, CI, tag v0.1.0**

---

### Task 12: Deployment Pipeline Steps

**Files (in workflow engine repo):**
- Create: `module/pipeline_step_deploy_rolling.go`
- Create: `module/pipeline_step_deploy_blue_green.go`
- Create: `module/pipeline_step_deploy_canary.go`
- Create: `module/pipeline_step_deploy_verify.go`
- Create: `module/pipeline_step_deploy_rollback.go`
- Create: `module/pipeline_step_container_build.go`
- Test files for each

**Step 1: Implement step.deploy_rolling**

Orchestrates incremental replacement:
1. Get current replica set from provider
2. For each batch (based on max_surge/max_unavailable):
   a. Create new replicas with updated image
   b. Wait for health check pass
   c. Terminate old replicas
3. If health check fails, trigger rollback

**Step 2: Implement step.deploy_blue_green**

1. Create parallel "green" environment with new version
2. Run health checks on green
3. Switch traffic (update LB target / DNS)
4. Tear down "blue" (old) environment

**Step 3: Implement step.deploy_canary**

1. Route percentage of traffic to canary (configurable stages: 1%, 10%, 50%, 100%)
2. At each stage, check metrics gate (error rate, latency, etc.)
3. If gate fails, route all traffic back to stable
4. If all gates pass, promote canary to stable

**Step 4: Implement step.deploy_verify and step.deploy_rollback**

**Step 5: Tests for each step type, commit**

---

## Phase 5: Polish + Integration

### Task 13: Registry Manifests + Scenarios

- Add manifests for all new plugins to `workflow-registry`
- Add manifests to `workflow-cloud-registry` for private plugins
- Create workflow-scenarios entries (scenario 64+):
  - `64-iac-do-basic` — DO VPC + database + app with rolling deploy
  - `65-iac-aws-basic` — AWS VPC + RDS + ECS Fargate
  - `66-iac-multi-cloud` — same infra.yaml deployed to two providers
  - `67-iac-tofu-generate` — generate TF files from infra.yaml
  - `68-ci-generator` — generate GHA + GitLab CI from infra.yaml

### Task 14: GoCodeAlone/setup-wfctl GHA Action

Create a GitHub Action that installs `wfctl` for use in CI pipelines:
- Downloads the correct binary for the runner OS/arch
- Caches it
- Adds to PATH

### Task 15: Documentation

- Update engine DOCUMENTATION.md with all new module/step types
- Add IaC getting-started guide
- Update wfctl --help for new subcommands
