package dockercompose

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/GoCodeAlone/workflow/platform"
	"github.com/GoCodeAlone/workflow/platform/providers/dockercompose/drivers"
)

const (
	// ProviderName is the identifier for the Docker Compose provider.
	ProviderName = "docker-compose"

	// ProviderVersion is the current version of the Docker Compose provider.
	ProviderVersion = "0.1.0"
)

// DockerComposeProvider implements platform.Provider by mapping abstract
// capabilities to Docker Compose services, networks, and volumes. It executes
// docker compose commands via exec.Command and persists state to the local
// filesystem. No Docker SDK dependency is required.
type DockerComposeProvider struct {
	mu         sync.RWMutex
	config     map[string]any
	executor   ComposeExecutor
	mapper     *ComposeCapabilityMapper
	stateStore *FileStateStore
	drivers    map[string]platform.ResourceDriver

	// projectDir is the directory where docker-compose.yml is written.
	projectDir string

	// stateDir is the directory where state JSON files are stored.
	stateDir string

	initialized bool
}

// NewProvider creates a new DockerComposeProvider. This is the ProviderFactory
// function registered with the engine.
func NewProvider() platform.Provider {
	return &DockerComposeProvider{
		executor: NewShellExecutor(),
		mapper:   NewCapabilityMapper(),
		drivers:  make(map[string]platform.ResourceDriver),
	}
}

// NewProviderWithExecutor creates a DockerComposeProvider with a custom executor,
// enabling tests to inject a mock.
func NewProviderWithExecutor(exec ComposeExecutor) *DockerComposeProvider {
	return &DockerComposeProvider{
		executor: exec,
		mapper:   NewCapabilityMapper(),
		drivers:  make(map[string]platform.ResourceDriver),
	}
}

// Name returns the provider identifier.
func (p *DockerComposeProvider) Name() string {
	return ProviderName
}

// Version returns the provider version string.
func (p *DockerComposeProvider) Version() string {
	return ProviderVersion
}

// Initialize validates that Docker is available and sets up the project directory.
// Config keys:
//   - project_dir: directory for docker-compose.yml (default: current directory)
//   - state_dir: directory for state files (default: project_dir/.platform-state)
func (p *DockerComposeProvider) Initialize(ctx context.Context, config map[string]any) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.executor.IsAvailable(ctx); err != nil {
		return fmt.Errorf("docker compose provider initialization failed: %w", err)
	}

	p.config = config

	// Resolve project directory
	p.projectDir = "."
	if dir, ok := config["project_dir"].(string); ok && dir != "" {
		p.projectDir = dir
	}

	// Resolve state directory
	p.stateDir = filepath.Join(p.projectDir, ".platform-state")
	if dir, ok := config["state_dir"].(string); ok && dir != "" {
		p.stateDir = dir
	}

	// Ensure project directory exists
	if err := os.MkdirAll(p.projectDir, 0o750); err != nil {
		return fmt.Errorf("create project directory: %w", err)
	}

	// Initialize state store
	store, err := NewFileStateStore(p.stateDir)
	if err != nil {
		return fmt.Errorf("initialize state store: %w", err)
	}
	p.stateStore = store

	// Register resource drivers
	p.drivers["docker-compose.service"] = drivers.NewServiceDriver(p.executor, p.projectDir)
	p.drivers["docker-compose.network"] = drivers.NewNetworkDriver(p.executor, p.projectDir)
	p.drivers["docker-compose.volume"] = drivers.NewVolumeDriver(p.executor, p.projectDir)
	p.drivers["docker-compose.stub"] = drivers.NewStubDriver()

	p.initialized = true
	return nil
}

// Capabilities returns the set of capability types this provider supports.
func (p *DockerComposeProvider) Capabilities() []platform.CapabilityType {
	return []platform.CapabilityType{
		{
			Name:        CapContainerRuntime,
			Description: "Docker Compose service from a container image",
			Tier:        platform.TierApplication,
			Properties: []platform.PropertySchema{
				{Name: "image", Type: "string", Required: true, Description: "Docker image name"},
				{Name: "replicas", Type: "int", Required: false, Description: "Number of service replicas", DefaultValue: 1},
				{Name: "memory", Type: "string", Required: false, Description: "Memory limit (e.g., 512M)"},
				{Name: "cpu", Type: "string", Required: false, Description: "CPU limit (e.g., 0.5)"},
				{Name: "ports", Type: "list", Required: false, Description: "Port mappings"},
				{Name: "health_check", Type: "map", Required: false, Description: "Container health check"},
				{Name: "env", Type: "map", Required: false, Description: "Environment variables"},
				{Name: "command", Type: "string", Required: false, Description: "Override container command"},
			},
			Fidelity: platform.FidelityPartial,
		},
		{
			Name:        CapDatabase,
			Description: "Database service (PostgreSQL, MySQL, Redis)",
			Tier:        platform.TierSharedPrimitive,
			Properties: []platform.PropertySchema{
				{Name: "engine", Type: "string", Required: false, Description: "Database engine", DefaultValue: "postgresql"},
				{Name: "version", Type: "string", Required: false, Description: "Engine version"},
				{Name: "storage_gb", Type: "int", Required: false, Description: "Storage in GB"},
			},
			Fidelity: platform.FidelityPartial,
		},
		{
			Name:        CapMessageQueue,
			Description: "Message queue service (RabbitMQ, Redis, Kafka)",
			Tier:        platform.TierSharedPrimitive,
			Properties: []platform.PropertySchema{
				{Name: "engine", Type: "string", Required: false, Description: "Queue engine", DefaultValue: "rabbitmq"},
				{Name: "version", Type: "string", Required: false, Description: "Engine version"},
			},
			Fidelity: platform.FidelityPartial,
		},
		{
			Name:        CapNetwork,
			Description: "Docker Compose network",
			Tier:        platform.TierInfrastructure,
			Properties: []platform.PropertySchema{
				{Name: "driver", Type: "string", Required: false, Description: "Network driver", DefaultValue: "bridge"},
				{Name: "cidr", Type: "string", Required: false, Description: "Subnet CIDR"},
			},
			Fidelity: platform.FidelityPartial,
		},
		{
			Name:        CapKubernetesCluster,
			Description: "Kubernetes cluster (stubbed -- not supported in Docker Compose)",
			Tier:        platform.TierInfrastructure,
			Fidelity:    platform.FidelityStub,
		},
		{
			Name:        CapLoadBalancer,
			Description: "Load balancer as nginx or traefik compose service",
			Tier:        platform.TierSharedPrimitive,
			Properties: []platform.PropertySchema{
				{Name: "type", Type: "string", Required: false, Description: "Load balancer type (nginx, traefik)", DefaultValue: "nginx"},
				{Name: "ports", Type: "list", Required: false, Description: "Port mappings"},
			},
			Fidelity: platform.FidelityPartial,
		},
		{
			Name:        CapNamespace,
			Description: "Namespace mapped to a Docker network (approximate)",
			Tier:        platform.TierSharedPrimitive,
			Fidelity:    platform.FidelityStub,
		},
		{
			Name:        CapPersistentVolume,
			Description: "Docker Compose named volume",
			Tier:        platform.TierSharedPrimitive,
			Properties: []platform.PropertySchema{
				{Name: "driver", Type: "string", Required: false, Description: "Volume driver", DefaultValue: "local"},
			},
			Fidelity: platform.FidelityFull,
		},
	}
}

// MapCapability resolves an abstract capability declaration to provider-specific
// resource plans using the capability mapper.
func (p *DockerComposeProvider) MapCapability(ctx context.Context, decl platform.CapabilityDeclaration, pctx *platform.PlatformContext) ([]platform.ResourcePlan, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if !p.initialized {
		return nil, platform.ErrProviderNotInitialized
	}

	if !p.mapper.CanMap(decl.Type) {
		return nil, &platform.CapabilityUnsupportedError{
			Capability: decl.Type,
			Provider:   ProviderName,
		}
	}

	return p.mapper.Map(decl, pctx)
}

// ResourceDriver returns the driver for a specific resource type.
func (p *DockerComposeProvider) ResourceDriver(resourceType string) (platform.ResourceDriver, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	driver, ok := p.drivers[resourceType]
	if !ok {
		return nil, &platform.ResourceDriverNotFoundError{
			ResourceType: resourceType,
			Provider:     ProviderName,
		}
	}
	return driver, nil
}

// CredentialBroker returns nil because Docker Compose does not support
// credential brokering.
func (p *DockerComposeProvider) CredentialBroker() platform.CredentialBroker {
	return nil
}

// StateStore returns the file-system-based state store.
func (p *DockerComposeProvider) StateStore() platform.StateStore {
	return p.stateStore
}

// Healthy returns nil if Docker is reachable.
func (p *DockerComposeProvider) Healthy(ctx context.Context) error {
	return p.executor.IsAvailable(ctx)
}

// Close releases any resources held by the provider.
func (p *DockerComposeProvider) Close() error {
	return nil
}

// ComposeFilePath returns the path to the generated docker-compose.yml.
func (p *DockerComposeProvider) ComposeFilePath() string {
	return filepath.Join(p.projectDir, "docker-compose.yml")
}

// GenerateComposeFile builds a ComposeFile from a set of resource plans and
// writes it to the project directory.
func (p *DockerComposeProvider) GenerateComposeFile(plans []platform.ResourcePlan) (*ComposeFile, error) {
	cf := NewComposeFile()

	for _, plan := range plans {
		switch plan.ResourceType {
		case "docker-compose.service":
			svc := buildComposeService(plan)
			cf.AddService(plan.Name, svc)
		case "docker-compose.network":
			net := buildComposeNetwork(plan)
			cf.AddNetwork(plan.Name, net)
		case "docker-compose.volume":
			vol := buildComposeVolume(plan)
			cf.AddVolume(plan.Name, vol)
		case "docker-compose.stub":
			// Stubs produce no compose output
		}
	}

	return cf, nil
}

// WriteComposeFile writes the compose file to the project directory.
func (p *DockerComposeProvider) WriteComposeFile(cf *ComposeFile) error {
	content, err := cf.MarshalYAML()
	if err != nil {
		return fmt.Errorf("marshal compose file: %w", err)
	}
	path := p.ComposeFilePath()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return fmt.Errorf("write compose file: %w", err)
	}
	return nil
}

// Up runs docker compose up for the project.
func (p *DockerComposeProvider) Up(ctx context.Context) (string, error) {
	return p.executor.Up(ctx, p.projectDir, p.ComposeFilePath())
}

// Down runs docker compose down for the project.
func (p *DockerComposeProvider) Down(ctx context.Context) (string, error) {
	return p.executor.Down(ctx, p.projectDir, p.ComposeFilePath())
}

// FidelityReports returns fidelity gap reports for capabilities that are not
// fully implemented by Docker Compose.
func (p *DockerComposeProvider) FidelityReports(decls []platform.CapabilityDeclaration) []platform.FidelityReport {
	var reports []platform.FidelityReport

	for _, decl := range decls {
		switch decl.Type {
		case CapKubernetesCluster:
			reports = append(reports, platform.FidelityReport{
				Capability: decl.Type,
				Provider:   ProviderName,
				Fidelity:   platform.FidelityStub,
				Gaps: []platform.FidelityGap{
					{
						Property:    "cluster",
						Description: "Docker Compose cannot provision a Kubernetes cluster",
						Workaround:  "Use kind or k3d for local Kubernetes if needed",
					},
				},
			})
		case CapContainerRuntime:
			var gaps []platform.FidelityGap
			if _, ok := decl.Properties["ingress"]; ok {
				gaps = append(gaps, platform.FidelityGap{
					Property:    "ingress.tls",
					Description: "TLS termination uses self-signed certs in Docker Compose",
					Workaround:  "Production uses ACM/cert-manager; local uses plain HTTP or self-signed",
				})
			}
			if _, ok := decl.Properties["health_check"]; ok {
				gaps = append(gaps, platform.FidelityGap{
					Property:    "health_check.liveness",
					Description: "Docker healthcheck has different semantics than Kubernetes probes",
					Workaround:  "Docker HEALTHCHECK is used as an approximation",
				})
			}
			if len(gaps) > 0 {
				reports = append(reports, platform.FidelityReport{
					Capability: decl.Type,
					Provider:   ProviderName,
					Fidelity:   platform.FidelityPartial,
					Gaps:       gaps,
				})
			}
		case CapDatabase:
			if multiAZ, ok := decl.Properties["multi_az"]; ok {
				if b, ok := multiAZ.(bool); ok && b {
					reports = append(reports, platform.FidelityReport{
						Capability: decl.Type,
						Provider:   ProviderName,
						Fidelity:   platform.FidelityPartial,
						Gaps: []platform.FidelityGap{
							{
								Property:    "multi_az",
								Description: "Multi-AZ is not supported in Docker Compose",
								Workaround:  "Single instance used for local development",
							},
						},
					})
				}
			}
		case CapNetwork:
			if _, ok := decl.Properties["availability_zones"]; ok {
				reports = append(reports, platform.FidelityReport{
					Capability: decl.Type,
					Provider:   ProviderName,
					Fidelity:   platform.FidelityPartial,
					Gaps: []platform.FidelityGap{
						{
							Property:    "availability_zones",
							Description: "Docker Compose uses a single bridge network",
							Workaround:  "Single network used for local development",
						},
					},
				})
			}
		case CapNamespace:
			reports = append(reports, platform.FidelityReport{
				Capability: decl.Type,
				Provider:   ProviderName,
				Fidelity:   platform.FidelityStub,
				Gaps: []platform.FidelityGap{
					{
						Property:    "resource_quotas",
						Description: "Resource quotas are not enforced in Docker Compose",
						Workaround:  "Docker resource limits approximate quota enforcement",
					},
				},
			})
		}
	}

	return reports
}

// buildComposeService creates a ComposeService from a ResourcePlan.
func buildComposeService(plan platform.ResourcePlan) *ComposeService {
	svc := &ComposeService{
		Image:   getStr(plan.Properties, "image"),
		Restart: "unless-stopped",
	}

	if cmd := getStr(plan.Properties, "command"); cmd != "" {
		svc.Command = cmd
	}

	// Replicas and resource limits
	replicas, _ := getIntProp(plan.Properties, "replicas")
	memory := getStr(plan.Properties, "memory")
	cpu := getStr(plan.Properties, "cpu")

	if replicas > 0 || memory != "" || cpu != "" {
		svc.Deploy = &DeployConfig{}
		if replicas > 0 {
			svc.Deploy.Replicas = replicas
		}
		if memory != "" || cpu != "" {
			svc.Deploy.Resources = &ResourcesConfig{
				Limits: &ResourceSpec{
					CPUs:   cpu,
					Memory: memory,
				},
			}
		}
	}

	// Ports
	if portsRaw, ok := plan.Properties["ports"]; ok {
		svc.Ports = parsePorts(portsRaw)
	}

	// Environment
	if envRaw, ok := plan.Properties["env"]; ok {
		svc.Environment = parseEnv(envRaw)
	}

	// Health check
	if hcRaw, ok := plan.Properties["health_check"]; ok {
		svc.Healthcheck = parseHealthcheck(hcRaw)
	}

	// Volume mount for database services
	if vol := getStr(plan.Properties, "volume"); vol != "" {
		// Determine mount path based on image
		mountPath := "/data"
		image := svc.Image
		switch {
		case contains(image, "postgres"):
			mountPath = "/var/lib/postgresql/data"
		case contains(image, "mysql"):
			mountPath = "/var/lib/mysql"
		case contains(image, "redis"):
			mountPath = "/data"
		}
		svc.Volumes = []string{vol + ":" + mountPath}
	}

	// DependsOn
	if len(plan.DependsOn) > 0 {
		svc.DependsOn = plan.DependsOn
	}

	return svc
}

// buildComposeNetwork creates a ComposeNetwork from a ResourcePlan.
func buildComposeNetwork(plan platform.ResourcePlan) *ComposeNetwork {
	net := &ComposeNetwork{}

	if driver := getStr(plan.Properties, "driver"); driver != "" {
		net.Driver = driver
	}

	if subnet := getStr(plan.Properties, "subnet"); subnet != "" {
		net.IPAM = &IPAMConfig{
			Config: []IPAMPoolConfig{
				{Subnet: subnet},
			},
		}
	}

	return net
}

// buildComposeVolume creates a ComposeVolume from a ResourcePlan.
func buildComposeVolume(plan platform.ResourcePlan) *ComposeVolume {
	vol := &ComposeVolume{}
	if driver := getStr(plan.Properties, "driver"); driver != "" {
		vol.Driver = driver
	}
	return vol
}

func getStr(props map[string]any, key string) string {
	val, ok := props[key]
	if !ok {
		return ""
	}
	s, ok := val.(string)
	if !ok {
		return ""
	}
	return s
}

func parsePorts(portsRaw any) []string {
	var result []string

	switch ports := portsRaw.(type) {
	case []any:
		for _, p := range ports {
			switch pp := p.(type) {
			case map[string]any:
				containerPort := fmt.Sprintf("%v", pp["container_port"])
				hostPort := containerPort
				if hp, ok := pp["host_port"]; ok {
					hostPort = fmt.Sprintf("%v", hp)
				}
				result = append(result, hostPort+":"+containerPort)
			case string:
				result = append(result, pp)
			}
		}
	case []map[string]any:
		for _, pp := range ports {
			containerPort := fmt.Sprintf("%v", pp["container_port"])
			hostPort := containerPort
			if hp, ok := pp["host_port"]; ok {
				hostPort = fmt.Sprintf("%v", hp)
			}
			result = append(result, hostPort+":"+containerPort)
		}
	}

	return result
}

func parseEnv(envRaw any) map[string]string {
	result := make(map[string]string)
	switch env := envRaw.(type) {
	case map[string]any:
		for k, v := range env {
			result[k] = fmt.Sprintf("%v", v)
		}
	case map[string]string:
		return env
	}
	return result
}

func parseHealthcheck(hcRaw any) *HealthcheckConfig {
	hcMap, ok := hcRaw.(map[string]any)
	if !ok {
		return nil
	}

	hc := &HealthcheckConfig{}

	if path, ok := hcMap["path"].(string); ok {
		hc.Test = []string{"CMD-SHELL", fmt.Sprintf("curl -f http://localhost%s || exit 1", path)}
	}

	if interval, ok := hcMap["interval"].(string); ok {
		hc.Interval = interval
	} else {
		hc.Interval = "30s"
	}

	hc.Timeout = "10s"
	hc.Retries = 3
	hc.StartPeriod = "10s"

	return hc
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsImpl(s, substr))
}

func containsImpl(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
