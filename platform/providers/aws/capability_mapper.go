//go:build aws

package aws

import (
	"fmt"

	"github.com/GoCodeAlone/workflow/platform"
)

// AWSCapabilityMapper maps abstract capability declarations to AWS resource plans.
type AWSCapabilityMapper struct{}

// NewAWSCapabilityMapper creates a new capability mapper for AWS.
func NewAWSCapabilityMapper() *AWSCapabilityMapper {
	return &AWSCapabilityMapper{}
}

func (m *AWSCapabilityMapper) CanMap(capabilityType string) bool {
	switch capabilityType {
	case "kubernetes_cluster", "network", "database", "message_queue",
		"container_runtime", "load_balancer":
		return true
	}
	return false
}

func (m *AWSCapabilityMapper) Map(decl platform.CapabilityDeclaration, pctx *platform.PlatformContext) ([]platform.ResourcePlan, error) {
	switch decl.Type {
	case "kubernetes_cluster":
		return m.mapKubernetesCluster(decl)
	case "network":
		return m.mapNetwork(decl)
	case "database":
		return m.mapDatabase(decl)
	case "message_queue":
		return m.mapMessageQueue(decl)
	case "container_runtime":
		return m.mapContainerRuntime(decl, pctx)
	case "load_balancer":
		return m.mapLoadBalancer(decl, pctx)
	default:
		return nil, &platform.CapabilityUnsupportedError{
			Capability: decl.Type,
			Provider:   ProviderName,
		}
	}
}

func (m *AWSCapabilityMapper) ValidateConstraints(decl platform.CapabilityDeclaration, constraints []platform.Constraint) []platform.ConstraintViolation {
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
				Message:    fmt.Sprintf("property %q value %v violates constraint %s %v from %s", c.Field, val, c.Operator, c.Value, c.Source),
			})
		}
	}
	return violations
}

func (m *AWSCapabilityMapper) mapKubernetesCluster(decl platform.CapabilityDeclaration) ([]platform.ResourcePlan, error) {
	version, _ := decl.Properties["version"].(string)
	if version == "" {
		version = "1.29"
	}
	nodeCount := intProp(decl.Properties, "node_count", 2)
	instanceType, _ := decl.Properties["instance_type"].(string)
	if instanceType == "" {
		instanceType = "t3.medium"
	}

	clusterName := decl.Name + "-eks"
	nodeGroupName := decl.Name + "-nodes"

	return []platform.ResourcePlan{
		{
			ResourceType: "aws.eks_cluster",
			Name:         clusterName,
			Properties: map[string]any{
				"version": version,
				"name":    clusterName,
			},
			DependsOn: decl.DependsOn,
		},
		{
			ResourceType: "aws.eks_nodegroup",
			Name:         nodeGroupName,
			Properties: map[string]any{
				"cluster_name":  clusterName,
				"node_count":    nodeCount,
				"instance_type": instanceType,
			},
			DependsOn: []string{clusterName},
		},
	}, nil
}

func (m *AWSCapabilityMapper) mapNetwork(decl platform.CapabilityDeclaration) ([]platform.ResourcePlan, error) {
	cidr, _ := decl.Properties["cidr"].(string)
	if cidr == "" {
		return nil, fmt.Errorf("aws: network capability requires 'cidr' property")
	}
	enableNAT := boolProp(decl.Properties, "enable_nat", true)

	return []platform.ResourcePlan{
		{
			ResourceType: "aws.vpc",
			Name:         decl.Name + "-vpc",
			Properties: map[string]any{
				"cidr":       cidr,
				"enable_nat": enableNAT,
				"name":       decl.Name + "-vpc",
			},
			DependsOn: decl.DependsOn,
		},
	}, nil
}

func (m *AWSCapabilityMapper) mapDatabase(decl platform.CapabilityDeclaration) ([]platform.ResourcePlan, error) {
	engine, _ := decl.Properties["engine"].(string)
	if engine == "" {
		return nil, fmt.Errorf("aws: database capability requires 'engine' property")
	}
	engineVersion, _ := decl.Properties["engine_version"].(string)
	instanceClass, _ := decl.Properties["instance_class"].(string)
	if instanceClass == "" {
		instanceClass = "db.t3.micro"
	}
	allocatedStorage := intProp(decl.Properties, "allocated_storage", 20)
	multiAZ := boolProp(decl.Properties, "multi_az", false)

	return []platform.ResourcePlan{
		{
			ResourceType: "aws.rds",
			Name:         decl.Name + "-rds",
			Properties: map[string]any{
				"engine":            engine,
				"engine_version":    engineVersion,
				"instance_class":    instanceClass,
				"allocated_storage": allocatedStorage,
				"multi_az":          multiAZ,
				"name":              decl.Name + "-rds",
			},
			DependsOn: decl.DependsOn,
		},
	}, nil
}

func (m *AWSCapabilityMapper) mapMessageQueue(decl platform.CapabilityDeclaration) ([]platform.ResourcePlan, error) {
	fifo := boolProp(decl.Properties, "fifo", false)
	visibilityTimeout := intProp(decl.Properties, "visibility_timeout", 30)
	retentionPeriod := intProp(decl.Properties, "retention_period", 345600)

	return []platform.ResourcePlan{
		{
			ResourceType: "aws.sqs",
			Name:         decl.Name + "-sqs",
			Properties: map[string]any{
				"fifo":               fifo,
				"visibility_timeout": visibilityTimeout,
				"retention_period":   retentionPeriod,
				"name":               decl.Name + "-sqs",
			},
			DependsOn: decl.DependsOn,
		},
	}, nil
}

func (m *AWSCapabilityMapper) mapContainerRuntime(decl platform.CapabilityDeclaration, pctx *platform.PlatformContext) ([]platform.ResourcePlan, error) {
	image, _ := decl.Properties["image"].(string)
	if image == "" {
		return nil, fmt.Errorf("aws: container_runtime capability requires 'image' property")
	}
	replicas := intProp(decl.Properties, "replicas", 1)
	memory, _ := decl.Properties["memory"].(string)
	cpu, _ := decl.Properties["cpu"].(string)

	// container_runtime maps to an EKS deployment, which needs a cluster
	deps := decl.DependsOn
	if pctx != nil {
		for name, out := range pctx.ParentOutputs {
			if out.Type == "kubernetes_cluster" {
				deps = append(deps, name)
				break
			}
		}
	}

	return []platform.ResourcePlan{
		{
			ResourceType: "aws.eks_nodegroup",
			Name:         decl.Name + "-deployment",
			Properties: map[string]any{
				"image":    image,
				"replicas": replicas,
				"memory":   memory,
				"cpu":      cpu,
				"name":     decl.Name + "-deployment",
			},
			DependsOn: deps,
		},
	}, nil
}

func (m *AWSCapabilityMapper) mapLoadBalancer(decl platform.CapabilityDeclaration, pctx *platform.PlatformContext) ([]platform.ResourcePlan, error) {
	scheme, _ := decl.Properties["scheme"].(string)
	if scheme == "" {
		scheme = "internet-facing"
	}

	deps := decl.DependsOn
	if pctx != nil {
		for name, out := range pctx.ParentOutputs {
			if out.Type == "network" {
				deps = append(deps, name)
				break
			}
		}
	}

	return []platform.ResourcePlan{
		{
			ResourceType: "aws.alb",
			Name:         decl.Name + "-alb",
			Properties: map[string]any{
				"scheme": scheme,
				"name":   decl.Name + "-alb",
			},
			DependsOn: deps,
		},
	}, nil
}

// Helper functions

func intProp(props map[string]any, key string, def int) int {
	v, ok := props[key]
	if !ok {
		return def
	}
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	case int64:
		return int(n)
	default:
		return def
	}
}

func boolProp(props map[string]any, key string, def bool) bool {
	v, ok := props[key]
	if !ok {
		return def
	}
	b, ok := v.(bool)
	if !ok {
		return def
	}
	return b
}

func checkConstraint(op string, actual, limit any) bool {
	actualF, aOK := toFloat(actual)
	limitF, lOK := toFloat(limit)
	if !aOK || !lOK {
		// For non-numeric, only == is supported
		if op == "==" {
			return fmt.Sprintf("%v", actual) == fmt.Sprintf("%v", limit)
		}
		return true // cannot evaluate, assume satisfied
	}
	switch op {
	case "<=":
		return actualF <= limitF
	case ">=":
		return actualF >= limitF
	case "==":
		return actualF == limitF
	default:
		return true
	}
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	default:
		return 0, false
	}
}
