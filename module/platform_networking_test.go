package module_test

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/module"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

func newTestNetworkConfig() map[string]any {
	return map[string]any{
		"provider": "mock",
		"vpc": map[string]any{
			"cidr": "10.0.0.0/16",
			"name": "test-vpc",
		},
		"subnets": []any{
			map[string]any{
				"name":   "public-a",
				"cidr":   "10.0.1.0/24",
				"az":     "us-east-1a",
				"public": true,
			},
			map[string]any{
				"name":   "private-a",
				"cidr":   "10.0.10.0/24",
				"az":     "us-east-1a",
				"public": false,
			},
		},
		"nat_gateway": true,
		"security_groups": []any{
			map[string]any{
				"name": "web",
				"rules": []any{
					map[string]any{
						"protocol": "tcp",
						"port":     float64(443),
						"source":   "0.0.0.0/0",
					},
					map[string]any{
						"protocol": "tcp",
						"port":     float64(80),
						"source":   "0.0.0.0/0",
					},
				},
			},
		},
	}
}

func setupNetworkApp(t *testing.T) (*module.MockApplication, *module.PlatformNetworking) {
	t.Helper()
	app := module.NewMockApplication()
	m := module.NewPlatformNetworking("prod-network", newTestNetworkConfig())
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return app, m
}

// ─── module tests ─────────────────────────────────────────────────────────────

func TestPlatformNetworking_Init(t *testing.T) {
	app := module.NewMockApplication()
	m := module.NewPlatformNetworking("prod-network", newTestNetworkConfig())
	if err := m.Init(app); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if m.Name() != "prod-network" {
		t.Errorf("expected name=prod-network, got %q", m.Name())
	}
	// Module should register itself in the service registry
	if _, ok := app.Services["prod-network"]; !ok {
		t.Error("expected prod-network in service registry")
	}
}

func TestPlatformNetworking_Plan(t *testing.T) {
	_, m := setupNetworkApp(t)

	plan, err := m.Plan()
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}
	if plan.VPC.CIDR != "10.0.0.0/16" {
		t.Errorf("expected vpc cidr=10.0.0.0/16, got %q", plan.VPC.CIDR)
	}
	if plan.VPC.Name != "test-vpc" {
		t.Errorf("expected vpc name=test-vpc, got %q", plan.VPC.Name)
	}
	if len(plan.Subnets) != 2 {
		t.Errorf("expected 2 subnets, got %d", len(plan.Subnets))
	}
	if !plan.NATGateway {
		t.Error("expected nat_gateway=true")
	}
	if len(plan.SecurityGroups) != 1 {
		t.Errorf("expected 1 security group, got %d", len(plan.SecurityGroups))
	}
	if len(plan.SecurityGroups[0].Rules) != 2 {
		t.Errorf("expected 2 security group rules, got %d", len(plan.SecurityGroups[0].Rules))
	}
	if len(plan.Changes) == 0 {
		t.Error("expected non-empty changes list")
	}
}

func TestPlatformNetworking_Apply(t *testing.T) {
	_, m := setupNetworkApp(t)

	state, err := m.Apply()
	if err != nil {
		t.Fatalf("Apply failed: %v", err)
	}
	if state.Status != "active" {
		t.Errorf("expected status=active, got %q", state.Status)
	}
	if state.VPCID == "" {
		t.Error("expected non-empty VPCID after apply")
	}
	if len(state.SubnetIDs) != 2 {
		t.Errorf("expected 2 subnet IDs, got %d", len(state.SubnetIDs))
	}
	if state.SubnetIDs["public-a"] == "" {
		t.Error("expected public-a subnet ID to be set")
	}
	if state.SubnetIDs["private-a"] == "" {
		t.Error("expected private-a subnet ID to be set")
	}
	if state.NATGatewayID == "" {
		t.Error("expected non-empty NAT gateway ID after apply")
	}
	if state.SecurityGroupIDs["web"] == "" {
		t.Error("expected web security group ID to be set")
	}
}

func TestPlatformNetworking_Status(t *testing.T) {
	_, m := setupNetworkApp(t)

	if _, err := m.Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	st, err := m.Status()
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	state, ok := st.(*module.NetworkState)
	if !ok {
		t.Fatalf("Status returned unexpected type %T", st)
	}
	if state.Status != "active" {
		t.Errorf("expected status=active, got %q", state.Status)
	}
}

func TestPlatformNetworking_PlanAfterApply_NoChanges(t *testing.T) {
	_, m := setupNetworkApp(t)

	if _, err := m.Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	plan, err := m.Plan()
	if err != nil {
		t.Fatalf("second Plan: %v", err)
	}
	// After apply the network is active; plan should show noop
	if len(plan.Changes) == 0 {
		t.Error("expected at least one change entry")
	}
	if plan.Changes[0] != "noop: network already active" {
		t.Errorf("expected noop change, got %q", plan.Changes[0])
	}
}

func TestPlatformNetworking_Destroy(t *testing.T) {
	_, m := setupNetworkApp(t)

	if _, err := m.Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if err := m.Destroy(); err != nil {
		t.Fatalf("Destroy failed: %v", err)
	}

	st, err := m.Status()
	if err != nil {
		t.Fatalf("Status after destroy: %v", err)
	}
	state := st.(*module.NetworkState)
	if state.Status != "destroyed" {
		t.Errorf("expected status=destroyed, got %q", state.Status)
	}
	if state.VPCID != "" {
		t.Error("expected VPCID to be empty after destroy")
	}
}

func TestPlatformNetworking_ApplyIdempotent(t *testing.T) {
	_, m := setupNetworkApp(t)

	state1, err := m.Apply()
	if err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	state2, err := m.Apply()
	if err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	// Second apply should return the same VPCID
	if state1.VPCID != state2.VPCID {
		t.Errorf("expected same VPCID on second apply, got %q vs %q", state1.VPCID, state2.VPCID)
	}
}

func TestPlatformNetworking_InvalidConfig_MissingVPCCIDR(t *testing.T) {
	app := module.NewMockApplication()
	m := module.NewPlatformNetworking("bad-net", map[string]any{
		"provider": "mock",
		"vpc": map[string]any{
			"name": "no-cidr",
		},
	})
	if err := m.Init(app); err == nil {
		t.Error("expected error for missing vpc.cidr, got nil")
	}
}

func TestPlatformNetworking_InvalidProvider(t *testing.T) {
	app := module.NewMockApplication()
	m := module.NewPlatformNetworking("bad-net", map[string]any{
		"provider": "digitalocean",
		"vpc":      map[string]any{"cidr": "10.0.0.0/16"},
	})
	if err := m.Init(app); err == nil {
		t.Error("expected error for unsupported provider, got nil")
	}
}

func TestPlatformNetworking_CloudAccountResolution(t *testing.T) {
	app := module.NewMockApplication()
	acc := module.NewCloudAccount("aws-prod", map[string]any{
		"provider": "mock",
		"region":   "us-east-1",
	})
	if err := acc.Init(app); err != nil {
		t.Fatalf("cloud account Init: %v", err)
	}

	cfg := newTestNetworkConfig()
	cfg["account"] = "aws-prod"
	m := module.NewPlatformNetworking("net-with-account", cfg)
	if err := m.Init(app); err != nil {
		t.Fatalf("networking Init: %v", err)
	}
}

func TestPlatformNetworking_InvalidAccount(t *testing.T) {
	app := module.NewMockApplication()
	cfg := newTestNetworkConfig()
	cfg["account"] = "nonexistent"
	m := module.NewPlatformNetworking("bad-net", cfg)
	if err := m.Init(app); err == nil {
		t.Error("expected error for nonexistent account, got nil")
	}
}

func TestPlatformNetworking_AWSStubPlan(t *testing.T) {
	app := module.NewMockApplication()
	m := module.NewPlatformNetworking("aws-net", map[string]any{
		"provider": "aws",
		"vpc":      map[string]any{"cidr": "10.0.0.0/16", "name": "aws-vpc"},
	})
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	plan, err := m.Plan()
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Changes) == 0 {
		t.Fatal("expected at least one change")
	}
}

func TestPlatformNetworking_AWSApplyNotImplemented(t *testing.T) {
	app := module.NewMockApplication()
	m := module.NewPlatformNetworking("aws-net", map[string]any{
		"provider": "aws",
		"vpc":      map[string]any{"cidr": "10.0.0.0/16"},
	})
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, err := m.Apply(); err == nil {
		t.Error("expected error from AWS Apply stub, got nil")
	}
}

// ─── pipeline step tests ──────────────────────────────────────────────────────

func TestNetworkPlanStep(t *testing.T) {
	app, _ := setupNetworkApp(t)
	factory := module.NewNetworkPlanStepFactory()
	step, err := factory("plan", map[string]any{"network": "prod-network"}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["network"] != "prod-network" {
		t.Errorf("expected network=prod-network, got %v", result.Output["network"])
	}
	if result.Output["changes"] == nil {
		t.Error("expected changes in output")
	}
	if result.Output["vpc"] == nil {
		t.Error("expected vpc in output")
	}
}

func TestNetworkApplyStep(t *testing.T) {
	app, _ := setupNetworkApp(t)
	factory := module.NewNetworkApplyStepFactory()
	step, err := factory("apply", map[string]any{"network": "prod-network"}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["status"] != "active" {
		t.Errorf("expected status=active, got %v", result.Output["status"])
	}
	if result.Output["vpcId"] == "" {
		t.Error("expected non-empty vpcId in output")
	}
}

func TestNetworkStatusStep(t *testing.T) {
	app, m := setupNetworkApp(t)

	if _, err := m.Apply(); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	factory := module.NewNetworkStatusStepFactory()
	step, err := factory("status", map[string]any{"network": "prod-network"}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["network"] != "prod-network" {
		t.Errorf("expected network=prod-network, got %v", result.Output["network"])
	}
	st := result.Output["status"].(*module.NetworkState)
	if st.Status != "active" {
		t.Errorf("expected status=active, got %q", st.Status)
	}
}

func TestNetworkPlanStep_MissingNetwork(t *testing.T) {
	factory := module.NewNetworkPlanStepFactory()
	_, err := factory("plan", map[string]any{}, module.NewMockApplication())
	if err == nil {
		t.Error("expected error for missing network, got nil")
	}
}

func TestNetworkPlanStep_NetworkNotFound(t *testing.T) {
	factory := module.NewNetworkPlanStepFactory()
	step, err := factory("plan", map[string]any{"network": "ghost"}, module.NewMockApplication())
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	_, err = step.Execute(context.Background(), &module.PipelineContext{Current: map[string]any{}})
	if err == nil {
		t.Error("expected error for missing network service, got nil")
	}
}

func TestNetworkApplyStep_MissingNetwork(t *testing.T) {
	factory := module.NewNetworkApplyStepFactory()
	_, err := factory("apply", map[string]any{}, module.NewMockApplication())
	if err == nil {
		t.Error("expected error for missing network, got nil")
	}
}

func TestNetworkStatusStep_MissingNetwork(t *testing.T) {
	factory := module.NewNetworkStatusStepFactory()
	_, err := factory("status", map[string]any{}, module.NewMockApplication())
	if err == nil {
		t.Error("expected error for missing network, got nil")
	}
}
