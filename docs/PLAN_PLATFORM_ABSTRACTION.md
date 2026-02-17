# Platform Abstraction Layer Design

## Executive Summary

This document specifies the design for a **platform abstraction layer** that extends the workflow engine to manage infrastructure, shared environment primitives, and application lifecycle as declarative YAML-driven workflows. The design introduces a three-tier model where infrastructure foundations (Tier 1), shared primitives (Tier 2), and application workflows (Tier 3) are expressed, planned, and executed through the same engine that currently handles application-level orchestration.

The key architectural additions are:

1. **Provider plugin system** -- a Go interface hierarchy (`Provider`, `ResourceDriver`, `CapabilityMapper`) that isolates infrastructure-specific logic behind abstract capability contracts.
2. **Context/constraint model** -- a hierarchical context chain (`OrgContext -> EnvironmentContext -> ApplicationContext`) where each tier's outputs constrain the tier below.
3. **Two-mode execution** -- plan-time resolution with approval gates for infrastructure changes; continuous reconciliation for runtime scaling/health.
4. **State and drift management** -- per-tier state stores with cross-tier dependency tracking and drift detection.
5. **RBAC/boundary enforcement** -- role-based authoring scoped by tier, with constraint validation at plan time.

The implementation is phased: context model first, then AWS/EKS as reference provider, tier boundary enforcement, Docker Compose as second provider to stress-test the abstraction, and additional providers afterward.

---

## 1. Architecture Overview

### 1.1 Three-Tier Model

```
+------------------------------------------------------------------+
|  Tier 1: Infrastructure Foundations                               |
|  Owner: SRE / Platform team                                      |
|  Changes: Infrequent, approval-gated                             |
|  Resources: K8s clusters, VPCs, IAM roles, networking            |
|  Execution: Plan-time resolution, explicit apply                 |
+------------------------------------------------------------------+
        |  Produces: capabilities, constraints, credentials
        v
+------------------------------------------------------------------+
|  Tier 2: Shared Primitives / Environment Context                 |
|  Owner: Platform team / DevOps                                   |
|  Changes: Moderate frequency                                     |
|  Resources: Namespaces, quotas, shared DBs, queues, ingress,     |
|             service mesh, observability stack                     |
|  Execution: Plan-time + reconciliation for quota enforcement     |
+------------------------------------------------------------------+
        |  Produces: resource endpoints, connection strings,
        |  credential refs, resource limits
        v
+------------------------------------------------------------------+
|  Tier 3: Application Workflows                                   |
|  Owner: Application teams / Developers                           |
|  Changes: Frequent, CI/CD-driven                                 |
|  Resources: Deployments, services, scaling policies,             |
|             app-specific configs                                  |
|  Execution: Continuous reconciliation                            |
+------------------------------------------------------------------+
```

### 1.2 How This Maps to the Existing Engine

The current engine architecture (`StdEngine`, `WorkflowHandler`, `Trigger`, `Pipeline`) already provides the execution substrate. The platform abstraction layer adds:

| Existing Concept | Platform Extension |
|---|---|
| `ModuleFactory` / module types | New module types: `platform.provider`, `platform.resource`, `platform.context` |
| `WorkflowHandler` | New handler: `PlatformWorkflowHandler` for plan/apply/destroy lifecycle |
| `PipelineStep` | New steps: `step.provision`, `step.plan`, `step.apply`, `step.drift_check`, `step.constraint_check` |
| `Trigger` | New trigger: `reconciliation` (periodic drift detection loop) |
| `secrets.Provider` | Extended: `CredentialBroker` issues scoped credentials per tier/context |
| `config.WorkflowConfig` | Extended: new top-level `platform` section in YAML configs |

### 1.3 Design Principles

1. **The YAML contract is sacred.** Users never write provider-specific resource definitions directly. They declare capabilities; the provider maps them to concrete resources.
2. **Capability contracts, not resource types.** Abstract declarations like "container runtime with 3 replicas and 512MB memory" resolve to EKS node groups, Docker Compose services, or ECS task definitions depending on the active provider.
3. **Plan before apply.** All infrastructure changes produce a diff/plan before execution. Tier 1 and Tier 2 require explicit approval; Tier 3 can auto-apply within constraints.
4. **Context flows downward, never upward.** Tier 1 outputs feed Tier 2 inputs. Tier 2 outputs feed Tier 3 inputs. A lower tier cannot modify a higher tier's state.
5. **Providers are swappable.** The same YAML config works against AWS, local Docker Compose, or a mock provider. Fidelity gaps are explicit and handled gracefully.

---

## 2. Go Interface Hierarchy

All interfaces live in a new package: `platform/`.

### 2.1 Core Types

```go
// platform/types.go

package platform

import (
    "context"
    "time"
)

// Tier represents the infrastructure tier a resource belongs to.
type Tier int

const (
    TierInfrastructure Tier = 1 // Tier 1: compute, networking, IAM
    TierSharedPrimitive Tier = 2 // Tier 2: namespaces, queues, shared DBs
    TierApplication     Tier = 3 // Tier 3: app deployments, scaling
)

// ResourceState represents the lifecycle state of a managed resource.
type ResourceState string

const (
    ResourceStatePending    ResourceState = "pending"
    ResourceStateCreating   ResourceState = "creating"
    ResourceStateActive     ResourceState = "active"
    ResourceStateUpdating   ResourceState = "updating"
    ResourceStateDeleting   ResourceState = "deleting"
    ResourceStateDeleted    ResourceState = "deleted"
    ResourceStateFailed     ResourceState = "failed"
    ResourceStateDegraded   ResourceState = "degraded"
    ResourceStateDrifted    ResourceState = "drifted"
)

// CapabilityDeclaration is a provider-agnostic resource requirement.
// This is what users write in YAML.
type CapabilityDeclaration struct {
    Name        string         `yaml:"name" json:"name"`
    Type        string         `yaml:"type" json:"type"`               // e.g., "container_runtime", "database", "message_queue", "ingress"
    Tier        Tier           `yaml:"tier" json:"tier"`
    Properties  map[string]any `yaml:"properties" json:"properties"`   // abstract properties (replicas, memory, ports, etc.)
    Constraints []Constraint   `yaml:"constraints" json:"constraints"` // hard limits from parent tier
    DependsOn   []string       `yaml:"dependsOn" json:"dependsOn"`    // other capability names
}

// Constraint represents a limit or requirement imposed by a parent tier.
type Constraint struct {
    Field    string `yaml:"field" json:"field"`       // e.g., "memory", "replicas", "cpu"
    Operator string `yaml:"operator" json:"operator"` // "<=", ">=", "==", "in", "not_in"
    Value    any    `yaml:"value" json:"value"`
    Source   string `yaml:"source" json:"source"`     // which context/tier imposed this
}

// ResourceOutput represents the concrete output of a provisioned resource.
// These become inputs/constraints for downstream tiers.
type ResourceOutput struct {
    Name          string         `json:"name"`
    Type          string         `json:"type"`
    ProviderType  string         `json:"providerType"`         // e.g., "aws.eks_cluster", "docker.service"
    Endpoint      string         `json:"endpoint,omitempty"`
    ConnectionStr string         `json:"connectionString,omitempty"`
    CredentialRef string         `json:"credentialRef,omitempty"` // reference to credential broker
    Properties    map[string]any `json:"properties"`
    State         ResourceState  `json:"state"`
    LastSynced    time.Time      `json:"lastSynced"`
}

// PlanAction represents a single planned change to infrastructure.
type PlanAction struct {
    Action       string         `json:"action"`       // "create", "update", "delete", "no-op"
    ResourceName string         `json:"resourceName"`
    ResourceType string         `json:"resourceType"`
    Provider     string         `json:"provider"`
    Before       map[string]any `json:"before,omitempty"` // current state (nil for create)
    After        map[string]any `json:"after,omitempty"`  // desired state (nil for delete)
    Diff         []DiffEntry    `json:"diff,omitempty"`
}

// DiffEntry represents a single field difference in a plan.
type DiffEntry struct {
    Path     string `json:"path"`
    OldValue any    `json:"oldValue"`
    NewValue any    `json:"newValue"`
}

// Plan is the complete execution plan for a set of changes.
type Plan struct {
    ID          string       `json:"id"`
    Tier        Tier         `json:"tier"`
    Context     string       `json:"context"`     // context path, e.g., "acme/production/api-service"
    Actions     []PlanAction `json:"actions"`
    CreatedAt   time.Time    `json:"createdAt"`
    ApprovedAt  *time.Time   `json:"approvedAt,omitempty"`
    ApprovedBy  string       `json:"approvedBy,omitempty"`
    Status      string       `json:"status"`      // "pending", "approved", "applying", "applied", "failed"
}
```

### 2.2 Provider Interface

```go
// platform/provider.go

package platform

import "context"

// Provider is the top-level interface for an infrastructure provider.
// A provider manages a collection of resource drivers and maps abstract
// capabilities to provider-specific resource types.
type Provider interface {
    // Name returns the provider identifier (e.g., "aws", "docker-compose", "gcp").
    Name() string

    // Version returns the provider version.
    Version() string

    // Initialize prepares the provider (authenticate, validate config).
    Initialize(ctx context.Context, config map[string]any) error

    // Capabilities returns the set of capability types this provider supports.
    // Used during plan-time to determine if a provider can satisfy a declaration.
    Capabilities() []CapabilityType

    // MapCapability resolves an abstract capability declaration to a provider-specific
    // resource plan. Returns an error if the capability cannot be satisfied.
    MapCapability(ctx context.Context, decl CapabilityDeclaration, pctx *PlatformContext) ([]ResourcePlan, error)

    // ResourceDriver returns the driver for a specific provider resource type.
    ResourceDriver(resourceType string) (ResourceDriver, error)

    // CredentialBroker returns the provider's credential management interface.
    CredentialBroker() CredentialBroker

    // StateStore returns the provider's state persistence interface.
    StateStore() StateStore

    // Healthy returns nil if the provider is reachable and authenticated.
    Healthy(ctx context.Context) error

    // Close releases any resources held by the provider.
    Close() error
}

// CapabilityType describes a capability a provider can satisfy.
type CapabilityType struct {
    Name          string            `json:"name"`          // e.g., "container_runtime"
    Description   string            `json:"description"`
    Tier          Tier              `json:"tier"`
    Properties    []PropertySchema  `json:"properties"`    // accepted properties
    Constraints   []PropertySchema  `json:"constraints"`   // constraints it can enforce
    Fidelity      FidelityLevel     `json:"fidelity"`      // how faithfully this is implemented
}

// PropertySchema describes a property accepted by a capability.
type PropertySchema struct {
    Name         string `json:"name"`
    Type         string `json:"type"`         // "string", "int", "bool", "duration", "map", "list"
    Required     bool   `json:"required"`
    Description  string `json:"description"`
    DefaultValue any    `json:"defaultValue,omitempty"`
}

// FidelityLevel indicates how faithfully a provider implements a capability.
type FidelityLevel string

const (
    FidelityFull    FidelityLevel = "full"     // production-equivalent
    FidelityPartial FidelityLevel = "partial"  // works but with limitations
    FidelityStub    FidelityLevel = "stub"     // mock/no-op (e.g., IAM on local)
    FidelityNone    FidelityLevel = "none"     // not supported
)

// ResourcePlan is the provider-specific plan for a single resource.
type ResourcePlan struct {
    ResourceType string         `json:"resourceType"` // provider-specific, e.g., "aws.eks_nodegroup"
    Name         string         `json:"name"`
    Properties   map[string]any `json:"properties"`
    DependsOn    []string       `json:"dependsOn"`
}
```

### 2.3 ResourceDriver Interface

```go
// platform/resource_driver.go

package platform

import "context"

// ResourceDriver handles CRUD lifecycle for a specific provider resource type.
// Each provider composes multiple drivers (one per resource type).
type ResourceDriver interface {
    // ResourceType returns the fully qualified resource type (e.g., "aws.eks_cluster").
    ResourceType() string

    // Create provisions a new resource. Returns outputs when complete.
    Create(ctx context.Context, name string, properties map[string]any) (*ResourceOutput, error)

    // Read fetches the current state of an existing resource.
    Read(ctx context.Context, name string) (*ResourceOutput, error)

    // Update modifies an existing resource to match desired properties.
    Update(ctx context.Context, name string, current, desired map[string]any) (*ResourceOutput, error)

    // Delete removes a resource.
    Delete(ctx context.Context, name string) error

    // HealthCheck returns the health status of a managed resource.
    HealthCheck(ctx context.Context, name string) (*HealthStatus, error)

    // Scale adjusts resource scaling parameters (if applicable).
    // Returns ErrNotScalable if the resource type does not support scaling.
    Scale(ctx context.Context, name string, scaleParams map[string]any) (*ResourceOutput, error)

    // Diff compares current state with desired state and returns differences.
    Diff(ctx context.Context, name string, desired map[string]any) ([]DiffEntry, error)
}

// HealthStatus represents the health of a managed resource.
type HealthStatus struct {
    Status  string         `json:"status"`  // "healthy", "unhealthy", "degraded", "unknown"
    Message string         `json:"message"`
    Details map[string]any `json:"details,omitempty"`
    CheckedAt time.Time    `json:"checkedAt"`
}
```

### 2.4 CapabilityMapper Interface

```go
// platform/capability_mapper.go

package platform

// CapabilityMapper translates abstract capability declarations into
// provider-specific resource plans. Each provider has one mapper.
type CapabilityMapper interface {
    // CanMap returns true if this mapper can handle the given capability type.
    CanMap(capabilityType string) bool

    // Map translates a capability declaration into one or more resource plans.
    // The PlatformContext provides parent tier outputs and constraints.
    Map(decl CapabilityDeclaration, pctx *PlatformContext) ([]ResourcePlan, error)

    // ValidateConstraints checks if a capability declaration satisfies all
    // constraints imposed by parent tiers. Returns constraint violations.
    ValidateConstraints(decl CapabilityDeclaration, constraints []Constraint) []ConstraintViolation
}

// ConstraintViolation describes a constraint check failure.
type ConstraintViolation struct {
    Constraint Constraint `json:"constraint"`
    Actual     any        `json:"actual"`
    Message    string     `json:"message"`
}
```

### 2.5 ContextResolver Interface

```go
// platform/context_resolver.go

package platform

import "context"

// PlatformContext is the hierarchical context flowing through the tier system.
// It carries org, environment, and application identifiers plus the accumulated
// outputs and constraints from parent tiers.
type PlatformContext struct {
    // Identity
    Org         string `json:"org"`
    Environment string `json:"environment"`   // e.g., "production", "staging", "dev"
    Application string `json:"application"`   // empty for Tier 1 and Tier 2
    Tier        Tier   `json:"tier"`

    // Inherited from parent tiers
    ParentOutputs map[string]*ResourceOutput `json:"parentOutputs"` // keyed by resource name
    Constraints   []Constraint               `json:"constraints"`

    // Resolved credentials for this scope
    Credentials map[string]string `json:"-"` // never serialized

    // Metadata
    Labels      map[string]string `json:"labels"`
    Annotations map[string]string `json:"annotations"`
}

// ContextPath returns the full hierarchical path: "org/env/app".
func (pc *PlatformContext) ContextPath() string {
    path := pc.Org + "/" + pc.Environment
    if pc.Application != "" {
        path += "/" + pc.Application
    }
    return path
}

// ContextResolver builds PlatformContext by aggregating parent tier state.
type ContextResolver interface {
    // ResolveContext builds a PlatformContext for the given tier and identifiers.
    // It reads parent tier state stores to populate ParentOutputs and Constraints.
    ResolveContext(ctx context.Context, org, env, app string, tier Tier) (*PlatformContext, error)

    // PropagateOutputs writes resource outputs into the context store so
    // downstream tiers can resolve them.
    PropagateOutputs(ctx context.Context, pctx *PlatformContext, outputs []*ResourceOutput) error

    // ValidateTierBoundary ensures a workflow operating at the given tier
    // does not attempt to modify resources outside its scope.
    ValidateTierBoundary(pctx *PlatformContext, declarations []CapabilityDeclaration) []ConstraintViolation
}
```

### 2.6 CredentialBroker Interface

```go
// platform/credential_broker.go

package platform

import (
    "context"
    "time"
)

// CredentialBroker issues and manages credentials scoped to specific
// tiers, environments, and applications. It integrates with the existing
// secrets.Provider system.
type CredentialBroker interface {
    // IssueCredential creates a scoped credential for the given context.
    // The credential is stored in the secrets backend and a reference is returned.
    IssueCredential(ctx context.Context, pctx *PlatformContext, request CredentialRequest) (*CredentialRef, error)

    // RevokeCredential invalidates a previously issued credential.
    RevokeCredential(ctx context.Context, ref *CredentialRef) error

    // ResolveCredential retrieves the actual credential value from a reference.
    ResolveCredential(ctx context.Context, ref *CredentialRef) (string, error)

    // RotateCredential replaces an existing credential with a new one.
    RotateCredential(ctx context.Context, ref *CredentialRef) (*CredentialRef, error)

    // ListCredentials returns all credential references for a given context.
    ListCredentials(ctx context.Context, pctx *PlatformContext) ([]*CredentialRef, error)
}

// CredentialRequest specifies what credential to issue.
type CredentialRequest struct {
    Name      string        `json:"name"`      // human-readable name
    Type      string        `json:"type"`      // "api_key", "database", "tls_cert", "token"
    Scope     []string      `json:"scope"`     // resource names this credential can access
    TTL       time.Duration `json:"ttl"`       // credential lifetime (0 = no expiry)
    Renewable bool          `json:"renewable"` // can be renewed before expiry
}

// CredentialRef is a pointer to a stored credential.
// The actual value is never stored in state -- only this reference.
type CredentialRef struct {
    ID          string    `json:"id"`
    Name        string    `json:"name"`
    SecretPath  string    `json:"secretPath"`  // path in secrets backend
    Provider    string    `json:"provider"`    // which secrets provider holds it
    ExpiresAt   time.Time `json:"expiresAt"`
    Tier        Tier      `json:"tier"`
    ContextPath string    `json:"contextPath"` // org/env/app
}
```

### 2.7 StateStore Interface

```go
// platform/state_store.go

package platform

import (
    "context"
    "time"
)

// StateStore manages the persistent state of provisioned resources.
// Each provider has its own state store, and there is an abstract
// layer that aggregates across providers.
type StateStore interface {
    // SaveResource persists the state of a resource.
    SaveResource(ctx context.Context, contextPath string, output *ResourceOutput) error

    // GetResource retrieves a resource's state.
    GetResource(ctx context.Context, contextPath, resourceName string) (*ResourceOutput, error)

    // ListResources returns all resources in a context path.
    ListResources(ctx context.Context, contextPath string) ([]*ResourceOutput, error)

    // DeleteResource removes a resource from state.
    DeleteResource(ctx context.Context, contextPath, resourceName string) error

    // SavePlan persists an execution plan.
    SavePlan(ctx context.Context, plan *Plan) error

    // GetPlan retrieves an execution plan.
    GetPlan(ctx context.Context, planID string) (*Plan, error)

    // ListPlans lists plans for a context path.
    ListPlans(ctx context.Context, contextPath string, limit int) ([]*Plan, error)

    // Lock acquires an advisory lock for a context path.
    // Prevents concurrent modifications to the same infrastructure.
    Lock(ctx context.Context, contextPath string, ttl time.Duration) (LockHandle, error)

    // Dependencies returns resource names that depend on the given resource.
    Dependencies(ctx context.Context, contextPath, resourceName string) ([]DependencyRef, error)

    // AddDependency records a cross-resource dependency.
    AddDependency(ctx context.Context, dep DependencyRef) error
}

// LockHandle represents an advisory lock.
type LockHandle interface {
    // Unlock releases the lock.
    Unlock(ctx context.Context) error
    // Refresh extends the lock TTL.
    Refresh(ctx context.Context, ttl time.Duration) error
}

// DependencyRef tracks cross-resource and cross-tier dependencies.
type DependencyRef struct {
    SourceContext  string `json:"sourceContext"`  // context path of the depended-upon resource
    SourceResource string `json:"sourceResource"` // resource name
    TargetContext  string `json:"targetContext"`  // context path of the dependent resource
    TargetResource string `json:"targetResource"` // resource name
    Type           string `json:"type"`           // "hard" (must exist) or "soft" (optional)
}
```

---

## 3. Context and Constraint Model

### 3.1 Context Hierarchy

The context chain flows top-down:

```
OrgContext (Tier 0 -- identity only)
  |
  +-- EnvironmentContext (Tier 1 outputs become constraints)
        |
        +-- NamespaceContext (Tier 2 outputs become constraints)
              |
              +-- ApplicationContext (Tier 3 operates within constraints)
```

### 3.2 YAML Configuration Model

A platform configuration file extends the existing `WorkflowConfig`:

```yaml
# platform/configs/production.yaml
platform:
  org: "acme-corp"
  environment: "production"
  provider:
    name: "aws"
    config:
      region: "us-east-1"
      account_id: "${env:AWS_ACCOUNT_ID}"
      credentials: "${vault:aws/production/credentials}"

  tiers:
    infrastructure:
      capabilities:
        - name: "primary-cluster"
          type: "kubernetes_cluster"
          properties:
            version: "1.29"
            node_groups:
              - name: "general"
                instance_type: "m5.xlarge"
                min_size: 3
                max_size: 10
                desired_size: 5
            networking:
              vpc_cidr: "10.0.0.0/16"
              private_subnets: 3
              public_subnets: 3

        - name: "primary-vpc"
          type: "network"
          properties:
            cidr: "10.0.0.0/16"
            availability_zones: 3

      constraints_for_downstream:
        - field: "memory"
          operator: "<="
          value: "4Gi"
        - field: "cpu"
          operator: "<="
          value: "2000m"

    shared_primitives:
      capabilities:
        - name: "app-namespace"
          type: "namespace"
          properties:
            resource_quotas:
              cpu: "8000m"
              memory: "16Gi"
              pods: 50

        - name: "shared-postgres"
          type: "database"
          properties:
            engine: "postgresql"
            version: "15"
            instance_class: "db.r6g.large"
            storage_gb: 100
            multi_az: true

        - name: "event-queue"
          type: "message_queue"
          properties:
            engine: "rabbitmq"
            version: "3.12"

      constraints_for_downstream:
        - field: "replicas"
          operator: "<="
          value: 10
        - field: "database_connections"
          operator: "<="
          value: 50

    application:
      capabilities:
        - name: "api-service"
          type: "container_runtime"
          properties:
            image: "${app.image}"
            replicas: 3
            memory: "512Mi"
            cpu: "500m"
            ports:
              - container_port: 8080
                protocol: "TCP"
            health_check:
              path: "/health"
              interval: "10s"
            ingress:
              host: "api.acme.com"
              port: 443
              tls: true
          dependsOn:
            - "shared-postgres"
            - "event-queue"

  execution:
    tier1_mode: "plan_and_approve"   # requires explicit approval
    tier2_mode: "plan_and_approve"   # requires explicit approval
    tier3_mode: "auto_apply"         # within constraints, auto-apply
    reconciliation_interval: "5m"    # drift detection frequency
    lock_timeout: "10m"              # advisory lock TTL
```

### 3.3 Constraint Propagation Flow

```
Tier 1 workflow executes
  -> Produces: cluster endpoint, VPC ID, IAM roles
  -> Stores outputs in StateStore
  -> Defines constraints_for_downstream

Tier 2 workflow executes
  -> ContextResolver reads Tier 1 outputs
  -> CapabilityMapper validates declarations against Tier 1 constraints
  -> Produces: namespace, DB endpoint, queue URL
  -> Stores outputs in StateStore
  -> Defines constraints_for_downstream

Tier 3 workflow executes
  -> ContextResolver reads Tier 1 + Tier 2 outputs
  -> CapabilityMapper validates against Tier 1 + Tier 2 constraints
  -> Provisions within the "box" defined by upper tiers
  -> Constraint violations are plan-time errors (not runtime)
```

### 3.4 Resolution Strategy

| Tier | Resolution Mode | Rationale |
|------|----------------|-----------|
| Tier 1 (Infrastructure) | Plan-time, approval-gated | Changes are expensive, risky, and affect everything downstream |
| Tier 2 (Shared Primitives) | Plan-time, approval-gated (configurable) | Shared resources need careful coordination |
| Tier 3 (Application) -- deploy | Plan-time, auto-apply within constraints | Frequent deploys within safe boundaries |
| Tier 3 (Application) -- scaling | Continuous reconciliation | Autoscaling needs real-time response |
| All Tiers -- drift detection | Periodic reconciliation | Detect config drift from external changes |

---

## 4. RBAC and Tier Boundary Enforcement

### 4.1 Role Model

```go
// platform/rbac.go

package platform

// TierRole defines what a principal can do within a specific tier.
type TierRole string

const (
    RoleTierAdmin   TierRole = "tier_admin"   // full CRUD on tier resources
    RoleTierAuthor  TierRole = "tier_author"  // create/update workflows for this tier
    RoleTierViewer  TierRole = "tier_viewer"  // read-only access to tier state
    RoleTierApprover TierRole = "tier_approver" // can approve plans for this tier
)

// PlatformRBAC enforces tier-based access control.
type PlatformRBAC interface {
    // CanAuthor checks if a principal can define workflows at the given tier.
    CanAuthor(ctx context.Context, principal string, tier Tier, contextPath string) (bool, error)

    // CanApprove checks if a principal can approve plans at the given tier.
    CanApprove(ctx context.Context, principal string, tier Tier, contextPath string) (bool, error)

    // CanView checks if a principal can read state at the given tier.
    CanView(ctx context.Context, principal string, tier Tier, contextPath string) (bool, error)

    // EnforceConstraints validates that a set of capability declarations
    // does not exceed the constraints imposed by parent tiers.
    // This is called at plan time before any resources are provisioned.
    EnforceConstraints(pctx *PlatformContext, declarations []CapabilityDeclaration) ([]ConstraintViolation, error)
}
```

### 4.2 Enforcement Points

1. **Workflow authoring** -- When a YAML config is loaded, the engine checks if the author has permission for the tier(s) referenced.
2. **Plan generation** -- `CapabilityMapper.ValidateConstraints()` is called for every capability declaration, checking against parent tier constraints stored in state.
3. **Plan approval** -- Tier 1 and Tier 2 plans require explicit approval from a principal with `tier_approver` role.
4. **Runtime reconciliation** -- The reconciliation loop only modifies resources within its tier scope.

---

## 5. State and Drift Management

### 5.1 State Architecture

```
StateStore (abstract interface)
  |
  +-- SQLiteStateStore (local development, single-node)
  |
  +-- PostgresStateStore (production, multi-node)
  |
  +-- S3StateStore (remote state, Terraform-like)
```

Each context path has its own state partition:

```
state/
  acme-corp/
    production/
      tier1.state.json        # Tier 1 resource state
      tier2.state.json        # Tier 2 resource state
      api-service/
        tier3.state.json      # Tier 3 resource state
      worker-service/
        tier3.state.json
    staging/
      tier1.state.json
      ...
```

### 5.2 Drift Detection

Drift detection runs as a reconciliation trigger:

```yaml
triggers:
  reconciliation:
    interval: "5m"
    tiers: [1, 2, 3]
    on_drift: "alert"           # "alert", "auto_remediate", "plan_remediation"
    alert_channel: "slack"
```

The drift detection loop:
1. Read desired state from YAML config
2. Read actual state from provider (`ResourceDriver.Read()`)
3. Compute diff (`ResourceDriver.Diff()`)
4. If drift detected: emit event, optionally create remediation plan

### 5.3 Cross-Tier Dependency Tracking

When Tier 3 resource "api-service" depends on Tier 2 resource "shared-postgres":
- `StateStore.AddDependency()` records the relationship
- When "shared-postgres" changes, `StateStore.Dependencies()` identifies affected Tier 3 resources
- The engine can trigger impact analysis and re-planning for dependent resources

---

## 6. Provider Plugin Architecture

### 6.1 Provider Registration

Providers register with the engine the same way module factories do:

```go
// In engine.go or cmd/server/main.go

engine.RegisterPlatformProvider("aws", aws.NewProvider)
engine.RegisterPlatformProvider("docker-compose", dockercompose.NewProvider)
engine.RegisterPlatformProvider("mock", mock.NewProvider)
```

### 6.2 Resource Driver Composition

Each provider composes multiple resource drivers:

```
aws.Provider
  |-- aws.EKSClusterDriver
  |-- aws.EKSNodeGroupDriver
  |-- aws.VPCDriver
  |-- aws.RDSDriver
  |-- aws.SQSDriver
  |-- aws.IAMRoleDriver
  |-- aws.ALBDriver
  |-- aws.Route53Driver

docker-compose.Provider
  |-- dc.ServiceDriver         (container_runtime -> docker-compose service)
  |-- dc.NetworkDriver         (network -> docker-compose network)
  |-- dc.VolumeDriver          (persistent_volume -> docker-compose volume)
  |-- dc.StubDriver            (IAM, DNS, etc. -> no-op with warning)
```

### 6.3 Capability Mapping Example

Abstract capability `container_runtime` maps differently per provider:

| Property | AWS/EKS | Docker Compose | Local K8s (kind) |
|----------|---------|----------------|-------------------|
| `replicas: 3` | K8s Deployment replicas | `deploy.replicas: 3` | K8s Deployment replicas |
| `memory: 512Mi` | resource limits | `deploy.resources.limits.memory` | resource limits |
| `ingress.host` | ALB + Route53 | localhost port mapping | NodePort + /etc/hosts |
| `ingress.tls` | ACM certificate | self-signed cert (stub) | self-signed cert |
| `health_check` | K8s liveness/readiness probes | Docker healthcheck | K8s probes |

### 6.4 Fidelity Gap Handling

When a provider cannot fully implement a capability:

```go
// platform/fidelity.go

// FidelityReport is returned during plan generation to inform users
// of capability gaps in the current provider.
type FidelityReport struct {
    Capability string        `json:"capability"`
    Provider   string        `json:"provider"`
    Fidelity   FidelityLevel `json:"fidelity"`
    Gaps       []FidelityGap `json:"gaps"`
}

type FidelityGap struct {
    Property    string `json:"property"`
    Description string `json:"description"`
    Workaround  string `json:"workaround,omitempty"` // what the provider does instead
}
```

Example output during `plan` for Docker Compose:
```
Fidelity Report:
  container_runtime "api-service":
    - ingress.tls: STUB - using self-signed cert (production uses ACM)
    - health_check.liveness: PARTIAL - Docker healthcheck (no k8s probe semantics)
  database "shared-postgres":
    - multi_az: STUB - single instance (production uses Multi-AZ RDS)
  network "primary-vpc":
    - availability_zones: STUB - single Docker network (production uses 3 AZs)
```

---

## 7. Workflow Composition and Reusability

### 7.1 Parameterized Templates

```yaml
# templates/microservice.yaml
template:
  name: "microservice"
  version: "1.0.0"
  description: "Standard microservice deployment template"
  parameters:
    - name: "service_name"
      type: "string"
      required: true
    - name: "image"
      type: "string"
      required: true
    - name: "replicas"
      type: "int"
      default: 3
    - name: "memory"
      type: "string"
      default: "512Mi"
    - name: "port"
      type: "int"
      default: 8080
    - name: "ingress_host"
      type: "string"
      required: false

  capabilities:
    - name: "${service_name}"
      type: "container_runtime"
      properties:
        image: "${image}"
        replicas: "${replicas}"
        memory: "${memory}"
        ports:
          - container_port: "${port}"
        health_check:
          path: "/health"
          interval: "10s"

  outputs:
    - name: "service_endpoint"
      value: "${service_name}.endpoint"
    - name: "service_port"
      value: "${port}"
```

### 7.2 Template Registry

```go
// platform/template.go

package platform

// TemplateRegistry manages versioned platform workflow templates.
type TemplateRegistry interface {
    // Register adds or updates a template.
    Register(ctx context.Context, template *WorkflowTemplate) error

    // Get retrieves a specific template version.
    Get(ctx context.Context, name, version string) (*WorkflowTemplate, error)

    // GetLatest retrieves the latest version of a template.
    GetLatest(ctx context.Context, name string) (*WorkflowTemplate, error)

    // List returns all available templates.
    List(ctx context.Context) ([]*WorkflowTemplateSummary, error)

    // Resolve instantiates a template with given parameters.
    Resolve(ctx context.Context, name, version string, params map[string]any) ([]CapabilityDeclaration, error)
}

// WorkflowTemplate is a parameterized, reusable platform workflow definition.
type WorkflowTemplate struct {
    Name         string                   `yaml:"name" json:"name"`
    Version      string                   `yaml:"version" json:"version"`
    Description  string                   `yaml:"description" json:"description"`
    Parameters   []TemplateParameter      `yaml:"parameters" json:"parameters"`
    Capabilities []CapabilityDeclaration  `yaml:"capabilities" json:"capabilities"`
    Outputs      []TemplateOutput         `yaml:"outputs" json:"outputs"`
}

type TemplateParameter struct {
    Name         string `yaml:"name" json:"name"`
    Type         string `yaml:"type" json:"type"`
    Required     bool   `yaml:"required" json:"required"`
    Default      any    `yaml:"default,omitempty" json:"default,omitempty"`
    Description  string `yaml:"description,omitempty" json:"description,omitempty"`
    Validation   string `yaml:"validation,omitempty" json:"validation,omitempty"` // regex or constraint
}

type TemplateOutput struct {
    Name  string `yaml:"name" json:"name"`
    Value string `yaml:"value" json:"value"` // expression referencing resource outputs
}

type WorkflowTemplateSummary struct {
    Name        string   `json:"name"`
    Version     string   `json:"version"`
    Description string   `json:"description"`
    Parameters  []string `json:"parameters"`
}
```

### 7.3 Output Contracts

Every tier workflow declares its output contract:

```yaml
platform:
  tiers:
    shared_primitives:
      capabilities:
        - name: "shared-postgres"
          type: "database"
          properties: { ... }

      # Explicit output contract
      outputs:
        - name: "database_endpoint"
          source: "shared-postgres.endpoint"
          type: "string"
        - name: "database_credential"
          source: "shared-postgres.credentialRef"
          type: "credential_ref"
        - name: "queue_url"
          source: "event-queue.endpoint"
          type: "string"
```

Downstream tiers reference these outputs:

```yaml
platform:
  tiers:
    application:
      capabilities:
        - name: "api-service"
          type: "container_runtime"
          properties:
            env:
              DATABASE_URL: "${tier2.database_endpoint}"
              DATABASE_PASSWORD: "${tier2.database_credential}"
              QUEUE_URL: "${tier2.queue_url}"
```

---

## 8. Implementation Phases

### Phase 1: Context Model and Constraint Propagation (Foundation)

**Goal:** Establish the core type system, context resolver, and constraint validation.

**New Files:**

| File | Purpose |
|------|---------|
| `platform/types.go` | Core types: `Tier`, `ResourceState`, `CapabilityDeclaration`, `Constraint`, `ResourceOutput`, `Plan`, `PlanAction`, `DiffEntry` |
| `platform/provider.go` | `Provider` interface, `CapabilityType`, `PropertySchema`, `FidelityLevel`, `ResourcePlan` |
| `platform/resource_driver.go` | `ResourceDriver` interface, `HealthStatus` |
| `platform/capability_mapper.go` | `CapabilityMapper` interface, `ConstraintViolation` |
| `platform/context_resolver.go` | `PlatformContext` struct, `ContextResolver` interface |
| `platform/credential_broker.go` | `CredentialBroker` interface, `CredentialRequest`, `CredentialRef` |
| `platform/state_store.go` | `StateStore` interface, `LockHandle`, `DependencyRef` |
| `platform/fidelity.go` | `FidelityReport`, `FidelityGap` |
| `platform/template.go` | `TemplateRegistry` interface, `WorkflowTemplate`, `TemplateParameter` |
| `platform/rbac.go` | `PlatformRBAC` interface, `TierRole` |
| `platform/errors.go` | Sentinel errors: `ErrConstraintViolation`, `ErrProviderNotFound`, `ErrResourceNotFound`, `ErrLockConflict`, `ErrTierBoundaryViolation`, `ErrCapabilityUnsupported`, `ErrNotScalable` |
| `platform/context_resolver_impl.go` | `StdContextResolver` -- default implementation that reads state stores and builds `PlatformContext` |
| `platform/constraint_validator.go` | `ValidateConstraints()` function implementing constraint operator logic (`<=`, `>=`, `==`, `in`, `not_in`) |
| `platform/config.go` | `PlatformConfig` YAML struct (new top-level `platform:` section), parsing, and validation |
| `platform/context_resolver_test.go` | Tests for context resolution and constraint propagation |
| `platform/constraint_validator_test.go` | Tests for constraint validation logic |
| `platform/config_test.go` | Tests for platform config parsing |

**Files to Modify:**

| File | Change |
|------|--------|
| `config/config.go` | Add `Platform map[string]any` field to `WorkflowConfig` struct |
| `engine.go` | Add `platformProviders map[string]ProviderFactory` field to `StdEngine`, add `RegisterPlatformProvider()` method, add `buildPlatformFromConfig()` called from `BuildFromConfig()` |
| `schema/module_schema.go` | Add schema definitions for new platform module types |

**Deliverables:**
- All Go interfaces compile and have godoc
- `PlatformContext` can be built from YAML config
- Constraint validation passes unit tests
- `PlatformConfig` parses from YAML with all three tiers

---

### Phase 2: AWS + EKS Reference Provider

**Goal:** Implement one provider deeply to prove the abstraction.

**New Files:**

| File | Purpose |
|------|---------|
| `platform/providers/aws/provider.go` | `AWSProvider` implementing `platform.Provider` |
| `platform/providers/aws/capability_mapper.go` | AWS capability mapper: `container_runtime` -> EKS Deployment, `database` -> RDS, etc. |
| `platform/providers/aws/drivers/eks_cluster.go` | `EKSClusterDriver` implementing `ResourceDriver` |
| `platform/providers/aws/drivers/eks_nodegroup.go` | `EKSNodeGroupDriver` |
| `platform/providers/aws/drivers/vpc.go` | `VPCDriver` |
| `platform/providers/aws/drivers/rds.go` | `RDSDriver` |
| `platform/providers/aws/drivers/sqs.go` | `SQSDriver` |
| `platform/providers/aws/drivers/iam.go` | `IAMRoleDriver` |
| `platform/providers/aws/drivers/alb.go` | `ALBDriver` |
| `platform/providers/aws/credential_broker.go` | AWS credential broker using STS + Secrets Manager |
| `platform/providers/aws/state_store.go` | S3-backed state store for AWS resources |
| `platform/providers/aws/provider_test.go` | Integration tests (mock AWS SDK calls) |
| `platform/providers/aws/drivers/*_test.go` | Unit tests for each driver |

**New Pipeline Steps:**

| File | Purpose |
|------|---------|
| `module/pipeline_step_platform_plan.go` | `step.platform_plan` -- generates execution plan from capability declarations |
| `module/pipeline_step_platform_apply.go` | `step.platform_apply` -- executes an approved plan |
| `module/pipeline_step_platform_destroy.go` | `step.platform_destroy` -- tears down resources |
| `module/pipeline_step_drift_check.go` | `step.drift_check` -- compares desired vs actual state |
| `module/pipeline_step_constraint_check.go` | `step.constraint_check` -- validates constraints before provisioning |

**New Workflow Handler:**

| File | Purpose |
|------|---------|
| `handlers/platform.go` | `PlatformWorkflowHandler` implementing `WorkflowHandler` -- handles `platform` workflow type with plan/apply/destroy actions |
| `handlers/platform_test.go` | Tests for platform workflow handler |

**Files to Modify:**

| File | Change |
|------|--------|
| `engine.go` | Register `step.platform_plan`, `step.platform_apply`, etc. in step registry; wire provider initialization in `BuildFromConfig` |
| `cmd/server/main.go` | Register `PlatformWorkflowHandler`, register built-in providers |

**Deliverables:**
- AWS provider passes unit tests with mocked AWS SDK
- Can generate a plan from YAML capability declarations
- Plan shows create/update/delete actions with diffs
- State is persisted and readable across runs

---

### Phase 3: Tier Boundary Enforcement and RBAC

**Goal:** Implement access control and cross-tier constraint enforcement.

**New Files:**

| File | Purpose |
|------|---------|
| `platform/rbac_impl.go` | `StdPlatformRBAC` -- default RBAC implementation using existing `auth.jwt` module |
| `platform/rbac_test.go` | RBAC unit tests |
| `platform/middleware/tier_boundary.go` | HTTP middleware that validates tier access on platform API calls |
| `platform/middleware/tier_boundary_test.go` | Tests |

**Files to Modify:**

| File | Change |
|------|--------|
| `handlers/platform.go` | Add RBAC checks before plan generation and apply; reject cross-tier operations |
| `platform/context_resolver_impl.go` | Add constraint accumulation from all parent tiers |

**Deliverables:**
- Tier 3 workflow cannot provision Tier 1 resources
- Constraint violations are returned at plan time
- RBAC roles can be assigned per tier per context path
- Approval gate blocks plan execution until approved

---

### Phase 4: Docker Compose as Second Provider (Abstraction Stress Test)

**Goal:** Prove the abstraction works with a fundamentally different backend.

**New Files:**

| File | Purpose |
|------|---------|
| `platform/providers/dockercompose/provider.go` | `DockerComposeProvider` |
| `platform/providers/dockercompose/capability_mapper.go` | Maps capabilities to Docker Compose YAML structures |
| `platform/providers/dockercompose/drivers/service.go` | `ServiceDriver` -- generates/manages docker-compose service definitions |
| `platform/providers/dockercompose/drivers/network.go` | `NetworkDriver` |
| `platform/providers/dockercompose/drivers/volume.go` | `VolumeDriver` |
| `platform/providers/dockercompose/drivers/stub.go` | `StubDriver` -- graceful no-op for unsupported capabilities (IAM, DNS, etc.) |
| `platform/providers/dockercompose/compose_file.go` | Docker Compose YAML generation and parsing |
| `platform/providers/dockercompose/executor.go` | Shell executor for `docker compose up/down/ps` |
| `platform/providers/dockercompose/state_store.go` | File-based state store for local development |
| `platform/providers/dockercompose/provider_test.go` | Tests |
| `platform/providers/dockercompose/drivers/*_test.go` | Tests |

**Files to Modify:**

| File | Change |
|------|--------|
| `cmd/server/main.go` | Register Docker Compose provider |
| `platform/fidelity.go` | Docker Compose fidelity gap declarations |

**Deliverables:**
- Same YAML config that targets AWS can target Docker Compose
- Fidelity report clearly shows capability gaps
- Local development workflow: `plan` -> `apply` -> running Docker Compose stack
- Stub driver logs warnings for unsupported capabilities

---

### Phase 5: State Management and Drift Detection

**Goal:** Production-ready state persistence with drift detection.

**New Files:**

| File | Purpose |
|------|---------|
| `platform/state/sqlite_store.go` | SQLite-backed `StateStore` implementation for single-node use |
| `platform/state/sqlite_store_test.go` | Tests |
| `platform/state/postgres_store.go` | PostgreSQL-backed `StateStore` for production |
| `platform/state/postgres_store_test.go` | Tests |
| `platform/state/migrations/` | Database migration files |
| `platform/reconciler.go` | `Reconciler` -- periodic loop that runs drift detection |
| `platform/reconciler_test.go` | Tests |
| `module/platform_reconciliation_trigger.go` | `ReconciliationTrigger` implementing `module.Trigger` |
| `module/platform_reconciliation_trigger_test.go` | Tests |

**Files to Modify:**

| File | Change |
|------|--------|
| `engine.go` | Add `reconciliation` to `canHandleTrigger()` switch; register reconciliation trigger type |

**Deliverables:**
- State survives engine restarts
- Drift detection runs on schedule and emits events
- Cross-tier dependency tracking works (Tier 1 change identifies affected Tier 2/3)
- Advisory locking prevents concurrent modifications

---

### Phase 6: Template Registry and Workflow Composition

**Goal:** Reusable, versioned workflow templates.

**New Files:**

| File | Purpose |
|------|---------|
| `platform/template_registry_impl.go` | `StdTemplateRegistry` -- file-based and DB-backed implementations |
| `platform/template_registry_test.go` | Tests |
| `platform/template_resolver.go` | Parameter substitution engine for templates |
| `platform/template_resolver_test.go` | Tests |
| `module/pipeline_step_platform_template.go` | `step.platform_template` -- resolves a template into capabilities within a pipeline |
| `example/platform-microservice-template.yaml` | Example template definition |
| `example/platform-deploy-from-template.yaml` | Example usage of template in deployment workflow |

**Files to Modify:**

| File | Change |
|------|--------|
| `engine.go` | Register `step.platform_template` in step registry |
| `handlers/platform.go` | Support `template` action that resolves template + generates plan |

**Deliverables:**
- Templates can be registered, versioned, and resolved
- Parameter substitution works for all property types
- Template outputs feed into downstream capability declarations
- Example YAML configs demonstrate end-to-end template workflow

---

## 9. Team Task Breakdown

The following tasks can be assigned to concurrent agents. Dependencies between tasks are noted.

### Agent 1: Core Types and Interfaces (Phase 1 -- no dependencies)

**Scope:** Create the `platform/` package with all type definitions and interfaces.

**Files to create:**
- `platform/types.go`
- `platform/provider.go`
- `platform/resource_driver.go`
- `platform/capability_mapper.go`
- `platform/context_resolver.go`
- `platform/credential_broker.go`
- `platform/state_store.go`
- `platform/fidelity.go`
- `platform/template.go`
- `platform/rbac.go`
- `platform/errors.go`

**Acceptance criteria:**
- All interfaces compile with `go build ./platform/...`
- Every exported type has godoc
- Run `go vet ./platform/...` clean

---

### Agent 2: Context Resolver and Constraint Validator (Phase 1 -- depends on Agent 1)

**Scope:** Implement the context resolution and constraint validation logic.

**Files to create:**
- `platform/context_resolver_impl.go`
- `platform/constraint_validator.go`
- `platform/context_resolver_test.go`
- `platform/constraint_validator_test.go`

**Acceptance criteria:**
- `StdContextResolver` can build `PlatformContext` from mock state stores
- Constraint operators `<=`, `>=`, `==`, `in`, `not_in` all work
- Memory/CPU string parsing works (e.g., "512Mi" <= "4Gi")
- Tests achieve >90% coverage

---

### Agent 3: Platform Config Parsing (Phase 1 -- depends on Agent 1)

**Scope:** Add `platform:` section to YAML config and implement parsing.

**Files to create:**
- `platform/config.go`
- `platform/config_test.go`

**Files to modify:**
- `config/config.go` -- add `Platform map[string]any` to `WorkflowConfig`

**Acceptance criteria:**
- Platform config round-trips through YAML marshal/unmarshal
- Validation catches missing required fields
- Existing `WorkflowConfig` tests still pass
- Example YAML with all three tiers parses correctly

---

### Agent 4: Platform Workflow Handler and Engine Integration (Phase 2 -- depends on Agents 1-3)

**Scope:** Wire the platform system into the engine and implement the workflow handler.

**Files to create:**
- `handlers/platform.go`
- `handlers/platform_test.go`

**Files to modify:**
- `engine.go` -- add `RegisterPlatformProvider()`, `buildPlatformFromConfig()`, wire new step types
- `cmd/server/main.go` -- register platform handler
- `schema/module_schema.go` -- add platform module schemas

**Acceptance criteria:**
- `PlatformWorkflowHandler.CanHandle("platform")` returns true
- Engine loads platform config and initializes providers
- Plan generation produces correct `PlanAction` list
- Tests cover plan, apply, destroy actions

---

### Agent 5: Pipeline Steps for Platform Operations (Phase 2 -- depends on Agents 1-3)

**Scope:** Implement pipeline steps for platform lifecycle operations.

**Files to create:**
- `module/pipeline_step_platform_plan.go`
- `module/pipeline_step_platform_apply.go`
- `module/pipeline_step_platform_destroy.go`
- `module/pipeline_step_drift_check.go`
- `module/pipeline_step_constraint_check.go`
- Corresponding `*_test.go` files for each

**Acceptance criteria:**
- Each step implements `PipelineStep` interface
- Steps are registered in the `StepRegistry` via factory functions
- `step.platform_plan` produces a `Plan` in pipeline context
- `step.constraint_check` returns violations in pipeline context
- Tests use mock providers

---

### Agent 6: AWS Provider (Phase 2 -- depends on Agents 1, 4)

**Scope:** Implement the AWS reference provider with EKS, VPC, and RDS drivers.

**Files to create:**
- `platform/providers/aws/provider.go`
- `platform/providers/aws/capability_mapper.go`
- `platform/providers/aws/credential_broker.go`
- `platform/providers/aws/state_store.go`
- `platform/providers/aws/drivers/eks_cluster.go`
- `platform/providers/aws/drivers/eks_nodegroup.go`
- `platform/providers/aws/drivers/vpc.go`
- `platform/providers/aws/drivers/rds.go`
- `platform/providers/aws/drivers/sqs.go`
- `platform/providers/aws/drivers/iam.go`
- `platform/providers/aws/drivers/alb.go`
- `platform/providers/aws/provider_test.go`
- `platform/providers/aws/drivers/*_test.go`

**Acceptance criteria:**
- Provider implements all `Provider` interface methods
- Capability mapper handles: `kubernetes_cluster`, `network`, `database`, `message_queue`, `container_runtime`
- All drivers implement `ResourceDriver` with mocked AWS SDK calls
- Credential broker uses STS for scoped credentials
- Tests achieve >80% coverage

---

### Agent 7: Docker Compose Provider (Phase 4 -- depends on Agents 1, 4)

**Scope:** Implement the Docker Compose provider as second provider.

**Files to create:**
- `platform/providers/dockercompose/provider.go`
- `platform/providers/dockercompose/capability_mapper.go`
- `platform/providers/dockercompose/compose_file.go`
- `platform/providers/dockercompose/executor.go`
- `platform/providers/dockercompose/state_store.go`
- `platform/providers/dockercompose/drivers/service.go`
- `platform/providers/dockercompose/drivers/network.go`
- `platform/providers/dockercompose/drivers/volume.go`
- `platform/providers/dockercompose/drivers/stub.go`
- `platform/providers/dockercompose/provider_test.go`
- `platform/providers/dockercompose/drivers/*_test.go`

**Acceptance criteria:**
- Same capability declarations that work with AWS produce valid Docker Compose YAML
- Stub driver handles unsupported capabilities with fidelity reports
- Generated `docker-compose.yml` is syntactically valid
- State is persisted to local file system
- Tests cover all capability types

---

### Agent 8: RBAC and Tier Boundary Enforcement (Phase 3 -- depends on Agents 1, 2)

**Scope:** Implement access control and tier boundary validation.

**Files to create:**
- `platform/rbac_impl.go`
- `platform/rbac_test.go`
- `platform/middleware/tier_boundary.go`
- `platform/middleware/tier_boundary_test.go`

**Acceptance criteria:**
- Tier 3 author cannot create Tier 1 resources
- Constraint violations are returned as structured errors
- RBAC integrates with existing `auth.jwt` module
- Middleware blocks unauthorized tier access on HTTP endpoints

---

### Agent 9: State Stores and Drift Detection (Phase 5 -- depends on Agents 1, 6 or 7)

**Scope:** Implement persistent state stores and the reconciliation loop.

**Files to create:**
- `platform/state/sqlite_store.go`
- `platform/state/sqlite_store_test.go`
- `platform/state/postgres_store.go`
- `platform/state/postgres_store_test.go`
- `platform/state/migrations/001_initial.sql`
- `platform/reconciler.go`
- `platform/reconciler_test.go`
- `module/platform_reconciliation_trigger.go`
- `module/platform_reconciliation_trigger_test.go`

**Files to modify:**
- `engine.go` -- add `reconciliation` trigger type

**Acceptance criteria:**
- SQLite store passes full CRUD + locking tests
- Drift detection correctly identifies added, removed, and changed resources
- Reconciliation trigger fires on configured interval
- Cross-tier dependency tracking identifies impact of Tier 1 changes on Tier 3

---

### Agent 10: Template System (Phase 6 -- depends on Agents 1, 3)

**Scope:** Implement the template registry and parameter resolution.

**Files to create:**
- `platform/template_registry_impl.go`
- `platform/template_registry_test.go`
- `platform/template_resolver.go`
- `platform/template_resolver_test.go`
- `module/pipeline_step_platform_template.go`
- `example/platform-microservice-template.yaml`
- `example/platform-deploy-from-template.yaml`

**Files to modify:**
- `engine.go` -- register `step.platform_template`

**Acceptance criteria:**
- Templates can be registered and retrieved by name/version
- Parameter substitution handles strings, ints, bools, lists, maps
- Missing required parameters produce clear errors
- Example configs parse and resolve correctly

---

## 10. Testing Strategy

### 10.1 Unit Tests

Every file in `platform/` has a corresponding `*_test.go`. Coverage target: **>85%** across the platform package.

Key test patterns:
- **Interface compliance tests**: verify each provider implements all required interface methods
- **Constraint validation**: table-driven tests for all operator types
- **Context resolution**: test context chain with mock state stores
- **Plan generation**: verify correct actions for create, update, delete, no-op scenarios
- **YAML round-trip**: ensure all config types survive marshal/unmarshal

### 10.2 Integration Tests

| Test | Scope | Location |
|------|-------|----------|
| Provider integration | Verify full plan/apply/read cycle with mock SDK | `platform/providers/aws/provider_test.go` |
| Cross-tier propagation | Tier 1 outputs feed into Tier 2 context | `platform/context_resolver_test.go` |
| Constraint enforcement | Tier 3 declaration rejected by Tier 2 constraint | `platform/constraint_validator_test.go` |
| RBAC enforcement | Unauthorized tier access blocked | `platform/rbac_test.go` |
| State persistence | State survives process restart | `platform/state/sqlite_store_test.go` |
| Drift detection | Detect and report resource drift | `platform/reconciler_test.go` |

### 10.3 Mock Provider

A `mock` provider is essential for testing and should be built alongside Phase 1:

```go
// platform/providers/mock/provider.go

// MockProvider implements platform.Provider with in-memory state.
// Used for unit tests and local development without real infrastructure.
type MockProvider struct {
    resources map[string]*platform.ResourceOutput
    plans     []*platform.Plan
    // Configurable behaviors for testing
    FailOnCreate bool
    FailOnUpdate bool
    DriftAfter   time.Duration // simulate drift after this duration
}
```

**File to create:** `platform/providers/mock/provider.go` (part of Agent 1 scope)

### 10.4 Example YAML Configs

| Example | Purpose | File |
|---------|---------|------|
| Platform production deploy | Full three-tier AWS deployment | `example/platform-production.yaml` |
| Platform local dev | Docker Compose equivalent of production | `example/platform-local-dev.yaml` |
| Microservice template | Reusable template | `example/platform-microservice-template.yaml` |
| Template instantiation | Using a template in a workflow | `example/platform-deploy-from-template.yaml` |
| Drift detection pipeline | Scheduled drift check with alerting | `example/platform-drift-detection.yaml` |

---

## 11. Complete File Listing

### New Files (Total: ~70 files)

```
platform/
  types.go
  provider.go
  resource_driver.go
  capability_mapper.go
  context_resolver.go
  context_resolver_impl.go
  credential_broker.go
  state_store.go
  fidelity.go
  template.go
  template_registry_impl.go
  template_resolver.go
  rbac.go
  rbac_impl.go
  errors.go
  config.go
  constraint_validator.go
  reconciler.go

  context_resolver_test.go
  constraint_validator_test.go
  config_test.go
  rbac_test.go
  reconciler_test.go
  template_registry_test.go
  template_resolver_test.go

  middleware/
    tier_boundary.go
    tier_boundary_test.go

  state/
    sqlite_store.go
    sqlite_store_test.go
    postgres_store.go
    postgres_store_test.go
    migrations/
      001_initial.sql

  providers/
    mock/
      provider.go

    aws/
      provider.go
      capability_mapper.go
      credential_broker.go
      state_store.go
      provider_test.go
      drivers/
        eks_cluster.go
        eks_nodegroup.go
        vpc.go
        rds.go
        sqs.go
        iam.go
        alb.go
        eks_cluster_test.go
        eks_nodegroup_test.go
        vpc_test.go
        rds_test.go
        sqs_test.go
        iam_test.go
        alb_test.go

    dockercompose/
      provider.go
      capability_mapper.go
      compose_file.go
      executor.go
      state_store.go
      provider_test.go
      drivers/
        service.go
        network.go
        volume.go
        stub.go
        service_test.go
        network_test.go
        volume_test.go
        stub_test.go

handlers/
  platform.go
  platform_test.go

module/
  pipeline_step_platform_plan.go
  pipeline_step_platform_apply.go
  pipeline_step_platform_destroy.go
  pipeline_step_drift_check.go
  pipeline_step_constraint_check.go
  pipeline_step_platform_template.go
  platform_reconciliation_trigger.go

  pipeline_step_platform_plan_test.go
  pipeline_step_platform_apply_test.go
  pipeline_step_platform_destroy_test.go
  pipeline_step_drift_check_test.go
  pipeline_step_constraint_check_test.go
  pipeline_step_platform_template_test.go
  platform_reconciliation_trigger_test.go

example/
  platform-production.yaml
  platform-local-dev.yaml
  platform-microservice-template.yaml
  platform-deploy-from-template.yaml
  platform-drift-detection.yaml
```

### Files to Modify

| File | Change Summary |
|------|---------------|
| `config/config.go` | Add `Platform map[string]any` field to `WorkflowConfig` |
| `engine.go` | Add provider registry, `RegisterPlatformProvider()`, `buildPlatformFromConfig()`, register new step types, add `reconciliation` trigger type |
| `cmd/server/main.go` | Register `PlatformWorkflowHandler`, register built-in providers (aws, docker-compose, mock) |
| `schema/module_schema.go` | Add schemas for `platform.provider`, `platform.resource`, `platform.context` module types |
| `go.mod` | Add AWS SDK v2 dependency (for AWS provider) |

---

## 12. Open Design Questions

These questions should be resolved during implementation:

1. **State store backend selection.** Should the default state store be SQLite (consistent with the existing `storage.sqlite` module) or should it use the same backend configured for the workflow engine? Recommendation: use SQLite for local/dev and allow PostgreSQL configuration for production via the existing `database.workflow` module.

2. **Plan approval mechanism.** Should approval be an HTTP API call, a CLI command, or integration with external approval systems (GitHub PRs, Slack)? Recommendation: start with HTTP API (`POST /api/v1/platform/plans/{id}/approve`), add external integrations as pipeline steps later.

3. **Provider plugin isolation.** Should providers run in-process or as separate processes (like Terraform providers)? Recommendation: in-process for v1 (simpler, leverages existing Go module system), consider out-of-process for third-party providers later using the existing `dynamic/` Yaegi infrastructure or HashiCorp go-plugin.

4. **Credential rotation lifecycle.** When a credential rotates, should dependent resources automatically restart/reconfigure? Recommendation: emit an event on rotation, let application tier workflows subscribe and handle restarts via the existing event trigger system.

5. **Multi-provider environments.** Can a single environment use multiple providers (e.g., AWS for infrastructure + Cloudflare for DNS)? Recommendation: yes, support this from the start. The `platform.provider` config section should accept a list of providers, and the capability mapper should route declarations to the appropriate provider based on capability type.

---

## 13. Success Criteria

The platform abstraction layer is complete when:

1. A single YAML file can describe a three-tier deployment (infrastructure + shared primitives + application).
2. The same YAML file produces a working deployment against both AWS (with mocked SDK) and Docker Compose (with real local execution).
3. Tier 3 workflows cannot escape constraints imposed by Tier 1 and Tier 2.
4. Drift detection identifies when infrastructure state diverges from the YAML declaration.
5. RBAC prevents unauthorized tier access.
6. Cross-tier dependency tracking correctly identifies the blast radius of infrastructure changes.
7. All tests pass with `go test -race ./platform/...`.
8. The platform extends the existing engine without breaking any existing workflow functionality -- all current tests continue to pass.
