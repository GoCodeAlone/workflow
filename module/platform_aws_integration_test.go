//go:build integration

package module_test

import (
	"os"
	"testing"

	"github.com/GoCodeAlone/workflow/module"
)

// newAWSCloudAccount creates a cloud.account backed by environment credentials.
func newAWSCloudAccount(t *testing.T, name string) *module.CloudAccount {
	t.Helper()
	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = "us-east-1"
	}
	acc := module.NewCloudAccount(name, map[string]any{
		"provider": "aws",
		"region":   region,
		"credentials": map[string]any{
			"type": "env",
		},
	})
	app := module.NewMockApplication()
	if err := acc.Init(app); err != nil {
		t.Fatalf("cloud account Init: %v", err)
	}
	return acc
}

// TestEKS_Integration_Plan verifies that EKS plan() can call DescribeCluster
// against a real AWS account and return a valid plan.
func TestEKS_Integration_Plan(t *testing.T) {
	acc := newAWSCloudAccount(t, "aws-account")
	app := module.NewMockApplication()

	k := module.NewPlatformKubernetes("integration-cluster", map[string]any{
		"type":    "eks",
		"version": "1.29",
		"account": "aws-account",
	})
	if err := acc.Init(app); err != nil {
		t.Fatalf("account Init: %v", err)
	}
	if err := k.Init(app); err != nil {
		t.Fatalf("k8s Init: %v", err)
	}

	plan, err := k.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if plan.Provider != "eks" {
		t.Errorf("expected provider=eks, got %q", plan.Provider)
	}
	if len(plan.Actions) == 0 {
		t.Error("expected at least one action")
	}
	t.Logf("EKS plan: %+v", plan.Actions)
}

// TestECS_Integration_Plan verifies that ECS plan() can call DescribeServices
// against a real AWS account.
func TestECS_Integration_Plan(t *testing.T) {
	clusterName := os.Getenv("ECS_CLUSTER_NAME")
	if clusterName == "" {
		clusterName = "default"
	}

	acc := newAWSCloudAccount(t, "aws-account")
	app := module.NewMockApplication()
	if err := acc.Init(app); err != nil {
		t.Fatalf("account Init: %v", err)
	}

	e := module.NewPlatformECS("integration-svc", map[string]any{
		"cluster": clusterName,
		"account": "aws-account",
	})
	if err := e.Init(app); err != nil {
		t.Fatalf("ECS Init: %v", err)
	}

	plan, err := e.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Actions) == 0 {
		t.Error("expected at least one action")
	}
	t.Logf("ECS plan: %+v", plan.Actions)
}

// TestNetworking_Integration_Plan verifies aws network plan() calls DescribeVpcs.
func TestNetworking_Integration_Plan(t *testing.T) {
	acc := newAWSCloudAccount(t, "aws-account")
	app := module.NewMockApplication()
	if err := acc.Init(app); err != nil {
		t.Fatalf("account Init: %v", err)
	}

	net := module.NewPlatformNetworking("integration-net", map[string]any{
		"provider": "aws",
		"account":  "aws-account",
		"vpc": map[string]any{
			"cidr": "10.0.0.0/16",
			"name": "integration-test-vpc",
		},
	})
	if err := net.Init(app); err != nil {
		t.Fatalf("network Init: %v", err)
	}

	plan, err := net.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Changes) == 0 {
		t.Error("expected at least one change")
	}
	t.Logf("Network plan: %+v", plan.Changes)
}

// TestDNS_Integration_Plan verifies route53 plan() calls ListHostedZonesByName.
func TestDNS_Integration_Plan(t *testing.T) {
	acc := newAWSCloudAccount(t, "aws-account")
	app := module.NewMockApplication()
	if err := acc.Init(app); err != nil {
		t.Fatalf("account Init: %v", err)
	}

	dns := module.NewPlatformDNS("integration-dns", map[string]any{
		"provider": "aws",
		"account":  "aws-account",
		"zone": map[string]any{
			"name": "integration.example.com",
		},
	})
	if err := dns.Init(app); err != nil {
		t.Fatalf("DNS Init: %v", err)
	}

	plan, err := dns.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Changes) == 0 {
		t.Error("expected at least one change")
	}
	t.Logf("DNS plan: %+v", plan.Changes)
}
