//go:build aws

package drivers

import (
	"context"
	"testing"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"

	"github.com/GoCodeAlone/workflow/platform"
)

type mockIAMClient struct {
	createFunc          func(ctx context.Context, params *iam.CreateRoleInput, optFns ...func(*iam.Options)) (*iam.CreateRoleOutput, error)
	getFunc             func(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error)
	updatePolicyFunc    func(ctx context.Context, params *iam.UpdateAssumeRolePolicyInput, optFns ...func(*iam.Options)) (*iam.UpdateAssumeRolePolicyOutput, error)
	deleteFunc          func(ctx context.Context, params *iam.DeleteRoleInput, optFns ...func(*iam.Options)) (*iam.DeleteRoleOutput, error)
	attachFunc          func(ctx context.Context, params *iam.AttachRolePolicyInput, optFns ...func(*iam.Options)) (*iam.AttachRolePolicyOutput, error)
	detachFunc          func(ctx context.Context, params *iam.DetachRolePolicyInput, optFns ...func(*iam.Options)) (*iam.DetachRolePolicyOutput, error)
	listAttachedFunc    func(ctx context.Context, params *iam.ListAttachedRolePoliciesInput, optFns ...func(*iam.Options)) (*iam.ListAttachedRolePoliciesOutput, error)
}

func (m *mockIAMClient) CreateRole(ctx context.Context, params *iam.CreateRoleInput, optFns ...func(*iam.Options)) (*iam.CreateRoleOutput, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, params, optFns...)
	}
	return &iam.CreateRoleOutput{
		Role: &iamtypes.Role{
			RoleName: params.RoleName,
			Arn:      awsv2.String("arn:aws:iam::123456789:role/" + *params.RoleName),
			RoleId:   awsv2.String("AROA123456789"),
		},
	}, nil
}

func (m *mockIAMClient) GetRole(ctx context.Context, params *iam.GetRoleInput, optFns ...func(*iam.Options)) (*iam.GetRoleOutput, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, params, optFns...)
	}
	return &iam.GetRoleOutput{
		Role: &iamtypes.Role{
			RoleName:    params.RoleName,
			Arn:         awsv2.String("arn:aws:iam::123456789:role/" + *params.RoleName),
			RoleId:      awsv2.String("AROA123456789"),
			Description: awsv2.String("test role"),
		},
	}, nil
}

func (m *mockIAMClient) UpdateAssumeRolePolicy(ctx context.Context, params *iam.UpdateAssumeRolePolicyInput, optFns ...func(*iam.Options)) (*iam.UpdateAssumeRolePolicyOutput, error) {
	if m.updatePolicyFunc != nil {
		return m.updatePolicyFunc(ctx, params, optFns...)
	}
	return &iam.UpdateAssumeRolePolicyOutput{}, nil
}

func (m *mockIAMClient) DeleteRole(ctx context.Context, params *iam.DeleteRoleInput, optFns ...func(*iam.Options)) (*iam.DeleteRoleOutput, error) {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, params, optFns...)
	}
	return &iam.DeleteRoleOutput{}, nil
}

func (m *mockIAMClient) AttachRolePolicy(ctx context.Context, params *iam.AttachRolePolicyInput, optFns ...func(*iam.Options)) (*iam.AttachRolePolicyOutput, error) {
	if m.attachFunc != nil {
		return m.attachFunc(ctx, params, optFns...)
	}
	return &iam.AttachRolePolicyOutput{}, nil
}

func (m *mockIAMClient) DetachRolePolicy(ctx context.Context, params *iam.DetachRolePolicyInput, optFns ...func(*iam.Options)) (*iam.DetachRolePolicyOutput, error) {
	if m.detachFunc != nil {
		return m.detachFunc(ctx, params, optFns...)
	}
	return &iam.DetachRolePolicyOutput{}, nil
}

func (m *mockIAMClient) ListAttachedRolePolicies(ctx context.Context, params *iam.ListAttachedRolePoliciesInput, optFns ...func(*iam.Options)) (*iam.ListAttachedRolePoliciesOutput, error) {
	if m.listAttachedFunc != nil {
		return m.listAttachedFunc(ctx, params, optFns...)
	}
	return &iam.ListAttachedRolePoliciesOutput{
		AttachedPolicies: []iamtypes.AttachedPolicy{
			{PolicyArn: awsv2.String("arn:aws:iam::aws:policy/AmazonEKSClusterPolicy")},
		},
	}, nil
}

func TestIAMDriver_ResourceType(t *testing.T) {
	d := NewIAMDriverWithClient(&mockIAMClient{})
	if d.ResourceType() != "aws.iam_role" {
		t.Errorf("ResourceType() = %q, want aws.iam_role", d.ResourceType())
	}
}

func TestIAMDriver_Create(t *testing.T) {
	d := NewIAMDriverWithClient(&mockIAMClient{})
	ctx := context.Background()

	out, err := d.Create(ctx, "eks-role", map[string]any{
		"policy_arns": []string{"arn:aws:iam::aws:policy/AmazonEKSClusterPolicy"},
	})
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if out.Name != "eks-role" {
		t.Errorf("Name = %q, want eks-role", out.Name)
	}
	if out.Properties["arn"] != "arn:aws:iam::123456789:role/eks-role" {
		t.Errorf("arn = %v", out.Properties["arn"])
	}
}

func TestIAMDriver_Read(t *testing.T) {
	d := NewIAMDriverWithClient(&mockIAMClient{})
	ctx := context.Background()

	out, err := d.Read(ctx, "eks-role")
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	if out.Properties["description"] != "test role" {
		t.Errorf("description = %v, want test role", out.Properties["description"])
	}
}

func TestIAMDriver_Update(t *testing.T) {
	attached := []string{}
	detached := []string{}
	d := NewIAMDriverWithClient(&mockIAMClient{
		attachFunc: func(ctx context.Context, params *iam.AttachRolePolicyInput, optFns ...func(*iam.Options)) (*iam.AttachRolePolicyOutput, error) {
			attached = append(attached, *params.PolicyArn)
			return &iam.AttachRolePolicyOutput{}, nil
		},
		detachFunc: func(ctx context.Context, params *iam.DetachRolePolicyInput, optFns ...func(*iam.Options)) (*iam.DetachRolePolicyOutput, error) {
			detached = append(detached, *params.PolicyArn)
			return &iam.DetachRolePolicyOutput{}, nil
		},
	})
	ctx := context.Background()

	_, err := d.Update(ctx, "eks-role",
		map[string]any{"policy_arns": []string{"arn:old"}},
		map[string]any{"policy_arns": []string{"arn:new"}},
	)
	if err != nil {
		t.Fatalf("Update() error: %v", err)
	}
	if len(attached) != 1 || attached[0] != "arn:new" {
		t.Errorf("attached = %v, want [arn:new]", attached)
	}
	if len(detached) != 1 || detached[0] != "arn:old" {
		t.Errorf("detached = %v, want [arn:old]", detached)
	}
}

func TestIAMDriver_Delete(t *testing.T) {
	d := NewIAMDriverWithClient(&mockIAMClient{})
	ctx := context.Background()

	if err := d.Delete(ctx, "eks-role"); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}
}

func TestIAMDriver_Scale(t *testing.T) {
	d := NewIAMDriverWithClient(&mockIAMClient{})
	ctx := context.Background()

	_, err := d.Scale(ctx, "eks-role", nil)
	if err == nil {
		t.Fatal("expected NotScalableError")
	}
	if _, ok := err.(*platform.NotScalableError); !ok {
		t.Errorf("expected NotScalableError, got %T", err)
	}
}

func TestIAMDriver_HealthCheck(t *testing.T) {
	d := NewIAMDriverWithClient(&mockIAMClient{})
	ctx := context.Background()

	health, err := d.HealthCheck(ctx, "eks-role")
	if err != nil {
		t.Fatalf("HealthCheck() error: %v", err)
	}
	if health.Status != "healthy" {
		t.Errorf("health = %q, want healthy", health.Status)
	}
}

func TestDiffSlice(t *testing.T) {
	result := diffSlice([]string{"a", "b", "c"}, []string{"b", "d"})
	if len(result) != 2 {
		t.Fatalf("diffSlice length = %d, want 2", len(result))
	}
}
