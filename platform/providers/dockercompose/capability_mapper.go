package dockercompose

import (
	"fmt"
	"strings"

	"github.com/GoCodeAlone/workflow/platform"
)

// Capability type constants used for mapping abstract declarations to compose resources.
const (
	CapContainerRuntime  = "container_runtime"
	CapDatabase          = "database"
	CapMessageQueue      = "message_queue"
	CapNetwork           = "network"
	CapKubernetesCluster = "kubernetes_cluster"
	CapLoadBalancer      = "load_balancer"
	CapNamespace         = "namespace"
	CapPersistentVolume  = "persistent_volume"
)

// ComposeCapabilityMapper maps abstract capability declarations to Docker Compose
// resource plans. It understands how to convert capability-level abstractions
// into the concrete services, networks, and volumes that Docker Compose manages.
type ComposeCapabilityMapper struct{}

// NewCapabilityMapper creates a new ComposeCapabilityMapper.
func NewCapabilityMapper() *ComposeCapabilityMapper {
	return &ComposeCapabilityMapper{}
}

// CanMap returns true if this mapper can handle the given capability type.
func (m *ComposeCapabilityMapper) CanMap(capabilityType string) bool {
	switch capabilityType {
	case CapContainerRuntime, CapDatabase, CapMessageQueue, CapNetwork,
		CapKubernetesCluster, CapLoadBalancer, CapNamespace, CapPersistentVolume:
		return true
	}
	return false
}

// Map translates a capability declaration into one or more resource plans.
func (m *ComposeCapabilityMapper) Map(decl platform.CapabilityDeclaration, _ *platform.PlatformContext) ([]platform.ResourcePlan, error) {
	switch decl.Type {
	case CapContainerRuntime:
		return m.mapContainerRuntime(decl)
	case CapDatabase:
		return m.mapDatabase(decl)
	case CapMessageQueue:
		return m.mapMessageQueue(decl)
	case CapNetwork:
		return m.mapNetwork(decl)
	case CapKubernetesCluster:
		return m.mapKubernetesCluster(decl)
	case CapLoadBalancer:
		return m.mapLoadBalancer(decl)
	case CapNamespace:
		return m.mapNamespace(decl)
	case CapPersistentVolume:
		return m.mapPersistentVolume(decl)
	default:
		return nil, &platform.CapabilityUnsupportedError{
			Capability: decl.Type,
			Provider:   "docker-compose",
		}
	}
}

// ValidateConstraints checks if a capability declaration satisfies all constraints.
func (m *ComposeCapabilityMapper) ValidateConstraints(decl platform.CapabilityDeclaration, constraints []platform.Constraint) []platform.ConstraintViolation {
	var violations []platform.ConstraintViolation
	for _, c := range constraints {
		val, ok := decl.Properties[c.Field]
		if !ok {
			continue
		}
		if !checkConstraint(c.Operator, val, c.Value) {
			violations = append(violations, platform.ConstraintViolation{
				Constraint: c,
				Actual:     val,
				Message:    fmt.Sprintf("property %q value %v violates constraint %s %v", c.Field, val, c.Operator, c.Value),
			})
		}
	}
	return violations
}

func (m *ComposeCapabilityMapper) mapContainerRuntime(decl platform.CapabilityDeclaration) ([]platform.ResourcePlan, error) {
	props := make(map[string]any)

	image, _ := getStringProp(decl.Properties, "image")
	if image == "" {
		return nil, fmt.Errorf("container_runtime %q: 'image' property is required", decl.Name)
	}
	props["image"] = image

	if replicas, ok := getIntProp(decl.Properties, "replicas"); ok {
		props["replicas"] = replicas
	}
	if memory, ok := getStringProp(decl.Properties, "memory"); ok {
		props["memory"] = memory
	}
	if cpu, ok := getStringProp(decl.Properties, "cpu"); ok {
		props["cpu"] = cpu
	}
	if ports, ok := decl.Properties["ports"]; ok {
		props["ports"] = ports
	}
	if healthCheck, ok := decl.Properties["health_check"]; ok {
		props["health_check"] = healthCheck
	}
	if env, ok := decl.Properties["env"]; ok {
		props["env"] = env
	}
	if cmd, ok := getStringProp(decl.Properties, "command"); ok {
		props["command"] = cmd
	}

	plans := []platform.ResourcePlan{
		{
			ResourceType: "docker-compose.service",
			Name:         decl.Name,
			Properties:   props,
			DependsOn:    decl.DependsOn,
		},
	}
	return plans, nil
}

func (m *ComposeCapabilityMapper) mapDatabase(decl platform.CapabilityDeclaration) ([]platform.ResourcePlan, error) {
	props := make(map[string]any)

	engine, _ := getStringProp(decl.Properties, "engine")
	if engine == "" {
		engine = "postgresql"
	}

	version, _ := getStringProp(decl.Properties, "version")

	image, env := databaseImageAndEnv(engine, version)
	props["image"] = image
	props["env"] = env

	// Map port based on engine
	props["ports"] = databasePorts(engine)

	if storageGB, ok := getIntProp(decl.Properties, "storage_gb"); ok {
		props["storage_gb"] = storageGB
		props["volume"] = fmt.Sprintf("%s-data", decl.Name)
	}

	plans := []platform.ResourcePlan{
		{
			ResourceType: "docker-compose.service",
			Name:         decl.Name,
			Properties:   props,
			DependsOn:    decl.DependsOn,
		},
	}

	// Add a volume for database persistence
	plans = append(plans, platform.ResourcePlan{
		ResourceType: "docker-compose.volume",
		Name:         decl.Name + "-data",
		Properties:   map[string]any{"driver": "local"},
	})

	return plans, nil
}

func (m *ComposeCapabilityMapper) mapMessageQueue(decl platform.CapabilityDeclaration) ([]platform.ResourcePlan, error) {
	props := make(map[string]any)

	engine, _ := getStringProp(decl.Properties, "engine")
	if engine == "" {
		engine = "rabbitmq"
	}

	version, _ := getStringProp(decl.Properties, "version")

	image, env, ports := messageQueueImageEnvPorts(engine, version)
	props["image"] = image
	props["env"] = env
	props["ports"] = ports

	plans := []platform.ResourcePlan{
		{
			ResourceType: "docker-compose.service",
			Name:         decl.Name,
			Properties:   props,
			DependsOn:    decl.DependsOn,
		},
	}
	return plans, nil
}

func (m *ComposeCapabilityMapper) mapNetwork(decl platform.CapabilityDeclaration) ([]platform.ResourcePlan, error) {
	props := make(map[string]any)

	driver, _ := getStringProp(decl.Properties, "driver")
	if driver == "" {
		driver = "bridge"
	}
	props["driver"] = driver

	if cidr, ok := getStringProp(decl.Properties, "cidr"); ok {
		props["subnet"] = cidr
	}

	return []platform.ResourcePlan{
		{
			ResourceType: "docker-compose.network",
			Name:         decl.Name,
			Properties:   props,
		},
	}, nil
}

func (m *ComposeCapabilityMapper) mapKubernetesCluster(decl platform.CapabilityDeclaration) ([]platform.ResourcePlan, error) {
	// Kubernetes cluster is stubbed for Docker Compose -- it cannot faithfully
	// reproduce a real K8s cluster.
	return []platform.ResourcePlan{
		{
			ResourceType: "docker-compose.stub",
			Name:         decl.Name,
			Properties: map[string]any{
				"original_type": CapKubernetesCluster,
				"stub_reason":   "Docker Compose cannot provision a Kubernetes cluster",
				"fidelity":      string(platform.FidelityStub),
			},
		},
	}, nil
}

func (m *ComposeCapabilityMapper) mapLoadBalancer(decl platform.CapabilityDeclaration) ([]platform.ResourcePlan, error) {
	props := make(map[string]any)

	lbType, _ := getStringProp(decl.Properties, "type")
	if lbType == "" || lbType == "nginx" {
		props["image"] = "nginx:alpine"
	} else {
		props["image"] = "traefik:v3.0"
	}

	if ports, ok := decl.Properties["ports"]; ok {
		props["ports"] = ports
	} else {
		props["ports"] = []map[string]any{
			{"container_port": 80, "host_port": 80},
			{"container_port": 443, "host_port": 443},
		}
	}

	return []platform.ResourcePlan{
		{
			ResourceType: "docker-compose.service",
			Name:         decl.Name,
			Properties:   props,
			DependsOn:    decl.DependsOn,
		},
	}, nil
}

func (m *ComposeCapabilityMapper) mapNamespace(decl platform.CapabilityDeclaration) ([]platform.ResourcePlan, error) {
	// Namespaces map to a Docker network in compose (loose approximation).
	props := map[string]any{
		"driver":        "bridge",
		"original_type": CapNamespace,
	}
	return []platform.ResourcePlan{
		{
			ResourceType: "docker-compose.network",
			Name:         decl.Name,
			Properties:   props,
		},
	}, nil
}

func (m *ComposeCapabilityMapper) mapPersistentVolume(decl platform.CapabilityDeclaration) ([]platform.ResourcePlan, error) {
	props := make(map[string]any)
	driver, _ := getStringProp(decl.Properties, "driver")
	if driver == "" {
		driver = "local"
	}
	props["driver"] = driver

	return []platform.ResourcePlan{
		{
			ResourceType: "docker-compose.volume",
			Name:         decl.Name,
			Properties:   props,
		},
	}, nil
}

// Helper functions

func getStringProp(props map[string]any, key string) (string, bool) {
	val, ok := props[key]
	if !ok {
		return "", false
	}
	s, ok := val.(string)
	return s, ok
}

func getIntProp(props map[string]any, key string) (int, bool) {
	val, ok := props[key]
	if !ok {
		return 0, false
	}
	switch v := val.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	}
	return 0, false
}

func databaseImageAndEnv(engine, version string) (string, map[string]string) {
	switch strings.ToLower(engine) {
	case "postgresql", "postgres":
		tag := "15"
		if version != "" {
			tag = version
		}
		return "postgres:" + tag, map[string]string{
			"POSTGRES_DB":       "app",
			"POSTGRES_USER":     "app",
			"POSTGRES_PASSWORD": "localdev",
		}
	case "mysql":
		tag := "8.0"
		if version != "" {
			tag = version
		}
		return "mysql:" + tag, map[string]string{
			"MYSQL_DATABASE":      "app",
			"MYSQL_USER":          "app",
			"MYSQL_PASSWORD":      "localdev",
			"MYSQL_ROOT_PASSWORD": "localdev",
		}
	case "redis":
		tag := "7"
		if version != "" {
			tag = version
		}
		return "redis:" + tag, nil
	default:
		tag := "latest"
		if version != "" {
			tag = version
		}
		return engine + ":" + tag, nil
	}
}

func databasePorts(engine string) []map[string]any {
	switch strings.ToLower(engine) {
	case "postgresql", "postgres":
		return []map[string]any{{"container_port": 5432, "host_port": 5432}}
	case "mysql":
		return []map[string]any{{"container_port": 3306, "host_port": 3306}}
	case "redis":
		return []map[string]any{{"container_port": 6379, "host_port": 6379}}
	default:
		return nil
	}
}

func messageQueueImageEnvPorts(engine, version string) (string, map[string]string, []map[string]any) {
	switch strings.ToLower(engine) {
	case "rabbitmq":
		tag := "3.12-management"
		if version != "" {
			tag = version + "-management"
		}
		return "rabbitmq:" + tag,
			map[string]string{
				"RABBITMQ_DEFAULT_USER": "guest",
				"RABBITMQ_DEFAULT_PASS": "guest",
			},
			[]map[string]any{
				{"container_port": 5672, "host_port": 5672},
				{"container_port": 15672, "host_port": 15672},
			}
	case "redis":
		tag := "7"
		if version != "" {
			tag = version
		}
		return "redis:" + tag, nil,
			[]map[string]any{
				{"container_port": 6379, "host_port": 6379},
			}
	case "kafka":
		tag := "latest"
		if version != "" {
			tag = version
		}
		return "confluentinc/cp-kafka:" + tag,
			map[string]string{
				"KAFKA_BROKER_ID":                        "1",
				"KAFKA_ZOOKEEPER_CONNECT":                "zookeeper:2181",
				"KAFKA_ADVERTISED_LISTENERS":             "PLAINTEXT://localhost:9092",
				"KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR": "1",
			},
			[]map[string]any{
				{"container_port": 9092, "host_port": 9092},
			}
	default:
		tag := "latest"
		if version != "" {
			tag = version
		}
		return engine + ":" + tag, nil, nil
	}
}

func checkConstraint(op string, actual, limit any) bool {
	// Simple numeric comparison for common constraint checks
	actualNum, actualOk := toFloat64(actual)
	limitNum, limitOk := toFloat64(limit)

	if actualOk && limitOk {
		switch op {
		case "<=":
			return actualNum <= limitNum
		case ">=":
			return actualNum >= limitNum
		case "==":
			return actualNum == limitNum
		}
	}

	// String equality fallback
	if op == "==" {
		return fmt.Sprintf("%v", actual) == fmt.Sprintf("%v", limit)
	}

	return true
}

func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	case float32:
		return float64(n), true
	}
	return 0, false
}
