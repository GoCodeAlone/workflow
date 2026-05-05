---
status: implemented
area: wfctl
owner: workflow
implementation_refs:
  - repo: workflow
    commit: 0f3541f
  - repo: workflow
    commit: 13566fd
  - repo: workflow
    commit: 31d4447
external_refs: []
verification:
  last_checked: 2026-04-25
  commands:
    - 'rg -n "Services|Mesh|Networking|Security|func runCIRun|phase.*deploy" config cmd -S'
    - 'git log --oneline --all -- config/services_config.go config/networking_config.go config/security_config.go cmd/wfctl/ci_run.go'
  result: pass
supersedes: []
superseded_by: []
---

# Tier 2 Platform Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement multi-service architecture (`services:` + `mesh:` sections), networking configuration, security policies, and the full `wfctl ci run deploy` phase with IaC integration.

**Architecture:** New config structs for `services:`, `mesh:`, `networking:`, `security:` top-level YAML keys. Each service compiles to a separate binary. `wfctl ci run --phase deploy` provisions infrastructure via IaC steps, injects secrets, and deploys using configurable strategies. Port detection, TLS, and network policy generation derived from config.

**Tech Stack:** Go 1.26, existing IaC/deploy step types, Docker/k8s for deployment providers

**Design Doc:** `docs/plans/2026-03-28-platform-vision-design.md` (Features 4, 7, 8 + Tier 2 deploy)

---

### Task 1: services: and mesh: config structs

**Files:**
- Create: `config/services_config.go`
- Create: `config/services_config_test.go`
- Modify: `config/config.go` — add Services and Mesh fields to WorkflowConfig

Create config structs for multi-service definitions and inter-service communication.

```go
// config/services_config.go
package config

// ServiceConfig defines a single service within a multi-service application.
type ServiceConfig struct {
	Description string             `json:"description,omitempty" yaml:"description,omitempty"`
	Binary      string             `json:"binary,omitempty" yaml:"binary,omitempty"`
	Scaling     *ScalingConfig     `json:"scaling,omitempty" yaml:"scaling,omitempty"`
	Modules     []ModuleConfig     `json:"modules,omitempty" yaml:"modules,omitempty"`
	Workflows   map[string]any     `json:"workflows,omitempty" yaml:"workflows,omitempty"`
	Pipelines   map[string]any     `json:"pipelines,omitempty" yaml:"pipelines,omitempty"`
	Triggers    map[string]any     `json:"triggers,omitempty" yaml:"triggers,omitempty"`
	Plugins     []string           `json:"plugins,omitempty" yaml:"plugins,omitempty"`
	Expose      []ExposeConfig     `json:"expose,omitempty" yaml:"expose,omitempty"`
}

// ScalingConfig defines how a service scales.
type ScalingConfig struct {
	Replicas int    `json:"replicas,omitempty" yaml:"replicas,omitempty"`
	Min      int    `json:"min,omitempty" yaml:"min,omitempty"`
	Max      int    `json:"max,omitempty" yaml:"max,omitempty"`
	Metric   string `json:"metric,omitempty" yaml:"metric,omitempty"`
	Target   int    `json:"target,omitempty" yaml:"target,omitempty"`
}

// ExposeConfig defines a port that the service exposes.
type ExposeConfig struct {
	Port     int    `json:"port" yaml:"port"`
	Protocol string `json:"protocol,omitempty" yaml:"protocol,omitempty"`
}

// MeshConfig defines inter-service communication.
type MeshConfig struct {
	Transport string            `json:"transport,omitempty" yaml:"transport,omitempty"`
	Discovery string            `json:"discovery,omitempty" yaml:"discovery,omitempty"`
	NATS      *MeshNATSConfig   `json:"nats,omitempty" yaml:"nats,omitempty"`
	Routes    []MeshRouteConfig `json:"routes,omitempty" yaml:"routes,omitempty"`
}

// MeshNATSConfig holds NATS-specific mesh configuration.
type MeshNATSConfig struct {
	URL       string `json:"url" yaml:"url"`
	ClusterID string `json:"clusterId,omitempty" yaml:"clusterId,omitempty"`
}

// MeshRouteConfig declares a communication path between services.
type MeshRouteConfig struct {
	From     string `json:"from" yaml:"from"`
	To       string `json:"to" yaml:"to"`
	Via      string `json:"via" yaml:"via"`
	Subject  string `json:"subject,omitempty" yaml:"subject,omitempty"`
	Endpoint string `json:"endpoint,omitempty" yaml:"endpoint,omitempty"`
}
```

Add to WorkflowConfig:
```go
Services map[string]*ServiceConfig `json:"services,omitempty" yaml:"services,omitempty"`
Mesh     *MeshConfig               `json:"mesh,omitempty" yaml:"mesh,omitempty"`
```

Write test that parses a multi-service YAML with mesh routes and validates all fields.

Run: `go test ./config/ -run TestServices -v`
Commit: `feat: add services: and mesh: config sections for multi-service architecture`

---

### Task 2: networking: and security: config structs

**Files:**
- Create: `config/networking_config.go`
- Create: `config/security_config.go`
- Create: `config/networking_config_test.go`
- Modify: `config/config.go` — add Networking and Security fields

```go
// config/networking_config.go
package config

// NetworkingConfig defines network exposure and policies.
type NetworkingConfig struct {
	Ingress  []IngressConfig  `json:"ingress,omitempty" yaml:"ingress,omitempty"`
	Policies []NetworkPolicy  `json:"policies,omitempty" yaml:"policies,omitempty"`
	DNS      *DNSConfig       `json:"dns,omitempty" yaml:"dns,omitempty"`
}

// IngressConfig defines an externally-accessible endpoint.
type IngressConfig struct {
	Service      string     `json:"service,omitempty" yaml:"service,omitempty"`
	Port         int        `json:"port" yaml:"port"`
	ExternalPort int        `json:"externalPort,omitempty" yaml:"externalPort,omitempty"`
	Protocol     string     `json:"protocol,omitempty" yaml:"protocol,omitempty"`
	Path         string     `json:"path,omitempty" yaml:"path,omitempty"`
	TLS          *TLSConfig `json:"tls,omitempty" yaml:"tls,omitempty"`
}

// TLSConfig defines TLS termination.
type TLSConfig struct {
	Provider   string `json:"provider,omitempty" yaml:"provider,omitempty"`
	Domain     string `json:"domain,omitempty" yaml:"domain,omitempty"`
	MinVersion string `json:"minVersion,omitempty" yaml:"minVersion,omitempty"`
}

// NetworkPolicy defines allowed communication between services.
type NetworkPolicy struct {
	From string   `json:"from" yaml:"from"`
	To   []string `json:"to" yaml:"to"`
}

// DNSConfig defines DNS management.
type DNSConfig struct {
	Provider string      `json:"provider,omitempty" yaml:"provider,omitempty"`
	Zone     string      `json:"zone,omitempty" yaml:"zone,omitempty"`
	Records  []DNSRecord `json:"records,omitempty" yaml:"records,omitempty"`
}

// DNSRecord is a single DNS record.
type DNSRecord struct {
	Name   string `json:"name" yaml:"name"`
	Type   string `json:"type" yaml:"type"`
	Target string `json:"target" yaml:"target"`
}
```

```go
// config/security_config.go
package config

// SecurityConfig defines security policies for the application.
type SecurityConfig struct {
	TLS      *SecurityTLSConfig      `json:"tls,omitempty" yaml:"tls,omitempty"`
	Network  *SecurityNetworkConfig  `json:"network,omitempty" yaml:"network,omitempty"`
	Identity *SecurityIdentityConfig `json:"identity,omitempty" yaml:"identity,omitempty"`
	Runtime  *SecurityRuntimeConfig  `json:"runtime,omitempty" yaml:"runtime,omitempty"`
	Scanning *SecurityScanningConfig `json:"scanning,omitempty" yaml:"scanning,omitempty"`
}

// SecurityTLSConfig defines TLS requirements.
type SecurityTLSConfig struct {
	Internal   bool   `json:"internal,omitempty" yaml:"internal,omitempty"`
	External   bool   `json:"external,omitempty" yaml:"external,omitempty"`
	Provider   string `json:"provider,omitempty" yaml:"provider,omitempty"`
	MinVersion string `json:"minVersion,omitempty" yaml:"minVersion,omitempty"`
}

// SecurityNetworkConfig defines network isolation policy.
type SecurityNetworkConfig struct {
	DefaultPolicy string `json:"defaultPolicy,omitempty" yaml:"defaultPolicy,omitempty"`
}

// SecurityIdentityConfig defines service identity management.
type SecurityIdentityConfig struct {
	Provider   string `json:"provider,omitempty" yaml:"provider,omitempty"`
	PerService bool   `json:"perService,omitempty" yaml:"perService,omitempty"`
}

// SecurityRuntimeConfig defines container runtime security.
type SecurityRuntimeConfig struct {
	ReadOnlyFilesystem bool     `json:"readOnlyFilesystem,omitempty" yaml:"readOnlyFilesystem,omitempty"`
	NoNewPrivileges    bool     `json:"noNewPrivileges,omitempty" yaml:"noNewPrivileges,omitempty"`
	RunAsNonRoot       bool     `json:"runAsNonRoot,omitempty" yaml:"runAsNonRoot,omitempty"`
	DropCapabilities   []string `json:"dropCapabilities,omitempty" yaml:"dropCapabilities,omitempty"`
	AddCapabilities    []string `json:"addCapabilities,omitempty" yaml:"addCapabilities,omitempty"`
}

// SecurityScanningConfig defines automated security scanning.
type SecurityScanningConfig struct {
	ContainerScan  bool `json:"containerScan,omitempty" yaml:"containerScan,omitempty"`
	DependencyScan bool `json:"dependencyScan,omitempty" yaml:"dependencyScan,omitempty"`
	SAST           bool `json:"sast,omitempty" yaml:"sast,omitempty"`
}
```

Add to WorkflowConfig:
```go
Networking *NetworkingConfig `json:"networking,omitempty" yaml:"networking,omitempty"`
Security   *SecurityConfig   `json:"security,omitempty" yaml:"security,omitempty"`
```

Write test. Run: `go test ./config/ -run TestNetworking -v`
Commit: `feat: add networking: and security: config sections`

---

### Task 3: wfctl ports and wfctl security commands

**Files:**
- Create: `cmd/wfctl/ports.go`
- Create: `cmd/wfctl/security_cmd.go`
- Modify: `cmd/wfctl/main.go` — register commands

`wfctl ports list` scans the parsed config for all port-bearing modules (http.server, websocket.server, messaging.nats, database.postgres, etc.) and prints a table with service, module, port, protocol, exposure (public/internal).

`wfctl security audit` scans config and reports: TLS status, network policy coverage, missing rate limiting, weak auth patterns.

`wfctl security generate-network-policies --output k8s/` generates Kubernetes NetworkPolicy YAML from the networking.policies + mesh.routes config.

Run: `go test ./cmd/wfctl/ -run TestPorts -v && go test ./cmd/wfctl/ -run TestSecurity -v`
Commit: `feat: wfctl ports list + security audit/generate commands`

---

### Task 4: wfctl ci run deploy phase — real deployment

**Files:**
- Modify: `cmd/wfctl/ci_run.go` — replace deploy stub with real implementation
- Create: `cmd/wfctl/deploy_providers.go` — provider interface + kubernetes/docker providers
- Create: `cmd/wfctl/deploy_providers_test.go`

Replace the `runDeployPhase` stub with real deployment logic:

1. **Pre-deploy**: if `preDeploy` steps listed, execute them (IaC plan/apply via wfctl infra)
2. **Secret injection**: fetch secrets from configured provider, inject as env vars or k8s secrets
3. **Deploy**: based on provider:
   - `kubernetes`: generate/apply k8s manifests (Deployment, Service, ConfigMap) from config
   - `docker`: docker-compose up with generated compose file
   - `aws-ecs`: generate ECS task definition + service update (stub for now, uses AWS SDK later)
4. **Health check**: poll the health endpoint until healthy or timeout
5. **Strategy**: rolling (default), blue-green, canary — for k8s, this maps to k8s deployment strategies

```go
// DeployProvider handles deploying to a specific infrastructure provider.
type DeployProvider interface {
	Deploy(ctx context.Context, cfg DeployConfig) error
	HealthCheck(ctx context.Context, cfg DeployConfig) error
}

type kubernetesProvider struct{}
type dockerProvider struct{}
```

Run: `go test ./cmd/wfctl/ -run TestDeploy -v`
Commit: `feat: wfctl ci run deploy — kubernetes + docker providers, health checks`

---

### Task 5: Multi-service build support in wfctl ci run

**Files:**
- Modify: `cmd/wfctl/ci_run.go` — detect services: section, build each service's binary
- Create: `cmd/wfctl/ci_multiservice.go` — service-aware build/deploy logic

When the config has a `services:` section, `wfctl ci run` builds each service's binary separately:

```go
func runMultiServiceBuild(services map[string]*config.ServiceConfig, verbose bool) error {
	for name, svc := range services {
		if svc.Binary == "" {
			continue
		}
		fmt.Printf("Building service %s (%s)...\n", name, svc.Binary)
		// Cross-compile the binary using the same logic as single-service build
	}
	return nil
}
```

For deploy, each service gets its own k8s Deployment/Service with the correct scaling config, exposed ports, and resource limits.

Run: `go test ./cmd/wfctl/ -run TestMultiService -v`
Commit: `feat: multi-service build and deploy in wfctl ci run`

---

### Task 6: Validation for services, mesh, networking, security

**Files:**
- Create: `config/services_config_validate.go`
- Create: `config/networking_config_validate.go`
- Modify: `cmd/wfctl/validate.go` — wire new validations

Validate:
- `services:` — each service has a unique name, binary path if specified, valid scaling config
- `mesh.routes` — `from`/`to` reference existing services, `via` is valid (nats/http)
- `networking.ingress` — references valid services and ports that are actually exposed
- `networking.policies` — `from` references valid services
- `security.tls` — provider is valid (letsencrypt/manual/acm/cloudflare)
- Cross-validation: if a service exposes port 8080 but no ingress routes to it, warn

Run: `go test ./config/ -run TestValidate -v`
Commit: `feat: validate services, mesh, networking, security config sections`

---

### Task 7: Documentation + schema updates

**Files:**
- Modify: `docs/dsl-reference.md` — add services:, mesh:, networking:, security: sections
- Modify: `cmd/wfctl/dsl-reference-embedded.md` — same
- Modify: `docs/WFCTL.md` — add ports list, security audit/generate commands
- Modify: `CHANGELOG.md`
- Modify: `schema/schema.go` — extend JSON Schema generation for new sections

Commit: `docs: services/mesh/networking/security YAML sections, wfctl ports + security commands`

---

## Summary

| Task | Scope | Key Deliverable |
|------|-------|-----------------|
| 1 | Config | services: + mesh: structs, multi-service YAML parsing |
| 2 | Config | networking: + security: structs |
| 3 | CLI | wfctl ports list + wfctl security audit/generate |
| 4 | CLI | wfctl ci run deploy — kubernetes + docker providers |
| 5 | CLI | Multi-service build + deploy support |
| 6 | Validation | Cross-section validation (services↔mesh↔networking) |
| 7 | Docs | DSL reference, WFCTL docs, schema, changelog |
