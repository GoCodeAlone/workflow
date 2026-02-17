//go:build aws

package aws

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/platform"
)

func TestNewProvider(t *testing.T) {
	p := NewProvider()
	if p == nil {
		t.Fatal("NewProvider returned nil")
	}
	if p.Name() != "aws" {
		t.Errorf("Name() = %q, want %q", p.Name(), "aws")
	}
	if p.Version() != "0.1.0" {
		t.Errorf("Version() = %q, want %q", p.Version(), "0.1.0")
	}
}

func TestProvider_NotInitialized(t *testing.T) {
	p := NewProvider()
	ctx := context.Background()

	err := p.Healthy(ctx)
	if err != platform.ErrProviderNotInitialized {
		t.Errorf("Healthy before init: got %v, want ErrProviderNotInitialized", err)
	}

	_, err = p.MapCapability(ctx, platform.CapabilityDeclaration{Type: "network"}, nil)
	if err != platform.ErrProviderNotInitialized {
		t.Errorf("MapCapability before init: got %v, want ErrProviderNotInitialized", err)
	}
}

func TestProvider_Capabilities(t *testing.T) {
	p := NewProvider()
	caps := p.Capabilities()

	expected := map[string]bool{
		"kubernetes_cluster": false,
		"network":            false,
		"database":           false,
		"message_queue":      false,
		"container_runtime":  false,
		"load_balancer":      false,
	}

	for _, c := range caps {
		if _, ok := expected[c.Name]; ok {
			expected[c.Name] = true
		}
	}

	for name, found := range expected {
		if !found {
			t.Errorf("missing capability: %s", name)
		}
	}
}

func TestProvider_ResourceDriverNotFound(t *testing.T) {
	ap := &AWSProvider{
		initialized: true,
		drivers:     make(map[string]platform.ResourceDriver),
	}

	_, err := ap.ResourceDriver("aws.nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent driver")
	}
	if _, ok := err.(*platform.ResourceDriverNotFoundError); !ok {
		t.Errorf("expected ResourceDriverNotFoundError, got %T", err)
	}
}

func TestProvider_Close(t *testing.T) {
	ap := &AWSProvider{initialized: true}
	if err := ap.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
	if ap.initialized {
		t.Error("expected initialized=false after Close")
	}
}

func TestCapabilityMapper_CanMap(t *testing.T) {
	m := NewAWSCapabilityMapper()

	supported := []string{"kubernetes_cluster", "network", "database", "message_queue", "container_runtime", "load_balancer"}
	for _, cap := range supported {
		if !m.CanMap(cap) {
			t.Errorf("CanMap(%q) = false, want true", cap)
		}
	}

	if m.CanMap("unsupported_type") {
		t.Error("CanMap(unsupported_type) = true, want false")
	}
}

func TestCapabilityMapper_MapKubernetesCluster(t *testing.T) {
	m := NewAWSCapabilityMapper()
	decl := platform.CapabilityDeclaration{
		Name: "my-cluster",
		Type: "kubernetes_cluster",
		Properties: map[string]any{
			"version":       "1.28",
			"node_count":    3,
			"instance_type": "m5.large",
		},
	}

	plans, err := m.Map(decl, nil)
	if err != nil {
		t.Fatalf("Map() error: %v", err)
	}
	if len(plans) != 2 {
		t.Fatalf("expected 2 resource plans, got %d", len(plans))
	}

	// First should be EKS cluster
	if plans[0].ResourceType != "aws.eks_cluster" {
		t.Errorf("plan[0].ResourceType = %q, want %q", plans[0].ResourceType, "aws.eks_cluster")
	}
	if plans[0].Properties["version"] != "1.28" {
		t.Errorf("plan[0] version = %v, want 1.28", plans[0].Properties["version"])
	}

	// Second should be node group
	if plans[1].ResourceType != "aws.eks_nodegroup" {
		t.Errorf("plan[1].ResourceType = %q, want %q", plans[1].ResourceType, "aws.eks_nodegroup")
	}
	if plans[1].Properties["node_count"] != 3 {
		t.Errorf("plan[1] node_count = %v, want 3", plans[1].Properties["node_count"])
	}
	if plans[1].Properties["instance_type"] != "m5.large" {
		t.Errorf("plan[1] instance_type = %v, want m5.large", plans[1].Properties["instance_type"])
	}

	// Node group depends on cluster
	if len(plans[1].DependsOn) == 0 || plans[1].DependsOn[0] != "my-cluster-eks" {
		t.Errorf("plan[1].DependsOn = %v, want [my-cluster-eks]", plans[1].DependsOn)
	}
}

func TestCapabilityMapper_MapNetwork(t *testing.T) {
	m := NewAWSCapabilityMapper()
	decl := platform.CapabilityDeclaration{
		Name: "main-vpc",
		Type: "network",
		Properties: map[string]any{
			"cidr": "10.0.0.0/16",
		},
	}

	plans, err := m.Map(decl, nil)
	if err != nil {
		t.Fatalf("Map() error: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("expected 1 resource plan, got %d", len(plans))
	}
	if plans[0].ResourceType != "aws.vpc" {
		t.Errorf("ResourceType = %q, want aws.vpc", plans[0].ResourceType)
	}
	if plans[0].Properties["cidr"] != "10.0.0.0/16" {
		t.Errorf("cidr = %v, want 10.0.0.0/16", plans[0].Properties["cidr"])
	}
}

func TestCapabilityMapper_MapNetworkMissingCIDR(t *testing.T) {
	m := NewAWSCapabilityMapper()
	decl := platform.CapabilityDeclaration{
		Name:       "main-vpc",
		Type:       "network",
		Properties: map[string]any{},
	}

	_, err := m.Map(decl, nil)
	if err == nil {
		t.Fatal("expected error for missing CIDR")
	}
}

func TestCapabilityMapper_MapDatabase(t *testing.T) {
	m := NewAWSCapabilityMapper()
	decl := platform.CapabilityDeclaration{
		Name: "app-db",
		Type: "database",
		Properties: map[string]any{
			"engine":            "postgres",
			"engine_version":    "15.4",
			"instance_class":    "db.r5.large",
			"allocated_storage": 100,
			"multi_az":          true,
		},
	}

	plans, err := m.Map(decl, nil)
	if err != nil {
		t.Fatalf("Map() error: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("expected 1 resource plan, got %d", len(plans))
	}
	if plans[0].ResourceType != "aws.rds" {
		t.Errorf("ResourceType = %q, want aws.rds", plans[0].ResourceType)
	}
	if plans[0].Properties["engine"] != "postgres" {
		t.Errorf("engine = %v, want postgres", plans[0].Properties["engine"])
	}
	if plans[0].Properties["multi_az"] != true {
		t.Errorf("multi_az = %v, want true", plans[0].Properties["multi_az"])
	}
}

func TestCapabilityMapper_MapMessageQueue(t *testing.T) {
	m := NewAWSCapabilityMapper()
	decl := platform.CapabilityDeclaration{
		Name: "order-queue",
		Type: "message_queue",
		Properties: map[string]any{
			"fifo":               true,
			"visibility_timeout": 60,
		},
	}

	plans, err := m.Map(decl, nil)
	if err != nil {
		t.Fatalf("Map() error: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("expected 1 plan, got %d", len(plans))
	}
	if plans[0].ResourceType != "aws.sqs" {
		t.Errorf("ResourceType = %q, want aws.sqs", plans[0].ResourceType)
	}
	if plans[0].Properties["fifo"] != true {
		t.Errorf("fifo = %v, want true", plans[0].Properties["fifo"])
	}
}

func TestCapabilityMapper_MapContainerRuntime(t *testing.T) {
	m := NewAWSCapabilityMapper()
	decl := platform.CapabilityDeclaration{
		Name: "api-service",
		Type: "container_runtime",
		Properties: map[string]any{
			"image":    "myapp:latest",
			"replicas": 3,
			"memory":   "512Mi",
		},
	}

	plans, err := m.Map(decl, nil)
	if err != nil {
		t.Fatalf("Map() error: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("expected 1 plan, got %d", len(plans))
	}
	if plans[0].Properties["image"] != "myapp:latest" {
		t.Errorf("image = %v, want myapp:latest", plans[0].Properties["image"])
	}
}

func TestCapabilityMapper_MapContainerRuntimeMissingImage(t *testing.T) {
	m := NewAWSCapabilityMapper()
	decl := platform.CapabilityDeclaration{
		Name:       "api-service",
		Type:       "container_runtime",
		Properties: map[string]any{},
	}

	_, err := m.Map(decl, nil)
	if err == nil {
		t.Fatal("expected error for missing image")
	}
}

func TestCapabilityMapper_MapLoadBalancer(t *testing.T) {
	m := NewAWSCapabilityMapper()
	decl := platform.CapabilityDeclaration{
		Name: "api-lb",
		Type: "load_balancer",
		Properties: map[string]any{
			"scheme": "internal",
		},
	}

	plans, err := m.Map(decl, nil)
	if err != nil {
		t.Fatalf("Map() error: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("expected 1 plan, got %d", len(plans))
	}
	if plans[0].ResourceType != "aws.alb" {
		t.Errorf("ResourceType = %q, want aws.alb", plans[0].ResourceType)
	}
	if plans[0].Properties["scheme"] != "internal" {
		t.Errorf("scheme = %v, want internal", plans[0].Properties["scheme"])
	}
}

func TestCapabilityMapper_UnsupportedType(t *testing.T) {
	m := NewAWSCapabilityMapper()
	decl := platform.CapabilityDeclaration{
		Name: "x",
		Type: "magic_service",
	}

	_, err := m.Map(decl, nil)
	if err == nil {
		t.Fatal("expected error for unsupported type")
	}
	if _, ok := err.(*platform.CapabilityUnsupportedError); !ok {
		t.Errorf("expected CapabilityUnsupportedError, got %T", err)
	}
}

func TestCapabilityMapper_ValidateConstraints(t *testing.T) {
	m := NewAWSCapabilityMapper()
	decl := platform.CapabilityDeclaration{
		Name: "test",
		Type: "database",
		Properties: map[string]any{
			"allocated_storage": 200,
			"replicas":          5,
		},
	}
	constraints := []platform.Constraint{
		{Field: "allocated_storage", Operator: "<=", Value: 100, Source: "tier1"},
		{Field: "replicas", Operator: "<=", Value: 10, Source: "tier1"},
	}

	violations := m.ValidateConstraints(decl, constraints)
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].Constraint.Field != "allocated_storage" {
		t.Errorf("violation field = %q, want allocated_storage", violations[0].Constraint.Field)
	}
}

func TestCheckConstraint(t *testing.T) {
	tests := []struct {
		op     string
		actual any
		limit  any
		want   bool
	}{
		{"<=", 5, 10, true},
		{"<=", 10, 10, true},
		{"<=", 11, 10, false},
		{">=", 10, 5, true},
		{">=", 5, 5, true},
		{">=", 4, 5, false},
		{"==", 5, 5, true},
		{"==", 5, 6, false},
		{"==", "foo", "foo", true},
		{"==", "foo", "bar", false},
	}
	for _, tt := range tests {
		got := checkConstraint(tt.op, tt.actual, tt.limit)
		if got != tt.want {
			t.Errorf("checkConstraint(%q, %v, %v) = %v, want %v", tt.op, tt.actual, tt.limit, got, tt.want)
		}
	}
}

func TestHelperFunctions(t *testing.T) {
	props := map[string]any{
		"count":   3,
		"enabled": true,
		"name":    "test",
	}

	if v := intProp(props, "count", 0); v != 3 {
		t.Errorf("intProp(count) = %d, want 3", v)
	}
	if v := intProp(props, "missing", 42); v != 42 {
		t.Errorf("intProp(missing) = %d, want 42", v)
	}
	if v := boolProp(props, "enabled", false); v != true {
		t.Errorf("boolProp(enabled) = %v, want true", v)
	}
	if v := boolProp(props, "missing", true); v != true {
		t.Errorf("boolProp(missing) = %v, want true", v)
	}
}

func TestSplitContextPath(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"acme/prod/api", []string{"acme", "prod", "api"}},
		{"acme/prod", []string{"acme", "prod"}},
		{"single", []string{"single"}},
		{"", nil},
	}
	for _, tt := range tests {
		got := splitContextPath(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("splitContextPath(%q) = %v, want %v", tt.input, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("splitContextPath(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}
